# 迁移编号历史例外与正式部署前数据库只读核对

本文档做两件事：

1. 把迁移编号的历史例外（两个 `0005_*.sql`、永久缺 `0006`）正式登记为**不修复、不回填、不重命名**的既定事实，并给出未来编号规则。
2. 给出正式部署（或任何"让后端第一次连上某个库"）之前，必须由人工执行的**只读**核对清单和分状态处理分支。

适用读者：执行正式部署的人。执行清单前不需要读代码，但需要能用 `psql` 以只读方式连上目标库。

## 1. 历史例外登记

- `backend/migrations/` 同时存在 `0005_import_history.sql` 和 `0005_product_series.sql`，且没有 `0006_*.sql`。
- 迁移器（`backend/internal/database/migrations.go`）按**完整文件名**排序执行，并把完整文件名写入 `schema_migrations.version`。因此两个 `0005` 是两条互不冲突、各自独立追踪的迁移，执行顺序恒为 `0005_import_history.sql` → `0005_product_series.sql`（字节序）。
- 该状态**永久保留**，理由：
  - 重命名已应用的迁移文件会改变它在 `schema_migrations` 里的身份，使旧库把它当作未应用而重放——绝对禁止；
  - 回填 `0006` 会在已经执行到更高编号的库上乱序执行——绝对禁止；
  - 最终 schema 本身完全正确，没有任何需要修复的数据库状态。
- 自 2026-07-15 起由离线单元测试固化（`backend/internal/database/migrations_test.go`、`backend/main_test.go`）：除 `0005` 外前缀必须唯一、除 `0006` 外编号必须连续、文件名必须为 `NNNN_snake_case.sql`、嵌入集必须与磁盘一致。违反任何一条，`go test` 失败。

### 未来编号规则

- 新迁移一律从当前最大编号 +1 顺延（当前最大为 `0019`，下一个是 `0020`）。
- 永不复用任何已出现过的数字前缀；永不回填 `0006`；永不重命名、删除或修改任何已存在的迁移文件；永不手工修改 `schema_migrations`。

## 2. 部署前只读核对清单

前提与纪律：

- 全部命令**只读**（仅 `SELECT` 与元数据查询），不修改任何数据；即便如此，仍建议在核对前确认目标库有可用的新鲜备份。
- 不要把含密码的 DSN 写入命令行或粘贴到日志；用 `psql` 交互输密码或 `~/.pgpass`。
- 任何一步出现"异常结果"，**立即停止，不启动后端**（后端启动即自动执行迁移），带着输出回来评审。不要现场手工"修一下"。

### 步骤 0：确认连接目标

```sql
select current_database(), current_user, inet_server_addr(), version();
```

- 作用：确认连的确实是目标正式库，不是本地或测试库。只读。
- 正常：数据库名、主机与部署计划一致。
- 异常：名称或主机不符 → **必须停止**（后续所有结论都会作废）。

### 步骤 1：`schema_migrations` 是否存在

```sql
select exists (
  select 1 from information_schema.tables
  where table_schema = 'public' and table_name = 'schema_migrations'
) as has_schema_migrations;
```

- 作用：区分"跑过迁移的库"与"全新库"。只读。
- 正常：`true`（已有历史）或 `false`（可能是全新库）。两者都不是错误，决定走分支 A–C 还是分支 D。
- 异常：无（本步骤不会有异常值）。若为 `false`，先跳到步骤 6 确认库确实为空，再走分支 D。

### 步骤 2：已应用迁移全量清单

```sql
select version, applied_at from schema_migrations order by version;
select count(*) as applied_count from schema_migrations;
```

- 作用：拿到目标库迁移历史的完整事实。只读。
- 正常：0–19 行；`version` 全部是仓库已知文件名；按 `version` 排序后 `applied_at` 大体单调递增。
- 异常：出现仓库中不存在的文件名、行数超过 19 → **必须停止**（库的迁移历史与当前代码线不一致，可能来自改名文件或另一分支）。

### 步骤 3：与仓库文件清单的双向对比

```sql
with repo(version) as (values
  ('0001_core_tables.sql'),
  ('0002_import_tracking.sql'),
  ('0003_admin_auth.sql'),
  ('0004_import_confirm.sql'),
  ('0005_import_history.sql'),
  ('0005_product_series.sql'),
  ('0007_import_revert.sql'),
  ('0008_query_sessions.sql'),
  ('0009_admin_payments.sql'),
  ('0010_payment_voids.sql'),
  ('0011_payment_fee_fields.sql'),
  ('0012_normalize_payment_methods.sql'),
  ('0013_cn_merge.sql'),
  ('0014_user_query_account_admin.sql'),
  ('0015_query_code_bind_tokens.sql'),
  ('0016_user_recovery_email.sql'),
  ('0017_recovery_email_verification_codes.sql'),
  ('0018_query_code_email_recovery.sql'),
  ('0019_admin_auth_audit_events.sql')
)
select r.version as pending_in_db
from repo r left join schema_migrations m using (version)
where m.version is null
order by r.version;
```

```sql
with repo(version) as (values
  ('0001_core_tables.sql'),
  ('0002_import_tracking.sql'),
  ('0003_admin_auth.sql'),
  ('0004_import_confirm.sql'),
  ('0005_import_history.sql'),
  ('0005_product_series.sql'),
  ('0007_import_revert.sql'),
  ('0008_query_sessions.sql'),
  ('0009_admin_payments.sql'),
  ('0010_payment_voids.sql'),
  ('0011_payment_fee_fields.sql'),
  ('0012_normalize_payment_methods.sql'),
  ('0013_cn_merge.sql'),
  ('0014_user_query_account_admin.sql'),
  ('0015_query_code_bind_tokens.sql'),
  ('0016_user_recovery_email.sql'),
  ('0017_recovery_email_verification_codes.sql'),
  ('0018_query_code_email_recovery.sql'),
  ('0019_admin_auth_audit_events.sql')
)
select m.version as unknown_to_repo
from schema_migrations m left join repo r using (version)
where r.version is null
order by m.version;
```

- 作用：第一条列出"仓库有、库里没有"（= 后端启动时会执行的迁移）；第二条列出"库里有、仓库没有"。只读。
- 正常：第二条**必须为空**；第一条为空（分支 A）、恰为 `0019` 一行（分支 B）、或为按 `version` 排序的**末尾连续一段**（分支 C）。
- 异常：第二条非空 → **必须停止**。第一条出现"中间洞"（例如缺 `0009` 但 `0010` 已应用）→ **必须停止**（启动会把 `0009` 乱序补跑在 `0019` 之后的语义之前，历史已分叉）。

### 步骤 4：双 `0005` 状态

```sql
select version, applied_at from schema_migrations
where version like '0005\_%' escape '\'
order by version;
```

- 作用：确认历史例外对在目标库中的真实状态。只读。
- 正常：两行都在（已升级过的旧库），或一行都没有（尚未跑到 `0005` 的库/空库）。
- 异常：只有一行，且步骤 3 显示 `0005` 之后的迁移已应用（例如 `0007` 已在库里）→ **必须停止**（正常执行顺序不可能跳过其中一个 `0005` 去执行 `0007`，说明记录被人为动过或历史异常）。只有一行且后续迁移也全部未应用，属于"上次启动恰好中断在两个 `0005` 之间"的罕见但合法状态，可继续（启动会补齐另一个）。

### 步骤 5：记录与真实 schema 抽样互证

```sql
select
  exists (select 1 from information_schema.columns
          where table_schema = 'public' and table_name = 'import_batches'
            and column_name = 'warnings_accepted') as has_0005_import_history_effect,
  exists (select 1 from information_schema.columns
          where table_schema = 'public' and table_name = 'products'
            and column_name = 'series_code') as has_0005_product_series_effect,
  exists (select 1 from information_schema.tables
          where table_schema = 'public' and table_name = 'admin_auth_audit_events') as has_0019_effect;
```

- 作用：防"记录说应用了、schema 里却没有"（手工插入过 `schema_migrations` 行的典型症状）。只读。
- 正常：每一项的 `true/false` 与步骤 2 中对应 `version` 行的有无**完全一致**。
- 异常：任一不一致 → **必须停止**（`schema_migrations` 曾被手工修改，直接启动会静默跳过本应执行的迁移或在重放时报错）。

### 步骤 6：（仅当步骤 1 为 `false`）确认全新库

```sql
select table_name from information_schema.tables
where table_schema = 'public'
order by table_name;
```

- 作用：确认"没有 `schema_migrations`"等于"整个库是空的"，而不是"有一套手工建的表"。只读。
- 正常：零行 → 真正的全新库，走分支 D。
- 异常：存在任何业务表（`users`、`orders`……）却没有 `schema_migrations` → **必须停止**（这是手工建库或极老的遗留库，直接跑 `0001` 会与现存表冲突或静默错配）。

## 3. 分状态处理分支

| 分支 | 核对结果 | 处理 |
| --- | --- | --- |
| A | 19 行全部应用，步骤 3 两条均为空，步骤 5 全一致 | 库已最新。启动后端时迁移为零操作，正常继续部署。 |
| B | 恰好 18 行（含两个 `0005`），仅 `0019` 待执行 | 预期中的旧库升级。确认新鲜备份后启动后端，`0019` 会在单事务内自动应用；启动日志应出现 `database migration applied: 0019_admin_auth_audit_events.sql`。 |
| C | 待执行集是末尾连续一段（不止 `0019`） | 机制上安全（启动会按序补齐），但与"正式库曾运行到 0018"的认知不符——先弄清这是哪个库、为何落后，确认备份后再启动。 |
| D | 无 `schema_migrations` 且步骤 6 为零行 | 全新安装。确认连接目标无误后启动后端，19 个迁移按序自动执行（注意：`main.go` 的 10 秒超时覆盖连接+迁移，远程高延迟库首启可能超时中断；中断是安全的，重启后端会从断点继续）。 |
| E（停止） | 步骤 0 目标不符 / 步骤 2–5 任一异常 / 步骤 6 有表无记录 | **不启动后端**。保留全部查询输出，回到评审流程。禁止现场手工增删 `schema_migrations`、禁止重命名文件、禁止手工建表"对齐"。 |

回滚方式：核对本身只读，无需回滚。启动后端后若迁移失败，失败的那个迁移已整体回滚（单事务），后端拒绝启动且不会带病服务；此时按分支 E 处理，必要时用既有备份工具恢复（见 `docs/database-backup-restore.md`）。

## 4. 与本问题相关的其他文档

- `HANDOVER.md` §14 已知问题（双 `0005` 条目）、§16 高风险操作（禁止重命名迁移/手改 `schema_migrations`）。
- `docs/development-logs/2026-07-15-migration-numbering-compatibility-review.md`：本方案的调查与决策过程。
- `docs/database-backup-restore.md`：备份/恢复与校验脚本（校验以 `schema_migrations` 最大 `version` 为准）。
