# 查询登录限流器键数量上限与过期清理

## 阶段 1：只读调查

### 基线

- 分支 `main`；`HEAD` = `origin/main` = `16a745736b5796d6c1122802ff352e168b68cc45`；工作区、暂存区干净。
- 只读检查了 `AGENTS.md`、`HANDOVER.md`、`internal/query/ratelimit.go`、`internal/query/handler.go`/`bindcode.go`/`query_code_recovery.go`（调用点）、`internal/admin/ratelimit.go`、`internal/clientip`、既有 query/admin 测试、开发日志。未连接数据库、未读取真实 `.env`/密钥。

### 调查结论

1. **限流键组成**：查询限流器有两张 map——`attempts` 按**客户端 IP**（每 IP 20 次/分钟的请求频率）；`failures` 按 **IP+CN**（`ip|cn`，5 次失败/10 分钟 → 封锁 10 分钟）。CN 经 `normalizeCN` 规范化后传入；IP 来自统一 `clientip` 解析器（`resolveClientIP`）。修改查询码用 `ip|change:<userID>`、绑定码用 `ip|bind:<cn>` 复用同一 `failures` map。
2. **数据结构**：`map[string]*windowCounter`（attempts）与 `map[string]*failureState`（failures），单 `sync.Mutex` 保护。
3. **键字段**：`windowCounter{windowStart, count}`；`failureState{windowStart, count, blockedUntil}`。**当前没有 lastSeenAt / expiresAt 字段。**
4. **键删除时机**：仅 `cleanupLocked(now)`（每次 `allow()` 调用时全量遍历两张 map）——attempts 键 `windowStart` 超过 `attemptWindow`(1min) 即删；failures 键 `windowStart` 超过 `failureWindow`(10min) **且** `now` 晚于 `blockedUntil` 才删（即封锁中的键不会被清理）。
5. **成功登录清键**：`recordSuccess(ip, cn)` 删除对应 `failures[ip|cn]`；不影响 attempts，也不影响其他键。
6. **封锁结束后旧键保留**：封锁结束（now>blockedUntil）后，键要到 `windowStart` 也超过 failureWindow 才被下一次 cleanup 删除；由于封锁时 `windowStart` 被重置为封锁时刻、blockDuration==failureWindow==10min，二者基本同时到期，键在封锁结束后不久被清理。
7. **后台 goroutine**：无。清理完全惰性（在 `allow()` 内）。
8. **线程安全**：是，所有读写在单锁内；锁内无网络/DB/文件/慢操作。
9. **可注入时钟**：`allow`/`recordFailure`/`recordSuccess` 均接收 `now time.Time`，由 handler 的 `h.now`（默认 `time.Now`，测试可注入）传入。**已支持确定性测试。**
10. **管理员限流器**：`internal/admin/ratelimit.go` 结构与查询端几乎相同，但**已有 `maxTrackedKeys=10000` 的 fail-closed 上限**（在管理员登录限流批次中加入）：attempts 满且为新键时拒绝，failures 满且为新键时不记录。查询端**没有**该上限。
11. **本阶段范围**：admin 与 query 是**两套独立实现**（不同包、`cn` vs `username`、admin 有 `normalizeLimiterUsername`）。抽取通用包会同时触碰 admin 并需重验其 22 项测试，风险外扩。**决定只改查询端**，并把"admin 用更简单的 fail-closed 上限、未来可统一"记为后续项。
12. **数据竞争/锁内慢操作**：未发现——单锁、无慢操作。唯一问题是**无键数上限**：分布式喷洒（大量不同 CN 或经可信代理的不同真实 IP）可在窗口内让 `failures`（10 分钟窗口）临时膨胀，长期运行下内存无硬上限。

### 无限增长风险评估

- `attempts`（IP，1min 窗口）实际受"1 分钟内不同真实客户端 IP 数"限制，内网场景很小。
- `failures`（IP+CN，10min 窗口）受"10 分钟内不同 IP+CN 组合数"限制；攻击者用大量不同 CN 猜测可在窗口内制造较多键；虽有窗口上限，但**无硬上限**，是本阶段要修复的点。

### 结论 / 下一阶段

- 给查询限流器加**硬键数上限 + 惰性过期清理 + 保护封锁键的 LRU 驱逐 + 全封锁时安全降级**，不改现有阈值/封锁时长/429/文案/handler 接口；admin 端不动（已自有上限）。

### Git 状态

- 阶段末仅新增本日志；`git diff --check` 干净；暂存区空；无删除、无重命名。

## 阶段 2：生命周期设计

- 时间语义：`windowCounter`/`failureState` 各新增 `lastSeenAt`（最近一次访问/失败时间，用于 LRU）；封锁键的"可清理时间"由既有条件保证晚于 `blockedUntil`（cleanup 要求 `now-windowStart>=failureWindow` 且 `now>blockedUntil`，二者约同时到期），封锁中不会被清理。
- 惰性清理：保留 `allow()` 内的 `cleanupLocked`；容量管理仅在插入**新键**且 `len>=cap` 时触发（避免每次 `recordFailure` 全量遍历）。无后台 goroutine。
- 容量上限：新增 `maxTrackedKeys`（默认 `defaultMaxTrackedKeys=10000`）；`effectiveMaxKeys()` 将 `<=0` 视为默认，杜绝"配置成 0 即无限"。
- 达到上限：先 `cleanupLocked` 回收过期键；仍满则驱逐**最久未见且未封锁**的键；`failures` 全部处于封锁时不创建新键（安全降级——每 IP 20 次/分钟的主限流仍生效）。**绝不驱逐封锁中的键**，不使用随机驱逐。
- 成功登录：保持 `recordSuccess` 只删对应 `ip|cn`，不影响他键。
- 配置：不新增环境变量（默认值足够，测试直接设字段）；因此 `.env.example`/`backend/.env.example` 无改动。

## 阶段 3：实现

- 仅改 `backend/internal/query/ratelimit.go`：新增 `maxTrackedKeys` 字段与默认、`lastSeenAt` 字段、`effectiveMaxKeys()`、`ensureAttemptsCapacityLocked`、`ensureFailuresCapacityLocked`（均在持锁内、纯内存 O(n) 扫描，仅在满容量时触发）。
- 保持不变：`maxAttempts=20`/`attemptWindow=1min`、`maxFailures=5`/`failureWindow=10min`/`blockDuration=10min`、429 行为、统一错误文案、handler 接口（`allow`/`recordFailure`/`recordSuccess` 签名不变）、可信代理与真实 IP 逻辑（IP 仍由 `resolveClientIP` 注入）。
- 未在锁内做网络/DB/文件/慢日志；未新增依赖；未记录完整 CN/查询码/Cookie/令牌（限流器本就不记日志）。
- **管理员端未改**：`internal/admin/ratelimit.go` 已有自己的 `maxTrackedKeys` fail-closed 上限，属独立实现；本轮不顺手改，统一为后续可选项（已记入 HANDOVER）。

## 阶段 4–5：单元与并发测试

- 新增 `backend/internal/query/ratelimit_cap_test.go`（14 个用例），覆盖：过期未封锁键清理、封锁键封锁期内不被 TTL 清理、`failures` 满容量驱逐最久未见未封锁键、容量驱逐绝不动封锁键（全封锁→不建新键且两封锁仍在）、封锁到期后可接受新键、`attempts` 键受上限、极小 cap(1/2) 正确、`cap<=0` 回退默认且仍有界、5000 个不同键最终受 100 上限、`lastSeenAt` 访问后前移、空 map 清理/容量操作不 panic、并发（100 同键 + 300 异键 + 并发 success/cleanup）后两 map 不超上限且热键仍封锁、封锁截止与过期顺序正确。
- 既有 3 个 `ratelimit_test.go` 用例（阈值封锁、成功清键、每 IP 上限）保持通过。
- 并发正确性：所有 map 读写在单 `sync.Mutex` 内，无锁内慢操作；并发用例单独 `-count=20` 通过。

## 阶段 6：接口回归

- `go test ./internal/query`（含 login/handler/recovery-email/bind/query-code-recovery 等集成与单元测试）全部通过：正确凭据可登录、错误凭据统一文案、达阈值 429、封锁不泄露额外信息、跨客户端不串联、会话创建/查询码修改强制退出/找回邮箱/绑定码/`/api/config` 行为不变。
- `go test ./...` 全量通过，`internal/admin`、`internal/api` 未受影响（管理员登录不变）。数据库集成测试沿用既有隔离机制，未连接正式 `pjsk`。

## 阶段 7：文档与交接

- `HANDOVER.md` 增补"查询登录限流器内存有界"条目并说明与管理员端的差异及未来统一为可选项。
- `.env.example`/`backend/.env.example` 未改（无新增环境变量）。
- 本日志按阶段追加，未删除/移动/重命名历史日志。

## 阶段 8：完整验证

- `go fmt ./...`、`go build ./...`、`go vet ./...`、`go test ./...` 全部通过。
- `go test ./internal/query -run Limiter -count=10` 通过；并发用例 `-count=20` 通过。
- `go test -race ./internal/query` 因本机工具链（go1.26 + MinGW race 运行时，`exit status 0xc0000139` STATUS_ENTRYPOINT_NOT_FOUND）**无法运行**——对无并发的 `internal/logsafe` 运行 `-race` 得到**完全相同**的启动期错误，证明是环境/工具链问题而非代码 data race；未安装额外工具，未把不支持记为测试失败。并发正确性由单锁设计与无检测器下 20 次并发压测保证。
- 未连接或修改数据库；未修改接口/错误文案/阈值/封锁时长；未降低限流强度；未泄露认证信息。
