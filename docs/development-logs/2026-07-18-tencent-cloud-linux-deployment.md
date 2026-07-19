# 腾讯云 Ubuntu Linux 部署只读调查与分阶段方案（2026-07-18）

> 本轮仅调查本地仓库并编写尚未执行的部署方案。未连接 `43.161.238.145`，未安装软件，未运行迁移，未连接或修改数据库，未读取本地真实 `.env` 内容，未部署、提交或推送，未使用子代理。

## 1. 调查基线与范围

- 本地仓库：`D:\pjsk`。
- 分支：`main`。
- HEAD：`898a711d6376d34069931ba275d0c9297fa72c50`。
- origin：`https://github.com/sugui1015-droid/pjsk-goods-manager.git`。
- 调查开始时：`main...origin/main`，ahead/behind 均为 0，工作树与暂存区干净。
- 目标主机事实（仅采用任务提供的信息，未联网验证）：Ubuntu 24.04.4 LTS、2 vCPU、4 GB RAM、60 GB 系统盘；公网安全组仅 TCP 22/80/443；预设目录 `/opt/pjsk`、`/etc/pjsk`、`/var/lib/pjsk`、`/var/lib/pjsk/backups`、`/var/log/pjsk`。
- 已阅读/扫描：`AGENTS.md`、根/前后端 README、`HANDOVER.md`、环境示例、Caddy/Nginx/Windows 部署文档、数据库备份/恢复/保留文档、迁移编号核对文档、全部开发日志的标题与部署相关内容，并完整复核与本次 Linux 部署直接相关的开发日志。

## 2. 构建、启动与运行要求

### 2.1 Go 后端

- `backend/go.mod` 明确要求 **Go 1.26.0**；本机只读版本检查为 `go1.26.5 windows/amd64`。
- Go module：`pjsk/backend`；生产入口是 `backend/main.go` 的根 `package main`，不是 `backend/cmd/*`。
- 本地验证命令（在 `backend/`）：

  ```bash
  go test ./...
  go vet ./...
  go build ./...
  ```

- Linux 发布二进制建议在受控构建机交叉构建，不要求生产服务器安装 Go：

  ```powershell
  Set-Location D:\pjsk\backend
  $env:GOOS = 'linux'
  $env:GOARCH = 'amd64'
  $env:CGO_ENABLED = '0'
  go build -trimpath -o <release-root>\bin\pjsk-backend .
  Remove-Item Env:\GOOS,Env:\GOARCH,Env:\CGO_ENABLED
  ```

- 后端通过 `SERVER_HOST` + 端口监听。主机默认 `127.0.0.1`；端口优先级为 `APP_PORT` > `SERVER_PORT` > `BACKEND_PORT` > `8080`。生产建议明确设 `SERVER_HOST=127.0.0.1`、`APP_PORT=8080`。
- HTTP server 地址由 `net.JoinHostPort` 生成；生产入口应保持 `127.0.0.1:8080`，不得开放到公网接口。
- 启动时先加载配置、连接并 Ping PostgreSQL（10 秒预算），随后**自动执行嵌入二进制的全部迁移**（2 分钟预算），最后才开始监听 HTTP。因此“启动后端”本身是数据库写入操作。

### 2.2 Node.js、pnpm/npm 与前端

- `frontend/package.json` 没有 `engines` 和 `packageManager`，仓库**未精确钉住 Node/pnpm/npm 版本**。
- 当前 Vite 8 依赖在 lockfile 中要求 Node `^20.19.0 || >=22.12.0`；本机只读检查为 Node `v24.18.0`、pnpm `11.11.0`、npm `11.16.0`。为复现当前成功环境，发布构建建议固定 Node 24.x + pnpm 11.11.0；正式采用前应把版本钉入仓库，当前这是可复现性风险而非应用启动阻断。
- `pnpm-lock.yaml` 的 `lockfileVersion` 为 `9.0`。
- 前端命令（在 `frontend/`）：

  ```bash
  pnpm install --frozen-lockfile
  pnpm test
  pnpm run build
  ```

- `build` 实际执行 `vue-tsc -b && vite build`，产物为 `frontend/dist`。生产不运行 Vite，不开放 5173。
- 同源部署保持 `VITE_API_BASE_URL` 为空；`VITE_BACKEND_TARGET` 只供 Vite 开发代理，不是生产后端变量。
- `frontend/public/templates/pjsk-goods-import-template.xlsx` 是 Git 跟踪的公开业务模板，会被 Vite 复制到 `dist/templates/`，属于合法发布静态资源。

### 2.3 PostgreSQL 与迁移

- 仓库没有声明 PostgreSQL 最低/唯一大版本。历史真实验证环境是 PostgreSQL 18.4（Windows），但这不是代码中的版本约束。
- SQL 使用 `pgcrypto`、`gen_random_uuid()`、`jsonb`、事务 DDL、普通/部分唯一索引等能力；Ubuntu 24.04 自带 PostgreSQL 16 是合理候选，但**当前仓库没有 PostgreSQL 16 的实测证据**。必须在隔离库完整跑 21 个迁移和集成验收后才能确认支持，不能把“语法看起来兼容”写成已验证。
- 当前迁移共 **21 个文件**，按完整文件名字节序执行并把完整文件名写入 `schema_migrations.version`：`0001` 至 `0021`，其中两个 `0005_*`、永久缺 `0006` 是既定历史例外；禁止重命名、删除、回填 `0006` 或手改迁移记录。
- 2026-07-16 公网计划和旧只读 SQL仍写“19 条/最高 0019”，已被 `0020_payment_qr_codes.sql`、`0021_payment_submissions.sql` 淘汰。部署核对必须改为 **21 条、最高 `0021_payment_submissions.sql`**，同时做仓库清单与目标库记录的双向全量比较。
- 迁移没有独立 CLI；唯一正式运行方式是启动当前 Go 后端，由 `database.RunMigrations` 创建/读取 `schema_migrations`，逐文件单事务应用。单个文件失败会回滚该文件并阻止后端启动，之前已提交的迁移保留。
- `0001` 会执行 `create extension if not exists pgcrypto`，数据库角色必须有足够权限；推荐由数据库管理员在首启前显式创建 `pgcrypto`，应用角色保持库 owner/最小必要 schema 权限，不授予 superuser。

## 3. 生产环境变量清单

### 3.1 Go 后端实际读取

| 类别 | 变量 | 生产要求 |
| --- | --- | --- |
| 环境 | `APP_ENV` | `production` |
| 监听 | `SERVER_HOST` | `127.0.0.1` |
| 监听端口 | `APP_PORT` / `SERVER_PORT` / `BACKEND_PORT` | 三选一，建议仅设 `APP_PORT=8080`；优先级依次降低 |
| 数据库方式 A | `DATABASE_URL` | 非空时优先；不要与 split 方式混用 |
| 数据库方式 B | `DATABASE_HOST`、`DATABASE_PORT`、`DATABASE_USER`、`DATABASE_PASSWORD`、`DATABASE_NAME`、`DATABASE_SSLMODE` | 推荐本机 TCP：host `127.0.0.1`、port `5432`、专用角色、`sslmode=disable`；公网绝不开放 5432 |
| 管理会话 | `ADMIN_SESSION_TTL` | 正时长，如 `12h` |
| Cookie | `ADMIN_COOKIE_SECURE` | HTTPS 必须 `true` |
| 可信代理 | `TRUSTED_PROXY_CIDRS` | 同机 Caddy：`127.0.0.1/32,::1/128` |
| CORS | `CORS_ALLOWED_ORIGINS` | 前后端同源时留空；production 空值表示不允许跨域 |
| 邮箱加密 | `RECOVERY_EMAIL_ENCRYPTION_KEY`、`RECOVERY_EMAIL_HMAC_KEY` | 可同时为空；一旦已有/启用恢复邮箱必须同时配置并保留原密钥。前者 Base64 解码恰好 32 字节，后者至少 32 字节 |
| 邮箱验证码 | `RECOVERY_EMAIL_VERIFICATION_HMAC_KEY` | SMTP 模式必填，Base64 解码至少 32 字节 |
| 查询码找回 | `QUERY_CODE_RECOVERY_HMAC_KEY` | production 无条件必填，Base64 解码至少 32 字节，且不得与其他密钥复用 |
| 邮件模式 | `RECOVERY_EMAIL_SENDER_MODE` | `disabled` 或 `smtp`；production 禁止 `fake` |
| SMTP | `RECOVERY_EMAIL_SMTP_HOST`、`RECOVERY_EMAIL_SMTP_PORT`、`RECOVERY_EMAIL_SMTP_USERNAME`、`RECOVERY_EMAIL_SMTP_PASSWORD`、`RECOVERY_EMAIL_SMTP_FROM`、`RECOVERY_EMAIL_SMTP_FROM_NAME`、`RECOVERY_EMAIL_SMTP_TLS_MODE` | 仅 `smtp` 模式配置；用户名/密码必须同时有或同时空；TLS mode 仅 `tls`/`starttls`，最低 TLS 1.2 且强制证书校验 |
| 旧系统展示 | `LEGACY_STREAMLIT_ADMIN_PORT`、`LEGACY_STREAMLIT_USER_PORT` | Go 配置仍读取，默认 8512/8513；新 Linux 生产不运行 legacy 时可不设 |

### 3.2 非当前 Go 生产运行变量

- 根 `.env.example` 的 `TZ` 是通用进程/系统时区变量，不由配置包直接读取；服务器建议系统时区保留 UTC 或明确设为 Asia/Shanghai，但数据库时间与 API 时间仍按 UTC 处理。
- `PJSK_DATA_DIR`、`SUPABASE_URL`、`SUPABASE_ANON_KEY`、`SUPABASE_SERVICE_ROLE_KEY`、`SUPABASE_STORAGE_BUCKET`、`UPLOAD_DIR`、`MAX_UPLOAD_SIZE_MB` 属于 legacy Streamlit 配置，不是新 Go 后端生产依赖。
- `VITE_API_BASE_URL` 是前端**构建时**变量；同源生产应为空。`VITE_BACKEND_TARGET` 只用于 Vite 开发。
- `PJSK_RUN_DB_INTEGRATION_TESTS`、`PJSK_TEST_DATABASE_ADMIN_DSN` 等是测试门禁，不得出现在生产服务配置。

## 4. 健康检查、邮件、上传与静态目录

- 健康检查：`GET /health`。它在 3 秒内 Ping 数据库；成功返回 HTTP 200，JSON 含 `service=pjsk-backend`、`status=ok`、`database=connected`；数据库不可达时返回 503。
- `GET /api/config` 是公开的前端能力状态接口，但不应替代 `/health` 探活。
- Caddy 只反代 `/api/*` 与 `/health`；所有其他路径（包括 `/admin/*`、`/query/*` 的前端深链）由 Vue 静态站点和 `index.html` SPA fallback 处理。
- Excel 导入最大 20 MiB；二维码最大 5 MiB；付款凭证最大 10 MiB。Caddy request body 上限应至少 25 MB，覆盖最大 Excel multipart 请求及少量表单开销。
- 二维码 `payment_qr_codes.image_data` 和付款凭证 `payment_submissions.image_data` 都存 PostgreSQL `bytea`；Excel 上传读取后写结构化数据库记录。**新 Go 栈没有持久上传目录要求**，图片随数据库逻辑备份一起备份。
- `ParseMultipartForm` 可能使用操作系统临时目录；systemd 服务应启用 `PrivateTmp=true`，服务用户无需拥有 `/opt/pjsk` 写权限。二维码/付款凭证 handler 会调用 `MultipartForm.RemoveAll()`；Excel 文件在 20 MiB 内通常驻留内存。
- 邮件通过 SMTP 直接发出，不写 spool/附件目录；只需要受限 env 配置和到批准 SMTP 主机/端口的出站网络。
- 静态资源目录：`/opt/pjsk/current/frontend/`（发布包内 `frontend/dist` 内容）。Caddy 只读。
- 后端日志建议进入 journald；Caddy access/error 日志可写 `/var/log/pjsk/caddy/` 并滚动。任何日志不得记录密钥、DSN、Cookie、验证码、查询码、图片内容或 SMTP 密码。
- 数据库数据由 PostgreSQL 自己管理（默认 `/var/lib/postgresql/...`），不要放进 `/opt/pjsk`。逻辑备份使用 `/var/lib/pjsk/backups`，建议 owner `postgres:postgres`、mode `0700`；同盘备份必须再加密复制到异地介质。

## 5. Windows 专属与 Linux 兼容性

### 5.1 仅适用于 Windows/不能原样用于 Linux

- `backend/run.cmd`、`frontend/run.cmd`。
- `deploy/windows-service/*`：WinSW/NSSM、Windows 路径和 PowerShell 启动示例。
- `docs/windows-service-deployment.md` 及相关 Windows 服务、防火墙、NSSM/WinSW 操作记录。
- `scripts/database/*.ps1`：备份、恢复、validation、保留扫描/清理和安全测试全部是 PowerShell/Windows 路径语义；Linux 当前没有等价已验证脚本。
- 文档中的 `D:\PostgreSQL\18\bin`、`D:\PJSK-*`、NTFS ACL、Windows Firewall、SCM 服务名等均不能照搬。
- legacy Streamlit 是独立 Python 应用；理论上可跨平台，但不属于本次 Vue+Go Linux 生产结构，不应随新栈一起启动或开放 8512/8513。

### 5.2 可在 Linux 正常运行/构建的功能

- Go API、嵌入式 SQL 迁移、管理员/普通用户会话与限流。
- Excel 预览/导入、历史、订单、用户、付款、导出。
- 恢复邮箱加密/HMAC、SMTP TLS/STARTTLS、查询码找回。
- 收款二维码上传/展示/禁用、付款凭证提交/审核；图片在数据库内，不受路径分隔符影响。
- Vue production 静态构建、Caddy 静态服务、SPA fallback 和 API 反代。
- PostgreSQL 逻辑数据模型。尚需在 PostgreSQL 16/目标大版本进行完整隔离实测，不能省略。

## 6. 敏感文件与禁止上传项检查

### 6.1 Git 跟踪树

- 当前 Git 跟踪 321 个文件。
- 仅跟踪 `.env.example` 与 `backend/.env.example`，未跟踪真实 `.env`。
- 未发现跟踪的 `.dump`、`.backup`、`.partial`、私钥/证书私钥、数据库文件、可执行文件或真实非空密钥赋值。
- 内容扫描命中两类已知假值：`backend/internal/logsafe/logsafe_test.go` 的脱敏测试 DSN，以及文档 `CHANGE_ME` 占位；均非真实凭据。
- 唯一跟踪的相关二进制业务资产是公开 Excel 导入模板 `frontend/public/templates/pjsk-goods-import-template.xlsx`，应随前端发布。

### 6.2 本地被忽略但真实存在的禁止上传项

- `backend/.env`（只检查了文件名/大小/时间，**未读取内容**）。
- `frontend/.env.development`（只检查元数据）。
- `backups/` 与 `pjsk-data-backup/`：各 3 个文件、约 2.71 MB，含 CSV 与付款图片，可能是真实历史业务数据。
- `backend.log`、`.claude/settings.local.json`、`push.cmd`、缓存、`backend/bin/`、`frontend/dist/`、`frontend/node_modules/` 和本地导出 CSV 等忽略项。
- 结论：**禁止**对 `D:\pjsk` 做整目录压缩、`scp -r` 或通配复制；`.gitignore` 不是发布包安全边界。发布包必须在仓库外新建空目录，只逐项复制已验证的 Linux 后端二进制、`frontend/dist`、revision 与校验清单。

## 7. 推荐 Linux 部署结构

```text
Internet
  └─ TCP 80/443 -> Caddy (system package/systemd)
       ├─ /api/*, /health -> 127.0.0.1:8080
       └─ other paths -> /opt/pjsk/current/frontend + SPA fallback

systemd pjsk-backend (User=pjsk, Group=pjsk)
  └─ /opt/pjsk/current/bin/pjsk-backend
       └─ 127.0.0.1:5432 -> PostgreSQL

/opt/pjsk/releases/<release-id>/  root:root, directories/binary 0755, files 0644
/opt/pjsk/current                 symlink to active release
/etc/pjsk/backend.env             root:pjsk 0640
/etc/caddy/Caddyfile              root:root 0644 (no secrets)
/var/lib/pjsk/backups             postgres:postgres 0700
/var/log/pjsk/caddy               caddy-owned writable logs
```

- 应用账户 `pjsk` 为 system user、nologin、非 root；只读应用与 env，不拥有 Caddy/PostgreSQL 配置。
- Caddy 与 PostgreSQL 使用各自发行版服务账户；不要让 `pjsk` 账户兼任数据库超级用户或 Caddy 用户。
- PostgreSQL `listen_addresses='localhost'`（或精确 `127.0.0.1,::1`），安全组/UFW 均不允许 5432；后端同理不允许 8080。
- systemd 建议 `NoNewPrivileges=true`、`PrivateTmp=true`、`ProtectSystem=strict`、`ProtectHome=true`、`PrivateDevices=true`、`RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`、失败重启并限流。

## 8. 代码传输方式比较与推荐

| 方式 | 优点 | 风险/成本 | 本项目结论 |
| --- | --- | --- | --- |
| 本地生成干净发布包后 `scp` | 云端不需要 Git/Go/Node；只上传 Linux 二进制和静态产物；可离线审计文件清单/SHA-256；服务器无仓库历史和 Deploy Key | 构建机必须可信；每次需生成、校验、上传新包 | **推荐**。最能隔离本地 ignored 秘密与源仓库，也适合单机、小规模、当前无 CI 的项目 |
| GitHub Deploy Key 拉取 | 更新源码方便，可锁定只读 key/commit | 服务器长期持有 Deploy Key；需 GitHub 出站与供应链可用；仍需云端安装 Go/Node/pnpm 并构建；误把工作树当运行目录；源代码和 `.git` 常驻 | 当前不推荐。只有后续建立 CI、受控 runner、key 轮换和不可变构建流程后再评估 |

推荐流程不是“压缩仓库”，而是“空发布目录 + 显式复制产物 + 生成 manifest/SHA-256 + `scp`”。真实 env、数据库 dump 与发布包分开传输、分开授权、分开审计。

## 9. 分阶段部署计划（全部尚未执行）

以下命令是下一轮获准连接服务器后的执行草案。尖括号必须先替换；任何停止点未通过都不得继续。

### 阶段 0：决策与只读预检

**前置条件**：用户提供正式域名、DNS 控制权、SMTP 决策、数据库来源（全新或从现役迁移）、备份异地位置、上线窗口；确认 SSH key 登录。

**实际命令**：

```bash
ssh <admin>@43.161.238.145
cat /etc/os-release
uname -m
nproc
free -h
df -hT /
timedatectl
ss -lntup
systemctl --failed
sudo apt-cache policy postgresql caddy
```

**验收命令**：

```bash
test "$(uname -m)" = x86_64
ss -lnt | grep -E ':(8080|5432)[[:space:]]' && echo 'STOP: port conflict' || true
getent ahostsv4 <DOMAIN>
```

**回滚方式**：全程只读，无需回滚。

**明确停止点**：OS/架构/磁盘不符、端口冲突、系统已有未知服务失败、正式域名未解析到该主机，立即停止。没有正式域名时只能做内部 HTTP 验收，不进入公网 443/正式切换。

### 阶段 1：本地验证并生成干净发布包

**前置条件**：本地 Git 仍为预定 commit、工作树/暂存区干净；Node/pnpm 版本已固定；不读取或复制 ignored 文件。

**实际命令（本地 PowerShell）**：

```powershell
Set-Location D:\pjsk
git status --short --branch
git rev-parse HEAD
$releaseId = (git rev-parse --short=12 HEAD)
$releaseRoot = "C:\tmp\pjsk-release-$releaseId"
New-Item -ItemType Directory -Path "$releaseRoot\bin","$releaseRoot\frontend" -Force

Set-Location D:\pjsk\backend
go test ./...
go vet ./...
$env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
go build -trimpath -o "$releaseRoot\bin\pjsk-backend" .
Remove-Item Env:\GOOS,Env:\GOARCH,Env:\CGO_ENABLED

Set-Location D:\pjsk\frontend
pnpm.cmd install --frozen-lockfile
pnpm.cmd test
pnpm.cmd run build
Copy-Item -LiteralPath .\dist\* -Destination "$releaseRoot\frontend" -Recurse

Set-Content -LiteralPath "$releaseRoot\REVISION" -Value (git -C D:\pjsk rev-parse HEAD) -Encoding ascii
tar.exe -czf "C:\tmp\pjsk-release-$releaseId.tar.gz" -C $releaseRoot .
$hash=(Get-FileHash -Algorithm SHA256 "C:\tmp\pjsk-release-$releaseId.tar.gz").Hash.ToLower()
Set-Content -LiteralPath "C:\tmp\pjsk-release-$releaseId.tar.gz.sha256" -Value "$hash  pjsk-release-$releaseId.tar.gz" -Encoding ascii
```

**验收命令**：

```powershell
tar.exe -tzf "C:\tmp\pjsk-release-$releaseId.tar.gz"
Get-ChildItem -LiteralPath $releaseRoot -Recurse -Force
rg -n -i '\.env|dump|backup|private key|password|token|secret' $releaseRoot
```

清单必须只有 `bin/pjsk-backend`、`frontend/**`、`REVISION`；不得有源码、`.git`、`.env`、日志、备份、node_modules、缓存或本地导出。

**回滚方式**：构建失败不改变现役环境；将失败包移到隔离诊断目录并重新从空目录构建，不覆盖已签收的旧发布包。

**明确停止点**：任一测试/构建失败、Git 不干净、包清单越界或 SHA-256 不能复现，禁止上传。

### 阶段 2：服务器基础包、账户、目录与防火墙

**前置条件**：阶段 0 通过；保留腾讯云控制台救援能力与当前 SSH 会话；先开第二个 SSH 会话验证 22。

**实际命令**：

```bash
sudo apt update
sudo apt upgrade
sudo apt install postgresql postgresql-client caddy
sudo adduser --system --group --home /var/lib/pjsk --no-create-home --shell /usr/sbin/nologin pjsk
sudo install -d -o root -g root -m 0755 /opt/pjsk /opt/pjsk/releases
sudo install -d -o root -g pjsk -m 0750 /etc/pjsk
sudo install -d -o pjsk -g pjsk -m 0750 /var/lib/pjsk
sudo install -d -o postgres -g postgres -m 0700 /var/lib/pjsk/backups
sudo install -d -o root -g adm -m 0750 /var/log/pjsk
sudo install -d -o caddy -g adm -m 0750 /var/log/pjsk/caddy

sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

**验收命令**：

```bash
id pjsk
getent passwd pjsk
namei -l /opt/pjsk /etc/pjsk /var/lib/pjsk/backups /var/log/pjsk/caddy
sudo ufw status verbose
ss -lntup
```

从第二个终端重新 SSH 登录，确认安全组和 UFW 都只允许 22/80/443；不得出现 8080/5432 allow 规则。

**回滚方式**：如 UFW 影响 SSH，保持当前会话，用 `sudo ufw disable`，或通过腾讯云控制台恢复；软件/用户不急于删除，保留日志后人工处理。

**明确停止点**：第二个 SSH 会话不能建立、目录 owner/mode 不符、包来源/版本异常或 60 GB 磁盘余量不足，停止。

### 阶段 3：PostgreSQL 隔离准备、备份与恢复验证

**前置条件**：明确全新库还是迁移库；迁移库已有新鲜 custom-format dump、SHA-256、来源迁移清单，并经安全通道单独上传；禁止把 dump 放进 `/opt/pjsk/releases`。

**实际命令**：

```bash
sudo -u postgres psql -Atqc "show server_version; show listen_addresses; show password_encryption;"
sudo -u postgres createuser --pwprompt --no-superuser --no-createdb --no-createrole pjsk_app
sudo -u postgres createdb --owner=pjsk_app pjsk_restore_test_<STAMP>
sha256sum -c /var/lib/pjsk/backups/<dump>.sha256
pg_restore --list /var/lib/pjsk/backups/<dump>
sudo -u postgres pg_restore --dbname=pjsk_restore_test_<STAMP> --no-owner --no-privileges --role=pjsk_app --exit-on-error --single-transaction /var/lib/pjsk/backups/<dump>
```

若 `show listen_addresses` 不是 `localhost` 或精确回环列表，先执行 `sudoedit /etc/postgresql/<MAJOR>/main/postgresql.conf`，设置 `listen_addresses = 'localhost'`，再 `sudo systemctl restart postgresql` 并重新验收；不得配置 `*` 或服务器公网/私网地址。

若为全新库，不恢复 dump；先在隔离库用候选后端完成 21 迁移验证，再另建正式库。

**验收命令**：

```bash
sudo -u postgres psql -X -d pjsk_restore_test_<STAMP> -c "select count(*), min(version), max(version) from schema_migrations;"
sudo -u postgres psql -X -d pjsk_restore_test_<STAMP> -c "select version from schema_migrations order by version;"
sudo -u postgres psql -X -d pjsk_restore_test_<STAMP> -c "select extname from pg_extension where extname='pgcrypto';"
sudo -u postgres psql -X -d pjsk_restore_test_<STAMP> -c "select to_regclass('public.payment_qr_codes'), to_regclass('public.payment_submissions');"
sudo -u postgres psql -Atqc "show listen_addresses;"
ss -lnt | grep ':5432'
```

刚恢复完成时，迁移集合必须与**来源备份基线**完全一致（来源可能仍是 19/0019）；不得伪称恢复本身会自动增加 0020/0021。随后必须在该隔离恢复库上用候选后端执行一次启动迁移，最终得到 21 条完整迁移、最高 `0021_payment_submissions.sql`、两个 `0005_*` 都存在、无未知/中间缺失版本，并完成关键表/约束/金额/图片行数的只读校验。候选启动方法见阶段 4 的隔离迁移门禁。

**回滚方式**：验证失败时不删除失败库、不建正式库、不启动生产后端；保留输出和 dump，修正流程后使用新的测试库名重试。不要就地覆盖或 `pg_restore --clean`。

**明确停止点**：PostgreSQL 大版本兼容未证实、hash/list/restore 任一步失败、迁移集合不等于当前 21 文件、`pgcrypto` 权限不足、5432 非回环监听，立即停止。

### 阶段 4：上传不可变发布、配置 env 与 systemd

**前置条件**：阶段 1 包已签收；阶段 3 隔离验证通过；正式密钥已由授权人员准备，禁止通过聊天/命令行参数传值。

**实际命令**：

```powershell
scp "C:\tmp\pjsk-release-$releaseId.tar.gz" "C:\tmp\pjsk-release-$releaseId.tar.gz.sha256" <admin>@43.161.238.145:/tmp/
```

```bash
cd /tmp
sha256sum -c pjsk-release-<RELEASE>.tar.gz.sha256
sudo install -d -o root -g pjsk -m 0750 /opt/pjsk/releases/<RELEASE>
sudo tar -xzf pjsk-release-<RELEASE>.tar.gz -C /opt/pjsk/releases/<RELEASE>
sudo chown -R root:root /opt/pjsk/releases/<RELEASE>
sudo find /opt/pjsk/releases/<RELEASE> -type d -exec chmod 0755 {} +
sudo find /opt/pjsk/releases/<RELEASE> -type f -exec chmod 0644 {} +
sudo chmod 0755 /opt/pjsk/releases/<RELEASE>/bin/pjsk-backend
sudo ln -sfn /opt/pjsk/releases/<RELEASE> /opt/pjsk/current.next

sudo install -o root -g pjsk -m 0640 /dev/null /etc/pjsk/backend.env
sudoedit /etc/pjsk/backend.env
sudoedit /etc/systemd/system/pjsk-backend.service
sudo systemctl daemon-reload
```

`backend.env` 只写第 3 节所列生产变量。unit 建议内容：

```ini
[Unit]
Description=PJSK Goods Manager Backend
After=network-online.target postgresql.service
Wants=network-online.target
Requires=postgresql.service
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
User=pjsk
Group=pjsk
WorkingDirectory=/opt/pjsk/current
EnvironmentFile=/etc/pjsk/backend.env
ExecStart=/opt/pjsk/current/bin/pjsk-backend
Restart=on-failure
RestartSec=5s
UMask=0027
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictSUIDSGID=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6

[Install]
WantedBy=multi-user.target
```

**验收命令**：

```bash
readlink -f /opt/pjsk/current.next
sudo -u pjsk test -r /etc/pjsk/backend.env
sudo -u pjsk test ! -w /etc/pjsk/backend.env
sudo -u pjsk test -x /opt/pjsk/releases/<RELEASE>/bin/pjsk-backend
sudo grep -E '^(User|Group|EnvironmentFile|ExecStart|ProtectSystem|PrivateTmp)=' /etc/systemd/system/pjsk-backend.service
```

在正式库首启前，还必须建立仅用于隔离恢复库的 `/etc/pjsk/backend.test.env`（root:pjsk 0640，使用独立测试密码/一次性 HMAC，`DATABASE_NAME=pjsk_restore_test_<STAMP>`、`APP_PORT=18080`），再用候选 release 完成自动迁移兼容性门禁：

```bash
sudo install -o root -g pjsk -m 0640 /dev/null /etc/pjsk/backend.test.env
sudoedit /etc/pjsk/backend.test.env
sudo systemd-run --unit=pjsk-backend-migration-test --uid=pjsk --gid=pjsk \
  --working-directory=/opt/pjsk/releases/<RELEASE> \
  --property=EnvironmentFile=/etc/pjsk/backend.test.env \
  /opt/pjsk/releases/<RELEASE>/bin/pjsk-backend
curl --fail --silent http://127.0.0.1:18080/health
sudo -u postgres psql -X -d pjsk_restore_test_<STAMP> -c "select count(*), max(version) from schema_migrations;"
sudo systemctl stop pjsk-backend-migration-test.service
```

只有隔离库最终为 21/0021 且服务没有 restart loop，才能把正式 `backend.env`/正式库带入下一阶段。测试 unit 停止后保留日志；测试 env 在取证完成后按已批准的秘密销毁流程处理，不复制进 release 或备份。

**回滚方式**：尚未切 `current`、尚未启动服务，删除/隔离候选 release 链接即可；保留上一 release 与 env 备份。不要删除数据库或 dump。

**明确停止点**：env 权限不是 0640/root:pjsk、服务用户可写 release/env、包 hash 不符、unit 校验失败，禁止切换和启动。

### 阶段 5：Caddy、DNS 与 HTTPS

**前置条件**：正式域名 A 记录已指向 `43.161.238.145`；80/443 从公网可达；Caddy 用户可读静态 release、可写自身日志；不得用裸公网 IP 冒充已完成正式 HTTPS。

**实际命令**：

```bash
sudoedit /etc/caddy/Caddyfile
sudo caddy fmt --overwrite /etc/caddy/Caddyfile
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

Caddyfile 目标结构：

```caddyfile
<DOMAIN> {
    encode zstd gzip

    @backend path /api/* /health
    handle @backend {
        reverse_proxy 127.0.0.1:8080 {
            header_up X-Forwarded-For {remote_host}
            header_up X-Forwarded-Proto {scheme}
            header_up Host {host}
        }
    }

    handle {
        root * /opt/pjsk/current/frontend
        try_files {path} /index.html
        file_server
    }

    request_body {
        max_size 25MB
    }

    header {
        X-Content-Type-Options nosniff
        X-Frame-Options DENY
        Referrer-Policy strict-origin-when-cross-origin
        -Server
    }

    log {
        output file /var/log/pjsk/caddy/access.log {
            roll_size 10MiB
            roll_keep 10
            roll_keep_for 720h
        }
    }
}
```

**验收命令**：

```bash
systemctl is-active caddy
journalctl -u caddy -n 100 --no-pager
curl -I http://<DOMAIN>/
curl -I https://<DOMAIN>/
openssl s_client -connect <DOMAIN>:443 -servername <DOMAIN> </dev/null
```

**回滚方式**：恢复 `/etc/caddy/Caddyfile` 的已知可用备份并 `caddy validate` + reload；若证书未签发，保持维护页/不切 DNS。Caddy 配置回滚不触碰数据库。

**明确停止点**：DNS 不一致、ACME/证书失败、Caddy validate 失败、HTTPS 有浏览器警告或 HTTP 不跳 HTTPS，禁止正式开放。

### 阶段 6：正式库切换、首次启动与自动迁移

**前置条件**：现役写入已冻结；有刚生成且已隔离恢复验证的备份；目标正式库只读核对通过；候选 release/env/Caddy 全部验收；明确此次启动会写 `schema_migrations` 和新 schema。

**实际命令**：

```bash
# 分支 A（全新库）：仅当正式库尚不存在时执行。
sudo -u postgres createdb --owner=pjsk_app pjsk
sudo -u postgres psql -d pjsk -c 'create extension if not exists pgcrypto;'

# 分支 B（迁移既有数据）：不要执行上面的分支 A；新建空库后直接恢复，
# 不预建 extension，避免与 dump 内对象重复。全新库则跳过本分支。
sudo -u postgres createdb --owner=pjsk_app pjsk
sudo -u postgres pg_restore --dbname=pjsk --no-owner --no-privileges --role=pjsk_app --exit-on-error --single-transaction /var/lib/pjsk/backups/<dump>

sudo ln -sfn /opt/pjsk/releases/<RELEASE> /opt/pjsk/current
sudo systemd-analyze verify /etc/systemd/system/pjsk-backend.service
sudo systemctl start pjsk-backend
```

**验收命令**：

```bash
systemctl is-active pjsk-backend
systemctl show pjsk-backend -p User -p Group -p MainPID -p NRestarts
journalctl -u pjsk-backend -n 200 --no-pager
ss -lntp | grep '127.0.0.1:8080'
curl --fail --silent http://127.0.0.1:8080/health
curl --fail --silent https://<DOMAIN>/health
curl --fail --silent https://<DOMAIN>/api/config
sudo -u postgres psql -X -d pjsk -c "select count(*), max(version) from schema_migrations;"
```

**回滚方式**：

- 二进制/前端故障且数据库 schema 向后兼容：停止后端，`current` 指回上一 release，启动并复查。
- 若新迁移已经提交，**仅回滚二进制不等于数据库回滚**。不得手工删表/删 `schema_migrations`。停止服务，把启动前 dump 恢复到一个新数据库，核验后切换 DSN；保留失败数据库供诊断。
- 尚未切 DNS时继续保留旧环境；已有云端新写入后禁止直接用旧库覆盖，先冻结并人工评估数据合并。

**明确停止点**：服务发生 restart loop、日志出现迁移失败、health 非 200/connected、8080 监听非回环、迁移不是完整 21 条，立即停服务且不开放用户流量。

### 阶段 7：业务、安全、重启与小流量验收

**前置条件**：阶段 6 稳定；仅使用批准测试账号/数据；先做只读路径，再做明确授权的最小写入验收。

**实际/验收命令**：

```bash
curl -I http://<DOMAIN>/
curl --fail https://<DOMAIN>/health
curl -s -o /dev/null -w '%{http_code}\n' https://<DOMAIN>/api/admin/me
curl -I https://<DOMAIN>/admin/orders
curl -I https://<DOMAIN>/query/payment
sudo ss -lntup
sudo ufw status numbered
sudo systemctl restart pjsk-backend
sudo systemctl restart caddy
systemctl is-active postgresql pjsk-backend caddy
```

还需人工验收：首页/SPA 深链、管理员与普通用户权限、Excel 20 MiB 边界、二维码、付款凭证、导出、SMTP（如启用）、320 px 页面、日志脱敏、不同网络真实设备访问；最终安排一次整机重启验收。

**回滚方式**：小流量失败立即停止开放/切维护页，后端回上一 release；涉及数据库写入时按阶段 6 的数据库回滚边界处理。

**明确停止点**：任何越权、Cookie 非 Secure、8080/5432 公网可达、上传或财务口径异常、SMTP 泄密、重启不能自恢复，禁止扩大流量。

### 阶段 8：Linux 备份、恢复演练与观察期

**前置条件**：生产稳定；确定加密和异地目的地；Linux 备份脚本尚未实现，先人工命令并双人复核。

**实际命令（首次人工基线）**：

```bash
sudo -u postgres pg_dump --format=custom --no-owner --no-privileges --file=/var/lib/pjsk/backups/pjsk-<UTCSTAMP>.dump pjsk
sudo -u postgres pg_restore --list /var/lib/pjsk/backups/pjsk-<UTCSTAMP>.dump
sudo -u postgres sha256sum /var/lib/pjsk/backups/pjsk-<UTCSTAMP>.dump | sudo -u postgres tee /var/lib/pjsk/backups/pjsk-<UTCSTAMP>.dump.sha256
```

随后把该 dump 恢复到新的 `pjsk_restore_test_<STAMP>`，按阶段 3 做 21 迁移与业务不变量验证；成功后才写 validation 记录并复制到加密异地位置。

**验收命令**：

```bash
sudo -u postgres sha256sum -c /var/lib/pjsk/backups/pjsk-<UTCSTAMP>.dump.sha256
sudo -u postgres pg_restore --list /var/lib/pjsk/backups/pjsk-<UTCSTAMP>.dump >/dev/null
sudo -u postgres psql -X -d pjsk_restore_test_<STAMP> -c "select count(*), max(version) from schema_migrations;"
df -h / /var/lib/pjsk/backups /var/log/pjsk
```

**回滚方式**：备份任务失败不删除上一份成功备份；不启用自动清理。恢复演练只用新测试库，失败库保留诊断并换新名称重试。

**明确停止点**：未完成一次真实恢复演练、没有异地副本、备份目录权限过宽、磁盘容量无告警/保留策略，部署不能视为完整生产交付。

## 10. 当前风险、未完成事项与下一步

### 高风险/阻断

1. 尚无正式域名和 DNS 信息；Caddy 自动公网 HTTPS 不能验收。
2. 目标 PostgreSQL 是全新库还是从现役库迁移尚未明确；真实 dump、恢复演练、21 条迁移只读核对均未执行。
3. 现有 Linux 备份/validation/保留脚本不存在；Windows `.ps1` 不能直接复用。
4. 本地有 ignored 的真实 `.env`、CSV、付款图片和备份目录；整仓库打包/递归 scp 会泄露数据。
5. Go 要求 1.26.0；若改为服务器源码构建，需要额外安装/钉住 Go。推荐上传预编译静态二进制规避。

### 中风险

1. Node/pnpm 未在 manifest 精确钉住；当前只以本机 Node 24.18.0 + pnpm 11.11.0 为复现基线。
2. PostgreSQL 最低版本未声明；18.4 有历史实测，Ubuntu 24.04 的候选 PostgreSQL 16 尚需隔离验证。
3. 旧部署文档的 19/0019 口径过时；当前必须使用 21/0021。
4. 单机 60 GB 同时承载数据库、日志和备份，图片存 bytea 会增加数据库/dump 体积；必须监控并做异地备份，不能仅依赖同盘目录。
5. SMTP 未决定；若保持 disabled，邮件验证/查询码邮件找回不可用；若启用，需要 DNS 发信记录、TLS、退信和额度验收。

### 建议下一步

1. 用户先确认正式域名、数据库迁移来源、SMTP、备份异地位置和部署窗口。
2. 单独评审并批准 Linux 部署文档/脚本批次：Caddyfile、systemd unit、release 打包脚本、只读迁移清单生成器；仍不连接服务器。
3. 在本地/隔离 Linux 环境验证 Go `linux/amd64` 二进制、Node/pnpm 固定版本、PostgreSQL 16 的 21 迁移与关键功能。
4. 获得明确服务器连接授权后，从阶段 0 开始逐阶段执行，每个停止点单独汇报，禁止跨阶段自动继续。

## 11. 本轮 Git 与安全边界

- 本轮唯一修改：新增本调查日志。
- 未删除、移动或重命名任何历史开发日志。
- 未连接腾讯云、数据库、SMTP、GitHub 或其他外部服务。
- 未运行构建、迁移、备份、恢复、安装或部署命令；文中的部署命令全部是尚未执行的计划。
- 未暂存、提交或推送。
- 未使用子代理。

## 12. 阶段 2：运行环境、构建与 PostgreSQL 隔离兼容性验证（2026-07-18）

### 12.1 阶段 2A 本地基线复核

- 已重新阅读 `AGENTS.md`、本文前序调查、根/前后端 README、`HANDOVER.md`、内网部署与密钥文档、迁移编号核对、数据库备份/恢复/保留、Windows 服务化、Caddy 示例、两份环境变量示例以及 Go/前端 manifest/lockfile。
- 当前分支：`main`。
- 当前 HEAD：`e03e4467c11dd27fff8443c3e1fcc5d3ee86c5ce`（上一轮仅提交本调查日志）。
- 本地 `origin/main` 引用：`898a711d6376d34069931ba275d0c9297fa72c50`；当前 `main` 比本地远端引用 ahead 1，未 fetch、未 push。
- 工作树与暂存区在本阶段开始时干净。
- 未读取或输出 SSH 私钥正文；私钥路径只作为 `ssh -i` 参数使用。

### 12.2 阶段 2A 首次服务器只读命令失败与停止点

- 计划操作：通过 SSH 一次性执行只读基线检查（身份、OS、CPU、内存、磁盘、时间、监听端口、目标目录权限/内容、相关包/服务/进程和失败服务）。
- 实际命令类别：`ssh.exe -i <LOCAL_SSH_PRIVATE_KEY> ... ubuntu@43.161.238.145 "set -eu; ..."`。日志不记录本机私钥绝对路径，也不记录/复制私钥正文。
- 结果：SSH 已到达远端 shell，但组合命令在 bash 解析阶段因本地到远端的引号嵌套不匹配而失败；错误为 `bash: -c: line 1: unexpected EOF while looking for matching '"'`。
- 退出码：`2`（bash 命令语法错误）。
- 服务器影响：整段 `bash -c` 在解析完成前即失败，没有进入命令执行；未安装软件、未写文件、未改目录/权限、未启停服务、未连接数据库、未改防火墙、未打开任何端口。
- 私钥边界：仅被 OpenSSH 作为身份文件读取；正文未由本轮命令、工具输出或日志显示。
- 依照任务硬约束“任何一步失败立即停止后续阶段，不用替代命令绕过，记录后等待人工确认”，本轮停在此处。未重试 SSH，未进入 2B 发布包、2C 安装、2D 上传构建、2E PostgreSQL 隔离验证或 2F 完成态。
- 建议修复：获得人工确认后，把远端只读检查改为无嵌套双引号的最小命令序列（或上传不含秘密的只读检查脚本后显式执行），先完成 2A 并确认无非预期服务/端口，再决定是否继续。
- 本阶段未使用子代理，未提交、未推送。

### 12.5 阶段 2A LF 修复后重跑失败与停止点

- 人工仅授权修复本地临时只读检查脚本的 CRLF 问题并重跑阶段 2A，不得进入 2B–2F。
- 已将本地临时脚本转换为 UTF-8 无 BOM、LF 换行；执行前验证结果为 2100 bytes、CR 字符数 0、LF 字符数 98、末字节 `0A`，未发现 PowerShell/Bash 提示符或写入、安装、服务状态变更、数据库操作命令。脚本 SHA-256 为 `6E0485E6D265EE79730819572C6EA82C09EAA3535CBE83CFAAE115A70841E5D5`。
- 上一次失败原因仍明确为 CRLF：远端 `bash -s` 把末尾 CR 解释为额外命令，导致 SSH 总退出码 127。本次脚本自身的 CRLF 问题已在本地消除。
- 本次计划通过标准输入执行脚本，脚本未永久上传到服务器；但 Windows `cmd` 在管道命令中错误解析带引号的本地脚本路径及 SSH `-i` 路径，先报告 `The filename, directory name, or volume label syntax is incorrect.`，随后 OpenSSH 报告身份文件不可访问并以公钥认证失败结束。
- 本次 SSH 总退出码为 `255`。由于 SSH 认证前即失败，远端 `bash -s` 未执行，阶段 2A 的系统、端口、目录、软件、服务及进程检查没有得到新的复验结果；未读取或输出私钥正文。
- 按“整个 SSH 命令最终退出码非 0 即立即停止、不得继续”的要求，本次不再改用其他本地管道或引号方式重试。阶段 2A **仍未正式通过**；之前取得的服务器基线结果不因本次失败而改变，但不能据此替代本轮要求的成功复验。
- 本次没有修改服务器文件，没有安装软件、上传发布包、创建用户/数据库/角色/配置文件，没有启动、停止或重启服务，也没有进入 2B–2F。
- 本轮未使用子代理，未提交、未推送；停止并等待新的人工确认。

### 12.3 数据迁移方向确认（人工补充）

- 用户已明确：本地 PostgreSQL 保存的是必须保留的现役数据；腾讯云最终目标是迁移该现役库，不是创建空库替代。
- 因此删除“空库直接上线”这一正式部署分支。云端正式库只能在以下链路全部通过后建立：冻结现役写入 → 生成一致性逻辑备份与 SHA-256 → 云端隔离测试库恢复 → 校验来源迁移集合与关键业务不变量 → 候选后端仅在隔离库补齐至 21/0021 → 再新建云端正式库并恢复同一份已验证备份 → 启动候选后端应用待执行迁移 → 验收后切换。
- 阶段 2E 本身仍只允许创建 `pjsk_compat_test` 等隔离角色/数据库；不得创建正式 `pjsk`，不得读取、备份、停止、恢复或修改本地现役数据库。现役数据导出与迁移必须放入后续获明确授权的停写窗口。
- 数据库回滚边界：任何隔离恢复或兼容性验证失败都不得影响本地现役库；云端正式切换后若已有新写入，不得用旧备份直接覆盖，必须先停写并评估增量数据处置。
- 本补充只记录部署决策；没有连接本地或云端 PostgreSQL，没有读取任何现役数据或凭据，阶段 2A 仍保持停止并等待人工确认是否允许重试只读基线命令。

### 12.4 阶段 2A 获准重试：只读检查结果与第二个停止点

- 人工已明确授权：只重试并完成阶段 2A，不得自动进入 2B–2F；允许把本地只读检查脚本经 stdin 传给远端 `bash -s`。
- 执行方式：在本机临时目录生成只含读取命令的脚本，先扫描确认没有安装、目录/权限修改、文件写入、服务启停、数据库操作等命令；随后用 `Get-Content -Raw | ssh.exe -i <LOCAL_SSH_PRIVATE_KEY> ... "bash -s"` 通过 stdin 执行。脚本未作为远端文件上传，私钥正文未读取或输出。
- 上一次失败与本次明确区分：12.2 是整段组合命令引号未闭合、一个检查也未执行；本次脚本中的全部计划只读检查均已执行并输出各自退出码，但脚本末尾混入 Windows CR 字符，远端最终把单独的 CR 解释为命令，报 `bash: line 99: $'\\r': command not found`，导致 SSH 总退出码 `127`。

#### 服务器基线（本次实际取得）

- OS：Ubuntu 24.04.4 LTS（Noble）；内核 `6.8.0-136-generic`；架构 `x86_64` / dpkg `amd64`。相关命令退出码均 0。
- CPU：2 vCPU，Intel Xeon E5-26xx v4，KVM；`nproc`/`lscpu` 退出码 0。`lscpu` 同时报告部分 CPU 漏洞状态为 vulnerable（MDS/MMIO stale data/spec store bypass）；这是云宿主/微码风险记录，阶段 2A 未修改内核或系统。
- 内存：3.6 GiB，总可用约 3.2 GiB；swap 1.9 GiB、当前使用 0。`free -h` 退出码 0。
- 系统盘：根分区 59 GiB，已用 6.0 GiB，可用 51 GiB，使用率 11%。`df -h /` 退出码 0。
- 时间：Asia/Shanghai（CST +08:00）；系统时钟已同步、NTP active、RTC 使用 UTC。`timedatectl` 退出码 0。
- 身份/主机：远端用户 `ubuntu`，主机名 `VM-0-15-ubuntu`，本次检查时 uptime 约 2 小时 12 分、load average `0.08/0.02/0.01`。三项退出码均 0。
- sudo 只读权限：`sudo -n ss -lntp` 与 `sudo -n ls -ld ...` 均退出 0，无密码提示或权限异常。

#### 监听与目录

- 全部 TCP 监听：`0.0.0.0:22`、`[::]:22`（sshd/systemd），以及仅回环的 `127.0.0.53:53`、`127.0.0.54:53`（systemd-resolved）。没有 80、443、5432、8080、5173、5174、5175，也没有其他非回环 TCP 监听。
- 重点端口过滤退出 0，仅命中 22 双栈；loopback DNS 53 不在重点公网端口集合，且不构成公网服务。
- 目录状态（`ls -ld` 退出 0）：
  - `/opt/pjsk`：`drwxr-xr-x ubuntu:ubuntu`；
  - `/etc/pjsk`：`drwxr-x--- root:root`；
  - `/var/lib/pjsk`：`drwxr-x--- root:root`；
  - `/var/lib/pjsk/backups`：`drwxr-x--- root:root`；
  - `/var/log/pjsk`：`drwxr-x--- root:root`。
- 本阶段只读取目录元数据，没有列举或写入目录内容，没有修改 owner/mode。

#### 软件、包、服务与进程

- `command -v go/node/pnpm/psql/postgres/caddy` 均退出 1：命令当前不在 PATH，属于“未安装/未匹配”的允许结果。
- `dpkg-query` 候选过滤退出 1：未匹配 PostgreSQL、Caddy、Go、Node.js 或 npm 已安装包。
- 两个 systemd 候选过滤均退出 1：没有 PostgreSQL、Caddy 或 PJSK unit file，也没有相应已加载 service。
- 进程过滤退出 1：没有 postgres、caddy、pjsk、node 或 vite 进程。
- 未发现现存 PostgreSQL/Caddy/PJSK 服务或残留进程，未发现 5432/8080/Vite 端口监听。

#### 退出码异常、影响与结论

- 每项计划检查均记录退出码；非零项只来自允许的“未安装/未匹配”。所有服务器基线本身符合继续准备构建环境的条件，未发现服务/端口/权限阻断项。
- 但 stdin 脚本末尾 CR 字符造成额外的非计划空白命令失败，SSH 总退出码为 127。该命令没有写入行为，服务器没有被修改；没有安装软件、上传发布包、创建用户/数据库/角色/配置、启停服务或连接数据库。
- 按本轮规则“SSH 失败立即停止”，阶段 2A **尚不能形式化标记为通过**；本轮停止于此，不进入 2B–2F。修复方式是先把本地临时脚本规范化为 LF，再在获得新的人工确认后只重跑 2A；不得借此进入后续阶段。
- 本阶段未使用子代理，未提交、未推送。

### 12.6 阶段 2A 审计收口：证据性通过

- 人工决定结束阶段 2A 的重复重跑，不再为了取得形式上的 SSH 总退出码 0 而第三次执行相同的服务器基线检查。
- 阶段 2A 的正式判断以 12.4 已经成功取得并逐项记录退出码的服务器检查结果为依据：OS、内核与架构、CPU、内存、磁盘、时间与时区、当前用户与主机、全部监听端口、目标目录权限、候选软件包、systemd 服务和相关进程均已实际检查。
- 12.4 中所有实际基线检查命令均成功；非零结果仅为 `command -v`、包/服务/进程过滤没有匹配，明确表示相关软件未安装或相关对象不存在。未发现 PostgreSQL、Caddy、PJSK 服务或残留进程，未发现 5432、8080、5173–5175 监听，也未发现目录权限或其他部署阻断项。
- 12.4 的 SSH 总退出码 127 仅由 Windows CR 字符在全部计划检查结束后被远端 shell 解释为额外空白命令造成；12.5 的退出码 255 则由 Windows 本地管道及 SSH 私钥路径引号解析失败造成，远端没有执行。二者均属于执行器包装问题，不代表任何服务器基线检查失败，也没有修改服务器。
- 12.2、12.4、12.5 的失败记录全部原样保留，作为完整审计过程；本节仅补充最终判断，不删除、不覆盖或改写既有证据。
- 阶段 2A 最终结论：**证据性通过**。截至已取得的实际只读证据，服务器无部署阻断项，可以在新的明确授权范围内进入阶段 2B；本结论不授权或代表已执行 2C–2F。

### 12.7 阶段 2B：本地干净运行发布包构建与审查停止点

#### 开始基线

- 当前分支 `main`，HEAD `e03e4467c11dd27fff8443c3e1fcc5d3ee86c5ce`，本地 `origin/main` 为 `898a711d6376d34069931ba275d0c9297fa72c50`；`origin/main...HEAD` 为 behind 0 / ahead 1，未 fetch、未 push。
- 阶段开始时唯一工作树改动为本部署日志，暂存区为空。
- 本机版本：Go `go1.26.5 windows/amd64`、Node.js `v24.18.0`、pnpm `11.11.0`。`backend/go.mod` 声明 `go 1.26.0`；前端存在 `pnpm-lock.yaml`，`pnpm run build` 对应 `vue-tsc -b && vite build`。
- 仓库外目标 `C:\PJSK-Release-Staging\e03e446` 及复验目录、目标压缩包在开始前均不存在；随后仅新建全新的版本目录及其 `bin` 子目录，没有覆盖或清空既有目录。

#### 测试与构建结果

- 后端 `go test ./...` 退出 0，全部包通过或无测试文件；`go vet ./...` 退出 0、无输出。
- 仅在交叉编译 PowerShell 进程内设置 `GOOS=linux`、`GOARCH=amd64`、`CGO_ENABLED=0`，执行 `go build -trimpath -o C:\PJSK-Release-Staging\e03e446\bin\pjsk-backend .` 成功；`finally` 已清除三个环境变量。
- 后端产物大小 17,505,435 bytes，前四字节为 `7F 45 4C 46`，确认为 ELF 魔数而非 Windows PE；目标为 Linux amd64，未执行该二进制。SHA-256：`AE9FE6C2336986A086028074708DE903DE74302FAD89F15EEFA72F89F4205022`。
- `pnpm.cmd install --frozen-lockfile` 退出 0，锁文件未更新；pnpm 的自身更新元数据请求出现 `ERR_PNPM_META_FETCH_FAIL` 警告，但安装报告 `Already up to date`，未安装或升级全局 pnpm。
- `pnpm.cmd test` 退出 0：185 项通过、0 失败、0 跳过。`pnpm.cmd run build` 退出 0，Vite 8.1.4 完成生产构建；仅把 `frontend/dist` 的 6 个文件显式复制到发布目录的 `frontend`。
- 构建后一次中间态 `git status` 曾短暂显示非预期未跟踪文件 `.claude/settings.local.json`；本阶段没有读取、修改、删除或复制该文件。最终复核时该路径已不再出现在工作树状态中，当前唯一改动仍为本部署日志。

#### 发布目录与元数据

- 发布目录顶层为 `bin`、`frontend`、`REVISION`、`MANIFEST.sha256`；`bin` 中仅有 `pjsk-backend`。
- 业务文件 8 个：后端二进制 1 个、前端生产文件 6 个、`REVISION` 1 个；加上 `MANIFEST.sha256` 后发布目录共 9 个文件，总大小 17,866,959 bytes。
- `REVISION` 仅包含完整 HEAD。`MANIFEST.sha256` 排除自身并记录全部 8 个业务文件的相对路径和 SHA-256。
- 清单检查未发现 `.git`、`.env`、密钥扩展名、dump/backup/partial、CSV、日志、`node_modules`、Go/Vue/TypeScript 源码或 Windows exe/cmd/ps1；前端包含允许的 SVG 构建图片与公开 Excel 导入模板。

#### 敏感扫描结果与停止原因

- 扫描范围严格限制为新发布目录，没有读取真实 `backend/.env`、其他 ignored 环境文件或本地秘密目录。
- `DATABASE_URL=`、`DATABASE_PASSWORD=`、`RECOVERY_EMAIL_ENCRYPTION_KEY=`、`RECOVERY_EMAIL_HMAC_KEY=`、`QUERY_CODE_RECOVERY_HMAC_KEY=`、`SMTP_PASSWORD=`、`Authorization:` 和精确 `D:\pjsk` 均无命中。
- 后端二进制命中通用字符串 `PRIVATE KEY`、`Bearer`、`localhost`、`127.0.0.1` 及 Windows 盘符模式；这些可能来自解析、认证、默认回环监听或校验代码，但因随后已触发明确的前端停止点，本阶段没有继续做发布批准判定。
- 前端生产 bundle `frontend/assets/index-kdHsKTnn.js` 明确命中 `http://localhost:5173` 与 `http://127.0.0.1:5173`，上下文为 `frontendOrigins` 配置。它们是 Vite 开发地址，违反“前端静态产物不得包含开发 API 地址、Vite 地址或 Windows 本地路径”的发布要求。
- 阶段 2B 因此前端产物审查失败而立即停止。没有生成 `pjsk-release-e03e446.tar.gz` 或其 SHA-256 文件，没有进行解压复验，也没有连接或修改腾讯云服务器。
- 未上传文件、未安装软件、未创建用户/数据库/角色、未启动后端、未运行迁移、未提交、未推送、未使用子代理；未进入阶段 2C。

### 12.8 阶段 2B-Fix：生产 bundle 开发地址修复与 retry1 发布包

#### 根因与调用链

- 原候选 `C:\PJSK-Release-Staging\e03e446` 因前端 bundle 包含 `http://localhost:5173` 与 `http://127.0.0.1:5173` 被拒绝，未生成压缩包。本轮将其保留并重命名隔离为 `C:\PJSK-Release-Staging\rejected-e03e446-localhost-bundle`，没有覆盖或删除。
- 两个字符串的真实源码来源是 `frontend/src/App.vue` 的 `fallbackConfig.frontendOrigins`。`load()` 并行请求同源 `/health` 与 `/api/config`；请求失败时才把 `config` 恢复为 `fallbackConfig`。
- `frontendOrigins` 在前端没有其他消费调用，不参与 API 请求地址、CORS 决策、登录跳转、`postMessage`/message origin、iframe 或页面导航。API 基址仍由 `frontend/src/api/client.ts` 独立决定：开发模式使用相对路径交给 Vite 代理，生产默认空基址并使用当前页面同源。
- 实际 CORS 安全判断在后端 `config.go` → `router.go/withCORS`：production 未配置时为空白名单；有 Origin 时仅精确匹配允许项，未知跨域来源不返回允许头；同源浏览器请求没有 Origin 头，不需要 CORS。既有定向 Go 测试覆盖这些语义。
- Vite 原先没有裁剪两个地址，是因为它们处于无条件对象字面量中，不属于 `import.meta.env.DEV` 可静态替换的死分支。

#### 最小修复与测试

- 修改 `frontend/src/App.vue`：`fallbackConfig.frontendOrigins` 改为 `import.meta.env.DEV ? localDevelopmentFrontendOrigins() : []`，生产同源部署不硬编码域名，也不扩大未知 origin 权限。
- 新增 `frontend/src/developmentOrigins.ts`：只提供两个精确的本地 Vite 开发 origin；仅由 DEV 分支调用，生产构建可静态消除整个模块及字符串。
- 新增 `frontend/tests/development-origins.test.mjs` 两项测试：开发辅助函数保留两个精确来源；App 的生产分支明确为空且不再使用无条件字面量。
- 定向测试：新前端测试 2/2 通过；`go test ./internal/api -run TestCORS` 与 `go test ./internal/config -run TestLoadCORSAllowedOrigins` 均通过，确认开发来源、production 默认、同源请求与未知跨域拒绝语义未变。
- `git diff --check` 通过。完整阶段 2B 重跑中：`go test ./...` 退出 0，`go vet ./...` 退出 0；`pnpm.cmd install --frozen-lockfile` 退出 0（锁文件未变化，只有 pnpm 自身更新元数据网络警告）；完整前端测试 187/187 通过；`pnpm.cmd run build` 退出 0，Vite 8.1.4 构建成功。
- 修复后的 `frontend/dist` 与最终发布/解压目录均无 `http://localhost:5173`、`http://127.0.0.1:5173`、其他 `:5173`、`/@vite`、`vite/client` 或 Windows 本地路径。

#### retry1 发布目录

- 从全新空目录 `C:\PJSK-Release-Staging\e03e446-retry1` 重建，未复用旧二进制、旧前端或旧元数据。
- 后端仅在构建 PowerShell 进程内设置 `GOOS=linux`、`GOARCH=amd64`、`CGO_ENABLED=0` 并在 `finally` 清除；`go build -trimpath` 成功。产物 17,505,435 bytes，魔数 `7F 45 4C 46`，Linux amd64 ELF SHA-256 为 `AE9FE6C2336986A086028074708DE903DE74302FAD89F15EEFA72F89F4205022`，未执行该二进制。
- 新发布目录顶层仅有 `bin`、`frontend`、`REVISION`、`MANIFEST.sha256`；`bin` 仅有 `pjsk-backend`。业务文件 8 个，加 manifest 后共 9 个文件、17,866,912 bytes。
- `REVISION` 按要求仅含当前完整 HEAD `e03e4467c11dd27fff8443c3e1fcc5d3ee86c5ce`；`MANIFEST.sha256` 排除自身并覆盖 8 个业务文件，发布目录内复验 8/8 通过。
- 严格清单无 `.git`、env、私钥文件、备份/dump/partial、CSV、日志、`node_modules`、Go/Vue/TypeScript 源码或 Windows 可执行/脚本；保留允许的生产 JS/CSS/HTML/SVG 与公开 Excel 模板。

#### 敏感扫描逐类结论

- 扫描仅覆盖新发布目录和最终解压目录，没有读取 ignored 的真实 env 或其他秘密文件。
- 前端全部指定模式无命中：两个 Vite URL、`localhost`、`127.0.0.1`、`D:\pjsk`、Windows 盘符路径、`PRIVATE KEY`、秘密变量赋值、`Authorization:`、`Bearer` 均为 0。
- 后端 `http://localhost:5173` 与 `http://127.0.0.1:5173` 来自 `config.go` 的 development/test 默认精确 CORS 来源，不用于 production 默认值；`127.0.0.1` 也包含允许的默认回环监听配置。
- 后端 `PRIVATE KEY` 命中来自 Go 标准库 crypto/tls/x509 的算法、PEM 类型标签及错误文本，没有 PEM 边界或密钥正文；`Bearer` 命中来自 pgx 依赖的 OAuthBearer/SASL 支持文本，没有令牌值。
- 后端 Windows 盘符正则命中是不可读二进制字节偶合，不是路径；精确 `D:\pjsk` 无命中。所有带 `=` 的数据库、恢复邮件、查询码与 SMTP 秘密变量模式均无命中，`Authorization:` 也无命中；未发现真实凭据。

#### 压缩包与解压复验

- 生成 `C:\PJSK-Release-Staging\pjsk-release-e03e446-retry1.tar.gz`，大小 8,566,883 bytes，SHA-256 `779F81D9543E9794E44777C498B90C0145FFDDE335916957E5688D65979D0D43`；对应 `.tar.gz.sha256` 已生成并验证一致。
- tar 内容从 `./bin`、`./frontend`、`./REVISION`、`./MANIFEST.sha256` 开始，没有额外版本目录或绝对路径层。
- 解压到全新的 `C:\PJSK-Release-Staging\verify-e03e446-retry1`。解压后顶层严格为四个允许项；源发布目录与解压目录均为 9 个文件、17,866,912 bytes，逐文件 SHA-256 比较 0 差异。
- 解压后 `MANIFEST.sha256` 8/8 通过；第二次完整敏感扫描与原发布目录分类一致，前端仍无任何开发服务器 URL 或本地路径。
- 本轮没有连接或修改腾讯云服务器，没有上传、安装、创建数据库/用户/角色、启动后端、运行迁移、提交或推送；未使用子代理，完成后停在阶段 2B-Fix，不进入 2C。

### 12.9 阶段 2C：基础运行环境与安全基线

#### 本地与远端开始基线

- 本地 `main` HEAD 为 `14d339e56677b6faff5d0769279cb2feaf82a9bc`，相对本地 `origin/main` behind 0 / ahead 2；工作树和暂存区开始时干净。
- 已批准但本阶段禁止上传的发布包为 `C:\PJSK-Release-Staging\pjsk-release-14d339e-final.tar.gz`，大小 8,566,913 bytes，复核 SHA-256 为 `B0A20924D9C1F7844C64A8873E6CDDD33725B5E03BF2D738272E335A434201AB`。
- 远端用户 `ubuntu`，主机 `VM-0-15-ubuntu`；Ubuntu 24.04.4 LTS、`x86_64`、2 vCPU、3.6 GiB 内存、59 GiB 根盘（约 51 GiB 可用）、Asia/Shanghai 且 NTP 已同步。
- 安装前 `systemctl --failed` 为 0；仅 SSH 22 双栈和 systemd-resolved 回环 DNS 53 监听。`psql`、Caddy 命令不存在；没有 PostgreSQL、Caddy、PJSK service 或进程，没有 80、443、5432、8080、Vite 端口占用。
- `systemctl list-unit-files` 中 timesyncd 兼容别名显示 `bad`，但实际时间同步使用已启用的 chrony，且 failed unit 为 0；未视为失败服务阻断。

#### Ubuntu 官方仓库安装

- APT 源只包含 Ubuntu Noble 的 `main universe restricted multiverse`、updates、backports 与 security 套件；服务器实际使用 `mirrors.tencentyun.com/ubuntu` 的 Ubuntu 镜像，没有添加 PGDG、NodeSource 或其他第三方仓库。
- `sudo apt update` 退出 0；`sudo apt install -y postgresql postgresql-client caddy` 退出 0。没有执行 `curl | bash`，没有安装 Go、Node、pnpm、Docker 或面板。
- 新增 15 个包：`caddy 2.6.2-6ubuntu0.24.04.3`、`postgresql 16+257build1.1`、`postgresql-16 16.14-0ubuntu0.24.04.1`、`postgresql-client 16+257build1.1`、`postgresql-client-16 16.14-0ubuntu0.24.04.1`、`postgresql-client-common 257build1.1`、`postgresql-common 257build1.1`、`libpq5 16.14-0ubuntu0.24.04.1`、`libcommon-sense-perl 3.75-3build3`、`libjson-perl 4.10000-1`、`libjson-xs-perl 4.040-0ubuntu0.24.04.1`、`libllvm17t64 1:17.0.6-9ubuntu1`、`libnss3-tools 2:3.98-1ubuntu0.2`、`libtypes-serialiser-perl 1.01-1`、`ssl-cert 1.1.2ubuntu1`。
- `psql --version` 与服务器均为 PostgreSQL `16.14 (Ubuntu 16.14-0ubuntu0.24.04.1)`；`caddy version` 为 `2.6.2`。APT policy 确认 PostgreSQL 来自 noble-updates/main，Caddy 来自 noble-updates/security universe。

#### 目录、账户与权限

- 修改权限前只读 `find`：`/opt/pjsk`、`/etc/pjsk`、`/var/log/pjsk` 无条目；`/var/lib/pjsk` 仅有预期的空 `/var/lib/pjsk/backups`，没有非预期文件、软链接或目录。
- 新建系统账户 `pjsk`：UID 112、GID 113、仅组 `pjsk`、home `/var/lib/pjsk`、shell `/usr/sbin/nologin`，未创建 home 内容。
- 使用非递归 `install -d` 设置并验收：`/opt/pjsk root:root 0755`、`/opt/pjsk/releases root:root 0755`、`/etc/pjsk root:pjsk 0750`、`/var/lib/pjsk pjsk:pjsk 0750`、`/var/lib/pjsk/backups postgres:postgres 0700`、`/var/log/pjsk root:adm 0750`、`/var/log/pjsk/caddy caddy:adm 0750`。全部 `namei -l` 链路已核对。

#### PostgreSQL 安全基线

- 首批五个只读 `psql` 查询因本地到远端的双引号被剥离，参数被误解析为数据库名并退出 1；没有执行 SQL 或写操作。随后改用可保留远端单引号的参数形式复查，五项均退出 0。
- 结果：server `16.14`；`listen_addresses=localhost`；`port=5432`；`password_encryption=scram-sha-256`；data directory `/var/lib/postgresql/16/main`。
- 实际监听仅为 `127.0.0.1:5432`，没有监听 `0.0.0.0`、`[::]`、公网或私网地址。配置未显式覆盖 `listen_addresses`，使用 PostgreSQL 默认 localhost；因此没有备份、修改、reload 或 restart 配置。
- `pg_hba.conf` 的 host 行仅允许 `127.0.0.1/32` 与 `::1/128`，认证均为 `scram-sha-256`（普通连接与 replication）；不存在 `0.0.0.0/0` 或 `::/0` 公网放行。
- 未创建应用角色、数据库或 extension，未运行迁移、恢复或任何业务 SQL。

#### UFW 与人工停止点

- 在保持原始 SSH 会话打开的前提下，初始 `ufw status verbose` 为 inactive。依次设置 default deny incoming、default allow outgoing，只允许 `22/tcp`、`80/tcp`、`443/tcp`，所有命令退出 0；`ufw --force enable` 退出 0并设置开机启用。
- 当前编号规则：IPv4 `[1] 22/tcp`、`[2] 80/tcp`、`[3] 443/tcp`，IPv6 `[4] 22/tcp`、`[5] 80/tcp`、`[6] 443/tcp`，全部 `ALLOW IN Anywhere`；没有 5432、8080、5173–5175 或 22222 规则。
- 启用后监听：SSH `0.0.0.0:22`/`[::]:22`；Caddy `*:80` 和仅回环管理端口 `127.0.0.1:2019`；PostgreSQL `127.0.0.1:5432`；systemd-resolved `127.0.0.53:53`/`127.0.0.54:53`。443 当前无监听可接受；8080 和 Vite 端口无监听。
- 原始 SSH 会话仍保持打开。按照批准停止点，阶段 2C-6 尚未执行，正在等待用户从新的本地 PowerShell 人工确认第二个 SSH 会话成功；确认前不关闭原会话、不进行服务终验。
- 截至本停止点未上传发布包、未创建应用数据库/角色、未恢复数据、未运行迁移、未启动 PJSK 后端、未配置正式域名/HTTPS、未修改腾讯云安全组、未提交或推送；未使用子代理。

#### 第二 SSH 人工确认与服务终验

- 用户已从新的本地 PowerShell 人工确认第二个独立 SSH 会话成功登录；UFW 启用后 SSH 22 的新连接可用。确认前保持的原始 SSH 会话随后用于终验，并在全部结果通过后正常 `exit`，连接退出码 0。
- `systemctl is-active postgresql`、`systemctl is-enabled postgresql`、`systemctl is-active caddy`、`systemctl is-enabled caddy` 均退出 0；PostgreSQL 与 Caddy 均为 active/enabled。
- 终态 `systemctl --failed` 退出 0，结果为 `0 loaded units listed`。
- 终态全部 TCP 监听：SSH `0.0.0.0:22` 与 `[::]:22`；Caddy HTTP `*:80` 与仅回环管理端口 `127.0.0.1:2019`；PostgreSQL `127.0.0.1:5432`；systemd-resolved `127.0.0.53:53` 与 `127.0.0.54:53`。443 当前无监听可接受；没有 8080、5173、5174、5175 或其他 PJSK 后端监听。
- 终态 UFW active/enabled，完整规则仍仅为 IPv4/IPv6 的 22、80、443 TCP 入站允许；默认 incoming deny、outgoing allow，没有数据库、后端或开发端口规则。
- 阶段 2C 最终结论：**通过**。基础软件、服务账户、目录权限、PostgreSQL 回环隔离、pg_hba 与 UFW 安全基线均符合批准要求；可以在取得新的明确授权后进入阶段 2D，但本轮停在 2C。
- 阶段 2C 全程未上传发布包，未创建 `pjsk_app` 或任何应用数据库，未恢复现役数据，未运行迁移，未启动 PJSK 后端，未写正式 Caddyfile、未配置域名或 HTTPS，未修改腾讯云安全组，未安装未批准软件，未提交或推送；未使用子代理。

### 12.10 阶段 2D：不可变发布包上传与候选签收（2026-07-19）

#### 本地预检与远端空目录检查

- 本地分支 `main`，HEAD `14d339e56677b6faff5d0769279cb2feaf82a9bc`，相对本地 `origin/main` behind 0 / ahead 2；开始时唯一工作树改动为本部署日志，暂存区为空。
- 本地 tar `C:\PJSK-Release-Staging\pjsk-release-14d339e-final.tar.gz` 与 `.sha256` 均存在；tar 大小 8,566,913 bytes，重新计算 SHA-256 严格等于 `B0A20924D9C1F7844C64A8873E6CDDD33725B5E03BF2D738272E335A434201AB`，校验文件记录同一值。
- 上传前再次用 `tar -tzf`/`tar -tvzf` 检查：顶层仅 `bin`、`frontend`、`REVISION`、`MANIFEST.sha256`；无绝对路径、`../`、多余版本目录、软链接、设备、FIFO 或其他文件类型。Windows tar 元数据为目录 0777、文件 0666；按照批准流程，仅在 root-owned、未发布的目标目录中解压后立即统一规范为目录 0755、普通文件 0644、后端 0755，因此不作为内容安全阻断。
- 远端用户/主机为 `ubuntu@VM-0-15-ubuntu`；根盘 59 GiB、可用约 51 GiB。`/opt/pjsk root:root 0755`、`/opt/pjsk/releases root:root 0755`；releases 为空，`/opt/pjsk/current` 与 `/opt/pjsk/current.next` 均不存在。
- 上传前额外确认 `/tmp` 中两个同名目标不存在，避免 scp 静默覆盖。

#### 上传、远端 SHA 与 tar 安全签收

- 使用单次非递归 `scp`，只上传 `pjsk-release-14d339e-final.tar.gz` 与对应 `.sha256` 到 `/tmp`；scp 退出码 0，没有上传目录或其他文件，私钥仅作为 `-i` 参数使用且正文未读取/输出。
- 远端 tar 为 8,566,913 bytes、`ubuntu:ubuntu 0664` 普通文件；校验文件为 100 bytes、`ubuntu:ubuntu 0664` 普通文件。
- 远端独立 `sha256sum` 退出 0，得到 `b0a20924d9c1f7844c64a8873e6cddd33725b5e03bf2d738272e335a434201ab`；`sha256sum -c` 退出 0 并返回 `OK`，与本地批准值严格一致。
- 远端解压前 `tar -tzf`/`tar -tvzf` 均退出 0，内容与本地一致：仅目录和普通文件，无路径逃逸、异常顶层、软链接、设备或 FIFO。`/tmp` 两个上传文件按要求保留，未删除。

#### 不可变 release 与内容验收

- 执行前 `sudo test ! -e /opt/pjsk/releases/14d339e56677` 退出 0。创建并解压到 `/opt/pjsk/releases/14d339e56677`，随后按批准命令设置为 root:root；全部目录 0755、普通文件 0644、`bin/pjsk-backend` 0755。未修改任何发布文件内容。
- 顶层严格为 `MANIFEST.sha256`、`REVISION`、`bin`、`frontend`。`REVISION` 严格等于完整提交 `14d339e56677b6faff5d0769279cb2feaf82a9bc`。
- 在 release 根目录执行 `sha256sum -c MANIFEST.sha256` 退出 0：后端、2 个前端 asset、2 个 SVG、index.html、公开 Excel 模板与 REVISION 共 8/8 全部 `OK`。
- 后端大小 17,505,435 bytes，root:root 0755；前四字节 `7f 45 4c 46`；远端 SHA-256 严格等于 `5D8DA04E32595450007D4BFAC7E8449023DD51AD831DA264CAD43BA59A364C13`。
- 前端共有 6 个文件：CSS、JS、favicon.svg、icons.svg、index.html 与公开 Excel 导入模板。开发地址扫描退出 1、无输出，即 `http://localhost:5173`、`http://127.0.0.1:5173`、`/@vite`、`vite/client`、`D:\pjsk` 均无命中。
- `namei -l` 和完整 `find` 确认 release 及所有子目录 root:root 0755，普通文件 root:root 0644，后端 root:root 0755。以 `pjsk` 身份验证：后端可执行、index.html 可读，二者均不可写；四项 `test` 全部退出 0。

#### 候选链接与终态

- 全部验收通过后，创建 `/opt/pjsk/current.next`；`readlink -f` 严格等于 `/opt/pjsk/releases/14d339e56677`。未创建 `/opt/pjsk/current`。
- PostgreSQL 与 Caddy 均保持 active；`systemctl --failed` 为 0。UFW 仍仅允许 IPv4/IPv6 的 22、80、443 TCP。
- 终态监听保持为 SSH `0.0.0.0:22`/`[::]:22`、Caddy `*:80` 与回环管理端口 `127.0.0.1:2019`、PostgreSQL `127.0.0.1:5432`、回环 DNS 53；443 当前无监听，没有 8080 或 Vite 端口。
- `/opt/pjsk/current` 不存在，`/etc/pjsk/backend.env` 不存在，systemd unit 列表没有 pjsk，进程列表没有 `pjsk-backend`。
- 阶段 2D 最终结论：**通过**。候选 release 已完成不可变内容签收并停在 `current.next`，可以在新的明确授权下进入阶段 2E；本轮未进入 2E。
- 本阶段未创建数据库角色或应用数据库，未恢复现役数据，未创建 env 或 systemd 服务，未启动后端，未运行迁移，未修改 Caddyfile、UFW 或腾讯云安全组，未配置域名/HTTPS，未提交或推送；未使用子代理。

### 12.11 阶段 2E：PostgreSQL 隔离兼容性、21 条迁移与本机健康检查（2026-07-19）

#### 本地与远端只读基线

- 本地分支 `main`，HEAD `14d339e56677b6faff5d0769279cb2feaf82a9bc`，相对本地 `origin/main` behind 0 / ahead 2；开始时唯一工作树改动为本部署日志，暂存区为空。
- 代码复核确认：后端在启动时先连接 PostgreSQL，再按嵌入 SQL 的完整文件名排序逐条执行迁移，迁移全部成功后才监听 HTTP；每条迁移在独立事务中执行。`APP_ENV=production` 下本次一次性测试必须提供的应用密钥为 `QUERY_CODE_RECOVERY_HMAC_KEY`，disabled 邮件发送模式不要求恢复邮箱加密/HMAC 或 SMTP 凭据。
- 远端 `current.next` 仍指向 `/opt/pjsk/releases/14d339e56677`；`/opt/pjsk/current`、`/etc/pjsk/backend.env`、`/etc/pjsk/backend.test.env` 和正式 `/etc/systemd/system/pjsk-backend.service` 均不存在。没有 PJSK unit、后端进程、18080、8080 或 Vite 监听。
- PostgreSQL 为 `16.14 (Ubuntu 16.14-0ubuntu0.24.04.1)`，`listen_addresses=localhost`，实际仅监听 `127.0.0.1:5432`；PostgreSQL 与 Caddy 均为 active/enabled，failed unit 为 0。
- 首次枚举 `pjsk*` 角色和数据库时，Windows 到 SSH 的双引号传递把只读 SQL 拆成了参数，`psql` 在连接阶段以 peer authentication 错误退出 2，SQL 未执行且无写入。改用远端单引号和 PostgreSQL dollar-quote 后两条查询均退出 0、无输出，确认开始前不存在任何 `pjsk*` 角色或数据库；该引号问题不属于服务器或 PostgreSQL 基线失败。

#### 隔离角色、唯一测试库与一次性 env

- 测试角色：`pjsk_compat_app`。创建前在同一受控脚本中再次确认该角色、正式 `pjsk_app` 角色、正式 `pjsk` 数据库及任何旧 `pjsk_compat_test_*` 数据库均不存在。
- 测试数据库：`pjsk_compat_test_20260718T164403Z`，名称使用服务器 UTC 时间戳；owner 为 `pjsk_compat_app`，编码 UTF8，基于 `template0` 创建。
- 数据库密码和 `QUERY_CODE_RECOVERY_HMAC_KEY` 分别由服务器本地 `openssl rand -base64 48` 独立生成，未在聊天、日志或命令行参数中输出。角色密码只短暂进入随机命名、`postgres:postgres 0600` 的临时 SQL 文件，创建角色后立即删除；服务器 `password_encryption` 使用 `scram-sha-256`，只读验收确认保存格式为 SCRAM。
- 角色属性验收为 LOGIN=true，SUPERUSER/CREATEDB/CREATEROLE/REPLICATION 均为 false。数据库 owner、编码和角色属性查询均退出 0。
- 在隔离库显式创建 `pgcrypto`。迁移前 public schema 的业务表数量为 0，`schema_migrations` 不存在。
- 一次性配置保存在 `/etc/pjsk/backend.test.env`，owner/mode 为 `root:pjsk 0640`；`pjsk` 用户可读但不可写。内容仅为隔离库连接、`127.0.0.1:18080`、同源生产安全默认值、disabled 邮件模式及一次性查询恢复 HMAC；没有正式 SMTP、正式域名、现役数据库密码或真实恢复邮箱密钥。配置正文及秘密值未被输出。

#### 临时后端、健康检查与迁移结果

- 使用 transient unit `pjsk-backend-migration-test-20260718T164403Z.service` 启动 `/opt/pjsk/releases/14d339e56677/bin/pjsk-backend`；User/Group 为 `pjsk`，WorkingDirectory 为候选 release，EnvironmentFile 为测试 env，并设置 `NoNewPrivileges`、`PrivateTmp`、`PrivateDevices`、`ProtectSystem=strict`、`ProtectHome` 和 `Restart=no`。没有创建持久 unit 文件。
- 启动命令退出 0；启动后 unit 为 active/running、Result=success、NRestarts=0，仅监听 `127.0.0.1:18080`，没有 `0.0.0.0:18080` 或 IPv6 全接口监听。日志显示数据库连接成功、21 条迁移依次成功和最终回环监听；`.env not loaded` 仅表示不可变 release 中不存在 `.env`，配置由批准的 EnvironmentFile 提供。journal 敏感标记扫描为 0，未发现数据库密码、DSN、HMAC、SMTP 密码或其他秘密输出。
- 健康检查只从服务器本机请求 `http://127.0.0.1:18080/health`：curl 退出 0、HTTP 200，JSON 摘要为 `service=pjsk-backend`、`status=ok`、`database=connected`。
- `schema_migrations` 为 21 条，最高版本严格为 `0021_payment_submissions.sql`。完整集合与当前 release 嵌入迁移逐项一致：`0001_core_tables.sql`、`0002_import_tracking.sql`、`0003_admin_auth.sql`、`0004_import_confirm.sql`、`0005_import_history.sql`、`0005_product_series.sql`、`0007_import_revert.sql`、`0008_query_sessions.sql`、`0009_admin_payments.sql`、`0010_payment_voids.sql`、`0011_payment_fee_fields.sql`、`0012_normalize_payment_methods.sql`、`0013_cn_merge.sql`、`0014_user_query_account_admin.sql`、`0015_query_code_bind_tokens.sql`、`0016_user_recovery_email.sql`、`0017_recovery_email_verification_codes.sql`、`0018_query_code_email_recovery.sql`、`0019_admin_auth_audit_events.sql`、`0020_payment_qr_codes.sql`、`0021_payment_submissions.sql`。两个 `0005_*` 均存在，历史既定的 0006 缺口没有被误判或补造，无未知或缺失迁移。
- `pgcrypto`、`schema_migrations`、`payment_qr_codes` 和 `payment_submissions` 均存在。0020/0021 结构门禁全部通过：两张表主键存在；`payment_qr_codes` 有 2 个外键、付款方式检查、禁用一致性约束、`payment_qr_codes_active_method_unique` 部分唯一索引及 `payment_qr_codes_method_created_idx`；`payment_submissions` 有 3 个外键、状态检查、reject reason/approved link 约束及 user/status/CN/linked-payment 四个索引。
- 空库业务基线通过：`admins`、`users`、`projects`、`products`、`orders`、`order_items`、`payments`、`payment_items`、`payment_qr_codes`、`payment_submissions` 行数均为 0；没有真实用户、订单、付款、二维码或付款凭证数据。

#### 停止临时服务与终态

- `systemctl stop` 退出 0。停止后 transient unit 为 LoadState=not-found、ActiveState=inactive、SubState=dead、Result=success、MainPID=0、NRestarts=0；18080 和 8080 均无监听，没有 `pjsk-backend` 进程。journal 日志仍按 unit 名保留。
- 按批准要求保留 `pjsk_compat_app`、`pjsk_compat_test_20260718T164403Z`、`/etc/pjsk/backend.test.env` 和 journal，等待人工确认后再决定销毁；未创建正式 `pjsk` 数据库或 `pjsk_app` 角色。
- PostgreSQL 与 Caddy 最终仍为 active/enabled，failed unit 为 0。最终 TCP 监听为 SSH `0.0.0.0:22`/`[::]:22`、Caddy `*:80` 与回环管理端口 `127.0.0.1:2019`、PostgreSQL `127.0.0.1:5432`、systemd-resolved 回环 DNS 53；443 当前无监听，18080、8080 与 5173–5175 均无监听。
- UFW 保持 active，入站规则仍只允许 IPv4/IPv6 的 22、80、443 TCP；未增加 18080、8080 或 5432 规则。
- `/opt/pjsk/current` 仍不存在，`/opt/pjsk/current.next` 仍指向候选 release；`/etc/pjsk/backend.env` 和正式 `pjsk-backend.service` 仍不存在。
- 阶段 2E 最终结论：**通过**。PostgreSQL 16.14 已完成当前不可变 release 的隔离兼容性、完整 21 条迁移、关键 schema 与本机健康检查验证。
- 本阶段未恢复或读取现役数据，未创建正式数据库/角色/env/systemd，未切换 `current`，未配置域名/HTTPS，未修改 Caddyfile、UFW 或腾讯云安全组，未开放客户访问，未提交或推送；未使用子代理。完成后停在阶段 2E，不进入 2F。

### 12.12 阶段 2F：现役库预演备份与云端隔离恢复（失败停止，2026-07-19）

#### 本地只读来源基线

- 开始时本地仍为 `main`，HEAD `14d339e56677b6faff5d0769279cb2feaf82a9bc`，相对本地 `origin/main` behind 0 / ahead 2；唯一工作树改动为本部署日志，暂存区为空。
- 使用 `D:\PostgreSQL\18\bin` 下的 PostgreSQL 18.4 `psql`、`pg_dump`、`pg_restore`。凭据由本机默认 pgpass 自动解析；只确认 `%APPDATA%\postgresql\pgpass.conf` 存在，未读取或输出其内容。`PGPASSWORD` 与 `DATABASE_URL` 均未设置，未读取 `backend/.env`。
- 本地 `postgresql-x64-18` 为 Running/Automatic。所有来源查询均显式置于 `BEGIN TRANSACTION READ ONLY`/`ROLLBACK`；实际 `current_database=pjsk`、server `18.4`、`transaction_read_only=on`、数据库大小 12,359,359 bytes、快照捕获时该库活跃连接数 1，`pgcrypto` 存在。
- 来源迁移严格为 19 条、最高 `0019_admin_auth_audit_events.sql`；完整集合从 `0001_core_tables.sql` 到 `0019_admin_auth_audit_events.sql` 与来源现役库实际记录一致，包含两个 `0005_*`、没有 0006。来源库尚无 0020/0021 表，符合“恢复后由候选后端补迁移”的预期。
- 只读取聚合不变量，未选择任何用户邮箱、密码 hash、查询码、Token、备注、文件名、图片或业务明细。核心行数：users 45、orders 44、order_items 120、import_batches 1、payments 7、payment_items 19；订单总金额与明细金额均为 6318.44，payment_items 已应用金额为 1145.40。完整 baseline 还记录全部核心/安全辅助表行数、数量与金额合计、状态分布、bytea 非空计数和 UTC 创建时间边界。

#### 一致性 custom-format dump 与本地验证

- 为保证 dump 与聚合 baseline 属于同一个 MVCC 快照，保持一个 `REPEATABLE READ READ ONLY` 事务，调用 `pg_export_snapshot()`；`pg_dump --snapshot` 使用该快照，同一事务内重新计算迁移清单和全部业务聚合后回滚。没有停止本地 PostgreSQL，也没有修改或迁移现役库。
- 新建仓库外目录 `C:\PJSK-Migration-Staging\20260718T165637Z`，此前不存在且未覆盖。目录最终严格只有四个文件：`pjsk-preflight-20260718T165637Z.dump`、对应 `.dump.sha256`、`source-baseline.json`、`source-migrations.txt`。
- `pg_dump 18.4` 使用 custom format、no-owner、no-privileges、单数据库和唯一文件名，退出 0；开始 `2026-07-18T16:56:37.5031580Z`，结束 `2026-07-18T16:56:37.7636610Z`。dump 大小 121,804 bytes，SHA-256 `5E0E39AE39CC9BCC7A66D5D77D529E3F6A38DF58D3B59C87E24B3E858B5D7200`。
- 本地重新计算 SHA 与 `.sha256`/baseline 严格一致；`pg_restore 18.4 --list` 退出 0，TOC 174 项，包含 `schema_migrations` 表及 TABLE DATA；JSON 可解析，迁移清单 19 行且最高 0019。四文件总大小 131,478 bytes。
- 第一次元数据敏感字段检查把允许的聚合键 `user_query_code_bind_tokens` 中的 `token` 误判为秘密属性并主动退出；这是验证器规则过宽，不是产物秘密命中。未上传前将规则收紧为顶层字段严格白名单，并允许附件要求的邮件/Token 相关表纯聚合键；完整验证重跑通过。私钥边界、数据库密码赋值、DATABASE_URL、PostgreSQL DSN、查询/恢复 HMAC、SMTP 密码和 Bearer 凭据值模式均为 0 命中。

#### 上传、云端兼容性阻断与停止点

- 首次远端预检的磁盘命令因 `/var/lib/pjsk/backups` 为 `postgres:postgres 0700` 而漏加 `sudo`，根盘结果已取得但该路径返回 permission denied；没有写入。改用 `sudo df` 后完整预检重跑通过：51 GiB 可用，拟用 incoming/中转路径、恢复角色、恢复库、restore-test env/unit 均不存在；正式对象、`current`、端口和 failed-unit 基线正常。
- 创建受限中转目录 `/home/ubuntu/.pjsk-upload-20260718T165637Z`（`ubuntu:ubuntu 0700`），只用一次显式 scp 上传四个文件，scp 退出 0。中转端 SHA 校验返回 `OK`。
- 在将文件移入最终 `/var/lib/pjsk/backups/incoming/20260718T165637Z` 前执行云端 `pg_restore 16.14 --list` 门禁，失败并退出 1：`unsupported version (1.16) in file header`。原因是 PostgreSQL 18.4 `pg_dump` 生成的 custom archive format 1.16 不能由较旧的 PostgreSQL 16.14 `pg_restore` 读取；本地 18.4 `pg_restore --list` 成功不代表目标 16.14 可读取。
- 按批准的“任何恢复检查错误立即停止”规则，未擅自改用其他 pg_dump 版本、纯 SQL、目录格式或其他绕过，也未升级云端 PostgreSQL。最终 incoming 目录未创建；四个文件保留在 0700 中转目录内，文件仍为 `ubuntu:ubuntu 0664`，但父目录只允许 ubuntu 遍历，尚未达到最终 `postgres:postgres 0600` 签收状态。
- 失败后只读取证确认：拟建角色 `pjsk_restore_app_20260718t165637z` 与数据库 `pjsk_restore_test_20260718T165637Z` 均不存在；`/etc/pjsk/backend.restore-test.env` 不存在；没有 restore-test unit、PJSK 后端进程、18081 或 8080 监听，failed unit 为 0。
- 正式 `pjsk` 数据库、`pjsk_app` 角色、`/opt/pjsk/current`、正式 `/etc/pjsk/backend.env` 与正式 `pjsk-backend.service` 仍不存在；`/opt/pjsk/current.next` 仍指向 `/opt/pjsk/releases/14d339e56677`。未修改 Caddy、UFW、安全组或域名，未开放客户访问。
- 阶段 2F 最终结论：**未通过，停止在云端签收门禁（2F-6 之前）**。后续必须由人工明确选择兼容方案，例如批准使用 PostgreSQL 16 官方客户端在本地重新生成 custom-format dump，或另行批准目标 PostgreSQL 大版本变更；不得在本轮自行绕过。
- 本轮未执行云端 `pg_restore`、未创建隔离恢复角色/数据库、未启动候选后端、未运行云端补迁移、未触碰正式生产切换、未提交或推送；未使用子代理。

### 12.13 阶段 2F-Fix-1：并行安装 PostgreSQL 18 与回环隔离空集群（2026-07-19）

#### 方案依据与开始基线

- 由于现役库预演包由 PostgreSQL 18.4 `pg_dump` 生成，而 PostgreSQL 官方说明 dump 输出不保证可装载到更旧的大版本服务器，本阶段没有尝试把 18 格式的 archive 强制降级恢复到 PostgreSQL 16；选择保留现有 16/main，并行建立 PostgreSQL 18 空集群。依据：PostgreSQL 官方 `pg_dump` 文档 <https://www.postgresql.org/docs/18/app-pgdump.html>。
- PGDG 仓库只采用 PostgreSQL 官方 Ubuntu 页面给出的 apt.postgresql.org 方案，Ubuntu 24.04 noble/amd64 与该仓库支持范围匹配。依据：<https://www.postgresql.org/download/linux/ubuntu/>。
- 本地开始及结束基线均为 `main`，HEAD `14d339e56677b6faff5d0769279cb2feaf82a9bc`，相对本地 `origin/main` behind 0 / ahead 2；唯一工作树改动仍为本部署日志，暂存区为空。
- 开始时远端只有 16/main：PostgreSQL `16.14 (Ubuntu 16.14-0ubuntu0.24.04.1)`，数据目录 `/var/lib/postgresql/16/main`，仅监听 `127.0.0.1:5432`。unit 为 active/running，MainPID `6759`，启动时间 `2026-07-18 23:24:39 CST`；保留的隔离验证对象 `pjsk_compat_app` 与 `pjsk_compat_test_20260718T164403Z` 存在。18/main、5433 监听与 PG18 配置/数据目录均不存在，failed unit 为 0。

#### PGDG 来源、签名与 APT 安装

- 初始只读检查确认不存在既有 PGDG source 或未知 PostgreSQL 第三方仓库；系统已有 PostgreSQL 官方 `postgresql-common` 提供的 key 文件 `/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc` 与官方辅助脚本。key 指纹核验为 `B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8`。
- 新增 `/etc/apt/sources.list.d/pgdg.sources`（root:root 0644），内容严格为 `Types: deb deb-src`、`URIs: https://apt.postgresql.org/pub/repos/apt`、`Suites: noble-pgdg`、`Architectures: amd64`、`Components: main`、`Signed-By: /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc`。`apt-get update` 退出 0；`apt-cache policy` 显示 PG18 候选仅来自该 PGDG noble 源。
- 一次 OS 预检的本地 SSH 引号把原本仅用于显示的分隔文本拆成远端命令，退出 127；改用无管道分隔的只读命令后确认 Ubuntu noble 24.04/amd64。一次目标路径检查因 key 文件已经存在而按断言退出 1，随后只读确认该 key 由 `postgresql-common` 管理。这两次均没有写入系统。
- 使用 root 身份调用 GPG 指纹检查时，GPG 自动建立 `/root/.gnupg`、`pubring.kbx`（32 bytes）与 `trustdb.gpg`（1,200 bytes），均为 root-only；没有私钥、没有导入用户密钥，也没有改变 APT 信任内容。该非必要副作用为保留完整审计未擅自删除。
- `apt-get -s install postgresql-18 postgresql-client-18` 退出 0：新增 5 个包、升级 5 个包、删除 0 个、另有 2 个包未升级；不会删除 PostgreSQL 16、Caddy、SSH 或迁移 16/main 数据目录。新增为 `libllvm19`、`liburing2`、`postgresql-18`、`postgresql-18-jit`、`postgresql-client-18`；升级为 `libpq5`、`postgresql`、`postgresql-client`、`postgresql-client-common`、`postgresql-common`。
- 实际安装过程中，PGDG `postgresql` 元包的 debconf 前端询问是否将 16/main 自动升级到 18。没有向提示输入 `yes`，也没有运行 `pg_upgradecluster`。在确认 16/main 仍以原 PID/启动时间运行且 18/main 尚未创建后，仅向已核实命令行的 debconf frontend 进程发送 SIGTERM，使阻塞的包配置安全退出；没有向 dpkg、数据库进程发送 SIGKILL，也没有停止 PG16。随后预置 `postgresql/auto_upgrade=false`，以 `DEBIAN_FRONTEND=noninteractive dpkg --configure -a` 完成包配置，退出 0。最终 `dpkg --audit` 无输出，APT/dpkg/debconf 无残留进程。
- 精确安装版本：`postgresql-18`、`postgresql-client-18`、`postgresql-18-jit` 均为 `18.4-1.pgdg24.04+1`；`postgresql`/`postgresql-client` 元包为 `18+293.pgdg24.04+1`；`postgresql-common`/`postgresql-client-common` 为 `293.pgdg24.04+1`；`libpq5` 为 `18.4-1.pgdg24.04+1`；Ubuntu 依赖 `libllvm19` 为 `1:19.1.1-1ubuntu1~24.04.2`、`liburing2` 为 `2.5-1build1`。PG18 的 `postgres`、`psql`、`pg_dump` 与 `pg_restore` 均报告 `18.4 (Ubuntu 18.4-1.pgdg24.04+1)`。

#### 18/main 创建、配置与备份

- 包安装未自动创建 18/main。执行 `pg_createcluster 18 main --port=5433 --start-conf=auto` 退出 0，创建独立数据目录 `/var/lib/postgresql/18/main`（postgres:postgres 0700），初始化为 UTF8、`en_US.UTF-8`、启用 data page checksums，初始状态为 down；没有复用、升级或修改 16/main 数据目录。
- 新集群默认 `pg_hba.conf` 已只包含 Unix socket peer，以及 `127.0.0.1/32`、`::1/128` 的 `scram-sha-256` 主机/复制规则；没有 `0.0.0.0/0`、`::/0`、公网/私网网段或 TCP trust，因此未改写 HBA。
- 配置写入前生成 UTC 时间戳 root-only 备份：`/etc/postgresql/18/main/postgresql.conf.pre-pjsk-20260718T171900Z.bak` 与 `/etc/postgresql/18/main/pg_hba.conf.pre-pjsk-20260718T171900Z.bak`，均为 root:root 0400。
- 仅新增 PG18 include 文件 `/etc/postgresql/18/main/conf.d/99-pjsk-isolation.conf`（root:postgres 0640），显式固定 `listen_addresses='localhost'`、`port=5433`、`password_encryption='scram-sha-256'`。启动前由 PG18 `postgres -C` 解析确认三个有效值和 HBA 路径均正确，禁止 HBA 规则计数为 0；没有写入任何 PG16 配置文件。
- `pg_ctlcluster 18 main start` 退出 0；没有重启整机，也没有停止或重启 16/main。

#### 并行运行、空集群与 archive 只读验收

- `pg_lsclusters` 最终显示 16/main online、端口 5432、数据目录 `/var/lib/postgresql/16/main`；18/main online、端口 5433、数据目录 `/var/lib/postgresql/18/main`。两套数据目录完全分离。
- 16/main 仍为 MainPID `6759`、启动时间 `2026-07-18 23:24:39 CST`，版本、数据目录、端口和监听设置均未改变。18/main 为 PostgreSQL 18.4，`listen_addresses=localhost`、`port=5433`、`password_encryption=scram-sha-256`。
- `ss -lntp` 实际显示 PG16 仅在 `127.0.0.1:5432`，PG18 仅在 `127.0.0.1:5433`；没有 0.0.0.0、IPv6 全接口、公网或私网数据库监听。两套 active HBA 规则均只含 Unix socket、127.0.0.1/32 和 ::1/128，禁止规则计数均为 0。
- UFW 保持 active，入站仍仅允许 IPv4/IPv6 的 22、80、443 TCP，没有 5432 或 5433 规则；未修改腾讯云安全组。Caddy、16/main、18/main 均 active，failed unit 为 0。
- PG18 数据库列表严格只有 `postgres`、`template0`、`template1`；没有任何 `pjsk%` 角色或数据库。PG16 中保留的隔离验证角色/数据库仍存在，正式 `pjsk_app` 与 `pjsk` 仍不存在。
- 原中转目录 `/home/ubuntu/.pjsk-upload-20260718T165637Z` 仍为 ubuntu:ubuntu 0700；四个原文件仍为 ubuntu:ubuntu 0664，大小仍分别为 dump 121,804、校验文件 103、baseline 9,052、迁移清单 519 bytes。dump SHA-256 仍为 `5E0E39AE39CC9BCC7A66D5D77D529E3F6A38DF58D3B59C87E24B3E858B5D7200`。
- 以 ubuntu 身份执行 PG18 `/usr/lib/postgresql/18/bin/pg_restore --list`，退出 0，TOC 计数严格为 174；只统计目录项，没有输出业务数据，没有复制或修改原中转文件，更没有执行恢复。

#### 最终停止点

- `/opt/pjsk/current`、正式 `/etc/pjsk/backend.env` 与正式 `/etc/systemd/system/pjsk-backend.service` 仍不存在；`/opt/pjsk/current.next` 保持指向 `/opt/pjsk/releases/14d339e56677`。没有 PJSK 后端进程，18080、18081、8080 与 5173–5175 均无监听。
- 阶段 2F-Fix-1 最终结论：**通过**。PG18 官方软件与仅回环的独立空集群已准备完成，PG16 保持原样并行运行，PG18 工具可读取现有 174 项 archive。
- 本阶段没有恢复 dump，没有创建恢复/正式 PJSK 角色或数据库，没有创建 extension、backend env 或 PJSK systemd，没有启动后端或运行迁移，没有切换生产，没有修改 Caddy、UFW 或腾讯云安全组，没有开放客户访问，没有提交或推送；未使用子代理。停止在 2F-Fix-1，等待人工明确批准后才可进入 2F-Fix-2 隔离恢复。

### 12.14 阶段 2F-Fix-2：PG18 隔离恢复签收权限阻断（安全停止，2026-07-19）

#### 开始基线与来源复核

- 本地开始基线符合批准值：`main`，HEAD `14d339e56677b6faff5d0769279cb2feaf82a9bc`，相对本地 `origin/main` behind 0 / ahead 2；唯一工作树改动为本部署日志，暂存区为空。
- 远端 16/main 与 18/main 均 online：PG16 仍仅监听 `127.0.0.1:5432`，MainPID `6759`、启动时间 `2026-07-18 23:24:39 CST` 未变；PG18 仍仅监听 `127.0.0.1:5433`。PG18 中没有任何 `pjsk%` 数据库或角色；没有 PJSK 后端进程、18081 或 8080 监听；failed unit 为 0。UFW 仍只允许 IPv4/IPv6 的 22、80、443 TCP。
- 只读读取 `/home/ubuntu/.pjsk-upload-20260718T165637Z/source-baseline.json` 与 `source-migrations.txt`。baseline 仍为 schema version 1、来源 PostgreSQL 18.4、19 条迁移、最高 `0019_admin_auth_audit_events.sql`；完整迁移清单为 19 行，包含两个 `0005_*`、没有 0006 或 0020/0021。聚合结构包含全部表计数、状态分布、numeric 金额/数量合计、bytea 非空计数和 UTC 时间边界，没有读取或输出逐行业务数据或秘密。
- 原中转 dump SHA-256 仍为 `5E0E39AE39CC9BCC7A66D5D77D529E3F6A38DF58D3B59C87E24B3E858B5D7200`；PG18 `pg_restore --list` 退出 0，TOC 严格为 174。目标 `/var/lib/pjsk/backups/incoming/20260718T165637Z` 开始时不存在，因此允许进入复制签收。

#### 文件复制签收与权限阻断

- 按批准命令创建目标目录并用 `install` 显式复制四个文件，没有移动或修改原中转文件。最终目标目录 `/var/lib/pjsk/backups/incoming/20260718T165637Z` 为 postgres:postgres 0700；dump、`.dump.sha256`、baseline 与迁移清单均为 postgres:postgres 0600，大小分别为 121,804、103、9,052、519 bytes。`install -d` 同时创建此前不存在的中间目录 `/var/lib/pjsk/backups/incoming`，其状态为 root:root 0755，但上级 `/var/lib/pjsk/backups` 仍为 postgres:postgres 0700。
- 随后严格以 postgres 身份执行目标目录内的 `sha256sum -c`。命令在 `cd /var/lib/pjsk/backups/incoming/20260718T165637Z` 阶段返回 `Permission denied`，SSH 总退出码 1；因此没有执行目标副本的 SHA 验收、`pg_restore --list` 或 TOC 复验。
- 只读取证 `namei -l` 定位根因：`/var/lib/pjsk` 为 pjsk:pjsk 0750，postgres 既不是 owner 也不属于 pjsk 组，无法穿越该目录；`sudo -u postgres test -x /var/lib/pjsk` 与读取签收 dump 均退出 1。阻断发生在更上级路径，不是目标目录或四个文件自身 owner/mode 错误。
- 按“签收 SHA/目录门禁失败立即停止”的批准规则，没有使用 root 绕过 postgres 身份验收，没有修改或放宽 `/var/lib/pjsk` 权限，没有给 postgres 增加组成员资格，也没有把文件移到替代路径。后续需要新的明确授权选择最小修复；优先评估只赋予 postgres 对 `/var/lib/pjsk` 的 execute-only ACL，而不是改为全局可遍历或递归改权。

#### 安全停止终态

- 失败后只读取证确认：PG18 中仍没有任何 `pjsk%` 数据库或角色，隔离恢复角色和数据库均未创建；没有执行 `pg_restore`，没有恢复任何对象或数据。
- PG16 仍为原 MainPID/启动时间并仅监听 `127.0.0.1:5432`；PG18 仅监听 `127.0.0.1:5433`；两套集群均 online，failed unit 为 0，没有 PJSK 后端进程、18081 或 8080 监听。
- 原中转目录和四文件仍完整保留，owner/mode/大小未变；新签收目录和四个 postgres-only 副本也按要求保留，未清理，等待人工决定权限修复后重新完成签收验收。
- 阶段 2F-Fix-2 最终结论：**未通过，安全停止在阶段 B 的 postgres 身份 SHA 验收**。阶段 C–J 均未进入；未创建恢复或正式角色/数据库，未启动后端，未运行 0020/0021 或任何迁移，未创建 restore-test env/unit，未切换 `current`，未修改 Caddy、UFW 或腾讯云安全组，未开放客户访问，未提交或推送；未使用子代理。

### 12.15 阶段 2F-Fix-2A：execute-only ACL 修复前工具门禁失败（安全停止，2026-07-19）

- 本地开始基线仍为 `main`，HEAD `14d339e56677b6faff5d0769279cb2feaf82a9bc`，相对本地 `origin/main` behind 0 / ahead 2；唯一工作树改动为本部署日志，暂存区为空。
- 按批准流程，在读取或修改 ACL 前先用 `command -v` 确认工具可用性。远端 `getfacl` 与 `setfacl` 均未找到，各自退出码为 1；受控预检脚本据此退出 20。
- 本轮明确规定“若 ACL 工具未安装，不得自动安装，立即停止并报告”，因此没有执行 `setfacl`，没有新增 `user:postgres:--x`，也没有修改 `/var/lib/pjsk` 或其下级任何 owner、group、mode、ACL。
- 因工具门禁先失败，没有继续 ACL 前目录状态、postgres 权限边界、签收副本 SHA-256 或 PG18 TOC 复验；上一阶段的权限阻断与签收未通过结论保持不变。
- 阶段 2F-Fix-2A 最终结论：**未通过，安全停止在阶段 A 的 ACL 工具可用性门禁**。需要新的明确授权决定是否仅安装 Ubuntu `acl` 包后重跑 2F-Fix-2A，或选择其他经审查的最小权限方案。
- 本轮没有安装软件，没有恢复 dump，没有创建角色或数据库，没有启动后端，没有运行迁移，没有切换生产，没有修改 PostgreSQL、Caddy、UFW 或腾讯云安全组，没有提交或推送；未使用子代理。

### 12.16 阶段 2F-Fix-2A-Install：安装 acl、execute-only ACL 与签收复验（2026-07-19）

#### acl 安装来源与结果

- 本地开始基线仍为 `main`，HEAD `14d339e56677b6faff5d0769279cb2feaf82a9bc`，相对本地 `origin/main` behind 0 / ahead 2；唯一工作树改动为本部署日志，暂存区为空。
- 安装前 `dpkg -s acl`、`command -v getfacl`、`command -v setfacl` 均确认未安装/不存在。`apt-cache policy acl` 显示候选 `2.3.2-1build1.1`，来自服务器既有腾讯云 Ubuntu 镜像 `http://mirrors.tencentyun.com/ubuntu` 的 `noble-updates/main amd64`，APT 元数据标识为 `Ubuntu:24.04/noble-updates`；没有未知第三方 acl 候选。
- `apt-get -s install acl` 退出 0，模拟结果严格为 `0 upgraded, 1 newly installed, 0 to remove, 2 not upgraded`，仅新增 `acl`，没有删除、升级或停止 PostgreSQL 16/18、Caddy、SSH 或关键系统包。
- `apt-get install -y acl` 退出 0，仅下载并安装 `acl 2.3.2-1build1.1`，增加约 197 kB；安装报告无服务需要重启。无 TTY 环境下 debconf 从 Dialog/Readline 回退到 Teletype 的提示不影响安装，`dpkg --audit` 最终无输出。
- 安装验收：`/usr/bin/getfacl` 与 `/usr/bin/setfacl` 均存在，工具版本均为 `2.3.2`；dpkg 状态为 installed，installed/candidate 均为 `2.3.2-1build1.1`。

#### ACL 基线、最小修复与边界验证

- ACL 写入前只读确认：`/var/lib/pjsk` 为 pjsk:pjsk 0750，ACL 只有 owner/group/other 基础条目，没有 postgres 命名用户条目；`/var/lib/pjsk/backups` 为 postgres:postgres 0700，`incoming` 为 root:root 0755，签收时间戳目录为 postgres:postgres 0700，四个签收文件均为 postgres:postgres 0600，大小未变。
- 仅执行一次非递归 `setfacl -m u:postgres:--x /var/lib/pjsk`；没有设置 default ACL，没有对其他路径执行 setfacl，也没有修改 owner/group 或 chmod。
- 写入后 `/var/lib/pjsk` 基础 owner/group/mode 仍为 pjsk:pjsk 0750，精确命名 ACL 为 `user:postgres:--x`，mask 为 `r-x`，无 default ACL。
- postgres 权限边界全部通过：`test -x /var/lib/pjsk` 退出 0；`test ! -r` 与 `test ! -w` 均退出 0；`ls -la /var/lib/pjsk` 返回 `Permission denied`、退出 2；没有通过创建测试文件验证写权限。
- postgres 对批准路径的穿越/读取门禁全部退出 0：可穿越 `backups`、`incoming`、签收时间戳目录，并可读取签收 dump。ACL 只解决上级路径穿越，不赋予 `/var/lib/pjsk` 列目录或写入权限。

#### 正式签收复验与安全终态

- 以 postgres 身份在签收目录执行 `sha256sum -c`，输出 `pjsk-preflight-20260718T165637Z.dump: OK`，退出 0。源 dump 与签收副本 SHA-256 均为 `5E0E39AE39CC9BCC7A66D5D77D529E3F6A38DF58D3B59C87E24B3E858B5D7200`。
- 以 postgres 身份使用 PG18 `pg_restore --list` 读取签收 archive，退出 0，TOC 计数严格为 174；只统计目录项，没有输出业务数据或执行恢复。
- 签收目录仍为 postgres:postgres 0700；四文件仍为 postgres:postgres 0600，大小依次为 dump 121,804、校验文件 103、baseline 9,052、迁移清单 519 bytes。原中转目录和四文件仍存在，owner/大小未变，原 dump SHA 与签收副本一致。
- 终态 16/main 仍为 active/running，MainPID `6759`、启动时间 `2026-07-18 23:24:39 CST` 未变，仅监听 `127.0.0.1:5432`；18/main 仍为 active/running，仅监听 `127.0.0.1:5433`。PG18 中没有任何 `pjsk%` 数据库或角色，PG16 中正式 `pjsk` 数据库与 `pjsk_app` 角色也不存在。
- `/opt/pjsk/current`、`/etc/pjsk/backend.restore-test.env`、正式 `/etc/pjsk/backend.env` 与正式 `pjsk-backend.service` 均不存在；`current.next` 保持指向 `/opt/pjsk/releases/14d339e56677`。没有 PJSK 后端进程、18081 或 8080 监听；Caddy 和两套 PostgreSQL 集群 active，failed unit 为 0；UFW 仍只允许 22、80、443，没有数据库端口规则，未修改腾讯云安全组。
- 阶段 2F-Fix-2A-Install 最终结论：**通过**。execute-only ACL 与 postgres 身份的正式备份签收门禁均已完成，可以在新的明确授权下回到 2F-Fix-2 的隔离恢复对象创建阶段。
- 本轮未恢复 dump，未创建恢复或正式角色/数据库，未启动后端，未运行任何迁移，未切换生产，未修改 PostgreSQL 配置、Caddy、UFW 或安全组，未提交或推送；未使用子代理。

### 12.17 阶段 2F-Fix-2 继续：PG18 隔离恢复成功、对象 owner 门禁失败停止（2026-07-19）

#### 恢复前基线与隔离对象

- 本地开始基线仍为 `main`，HEAD `14d339e56677b6faff5d0769279cb2feaf82a9bc`，相对本地 `origin/main` behind 0 / ahead 2；唯一工作树改动为本部署日志，暂存区为空。
- 恢复前 16/main 与 18/main 均 online。PG16 仍为 MainPID `6759`、启动时间 `2026-07-18 23:24:39 CST`，仅监听 `127.0.0.1:5432`；PG18 仅监听 `127.0.0.1:5433`，其中没有任何 `pjsk%` 数据库或角色。failed unit 为 0，没有 PJSK 后端进程、18081 或 8080 监听。
- 正式签收 dump 的 postgres 身份 SHA 校验再次为 `OK`，PG18 `pg_restore --list` TOC 严格为 174。读取签收的来源清单确认 19 条迁移、最高 `0019_admin_auth_audit_events.sql`；baseline schema version 1、来源 PostgreSQL 18.4，包含 counts、status distributions、binary nonempty counts、sums 与 UTC time bounds 五组聚合。
- 以新 UTC 时间戳创建隔离角色 `pjsk_restore_app_20260719t011340z` 和隔离数据库 `pjsk_restore_test_20260719t011340z`，创建前均确认不存在，且正式 `pjsk`/`pjsk_app` 不存在。
- 隔离密码由服务器本地 `openssl rand` 生成，未输出或进入命令行参数。postgres-only 0600 临时建角 SQL 在角色创建成功后立即删除；角色密码保存格式只验证为 SCRAM，没有读取或记录 hash。角色属性为 LOGIN=true，SUPERUSER/CREATEDB/CREATEROLE/REPLICATION 均为 false。
- 隔离数据库 owner 为本次隔离角色，编码 UTF8，基于 template0 创建；恢复前 public 普通表数为 0，`schema_migrations` 不存在。没有预建 pgcrypto、schema 或业务表。

#### 单事务恢复与数据完整性核对

- 使用 `pg_restore (PostgreSQL) 18.4 (Ubuntu 18.4-1.pgdg24.04+1)`，明确连接 `127.0.0.1:5433`，使用本次隔离角色/数据库及 `--no-owner --no-privileges --role --exit-on-error --single-transaction`；未使用 clean、create、if-exists、并行、纯 SQL 转换或 archive 修改。
- 恢复开始 UTC `2026-07-19T01:13:41.389999265Z`，结束 `2026-07-19T01:13:41.583727195Z`，耗时 191 ms，`pg_restore` 退出 0。数据库初始大小 7,853,759 bytes，恢复完成后立即为 10,065,599 bytes；后续只读验证后为 10,163,903 bytes。
- 恢复日志保存在 `/var/log/pjsk/pg-restore-20260719t011340z.log`，root:root 0600，大小 0 bytes；这与非 verbose 的成功恢复无输出一致。临时 pgpass 在恢复/验证终止时由 trap 删除，连同临时建角 SQL 的终态计数为 0；没有留下密码、DSN 或其他秘密文件。
- 迁移核对在 owner 门禁之前全部通过：数量严格为 19，最高严格为 `0019_admin_auth_audit_events.sql`，完整逐行列表与签收的 `source-migrations.txt` 一致；两个 `0005_*` 存在，0006、0020、0021 不存在，无未知或缺失迁移。
- `pgcrypto` 存在；来源核心表存在；迁移前 `payment_qr_codes` 和 `payment_submissions` 均不存在，符合来源仅到 0019 的预期。
- 全部业务聚合与 `source-baseline.json` 精确一致：counts 23/23、status distributions 12/12、binary nonempty counts 3/3、numeric sums 10/10、UTC time bounds 5/5 均匹配。核心计数为 users 45、orders 44、order_items 120、import_batches 1、payments 7、payment_items 19；订单与明细金额均为 6318.44，payment_items 已应用金额为 1145.40。NULL/0、numeric 文本和 UTC 微秒格式均按精确值比较，没有读取或输出逐行业务数据或秘密。

#### 对象 owner 门禁失败与安全停止

- 所有表/普通关系 owner 检查显示 public schema 下 22 张关系均由本次隔离角色拥有；数据库 owner 与 pgcrypto extension owner 也均为本次隔离角色。public schema owner 为 `pg_database_owner`，ACL 仅为 owner 的 UC 与 PUBLIC 的 U，没有 PUBLIC CREATE。
- 但 public schema 下 37 个函数的 owner 为 `postgres`，而受控验证器要求所有恢复函数 owner 均为隔离角色，因此抛出 `RuntimeError: restored object owner mismatch`，总 SSH 退出 1。当前证据与 pgcrypto 扩展托管函数有关，但本轮没有预先批准该 owner 例外，不能自行将其降级为可接受结果。
- 按“任何权限/完整性不一致立即停止”规则，没有 ALTER OWNER、没有重新恢复、没有删除或重建角色/数据库，也没有启动后端或运行补迁移。隔离角色、数据库、恢复日志和两套预演文件均保留等待人工审查。
- 第一次失败后终态文件审计使用 `sudo stat` 搭配由 ubuntu shell 预展开的受限目录 glob，glob 无法展开而以字面量传入，`stat` 退出 1；没有写入。改用 `sudo find` 后同一只读审计退出 0。

#### 失败后终态

- PG16 仍为原 PID/启动时间，仅监听 `127.0.0.1:5432`；PG18 仍仅监听 `127.0.0.1:5433`。Caddy 与两套 PostgreSQL 集群 active，failed unit 为 0；完整监听仍只有 SSH 22、Caddy 80/回环 2019、PG16 5432、PG18 5433 和本地 DNS 53，没有 18081 或 8080。
- PG18 `pg_hba.conf` 禁止规则计数为 0；UFW 仍只允许 IPv4/IPv6 的 22、80、443，没有 5433 规则；未修改腾讯云安全组。`/var/lib/pjsk` ACL 仍严格包含 `user:postgres:--x`，没有放宽读写。
- 正式 `pjsk` 数据库和 `pjsk_app` 角色在 PG16/PG18 中仍不存在。`/opt/pjsk/current`、`/etc/pjsk/backend.restore-test.env`、正式 `/etc/pjsk/backend.env` 与正式 `pjsk-backend.service` 均不存在；`current.next` 仍指向 `/opt/pjsk/releases/14d339e56677`。
- 原中转四文件和正式签收四文件均完整保留，大小/owner/mode 未变；两份 dump SHA-256 仍一致，为 `5E0E39AE39CC9BCC7A66D5D77D529E3F6A38DF58D3B59C87E24B3E858B5D7200`。
- 阶段 2F-Fix-2 最终结论：**未通过，安全停止在恢复后的对象 owner 权限门禁**。数据恢复、19/0019 和全部业务聚合本身已通过，但需人工明确判断 PostgreSQL trusted extension/pgcrypto 函数由 postgres 持有是否为预期安全语义，或批准专门修复方案；本轮不进入 2F-Fix-3。
- 本轮未启动后端，未运行 0020/0021 或其他迁移，未切换生产，未创建 env/systemd，未修改 PostgreSQL 配置、Caddy、UFW 或安全组，未开放客户访问，未提交或推送；未使用子代理。

### 12.18 阶段 2F-Fix-2B：trusted extension 语义确认后的 TCP 凭据门禁停止（2026-07-19）

- 本地开始基线仍为 `main`，HEAD `14d339e56677b6faff5d0769279cb2feaf82a9bc`，相对本地 `origin/main` behind 0 / ahead 2；唯一工作树改动为本部署日志，暂存区为空。
- 已读取 PostgreSQL 18 官方文档：`CREATE EXTENSION` 说明 trusted extension 可由具有当前数据库 CREATE 权限的非超级用户安装；extension 本体归调用者，但 contained objects 默认归 bootstrap superuser，除非安装脚本显式另行分配。依据：<https://www.postgresql.org/docs/18/sql-createextension.html>。
- PostgreSQL 18 的 pgcrypto 官方文档明确将 pgcrypto 标记为 trusted extension：<https://www.postgresql.org/docs/18/pgcrypto.html>。`pg_extension.extowner` 是 extension owner；`pg_depend` 的 `deptype='e'` 明确表示 dependent object 是所引用 extension 的成员，只能通过 DROP EXTENSION 删除。依据：<https://www.postgresql.org/docs/18/catalog-pg-extension.html> 与 <https://www.postgresql.org/docs/18/catalog-pg-depend.html>。
- 因此拟收紧的 owner 门禁不能按 `owner=postgres`、`schema=public`、对象名称或固定函数数量放行；必须将每个例外对象用 `pg_depend.classid/objid/refclassid/refobjid` 与 pg_extension 精确关联，并要求 `deptype='e'`、目标 extension 严格为 pgcrypto。数据库、extension 本体及所有非 extension 业务对象仍必须由隔离角色拥有。
- 但本轮同时要求所有 SQL 明确使用 `host=127.0.0.1`、`port=5433`、指定隔离数据库。只读凭据门禁确认 postgres 默认 `/var/lib/postgresql/.pgpass` 不存在；未读取任何凭据文件内容。
- 随后以 PostgreSQL 18 psql、`-w`、`PGCONNECT_TIMEOUT=5` 显式连接 `127.0.0.1:5433`、用户 postgres、数据库 `pjsk_restore_test_20260719t011340z`，连接阶段以 `fe_sendauth: no password supplied` 退出 2，`select 1` 未执行。该失败没有数据库写入。
- 上一阶段已按批准要求删除隔离角色的明文密码和临时 pgpass；数据库仅保留不可逆 SCRAM verifier，不能安全恢复原密码。本轮只允许只读查询，未授权重置隔离角色密码、创建新凭据、修改 HBA 或改用 Unix socket peer，因此无法在不违反范围的前提下执行 pg_extension/pg_depend 核对和重新终验。
- 阶段 2F-Fix-2B 最终结论：**未通过，安全停止在显式 TCP SQL 凭据门禁**。没有修改本地验证器代码；只在本日志完整记录了下一版门禁的严格判定原则。
- 后续若坚持所有 SQL 走 127.0.0.1:5433，需要新的明确授权：为现有隔离角色生成并设置一次新随机密码，保存于 postgres-only 0600 临时 pgpass，仅用于本次只读核对，完成后立即删除。另一方案是明确批准使用 PG18 binary、端口 5433 和指定数据库的 Unix socket peer 查询，但这会改变本轮“所有 SQL 必须 host=127.0.0.1”的约束。
- 本轮没有修改任何数据库对象或 owner，没有重置密码，没有重新恢复 dump，没有启动后端，没有运行 0020/0021，没有切换生产，没有修改 PostgreSQL 配置、Caddy、UFW 或安全组，没有开放客户访问，没有提交或推送；未使用子代理。

### 12.19 阶段 2F-Fix-2C：PG18 Unix socket peer 管理员只读取证与 owner 终验（2026-07-19）

#### 授权边界与 socket 连接证据

- 本轮依据新的明确授权，仅将管理员取证 SQL 改为操作系统用户 `postgres`、数据库用户 `postgres`，使用 PostgreSQL 18 的 `/usr/lib/postgresql/18/bin/psql`、端口 5433 和 Unix socket；没有传 `-h 127.0.0.1`，没有设置或读取数据库密码，没有创建 `.pgpass`。此授权不改变后续候选后端必须以隔离应用角色通过 `127.0.0.1:5433` TCP 连接的要求。
- PostgreSQL 18 官方文档说明：`local` HBA 记录匹配 Unix-domain socket；peer 认证从内核取得客户端操作系统用户名并将其作为允许的数据库用户名（可选映射），且只适用于本地连接。依据：<https://www.postgresql.org/docs/18/auth-pg-hba-conf.html> 与 <https://www.postgresql.org/docs/18/auth-peer.html>。
- 首次发现脚本的全部查询已经成功并输出 `DISCOVERY_OK`，但 PowerShell 字符串管道在 LF 脚本末尾附加了孤立 CR，远端在完成所有查询后将其解释为额外空命令，SSH 总退出码为 127。该问题属于本地执行器包装，不是 SQL 或服务器检查失败，没有数据库写入。保留该失败记录后，改用 `cmd.exe` 对同一已验证无 CR、末尾为 LF 的本地文件进行原始 stdin 重定向；重跑总退出码为 0。
- socket 身份证据严格为 `current_user=postgres`、`session_user=postgres`、目标库 `pjsk_restore_test_20260719t011340z`，`inet_client_addr()` 与 `inet_server_port()` 均为 NULL，证明不是 TCP 且连接到 PG18 目标库。`unix_socket_directories=/var/run/postgresql`，PG18 `hba_file=/etc/postgresql/18/main/pg_hba.conf`。
- PG18 三条有效 local 规则分别为 `local all postgres peer`、`local all all peer`、`local replication all peer`。当前 HBA 与创建集群前 root-only 备份的 SHA-256 完全一致，均为 `5a362999da209cb6cb6f121a7c900dd0286c1431d39dc911c54cece773a9c9d2`；host 规则仍仅为 127.0.0.1/32 与 ::1/128 的 `scram-sha-256`，无配置错误，未修改或 reload HBA。

#### pgcrypto trusted 状态与 extension dependency 归属

- PostgreSQL 18 `CREATE EXTENSION` 官方文档说明：trusted extension 可由具有当前数据库 CREATE 权限的非超级用户安装；extension 本体归调用者，而 contained objects 默认归 bootstrap superuser，除非安装脚本显式另行指定。pgcrypto 官方文档将其明确标记为 trusted extension。依据：<https://www.postgresql.org/docs/18/sql-createextension.html> 与 <https://www.postgresql.org/docs/18/pgcrypto.html>。
- 目标库中的 pgcrypto 版本为 1.4、schema 为 public，`pg_available_extension_versions.trusted=true`，`pg_extension.extowner` 严格为隔离角色 `pjsk_restore_app_20260719t011340z`。
- 成员识别没有按名称、schema 或数量猜测：查询以 `pg_extension` 找到 pgcrypto OID，再要求 `pg_depend.refclassid='pg_extension'::regclass`、`refobjid=pgcrypto OID`、`deptype='e'`，并通过 dependent object 的 `classid/objid/objsubid` 关联对应 catalog。官方 `pg_depend` 文档明确 `deptype='e'` 表示对象是所引用 extension 的成员：<https://www.postgresql.org/docs/18/catalog-pg-depend.html>。
- 精确依赖统计显示 pgcrypto contained objects 只有 public schema 中的 37 个函数；owner 分布严格为 postgres 37。反向检查 public 下 postgres-owned 函数得到总数 37、精确 pgcrypto 成员 37、非成员 0。该数量只作为观察结果记录，放行条件不是硬编码 37，而是每个对象均必须存在上述精确 extension dependency。

#### 收紧后的 owner 门禁与恢复终验

- 最终 owner 门禁为：数据库 owner 必须是隔离角色；pgcrypto extension owner 必须是隔离角色；所有非系统、非 extension 业务对象必须是隔离角色；仅允许通过 `pg_depend`、`deptype='e'` 且精确引用 pgcrypto OID 证明的 contained objects 由 postgres 拥有。门禁不按 owner、schema、对象名或固定数量整体放行，也不修改数据库对象来迎合验证器。
- 门禁总退出码为 0：22 张业务关系全部由隔离角色拥有，owner mismatch 0；非 extension 函数 0/owner mismatch 0；44 个非 extension 自定义/关系类型全部由隔离角色拥有，owner mismatch 0；自定义 schema 0；非系统 extension 只有 pgcrypto 1 个且 owner mismatch 0。排除 pg_catalog/information_schema、系统预置 plpgsql 与精确 pgcrypto 成员后，postgres-owned 非 extension 业务对象数量严格为 0。public 预置 schema 仍由 `pg_database_owner` 管理，不被误分类为业务 schema。
- 迁移终验仍严格为 19 条、最高 `0019_admin_auth_audit_events.sql`，完整有序列表与签收 `source-migrations.txt` 精确一致；两个 `0005_*` 存在，0006、0020、0021 不存在。`payment_qr_codes` 与 `payment_submissions` 仍不存在。
- 全部来源业务聚合再次精确匹配：counts 23/23、status distributions 12/12、binary nonempty counts 3/3、numeric sums 10/10、UTC time bounds 5/5。验证器只比较聚合不变量，没有输出逐行业务数据或秘密。隔离角色属性仍为 LOGIN=true，SUPERUSER/CREATEDB/CREATEROLE/REPLICATION 均为 false。

#### 安全终态与停止点

- PG16 仍为 active，MainPID `6759`、启动时间 `2026-07-18 23:24:39 CST` 未变，仅监听 `127.0.0.1:5432`；PG18 为 active，MainPID `21095`、启动时间 `2026-07-19 01:19:37 CST`，仅监听 `127.0.0.1:5433`。Caddy active，failed unit 为 0。
- 完整 TCP 监听仍只有 SSH 22、Caddy 80/回环管理端口 2019、PG16 5432、PG18 5433 和本地 DNS 53；没有 18081、8080 或其他 PJSK 后端监听。UFW 仍为 incoming deny，仅允许 IPv4/IPv6 的 22、80、443 TCP，没有 5432/5433 规则；本轮没有修改 Caddy、UFW 或腾讯云安全组。
- 隔离恢复数据库与角色按要求保留。正式 `pjsk` 数据库和 `pjsk_app` 角色在 PG16/PG18 中仍不存在；`/opt/pjsk/current`、`/etc/pjsk/backend.restore-test.env`、正式 `/etc/pjsk/backend.env` 与正式 `pjsk-backend.service` 均不存在，`current.next` 仍指向 `/opt/pjsk/releases/14d339e56677`。
- 阶段 2F-Fix-2C 最终结论：**通过**。建议在新的明确授权下进入 2F-Fix-3；本轮完成后停止，不自动进入下一阶段。
- 本轮没有修改或重置任何角色密码，没有创建 `.pgpass`，没有修改 HBA 或任何数据库对象 owner，没有 ALTER FUNCTION/EXTENSION OWNER、REASSIGN OWNED、重新恢复 dump、删除或重建 pgcrypto，没有启动 PJSK 后端，没有运行 0020/0021 或任何迁移，没有修改 PG16、Caddy、UFW 或安全组，没有切换生产或开放客户访问，没有提交或推送；未使用子代理。

### 12.20 阶段 2F-Fix-3：候选后端补齐 0020/0021 与恢复数据迁移后终验（2026-07-19）

#### 本地复核与远端开始基线

- 本地开始时为 `main`，HEAD `14d339e56677b6faff5d0769279cb2feaf82a9bc`，相对本地 `origin/main` behind 0 / ahead 2；唯一工作树改动为本部署日志，暂存区为空。已复核 `0020_payment_qr_codes.sql`、`0021_payment_submissions.sql`、后端配置加载、数据库连接与启动迁移代码：后端先连接数据库，再按完整迁移文件名排序逐条事务执行，全部成功后才监听 HTTP；生产模式下 restore-test env 必须提供独立有效的查询恢复 HMAC。
- 第一次远端基线脚本把 `current_setting('server_version')` 错误断言为短字符串 `18.4`，而 PG18 实际正确返回带 Ubuntu 包后缀的完整版本，脚本在取得 PG16/PG18 身份后退出 1，未执行任何写入。将断言改为稳定的 `server_version_num=180004` 与目标库名后，同一阶段 A 重跑总退出码 0。
- 正式开始基线确认：16/main 与 18/main 均 online；PG16 为 16.14、MainPID `6759`、启动时间 `2026-07-18 23:24:39 CST`；PG18 为 18.4、目标库 `pjsk_restore_test_20260719t011340z` 严格为 19 条迁移、最高 `0019_admin_auth_audit_events.sql`。候选 release 的 REVISION 严格为批准提交，`current.next` 指向 `/opt/pjsk/releases/14d339e56677`，`current` 不存在。
- 开始时 `/etc/pjsk/backend.restore-test.env`、正式 `/etc/pjsk/backend.env` 和正式 `pjsk-backend.service` 均不存在；没有 transient/正式 PJSK unit、后端进程或 18081/8080 监听，failed unit 为 0。数据库仅监听 PG16 `127.0.0.1:5432` 与 PG18 `127.0.0.1:5433`。

#### 一次性隔离凭据与 restore-test env

- 在服务器本地用 `openssl rand` 分别独立生成新的随机数据库密码与 base64 HMAC；未输出正文、未放入聊天或日志、未复用其他测试/生产秘密。角色密码只通过随机时间戳、postgres:postgres 0600 的临时 SQL 文件传给 PG18 socket 管理连接，且只执行现有隔离角色的 `ALTER ROLE ... PASSWORD`；执行后立即删除临时 SQL，终态不存在。
- PG18 `password_encryption=scram-sha-256`；只以布尔断言确认新 verifier 为 SCRAM，未读取或记录 hash。隔离角色仍为 LOGIN=true，SUPERUSER/CREATEDB/CREATEROLE/REPLICATION 均为 false；没有修改角色名、权限属性、数据库 owner 或 PG16 对象。
- 创建一次性 `/etc/pjsk/backend.restore-test.env`，owner/mode 为 `root:pjsk 0640`；pjsk 可读不可写。内容仅包含批准的生产模式安全默认值、`127.0.0.1:18081`、PG18 隔离角色经 `127.0.0.1:5433` TCP 连接、disabled 邮件模式与本轮独立 HMAC；没有 SMTP 凭据、正式域名、生产秘密或本地现役数据库凭据，正文从未输出。该 env 按要求保留等待人工确认。

#### transient 后端、监听与健康检查

- 使用 transient unit `pjsk-backend-restore-test-20260719t014739z.service` 启动批准 release。User/Group 为 pjsk，WorkingDirectory 与 ExecStart 均指向 `/opt/pjsk/releases/14d339e56677`，EnvironmentFile 为 restore-test env；Restart=no，并启用 NoNewPrivileges、PrivateTmp、PrivateDevices、ProtectSystem=strict、ProtectHome 与仅 AF_UNIX/AF_INET/AF_INET6 的地址族限制。没有创建持久 unit 文件。
- 启动命令成功，unit 为 active/running、MainPID `28775`、NRestarts=0、Result=success。首次启动脚本在 systemd 标记进程 active 后仅约 26 ms 就检查端口，此时迁移尚未完成，因空监听行退出 1；这是验证器等待条件过早，不是服务失败，没有启动第二个 unit 或更换 release。对同一 unit 延迟只读复核后确认数据库连接成功、0020 与 0021 依次迁移成功，并只监听 `127.0.0.1:18081`，没有 0.0.0.0、IPv6 全接口或 8080 监听。
- journal 只包含 `.env` 不存在提示、数据库连接成功、两条迁移成功和回环监听信息；数据库密码、DSN、HMAC、恢复密钥、SMTP 密码与 Bearer 凭据标记扫描为 0。`.env not loaded` 符合不可变 release 不包含 `.env`、由 EnvironmentFile 提供配置的设计。
- 服务器本机请求 `http://127.0.0.1:18081/health`：curl 退出 0、HTTP 200，JSON 摘要严格为 `service=pjsk-backend`、`status=ok`、`database=connected`。没有开放 18081 公网。

#### 21 条迁移、0020/0021 结构与数据不变量

- `schema_migrations` 严格为 21 条、最高 `0021_payment_submissions.sql`；完整有序集合与批准候选 release 的 21 个迁移文件一致，前 19 条与签收 `source-migrations.txt` 精确一致。两个 `0005_*` 均存在，0006 不存在，0020 与 0021 均存在，无未知或缺失迁移。
- `payment_qr_codes` 与 `payment_submissions` 均存在且各有 1 个主键；外键数量分别为 2 与 3。六项重点检查约束全部存在：两表付款方式、二维码禁用一致性、submission 状态、reject reason 与 approved link。
- 六个指定业务索引全部存在且 owner 正确：二维码 active-method 部分唯一索引、method/created 索引，以及 submission 的 user、status、CN、linked-payment 四个索引。部分唯一索引为 unique、确有 predicate，PG18 规范化谓词为 `(enabled = true)`。第一次完整验证器使用无括号的等价值字面量断言而退出 1；只读读取规范化文本后修正格式断言，完整验证重跑总退出码 0，没有修改 schema。
- 迁移前已有业务数据的全部来源聚合精确不变：counts 23/23、status distributions 12/12、bytea 非空计数 3/3、numeric sums 10/10、UTC time bounds 5/5。核心计数仍为 users 45、orders 44、order_items 120、import_batches 1、payments 7、payment_items 19；订单与明细金额合计仍均为 6318.44，payment_items 已应用金额仍为 1145.40。NULL/0、numeric 文本与 UTC 微秒均严格比较，没有输出逐行业务数据。
- 两张新表 `payment_qr_codes` 与 `payment_submissions` 均严格为 0 行；迁移只生成设计中的 schema 对象，没有产生业务数据。

#### owner 门禁、停止结果与安全终态

- owner 门禁重跑通过：数据库与 pgcrypto extension owner 均为隔离角色；包含新增表和索引在内的 121 个非系统、非 extension 业务关系/索引 owner mismatch 为 0；48 个非 extension 类型 owner mismatch 为 0；非 extension 函数 mismatch 为 0。postgres-owned 非 extension 业务对象数量严格为 0；37 个 postgres-owned public 函数仍全部且仅通过 `pg_depend.deptype='e'` 精确关联至 pgcrypto。新增两表与六个指定索引均由隔离角色拥有，约束随所属关系受同一 owner 控制。
- 全部验收完成后执行 `systemctl stop`，退出 0。终态 transient unit 为 LoadState=not-found、ActiveState=inactive、SubState=dead、MainPID=0、NRestarts=0、Result=success；18081 与 8080 均无监听，无 PJSK 后端进程。journal 仍按 unit 名保留，本轮 restore-test env、隔离数据库与隔离角色也按要求保留。
- PG16 未停止、重启或修改：仍为 MainPID `6759`、原启动时间并仅监听 `127.0.0.1:5432`。PG18 仍为 MainPID `21095`、active，仅监听 `127.0.0.1:5433`；目标隔离库终态为 21/0021。PG18 当前 HBA 与创建集群前备份 SHA-256 仍完全一致。
- 正式 `pjsk` 数据库与 `pjsk_app` 角色在 PG16/PG18 中仍不存在；`/opt/pjsk/current`、正式 `/etc/pjsk/backend.env` 与正式持久 `pjsk-backend.service` 仍不存在，`current.next` 保持指向批准 release。
- 最终 TCP 监听只有 SSH 22、Caddy 80/回环管理 2019、PG16 5432、PG18 5433 和本地 DNS 53；443、18081 与 8080 均无监听。UFW 仍为 incoming deny，只允许 IPv4/IPv6 的 22、80、443 TCP；failed unit 为 0。本轮没有修改 Caddy、UFW、腾讯云安全组或域名。
- 阶段 2F-Fix-3 最终结论：**通过**。恢复数据在候选后端自动补齐 0020/0021 后，结构、完整迁移集合、全部来源聚合和精确 owner 门禁均通过。建议下一步先安排正式停写迁移窗口和人工执行清单；本轮停止，不进入正式迁移或生产切换。
- 本轮没有手工执行迁移 SQL，没有修改既有业务数据，没有创建正式数据库/角色/env/systemd，没有创建或切换 `current`，没有修改 PG16、Caddy、UFW、安全组或域名，没有开放客户访问，没有删除隔离对象或备份，没有提交或推送；未使用子代理。

### 12.21 阶段 2G-Plan：正式停写迁移、生产切换与回滚方案审查（仅计划，2026-07-19）

> **本节所有命令均为待审批草案，本轮一条也未执行。** 尖括号占位符必须在维护窗口前替换并由第二人复核；任何停止点失败都不得临场换端口、换版本、手改迁移或就地覆盖数据库。

#### 调查事实与硬决策

- 正式数据库明确使用 PostgreSQL 18.4，固定为 `127.0.0.1:5433`；正式 `/etc/pjsk/backend.env` 必须显式写 `DATABASE_PORT=5433`。不为追求默认端口在切换窗口内把 PG18 改到 5432。PG16 不承载正式 `pjsk`；其保留、停用或清理必须另开后续阶段，不能混入正式切换。
- 本机现役写入链路只读调查：Windows 服务 `pjsk-backend` 与 `pjsk-caddy` 均为 Automatic/Running、运行账户为 LocalService；后端监听 `127.0.0.1:8080`，Caddy 监听 8081，8081 与 8080 `/health` 均返回数据库 connected。PostgreSQL 服务 `postgresql-x64-18` 为 Automatic/Running、仅监听回环 5432；IIS/W3SVC 占用本机 80。
- 本机还存在两个 Vite 开发监听 `127.0.0.1:5173` 与 `127.0.0.1:5174`，命令行均指向 Node/Vite；没有 8512/8513 Streamlit 监听。停写不能粗暴终止全部 `node.exe`（Codex/MCP 等也使用 Node），只能按监听 PID、`vite` 命令行和仓库路径三重确认后停止对应开发进程。
- 默认无密码的本机 PG18 `psql -w -d pjsk` 被 SCRAM 正确拒绝，未执行 SQL。因此正式窗口前必须确认一种受保护的数据库管理员认证方式（现有受 ACL 保护的 pgpass 或人工交互输入），不得读取真实 env、把密码写进命令行或临时降低 HBA。
- 云端只读核对 PGDG 实际实例 unit 为 `postgresql@18-main.service`；模板是 Type=forking，调用 `pg_ctlcluster --skip-systemctl-redirect 18-main start`，并 `PartOf=postgresql.service`。正式后端必须显式 `Requires=`/`After=postgresql@18-main.service`，不能只依赖通用 `postgresql.service`，更不能错误依赖 PG16。
- 当前隔离恢复快照中 `user_recovery_emails` 总数、非空 `encrypted_email` 与非空 `email_lookup_hash` 均为 0。因此按当前快照没有必须延续的恢复邮箱密钥；但最终停写快照必须再次做相同聚合。若任一加密记录非零，必须安全迁移原 `RECOVERY_EMAIL_ENCRYPTION_KEY` 与 `RECOVERY_EMAIL_HMAC_KEY`，并用不暴露值的解码长度/指纹对比验收，禁止生成新密钥覆盖旧密文。

#### A. 正式维护窗口前置条件

必须在停写前完成，任一缺失就不开始窗口：

1. 用户确认正式域名 `<PJSK_DOMAIN>`，DNS 管理权限与证书联系人；A 记录提前指向 `43.161.238.145`，TTL 建议提前降至 300 秒并等待旧 TTL 失效。
2. 确认 80/443 从公网可达，安全组/UFW 仍只开放 22/80/443；8080/5433 不开放。
3. 确认维护窗口负责人、第二复核人、第一批测试用户、反馈渠道和明确的回滚决策时刻。
4. 确认本机数据库管理员认证方式、现役应用数据库用户名且该角色不是 superuser；记录数据库原 `datconnlimit`，预期为 `-1`。
5. 确认仓库外最终包根目录，例如 `C:\PJSK-Migration-Final`，目标时间戳子目录必须不存在；确认至少两份受保护存储和足够空间。
6. 先在非正式库上演练“导出 snapshot → 同 snapshot dump/迁移清单/聚合 → 回滚协调事务”的最终脚本；正式窗口不得首次运行未经演练的 snapshot coordinator。
7. 最终快照前再次确认恢复邮箱加密记录数、待用查询码恢复验证码/令牌数量。正式 `QUERY_CODE_RECOVERY_HMAC_KEY` 使用新独立值会使旧验证码、未用令牌及旧限流关联失效；必须确认这些临时对象为 0，或先等待 10 分钟流程 TTL 与 1 小时限流窗口并由业务负责人接受失效。
8. 正式域名未确认、DNS 未就绪或无法取得本机数据库管理员凭据时，切换窗口不得开始。

建议维护窗口：**预留 2 小时停写窗口，外加至少 2 小时仅管理员观察期**。当前数据量很小，技术操作预计 45–75 分钟；剩余预算用于人工双检、证书签发和一次可控回滚。DNS 传播必须在窗口前完成，不能把不可控传播时间算进 2 小时。

#### B. 本地正式停写方案与证据

停写顺序是“通知 → 封闭入口 → 停后端 → 精确停止 Vite → 数据库阻止新连接 → 确认静默”，PostgreSQL 始终保持运行供最终 dump 使用。

```powershell
# 管理员 PowerShell；先只读取证，不停止
$cutoverStartedUtc = (Get-Date).ToUniversalTime().ToString('o')
Get-Service pjsk-caddy,pjsk-backend,postgresql-x64-18 |
  Select-Object Name,Status,StartType
Get-NetTCPConnection -State Listen |
  Where-Object LocalPort -in 5432,8080,8081,5173,5174,5175,8512,8513 |
  Sort-Object LocalPort
curl.exe --fail --silent http://127.0.0.1:8081/health

# 通知用户退出并人工确认浏览器不再提交后，先封闭网关，再停止写后端
Stop-Service pjsk-caddy
Stop-Service pjsk-backend
Get-Service pjsk-caddy,pjsk-backend

# 只精确处理实际监听5173–5175且命令行含vite的仓库前端进程；禁止Stop-Process -Name node
$viteListeners = Get-NetTCPConnection -State Listen |
  Where-Object LocalPort -in 5173,5174,5175
foreach ($listener in $viteListeners) {
  $proc = Get-CimInstance Win32_Process -Filter "ProcessId=$($listener.OwningProcess)"
  if ($proc.Name -ne 'node.exe' -or $proc.CommandLine -notmatch 'vite' -or
      $proc.CommandLine -notmatch 'D:\\pjsk\\frontend|node_modules\\vite') {
    throw "Unexpected process on Vite port $($listener.LocalPort); stop and review"
  }
  Stop-Process -Id $listener.OwningProcess
}

# PostgreSQL必须仍为Running；8080/8081/5173–5175/8512/8513必须无监听
if ((Get-Service postgresql-x64-18).Status -ne 'Running') { throw 'PostgreSQL stopped unexpectedly' }
$blockedPorts = Get-NetTCPConnection -State Listen |
  Where-Object LocalPort -in 8080,8081,5173,5174,5175,8512,8513
if ($blockedPorts) { $blockedPorts; throw 'Write entry still listening' }
```

数据库侧使用已批准的本机 PG18.4 工具和受保护管理员认证，先连接 `postgres` 库而不是目标库：

```powershell
$psql = 'D:\PostgreSQL\18\bin\psql.exe'
# 先只读确认目标、现役应用角色、剩余连接和原连接上限；不输出query文本
& $psql -X -v ON_ERROR_STOP=1 -d postgres -c @'
select datname, datconnlimit from pg_database where datname='pjsk';
select usename, application_name, client_addr, state, count(*)
from pg_stat_activity where datname='pjsk'
group by usename, application_name, client_addr, state
order by usename, application_name, state;
'@
# 预期除当前受控管理员/备份连接外无应用连接；若仍有未知连接，立即停止，不强制终止。

# 经双人确认原datconnlimit=-1、现役应用角色非superuser后，阻止新的非超级用户连接
& $psql -X -v ON_ERROR_STOP=1 -d postgres -c 'alter database pjsk connection limit 0;'
# 再次确认没有应用连接；PostgreSQL服务保持Running
& $psql -X -v ON_ERROR_STOP=1 -d postgres -c @'
select usename, application_name, client_addr, state, count(*)
from pg_stat_activity where datname='pjsk'
group by usename, application_name, client_addr, state;
'@
```

**停写完成证据**：维护通知已确认；`pjsk-caddy`/`pjsk-backend` 为 Stopped；8080/8081/5173–5175/8512/8513 无监听；PostgreSQL 仍 Running；目标库 `datconnlimit=0`；`pg_stat_activity` 无未知应用连接。任一不满足就不得生成最终 dump。

停写前失败的本地回滚草案：

```powershell
# 仅当尚未生成最终dump/云端未变化，且原datconnlimit确认是-1
& $psql -X -v ON_ERROR_STOP=1 -d postgres -c 'alter database pjsk connection limit -1;'
Start-Service pjsk-backend
Start-Service pjsk-caddy
curl.exe --fail --silent http://127.0.0.1:8081/health
```

Vite 仅为开发入口，不是现役服务恢复的必要条件；如确需恢复，由原终端和原命令人工启动，不能猜测命令或批量拉起 Node。

#### C. 同一快照的最终 cutover 包

最终产物必须与预演包物理隔离，命名明确带 `cutover-final`：

```powershell
$stamp = (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ')
$root = 'C:\PJSK-Migration-Final'
$dir = Join-Path $root $stamp
if (Test-Path -LiteralPath $dir) { throw 'Final staging directory already exists' }
New-Item -ItemType Directory -Path $dir | Out-Null
$dump = Join-Path $dir "pjsk-cutover-final-$stamp.dump"
$baseline = Join-Path $dir 'source-baseline.json'
$migrations = Join-Path $dir 'source-migrations.txt'
$shaFile = "$dump.sha256"
```

必须使用已演练的 snapshot coordinator 保持一个 `REPEATABLE READ READ ONLY` 事务：

```sql
begin transaction isolation level repeatable read read only;
select pg_export_snapshot(); -- snapshot ID只在该事务存活期间有效
-- 保持本连接和事务不退出；在同一事务内导出迁移清单和全部baseline聚合
-- 外部pg_dump完成并且聚合文件安全写完后：
rollback;
```

外部 dump 命令必须在上述事务仍存活时执行；`<EXPORTED_SNAPSHOT>` 不是密码，但不得手工重复使用旧值：

```powershell
$pgDump = 'D:\PostgreSQL\18\bin\pg_dump.exe'
$pgRestore = 'D:\PostgreSQL\18\bin\pg_restore.exe'
& $pgDump --format=custom --no-owner --no-privileges `
  --snapshot='<EXPORTED_SNAPSHOT>' --file="$dump.partial" pjsk
if ($LASTEXITCODE -ne 0) { throw 'final pg_dump failed' }

# snapshot coordinator在同一只读事务中写source-migrations.txt和source-baseline.json后回滚。
# 清单必须19/0019、含两个0005、无0006/0020/0021；baseline仍为五组聚合。
Move-Item -LiteralPath "$dump.partial" -Destination $dump
& $pgRestore --list $dump | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'PG18 TOC validation failed' }
$hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $dump).Hash.ToUpperInvariant()
"$hash  $([IO.Path]::GetFileName($dump))" |
  Set-Content -LiteralPath $shaFile -Encoding ascii -NoNewline
```

发布门禁：四个文件且仅四个文件；dump/聚合/迁移清单来自同一 exported snapshot；SHA 回读一致；PG18 `pg_restore --list` 退出 0；baseline schema、19/0019、五组聚合和核心值通过；敏感扫描无密码、DSN、密钥、Token、邮箱、备注或逐行业务数据。**预演时间戳 `20260718T165637Z` 的 dump 永远不得作为正式恢复输入。**

当前仓库没有永久保存的 snapshot coordinator；正式窗口前必须把阶段 2F 已证明可行的协调逻辑整理成可审查脚本并在隔离库演练。脚本缺失或演练未通过是硬阻断，不能在窗口中用两个独立快照替代。

#### D. 云端上传、签收、正式角色与正式库

上传仅使用最终包的新时间戳目录；以下均为草案：

```powershell
$remoteStage = "/home/ubuntu/.pjsk-cutover-final-$stamp"
ssh -i 'D:\pjskgood\pjsk.pem' ubuntu@43.161.238.145 "install -d -m 0700 '$remoteStage'"
scp -i 'D:\pjskgood\pjsk.pem' -- $dump,$shaFile,$baseline,$migrations "ubuntu@43.161.238.145:$remoteStage/"
```

云端签收必须先核对文件名、数量、owner/mode、大小和 SHA，再复制到新的 postgres-only 目录；不得覆盖预演目录：

```bash
STAMP='<CUTOVER_UTC_STAMP>'
SRC="/home/ubuntu/.pjsk-cutover-final-$STAMP"
DST="/var/lib/pjsk/backups/incoming/$STAMP-final"
DUMP="pjsk-cutover-final-$STAMP.dump"
test ! -e "$DST"
cd "$SRC" && sha256sum -c "$DUMP.sha256"
sudo install -d -o postgres -g postgres -m 0700 "$DST"
sudo install -o postgres -g postgres -m 0600 \
  "$SRC/$DUMP" "$SRC/$DUMP.sha256" "$SRC/source-baseline.json" "$SRC/source-migrations.txt" "$DST/"
sudo -u postgres /usr/lib/postgresql/18/bin/pg_restore --list "$DST/$DUMP" >/dev/null
```

创建前必须再次断言 PG18:5433 中正式角色/库均不存在。密码仅在服务器本地独立随机生成；使用 postgres-only 0600 临时 SQL/pgpass，任何输出不得含正文：

```bash
# 伪代码：实际脚本必须trap删除临时SQL/pgpass，并在日志中只输出PASS/FAIL
/usr/lib/postgresql/18/bin/psql -p 5433 -d postgres  # 由sudo -u postgres经socket执行
# create role pjsk_app login nosuperuser nocreatedb nocreaterole noreplication password '<RANDOM>';
# create database pjsk owner pjsk_app encoding 'UTF8' template template0;
```

恢复前门禁：数据库 owner=`pjsk_app`、UTF8/template0、public 普通表为 0、`schema_migrations` 不存在、未预建 pgcrypto。恢复命令必须显式 PG18 工具、端口和单事务，且不使用 `--clean`/`--create`：

```bash
sudo -u postgres /usr/lib/postgresql/18/bin/pg_restore \
  --host=127.0.0.1 --port=5433 --username=pjsk_app --dbname=pjsk \
  --no-owner --no-privileges --role=pjsk_app \
  --exit-on-error --single-transaction "$DST/$DUMP"
```

恢复后、启动任何后端前必须确认：19/0019；完整迁移列表等于最终 `source-migrations.txt`；两项未来表不存在；全部五组聚合与最终 baseline 一致；数据库/业务对象 owner 正确；37 个 postgres-owned pgcrypto 函数只能通过精确 `pg_depend.deptype='e'` 例外。失败时保留该库并停止，用**新数据库名**重试；禁止就地 `--clean`。

#### E. 正式 backend.env 与密钥连续性

正式 env 路径固定 `/etc/pjsk/backend.env`、`root:pjsk 0640`，使用原子临时文件安装；不得 cat：

```dotenv
APP_ENV=production
SERVER_HOST=127.0.0.1
APP_PORT=8080
DATABASE_HOST=127.0.0.1
DATABASE_PORT=5433
DATABASE_USER=pjsk_app
DATABASE_PASSWORD=<SERVER_LOCAL_RANDOM_SCRAM_PASSWORD>
DATABASE_NAME=pjsk
DATABASE_SSLMODE=disable
ADMIN_SESSION_TTL=12h
ADMIN_COOKIE_SECURE=true
TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128
CORS_ALLOWED_ORIGINS=
RECOVERY_EMAIL_SENDER_MODE=disabled
QUERY_CODE_RECOVERY_HMAC_KEY=<INDEPENDENT_BASE64_32_BYTES_OR_MORE>
```

- 若最终 baseline 的加密恢复邮箱数仍为 0，可以不配置恢复邮箱 AES/HMAC；若大于 0，必须额外加入从本地受保护配置安全迁移的 `RECOVERY_EMAIL_ENCRYPTION_KEY` 与 `RECOVERY_EMAIL_HMAC_KEY`，两者同时存在并做不暴露值的指纹一致性检查。不得通过聊天、剪贴板、日志或 Git 传递。
- `RECOVERY_EMAIL_SENDER_MODE=disabled` 时，匿名查询码邮件找回入口不可用，已登录用户也不能收取邮箱验证码；管理员仍可管理恢复邮箱记录。SMTP 启用必须另开阶段验证证书、TLS、发件人和出站 ACL，不能在切换窗口临时开启。
- env 验收只运行 `stat`、pjsk 可读/不可写测试、变量名白名单和 base64 解码长度断言；不得输出值。

#### F. 正式 systemd unit 草案

PGDG 实例依赖使用实际 `postgresql@18-main.service`：

```ini
[Unit]
Description=PJSK backend
Requires=postgresql@18-main.service
After=network-online.target postgresql@18-main.service
Wants=network-online.target
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=simple
User=pjsk
Group=pjsk
WorkingDirectory=/opt/pjsk/current
EnvironmentFile=/etc/pjsk/backend.env
ExecStartPre=/usr/lib/postgresql/18/bin/pg_isready --quiet --host=127.0.0.1 --port=5433 --dbname=pjsk
ExecStart=/opt/pjsk/current/bin/pjsk-backend
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
UMask=0027
StandardOutput=journal
StandardError=journal
SyslogIdentifier=pjsk-backend

[Install]
WantedBy=multi-user.target
```

先用一份仅把 `current` 两处替换为已签收 release 绝对路径的临时 verify 副本运行 `systemd-analyze verify`，确保可执行文件存在并验证所有 directive；最终文件与 verify 副本除这两处路径外必须逐行一致。安装最终 unit 后只做 `daemon-reload` 与 `systemctl cat/show`，尚不启动。这样可满足“unit verify 通过后才创建 current”，避免因 `current` 尚不存在导致 verify 的可执行路径检查误报。

#### G. current、启动与数据库迁移

只有正式库恢复后 19/0019 聚合通过、env 权限通过、等价 unit verify 通过后才创建：

```bash
test ! -e /opt/pjsk/current
test "$(readlink -f /opt/pjsk/current.next)" = '/opt/pjsk/releases/14d339e56677'
sudo ln -sfn /opt/pjsk/releases/14d339e56677 /opt/pjsk/current
test "$(readlink -f /opt/pjsk/current)" = '/opt/pjsk/releases/14d339e56677'
sudo systemctl enable --now pjsk-backend.service
```

启动门禁：unit active/running、NRestarts=0、只监听 `127.0.0.1:8080`、journal 无秘密、`curl http://127.0.0.1:8080/health` 为 HTTP 200/database connected；迁移严格 21/0021、集合与 release 一致；0020/0021 结构/owner/索引通过；全部来源聚合仍与最终 baseline 完全相同，新两表为 0。失败立即停止 unit，保留数据库/env/journal，不手工执行 SQL。

`current` 只是二进制选择，不是数据库回滚。0020/0021 已提交后，仅把 symlink 切回旧二进制不能撤销 schema；禁止手工删除 `schema_migrations` 行或两张表。

#### H. 正式域名与 Caddyfile 草案

没有正式域名时，只能完成服务器本机 `/health`、数据库、结构和端口验收；`ADMIN_COOKIE_SECURE=true` 下不能把明文 HTTP 浏览器登录当作正式验收，也不能完成公开 HTTPS 或客户试用。

域名确认后的 Caddyfile 草案（`<PJSK_DOMAIN>` 必须替换）：

```caddyfile
<PJSK_DOMAIN> {
    encode zstd gzip

    request_body {
        max_size 25MB
    }

    @backend path /api/* /health
    handle @backend {
        reverse_proxy 127.0.0.1:8080 {
            header_up X-Forwarded-For {remote_host}
            header_up X-Forwarded-Proto {scheme}
            header_up Host {host}
        }
    }

    handle {
        root * /opt/pjsk/current/frontend
        try_files {path} /index.html
        file_server
    }

    header {
        X-Content-Type-Options nosniff
        X-Frame-Options DENY
        Referrer-Policy strict-origin-when-cross-origin
        Permissions-Policy "camera=(), microphone=(), geolocation=()"
        Strict-Transport-Security "max-age=31536000"
        -Server
    }

    log {
        output file /var/log/pjsk/caddy/access.log {
            mode 0640
            roll_size 100MiB
            roll_keep 10
            roll_keep_for 720h
        }
        format json
    }
}
```

切换时先为 Caddy 用户创建专用日志目录，再把当前 Caddyfile备份为 root-only 时间戳文件；将草案写入 `.next`，运行 `caddy fmt --diff` 与 `caddy validate --config <next>`，只有验证通过才原子安装并 `systemctl reload caddy`。Caddy 自动申请证书并把 HTTP 跳转 HTTPS；验收 A/AAAA、证书 SAN/有效期、HTTP→HTTPS、SPA fallback、25MB 上限、响应头和日志滚动。Caddy 官方文档确认 `request_body max_size`、`reverse_proxy`、`try_files`/`file_server` 与 file log rolling 的语法：<https://caddyserver.com/docs/caddyfile/directives/request_body>、<https://caddyserver.com/docs/caddyfile/directives/reverse_proxy>、<https://caddyserver.com/docs/caddyfile/directives/try_files>、<https://caddyserver.com/docs/caddyfile/directives/file_server>、<https://caddyserver.com/docs/caddyfile/directives/log>。`request_body` 在当前 2.11.4 可用但官方仍标为 experimental，升级 Caddy 前必须重新验证。

#### I. 严格切换顺序与停止点

| # | 动作与验收 | 失败停止点 |
|---|---|---|
| 1 | 宣布维护开始，记录UTC、负责人/复核人、回滚时刻 | 通知未覆盖全部使用者则不操作 |
| 2 | 停 `pjsk-caddy`、`pjsk-backend`，精确停止Vite；PG仍Running | 任一写入口仍监听则停止 |
| 3 | 核对连接并设置本地 `pjsk connection limit 0` | 未知连接、应用角色为superuser或原limit非预期则停止 |
| 4 | exported snapshot协调事务生成final dump/baseline/migrations | 不是同一snapshot、脚本未演练或任一命令非0则恢复本地 |
| 5 | 本地SHA、PG18 TOC、19/0019、五组聚合、敏感扫描 | 任一不一致则废弃本次final目录，不上传 |
| 6 | 上传全新时间戳目录并由postgres签收 | SHA/TOC/文件权限不符则不建库 |
| 7 | 在PG18:5433创建`pjsk_app`与`pjsk` | 名称已存在、角色权限或SCRAM不符则停止 |
| 8 | 单事务恢复final dump | 失败则保留失败库，不clean、不启动后端 |
| 9 | 核对19/0019、完整来源集合、聚合、owner | 任一失败则停止 |
| 10 | 原子创建正式backend.env并做无回显验收 | 权限/变量/密钥连续性不符则停止 |
| 11 | 生成并验证等价systemd unit，安装但不启动 | verify或PG18依赖不符则停止 |
| 12 | 创建`current`指向批准release | 目标/REVISION/MANIFEST不符则停止 |
| 13 | 启动正式后端，由候选自动补0020/0021 | unit重启、日志异常、非回环监听则停服 |
| 14 | 核对21/0021、结构、owner、全部聚合，新表为0 | 任一不符则停服并进入迁移后回滚分支 |
| 15 | 本机8080 `/health` HTTP 200，8080/5433公网不可达 | health或端口边界失败则不配Caddy |
| 16 | 仅在域名/DNS就绪后验证并原子切换Caddy | 证书/配置失败则恢复旧Caddyfile，不开放 |
| 17 | 验收HTTPS、HTTP跳转、Secure Cookie、SPA/API、响应头 | 任一失败则只允许本机，不开放客户 |
| 18 | 管理员完成人工业务验收清单 | 任一关键路径失败则停在管理员阶段 |
| 19 | 明确名单的小范围客户试用 | 未满足开放条件则不得发链接 |
| 20 | 至少2小时观察，审查错误率、日志、磁盘、备份 | 异常冻结扩容并按客户开放后分支处置 |

#### J. 回滚矩阵

| 失败边界 | 权威数据源 | 回滚动作 | 禁止事项 |
|---|---|---|---|
| A. final dump前 | 本地现役库 | 恢复原connection limit，启动本地backend后caddy，health通过后通知恢复 | 不动云端正式对象 |
| B. final dump后、云端恢复前 | 本地现役库 | 保留final包审计；如已建空正式对象则保留并人工决定，或在明确授权下删除空对象后重排窗口 | 不用预演dump替代final |
| C. 云端恢复后、迁移前 | 本地现役库 | 不启动云端后端；保留失败正式库和日志；用新数据库名重新恢复并重新验收 | 不就地`--clean`、不覆盖失败库 |
| D. 迁移后、客户开放前 | 本地现役库 | 停止/禁用云端后端与Caddy新站点；确认云端无业务新写入后恢复本地connection limit和服务 | 二进制回滚不冒充schema回滚；不删0020/0021或迁移行 |
| E. 客户开放后已有云端写入 | 需人工确定，不能自动假设本地 | 立即停写两端，保存云端新备份，比较增量，制定人工合并/双向迁移方案 | 禁止用旧final dump覆盖云端、禁止直接切回本地丢失新写入 |

所有分支都必须明确：回滚二进制不等于回滚数据库；不手工删除 `schema_migrations`、0020/0021表；不使用旧 dump 覆盖含新写入的库。

#### K. 管理员人工验收与客户开放门禁

管理员必须在正式 HTTPS 域名上逐项记录 PASS/FAIL：

- 管理员登录、退出、错误密码限流、Secure/HttpOnly/SameSite Cookie；普通用户登录/查询及越权拒绝。
- 订单与明细查询、金额一致；Excel 导入预览、确认、失败反馈与安全批次回滚；Excel 导出可打开且字段正确。
- 付款二维码上传/替换/禁用；用户付款凭证上传；管理员审核通过/驳回；金额只能由正式 approved payment 改变。
- 文件类型、尺寸和25MB反代边界；未知Origin无CORS放行；伪造X-Forwarded-For不越过可信代理边界。
- 320px移动端的登录、查询、付款和上传；至少一台非服务器、不同网络设备通过HTTPS访问。
- 后端/Caddy/PostgreSQL日志无密码、DSN、Cookie、验证码、Token、邮箱、备注或业务明细；磁盘与日志滚动正常。
- `systemctl restart pjsk-backend` 后自动恢复、health 200、NRestarts合理；服务器重启演练需另获授权，但客户扩围前至少完成一次受控重启自恢复验收。
- 从外部网络确认8080与5433不可达，80只跳HTTPS，443证书可信；UFW/security group无新增数据库/后端端口。

客户试用必须同时满足：正式域名/HTTPS；正式库21/0021及全部聚合匹配；管理员验收全通过；切换后正式备份成功且SHA/TOC通过；至少一次云端恢复验证；systemd自恢复；failed unit为0；8080/5433不公网开放；日志无秘密；第一批测试用户名单、反馈渠道、试用期限、暂停与回滚负责人明确。

#### L. 尚需用户提前确认与风险清单

必须确认的信息：正式域名；DNS/证书控制权；维护窗口日期和通知范围；本机数据库管理员认证方式；现役应用数据库角色与非superuser证明；最终备份根和第二份受保护存储；若最终出现加密恢复邮箱记录时原密钥的安全迁移渠道；查询恢复HMAC轮换是否接受；第一批测试用户、反馈渠道和试用期；PG16后续单独保留/停用评审时间。

主要风险：停写遗漏Vite/浏览器或未知脚本导致快照后仍有写入；snapshot coordinator未固化；把预演包误作final；凭据出现在命令行/日志；恢复邮箱密钥断裂；PG18 unit依赖写错；迁移后误以为切回二进制可回滚schema；DNS/ACME未提前就绪；Caddy实验性request_body语法随升级变化；Secure Cookie在无HTTPS时使登录看似失败；开放后产生云端新写入导致不能简单回切；日志/备份磁盘增长；PG16与PG18端口混淆。

无域名时可以先完成服务器本机数据库、21条迁移、owner、聚合、8080 health和端口隔离验收，但不能完成正式HTTPS、Secure Cookie浏览器登录、不同网络设备验收或客户试用。

阶段 2G-Plan 结论：方案层面可进入“维护窗口前准备”，但**尚不可预约执行正式停写**；正式域名、数据库管理员认证、snapshot coordinator固化与演练、DNS/证书前置、测试用户名单等仍是硬前置。本节仅形成方案，未执行任何生产切换动作。

### 12.22 阶段 2G-1：Snapshot Coordinator 固化与本地隔离演练（2026-07-19）

#### 调查结论与旧流程风险

- 开始时 Git 仍为 `main`，HEAD `14d339e56677b6faff5d0769279cb2feaf82a9bc`，相对 `origin/main` ahead 2 / behind 0；唯一既有工作树改动为本部署日志，暂存区为空。
- 既有 `Backup-Postgres.ps1` 只运行独立的 `pg_dump --format=custom --no-owner --no-privileges`，再做 TOC、SHA 和普通 metadata 发布；它没有 `pg_export_snapshot()`、没有 `--snapshot`，也不生成业务 baseline 或完整 migrations 清单。如果把它与另一次独立查询拼成正式迁移包，会存在 dump 与 baseline 时间点不同、baseline 多条查询之间被并发写入穿插、migration count/max 相同但完整集合不同、dump 失败而其他文件被误当成功产物等风险。
- 2026-07-18 的 preflight 曾用临时协调逻辑成功取得一致快照，但仓库此前没有可审查、可复用、带失败原子性门禁的永久 coordinator；因此它不能直接作为正式维护窗口的执行器。
- 只读确认默认 `%APPDATA%\postgresql\pgpass.conf` 存在，长度仅作存在性检查，ACL 为当前 Windows 用户的显式读写权限且无 Everyone/Users/Authenticated Users 广泛读取；从未读取或输出文件正文。PG18 `psql -w` 可用该既有认证连接 `127.0.0.1:5432/pjsk`，所以不需要把密码放进参数、环境日志、进程列表或仓库。

#### 固化实现

- 新增 `scripts/database/Export-PostgresSnapshot.ps1`：参数明确包含 SourceHost/Port/Database/User、仓库外 OutputRoot、PG18 bin 目录、Mode 和可选 Passfile；所有标识符、主机、路径、工具和 ACL 先验证，显式拒绝仓库内输出、旧目录覆盖、非 PG18 工具及不一致的 psql/pg_dump/pg_restore/server 版本。
- coordinator 以一个持久在线 `psql` 进程执行 `BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY` 和 `pg_export_snapshot()`；同一事务读取完整 `schema_migrations` 与全部聚合，事务保持打开期间显式调用 PG18.4 `pg_dump --snapshot=<本次snapshot>`。dump 只写 `.partial`，须满足退出 0、非空、PG18 `pg_restore --list` 非空并且协调事务仍为只读 repeatable-read 后才结束事务和发布。
- baseline schemaVersion 2 动态覆盖：22 张现存 public 表计数、12 组 status/payment_status 分布、1 组实际 bytea 非空计数、9 组 numeric 精确合计（字符串）、3 组整数精确合计、69 组 UTC timestamp 边界及 10 个安全辅助表计数；每个合计同时记录 total/nonNull 或 NULL，金额从不转成浮点数。
- 为保持与旧 `source-baseline.json` 完整兼容，另保留旧五组全部键：10 sums、23 counts（包括尚不存在的 payment_qr_codes/payment_submissions=0）、5 UTC time bounds、12 status distributions、3 binary nonempty counts。兼容层与动态层交叉绑定，不是未验证的复制。
- `source-migrations.txt` 来自同一协调事务，按完整文件名确定性排序，逐行保存 19 个实际版本，保留两个 0005；空值、重复、未知格式或非确定顺序全部失败，绝不只用 count/max 推断集合。
- 成功目录严格发布六个文件：rehearsal custom dump、`.dump.sha256`、`source-baseline.json`、`source-migrations.txt`、`export-metadata.json`、`validation.json`。metadata/validation 通过 run ID 与 baseline 绑定，并同时记录 baseline/migrations SHA、dump SHA/大小/TOC、PG 版本、Git SHA、UTC 时间和 `productionRestoreAllowed=false`；成功终态不得有 partial。
- rehearsal 名称必须为 `pjsk-rehearsal-<UTC>` 且严禁 final/cutover 字样。cutover 没有默认入口，必须显式 `Mode=cutover`、`WriteFreezeConfirmed` 和 8–64 位唯一 MaintenanceWindowId；任何一项缺失即在连接数据库前拒绝。本轮没有执行 cutover。
- 新增 `_SnapshotCoordinator.ps1` 纯验证辅助模块、`Invoke-SnapshotCoordinatorTests.ps1` 失败原子性 mock 测试、`Test-PostgresSnapshotBaseline.ps1` 隔离恢复全聚合比较器；`Invoke-ScriptSafetyTests.ps1` 纳入 coordinator 套件。另修正 `Invoke-BackupValidationTests.ps1` 的陈旧 mock：current 模式从仓库迁移目录动态取得 21/0021，不再硬编码 19/0019；pre-migration 的显式落后集合门禁未放宽。

#### 演练过程与失败审计

- 演练前只读基线：PostgreSQL 18.4、19/`0019_admin_auth_audit_events.sql`；核心聚合为 users 45、orders 44、order_items 120、payments 7、payment_items 19，orders/order_items 金额合计均 6318.44，payment_items applied 合计 1145.40。仅记录聚合，没有逐行业务数据。
- 第一次真实 rehearsal 在 `C:\PJSK-Snapshot-Rehearsal\pjsk-rehearsal-20260719T023513122Z` 安全失败：PowerShell SQL 中 `COLLATE "C"` 被错误传成 psql 反斜杠命令，退出发生在 TABLE_CATALOG，尚未启动 pg_dump。事务回滚；目录仅保留 `source-migrations.txt.partial` 和 `validation.failed.json`，verdict 非 PASS，未覆盖或伪装成成功产物。
- 修正为 PowerShell 原生双引号转义并增加静态门禁后，mock 测试通过；中间 rehearsal `20260719T023640448Z` 成功，但复核旧 baseline 原件后发现动态模型尚未显式保留旧 23/10/5/12/3 固定键，因此该目录保留为历史候选，不作为本阶段最终候选。
- 补齐兼容层后从全新目录运行最终 rehearsal：`C:\PJSK-Snapshot-Rehearsal\pjsk-rehearsal-20260719T024550122Z`。开始 `2026-07-19T02:45:50.1228660Z`，结束 `2026-07-19T02:45:51.1999854Z`，metadata 记录 1077 ms。
- 最终 dump `pjsk-rehearsal-20260719T024550122Z.dump` 为 121,804 bytes，SHA-256 `A197770BEEC8DE3BF4EDE486A283EECBF709E2BEEE7BE5E59B1266CEA4398B9F`；独立 SHA 回读一致，PG18.4 `pg_restore --list` 退出 0，TOC 174。六文件合计 178,729 bytes，无 `.partial`，mode=rehearsal、productionRestoreAllowed=false、validation verdict=PASS。
- baseline、migrations、dump 都由一个 exported snapshot 协调：baseline 与 metadata/validation run ID 一致，migration 文件 SHA 被 metadata/validation 绑定，pg_dump 实际参数含本次 snapshot ID；最终清单严格 19/0019、两个 0005。
- 对最终目录六文件逐个做凭据标记扫描：PRIVATE KEY、DATABASE_URL/DATABASE_PASSWORD/PGPASSWORD 赋值、PostgreSQL DSN、恢复邮箱/HMAC/SMTP 密钥及 Authorization Bearer 均 0 命中。没有读取 ignored env 或 pgpass 正文。

#### 本地隔离恢复与服务无影响证明

- 使用既有严格测试前缀流程将最终 dump 恢复到全新 `pjsk_restore_test_snapshot_20260719t024550z`；未使用 clean/create archive 参数、未启动后端、未运行 0020/0021。既有结构验证通过：19/0019 完整集合、22 表、22 主键、39 外键、89 索引、0 sequence、pgcrypto 与 UUID 函数均正确。
- 新全聚合 verifier 通过：19 条 migrations 精确一致，22 动态 counts、12 statuses、1 binary、9 numerics、3 integers、69 UTC bounds 全部与 baseline 匹配；旧五组兼容层全部与动态层/恢复库交叉一致，数据库及 public 关系 owner 均为预期 postgres。
- 验证后通过仓库严格前缀清理脚本删除该一次性恢复库；终态 `pjsk_restore_test_snapshot_%` 数据库数量为 0。隔离库的创建、restore 和删除是本轮唯一数据库写操作；现役来源 `pjsk` 只执行 read-only repeatable-read 查询和 pg_dump，从未写入或迁移。
- 来源终态仍为 19/0019，核心计数和三项金额聚合与演练前相同。本结果不把业务运行期间可能出现的自然并发变化当成 coordinator 写入证据；本次实际窗口内所记录聚合恰好未变化。
- 服务前后 PID 保持：pjsk-backend 7656、pjsk-caddy 21288、postgresql-x64-18 5340、W3SVC 5976；监听进程保持 80 PID4、5173 PID4196、5174 PID12824、5432 PID9108、8080 PID21248、8081 PID1176。没有停止、重启或修改后端、Caddy、PostgreSQL、IIS、Vite。

#### 测试与停止点

- PowerShell 语法检查：新增/修改脚本 0 parse error。
- snapshot coordinator mock：29/29 通过，覆盖 snapshot/baseline/migrations/dump/空文件/TOC/SHA/事务提前结束/快照参数/cutover 参数/命名/覆盖/partial/浮点金额/完整迁移集合/秘密/PG18/外部退出码等门禁。
- 既有数据库脚本组件：retention 66/66、migration facts 27/27、backup publish 40/40、backup validation 101/101 通过；测试只使用 mock 或抛弃目录，不连接真实业务库。
- `go test ./...` 退出 0；`go vet ./...` 退出 0。
- 本阶段未生成任何 final/cutover dump，未上传云端，未停止现役服务，未阻止应用连接，未修改现役业务数据，未运行数据库迁移，未创建云端对象，未修改 Caddy/DNS/UFW/安全组，未切生产，未提交或推送。

阶段 2G-1 结论：snapshot coordinator 的代码、失败门禁、rehearsal 产物和本地可恢复性均已通过。建议在独立代码审查后提交本批脚本与日志；提交/推送须由用户另行授权。正式 cutover 仍需用户确认维护窗口 ID、明确停写已完成并再次授权执行，不能把任何 rehearsal 目录作为生产恢复输入。

### 12.23 阶段 2G-1A：提交前阻断项修复与原子发布复验（2026-07-19）

#### 提交前审查阻断与根因

- 阻断 1：rehearsal 原实现只检查生成的 `pjsk-rehearsal-<UTC>` basename；`OutputRoot` 只做绝对路径和仓库外判断，因此规范化路径的上级目录仍可能包含 `final` 或 `cutover` 正式语义。
- 阻断 2：原版本解析器只要求 PostgreSQL major=18，并要求 psql/pg_dump/pg_restore/server 相互一致；因此全套 18.3 或 18.5 仍会通过，未达到本项目已批准的 18.4 精确版本门禁。
- 阻断 3：原实现预先创建最终命名目录，再逐个把六个 `.partial` 文件移动为最终文件；若中间一次 `Move-Item` 失败，可能留下部分正式命名文件，不能证明成功集合整体原子可见。
- 审查发现阻断后未暂存、提交或推送；随后仅在已批准的七文件范围内修复。此前 2G-1 的失败记录和 rehearsal 记录均原样保留。

#### 修复实现

- `_SnapshotCoordinator.ps1` 新增 `Resolve-SnapshotOutputRoot`：先用 `GetFullPath` 规范化，再逐级检查路径组成部分；rehearsal 对大小写不敏感地拒绝任何包含 `final` 或 `cutover` 的目录分量，然后继续执行绝对路径及仓库外门禁。cutover 不套用 rehearsal 语义门禁，但仍受显式 mode、停写确认、维护窗口 ID 和独立命名规则约束。
- 固定唯一允许版本为 PostgreSQL 18.4，对应 `server_version_num=180004`。psql、pg_dump、pg_restore 分别解析并精确比较 18.4；协调事务另读取结构化 `server_version_num`，服务器展示版本与数值必须同时精确匹配，18.3、18.5、仅 major 18、不可解析文本及工具/服务器交叉不一致全部拒绝，错误包含实际值与要求值。
- exporter 不再预建最终目录。它在最终目录同一父目录创建唯一 `.pjsk-snapshot-staging-<32hex>` 工作目录；dump 先写 `.partial`，其余文件也只在 staging 内构建。事务存活、非空、TOC、SHA、敏感扫描和全部模型完成后，先在 staging 内最终化文件名。
- 发布前 `Assert-StagingArtifactSet` 再检查严格六文件、无 `.partial`、dump 非空、SHA sidecar 与真实 dump 文件名/哈希绑定、三份 JSON 可解析、validation=PASS、完整 migration 清单合法；PG18 `pg_restore --list` 再次读取最终化 staging dump 并要求 TOC 与前次一致。
- staging 与 final 必须同父目录、同卷；发布前再次拒绝已存在 final。成功路径只有一次目录级 `Move-Item $stagingDirectory -> $finalDirectory`，不再逐文件写入最终目录。任何生成、验证或 rename 前失败都只保留明确 staging 语义和 `validation.failed.json`，不创建最终目录；历史目录不覆盖、不合并、不清理。

#### 针对性测试与开发中失败记录

- PowerShell 六个相关脚本最终均为 0 parse error。
- coordinator mock 最终 68/68。正例覆盖完整 staging 六文件、无 partial、单次目录 rename 后 staging 消失；负例覆盖 snapshot/baseline/migrations/dump/空 dump/TOC、SHA、事务提前结束，以及 migrations、baseline、metadata、validation、staging-finalization、pre-publication、directory-rename 八个故障注入点均不出现 final 目录。
- 路径门禁明确拒绝 `C:\PJSK-cutover-artifacts`、`C:\PJSK-FINAL-artifacts`、`C:\temp\cutover\rehearsal`、`C:\temp\Final_Output\rehearsal` 和规范化后落入 cutover 的相对路径；接受 `C:\PJSK-Snapshot-Rehearsal`、`C:\PJSK-Rehearsal-Artifacts\`，且 cutover 不被 rehearsal 路径规则误拒绝。
- 版本测试明确接受 18.4；拒绝 18.3、18.5、仅 `18`、不可解析输出、工具/服务器交叉不一致及错误的 `server_version_num`。静态门禁确认源码恰好存在一次 staging 到 final 的目录级 rename。
- 发布集合测试分别拒绝缺少任一文件、残留 `.partial`、validation 非 PASS、额外子目录、不同父目录、不同卷和已存在 final；已存在目录的 sentinel 哈希保持不变。
- 第一次改后 mock 安全失败于 PowerShell 数组表达式把预期六文件合并为单个值，未发布 final；修正为逐项数组后，下一次仅因故障测试目录名自身含 `staging-finalized`（命中新增的 `final` 路径门禁）而 65/66。测试目录改用无正式语义名称后最终 66/66。两次均只涉及自动清理的临时 mock 根，不连接数据库、不影响历史 rehearsal。

#### 全量回归

- Retention 66/66、Migration Facts 27/27、Backup Publish 40/40、Backup Validation 101/101、Snapshot Coordinator 68/68；`Invoke-ScriptSafetyTests.ps1` 最终复跑汇总退出 0 并输出 `all safety tests passed`。
- `go test ./...` 退出 0；`go vet ./...` 退出 0。
- 修改范围复核、七文件真实敏感值扫描、`git diff --check` 和最终 Git 状态在本节收尾门禁中执行；本阶段始终保持暂存区为空。

#### 修复后真实 rehearsal

- 自动化门禁通过后，用修复后代码、显式 `Mode=rehearsal`、PostgreSQL 18.4 和既有受保护 pgpass 对本地现役 `127.0.0.1:5432/pjsk` 执行一次新的只读 exported-snapshot rehearsal；未读取或输出 pgpass 正文，未把密码放入参数、日志或进程列表。
- 新目录：`C:\PJSK-Snapshot-Rehearsal\pjsk-rehearsal-20260719T053959776Z`。dump 121,804 bytes，SHA-256 `31072269001BCA9B70FA9D736A2D6CA442BCA31B4A220CFC3C9832420FE802D4`，PG18.4 TOC 174；严格六文件合计 178,728 bytes，无 `.partial`，根目录无残留 `.pjsk-snapshot-staging-*`，validation=PASS、productionRestoreAllowed=false、mode=rehearsal。
- 该真实运行经过源码唯一的同父目录 staging-to-final rename 路径；发布后 final 恰好一个、staging 不存在、六文件完整。mock 故障注入另证明 rename 前任一失败不会产生 final。没有删除或覆盖 2G-1 的旧成功候选、第一次失败取证或中间历史候选。
- 旧 `pjsk-rehearsal-20260719T024550122Z` 明确是修复前产物，只作为旧一致快照/恢复证据，不能证明新目录级原子发布实现；本节的新目录才是修复后真实流程证据。
- rehearsal 后只读事务核对：残留 `pjsk_restore_test_%` 数据库为 0，来源仍为 19/`0019_admin_auth_audit_events.sql`，查询显式 rollback。pjsk-backend、pjsk-caddy、PostgreSQL 18 和 W3SVC 均保持 Running；监听保持 80、127.0.0.1/::1:5432、127.0.0.1:8080 和 8081。

本轮没有正式 cutover、final/cutover dump、云端上传、服务停止或重启、现役数据库写入或迁移、流量切换、历史目录删除、提交或推送；未使用子代理。完成验证后停在 2G-1A，等待再次提交前审查与单独提交授权。
