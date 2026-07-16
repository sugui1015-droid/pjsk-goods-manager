# Windows 服务化部署指南

本文针对本项目在**局域网内、单台 Windows 主机**上以 Windows 服务方式长期运行，给出后端与反向代理的服务化方案、发布/升级/回滚流程与示例配置。所有域名、路径、账户、密钥均为占位符，**不含任何真实值**。

关联文档：[internal-https-reverse-proxy.md](internal-https-reverse-proxy.md)（反代与 HTTPS）、[internal-deployment-secrets.md](internal-deployment-secrets.md)（密钥生成与保存）、[internal-network-deployment.md](internal-network-deployment.md)（`SERVER_HOST`/`TRUSTED_PROXY_CIDRS`/CORS 语义）、[database-backup-restore.md](database-backup-restore.md)（备份恢复）。

> 占位约定：域名 `pjsk.internal.example`、待替换值 `CHANGE_ME`、密钥文件 `D:\pjsk\secrets\backend.env`、日志目录 `D:\pjsk\runtime\logs`、服务账户 `LocalSystem`/`NetworkService`/专用账户仅作说明。请在实际部署时替换，切勿把真实值提交 Git。
>
> **本文正文中的安装、注册、启动、账户、ACL、防火墙命令均为通用示例。** 实际部署状态（2026-07-16）：后端 `pjsk-backend` 已在本机由 **NSSM** 以 `NT AUTHORITY\LocalService` 服务化运行（本次使用的 WinSW v3 系列二进制在受限服务账户下启动失败，见 §3 注意事项；完整取证与实际参数见 [development-logs/2026-07-16-windows-service-deployment.md](development-logs/2026-07-16-windows-service-deployment.md)）。`pjsk-caddy` 反代服务仍未部署。

---

## 1. 推荐运行结构

```text
D:\pjsk\
├─ backend\
│  ├─ bin\
│  │  └─ pjsk-backend.exe          # 编译产物（不进 Git，backend\bin\ 已 gitignore）
│  └─ migrations\                  # 已 //go:embed 进 exe，运行时不依赖本目录
├─ frontend\
│  └─ dist\                        # 正式前端静态产物（构建生成）
├─ deploy\
│  ├─ caddy\                       # Caddyfile 示例（进 Git）
│  └─ windows-service\             # 服务示例（进 Git，仅占位值）
├─ runtime\                        # 仅部署机存在（不进 Git）
│  ├─ logs\                        # 后端 / Caddy 服务日志
│  └─ temp\
└─ secrets\                        # 仅部署机存在（不进 Git，严格 ACL）
   ├─ backend.env                  # 后端真实环境变量与密钥
   └─ ...证书 / pgpass 等
```

- **可纳入 Git**：`deploy/` 下示例配置（占位值）、`docs/`、源码。
- **只能存在于部署机、禁止进 Git**：`secrets\`（密钥、证书私钥、`backend.env`）、`runtime\logs`（运行日志）、数据库备份、`backend\bin\pjsk-backend.exe`、任何真实 `.env`。`.gitignore` 已忽略 `.env`、`*.log`、`backend/bin/`、`*.dump`/`*.backup` 等。
- **服务运行时不依赖 Vite 开发服务器**；正式前端由 `frontend/dist` 静态提供（经 Caddy/Nginx）。

---

## 2. 推荐服务拆分

| 服务 | 内容 | 由本项目管理 |
| --- | --- | --- |
| `pjsk-backend` | Go 后端 `pjsk-backend.exe`，仅监听 `127.0.0.1:8080` | 是（本文示例） |
| `pjsk-caddy` | Caddy：HTTPS 443 + 静态前端 + 反代 `/api`、`/health` | 是（本文示例） |
| PostgreSQL | 数据库，保留其**现有独立服务** | **否**——不由本项目脚本创建、启动、停止或改配置 |

**服务关系**：

- **启动顺序**：PostgreSQL 应先就绪。Caddy 可先于后端启动，此时 API 暂时返回代理错误（502/503），后端就绪后恢复；静态前端不受影响。
- **后端依赖 PostgreSQL**：`main.go` 启动即连接数据库并执行嵌入式迁移，连接失败会退出。**不要用脚本自动改 PostgreSQL 服务**；改用"失败自动重启 + 退避"让后端等待数据库可用。
- **恢复策略**：建议为 `pjsk-backend` 配置失败重启（见下），但必须设置**重启退避/重置窗口**，避免数据库长期不可用时进程**快速无限重启**（刷爆日志、占用 CPU）。示例中给出 `resetfailure`/延迟重启。
- 不建议开启无条件即时重启；应有重启间隔与最大次数考量。

---

## 3. WinSW 推荐方案（首选）

**优先推荐 WinSW**，理由：配置为可审查的 XML（服务 ID/名称/工作目录/环境变量/日志策略/失败重启都在文件里，可随部署模板管理与评审）；支持失败重启与日志滚动；无需把密码放到服务命令行参数。

> **本机实测注意（2026-07-16）**：本次使用的 WinSW v3 系列二进制（文件版本 3.0.0.0）以受限账户 `NT AUTHORITY\LocalService` 运行时，进入 service mode 约 97 毫秒即 `FATAL: Failed to open the service. 拒绝访问`，失败发生在后端子进程启动之前（后端无日志、8080 未监听、数据库未变）。表现与 WinSW v3 打开自身服务对象时请求过高访问权限、受限账户被拒的已知问题一致（<https://github.com/winsw/winsw/issues/872>）。本机因此改用 §4 的 NSSM 方案完成部署。若要在受限账户下使用 WinSW，先在目标版本上验证该问题已修复；**不要**用扩大服务 SDDL、授予服务账户完全访问或改用 `LocalSystem` 绕过。

完整示例见 [deploy/windows-service/backend-winsw.xml.example](../deploy/windows-service/backend-winsw.xml.example) 与 [caddy-winsw.xml.example](../deploy/windows-service/caddy-winsw.xml.example)。要点字段：服务 `id`/`name`/`description`、`executable`、`workingdirectory`、`arguments`、`log`（`logpath` + 滚动模式）、`stoptimeout`、`startmode=Automatic`、`onfailure` 重启 + `resetfailure`、`env` 环境变量、依赖说明。

**WinSW 对环境文件的真实支持**（不虚构功能）：WinSW 通过 XML 的 `<env name="X" value="Y"/>` 设置进程环境变量，**并不原生解析 dotenv `.env` 文件**。因此对密钥有两种可验证做法：

- **做法 A（推荐，利用后端自身能力）**：把 `workingdirectory` 设为存放受保护 `secrets\backend.env` 的目录，后端 `config.Load()` 会用 `godotenv.Load()` 从工作目录读取该 `.env`。这样密钥只存在于 ACL 受限的本地文件，**不进入 XML、不进入命令行、不进入 Git**。XML 里只放非敏感变量（如 `APP_ENV`）或干脆不放。
- **做法 B**：在 XML `<env>` 中直接写变量。**仅当** XML 文件本身受严格 ACL 保护且绝不进 Git 时才可用于密钥；一般只用于非敏感变量。
- 需要更复杂加载时可用受限权限的包装脚本读取本地环境文件后再启动，但不要在脚本或日志中回显变量值。

> 若使用的 WinSW 版本对某字段语义存疑，按该版本**官方文档核对**，不要照搬本示例的字段名想当然使用。

---

## 4. NSSM 方案（本机实际采用，2026-07-16 部署并验收通过）

因 WinSW v3 受限账户失败（见 §3 注意事项），本机后端最终采用 **NSSM 2.24-101-g897c7ad**（nssm.cc 官方下载，zip 经 nssm.cc SHA-1 与 Microsoft winget 清单 SHA-256 双源核验；Windows 10 Creators Update 及更新系统官方建议用该预发布而非 2014 年的 2.24 稳定版）。仍然**不得自动下载 NSSM**；真实密钥只放工作目录下受 ACL 保护的 `.env`，不写入 NSSM 注册表参数。以下参数与本机实际配置一致：

```powershell
# 与本机实际部署一致（管理员 PowerShell；nssm.exe 位于仓库外服务目录）
nssm install pjsk-backend "D:\pjsk\backend\bin\pjsk-backend.exe"
nssm set pjsk-backend DisplayName "PJSK Goods Manager Backend"
# 工作目录：含受 ACL 保护的 .env，由后端 godotenv 读取，密钥不进注册表
nssm set pjsk-backend AppDirectory "D:\pjsk\backend"
# 知名低权限账户，无需密码；不要用 LocalSystem
nssm set pjsk-backend ObjectName "NT AUTHORITY\LocalService"
nssm set pjsk-backend Start SERVICE_AUTO_START
nssm set pjsk-backend DependOnService postgresql-x64-18
# stdout/stderr 落盘（后端标准库日志走 stderr，backend-out.log 为空属正常）
nssm set pjsk-backend AppStdout "D:\PJSK-Runtime\logs\backend\backend-out.log"
nssm set pjsk-backend AppStderr "D:\PJSK-Runtime\logs\backend\backend-err.log"
# 日志滚动：每日或 10 MB；注意 NSSM 无保留份数上限，日志目录需定期清理
nssm set pjsk-backend AppRotateFiles 1
nssm set pjsk-backend AppRotateOnline 1
nssm set pjsk-backend AppRotateSeconds 86400
nssm set pjsk-backend AppRotateBytes 10485760
# 异常退出自动重启 + 节流（固定延迟，非递增退避）
nssm set pjsk-backend AppExit Default Restart
nssm set pjsk-backend AppRestartDelay 5000
nssm set pjsk-backend AppThrottle 10000
# 状态 / 停止 / 卸载
nssm status pjsk-backend
nssm stop pjsk-backend
nssm remove pjsk-backend confirm
```

验收结果（2026-07-16，详见开发日志）：首次启动 Running、`/health` 200、`database=connected`、无迁移重放；受控停止/重新启动通过；强制终止后端子进程后 NSSM 按 5 秒延迟自动拉起新进程（恢复约 15.9 秒）、health 恢复 200。真实开机自启未做重启验证。

---

## 5. 后端构建与发布流程

后端入口是 `pjsk/backend` 的 `package main`（`backend/` 根目录，**不是 `cmd/`**）。发布用编译产物，不用 `go run`。

```powershell
Set-Location D:\pjsk\backend
go test ./...
go vet ./...
go build -trimpath -o .\bin\pjsk-backend.exe .
```

- **发布前检查**：`go test ./...`、`go vet ./...` 通过（数据库集成测试需要可达的隔离测试库；纯发布可只 build）。
- **保留旧版本**：替换前把现役 `pjsk-backend.exe` 复制为带时间戳的备份（如 `bin\pjsk-backend.<yyyyMMdd-HHmmss>.exe`），便于回滚；**不得自动覆盖唯一可用版本**。
- **原子替换**：先把新 exe 构建到 `bin\pjsk-backend.new.exe` 校验，再：停止 `pjsk-backend` 服务 → 备份现役 exe → `Move-Item` 覆盖 → 启动服务。
- **更新前停服**：exe 被服务占用时无法替换，必须先 `Stop-Service pjsk-backend`（示例，按实际执行）。
- **回滚**：新版本启动或 `/health` 失败时，停服 → 用备份 exe 覆盖 → 启动 → 复查。

---

## 6. 前端构建与发布流程

```powershell
Set-Location D:\pjsk\frontend
pnpm.cmd install --frozen-lockfile   # 锁文件 pnpm-lock.yaml 存在，用 frozen 保证可复现
pnpm.cmd run build                   # vue-tsc -b && vite build → frontend\dist
```

- **正式产物是 `frontend/dist`**；`pnpm dev` / 5173 **不注册为 Windows 服务**。
- `VITE_API_BASE_URL`：同源反代部署保持**空**（相对路径，同域名同端口）；仅当前后端不同源时按 [internal-https-reverse-proxy.md](internal-https-reverse-proxy.md) 设置。
- **替换与回滚**：更新前把现役 `dist` 复制为 `dist.<时间戳>` 备份；构建新 `dist` 校验后替换；异常时用备份目录还原。前端是静态文件，替换不需停后端，但建议在低峰执行。

---

## 7. 密钥与环境变量管理

后端所需变量（以 `config.go` 与 `backend/.env.example` 为准，**只列名与占位，不含真实值**）：

- 数据库：`DATABASE_URL`（或 `DATABASE_HOST/PORT/USER/PASSWORD/NAME/SSLMODE`）
- 监听与代理：`SERVER_HOST=127.0.0.1`、`APP_PORT=8080`、`TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128`、`CORS_ALLOWED_ORIGINS=`（同源留空）
- 会话/Cookie：`ADMIN_SESSION_TTL`、`ADMIN_COOKIE_SECURE=true`（HTTPS 下）
- 加密/HMAC：`RECOVERY_EMAIL_ENCRYPTION_KEY`、`RECOVERY_EMAIL_HMAC_KEY`、`RECOVERY_EMAIL_VERIFICATION_HMAC_KEY`、`QUERY_CODE_RECOVERY_HMAC_KEY`
- 邮件找回：`RECOVERY_EMAIL_SENDER_MODE`（默认 `disabled`）及 SMTP 系列
- 环境：`APP_ENV=production`

**必须遵守**：

- 正式值**不得写进 Git**；不得作为服务命令行参数；优先放入 ACL 受限的 `D:\pjsk\secrets\backend.env`（由后端 godotenv 从工作目录读取）。
- 服务账户对密钥目录只给**读取**、对日志目录给**写入**、对 exe 给**执行**的最小权限。
- 不建议长期用管理员个人账户运行服务；用专用低权限本地账户。
- **不在日志中打印环境变量**（后端当前不打印；包装脚本也不得回显）。

ACL 示例（**通用示例与检查方法**；本机实际已执行的 ACL——`backend\.env` 收紧为 SYSTEM/Administrators 完全控制、所有者修改、LocalService 只读，以及服务/日志目录授权——以开发日志为准）：

```powershell
# 通用示例 —— 查看密钥目录 ACL
Get-Acl D:\pjsk\secrets | Format-List
# 通用示例 —— 仅授予专用服务账户读取（占位账户名 CHANGE_ME）
# icacls D:\pjsk\secrets /inheritance:r /grant:r "CHANGE_ME:(OI)(CI)R"
```

---

## 8. 服务账户建议

| 账户 | 特点 | 本项目评价 |
| --- | --- | --- |
| `LocalSystem` | 权限极高，可访问大部分系统资源 | **不推荐**：权限过大，违反最小权限 |
| `NetworkService` | 低权限，网络访问用机器身份 | 可用，但对 `D:\pjsk` 各目录仍需显式授权 |
| **专用本地低权限账户** | 单独为服务创建、按需授权 | **推荐**：最小权限、可审计、避免共享 |

- **PostgreSQL 本地 TCP 连接**：本项目用 `host/port` + 用户名密码走 TCP（`config.go` 组装 DSN），**不依赖 Windows 集成身份**；因此服务账户无需 PostgreSQL 的 Windows 权限，只需网络到 `127.0.0.1:5432` 的能力和 `backend.env` 中的库凭据。
- **最小权限**：对 `D:\pjsk\backend\bin`（执行）、`D:\pjsk\frontend\dist`（读取，Caddy 账户）、`D:\pjsk\secrets`（读取）、`D:\pjsk\runtime\logs`（写入）分别授权。
- 不建议用日常管理员账户运行服务；专用账户应**禁止交互式登录**（本文不实际创建账户）。

---

## 9. 日志与故障恢复

- **后端 stdout/stderr**：由服务包装器（WinSW `<log>` / NSSM `AppStdout/AppStderr`）落盘到 `D:\pjsk\runtime\logs`。
- **Caddy 日志**：由 `pjsk-caddy` 服务或 Caddyfile `log` 指令写入 `runtime\logs`。
- **滚动与保留**：WinSW 支持按大小/时间滚动；建议保留 7–30 天，视磁盘而定。
- **不记录敏感内容**：日志不含密码、查询码、Cookie、验证码、完整恢复令牌（后端 `logsafe` 已保证数据库错误脱敏，请求日志只有方法/路径/耗时）。
- **崩溃自动重启 + 退避**：配置失败重启，但设置重启延迟（如 5–10s）与失败计数重置窗口，避免数据库长期不可用时**快速无限重启**。
- **磁盘写满风险**：日志目录写满会导致服务或系统异常；需监控 `runtime\logs` 容量并设滚动上限。
- **状态检查**（示例）：

```powershell
Get-Service pjsk-backend, pjsk-caddy               # 服务状态
curl.exe -s http://127.0.0.1:8080/health           # 后端健康（本机回环）
Test-NetConnection 127.0.0.1 -Port 8080            # 端口监听
```

---

## 10. 安装 / 升级 / 回滚 / 卸载流程（人工步骤；后端已于 2026-07-16 按 §4 NSSM 路线完成安装，Caddy 部分仍未执行）

### 安装（通用示例；本机后端实际安装见 §4 与开发日志）

1. 构建后端 exe 与前端 dist（见 §5、§6）。
2. 准备 `D:\pjsk\secrets\backend.env`（真实值，ACL 受限，不进 Git）。
3. 复制 WinSW 可执行文件与本仓库 XML 示例，替换全部占位值。
4. 用专用服务账户注册 `pjsk-backend`、`pjsk-caddy`（`winsw install`，示例）。
5. 确认 PostgreSQL 已运行，启动 `pjsk-backend`、`pjsk-caddy`。
6. 复查 `/health`、HTTPS、登录。

### 升级（通用示例；本机尚未执行过升级）

1. 备份当前 `pjsk-backend.exe`、`frontend\dist`、`backend.env` 与相关配置。
2. 构建并本地验证新版本（`go test`/`go build`；`pnpm build`）。
3. 按合理顺序停服：先停 `pjsk-backend`（前端静态可继续由 Caddy 提供），必要时再停 `pjsk-caddy`。
4. 原子替换 exe 与 `dist`。
5. 启动服务。
6. 检查 `/health`。
7. 检查管理员登录、普通用户查询登录。
8. 检查上传、导出、邮件找回相关配置（若启用 SMTP）。
9. 任一步失败：停服 → 用备份还原 exe/dist/配置 → 启动 → 复查。

### 回滚（通用示例；本机尚未执行过回滚）

- 停 `pjsk-backend` → 覆盖回备份 exe → 启动 → `/health` 复查；前端同理还原 `dist` 备份。

### 卸载（通用示例；本机仅卸载过启动失败的 WinSW 服务，现役 NSSM 服务未卸载）

- 停止并移除 `pjsk-backend`、`pjsk-caddy` 服务（`winsw uninstall` / `nssm remove ... confirm`）。
- **卸载不得删除数据库、备份、日志或密钥**，除非人工单独确认。PostgreSQL 服务不由本项目卸载。

---

## 11. 安全边界

- 本文命令均为通用示例；本机实际执行情况（NSSM 部署 `pjsk-backend`、相关目录/文件 ACL 与验收）以 [development-logs/2026-07-16-windows-service-deployment.md](development-logs/2026-07-16-windows-service-deployment.md) 为准。防火墙、hosts、系统环境变量、PostgreSQL 配置均未改动；注册表仅由 NSSM 自身写入其服务参数（不含任何密钥）。
- 本仓库不含真实域名、IP、账户、密码、DSN、密钥、证书私钥；`deploy/windows-service/` 下均为占位示例。
- PostgreSQL 服务保留现状，本项目脚本不创建、不启停、不改配置。
- 密钥只存放于仓库外、ACL 受限的本地文件，不进入 XML 命令行、不进入 Git、不在日志回显。
