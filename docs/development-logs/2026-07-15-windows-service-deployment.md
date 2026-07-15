# Windows 服务化部署方案与示例配置

## 阶段 1：只读调查

### 基线

- 分支 `main`；`HEAD` = `origin/main` = `6ebd87b690249c2396bd3b578d79c55fc2994442`；工作区、暂存区干净。
- 只读检查了 `AGENTS.md`、`HANDOVER.md`、`docs/internal-https-reverse-proxy.md`、`docs/internal-deployment-secrets.md`、`.env.example`、`backend/.env.example`、`backend/run.cmd`、`backend/main.go`、`backend/go.mod`、`frontend/package.json`、`deploy/` 结构、`.gitignore`。未连接数据库、未读取真实 `.env`/密钥。

### 调查结论

1. **后端运行方式**：`backend/main.go` 是 `package main`（模块 `pjsk/backend`，入口即 `backend/` 根目录，**不是 `cmd/` 子目录**——`cmd/` 下是 create-admin 等 CLI 工具）。适合编译成独立 `.exe` 运行：`go build -trimpath -o ./bin/pjsk-backend.exe .`（已实测通过，产物 ~17MB，位于已被 gitignore 的 `backend/bin/`）。当前 `run.cmd` 用 `go run .` 仅适合开发，**服务化不应用 `go run`**。
2. **后端运行时环境变量**（`config.go` 实际读取）：数据库（`DATABASE_URL` 或 `DATABASE_HOST/PORT/USER/PASSWORD/NAME/SSLMODE`）、`SERVER_HOST`、`APP_PORT`、`APP_ENV`、`ADMIN_SESSION_TTL`、`ADMIN_COOKIE_SECURE`、`TRUSTED_PROXY_CIDRS`、`CORS_ALLOWED_ORIGINS`、`RECOVERY_EMAIL_ENCRYPTION_KEY`、`RECOVERY_EMAIL_HMAC_KEY`、`RECOVERY_EMAIL_VERIFICATION_HMAC_KEY`、`QUERY_CODE_RECOVERY_HMAC_KEY`、`RECOVERY_EMAIL_SENDER_MODE` 及 SMTP 系列。
3. **前端**：`package.json` 的 `build` = `vue-tsc -b && vite build`，产物 `frontend/dist`，锁文件为 `pnpm-lock.yaml`（`--frozen-lockfile` 适用）。**正式部署只需提前构建静态文件，不需要长期运行 Node/Vite 服务**，5173 不注册为服务。
4. **Caddy 是否适合单独做 Windows 服务**：适合。Caddy 是独立可执行文件，可作为 `pjsk-caddy` 服务，与后端 `pjsk-backend` 分离；配置对齐已有 `deploy/caddy/Caddyfile.example`。
5. **工作目录**：后端 `config.Load()` 调用 `godotenv.Load()` 从**当前工作目录**读取 `.env`（缺失时仅记录 `.env not loaded` 并继续，可改由服务环境块注入变量）；migrations 通过 `//go:embed migrations/*.sql` **嵌入二进制**，不依赖工作目录。因此后端服务工作目录应设为存放受保护 `secrets\backend.env` 的目录（或改用服务环境块）；Caddy 工作目录设为其配置/证书所在目录。
6. **日志**：后端用标准库 `log` 输出到 stderr，`logsafe` 已对数据库错误脱敏，既往阶段已确认不打印密码/令牌/验证码。建议**保持标准输出/错误，由服务包装器（WinSW/NSSM）接管落盘并滚动**，而非改代码写文件或 Event Log。
7. **启动依赖**：后端依赖 PostgreSQL（`main.go` 启动即 `database.Connect` + 迁移，连接失败 `log.Fatal` 退出）。应在服务层说明"PostgreSQL 先就绪"，但**不由本项目脚本创建或改动 PostgreSQL 服务**；用失败重启 + 退避让后端等待数据库可用。Caddy 可先启动，后端未就绪时 API 暂时返回代理错误。
8. **是否存在服务化代码阻断**：**无阻断。**
   - 非交互式启动：`main()` 无终端交互，可作为服务进程运行。✅
   - 工作目录/相对路径：迁移已嵌入；`.env` 可用服务环境块替代或把工作目录设到密钥目录，可稳定设置。✅
   - 停止信号：`main()` **未实现** `signal.Notify`/`http.Server.Shutdown` 优雅关闭，但服务包装器可强制终止进程（进程能被停止，不满足"无法停止"的阻断定义）；HTTP 请求短、无长连接，强制终止可接受。→ 记为**可选增强，非阻断，本轮不改代码**。
   - 日志泄露：既往阶段确认无密码/令牌/验证码明文输出。✅
   - 健康检查：`/health` 可用于服务监控。✅

### 结论

无服务化代码阻断 → **本阶段为纯文档 + 示例配置，不修改任何 Go/前端业务代码**。优雅关闭作为未决的可选增强记录，不在本轮实现（避免超范围）。

### Git 状态

- 阶段末仅新增本日志；`git diff --check` 干净；暂存区空；无删除、无重命名。构建产物 `backend/bin/pjsk-backend.exe` 已被 `.gitignore` 忽略，不进入 Git。

## 阶段 2：Windows 服务化主文档

- 新增 `docs/windows-service-deployment.md`，结合真实入口/目录/命令编写：推荐运行结构（哪些进 Git、哪些只在部署机）、服务拆分（`pjsk-backend`/`pjsk-caddy`/保留 PostgreSQL 现有服务）、WinSW 首选方案与真实环境文件支持、NSSM 备选命令模板（均标"示例，当前未执行"）、后端/前端构建发布与升级回滚、密钥与环境变量管理、服务账户比较与推荐、日志与故障恢复、安装/升级/回滚/卸载人工步骤、安全边界。

## 阶段 3：示例文件

- 新增 `deploy/windows-service/`：`backend-winsw.xml.example`、`caddy-winsw.xml.example`、`Start-PjskBackend.example.ps1`、`README.md`，全部顶部标注"示例，禁止直接用于生产"，仅占位值。
- WinSW XML：服务 id/name/描述、exe 路径、工作目录、日志滚动、`stoptimeout`、`Automatic`、失败重启 + 退避（`onfailure` 多级 delay + `resetfailure`），非敏感 `<env>` 占位；密钥策略明确走工作目录下受 ACL 保护的 `backend.env`（后端 godotenv 读取），不写进 XML。PostgreSQL `<depend>` 因服务名未知而注释、不指向不存在的名字。
- 包装脚本默认只检查（`-CheckOnly` 语义，无 `-Run` 不启动）；严格错误处理、验证 exe 与环境文件存在、设置工作目录、启动返回退出码；**不安装服务、不提权、不下载、不改系统环境变量、不读取或回显环境文件内容**。
- Caddy 服务示例与 `deploy/caddy/Caddyfile.example` 对齐。

## 阶段 4：最小代码修复

- **未修改任何业务代码。** 阶段 1 已确认无服务化阻断（非交互启动、迁移嵌入、`.env` 可由工作目录或环境块提供、force-kill 可停止、日志已脱敏、`/health` 可用）。优雅关闭（`signal.Notify` + `http.Server.Shutdown`）记为可选增强，本轮不实现以免超范围。

## 阶段 5：离线验证

- `git diff --check` 干净。
- 后端入口 `go list` 确认为 `main pjsk/backend`；实测 `go build -trimpath -o ./bin/pjsk-backend.exe .` 成功（产物 ~17MB，`backend/bin/` 已 gitignore），验证文档构建命令指向真实入口、不指向 `cmd/`。
- 前端 `package.json` 确认 `build = vue-tsc -b && vite build`、锁文件 `pnpm-lock.yaml` 存在（`--frozen-lockfile` 适用）；未重复完整构建。
- PowerShell 示例脚本：`Parser::ParseFile` 语法解析通过；两份 WinSW XML `[xml]` 解析通过（**未经真实 WinSW 版本语义验证**，本机未安装 WinSW，未下载安装）。
- 脚本离线失败路径（占位临时目录，不接触真实密钥）：缺 exe → exit 1；缺环境文件 → exit 1；exe+env 齐备且默认 check-only → exit 0 且**不启动进程、不回显环境文件内容**（注入的假密钥标记 `SHOULD_NOT_APPEAR_SECRET` 未出现在输出，leak=False）。
- Caddy/Nginx 未安装，未做真实语法解析、未启动。
- 敏感信息扫描：本轮文件仅占位值，无数据库/邮箱密码、API/会话/加密/HMAC 密钥、证书私钥、真实 DSN、真实 `.env`、`.dump`/`.backup`/`.local-secrets`。

## 阶段 6：交接与日志收尾

- 更新 `HANDOVER.md` 第 15 节：补充 Windows 服务化指南与示例的完成状态、真实构建入口、密钥策略、无代码改动、未决事项（注册服务/建账户/设 ACL）。
- 未删除、移动或重命名任何历史日志；仅追加。

### 安全边界确认

- 未连接或修改正式数据库 `pjsk`，未运行迁移。
- 未读取真实 `.env`/`backend/.env`/`.claude/settings.local.json`，未读取或输出任何密钥/证书/密码/环境文件内容。
- 未安装 WinSW/NSSM/Caddy/Nginx；未创建、删除、启动、停止或修改任何 Windows 服务；未创建账户、未改 ACL/注册表/计划任务/防火墙/hosts/DNS/系统环境变量。
- 未启停现有前后端或 PostgreSQL；未重启电脑。
- 未使用子代理。构建仅产出被 gitignore 的本地 exe，不进入 Git。
