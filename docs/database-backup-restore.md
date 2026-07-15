# PostgreSQL 备份、恢复与恢复演练

本文描述 `scripts/database/` 下备份/恢复工具的用途与安全用法。所有示例只用通用路径与测试库名，不含真实密码、DSN、主机或业务数据。

> 备份不做过恢复演练，等于没有备份。一份从未成功还原过的 dump，可能因格式、版本或损坏在真正需要时无法使用。

## 所需工具

- PostgreSQL 客户端工具（`pg_dump`、`pg_restore`、`createdb`、`dropdb`、`psql`），默认路径 `D:\PostgreSQL\18\bin`，可用各脚本的 `-PostgresBin` 参数覆盖。
- 恢复用的 `pg_restore` 版本必须 **等于或高于** 备份来源服务器的大版本。

## 密码处理原则

脚本 **从不** 接受密码参数、不打印、不写日志、不写 metadata、不设置永久环境变量。按以下任一方式提供凭据：

1. 使用 PostgreSQL 默认密码文件，或在当前 PowerShell 命令上下文中临时设置 `PGPASSFILE` 指向受保护的 pgpass 文件；命令结束后立即清除该变量。
2. 使用 PostgreSQL 的 `%APPDATA%\postgresql\pgpass.conf`（本工具不创建该文件）。
3. 让工具在需要时交互式提示密码。

不要把密码写进脚本参数、命令行、命令历史或 Git。

## 备份目录

- 建议放在仓库外，例如 `D:\PJSK-Backups\PostgreSQL`。脚本会拒绝位于本 Git 仓库内部的 `-BackupRoot`，并按 `yyyy\MM` 建立日期层级。
- 备份文件 **不进入 Git**（`.gitignore` 已忽略 `*.dump`/`*.backup`/`*.partial`），也 **不上传公网或第三方网盘**。
- 备份文件应设置 NTFS ACL 仅限必要账户可读，并考虑静态加密；恢复演练用的临时 dump 用后删除。

## 备份

```powershell
$env:PGPASSFILE = 'D:\path\to\pgpass.conf'
./scripts/database/Backup-Postgres.ps1 `
    -DatabaseName pjsk `
    -BackupRoot 'D:\PJSK-Backups\PostgreSQL' `
    -Username postgres
Remove-Item Env:\PGPASSFILE
```

流程：`pg_dump --format=custom --no-owner --no-privileges` 写入 `<name>.dump.partial` → `pg_restore --list` 校验可读 → 计算 SHA-256 → 原子重命名为 `<name>.dump` → 生成 `<name>.metadata.json`。任一步失败会删除 `.partial` 并返回非零退出码，不留伪成功文件、不覆盖同名文件。

`-DryRun` 只打印将执行的安全概要，不连接数据库。

### 校验 SHA-256 与查看 dump 内容

```powershell
Get-FileHash -Algorithm SHA256 'D:\PJSK-Backups\PostgreSQL\2026\07\pjsk-20260714-120000.dump'
& 'D:\PostgreSQL\18\bin\pg_restore.exe' --list 'D:\...\pjsk-20260714-120000.dump'
```

metadata JSON 只含：创建时间(UTC)、客户端版本、dump 文件名、大小、SHA-256、数据库逻辑别名、对象数量、格式——无任何凭据或业务数据。

### 两种备份模式

脚本同时支持两种模式，由 `-RequireIsolatedSource` 区分，并记录在 metadata 的 `isolatedTestBackup` 字段：

| | 正式备份 | 隔离恢复演练备份 |
| --- | --- | --- |
| 参数 | 不传 `-RequireIsolatedSource`（如上例的 `-DatabaseName pjsk`） | 必须传 `-RequireIsolatedSource` |
| 允许的库名 | 任意合法标识符（正式使用时显式传 `pjsk`） | 仅 `pjsk_backup_source_test_*`，正式库被拒绝 |
| `isolatedTestBackup` | `false` | `true` |
| 演练 fixture 预期行数 | 不写入 | 写入（迁移数量由 `backend/migrations/` 动态得出） |
| 保留策略 | 参与日/周/月保留分层 | 永久 `Protected`，视为演练证据，不参与分层 |

发布前会校验回读的 metadata 与本次运行一致，其中 `isolatedTestBackup` 必须**与本次模式相符**（而非恒为真）；字段缺失或非布尔类型一律判定失败。两种模式都保留哈希校验、`.partial` 原子写入与拒绝覆盖同名文件的保护。

## 恢复到临时测试数据库

恢复 **只允许** 恢复到一个全新的、名字以 `pjsk_restore_test_` 开头的数据库；绝不恢复进正在使用的正式库。脚本不使用 `--clean`/`--create`，目标库已存在时直接失败。

```powershell
$env:PGPASSFILE = 'D:\path\to\pgpass.conf'
$target = 'pjsk_restore_test_' + (Get-Date -Format 'yyyyMMdd_HHmmss')
./scripts/database/Restore-PostgresTest.ps1 `
    -BackupFile 'D:\PJSK-Backups\PostgreSQL\2026\07\pjsk-20260714-120000.dump' `
    -TargetDatabase $target `
    -Username postgres
```

流程：`pg_restore --list` 预检 → 确认目标库不存在 → `createdb` 建空库 → `pg_restore --no-owner --no-privileges --exit-on-error --single-transaction`。恢复失败时目标库保留供诊断（由本次运行创建、无正式数据），应删除后换新名重试。

## 校验恢复结果

```powershell
./scripts/database/Test-PostgresBackup.ps1 `
    -RestoredDatabase $target `
    -SourceDatabase pjsk_backup_source_test_20260714_120000 `
    -Username postgres
```

### 生成 validation（闭合保留策略）

加上 `-BackupFile` 指向被验证的 dump，全部检查通过后会在 dump 旁生成 `<name>.validation.json`：

```powershell
./scripts/database/Test-PostgresBackup.ps1 `
    -RestoredDatabase $target `
    -BackupFile 'D:\PJSK-Backups\PostgreSQL\2026\07\pjsk-20260714-120000.dump' `
    -Username postgres
```

**这一步是保留策略的前提**：`Remove-ExpiredPostgresBackups.ps1` 只对 `verified` 的备份做分层保留，而 `verified` 需要 dump、metadata、validation 三者哈希互相绑定（并且报告需带 `-VerifyHash`）。没有 validation 的备份一律 `unverified` → 永久 Protected，即保留策略不会回收任何空间。

### 升级前基线验证（仓库领先正式库时）

当仓库的迁移比目标库新（例如正式库停在 `0018`、仓库已到 `0019`），默认验证会**正确地失败**——它要求恢复库与仓库迁移完全一致。要为"运行新迁移之前"留一份可验证的回滚基线，必须**显式**声明预期基线，绝不会因为库落后就被静默接受：

```powershell
# 1. 只读导出目标库当前迁移集合到仓库外的临时文件
#    （绝不手工誊写迁移名；也绝不从恢复库自身推导）
$baseline = 'D:\<仓库外临时目录>\production-baseline-migrations.txt'

# 2. 显式声明基线；三者必须互相一致，否则拒绝
./scripts/database/Test-PostgresBackup.ps1 `
    -RestoredDatabase $target `
    -BackupFile 'D:\...\pjsk-<stamp>.dump' `
    -ValidationPurpose pre-migration `
    -ExpectedMigrationSetFile $baseline `
    -ExpectedMigrationCount 18 `
    -ExpectedMigrationMax '0018_query_code_email_recovery.sql' `
    -Username postgres
```

规则：

- **默认 `-ValidationPurpose current` 不变**：恢复库迁移集合必须与仓库当前迁移完全一致。**没有"忽略迁移差异"或"允许任意落后"的开关。**
- `pre-migration` 必须**同时**提供 `-ExpectedMigrationSetFile`、`-ExpectedMigrationCount`、`-ExpectedMigrationMax`；缺一即拒绝。**期望集合永不从被验证的库推导**——否则验证毫无意义。
- 两个标量必须与集合文件一致，不一致即拒绝（不静默取其一）。
- 期望集合必须：非空、无重复、文件名合法、严格升序；违反任一即拒绝。
- **两种模式都做完整文件名集合双向比对**，不只比数量与最大值——数量与最大值相同但中间集合不同的库必须失败。
- validation 记录 `validationPurpose`、`expectedMigrationCount`、`expectedMigrationMax` 与期望集合的 SHA-256。
- 正式库备份的 `isolatedTestBackup` 仍为 `false`：**恢复目标是隔离库不代表来源备份是演练备份**，两者不可混淆。

> **保留策略根目录守卫**（2026-07-16 已收敛）：守卫按**内容标记**识别 PostgreSQL 安装/数据目录——数据目录恒含 `PG_VERSION`，安装目录恒含 `bin\postgres.exe`——并检查路径自身、全部祖先与直接子目录；目录**仅仅取名叫** `PostgreSQL`（如本文推荐的 `D:\PJSK-Backups\PostgreSQL`）不再被误拒。真实安装树、数据目录及其内部任意路径仍被拒绝；无法枚举内容的目录按失败关闭处理。

行为要点：

- 只有**全部检查通过**才发布；先写 `.partial`，回读校验后原子改名。
- **拒绝覆盖**已存在的 validation；需要重新验证时先人工确认并移走旧文件。
- 验证失败时**不生成** validation，改为写 `<name>.validation-failed.json`。该文件名不被保留策略读取，备份保持 `unverified` → Protected，既留证据又不会被误判为成功。
- 发布前会校验 dump 的真实 SHA-256 与 metadata 相符；不符（dump 被改动）则拒绝验证。
- metadata 缺失、非法 JSON，或 `isolatedTestBackup` 不是真布尔时，一律拒绝验证。
- validation 只记录文件名、哈希、大小、UTC 时间、结果、一次性测试库名、验证器版本与迁移事实；**不含**主机、端口、用户名、密码、DSN、命令行或业务行数。

只输出对象名、数量与 PASS/FAIL，从不输出业务行。核对 `schema_migrations` 最大版本、关键表存在、主键/外键/索引/序列数量；提供 `-SourceDatabase`（必须是隔离测试库）时逐项对比两库的迁移版本、表数、行数与约束数。

## 清理临时数据库

```powershell
./scripts/database/Remove-PostgresRestoreTest.ps1 -TargetDatabase $target -Username postgres
```

只接受 `pjsk_restore_test_*` 与 `pjsk_backup_source_test_*` 两个测试前缀；拒绝 `pjsk`/`postgres`/`template0`/`template1` 及任何其他名称；一次只删一个显式命名的库；先终止连接再 `dropdb`，最后确认已不存在。支持 `-DryRun`。

## 保留策略与安全清理

分层保留策略、只读扫描报告脚本（`Get-PostgresBackupRetentionReport.ps1`）与默认 DryRun 的清理脚本（`Remove-ExpiredPostgresBackups.ps1`）见 [database-backup-retention.md](database-backup-retention.md)。要点：默认只报告不删除；真正删除需 `-Execute` + `-ExpectedRootName` + 确认短语 + 干净报告；只删除通过验证（有 `<basename>.validation.json`、`overallResult=passed`、SHA-256 一致）且落在所有保留分层之外的备份；演练证据、未验证/损坏/未知集合、最新备份一律保护。

## 正式恢复（生产）——务必谨慎

本仓库的脚本 **仅用于恢复到测试库**，不提供恢复进正式库的自动化。正式恢复属于高风险停机操作，必须：

1. 先对当前正式库做一次全新备份并校验。
2. 停止后端服务（不再写入），确认无活动连接。
3. 二次确认目标确实是要恢复的实例，且已获得明确停机与恢复授权。
4. 恢复到一个新实例/新库后切换，而不是就地覆盖正在使用的库。
5. 恢复后运行结构校验，再恢复服务。

**禁止把 dump 恢复进正在使用的正式库、禁止 `pg_restore --clean` 就地覆盖、禁止把备份文件提交 Git 或上传公网。**

## 本批次状态

`scripts/database/` 的脚本已通过离线参数验证与 DryRun。截至本次提交，**尚未执行真实隔离恢复演练**（本机 PostgreSQL 需要密码，且不读取真实 `.env`）。首次真实演练应由掌握测试库凭据者按上述步骤在隔离测试库上执行，切勿在正式库上进行。
