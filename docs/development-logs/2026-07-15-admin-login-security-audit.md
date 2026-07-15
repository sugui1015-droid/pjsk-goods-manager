# 管理员登录安全审计记录

## 阶段 1：只读调查与安全边界确认

- 基线确认：分支 `main`，`HEAD = origin/main = 3904b02f9fe858c1a96990fa4025a9bc4b1c8caa`；工作区仅有受保护的 `.claude/settings.local.json` 未跟踪，未读取、未修改、未暂存。
- 已读取 `AGENTS.md`、`HANDOVER.md`、开发日志规则、管理员登录 handler/store/session/limiter、现有迁移、`clientip`、`logsafe`、管理员登录与限流测试、普通用户安全审计相关代码；未读取真实 `.env`，未连接或修改正式数据库。
- 管理员登录账号标识：登录输入经 `strings.TrimSpace` 后传入 `FindByUsername`；数据库查找语义是 `lower(btrim(username)) = lower($1)`，限流键用 `normalizeLimiterUsername`（trim + lower）。审计应记录该规范化用户名，成功时可记录 `admin_id` 与数据库中的规范化用户名。
- 登录失败内部原因：当前可区分 `admin_not_found`（`ErrNotFound`）、`account_disabled`（`status != active`）、`invalid_credentials`（bcrypt 不匹配）、`rate_limited`、以及查找/建会话等 `database_error`；对外响应保持统一的 `invalid username or password`、429 文案或通用 500，不泄露内部原因。
- 外部响应现状：未知用户名、禁用账号、密码错误均返回 401 `invalid username or password`；限流返回 429 `too many login attempts, please try again later`；没有对外账号枚举信息。
- 客户端 IP：由 router 创建的 `clientip.Resolver` 解析并注入 admin handler；默认不信任伪造 `X-Forwarded-For`，只在 `TRUSTED_PROXY_CIDRS` 配置可信代理时使用转发链；审计使用已解析的稳定 key。
- request ID：项目当前没有统一 request ID/correlation ID 中间件或上下文键，本阶段不新增全局 request ID，审计表中不设计强制 request_id 字段。
- 登出定位当前管理员：`RequireAuthentication` 先通过 admin session cookie 的 hash 调 `FindBySession`，成功后把 `Admin` 放进 request context，`Logout` 可通过 `CurrentAdmin` 取得管理员身份；无 session 登出在中间件处被 401 拒绝，不进入 `Logout` handler。
- 会话校验失败定位：当前 `FindBySession` 失败只知道 token hash 对应会话无效/过期/账号禁用，不能安全定位具体管理员；为避免高噪声与 token/hash 泄露，本阶段不对每个 `/api/admin/me` 或普通受保护请求失败写数据库审计，仅保留现有安全日志分类。
- 现有审计模型：`account_security_audit_logs` 由找回邮箱/账号安全流程使用，字段要求 `target_user_id not null`，语义是用户账号安全事件，不适合记录匿名管理员登录失败或管理员认证事件；为避免错误复用，应新增专用管理员认证审计表。
- 审计存储位置：采用结构化数据库审计，便于后续查询与排障；应用日志只用于审计写入失败的非敏感分类记录。
- 数据库不可用时登录表现：现有登录查找管理员或创建 session 失败返回 500；本阶段不改变该语义。审计写入失败不应放宽认证，不应泄露内部错误，也不应把失败凭据当成功。
- 审计写入失败策略：登录成功事件若在 session 创建同一事务内写入失败，应阻止登录并回滚 session，避免“成功登录但审计完全丢失”；失败/限流/登出审计写入失败只记录脱敏应用日志，不改变原有认证响应，避免审计系统故障造成全体管理员无法登录或放大 DoS。
- 需要新增迁移：新增专用 `admin_auth_audit_events` 表，避免复用 `account_security_audit_logs` 的用户目标约束。
- 适合记录事件：登录成功、登录失败、登录被限流、登出成功；暂不记录每次 `/admin/me` 成功、无 session 登出、每个受保护路由 session 失败、凭据变更（当前无管理员凭据变更功能）。
- 本阶段不新增审计读取 API：当前任务核心是认证事件落库；完整管理查询界面/API 会扩大范围。后续可基于表新增只读管理员 API。
- 未发现必须先修复的认证安全问题；现有登录限流、统一失败文案、Cookie 行为和 client IP 解析可作为审计接入基础。

## Stage 2 - Implementation draft and documentation

- Added dedicated migration `backend/migrations/0019_admin_auth_audit_events.sql` for `admin_auth_audit_events`.
- Added bounded admin authentication audit event types, validation, sanitization and PostgreSQL storage hooks.
- Login success now writes session creation, `last_login_at`, and success audit row in one database transaction.
- Login failure, rate-limited login and logout now record best-effort audit rows while preserving existing external responses.
- Added in-memory rate-limit audit de-duplication to avoid repeated `admin_login_rate_limited` audit noise for a tight retry loop.
- Added/updated tests for handler audit behavior, audit validation, limiter de-duplication and affected admin auth stubs in other packages.
- Added `docs/admin-auth-security-audit.md` and updated `HANDOVER.md` with operational notes.
- Did not read real `.env`, did not read or process `.claude/settings.local.json`, did not connect to any database, and did not modify real business data.

Verification after this stage:

- Pending: Go formatting/build/vet/tests and final security scan.
## Stage 3 - Automated verification and adjustment

- First full `go build ./...`, `go vet ./...`, `go test ./...` run found API integration tests needed the new audit table in their isolated test setup; the failing login returned 500 because `admin_auth_audit_events` was missing in the test database.
- Added the audit table setup to the API test helper and cleaned API test audit rows by synthetic username prefix.
- Re-ran `go fmt ./...` with a temporary local `GOCACHE` because the default Go cache path was not writable in this environment.
- `go build ./...`: passed.
- `go vet ./...`: passed.
- `go test ./...`: passed.
- `go test ./internal/admin -count=10`: passed.
- `go test ./internal/api -run 'AdminLogin|AdminExport' -count=5`: passed.
- `go test -race ./internal/admin`: environment failure `0xc0000139`; control `go test -race ./internal/logsafe` failed with the same `0xc0000139`, so this is recorded as local race runtime unavailable rather than a package assertion failure.
- `git diff --check`: passed after this stage.
- No database migration was applied to a real database, no service was started, no real business data was read or modified, and no real `.env` or `.claude/settings.local.json` was read.
## Stage 4 - Pre-commit final review

- Branch: `main`.
- Pre-commit base: `3904b02f9fe858c1a96990fa4025a9bc4b1c8caa`; `origin/main` matched the same commit.
- Staging area was empty before staging.
- Final non-race verification passed: `go build ./...`, `go vet ./...`, `go test ./...`, `go test ./internal/admin -count=10`, and `go test ./internal/api -run 'AdminLogin|AdminExport' -count=5`.
- Race verification could not run in this Windows environment: `go test -race ./internal/admin` and control `go test -race ./internal/logsafe` both failed with `0xc0000139`.
- Sensitive pattern check over the intended files found no private key/PEM, password DSN, `DATABASE_URL=` assignment, `PGPASSWORD=` assignment, recovery/SMTP secret assignment, literal Authorization header, or literal Cookie header.
- `.claude/settings.local.json` remained untracked and was not read, modified, staged, or committed.
- No real `.env` was read or modified by manual inspection, no production database was connected or modified, and no real business data was changed.
- Intended commit scope: admin auth audit migration, admin audit implementation/tests, API test setup compatibility, admin auth audit documentation, handover note, and this development log.
## Stage 5 - Staging blocked by local Git metadata permission

- Attempted explicit `git add --` for the intended admin authentication audit file list only; did not use `git add .` or `git add -A`.
- Staging failed before any index change: Git could not create `D:/pjsk/.git/index.lock` due to `Permission denied`.
- Read-only check confirmed `.git/index.lock` did not already exist at the time of inspection.
- No files were staged, no commit was created, and nothing was pushed.
- This appears to be a local execution-environment permission block on writing Git metadata, not a repository content failure.