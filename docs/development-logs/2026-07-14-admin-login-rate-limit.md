# 管理员登录防暴力破解与交接文档修正

## 阶段 1：管理员登录现状调查

### 基线

- 分支 `main`；`HEAD` 与 `origin/main` 均为 `8dc21e1b587c7c69214ace268c114e3bdb3eadf0`（`feat: harden internal deployment security`）；工作区干净、暂存区为空。
- 只读调查 `backend/internal/admin`（handler/repository/middleware/handler_test）、`api/router.go`、`query/ratelimit.go`、`clientip` 包与审计表使用位置；未修改任何文件。

### 调查结论表

| 调查项 | 当前实现 | 风险 | 本轮是否处理 |
| --- | --- | --- | --- |
| 登录接口 | `POST /api/admin/login`，JSON `username`/`password`，body 1MB 上限、拒绝未知字段 | — | 不改字段 |
| 响应状态 | 成功 200 + HttpOnly Cookie；失败统一 401 `invalid username or password`；格式错 400；DB 错 500 | — | 保持不变 |
| 用户名不存在 vs 密码错误 | 统一 401 同一文案（含 disabled 账户） | 无枚举差异 | 保持 |
| 时序防护 | 用户名不存在时对 dummy bcrypt 哈希比较 | 已防明显时序枚举 | 保持 |
| 限流 | **完全没有**：无每 IP 频率限制、无失败封禁 | 可无限暴力破解管理员密码 | **本轮核心** |
| 账户锁定 | `admins.status` 有 active/disabled，但无失败计数或锁定字段 | 无数据库级锁定 | 不新增迁移，用进程内限流 |
| 登录审计 | 无成功/失败审计写入（`account_security_audit_logs` 目前只被 query/users 流程使用） | 无登录事件追溯 | 仅记录为后续事项（见阶段 2） |
| 登录日志 | 仅 DB 错误经 `logsafe.Category` 输出；不记录用户名、密码或底层错误 | 无泄露 | 保持 |
| 客户端 IP | 管理员登录完全不读 IP，未接入 `clientip` Resolver | 反向代理语义缺失（因为没有限流所以尚未暴露） | 本轮接入统一 Resolver |
| router 装配 | `admin.NewHandler(store, cfg.AdminSessionTTL, cfg.CookieSecure)`；router 已有一个 `clientip.Resolver` 注入 query | — | 复用同一 Resolver 注入 admin |
| 测试构造 | `fakeStore` 实现 `admin.Store` 接口，bcrypt.MinCost 测试哈希，`handler.now`/`handler.random` 可注入 | — | 复用该模式 |
| 用户名匹配语义 | 数据库 `lower(btrim(username)) = lower($1)`，handler 先 TrimSpace | — | 限流键 = TrimSpace 后 ToLower，与登录语义一致 |
| 进程内限流可行性 | 单进程部署，无需数据库表 | 重启清空限流（可接受：目标是减缓暴力破解，不是永久锁定）；多实例部署时各实例独立计数（当前无多实例计划，记录为限制） | 采用进程内 |
| query `loginLimiter` 增长边界 | 每次 `allow()` 都执行全量过期清理，键存活期不超过各自窗口；但**窗口内**分布式 IP 喷洒仍可短时膨胀 map，无键数上限 | 内存压力（query 侧现状风险低：窗口短 + 单机） | admin 限流器加入键数上限、fail-closed；query 现状记录为后续待办，不借本轮改 query |

### 与设计假设的核对

- 指令假设与源码一致，无冲突：管理员登录确实零限流、未接 IP、响应已统一、dummy bcrypt 已存在。
- 唯一需要的方案取舍：query limiter 可整体提取为通用包，但会牵动 query 包文件与既有测试；按"改动范围小"原则，admin 包内实现独立最小 limiter（复用同一模式、参数一致），不做提取重构。

### Git 状态

- 本阶段结束时仅新增本日志文件；`git diff --check` 通过，暂存区为空，无删除、无重命名。

### 下一阶段

- 阶段 2：限流设计定稿（维度、阈值、键规范化、429 响应文案、Retry-After 取舍、审计取舍）。

## 阶段 2：限流安全设计

### 维度与阈值（与普通查询登录保持一致）

- 每客户端 IP：每分钟最多 20 次管理员登录尝试（无论成败），超过返回 429。
- IP + 规范化用户名：10 分钟窗口内最多 5 次失败，达到后该组合封禁 10 分钟；成功登录清除该组合的失败状态。
- 目标是减缓暴力破解，不做永久账户锁定；进程重启清空限流为已接受的取舍。

### 实现位置

- admin 包内独立最小 limiter（`backend/internal/admin/ratelimit.go`），复用 query `loginLimiter` 的成熟模式与参数；不提取通用包、不让 admin 依赖 query、不改 query 阈值——理由：提取会牵动 query 文件与既有测试，违背"改动范围小"。
- 与 query 版本的差异：新增键数上限（每张表 10000 键，管理员登录流量远低于此）；达到上限且请求键不在表中时**拒绝（fail-closed）**，防止分布式 IP 喷洒造成无界内存增长。每次 `allow()` 全量懒清理过期键（同 query 模式），不启动后台 goroutine。

### 用户名规范化

- 限流键 = `strings.ToLower(strings.TrimSpace(username))`，与数据库查询 `lower(btrim(username)) = lower($1)` 语义一致；不改变登录匹配规则；密码不进入任何键；不在日志输出登录输入。

### 客户端 IP

- 接入既有 `clientip` Resolver：Handler 新增 resolver 函数注入（默认零可信代理），router 将已创建的同一个 Resolver 同时注入 query 与 admin；不自行实现第二套解析、不直接读 XFF；非法 RemoteAddr 进入 `unknown-client` 共享桶。

### 公开响应

- 429 统一返回 `too many login attempts, please try again later`（英文与 admin 包既有响应风格一致），不含用户名是否存在、失败次数、封禁截止时间、IP 或内部键。
- 401 文案与现状完全一致；封禁期内即使密码正确也返回 429。
- 不设置 `Retry-After`：普通查询登录现有 429 也不设置，保持一致，避免暴露内部窗口信息。

### 失败计数边界

- 用户名不存在、账户非 active、密码错误均计一次失败（三者公开响应本就统一）。
- 数据库错误（500 路径）不计失败，不影响封禁状态。

### 日志与审计

- 不为 401/429 增加日志（现状不记录普通认证失败，避免高频日志）；DB 错误日志沿用 `logsafe.Category`。
- 不接入 `account_security_audit_logs`：管理员登录当前无审计路径，接入需给 admin store 增加审计写入并让匿名失败可触发数据库写（写放大/DoS 面），超出本轮进程内限流范围——记录为后续事项。

### 不修改清单确认

密码哈希、会话令牌算法、Cookie 属性、会话 TTL、admins 表、登录请求字段、登录成功响应、前端管理员界面、query 限流阈值、数据库迁移——全部保持不变。

## 阶段 3：实现

- 新增 `backend/internal/admin/ratelimit.go`：`loginLimiter`（mutex 并发安全；每 IP 1 分钟 20 次；IP+用户名 10 分钟 5 次失败封 10 分钟；每次 `allow()` 全量懒清理；`maxTrackedKeys=10000`，两张 map 满时新键 fail-closed 拒绝，`recordFailure` 满时不增长；无后台 goroutine）。`normalizeLimiterUsername` = TrimSpace + ToLower，与数据库 `lower(btrim())` 一致。
- `handler.go`：Handler 新增 `limiter` 与 `resolveClientIP`（默认零可信代理 `clientip.Resolver`）、`ConfigureClientIPResolver`；Login 在字段校验后、任何数据库操作前调用 `allow()`，超限返回 429 `too many login attempts, please try again later`；用户名不存在/账户非 active/密码错误统一 `recordFailure`；成功 `recordSuccess`；DB 错误保持 500 且不计失败。时间来自既有可注入 `h.now`。
- `router.go`：把已有的单个 `clientip.Resolver` 包装为共享 `resolveClientIP` 函数，同时注入 admin 和 query Handler（各自 limiter 状态独立）。
- 未修改：密码哈希、会话令牌、Cookie、TTL、请求字段、成功响应、admins 表、迁移、query 限流、前端。

## 阶段 4：测试

- 新增 `admin/ratelimit_test.go`（7 个用例）：每 IP 窗口上限/隔离/窗口重置；失败第 4 次不封、第 5 次封、跨用户名/跨 IP 隔离、封禁到期解除；成功只清自己组合；懒清理删除过期键；键数上限 fail-closed 且失败表不超限增长；100 并发 `allow` 恰好放行 20 次 + 并发失败/成功不 panic、只封被滥用组合；用户名规范化。
- 新增 `admin/login_ratelimit_test.go`（11 个用例）：失败封禁后正确密码也 429、封禁期满恢复；不存在用户名与错误密码同文案同封禁规则；大小写/空白变体共享限流键；成功清除仅自身组合；每 IP 20 次上限、其他 IP 不受影响、窗口重置；429 响应与日志不含用户名/密码/IP/内部状态、无 Retry-After（与 query 登录一致）；DB 错误 500 不计失败且日志只有分类；非法 RemoteAddr 共享 `unknown-client` 桶；默认忽略伪造 XFF；可信 loopback 代理下转发客户端互不影响；成功登录 Cookie 属性与会话创建不变。既有 4 个管理员测试全部保持通过。
- 新增 `api/admin_login_ratelimit_routes_test.go`（3 个用例）：router 注入验证——默认忽略伪造 XFF 且 admin/query limiter 状态互不串扰；可信代理下转发客户端分离；端到端（见阶段 5）。
- `go fmt/build/vet` 通过；`go test -v ./internal/admin` 22 个测试全 PASS。
- **race 测试**：`go test -race ./internal/admin` 以 `exit status 0xc0000139`（STATUS_ENTRYPOINT_NOT_FOUND）失败。判定为环境问题而非代码 data race，证据：对本批次未触及、测试无并发的 `./internal/logsafe` 运行 `-race` 得到完全相同的启动期错误；真实 data race 会输出 `WARNING: DATA RACE` 与栈信息而非进程入口点错误。本机为 go1.26.5 + MinGW-W64 gcc 8.1.0，race 运行时 DLL 初始化失败。并发正确性由无 race 模式下的确定性并发测试（100 并发恰好放行 20 次）与全 mutex 保护设计保证；在支持 race 的环境重跑列为后续事项。

## 阶段 5：人工接口验证

- 通过 `router.ServeHTTP`（httptest）+ 项目既有隔离测试库机制完成，未启动真实 HTTP 服务、未占用 8080、未读取真实 `.env`（测试基建自身按既有机制加载连接配置）。
- `TestRouterAdminLoginRateLimitEndToEnd`：用带唯一时间戳前缀的临时测试管理员（既有 `loginAPITestAdmin`/`cleanupAPITestAdmin` 机制，t.Cleanup 删除账户与会话）验证——正确登录成功；4 次错误密码后正确密码仍成功（失败被清除）；再 5 次失败后正确密码 429；429 响应不含用户名/IP/内部状态；封禁期内其他 IP 正常登录。
- 不存在用户名一致性、不同 IP 隔离、窗口推进恢复均由单元测试注入 clock 完成，未真实等待。

## 阶段 6：HANDOVER.md 修正

- §5：补充实际已读取的 `APP_ENV`、`SERVER_HOST`、`TRUSTED_PROXY_CIDRS`、`CORS_ALLOWED_ORIGINS` 及密钥变量文档指引。
- §6：迁移范围 `0001…0012` 更正为 `0001…0018`；历史双 `0005` 说明保留。
- §7：补充已完成且可从代码/Git/日志证明的能力——导出、查询账户管理/绑定码/CN 合并、找回邮箱与查询码邮箱找回（独立 HMAC）、SMTP/fake 隔离与 `emailDeliveryEnabled`、内网安全加固（提交 `8dc21e1b…`：可信代理、SERVER_HOST、CORS、日志脱敏）、本轮管理员登录限流。
- §15：第 1 条更正为迁移编号已自 `0013` 连续至 `0018`、仅余历史 `0005` 对是否清理的独立决定；第 2 条数据导出标记为已完成。
- 未宣称任何未完成事项（Nginx/Caddy、真实 SMTP、内网 HTTPS、服务化、恢复演练均未声称完成）；未写入真实密码、密钥、域名或 IP。

## 阶段 7–9：完整验证、敏感检查与提交前复核

- `go fmt ./...`、`go build ./...`、`go vet ./...`、`go test -count=1 ./...` 全部通过（15 个含测试包全 ok）。
- 定向 verbose：admin 22、clientip 16、config 27、logsafe 6、api 15、querycoderecovery 12、recoveryemailverification 18 全 PASS，0 FAIL；query 定向 27 个 PASS。
- `go test -race ./internal/admin` 因本机工具链（go1.26.5 + MinGW 8.1.0，`0xc0000139` 进程入口点错误）无法运行，经无关无并发包对照确认为环境问题而非代码 data race（详见阶段 4）。
- `pnpm run build` 通过（本批次未改前端，仅回归确认）。
- 数据库集成测试沿用既有隔离机制，本批次端到端测试的临时管理员及其会话在 `t.Cleanup` 中删除。
- 8080 与 5173 未监听；无残留测试进程；`go.mod`/`go.sum` 零变化；无新增迁移；无删除、无重命名；`git diff --check` 通过；暂存区为空。
- 本批次共 8 个文件：修改 `HANDOVER.md`（交接修正）、`backend/internal/admin/handler.go`（限流接入）、`backend/internal/api/router.go`（共享 resolver 注入）；新增 `backend/internal/admin/ratelimit.go`（limiter）、`ratelimit_test.go` 与 `login_ratelimit_test.go`（测试）、`backend/internal/api/admin_login_ratelimit_routes_test.go`（router 注入与端到端测试）、本日志。无范围外文件。
- 敏感检查：8 个文件内密码/IP/主机均为显式测试值（`correct-password`、`SecretPass-123`、`SENSITIVE-DB-DETAIL`、RFC 5737 地址、loopback），无真实凭据、DSN、邮箱、密钥、证书、域名或局域网 IP；HANDOVER 新增内容仅含公开提交哈希与代码事实。
- 未读取真实 `.env`，未处理 `.claude/settings.local.json`，未连接 SMTP，未修改真实业务数据（含真实管理员），未使用子代理。
- 下一步：按明确文件清单暂存并创建提交 `feat: add admin login rate limiting`，普通推送。
