# 正式库升级前真实备份、隔离恢复与 validation 演练

## 任务边界

- 日期：2026-07-15
- 目标：为"仓库领先正式库一个迁移"的升级前备份增加**显式、不可静默启用**的验证模式；对正式库执行真实 `pg_dump`；恢复到严格隔离的临时库；验证一致性；产出正式 `.validation.json`；清理恢复库；保留已验证备份作为运行 `0019` 前的回滚基线。
- **本阶段禁止**：运行 `0019`、运行任何迁移、启动正式后端、手工修改 `schema_migrations`、修改正式库。
- 日志不记录密码、pgpass 内容、完整 DSN、`PGPASSWORD`、邮箱、查询码、token、session、订单或付款明细。

## 阶段 1：Git 与现场基线

- 开始时间：2026-07-15 19:05（本机时间）
- 分支 `main`；`git status --short` 空；无未跟踪文件；暂存区空。
- `HEAD` = `origin/main` = **`d837470e0f6dafcd55b7c567cd2ce5f4a5f8a710`** ✅
- `git log -5`：`d837470` / `55cddd9` / `84fd194` / `ce6cb57` / `58e7a54`
- 8080 未监听；无 `pjsk-backend`/`go`/`psql`/`pg_dump`/`pg_restore` 进程。
- `PGPASSWORD`/`DATABASE_URL`/`PJSK_RUN_DB_INTEGRATION_TESTS`/`PJSK_TEST_DATABASE_ADMIN_DSN` 在 Process/User/Machine 三级**均 unset**。
- `pjsk_%test%` 数据库：**零行**（无 `pjsk_integration_test_%` / `pjsk_migration_test_%` 残留）。

## 阶段 2：正式库升级前基线（只读）

READ ONLY 事务 + `ROLLBACK`：

| 指标 | 值 |
| --- | --- |
| `migration_count` | **18** |
| `migration_max` | **`0018_query_code_email_recovery.sql`** |
| `audit_count` | **208** |
| `has_0019` | **0** |
| `audit_index_count` | **1** |

## 阶段 3：现有验证脚本兼容性评审（**先记录结论，再改代码**）

### 3.1 `Test-PostgresBackup.ps1` 当前如何得到期望迁移

`:45-52` —— 无条件从**仓库迁移目录**推导：`Resolve-RepositoryMigrationsDirectory` + `Get-MigrationFacts`，得到 `Count=19`、`MaxVersion=0019_admin_auth_audit_events.sql`。**没有任何参数可以覆盖**。

### 3.2 是否硬性要求恢复库最大迁移等于仓库当前最大迁移

**是。** `:175-176` 两条断言：

```powershell
Assert ($restored.migrationMax -eq $migrationFacts.MaxVersion) ...
Assert ([int]$restored.migrationCount -eq [int]$migrationFacts.Count) ...
```

### 3.3 正式库 18 条、仓库 19 条时会在哪里失败

**两条都失败**：

| 断言 | 恢复库实际 | 脚本期望 | 结果 |
| --- | --- | --- | --- |
| `:175` migrationMax | `0018_query_code_email_recovery.sql` | `0019_admin_auth_audit_events.sql` | **FAIL** |
| `:176` migrationCount | 18 | 19 | **FAIL** |

### 3.4–3.6 失败后的行为（已确认，符合设计）

- `$script:failed = $true` → 末尾分支：**不生成** final `.validation.json`；改写 `<base>.validation-failed.json`；退出码 1。
- retention：`.validation-failed.json` 不匹配 `*.validation.json` 通配，被完全忽略 → 备份集保持 `unverified` → **Decision = Protected，永不自动删除**。安全，但**该备份永远无法成为 `verified`**，因而无法作为回滚基线纳入保留分层。

### 3.7 当前 schema 是否允许记录"升级前基线"

**不允许。** `New-BackupValidationRecord` 无 `validationPurpose` / `expectedMigrationCount` / `expectedMigrationMax` 字段。

### 3.8 附带发现：当前验证**只比 count 与 max，未比完整文件名集合**

`:175-176` 仅比较数量与最大值。这意味着**一个数量相同、最大值相同但中间集合不同的库会被误判为通过**。任务要求"必须比较完整迁移文件名集合"——因此本轮**同时强化默认模式**，两种模式都做双向全集合比对。

### 3.9 `-SourceDatabase` 不能指向正式库（保留该安全门禁）

`:33-35` 要求 `-SourceDatabase` 匹配 `^pjsk_backup_source_test_[a-z0-9_]+$`，即**拒绝 `pjsk`**。这是刻意的安全门禁，**本轮不放宽**。改为：升级前基线由正式库只读导出到**仓库外临时文件**，再作为显式参数传入——比让验证脚本直连正式库更安全，且满足"恢复库集合与正式库备份前导出集合完全一致"的要求。

- 是否连接数据库：是（正式库仅只读）。是否写操作：否。是否读敏感信息：否。

## 阶段 4：脚本修改（显式升级前基线模式 + 强化默认模式）

- 开始时间：2026-07-15 19:20（本机时间）

### 4.1 设计

新增参数（`Test-PostgresBackup.ps1`）：`-ValidationPurpose`（`ValidateSet('current','pre-migration')`，默认 `current`）、`-ExpectedMigrationSetFile`、`-ExpectedMigrationCount`、`-ExpectedMigrationMax`。

| 规则 | 实现 |
| --- | --- |
| 默认行为不放宽 | `current` 仍从仓库迁移文件推导，恢复库必须与之完全一致 |
| 升级前模式不可静默启用 | `pre-migration` **必须同时**提供三个显式参数，缺一即 `Fail` |
| 期望集合绝不从被验证库推导 | 只从调用者提供的**仓库外**集合文件读取；脚本内无任何"从恢复库反推"的路径 |
| 标量与集合文件必须一致 | `-ExpectedMigrationCount`/`-ExpectedMigrationMax` 与文件不符即 `Fail`（不静默取其一） |
| 集合文件严格校验 | `Read-ExpectedMigrationSetFile`：非空、无重复、文件名匹配 `^\d{4}_[a-z0-9_]+\.sql$`、**严格 Ordinal 升序**；违反即拒绝 |
| 基线参数不得越界使用 | 非 `pre-migration` 模式下传入这些参数即 `Fail` |
| **两种模式都做全集合双向比对** | 新增 `missingInRestored` / `unknownInRestored` 两条断言 |
| 无"忽略差异"开关 | 未新增任何允许任意落后的旁路 |
| validation 记录基线 | `validationPurpose` / `expectedMigrationCount` / `expectedMigrationMax` / `expectedMigrationSetSha256`；round-trip 校验含枚举与一致性检查 |
| 不记录绝对路径 | 只输出集合文件的**文件名** |

**保留既有安全门禁**：`-SourceDatabase` 仍限 `^pjsk_backup_source_test_*`，即**拒绝 `pjsk`**。本轮**不放宽**——升级前基线改由正式库只读导出到临时文件再显式传入，比让验证脚本直连正式库更安全。

### 4.2 修复过程中发现的自身缺陷（透明记录）

1. **`$expectedSource:` 语法错误**：PowerShell 把 `$var:` 解析为作用域限定符（如 `$env:PATH`），需 `${expectedSource}:`。
2. **`Invoke-Scalar` 会压平多行结果**：`"$array"` 用空格连接，导致版本列表被压成一行。新增 `Invoke-Rows` 专用于多行查询。这是真实缺陷——若不修，全集合比对会静默失效。

## 阶段 5：离线测试

`Invoke-BackupValidationTests.ps1`：mock psql 新增 `PJSK_MOCK_MIGRATIONS` 环境变量，可在**不接触真实 PostgreSQL** 的前提下模拟"库落后于仓库"；并实现 `select version from schema_migrations` 多行返回。

新增 **24 项**测试（77 → **101**），覆盖任务书 §6 全部 14 项要求：

- 默认模式仍要求仓库 19 个迁移 ✅
- 默认模式面对 18 个迁移**必须失败** ✅
- 显式 `pre-migration` 18 条基线通过 ✅
- **同数量同最大值但中间集合不同必须失败**（证明比的是全集合，不是 count） ✅
- 期望集合含重复 / 非法文件名 / 乱序 / 文件缺失 → 全部拒绝 ✅
- 标量与集合文件不一致（count 与 max 各一例）→ 拒绝 ✅
- `pre-migration` 缺显式基线 → 拒绝（永不推导） ✅
- 非法 `ValidationPurpose` → 拒绝 ✅
- 基线参数用于默认模式 → 拒绝 ✅
- pre-migration validation 正确记录 18 / `0018` / 集合 SHA-256 ✅
- 默认 validation 记录 `validationPurpose = current`，**不被误标为 pre-migration** ✅
- 失败不生成 final validation ✅
- 原 77 项**无回归** ✅

### 变异验证（含一次失败的变异尝试，如实记录）

- **变异 1（无效）**：把默认 count 断言改为 `-le` 并清空 `missingInRestored` → 测试**仍全部通过**。原因：`migrationMax` 断言（0018 ≠ 0019）**仍然拦截**，掩盖了被削弱的检查。**这是一次无效变异，说明该变异未能隔离目标断言**，已如实记录。
- **变异 2（有效）**：只清空**双向**集合比对（`missingInRestored` 与 `unknownInRestored` 同时置空）→ **恰好 2 项 FAIL**：`same count and max but a different set in between FAILS` 与 `a mismatched set publishes no validation`。这两项的**唯一**保护就是集合比对，证明该检查确实被测试覆盖。还原后 101 项全通过。

- 全量 PowerShell：**270 PASS / 0 FAIL**（48 + 54 + 27 + 40 + **101**），较此前 246 增加 24。
- 全程**未调用真实 PostgreSQL 工具**完成离线测试（mock psql）。

## 阶段 6：正式备份（真实 `pg_dump`）

- 备份根：`D:\PJSK-Backups\PostgreSQL`（仓库外；脚本守卫通过）。
  - 附注：我最初用 `StartsWith('D:\pjsk')` 自查，得到**假阳性**（不区分大小写时 `D:\PJSK-B` 匹配 `D:\pjsk`）；脚本真实守卫追加反斜杠比较（`D:\pjsk\` vs `D:\PJSK-B`），正确放行，dry-run 成功即为佐证。
- 先 `-DryRun`（不接触数据库）确认目标路径与参数，再执行真实备份。
- 未使用 `-RequireIsolatedSource`（来源就是正式库）。
- 凭据：pgpass 自动认证；**未读取 pgpass 内容、未设置 `PGPASSWORD`、未输出完整连接串、未向用户索取密码**。

| 项 | 值 |
| --- | --- |
| dump | `D:\PJSK-Backups\PostgreSQL\2026\07\pjsk-20260715-221906.dump` |
| 大小 | **118,890 字节**（非零） |
| SHA-256 | `F32045584C35CA524FEE252261250AC3C64EC357CE76022BF0E58F986FFA32BA` |
| 对象数 | 170 |
| metadata | `pjsk-20260715-221906.metadata.json`（573 字节） |
| `.partial` 残留 | **无**（原子发布成功） |
| 覆盖旧文件 | **无**（该根目录此前为空） |

metadata 校验：`isolatedTestBackup = false`（真布尔）；`sourceDatabaseName = pjsk`；`fixtureExpectedRowCounts = null`（真实备份**不含**演练 fixture）；`dumpSha256`/`dumpSizeBytes` 与实际文件**一致**；**无任何凭据字段**。

> **值得记录**：这是本项目**第一份成功发布的真实正式备份**。在 `ce6cb57 fix: allow verified publication of real backups` 之前，`isolatedTestBackup = false` 会导致发布校验必然失败并删除 dump——即当时根本产不出正式备份。本次演练实证了该修复的价值。

## 阶段 7：隔离恢复

- 目标库：`pjsk_restore_test_20260715_156417`（匹配 `^pjsk_restore_test_[a-z0-9_]+$`；建库前确认不存在；非 `pjsk`/`postgres`/`template0`/`template1`）。
- 真实 `pg_restore` + `createdb` + `psql`；**只对隔离恢复库写入**；未触碰正式库；未使用 `--clean`/`--create`。
- 结果：`Restore complete into new test database 'pjsk_restore_test_20260715_156417'.`，退出 0。

## 阶段 8：恢复验证

调用改造后的 `Test-PostgresBackup.ps1`，显式提供 `-BackupFile`、恢复库名、18 条预期集合文件、`-ExpectedMigrationCount 18`、`-ExpectedMigrationMax 0018_query_code_email_recovery.sql`、`-ValidationPurpose pre-migration`。

**结果：`RESULT: PASSED`，退出 0。** 关键断言：

```
PASS  maximum migration is 0018_query_code_email_recovery.sql (from the explicit pre-migration baseline: 18 entries)
PASS  schema_migrations has the expected 18 entries
PASS  no expected migration is missing from the restored database
PASS  the restored database contains no migration outside the expected set
```

恢复库剖面：22 表 / 22 主键 / 39 外键 / 85 索引 / 0 序列 / pgcrypto ✓ / `gen_random_uuid()` ✓；业务表仅**汇总行数**（users 45、orders 44、order_items 120、payments 7、payment_items 19、admins 2 …），**未输出任何业务内容**。

### `0019` 异常状态保真（证明备份忠实保存了修复前状态）

| 指标 | 恢复库 | 正式库（只读） | 一致 |
| --- | --- | --- | --- |
| `admin_auth_audit_events` 表存在 | 1 | 1 | ✅ |
| 审计行数 | **208** | 208 | ✅ |
| 审计表索引数 | **1**（仅 `admin_auth_audit_events_pkey`） | 1 | ✅ |
| `has_0019` | **0** | 0 | ✅ |
| ghost-a / ghost-b | **96 / 112** | 96 / 112 | ✅ |

四个 `0019` 索引在恢复库中**同样缺失**——这不是把异常当正常，而是证明**备份忠实保存了修复前状态**，可作为运行 `0019` 前的回滚基线。

## 阶段 9：validation 产出

`D:\PJSK-Backups\PostgreSQL\2026\07\pjsk-20260715-221906.validation.json`（850 字节）：

| 字段 | 值 |
| --- | --- |
| `overallResult` | **`passed`** |
| `validationPurpose` | **`pre-migration`** |
| `expectedMigrationCount` | **18** |
| `expectedMigrationMax` | **`0018_query_code_email_recovery.sql`** |
| `migrationCount` / `migrationMax` | 18 / `0018_query_code_email_recovery.sql` |
| `dumpSha256` | `F32045…32BA`（与 dump 实际一致） |
| `backupFileName` / `metadataFileName` | `pjsk-20260715-221906.dump` / `.metadata.json` |
| `restoreDatabaseName` | `pjsk_restore_test_20260715_156417` |
| `validatorVersion` | `psql (PostgreSQL) 18.4` |
| `isolatedTestBackup` | **`false`**（来源是正式库；恢复目标是隔离库**不改变**这一点） |
| `expectedMigrationSetSha256` | `C57B6AE1…E2C7` |

**三方绑定**：dump 实际 SHA = metadata SHA = validation SHA；文件名与大小一致。

### retention 判定

**发现一处既有缺陷（本次演练暴露）**：`_RetentionCommon.ps1:41` 的根目录守卫 `if ($normalized -match '(?i)\\PostgreSQL(\\|$)')` 会拒绝**任何路径中含 `\PostgreSQL` 的目录**——包括 `docs/database-backup-restore.md:24,34` **推荐的** `D:\PJSK-Backups\PostgreSQL`。实测：

```
D:\PJSK-Backups\PostgreSQL  -> allowed=False  reason=refusing a PostgreSQL install/data tree
D:\PostgreSQL\18\data       -> allowed=False  reason=refusing a PostgreSQL install/data tree
```

守卫的本意（挡住 PostgreSQL 安装/数据目录）正确，但**过度匹配**，导致保留策略**永远无法扫描文档推荐的备份根**。备份/恢复/验证本身不受影响。

`_RetentionCommon.ps1` **不在本阶段允许修改的文件范围内**，故**只记录不修改**。为仍能回答"retention 能否识别为 verified 正式备份"，把三个文件**复制**到一个不含 `PostgreSQL` 字样的临时根做只读判定（**原件未动**），结果：

```
SetId=pjsk-20260715-221906  Status=verified  IsolatedTest=False
Decision=Protected  Reason=recently modified (still changing?)  ValidationResult=passed
```

即 **validation 内容完全符合 retention 的 verified 判定**（`IsolatedTest=False` 正确识别为正式备份而非演练证据）；`Protected` 仅因 15 分钟"近期修改"保护，属正常。临时副本已删除。

## 阶段 10：演练后正式库复核与清理

| 指标 | 演练后 |
| --- | --- |
| `schema_migrations` | **18** |
| `admin_auth_audit_events` | **208** |
| `has_0019` | **0** |
| audit indexes | **1** |

**正式库全程未变化。**

清理：
- 恢复测试库 `pjsk_restore_test_20260715_156417` **已按精确名删除**（先仅终止该库连接）；`pjsk_restore_test_%` / `pjsk_%test%` 查询**零行**。
- 临时预期集合目录 `D:\PJSK-Drill-Temp-…` **已删除**。
- 临时 retention 检查副本 **已删除**。
- **保留**：dump、metadata、final validation（演练证据 + 回滚基线）。
- 未运行 retention 删除；未删除任何旧备份；无 `.partial`、无 `.validation-failed.json` 残留。
- 无 `psql`/`pg_dump`/`pg_restore`/`pjsk-backend`/`go` 进程；8080 未监听；`PGPASSWORD`/`DATABASE_URL`/`PJSK_MOCK_*` 三级均 unset。

## 阶段 11：备份保留位置与 SHA-256

| 文件 | 大小 | SHA-256 |
| --- | --- | --- |
| `D:\PJSK-Backups\PostgreSQL\2026\07\pjsk-20260715-221906.dump` | 118,890 | `F32045584C35CA524FEE252261250AC3C64EC357CE76022BF0E58F986FFA32BA` |
| `…\pjsk-20260715-221906.metadata.json` | 573 | — |
| `…\pjsk-20260715-221906.validation.json` | 850 | — |

**这是运行 `0019` 前的已验证回滚基线。**

## 阶段 12：安全边界

未运行 `0019`；未运行任何迁移；未启动正式后端；未手工修改 `schema_migrations`；未修改正式库（前后计数逐次核对一致）；未读取密码或 pgpass 内容；未设置 `PGPASSWORD`；未输出完整 DSN；未读取业务明细（仅汇总行数）；未删除任何旧备份；未运行 retention 删除；未运行 Go 测试（本阶段未改 Go）；未使用子代理；未提交、未推送。

## 阶段 13：retention 根目录守卫修复

- 开始时间：2026-07-15 20:10（本机时间）
- Git 基线：`HEAD` = `origin/main` = `401e66af617c0dd676632573b836189fa4733aac`，工作区干净。

### 13.1 只读调查结论（先记录，后改码）

**守卫位置与调用方**：`_RetentionCommon.ps1:11` `Test-BackupRootGuard`；调用方仅两处——`Get-PostgresBackupRetentionReport`（`:78`，只读扫描）与 `Remove-ExpiredPostgresBackups.ps1:82`（删除入口）。

**守卫本意（逐层）**：空路径/相对路径/无法规范化 → 拒；驱动器根、UNC 共享根 → 拒；仓库根 + `WINDIR`/`ProgramFiles`/`ProgramData`/`USERPROFILE`/`C:\Users` 等保护目录（含子路径）→ 拒；**PostgreSQL 安装/数据目录 → 拒**（防止 retention 删除流程接近数据库文件）；不存在目录、reparse point → 拒。

**误判原因**：第 41 行 `$normalized -match '(?i)\\PostgreSQL(\\|$)'` 是**纯名称关键词匹配**——只要路径中任何一级目录名为 `PostgreSQL` 即拒绝。它无法区分：

- `D:\PostgreSQL\18`（真实安装目录，含 `bin\postgres.exe`）——应拒
- `D:\PJSK-Backups\PostgreSQL`（**文档自己推荐的备份根**，只是目录取名叫 PostgreSQL）——应允许

第 42 行 `(\\|^)data$` + 含 `postgres` 同为名称启发式，存在同类误判面。

**现有测试覆盖**：`Invoke-RetentionSafetyTests.ps1:172-191`（18–27 共 13 项守卫用例）。其中第 22 项用 `C:\Program Files\PostgreSQL\18\data`——该路径位于 `ProgramFiles` 内，**由保护目录检查拒绝**，与 postgres 正则无关，故重构后该用例语义不变。

**修复设计（内容判定替代名称判定）**：真实 PostgreSQL 目录有**确定性内容标记**，与目录叫什么名字无关：

- **数据目录**：恒含 `PG_VERSION` 文件（即使实例停止也存在）
- **安装目录**：恒含 `bin\postgres.exe`（辅以 `bin\pg_ctl.exe`、`bin\initdb.exe`）

据此：① 删除两条名称正则；② 新增 `Test-IsPostgresInstallOrDataDirectory`（查内容标记）；③ **祖先链检查**——路径自身或任何祖先命中标记即拒（覆盖 `D:\PostgreSQL\18`、`D:\PostgreSQL\18\data` 及其内部任意子路径）；④ **伞目录检查**——路径的直接子目录命中标记即拒（覆盖 `D:\PostgreSQL` 这类版本伞目录；也顺带保护"有人把真实数据目录放进备份根"的情形）。不新增任何 `-Force`/`-IgnoreSafety` 绕过参数；其余保护层一律不动。

### 13.2 实现（2026-07-16）

`scripts/database/_RetentionCommon.ps1`：

1. **删除**两条名称正则（`\\PostgreSQL(\\|$)` 与 `data$`+`postgres` 启发式）。
2. **新增** `Test-IsPostgresInstallOrDataDirectory`：按内容标记判定——`PG_VERSION` 文件（数据目录）或 `bin\postgres.exe`/`bin\pg_ctl.exe`/`bin\initdb.exe`（安装目录）；探测用 `-ErrorAction SilentlyContinue` + try/catch（ACL 拒绝不外泄、不刷屏）。
3. **祖先链检查**：路径自身与每一级祖先逐个探测，命中即拒——覆盖 `D:\PostgreSQL\18`、`D:\PostgreSQL\18\data` 及安装树内任意子路径（数据目录即使 ACL 不可读，也经由可读的安装目录祖先被拒）。
4. **伞目录检查**（存在性检查之后）：直接子目录命中标记即拒——覆盖 `D:\PostgreSQL` 版本伞目录；子目录**无法枚举时按失败关闭**拒绝（retention 绝不在无法检视的目录旁操作）。
5. **未新增**任何 `-Force`/`-IgnoreSafety` 类绕过参数；驱动器根/UNC 根/仓库根/系统目录/不存在/reparse 等既有保护层全部原样保留。

### 13.3 测试（`Invoke-RetentionSafetyTests.ps1` 新增 12 项，54 → 66）

允许：29a `…\PJSK-Backups\PostgreSQL`、29b `…\Backups\PostgreSQL`、29c `…\Company\PostgreSQL-Backups`（临时目录合成，仅名称含 PostgreSQL）。
拒绝：29d 合成安装树（`bin\postgres.exe` 标记）、29e 安装树**内部**路径（祖先探测）、29f 含安装树的伞目录、29g 含 `PG_VERSION` 的数据目录（**无论目录叫什么名字**）、29h 数据目录内部、29i 含数据目录的父目录、29j/29k/29l 本机**真实** `D:\PostgreSQL\18`/`D:\PostgreSQL`/`D:\PostgreSQL\18\data`（不存在时 SKIP）。
既有 13 项守卫用例（18–28）全部保留且通过——第 22 项 `C:\Program Files\PostgreSQL\18\data` 本就由 ProgramFiles 保护层拒绝，语义不受重构影响。

**过程中发现并修正**：初版 29l 的门槛用 `Test-Path …data\PG_VERSION`，在 `$ErrorActionPreference='Stop'` 下对 ACL 保护的真实数据目录抛 `UnauthorizedAccessException` 使套件中途终止；改为以可读的安装标记 `bin\postgres.exe` 作门槛。同一发现促成 13.2 第 2 点的 `-ErrorAction SilentlyContinue`（否则交互式会话会打印无害但吓人的 Access denied）。

**变异验证**：临时恢复旧正则 → **恰好 29a/29b 两项 FAIL**（64/2），证明新测试确实钉住"名称不再是拒绝理由"；还原后 66/0。

### 13.4 验证结果

| 项 | 结果 |
| --- | --- |
| 语法检查（2 个脚本） | OK |
| retention 专项 | **66 PASS / 0 FAIL** |
| 全量安全测试 | **282 PASS / 0 FAIL**（66+27+40+101+48） |
| `git diff --check` | 0 |
| 守卫实测 | `D:\PJSK-Backups\PostgreSQL` → **allowed**；`D:\PostgreSQL\18` → refused（安装树）；`D:\PostgreSQL\18\data` → refused（经祖先 `D:\PostgreSQL\18`）；`D:\PostgreSQL` → refused（伞目录）；`D:\`、`D:\pjsk` → refused（既有层） |
| **真实根只读扫描** | `Get-PostgresBackupRetentionReport -BackupRoot 'D:\PJSK-Backups\PostgreSQL' -VerifyHash` → `SetId=pjsk-20260715-221906  Status=verified  IsolatedTest=False  Decision=Keep  Reason=retention tier: newest` —— **备份→验证→保留闭环首次在文档推荐路径上端到端打通**；未执行任何删除 |
| 正式库 | **18 / 208 / has_0019=0 / idx=1**（未变） |
| 备份 SHA | `F32045…32BA` **未变**；dump/metadata/validation 三件俱在 |
| 残留 | 无恢复库/临时库/`.partial`；清理了两个此前套件中途崩溃遗留的 `D:\bkretention-tests-*` 合成 fixture 根（27 字节假 dump，非真实备份） |

- 未运行 `0019`；未启动后端；未修改正式库；未删除真实备份；未使用子代理。
- 文档同步：`docs/database-backup-restore.md` 的"已知问题"段更新为"已收敛"说明。

## 下一阶段入口

提交并推送本修复后，进入 **受控执行 `0019`** 准备。
