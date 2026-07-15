# 正式备份验证闭环与历史 metadata 兼容性收口

## 任务边界

- 日期：2026-07-15
- 目标：(1) `.validation.json` 的正式生成、校验与发布闭环；(2) retention 读取历史 metadata 时对 `isolatedTestBackup` 的严格布尔兼容。
- 全程不连接任何数据库，不执行真实 `pg_dump`/`pg_restore`/`psql`，不操作真实备份目录，不读取真实 `.env` 或密钥，不改迁移器/Go 启动超时/历史迁移，不做正式库核对，不改系统配置，不用子代理。
- 本阶段不提交、不推送。

## 阶段 1：只读接管检查

- 开始时间：2026-07-15 16:20（本机时间）
- 分支 `main`；`git status --short` 为空（工作区、暂存区干净）；无未跟踪文件。
- `git rev-parse HEAD` = `git rev-parse origin/main` = `ce6cb570873bf918cacbf4ffe2e63da026ca021c`，与交接基线一致。
- `git log -5 --oneline`：`ce6cb57` / `58e7a54` / `c45e85c` / `ab25adb` / `4edb307`，与基线一致。
- 已阅读：`AGENTS.md`、`HANDOVER.md`、`docs/database-backup-restore.md`、`docs/development-logs/2026-07-15-pre-deployment-offline-risk-cleanup.md`、`scripts/database/` 下 `Backup-Postgres.ps1`、`Test-PostgresBackup.ps1`、`Restore-PostgresTest.ps1`、`Invoke-ScriptSafetyTests.ps1`、`Invoke-RetentionSafetyTests.ps1`、`_RetentionCommon.ps1`、`_BackupMetadata.ps1`、`Invoke-BackupPublishTests.ps1`。
- 结论：基线一致，可继续。
- 是否连接数据库：否。是否执行真实工具：否。是否读取敏感信息：否。是否使用子代理：否。

## 阶段 2：`.validation.json` 职责调查

### 2.1 十二个问题的准确答案

1. **谁读取**：**只有 `_RetentionCommon.ps1`**（`:92` 组装路径，`:121-133` 解析）。`Get-PostgresBackupRetentionReport.ps1` 与 `Remove-ExpiredPostgresBackups.ps1` 都经由它间接使用。`Invoke-RetentionSafetyTests.ps1:59-64` 只是造 fixture。**没有任何生产写入者。**
2. **哪些字段决定 `verified`**：只有两个——**`overallResult`**（必须恰为字符串 `'passed'`）与 **`dumpSha256`**。`:126`：
   ```powershell
   if ($ovr -eq 'passed' -and $metaSha -and $valSha -and ($valSha -ieq $metaSha)) { $validationResult = 'passed' }
   elseif ($ovr -eq 'passed') { $validationResult = 'passed-hash-mismatch' }
   else { $validationResult = 'failed' }
   ```
   **关键**：任务书示例中的 `"status": "verified"` **不会被读取**——retention 只认 `overallResult`。若照抄示例，`$ovr` 为空 → 落入 `else` → `failed` → 状态变 `validation-failed`，备份永远无法成为 `verified`，闭环静默失效。故 schema 必须对齐 `overallResult` + `dumpSha256`。
3. **文件名配对**：retention 以 **metadata 文件名**为锚——扫描 `*.metadata.json`，取 `$base = <name>.metadata.json` 去掉后缀，同目录下拼 `$base + '.validation.json'` 与 `$base + '.dump'`。因此三者必须同目录、同 base 名。
4. **validation 缺失**：`$validationResult` 保持 `'none'` → 不匹配 `failed` 系列，也不满足 `'passed'` → 落入 `:162` `else` → `$status = 'unverified'` → `:242` `if ($s.Status -ne 'verified') { Decision = 'Protected' }` → **永不自动删除**。安全默认。
5. **内容错误/哈希不一致**：`overallResult` 非 `passed` → `failed`；`passed` 但 `dumpSha256` 与 metadata 不符 → `passed-hash-mismatch`；JSON 解析失败 → `unparseable`。三者在 `:155` 均归为 `validation-failed` → Protected，**永不自动删除**。
6. **`Test-PostgresBackup.ps1` 现状**：内部**已有结构化结果**（`Get-DatabaseProfile` 返回含 `migrationMax`/`migrationCount`/`tableCount`/`primaryKeys`/`foreignKeys`/`indexes`/`sequences`/`pgcrypto`/`uuidFunction`/`rowCounts` 的 ordered hashtable），并用 `Assert` 累积 `$script:failed`，但**只打印 PASS/FAIL，不落盘**，最后 `exit 0/1`。
7/8. **谁负责写入**：应由 **`Test-PostgresBackup.ps1`** 写入。它已持有全部验证事实与成败状态；新增独立验证脚本会重复执行恢复校验、引入第二套真相来源。但它当前**不知道 dump 路径**（只收 `-RestoredDatabase`/`-SourceDatabase`），因此需要新增一个 `-BackupFile` 参数才能定位并绑定 dump/metadata/validation 三者。
9. **何时发布**：**全部验证通过之后**才发布 final。先写 `.validation.json.partial`，回读校验，再原子改名——与 `Backup-Postgres.ps1` 既有模式一致。
10. **失败是否留报告**：应该留，但**绝不能占用 `.validation.json` 这个名字**。采用明确不同的文件名 `.validation-failed.json`。经核对 `_RetentionCommon.ps1:181` 的孤儿扫描条件为 `$_.Name -like '*.dump' -or $_.Name -like '*.validation.json'`，`<base>.validation-failed.json` **不匹配** `*.validation.json`（不以其结尾），因此 retention 完全忽略它，备份集保持 `unverified` → Protected。既留证据又零误判风险。
11. **应记录哪些字段**：见 2.2。
12. **禁止写入（泄露风险）**：`HostName`、`Port`、`Username`、`PGPASSWORD`/`PGPASSFILE`、完整命令行、DSN、私钥、Cookie/Authorization/token，以及 **dump/metadata 的绝对路径**（会暴露内部目录布局——只记文件名）。**业务行数摘要也排除**：`Test-PostgresBackup.ps1` 文件头自述"从不输出业务行"，虽然行数是聚合值而非行内容，但把业务量持久化到备份目录旁属不必要的信息暴露，且 retention 并不需要它；只保留结构性事实。

### 2.2 schema 决策（与 retention 读取逻辑对齐，非照抄示例）

| 字段 | 类型 | 理由 |
| --- | --- | --- |
| `schemaVersion` | number 1 | 与 metadata 一致的版本化习惯 |
| **`overallResult`** | string，枚举仅 `'passed'` | **retention 唯一认可的成功标记**（非示例里的 `status`） |
| **`dumpSha256`** | string | **retention 用于与 metadata 绑定**；必须等于 dump 当前真实哈希 |
| `backupFileName` | string（仅文件名） | 绑定 dump，不泄露绝对路径 |
| `metadataFileName` | string（仅文件名） | 绑定 metadata |
| `dumpSizeBytes` | number | 与 metadata 交叉校验 |
| `validatedUtc` | string（ISO 8601 `o`，UTC） | 固定格式、统一 UTC |
| `restoreDatabaseName` | string | **保留**：已被正则限定为 `pjsk_restore_test_*` 一次性测试库名，不含主机/端口/用户名，对追溯"哪次演练验证了它"有实际价值 |
| `validatorVersion` | string | psql 版本。**保留**：文档明确要求"恢复用 `pg_restore` 版本必须 ≥ 备份来源大版本"，记录"由哪个版本验证"对版本偏斜是真实有用的证据 |
| `migrationCount` / `migrationMax` | number / string | 结构性事实，与本仓库迁移集绑定 |
| `isolatedTestBackup` | bool | **从 metadata 严格读取并校验为真布尔**后复制，供人工核对模式 |

排除：`sourceDatabaseName`（metadata 已有，validation 无需）、业务 `rowCounts`（见上）、主机/端口/用户名/路径（安全）。

### 2.3 当前缺口总结

链条 `备份 → 恢复验证 → 保留策略` 的**最后一环缺失**：retention 依赖 `.validation.json` 判定 `verified`，而 `verified` 是保留分层与自动删除的**唯一入口**（`:202` 只挑 `Status -eq 'verified'`）。既然无人产出该文件，**所有备份永远停留在 `unverified` → 一律 Protected**——保留分层与 `-Execute` 清理实际上是永不触发的死路径。这不是安全隐患（失败方向是"不删"），但意味着保留策略从未真正生效，备份会无限增长。

### 2.4 `verified` 的完整前置条件（三方绑定）

```
status = 'verified'  ⟺  validation.overallResult == 'passed'
                    ∧  validation.dumpSha256 =i= metadata.dumpSha256
                    ∧  -VerifyHash 开启
                    ∧  dump 文件当前真实哈希 =i= metadata.dumpSha256
```
即 **validation ↔ metadata ↔ dump 实际内容**三者互相绑定，缺一不可。（`:159`；未开 `-VerifyHash` 时 `$actualSha` 为空 → 恒为 `unverified` → Protected。）

- 是否连接数据库：否。是否执行真实工具：否。是否读取敏感信息：否。是否使用子代理：否。本阶段仅调查，未改文件。

## 阶段 3：实现与离线测试

- 开始时间：2026-07-15 16:40（本机时间）

### 修改范围（7 个文件）

| 文件 | 目的 |
| --- | --- |
| `scripts/database/_BackupValidation.ps1`（新） | validation 记录构造、一致性校验、原子发布、失败报告；只定义函数 |
| `scripts/database/Invoke-BackupValidationTests.ps1`（新） | 77 项离线测试（mock psql 驱动真实入口） |
| `scripts/database/_BackupMetadata.ps1` | 新增 `Get-MetadataIsolatedTestFlag`（严格三态读取），并让原 `Test-BackupMetadataConsistency` 复用它 |
| `scripts/database/_RetentionCommon.ps1` | 用严格三态读取替换 `[bool]` 隐式转型；模式不可信的备份判为 `unknown` + Error |
| `scripts/database/Test-PostgresBackup.ps1` | 新增 `-BackupFile`；全部通过才发布 validation，失败写独立失败报告 |
| `scripts/database/Invoke-ScriptSafetyTests.ps1` | 串接新套件 |
| `docs/database-backup-restore.md` | 新增"生成 validation（闭合保留策略）"小节 |

**未修改**（本轮边界）：Go 代码、迁移器、历史迁移、`Backup-Postgres.ps1`、`Restore-PostgresTest.ps1`、清理脚本。`git status -- backend` 为空。

### 关键设计决策

1. **字段名必须是 `overallResult`，不是示例里的 `status`**。retention 只读 `overallResult`（必须恰为 `'passed'`）与 `dumpSha256`。若照抄任务书示例的 `"status": "verified"`，`$ovr` 为空 → 落入 `else` → `failed` → 备份永远 `validation-failed`，闭环静默失效。已在 `_BackupValidation.ps1` 文件头注明该耦合，并有测试锁定。
2. **失败报告用 `.validation-failed.json`**：retention 的孤儿扫描条件是 `-like '*.validation.json'`，该名字不以其结尾 → 被完全忽略 → 备份保持 `unverified` → Protected。既留证据又零误判。已有测试证明 retention 不会把它当成 verified。
3. **严格方案（非历史兼容方案）**：`git log -S isolatedTestBackup` 证明该字段只在 `1a89c95` 引入且始终写 `[bool]$RequireIsolatedSource`（真布尔），**无任何历史版本写入字符串的证据**，故按任务书要求采用严格方案。
4. **三态而非布尔**：`Get-MetadataIsolatedTestFlag` 返回 `$true`/`$false`/`$null`（缺失或非布尔）。retention 遇 `$null` 判 `status='unknown'` + `errorText` → `Decision='Error'` → 既不进入 `verified` 集合（`:202` 只挑 verified）故**永不自动删除**，也不被当作 isolated 证据，且明确要求人工检查。

### retention 布尔兼容行为矩阵（实测）

| metadata 中的 `isolatedTestBackup` | `IsolatedTest` | `Status` | 可否自动删除 |
| --- | --- | --- | --- |
| JSON `true` | `$true` | `verified` | 否（Protected，演练证据） |
| JSON `false` | `$false` | `verified` | 是（正常参与分层） |
| 字符串 `"true"` | `$null` | `unknown` | **否** |
| 字符串 `"false"` | `$null` | `unknown` | **否** |
| 数字 `1` | `$null` | `unknown` | **否** |
| 数字 `0` | `$null` | `unknown` | **否** |
| JSON `null` | `$null` | `unknown` | **否** |
| 字段缺失 | `$null` | `unknown` + `Decision=Error` | **否** |

修复前的危险方向：字段缺失/`0` → `[bool]` 得 `$false` → 被当作正式备份 → **可被自动删除**；`"false"`/`"true"`/`1` → `[bool]` 得 `$true` → 被当作演练证据（分类错误但失败方向安全）。

### 测试有效性验证（两次变异）

1. **让失败的演练也发布 validation**（`if ($false -and $script:failed)`）：**4 项 FAIL** —— `a failing drill exits non-zero`、`a failing drill publishes NO .validation.json`、`a failing drill leaves a failure report under a different name`、`retention never treats a failure report as verified`。还原后全通过。
2. **恢复 `[bool]` 隐式转换**（删除 `-isnot [bool]` 检查）：validation 套件 **13 项 FAIL**、publish 套件 **3 项 FAIL**。其中 `isolatedTestBackup strfalse -> status 'unknown' (actual: verified)` 直接复现了误判——字符串 `"false"` 会被当成已验证的演练证据。还原后全通过。

两次变异均只改本次待提交代码，未触碰删除/覆盖/路径保护逻辑，未连接数据库，未操作真实备份；`Select-String` 确认均已完整还原。

### 测试过程中修正的自身缺陷（诚实记录）

- 首次运行 `%TEMP%` 测试根被 `Test-BackupRootGuard` 正确拒绝（`refusing a path inside a protected directory (C:\Users\…)`）——这是守卫**按设计正常工作**。改用与 `Invoke-RetentionSafetyTests.ps1` 相同的约定（`D:\bkvalidation-tests-<guid>`，位于仓库外且非受保护目录）。
- 首次实现中 `$missingSet = Test-IsolatedFlagCase ...` 的赋值**吞掉了函数整个输出流**（Check 消息与返回值一起被捕获），导致 8 组用例的 PASS/FAIL 完全不显示、且 `$missingSet` 变成字符串数组。改为经 `$script:lastFlagSet` 传值、函数以语句形式调用。
- `$IsolatedOverride -eq '__none__'` 哨兵比较在传入数字时会触发类型转换错误，且 `-Override $null` 会退化成"字段缺失"而非 JSON `null`。改用 `-OmitIsolated` / `-UseOverride -OverrideValue` 显式开关。

### 验证结果

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `[Parser]::ParseFile` × 15 个脚本 | — | 全部 SYNTAX OK，0 失败 |
| `Invoke-BackupValidationTests.ps1` | 0 | **77 项通过** |
| `Invoke-BackupPublishTests.ps1` | 0 | 40 项通过（未回归） |
| `Invoke-RetentionSafetyTests.ps1` | 0 | 54 项通过（未回归，尽管改了 `_RetentionCommon.ps1`） |
| `Invoke-ScriptSafetyTests.ps1` 全量 | 0 | **246 项 PASS / 0 FAIL** —— 原 48 + 保留 54 + 迁移 27 + 发布 40 + 验证 77 |
| `git diff --check` | 0 | 通过 |

- **原 169 项全部仍然通过**（48 + 54 + 27 + 40），新增 77 项，合计 246 项，0 失败。
- 临时目录已清理（`bkvalidation-tests-*`、`bkretention-tests-*`、`pjsk-*-tests-*` 均无残留）；仓库内无 `.dump`/`.backup`/`.partial`/`.metadata.json`/`.validation.json`/`.validation-failed.json`/mock C# 源码残留；mock exe 随临时根删除。
- **未调用真实 PostgreSQL 工具**（仅 mock psql）；**未连接数据库**；未操作真实备份目录；未读取真实 `.env` 或密钥；未改系统配置；未使用子代理；未提交、未推送。
- 本阶段**未修改 Go**，故未运行 Go 测试。

## 未完成事项

1. 隔离 PostgreSQL 迁移集成测试（含超时行为测试设计）——待批准隔离测试库后执行。
2. 正式数据库 `schema_migrations` 只读核对——待获准部署窗口。
3. 正式部署（服务注册、证书、ACL、防火墙、DNS）。
4. 真实备份演练（真实 `pg_dump` + 恢复 + validation 生成）——本轮只做离线验证。

## 下一阶段入口

等待人工审阅。获准后再复核差异、重跑全部测试、按明确文件列表暂存并提交。
