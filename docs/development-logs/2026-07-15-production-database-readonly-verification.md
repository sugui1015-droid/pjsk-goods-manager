# 正式数据库 `schema_migrations` 与关键 schema 只读核对

## 任务边界

- 日期：2026-07-15
- 本阶段**允许**连接正式库 `pjsk`，但**只允许执行明确列出的只读 SQL**。
- 禁止运行迁移、备份、恢复或任何写操作；禁止 `INSERT`/`UPDATE`/`DELETE`/`ALTER`/`DROP`/`TRUNCATE`/`CREATE`/`GRANT`/`REVOKE`/`VACUUM`/`ANALYZE`/`REINDEX`/`CLUSTER`/`CALL`/`DO`/`COPY`；禁止启动后端；禁止修改 `schema_migrations`。
- 日志不记录密码、pgpass 内容、完整 DSN、`PGPASSWORD`、业务数据、邮箱、查询码、token、session、恢复码、订单或付款内容。
- 本阶段不提交、不推送。

## 阶段 1：只读 Git 基线检查

- 开始时间：2026-07-15 17:55（本机时间）
- 分支：`main`
- `git status --short`：空（工作区、暂存区干净）
- `git rev-parse HEAD` = `git rev-parse origin/main` = `55cddd956de3f22907d950ed04658510bb12521d` —— 与交接基线一致
- `git log -5 --oneline`：`55cddd9` / `84fd194` / `ce6cb57` / `58e7a54` / `c45e85c`
- `git ls-files --others --exclude-standard`：空（无未跟踪文件）
- 结论：基线一致，允许继续。

## 阶段 2：任务书迁移清单与仓库实际文件的差异（**连接数据库前发现，必须先纠正**）

任务书第七、八节内联的"仓库当前应包含"清单与 `backend/migrations/` 的**实际文件名不符：19 个中有 9 个不同**。任务书自身指定的核对依据 `docs/migration-numbering-deployment-check.md`（第 71–89 行）所列清单则与仓库**完全一致**。

| 任务书内联清单 | 仓库实际文件 |
| --- | --- |
| `0001_initial.sql` | `0001_core_tables.sql` |
| `0002_import_batches.sql` | `0002_import_tracking.sql` |
| `0003_import_preview.sql` | `0003_admin_auth.sql` |
| `0009_payments.sql` | `0009_admin_payments.sql` |
| `0010_payment_void_audit.sql` | `0010_payment_voids.sql` |
| `0011_payment_fee_amounts.sql` | `0011_payment_fee_fields.sql` |
| `0013_merge_cn_users.sql` | `0013_cn_merge.sql` |
| `0016_recovery_email.sql` | `0016_user_recovery_email.sql` |
| `0017_recovery_email_verification.sql` | `0017_recovery_email_verification_codes.sql` |

其余 10 个一致：`0004_import_confirm.sql`、`0005_import_history.sql`、`0005_product_series.sql`、`0007_import_revert.sql`、`0008_query_sessions.sql`、`0012_normalize_payment_methods.sql`、`0014_user_query_account_admin.sql`、`0015_query_code_bind_tokens.sql`、`0018_query_code_email_recovery.sql`、`0019_admin_auth_audit_events.sql`。

**若按任务书内联清单原样执行 CTE 对比，后果**：`missing_from_database` 会列出 9 个仓库中并不存在的名字；`unknown_in_database` 会把 9 个**真实**迁移误判为"未知迁移"，从而触发"出现未知迁移时立即停止"——得出**错误的分支 E**，即错误地宣告正式库损坏。

**处置**：以**仓库实际文件**为唯一事实来源（与核对依据文档一致），且 CTE 由脚本从 `backend/migrations/*.sql` **程序化生成**，不手工转录，彻底消除转录错误。核对原则"必须比较完整文件名、不得只比较数量"照常严格执行。

- 是否连接数据库：否（本阶段仅本地文件比对）。是否读取敏感信息：否。是否使用子代理：否。修改文件：仅新建本日志。

## 阶段 3：连接与只读保护

- 开始时间：2026-07-15 18:05（本机时间）
- 客户端：`D:\PostgreSQL\18\bin\psql.exe`，使用 `-w`（永不提示密码）。
- 凭据：本机 `%APPDATA%\postgresql\pgpass.conf` **仅确认存在，未读取、未输出、未复制内容**；未设置 `PGPASSWORD`（连接前核查为 unset）；未使用 `backend/.env` 或 `DATABASE_URL`（连接前核查 `DATABASE_URL` 为 unset）；未创建任何密码文件。
- 只读保护（连接后第一组命令）：

| 设置 | 实测值 |
| --- | --- |
| `default_transaction_read_only` | **on** |
| `statement_timeout` | 30s |
| `lock_timeout` | 5s |

- 全部核对 SQL 均置于 `BEGIN TRANSACTION READ ONLY;` … `ROLLBACK;` 显式只读事务中。

### 步骤 0：连接目标确认

| 项 | 值 |
| --- | --- |
| `current_database` | **pjsk** ✅ |
| `current_user` | postgres |
| `server_address` | 127.0.0.1 |
| `server_port` | 5432 |
| `postgres_version` | PostgreSQL 18.4 on x86_64-windows |
| `transaction_read_only` | **on** ✅ |

未查询任何业务表来确认连接。

## 阶段 4：步骤 1–6 核对结果

### 步骤 1：`schema_migrations` 存在

`schema_migrations_exists = t` ✅

### 步骤 2：完整迁移历史

**18 行**；`first_version = 0001_core_tables.sql`；`last_version = 0018_query_code_email_recovery.sql`。按 `version` 排序时 `applied_at` 单调递增（2026-07-10 17:45 → 2026-07-14 16:02），历史连贯，无改写迹象。

两个 `0005` 的 `applied_at`（`0005_import_history.sql` = 07-11 08:07:16、`0005_product_series.sql` = 07-11 17:40:30）与 `docs/development-logs/2026-07-12-今日更新汇总.md` 记载的"07-11 08:07 / 17:40"**完全吻合**，互为佐证。

### 步骤 3：双向集合对比（以仓库实际文件为准）

- `unknown_in_database`：**0 行** ✅ —— 无任何仓库中不存在的迁移记录。
  （若按任务书内联清单执行，此处会出现 9 行"未知迁移"的**误报**，见阶段 2。）
- `missing_from_database`：**1 行** —— 仅 `0019_admin_auth_audit_events.sql`。
- 无中间洞、无重复 version、无改名、记录均为完整文件名（非数字编号）、无 `0006_*`、迁移数量（18）未超过仓库文件数（19）。

### 步骤 4：双 `0005`

**两行均存在** ✅

| version | applied_at |
| --- | --- |
| `0005_import_history.sql` | 2026-07-11 08:07:16.555772+08 |
| `0005_product_series.sql` | 2026-07-11 17:40:30.340639+08 |

不属于分支 D。

### 步骤 6：重复记录与唯一约束

- 重复 version：**0 行** ✅
- 约束：`schema_migrations_pkey` = **PRIMARY KEY on `version`** ✅（唯一性受保护），另有两个 NOT NULL CHECK。

### 步骤 5：记录与实际 schema 互证 —— **发现严重不一致**

| 迁移 | 记录在 `schema_migrations`？ | 结构存在？ | 判定 |
| --- | --- | --- | --- |
| `0005_import_history.sql` | 是 | `import_batches.warnings_accepted` = **t** | 一致 ✅ |
| `0005_product_series.sql` | 是 | `products.series_code` = **t** | 一致 ✅ |
| `0019_admin_auth_audit_events.sql` | **否** | `admin_auth_audit_events` = **t** | **严重异常** ❌ |

按任务书判断规则："**迁移记录不存在但结构存在：严重异常，可能被手工执行过，停止。**"

#### 异常详情（进一步只读取证）

`admin_auth_audit_events` **表存在**，且其表级结构与 `0019` 定义**逐项吻合**：

- 10 个列、类型、可空性全部匹配（`id uuid NOT NULL`、`event_type text NOT NULL`、`occurred_at timestamptz NOT NULL`、`admin_id uuid NULL`、`username_normalized text NOT NULL`、`client_ip text NOT NULL`、`result text NOT NULL`、`reason_code text NOT NULL`、`user_agent_summary text NULL`、`created_at timestamptz NOT NULL`）
- 默认值匹配：`id = gen_random_uuid()`、`occurred_at = now()`、`created_at = now()`
- 约束齐全：外键 `admin_auth_audit_events_admin_id_fkey`、5 个 CHECK（event_type / client_ip / reason_code / result / username_normalized / user_agent_summary）、复合 CHECK `admin_auth_audit_reason_result`、主键

**但是 `0019` 定义的 4 个索引全部缺失**——`pg_indexes` 中该表只有 `admin_auth_audit_events_pkey`，以下 4 个**均不存在**：

- `admin_auth_audit_events_occurred_at_index`
- `admin_auth_audit_events_type_time_index`
- `admin_auth_audit_events_admin_time_index`
- `admin_auth_audit_events_username_time_index`

**表内有 208 行**（仅执行 `count(*)` 聚合，**未读取任何审计事件记录内容**；此聚合对判断"表是否已在使用"具有决定性意义，故予以采集并在此透明记录）。208 行说明该表**已被实际写入**，即带审计代码的后端曾对 `pjsk` 运行过。

当前 8080 未监听、无 `pjsk-backend` 进程，即此刻没有后端在运行。

#### 成因分析（**无法从只读目录数据确证，不作断言**）

事实无法用"迁移器正常执行"解释：`RunMigrations` 把每个迁移的 SQL 与 `schema_migrations` 插入放在**同一事务**中，因此正常路径下不可能出现"表建好、索引没建、记录没写"的组合——要么全有，要么全无。故该表**几乎可以确定是绕过迁移器、由人工执行了 `0019` 的部分 SQL（仅建表语句、未含 4 条建索引语句）**而产生。至于 208 行审计数据由哪次后端运行写入，只读目录数据不足以确证。仓库中另存在一个预编译产物 `backend/bin/pjsk-backend.exe`（时间戳 2026-07-15 09:16，Git 忽略、未跟踪），仅作为背景信息记录，不作因果断言。

#### 若此时启动后端会发生什么（**仅为风险评估，本阶段未执行**）

`0019` 使用 `create table if not exists` 与 `create index if not exists`。启动后端将会：建表语句成为**空操作**（静默接受现有表）→ 补建 4 个缺失索引 → 写入 `schema_migrations` 记录并提交。**结果反而会把状态修正为完整。**

但这**不构成"可以直接启动"的理由**，原因有三：

1. `if not exists` 意味着迁移器**无法检测表结构差异**——本次是"恰好"匹配才安全；若现有表与 `0019` 有任何出入，迁移器会静默接受并把 `0019` 标记为已应用，永久掩盖差异。这是该幂等写法的固有盲区。
2. **根因未查明**。有人绕过迁移器手工改动过正式库 schema，这意味着迁移历史已不再是"实际发生了什么"的可靠记录——**无法排除还存在其他未登记的手工改动**。
3. 任务书对该状态的规定是明确的停止条件。

**禁止为消除不一致而手工修改 `schema_migrations`**——本阶段未做，也不建议在根因查明前做。

### 步骤 13：结束事务与连接

每组查询均以 `ROLLBACK;` 显式结束；psql 每次执行后正常退出，无常驻进程。全程未执行任何写 SQL。

## 最终分支判断：**分支 E**

条件命中："记录与结构不符"——`0019` 未记录但 `admin_auth_audit_events` 存在（且其 4 个索引缺失）。

结论：**禁止启动后端，禁止运行迁移，保留只读输出进入专项修复评审。**

说明：除该 `0019` 异常外，其余各项（18 条记录完整连贯、零未知迁移、双 `0005` 齐备且结构一致、零重复、`version` 有主键保护）**均通过**。异常范围目前已知仅限 `0019` 相关对象。

## 是否连接/读取/修改

- 是否执行任何写 SQL：**否**（全部为 `SELECT` / `SHOW` / `information_schema` / `pg_catalog` 查询，且在 READ ONLY 事务内）
- 是否运行迁移：**否**
- 是否读取业务数据：**否**（未读取 users/orders/payments/邮箱/查询码/token/session/恢复码等任何业务内容；对审计表仅取 `count(*)` 聚合，未读任何记录内容）
- 是否读取密码或 pgpass 内容：**否**（仅确认文件存在）
- 是否修改正式库：**否**
- 是否使用子代理：否
- 修改文件：仅本日志

## 未完成事项

1. **`0019` 记录/结构不一致的根因调查与受控修复**（新增，最高优先级）
2. 真实备份恢复演练
3. 正式部署

## 阶段 5：`0019` 部分落地根因调查

- 开始时间：2026-07-15 17:35（本机时间）
- Git 基线未变：`55cddd9`。

### 5.1 Git 时间线（事实）

| 事件 | 时间 | 来源 |
| --- | --- | --- |
| `0019_admin_auth_audit_events.sql` 首次进入 Git | **2026-07-15 12:54:25 +0800**（提交 `4edb307`，`--diff-filter=A` 确认） | `git log` |
| 审计写入代码首次进入 Git | **同一提交 `4edb307`，12:54:25** | `git log -S "admin_auth_audit_events"` |
| 正式库 `0018` 应用时间 | 2026-07-14 16:02:38 | `schema_migrations` |
| `backend/bin/pjsk-backend.exe` 修改时间 | 2026-07-15 09:16:55（17,105,920 字节，SHA-256 `82456C37…45715`，无 PE 版本资源） | 文件元数据 |

**无法确认**：该二进制对应哪个提交。仓库中没有已跟踪的构建 manifest/发布记录可供 SHA 关联，Go 二进制也无 PE 版本资源。按任务要求**不作断言**，且**未运行**该二进制。

### 5.2 审计表聚合时间范围（事实，仅聚合，未读任何记录内容）

| 指标 | 值 |
| --- | --- |
| `row_count` | 208 |
| `first_created_at` / `first_occurred_at` | **2026-07-15 12:31:03** |
| `last_created_at` / `last_occurred_at` | **2026-07-15 16:59:59** |

**关键事实**：最早写入（12:31:03）**早于 `0019` 进入 Git（12:54:25）约 23 分钟**。当前（17:31）`pg_stat_activity` 中 `pjsk` 只有本次核对用的 psql 连接，无后端连接；8080 未监听。

### 5.3 创建路径调查 —— **根因已确认**

`git grep` 全仓库检索 `create table.*admin_auth_audit_events`，命中**两处**，而非仅迁移：

1. `backend/migrations/0019_admin_auth_audit_events.sql:6`（迁移，含 4 条 `create index`）
2. **`backend/internal/api/export_routes_test.go:91`（测试夹具，不含任何 `create index`）** ←

`export_routes_test.go` 的 `newAPITestPool`（第 71–124 行）行为：

```
73-74:  godotenv.Load("../.env") / godotenv.Load("../../.env")   ← 加载真实 .env
75-78:  databaseURL := os.Getenv("DATABASE_URL")；为空才 Skip
79:     pgxpool.New(ctx, databaseURL)                            ← 连接该 DSN 指向的库
83-89:  alter table users add column if not exists ...           ← DDL 写操作
90-121: create table if not exists admin_auth_audit_events (...) ← 建表，无索引、无迁移记录
134-140:insert into admins (...)                                 ← 插入测试管理员
142-148:POST /api/admin/login                                    ← 触发审计写入路径
```

该夹具第 91–118 行的建表 DDL 与 `0019` 的建表部分**逐字一致**（同样的 10 列、同样的 CHECK、同样的约束名 `admin_auth_audit_reason_result`），但**完全没有 `0019` 的 4 条 `create index`，也从不写 `schema_migrations`**。

**这精确解释了正式库观测到的全部现象**：

| 观测事实 | 由该夹具解释 |
| --- | --- |
| 表存在且表级结构与 `0019` 逐项吻合 | 夹具 DDL 是 `0019` 建表部分的逐字副本 |
| 4 个索引全部缺失 | 夹具 DDL 不含任何 `create index` |
| `schema_migrations` 无 `0019` 记录 | 夹具从不写迁移记录 |
| 208 行审计数据 | 夹具 `insert admins` + 走 `/api/admin/login` 触发审计写入 |
| 最早写入 12:31 < `0019` 提交 12:54 | 开发审计功能期间运行测试，早于提交 |
| `cleanupAPITestAdmin` 只按前缀删除（`username_normalized like lower(prefix%)`），非前缀行残留 | 208 行得以留存 |

**推断（非断言）**：`RunMigrations` 把迁移 SQL 与 `schema_migrations` 插入放在**同一事务**（`applyMigration`），因此正常迁移路径**不可能**产生"表在、索引不在、记录不在"的组合——要么全有要么全无。故该表**确定不是由迁移器创建的**；证据强烈指向上述测试夹具。

### 5.4 **系统性重大发现：测试套件会对 `DATABASE_URL` 所指库执行 DDL 与业务数据写入**

这不是 `0019` 的孤立问题。检索显示 **8 个测试文件**加载真实 `.env` 并连接 `DATABASE_URL`：

`api/export_routes_test.go`、`export/handler_test.go`、`payments/void_integration_test.go`、`query/payment_items_integration_test.go`、`querycoderecovery/store_integration_test.go`、`recoveryemailverification/store_integration_test.go`、`users/query_account_integration_test.go`（+ `config/config.go` 生产加载）。

其中多个执行 **DDL**：

- `api/export_routes_test.go:84` `alter table users add column ...`；`:91` `create table admin_auth_audit_events`
- `users/query_account_integration_test.go:224` `alter table users`
- **`payments/void_integration_test.go:490-525` `applyVoidMigration`**——最严重：
  - `alter table payments add column if not exists voided_at / voided_by_admin_id / void_reason`
  - **`alter table payments drop constraint if exists payments_status_check;`** 随后 `add constraint payments_status_check check (...)` ← **删除并重建正式 `payments` 表的约束**
  - `alter table payments add column if not exists fee_amount / payable_amount`
  - **`update payments set fee_amount = 0, payable_amount = submitted_amount where fee_amount is null or payable_amount is null;`** ← **对正式业务表的 UPDATE，且不限定测试前缀**
  - `alter column ... set not null`；`drop constraint payments_fee_amount_check / payments_payable_amount_check` 后重建

`backend/.env` **存在**（Git 忽略；**本轮未读取其内容**）。若其 `DATABASE_URL` 指向正式库 `pjsk`，则**执行 `go test ./...` 就会对正式库做 schema DDL 与业务数据 UPDATE**。观测证据（审计表在 `pjsk` 中被建出且持续写入）与该机制完全一致。

### 5.5 **本 AI 自身很可能已对正式库造成写入（主动披露）**

必须诚实披露：**本次会话中我多次执行过 `go test ./...` 与 `go test -count=1 ./...`**。我当时核查的是**我 shell 中的** `DATABASE_URL` 为 unset，但**这不足以阻止连接**——测试在进程内部通过 `godotenv.Load("../.env")` **自行加载 `backend/.env`** 并据此设置 `DATABASE_URL`。

时间线高度吻合：审计表 `last_created_at = 16:59:59`，而我在 **17:04:37 提交 `55cddd9`** 之前的最终验证步骤中恰好执行了非缓存的 `go test -count=1 ./...`（输出显示 `ok pjsk/backend/internal/api 3.481s`，非 cached，即该包测试**实际运行**）。

**因此：16:59:59 的审计写入极可能由我执行的测试产生；更早的写入（12:31 起）则早于本次会话，来自他人运行测试。**

这属于**事实陈述与推断的分界**：
- **事实**：我运行了 `go test ./...`；测试代码会自行加载 `backend/.env`；审计表最后写入时间与我的运行时刻吻合。
- **推断（未确证）**：这些行由我的运行写入。要确证需读取 `backend/.env`（禁止）或审计行内容（禁止）。

**立即措施**：本阶段起**停止执行 `go test ./...`**，直到 `.env` 指向确认为止。本阶段后续所有验证改用**只读查询**与**隔离测试库**。

### 5.6 是否访问业务数据 / 是否写操作

- 本阶段对正式库：仅 `SELECT`/`SHOW`/`information_schema`/`pg_catalog`/审计表聚合，全部在 READ ONLY 事务内，**未执行任何写操作**。
- 未读取审计事件内容、`username_normalized`、`client_ip`、`user_agent_summary`、`admin_id`、逐行 `reason_code`。
- 未读取 `backend/.env`。

### 5.7 停止条件

`0019` 之外已发现**系统性写入路径**（测试套件对 `DATABASE_URL` 做 DDL + 业务 UPDATE）。按任务书第十节："发现任何 `0019` 之外的漂移，最终结论必须保持分支 E，并升级为'全库 schema 漂移'，不得进入局部修复。"

需先完成阶段 6 全库漂移比对，量化 `payments`/`users` 等表的实际差异。

## 阶段 6：正式库全量 schema 漂移核对

- 开始时间：2026-07-15 17:40（本机时间）
- 方法：两个独立事实来源比对。
  - **来源 A**：正式库 `pjsk`，只读 catalog（READ ONLY 事务）。
  - **来源 B**：隔离期望库 `pjsk_migration_test_20260715_drift_811903`（严格前缀，建库前已校验名称与不存在性），**仅从仓库 19 个迁移文件按字节序生成**，0 失败。**未从正式库复制任何数据或 schema**。比对结束后已删除（见清理小节）。

### 6.1 PowerShell 空结果误判修正（承接阶段 5 遗留）

前一轮 `ENUM/DOMAIN` 比对报 "MATCH"，实为**假结果**：`t.typtype` 是 `"char"` 类型，`||` 拼接歧义导致 SQL 报错；随后改用 `$null` 判断的辅助函数又把"成功但零行"误判为"查询失败"（PowerShell 会把空数组 `@()` 折叠成 `$null`）。

本轮改用返回 `[pscustomobject]@{ Ok; Rows; Count }` 的辅助函数，**显式区分**四种情况：查询失败 / 成功且零行 / `$null` / 空数组；两边任一失败即报 `QUERY FAILED - NOT a match`，绝不当作 MATCH。零行且两边查询均成功才判 `MATCHING (both query-ok, zero rows)`。

### 6.2 全量比对结果

| 对象类别 | production | expected | 结论 |
| --- | --- | --- | --- |
| tables | 21 | 21 | matching |
| columns（名/类型/nullable/default/顺序） | 232 | 232 | matching |
| constraints（全量 `pg_get_constraintdef`） | 275 | 275 | matching |
| CHECK 表达式 | 47 | 47 | matching |
| UNIQUE 约束 | 10 | 10 | matching |
| FK 动作（confdeltype/confupdtype） | 39 | 39 | matching |
| 约束 validated/deferrable | 275 | 275 | matching |
| **indexes（完整 `indexdef`）** | **84** | **88** | **definition drift：缺 4** |
| sequences | 0 | 0 | matching（两边查询成功且零行） |
| extensions | 2 | 2 | matching |
| ENUM 类型 | 0 | 0 | matching（两边查询成功且零行） |
| DOMAIN 类型 | 0 | 0 | matching（两边查询成功且零行） |
| functions | 37 | 37 | matching |
| triggers | 0 | 0 | matching（两边查询成功且零行） |
| views | 0 | 0 | matching |
| identity / generated 列 | 0 | 0 | matching（两边查询成功且零行） |

### 6.3 结构化差异摘要

```
missing_in_production (4):
  admin_auth_audit_events_occurred_at_index  ON admin_auth_audit_events USING btree (occurred_at DESC)
  admin_auth_audit_events_type_time_index    ON admin_auth_audit_events USING btree (event_type, occurred_at DESC)
  admin_auth_audit_events_admin_time_index   ON admin_auth_audit_events USING btree (admin_id, occurred_at DESC)
  admin_auth_audit_events_username_time_index ON admin_auth_audit_events USING btree (username_normalized, occurred_at DESC)

extra_in_production (0):    （无）
definition_mismatch (0):    （无）
matching:                   其余全部对象类别
```

正式库全部索引 `indisvalid / indisready / indislive` 均为真。

**结论（事实）**：正式库与"19 个迁移应形成的 schema"之间，**唯一差异就是 `0019` 的 4 个索引缺失**。没有多余对象，没有定义不一致。`0019` 之外**未发现任何 schema 漂移**。

**重要推论**：`void_integration_test.go` 对 `payments` 的 `drop constraint` + `add constraint` **未造成漂移**——CHECK 表达式 47/47 完全一致，因为测试里的定义是迁移定义的逐字副本。

## 阶段 7：测试误连正式库的全面调查与影响评估

### 7.1 会连接真实 `DATABASE_URL` 的测试文件（完整清单）

共 **8 个**测试文件加载真实 `.env`（`godotenv.Load("../.env")` / `"../../.env"`）并读取 `DATABASE_URL`，仅当其为空才 `t.Skip`：

| # | 文件 | 执行的 DDL | 执行的 DML | 分类 |
| --- | --- | --- | --- | --- |
| 1 | `internal/api/export_routes_test.go` | `alter table users add column if not exists`（:84）；**`create table if not exists admin_auth_audit_events`（:91，无索引、无迁移记录）** | `insert admins`；登录触发审计写入；`delete` 均按前缀 | **5（执行 DDL）** + 2 |
| 2 | `internal/payments/void_integration_test.go` | `alter table payments add column`×3（:493,:505,:514）；**`drop/add constraint payments_status_check`（:497-499）**；`drop/add payments_fee_amount_check`、`payments_payable_amount_check`（:517-524）；`alter column set not null`（:514-516） | **`update payments set fee_amount=0, payable_amount=submitted_amount where fee_amount is null or payable_amount is null`（:508-513，无测试范围限制）**；其余 `delete` 均按前缀 | **5（执行 DDL）+ 6（无范围 UPDATE）** |
| 3 | `internal/users/query_account_integration_test.go` | `alter table users`（:224） | `delete` 均按前缀 | 5 + 2 |
| 4 | `internal/export/handler_test.go` | 无 | `delete` 均按前缀（:371-377） | 2 |
| 5 | `internal/query/payment_items_integration_test.go` | 无 | `delete` 均按前缀（:173-179） | 2 |
| 6 | `internal/querycoderecovery/store_integration_test.go` | 无 | `delete` 均按前缀（:353-361） | 2 |
| 7 | `internal/recoveryemailverification/store_integration_test.go` | 无 | `delete` 均按前缀（:360-366） | 2 |
| 8 | `internal/query/query_code_recovery_integration_test.go`、`recovery_email_integration_test.go`、`users/bind_token_integration_test.go`、`users/recovery_email_integration_test.go` 等 | 无 | `delete` 均按前缀 | 2 |

**分类 1（只读）**：无。**分类 3（有事务回滚隔离）**：**无**——所有集成测试直接写库，**没有任何一个用事务回滚隔离**。**分类 7（可能修改约束/迁移状态）**：#1（建表不登记）、#2（替换约束）。

**关键澄清（修正阶段 5 的过度告警）**：`void_integration_test.go` 的 `applyVoidMigration`（:490-525）是**迁移 `0010` + `0011` 逻辑的逐字副本**。特别是那条"无范围 UPDATE"——`0011_payment_fee_fields.sql:9-14` 本身就包含**完全相同**的 `update payments set fee_amount=0, payable_amount=submitted_amount where fee_amount is null or payable_amount is null;`。因此该 UPDATE **不可能产生与迁移不同的状态**：它就是迁移自身为存量行设计的回填行为。阶段 5 称其为"对正式业务表的 UPDATE"字面属实，但**效果被高估**，特此更正。同理，其 `drop/add constraint` 的定义与迁移一致（已由 47/47 CHECK 比对证实）。

`admin_login_ratelimit_routes_test.go` 经检索**不含** `godotenv`/`DATABASE_URL`/`pgxpool`，为纯内存测试，**不可能**写入审计表。

### 7.2 正式库数据影响只读评估（仅聚合与结构，未读业务明细）

| 检查项 | 结果 | 判断 |
| --- | --- | --- |
| `payments.fee_amount` / `payable_amount` 可空性 | 均为 **NOT NULL** | 无范围 UPDATE 的 `where ... is null` **当前匹配 0 行** |
| 无范围 UPDATE 现在会命中的行数 | **0** | 当前为空操作 |
| 业务表规模 | payments=7、users=45、orders=44、admins=2、projects=1 | 数据量很小 |
| 测试前缀残留（admins / users / projects） | **全部 0** | 测试的前缀化 `delete` 清理有效，**无测试数据残留** |
| 孤立数据（orders/payments 无 user、payment_items 无 payment） | **全部 0** | 引用完整性完好 |
| 已撤销付款 | 3（共 7 笔） | 归属非测试前缀用户；**无法从现有证据确定**是否与测试有关，倾向为正常业务撤销 |

### 7.3 审计表 208 行的归属（重大发现，推翻先前推断）

| 聚合 | 结果 |
| --- | --- |
| 事件类型分布 | `admin_login_failed`/failure = **176**；`admin_login_rate_limited`/failure = **32**；**成功登录 0 条** |
| 匹配测试前缀（`rl-e2e-` / `API_EXPORT_ROUTE_TEST_` / `PAYMENT_VOID_TEST_` / `TEST_%` 等）的行数 | **全部 0** |
| 不匹配任何测试标识的行数 | **208（全部）** |
| distinct username_normalized / client_ip / admin_id | **2 / 3 / 0**（仅基数，未取任何值） |
| admin_id 为 NULL 的行 | 208（全部） |
| 按小时分布（仅时间戳） | 07-15 12 时=78、13 时=91、15 时=26、16 时=13 |

**事实**：这 208 行**不是测试产生的**——没有一行匹配任何测试前缀，且负责登录限流 e2e 的测试根本不连数据库。它们是**真实的失败登录与限流审计记录**（2 个用户名、3 个客户端 IP、全部失败、集中在 07-15 当天）。

**这修正了我在阶段 5 的推断**：先前我推断"16:59:59 的审计写入极可能由我执行的 `go test` 产生"。现在证据表明**并非如此**——测试写入的行都会带测试前缀并被其自身清理，而现存 208 行均无测试前缀。**我执行的 `go test` 是否曾连接正式库仍无法确证**（需读取 `backend/.env`，本轮禁止），但**现存审计数据并非我的测试产生**。

**推断（非断言）**：2 个用户名 + 3 个 IP + 全部失败 + 全部集中在审计功能开发当日（07-15），与**开发者手工测试登录/限流功能**的特征高度吻合；不符合凭据填充攻击的特征（那会呈现大量不同用户名）。**无法从只读聚合确证**，需人工确认。

### 7.4 `0019` 表来源的最终判断

**事实**：
1. `RunMigrations`/`applyMigration` 把迁移 SQL 与 `schema_migrations` 插入放在**同一事务**，故正常迁移路径**不可能**产生"表在、索引不在、记录不在"。该表**确定不是迁移器创建的**。
2. 仓库中存在**唯一**一个能产生该精确形态的已知机制：`export_routes_test.go:91` 的 `create table if not exists`（`0019` 建表部分的逐字副本，无索引、不写记录）。
3. `0019` 与审计代码同在 `4edb307`（07-15 12:54:25）进入 Git；首条审计行 12:31:03，**早 23 分钟**。

**无法确认**：
- `backend/.env` 的 `DATABASE_URL` 是否指向 `pjsk`（本轮**禁止读取** `.env`）。**这是确证链上唯一缺失的一环**。
- 该表究竟由 `export_routes_test.go` 创建，还是由人手工粘贴 `0019` 的建表部分创建——两者产生**完全相同**的结果，仅凭 schema **无法区分**。
- `backend/bin/pjsk-backend.exe`（09:16:55，SHA-256 `82456C37…45715`）对应哪个提交（无 PE 版本资源、无已跟踪构建 manifest）。**未运行**该二进制。

### 7.5 根因分类

| 分类 | 判定 | 证据强度 |
| --- | --- | --- |
| **R4：测试基础设施误连正式库，导致未登记 schema** | **主判定（针对 4 个缺失索引 + 缺失迁移记录）** | **强，但未完全确证**——机制在仓库中真实存在且能精确复现该形态；缺 `.env` 的 `DATABASE_URL` 指向这一环 |
| R3：手工执行 `0019` 部分 SQL，来源不明 | **不能排除** | 与 R4 产生相同结果，无法从 schema 区分 |
| R5：证据不足 | **适用于 208 行审计数据的来源** | 已确证**非测试产生**；真实来源无法从只读聚合确定 |
| R1：已知一次性人工建表 | 未采用 | 无开发日志或操作记录支持 |
| R2：旧二进制使用未登记 schema | 未采用 | 无法把二进制与提交建立可信关联，按要求不作断言 |

**关于"是否升级为全库 schema 漂移"**：任务书第十节要求"发现任何 `0019` 之外的漂移即升级"。**全量比对未发现 `0019` 之外的任何 schema 漂移**，故不触发升级。但**测试基础设施缺陷本身**是独立于 schema 漂移的、更高优先级的问题（它是可重复触发的持续风险，而非一次性事故）。

### 7.6 当前分支：仍为 **分支 E**

`0019` 记录与结构不符，且根因未获人工确认。**禁止启动后端、禁止运行迁移、禁止手工修改 `schema_migrations`。新增：禁止运行任何 Go 测试**，直至测试隔离缺陷修复完成。

## 阶段 8：两个独立修复计划（**仅设计，本轮未实施**）

### 修复计划 1：测试数据库隔离（**正式部署前最高优先级**）

**为何优先于 `0019` 修复**：`0019` 是一次性后果；测试缺陷是**持续的、可重复触发的**产生源。不先修它，任何人（包括 CI）执行 `go test ./...` 都可能再次改动正式库。

设计要点（**本轮未改任何测试代码**）：

1. **默认必须跳过**：引入显式门禁（如 `PJSK_RUN_DB_INTEGRATION_TESTS=1`），未设置即 `t.Skip`——**不得**以"`DATABASE_URL` 是否为空"作为门禁。
2. **禁止读取真实 `.env`**：移除全部 8 处 `godotenv.Load("../.env")` / `"../../.env"`。
3. **禁止使用通用 `DATABASE_URL`**：改用专用变量（如 `PJSK_TEST_DATABASE_URL`），与生产配置变量彻底分离。
4. **库名严格前缀 + 生产名硬拒绝**：复用已提交的 `isolated_test_support_test.go` 框架（`^pjsk_..._test_[a-z0-9_]+$` 正则、`pjsk`/`postgres`/`template0`/`template1` 禁用名单、连接后 `select current_database()` 二次确认、建库前存在性检查、按精确名删库、`t.Cleanup` 双保险）。
5. **测试自建自删隔离库**，并由迁移器建立 schema——**不得**再用测试内嵌的 DDL 副本（那正是本次 `0019` 形态的成因）。
6. **禁止无范围 UPDATE/DELETE**。
7. **CI 无测试库时安全 SKIP**。
8. **新增静态安全测试**：断言 `*_test.go` 中不出现 `godotenv.Load(".../.env")`、不出现裸 `os.Getenv("DATABASE_URL")`、不出现针对真实业务表的 `alter table`/`create table`——防止未来再次引入。

### 修复计划 2：正式库 `0019` 状态修复（**前置条件未满足，暂不可执行**）

**前置条件**（须全部满足）：

- [x] 全库除 `0019` 外无 schema 漂移 —— **已由阶段 6 全量比对确认**
- [x] 现有表完整匹配 `0019`（列/类型/默认值/约束/FK 全部逐项一致） —— **已确认**
- [x] 仅缺 4 个索引与迁移记录 —— **已确认**
- [x] 无测试数据污染需优先处理 —— **已确认**（测试前缀残留 0、孤立数据 0）
- [ ] **根因获人工确认** —— **未满足**（需确认 `backend/.env` 的 `DATABASE_URL` 指向，及 208 行审计数据来源）
- [ ] **修复计划 1 已实施** —— **未满足**（否则修好后仍可能被再次破坏）
- [ ] **已完成真实备份恢复演练** —— **未满足**

**方案评估**：

| 方案 | 评估 | 结论 |
| --- | --- | --- |
| **A：让现有迁移器在受控窗口执行 `0019`** | `create table if not exists` 空操作 → 4 条 `create index if not exists` 补齐 → 同事务写入 `schema_migrations`。已由阶段 6 全量比对消除"`IF NOT EXISTS` 静默接受未知差异"的风险（现有表已确认逐项匹配）。索引创建对 **208 行**小表几乎瞬时，`CREATE INDEX`（非 CONCURRENTLY）取 `SHARE` 锁、阻塞写入但耗时以毫秒计，**无需停机**。 | **推荐**（前置条件满足后） |
| B：新增 `0020` 修复迁移 | **否决**。`0019` 未记录，迁移器会**先执行 `0019`**（已补齐索引与记录），`0020` 将变成无意义的重复操作；且会让全新库执行不必要的步骤，并违反"不为规避 `0019` 而新增占位迁移"的原则。 | 否决 |
| C：手工补索引 + 手工插入迁移记录 | **否决**。绕过迁移器、再次手工改 `schema_migrations`、事务边界与审计困难——正是本次事故的同类操作。 | 否决 |
| D：新建一致表并迁移审计数据 | **不适用**。现有表已确认与 `0019` 完全一致，无需重建；且会牺牲 208 行真实审计数据的连续性。 | 不适用 |

**方案 A 执行规格（待前置条件满足后由人工在受控窗口执行）**：
- 执行窗口：低峰期，确认无后端在跑（8080 未监听）。
- 备份要求：**先完成真实备份 + 恢复演练并产出 `.validation.json`**。
- 执行方式：正常启动后端，由 `RunMigrations` 自动完成（**不手工执行 SQL**）。
- 验证 SQL（只读）：`schema_migrations` 出现 `0019_admin_auth_audit_events.sql`；`pg_indexes` 中该表索引数由 1 → 5；`count(*)` 仍为 208（审计数据未丢）；再次运行阶段 6 全量比对应为零差异。
- 停止条件：启动日志出现 `database migration failed`；或迁移后比对仍有差异；或审计行数变化。
- 回滚：迁移在单事务内，失败自动回滚；若已提交但需回退，用演练验证过的备份恢复。
- 预计影响：新增 4 个索引，208 行表，毫秒级，无停机。

## 是否连接/读取/修改（阶段 6–7）

- 正式库：**仅只读**（`SELECT`/`SHOW`/`information_schema`/`pg_catalog`/聚合），全部在 `default_transaction_read_only = on` + `BEGIN TRANSACTION READ ONLY` 内，每组以 `ROLLBACK` 结束。**未执行任何写操作**。
- 隔离期望库：**已创建并已删除**（仅从仓库迁移生成，未复制正式数据）。
- 未读取业务明细：未取任何 users/orders/payments 行内容、邮箱、查询码、token、session、恢复码；审计表仅取 `count`/`min`/`max`/枚举分布/基数与固定测试标识计数，**未输出任何 username、IP、User-Agent、admin_id 值**。
- 未读取 `backend/.env`；未运行 `backend/bin/pjsk-backend.exe`；**未运行任何 Go 测试**；未启动后端；未运行迁移；未修改测试代码。

## 清理与残留

- 隔离期望库 `pjsk_migration_test_20260715_drift_811903` 已按精确名删除；`SELECT datname FROM pg_database WHERE datname LIKE 'pjsk_%test%'` **零行**。
- 正式库 `pjsk` 仍存在且未被修改。
- 8080 未监听；无 `pjsk-backend`/`go`/`psql` 遗留进程；`PJSK_RUN_ISOLATED_MIGRATION_TESTS`/`DATABASE_URL`/`PGPASSWORD` 在 User/Machine/Process 三级均 unset；无临时 SQL/dump/metadata/validation 文件。

## 当前禁止事项（累计）

1. 禁止启动后端；2. 禁止运行迁移；3. 禁止手工修改 `schema_migrations`；4. **禁止运行任何 Go 测试**（直至修复计划 1 完成）；5. 禁止对正式库写入；6. 禁止在根因确认前执行 `0019` 修复。

## 阶段 9：`DATABASE_URL` 指向确认与 208 行审计来源的最终定性

- 开始时间：2026-07-15 17:55（本机时间）

### 9.1 用户确认（人工事实）

用户确认：**`backend/.env` 中的 `DATABASE_URL` 指向本机正式数据库 `pjsk`**。用户**未**提供密码、完整 DSN 或 `.env` 内容，仅确认了安全解析后的数据库名与本机连接信息。本 AI **始终未读取** `.env`、pgpass 或任何密码。

**R4 由此确认**：现有数据库集成测试会自行 `godotenv.Load("../.env")` 并连接 `DATABASE_URL` → 即正式库 `pjsk`。测试基础设施存在**真实、可重复触发**的正式库写入风险。

### 9.2 用户提供的时间事实，推翻了我先前的推断

用户确认：**2026-07-15 当天用户本人没有进行任何管理员错误登录或限流的人工验收**；用户的人工验收发生在前两天；**07-15 中午到下午的开发、测试与验收主要由 AI 代理执行**。

**据此，我在阶段 7.3 的推断"208 行与开发者手工测试登录/限流特征高度吻合"被推翻，正式作废。**

### 9.3 208 行审计的最终定性：**已确证为测试产生**（强于"高度可能"）

在用户更正后继续取证，发现我在阶段 7.1 的另一处判断也是**错误**的：

> **更正**：阶段 7.1 称 `admin_login_ratelimit_routes_test.go` "不含 `godotenv`/`DATABASE_URL`/`pgxpool`，为纯内存测试，**不可能**写入审计表"——**该结论错误**。我当时按**单文件** grep，忽略了 Go **同包测试文件共享 helper**：该文件第 30、63、115 行调用 `newAPITestPool(t)`，而该 helper 定义在**同包**的 `export_routes_test.go:71`，正是它加载 `.env` 并连接 `DATABASE_URL`。因此该测试**确实直接写入正式库**。

关键代码事实：

| 测试函数 | 使用的用户名 | 每次运行写入的审计行 | 是否清理 |
| --- | --- | --- | --- |
| `TestRouterAdminLoginRateLimitIgnoresSpoofedXFF`（:29） | **硬编码 `router-rl-ghost-a`**（:36） | 5×`admin_login_failed` + 1×`admin_login_rate_limited` = **6** | **无任何 cleanup** |
| `TestRouterAdminLoginRateLimitHonorsTrustedProxy`（:114） | **硬编码 `router-rl-ghost-b`**（:122） | 6×`admin_login_failed` + 1×`admin_login_rate_limited` = **7** | **无任何 cleanup** |
| `TestRouterAdminLoginRateLimitEndToEnd`（:62） | 前缀 `rl-e2e-…` | 含成功登录 | 有（:69 `cleanupAPITestAdmin`） |

只有 EndToEnd 注册了清理，这解释了为何**成功登录 0 条**而失败行不断累积。

数据库实测（仅按**固定测试标识**计数，未输出任何用户名/IP/UA 值；任务书第四节明确授权"固定测试标识的数量"聚合）：

| 固定测试标识 | 行数 | 明细 |
| --- | --- | --- |
| `router-rl-ghost-a` | **96** | failed 80 + rate_limited 16 |
| `router-rl-ghost-b` | **112** | failed 96 + rate_limited 16 |
| **既非 ghost-a 也非 ghost-b** | **0** | — |
| 合计 | **208** | — |

**算术完全闭合**：
- ghost-a：80 ÷ 5 = **16 次**；16 ÷ 1 = **16 次**
- ghost-b：96 ÷ 6 = **16 次**；16 ÷ 1 = **16 次**

**结论（事实，非推断）**：208 行**全部**由 `admin_login_ratelimit_routes_test.go` 的两个测试对正式库 `pjsk` 运行产生，各恰好 **16 次**。这两个用户名是**仅存在于该测试文件中的硬编码字面量**，不可能由人工登录产生。

**因此，我在阶段 7.3 的另一处结论——"这 208 行是真实的失败登录与限流审计记录，不是测试产生的"——同样错误，正式作废。** 当时误判的原因：我只按自己猜测的若干测试前缀（`rl-e2e-`、`API_EXPORT_ROUTE_TEST_` 等）过滤，而这两个 ghost 用户名不带任何此类前缀，故被计入"不匹配任何测试标识"。

**仍无法确定（诚实边界）**：**是谁执行了那 32 次测试运行**。可能来自今日更早的 Codex AI 会话、本 AI 会话（我在本会话中确实多次执行过 `go test ./...`），或两者兼有。要逐次归因需要完整的命令时间记录，现有证据不足。按用户指示：**不删除、不改动这些记录**。

**修正后的正式表述**：
> 208 条审计记录**已确证**由 `admin_login_ratelimit_routes_test.go` 的限流测试对正式库运行产生（依据：仅存在于该测试的硬编码用户名 `router-rl-ghost-a`/`router-rl-ghost-b`，且行数与每次运行写入数精确整除为各 16 次）；**并非用户本人当天人工验收**；但**无法逐次确认由哪个 AI 代理会话执行**。

### 9.4 本轮我已作出的错误判断汇总（透明记录）

| # | 我先前的判断 | 状态 | 更正 |
| --- | --- | --- | --- |
| 1 | "`void_integration_test.go` 的无范围 `UPDATE payments` 是对正式业务表的危险写入" | **效果被高估** | 该语句是迁移 `0011:9-14` 的逐字副本，产生的状态与迁移自身回填完全相同 |
| 2 | "16:59:59 的审计写入极可能由我的 `go test` 产生" | **表述过强** | 方向正确（确为测试产生），但当时无依据；真实归因至今无法逐次确定 |
| 3 | "208 行不是测试产生的，是真实失败登录" | **错误** | 已确证全部由限流测试的 ghost 账号产生 |
| 4 | "`admin_login_ratelimit_routes_test.go` 为纯内存测试，不可能写审计表" | **错误** | 它通过同包 helper `newAPITestPool` 直连正式库 |
| 5 | "208 行与开发者手工测试高度吻合" | **已被用户时间事实推翻** | 用户当天未做人工登录验收 |

（第 3、4 条错误同源：按单文件 grep 判断"是否连库"，忽略了 Go 同包测试共享 helper。）

### 9.5 R4 对 `0019` 形态的解释力（不变）

- 测试基础设施加载真实 `.env` → 连接 `DATABASE_URL` → 正式库 `pjsk`：**已确认**。
- `export_routes_test.go:91` 的 `create table if not exists admin_auth_audit_events`（`0019` 建表部分逐字副本、**无 4 条 create index**、**不写 `schema_migrations`**）能**精确产生**观测到的"表存在 / 4 索引缺失 / 无 `0019` 记录"形态：**已确认**。
- 因此**测试与正式库隔离仍是正式部署前的最高优先级修复**。
- 全量 schema 比对结论不变：`0019` 之外**无任何漂移**。

- 是否连接数据库：是（正式库仅只读，READ ONLY 事务 + ROLLBACK）。是否写操作：否。是否读业务明细：否。是否读 `.env`/pgpass：否。是否运行 Go 测试：**否**。是否使用子代理：否。

## 阶段 10：数据库集成测试隔离修复（**已实施**）

- 开始时间：2026-07-15 18:10（本机时间）

### 10.1 不安全测试入口的完整清单（以当前仓库实际检索为准）

以 `pgxpool.New` 为准确入口，共 **7 个包级 helper**（不含已安全的 `internal/database/isolated_test_support_test.go`）。**每个 helper 由同包多个测试文件共享**——这正是我阶段 7.1 按单文件 grep 而误判的原因：

| # | 包 | 不安全 helper（行号为修改前） | 该包内共享它的测试文件 |
| --- | --- | --- | --- |
| 1 | `internal/api` | `newAPITestPool`（export_routes_test.go:71） | export_routes_test.go、**admin_login_ratelimit_routes_test.go**（:30/:63/:115）、admin_payment_routes_test.go 等 |
| 2 | `internal/export` | `newExportTestPool`（handler_test.go:350） | handler_test.go |
| 3 | `internal/payments` | `newPaymentDBFixture`（void_integration_test.go:460） | void_integration_test.go |
| 4 | `internal/query` | `newQueryTestPool`（payment_items_integration_test.go:153） | payment_items、query_code_recovery、recovery_email、recovery_email_verification |
| 5 | `internal/querycoderecovery` | `newRecoveryFixture`（store_integration_test.go:219） | store_integration_test.go |
| 6 | `internal/recoveryemailverification` | `newVerificationFixture`（store_integration_test.go:236） | store_integration_test.go |
| 7 | `internal/users` | `newUsersTestPool`（query_account_integration_test.go:211） | query_account、bind_token、recovery_email |

7 个 helper 即 7 个收口点，修好它们即覆盖全部消费者。

### 10.2 新增共享隔离包 `backend/internal/testdb`

`testdb.New(t, label)` 一个入口，强制全部规则：

| 规则 | 实现 |
| --- | --- |
| 默认必须跳过 | `SkipUnlessEnabled`：`PJSK_RUN_DB_INTEGRATION_TESTS != "1"` 即 `t.Skip`（**不再**以"`DATABASE_URL` 是否为空"当门禁） |
| 禁止读真实 `.env` | 包内无 `godotenv`，静态测试强制 |
| 禁止用通用 `DATABASE_URL` | 只读 `PJSK_TEST_DATABASE_ADMIN_DSN`，静态测试强制 |
| DSN 禁止含密码 | `assertNoPassword`（URL userinfo + `password=`），凭据由 pgpass 解析 |
| 只允许本机 | Host 限 `127.0.0.1`/`localhost`/`::1` |
| 维护连接必须是 `postgres` | `AdminDSN` 强制 `config.Database == "postgres"`，否则 Fatal |
| 库名严格前缀 | `^pjsk_integration_test_[a-z0-9_]+$`，label 经消毒不被信任 |
| 硬拒绝生产/系统库 | `pjsk`/`postgres`/`template0`/`template1` |
| 建库/连接/删库前各校验一次 | `AssertSafeName` 在三处分别调用 |
| 运行时二次确认 | 维护连接与测试池均 `select current_database()`，落在 `pjsk` 立即 Fatal |
| schema 由真实迁移器建立 | `database.RunMigrations` + `runtime.Caller` 定位 `backend/migrations`（不受各包 CWD 影响），mirror 生产的 `dir="migrations"` |
| 自建自删 + 双保险 | 建库前存在性检查；`t.Cleanup` 注册删除 |
| 清理失败报库名 | `CLEANUP FAILED: ... drop it by hand` |
| 禁止通配删除 | 仅按精确名 `drop database`，先 `pg_terminate_backend ... where datname = $1` 仅终止该库连接 |
| 不进入正式二进制 | 仅被 `_test.go` 导入；`go list -deps .` 确认不可达；静态测试 `TestNoProductionCodeImportsTestdb` 强制 |

### 10.3 删除的测试内嵌 DDL/DML

| 文件 | 删除内容 |
| --- | --- |
| `internal/api/export_routes_test.go` | `alter table users add column if not exists query_code_updated_at, last_query_login_at`；**`create table if not exists admin_auth_audit_events (...)`（`0019` 建表部分的逐字副本，无索引、不写迁移记录）** —— **正式库那张未登记表的直接成因** |
| `internal/payments/void_integration_test.go` | **整个 `applyVoidMigration`**：`alter table payments` ×3、`drop/add constraint payments_status_check`、`drop/add payments_fee_amount_check`/`payments_payable_amount_check`、`alter column set not null`、以及**无范围 `update payments set fee_amount=0, payable_amount=submitted_amount`** |
| `internal/users/query_account_integration_test.go` | `alter table users add column if not exists ...` |

全部 7 个文件均已移除 `godotenv.Load(".../.env")` 与 `os.Getenv("DATABASE_URL")`，改为 `testdb.New`。前缀化 `delete` 清理保留为测试内部清理，但**数据库级隔离才是安全边界**。

## 阶段 11：静态防回归（**已实施**）

新增 `backend/internal/testdb/no_production_access_test.go` —— **纯静态、无需数据库、随每次 `go test ./...` 运行**。使用 **Go AST 解析**（非文本 grep），故注释与本文件自身的测试数据不会误判：

| 测试 | 拦截内容 |
| --- | --- |
| `TestNoTestLoadsDotEnv` | 任何 `_test.go` 调用 `godotenv.*`（生产 `internal/config/config.go` 白名单放行） |
| `TestNoTestReadsDatabaseURL` | 任何 `_test.go` 中 `os.Getenv/LookupEnv("DATABASE_URL")`（只检查**字符串字面量实参**；本守卫文件自身豁免） |
| `TestNoTestEmbedsProductionSchemaDDL` | `_test.go` 的**字符串字面量**含 `alter table users`/`alter table payments`/`create table if not exists admin_auth_audit_events`/`drop constraint if exists payments_*`/`update payments`（`internal/database` 的合成迁移测试豁免——它们跑在自己的一次性库上；本守卫文件自身豁免） |
| `TestNoProductionCodeImportsTestdb` | 任何非 `_test.go` 导入 `internal/testdb` |

**允许**：生产代码正常使用 `DATABASE_URL`；`testdb` 使用 `PJSK_TEST_DATABASE_ADMIN_DSN`；合成迁移测试的故意失败 SQL；守卫自身引用被禁字符串作为测试数据。

### 变异验证（证明守卫真的会失败）

在 `internal/orders` 临时植入探针文件，同时包含 `godotenv.Load("../.env")`、`os.Getenv("DATABASE_URL")`、`alter table payments`、`create table if not exists admin_auth_audit_events`。结果：**3 个守卫全部 FAIL，退出码 1**，报错精确定位到探针的行号：

```
--- FAIL: TestNoTestLoadsDotEnv        zz_mutation_probe_test.go:15 ...
--- FAIL: TestNoTestReadsDatabaseURL   zz_mutation_probe_test.go:16 ...
--- FAIL: TestNoTestEmbedsProductionSchemaDDL  zz_mutation_probe_test.go:17 / :18 ...
```

探针随后**已删除**（`internal/orders` 现仅 `handler.go`、`handler_test.go`）。

## 阶段 12：隔离数据库验证

### 12.1 无门禁：必须什么都不碰（**实测确认，非代码阅读**）

| 指标 | 结果 |
| --- | --- |
| 正式库 **运行前** | **18 migrations / 208 audit rows** |
| `go test -count=1 ./...`（`PJSK_RUN_DB_INTEGRATION_TESTS` 未设置） | 退出 0，全部包 ok |
| **SKIP 计数（实测输出）** | **46 个数据库集成测试 SKIP，0 FAIL**——含 `TestRouterAdminLoginRateLimitIgnoresSpoofedXFF`/`EndToEnd`/`HonorsTrustedProxy`（即写入 208 行的元凶） |
| SKIP 原因（实测输出） | `PJSK_RUN_DB_INTEGRATION_TESTS is not set to 1; skipping database integration test` |
| 正式库 **运行后** | **18 migrations / 208 audit rows**（**完全一致**） |
| 是否创建任何数据库 | **否**（`pjsk_%test%` 零行） |

### 12.2 带门禁：只碰隔离库

仅对子进程设置 `PJSK_RUN_DB_INTEGRATION_TESTS=1`（未写 `.env`、未设 User/Machine 级、结束即清空），管理 DSN 为无密码本机 DSN 由 pgpass 认证。

| 指标 | 结果 |
| --- | --- |
| 7 个包的集成测试 | **全部 ok**（api 9.5s / export 5.1s / payments 15.5s / query 11.1s / querycoderecovery 9.1s / recoveryemailverification 5.1s / users 10.2s），退出 0 |
| 正式库 **运行后** | **18 migrations / 208 audit rows**（未变） |
| ghost 账号审计行 | `router-rl-ghost-a`=**96**、`router-rl-ghost-b`=**112**（**未增长**——限流测试已改到隔离库） |
| `0019` 记录 / 审计表索引数 | `has_0019=0` / `audit_indexes=1`（正式库状态**未被改动**，`0019` 仍待受控修复） |
| 测试库残留 | **零行** |

### 12.3 完整验证结果

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `go fmt ./...` | 0 | 无差异 |
| `go build ./...` | 0 | 通过 |
| `go vet ./...` | 0 | 通过（含全部测试编译） |
| `go test -count=1 ./...`（无门禁） | 0 | 全部通过，46 个 DB 测试 SKIP |
| `go test ./internal/testdb`（静态守卫） | 0 | 4 个守卫全部 PASS；变异后如期 FAIL |
| 带门禁 7 包集成测试 | 0 | 全部通过，仅隔离库 |
| `go list -deps .` | — | **`internal/testdb` 不在正式二进制依赖中** |
| `Invoke-ScriptSafetyTests.ps1` | 0 | **246 PASS / 0 FAIL**（48+54+27+40+77，无回归） |
| `git diff --check` | 0 | 通过 |

## 修改文件（阶段 10–12）

| 文件 | 目的 |
| --- | --- |
| `backend/internal/testdb/testdb.go`（新） | 共享隔离 helper，强制全部安全规则 |
| `backend/internal/testdb/no_production_access_test.go`（新） | 4 个 AST 静态防回归守卫 |
| `backend/internal/api/export_routes_test.go` | 移除 `.env`/`DATABASE_URL`/**建表 DDL 副本**，改用 `testdb.New` |
| `backend/internal/payments/void_integration_test.go` | 移除 `.env`/`DATABASE_URL`/**整个 `applyVoidMigration`**（含无范围 UPDATE），改用 `testdb.New` |
| `backend/internal/export/handler_test.go` | 同上（无 DDL） |
| `backend/internal/query/payment_items_integration_test.go` | 同上（无 DDL） |
| `backend/internal/querycoderecovery/store_integration_test.go` | 同上（无 DDL） |
| `backend/internal/recoveryemailverification/store_integration_test.go` | 同上（无 DDL） |
| `backend/internal/users/query_account_integration_test.go` | 同上 + 移除 `alter table users` |
| `docs/development-logs/2026-07-15-production-database-readonly-verification.md` | 本日志 |

**未修改**：正式库、迁移文件、生产 Go 代码、PowerShell 脚本、`.env`、HANDOVER、部署配置。

## 安全边界确认（阶段 10–12）

未启动正式后端；未运行正式迁移；未修改正式库（前后聚合计数逐次核对一致）；未修复 `0019`；未创建正式索引；未手工改 `schema_migrations`；未执行真实备份恢复；未读业务明细；未读 `.env`/pgpass 内容；未把密码写入任何测试变量（DSN 无密码，pgpass 认证）；未提交、未推送；未使用子代理。

## 下一阶段入口

等待人工审阅本轮测试隔离修复并决定是否允许提交。提交后方可评估修复计划 2（`0019` 受控修复），且其前置条件仍包含"完成真实备份恢复演练"。
