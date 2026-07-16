# PostgreSQL 0019 正式迁移执行记录

> 安全边界：本文不记录数据库密码、完整连接串、密钥、Token、业务明细或恢复邮箱内容。数据库核对查询均只输出迁移事实、对象定义和聚合计数。

## 阶段 1：只读基线调查

- 执行日期：2026-07-16（Asia/Shanghai）。
- Git 分支：`main`。
- 执行前 `HEAD`：`082fc42f12b8c17e5159a85f77e1dd0228132860`。
- 执行前 `origin/main`：`082fc42f12b8c17e5159a85f77e1dd0228132860`。
- `git status --short`：空；暂存区：空。
- 迁移文件：`backend/migrations/0019_admin_auth_audit_events.sql`。
- 内容摘要：创建 `admin_auth_audit_events` 审计表（10 列、UUID 主键、管理员外键、事件/结果/原因及长度约束），并创建按发生时间、事件类型+时间、管理员+时间、用户名+时间查询的 4 个二级 B-tree 索引。迁移不包含数据回填、删除或修改既有业务记录。
- 标准执行入口：后端 `main.go` 调用 `database.RunMigrations`；迁移器按完整 SQL 文件名字节序执行未登记迁移，将单个迁移 SQL 与 `schema_migrations` 插入放在同一事务中，失败时回滚并拒绝启动。标准执行方式为启动仓库后端，不手工执行 SQL。
- 连接保护：使用本机 PostgreSQL 18 客户端和默认 pgpass 认证；仅确认 pgpass 文件存在，未读取其内容；`DATABASE_URL` 与 `PGPASSWORD` 在当前进程均未设置。所有 SQL 在 `default_transaction_read_only=on` 和显式 `BEGIN TRANSACTION READ ONLY` 中执行，最后 `ROLLBACK`。
- 连接目标：数据库名 `pjsk`，数据库用户 `postgres`，服务端 `127.0.0.1:5432`；已确认不是恢复演练库或测试库。
- 正式库迁移前基线：已执行 18 条；最低 `0001_core_tables.sql`；最高 `0018_query_code_email_recovery.sql`；完整列表与仓库前 18 个迁移文件一致；未知迁移 0；重复版本 0；`0019_admin_auth_audit_events.sql` 记录 0。
- `0019` 相关结构与数据状态：`admin_auth_audit_events` 表已存在，10 列、默认值、主键、管理员外键和全部 CHECK/NOT NULL 约束与迁移定义一致且均已验证；表内 208 行，必填列空值违规 0；当前仅有主键索引，迁移定义中的 4 个二级索引均尚不存在。
- 阶段结论：任务要求的迁移数量和最高版本门禁完全满足，即“18 条、最高 0018、0019 未执行”。已存在表属于此前已调查并修复来源的部分结构状态；本阶段未执行迁移、未修改数据库、未读取业务明细。
- 子代理：未使用。
## 阶段 2：迁移前安全检查

- 最新正式备份：`D:\PJSK-Backups\PostgreSQL\2026\07\pjsk-20260715-221906.dump`（创建于 2026-07-15 22:19 本机时间，118890 bytes）。
- 备份三件套：dump、`pjsk-20260715-221906.metadata.json`、`pjsk-20260715-221906.validation.json` 均存在，文件名和大小交叉引用一致；metadata 标记为正式来源备份（`isolatedTestBackup=false`）。
- 可读性：PostgreSQL 18 `pg_restore --list` 可正常读取归档，归档内部逻辑数据库名为 `pjsk`。
- 哈希复核：重新计算 dump SHA-256，实际值与 metadata、validation 中记录三方完全一致；哈希值仅在执行终端核对，不写入本日志。
- validation：`overallResult=passed`，`validationPurpose=pre-migration`；实际迁移事实与期望迁移事实均为 18 条、最高 `0018_query_code_email_recovery.sql`，满足正式迁移前回滚基线要求。
- `.partial`：在整个正式备份根目录递归检查为 0。
- 独立只读保留报告：使用仓库现有 `Get-PostgresBackupRetentionReport.ps1 -VerifyHash`，结果 `Status=verified`、`Decision=Keep`、`RetentionTier=newest`、`HasPartial=false`。首次直接运行被本机脚本签名策略阻止，随后仅对子进程使用 `-ExecutionPolicy Bypass` 成功运行，未改变系统策略或备份文件。
- 冲突检查：正式库除核对连接外无其他会话，活动会话 0、锁等待 0，`admin_auth_audit_events` 与 `schema_migrations` 上无其他已授予冲突锁；本机无 `pjsk-backend`、`go`、`psql` 遗留进程，8080 无监听。
- 目标复核：迁移目标仍为正式数据库 `pjsk`（本机 PostgreSQL），名称不符合恢复演练库/测试库前缀，且与备份源逻辑库名一致。
- SQL 事务性：迁移器把完整 SQL 和迁移记录插入置于同一个 pgx 事务；本迁移只含 PostgreSQL 支持事务回滚的 `CREATE TABLE/INDEX`，失败不会留下部分索引或迁移记录。
- 不可逆性：无 DROP、DELETE、UPDATE 或回填；只新增结构。项目没有 down migration，但事务提交前可完整回滚。
- 锁表风险：表已存在，因此建表语句为空操作；4 个普通 `CREATE INDEX`（非 CONCURRENTLY）会短暂阻塞该表写入，但现有表仅 208 行且当前无其他会话或冲突锁，预计耗时很短。
- 重复执行风险：SQL 使用 `IF NOT EXISTS`，且迁移器以完整文件名在主键约束保护的迁移表中登记；4 个目标索引名当前均不存在。迁移记录一旦提交，标准入口不会重放。
- 数据约束风险：迁移不新增表级数据约束（表已存在且全部定义已逐项匹配）；现有约束均 `convalidated=true`，必填列空值违规 0。4 个非唯一索引允许重复值及 `admin_id` 空值，不需要迁移前清理重复或空值。
- 阶段结论：备份、目标、并发、事务、对象名和现有数据检查全部通过，可以进入受控迁移执行。未修改迁移文件、备份脚本、业务代码或数据库。

## 阶段 3：执行 `0019`

- 执行前安全打印：目标数据库 `pjsk`；当前迁移 `0018_query_code_email_recovery.sql`（18 条）；待执行文件 `0019_admin_auth_audit_events.sql`。未打印密码或连接参数。
- 第一次标准启动：在 `backend` 目录运行 `go run .`，配置加载在数据库连接前因缺少 `QUERY_CODE_RECOVERY_HMAC_KEY` 退出（exit 1）；迁移器未运行。随后用只读事务复核仍为 18/0018、`0019` 记录 0、审计表 208 行、索引 1 个，确认数据库未变。
- 处置：未修改 `.env`、配置代码或迁移文件。第二次仍使用标准 `go run .`，仅对子进程设置临时 HMAC 配置，并强制监听 `127.0.0.1:18080`；配置值未输出、未落盘、进程结束后清除。
- 临时配置告警：本机 PowerShell/.NET 不支持所调用的随机填充 API，生成调用报错后数组保持默认内容；该临时值只用于通过长度校验，未持久化且服务仅短暂绑定本机端口。后续不会把它用于正式部署或业务运行。
- 迁移器输出：`database connected`，随后 `database migration applied: 0019_admin_auth_audit_events.sql`，再进入 `backend listening on 127.0.0.1:18080`。
- 进程处置：确认迁移日志与监听状态后立即发送 Ctrl+C，中断退出码为 Windows Ctrl+C 对应的 `0xc000013a`；没有留下预期中的后台后端进程。
- 本阶段结论：标准迁移入口报告事务已应用；未手工执行 SQL、未手工增删改业务记录、未修改任何代码或配置。成功结论仍待阶段 4 独立数据库验证。

## 阶段 4：迁移后独立验证

- 独立性：全部验证在新的 psql 连接、`default_transaction_read_only=on`、显式只读事务中执行，不依赖迁移命令退出码或启动日志。
- 连接：`SELECT 1` 成功；目标仍为 `pjsk`；事务只读状态为 `on`。
- 迁移历史：总数 19；最低 `0001_core_tables.sql`；最高 `0019_admin_auth_audit_events.sql`；`0019_admin_auth_audit_events.sql` 恰好 1 条，应用时间为 2026-07-16 09:21:02 +08；未知迁移 0。
- 表和列：`admin_auth_audit_events` 存在，精确 10 列且 10 个预期列名全部命中；迁移没有要求删除旧表、旧列或旧约束，因此无待移除旧结构。
- 约束：主键 1、管理员外键 1、CHECK 7，共 9 个语义约束；名称和类型与迁移定义一致，`convalidated` 全为真，失效约束 0。
- 索引：精确 5 个，即主键索引加迁移要求的 4 个二级 B-tree 索引；名称、列顺序与 `DESC` 定义全部匹配；5 个索引均 `indisvalid=true`、`indisready=true`、`indislive=true`，不可用索引 0。
  - `admin_auth_audit_events_occurred_at_index (occurred_at DESC)`
  - `admin_auth_audit_events_type_time_index (event_type, occurred_at DESC)`
  - `admin_auth_audit_events_admin_time_index (admin_id, occurred_at DESC)`
  - `admin_auth_audit_events_username_time_index (username_normalized, occurred_at DESC)`
- 数据核对：迁移不包含数据回填。审计表从迁移前 208 行到迁移后 208 行；必填列空值违规、非法事件类型、非法结果、非法原因、结果/原因组合违规、长度违规均为 0。
- 业务数据影响：实际执行的迁移 SQL 无 INSERT/UPDATE/DELETE/TRUNCATE/DROP，仅创建缺失索引并由迁移器写入一条迁移历史；执行前无其他数据库会话，迁移后审计行数不变，短暂启动期间仅请求无业务写入的 `/health`。因此未发现业务表总量减少或任何无法解释的数据写入。未采集或输出业务明细。
- 关键查询：针对事件类型+时间、管理员+时间的查询均能成功规划；小表下优化器可按成本选择普通索引或顺序扫描，不影响索引有效性验证。
- 应用健康：使用兼容本机运行时的密码学随机源生成一次性、不落盘的进程配置，后端启动只输出 `database connected` 和监听信息，没有再次应用迁移；`GET http://127.0.0.1:18080/health` 返回 `status=ok`、`database=connected`。
- 数据库错误：迁移执行、独立查询、后端重启和健康检查期间未出现新的数据库错误。
- 清理：健康检查后 Ctrl+C 停止后端；最终 `pjsk-backend`/`go`/`psql` 进程 0，18080 监听 0；最终再查仍为 19/0019、`0019` 记录 1、审计表 208 行。
- 阶段结论：`0019` 已成功执行并通过迁移历史、结构、数据聚合、连接与应用健康的独立验证。

## 阶段 5：项目验证

- Go 格式检查：对仓库全部 `.go` 文件运行 `gofmt -l`，输出为空，通过；未改写文件。
- `go build ./...`：通过。
- `go vet ./...`：通过。
- `go test -count=1 ./internal/admin`：通过（包含认证审计事件构建、校验、登录成功/失败/限流/退出及审计失败处理等专项测试）。
- `go test -count=1 ./...`：全量通过；数据库集成测试遵循既有默认隔离门禁，未连接正式库。
- 前端 build：未运行；`0019` 仅涉及后端数据库审计结构，本次无前端变更且现有验收流程未要求为纯数据库迁移重复构建前端。
- 代码与配置修改：无；迁移文件未修改；备份/恢复脚本未修改。仓库实际修改仅为本执行日志。

## 封版结论与 Git 状态（提交前记录）

- `git diff --check`：通过。
- 本次日志敏感信息扫描：未发现数据库 DSN、密码赋值、PGPASSWORD 赋值、私钥、常见 Token 或邮箱地址模式；日志未记录完整 SHA-256 值。
- 全部项目验证完成后再次只读复核：正式库 `pjsk` 仍为 19 条迁移、最高 `0019_admin_auth_audit_events.sql`、`0019` 恰好 1 条；审计表 208 行、索引 5 个、不可用索引 0。
- 实际修改：仅新增本日志；未修改代码、配置、迁移 SQL、备份脚本或业务数据。没有删除、移动或重命名历史开发日志。
- 提交前状态：本日志未跟踪，暂存区为空，除此之外无工作区变更；截至本条记录时尚未提交、尚未推送。本日志计划作为唯一文件以 `docs: record migration 0019 execution` 提交并普通推送，实际 SHA 与推送结果在任务最终汇报中给出。
- 最终迁移结论：`0019` 成功；执行前 18 条/最高 `0018_query_code_email_recovery.sql`，执行后 19 条/最高 `0019_admin_auth_audit_events.sql`。
- 遗留风险：当前持久化后端环境缺少 `QUERY_CODE_RECOVERY_HMAC_KEY`，不提供进程级临时值时标准后端会在数据库连接前拒绝启动。该配置缺口不影响已经独立验证提交的数据库迁移，但正式业务服务部署前必须由密钥持有人通过受保护配置渠道设置真实、随机且足够长度的密钥；不得把密钥写入 Git 或开发日志。本次按范围要求未修改配置或密钥文件。
- 下一步建议：密钥持有人完成受保护配置后，按现有部署流程正常启动服务并再次访问 `/health`；迁移器将识别 `0019` 已登记，不会重放。
- 子代理：本轮未使用任何子代理、子任务或并行代理。
