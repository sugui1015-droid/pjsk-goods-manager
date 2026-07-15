# 内网 HTTPS 与反向代理部署指南

本文针对本项目（Vue 3 静态前端 + Go 后端 `127.0.0.1:8080` + PostgreSQL）在**局域网内、使用自有域名、不开放公网**的部署场景，给出可复制后替换占位值的反向代理与 HTTPS 方案。示例中的域名、IP、证书路径、密钥全部是占位符，**不含任何真实值**。

相关文档：[internal-network-deployment.md](internal-network-deployment.md)（`SERVER_HOST` / `TRUSTED_PROXY_CIDRS` / `CORS_ALLOWED_ORIGINS` 语义）、[internal-deployment-secrets.md](internal-deployment-secrets.md)（密钥生成与 Windows 保存）、[database-backup-restore.md](database-backup-restore.md)（备份恢复）。

> 占位约定：域名 `pjsk.internal.example`、内网 IP `192.168.1.10`、待替换值 `CHANGE_ME`、密钥/证书目录 `D:\pjsk\secrets\...`。请在实际部署时全部替换，切勿把真实值提交到 Git。

---

## 1. 推荐部署结构

```text
局域网客户端（浏览器）
    │  HTTPS 443（内部 CA 证书）
    ▼
内网 DNS / hosts 中的 pjsk.internal.example  →  192.168.1.10
    │
    ▼
Caddy 或 Nginx（与后端同一台 Windows 主机）
    ├─ 提供静态前端  D:\pjsk\frontend\dist   （SPA，history fallback 到 index.html）
    └─ 反向代理  /api/*  和  /health  →  127.0.0.1:8080
                                            │
                                            ▼
                                        Go 后端（仅监听 127.0.0.1:8080）
                                            │
                                            ▼
                                        PostgreSQL（仅监听 127.0.0.1:5432）
```

**路由事实（来自 `backend/internal/api/router.go`，务必据此配置，不要臆测）**：后端只暴露 `/health` 和 `/api/*`（`/api/config`、`/api/admin/*`、`/api/query/*`）。除此之外的所有路径都属于前端 SPA，由反代提供 `frontend/dist` 并在找不到文件时回退到 `index.html`。管理界面是前端路由（如 `/admin/...`），**不是后端路径**，因此与普通用户共用同一域名与同一静态站点，无需单独代理。

前后端同源发布（同一域名、同一端口 443），因此**正式部署不需要配置 CORS 跨域来源**。

---

## 2. 内网域名解析方案

域名 `pjsk.internal.example` 只需在局域网内可解析到反代主机 `192.168.1.10`。按推荐优先级：

1. **内网 DNS 服务器 / 路由器自定义解析（推荐）**：在路由器或内网 DNS（如 Windows DNS Server、dnsmasq、AdGuard Home 等）增加一条 A 记录 `pjsk.internal.example → 192.168.1.10`。所有设备自动生效，便于集中管理与后续改 IP。
2. **Windows DNS Server**：域环境下在内网 DNS 区域添加 A 记录，适合已有 AD/DNS 基础设施的组织。
3. **少量设备临时用 hosts**：仅在个别测试设备的 `C:\Windows\System32\drivers\etc\hosts`（或各系统 hosts）临时加 `192.168.1.10 pjsk.internal.example`。**仅用于验证，不适合正式多设备场景**（每台都要改、易漏、难维护）。
4. **不建议**让内网域名解析到公网地址，或把该域名对公网开放解析——本项目定位为纯内网访问。

**内网专用域名的行为与限制**：该域名只在配置了上述解析的网络内可用；离开局域网无法访问；公网 CA 通常不会为不可公网验证的内网名签发证书（除非走 DNS-01 且域名可公网解析）。因此内网 HTTPS 一般走**内部 CA**（见下一节）。

---

## 3. 内网 HTTPS 证书方案

| 方案 | 说明 | 适用 |
| --- | --- | --- |
| **Caddy `tls internal`** | Caddy 内置本地 CA，自动为站点签发证书并自动续期 | 单机、想最省事；**每台访问设备需信任 Caddy 本地根证书** |
| **自建内部 CA 签发** | 用 `mkcert`／`openssl`／`step-ca` 等建内部 CA，为 `pjsk.internal.example` 签发服务器证书，配到 Nginx/Caddy | 有多台服务或希望统一管理证书 |
| **企业/家庭已有内网 CA** | 组织已有内网 CA（如 AD 证书服务），由其签发并通过组策略统一下发根证书 | 已有 PKI 基础设施 |
| **公网 ACME（Let's Encrypt 等）** | 纯内网无法完成 HTTP-01 / TLS-ALPN-01 挑战；只有域名可公网 DNS 解析且用 DNS-01 才可能 | 一般**不适用**纯内网 |

> **关键提醒**：无论用 Caddy 本地 CA 还是自建内部 CA，**每台访问设备的操作系统/浏览器必须信任对应的根证书**，否则浏览器仍会显示"不安全 / 证书无效"。根证书分发（手动导入或组策略）是内网 HTTPS 能"绿锁"的前提。

本仓库**不生成、不包含任何真实私钥、根证书或服务器证书**；示例中的证书路径都是占位符。

---

## 4. Caddy 推荐方案

**本项目优先推荐 Caddy**，理由：内置本地 CA（`tls internal`）与自动续期，配置量最小；`try_files` 天然支持 SPA history 回退；`handle`/`reverse_proxy` 默认就会设置 `X-Forwarded-For`/`X-Forwarded-Proto`/`Host`，与本项目 `TRUSTED_PROXY_CIDRS` 机制契合，运营心智负担低。Nginx 作为已有 Nginx 运维经验时的等价备选（见第 5 节）。

完整示例见 [deploy/caddy/Caddyfile.example](../deploy/caddy/Caddyfile.example)，要点：

```caddyfile
# 占位域名，替换为你的内网域名
pjsk.internal.example {
    # 内网自签：Caddy 本地 CA 自动签发（访问设备需信任 Caddy 根证书）
    tls internal
    # 或改用自建内部 CA 证书：
    # tls D:\pjsk\secrets\pjsk.internal.example.crt D:\pjsk\secrets\pjsk.internal.example.key

    encode zstd gzip

    # 1) 后端 API 与健康检查 → 本机 Go 后端
    @backend path /api/* /health
    handle @backend {
        reverse_proxy 127.0.0.1:8080 {
            header_up X-Forwarded-For {remote_host}   # 覆盖客户端伪造值，不透传
            header_up X-Forwarded-Proto {scheme}
            header_up Host {host}
        }
    }

    # 2) 其余路径 → 静态前端，SPA 回退 index.html
    handle {
        root * D:\pjsk\frontend\dist
        try_files {path} /index.html
        file_server
    }

    # 上传预览上限 20 MiB（后端 maxPreviewFileSize），留余量
    request_body {
        max_size 25MB
    }

    # 安全响应头
    header {
        X-Content-Type-Options nosniff
        X-Frame-Options DENY
        Referrer-Policy strict-origin-when-cross-origin
        -Server
    }

    log {
        output file D:\pjsk\secrets\logs\caddy-access.log
    }
}
```

- **客户端真实 IP**：`header_up X-Forwarded-For {remote_host}` 用真实连接地址**覆盖**（而非追加）客户端可能伪造的头；配合后端 `TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128`，后端即可取到真实客户端 IP。
- **PostgreSQL 不代理**：配置中没有任何指向 5432 的入口，数据库不经反代暴露。
- **开发端口不代理**：不代理 Vite 5173；正式访问只走 443。
- **WebSocket**：**当前项目无 WebSocket 需求**，无需 `@ws`/`Connection Upgrade` 特殊处理。

---

## 5. Nginx 备选方案

等价配置见 [deploy/nginx/pjsk.conf.example](../deploy/nginx/pjsk.conf.example)。选择 Nginx 的唯一理由是团队已有 Nginx 运维经验；否则优先 Caddy（证书与续期更省事）。要点：

```nginx
server {
    listen 443 ssl;
    server_name pjsk.internal.example;               # 占位域名

    # 占位证书路径（由内部 CA 签发，切勿提交真实私钥）
    ssl_certificate     D:/pjsk/secrets/pjsk.internal.example.crt;
    ssl_certificate_key D:/pjsk/secrets/pjsk.internal.example.key;
    ssl_protocols TLSv1.2 TLSv1.3;

    client_max_body_size 25m;                          # ≥ 后端 20 MiB 上传上限
    root D:/pjsk/frontend/dist;
    index index.html;

    # 1) API 与健康检查 → 本机 Go 后端
    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Forwarded-For   $remote_addr;   # 覆盖，不透传客户端伪造值
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout    60s;
        proxy_connect_timeout 10s;
    }
    location = /health {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Forwarded-For   $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # 2) SPA history 回退
    location / {
        try_files $uri $uri/ /index.html;
    }

    # 安全响应头
    add_header X-Content-Type-Options nosniff always;
    add_header X-Frame-Options DENY always;
    add_header Referrer-Policy strict-origin-when-cross-origin always;
}

# 可选：80 端口仅用于跳转到 443
server {
    listen 80;
    server_name pjsk.internal.example;
    return 301 https://$host$request_uri;
}
```

> Nginx 的 `X-Forwarded-For` 用 `$remote_addr`（真实连接地址）而非 `$proxy_add_x_forwarded_for`（后者会把客户端带来的头追加进去）；配合后端右向左剥离逻辑，用 `$remote_addr` 覆盖最干净。

---

## 6. 后端环境变量示例（部署用）

只列变量名、作用与占位值，**不含真实值**。以 `backend/.env.example` 与 `config.go` 为准。

```dotenv
APP_ENV=production
# 后端只监听回环，由本机反代对内网提供 HTTPS
SERVER_HOST=127.0.0.1
APP_PORT=8080

# 反代与后端同机：只信任回环，绝不信任整个局域网，绝不用 0.0.0.0/0
TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128

# 同源部署留空即可（前端与 API 同域名同端口）
CORS_ALLOWED_ORIGINS=

# 正式 HTTPS 下必须为 true，否则会话 Cookie 不带 Secure
ADMIN_COOKIE_SECURE=true
ADMIN_SESSION_TTL=12h

# 数据库：仅本机
DATABASE_HOST=127.0.0.1
DATABASE_PORT=5432
DATABASE_USER=CHANGE_ME
DATABASE_PASSWORD=CHANGE_ME
DATABASE_NAME=pjsk
DATABASE_SSLMODE=disable

# 会话/加密/HMAC 密钥（生成与保存见 internal-deployment-secrets.md）
RECOVERY_EMAIL_ENCRYPTION_KEY=CHANGE_ME
RECOVERY_EMAIL_HMAC_KEY=CHANGE_ME
RECOVERY_EMAIL_VERIFICATION_HMAC_KEY=CHANGE_ME
QUERY_CODE_RECOVERY_HMAC_KEY=CHANGE_ME
# 邮件默认关闭；启用见 internal-deployment-secrets.md
RECOVERY_EMAIL_SENDER_MODE=disabled
```

**必须明确的几点**：

- 反代与后端在**同一台机器**时，可信代理应限制为**回环地址** `127.0.0.1/32,::1/128`，而不是信任整个局域网网段。
- **绝不**把 `TRUSTED_PROXY_CIDRS` 设为 `0.0.0.0/0` 或 `::/0`（后端会直接拒绝启动，且这会让任意来源都能伪造客户端 IP）。
- 正式 HTTPS 下 `ADMIN_COOKIE_SECURE=true`；Cookie 为 `HttpOnly` + `SameSite=Lax` + 无 `Domain`（host-only）+ `Path=/`，同源部署下会话正常工作。
- **内网域名改变后需要同步调整的来源地址**：若从"同源 + 空 CORS"改为前后端**不同源**部署，才需要把新前端 origin 写进 `CORS_ALLOWED_ORIGINS`；同源部署改域名无需改 CORS。前端若用非同源 API，还需重建时设置 `VITE_API_BASE_URL`。

---

## 7. 防火墙与端口原则（只写原则，不实际执行）

- 局域网只开放 **443**；如需 HTTP→HTTPS 跳转再开放 **80**（仅做 301 跳转）。
- 后端 **8080 只监听回环**（`SERVER_HOST=127.0.0.1`），不对局域网开放。
- Vite **5173 不是正式访问入口**，正式环境不运行开发服务器。
- PostgreSQL **5432 不对普通局域网客户端开放**，只允许本机反代所在主机的后端访问。
- 管理入口与普通用户入口**共用同一域名**：二者都是同一前端 SPA 的不同路由，后端按会话 Cookie（管理员 `pjsk_admin_session` / 用户 `pjsk_query_session`）区分权限，无需为管理端单独开域名或端口。

---

## 8. 部署前后检查清单（不修改系统）

命令为 Windows PowerShell；HTTP 探测用 `curl.exe`（避免 PowerShell `curl` 别名指向 `Invoke-WebRequest`）。

**构建与后端自检**

```powershell
Set-Location D:\pjsk\frontend; pnpm.cmd run build          # 产出 frontend\dist
Set-Location D:\pjsk\backend;  go build ./...; go vet ./...; go test ./...
```

**HTTPS / 证书 / 健康检查**（`-k` 仅用于内部 CA 根证书尚未导入时的临时验证）

```powershell
curl.exe -k https://pjsk.internal.example/health          # 期望 {"status":"ok",...}
curl.exe -kv https://pjsk.internal.example/ 2>&1 | Select-String "subject:|issuer:|HTTP/"
```

**登录 Cookie / 限流 / 查询登录**

```powershell
# 管理员登录（占位凭据）：观察 Set-Cookie 是否含 Secure、HttpOnly
curl.exe -k -i -c NUL -X POST https://pjsk.internal.example/api/admin/login `
  -H "Content-Type: application/json" -d '{"username":"CHANGE_ME","password":"CHANGE_ME"}' |
  Select-String "Set-Cookie|HTTP/"
# 连续错误密码应在阈值后返回 429（管理员登录限流）
# 普通用户查询登录同理：POST /api/query/login
```

**真实客户端 IP / 未授权 Origin**

```powershell
# 伪造 XFF 在同机回环可信代理下应被后端正确处理（真实 IP 取自反代连接地址）
curl.exe -k https://pjsk.internal.example/health -H "X-Forwarded-For: 203.0.113.9"
# 跨域来源应被拒绝（生产同源部署下不应返回 Access-Control-Allow-Origin）
curl.exe -k -i https://pjsk.internal.example/api/config -H "Origin: https://evil.example" |
  Select-String "Access-Control-Allow-Origin"   # 期望：无输出
```

**SPA 刷新不 404 / 上传导出**

```powershell
curl.exe -k -o NUL -w "%{http_code}`n" https://pjsk.internal.example/admin/orders   # 期望 200（回退 index.html）
# 登录后测试 Excel 导入预览（≤20MiB）与导出，确认反代 body 上限未截断
```

**端口暴露自查（8080 / 5173 / 5432 不应对局域网开放）**

```powershell
# 从另一台局域网设备执行；期望这三个端口连接失败/超时
Test-NetConnection 192.168.1.10 -Port 8080
Test-NetConnection 192.168.1.10 -Port 5173
Test-NetConnection 192.168.1.10 -Port 5432
Test-NetConnection 192.168.1.10 -Port 443    # 期望 TcpTestSucceeded : True
```

---

## 9. 安全边界重申

- 反代**不代理** 5432/8080/5173 直连入口；数据库与后端只在本机可达。
- `X-Forwarded-For` 由反代**覆盖**为真实连接地址，配合后端可信代理白名单，防止客户端伪造。
- 本仓库不含真实域名、真实 IP、真实证书或私钥、真实密钥；所有部署秘密只存放在仓库外并按 [internal-deployment-secrets.md](internal-deployment-secrets.md) 保护。
- 本文与 `deploy/` 下示例均为**示例，禁止直接用于生产**，必须替换全部占位值并由运营者复核后使用。
