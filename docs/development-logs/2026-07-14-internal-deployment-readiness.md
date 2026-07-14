# 正式环境配置与内网试运行准备

## 阶段 1：Git 基线和配置入口调查

### 完成范围

- 已确认分支为 `main`。
- 提交前 `HEAD` 与 `origin/main` 均为 `a964cbacc3ec4763c461c7b830f706ff65551799`。
- 调查开始时已跟踪工作区干净、暂存区为空，`git diff --check` 通过。
- 仅调查示例配置、Go/Vue 配置入口、邮件 sender、数据库连接与迁移启动、启动脚本及已有说明文档。
- 未读取真实 `.env`，未读取或处理 `.claude/settings.local.json`，未读取数据库数据或操作系统凭据。

### 实际配置入口

| 变量 | 用途 | 必需性与格式 | 安全要求 |
| --- | --- | --- | --- |
| `APP_ENV` | 应用环境；当前仅用于限制 fake sender | 可选，默认 `development`；fake sender 只接受 `test` | 正式环境应显式设为 `production`，但当前代码没有基于 production 的完整启动保护 |
| `APP_PORT` / `SERVER_PORT` / `BACKEND_PORT` | 后端端口，按此前顺序取第一个非空值 | 可选，默认 `8080` | 当前只能配置端口，不能配置监听主机 |
| `DATABASE_URL` | PostgreSQL 完整连接地址 | 与拆分变量二选一；非空时优先 | 视为秘密，不得输出、记录或提交；生产环境应使用合适的 TLS 参数 |
| `DATABASE_HOST` | PostgreSQL 主机 | 未使用 `DATABASE_URL` 时可选，默认 `localhost` | 正式环境建议仅连接本机数据库 |
| `DATABASE_PORT` | PostgreSQL 端口 | 未使用 `DATABASE_URL` 时可选，默认 `5432` | 不应向局域网开放数据库端口 |
| `DATABASE_USER` | PostgreSQL 用户 | 未使用 `DATABASE_URL` 时必需，非空字符串 | 使用最小权限账户，不记录实际值 |
| `DATABASE_PASSWORD` | PostgreSQL 密码 | 未使用 `DATABASE_URL` 时由代码读取；当前允许空值 | 视为秘密；正式环境不得为空，不得写入命令行、日志或 Git |
| `DATABASE_NAME` | PostgreSQL 数据库名 | 未使用 `DATABASE_URL` 时必需，非空字符串 | 正式库与恢复演练库必须分离 |
| `DATABASE_SSLMODE` | PostgreSQL SSL 模式 | 未使用 `DATABASE_URL` 时可选，默认 `disable` | 跨主机连接时必须按部署边界选择安全模式 |
| `ADMIN_SESSION_TTL` | 管理员会话和普通查询会话的共同有效期 | 可选，默认 `12h`；必须是正的 Go duration | 当前两类会话共用一个 TTL，正式值需审慎设置 |
| `ADMIN_COOKIE_SECURE` | 管理员和普通查询 Cookie 的 `Secure` 属性 | 可选，默认 `false`；仅接受布尔值 | 内网 HTTPS 必须显式设为 `true`；Cookie 已使用 `HttpOnly`、`SameSite=Lax` |
| `LEGACY_STREAMLIT_ADMIN_PORT` | 旧管理员端口，仅用于配置接口展示 | 可选，默认 `8512` | 不应误认为新后端监听配置 |
| `LEGACY_STREAMLIT_USER_PORT` | 旧用户端口，仅用于配置接口展示 | 可选，默认 `8513` | 不应误认为新后端监听配置 |
| `RECOVERY_EMAIL_ENCRYPTION_KEY` | 找回邮箱 AES-GCM 加密 | 与邮箱 HMAC 密钥成对可选；Base64 解码后必须正好 32 字节 | 必须独立保存和轮换，不得输出或提交 |
| `RECOVERY_EMAIL_HMAC_KEY` | 找回邮箱盲索引/查找 HMAC | 与邮箱加密密钥成对可选；Base64 解码后至少 32 字节 | 不得与其他用途密钥复用 |
| `RECOVERY_EMAIL_VERIFICATION_HMAC_KEY` | 已登录邮箱验证码 HMAC；当前也被查询码邮箱找回验证码和重置令牌共用 | sender 为 `smtp` 或 `fake` 时必需；Base64 解码后至少 32 字节 | 当前跨两个安全用途复用同一根密钥，不满足后续阶段要求的密钥独立性 |
| `RECOVERY_EMAIL_SENDER_MODE` | 邮件 sender 模式 | 可选，默认 `disabled`；仅允许 `disabled`、`smtp`、`fake` | `fake` 只允许 `APP_ENV=test`，正式环境应显式使用 `disabled` 或完成配置后的 `smtp` |
| `RECOVERY_EMAIL_SMTP_HOST` | SMTP 主机 | `smtp` 模式必需；非空且不得含换行 | 不得把真实主机与凭据写入日志或 Git |
| `RECOVERY_EMAIL_SMTP_PORT` | SMTP 端口 | `smtp` 模式必需；整数 1–65535 | 与服务商 TLS 模式匹配 |
| `RECOVERY_EMAIL_SMTP_USERNAME` | SMTP 用户名 | 可选；必须与密码同时配置或同时为空 | 视为敏感配置，不记录实际值 |
| `RECOVERY_EMAIL_SMTP_PASSWORD` | SMTP 密码 | 可选；必须与用户名同时配置或同时为空 | 视为秘密，不得输出、记录或提交 |
| `RECOVERY_EMAIL_SMTP_FROM` | SMTP 发件地址 | `smtp` 模式必需；必须是规范有效地址 | 不在诊断日志中输出完整地址 |
| `RECOVERY_EMAIL_SMTP_FROM_NAME` | 发件人显示名 | 可选；不得含换行 | 防止邮件头注入 |
| `RECOVERY_EMAIL_SMTP_TLS_MODE` | SMTP TLS 模式 | `smtp` 模式必需；仅允许 `starttls` 或 `tls` | sender 强制 TLS 1.2 及以上，不支持明文模式 |
| `VITE_API_BASE_URL` | 生产前端 API 基地址 | 可选；生产构建为空时使用同源相对路径 | 内网部署优先同源 `/api`，避免额外 CORS 和跨站 Cookie 风险 |

### 示例配置与代码差异

- 根目录 `.env.example` 使用 `DATABASE_URL`，`backend/.env.example` 使用拆分数据库变量；代码支持两种方式并优先使用 `DATABASE_URL`。
- `backend/.env.example` 中的 `JWT_SECRET` 未被当前 Go 代码读取；管理员和普通用户均使用随机不透明会话令牌，数据库只保存令牌哈希。
- 根目录示例中的 `SUPABASE_*`、`UPLOAD_DIR`、`MAX_UPLOAD_SIZE_MB`、`TZ` 未被本次调查的新 Go/Vue 配置路径直接读取；不得据此判定它们在旧系统中无用。
- `DATABASE_SSLMODE`、`BACKEND_PORT` 未完整出现在两份示例配置中；两份示例也没有统一覆盖当前 Go 配置入口。
- 当前没有独立的查询码邮箱找回 HMAC 环境变量；查询码找回与已登录邮箱验证共用 `RECOVERY_EMAIL_VERIFICATION_HMAC_KEY`。
- 当前没有可配置的 CORS/允许来源变量，允许来源固定为两个本机 Vite 开发地址。
- 当前没有监听主机变量；`http.Server.Addr` 使用 `:<port>`，实际监听所有接口，与现有文档所述“仅 127.0.0.1”不一致。
- 当前没有可信代理配置、日志级别配置或迁移开关；迁移在每次后端启动时自动执行。
- `APP_ENV` 虽然存在，但目前不负责强制 Cookie、监听地址、CORS、数据库 TLS 或迁移策略等生产安全规则。
- 管理员认证没有环境变量密码或 JWT 密钥；管理员凭据由数据库账户记录管理。

### 安全失败与日志核对

- 数据库拆分配置缺少用户或数据库名时会明确失败；但数据库密码允许为空，`DATABASE_SSLMODE` 默认 `disable`，不适合作为未经复核的正式默认值。
- 邮箱加密与盲索引密钥只配置一项、格式错误或长度不足时会明确失败。
- SMTP 模式缺少验证 HMAC、主机、端口、发件地址、TLS 模式，或用户名/密码只配置一项时会明确失败。
- sender 为 `disabled` 时若残留任意 SMTP 字段，配置加载会失败；`fake` 只允许测试环境并禁止同时存在 SMTP 字段，正式环境意外启用 fake 的路径已被阻止。
- sender 默认 `disabled`，因此 SMTP 未配置时不会尝试连接；相关邮件功能表现为不可用，而不是自动降级到 fake。
- SMTP sender 默认 10 秒超时，错误统一为不可用，不把认证信息回传；请求日志只记录 HTTP 方法、URL path 和耗时，不记录 query string 或请求体。
- 当前未发现直接记录完整邮箱、验证码、查询码、Cookie、SMTP 密码或密钥的日志语句。
- 仍需后续重点复核数据库连接失败和底层数据库错误的日志脱敏边界；当前启动代码会记录连接错误对象，业务 handler 也会记录若干底层存储错误。

### 执行命令与验证

- 执行了 Git 基线命令：`git status --short`、`git branch --show-current`、`git rev-parse HEAD`、`git rev-parse origin/main`、`git log -1 --oneline`、`git diff --check`、`git diff --cached --name-status`。
- 使用只读文本搜索和读取核对示例配置、配置加载、sender、数据库、迁移、Cookie、CORS、前端 API 和已有运行文档。
- 本阶段未运行应用、未启动 8080 或 5173 服务、未连接数据库、未连接 SMTP。
- 本阶段未运行自动化测试；仅调查和新增文档日志，阶段末执行 `git diff --check` 与 Git 状态复核。

### 数据与清理

- 未创建测试数据，未修改真实业务数据，无需数据库清理。
- 未生成密钥，未创建临时工具或构建产物。
- 未执行 `git add`、`git commit` 或 `git push`。

### 下一阶段

- 等待用户确认后进入阶段 2：结合代码实际要求调查密钥长度、编码、用途隔离、轮换影响和 Windows 安全保存方案；只编写脚本，不执行脚本生成真实密钥。

## 阶段 2.1：密钥使用位置与边界调查

### 当前密钥与认证材料清单

| 配置或材料 | 当前用途 | 主要使用文件 | 实际长度与编码 | 共用情况 | 轮换影响摘要 |
| --- | --- | --- | --- | --- | --- |
| `RECOVERY_EMAIL_ENCRYPTION_KEY` | AES-256-GCM 加密、解密找回邮箱 | `config.go`、`recoveryemail/email.go` | 标准 Base64；解码后必须正好 32 字节 | 不得共用 | 旧密文需双密钥读取或重新加密 |
| `RECOVERY_EMAIL_HMAC_KEY` | 规范化邮箱的 SHA-256 HMAC 盲索引 | `config.go`、`recoveryemail/email.go` | 标准 Base64；解码后至少 32 字节 | 不得共用 | 旧索引需从解密邮箱重算 |
| `RECOVERY_EMAIL_VERIFICATION_HMAC_KEY` | 已登录邮箱验证码；当前也作为查询码找回根 HMAC | `config.go`、`router.go`、两个 security/verification 文件 | 标准 Base64；解码后至少 32 字节 | 当前跨功能复用 | 同时影响两类未完成流程及找回限流关联 |
| 查询码找回内部 HMAC | 验证码、重置令牌、CN/IP 匿名标识 | `querycoderecovery/security.go`、`store.go` | 当前无独立变量，沿用验证 HMAC；至少 32 字节 | 根密钥复用、上下文分离 | 旧验证码、令牌及限流关联失效 |
| 管理员/普通查询会话令牌 | Cookie；数据库只存 SHA-256 | `admin/handler.go`、`query/handler.go` | `crypto/rand` 生成 32 字节，无填充 Base64URL | 不使用 HMAC | 不受邮箱密钥轮换影响 |
| 管理员密码、查询码 | bcrypt 校验与存储 | admin、query、querycoderecovery | 人工输入，不是环境密钥 | 不共用 | 不受邮箱密钥轮换影响 |
| `JWT_SECRET` | 无当前用途 | 仅存在于 `backend/.env.example` | 代码无要求 | 不适用 | 应移除或标记废弃 |
| `ADMIN_SESSION_TTL` | 管理员与普通查询会话共同 TTL | `config.go`、`router.go` | 正的 Go duration，不是密钥 | 两类会话共用时长 | 仅影响新会话到期时间 |

### 上下文与比较方式

- 已登录邮箱验证码使用版本化 verification 上下文并绑定找回邮箱记录 ID。
- 查询码找回分别使用 code、token、identifier 三种版本化上下文；验证码还绑定用户、邮箱、规范化 CN 和 purpose，identifier 再区分 `cn`/`ip`。
- 上下文隔离阻止摘要直接跨用途互用，但不能消除根密钥泄露和轮换的共同故障域，因此正式环境仍应使用独立查询码找回根密钥。
- 验证码与找回摘要使用 `subtle.ConstantTimeCompare`；管理员密码和查询码使用 bcrypt；高熵会话令牌仅保存 SHA-256。
- 找回邮箱密文和盲索引是持久数据，AES/查找 HMAC 轮换需要数据迁移；已验证邮箱状态不依赖验证码 HMAC。
- 查询码找回只持久化验证码、令牌和匿名标识的 HMAC，不持久化相应明文。

### 本小节边界

- 只读调查明确允许的配置、加密、会话、迁移与测试引用。
- 未修改 Go/Vue 或示例配置，未运行密钥生成代码。
- 未读取真实 `.env`，未处理 `.claude/settings.local.json`，未连接数据库、SMTP 或外部服务。

## 阶段 2.2：正式密钥隔离方案

### 推荐配置结构

- 保留 `RECOVERY_EMAIL_ENCRYPTION_KEY`：只用于邮箱 AES-256-GCM。
- 保留 `RECOVERY_EMAIL_HMAC_KEY`：只用于邮箱盲索引；虽然候选名 `RECOVERY_EMAIL_LOOKUP_HMAC_KEY` 更清晰，但立即重命名会增加现有部署兼容成本，可在未来通过显式别名迁移。
- 保留 `RECOVERY_EMAIL_VERIFICATION_HMAC_KEY`：只用于已登录邮箱验证码。
- 后续新增 `QUERY_CODE_RECOVERY_HMAC_KEY`：作为查询码找回内部验证码、重置令牌、CN 标识和 IP 标识的共同根密钥；内部继续用现有上下文隔离，不为每个上下文增加变量。

### 方案比较

- 方案 A（维持单一验证 HMAC）：改动和运维成本最低，现有流程完全兼容；缺点是泄露与轮换的故障域覆盖两个功能及全部匿名限流标识，不建议正式长期使用。
- 方案 B（新增独立找回 HMAC并永久允许旧变量回退）：便于平滑升级，但容易让正式环境长期停留在隐式复用状态，配置错误不易被发现；只能作为有截止时间的过渡策略。
- 方案 C（新增独立找回 HMAC并在 production 强制配置）：安全边界最清楚，配置遗漏会启动失败；代价是部署前必须生成、保存并显式注入新密钥，同时处理已有未完成流程。

### 推荐与兼容策略

- 推荐方案 C。项目尚未正式上线，当前是消除根密钥复用的最佳窗口。
- 后续 Go 改动可允许非 production 的开发/测试环境在短兼容期回退到旧验证 HMAC，并只记录变量名级别的弃用警告；production 在查询码找回启用时禁止回退并明确启动失败。
- 若上线前没有需要保留的未完成流程，可在维护窗口部署新密钥，并等待至少覆盖验证码/令牌 TTL 和限流窗口后开放入口。
- 若已经存在必须连续保留的流程，则需要短期双密钥验证：新记录只用新密钥，旧验证码/令牌允许旧密钥验证到期；限流事件需在过渡窗口同时查询新旧标识，不能仅切换后清空关联。
- 迁移顺序：先实现并测试新配置读取和 Manager 注入；再生成并安全注入独立密钥；确认旧流程处置策略；部署代码；验证启动失败规则与限流连续性；最后移除非 production 回退。
- 本阶段不修改 Go 代码，也不在示例配置中提前加入尚未生效的变量。
*** End Patch

## 阶段 2.3：轮换影响分析

### 邮箱加密密钥

- 立即失效：直接替换后，现有邮箱密文无法用新 AES-GCM 密钥解密，所有依赖邮箱明文的发送、掩码和索引重算流程不可用。
- 需要数据迁移：必须逐条读取旧密文、用旧密钥解密、用新密钥重新加密，并验证记录仍可读取。
- 需要短期双密钥兼容：在线迁移时新写入使用新密钥，读取先尝试带版本的新密钥并在明确版本下回退旧密钥；当前密文只有单一版本标记且代码不支持密钥环，需要单独设计。
- 不建议在线直接轮换：必须保留旧密钥直到全部记录迁移和抽样验证完成，且旧备份的恢复策略已经确定。

### 邮箱查找 HMAC

- 立即失效：旧 `email_lookup_hash` 与新密钥计算结果不同，按新摘要查找不到旧记录。
- 需要数据迁移：必须解密每个邮箱、规范化后重算盲索引并更新；迁移过程不得输出完整邮箱。
- 需要短期双密钥兼容：若不中断写入，需要同时处理新旧索引查询或维护版本化索引；当前单列结构没有原生双索引支持。
- 不建议在线直接轮换：应与加密密钥迁移分开计划，保留旧查找密钥至索引重算、唯一性检查和验证完成。

### 已登录邮箱验证码 HMAC

- 立即失效：所有尚未完成且由旧密钥生成的验证码无法匹配。
- 不受影响：已验证邮箱的 `verified` 状态和 `verified_at` 不依赖验证码 HMAC，不会因轮换撤销。
- 运维建议：在维护窗口先停止发送，等待 10 分钟 TTL 或明确使 sending/active 记录失效，再切换密钥；无需迁移已完成验证码。

### 查询码找回 HMAC

- 立即失效：旧找回验证码和未使用重置令牌无法匹配新 HMAC。
- 不受影响：已经完成的查询码重置、bcrypt 查询码哈希以及已记录的完成审计不受影响。
- 限流影响：旧 CN/IP 事件仍在数据库中，但新根密钥产生不同标识，无法继续关联；新窗口积累前可能暂时绕过旧计数。
- 需要短期双密钥兼容：要求无缝轮换时，旧验证码/令牌应只验证到自然到期，限流检查在窗口期同时计算并查询新旧标识；新记录只使用新密钥。
- 简化方案：试运行前可暂停入口，等待 10 分钟验证码/令牌 TTL 和 1 小时限流窗口过去，再切换并开放；不得把清空限流记录当作安全迁移。

### 会话相关

- 不受影响：管理员和普通查询会话令牌由 `crypto/rand` 生成 32 字节，数据库只保存 SHA-256；不依赖邮箱加密或 HMAC 密钥。
- TTL 配置变化不追溯修改已存在会话的 `expires_at`。
- 普通查询会话会在登出、过期、查询码修改/管理员重置/邮箱找回重置查询码时失效；用户 disabled/merged 状态也会阻止继续使用。
- 查询码邮箱找回成功会删除该用户全部普通查询会话，但不会删除管理员会话。
- 找回邮箱换绑、解绑或相关用户状态变化会使未完成找回流程失效，不等同于全局管理员会话轮换。
*** End Patch

## 阶段 2.4：兼容旧 PowerShell 的密钥生成示例

- 新增 `docs/internal-deployment-secrets.md`，包含使用 `RandomNumberGenerator.Create()`、实例 `GetBytes()`、`Dispose()` 和字节数组清零的兼容函数。
- 当前三个有效配置均使用源码允许的 32 字节：AES 密钥要求正好 32 字节，两个 HMAC 配置要求至少 32 字节。
- 规划中的查询码找回独立密钥明确标注为当前版本尚未读取，不得提前视为生效配置。
- 文档提供推荐的逐个生成模式和风险更高的一次生成模式；均不写文件、`.env`、剪贴板或日志。
- 文档提供 `Remove-Variable` 清理示例，并说明终端回滚、Transcript、远程协助、截图和录屏风险；`Clear-Host` 不被描述为安全擦除。
- 本阶段没有执行脚本，没有生成或输出任何真实密钥。

## 阶段 2.5：Windows 密钥保存方案

- 方式 A（仓库外 NTFS ACL 文件）：适合当前最小方案，但当前 Go 程序只调用无参数 `godotenv.Load()`，不会自动加载任意外部路径；必须由受控启动流程或服务管理器注入。目录只允许专用服务账户和必要管理员读取，普通用户不可读，备份必须加密，替换时防止编辑器临时副本和旧文件残留。
- 方式 B（用户级/系统级环境变量）：进程只在启动时继承，变更后必须重启；系统级暴露面较大，同账户或高权限进程仍可能读取，不建议默认存放全部应用密钥。
- 方式 C（Windows 凭据管理器）：当前程序无原生读取支持；增加支持需要 Windows 凭据 API/库、凭据命名、服务账户授权、失败策略、轮换与恢复测试。
- 方式 D（NSSM/WinSW 注入）：适合服务化，但密钥可能进入注册表、XML 或管理界面；必须使用专用低权限服务账户并严格保护配置 ACL，配置导出和 XML 不得进入 Git。
- 方式 E（专门秘密管理服务）：当前单机/小型内网阶段通常过重；多服务器、多人运维、频繁轮换或公网部署时具有集中审计、版本和吊销价值。
- 当前最小可行方案：仓库外受 ACL 保护的配置文件，加仅管理员可执行的受控环境注入流程；不能假设程序自动加载该文件。
- 更安全服务化方案：专用低权限服务账户，由 WinSW/NSSM 注入，严格保护文件/注册表与日志权限。
- 后续升级方案：多机或公网阶段采用专门秘密管理服务及应用身份；持久 AES/盲索引数据仍需应用层版本化迁移。

## 阶段 2.6：示例配置更新

- 更新根目录 `.env.example`：完整数据库 DSN 改为空值；补充 `ADMIN_SESSION_TTL`、`ADMIN_COOKIE_SECURE`、production 提示、HTTPS Cookie 提示以及密钥长度和禁止复用说明。
- 更新 `backend/.env.example`：数据库密码改为空值，新增代码已读取的 `DATABASE_SSLMODE`，移除完全未使用的 `JWT_SECRET`，补充 production、HTTPS Cookie、Base64 长度和禁止复用说明。
- 两份示例均明确当前 `RECOVERY_EMAIL_VERIFICATION_HMAC_KEY` 仍被查询码找回复用，独立变量需要后续代码改动。
- 未提前加入 `QUERY_CODE_RECOVERY_HMAC_KEY`，避免让部署人员误以为当前版本已经读取并生效。
- 示例中没有加入真实密钥、真实数据库密码、真实 SMTP 信息、真实邮箱或固定可用凭据。
- 未修改真实 `.env`，未修改任何 Go/Vue 代码。

### 阶段 2 日志格式更正

- 本轮追加阶段 2.2 和 2.3 时，追加命令误将两行纯文本补丁结束标记写入日志（现位于前文两节末尾）。这些行不含密钥、凭据、邮箱或业务数据，也不改变调查结论。
- 根据日志只能追加、不得删除或重写的规则，保留原行并在此追加说明；后续追加不再使用该标记。

## 阶段 2.7：最终检查

- 实际修改范围：根目录 `.env.example`、`backend/.env.example`、本专题开发日志、新增 `docs/internal-deployment-secrets.md`。
- 未修改 Go/Vue 代码、迁移、真实 `.env` 或业务数据。
- 明确路径敏感检查未发现非空密钥赋值、数据库密码/完整 DSN、SMTP 主机/用户名/密码/发件地址或私钥块。
- `git diff --check` 通过；两个新增 Markdown 文件另以 `git diff --no-index --check` 检查空白错误。
- 暂存区为空；未执行 `git add`、`git commit` 或 `git push`。
- 未生成真实密钥，未连接数据库、SMTP 或外部服务，未启动 8080/5173。
- 后续需要单独修改 Go 代码：增加并注入独立查询码找回 HMAC，在 production 强制配置，并为兼容/轮换策略补充测试。

## 阶段 3.1：当前装配路径确认

- 当前 `config.Load()` 先读取 `APP_ENV`，再解析邮箱验证码配置；`router.NewRouter` 使用 `cfg.RecoveryEmailVerificationHMACKey` 创建邮箱验证码 Manager，并再次把同一字节切片传给 `querycoderecovery.NewManager`。
- 查询码找回 Service、Store 和 Handler 本身通过显式 Manager/Service 注入，不自行读取环境变量；单元测试可继续直接构造 Manager，不依赖机器环境。
- 新变量最合适在 `backend/internal/config` 解析，加入 `Config` 字段，并在 router 只把该字段传给查询码找回 Manager；无需新增配置包或修改 querycoderecovery 内部算法。
- `APP_ENV` 当前无枚举校验，空值经 `EnvOr` 变为 `development`；fake sender 已使用去空白、大小写不敏感的 `test` 判断。新 production 判断应保持同样的去空白与大小写兼容。
- 找回路由始终注册，但只有邮箱加密器、验证码 Manager、sender 和查询码找回 Manager 装配成功时才真正可用；本阶段明确要求配置层在新旧 HMAC 都缺失时失败，production 更不得回退旧变量。
- 测试策略：配置解析测试使用 `t.Setenv`；用途隔离测试使用显式不同字节密钥；不得依赖真实机器变量。
- Git 基线仍为 `main`，`HEAD`/`origin/main` 均为 `a964cbacc3ec4763c461c7b830f706ff65551799`，暂存区为空。

## 阶段 3.2：兼容与强制策略

- 新变量确定为 `QUERY_CODE_RECOVERY_HMAC_KEY`：标准 Base64，解码后至少 32 字节，只供查询码找回验证码、重置令牌和 CN/IP 标识使用。
- 新变量存在时在所有环境优先使用；其 Base64 非法或长度不足时直接失败，不回退旧变量。
- `APP_ENV` 经去空白后大小写不敏感地等于 `production` 时，新变量必须存在；禁止回退 `RECOVERY_EMAIL_VERIFICATION_HMAC_KEY`。
- 非 production 仅在新变量缺失时允许回退到合法的旧验证 HMAC；每次应用配置加载最多记录一次仅含变量名的兼容警告，不输出值、邮箱、验证码或令牌。
- 非 production 新旧变量都缺失，或旧变量非法/不足 32 字节时明确失败。
- 不修改 HMAC 上下文、数据库结构、路由、接口、有效期、限流阈值、bcrypt、会话或邮箱加密逻辑。
- 推荐在试运行前完成独立密钥配置；兼容回退只用于开发/test 迁移，不作为正式长期策略。

## 阶段 3.3：独立密钥装配实现

- `backend/internal/config/config.go` 新增 `QueryCodeRecoveryHMACKey` 字段和 `loadQueryCodeRecoveryHMACKey`/`decodeHMACKey` 解析逻辑。
- 新变量使用标准 Base64 解码并要求至少 32 字节；错误只包含变量名和格式要求，不包含原始值。
- production 缺少新变量时明确失败；非 production 缺少新变量时仅允许合法旧验证 HMAC 回退，并记录一次变量名级安全警告。
- `backend/internal/api/router.go` 改为用 `cfg.QueryCodeRecoveryHMACKey` 创建查询码找回 Manager；邮箱验证码 Manager 继续只使用 `cfg.RecoveryEmailVerificationHMACKey`。
- 未修改 querycoderecovery 包的 code/token/identifier 上下文、算法、TTL、阈值、Store、接口或数据库。
- 新增 `backend/internal/config/query_code_recovery_test.go`，初步配置与用途隔离测试已通过。
- `go test -v ./internal/config` 首次因默认 Go 缓存目录权限失败；按项目约定切换到仓库内缓存后通过。该失败与代码无关。

## 阶段 3.4：测试与完整回归

- 新增配置测试覆盖：新变量合法、优先于旧变量、非法 Base64、少于 32 字节、production 缺失禁止回退、production 合法、development/test/空环境回退、两变量都缺失、旧变量非法、错误与警告不含输入值、APP_ENV 大小写/空白兼容。
- 新增用途隔离测试：邮箱验证根密钥生成的查询码找回摘要不能由新查询码找回密钥通过，反向也不能通过。
- 原 querycoderecovery code/token/identifier 上下文隔离测试全部保持通过。
- `go fmt ./...`、`go build ./...`、`go vet ./...`、`go test ./...` 全部通过。
- `go test -v ./internal/config`、`./internal/recoveryemailverification`、`./internal/querycoderecovery` 全部通过。
- `go test -v ./internal/query -run 'Recovery|QueryCode'`、`./internal/api -run 'Recovery|QueryCode'`、`./internal/users -run RecoveryEmail` 全部通过。
- 数据库集成测试使用项目既有隔离测试机制，测试通过并执行其既有清理/回滚流程；未手工创建或修改真实业务数据。
- 未生成临时凭据或密钥，仓库内 Go 缓存为项目既有缓存路径，不属于新增依赖。

## 阶段 3.5：示例配置和密钥文档更新

- `.env.example` 与 `backend/.env.example` 新增空的 `QUERY_CODE_RECOVERY_HMAC_KEY=`，明确标准 Base64、至少 32 字节、production 必填、development/test 回退仅为临时兼容且禁止密钥复用。
- `docs/internal-deployment-secrets.md` 更新为当前代码已经支持独立变量；逐个生成和一次生成全部两种示例均保留，但本轮没有执行。
- `JWT_SECRET` 保持移除；数据库密码和完整 DSN 保持空值；HTTPS 下 `ADMIN_COOKIE_SECURE=true` 提示保持清楚。
- 未加入固定密钥、真实 SMTP 信息、真实邮箱或任何可用凭据。

## 阶段 3.6：静态敏感信息与范围检查

- 明确检查本轮七个文件，未发现非空密钥赋值、数据库密码、SMTP 密码/主机/用户/发件地址、带密码完整 DSN、完整邮箱或私钥块。
- 配置错误与兼容警告只包含变量名及格式/策略，不拼接密钥原值；对应脱敏测试通过。
- 未修改 API 请求/响应、路由列表、数据库迁移、HMAC 上下文或算法；router 仅替换查询码找回 Manager 的密钥来源。
- `git diff --check` 通过；三个新增文件的 `git diff --no-index --check` 仅因正常存在内容返回差异状态 1，未输出空白错误。
- 当前无删除或重命名，无范围外业务变化，暂存区为空。

## 阶段 3.7：可选本机启动验证评估

- 未执行后端本机启动验证。从 `backend` 工作目录运行会触发 `godotenv.Load()` 读取真实 `.env`，违反本阶段边界；使用合法测试密钥继续启动又会进入数据库连接阶段。
- production 缺失/合法、development/test 回退、错误和警告脱敏已经由不读取真实 `.env` 的配置单元测试覆盖，因此不为可选验证突破安全边界。
- 本阶段未启动 8080 或 5173；阶段末只读检查端口监听状态。

## 阶段 3.8：阶段 3 完成摘要

- 独立变量 `QUERY_CODE_RECOVERY_HMAC_KEY` 已在配置层实现并显式注入查询码找回 Manager；标准 Base64，解码后至少 32 字节。
- production 强制新变量且禁止旧变量回退；非 production 新变量优先，缺失时可临时回退合法旧验证 HMAC并记录一次变量名级警告。
- 邮箱验证码与查询码找回已使用不同 Config 字段和根密钥；未修改 HMAC 上下文、算法、接口、路由列表、数据库或迁移。
- 全量与专项 Go 验证通过；配置格式微调后再次执行配置测试和完整构建通过。
- 可选启动验证因真实 `.env` 自动加载和后续数据库连接风险而跳过；8080、5173 均未监听。
- 本阶段最终修改七个文件，无删除或重命名，暂存区为空。
- 未生成正式密钥，未读取真实 `.env`，未处理 `.claude/settings.local.json`，未连接真实 SMTP，未修改真实业务数据，未提交、未推送，未使用子代理。
- 下一步等待用户确认后进入 SMTP 与 fake sender 正式环境隔离调查。

## 阶段 4.1：sender 装配与配置路径调查

- `RECOVERY_EMAIL_SENDER_MODE` 默认 `disabled`，支持 `disabled`、`smtp`、`fake`；mode 去空白并转小写。
- fake 只允许 `APP_ENV` 去空白、大小写不敏感等于 `test`；development 与 production 均拒绝，production 已绝对禁止 fake。
- disabled/fake 存在任意 SMTP 残留字段都会配置失败；SMTP 配置在 `config.Load()` 阶段完成格式校验，不会等到发送时才发现。
- router 始终注册邮箱验证与匿名找回接口。disabled 时 sender/service 不装配：已登录发送返回全局 503；匿名请求仍返回防枚举统一 200，但前端会推进验证码步骤，可能误导用户以为已发送。
- SMTP sender 构造只保存不可变配置，不在启动时连接；两个邮件用途复用同一 sender 实例，但调用不同接口和独立模板。SMTP sender 不复用网络连接，每次发送新建并关闭连接；无全局可变发送状态。FakeSender 用 mutex 保护记录，适合并发测试。
- 已登录邮箱验证与查询码找回使用不同主题和正文；后者不会包含旧查询码、重置令牌、Cookie、内部 ID 或自动登录链接。方法接口分离，模板不会被普通调用路径互换。
- 配置错误只描述变量/配置类型，不回显密码或完整地址；底层 SMTP 错误统一为 `SMTP service unavailable`，API 映射为通用不可用或匿名统一响应。
- 请求日志只记录 HTTP method、URL path 和耗时，不记录 body、query string、验证码或令牌。
- 当前无自动重试，因此不会由 sender 主动重复发送；但 SMTP 已接受邮件后客户端超时，或邮件发送成功后数据库 ConfirmSent 失败，用户重试仍可能产生重复邮件/不可用验证码，这是 SMTP 的不可判定交付边界。
- 使用标准库 `net/smtp`：`net.Dialer.DialContext` 建连，`tls` 用 `tls.Client` + `HandshakeContext`，`starttls` 用 SMTP `STARTTLS`；TLS 最低 1.2，设置 ServerName，默认启用证书验证，无跳过验证开关。
- 当前 10 秒配置超时用于拨号；连接后若请求有 deadline，代码直接采用请求 deadline，否则采用 10 秒 deadline。HTTP 发送请求有 15 秒总 context。TLS 直连握手支持 context，但 STARTTLS/认证/SMTP 命令在请求取消后不能立即停止，只受连接 deadline 限制。
- `/api/config` 尚无邮件服务全局状态；前端在 disabled 时仍显示匿名入口和已登录发送按钮。
*** End Patch

## 阶段 4.2：正式环境 sender 模式规则

- 保持现有规则：production 与 development 均禁止 fake；只有去空白、大小写不敏感的 `APP_ENV=test` 可显式启用 fake。
- sender 默认 disabled；production 允许 disabled 启动，但全局邮件状态必须明确不可用，不能让用户误认为邮件已发送。
- smtp 模式必须在启动期完整校验；不允许自动降级 fake 或 disabled，配置错误直接启动失败。
- test fake 禁止携带任何 SMTP 残留字段；测试只使用内存 fake 或本机隔离 listener。

## 阶段 4.3：SMTP 配置完整性与格式策略

- host 必填、去空白、禁止 CR/LF；port 必填且限定 1–65535，不设置隐式默认端口。
- username/password 必须同时存在或同时为空。继续明确允许无认证 SMTP，以兼容受控内网 relay；是否可用由部署方 SMTP 策略决定，公网/跨网场景建议认证。用户名视为敏感配置并新增 CR/LF 拒绝，密码从不记录。
- from 必须是无显示名的规范地址，禁止换行；不会从收件地址或用户名推导。
- from name 可选，禁止 CR/LF，并新增最多 128 UTF-8 字节限制，防止异常邮件头膨胀。
- TLS 只允许 `tls`（连接即 TLS）或 `starttls`（先建立 SMTP 后强制升级）；最低 TLS 1.2，ServerName 为 SMTP host，证书验证默认开启，无跳过验证配置。
- 配置错误只包含变量名/类型/范围，不包含原始 host、username、password、from 或 TLS 输入值。

## 阶段 4.4-4.5：SMTP 传输边界、超时与模板加固

- 两类邮件发送收敛到同一 SMTP 传输函数，但继续保留已登录邮箱验证与匿名查询码重置的独立方法、fake purpose、标题和正文。
- 仅允许 `tls` 与 `starttls`；最低 TLS 1.2，使用目标主机名进行默认系统证书链校验，不提供跳过校验或明文降级。
- 发送器不自动重试，每次请求最多一次连接；连接、TLS、认证、信封、DATA 和 QUIT 失败统一转换为不包含配置值或服务器响应的受控错误。
- 默认单次发送上限为 10 秒；实际 deadline 取配置上限与请求上下文 deadline 的较早者，请求取消会主动关闭底层连接。
- SMTP 用户名与发件显示名禁止 CR/LF；显示名最多 128 个 UTF-8 字节；发件地址继续要求规范单一地址。
- 两类纯文本模板都明确验证码用途、UTC 过期时间、不会自动登录及非本人操作提示，不包含查询码、Cookie、会话令牌、数据库信息或密钥。
- 本阶段未连接 SMTP、未发送真实邮件、未读取或修改真实 `.env`，未修改真实业务数据。

## 阶段 4.6：disabled 模式产品行为

- `/api/config` 新增单一布尔状态 `emailDeliveryEnabled`，只表示邮件投递是否可用，不暴露 sender 模式或 SMTP 配置细节。
- disabled 模式下，前端隐藏匿名“忘记查询码”入口并显示通用不可用提示；函数入口仍有保护，避免旧页面状态触发请求。
- 已登录用户的待验证邮箱界面不再提供发送/验证表单，而显示邮件服务暂未启用；管理员仍可管理邮箱记录，同时明确提示用户当前无法接收验证码。
- smtp 与仅限测试环境的 fake 模式报告为可用；配置加载仍负责阻止 fake 进入 development/production。
- 本阶段未再次扩大找回功能范围，未连接外部服务，未修改真实业务数据。
## 阶段 4.7：SMTP 隔离与回归测试

- 配置测试覆盖：默认 disabled；production/development 拒绝 fake；仅规范化后的 test 允许 fake；disabled/fake 拒绝残留 SMTP 字段；SMTP 必填项、端口、地址、账号密码配对、TLS 模式、换行注入与发件人名称长度校验；错误不回显测试配置值。
- sender 测试覆盖：fake 分用途记录；两个模板用途隔离；TLS 最低版本、ServerName 与证书校验开关；连接失败、无 STARTTLS、TLS 握手失败、SMTP 命令超时和 context 取消；每次失败只有一次连接，不自动重试；错误不包含测试验证码或测试邮箱。
- API 测试覆盖 `/api/config` 在 disabled、smtp、fake 下的全局邮件可用布尔值；原有找回邮箱和查询码找回接口回归保持通过。
- 首次完整序列在 `go vet ./...` 发现新增测试缺少两个标准库 import，未进入后续测试；仅修正测试 import 后，从 `go fmt ./...` 开始完整重跑并全部通过。
- 最终通过命令：`go fmt ./...`、`go build ./...`、`go vet ./...`、`go test ./...`、config/recoveryemailverification/querycoderecovery 定向 verbose 测试、query/api/users 指定回归测试。
- `pnpm.cmd run build` 通过（Vue TypeScript 检查与 Vite production build）。构建产物位于既有忽略目录，未进入 Git 状态。
- SMTP 测试仅启动进程内 `127.0.0.1` 临时 listener，测试结束均关闭；未连接公网或真实 SMTP，未发送邮件。
- PostgreSQL 集成测试使用项目既有隔离测试机制，测试全部通过并完成各自回滚/清理；未创建或修改真实业务数据。

## 阶段 4.8：示例配置与部署文档

- `.env.example` 与 `backend/.env.example` 补充 disabled/smtp/fake 边界、fake 仅限 test、TLS 1.2 与证书校验、账号密码配对和受控匿名 relay 说明；所有敏感示例值继续为空。
- `docs/internal-deployment-secrets.md` 增加 SMTP/fake 正式环境边界、失败关闭、超时与取消、无自动重试、模板用途隔离、仓库外保存及启用/回退顺序。
- 文档明确 production 禁止 fake、SMTP 配置不完整时启动失败、disabled 不代表邮件已发送、不得提交真实 SMTP 配置或关闭证书校验。
- 未新增依赖，未写入真实主机、账号、邮箱、密码、验证码或密钥，未修改真实 `.env` 或真实业务数据。
## 阶段 4.9：静态安全与最终范围复核

- 仅检查本轮明确文件与 Git 新增行，未读取真实 `.env`，未读取、哈希或处理 `.claude/settings.local.json`。
- 新增内容扫描结果：非保留域完整邮箱 0、有效数据库 DSN 0、带密码 DSN 0、非保留域 SMTP 主机 0、非空敏感示例赋值 0、私钥标记 0。
- 示例和测试中只使用保留域名、回环地址与明确测试值；未发现真实 SMTP 主机、用户名、密码、发件邮箱、验证码、密钥、数据库密码或私钥。
- 未新增依赖、测试证书、临时 SMTP 工具或受 Git 关注的构建产物；SMTP sender 无自动重试，也不记录配置值、验证码或底层 SMTP 响应。
- 本轮差异无删除、无重命名；暂存区为空；`git diff --check` 通过。
- 分支仍为 main；HEAD 与 origin/main 均为 `a964cbacc3ec4763c461c7b830f706ff65551799`。
- 8080 与 5173 均未监听；所有测试回环 listener 已随测试结束关闭。
- 后续仍需单独处理可信反向代理客户端 IP、监听主机配置、CORS 正式值和数据库日志脱敏；本阶段未进入这些范围。
- 未连接真实 SMTP、未发送真实邮件、未修改真实业务数据、未暂存、未提交、未推送，未使用子代理。
### 阶段 4.9 补充：总发送 deadline 最终对齐

- 最终复核发现连接后的 deadline 原先从拨号成功时重新起算，可能形成拨号上限与后续命令上限叠加，不符合文档所述的单次总上限。
- 已将 operation deadline 提前到拨号前计算，并用同一 deadline 覆盖 DNS/拨号、TLS 握手、认证、SMTP 命令与写入；请求上下文 deadline 更早时仍优先采用更早值。
- 仅修改 SMTP sender 的 deadline 计算，没有改变公开响应、冷却、限流、验证码 TTL、模板或重试策略。
- 修正后重新执行完整 Go 验证、全部指定定向回归和前端 production build，全部通过；未连接真实 SMTP。

## 阶段 5.1：客户端 IP 使用路径调查

### 使用位置清单

| 模块/功能 | 文件与函数 | 当前 IP 来源 | 当前规范化方式 | 用途 | 反向代理风险 |
| --- | --- | --- | --- | --- | --- |
| 普通查询登录限流 | `query/handler.go` `Login`（约 207 行）→ `query/ratelimit.go` `clientIP` | `r.RemoteAddr` | `net.SplitHostPort` 剥离端口；失败时返回原始字符串 | 每 IP 每分钟最多 20 次尝试；IP+CN 组合 10 分钟内 5 次失败封禁 10 分钟（内存） | 所有用户共享代理 IP：每分钟 20 次的全局瓶颈；攻击者可用共享 IP+目标 CN 触发失败封禁，锁死任意 CN 的登录 |
| 已登录修改查询码限流 | `query/handler.go` `ChangeQueryCode`（约 344 行） | `r.RemoteAddr`（同 `clientIP`） | 同上 | IP + `change:<userID>` 组合复用同一 loginLimiter | 同上：共享代理 IP 使每分钟 20 次尝试上限被全体用户共同消耗 |
| 首次绑定码限流 | `query/bindcode.go` `BindCode`（约 71 行） | `r.RemoteAddr`（同 `clientIP`） | 同上 | IP + `bind:<cn>` 组合复用同一 loginLimiter | 同上；且攻击者可对目标 CN 制造失败封禁 |
| 查询码找回请求限流 | `query/query_code_recovery.go` `RequestQueryCodeRecovery`（约 67 行）→ `querycoderecovery.Service.Request` → `PostgresStore.PrepareRequest` | `r.RemoteAddr`（同 `clientIP`） | 同上；随后 `IdentifierHash("ip", ip)` 做 HMAC，数据库只存 `ip_hash` | CN 维度与 IP 维度各自的窗口次数限制（数据库持久） | 所有找回请求折算为同一 `ip_hash`，IP 窗口限额被全体用户共同耗尽，正常用户无法发起找回 |
| 管理员登录 | `admin/handler.go` `Login` | 不读取 IP | 不适用 | 当前完全没有限流，也不记录 IP | 无代理头风险，但缺少限流本身是已知待办，不属于本轮新增范围 |
| 邮箱验证码限流 | `recoveryemailverification/store.go` `PrepareSend` | 不读取 IP | 不适用 | 仅按 `user_id` 做 60 秒冷却 + 窗口 5 次（数据库） | 与 IP 无关，反向代理不影响 |
| HTTP 请求日志 | `api/router.go` `loggingMiddleware` | 不读取 IP | 不适用 | 只记录方法、路径、耗时 | 不受影响 |
| 安全审计 | `querycoderecovery/store.go`、`users/recovery_email.go` 写 `account_security_audit_logs` | 不直接读请求 | metadata 不含明文 IP；找回事件表只存 `ip_hash`（HMAC） | 审计 | 不直接受影响，但 `ip_hash` 的可用性依赖上游 IP 正确性 |

### 调查结论

1. 全部非测试代码只有一处读取 `r.RemoteAddr`：`query/ratelimit.go` 的 `clientIP`，共 4 个调用点（登录、修改查询码、首次绑定、找回请求）。不存在重复的 IP 提取实现。
2. 没有任何代码读取 `X-Forwarded-For`、`X-Real-IP` 或 `Forwarded`；当前不信任任何代理头，因此不存在已有的伪造或不一致风险。
3. `clientIP` 用 `net.SplitHostPort` 剥离端口；解析失败时原样返回 `r.RemoteAddr`（可能为空串或含端口的畸形值），该值直接进入内存限流键。
4. 无任何 IPv4/IPv6 规范化：IPv6 按 Go 字符串原样使用，IPv4-mapped IPv6（`::ffff:1.2.3.4`）与 `1.2.3.4` 会被当作两个不同标识；未使用 `net/netip`。
5. 空或非法 `RemoteAddr`：内存限流接受任意字符串键（包括空串，所有异常请求会合并进同一个键）；查询码找回路径中空 IP 会使 `IdentifierHash` 返回 `ErrUnavailable`，`Request` 的错误被 handler 静默忽略（防枚举 200），实际效果是找回静默失败。
6. 请求日志不记录 IP；审计日志不记录明文 IP，查询码找回事件只持久化 HMAC `ip_hash`。
7. 限流组合方式：普通登录=纯 IP 窗口 + IP+CN 失败封禁；修改查询码=IP+userID；首次绑定=IP+CN；找回=CN 哈希与 IP 哈希双维度。管理员登录当前没有任何限流，邮箱验证码限流只按 user_id。
8. 反向代理后：上表前四项全部把所有客户端识别为代理地址，既造成全局限流瓶颈，也允许攻击者利用共享 IP 触发针对任意 CN/用户的失败封禁。
9. 建议统一接入点：新增独立的客户端 IP 解析器（如 `backend/internal/clientip`），替换 `query.clientIP` 的实现或其 4 个调用点入口；管理员登录和邮箱验证码当前没有 IP 入口，不在替换范围内，也不得借本轮新增限流。
10. 对 5.2 设计的影响：由于全仓库只有一个提取函数和 4 个调用点，统一解析器落点清晰；指令中"管理员登录限流、邮箱验证码限流"两项在当前代码中不存在按 IP 的实现，接入清单需据实缩减为查询包内 4 个入口。

### 本小节边界

- 只读搜索与读取源码；未修改任何 Go/Vue 代码或示例配置，仅追加本日志。
- 未读取真实 `.env`，未处理 `.claude/settings.local.json`，未连接数据库、SMTP 或外部服务，未运行写数据库的命令，未使用子代理。

## 阶段 5.2：可信代理与客户端 IP 解析设计

### 配置变量

- 新增 `TRUSTED_PROXY_CIDRS`：逗号分隔 CIDR 列表，如 `127.0.0.1/32,::1/128`；代码默认值为空列表，即默认不信任任何代理。
- 解析规则：每项去首尾空白；支持 IPv4 与 IPv6 CIDR；整串空白视为空列表；出现中间空项（如 `a,,b`）配置失败；规范化后去重；裸 IP、hostname、非法 CIDR 启动失败；`0.0.0.0/0` 与 `::/0` 明确拒绝；不自动信任 loopback 或 RFC1918 私网；不做 DNS 查询。
- 配置错误只包含变量名、条目序号和违反的规则，不回显完整原始配置值。
- 文档建议单机反向代理只信任 `127.0.0.1/32` 和 `::1/128`；允许用户显式配置真实需要的内网代理 CIDR。

### 代理头范围

- 仅支持 `X-Forwarded-For`；明确不支持 `X-Real-IP` 和 `Forwarded`，避免多头优先级冲突和不完整解析。
- 未配置可信代理，或 TCP 连接来源不在可信 CIDR 内时，完全忽略 `X-Forwarded-For`。

### XFF 防滥用上限（包内常量，不新增环境变量）

- 多个同名 header 值按 HTTP 语义以逗号合并后统一检查：总长最多 4096 字节，最多 32 个地址。
- 超过任一上限整条链视为无效并回退合法 `RemoteAddr`；不截断续解析，不跳过非法条目。

### 地址解析规则

- 使用 `net/netip`。`RemoteAddr` 先按 `host:port`（`ParseAddrPort`）解析，无端口时再按纯 IP（`ParseAddr`）解析；支持 IPv4/IPv6；一律 `Unmap()` 统一 IPv4-mapped IPv6；拒绝 hostname、空串和带 zone（如 `%eth0`）的地址；不把原始非法字符串当作限流键。
- XFF 每项必须是不带端口的纯 IP：拒绝端口、hostname、`unknown`、空项、zone；同样 `Unmap()`。任一条目非法则整条链无效，回退合法 `RemoteAddr`。

### 从右向左解析算法

完整节点链 = XFF 中的地址 + `RemoteAddr`。步骤：
1. 解析 `RemoteAddr`；非法则进入统一未知客户端结果。
2. `RemoteAddr` 不属于可信代理：忽略全部 XFF，返回 `RemoteAddr`。
3. `RemoteAddr` 可信且 XFF 为空：返回 `RemoteAddr`。
4. 严格解析完整 XFF；非法、过长或地址过多时返回合法 `RemoteAddr`。
5. 从完整链最右侧向左遍历，跳过可信 CIDR 内的地址；第一个不可信合法地址即客户端地址，更左侧不再参与选择。
6. 链上全部地址都可信时，返回 XFF 最左侧合法地址。
7. 左侧伪造示例：`RemoteAddr=127.0.0.1:50000`、可信 `127.0.0.1/32`、`XFF=1.2.3.4, 203.0.113.10` → 结果必须是 `203.0.113.10`，不得取最左侧。

### 统一失败策略与结果结构

- `Result{Addr netip.Addr; Source Source; Valid bool}`，`Source` 区分 `SourceRemote`/`SourceForwarded`/`SourceUnknown`。
- `Result.Key()`：合法地址返回 `Addr.String()`；非法 `RemoteAddr` 返回固定值 `unknown-client`；不返回空串或原始非法字符串；错误与日志不输出完整代理链。
- 查询码找回用同一 `Key()` 结果计算既有 `IdentifierHash("ip", …)`：非法来源共享固定未知桶，不再空值静默失败，也不能绕过限流——这是有意的安全失败行为。

### 依赖注入与落点

- 新增 `backend/internal/clientip/clientip.go` 与 `clientip_test.go`：`NewResolver(trusted []netip.Prefix) *Resolver`、`(*Resolver) Resolve(*http.Request) Result`。
- 配置层新增 `TrustedProxyCIDRs []netip.Prefix`（规范化后保存）。
- Router 装配一个 Resolver，将 `func(r *http.Request) string { return resolver.Resolve(r).Key() }` 注入 `query.Handler`。
- 注入方式：`query.Handler` 新增函数字段并提供 Configure 方法，`NewHandler` 默认使用零可信代理的 Resolver（与现状语义一致且修复畸形键），避免改动 `NewHandler` 签名而牵连全部既有测试构造；4 个入口（登录、修改查询码、首次绑定、找回请求）复用同一注入函数，query 包不读环境变量，测试可注入固定 IP 函数。
- 旧 `query.clientIP()` 在替换后删除（删除函数，不删除文件）。

### 与源码核对结论

- 现有 4 个入口与 5.1 清单一致；现有 query 测试用 `RemoteAddr="10.0.0.x:50000"` 构造，新解析器对其产生与旧实现相同的键，既有测试兼容。
- 本阶段仅设计与日志追加，未修改任何代码。未读取真实 `.env`，未处理 `.claude/settings.local.json`，未使用子代理。

## 阶段 5.3：clientip 包与可信代理实现

### 实现内容

- 新增 `backend/internal/clientip/clientip.go`：`NewResolver([]netip.Prefix)`、`Resolve(*http.Request) Result`、`Result.Key()`；包内常量 XFF 合并总长上限 4096 字节、最多 32 个地址；非法 `RemoteAddr` 的固定键 `unknown-client`。
- `RemoteAddr` 先按 `ParseAddrPort` 再按 `ParseAddr` 解析；拒绝空串、hostname、zone；`Unmap()` 统一 IPv4-mapped IPv6。XFF 多值按逗号合并后严格解析，任一条目非法（端口、hostname、`unknown`、空项、zone）或超限则整链无效回退 `RemoteAddr`。
- 从右向左剥离可信代理；第一个不可信合法地址即客户端；全链可信时取 XFF 最左地址；默认零可信代理时完全忽略 XFF。
- `backend/internal/config/config.go` 新增 `TrustedProxyCIDRs []netip.Prefix` 与 `loadTrustedProxyCIDRs()`：空/空白为空列表；中间空项、裸 IP、hostname、非法掩码、`/0` 全网段启动失败；`Masked()` 规范化去重；错误只含变量名、条目序号和规则。
- `query.Handler` 新增 `ClientIPResolver` 函数字段与 `ConfigureClientIPResolver`；`NewHandler` 默认使用零可信代理 Resolver；登录、修改查询码、首次绑定、找回请求 4 个入口改用注入函数；删除旧 `query.clientIP()` 及其 `net`/`net/http` 残留 import。
- `router.go` 用 `cfg.TrustedProxyCIDRs` 创建单个 Resolver 并以 `Resolve(r).Key()` 包装注入；query 包不读取环境变量。
- 未修改限流阈值、窗口、封禁时间、HMAC 上下文、公开响应、审计事件类型或数据库结构；未给管理员登录或邮箱验证码新增 IP 限流。

### 测试

- 新增 `clientip_test.go`（16 个用例函数覆盖指令列出的 30 项场景：无可信代理/非可信来源忽略 XFF、单层/两层/多层代理、从右向左剥离、左侧伪造、全链可信、无 XFF、多 header 值、空项/非法 IP/`unknown`/端口/hostname/zone、IPv4/IPv6/IPv4-mapped、RemoteAddr 各种格式与非法值、4096 字节与 32 条目边界内外、回退行为、Key 不回显原始畸形输入）。
- 新增 `config/trusted_proxy_test.go`：未设置/空白为空、单个与多个 IPv4/IPv6 CIDR、去重、未掩码位规范化、中间空项/裸 IP/hostname/非法掩码/`0.0.0.0/0`/`::/0` 失败、错误不回显完整配置值。
- 新增 `query/clientip_injection_test.go`：注入固定键驱动内存限流、默认解析器下伪造 XFF 无法逃逸封禁、找回请求收到规范化键（IPv4-mapped 解映射）、非法 RemoteAddr 进入 `unknown-client` 而非空值静默失败、可信 loopback 代理下不同转发客户端限流互不影响。
- `go fmt ./...`、`go build ./...`、`go vet ./...`、`go test ./...`（全部包）、`go test -v ./internal/clientip`、`./internal/config`、`./internal/query` 定向、`./internal/querycoderecovery` 全部通过；数据库集成测试使用项目既有隔离机制并完成自身清理/回滚。
- Go 缓存沿用项目既有 `D:\pjsk\.cache` 路径。未读取真实 `.env`，未处理 `.claude/settings.local.json`，未连接 SMTP，未修改真实业务数据，未使用子代理。

## 阶段 5.4：监听主机配置

### 实现内容

- `config.go` 新增 `Host` 字段与 `loadServerHost(appEnvironment)`：读取 `SERVER_HOST`，默认 `127.0.0.1`（development/test/production 一致）。
- 只接受字面 IP：`netip.ParseAddr` 解析，拒绝 hostname（含 `localhost`，避免 DNS 歧义）、带端口、CIDR、zone、空值解析成全网卡；`Unmap()` 规范化 IPv4-mapped IPv6；非法值启动失败，错误只含变量名和规则。
- 显式 `0.0.0.0` 或 `::` 被接受为有意的全接口监听；`APP_ENV=production`（去空白、大小写不敏感）下使用全接口地址时输出一次仅含变量名和建议的安全警告，不输出其他配置或凭据；不增加额外确认变量。
- `main.go` 监听地址从 `":"+cfg.Port` 改为 `net.JoinHostPort(cfg.Host, cfg.Port)`，正确支持 IPv6 字面量；启动日志改为输出实际绑定地址。
- **安全行为变化**：后端默认监听从全部网络接口改为仅 loopback `127.0.0.1`。此前局域网可直接访问 `http://<本机IP>:8080` 的部署方式将失效，需要显式设置 `SERVER_HOST=0.0.0.0`（或前置反向代理）才能恢复，这是有意的变更。
- 端口优先级保持 `APP_PORT` > `SERVER_PORT` > `BACKEND_PORT` > `8080` 不变；仅将原表达式原样提取为 `loadPort()` 以便测试，无行为变化。

### 测试

- 新增 `config/server_host_test.go`：默认/空白回退 loopback；合法 IPv4、IPv6、IPv6 loopback、IPv4-mapped 解映射、显式 `0.0.0.0`/`::`；hostname、DNS 名、IPv4/IPv6 带端口、CIDR、zone 失败且错误含变量名；production 全接口警告存在、不含无关配置、loopback 不触发警告；IPv6 `JoinHostPort` 产生 `[::1]:8080`；端口优先级四级链测试。
- `go fmt ./...`、`go build ./...`、`go vet ./...`、`go test -v ./internal/config` 全部通过。
- 未启动服务器，未监听任何端口；未读取真实 `.env`，未使用子代理。

## 阶段 5.5：正式 CORS 配置

### 实现内容

- `config.go` 新增 `loadCORSAllowedOrigins(appEnvironment)` 与 `normalizeCORSOrigin`：读取 `CORS_ALLOWED_ORIGINS`（逗号分隔精确 origin），结果继续存入既有 `FrontendOrigins` 字段（router 与 `/api/config` 展示复用）。
- 环境默认值：development/test/未知环境未配置时保留 `http://localhost:5173`、`http://127.0.0.1:5173` 两个 Vite 开发 origin；production 未配置时为空列表（推荐前端与 API 同源部署）。取代原先无条件硬编码。
- 显式 origin 校验（基于 `url.Parse`，非字符串手工拆分）：scheme 仅 http/https 且转小写；必须有 host，host 转小写；允许可选端口且不折叠 `:80`/`:443`；拒绝用户名/密码、path（仅 `/` 规范化为无斜杠）、query、fragment、`*`、`null`、opaque URL；规范化后重复项配置失败；中间空项配置失败；错误只含变量名、条目序号和规则，不回显配置值。精确匹配天然防前缀攻击（`allowed.example.evil`、`allowed.example@evil` 均无法通过）。
- `router.go` 重写 `withCORS`：无 `Origin` 头的请求不加任何 CORS 头直接进入业务；有 `Origin` 时一律安全追加 `Vary: Origin`（`appendVaryOrigin` 不覆盖已有 Vary 值、不重复追加）；允许来源返回精确 `Access-Control-Allow-Origin` + `Access-Control-Allow-Credentials: true`；不允许来源不返回任何允许头，普通请求仍进入业务（与原行为一致，由浏览器阻止读取），OPTIONS 返回 204 但无允许头。
- 预检收紧：仅允许来源的 OPTIONS 返回 `Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS`（前端真实使用全部五种方法）与 `Allow-Headers: Content-Type`（前端唯一自定义头；移除未使用的 `Authorization`）。原实现对所有响应（含非 OPTIONS、不允许来源）无条件输出这两个头的行为已移除。
- 未修改 Cookie 的 `HttpOnly`、`SameSite=Lax`、`Secure` 规则。

### 测试

- 新增 `config/cors_origins_test.go`：development/test/空环境/production 默认值；合法 http/https/带端口/IPv6 literal/尾斜杠规范化/大小写规范化/多值；`*`、`null`、path、query、fragment、用户名、密码、中间空项、规范化后重复、无 scheme、ftp、opaque 全部失败且错误不回显值；相似前缀 origin 规范化后仍互不相同。
- 新增 `api/cors_middleware_test.go`：允许来源精确 ACAO + credentials + `Vary: Origin`；不允许来源（含 `null`、前缀伪造、scheme 不同）无任何允许头但业务照常、Vary 仍设置；无 Origin 请求完全不加 CORS 头；允许来源 OPTIONS 返回收紧的方法/头集合；不允许来源 OPTIONS 无任何允许头；已有 Vary 值不被覆盖、重复追加只产生一个 Origin。
- `go fmt ./...`、`go build ./...`、`go vet ./...`、`go test ./internal/config ./internal/api` 全部通过（api 包全量含既有管理员/查询 Cookie 接口回归）。
- 未读取真实 `.env`，未使用子代理，未连接外部服务。

## 阶段 5.6：示例配置与部署文档

- `.env.example` 与 `backend/.env.example` 新增 `SERVER_HOST=127.0.0.1`、空的 `TRUSTED_PROXY_CIDRS=`、空的 `CORS_ALLOWED_ORIGINS=`，注释说明默认仅监听 loopback、显式 `0.0.0.0`/`::` 才全接口、默认不信任代理头、单机代理只建议 loopback CIDR、禁止 `/0`、CORS 禁止通配符与 `null`、production 默认无跨域并推荐同源部署。
- 新增 `docs/internal-network-deployment.md`（本轮唯一新增部署文档）：推荐结构（内网客户端 → HTTPS → 内网域名/内部 DNS → 本机 Nginx/Caddy → Go 后端 127.0.0.1:8080 → PostgreSQL 127.0.0.1:5432）；`SERVER_HOST`/`TRUSTED_PROXY_CIDRS`/`CORS_ALLOWED_ORIGINS` 的完整语义；只支持 `X-Forwarded-For`；反向代理必须覆盖客户端原始 XFF；不要把整个内网网段配置为可信代理；`unknown-client` 统一失败桶说明；部署检查要点。只写建议，未安装或启动任何代理。
- `docs/internal-deployment-secrets.md` 增加指向网络部署文档的交叉引用，未重复网络内容。
- 示例与文档不含真实域名、真实内网 IP、真实 SMTP、真实邮箱、真实密码、真实密钥、可用 DSN、私钥或证书。
- 未修改 `HANDOVER.md`（其中迁移编号已过时一事列为后续文档修复项，不混入本轮）。

## 阶段 5.7：本轮最终完整验证

- `go fmt ./...`、`go build ./...`、`go vet ./...`、`go test ./...`（全部包，count=1）全部通过。
- 定向 verbose 全部通过：`clientip` 16 个、`config` 27 个、`api` 12 个、`querycoderecovery` 12 个、`recoveryemailverification` 18 个测试函数；`query -run 'Login|ChangeQueryCode|Bind|Recovery|Rate|Limit'` 与 `users -run RecoveryEmail` 通过。管理员包无 IP 限流（本轮未新增），未新增管理员限流测试。
- `pnpm run build`（vue-tsc + Vite production build）通过；构建产物在既有忽略目录 `frontend/dist`，未进入 Git 状态。
- 数据库集成测试使用项目既有隔离机制并完成各自清理/回滚；未创建或修改真实业务数据。SMTP 测试仅进程内回环 listener 且随测试结束关闭；未连接真实 SMTP。
- 阶段末检查：`git diff --check` 通过（仅换行规范化警告）；暂存区为空；无删除、无重命名；`go.mod`/`go.sum` 未变，无新增依赖；无新增迁移；8080 与 5173 均未监听。
- 本轮实际修改：`backend/main.go`、`backend/internal/config/config.go`、`backend/internal/api/router.go`、`backend/internal/query/{handler,bindcode,query_code_recovery,ratelimit}.go`、两份 `.env.example`；新增 `backend/internal/clientip/{clientip,clientip_test}.go`、`backend/internal/config/{trusted_proxy,server_host,cors_origins}_test.go`、`backend/internal/query/clientip_injection_test.go`、`backend/internal/api/cors_middleware_test.go`、`docs/internal-network-deployment.md`。
- 未读取真实 `.env`，未处理 `.claude/settings.local.json`，未连接真实 SMTP，未发送真实邮件，未修改真实业务数据，未安装反向代理，未修改 DNS/hosts/防火墙/Windows 服务，未 `git add`/`commit`/`push`，未使用子代理。
- 后续待办（本轮未触及）：管理员登录限流、数据库错误日志脱敏、HANDOVER 迁移编号更新、数据库备份恢复、内网 HTTPS 与真实代理配置、Windows 服务化。

## 阶段 6.1：错误日志路径调查

### 日志入口清单（非测试代码）

| 文件与函数 | 当前日志内容 | 是否直接输出 error | 潜在敏感信息 | 建议处理 |
| --- | --- | --- | --- | --- |
| `main.go` 数据库连接失败 | `connect to database: %v` | 是 | pgx `ConnectError` 文本含 host、user、database（不含密码，但均为敏感部署信息） | 改为安全分类 |
| `main.go` 迁移失败 | `run database migrations: %v` | 是 | PostgreSQL 语法/约束错误可能引用 SQL 片段 | 改为安全分类；失败迁移文件名由 migrations 包单独安全输出 |
| `database/migrations.go` 回滚失败 | `rollback migration transaction: %v` | 是 | 底层 pg 错误 | 改为安全分类 |
| `payments/handler.go:382` CreatePayment 失败 | `cn=%q method=%q ik=%q item_count=%d err=%v` | 是 | **直接记录用户 CN、支付方式原始输入和幂等键，外加原始 DB 错误——本次调查发现的最明确泄露点** | 移除 cn/method/ik 值，err 改为安全分类 |
| `payments/handler.go` 247、1433 | 固定前缀 + `%v` | 是 | 底层 pg 错误 | 改为安全分类 |
| 各 handler 的 store/service 错误（admin 4+1、users 9、export 8、importpreview 6、orders 2、query 12） | 固定操作名 + `%v` | 是 | PostgreSQL 错误 Message 在部分类别（如 22P02 invalid_text_representation）会内联 SQL 参数值，即用户输入（CN、日期串等）可进入日志；`PgError.Error()` 含 Message+SQLSTATE（Detail/Hint 不含） | 改为安全分类 |
| `importpreview/handler.go:104` Excel 解析失败 | `parse xlsx preview: %v` | 是 | excelize 错误可能引用用户文件内的 sheet 名等内容 | 改为安全分类 |
| `query/recovery_email_verification.go:124` default 分支 | `%s: %v` | 是 | 已知 sentinel 已在上层映射，此处只剩底层存储错误 | 改为安全分类 |
| cmd 三个工具的 `connect to database: %v` | 同 main.go | 是 | 同 main.go（host/user/db） | 改为安全分类 |
| `cmd/set-query-code`、`create-admin` 的 find/create/set `%v` | 固定前缀 + `%v` | 是 | 底层 pg 错误 | 改为安全分类；`%q` 回显的 CN/用户名为操作员自己的命令行参数，保留 |
| 各包 `encode ... JSON response: %v` | JSON 编码/网络写失败 | 是 | encoder 错误为类型或网络写错误，不含业务数据 | 不改，理由见下 |
| `config.go` 三处 | `.env not loaded`（相对路径）、全接口警告、HMAC 回退警告 | 部分 | 均只含变量名/固定文本，阶段 3–5 已做脱敏设计 | 不改 |
| `api/router.go` 请求日志 | method、path、耗时 | 否 | 无 query string、无 body、无 IP | 不改 |
| 密码/令牌生成失败（bcrypt、crypto/rand，共 6 处） | 固定前缀 + `%v` | 是 | bcrypt/rand 错误为固定文本，不携带输入 | 不改 |
| `users/recovery_email.go:130` protect、`query/recovery_email.go:72` mask | protector 错误 | 是 | recoveryemail 包错误均为固定 sentinel（unavailable/invalid），不含邮箱明文 | 不改 |
| SMTP sender | 无日志语句 | — | 阶段 4 已统一为受控错误 | 不改 |

### 结论

1. 明确泄露：`payments/handler.go:382` 直接记录 CN 与幂等键。
2. 部署信息泄露：数据库连接失败 `%v`（host/user/db）；迁移失败可能引用 SQL。
3. 条件性泄露：所有 handler 对底层 pg 错误的 `%v`——PostgreSQL 部分错误类别的 Message 会内联参数值（用户输入）。
4. 无需修改并有证据：请求日志无 IP/参数；config 日志只含变量名；sentinel 错误为固定字符串；bcrypt/rand/JSON-encode 错误不含业务数据；SMTP 已在阶段 4 收敛。
5. 无发现记录查询码、验证码、令牌、Cookie、邮箱明文、密钥或完整 DSN（含密码）的日志语句。

## 阶段 6.2：日志分级规则

- 可以记录：操作类型/模块名固定文本、安全错误分类、HTTP 状态、超时/冲突/不可用等类别、SQLSTATE 五位代码、迁移文件名（仓库内已知）。
- 不得记录：`%v` 原始数据库错误、完整 DSN、SQL 与参数、用户输入（CN、支付方式原始值、幂等键、日期串）、完整邮箱、查询码/验证码/令牌、Cookie、SMTP 响应、含用户名的绝对路径。
- 启动失败仍以 `log.Fatal*` 退出并给出安全分类（如 `database connection failed: authentication or connectivity error`），不输出连接目标。
- 所有环境统一脱敏，不做 development 例外——开发日志同样可能被保存或提交。
- 实现方式：新增小型辅助 `logsafe.Category(err) string`，用 `errors.Is`/`errors.As` 分类（context 超时/取消、`pgconn.PgError` 按 SQLSTATE：23505 唯一约束、23503 外键、40001/40P01 串行化/死锁、57014 取消或超时、08 类连接、28 类认证，其余输出 `database error (SQLSTATE xxxxx)`；`pgconn.ConnectError` 固定文本；`net.Error` 网络类），日志处输出固定文本 + 分类，不拼原始 error。错误对象本身照常向上传播/触发既有回滚，仅日志文本改变。

## 阶段 6.3：日志脱敏实施

### 实施内容

- 新增 `backend/internal/logsafe/logsafe.go`：`Category(err error) string`，仅返回固定分类文本（数据库分类含五位 SQLSTATE 代码），永不包含原始错误文本；使用既有 `pgconn` 依赖，无新增依赖。
- `payments/handler.go` CreatePayment 失败日志移除 `cn`、`method`、`ik` 三个用户输入值，仅保留 item_count 与安全分类——本阶段最明确的泄露修复。
- 数据库连接失败（`main.go` 与三个 cmd 工具）与迁移失败改为安全分类；`database/migrations.go` 在迁移失败时单独输出安全的迁移文件名（`database migration failed: <文件名>`），回滚失败日志同样分类化。
- 所有调查确认可能承载 pg/excelize 错误的 handler 日志点统一改为 `logsafe.Category`：payments 3 处、users 9 处、admin 4 处、export 8 处、importpreview 6 处、orders 2 处、query 12 处、cmd 工具 7 处（共约 44 处，全部只改日志格式化，不改控制流）。
- 明确不改（调查有据）：JSON encode/网络写错误、bcrypt/crypto-rand 错误、recoveryemail sentinel 错误、config 已脱敏日志、请求日志——均不携带业务数据。
- 未修改任何 API 响应、业务规则、事务/回滚逻辑、限流、HMAC、数据库结构；无新增迁移；错误仍照常传播并触发既有错误映射。

### 测试

- 新增 `logsafe/logsafe_test.go`（6 个用例）：构造携带敏感标记（假密码、假主机、假用户、SQL 文本、`Key (cn)=(...)`、假邮箱）的 `PgError`（含 Detail/Hint/Where），断言各分类输出不含任何标记；context 超时/取消（含 wrap）分类正确；23505/23503/40001/40P01/57014/08006/28P01/22P02 分类正确；`ConnectError` 输出固定文本；net.Error 超时与一般错误；未知错误回落 `internal error` 且不回显假 DSN；nil 为空串。
- 新增 `query/logsafe_handler_test.go`：登录 store 返回带 `SENSITIVE-CN-INPUT`/`SENSITIVE-QUERY-CODE`/SQL 片段的 PgError 时，日志只输出 `find query user: database error (SQLSTATE 22P02)`，不含任何标记，公开响应保持 500 通用文案；超时错误日志分类为 `timeout` 且响应映射不变。日志捕获使用 `bytes.Buffer`，不写真实日志文件。
- `go fmt ./...`、`go build ./...`、`go vet ./...`、`go test ./...`（全部包，count=1）全部通过；数据库集成测试沿用既有隔离机制并完成回滚/清理。
- `pnpm run build` 通过（前端本阶段无修改，作最终复核构建）。

## 阶段 6.4：阶段 6 最终检查

- `git diff --check` 通过；暂存区为空；无删除、无重命名；`go.mod`/`go.sum` 未变（无新增依赖）；无新增迁移。
- 新增文件均不含真实 DSN、密码、邮箱、密钥；测试敏感值全部为显式假标记（`SENSITIVE-*`、保留地址、`example.invalid`）。
- 8080 与 5173 未监听；无残留测试进程。
- 未读取真实 `.env`，未处理 `.claude/settings.local.json`，未连接真实 SMTP，未修改真实业务数据，未提交、未推送，未使用子代理。
- 阶段 6 完成。等待统一复核与提交指令；后续待办不变：管理员登录限流、HANDOVER 迁移编号更新、数据库备份恢复、内网 HTTPS 与真实代理配置、Windows 服务化。