# PostgreSQL listen_addresses 收敛（2026-07-16：阶段 1 只读调查与实施方案；未做任何修改）

> 安全边界：本轮为纯只读调查。未执行 ALTER SYSTEM/ALTER ROLE/ALTER DATABASE/pg_reload_conf()，未启停或 reload 任何服务，未修改任何配置文件、防火墙、ACL 或业务数据；数据库查询全部在显式 `BEGIN READ ONLY` 事务中执行；未读取业务表内容与 SQL 正文；未读取 `.env`；本文不含密码、连接串、密钥或令牌。未使用子代理。

## 1A. Git 与服务基线（只读，通过）

- Git：`main`，工作区干净（本日志创建前无任何待提交改动）；HEAD 与 `origin/main` 均为 `dcc807930e7c66fdc10e1360c7277706a27e4cc6`。
- 服务：`postgresql-x64-18` / `pjsk-backend` / `pjsk-caddy` 均 Running / Automatic。
- 监听：5432 = `0.0.0.0` 与 `[::]`（PID 9628，进程名 postgres.exe）；8080 = 仅 `127.0.0.1`（PID 10944）；8081 = `[::]`（PID 16128）；5173 无监听。（PID 为本次证据值。）
- HTTP：后端直连 health、Caddy 首页、Caddy 代理 health 均 200。
- 活动网络：WLAN "CMCC-4cXx"，NetworkCategory = Private。

## 1B. PostgreSQL 实际配置来源（只读，已交叉核对）

- 服务命令行（Win32_Service.PathName）：`"D:\PostgreSQL\18\bin\pg_ctl.exe" runservice -N "postgresql-x64-18" -D "D:\PostgreSQL\18\data" -w`——**无 `-h`/`-p`/`-o` 等覆盖项**；运行账户 `NT AUTHORITY\NetworkService`。
- 数据库内核对（READ ONLY）：server_version **18.4**；data_directory `D:/PostgreSQL/18/data`；config_file `D:/PostgreSQL/18/data/postgresql.conf`；hba_file `.../pg_hba.conf`；ident_file `.../pg_ident.conf`；port 5432。
- `pg_settings`：`listen_addresses = '*'`（boot_val=localhost，reset_val=*），source=configuration file，**sourcefile=postgresql.conf，sourceline=60**，pending_restart=f，context=postmaster（修改需完整重启，reload 无效）；`port=5432` 来自同文件第 64 行。
- `pg_file_settings`：仅上述两条相关条目，applied=t，无 error。
- `postgresql.conf` 中与 listen/port/include 相关的行仅两行：第 60 行 `listen_addresses = '*'`、第 64 行 `port = 5432`；**无 include / include_if_exists / include_dir**。
- `postgresql.auto.conf`：无任何非注释行——**不存在 ALTER SYSTEM 覆盖**。
- （数据目录对非管理员会话不可遍历，以上文件内容经 `pg_read_file()` 在 READ ONLY 事务内按行过滤获取，未输出全文。）
- 结论：生效值唯一来源是 `postgresql.conf:60`，修改入口唯一且明确。

## 1C. 后端真实连接方式（只读，未读 .env）

- 证据方法：不读取 `.env`，改用 OS 连接表交叉：`pjsk-backend.exe`（PID 10944）当前与 5432 的既有连接为 **`::1:49686 → ::1:5432`**。
- 结论：后端实际经 **IPv6 回环 `::1`** 连接 PostgreSQL（配置值高概率为 `localhost`，本机解析优先 `::1`；未读取原值）；port=5432；database 已配置（health 显示 database=connected）；密码已设置但未读取、未输出；未使用 Unix socket。
- 其他依赖排查：无包含 pjsk/postgres/pg 的计划任务；除后端外本机当前无其他进程与 5432 建立连接（见 1D）。

## 1D. 当前数据库连接来源统计（READ ONLY，排除本调查会话，未读 SQL 正文）

| client_addr | backend_type | state | 数量 |
|---|---|---|---|
| ::1/128 | client backend | idle | 1（即 pjsk-backend 连接池） |
| <local> | autovacuum launcher / background writer / checkpointer / io worker ×3 / logical replication launcher / walwriter | — | 8（全部为内部后台进程） |

- 非回环地址连接数：**0**；无 `192.168.*`/`172.*` 或其他远程连接。
- 存在 `::1` 连接：**是**（正是 pjsk-backend——对目标值选择有决定性影响）。
- 复制/备库连接（walsender）：0；无监控类连接。
- 结论：数据库客户端只有 pjsk-backend（经 ::1）与本地管理工具（psql，调查会话自身已排除）。

## 1E. pg_hba.conf 有效规则（只读，经 pg_hba_file_rules 视图，未粘贴文件）

| 行号 | type | database | user | address | auth |
|---|---|---|---|---|---|
| 113 | local | all | all | — | scram-sha-256 |
| 115 | host | all | all | 127.0.0.1/32 | scram-sha-256 |
| 117 | host | all | all | ::1/128 | scram-sha-256 |
| 120–122 | local/host | replication | all | —/127.0.0.1/32/::1/128 | scram-sha-256 |

- **不存在** `0.0.0.0/0`、`::/0`、`trust`、或任何允许局域网地址的规则；视图无 error 行。
- 语义区分：`listen_addresses` 决定监听接口；`pg_hba.conf` 决定到达后的认证。当前 hba 已严格（远程 TCP 会因无匹配规则被拒），但既无远程访问需求，仍应收敛监听接口，把暴露面消灭在 TCP 层。本阶段不修改 pg_hba.conf，也未发现必须修改的问题。

## 1F. Windows 防火墙与实际端口暴露（只读）

- 非管理员会话读取防火墙规则（CIM）被拒；`netsh` 枚举已启用入站规则中**无任何含 5432 的规则**。
- **重大既有发现：Windows 防火墙三个 profile（Domain/Private/Public）State 全部为 OFF**（默认策略 BlockInbound,AllowOutbound 仅在开启时生效）。即当前 5432 在 TCP 层对局域网实际可达，仅靠 pg_hba 在认证层拒绝；此前创建的 "PJSK Caddy HTTP LAN" 规则在防火墙关闭期间实际不起过滤作用。
- 此为**本轮之前已存在的独立风险**，与 Caddy/本调查无关；是否开启防火墙属独立决策，本轮不改动（建议管理员会话复核并另立任务评估）。
- 5432 监听 PID 9628 确属 postgres.exe（PostgreSQL bin 目录 `D:\PostgreSQL\18\bin\`）。

## 1G. 实施方案（唯一推荐；本轮不执行）

**推荐目标值：`listen_addresses = '127.0.0.1, ::1'`**

- 不推荐仅 `'127.0.0.1'`：1C/1D 证实 pjsk-backend 实际经 `::1` 连接，仅留 IPv4 回环会立刻断开后端现行连接路径（Go/pgx 或可回退到 IPv4，但属未验证的行为变更，且每次建连多一次失败尝试）；`'127.0.0.1, ::1'` 同样消灭全部非回环监听（目标达成），对现有链路零行为变更。
- 满足推荐前提：无任何合法远程数据库客户端（1D=0）；无复制/监控/备库依赖（1D）；无服务启动参数覆盖（1B）；`::1` 依赖真实存在（1C）故必须保留 IPv6 回环。

实施要点（待批准后由管理员执行）：

1. 生效配置文件：`D:\PostgreSQL\18\data\postgresql.conf`（唯一入口，第 60 行）。
2. 当前值 `'*'`，来源 postgresql.conf:60，pending_restart=f。
3. 新值：`listen_addresses = '127.0.0.1, ::1'`（仅改这一行；文件为 CRLF，编辑时保持）。
4. 唯一修改入口即上述文件；无 auto.conf/include/启动参数需要处理。
5. 不采用 ALTER SYSTEM 的原因：它写入 postgresql.auto.conf 并静默覆盖 postgresql.conf，使同一参数出现双来源、日后审计易被误读；且本任务边界明确禁止。直接编辑唯一来源文件 + 备份 + 哈希，可审计可回滚。
6. 备份：修改前复制到 `D:\PJSK-Archive\postgres\postgresql.conf.bak-<yyyyMMdd-HHmmss>`（目录仓库外；数据目录内不留副本，避免混淆）。
7. 记录修改前后文件 SHA-256。
8. 语法/配置检查：改后先 `D:\PostgreSQL\18\bin\postgres.exe -D D:\PostgreSQL\18\data -C listen_addresses`（只读取配置并打印，不启动实例）确认输出为新值；重启后再查 `pg_file_settings.error` 应为空、`pg_settings.listen_addresses` 为新值。
9. 受控重启顺序（`listen_addresses` context=postmaster，必须完整重启；服务依赖链 pjsk-caddy→pjsk-backend→postgresql-x64-18，不用 -Force 级联）：`Stop-Service pjsk-caddy` → `Stop-Service pjsk-backend` → `Restart-Service postgresql-x64-18` → `Start-Service pjsk-backend` → `Start-Service pjsk-caddy`。
10. 预计中断：整链约 30–90 秒（PostgreSQL 本体重启通常 <10 秒）。
11. 重启后监听验证：`Get-NetTCPConnection -LocalPort 5432 -State Listen` 应仅剩 `127.0.0.1` 与 `::1`，不再有 `0.0.0.0`/`[::]`。
12. 健康验证：后端直连 health 200 且 database=connected；Caddy 首页 200；Caddy 代理 health 200；确认后端连接池恢复（pg_stat_activity 重新出现 ::1 client backend）。
13. 数据库完整性复核（READ ONLY）：schema_migrations 仍为 19 条 / 最高 `0019_admin_auth_audit_events.sql` / `0019` 恰 1 条。
14. 失败回滚：还原备份文件（比对 SHA-256 与修改前一致）→ 按第 9 步顺序再次重启三服务。
15. 回滚后重复第 11–13 步验证（监听恢复 `0.0.0.0`/`[::]` 属回滚预期）。
16. 文档：实施结果追加本日志；更新 HANDOVER 的 5432 风险条目；Git 仅提交文档，普通推送。

- 残余风险与说明：收敛后本机仍可经回环访问，安全性不下降；若未来需要远程数据库客户端，须同时改 listen_addresses 与 pg_hba 并另行评估；防火墙全关（1F）为独立风险，不与本任务混合处理。

## 1H. 阶段 1 结束

- 本阶段未做任何修改；等待用户批准后进入实施阶段（管理员执行：备份、编辑 postgresql.conf:60、`postgres -C` 校验、按序重启三服务、验证与回滚预案）。
- 新增文件仅本日志；`git diff --check` 通过；工作区除本日志外干净。

## 阶段 2A：实施前门禁复核（2026-07-16 15:08 +08:00，未通过，停止实施）

- 执行范围：仅进行了 Git、当前身份、服务、TCP 监听、HTTP 健康和配置文件可访问性的只读检查；未修改 PostgreSQL 配置，未启停或重启任何服务，未连接数据库执行 SQL，未修改防火墙、`pg_hba.conf`、角色、权限、密码或业务数据，未读取 `.env`、pgpass 内容或任何秘密；未使用子代理。
- Git 基线：当前目录 `D:\pjsk`，分支 `main`；`HEAD` 与 `origin/main` 均为 `dcc807930e7c66fdc10e1360c7277706a27e4cc6`，与阶段 1 基线一致；工作区只有预期的未跟踪日志 `docs/development-logs/2026-07-16-postgresql-listen-address-hardening.md`，未发现未知代码改动。
- 当前会话身份：`SUGUI\苏归`；Windows 管理员角色检查结果为 **False**。这命中阶段 2A 的硬停止条件，因此本轮不得进入阶段 2B、2C、2D，也不得尝试提权绕过。
- 服务（门禁检查时）：`postgresql-x64-18`、`pjsk-backend`、`pjsk-caddy` 均为 Running / Auto。
- 监听（门禁检查时）：5432 仍为 `0.0.0.0:5432` 与 `[::]:5432`，监听 PID 9628；8080 仍仅为 `127.0.0.1:8080`，监听 PID 10944。
- 后端数据库连接（OS TCP 表）：`pjsk-backend.exe` 对应进程 PID 10944 仍通过 `::1` 连接 `::1:5432`；本次已列出的 5432 Established 连接未出现非回环客户端。
- HTTP（门禁检查时）：后端直连 health = 200 / database=connected；Caddy 首页 = 200；Caddy 代理 health = 200 / database=connected。
- 配置文件复核：非管理员会话读取 `D:\PostgreSQL\18\data\postgresql.conf`、`postgresql.auto.conf` 及执行 `postgres.exe -D ... -C listen_addresses` 均因数据目录访问被拒而未完成。本轮没有用读取秘密、改变 ACL 或其他越界方式绕过。阶段 1 已记录的 `postgresql.conf:60`、当前值 `*`、无 auto.conf/include 覆盖等事实因此未能在本会话重新确认，不能据此放行实施。
- 门禁结论：**未通过**。直接失败点是当前 Codex/PowerShell 会话不具管理员权限；配置文件当前状态也因同一权限边界无法复核。依照批准方案的停止规则，本轮没有生成仓库外管理员脚本、没有创建配置备份或运行日志、没有修改 `listen_addresses`、没有执行服务操作、没有进入验收或 Git 提交/推送。
- 恢复入口：以管理员身份启动新的 Codex/PowerShell 会话后，从阶段 2A 全量重跑门禁；不得直接从阶段 2B 或 `-Apply` 继续。

## 阶段 3A：`pg_file_settings` 误回滚只读调查（2026-07-16 16:xx +08:00）

- 只读确认：仓库外脚本 `D:\PJSK-Archive\postgres\Set-PostgresLoopbackListen.ps1` 当前 SHA-256 为 `DFBB6C12BB9C97AC5924F262FF320EDFF4889F2C9E2DCE30C48DEFA4767FB4D2`；正式配置已由脚本回滚为 `373DC572006C816584CC7072EF08B700E22C93E14746F6E7154DA5B5328B03B5`，有效行仍为第 60 行 `listen_addresses = '*'`；三项服务均为 Running / Automatic。
- 本次 `-Apply` 日志 `D:\PJSK-Archive\postgres\Set-PostgresLoopbackListen-20260716-161927.log` 显示：内存演练、原子替换自测、备份、临时候选 reread 校验均通过，正式配置一度替换到预测 SHA-256 `72926D40F67E0B97CE84507D8177DED50465B0CDEC8531BF9337F9B031BECA99`，随后在重启前 `pg_file_settings` 校验处失败并成功回滚；失败前尚未进入 PostgreSQL / backend / Caddy 服务操作。
- 脚本当前 `Assert-FileSettingsBeforeRestart` 的 SQL 查询 `pg_file_settings` 中 `listen_addresses` 的 `sourcefile/sourceline/seqno/name/setting/applied/error`，但最终只输出聚合字段；判断条件将 `error_count != 0`、`listen_count != 1`、目标值不一致、sourcefile 不一致、`sourceline <= 0` 或 `listen_applied = false`、auto.conf 覆盖等统一抛为失败。
- 可诊断性缺陷：失败日志仅记录 `FAILURE: pg_file_settings reports one or more configuration errors.`，没有保存失败时的 `sourcefile/sourceline/seqno/name/setting/applied/error` 原始字段，因此无法从日志直接区分文件级语法错误、非目标参数错误、重复覆盖，还是 `listen_addresses` 作为 `postmaster` 参数等待重启。
- 结论：需要双层校验。正式替换前用 PostgreSQL 原生离线解析候选配置；正式替换后继续读取 `pg_file_settings`，但必须把 `listen_addresses`、`context=postmaster`、离线解析已确认合法的待重启状态与真正配置错误分开处理，并记录原始元数据。

## 阶段 3B：原生离线解析可用性探针（2026-07-16 16:xx +08:00，停止）

- 只读数据库核对：通过显式 `BEGIN READ ONLY` 查询 `pg_settings`，当前 `listen_addresses` 仍为 `*`，`context=postmaster`，`sourcefile=D:/PostgreSQL/18/data/postgresql.conf`，`sourceline=60`，`pending_restart=f`。
- 按阶段要求对 PostgreSQL 原生命令做了一次不接触正式配置和服务的临时目录探针：临时配置仅包含合法候选值 `listen_addresses = '127.0.0.1, ::1'`，命令形态为 `postgres.exe -D <临时目录> -C listen_addresses -c config_file=<临时配置>`；命令未启动数据库实例，退出码为 1。
- 探针结果：PostgreSQL 18 在当前 Windows 令牌下直接拒绝运行 `postgres.exe`，错误为 administrative permissions 不允许执行 PostgreSQL。该拒绝发生在解析候选配置之前，说明本环境不能安全依赖提升/管理员令牌直接执行 `postgres -C` 完成原生离线解析。
- 按用户阶段二规则，若该调用在当前 Windows/PostgreSQL 18 环境无法安全使用，必须停止并报告原因；本轮没有尝试创建临时服务、计划任务、降权绕过、关闭 PostgreSQL 安全检查，或启动第二个 PostgreSQL 实例。
- 因该硬停止条件，尚未修改仓库外实施脚本；也未执行新的计划模式或 `-Apply`，未修改 PostgreSQL 配置、服务或数据库内容。

## 阶段 3C：废止 `postgres.exe -C` 路径后的专用脚本修正（2026-07-16 16:55 +08:00）

- 方案修正：阶段 1 中原计划使用管理员令牌直接运行 `postgres.exe -D ... -C listen_addresses`，已被阶段 3B 的实证结果否定；当前 Windows/PostgreSQL 18 环境下，该命令在解析候选配置前即因管理员权限被拒绝。本轮未再次运行 `postgres.exe -C` 探针，也未尝试计划任务、临时服务、降权绕过、第二实例或修改 PostgreSQL 管理员限制。
- 专用边界：仓库外脚本仍只服务固定变更 `listen_addresses = '*'` → `listen_addresses = '127.0.0.1, ::1'`；未增加传入其他地址、主机名、参数名、配置值或配置文件路径的能力，不把脚本推广为通用 PostgreSQL 配置编辑器。
- 新增本地结构校验：脚本新增固定目标校验逻辑，要求目标值按逗号拆分后精确为 `127.0.0.1` 和 `::1`，通过 `IPAddress.TryParse()` 与 `IPAddress.IsLoopback()`，且分别为 IPv4 `InterNetwork` 与 IPv6 `InterNetworkV6`；拒绝 `*`、`0.0.0.0`、`::`、DNS 主机名、IPv6 scope ID、空条目、重复条目和第三个地址。该校验仅适用于本专用回环目标，不作为任意 PostgreSQL 参数值的通用解析替代。
- 重启前 `pg_file_settings` 分类：脚本不再以 `error_count != 0` 作为全局直接失败条件，也不再要求目标行重启前必须 `applied=true`；改为记录并分类原始 `sourcefile/sourceline/seqno/name/setting/applied/error`，同时读取 `pg_settings.listen_addresses` 的 `name/setting/context/source/sourcefile/sourceline/pending_restart`。文件级错误、非目标错误、来源不一致、auto.conf 覆盖、include 重复、后续 seqno 覆盖、目标值或目标行不一致仍失败；只有固定目标、唯一来源、目标行号匹配且 `context=postmaster` 全部通过时，才允许将目标行的 `applied=false` 或目标行 error 视为“等待计划内服务重启应用”的受限例外。
- 重启后决定性验证：脚本未来在 `-Apply` 重启后会要求 `pg_settings.setting='127.0.0.1, ::1'`、`context=postmaster`、正式 `sourcefile`、目标 `sourceline`、`pending_restart=false`，且 `pg_file_settings` 目标行 `setting` 正确、`applied=true`、`error IS NULL`；同时保留 5432 仅监听 `127.0.0.1`/`::1`、后端经 `::1` 连接、三项 HTTP 200、迁移完整性 `19 / 0019 / 1` 等验收与失败回滚逻辑。
- 测试结果：PowerShell 语法解析通过；静态检查未发现 `postgres.exe -D`、`-C listen_addresses`、`pg_reload_conf`、`pg_ctl reload`、`error_count`、`listen_applied`、`Set-Content` 或 `Get-Content | Set-Content`；新增 `-ValidationLogicSelfTest` 在正式脚本路径通过 `23 / 23`；`-AtomicReplaceSelfTest` 通过并确认 `[NullString]::Value` 路径仍存在。
- 安全边界：本轮未执行新的正式 `-Apply`，未修改正式 `postgresql.conf`，未 stop/start/restart/reload 任一服务，未修改数据库、未调用 `pg_reload_conf()`、未改防火墙，未创建计划任务或临时 Windows 服务，未尝试降权运行 PostgreSQL，未使用子代理。
- 当前脚本 SHA-256：`FB2A1F70C0206F48BF7FFCFD7B4F521DCF3F5364A43D933DF927101B6CF2ED57`。防火墙三个 profile 全部关闭仍是独立待办，本轮不混入处理。

## 阶段 4：正式实施与最终验收（2026-07-16 17:06 +08:00，成功）

- 执行结论：管理员 PowerShell 中计划模式、自测和正式应用均通过；正式应用退出码 `APPLY_EXIT_CODE=0`；最终脚本输出 `SUCCESS: PostgreSQL configuration, listeners, backend IPv6 connection, services, HTTP endpoints, and READ ONLY database integrity checks all passed.`。
- 最终脚本 SHA-256：`FB2A1F70C0206F48BF7FFCFD7B4F521DCF3F5364A43D933DF927101B6CF2ED57`。
- 配置哈希：修改前 `373DC572006C816584CC7072EF08B700E22C93E14746F6E7154DA5B5328B03B5`；修改后 `72926D40F67E0B97CE84507D8177DED50465B0CDEC8531BF9337F9B031BECA99`。
- 正式备份保留在仓库外：`D:\PJSK-Archive\postgres\postgresql.conf.bak-20260716-170604`；执行日志为 `D:\PJSK-Archive\postgres\Set-PostgresLoopbackListen-20260716-170604.log`。
- 最终有效配置：`postgresql.conf:60 listen_addresses = '127.0.0.1, ::1'`。
- 服务状态：`postgresql-x64-18`、`pjsk-backend`、`pjsk-caddy` 均为 Running / Automatic。
- 最终监听：PostgreSQL 5432 仅监听 `127.0.0.1` 与 `::1`；不再监听 `0.0.0.0` 或 `::`。
- 后端数据库连接：`pjsk-backend` 已验证继续通过 IPv6 回环连接数据库，路径为 `::1 -> ::1:5432`。
- HTTP 验收：后端直连 health 为 HTTP 200，`status=ok` 且 `database=connected`；Caddy 首页为 HTTP 200；Caddy 代理 health 为 HTTP 200。
- PostgreSQL 重启后 READ ONLY 验收：`listen_addresses='127.0.0.1, ::1'`；`context=postmaster`；`pending_restart=false`；`pg_file_settings.applied=true`；`pg_file_settings.error IS NULL`。
- 数据库完整性 READ ONLY 验收：`schema_migrations` 共 19 条；最高版本 `0019_admin_auth_audit_events.sql`；`0019` 恰好 1 条。
- 回滚状态：无回滚发生。
- 未变更范围：未修改 `pg_hba.conf`、数据库角色、防火墙或业务数据；未读取或提交 pgpass、密码、连接串、密钥、令牌、数据库 dump 或验证产物。
- 子代理：未使用。
