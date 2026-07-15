# 部署前总验收与交接收口

## 验收边界

- 日期：2026-07-15
- 仅执行只读调查、离线验证、文档校对和必要的最小修正。
- 未连接或查询正式数据库，未运行正式迁移，未读取真实 `.env` 或任何真实密钥。
- 未安装或操作反向代理、证书、Windows 服务、防火墙、DNS、ACL 或公网入口。

## 阶段 1：Git 与文件完整性

- 分支：`main`
- 起始 `HEAD`：`4edb307e741720bebb5c7b0c23b84dca5ab125df`
- 起始 `origin/main`：`4edb307e741720bebb5c7b0c23b84dca5ab125df`
- 验收开始时工作区、暂存区均干净，`git diff --check` 通过。
- 最近 10 个提交连续，最新提交为 `4edb307 feat: add admin authentication audit`，未发现意外改写历史的迹象。
- 任务列出的关键交接、部署、备份、服务化和 `0019` 迁移文件均存在。
- 未发现未跟踪的 `.dump`、`.backup`、`.partial` 或测试 fixture；`frontend/dist`、数据库备份后缀和 `.env` 已由 Git 忽略。
- `.claude/settings.local.json` 未进入 Git 跟踪；本轮未读取或修改该文件。

## 阶段 2：迁移链复核

- `backend/main.go` 使用 `//go:embed migrations/*.sql`，启动迁移加载会包含 `0019_admin_auth_audit_events.sql`。
- 迁移器按完整文件名排序并以完整文件名写入 `schema_migrations.version`；`0019` 会按现有机制被识别并执行。
- 当前最大编号为 `0019`；`0019` 的表、索引、约束、事件类型、结果与原因枚举和后端审计写入代码一致。
- 基本 SQL 结构检查未发现明显语法问题；实际 PostgreSQL 执行结果未在本阶段验证。
- **发现的部署前风险**：迁移编号不连续且不唯一，存在 `0005_import_history.sql` 与 `0005_product_series.sql` 两个 `0005`，并缺少 `0006`。现有迁移器以完整文件名识别，两文件仍会分别执行，但这不满足“编号连续、无重复编号”的验收要求。
- 未重命名任何历史迁移：对已经应用过迁移的环境改名会令迁移器把它视为新文件，可能重复执行。真实部署前必须人工核对目标数据库 `schema_migrations`，并另行制定向后兼容的修复方案。
- 正式数据库是否已应用 `0019`：未知；本阶段按安全边界未连接或查询正式数据库，也未执行任何迁移。

## 阶段 3：后端完整验证

- `go fmt ./...`：通过，未产生 Go 源码差异。
- `go build ./...`：通过。
- `go vet ./...`：通过。
- `go test ./...`：通过；所有有测试的后端包均通过。
- `go test ./internal/admin -count=10`：通过。
- `go test ./internal/query -count=10`：通过。
- `go test ./internal/api -count=5`：通过。
- `go test ./internal/payments -count=5`：通过。
- `go test ./internal/users -count=5`：通过。
- 全量及专项测试覆盖管理员认证审计、查询登录限流上限、邮箱找回与绑定流程、付款创建/详情/撤销/导出、管理员和普通用户认证以及 `/health`、`/api/config` 相关行为，未发现回归。
- 本阶段未运行 `-race`；既有交接信息已将 Windows 本机 Go/MinGW race runtime 的 `0xc0000139` 记录为环境限制，本轮未安装额外工具，也未将其伪报为通过。

## 阶段 4：前端完整验证

- `frontend/package.json`、`pnpm-lock.yaml` 与 Vite 配置结构正常；构建脚本为 `vue-tsc -b && vite build`。
- 生产 API 基址默认空，使用同源相对 `/api` 与 `/health`；`VITE_API_BASE_URL` 仅用于非开发构建的可选跨源覆盖。
- 5173 仅用于 Vite 开发服务器及本地回退展示信息，不是生产服务入口；正式产物目录为 `frontend/dist`。
- 首次从仓库根目录误执行 `pnpm.cmd run build`，因该目录无 `package.json` 退出 1，脚本未进入项目构建；随后在任务指定的 `frontend` 目录重跑。
- `pnpm.cmd run build`（`D:\pjsk\frontend`）：通过；`vue-tsc` 与 Vite 均成功，`frontend/dist` 已生成。
- `frontend/dist` 被 Git 忽略，构建后没有产物进入版本状态；正式部署不使用 `pnpm dev`。

## 阶段 5：备份和恢复工具复核

- 直接调用脚本首次被本机 PowerShell 数字签名执行策略拦截，脚本未运行；随后仅对一次子进程使用 `-ExecutionPolicy Bypass`，未修改系统执行策略。
- `Invoke-ScriptSafetyTests.ps1`：原备份/恢复安全测试 48 项通过；保留策略安全测试 54 项通过；0 失败；总退出码 0。
- 全部测试在脚本自建的隔离临时根目录执行，没有连接数据库、扫描真实备份目录、删除真实备份或修改演练证据。
- 默认清理仍为 DryRun；测试结束后 throwaway test root 已删除。
- 工作区未残留 fixture、报告、`.dump`、`.backup` 或 `.partial` 文件。

## 阶段 6：环境变量完整性复核

- 代码实际读取的运行变量已逐项与根 `.env.example`、`backend/.env.example`、HANDOVER 和正式部署文档对照。
- 数据库支持两种完整配置：优先使用非空 `DATABASE_URL`，否则使用 `DATABASE_HOST/PORT/USER/PASSWORD/NAME/SSLMODE`；文档已说明优先级。
- 监听端口优先级为 `APP_PORT` > `SERVER_PORT` > `BACKEND_PORT` > `8080`；正式部署统一建议 `APP_PORT=8080`。
- `SERVER_HOST=127.0.0.1`、`TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128`、同源 `CORS_ALLOWED_ORIGINS=` 与 HTTPS 下 `ADMIN_COOKIE_SECURE=true` 的生产建议一致。
- 管理员和查询会话令牌由程序随机生成并只存哈希，没有额外的静态 session secret 环境变量；`ADMIN_SESSION_TTL` 控制管理员会话时长。
- 找回邮箱 AES/HMAC、邮箱验证 HMAC、查询码找回 HMAC、sender mode 与全部 SMTP 变量均在示例中覆盖；独立性、Base64 长度、成对配置及 production 必填条件与代码一致。
- 前端 `VITE_API_BASE_URL` 为可选生产构建覆盖；推荐同源部署保持空值。
- 未发现代码读取而示例/部署文档完全遗漏的变量，也未发现文档继续要求代码已删除的变量。
- 未读取任何真实 `.env`；仅核对示例中的变量名、空占位、默认值、格式和必填条件。

## 阶段 7：部署文档一致性复核

- Caddy、Nginx、Windows 服务、WinSW、密钥、备份恢复及 HANDOVER 的部署边界一致：后端只监听 `127.0.0.1:8080`，正式入口为 443（必要时 80 仅跳转），不开放 5173 或 5432。
- Caddy 与 Nginx 均将 `/api/*`、`/health` 代理到后端，并从 `frontend/dist` 提供 SPA；CORS 同源留空、Secure Cookie 与可信代理建议一致。
- 内部 CA 信任、证书/私钥不进 Git、专用低权限服务账户、密钥不进 XML/命令行/日志、备份与日志目录不进 Git 的说明一致。
- 发现并最小修正日志路径不一致：Caddy/Nginx/HTTPS 文档原写 `secrets\\logs`，已统一到 Windows 服务文档和 WinSW 示例采用的 `runtime\\logs`；`secrets` 继续只承载密钥/证书。
- 修正 HANDOVER 两处过时状态：删除已不存在的 `JWT_SECRET` 描述，明确两种数据库配置均受支持；迁移最大编号从 `0018` 更新为 `0019`。
- 未安装、配置或启动 Caddy/Nginx/WinSW/NSSM，未申请证书，也未修改任何系统设置。

## 阶段 8：敏感信息扫描

- 扫描范围包括 Git 已跟踪文件、当前修改文件、未跟踪文件名及敏感后缀；真实 `.env`、本地设置和构建目录只核对 Git 状态，未读取内容。
- 未发现私钥/PEM/证书正文、真实凭据 DSN、非空 `DATABASE_URL`/`PGPASSWORD`、SMTP 密码、真实加密/HMAC/session token、Authorization/Cookie 头或恢复令牌。
- 候选命中均已分类：`logsafe_test.go` 中的密码 DSN 是脱敏单元测试假值；`CHANGE_ME`、空值和中文说明是文档占位；`pjsk.internal.example`、`192.168.1.10`、`203.0.113.0/24` 等是示例/保留测试地址。
- Git 跟踪的敏感命名文件仅为 `.env.example` 与 `backend/.env.example`；未跟踪或已跟踪范围均无 `.dump`、`.backup`、`.partial`、`.local-secrets`、证书或私钥文件。
- `.claude/settings.local.json` 未被 Git 跟踪，本轮未读取或修改。

## 本轮修改与未执行事项

- 未修改 Go、Vue、SQL 或测试代码；仅新增本验收日志，并最小修正部署示例/文档与 HANDOVER。
- 未连接或查询正式数据库，未运行正式迁移，未读取真实密钥，未修改真实业务数据。
- 未执行真实备份/恢复或 `-Execute` 清理，未安装反向代理，未申请证书，未注册 Windows 服务。
- 未修改防火墙、DNS、hosts、注册表、计划任务、ACL、系统环境变量或公网入口。
- 未启动、停止或重启现有前端、后端或 PostgreSQL 进程。
- 未使用子代理，未执行强制推送。

## 验收结论与提交前 Git 状态

- 后端、前端、备份工具、环境变量、部署文档和敏感信息的离线检查均达到本阶段预期。
- **部署门槛未全部通过**：历史迁移编号存在重复 `0005`、缺少 `0006`，不满足编号连续唯一的验收项。现有按完整文件名加载的机制可分别识别两文件，但正式部署前仍需人工核对目标库 `schema_migrations` 并评审兼容处理方案。
- 正式数据库是否已应用 `0019` 仍为未知；这不是离线阶段可安全确认的事项，必须在获准的部署窗口由人工核验。
- 提交前工作区仅包含 4 个已修改文档/示例和本验收日志；暂存区为空；没有历史开发日志删除、移动或重命名。
- `git diff --check` 通过；拟使用提交标题 `docs: record pre-deployment verification`，随后普通推送到 `origin/main`。
- 最终提交 SHA、推送结果以及提交后的工作区/暂存区状态在任务最终汇报中给出。

### 真实部署前仍需人工完成

1. 在受控窗口核对目标 PostgreSQL 的备份可恢复性及 `schema_migrations` 全量记录，特别是两个 `0005` 与 `0019`。
2. 单独评审历史迁移编号风险的兼容处理；不得直接重命名已应用迁移或手改 `schema_migrations`。
3. 生成并安全注入正式数据库、加密/HMAC、SMTP 等密钥配置，设置严格 ACL，且不进入 Git/日志/命令行。
4. 构建发布产物，安装并配置反向代理和 Windows 服务，申请/配置内部证书及向客户端分发 CA 信任。
5. 配置防火墙/DNS/服务账户权限和备份目录，确认只开放 443（可选 80 跳转），8080/5173/5432 不对普通局域网开放。
6. 部署后执行迁移、`/health`、`/api/config`、HTTPS、管理员/普通用户登录及关键业务烟雾验收，并准备回滚。
