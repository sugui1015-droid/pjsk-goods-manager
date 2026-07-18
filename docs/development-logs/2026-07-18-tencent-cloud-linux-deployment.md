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
