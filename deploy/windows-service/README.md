# deploy/windows-service — 服务化示例（占位，非生产）

> **示例文件，禁止直接用于生产环境。** 全部为占位值，需按实际部署替换并复核。完整说明见 [docs/windows-service-deployment.md](../../docs/windows-service-deployment.md)。

本目录内容：

| 文件 | 用途 |
| --- | --- |
| `backend-winsw.xml.example` | WinSW 注册 Go 后端 `pjsk-backend` 服务的 XML 示例 |
| `caddy-winsw.xml.example` | WinSW 注册 Caddy `pjsk-caddy`（HTTPS + 静态前端 + 反代）的 XML 示例 |
| `Caddyfile.http-lan.example` | 局域网 HTTP 阶段（无 HTTPS）的 Caddy 网关示例：静态前端 + SPA 回退 + 反代 `/api/*`、`/health`；占位端口/路径 |
| `Start-PjskBackend.example.ps1` | 受控启动后端的示例包装脚本，默认只检查配置（`-CheckOnly` 语义），加 `-Run` 才启动 |

## 边界与约定

- 这些文件**不会**自动安装/注册/启动/停止任何 Windows 服务，**不会**创建账户、改 ACL、改注册表、改防火墙，**不会**下载 WinSW/NSSM/Caddy。
- 真实密钥/DSN/证书私钥**不放入**本目录任何文件；后端真实环境变量放在仓库外、ACL 受限的 `D:\pjsk\secrets\backend.env`，由后端 `godotenv` 从工作目录读取。
- 模板层面首选 **WinSW**（配置可审查、失败重启、日志滚动）；但 2026-07-16 本机实测：本次使用的 WinSW v3 系列二进制（文件版本 3.0.0.0）以 `NT AUTHORITY\LocalService` 进入 service mode 即 `Failed to open the service. 拒绝访问`（与 winsw/winsw#872 一致，失败在后端子进程启动前），**本机实际改用 NSSM 完成 `pjsk-backend` 部署并通过启停/异常恢复验收**。实际参数见主文档 §4，取证与验收记录见 [docs/development-logs/2026-07-16-windows-service-deployment.md](../../docs/development-logs/2026-07-16-windows-service-deployment.md)。
- 本目录只是占位模板，模板本身不会部署任何东西。本机实际使用的包装器、正式配置与运行日志（`D:\PJSK-Service\backend\nssm.exe`、`D:\PJSK-Service\caddy\{caddy.exe,Caddyfile}`、旧 WinSW 取证文件、`D:\PJSK-Runtime\logs\{backend,caddy}\`）全部位于**仓库外**，不属于本目录内容，也不得复制入仓库；本机实际的 `.env` 位于 `backend\.env`（已 gitignore、ACL 收紧，LocalService 只读）。
- **Caddy 网关实际部署（2026-07-16）**：本机以 NSSM 注册 `pjsk-caddy`（Caddy v2.11.4，LocalService，依赖 `pjsk-backend`），监听 **8081**（80 被本机 IIS/W3SVC 占用），静态前端 `D:\PJSK-Deploy\frontend` + SPA 回退 + 反代 `/api/*`、`/health`；防火墙 `PJSK Caddy HTTP LAN` 仅放行 TCP 8081（Private+LocalSubnet+限定 caddy.exe）。验收（首页/静态资源/SPA 回退/API 代理/健康检查/启停与异常恢复）全部通过；跨设备访问已通过 Windows 移动热点完成验证（原 CMCC-4cXx 网络因疑似客户端隔离无法设备互访）；真实整机重启自启验收已通过（三个服务自动 Running、health 200）；HTTPS 仍待办；记录见 [docs/development-logs/2026-07-16-internal-caddy-deployment.md](../../docs/development-logs/2026-07-16-internal-caddy-deployment.md)。`caddy-winsw.xml.example` 为 WinSW 形态示例，本机未采用。
- Caddy 服务示例与 [deploy/caddy/Caddyfile.example](../caddy/Caddyfile.example) 对齐。
- WinSW 字段语义以所用版本官方文档为准；若存疑不要照搬。

## 快速指引

1. 构建产物：后端 `go build -trimpath -o .\bin\pjsk-backend.exe .`（在 `backend/`）；前端 `pnpm.cmd run build`（产物 `frontend/dist`）。
2. 准备 `D:\pjsk\secrets\backend.env`（真实值、ACL 受限、不进 Git）。
3. 复制 `*.example` 去掉 `.example`，替换占位路径/值。
4. 用专用低权限服务账户注册服务（人工执行，本仓库不代为执行）。
5. 确认 PostgreSQL 已运行后启动服务，复查 `/health` 与登录。
