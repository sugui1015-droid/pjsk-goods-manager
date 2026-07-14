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

只输出对象名、数量与 PASS/FAIL，从不输出业务行。核对 `schema_migrations` 最大版本、关键表存在、主键/外键/索引/序列数量；提供 `-SourceDatabase`（必须是隔离测试库）时逐项对比两库的迁移版本、表数、行数与约束数。

## 清理临时数据库

```powershell
./scripts/database/Remove-PostgresRestoreTest.ps1 -TargetDatabase $target -Username postgres
```

只接受 `pjsk_restore_test_*` 与 `pjsk_backup_source_test_*` 两个测试前缀；拒绝 `pjsk`/`postgres`/`template0`/`template1` 及任何其他名称；一次只删一个显式命名的库；先终止连接再 `dropdb`，最后确认已不存在。支持 `-DryRun`。

## 保留策略（建议，脚本不自动删除）

- 最近 7 天：每日备份
- 最近 4 周：每周备份
- 最近 12 个月：每月备份

自动清理未实现；删除正式备份需人工判断。

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
