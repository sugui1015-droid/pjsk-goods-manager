# 2026-07-14 找回邮箱验证码与已登录用户验证

## 阶段 1：现状调查与边界确认

- 开始时分支为 `main`，`HEAD` 与本地 `origin/main` 均为 `800b84bd3846e84e451e518ba1d508d3e3c00909`；暂存区为空，除未跟踪的 `.claude/settings.local.json` 外无其他工作区变化。该本地文件未读取、未修改、未删除、未移动或暂存。
- 已完整读取并遵守根目录 `AGENTS.md`、开发日志目录规则、上一阶段找回邮箱专题日志；已检查 `backend/run.cmd`、`frontend/package.json`、Go 模块与缓存约定、迁移器、数据库连接、API 路由、CORS、查询会话鉴权、PostgreSQL 集成测试连接方式及清理保护。
- 迁移器按文件名排序并逐文件事务应用嵌入的 SQL；当前迁移目录最高编号为 `0016_user_recovery_email.sql`，`0017` 未被占用，可用于本阶段且不会自动写入现有用户数据。
- 上一阶段的 `user_recovery_emails` 已提供当前邮箱唯一约束、`pending/verified/disabled` 状态、`verified_at`、逻辑失效和加密邮箱；`account_security_audit_logs` 可复用用户/系统安全审计。管理员替换或解绑会在事务中锁定用户与当前邮箱，并逻辑失效旧记录。
- 普通查询接口通过 `pjsk_query_session` 的哈希查找当前 active 用户；过期会话以及 disabled/merged 用户统一失效并清除 Cookie。普通用户邮箱响应只包含脱敏状态，不接受用户 ID、CN 或邮箱作为授权依据。
- 全仓库搜索确认目前没有 SMTP 配置、邮件 Sender 接口、fake/mock sender、邮箱验证码模型、outbox、任务队列或邮件测试记录。现有 `loginLimiter` 是仅适用于单进程登录/绑定尝试的内存限流，不适合作为验证码发送冷却和窗口上限的权威状态。
- 可复用组件包括：`recoveryemail.Protector` 的 AEAD 解密/脱敏能力、独立 HMAC 设计、`crypto/rand` 使用模式、查询会话鉴权、PostgreSQL 事务和行锁、安全 JSON 响应、账号安全审计、保留示例域名测试以及带订单/付款/CN 合并保护的集成测试清理方式。
- 本阶段边界固定为已登录普通用户发送与提交邮箱验证码、状态更新、发送冷却/窗口上限/尝试次数、邮件发送抽象和前端交互；不实现忘记查询码、邮箱重置查询码、手机号、短信、密保、普通用户编辑邮箱、管理员代验证或一次性绑定码改造。
- 调查阶段未连接外部网络或真实 SMTP，未读取或修改真实 `.env`，未创建测试数据，未接触真实业务数据，未修改业务代码。
- 阶段收尾 `git diff --check` 通过；`git status --short` 仅包含未跟踪 `.claude/settings.local.json` 与本日志；`git diff --stat` 对已跟踪文件无输出，暂存区为空。敏感检查未发现验证码、真实邮箱、密钥、Cookie、会话令牌、SMTP 或数据库秘密进入差异。
- 下一阶段先在本日志追加邮件抽象、验证码模型、事务边界、限流、审计与错误映射的明确设计，再开始迁移和核心代码。

## 阶段 2：验证码与邮件发送设计

- 新增独立 `internal/recoveryemailverification` 包，集中放置 6 位数字验证码生成、独立 HMAC-SHA-256、常量时间比较、固定策略、Sender 接口、fake sender、SMTP sender、业务服务与 PostgreSQL 存储；HTTP handler 只负责会话、输入和安全错误映射，不直接调用 SMTP。
- 验证码使用 `crypto/rand` 生成 6 位数字；数据库只保存以当前 `recovery_email_id` 作为上下文的 HMAC，不保存明文。固定策略集中为有效期 10 分钟、最大错误尝试 5 次、同一用户/当前邮箱冷却 60 秒、每小时最多 5 次发送。
- `0017_recovery_email_verification_codes.sql` 新建验证码表，状态采用 `sending/active/used/locked/expired/invalidated/delivery_failed`，保存用户、邮箱版本、HMAC、过期/发送/使用/失效时间、尝试次数和时间戳；部分唯一索引保证每个用户最多一个 `sending/active` 验证码。
- 发送事务边界：第一段短事务锁定 active 用户与当前 pending 邮箱，执行数据库权威冷却/窗口上限、失效旧码并插入 `sending` 记录后提交；随后解密收件地址并在无数据库行锁时调用 Sender；发送成功后第二段事务确认邮箱仍为当前记录，将验证码改为 `active`、写 `sent_at` 与 `recovery_email_verification_sent` 审计。发送失败或最终确认失败时验证码保持不可验证并尽力标记 `delivery_failed`，前端只收到通用 503/500，不收到底层错误。
- 验证事务边界：同一事务依次锁定用户、当前邮箱和最新验证码，确认用户 active、邮箱仍为当前 pending 版本、验证码 active 且未过期；错误码增加尝试次数，达到上限改为 `locked` 并写 `recovery_email_verification_locked` 审计；正确码将邮箱改为 `verified`、写 `verified_at`，验证码改为 `used`，其他未完成验证码全部失效，并写 `recovery_email_verified` 审计后一次提交。
- 管理员替换或解绑邮箱时，在现有用户/邮箱事务中同步把旧邮箱版本的 `sending/active` 验证码改为 `invalidated`；即使遗漏显式更新，验证事务也要求验证码绑定当前邮箱记录，旧码不能跨邮箱版本使用。
- 邮件模式设计为默认 `disabled`、正式 `smtp` 和仅 `APP_ENV=test` 可用的 `fake`。SMTP 仅支持 `starttls` 或直接 `tls`，配置必须完整、端口和发件地址合法、用户名与密码成对；fake sender会在内存记录投递而不是空返回成功，自动化测试不访问外部服务。
- 新增独立 Base64 HMAC 配置 `RECOVERY_EMAIL_VERIFICATION_HMAC_KEY`，至少 32 字节；示例文件只保留空占位。SMTP 密码、完整收件邮箱、验证码和底层发送错误不进入响应、日志或审计。
- 普通用户接口为 `POST /api/query/recovery-email/send-verification` 和 `POST /api/query/recovery-email/verify`。两者只从当前查询会话取得用户；发送请求不接收邮箱，验证请求只接收严格 6 位验证码。响应仅包含成功、中文消息、脱敏邮箱、状态、过期时间、验证时间和安全等待秒数。
- 前端仅在 pending 状态显示发送、验证码输入与确认按钮；验证码只存当前 Vue 内存，退出、改码、会话失效、切换用户和刷新都会清空。倒计时只展示服务端返回的冷却，后端数据库仍是权威限流。verified 状态只显示脱敏邮箱、已验证和验证时间；管理员页面不增加代验证操作。
- 计划修改范围仅限迁移、验证码新包、配置与示例、查询接口/路由/测试、管理员替换解绑的旧码失效、前端三个既有文件和本日志；不修改订单、付款、导入、CN 合并或一次性绑定码逻辑。
- 本阶段只追加设计日志，未修改业务代码、未创建测试数据、未连接 SMTP 或外部网络、未接触真实业务数据。下一阶段实现迁移和验证码核心包并先运行专项单元测试。
## 阶段 3：迁移与验证码核心

- 新增 `backend/migrations/0017_recovery_email_verification_codes.sql`，只创建验证码表、约束和索引，不为现有用户创建记录，也不改变任何现有邮箱状态。迁移为 UTF-8 无 BOM。
- 表记录用户、当前邮箱版本、HMAC、`sending/active/used/locked/expired/invalidated/delivery_failed` 状态、过期/发送/使用/失效时间、尝试次数和最大尝试数；部分唯一索引保证每个用户最多一个未失效的 sending/active 验证码。
- 新增 `backend/internal/recoveryemailverification/verification.go` 与 `verification_test.go`：使用 `crypto/rand` 拒绝采样生成 6 位数字验证码；HMAC-SHA-256 同时绑定邮箱记录 ID；存储哈希按十六进制解码后用常量时间比较。
- 10 分钟有效期、60 秒冷却、每小时 5 次、最多失败 5 次集中在默认策略中，没有散落到 HTTP handler。
- 专项执行 `go fmt ./internal/recoveryemailverification` 和 `go test -v ./internal/recoveryemailverification -run Verification`，随机格式/基本差异、HMAC 范围、正确/错误码、非法哈希、配置长度和随机源失败测试全部通过。
- 本阶段未访问数据库或 SMTP，未创建测试数据，未接触真实业务数据；未读取、修改真实 `.env`，真实 `.env` 差异为空。
- 阶段收尾 `git diff --check` 通过；`git status --short` 仅包含 `.claude/settings.local.json`、新验证码包、`0017` 迁移和本日志；暂存区为空。新增文件尚未计入 `git diff --stat`，敏感扫描未发现验证码值、真实邮箱、密钥、Cookie、会话令牌或 SMTP/数据库秘密。
- 下一阶段实现配置校验、Sender/fake sender/SMTP sender、邮件模板和单元测试，不连接真实邮件服务。
## 阶段 4：配置与邮件发送器

- 修改 `backend/internal/config/config.go`，新增验证码独立 HMAC、发送模式和 SMTP 配置校验；新增 `backend/internal/config/recovery_email_verification_test.go`。默认模式为 disabled，fake 只允许 `APP_ENV=test`，smtp 要求 HMAC、主机、端口、发件地址和 TLS 模式完整有效，用户名与密码必须成对。
- 修改 `.env.example` 与 `backend/.env.example`，只新增空的验证码 HMAC/SMTP 占位和 disabled 模式；没有写入任何密钥、SMTP 凭据或真实地址，没有读取或修改真实 `.env`。
- 新增 `fake_sender.go`：fake sender 在内存记录投递供测试断言，不是空实现返回成功；可模拟发送失败。新增 `smtp_sender.go`：基于标准库实现 STARTTLS 或直接 TLS，SMTP 网络调用支持超时/上下文，不记录收件地址、验证码或密码，底层错误统一收敛。
- 中文邮件模板只包含验证码、UTC 有效期和安全提醒，不包含查询码、Cookie 或其他账号凭据；发件人、收件人和头部字段均做格式或换行校验。
- 执行 `go fmt ./internal/config ./internal/recoveryemailverification` 和 `go test -v ./internal/config ./internal/recoveryemailverification`，缺失/部分配置、非法端口/TLS/HMAC、测试 fake、SMTP 配置、fake 投递/失败和中文模板测试全部通过；未发起任何真实 SMTP 连接。
- 本阶段未创建数据库测试数据、未接触真实业务数据。`git diff --check` 通过；`git status --short` 仅为本阶段配置示例、配置代码/测试、验证码包、`0017` 迁移、本日志和未触碰的 `.claude/settings.local.json`；暂存区为空。已跟踪差异统计为 3 个文件、127 行新增、13 行删除，未跟踪新文件尚不计入。
- 敏感检查确认真实 `.env` 差异为空，示例中的新增配置值均为空；未发现验证码、非空密钥、SMTP 密码、完整真实邮箱、Cookie、会话令牌或数据库秘密进入差异。
- 下一阶段实现 PostgreSQL 发送准备/确认/失败和验证事务、服务编排、发送/验证 HTTP 接口与集成测试。
## 阶段 5：发送/验证服务与 HTTP 接口接入（进行中）

- 修改范围：新增 PostgreSQL 验证码存储与服务编排、发送/验证 HTTP handler、路由装配；管理员替换或解绑当前找回邮箱时，在同一事务中使旧邮箱的发送中/有效验证码失效。
- 修改文件：`backend/internal/recoveryemailverification/store.go`、`service.go`、`service_test.go`，`backend/internal/query/handler.go`、`recovery_email_verification.go`、`recovery_email_verification_test.go`，`backend/internal/api/router.go`，`backend/internal/users/recovery_email.go`。
- 关键边界：SMTP 调用位于数据库事务之外；先创建 `sending` 记录，发送成功后才转为 `active` 并写发送审计；解密、脱敏、发送或最终确认失败时尝试标记 `delivery_failed`。接口只从当前查询会话取得用户，不接受前端用户 ID、CN 或邮箱。
- 已执行测试：验证码核心包全量测试通过；query 验证码专项测试通过；api、query、验证码核心包组合测试通过。
- 如实记录的失败：`go test ./internal/users -run RecoveryEmail` 当前失败，原因是本地测试数据库尚未应用新增迁移，PostgreSQL 返回 `recovery_email_verification_codes` 关系不存在。未跳过或删除测试；下一步通过项目现有迁移启动流程应用 `0017` 后重跑。
- 测试数据：本小阶段未创建新的数据库测试用户或邮件记录，未接触真实业务数据。
- 敏感信息：未调用外部 SMTP；API 响应不含完整邮箱、验证码、验证码哈希、邮箱密文、会话值或 SMTP 配置；真实 `.env` 无 Git 差异。
- 临时工具：未创建。
- `git diff --check`：通过（仅示例环境文件有行尾提示）。
- 当前 Git 状态：仅本阶段预期源码、测试、迁移、示例配置和本阶段日志发生变化，暂存区为空。
- 下一步：应用 `0017` 到本地开发数据库，补充 PostgreSQL 集成测试并重跑管理员邮箱回归测试。
## 阶段 6：数据库与 HTTP 集成验证完成

- 修改范围：新增验证码 PostgreSQL 生命周期集成测试和真实查询会话 HTTP 集成测试。
- 修改文件：`backend/internal/recoveryemailverification/store_integration_test.go`、`backend/internal/query/recovery_email_verification_integration_test.go`；并为发送失败后的状态收尾增加独立 3 秒清理上下文。
- 数据库迁移：通过项目现有启动迁移器应用 `0017_recovery_email_verification_codes.sql`；迁移成功后，原先因关系不存在而失败的管理员邮箱专项回归测试已通过。
- 覆盖结果：首次发送、60 秒冷却、1 小时窗口上限、重发使旧码失效、错误尝试锁定、过期、发送失败、换绑/解绑失效、审计安全字段、审计失败事务回滚、并发正确提交仅一次成功、HTTP 会话身份隔离、过期会话、未配置服务 503、响应不含内部安全字段均通过。
- 测试数据：仅使用 `TEST_RECOVERY_VERIFY_*` 前缀和 `example.com`/`example.org`/`example.net`；清理前检查订单、付款、CN 合并关联均为 0，测试结束由清理钩子删除验证码、审计、会话、邮箱、用户和临时管理员。
- 外部服务：未连接真实 SMTP，自动化发送全部使用内存 fake sender。
- 真实业务数据：未修改。
- 临时工具：未创建。
- 敏感信息：未输出完整邮箱、验证码、验证码哈希、Cookie、会话令牌、SMTP 密码、数据库密码或密钥。
- `git diff --check`：通过。
- 下一步：完成前端状态、倒计时和清理行为后执行全量验证。

## 阶段 7：普通用户邮箱验证前端完成

- 修改范围：仅修改 `frontend/src/App.vue`、`frontend/src/api/client.ts`、`frontend/src/style.css`。
- 完成内容：未登记状态只显示联系管理员提示；待验证状态显示脱敏邮箱、发送按钮、6 位数字输入、确认按钮、10 分钟有效期提示和展示倒计时；已验证状态显示脱敏邮箱与验证时间并隐藏操作入口。管理员页面继续只显示状态且明确没有代验证入口。
- 安全处理：验证码仅存在 Vue 内存状态，不写 URL、localStorage 或 sessionStorage；退出、查询码修改成功、会话 401、切换登录用户及刷新时清空；请求期间禁用按钮；429 使用后端等待秒数，前端倒计时不替代后端限流。
- 响应式：输入和按钮限制在容器内，720px 以下单列，脱敏邮箱可换行，适配 1280px 与 375px 验收目标。
- 测试：两次执行 `pnpm.cmd run build`，TypeScript 检查和 Vite 构建均通过，无立即构建错误。
- 构建产物：仅生成被项目忽略的现有 `frontend/dist` 构建结果，未进入 Git 状态；未创建临时工具。
- 真实业务数据：未修改；未连接外部 SMTP。
- 敏感信息：未写入或输出真实邮箱、验证码、查询码、绑定码、Cookie、会话令牌、密码或密钥。
- `git diff --check`：通过（仅已有行尾转换提示）。
- 当前 Git 状态：仅本阶段预期后端、前端、示例配置、迁移、测试及本阶段日志变更；暂存区为空。
- 下一步：执行 Go 全量格式化、构建、vet、测试和前端最终构建，再进行普通 Chrome 人工验收与测试数据回查。
## 阶段 9：普通 Chrome 人工验收（进行中）

- 服务：后端以进程级 `APP_ENV=test` + fake sender 配置启动，未修改真实 `.env`，健康检查和数据库连接正常；前端沿用项目现有 5173 开发服务。
- 已完成：普通用户待验证状态、脱敏邮箱、发送成功提示、10 分钟过期提示、冷却倒计时、错误验证码中文提示、正确验证、验证后隐藏操作、刷新后保持已验证、未登记邮箱不显示操作均通过。
- 响应式：1280px 页面无整体横向溢出；375px 下邮箱区域宽度未超过容器，输入和按钮样式未挤出。管理员已验证状态、脱敏显示、无代验证入口、原替换/解绑入口保留均通过。
- Console：截至当前验收步骤，本地页面未发现产品脚本红色错误；浏览器控制组件自身曾报告其外部统计请求超时，该请求不属于本项目，且本轮未主动访问外部服务。
- 当前停点：管理员测试邮箱替换的原生确认框已打开；Chrome 控制在该原生对话框处被阻塞，已将普通 Chrome 页面交还用户等待确认。尚未声称替换、解绑及其后续旧验证码人工回归通过。
- 测试数据：仅 `TEST_RECOVERY_VERIFY_CHROME_*` 和保留示例域名；尚未清理，因为人工验收仍在进行。清理前订单、付款、CN 合并关联检查由临时工具强制执行。
- 临时工具：`backend/.tmp/recovery-verification-acceptance.go` 仍仅用于当前未完成验收，未受 Git 跟踪；验收结束必须删除。
- 真实业务数据：未修改；仅查看管理员页面并操作隔离测试用户。
- 敏感信息：未连接真实 SMTP，未在日志中记录验证码、查询码、Cookie、会话令牌、密码或密钥。
- 下一步：用户确认当前替换对话框后，继续替换/解绑旧验证码失效、退出清空、disabled/merged/过期会话、Console 复核、测试数据与临时工具清理。
## 阶段 9：普通 Chrome 人工验收完成

- 管理员页面：未登记、待验证、已验证状态显示正常；只显示脱敏邮箱；没有代验证入口；原登记、替换、解绑入口仍存在。隔离测试用户的替换确认与结果通过。
- 普通用户页面：未登记邮箱不显示验证操作；待验证可发送；错误码提示、60 秒展示倒计时、后端限流提示、正确码成功、成功后刷新保持已验证均通过。
- 验证码状态：新验证码生成后旧码不能使用；已使用验证码不能重复使用；邮箱替换与解绑后旧码不能使用。
- 会话边界：退出后、页面刷新后、切换账号后、查询码修改成功并重新登录后，验证码输入均为空；disabled、merged 和过期查询会话都不能继续发送或验证。
- 权限与回归：登录页没有“忘记查询码”入口；首次设置查询码入口仍存在；普通用户没有邮箱新增、替换或解绑入口；管理员没有代验证入口。
- 响应式：1280px 普通用户页面无整体横向溢出；375px 普通用户邮箱区域和管理员邮箱区域均未超过容器，输入框和按钮未挤出。
- Chrome Console：本项目页面红色错误为 0。浏览器控制组件自身的外部统计请求超时不属于本项目，且没有影响页面验收；本轮未主动访问外部服务。
- Network 说明：Chrome 工具未直接读取 Network 面板；接口结果通过页面 UI、DOM、后端专项集成测试、数据库状态与审计计数交叉验证。
- 测试数据：仅 `TEST_RECOVERY_VERIFY_CHROME_*` 和保留示例域名。清理前订单、付款、CN 合并关联均为 0；清理后用户、临时管理员、验证码、邮箱、审计、查询会话计数均为 0。
- 临时工具：`backend/.tmp/recovery-verification-acceptance.go` 已删除；`backend/.tmp/` 当前为空；临时后端日志已删除。
- 服务恢复：fake sender 验收后端已停止；后端已按项目正常 `run.cmd` 方式重新启动，`/health` 为 `ok` 且数据库为 `connected`。前端继续使用项目现有 5173 开发服务。
- 真实业务数据：未修改，仅操作并清理隔离测试用户。
- 敏感信息：未在开发日志或最终记录中写入验证码、查询码、完整邮箱、Cookie、会话令牌、密码、连接串或密钥。
- 下一步：重新执行最终全量 Go 验证和前端构建，完成 Git 与敏感内容收口检查。
## 阶段 8/10：最终自动化验证与收口检查

- `go fmt ./...`：通过，未产生无关文件变化。
- `go build ./...`：通过。
- `go vet ./...`：通过。
- `go test ./...`：通过，包含配置、路由、HTTP、PostgreSQL 生命周期、并发、事务回滚、管理员换绑/解绑回归与清理测试。
- 专项测试：`internal/recoveryemailverification`、`internal/query`、`internal/api` 验证码相关测试均通过。
- `pnpm.cmd run build`：通过，Vue TypeScript 检查和 Vite 构建成功。
- 数据清理：`TEST_RECOVERY_VERIFY_*` 订单、付款、CN 合并关联清理前均为 0；清理后测试用户、管理员、验证码、邮箱、审计、查询会话均为 0。
- 临时内容：`backend/.tmp/` 为空，验收工具与临时后端日志已删除；构建产物未进入 Git 状态。
- 服务：后端已恢复项目正常启动方式，健康状态 `ok`，数据库 `connected`；前端 5173 正常。
- Git：分支 `main`；`HEAD` 与 `origin/main` 均为 `800b84bd3846e84e451e518ba1d508d3e3c00909`；暂存区为空；未提交、未推送。
- 变更范围：26 个预期文件，仅邮箱验证第二阶段的迁移、配置、后端核心/接口/测试、前端、示例环境文件和本阶段日志；无删除或重命名，无无关模块变化。
- 环境保护：真实 `.env` 无差异；新验证 HMAC 与 SMTP 密码在两个示例环境文件中均为空占位；未读取、修改或暂存 `.claude/settings.local.json`。
- 敏感检查：未发现非保留域名完整邮箱、私钥、Cookie/会话明文或非空新密钥占位；代码中的数字验证码和邮箱均为自动化/隔离验收使用的合成测试值与保留示例域名，不含运行时生成值或真实信息。
- `git diff --check`：通过，仅有 Git 行尾转换提示。
- 真实业务数据：未修改。
- 本轮子代理：未使用。
## 最终提交与推送

- 人工验收：全部通过，无未解决产品问题；隔离测试数据已经清理。
- 最终自动化验证：`go fmt ./...`、`go build ./...`、`go vet ./...`、`go test ./...`、验证邮箱相关 Go 专项测试及 `pnpm.cmd run build` 全部通过；`go fmt` 未产生额外文件变化。
- 准备提交范围：共 26 个预期文件，仅包含邮箱验证第二阶段的示例环境配置、`0017` 迁移、后端配置与验证服务、查询接口与路由、管理员换绑/解绑失效处理、对应测试、前端三个既有文件和本专题开发日志。
- 行尾复核：已结合 `git diff --numstat`、关键文件 word diff 与 Git 属性检查；仅存在已知行尾转换提示，没有无意义的整文件重写。
- 本地文件保护：`.claude/settings.local.json` 未读取、未修改、未删除、未暂存；真实 `.env` 未读取、未修改且不在 Git 状态中。
- 敏感信息：未输出或准备提交真实密钥、SMTP 密码、完整真实邮箱、验证码、查询码、绑定码、Cookie、会话令牌、数据库密码或私钥；新增密钥与 SMTP 密码示例项均为空占位。
- 数据与临时内容：未修改真实业务数据，测试数据已清理，未留下测试数据工具、临时凭据、临时日志或进入 Git 状态的构建产物。
- 提交前 Git：分支 `main`；`HEAD` 与 `origin/main` 均为 `800b84bd3846e84e451e518ba1d508d3e3c00909`；暂存区为空。
- 本阶段未再次修改业务代码、测试、迁移或页面内容，仅按仓库规则追加本次最终提交准备记录。
- 本轮未使用子代理；当前尚未提交、尚未推送，下一步为精确暂存、提交和普通推送。