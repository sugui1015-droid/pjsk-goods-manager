# PostgreSQL 备份保留策略与安全清理

## 阶段 1：只读调查

### 基线

- 分支 `main`；`HEAD` = `origin/main` = `e85e8bf477f1f7bd45edbf73092f666e95da200e`；工作区、暂存区干净。
- 只读检查了 `AGENTS.md`、`HANDOVER.md`、`docs/database-backup-restore.md`、备份恢复开发日志（`2026-07-14-database-backup-restore.md`）、`scripts/database/` 全部脚本、`.gitignore`。未连接数据库、未读取真实 `.env`/密钥、未修改任何演练目录。

### 调查结论（逐项）

1. **一个完整备份集合包含**：主 dump `<basename>.dump` + 元数据 `<basename>.metadata.json`。可选恢复验证旁文件 `<basename>.validation.json`（约定，见下）。`basename` 默认 `pjsk-yyyyMMdd-HHmmss`（`Backup-Postgres.ps1` 输出布局 `<BackupRoot>\yyyy\MM\pjsk-yyyyMMdd-HHmmss.dump`）。
2. **主/元数据/验证**：主备份 = `.dump`；元数据 = `.metadata.json`（字段：`schemaVersion`、`createdAtUtc`(ISO-8601 UTC)、`clientVersion`、`sourceDatabaseName`、`isolatedTestBackup`、`dumpFormat`、`dumpFileName`、`dumpSizeBytes`、`dumpSha256`、`backupScriptSha256`、`fixtureExpectedRowCounts`、`objectCount`）；验证 = `.validation.json`。
3. **如何判断备份已验证成功**：元数据本身**不含**恢复验证结果——`Test-PostgresBackup.ps1` 只向 stdout 输出 PASS/FAIL，恢复演练的 `*.validation.json` 是**单独**产物（演练中命名为 `<uniqueid>.validation.json`，与 dump basename 不同）。因此正式备份仅凭元数据只能判定**完整性/哈希一致**，无法判定**恢复可用**。→ 本工具定义前向约定：只有当备份集合旁存在 `<basename>.validation.json` 且 `overallResult=passed` 且其 `dumpSha256` 与元数据一致时，才判为 `verified`；否则为 `unverified`（受保护，永不成为删除候选）。
4. **如何区分演练证据 vs 正式备份**：演练证据目录（如日志记录的 `D:\pjsk-backup-restore-tests\...`）由演练流程创建，dump 源库名带 `pjsk_backup_source_test_` 前缀、元数据 `isolatedTestBackup=true`。正式备份源库为 `pjsk`、`isolatedTestBackup=false`。**保留工具默认把 `isolatedTestBackup=true` 的集合标为 `protected`（演练证据），永不删除。**
5. **是否有未完成 `.partial`**：约定 `<basename>.dump.partial` 与 `<basename>.metadata.json.partial` 为写入中/失败残留；任一存在即集合 `incomplete`，受保护并报告异常。
6. **绝不能自动删除**：最新一份成功（verified）备份、最新成功恢复验证证据、任何 `.partial` 集合、无 metadata/无法解析集合、SHA-256 不一致集合、`isolatedTestBackup=true` 演练证据、未知文件/目录、用户保护列表、近期仍在变化的文件。
7. **命名含可解析时间戳**：是——正式 basename `pjsk-yyyyMMdd-HHmmss`，且元数据 `createdAtUtc` 为权威 ISO-8601。
8. **保留层级按何计算**：**按元数据 `createdAtUtc`**（权威、可离线重复）；文件修改时间仅作报告展示与"近期变化"安全判断，不作删除分层依据（删除候选本就要求元数据可解析）。
9. **可复用的路径守卫**：现有脚本没有独立共享守卫，均为内联检查（`Backup-Postgres.ps1` 有"BackupRoot 必须绝对、且不在仓库内"的 `GetFullPath`+前缀比较逻辑，可作为参考）。本轮新写一个更严格的集中守卫并被扫描/清理脚本共用。
10. **脚本化清理的代码阻断**：无阻断。可完全离线实现（只读扫描 + DryRun + 仓库外临时 fixture 受控执行测试）。

### `.gitignore` 现状

- 已忽略 `*.dump`、`*.backup`、`*.partial`。未忽略 `*.metadata.json`/`*.validation.json`/保留报告/审计产物；确认无已跟踪文件命中这些后缀，可安全在阶段 6 追加精确忽略。正式备份根目录在仓库外，产物本就不会从仓库树误提交。

### 结论 / 下一阶段

- 无阻断 → 实现：保留策略主文档、只读扫描报告脚本、默认 DryRun 的多重确认清理脚本、扩展安全测试（仓库外 fixture）。**本轮不对任何现有真实备份/演练证据执行删除。**

### Git 状态

- 阶段末仅新增本日志；`git diff --check` 干净；暂存区空；无删除、无重命名。

## 阶段 2：保留策略主文档

- 新增 `docs/database-backup-retention.md`：备份集合与验证旁文件约定、7 类状态分类（verified/unverified/validation-failed/incomplete/orphan/unknown/protected）、14 项删除候选条件、分层保留（KeepAll/Daily/Weekly/Monthly/MinimumVerified + 最新永久保护）、第一版"默认只报告不删除"原则、决策状态、清理脚本多重保护、建议调度、目录/产物区分。

## 阶段 3–4：扫描报告与清理脚本

- 新增 `scripts/database/_RetentionCommon.ps1`：**无 param 块**的共享函数 `Test-BackupRootGuard`、`Test-IsReparsePoint`、`Get-PostgresBackupRetentionReport`。设计要点：报告脚本与清理脚本都 dot-source 本文件，避免"dot-source 带 param 的脚本会重置调用方 `$BackupRoot` 变量"的坑（本轮实际踩到并修复）。
- `Get-PostgresBackupRetentionReport.ps1`：只读扫描薄封装，支持 `-BackupRoot/-Now/-KeepAllDays/-DailyDays/-WeeklyWeeks/-MonthlyMonths/-MinimumVerifiedBackups/-VerifyHash/-ProtectName/-OutputJson/-OutputCsv/-AsObjects`；输出固定顺序对象，含状态、层级、决策、原因、错误；lone dump/validation 无 metadata 锚点者报告为 orphan（受保护）；从不删改文件。
- `Remove-ExpiredPostgresBackups.ps1`：**默认 DryRun**，复用 `Get-PostgresBackupRetentionReport`（不另写宽松判断）。真正执行需同时满足 `-Execute` 且非 `-DryRun`、`-ExpectedRootName` 精确匹配根末段、`-ConfirmationText "DELETE_EXPIRED_PJSK_BACKUPS"`、报告无 Error、删除前重新扫描逐项复核（仍是 Candidate、SHA/validation/partial 状态未变、文件仍在根内）。只删除已识别候选文件；空月目录删除前确认已空且在根内；逐项审计；任一失败继续保护其他对象并返回非零。
- **接口修正**：原设计用 `[bool]$DryRun=$true` + `-DryRun:$false`，实测 `powershell -File` 无法绑定 bool 参数（真实运维也会踩坑），改为 `[switch]$Execute` + `[switch]$DryRun`（默认无 Execute 即 DryRun；显式 `-DryRun` 强制 DryRun）。
- 路径守卫：拒绝盘符根、UNC 根、仓库内、Windows/ProgramFiles/ProgramData/USERPROFILE/`C:\Users`、任何含 `\PostgreSQL\` 或 PostgreSQL data 目录、相对路径、不存在目录、reparse point；`GetFullPath` + 大小写不敏感 + 尾分隔符规范化比较；不自动创建根目录。

## 阶段 5：安全测试

- 新增 `scripts/database/Invoke-RetentionSafetyTests.ps1`：全部 fixture 在仓库外、非 `pjsk`、`D:\bkretention-tests-<guid>` 根下创建，结束时经路径守卫删除；无数据库、无密码、`-Now` 固定可重复。
- 覆盖 54 项断言（含任务列出的扫描 1–17、路径守卫 18–28、DryRun/执行门禁 29–40、受控执行 41–48）：完整/缺 dump/缺 metadata/缺 validation/validation 失败/metadata 损坏/validation 损坏/SHA 匹配与不匹配/partial/未知文件/跨分层/最少验证数/最新永久保护/固定 Now 可重复/JSON+CSV 生成/输出无密钥；拒绝盘符根/仓库/Windows/用户/PostgreSQL data/相对路径/reparse 跳过/逃逸/大小写/空/不存在/不自动创建；默认不删/仅 -Execute 不删/缺 ExpectedRootName 不删/确认短语错不删/报告 Error 不删/partial 出现不删/validation 变更不删/非候选不删/审计记录；受控执行只删候选、保留其他、未知文件存活、根目录与上级不删、越界不删、删除失败非零退出、空目录移除、无 partial/密钥残留。
- 已整合进既有 `Invoke-ScriptSafetyTests.ps1`（在原 48 项后追加调用本测试并汇入退出码），原 48 项保留未替换。
- 调试中修复三个测试基建问题（非脚本缺陷）：dot-source param 脚本的变量污染（抽出无 param 公共文件）；`powershell -File` 不能绑定 bool（改 switch）；PS 5.1 把子进程 stderr 包成 NativeCommandError（子调用改 `Continue` + 落文件）；以及 fixture mtime 与固定 `-Now` 的确定性（把 fixture 文件 mtime 设为其逻辑创建时间）、单对象 `.Count` 用 `@()` 包裹、锁文件测试用 `FileShare.Read` 让候选先通过再于删除阶段失败。

## 阶段 6：文档、交接与 .gitignore

- `.gitignore` 追加 `*.metadata.json`、`*.validation.json`、`*.retention-report.json`、`*.retention-report.csv`、`*.retention-audit.json`（正式产物均在仓库外，此为防御性忽略）；确认无已跟踪文件命中。
- `docs/database-backup-restore.md` 增加保留策略与安全清理的交叉引用。
- `HANDOVER.md` 更新备份工具状态，补充保留/清理脚本与文档。

## 阶段 7：离线验证与实际数据保护

- PowerShell 语法解析：`_RetentionCommon.ps1`、`Get-PostgresBackupRetentionReport.ps1`、`Remove-ExpiredPostgresBackups.ps1`、`Invoke-RetentionSafetyTests.ps1` 均通过（PS 5.1）。
- `Invoke-ScriptSafetyTests.ps1`（含原 48 项 + 54 项保留测试）整体运行 `RESULT: all safety tests passed`，退出码 0。
- **未扫描 `D:\pjsk-backup-restore-tests\` 或任何现有真实备份目录，未对其执行删除；真实备份删除数量为 0。** 所有测试只针对本测试自建的仓库外 fixture，且已清理（包括早期失败运行/探针留下的 `D:\bkretention-*` 目录，经守卫后删除，最终为 0）。
- 未连接数据库、未运行迁移、未读取真实 `.env`/密钥、未修改业务代码。工作区无 `.dump`/`.backup`/`.partial`/报告/fixture 残留。
