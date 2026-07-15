# 部署前剩余离线风险收口：备份演练迁移数量与启动迁移超时评审

## 任务边界

- 日期：2026-07-15
- 目标：收口上一阶段登记的两项离线风险——(1) 备份演练 fixture 迁移数量写死；(2) `main.go` 10 秒上下文同时覆盖连接与迁移。
- 全程不连接任何数据库，不执行迁移或任何真实 SQL，不读取真实 `.env` 或密钥，不操作真实备份，不使用 `-Execute`，不改系统配置，不用子代理。
- 本阶段不提交、不推送，等待人工审阅。

## 阶段 1：只读基线检查

- 开始时间：2026-07-15 14:30（本机时间）
- 分支 `main`；`git status --short` 为空（工作区、暂存区干净）。
- `git rev-parse HEAD` = `git rev-parse origin/main` = `c45e85c048ac66be5f4ee92702ce3653736bb86f`，与交接基线一致。
- `git log -3 --oneline`：`c45e85c test: document migration numbering compatibility` / `ab25adb docs: record pre-deployment verification` / `4edb307 feat: add admin authentication audit`。
- `git ls-files --others --exclude-standard`：空，无他人遗留未跟踪文件。
- 已阅读：`AGENTS.md`、`HANDOVER.md`、`docs/migration-numbering-deployment-check.md`、`docs/development-logs/2026-07-15-migration-numbering-compatibility-review.md`、`scripts/database/Backup-Postgres.ps1`、`Test-PostgresBackup.ps1`、`Invoke-ScriptSafetyTests.ps1`、`_RetentionCommon.ps1`、`Invoke-RetentionSafetyTests.ps1`、`backend/main.go`、`backend/internal/database/migrations.go`、`docs/database-backup-restore.md`。
- 结论：状态与基线一致，可继续。
- 是否连接数据库：否。是否读取敏感信息：否。是否使用子代理：否。是否修改文件：仅新建本日志。

## 阶段 2：备份演练写死问题根因调查

- 开始时间：2026-07-15 14:35（本机时间）
- 发现**两处**写死，而不是任务书预估的一处：

| # | 位置 | 内容 | 是否被消费 | 后果 |
| --- | --- | --- | --- | --- |
| 1 | `Backup-Postgres.ps1:166` | `schema_migrations = 18`（在 `$fixtureExpectedRowCounts` 内） | **从不被任何脚本读取**（全仓库 `.ps1` 检索确认只有写入点，无读取点） | 写入每个备份的 metadata JSON，已过时但不会导致演练失败 |
| 2 | `Test-PostgresBackup.ps1:105` | `Assert ($restored.migrationMax -eq '0018_query_code_email_recovery.sql')` | **是**，演练必经断言 | 迁移已到 `0019`，下次恢复演练此断言**必然 FAIL** |

- 根因（两处共同）：把"当前仓库迁移集"这一会随开发前进而变化的事实，以字面量硬编码进脚本，且没有任何测试把它与 `backend/migrations/` 的真实内容绑定。`0019` 合入时无人同步这两处，问题即产生；将来 `0020` 会再犯。
- 第 1 处的附加问题：`$fixtureExpectedRowCounts`（`admins=1, users=2, … schema_migrations=18`）**无条件**写入每个备份的 metadata，包括非 `-RequireIsolatedSource` 的真实生产备份。对真实备份而言，声称"users 期望 2 行"是误导性元数据。它是演练 fixture 的期望值，只在隔离演练语境下有意义。
- 真正会阻断演练的是第 2 处；第 1 处是误导性死数据。两者同源，一并收口。
- 是否连接数据库：否。是否读取敏感信息：否。是否使用子代理：否。本阶段仅调查，未改文件。

## 阶段 3：迁移数量改为动态计算

- 开始时间：2026-07-15 14:45（本机时间）
- 结论：**采用动态计算**，不是把 `18` 改成 `19`。

### 修改内容

1. 新增 `scripts/database/_MigrationFacts.ps1`（只读共享 helper，遵循 `_RetentionCommon.ps1` 既有约定：只定义函数、无 param 块、无顶层动作、可安全 dot-source）：
   - `Resolve-RepositoryMigrationsDirectory -ScriptDirectory`：由 `$PSScriptRoot/../../backend/migrations` 定位仓库迁移目录（脚本已有同样的 `..\..` 定位先例，无需额外参数传递）。
   - `Get-MigrationFacts -MigrationsDirectory`：返回 `Directory` / `Names` / `Count` / `MaxVersion`。
   - 用 `-File` 排除目录；不用 `-Recurse`，与 Go 侧非递归 `fs.ReadDir` 对齐；`-Filter '*.sql'` 之外再加 `EndsWith('.sql', Ordinal)` 兜底（Windows 通配符会把 `.sqlx` 之类误纳入）。
   - 文件名必须匹配 `^\d{4}_[a-z0-9_]+\.sql$`（与 Go 测试同一规则），否则抛错。
   - 目录缺失、无 `.sql`、文件名不规范一律**抛错**，绝不回退到固定数字。
   - 用 `[Array]::Sort(..., StringComparer::Ordinal)` 排序，避免 PowerShell 默认区域性排序与 Go 字节序排序产生分歧。
2. `Backup-Postgres.ps1`：`schema_migrations` 由 `$migrationFacts.Count` 动态得出；同时把整个 `$fixtureExpectedRowCounts` 收敛为**仅演练备份（`-RequireIsolatedSource`）才写入**——它描述的是演练 fixture，盖在真实备份上属误导性元数据。
3. `Test-PostgresBackup.ps1`：新增可选 `-MigrationsDirectory` 参数（默认解析仓库路径，便于离线测试注入临时目录）；`0018_query_code_email_recovery.sql` 字面断言改为与 `$migrationFacts.MaxVersion` 比较；**新增**一条 `migrationCount` 与 `$migrationFacts.Count` 相等的断言。迁移事实查找放在既有参数校验之后，故三条离线用例仍在校验阶段退出 1，行为不变。
4. 新增 `scripts/database/Invoke-MigrationFactsTests.ps1`（27 项离线测试），并按保留策略测试的既有先例串接进 `Invoke-ScriptSafetyTests.ps1`。

### 测试覆盖（对照任务书 7 项要求）

| 要求 | 覆盖 |
| --- | --- |
| 当前 19 个迁移时验证通过 | 真实仓库目录校验通过，实测 `Count=19`、`MaxVersion=0019_admin_auth_audit_events.sql` |
| 增加一个合法新迁移后期望值自动变化 | 合成目录加入 `0020_future_feature.sql` → Count 6→7、Max 自动移动到 `0020`，并断言 Max **未**冻结在旧值 |
| 非 SQL 文件不计入 | `.txt` / `.md` / `.sql.bak` / `.sqlx` 均不计入 |
| 子目录中的 SQL 不计入 | 嵌套 `.sql` 不影响 Count 与 MaxVersion |
| 找不到迁移目录时明确失败 | 抛错且错误信息含成因；空目录亦抛错而非报 0 |
| 不能静默回退到固定数字 | helper 无 18/19 回退（静态检查）；两个消费脚本不再出现字面量（静态检查） |
| 不接触真实备份或数据库 | helper 静态检查不含任何 pg 工具与写文件命令；全部 fixture 在自建临时目录 |

- 对真实仓库**刻意不断言精确数字 19**，只断言 `>= 19` 及永久事实（双 `0005`、`0019` 在列、命名规范）——把 19 钉死正是本次要消除的问题；精确计数行为改由我完全掌控内容的合成目录验证。

### 验证结果

- 5 个脚本 `[Parser]::ParseFile` 语法检查：全部 OK。
- `Invoke-MigrationFactsTests.ps1`（子进程 `-ExecutionPolicy Bypass`，未改系统执行策略）：退出码 0，**27 项全通过**。
- `Invoke-ScriptSafetyTests.ps1` 全量：退出码 0 —— 原备份/恢复安全测试 **48** 项通过、保留策略 **54** 项通过、新增迁移事实 **27** 项通过，**共 129 项、0 失败**。
- 临时目录已清理（`pjsk-migfacts-tests-*`、`pjsk backup safety tests`、`bkretention-tests-*` 均无残留）；仓库内无 `.dump` / `.backup` / `.partial` / `.metadata.json` / `.validation.json` 残留。
- 未接触真实数据库或真实备份目录；默认仍为 DryRun；未使用 `-Execute`。

### 附带发现（**未修改**，仅登记）

`Backup-Postgres.ps1:202` 的元数据校验含 `-not $metadataCheck.isolatedTestBackup`，与其它条件以 `-or` 相连。这意味着**非** `-RequireIsolatedSource` 的真实备份在发布阶段必然抛错，dump 被删除并以 "Could not atomically publish the backup and metadata." 失败——即该脚本目前只能成功产出演练备份，无法产出真实生产备份。这是既有逻辑，非本轮引入，且超出本阶段范围，未改动。它需要用户决策：脚本是"仅演练"设计，还是这是首次真实备份时才会暴露的缺陷。（本轮把 fixture 收敛为仅演练写入，在两种结论下都成立。）

- 是否连接数据库：否。是否读取敏感信息：否。是否使用子代理：否。

## 阶段 4：`main.go` 10 秒上下文评审

- 开始时间：2026-07-15 15:05（本机时间）
- 调查方式：只读 `backend/main.go`、`backend/internal/database/postgres.go`、`migrations.go`，并直接查阅模块缓存中的 pgx v5.10.0 源码 `pgxpool/pool.go`（`C:\Users\…\go\pkg\mod\github.com\jackc\pgx\v5@v5.10.0`）核实连接语义，未连接任何数据库。

### 10 个问题的准确答案

1. **超时起点**：`main.go:26` `context.WithTimeout(context.Background(), 10*time.Second)`，紧跟 `config.Load()` 之后开始计时。这是**绝对墙钟截止时间**，不在阶段之间重置。
2. **`pgxpool.New` 是否建立连接**：**不建立**。pgx v5 `NewWithConfig`（`pool.go:220`）只构造池对象即返回；唯一的连接尝试在后台 goroutine（`pool.go:333-337`）调用 `createIdleResources(ctx, max(minConns, minIdleConns))`。`defaultMinConns` 与 `defaultMinIdleConns` 均为 `0`（`pool.go:20-21`），`createIdleResources` 目标为 0 时两个循环各迭代 0 次并立即返回 nil（`pool.go:568-595`）。故 `pgxpool.New` **无任何网络 I/O**，只在 DSN/配置解析失败时报错。
3. **首次真实连接**：`postgres.go:15` 的 `pool.Ping(ctx)`。Ping 从池中获取连接，触发第一次 TCP 连接 + TLS 握手 + 认证，受共享的 10 秒 ctx 约束。
4. **19 个迁移是否共享剩余时间**：**是**。同一个 ctx 传给 `RunMigrations`，其中每次 `Exec`/`QueryRow`/`Begin`/`tx.Exec`/`tx.Commit` 都用它。没有逐条预算，全部迁移共享 Ping 之后剩下的时间。
   - 往返次数量级：已最新的库仍需 1 次建表 + 19 次 exists 检查 ≈ **20 次往返**；全新空库另加每个迁移约 5 次往返（exists/BEGIN/DDL/INSERT/COMMIT）≈ **95–100 次往返**。
5. **超时后当前迁移事务**：ctx 到期时在途语句返回 ctx 错误，`applyMigration` 返回错误；`defer` 里的 `tx.Rollback(ctx)` 因 ctx 已死无法发送 ROLLBACK 而失败（只记一条脱敏日志）。**但事务仍被回滚**：pgx 在 ctx 取消时将连接标记为损坏并关闭（`pool.go:320-324` 的 `CleanupDone`/`ctx.Done()` 逻辑），连接断开导致 PostgreSQL 服务端中止该未提交事务。**不会半提交**。
6. **已提交迁移是否保留**：**保留**。每个迁移独立提交，第 1..k 个已提交的迁移及其 `schema_migrations` 行都在，只丢失在途的那一个。
   - 边界情况：若 ctx 恰在 `tx.Commit(ctx)` 期间到期，客户端放弃而服务端可能已提交成功——此时该迁移的 **SQL 与 version 行在同一事务内一起提交**，状态仍然自洽，重启后被正确识别为已应用并跳过。这是现有设计的一个真正优点。
7. **是否拒绝启动**：**是**。`log.Fatalf` → `os.Exit(1)`，`ListenAndServe` 从不执行。fail-closed，超时不可能产出"半迁移且在跑"的后端。
8. **重启是否安全断点续跑**：**是**。重启获得全新 10 秒预算，已应用迁移按完整文件名精确匹配跳过，从第一个未应用文件继续。代价是每次重启重付约 20 次 exists 往返（相对 DDL 很便宜）。
9. **本地 vs 远程风险差异**：
   - 本地 `127.0.0.1`：RTT ≈ 0.1ms，100 次往返 ≈ 10ms，耗时由 DDL 执行主导，全新库全量迁移远低于 1 秒。10 秒**非常充裕**，与开发环境实际观察一致。
   - 远程（如 Supabase 经 WAN + TLS）：RTT 20–200ms+，Ping 本身含 TCP+TLS+认证多次往返。全新空库 95–100 次往返 ≈ 2–20 秒，**RTT ≥ 100ms 时全量迁移可能真的超过 10 秒**；已最新的库约 20 次往返较安全，但 200ms RTT 下仍约 4 秒 + Ping。另外 Supabase 免费层会休眠，**冷启动唤醒单次 Ping 就可能耗时数秒甚至逼近 10 秒**。
   - 关键歧义：`HANDOVER.md` §3 记载生产/预发使用 **Supabase 托管实例**，而部署文档假定本机 `D:\PostgreSQL\18\bin`。正式库究竟是本地还是远程**当前未确认**，这本身抬高了远程分支的权重。
10. **是否应在正式部署前修改**：**建议修改（方案 A）**。当前失败模式虽然安全（fail-closed、不半提交、可续跑），但在远程/冷启动分支下 10 秒确实可能不足，而修复成本极小。

### 方案对比

| 方案 | 结论 | 理由 |
| --- | --- | --- |
| **A. 连接短超时 + 迁移独立更长超时** | **采纳** | 精确消除唯一现实失败场景（远程/慢链路上的全量迁移或冷启动），且**保留 DSN 配错/库不可达时的快速失败**。改动约 6 行，两个常量 + 两个 ctx。 |
| B. 迁移无总时限，仅靠数据库侧逐条超时 | 否决 | 违反"不能无限挂死"：某迁移若被锁阻塞（例如另一会话持有 `import_batches` 锁），客户端无截止时间会永久挂起。要补救需在真实库上设置 `lock_timeout`/`statement_timeout`——本阶段禁止连库，且把安全性押在服务端配置上属于风险外移。 |
| C. 整体提高超时 | 否决 | 会连带放大连接阶段：把单一超时提到 2 分钟意味着 DSN 写错或数据库没起时，服务启动要挂 2 分钟才失败，与"失败自动重启 + 退避"策略叠加后表现更糟。快速失败与充足迁移预算是两个不同需求，不该共用一个数字。 |
| D. 不改代码，只写部署注意事项 | 否决（但可接受作为下策） | 本地库场景今天确实够用，且 WinSW/NSSM 的失败重启会自动重试。但它把已知隐患留给未来的远程分支，并依赖人工记得"首启失败要重试"，属于把风险转移到人工。A 的成本远低于这个代价。 |

### 关于 Windows 服务启动超时

WinSW/NSSM 以**子进程**方式包装后端，向 SCM 汇报的是包装器自身的启动，后端 exe 绑定端口的耗时不受 SCM 默认 30 秒服务启动超时约束。因此把迁移预算提到 2 分钟**不会**破坏服务化模型。文档已建议失败重启 + 退避（`docs/windows-service-deployment.md:51-53`），与 fail-closed 的迁移失败行为相容。

### 选定参数

- 连接：**10 秒**（保持不变）——足够本地与远程的 TCP+TLS+认证，且能快速暴露配错。
- 迁移：**2 分钟**——相对远程最坏估算（约 20 秒）留约 6 倍余量，同时仍为有限值，锁阻塞不会永久挂死。以具名常量 + 注释表达，需要调整时一眼可见；不新增环境变量，避免扩大配置面。

- 是否连接数据库：否。是否读取敏感信息：否（未读取真实 DSN 或 `.env`）。是否使用子代理：否。

## 阶段 5：实施方案 A 与全量验证

- 开始时间：2026-07-15 15:20（本机时间）

### 修改内容

1. `backend/main.go`：新增两个具名常量 `databaseConnectTimeout = 10 * time.Second`（不变）与 `databaseMigrationTimeout = 2 * time.Minute`；把原来单一的 `ctx` 拆成 `connectCtx`（给 `database.Connect`）与 `migrateCtx`（给 `database.RunMigrations`）。除超时预算外**没有任何行为改动**：连接方式、迁移顺序、事务、fail-closed 退出、日志脱敏全部不变。
2. `backend/main_test.go`：新增 `TestStartupTimeoutsAreSeparateBudgets`，锁定超时策略——两者均为正数、迁移预算有限（`<= 10m`，防止锁阻塞永久挂死）、迁移预算严格大于连接预算（防止回退成共享预算）、连接预算保持短（`<= 30s`，保证快速失败）。
3. `docs/migration-numbering-deployment-check.md`：分支 D 原文描述"10 秒超时覆盖连接+迁移，远程首启可能超时"，改动后已过时，同步更正为分开的 10 秒 / 2 分钟预算。

### 测试有效性验证（变异测试）

具名常量断言容易写成永真的空测试，因此做了一次变异验证：把 `databaseMigrationTimeout` 临时改成 `10 * time.Second`（即回退为与连接共享同一预算的旧语义），`go test` **如期 FAIL**，报错 `databaseMigrationTimeout (10s) must exceed databaseConnectTimeout (10s)`；随后完整还原，`git diff -- backend/main.go` 确认只剩预期改动，测试恢复 PASS。该测试确实能捕获它要防的回归。

### 无法离线验证的部分（测试设计，待隔离数据库阶段执行）

- **迁移超时不半提交**：对隔离测试库，用一个人为极短的迁移 ctx（如 1ms）运行 `RunMigrations`，断言返回错误、`schema_migrations` 无新增行、且该迁移的 DDL 未生效（验证连接断开导致服务端中止事务这一路径）。
- **连接超时快速失败**：把 DSN 指向一个不可达端口（如 `127.0.0.1:1`），断言 `Connect` 在约 10 秒内返回错误且不进入迁移阶段。
- **断点续跑**：让迁移在第 k 个中断，重启后断言从第 k 个继续、前 k-1 个不重放。
- 上述均需真实 PostgreSQL，本阶段按边界未执行、未连接。

### 验证结果

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `[Parser]::ParseFile` × 5 个脚本 | — | 全部 SYNTAX OK |
| `Invoke-MigrationFactsTests.ps1` | 0 | 新增 27 项全通过 |
| `Invoke-ScriptSafetyTests.ps1`（全量） | 0 | 原 48 + 保留 54 + 新增 27 = **129 项通过，0 失败** |
| `go fmt ./...` | 0 | 无差异 |
| `go build ./...` | 0 | 通过 |
| `go vet ./...` | 0 | 通过 |
| `go test ./...` | 0 | 全部通过 |
| `go test -count=1`（新增测试强制非缓存） | 0 | 通过；变异后如期 FAIL，还原后 PASS |
| `git diff --check` | 0 | 通过（仅 `.ps1` 的 CRLF 规范化警告，符合 `.gitattributes`） |

- `DATABASE_URL` 全程未设置，需库的集成测试按内置逻辑跳过，**未发生任何数据库连接**。
- 临时目录已清理，仓库无 `.dump` / `.backup` / `.partial` / `.metadata.json` / `.validation.json` 残留，未接触真实备份目录或真实数据库。

## 未完成事项

1. 正式库 `schema_migrations` 只读核对——待获准部署窗口人工执行（`docs/migration-numbering-deployment-check.md`）。
2. 需隔离 PostgreSQL 的迁移集成测试（跳过语义、空库全量、失败回滚）+ 本阶段新增的 3 项超时行为测试设计，均待批准后执行。
3. **`Backup-Postgres.ps1:202` 的 `-not $metadataCheck.isolatedTestBackup` 校验使真实（非演练）备份必然发布失败**——既有逻辑，非本轮引入，未修改，需用户决策。
4. 备份演练本身（真实 `pg_dump`/恢复）未重跑；本轮只做离线校验。

## 安全边界确认

未连接任何数据库；未执行迁移或任何真实 SQL；未读取真实 `.env`、密码、DSN、SMTP 密码、密钥或令牌；未修改真实业务数据或 `schema_migrations`；未重命名/删除历史迁移或历史日志；未操作真实备份；未使用 `-Execute`（默认仍为 DryRun）；未安装软件；未注册 Windows 服务；未修改防火墙/DNS/ACL/证书/注册表/环境变量；未启动公网入口；未使用子代理；未提交、未推送、未强制推送。仅对测试子进程使用 `-ExecutionPolicy Bypass`，未修改系统执行策略。

## 阶段 6：`Backup-Postgres.ps1` 正式备份发布校验缺陷调查

- 开始时间：2026-07-15 15:45（本机时间）
- Git 基线未变：`c45e85c`，未提交、未推送。
- 按指令：先完成只读控制流与设计意图追踪并记录，**再**动代码。

### 6.1 `isolatedTestBackup` 的完整数据流

1. **参数**：`Backup-Postgres.ps1:19` `[switch]$RequireIsolatedSource`，默认不给即为 `$false`。
2. **源库门禁**（`:36-38`）：`if ($RequireIsolatedSource -and $DatabaseName -notmatch '^pjsk_backup_source_test_[a-z0-9_]+$') { Fail }`。注意该门禁**以 `-and` 条件化**——只有开关为真时才限制库名。开关为假时任意合法标识符（含正式库 `pjsk`）都被允许。这本身就是"两种模式"的设计证据。
3. **赋值**（`:185`）：`isolatedTestBackup = [bool]$RequireIsolatedSource`。
   - `-RequireIsolatedSource` 为真 → 写入 `true`；
   - 不传 → 写入 `false`。
4. **回读**（`:197`）：`$metadataCheck = Get-Content $metadataPartialPath -Raw | ConvertFrom-Json`。
5. **校验**（`:198-204`）。

### 6.2 原布尔表达式（逐项）

```powershell
if ($metadataCheck.sourceDatabaseName -ne $DatabaseName -or   # (1)
    $metadataCheck.dumpSha256        -ne $hash          -or   # (2)
    $metadataCheck.dumpSizeBytes     -ne $sizeBytes     -or   # (3)
    $metadataCheck.dumpFormat        -ne 'custom'       -or   # (4)
    -not $metadataCheck.isolatedTestBackup) {                 # (5)
    throw "metadata partial validation failed"
}
```

整体是"**任一项为真即判定失败**"的析取式。逐项含义：

| 项 | 含义 | 判定是否合理 |
| --- | --- | --- |
| (1) | 回读的库名与本次参数不符 → 元数据写错或串号 | 合理 |
| (2) | 回读的 SHA-256 与刚算出的不符 → 序列化/落盘损坏 | 合理 |
| (3) | 回读的字节数与实际不符 | 合理 |
| (4) | 格式不是 `custom` | 合理 |
| (5) | **`isolatedTestBackup` 为假即失败** | **缺陷**：它把"必须是隔离演练备份"当成了不变量，而正式备份该字段本就应为 `false` |

前 4 项都是"回读值 ≠ 期望值"的一致性校验；第 5 项却是"回读值必须为真"的**绝对断言**，与前 4 项的语义不同类。

### 6.3 正式备份为何必然失败

正式备份（不传 `-RequireIsolatedSource`）：`:185` 写入 `isolatedTestBackup = false` → `:202` `-not $false` = `$true` → 析取式为真 → `throw` → 进入 `catch`（`:208-214`）：

1. `Remove-Partial` 删除 `$partialPath` 与 `$metadataPartialPath`；
2. 若 `$dumpPublished` 为真且 `$dumpPath` 存在，则删除刚发布的 dump；
3. `Fail "Could not atomically publish the backup and metadata."` → 退出码 1。

**结论：问题真实存在。** 正式备份 100% 必然失败，且已成功 `pg_dump` 出来的 dump 会被删除，最终一个文件都不留、退出码 1。参数无法绕过——`isolatedTestBackup` 完全由 `$RequireIsolatedSource` 决定，要让 (5) 通过就必须传 `-RequireIsolatedSource`，而那又会触发 `:36-38` 的门禁强制库名为 `pjsk_backup_source_test_*`，正式库 `pjsk` 被拒。**两条路径互斥，正式备份无解**。

### 6.4 失败后各文件状态

| 文件 | 状态 |
| --- | --- |
| `<name>.dump.partial` | 被 `Remove-Partial` 删除 |
| `<name>.metadata.json.partial` | 被 `Remove-Partial` 删除 |
| `<name>.dump`（final） | 校验在 `Move-Item` 之前失败，此时 `$dumpPublished=$false`，final 从未生成 |
| `<name>.metadata.json`（final） | 从未生成 |
| `<name>.validation.json` | 本脚本从不生成（见 6.6） |

**已有的旧备份不会被误删**：`:84-88` 在任何工作之前就检查 final/partial/metadata 四个目标是否已存在，存在即 `Fail "Refusing to overwrite..."`；且 `catch` 中的删除以 `$dumpPublished` 为条件，只会删掉**本次**刚 `Move-Item` 出来的 dump。该失败路径本身是安全的。

### 6.5 设计意图证据（四条独立证据，均指向"支持两种模式"）

1. **文档把正式备份列为首要用法**——`docs/database-backup-restore.md:30-37` 的主备份示例正是：
   ```powershell
   ./scripts/database/Backup-Postgres.ps1 -DatabaseName pjsk -BackupRoot 'D:\PJSK-Backups\PostgreSQL' -Username postgres
   ```
   即 `-DatabaseName pjsk`（正式库）且**不带** `-RequireIsolatedSource`——恰好就是当前必然失败的那条路径。
2. **保留策略的分层逻辑只对正式备份生效**——`_RetentionCommon.ps1:202`：
   ```powershell
   $verified = @($sets | Where-Object { $_.Status -eq 'verified' -and $_.IsolatedTest -ne $true -and $_.CreatedUtc })
   ```
   日/周/月保留分层**只处理非隔离备份**；`:248` 则把隔离备份判为 `Protected`（"isolated restore-drill evidence"）永不删除。若正式备份根本无法产生，整套保留分层就是永远不可能触发的死代码——而它有 54 项测试。
3. **原开发日志明说两种模式**——`docs/development-logs/2026-07-14-database-backup-restore.md:96`："备份脚本的 `DatabaseName` 允许安全 PostgreSQL 标识符，**正式使用时调用方仍必须显式传入 `pjsk`**；隔离演练则只传入本轮唯一 `pjsk_backup_source_test_*`。"
4. **缺陷成因可追溯**——同日志 `:168`："由于正式备份前发现 metadata 字段尚不足，本阶段仅最小补强备份脚本：metadata 增加 `schemaVersion`、`sourceDatabaseName`、`isolatedTestBackup`…"。该字段是在**隔离演练过程中**加入的，当时 `-RequireIsolatedSource` 恒为真（`:153` 记载"启用 `RequireIsolatedSource` 门禁；正式数据库 `pjsk` 被该门禁拒绝"），因此 `-not $false` 这条分支从未被走到。`git log` 确认这些代码全部来自单次提交 `1a89c95 feat: add PostgreSQL backup and restore tooling`，此后未再修改。

**判定**：脚本设计为同时支持正式备份与隔离演练备份，当前 (5) 项校验是缺陷，属正式部署前阻断项，必须修复。

### 6.6 为什么现有 129 项测试全都没抓到

`Backup-Postgres.ps1` 的所有离线用例都带 `-DryRun`，而 `-DryRun` 在 `:104-119` 打印概要后 **`exit 0`**，早于 `:133` 的 `pg_dump` 与 `:196-214` 的发布校验。**整个发布路径的测试覆盖为零**。这正是本轮必须补的洞——而且要在不碰真实 PostgreSQL 的前提下补。

### 6.7 同类布尔逻辑排查结果

在 `scripts/database` 全量检索 `isolatedTestBackup` / `RequireIsolatedSource` / `metadataCheck` / `fixtureExpectedRowCounts` / `Could not atomically publish` / `-not ... -or`：

| 检查项 | 结论 |
| --- | --- |
| 把正式模式误判成演练模式 | **仅 `Backup-Postgres.ps1:202` 一处**（本轮修复） |
| 把演练模式误判成正式模式 | 无 |
| `-or`/`-and` 方向错误 | 除 `:202` 外无；`:36` 的 `-and` 方向正确 |
| 字符串 `"false"` 被当作布尔真 | **存在隐患**：`_RetentionCommon.ps1:115` 用 `[bool]$metaObj.isolatedTestBackup`，若 JSON 写成字符串 `"false"` 则 `[bool]"false"` 在 PowerShell 中为 `$true`。但本仓库只有 `Backup-Postgres.ps1` 产出该字段且写的是真布尔，风险为理论值；且它属保留策略读取侧，按指令"不扩大到保留策略"，本轮**不改**，仅登记。新校验函数会显式拒绝非布尔类型，从产出侧堵住这个入口。 |
| JSON 反序列化类型判断错误 | `$metadataCheck.isolatedTestBackup` 缺失时返回 `$null`，`-not $null` = `$true` → 与"字段为 false"同样失败，无法区分"缺失"与"为假"。新校验显式区分。 |
| 参数默认值与 metadata 不一致 | 修复后二者按定义绑定（metadata 由 `$RequireIsolatedSource` 写入，校验也与之比对） |
| 发布失败删除有效旧备份 | **不会**（见 6.4） |
| partial 清理不完整 | **完整**，`Remove-Partial` 覆盖 dump partial 与 metadata partial |

**附带发现（不修，仅登记）**：`.validation.json` **没有任何生产脚本产出**——只有 `_RetentionCommon.ps1` 读取、`Invoke-RetentionSafetyTests.ps1` 造 fixture。`Test-PostgresBackup.ps1` 只打印 PASS/FAIL，不落盘 validation。因此"备份→校验→保留"链条中，`verified` 状态所依赖的 validation 文件目前需人工产生。这与备份发布安全不直接相关，按指令不在本轮处理。

- 是否连接数据库：否。是否读取敏感信息：否。是否使用子代理：否。本节仅调查，未改代码。

## 阶段 7：最小修复与发布路径离线测试

- 开始时间：2026-07-15 16:05（本机时间）

### 最小修复

1. **新增 `scripts/database/_BackupMetadata.ps1`**（只读共享校验，遵循 `_RetentionCommon.ps1` 约定：只定义函数、无 param 块、可 dot-source）：`Test-BackupMetadataConsistency`，沿用既有 `Test-BackupRootGuard` 的 `[ref]$Reason` + 布尔返回风格。
   - 前 4 项一致性校验（库名、SHA-256、字节数、格式）**语义不变**，全部保留。
   - 第 5 项由"必须为真"改为**"必须与本次运行模式相符"**：`$isolatedProperty.Value -ne $ExpectedIsolatedTestBackup`。
   - 显式区分三种坏情况：字段**缺失**、**非布尔类型**、**模式不符**。这一点必要——`$null` 与字符串 `"false"` 在 PowerShell 中 `[bool]` 转换分别得到 `$false` 与 **`$true`**，直接转型会同时放过"字段缺失"和"字符串 false"。
   - `$Reason` 只含字段级原因，不含哈希、路径、凭据或 DSN。
2. **`Backup-Postgres.ps1`**：dot-source 新文件；发布校验改为调用 `Test-BackupMetadataConsistency`，期望值传 `([bool]$RequireIsolatedSource)`。删除/清理/`.partial`/拒绝覆盖/哈希校验逻辑**一律未动**，安全边界未放宽。
3. **未改** `_RetentionCommon.ps1:115` 的 `[bool]` 转型（按指令不扩大到保留策略；产出侧已堵住非布尔值入口）。

### 正式 / 演练行为矩阵（修复后）

| | 正式备份 | 隔离演练备份 |
| --- | --- | --- |
| 参数 | 不传 `-RequireIsolatedSource` | 传 `-RequireIsolatedSource` |
| 允许库名 | 任意合法标识符（正式用显式传 `pjsk`） | 仅 `pjsk_backup_source_test_*`；`pjsk` 被拒 |
| `isolatedTestBackup` | `false` | `true` |
| 发布校验期望 | `false` | `true` |
| 演练 fixture 行数 | **不写入** | 写入（迁移数量动态） |
| 原子发布 dump + metadata | **允许**（修复前必然失败） | 允许 |
| 哈希校验 / `.partial` / 拒绝覆盖 | 全部保留 | 全部保留 |
| 保留策略 | 参与日/周/月分层 | 永久 `Protected`（演练证据） |

### 新增测试：`Invoke-BackupPublishTests.ps1`（40 项）

关键设计：**用 mock 可执行文件驱动真实入口**，而不是绕开发布路径。用 Windows 自带的 .NET Framework `csc.exe`（非安装软件）把最小 mock `pg_dump.exe` / `pg_restore.exe` 编译到一次性临时目录，再以 `-PostgresBin` 指向它运行**真实的 `Backup-Postgres.ps1`**，因此真实发布路径（ConvertTo-Json → 落盘 → 回读 → 校验 → `Move-Item` 原子发布 → 失败清理）逐行真实执行。**从未调用真实 PostgreSQL 工具，从未连接数据库。**

- 端到端（真实入口）：正式模式发布成功、dump/metadata final 生成、无 `.partial` 残留、metadata 记录 `isolatedTestBackup=false` 且为真布尔、不含 fixture 行数、不被误判为演练证据；演练模式发布成功、`isolatedTestBackup=true`、含 fixture 行数且 `schema_migrations >= 19`（动态非字面量）；两种模式在 metadata 中可区分；演练门禁仍拒绝 `pjsk` 且被拒运行**一个文件都不产生**。
- 校验函数（入口无法注入的损坏场景，但目标是真实发布路径调用的同一函数）：正式模式收 `true` 必须失败、演练模式收 `false` 必须失败、字段缺失必须失败、字符串 `"false"`/`"true"`/整数 `1` 必须以"非布尔"失败、null 必须失败；库名/SHA/字节数三项旧校验仍必须失败。
- 覆盖保护：目标 dump 已存在时拒绝运行且**既有备份哈希不变**；后续换新名成功发布后，**无关的既有备份依然存活**。
- 保密：输出不含 `PGPASSWORD`；published metadata 不含 password/pgpass/DSN 类字段。
- 全部 fixture 位于脚本自建临时目录，`finally` 中清理。

### 变异验证（证明"旧逻辑会失败，新逻辑可区分两种模式"）

把 `_BackupMetadata.ps1` 的模式比较临时改回原缺陷写法 `if (-not $isolatedProperty.Value)`，重跑发布测试：**12 项 FAIL，退出码 1**，其中包括 `FAIL  real production backup (pjsk, no -RequireIsolatedSource) publishes successfully`、`FAIL  real production backup: final dump published`、`FAIL  real mode rejects metadata claiming isolatedTestBackup = true`。这**在离线环境中复现了原缺陷**：正式备份确实无法发布。随后完整还原，`Select-String` 确认 `-not $isolatedProperty.Value` 已不存在，40 项全部恢复 PASS。变异只改本次待提交代码，未触碰删除/清理/路径保护逻辑，未接触数据库或真实备份。

### 附带修正

测试名中误用 `\"` 转义（PowerShell 用反引号而非反斜杠），导致两条用例名输出被截断成 `PASS  the string \` 且彼此无法区分；已改用单引号字符串，现正确显示为 `the string "false" is rejected as not a boolean` / `the string "true" is rejected as not a boolean`。

### 本阶段已有修改的复核

| 复核项 | 结论 |
| --- | --- |
| 动态迁移 helper 未引入真实备份模式依赖 | 通过——`_MigrationFacts.ps1` 只列文件名，与备份模式无关；仅在演练分支被调用 |
| 正式备份 metadata 不再写演练 fixture | 通过（端到端断言） |
| 演练校验仍能取得动态迁移数量与最大文件名 | 通过（`Test-PostgresBackup.ps1` + 27 项测试 + 端到端 `schema_migrations >= 19`） |
| `Backup-Postgres.ps1` 默认仍为安全模式 | 通过——`-DryRun` 行为未变；未加任何默认破坏性行为 |
| `-Execute` 或等价真实清理未放宽 | 通过——本轮未触碰 `Remove-ExpiredPostgresBackups.ps1`，默认仍 DryRun |
| 失败路径不留 `.partial` | 通过（端到端断言） |
| 失败路径不删无关既有备份 | 通过（端到端断言，哈希比对） |
| 日志不打印 DSN/密码/敏感命令行 | 通过——`$Reason` 仅字段级原因；输出无 `PGPASSWORD` |

### 验证结果

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `[Parser]::ParseFile` × 4 | — | 全部 SYNTAX OK |
| `Invoke-BackupPublishTests.ps1` | 0 | **40 项通过** |
| `Invoke-MigrationFactsTests.ps1` | 0 | 27 项通过 |
| `Invoke-ScriptSafetyTests.ps1` 全量 | 0 | **169 项 PASS，0 FAIL**（原 48 + 保留 54 + 迁移 27 + 发布 40） |
| 变异（还原原缺陷逻辑） | 1 | **12 项如期 FAIL**，还原后全通过 |
| `go fmt` / `go build` / `go vet` | 0 / 0 / 0 | 通过 |
| `go test -count=1 ./...` | 0 | 全部通过（非缓存） |
| `git diff --check` | 0 | 通过（仅换行规范化警告，符合 `.gitattributes`） |

- 临时目录已清理；仓库内 `.dump` / `.backup` / `.partial` / `.metadata.json` / `.validation.json` **无残留**；`pjsk-*-tests-*`、`bkretention-tests-*` 均无残留。
- `DATABASE_URL` 全程未设置；**未连接任何数据库**；未执行真实 `pg_dump`；未读取真实备份目录；未读取真实 `.env` 或密钥；未使用子代理；**未提交、未推送**。

### 文档同步

- `docs/database-backup-restore.md`：新增"两种备份模式"小节（参数、允许库名、`isolatedTestBackup`、fixture、保留策略对照表），并说明发布校验要求该字段与模式相符、字段缺失或非布尔一律失败。原文只描述 metadata 字段而未说明模式差异，与实际逻辑不一致。

## 未完成事项（更新）

1. 正式库 `schema_migrations` 只读核对——待获准部署窗口人工执行。
2. 需隔离 PostgreSQL 的迁移集成测试与 3 项超时行为测试设计，待批准后执行。
3. **`.validation.json` 无任何生产脚本产出**——只有 `_RetentionCommon.ps1` 读取、retention 测试造 fixture；`Test-PostgresBackup.ps1` 只打印 PASS/FAIL 不落盘。因此保留策略的 `verified` 状态目前依赖人工产生该文件。与备份发布安全不直接相关，按指令未在本轮处理。
4. `_RetentionCommon.ps1:115` 的 `[bool]$metaObj.isolatedTestBackup` 对字符串 `"false"` 会得到 `$true`（理论隐患，产出侧已堵）。属保留策略读取侧，按指令未扩大处理。
5. 备份演练本身（真实 `pg_dump`/恢复）未重跑。

## 下一阶段入口

等待人工审阅。获准后再复核差异、重跑全部测试、按明确文件列表暂存并提交。
