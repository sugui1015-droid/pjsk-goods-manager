# 隔离 PostgreSQL 迁移与启动超时集成测试

## 任务边界

- 日期：2026-07-15
- 本阶段**允许**连接本机 PostgreSQL 的隔离测试库（名称须匹配 `^pjsk_migration_test_[a-z0-9_]+$`）。
- **严禁**连接、查询或修改正式库 `pjsk`；禁止读取 `backend/.env`、使用正式 `DATABASE_URL`、查询正式业务表、修改正式 `schema_migrations`、修改历史迁移 SQL。
- 本阶段不提交、不推送。

## 阶段 1：只读接管检查与环境确认

- 开始时间：2026-07-15 17:05（本机时间）
- 分支 `main`；`git status --short` 为空（工作区、暂存区干净）；无未跟踪文件。
- `git rev-parse HEAD` = `git rev-parse origin/main` = `84fd194e1150b7898c6a3769df93da713f7aaeba`，与交接基线一致。
- `git log -5 --oneline`：`84fd194` / `ce6cb57` / `58e7a54` / `c45e85c` / `ab25adb`，一致。
- 已阅读：`AGENTS.md`、`HANDOVER.md`、`docs/migration-numbering-deployment-check.md`、`docs/database-backup-restore.md`、两份 2026-07-15 开发日志、`backend/main.go`、`backend/internal/database/postgres.go`、`backend/internal/database/migrations.go`、`migrations_test.go`、`backend/main_test.go`、`backend/migrations/`（19 个文件）。

### 工具与环境

- `psql.exe` / `createdb.exe` / `dropdb.exe` **不在 PATH**；使用已知安装目录 `D:\PostgreSQL\18\bin`（未扫描磁盘）。
- 连接前环境核查：`PGPASSWORD` 未设置、`DATABASE_URL` 未设置、`PGPASSFILE` 未设置；8080 未监听；5432 正在监听。

### 凭据处理（关键）

- 本机存在 `%APPDATA%\postgresql\pgpass.conf`（**仅确认存在，未读取内容**）。
- 因此 `psql -w`（`-w` 永不提示密码）可直接连接，**全程无需输入、读取或设置任何密码**：未设置 `PGPASSWORD`、未写入脚本、未写入日志、未回显、未设置持久环境变量。
- `backend/go.mod` 含 `github.com/jackc/pgpassfile v1.0.0`（pgx 间接依赖），故 Go 侧同样可用**不含密码**的 DSN 由 pgpass 解析，密码不会出现在测试代码、DSN 或日志中。

### 连接目标确认（任务书第四节）

对默认维护库执行 `SELECT current_database(), current_user, version();`：

- `current_database()` = **`postgres`** —— **确认不是 `pjsk`**
- `current_user` = `postgres`
- 版本：PostgreSQL 18.4 on x86_64-windows

### 目标测试库预检

- `SELECT datname FROM pg_database WHERE datname LIKE 'pjsk_migration_test_%'` → **零行**，无他人遗留测试证据，可安全创建。
- 另在维护库中只读确认 `pjsk` 存在（`count = 1`）——**仅为目录存在性检查，未连接、未查询其任何业务表**。

- 是否接触正式库：否。是否读取敏感信息：否。修改文件：仅新建本日志。

## 阶段 2：集成测试实现与执行

- 开始时间：2026-07-15 17:20（本机时间）

### 新增文件（4 个，均为新增；未修改任何既有文件）

| 文件 | 用途 |
| --- | --- |
| `backend/internal/database/isolated_test_support_test.go` | 安全门禁与共用助手：环境门禁、库名正则、禁用名单、DSN 校验、建库/删库、断言助手 |
| `backend/internal/database/migrations_integration_test.go` | 测试 1–4（空库全量、单个 `0005` 跳过语义、失败回滚、超时与断点续跑） |
| `backend/internal/database/connect_integration_test.go` | 测试 5（连接失败快速返回 + 日志不泄露） |
| `docs/development-logs/2026-07-15-isolated-postgres-migration-integration.md` | 本日志 |

### 安全门禁实现

- 环境门禁 `PJSK_RUN_ISOLATED_MIGRATION_TESTS=1`，未设置时 5 个测试全部 `t.Skip`（已实测：`go test ./...` 下全部 SKIP）。
- 库名必须匹配 `^pjsk_migration_test_[a-z0-9_]+$`；禁用名单 `pjsk`/`postgres`/`template0`/`template1`；**建库、连接、删库前各校验一次**（`assertSafeTestDatabaseName` 在三处分别调用），并拒绝含 `"'%;\` 空格 的名称。
- 管理 DSN 校验：主机必须是 `127.0.0.1`/`localhost`/`::1`；库名不得为 `pjsk`；**DSN 字符串不得含密码**（URL userinfo 或 `password=` 关键字）。
- 运行时二次确认：连上管理库后执行 `select current_database()`，若落在 `pjsk` 立即 `t.Fatal`。
- 建库前检查目标是否已存在，存在则 `t.Fatal` 拒绝复用或删除他人遗留库。
- 删库：先 `pg_terminate_backend` **仅针对该库**（`where datname = $1`，参数化），再按精确名 `drop database`；**无通配、一次只删一个**。`t.Cleanup` + 显式删除双重保障（实测失败时也照常删除）。清理失败会以 `CLEANUP FAILED` + 库名报错。
- 标识符用 `pgx.Identifier{}.Sanitize()`，且名称已先过正则。

### 凭据处理

DSN 全程**不含密码**（`postgres://postgres@127.0.0.1:5432/...`），由 pgx 经 `pgpassfile` 依赖读取本机 pgpass 解析——与 `psql -w` 同一机制。未设置 `PGPASSWORD`、未写入脚本/日志、未回显、未设置任何持久环境变量（已逐项核查 User/Machine/Process 三级均为 unset）。

### 使用的隔离数据库（全部已删除）

- `pjsk_migration_test_20260715_fresh_64200`
- `pjsk_migration_test_20260715_s5_import_202500`
- `pjsk_migration_test_20260715_s5_product_542600`
- `pjsk_migration_test_20260715_rollback_517400`
- `pjsk_migration_test_20260715_timeout_485300`

### 测试结果（全部 PASS）

**测试 1 — 空库全量迁移**：19 个迁移按序全部应用，日志逐条可见 `0001` → `0005_import_history.sql` → `0005_product_series.sql` → `0007` → … → `0019`。完整**文件名集合双向对比**通过（仓库→库、库→仓库均无遗漏/无未知记录）；两个 `0005` 均在且 import_history 在前；无 `0006`；`0019` 已应用；`import_batches.warnings_accepted`、`products.series_code`、`admin_auth_audit_events` 表及 6 张业务表均存在；第二次运行为零操作（记录数与表数均不变）。

**测试 2 — 单个 `0005` 跳过语义（两个方向，各用独立新库）**：
- 已有 `0005_import_history.sql` → 全量运行**只补跑** `0005_product_series.sql`，日志证实；已应用者 `applied_at` 未变（未重放）。
- 已有 `0005_product_series.sql` → 全量运行**只补跑** `0005_import_history.sql`。**这条日志顺序（先 product_series 被 seed，后 import_history 被应用）是"按完整文件名精确匹配、而非按数字前缀或顺序"的直接证据。**
- 两种情况最终两条 `0005` 记录各恰好一条、两个 schema 效果都在、链条继续到最大版本 `0019`。

**测试 3 — 失败事务回滚**（合成 FS，未改任何真实迁移文件）：`0002_rollback_failing.sql` 内先建表/加列再执行必然失败的 SQL（同一迁移文件 = 同一事务）。结果：`RunMigrations` 返回错误；该 version **未**记录；`rollback_should_not_exist` 表**不存在**；`rollback_first.added_by_failing_migration` 列**不存在**；先前独立事务提交的 `0001` **保留**。修正后重跑成功，`0001` 未重放（仍只有一条记录）。**无半提交**。

**测试 4 — 超时与断点续跑**：`0002_timeout_slow.sql` 建表后 `pg_sleep(1.5)`，ctx 仅 300ms。结果：300ms 即返回 `timeout: context deadline exceeded`；日志出现 `rollback migration transaction: internal error`——**这正是先前离线分析预测的行为**（ctx 已死无法发送 ROLLBACK，事务由 pgx 关闭连接后服务端中止）；该 version **未**记录；`timeout_slow` 表**不存在**；`0001` 保留。用 60s 新 ctx 重跑后 `0002` 成功应用、`0001` 的 `applied_at` 未变（未重放）、最终恰好 2 条记录。**离线分析在真实数据库上得到验证。**

**测试 5 — 连接失败快速返回**：目标 `127.0.0.1:1`（无监听），凭据为无价值假值。结果：**7ms** 返回错误（远低于 10 秒连接预算，更远低于 2 分钟迁移预算），未返回可用池；`main.go` 实际会记录的 `logsafe.Category(err)` 输出为 `"database connection failed: authentication or connectivity error"`，**不含**哨兵密码、DSN、用户名、`postgres://` 或主机。

### 过程中修正的自身缺陷（诚实记录）

1. 初版守卫用 `config.Password != ""` 判断"DSN 含密码"，**误报**——pgx 的 `ParseConfig` 会从 pgpass 文件解析并填充 `config.Password`，而这正是我们希望它使用的机制。改为校验 **DSN 字符串本身**（URL userinfo + `password=` 关键字）。
2. 初版对 `RunMigrations` 传 `dir="."`，导致 `applyMigration` 拼出 `./x.sql`，被 `io/fs` 判为非法路径（`invalid argument` / `file does not exist`）。**这是测试缺陷，非生产缺陷**——生产 `main.go` 传的是 `"migrations"`。已改为 `os.DirFS("../..")` + `"migrations"`，与生产完全一致；合成 FS 的键也统一放在 `migrations/` 子目录下。
3. 用 PowerShell `Set-Content -Encoding utf8` 批量替换时把注释中的 em-dash 破坏为乱码 `鈥?`。已改回纯 ASCII 并核验文件无任何非 ASCII 字符。

### 验证结果

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `go test -run TestIsolated`（**无**门禁） | 0 | 5 个测试全部 SKIP |
| `go test -count=1 -v -run TestIsolated`（门禁=1） | 0 | **5 个测试全部 PASS**（含 2 个子测试） |
| `go fmt ./...` | 0 | 无差异 |
| `go build ./...` | 0 | 通过 |
| `go vet ./...` | 0 | 通过 |
| `go test ./...`（门禁未设置） | 0 | 全部通过，隔离测试按预期跳过 |
| `Invoke-ScriptSafetyTests.ps1` | 0 | **246 PASS / 0 FAIL**（无回归） |
| `git diff --check` | 0 | 通过 |

### 残留检查

- **测试数据库残留：无**——`SELECT datname FROM pg_database WHERE datname LIKE 'pjsk_migration_test_%'` 返回**零行**；扩展查询 `LIKE 'pjsk_%test%'` 亦零行（早期阶段的 restore/backup 演练库也无残留）。
- 正式库 `pjsk` 仍存在（`count=1`），**仅目录存在性检查，全程未连接、未查询其业务表、未修改**。
- 仓库内无临时 `.sql`、`.dump`、`.backup`、`.partial`、`.metadata.json`、`.validation.json`、pgpass 文件；无 `.sql` 文件进入 Git。
- `backend/migrations` 仍为 19 个文件且 `git status` 为空——**历史迁移未被修改**。
- 环境变量：`PJSK_RUN_ISOLATED_MIGRATION_TESTS`、`PGPASSWORD`、`DATABASE_URL`、`PJSK_ISOLATED_TEST_ADMIN_DSN`、`PGPASSFILE` 在 User/Machine/Process 三级**均为 unset**（门禁仅在测试子进程内短暂设置并在 `finally` 中清除）。
- 8080 未监听；无残留 `pjsk-backend`/`go`/`psql` 进程。

### 安全边界确认

未连接、查询或修改正式库 `pjsk`；未读取 `backend/.env`；未使用正式 `DATABASE_URL`；未查询正式业务表；未修改正式 `schema_migrations`；未修改历史迁移 SQL；未执行正式备份或恢复；未修改 Windows 服务/证书/防火墙/DNS/ACL；未安装软件；未使用子代理；未提交、未推送。密码全程未被输入、读取、显示或持久化。

## 未完成事项

1. 正式数据库 `schema_migrations` 只读核对——待获准部署窗口。
2. 真实备份恢复演练（真实 `pg_dump` + 恢复 + validation 生成）。
3. 正式部署（服务注册、证书、ACL、防火墙、DNS）。

## 下一阶段入口

等待人工审阅。获准后再复核差异、重跑测试、按明确文件列表暂存并提交。
