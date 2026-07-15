# 内网 HTTPS 与反向代理部署方案落地

## 阶段 1：只读调查

### 基线

- 分支 `main`；`HEAD` = `origin/main` = `58e5e53ad975d27db9f7eb966629f5f7b903514d`；工作区、暂存区干净。
- 只读检查了 `AGENTS.md`、`HANDOVER.md`、`.env.example`、`backend/.env.example`、`internal-network-deployment.md`、`internal-deployment-secrets.md`、`router.go`、`admin/handler.go`、`query/handler.go`、`config.go`、`clientip/clientip.go`、`importpreview/handler.go`、`frontend/vite.config.ts`、`frontend/src/api/client.ts`。未连接数据库、未读取真实 `.env`/密钥。

### 后端实际路由（决定代理规则的关键事实）

后端只暴露两类前缀，全部由 `net/http` ServeMux 注册（`internal/api/router.go`）：

- `GET /health` — 健康检查（含数据库 Ping，返回 200/503）。
- `GET /api/config` — 前端读取的模块状态与 `emailDeliveryEnabled`。
- `/api/admin/*` — 管理端（login/me/logout、imports、orders、users、payments、export）。
- `/api/query/*` — 普通用户查询与找回（login/logout/orders/bind-code/change-code/recovery-email*/recovery/*）。

**结论：反向代理只需把 `/api/*` 与 `/health` 转发到 `127.0.0.1:8080`，其余路径由静态前端处理。** 无 WebSocket、无独立管理域名、无 `/admin` 顶层路径（管理界面是前端 SPA 路由，走静态回退）。

### 调查问题逐项回答

1. **前端由谁提供**：`frontend/src/api/client.ts` 生产构建使用 `VITE_API_BASE_URL`（默认空 = 同源相对路径）；`vite.config.ts` 仅用于开发代理（`/health`、`/api` → 127.0.0.1:8080）。因此正式部署最适合：`pnpm run build` 产出 `frontend/dist`，由反向代理作为静态站点提供，`/api`+`/health` 反代到后端——前后端同源，无需 CORS。
2. **后端是否只监听 127.0.0.1:8080**：是。`SERVER_HOST` 默认 `127.0.0.1`（`config.go` `loadServerHost`），反代与后端同机时应保持该默认，不要改成 `0.0.0.0`。
3. **可信代理/真实 IP/HTTPS 判断/CORS 支持是否足够**：
   - 客户端真实 IP：`clientip` 包已完整支持 `X-Forwarded-For` + `TRUSTED_PROXY_CIDRS`（默认不信任、右向左剥离、非法链回退、`unknown-client` 兜底）。**足够**。
   - CORS：`CORS_ALLOWED_ORIGINS` 支持精确来源，同源部署可留空。**足够**。
   - HTTPS 判断：后端**不读 `X-Forwarded-Proto`**，也**不做 HTTP→HTTPS 重定向**。Cookie 的 `Secure` 由 `ADMIN_COOKIE_SECURE` 环境变量决定（`admin/handler.go`、`query/handler.go` 均为 `Secure: h.cookieSecure`，`SameSite=Lax`，无 `Domain`，`Path=/`，`HttpOnly`）。同源 HTTPS 反代下，运营者设置 `ADMIN_COOKIE_SECURE=true` 即可，后端无需感知协议。**足够**——重定向与 TLS 终止由反代负责，符合职责分离。
4. **内网自有域名证书方案**：内部 CA 签发、Caddy `tls internal`（自带本地 CA）、已有企业/家庭内网 CA；公网 ACME 在纯内网无法完成 HTTP-01/TLS-ALPN-01 挑战（除非用 DNS-01 且域名可公网解析）。详见主文档。
5. **是否存在必须先改代码才能安全反代的阻断**：**无阻断**。
   - Secure Cookie 由环境变量控制，不依赖 `X-Forwarded-Proto`，同源反代场景正确。
   - 上传大小：`importpreview` 限制 `maxPreviewFileSize = 20<<20`（20 MiB），`MaxBytesReader` 为 `+1`。反代 body 上限须 ≥ ~21 MiB（示例用 25m/25M），否则大文件会被代理提前截断，早于后端自身校验。这是**配置注意事项，非代码阻断**。
   - 健康检查 `/health` 适合反代探活。

### 阶段 1 结论

当前代码已具备安全反向代理所需的全部能力（回环监听、可信代理真实 IP、可配置 CORS、环境驱动 Secure Cookie）。**本阶段为纯文档 + 示例配置，不修改任何 Go/前端业务代码。** 唯一需在文档中强调的运营注意点：`ADMIN_COOKIE_SECURE=true`、`TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128`、代理 body 上限 ≥ 25 MiB、代理必须重写而非透传 `X-Forwarded-For`。

### 附带发现（记录，不在本阶段扩大范围）

- `HANDOVER.md` 第 16 节"高风险操作"仍写"`FrontendOrigins` 在 `Load()` 中硬编码为 localhost"——该描述在 `8dc21e1` 引入 `CORS_ALLOWED_ORIGINS` 后已过时。将在阶段 6 随部署状态一并订正（属于部署相关文档修正）。

### Git 状态

- 阶段末仅新增本日志；`git diff --check` 干净；暂存区空；无删除、无重命名。

## 阶段 2：内网部署主文档

- 新增 `docs/internal-https-reverse-proxy.md`，结合真实路由（`/health` + `/api/*`，其余为前端 SPA）编写，含：推荐结构、内网域名解析（内网 DNS/路由器 > Windows DNS > 少量 hosts > 不建议公网解析）、证书方案（Caddy `tls internal`/自建内部 CA/企业 CA/公网 ACME 限制，强调访问设备须信任根证书）、Caddy 推荐 Caddyfile、Nginx 等价配置、部署环境变量示例（全占位）、防火墙端口原则、PowerShell 检查清单（`curl.exe`、`Test-NetConnection`）、安全边界。
- 选择理由：优先 Caddy（内置本地 CA + 自动续期、`try_files` 天然 SPA 回退、默认设置转发头，运营负担最小）；Nginx 作为已有运维经验时的等价备选。

## 阶段 3：示例配置文件

- 新增 `deploy/caddy/Caddyfile.example` 与 `deploy/nginx/pjsk.conf.example`，均在顶部标注"示例文件，禁止直接用于生产环境"，只用占位域名/IP/证书路径。
- 两份配置与主文档一致：`/api/*`+`/health` 反代到 `127.0.0.1:8080`，其余静态前端 + SPA 回退 `index.html`；`X-Forwarded-For` 用真实连接地址**覆盖**（Caddy `{remote_host}`、Nginx `$remote_addr`）而非透传；`X-Forwarded-Proto`/`Host` 透传；body 上限 25MB（≥ 后端 20MiB）；安全响应头；不代理 5432/5173；注明无 WebSocket 需求。

## 阶段 4：最小代码修复

- **未修改任何业务代码。** 阶段 1 确认无反向代理阻断：回环监听、可信代理真实 IP、可配置 CORS 均已具备；Secure Cookie 由 `ADMIN_COOKIE_SECURE` 环境变量驱动、不依赖 `X-Forwarded-Proto`，同源 HTTPS 反代场景正确；后端不做协议重定向（由反代负责）。因此无需为反向代理改代码。

## 阶段 5：离线验证

- `git diff --check` 干净。
- 未改 Go/前端代码，故本轮不强制跑 `go build/test` 与 `pnpm build`（无代码变更，无回归面）；仅新增文档与示例配置。
- Caddy、Nginx 均**未安装**（`command -v caddy/nginx` 均无）：按边界不下载安装，示例配置仅做人工静态核对，**未进行真实语法解析**，未启动任何反代。
- 敏感信息扫描：本轮文件仅含占位值（`pjsk.internal.example`、`192.168.1.10`、`CHANGE_ME`、`D:\pjsk\secrets\...`），无真实域名/IP/密码/DSN/密钥/证书/私钥/`.env`/`.dump`/`.local-secrets`。

## 阶段 6：文档与日志收尾

- 更新 `HANDOVER.md`：第 15 节补充备份工具与内网 HTTPS 反代指南的完成状态与未决事项；第 16 节订正过时的"CORS/`FrontendOrigins` 硬编码 localhost"描述（`CORS_ALLOWED_ORIGINS` 已可配置，production 默认无跨域）。
- 未删除、移动或重命名任何历史日志；仅追加。

### 安全边界确认

- 未连接或修改正式数据库 `pjsk`，未运行迁移。
- 未读取真实 `.env`/`backend/.env`/`.claude/settings.local.json`，未读取或输出任何密钥/证书/密码。
- 未安装或启动 Caddy/Nginx/WinSW/NSSM，未申请证书、未调用 ACME、未开放公网端口。
- 未修改防火墙/hosts/DNS/注册表/系统服务/计划任务，未启停现有前后端或 PostgreSQL 服务。
- 未使用子代理。
