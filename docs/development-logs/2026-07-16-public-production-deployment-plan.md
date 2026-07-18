# 2026-07-16 公网生产部署只读调查与实施方案

> 本轮范围：只读调查、方案设计和文档更新。未购买服务器，未修改 DNS，未开放公网端口，未上传数据库，未修改当前本机服务、防火墙、Caddy、PostgreSQL 或真实业务数据，未读取或记录任何真实密码、查询码、Cookie、验证码、令牌、数据库连接串或 SMTP 凭据，未使用子代理。
>
> 重要边界更正：截至本轮开始，PJSK 已完成的是 Windows 本机与局域网部署验收；`127.0.0.1:8081` 是本机入口，不是公网生产入口。不同地区用户尚无法通过正式域名和 HTTPS 使用系统。

## 1. 当前运行依赖梳理

### 1.1 应用运行时

- **Go 后端**：`backend/main.go` 启动 HTTP server，读取 `backend/internal/config/config.go` 的环境配置，默认监听 `127.0.0.1:8080`；启动时连接 PostgreSQL 并运行 embed 的 `backend/migrations/*.sql`。
- **Vue 前端**：`frontend` 使用 Vue 3 + Vite，`pnpm run build` 产出静态文件，可由 Caddy/Nginx 直接提供；正式环境不需要运行 Vite dev server。
- **PostgreSQL**：后端通过 `DATABASE_URL` 或 `DATABASE_HOST/PORT/USER/PASSWORD/NAME/SSLMODE` 连接；当前正式库迁移 19 条，最高 `0019_admin_auth_audit_events.sql`。
- **Caddy**：当前本机局域网阶段使用 Caddy 作为静态前端与 `/api/*`、`/health` 反向代理；公网方案继续推荐 Caddy，但监听 80/443 并使用公网域名自动 HTTPS。
- **SMTP 邮件**：当前邮件发送可为 disabled 或 smtp；`fake` 只允许 `APP_ENV=test`。公网正式使用查询码找回前，需要配置真实 SMTP 并通过 TLS/STARTTLS 验收。

### 1.2 配置与秘密

生产配置需要以下类别，但真实值不得进入 Git、日志或聊天：

- 数据库连接：`DATABASE_URL` 或 split database variables。
- 监听与代理：`APP_ENV=production`、`APP_PORT=8080`、`SERVER_HOST=127.0.0.1`、`TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128`、`CORS_ALLOWED_ORIGINS=`。
- Cookie：公网 HTTPS 下 `ADMIN_COOKIE_SECURE=true`。
- 会话：`ADMIN_SESSION_TTL`。
- 找回邮箱密钥：`RECOVERY_EMAIL_ENCRYPTION_KEY`、`RECOVERY_EMAIL_HMAC_KEY`。
- 邮箱验证码密钥：`RECOVERY_EMAIL_VERIFICATION_HMAC_KEY`。
- 查询码找回密钥：`QUERY_CODE_RECOVERY_HMAC_KEY`，production 必填。
- SMTP：`RECOVERY_EMAIL_SENDER_MODE=smtp` 及 SMTP host/port/username/password/from/from_name/tls_mode。

### 1.3 迁移、备份与恢复

- 迁移由 Go 后端启动时自动运行，文件名排序，`schema_migrations.version` 记录完整文件名。既有重复 `0005` 是已记录的历史例外，不得重命名。
- 现有备份/恢复工具位于 `scripts/database/`，主要是 Windows PowerShell 脚本，已包含 pg_dump custom format、metadata、validation、保留策略和测试库恢复思路。
- 公网 Linux 部署不应直接依赖 Windows PowerShell 脚本；应沿用同样安全原则，用 Linux shell/systemd timer/cron 或受控运维脚本实现 pg_dump、加密、校验、异地备份和恢复演练。
- **禁止直接复制 PostgreSQL data 目录**。跨机器迁移必须用逻辑备份/恢复（如 `pg_dump --format=custom` + `pg_restore`）或受控数据库迁移工具。

## 2. Linux 云服务器兼容性判断

### 2.1 可直接在 Linux 运行的部分

- Go 后端：业务代码不依赖 Windows API；使用标准库 HTTP、pgx、bcrypt、godotenv，可在 Linux 构建运行。
- 数据库迁移：SQL 文件通过 Go embed 打包进后端二进制，不依赖 Windows 路径。
- Vue 前端：构建产物是静态文件，Linux 上可由 Caddy 提供。
- Caddy：有 Linux 版本，适合 systemd 管理和自动 HTTPS。
- PostgreSQL：Linux 原生支持，适合本机 loopback 或 Unix socket 部署。
- SMTP：Go `net/smtp` + TLS/STARTTLS，跨平台。

### 2.2 Windows 专属内容和用途

这些是本机部署/运维辅助，不是应用运行时硬依赖：

- `backend/run.cmd`、`frontend/run.cmd`：Windows 开发启动脚本。
- `deploy/windows-service/*`：WinSW/NSSM/PowerShell 示例，用于 Windows 服务化。
- `docs/windows-service-deployment.md` 与相关开发日志：Windows 服务部署取证。
- `scripts/database/*.ps1`：Windows PowerShell 备份、恢复、保留策略工具。
- `D:\PJSK-*`、`D:\pjsk\...` 等路径：当前本机目录约定。
- NSSM、WinSW、Windows 防火墙、Windows 服务、PowerShell UAC：只属于当前 Windows 本机部署。

### 2.3 Linux 部署必须调整的内容

- 新增或编写 Linux systemd unit：后端以普通非 root 用户运行，Caddy 用发行版服务或官方 systemd 单元。
- 新增 Linux 路径约定：例如 `/opt/pjsk/app`、`/opt/pjsk/frontend`、`/etc/pjsk/backend.env`、`/var/lib/pjsk/backups`、`/var/log/pjsk`。
- 新增 Linux Caddyfile：公网域名站点块，80/443，自动 HTTPS，静态前端 root，`/api/*` 与 `/health` 反代到 `127.0.0.1:8080`。
- 新增 Linux 备份/恢复操作方案：不能直接复用 `.ps1`；可先写 runbook，实施时再写脚本并单独测试。
- 配置 PostgreSQL 仅监听 `127.0.0.1`/`::1` 或 Unix socket。
- 配置云安全组和主机防火墙只开放 80/443，禁止 8080/5432 公网访问。
- 配置 SMTP 正式凭据与发信域名策略。

### 2.4 无需修改的内容

- 普通用户、管理员、导入、付款、导出等业务代码。
- Go 后端路由：仍然只有 `/api/*` 与 `/health` 给反向代理。
- 前端构建逻辑：同源部署下无需 `VITE_API_BASE_URL`。
- CORS 模型：前端与 API 同源，`CORS_ALLOWED_ORIGINS` 留空。
- 普通用户 DTO 最小下发、移动端布局和权限边界修复。

## 3. 推荐公网生产架构

优先方案：**单台 Linux 云服务器起步**，公网只开放 TCP 80/443。

```text
不同地区用户浏览器
    ↓ HTTPS 443（HTTP 80 自动跳转）
正式域名（A/AAAA 记录）
    ↓
云服务器 Caddy
    ├─ 静态前端文件（Vue dist）
    └─ /api/*、/health → 127.0.0.1:8080
                         ↓
                    Go 后端（普通非 root 用户，systemd）
                         ↓
                    PostgreSQL（127.0.0.1:5432 或 Unix socket）
```

安全约束：

- 公网安全组/防火墙只允许 80/443 入站；SSH 仅允许运维来源 IP 或使用堡垒/控制台方式。
- 后端仅监听 `127.0.0.1:8080`，禁止公网或内网直连。
- PostgreSQL 仅监听 `127.0.0.1:5432`、`::1` 或 Unix socket，禁止公网直连。
- Caddy 负责 HTTPS、静态前端、SPA fallback、反向代理和安全响应头。
- `TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128`，只信任同机 Caddy。
- `ADMIN_COOKIE_SECURE=true`，同源部署下 `CORS_ALLOWED_ORIGINS=`。
- 后端使用普通非 root 服务账号运行；配置文件仅该账号和必要管理员可读。
- PostgreSQL、应用和备份目录设置最小权限。
- 每日数据库备份；每次发布前做一次一致性备份；定期恢复演练。
- systemd 管理后端和 Caddy；journald/logrotate 管理日志，日志不得含秘密。

## 4. 服务器规格建议

预计规模：300-400 名普通用户，业务形态以登录查询、订单明细、付款历史、管理员少量导入/导出为主，非高并发交易系统。

### 最低规格（试运行可用）

- CPU：2 vCPU。
- 内存：2 GB（可跑 Go 后端 + Caddy + PostgreSQL，但备份、导入、导出时余量较紧）。
- 磁盘：40-60 GB SSD。
- 带宽：3-5 Mbps 或等价按量带宽。
- 适用：小范围公网试用、低峰访问、严格控制备份保留量。

### 推荐规格（更稳妥）

- CPU：2-4 vCPU。
- 内存：4 GB。
- 磁盘：80-120 GB SSD，优先云盘可快照；数据库、备份和日志预留增长空间。
- 带宽：5-10 Mbps 或按量计费带宽。
- 适用：300-400 用户正式试用与日常运行。

### 是否需要独立数据库服务器

- 当前阶段适合先用**单机部署**：应用、Caddy、PostgreSQL 同机，运维简单，安全边界仍可做到公网只开 80/443。
- 独立数据库服务器不是第一优先级，除非出现以下条件：数据量明显增长、管理员导入/导出影响在线查询、需要高可用、需要托管备份/监控、或运维团队能承担跨主机私网安全配置。
- 若后续拆分数据库，数据库必须只在私网可达，并通过安全组限制仅应用服务器访问；不得把 5432 暴露公网。

## 5. 域名与 HTTPS 流程

### 5.1 DNS

- 用户需决定正式域名，例如根域名或子域名；文档中不记录真实域名。
- 为正式入口配置 A 记录到云服务器公网 IPv4；若服务器有稳定 IPv6，可配置 AAAA。
- 可以选择只使用根域名，或使用 `www`/业务子域名并将另一个 301 跳转到主域名。
- DNS TTL 可在切换期先设较短，稳定后再调高。

### 5.2 Caddy 自动 HTTPS 前置条件

- 域名 A/AAAA 已解析到云服务器公网 IP。
- 云服务器安全组和主机防火墙允许 TCP 80/443 入站。
- Caddy 能从公网接收 ACME HTTP-01/TLS-ALPN-01 验证请求。
- 服务器时间正确。
- 域名没有被错误代理到旧主机或内网地址。

### 5.3 应用配置

- Caddy 监听 80/443；80 自动跳转 HTTPS。
- 前端与 API 同源，`CORS_ALLOWED_ORIGINS=`。
- `ADMIN_COOKIE_SECURE=true`。
- `SERVER_HOST=127.0.0.1`，`APP_PORT=8080`。
- `TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128`。
- 不在文档、Git、systemd unit 明文命令行或 shell history 中记录真实秘密。秘密放入权限受限的 env 文件或秘密管理系统。

## 6. 数据库迁移流程设计

### 6.1 切换前只读预检

- 本机正式数据库保持只读核对，不改业务数据。
- 核对迁移：`count=19`、最高 `0019_admin_auth_audit_events.sql`、关键表存在。
- 核对金额口径：订单金额、已付、未付、付款分摊、void 后金额不漂移。
- 核对关键关系：users/orders/order_items/payments/payment_items/import_batches/schema_migrations 外键和行数大类一致。
- 核对当前后端无正在进行的导入、付款录入或管理员导出任务。

### 6.2 一致性备份

- 切换窗口开始前通知冻结写入：暂停管理员导入、付款、用户管理、查询码重置、邮箱绑定等写操作。
- 停止或临时阻断旧后端写入，确认无活动业务连接。
- 使用 `pg_dump --format=custom --no-owner --no-privileges` 创建逻辑备份。
- 生成 SHA256 和 metadata；备份文件不进入 Git。
- 上传前加密，或使用 SSH/SFTP/scp/rsync over SSH 等安全传输；禁止通过公开网盘或聊天传输。

### 6.3 云端隔离恢复验证

- 先恢复到隔离测试库，不直接恢复到正式库。
- 使用 `pg_restore --no-owner --no-privileges --exit-on-error --single-transaction`。
- 核对迁移 `19 / 0019 / 1`。
- 核对关键表、外键、索引、序列、金额汇总、付款分摊、void 状态、导入历史。
- 启动后端指向隔离库做内部只读冒烟；不对公网开放。

### 6.4 正式恢复和切换

- 新建正式库和最小权限数据库用户。
- 将已验证 dump 恢复到正式库。
- 配置后端 env 指向云端正式库。
- 启动后端，确认迁移不会重复异常执行。
- Caddy 先用临时域名或 hosts 内部验收，再切 DNS。
- DNS 切换后继续保持旧本机环境只读保留一段观察期。

### 6.5 回滚

- 若云端恢复失败：不切 DNS，继续使用本机/内网环境。
- 若 DNS 已切但 HTTPS/应用失败：将 DNS 回滚到旧入口或维护页；由于当前旧入口不是公网入口，实际公网回滚更现实的做法是切到云端上一版备份或临时维护页。
- 若数据写入已在云端发生，不得简单回滚旧库覆盖；需要先评估新写入是否可迁回，必要时暂停服务并人工决定。
- 所有回滚前必须保留失败现场日志和数据库备份，不得就地清理。

## 7. 秘密迁移设计

- 为云端生成新的数据库密码，使用最小权限数据库用户；不要复用本机管理员密码。
- 管理员会话配置保留 `ADMIN_SESSION_TTL`，HTTPS 下启用 Secure Cookie。
- `RECOVERY_EMAIL_ENCRYPTION_KEY` 与 `RECOVERY_EMAIL_HMAC_KEY` 必须随已有加密邮箱数据一起安全迁移；如果更换密钥，需要单独设计解密重加密迁移，不能直接替换。
- `RECOVERY_EMAIL_VERIFICATION_HMAC_KEY`、`QUERY_CODE_RECOVERY_HMAC_KEY` 可按轮换规则生成独立新值；切换时会使未完成验证码/重置令牌失效。
- SMTP 凭据由用户在云端受保护位置配置；不提交 Git，不写入命令行参数，不粘贴到聊天。
- Linux 推荐：`/etc/pjsk/backend.env` 或秘密管理服务；文件 owner 为 root，group 为 `pjsk`，权限 `0640`，后端服务账号只读。
- systemd unit 使用 `EnvironmentFile=/etc/pjsk/backend.env`；unit 本身不写真实秘密。
- 禁止在 shell history 中用 `export` 直接写入真实配置值；必要时使用交互式编辑受保护文件或秘密管理工具。

## 8. 公网安全验收设计

上线前必须至少验证：

- HTTPS 证书有效，浏览器无证书警告。
- HTTP 自动跳转 HTTPS。
- TLS 至少支持 TLS 1.2/1.3，无弱协议。
- 公网扫描确认 8080/5432 不可达；安全组也禁止它们入站。
- `/api/admin/*` 未认证返回 401。
- 普通用户会话不能访问管理员页面数据和管理员接口。
- 登录限流正常，错误提示不区分账号是否存在。
- 普通用户页面和 JSON 不泄露 `.xlsx`、`IMP-`、订单号、SKU、SHA、导入 ID、数据库主键等技术字段。
- 320 px 移动端无横向溢出；有订单明细卡片可读。
- SMTP 启用后，验证码邮件能发送且日志不含验证码、邮箱明文以外的敏感上下文或 SMTP 凭据。
- 备份任务成功，备份可恢复到隔离库。
- 后端、Caddy、PostgreSQL 服务重启恢复正常。
- 整机重启后服务自动恢复，80/443 正常，8080/5432 仍不可公网访问。
- 日志不含密码、查询码、Cookie、验证码、令牌、连接串或 SMTP 密码。
- 至少两地/两网络真实设备访问验证：普通用户登录、0 订单、有订单只读、注销、管理员只读关键页。

## 9. 推荐切换步骤

1. 用户选择云服务器规格、操作系统、正式域名、是否启用 IPv6、SMTP 服务商和备份保存位置。
2. 服务器基础准备：系统更新、创建普通服务账号、安装 Go/Node 或使用构建产物、安装 PostgreSQL/Caddy、配置时区和时间同步。
3. 配置防火墙/安全组：仅开放 80/443；SSH 最小暴露；禁止 8080/5432。
4. 部署应用：构建后端二进制和前端静态文件，写入 Linux systemd unit、Caddyfile 和受限 env 文件。
5. 数据库准备：创建库和最小权限用户；先恢复隔离库验证，再恢复正式库。
6. 配置域名 DNS：A/AAAA 指向云服务器公网地址，等待解析生效。
7. Caddy 自动 HTTPS：验证证书申请成功，HTTP 跳 HTTPS。
8. 内部验收：使用 hosts 或临时域名完成管理员端、普通用户端、移动端、安全边界和备份恢复演练。
9. 小范围试用：邀请少量真实用户跨地区访问，观察错误率、日志、资源占用和邮件送达。
10. 正式开放：确认无阻止问题后公告正式域名。
11. 观察期：保留本机旧环境和切换前备份，只读保存，不新增写入。

## 10. 用户需要决定或购买的事项

- 云服务器供应商、地域、操作系统镜像、CPU/内存/磁盘/带宽规格。
- 正式域名或子域名，是否需要 `www` 跳转。
- 是否启用 IPv6。
- SMTP 服务来源、发件域名、发件地址和发信额度。
- 备份保留位置：同机备份、云盘快照、对象存储或另一台机器；是否加密和异地保存。
- SSH 运维方式：固定源 IP、密钥登录、是否禁用密码登录、是否需要堡垒/控制台。
- 上线窗口、写入冻结时长、回滚接受条件。

## 11. 本轮未做事项

- 未购买或登录任何云服务器。
- 未修改 DNS。
- 未开放公网端口。
- 未上传、导出或恢复真实数据库。
- 未修改本机 Caddy、防火墙、PostgreSQL、Windows 服务或网络类别。
- 未修改真实业务数据。
- 未提交或推送 Git。

## 12. 结论

当前代码和架构适合部署到 Linux 云服务器。推荐先使用单机 Linux 云服务器：Caddy 负责 80/443 与自动 HTTPS，后端和 PostgreSQL 均保持 loopback-only，前端同源静态发布，数据库每日逻辑备份并定期恢复演练。下一步必须由用户先选择服务器和域名，再进入受控实施。
