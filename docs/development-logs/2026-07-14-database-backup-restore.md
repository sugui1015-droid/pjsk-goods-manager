# PostgreSQL 备份、恢复脚本与隔离恢复演练

## 阶段 1：只读调查

### 基线

- 分支 `main`；`HEAD` = `origin/main` = `9c2c22547f2823b50e5ba5460f0fdd0ade59f183`（`feat: add admin login rate limiting`）；工作区干净、暂存区空。
- 只读调查示例配置、config/database 包、迁移目录、测试隔离机制、`.gitignore`、HANDOVER 与既有文档；未读取真实 `.env`，未连接正式数据库。

### 调查结论表

| 调查项 | 当前情况 | 风险 | 本批次处理方式 |
| --- | --- | --- | --- |
| 连接配置 | `DATABASE_URL` 优先，否则由 `DATABASE_HOST/PORT/USER/PASSWORD/NAME/SSLMODE` 拼装（`config.go`） | — | 脚本不读取任何 `.env`，连接参数由调用者显式传入 |
| 正式数据库名 | 示例配置为 `pjsk`（`backend/.env.example` `DATABASE_NAME=pjsk`）；真实值未读取 | 恢复脚本必须拒绝该名称 | 目标名强制 `pjsk_restore_test_` 前缀，显式黑名单 `pjsk/postgres/template0/template1` |
| 测试隔离机制 | Go 集成测试通过测试进程自身加载 `DATABASE_URL` 连到开发库，用唯一前缀 fixture + `t.Cleanup` 清理；**没有**自动创建临时数据库/schema 的机制 | 演练需要新建独立测试数据库 | 演练用 `pjsk_backup_source_test_<ts>` 全新库，由 psql 按迁移文件顺序初始化 |
| 备份脚本现状 | 仓库内无任何 `pg_dump/pg_restore/createdb/dropdb` 脚本；`backups/`、`pjsk-data-backup/` 是历史数据快照目录（已被 gitignore） | 无可复用工具 | 本批次新建 `scripts/database/*.ps1` |
| PostgreSQL 工具 | `D:\PostgreSQL\18\bin` 存在，`pg_dump (PostgreSQL) 18.4`；不在 PATH | — | 脚本默认 `D:\PostgreSQL\18\bin`，可参数覆盖，用 `Test-Path` 精确探测不扫盘 |
| `.gitignore` | 已忽略 `backups/`、`pjsk-data-backup/`、`*.db` 等；未忽略 `*.dump/*.backup/*.partial` | dump 误提交风险 | 追加三条精确扩展名规则，不加宽泛 JSON 规则 |
| 迁移记录 | `schema_migrations(version text pk, applied_at)`，version=完整文件名；迁移止于 `0018_query_code_email_recovery.sql`（历史双 `0005` 无 `0006`） | — | 备份必须包含 `schema_migrations`（完整逻辑备份天然包含），校验以最大 version 为准 |
| 数据库对象 | 21 张表（注意实际是 `admins`，不是指令示例中的 `admin_users`）+ runner 建的 `schema_migrations`；扩展 `pgcrypto`；UUID 主键 `gen_random_uuid()`；**无** serial/identity → 无序列；无自建函数/触发器；无大对象；53 个索引、13 个迁移文件含外键 | — | 校验脚本按实际对象类型比对（序列按"数量一致（预期 0）"处理） |
| 备份格式 | — | plain SQL 明文暴露业务数据、恢复可靠性差 | custom format（`--format=custom`，`.dump`），`pg_restore --list` 可校验、可选择恢复 |
| 恢复演练载体 | — | 同库新 schema 会与源对象混杂且 `pgcrypto` 装在库级 | 使用**全新数据库**恢复 |
| 版本兼容 | 客户端/服务端同为 PostgreSQL 18 | 跨大版本恢复需用等于或更高版本的 pg_restore | 文档说明；本批次同版本演练 |
| 备份保存目录 | — | 仓库内目录会被 Git/同步工具波及 | 建议 `D:\PJSK-Backups\PostgreSQL`（通用示例），脚本拒绝仓库内路径 |
| 凭据 | 当前 shell 无 `PGPASSWORD`、无 `%APPDATA%\postgresql\pgpass.conf`；`psql -w` 连接 127.0.0.1 报 `fe_sendauth: no password supplied`——**本机需要密码** | 无法在不读真实 `.env` 的前提下获得连接 | 脚本仅接受进程内 `PGPASSWORD`/pgpass/交互提示；实际演练前询问用户以安全方式提供凭据，否则按指令降级为"脚本 + dry-run" |

### Git 状态

- 阶段末仅新增本日志；`git diff --check` 通过；暂存区空；无删除、无重命名。

### 下一阶段

- 阶段 2/3：备份与恢复方案设计定稿。

## 阶段 2/3：备份与恢复方案设计

### 备份

- 格式：custom format（`pg_dump --format=custom`，扩展名 `.dump`）。不用 plain SQL 作默认：明文暴露业务数据、无法 `pg_restore --list` 校验、无选择性/并行恢复空间。
- 参数：`--format=custom --no-owner --no-privileges --verbose`。采用 `--no-owner/--no-privileges`：单机恢复到任意角色下最可靠，本项目无跨角色授权需求。不使用 `--clean/--create`；备份脚本只生成 dump，不删不建库。
- 范围：完整逻辑备份（schema、表、索引、约束、数据、`schema_migrations`；本库无序列/自建函数/触发器/大对象，`pgcrypto` 扩展声明包含在 dump 中，PG13+ 为 trusted extension，恢复无需超级用户）。
- 密码：不接受密码参数；仅使用进程内已有 `PGPASSWORD`、pgpass.conf 或 PostgreSQL 工具自身交互提示。不打印、不写日志、不写 metadata、不设永久环境变量、不删除用户已有 `PGPASSWORD`。
- 目录：调用者显式传入 `-BackupRoot`；脚本解析为绝对路径并**拒绝位于本 Git 仓库内**；自动建 `yyyy\MM` 层级；文件名 `pjsk-yyyyMMdd-HHmmss.dump`（不含用户/主机/密码/业务信息）。
- 原子性：先写 `<名称>.dump.partial`；`pg_dump` 退出码 0 → `pg_restore --list` 可读 → SHA-256 计算成功 → 改名 `.dump`（同名已存在则失败不覆盖）→ 生成 `<名称>.metadata.json`。任一步失败删除 `.partial` 并返回非零退出码。
- metadata 字段仅：创建时间(UTC)、pg_dump 客户端版本、dump 文件名、字节大小、SHA-256、数据库逻辑别名（调用者传入的库名）、`pg_restore --list` 对象行数、格式。无密码/DSN/用户/主机/业务数据/环境变量值。
- 保留策略：只写入文档建议（7 天日备/4 周周备/12 月月备），不实现自动删除。

### 恢复（仅测试目标）

- 两个动作分离在一个脚本内顺序执行：`createdb` 新建空库 → `pg_restore` 恢复；绝不恢复到源库。
- 目标名强制 `^pjsk_restore_test_[a-z0-9_]+$` 且总长 ≤63；显式拒绝 `pjsk/postgres/template0/template1`；目标已存在直接失败，不自动 drop。
- 恢复参数：`--dbname=<target> --no-owner --no-privileges --exit-on-error --single-transaction --verbose`。`--single-transaction` 与本 dump 兼容（无 CONCURRENTLY 类语句）。禁止 `--clean/--create`。
- 恢复失败：保留目标库供诊断并明确提示；修复后换新目标名重试；自动化演练最终由 cleanup 删除。
- 建库/删库用 `createdb`/`dropdb` 可执行文件 + 参数数组传名（名称已过严格白名单正则），不拼接任意 SQL；存在性检查用 `psql -v` 变量绑定（`:'name'`），不字符串内插。
- 清理脚本：仅允许 `pjsk_restore_test_` 与 `pjsk_backup_source_test_` 两个明确测试前缀（后者为本批次演练源库前缀——比用裸 dropdb 清理源库更安全，是对指令"仅 restore 前缀"的显式扩展）；一次只删一个显式命名的库；先 `pg_terminate_backend` 断连，再 `dropdb`，最后确认不存在；支持 DryRun。
- 所有脚本：参数数组调用外部命令，无 `Invoke-Expression`、无 `cmd /c` 拼接；检查 `$LASTEXITCODE`；DryRun 只输出安全概要；不用 `$Host` 作变量名。

## 阶段 4：脚本实现

- 新增 `scripts/database/`：`Backup-Postgres.ps1`、`Restore-PostgresTest.ps1`、`Test-PostgresBackup.ps1`、`Remove-PostgresRestoreTest.ps1`，以及离线安全测试 `Invoke-ScriptSafetyTests.ps1`。
- 均按阶段 2/3 设计实现；`Test-PostgresBackup.ps1` 的关键表清单按实际 schema 修正为 `admins`（非 `admin_users`），并纳入 `query_sessions`、`user_recovery_emails`、`recovery_email_verification_codes`、`query_code_recovery_*`、`user_query_code_bind_tokens`、`account_security_audit_logs`；表名来自固定白名单常量，`to_regclass('public.<table>')` 判断存在，不接受外部表名。
- 存在性/终止连接的 psql 查询使用 `-v target=...` + `:'target'` 变量绑定，不做字符串内插。

## 阶段 5：脚本静态安全测试

- `Invoke-ScriptSafetyTests.ps1` 无需数据库连接、无需密码，覆盖：缺参失败、非法库名、仓库内 BackupRoot 拒绝、相对路径拒绝、缺工具失败、DryRun 成功且不含 `PGPASSWORD`；恢复目标 `pjsk`/`postgres`/`template1`/无前缀/大写/缺 dump/非 `.dump`/缺工具全部拒绝、DryRun 成功且注明禁用 `--clean/--create`；校验脚本拒绝非测试库；cleanup 拒绝 `pjsk`/`postgres`/`template0`/任意名/含 `%` 名、DryRun 成功；含空格路径；静态检查四个脚本无 `Invoke-Expression`、无 `cmd /c`、不传 `--clean`、不传 `--create`、不赋值 password。
- 结果：全部 44 项 PASS（`RESULT: all safety tests passed`）。运行中修正两处：PS 5.1 正则转义写法、以及子脚本 stderr 在 5.1 下产生 NativeCommandError 需临时降级 `$ErrorActionPreference` 收集输出。

## 阶段 6–11：隔离恢复演练（未执行，按边界降级）

- 演练需连接本机 PostgreSQL（127.0.0.1:5432）创建 `pjsk_backup_source_test_*` 与 `pjsk_restore_test_*` 两个临时库。核对：当前 shell 无 `PGPASSWORD`、无 `%APPDATA%\postgresql\pgpass.conf`，`psql -w` 明确报 `fe_sendauth: no password supplied`——本机需要密码。
- 指令禁止读取真实 `.env`/`backend/.env` 获取凭据；就"如何安全提供测试库凭据"询问用户，用户未回应。
- 按指令降级规则（"若无法在不读取真实 .env 的前提下安全获得测试数据库连接，停止实际演练，只完成脚本与 dry-run，不得突破边界"）：**未执行真实备份/恢复/校验/清理**；未创建任何临时数据库；未生成任何真实 dump/metadata/.partial 文件；未连接数据库。脚本正确性由离线参数验证与 DryRun 覆盖。
- 未对正式数据库执行任何备份、恢复、删除、覆盖或结构修改；未连接正式数据库。
- 真实隔离演练留待用户以安全方式提供测试库凭据后，按 `docs/database-backup-restore.md` 步骤执行。

## 接手补充：真实演练前脚本复核与最小修复

- 接手时分支、HEAD、origin/main、空暂存区及 7 个未跟踪项目文件均与交接一致；另有受保护的本地 `.claude/settings.local.json`，未读取、修改、哈希或处理。
- 重新运行 Claude 原始离线安全测试，44 项全部通过。
- 复核迁移 runner：按完整文件名字典序执行并把完整文件名写入 `schema_migrations`；历史两个 `0005` 都会执行，没有 `0006`，最终迁移为 `0018_query_code_email_recovery.sql`。
- 按实际 0018 迁移确认查询码找回对象为 `query_code_recovery_request_events`、`query_code_recovery_codes` 和 `query_code_recovery_sessions`；未采用不存在的泛称 tokens/events 表。
- 最小修复备份发布流程：metadata 先写 `.metadata.json.partial`，dump 与 metadata 分别原子提升；拒绝覆盖任一最终文件；异常清理 partial，若 dump 已提升但 metadata 发布失败则明确标记为不完整，禁止误报成功。
- 最小修复恢复校验：源库只允许 `pjsk_backup_source_test_*`；最大迁移必须精确为 0018；主外键只统计 public schema；明确校验序列为 0、`pgcrypto` 存在且 `gen_random_uuid()` 可调用。
- `.gitignore` 仅追加 `*.dump`、`*.backup`、`*.partial`，未添加宽泛 JSON、scripts 或 docs 忽略规则。
- 本批次尚未连接 PostgreSQL、未创建数据库或备份文件、未操作正式数据库；未读取或输出 `PGPASSWORD`，未读取真实 `.env`。

## 阶段 7 补充：备份前只读数据库与命令目标核对

- 通过 maintenance 数据库 catalog 只读查询确认服务端版本为 PostgreSQL 18.4。
- 当前检测到的数据库名称：pjsk, postgres, template0, template1。仅查询数据库名称，未连接任何业务表或读取业务数据。
- 正式业务数据库准确名称确认为 `pjsk`；本阶段未连接该数据库。后续它只允许作为 `pg_dump` 的只读备份源，绝不作为 createdb、pg_restore 或 dropdb 目标。
- 隔离源库前缀为 `pjsk_backup_source_test_`；隔离恢复库前缀为 `pjsk_restore_test_`。恢复脚本仅接受后者，校验脚本源库仅接受前者。
- `createdb` 仅在明确的新 `pjsk_restore_test_*` 目标上由恢复脚本使用；`pg_restore` 同样只指向该新目标，且禁止 clean/create；`dropdb` 清理脚本只接受两个测试前缀、一次一个明确名称，并在删除前后检查存在性。
- 备份脚本的 `DatabaseName` 允许安全 PostgreSQL 标识符，正式使用时调用方仍必须显式传入 `pjsk`；隔离演练则只传入本轮唯一 `pjsk_backup_source_test_*`。该参数边界不具破坏性，但执行前仍需再次核对。
- 文档与脚本未发现会把恢复或删除目标指向正式库的阻断性风险；密码处理仍依赖调用环境，脚本不接受或记录密码。
- 未读取密码文件内容或真实 .env，未执行写入、创建、备份、恢复、删除或迁移。
- 本阶段仅追加本开发日志。下一阶段在用户确认后生成一次性安全名称和仓库外测试目录，并在执行任何创建前再次核对所有参数。


## 阶段 8 补充：隔离演练名称、路径与命令参数准备

- 本轮唯一标识：`20260714_144913_41b381af`。仅含小写字母、数字和下划线。
- 隔离源数据库：`pjsk_backup_source_test_20260714_144913_41b381af`；符合 `pjsk_backup_source_test_` 前缀、字符集和 63 字节内长度限制。
- 隔离恢复数据库：`pjsk_restore_test_20260714_144913_41b381af`；符合 `pjsk_restore_test_` 前缀、字符集和 63 字节内长度限制。两个名称不同，且均不等于受保护数据库 `pjsk/postgres/template0/template1`。
- 仓库外空测试目录：`D:\pjsk-backup-restore-tests\20260714_144913_41b381af`。已确认是绝对路径且不位于 `D:\pjsk` 内。
- 预定 dump：`D:\pjsk-backup-restore-tests\20260714_144913_41b381af\pjsk_backup_source_test_20260714_144913_41b381af.dump`；partial：`D:\pjsk-backup-restore-tests\20260714_144913_41b381af\pjsk_backup_source_test_20260714_144913_41b381af.dump.partial`；metadata：`D:\pjsk-backup-restore-tests\20260714_144913_41b381af\pjsk_backup_source_test_20260714_144913_41b381af.metadata.json`；校验结果：`D:\pjsk-backup-restore-tests\20260714_144913_41b381af\20260714_144913_41b381af.validation.json`。均为绝对仓库外路径，扩展名符合约定，且当前均不存在。
- 路径文本未包含密码、连接字符串或用户名密码组合；未读取密码文件内容或真实 .env。
- 未来命令参数模板固定为 127.0.0.1:5432、用户 postgres、`-w` 和显式 PGPASSFILE；本阶段只输出模板，未执行 createdb、pg_dump、pg_restore 或 dropdb。
- 实际仅创建上述仓库外空目录并追加本日志；未连接正式数据库，未创建数据库、备份、恢复或删除。
- 下一阶段在用户确认后，仅创建并初始化 `pjsk_backup_source_test_20260714_144913_41b381af`，执行前再次核对名称和 createdb/迁移目标；`pjsk` 不作为任何写入目标。


### 阶段 9.1：源库创建前只读门禁

- maintenance catalog 确认 `pjsk_backup_source_test_20260714_144913_41b381af` 不存在、`pjsk_restore_test_20260714_144913_41b381af` 不存在、正式数据库 `pjsk` 存在。
- 两个测试名称重新通过固定值、前缀、字符集、长度、互异及受保护名称检查。
- 仅查询数据库目录，未连接正式业务数据库或业务表，未执行写操作。

### 阶段 9.2：创建隔离源数据库

- 使用 `createdb -w`、maintenance 数据库 `postgres` 和系统模板 `template0`，仅创建 `pjsk_backup_source_test_20260714_144913_41b381af`。
- 创建后通过 maintenance catalog 确认该精确名称存在。
- 未创建恢复数据库，未使用正式数据库作为模板，未执行跨库复制、备份、恢复或删除。

### 阶段 9.3：初始化隔离源数据库结构

- 未读取真实 .env，直接把每条 `psql -w` 命令的目标显式设为 `pjsk_backup_source_test_20260714_144913_41b381af`。
- 按完整文件名字典序、逐文件单事务应用 18 个迁移，并把完整文件名写入 `schema_migrations`：0001_core_tables.sql, 0002_import_tracking.sql, 0003_admin_auth.sql, 0004_import_confirm.sql, 0005_import_history.sql, 0005_product_series.sql, 0007_import_revert.sql, 0008_query_sessions.sql, 0009_admin_payments.sql, 0010_payment_voids.sql, 0011_payment_fee_fields.sql, 0012_normalize_payment_methods.sql, 0013_cn_merge.sql, 0014_user_query_account_admin.sql, 0015_query_code_bind_tokens.sql, 0016_user_recovery_email.sql, 0017_recovery_email_verification_codes.sql, 0018_query_code_email_recovery.sql。
- 历史两个 0005 均已执行；最终计数 18，最大版本为 `0018_query_code_email_recovery.sql`。全部迁移成功。
- 未连接正式数据库，未复制、导入或查询任何正式业务数据。

### 阶段 9.4：迁移完整性与空库验证

- `pjsk_backup_source_test_20260714_144913_41b381af` 可连接；关键表全部存在，public schema 共 21 张表。
- `schema_migrations` 有 18 行；所有 20 张业务表均为 0 行：account_security_audit_logs=0, admin_sessions=0, admins=0, cn_merge_logs=0, import_batches=0, import_errors=0, order_items=0, orders=0, payment_items=0, payments=0, products=0, projects=0, query_code_recovery_codes=0, query_code_recovery_request_events=0, query_code_recovery_sessions=0, query_sessions=0, recovery_email_verification_codes=0, user_query_code_bind_tokens=0, user_recovery_emails=0, users=0。
- 结束时 catalog 再确认源库存在、恢复库不存在、正式库存在；未连接正式业务数据库。
- 本阶段未生成 dump、metadata 或 validation 文件，未删除数据库。下一阶段在用户确认后仅向隔离源库写入最小虚构 fixture。
## 阶段 10 补充：隔离源库最小虚构 fixture

- 写入目标在执行前再次固定并验证为 `pjsk_backup_source_test_20260714_144913_41b381af`；maintenance catalog 同时确认恢复库仍不存在、正式库仅存在但未连接。
- 仅通过隔离源库的 information_schema/pg_constraint 核对列、默认值、主键、外键、唯一约束、非空和检查约束；未查询正式数据库结构或业务表。
- 使用一个明确事务写入完全虚构实体：1 个管理员、2 个用户、1 个项目、2 个商品、2 个订单、2 个订单明细、1 个 approved 付款、1 个付款分配。事务完整提交，无半套数据。
- 虚构标识使用 `TEST_FIXTURE_ADMIN_001`、`TEST_FIXTURE_001/002`、`BACKUP_RESTORE_FIXTURE_PROJECT`、`BACKUP_RESTORE_FIXTURE_SKU_01/02`、`BACKUP_RESTORE_FIXTURE_ORDER_001/002`；未使用真实邮箱、密码、查询码、令牌、绑定码或验证码。
- 预期并实测行数：admins=1，users=2，projects=1，products=2，orders=2，order_items=2，payments=1，payment_items=1；schema_migrations 保持 18。
- 外键和唯一约束通过；2 个订单各有 1 条明细；付款及分配属于第一个用户的订单；超额分配 0，订单金额不一致 0，付款分配不一致 0；两个用户各自拥有独立订单。
- 未关闭约束、外键或触发器，未修改 schema_migrations，未执行迁移、建库、删库、备份或恢复。
- 未创建临时 SQL 文件；SQL 仅作为当前 psql 调用的内存参数。未读取密码文件内容或真实 .env，未连接或读取正式业务数据库。
- 下一阶段在用户确认后对该隔离源库执行备份 DryRun，再执行正式 custom-format 备份；不会自动进入。
## 阶段 11：隔离源数据库备份脚本 DryRun

- 固定隔离源数据库为 `pjsk_backup_source_test_20260714_144913_41b381af`，启用 `RequireIsolatedSource` 门禁；正式数据库 `pjsk` 被该门禁拒绝，未作为本次备份源或任何写入、恢复、删除目标。
- 固定仓库外目录为 `D:\pjsk-backup-restore-tests\20260714_144913_41b381af`；固定最终文件为同目录下 `pjsk_backup_source_test_20260714_144913_41b381af.dump`，临时文件为其 `.partial`，metadata 为同名前缀 `.metadata.json`。
- DryRun 前发现原脚本会自动生成年月目录和时间文件名、在覆盖检查前退出，且预计命令缺少 `-w`。仅加固 `Backup-Postgres.ps1`：支持显式 `OutputPath`、隔离源门禁、扩展名与 BackupRoot 边界校验、final/partial/metadata/metadata partial 四项覆盖检查，以及与真实执行共用的 `-w` custom-format 参数数组；未修改业务代码、迁移或数据库内容。
- 更新离线安全测试，覆盖正式库被隔离源门禁拒绝、固定路径与 `.partial` 预览、`-w`、禁止 clean/create，以及 DryRun 遇到已存在目标时拒绝覆盖。离线安全测试全部通过（48 项 PASS）。
- 固定参数 DryRun 成功；预计命令类型为 `pg_dump` custom-format 逻辑备份，主机 127.0.0.1、端口 5432、用户 postgres、`-w`，输出先写固定 `.dump.partial`。真实运行时仅在 dump 可被 `pg_restore --list` 读取、SHA-256 与 metadata 准备完成后，才把 partial 原子发布到最终路径。
- DryRun 未执行 `psql`、`pg_dump`、`pg_restore`、`createdb` 或 `dropdb`，未连接 PostgreSQL，未读取密码文件或真实 `.env`，未输出或记录密码、DSN 或其他凭据值。
- 仓库外测试目录运行前后均为 0 个条目；未创建、覆盖、重命名或删除 dump、partial、metadata 或 validation 文件，无仓库外副作用。
- 本阶段仓库修改仅为 `scripts/database/Backup-Postgres.ps1`、`scripts/database/Invoke-ScriptSafetyTests.ps1` 和本日志追加；未暂存、未提交、未推送。
- 下一阶段需经用户确认后才对同一隔离源库执行正式 custom-format 备份，并核对 dump 可读性、metadata、校验值和零残留 partial；本阶段不自动进入。
## 阶段 12：隔离源数据库正式 custom-format 备份

- 固定唯一标识：`20260714_144913_41b381af`。
- 备份源数据库：`pjsk_backup_source_test_20260714_144913_41b381af`，执行前已再次确认精确名称、`pjsk_backup_source_test_` 前缀、小写字母/数字/下划线字符集、长度不超过 63，且不等于 `pjsk/postgres/template0/template1`。
- 执行脚本：`D:\pjsk\scripts\database\Backup-Postgres.ps1`，参数摘要为固定 `BackupRoot`、固定 `OutputPath`、`PostgresBin=D:\PostgreSQL\18\bin`、`HostName=127.0.0.1`、`Port=5432`、`Username=postgres`、`RequireIsolatedSource`；凭据仅通过临时 `PGPASSFILE` 路径交给 PostgreSQL 工具，未读取或记录密码文件内容。
- 正式备份前五个目标均不存在：final dump、dump partial、final metadata、metadata partial、validation。
- 由于正式备份前发现 metadata 字段尚不足，本阶段仅最小补强备份脚本：metadata 增加 `schemaVersion`、`sourceDatabaseName`、`isolatedTestBackup`、`dumpFormat`、dump 文件名、dump 字节数、dump SHA-256、备份脚本 SHA-256、fixture 预期行数和对象计数；发布失败时会清理本次生成的 dump final，避免留下 dump 已发布但 metadata 未发布的不一致结果。
- 补强后重新运行离线安全测试：48 项 PASS，`RESULT: all safety tests passed`；随后固定参数 DryRun 通过，确认预期 `pg_dump` 为 custom format、使用 `-w`、源库为隔离源库、先写 `.dump.partial`，且不使用 `--clean` 或 `--create`。
- 正式 `pg_dump` 成功，仅连接隔离源数据库并执行只读 custom-format 逻辑备份；未连接正式业务数据库 `pjsk`，未读取或复制正式业务数据，未修改隔离源数据库。
- final dump：`D:\pjsk-backup-restore-tests\20260714_144913_41b381af\pjsk_backup_source_test_20260714_144913_41b381af.dump`，大小 `76979` bytes，SHA-256 `DD37ADADBF935D25EA7031A5645DD18CC6A448F72CBE8838937F0BAD04007730`。
- final metadata：`D:\pjsk-backup-restore-tests\20260714_144913_41b381af\pjsk_backup_source_test_20260714_144913_41b381af.metadata.json`；可解析，源库名、dump 格式、dump 文件名、大小和 SHA-256 均与 final dump 一致，且标记为隔离测试备份。
- `pg_restore --list` 校验成功：TOC 对象行 `166`，包含 `pgcrypto` extension、21 个 public 表定义、21 个 public TABLE DATA 条目、69 个 public 约束和 53 个 public 索引。PostgreSQL 18 的 TOC 未单独输出默认 `public` schema 条目，但表结构和数据对象均位于 public schema。
- fixture 预期行数写入 metadata：admins=1, users=2, projects=1, products=2, orders=2, order_items=2, payments=1, payment_items=1, schema_migrations=18。
- 原子发布结果：dump partial 校验成功后写入 metadata partial 并校验，然后发布 final dump 与 final metadata；发布后 final dump 与 final metadata 同时存在，`.dump.partial` 与 `.metadata.json.partial` 均不存在。
- 本阶段未生成 validation 文件，validation 留待恢复验证阶段。
- 未读取真实 `.env`，未读取、显示、复制、哈希或记录密码文件内容，未使用 `PGPASSWORD`，未记录 DSN 或带密码连接字符串。
- 仓库内修改范围：`scripts/database/Backup-Postgres.ps1` 最小补强、`docs/development-logs/2026-07-14-database-backup-restore.md` 追加本记录；未暂存、未提交、未推送。
- 下一阶段：经用户确认后，基于该 final dump 创建 `pjsk_restore_test_20260714_144913_41b381af` 隔离恢复数据库并执行恢复演练；本阶段不自动进入恢复。

## 阶段 13：隔离恢复数据库创建、恢复与验证

- 固定唯一标识：`20260714_144913_41b381af`。
- 恢复前门禁：maintenance 数据库只读确认正式数据库 `pjsk` 存在、隔离源数据库 `pjsk_backup_source_test_20260714_144913_41b381af` 存在、隔离恢复目标 `pjsk_restore_test_20260714_144913_41b381af` 不存在。恢复目标名称通过精确值、`pjsk_restore_test_` 前缀、小写字母/数字/下划线、长度不超过 63、非受保护库名、不同于源库等检查。
- 恢复前产物校验：final dump 存在且大小为 `76979` bytes，SHA-256 为 `DD37ADADBF935D25EA7031A5645DD18CC6A448F72CBE8838937F0BAD04007730`；metadata 可解析且源库名、dump 文件名、大小、SHA-256、custom format、隔离测试标记和 fixture 预期行数均一致；`pg_restore --list` 成功，TOC 对象行 `166`；validation 与 validation partial 起始均不存在。
- 恢复脚本最小加固：`Restore-PostgresTest.ps1` 与 `Test-PostgresBackup.ps1` 的 PostgreSQL 命令参数显式加入 `-w`，恢复建库命令显式使用 `template0`；加固后 PowerShell 语法检查通过，离线安全测试 48 项 PASS。未修改业务代码、迁移或正式数据。
- 创建隔离恢复数据库：仅创建 `pjsk_restore_test_20260714_144913_41b381af`，使用 `createdb -w --template template0`。创建后一次 maintenance 存在性检查因 `psql -c` 变量替换语法未生效而失败；未删除数据库、未执行恢复。随后使用已验证的固定目标库名只读连接确认 `current_database()` 正确，且恢复前 public base table count 为 0。
- 恢复执行：使用 `pg_restore -w --dbname pjsk_restore_test_20260714_144913_41b381af --no-owner --no-privileges --exit-on-error --single-transaction --verbose` 从已发布 final dump 恢复；未使用 `--clean`、`--create` 或 `--if-exists`；恢复成功。
- 恢复后结构验证：public base table count `21`；`schema_migrations` 行数 `18`，最大版本 `0018_query_code_email_recovery.sql`；primary keys `21`，foreign keys `38`，unique constraints `10`，check constraints `40`，indexes `84`，`pgcrypto` 存在。
- fixture 行数验证：admins=1, users=2, projects=1, products=2, orders=2, order_items=2, payments=1, payment_items=1, schema_migrations=18。未写入 fixture 的业务表均为 0：account_security_audit_logs, admin_sessions, cn_merge_logs, import_batches, import_errors, query_code_recovery_codes, query_code_recovery_request_events, query_code_recovery_sessions, query_sessions, recovery_email_verification_codes, user_query_code_bind_tokens, user_recovery_emails。
- 关系与金额验证：两个订单各 1 条 order item；两个虚构用户各 1 个订单；approved 付款正确关联第一个虚构用户；payment item 正确关联对应订单明细；超额分配数 0；订单汇总金额不一致数 0；付款分配金额不一致数 0；order item/payment item 孤儿记录均为 0；fixture 用户、项目、商品、订单标识计数均符合预期。
- 隔离源库与隔离恢复库非敏感对比：迁移数、表数量、主外键/索引/扩展状态、fixture 相关表行数和关系统计均一致，无 mismatch。
- validation 已生成并发布：`D:\pjsk-backup-restore-tests\20260714_144913_41b381af\20260714_144913_41b381af.validation.json`，总体结果 `passed`；validation partial 发布后不存在。
- 安全结论：未读取真实 `.env`；未读取、显示、复制、哈希或记录密码文件内容；未记录 DSN 或带密码连接字符串；未连接或修改正式数据库 `pjsk` 的业务表；未修改隔离源数据库；未删除隔离源库、隔离恢复库、dump、metadata、validation、测试目录或 `.local-secrets`。
- 仓库内修改范围：`scripts/database/Restore-PostgresTest.ps1`、`scripts/database/Test-PostgresBackup.ps1` 和本开发日志追加。仓库外新增 validation final 文件，且保留已生成的 dump 与 metadata。
- 本阶段未暂存、未提交、未推送；未使用子代理。
- 下一阶段：经用户确认后执行最终复核或清理计划；本阶段不自动删除任何隔离数据库，也不自动进入提交或推送。

## 阶段 14：最终只读复核、清理与提交前验证

- 最终只读复核通过：maintenance 数据库确认正式数据库 `pjsk`、隔离源数据库 `pjsk_backup_source_test_20260714_144913_41b381af`、隔离恢复数据库 `pjsk_restore_test_20260714_144913_41b381af` 在清理前均存在；两个隔离库 `current_database()` 正确，public 表数量均为 21，`schema_migrations` 均为 18，fixture 相关表非敏感计数一致。
- 仓库外产物完整性复核通过：dump、metadata、validation 均存在；`.dump.partial`、`.metadata.json.partial`、`.validation.json.partial` 均不存在；dump 大小 `76979` bytes；SHA-256 `DD37ADADBF935D25EA7031A5645DD18CC6A448F72CBE8838937F0BAD04007730`；metadata 可解析且与 dump 一致；validation 可解析且 `overallResult=passed`；`pg_restore --list` 可读取 dump，TOC 对象行 166。
- 脚本与文档复核发现并完成最小修复：文档示例改为使用 `PGPASSFILE` 与 pgpass 文件，不再引导把密码写入命令行；恢复/清理脚本显式使用 `-w`；恢复/清理脚本中 maintenance catalog 检查改为在严格库名白名单校验后使用固定库名 SQL，避免 Windows `psql -c` 变量替换问题。未重新执行正式备份或恢复。
- 清理顺序按要求完成：先删除恢复数据库 `pjsk_restore_test_20260714_144913_41b381af` 并确认不存在；再删除源数据库 `pjsk_backup_source_test_20260714_144913_41b381af` 并确认不存在；随后确认正式数据库 `pjsk` 仍存在。
- 临时凭据目录 `D:\pjsk\.local-secrets` 已删除并确认不存在；当前命令环境中的 `PGPASSFILE` 与 `PGPASSWORD` 已清除；未读取、显示、复制、哈希或记录密码文件内容；未读取真实 `.env`。
- 演练产物目录保留：`D:\pjsk-backup-restore-tests\20260714_144913_41b381af`。保留文件包括 final dump、metadata 和 validation，作为本次演练证据；未删除或覆盖仓库外产物。
- 清理后最终测试通过：PowerShell 脚本语法检查全部通过；离线安全测试 48 项 PASS，`RESULT: all safety tests passed`；固定隔离源库参数 DryRun 通过且未连接数据库、未创建产物；`git diff --check` 无阻断性 whitespace 错误。
- Git 范围复核：暂存前仅包含 `.gitignore`、`docs/database-backup-restore.md`、`docs/development-logs/2026-07-14-database-backup-restore.md` 与 `scripts/database/` 下 5 个数据库脚本；`.claude/settings.local.json` 仍为本地未跟踪文件，未读取、未修改、未暂存；`.local-secrets` 未进入 Git；仓库内未发现 `.dump`、`.backup` 或 `.partial` 产物。
- 已知非阻断警告：Git 仍提示用户级 ignore `C:\Users\苏归/.config/git/ignore` 权限不可访问；`.gitignore` 下次被 Git 触碰时可能出现 LF 转 CRLF 警告。按要求仅记录，不修改全局 Git 配置，不做无关换行转换。
- 推荐清理顺序已执行完毕：恢复库 -> 源库 -> 临时凭据目录；正式数据库保留，演练产物保留。
- 推荐提交范围：`.gitignore`、`docs/database-backup-restore.md`、本开发日志、`scripts/database/Backup-Postgres.ps1`、`scripts/database/Restore-PostgresTest.ps1`、`scripts/database/Test-PostgresBackup.ps1`、`scripts/database/Invoke-ScriptSafetyTests.ps1`、`scripts/database/Remove-PostgresRestoreTest.ps1`。
- 排除提交：`.claude/settings.local.json`、`.local-secrets`、仓库外演练目录、dump、metadata、validation、任何密码/DSN/真实 `.env`、无关业务代码。
- 本阶段未提交、未推送；下一步为精确暂存上述允许文件，创建提交并普通推送。

## 阶段 15：主提交与首次推送结果记录

- 主提交已创建：`1a89c957970c6035d27c89352f27072157b3e304`。
- 主提交标题：`feat: add PostgreSQL backup and restore tooling`。
- 主提交文件范围：`.gitignore`、`docs/database-backup-restore.md`、本开发日志、`scripts/database/Backup-Postgres.ps1`、`scripts/database/Restore-PostgresTest.ps1`、`scripts/database/Test-PostgresBackup.ps1`、`scripts/database/Invoke-ScriptSafetyTests.ps1`、`scripts/database/Remove-PostgresRestoreTest.ps1`。
- 首次普通推送结果：成功，`main -> origin/main`，未使用强制推送，未改写历史。
- 首次推送后 `HEAD` 与 `origin/main` 一致，均为 `1a89c957970c6035d27c89352f27072157b3e304`。
- 清理后确认：两个隔离数据库已删除；正式数据库 `pjsk` 在清理后确认仍存在；`D:\pjsk\.local-secrets` 已删除；仓库外 dump、metadata、validation 均保留。
- 本条为满足提交与推送结果留痕的日志收尾记录；后续仅将本日志追加作为日志-only 收尾提交并普通推送。
- 未记录密码、DSN、密码文件内容或真实业务数据。
