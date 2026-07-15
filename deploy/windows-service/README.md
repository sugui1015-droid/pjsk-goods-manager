# deploy/windows-service — 服务化示例（占位，非生产）

> **示例文件，禁止直接用于生产环境。** 全部为占位值，需按实际部署替换并复核。完整说明见 [docs/windows-service-deployment.md](../../docs/windows-service-deployment.md)。

本目录内容：

| 文件 | 用途 |
| --- | --- |
| `backend-winsw.xml.example` | WinSW 注册 Go 后端 `pjsk-backend` 服务的 XML 示例 |
| `caddy-winsw.xml.example` | WinSW 注册 Caddy `pjsk-caddy`（HTTPS + 静态前端 + 反代）的 XML 示例 |
| `Start-PjskBackend.example.ps1` | 受控启动后端的示例包装脚本，默认只检查配置（`-CheckOnly` 语义），加 `-Run` 才启动 |

## 边界与约定

- 这些文件**不会**自动安装/注册/启动/停止任何 Windows 服务，**不会**创建账户、改 ACL、改注册表、改防火墙，**不会**下载 WinSW/NSSM/Caddy。
- 真实密钥/DSN/证书私钥**不放入**本目录任何文件；后端真实环境变量放在仓库外、ACL 受限的 `D:\pjsk\secrets\backend.env`，由后端 `godotenv` 从工作目录读取。
- 推荐 **WinSW**（配置可审查、失败重启、日志滚动）；**NSSM** 仅作备选，命令模板见主文档 §4，均标注"示例，当前未执行"。
- Caddy 服务示例与 [deploy/caddy/Caddyfile.example](../caddy/Caddyfile.example) 对齐。
- WinSW 字段语义以所用版本官方文档为准；若存疑不要照搬。

## 快速指引

1. 构建产物：后端 `go build -trimpath -o .\bin\pjsk-backend.exe .`（在 `backend/`）；前端 `pnpm.cmd run build`（产物 `frontend/dist`）。
2. 准备 `D:\pjsk\secrets\backend.env`（真实值、ACL 受限、不进 Git）。
3. 复制 `*.example` 去掉 `.example`，替换占位路径/值。
4. 用专用低权限服务账户注册服务（人工执行，本仓库不代为执行）。
5. 确认 PostgreSQL 已运行后启动服务，复查 `/health` 与登录。
