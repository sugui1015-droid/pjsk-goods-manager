# PostgreSQL 备份保留策略与安全清理

本文定义本项目正式备份的保留策略、备份状态分类、删除候选条件与运行原则，配合 `scripts/database/` 下的**只读扫描报告**脚本与**默认 DryRun** 清理脚本使用。备份产物结构见 [database-backup-restore.md](database-backup-restore.md)。

> 第一版原则：**默认只报告、不删除**。所有删除能力都需要显式多重确认；默认路径绝不指向现有演练证据目录。

---

## 1. 备份集合与文件约定

一个"备份集合"由同一 `basename` 的文件组成，位于备份根目录（`-BackupRoot`）下：

| 文件 | 说明 | 必需 |
| --- | --- | --- |
| `<basename>.dump` | 主备份（`pg_dump` custom format） | 是 |
| `<basename>.metadata.json` | 元数据（含 `createdAtUtc`、`dumpSha256`、`isolatedTestBackup` 等） | 是 |
| `<basename>.validation.json` | 恢复验证旁文件（`overallResult`、`dumpSha256`），**约定放在集合旁** | 否（缺失即视为未验证） |
| `<basename>.dump.partial` / `<basename>.metadata.json.partial` | 写入中/失败残留 | 出现即异常 |

正式备份 `basename` 形如 `pjsk-yyyyMMdd-HHmmss`；分层保留按元数据 `createdAtUtc`（权威 UTC 时间）计算，不用文件修改时间。

### 验证旁文件约定（`<basename>.validation.json`）

元数据本身不记录"恢复是否成功"。要让某备份被判为 `verified`（从而可能进入删除分层），运营者需在该备份集合旁放置：

```json
{
  "overallResult": "passed",
  "dumpSha256": "<与该备份 metadata.dumpSha256 相同的大写十六进制>"
}
```

- `overallResult` 必须为 `passed`；`dumpSha256` 必须与元数据一致（防止验证旁文件张冠李戴）。
- **没有该旁文件、或不匹配、或 `overallResult != passed`** → 备份为 `unverified` / `validation-failed`，**受保护，永不成为删除候选**。
- 恢复演练目录里那种 `<uniqueid>.validation.json`（名字与 dump basename 不同）不会被误认为某正式备份的验证，且演练集合本身按 `isolatedTestBackup=true` 一律受保护。

---

## 2. 备份状态分类

| 状态 | 判断依据 | 默认处置 |
| --- | --- | --- |
| `verified` | dump+metadata 齐全、元数据可解析、（`-VerifyHash` 时）实际 SHA-256 与元数据一致、有匹配的 `passed` 验证旁文件、无 `.partial`、非演练证据 | 参与分层保留；可能成为候选 |
| `unverified` | dump+metadata 齐全且可解析，但缺验证旁文件或未算哈希 | **保护**，永不候选 |
| `validation-failed` | 验证旁文件存在但 `overallResult != passed` 或哈希不符 | **保护**，报警 |
| `incomplete` | 存在 `.partial`，或 metadata 有而 dump 缺（或反之） | **保护**，报异常 |
| `orphan` | 只有 dump 无 metadata，或只有 validation 无 dump/metadata | **保护** |
| `unknown` | metadata/validation JSON 无法解析、reparse point、访问被拒 | **保护** |
| `protected` | 命中保护规则（最新 verified、保留窗口/分层点、最少验证份数、用户保护列表、演练证据 `isolatedTestBackup=true`） | **保护** |

`unverified`/`validation-failed`/`incomplete`/`orphan`/`unknown` 均**不会**被自动删除。

---

## 3. 删除候选条件（必须同时满足全部）

一个备份集合只有**同时满足以下全部**才可成为删除候选：

1. 位于明确允许的 `-BackupRoot` 之下（通过路径安全守卫）。
2. 文件命名与 metadata 可解析。
3. 主 dump 存在。
4. metadata 存在。
5. 验证旁文件存在且 `overallResult=passed`（即状态 `verified`）。
6. SHA-256 一致（需 `-VerifyHash`；未算哈希则不作候选）。
7. 不存在任何 `.partial`。
8. 不在最近保护窗口（`-KeepAllDays`）。
9. 不是每日/每周/每月保留分层点。
10. 不是最新一份成功（verified）备份。
11. 不在用户 `-ProtectName` 保护列表。
12. 未被锁定或近期修改（最后修改早于安全余量，默认 15 分钟）。
13. 非演练证据（`isolatedTestBackup=false`）。
14. 不是未知文件/未知目录。

保留分层（按 `createdAtUtc`，可用 `-Now` 固定以便离线可重复测试）：

- **最近 `-KeepAllDays`（默认 7）天**：全部保留。
- **`-DailyDays`（默认 30）天内**：每天保留当天最新一份 verified。
- **`-WeeklyWeeks`（默认 8）周内**：每 ISO 周保留最新一份 verified。
- **`-MonthlyMonths`（默认 12）月内**：每月保留最新一份 verified。
- **`-MinimumVerifiedBackups`（默认 3）**：无论时间，最近的这么多份 verified 永远保护。
- **最新一份成功备份、最新一份成功恢复验证证据**：永远保护。

只有落在所有保留分层之外、且满足全部 14 项的 `verified` 备份，才标为 `Candidate`。

---

## 4. 第一版运行原则

- 默认**只生成报告**；默认**不删除、不移动、不压缩、不改 metadata、不修复损坏集合、不清理演练证据**。
- 正式删除必须显式指定参数（见 §5 与清理脚本）。
- 删除前**重新扫描**，逐项复核候选仍满足全部条件，避免"扫描后状态变化"。
- 任何**未知对象一律保留**并报告，不因"看起来旧"而删除。

---

## 5. 决策状态与清理脚本多重保护

扫描报告对每个集合给出决策：`Keep` / `Candidate` / `Protected` / `Skipped` / `Error`。

清理脚本 `Remove-ExpiredPostgresBackups.ps1`（默认 `-DryRun`）真正执行删除，**必须同时满足**：

1. 显式 `-Execute`（且 `-DryRun` 未被设为 `$true`）。
2. 显式 `-BackupRoot`（绝对路径，通过路径守卫）。
3. 显式 `-ExpectedRootName`（末段目录名必须精确匹配，防止指错根）。
4. 显式确认短语 `-ConfirmationText "DELETE_EXPIRED_PJSK_BACKUPS"`。
5. 重新扫描报告无 `Error` 级严重问题。
6. 每个候选在删除前逐项复核：仍在允许根内、文件未变、SHA-256 状态未变、验证状态未变、无新 `.partial`。

- 仅 `-Confirm:$false` **不能**绕过保护；仅 `-Execute` 而缺其他条件**不删除**。
- 只删除已明确识别的候选**文件**（dump/metadata/validation）；删除其空目录前确认目录已空且属已识别集合。
- **不** `Remove-Item -Recurse` 删除未逐项确认的未知目录；不绕过回收站做底层删除。
- 默认**不删除报告文件**；本版**不实现**清理演练证据的执行能力。
- 逐个动作输出审计记录；任一删除失败继续保护其他对象并最终返回非零退出码。

---

## 6. 建议调度（仅建议，不创建计划任务）

- 每次备份完成后运行**只读**保留报告（`Get-PostgresBackupRetentionReport.ps1`），归档 JSON/CSV。
- 每周人工审阅报告与 `Candidate` 列表。
- 成熟后再考虑自动执行；自动执行前应先有一段时间的 **DryRun 观察记录**。

---

## 7. 目录与产物区分（务必分清）

| 类别 | 位置/命名 | 保留工具处置 |
| --- | --- | --- |
| 正式备份 | `-BackupRoot` 下 `pjsk-*.dump` + metadata，`isolatedTestBackup=false` | 按本策略分层，仅 verified 可能候选 |
| 恢复演练证据 | 如 `D:\pjsk-backup-restore-tests\...`，`isolatedTestBackup=true` | 一律 `protected`，永不删除 |
| 临时测试 fixture | 安全测试在仓库外临时目录创建 | 仅测试自身清理 |
| 报告文件 | `*.retention-report.json/csv` | 默认不删除 |
| 删除审计文件 | `*.retention-audit.json` | 默认不删除 |

**所有真实备份、演练证据、metadata、validation 都不进入 Git，也不上传公网。**
