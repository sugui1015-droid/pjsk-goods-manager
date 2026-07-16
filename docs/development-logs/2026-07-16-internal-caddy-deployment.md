# 内网 Caddy 网关部署执行记录（2026-07-16：正式前端构建 + Caddy 局域网 HTTP 入口 + NSSM 服务化）

> 安全边界：本文不记录任何密钥值、数据库密码、完整连接串、查询码或令牌。真实 Caddyfile、Caddy/NSSM 二进制、前端部署产物、运行日志均位于仓库外，不进入 Git。
> 执行模式：Claude 会话无管理员权限；构建、下载核验、配置生成、脚本编写与文档由 Claude 直接完成；ACL 收紧、NSSM 服务注册、防火墙与启停/恢复测试由带门禁与回滚的 `.ps1` 脚本（`D:\PJSK-Tools\Scripts\`）交管理员 PowerShell 执行。
> 恢复指引：若中途中断，按本文各节"状态"续做；所有已完成步骤均可只读复核（哈希、ACL、服务配置）。

## 1. Git 与后端运行态基线（只读，已通过）

- 时间：2026-07-16 12:3x（Asia/Shanghai）。
- Git：`main`；HEAD 与 `origin/main` 均为 `3a12f0364646c7d88fb557b6efbfeb8fc1f7ba55`（`docs: record successful NSSM backend deployment`）；工作区干净。
- 后端门禁：`pjsk-backend` Running（NSSM 包装 `D:\PJSK-Service\backend\nssm.exe`，`NT Authority\LocalService`，Auto）；后端进程 1 个（PID 19516，本次证据值）；仅 `127.0.0.1:8080` 监听；`/health` HTTP 200、`database=connected`。
- `pjsk-caddy` 服务不存在；无 caddy 进程；5173 无监听。
- 本轮不修改 `pjsk-backend`、后端 exe、`backend\.env` 及其 ACL、PostgreSQL。

## 2. 端口 80 冲突调查与端口决策（已决策：8081）

- `[::]:80` 由 PID 4（System/http.sys）监听；`netsh http show servicestate` 显示 URL `HTTP://*:80/` 注册给 IIS `DefaultAppPool`（W3SVC "World Wide Web 发布服务" Running）。本机装有 Siemens TIA/SIMATIC 组件，IIS 可能被其使用。
- 安全否决：不停用/修改 IIS、W3SVC 或其站点绑定（未授权、可能影响 Siemens 工具链）。
- 用户决策：Caddy 局域网 HTTP 入口使用 **8081**。核验 8081 当前无 TCP 监听、http.sys 无 8081 前缀注册。
- 防火墙将只放行 TCP 8081（Private profile + LocalSubnet + 限定 caddy.exe 程序），不动 80/8080/5173/5432。

## 3. 前端与后端路由只读调查结论（引用实际代码）

- 构建命令：`frontend/package.json` → `build = vue-tsc -b && vite build`；包管理器 pnpm（`pnpm-lock.yaml` 存在）；本机 node v24.18.0、pnpm 11.11.0。
- `frontend/vite.config.ts`：无自定义 `base`（默认 `/`）；dev server 与 proxy 仅 dev 模式生效，不进构建产物。
- `frontend/.env.development` 存在，但 `vite build`（production mode）不加载 `.env.development`，对产物无影响；`frontend/` 无 `.env` / `.env.production`。
- API 基址：`frontend/src/api/client.ts` — 生产构建使用 `VITE_API_BASE_URL`（未设置 → 空字符串 → 相对路径）。同源部署无需设置，保持相对路径。
- 前端路由：history 模式（`App.vue` 使用 `window.location.pathname` + `history.pushState`）。SPA 路由包括 `/`、`/query`、`/admin/imports*`、`/admin/orders*`、`/admin/payments*`、`/admin/users*` 等 → 需要 SPA 回退到 `/index.html`。
- 后端真实路由（`backend/internal/api/router.go`）：仅 `/health` 与 `/api/*`（`/api/config`、`/api/admin/*`、`/api/query/*`）。**`/admin/*` 是前端路由，不是后端路径，不得代理到后端**（与 `docs/internal-https-reverse-proxy.md` §1 一致）。
- 反代范围决定：仅 `path /api/*` 与 `path /health` → `127.0.0.1:8080`；其余 → 静态 + `try_files {path} /index.html`。不代理 5432/5173/任何其他端口。
- 上传上限：沿用文档结论 `request_body max_size 25MB`（后端预览上限 20 MiB 留余量）。
- 网络边界：活动网络 WLAN "CMCC-4cXx"，**Private**，IPv4 `192.168.1.10/24`（DHCP），默认网关 `192.168.1.1`；iNode VPN `172.18.104.26/21` 不承载默认路由，排除。

## 4. 正式前端构建（已完成）

- 命令：`frontend/` 下 `pnpm.cmd install --frozen-lockfile`（Already up to date）→ `pnpm.cmd run build`（`vue-tsc -b && vite build`），构建前清空旧 `dist`。
- 结果：退出码 0；产物 `frontend/dist` 共 **6 个文件、262,899 字节**（index.html 457 B、assets/index-AhF4J6Qd.css 26,671 B、assets/index-CDWR5hqb.js 212,044 B、favicon.svg、icons.svg、templates/pjsk-goods-import-template.xlsx）。
- `index.html` SHA-256：`F883B1FB11F7AC2C59D1A69E728C9553D44EE7AC98F63BAC2CE8F4FA6B6734D4`；构建时间 2026-07-16 12:4x。
- 扫描：产物中无 `localhost:5173`/`127.0.0.1:5173`、无 DSN 前缀、无数据库变量特征。`frontend/dist`、`node_modules` 均被 gitignore；构建后 `git status` 仅本日志文件，`git diff --check` 通过；未修改任何已跟踪文件。

## 5. 前端发布到仓库外（已完成）

- 目标 `D:\PJSK-Deploy\frontend`（此前不存在，无需备份现役版本；后续更新须先备份到 `D:\PJSK-Archive\frontend\<时间戳>\`）。
- 流程：复制到临时目录 `frontend-staging-<ts>` → 核对文件数 6/6、总大小 262,899/262,899、`index.html` 哈希一致 → `Rename-Item` 近原子切换为正式目录。
- ACL（`/inheritance:r` 后全显式）：SYSTEM `(OI)(CI)F`、Administrators `(OI)(CI)F`、LOCAL SERVICE `(OI)(CI)RX`；目录级已核验。子文件继承核验因当前非管理员会话被新 ACL 挡住无法读取（预期），已列入安装脚本门禁（icacls index.html 须含 LOCAL SERVICE）。普通用户无写权限；未触碰父目录 ACL。

## 6. Caddy 获取与核验（已完成）

- 本机搜索：`where.exe caddy`、`D:\PJSK-Tools`、`D:\PJSK-Service` 均无既有 caddy.exe。
- 版本：官方 GitHub 最新稳定版 **v2.11.4**（2026-06-03 发布）。
- 下载（curl 经 127.0.0.1:7897 代理，官方 GitHub release）：
  - `caddy_2.11.4_windows_amd64.zip`：17,559,418 字节；SHA-512 与官方 `caddy_2.11.4_checksums.txt` **完全匹配**（`cd5ccfd8…88e35`）；SHA-256 `1708333F79E274C7697285AFE6D592AB39314E0B131E9EC6BEA08AD27DF62EBF`（本地留档）。
  - 存放：`D:\PJSK-Tools\Caddy\2.11.4\`（zip、checksums、extracted\）。
- `caddy.exe`：49,535,488 字节；SHA-256 `5CB9AB71E5756CE72840B8234177A2F40C8B4AB47A806B8E841E2B784E9DF62B`；`caddy version` 输出 `v2.11.4 h1:XKxkMTgNSizEvKG6QHue6cAsFOteU2qA61w2tKkCWi0=`；Authenticode 未签名（Caddy 官方发布即不签名，可信度以官方 checksum 匹配为准）。
- 复制为 `D:\PJSK-Service\caddy\caddy.exe`，复制后哈希复核一致。

## 7. 正式 Caddyfile（已完成，仓库外）

- 路径：`D:\PJSK-Service\caddy\Caddyfile`；`caddy validate` 通过 → `caddy fmt --overwrite` → 再次 validate 通过；格式化后 **1,225 字节，SHA-256 `A6B0B6C99680A0AA74180545D7EA4B8A10AE0293AD8119D74EAC604B2B976E37`**。
- 要点：全局 `admin off`（不开 2019 管理端口）+ `auto_https off`；站点 `:8081`；`@backend path /api/* /health` → `reverse_proxy 127.0.0.1:8080`（`X-Forwarded-For {remote_host}` 覆盖式、`X-Forwarded-Proto`、`Host`），响应 `Cache-Control: no-store`；其余路径 `root * D:\PJSK-Deploy\frontend` + `try_files {path} /index.html` + `file_server`（无目录浏览）；`/assets/*` 长缓存 immutable，非 assets `no-cache`；`request_body max_size 25MB`；安全头 nosniff/DENY/Referrer-Policy、去 Server 头；访问日志 `D:\PJSK-Runtime\logs\caddy\access.log`（10MiB × 8 滚动；Caddy 默认将 Authorization/Cookie/Set-Cookie 记为 REDACTED）。
- 无任何密钥/IP 以外的敏感值；真实 Caddyfile 不进 Git。仓库新增脱敏模板 `deploy/windows-service/Caddyfile.http-lan.example`（占位端口/路径）。
- 未采用严格 CSP（未经实际验证，遵守边界）；无 WebSocket 需求。

## 8. Caddy 目录与 ACL（已完成）

- `D:\PJSK-Service\caddy`：SYSTEM `(OI)(CI)F`、Administrators `(OI)(CI)F`、LOCAL SERVICE `(OI)(CI)RX`（LocalService 不可写二进制与配置）。
- `D:\PJSK-Runtime\logs\caddy`：SYSTEM `(OI)(CI)F`、Administrators `(OI)(CI)F`、LOCAL SERVICE `(OI)(CI)M`（滚动需要）。均 `/inheritance:r` 后核验。
- 说明：validate 曾生成 0 字节 `access.log`（继承新 ACL，LocalService 可写）。

## 9. 管理员执行脚本（已生成并通过 PSParser 语法检查，0 错误）

位于 `D:\PJSK-Tools\Scripts\`（仓库外，不进 Git），按序执行，均含门禁/错误退出/回滚/只读复核/不输出秘密：

1. `Install-PjskCaddyService.ps1` — 门禁（提权、服务不存在、caddy.exe 与 Caddyfile 哈希匹配、validate、前端与日志 ACL、后端 health 200、8081 空闲、无游离 caddy 进程）→ NSSM 安装配置（`pjsk-caddy` / `PJSK Goods Manager Web Gateway` / LocalService / SERVICE_AUTO_START / DependOnService=pjsk-backend / AppParameters=`run --config … --adapter caddyfile` / AppDirectory / stdout+stderr=caddy-out/err.log / 滚动 1-1-86400-10485760 / AppExit Restart+5000+10000）→ 保持 Stopped 并复核（sc qc + nssm dump）；失败自动 `nssm remove`。
2. `Add-PjskCaddyFirewallRule.ps1` — 门禁（默认路由接口必须 Private、同名规则不存在）→ 创建 `PJSK Caddy HTTP LAN`（Inbound/Allow/TCP 8081/Profile=Private/RemoteAddress=LocalSubnet/Program=caddy.exe）→ 读回逐项核验，失败即删规则。
3. `Start-PjskCaddyAcceptance.ps1` — 启动并验收：进程/端口归属、后端不受影响、`/`、`localhost`、`/health`、抽样 JS 资源、SPA 回退（/admin/orders、/query）、`/api/config` 200 JSON、未认证 `/api/admin/orders` 401 非 HTML、无目录列表、无 Vite/Caddy 欢迎页特征、本机经 `192.168.1.10:8081` 访问。
4. `Test-PjskCaddyReliability.ps1` — 受控停止（含后端不受影响核验）→ 重启 → 精确识别 NSSM 子进程并单杀一次验证自动恢复（识别不唯一则跳过并说明）→ 终态复核（8080 仅回环、5173 无监听、5432 只读记录）。
5. `Remove-PjskCaddyRollback.ps1` — 仅回滚本轮资源（服务 + 防火墙规则），保留下载物/哈希/日志/前端目录，不触碰后端。

## 10. 状态快照（脚本交付时的历史中间状态；后续结果见 §11–§16，最终状态以 §15–§16 为准）

> 以下是脚本交付时的历史中间状态记录，其中的"待执行/待人工"事项此后均已推进，不代表当前待办。

- 当时已完成：§1–§9；`pjsk-backend` 全程 Running、health 200；Git 工作区仅含本日志与 `deploy/windows-service/`（新模板 + README 行）。
- 当时待执行（管理员）：脚本 1→2→3→4——后续已全部执行并通过，见 §11–§12。
- 当时待人工事项中：跨设备访问已在 §16 通过 Windows 移动热点完成验证；真实整机重启自启验收已在 §17 通过；内网域名/DNS、HTTPS 与内部 CA、Caddy 升级回滚演练、前端业务人工验收至今仍为待办（见 §15）。

## 11. 安装 / 防火墙 / 验收执行结果（管理员脚本，已通过）

- `Install-PjskCaddyService.ps1`：INSTALL-OK——`pjsk-caddy`（PJSK Goods Manager Web Gateway）安装并保持 Stopped，LocalService、SERVICE_AUTO_START、依赖 `pjsk-backend`，NSSM 参数与 §9 一致。
- `Add-PjskCaddyFirewallRule.ps1`：FIREWALL-OK——`PJSK Caddy HTTP LAN`：Inbound / Allow / TCP 8081 / Profile=Private / RemoteAddress=LocalSubnet / Program=`D:\PJSK-Service\caddy\caddy.exe`，读回逐项核验一致。
- `Start-PjskCaddyAcceptance.ps1`：ACCEPTANCE-OK——全部检查通过：服务 Running、恰 1 个 caddy 进程且拥有 8081、后端不受影响（Running、8080 仅回环）、`/` 与 `localhost` 200（产物 SPA index，非 Vite/非 Caddy 欢迎页）、经代理 `/health` 200 且 database=connected、抽样 JS 资源 200、SPA 回退（/admin/orders、/query）200、`/api/config` 200 JSON、未认证 `/api/admin/orders` 401 非 HTML、无目录列表、本机经 `http://192.168.1.10:8081/` 与 `/health` 均 200。**该脚本执行当时跨设备验收尚未完成；后续已通过 Windows 移动热点完成人工验证，见 §16。**

## 12. 第一次可靠性测试：终态检查假失败与独立复核（重要更正）

- `Test-PjskCaddyReliability.ps1`（v1）实测：受控停止通过；重新启动通过；强杀旧 caddy 子进程 PID 20208 后 NSSM 自动拉起新 PID 14536，包装器 PID 10736 保持不变，恢复后首页 200、代理 health 200，后端不受影响——**但最终状态段报出 `FAIL: pjsk-caddy Running`**。
- 管理员独立只读复核（事后立即执行）证明该 FAIL 为**假失败**：`pjsk-caddy` Running/Auto（Win32_Service State=Running，ProcessId=10736，StartName=NT Authority\LocalService）；caddy.exe PID 14536（`D:\PJSK-Service\caddy\caddy.exe`）；8081 监听且 OwningProcess=14536；`http://127.0.0.1:8081/health` HTTP 200；`pjsk-backend` Running。
- 根因定性：v1 终态段在杀进程恢复后的短暂状态收敛窗口内做单次瞬时判定（且依赖当时取得的状态快照），未做轮询重查。**不得记录为部署失败。**
- 修复（v2，已通过 PSParser 0 错误）：终态段改为最多 30 秒轮询，每次迭代全部重新执行 `Get-Service`、`Get-CimInstance Win32_Service`、`Get-Process caddy`、`Get-NetTCPConnection -LocalPort 8081`、首页/代理 health/后端 health HTTP 探测，七项同时满足才通过；不复用任何旧 ServiceController 对象；失败时输出最后一条不满足原因。
- v2 正式重跑结论（管理员执行，**正式验收以本次 RELIABILITY-OK 为准**）：
  - 受控停止通过：pjsk-caddy Stopped、caddy 进程 0、8081 释放、`pjsk-backend` 保持 Running、后端直连 health 200。
  - 重新启动通过：Running、首页 200、代理 health 200。
  - 子进程异常恢复通过：NSSM 包装器 PID 17220；强杀 caddy PID 21784 后自动拉起新 PID 10572；包装器 PID 保持 17220 不变。
  - v2 终态轮询（≤30 秒全新查询）正式收敛：SCM 与 Win32_Service 均 Running、caddy 进程恰 1 个、8081 OwningProcess=10572、首页/代理 health/后端直连 health 均 200、8080 仍仅回环、5173 无监听。
  - 最终输出：`RELIABILITY-OK: all checks passed.`
- 结论定性：v1 终态 FAIL 为瞬时状态判定造成的假失败；独立只读复核已证明服务当时实际正常；v2 改为全新查询 + 30 秒轮询后正式重跑通过。

## 13. PostgreSQL 5432 监听现状（只读记录，独立遗留风险）

- 管理员只读观察：PostgreSQL 监听 `0.0.0.0:5432` 与 `[::]:5432`（全地址）。这是**本轮之前已存在**的状态，与 Caddy 部署无关；本轮未开放 5432 防火墙、未代理 5432、未修改 PostgreSQL 任何配置。
- 登记为独立安全风险：建议后续单独评估将 `listen_addresses` 收敛为 `127.0.0.1`（需 PostgreSQL 重启窗口与授权），本轮不动。

## 14. Caddyfile 精简（去冗余 header_up，已完成）

- 依据：Caddy 会自行设置 `X-Forwarded-For` / `X-Forwarded-Proto`，且默认忽略不受信客户端自带的 X-Forwarded-* 值；显式 `header_up` 两行冗余。删除后行为不变（后端 `TRUSTED_PROXY_CIDRS` 回环白名单不变）。
- 脱敏模板 `deploy/windows-service/Caddyfile.http-lan.example` 已同步删除并更新注释。
- 正式 Caddyfile 由 `Update-PjskCaddyfile.ps1`（管理员）执行：备份 → 写入新内容 → `caddy fmt` → `caddy validate` → 受控重启（admin API 已关闭，无法热 reload）→ 首页/代理 health/api/config/后端 health 复核 → 记录新哈希；任一步失败自动还原备份并回滚重启。
- 执行结果（`UPDATE-OK`）：
  - 已删除 `header_up X-Forwarded-For` 与 `header_up X-Forwarded-Proto` 两行；代理、静态、SPA 回退、缓存、安全头、日志与上传限制逻辑均保持不变。
  - 备份：`D:\PJSK-Service\caddy\Caddyfile.bak-20260716-134236`。
  - 新 Caddyfile validate 通过；受控重启 `pjsk-caddy` 成功。
  - 复核：`/api/config` 经 Caddy HTTP 200；后端直连 health HTTP 200；`pjsk-backend` Running。
  - 新 Caddyfile：**1,278 字节，SHA-256 `CF5485980292195ECDED184F5958B3BFDEB70417312F17CB26D47E9C2DA00720`**。

## 15. 最终状态汇总

- 局域网入口 `http://192.168.1.10:8081/`；本机回环 `http://127.0.0.1:8081/`；代理健康 `http://192.168.1.10:8081/health`。跨设备访问已通过 Windows 移动热点完成人工验证（见 §16；原 CMCC-4cXx 网络因疑似客户端隔离无法设备互访）。
- 端口 80 仍由 IIS/W3SVC 持有，本轮未停止、未修改。
- 服务：`pjsk-backend` Running/Automatic（未受本轮影响）；`pjsk-caddy` Running/Automatic，LocalService，依赖 `pjsk-backend`，Application `D:\PJSK-Service\caddy\caddy.exe`，配置 `D:\PJSK-Service\caddy\Caddyfile`，前端 `D:\PJSK-Deploy\frontend`，日志 `D:\PJSK-Runtime\logs\caddy`，监听 8081。
- 防火墙：`PJSK Caddy HTTP LAN`（Inbound/Allow/TCP 8081/Private/LocalSubnet/限定 caddy.exe）。
- 尚未完成（不得声称已通过）：内网域名与 DNS、HTTPS、内部 CA/证书信任、Caddy 升级与回滚演练、全部业务页面人工验收、PostgreSQL 5432 全地址监听收敛（独立处理，见 §13）、原 CMCC-4cXx 网络设备隔离问题（仅在需要该网络多设备互访时处理）。（跨设备访问已于 §16 验证通过；真实整机重启自启验收已于 §17 通过。）
- 全程未使用子代理；未修改后端/数据库/PostgreSQL/`.env`；未强制推送。

## 16. 跨设备人工验收（已完成，2026-07-16 下午）

**结论：跨设备访问已通过 Windows 移动热点完成验证；原 CMCC-4cXx 网络因疑似客户端隔离无法设备互访。**

- 原 CMCC-4cXx Wi-Fi 下的实测（失败，网络侧限制）：
  - 电脑 IP `192.168.1.10`，手机 IP `192.168.1.11`；
  - 手机访问 `http://192.168.1.10:8081/` 返回 `ERR_ADDRESS_UNREACHABLE`；
  - 电脑 `ping 192.168.1.11` 返回"无法访问目标主机"；
  - 判定为该 Wi-Fi 存在客户端/AP 隔离或设备互访限制（双向皆不可达，非 Caddy/防火墙问题）；
  - 未修改路由器、Wi-Fi、Caddy、防火墙或任何服务。
- 替代验证（成功）：
  - Windows 电脑开启移动热点，手机关闭 VPN 与移动数据后接入该热点；
  - 手机成功打开 Caddy 局域网入口：首页正常、`/health` 正常，前后端链路正常。
- 因此网关的跨设备访问能力已实际验证通过；若需在 CMCC-4cXx 下多设备使用，需由用户自行评估调整路由器隔离设置（本轮不涉及）。

## 17. 真实整机重启与自动启动验收（已通过，2026-07-16）

**结论：三个服务全部通过真实整机重启后的自动启动验收；全程未人工启动任何程序。**

- 验收条件：执行 Windows 真实整机重启；登录桌面后仅等待服务自行启动；未手动运行 `pnpm dev`、`run.cmd`、`pjsk-backend.exe`、`caddy.exe`、`nssm.exe`，未手动启动或重启任何服务。
- 重启后服务状态（`Get-Service` 只读）：`postgresql-x64-18` Running/Automatic、`pjsk-backend` Running/Automatic、`pjsk-caddy` Running/Automatic。
- 重启后监听（只读观察；**PID 仅为本次验收证据，重启或服务恢复后会变化，不是固定配置**）：
  - PostgreSQL：`0.0.0.0:5432` 与 `[::]:5432`（本次证据 PID 9628）——仍为全地址监听，属已登记的独立遗留风险（§13），本次验收未处理、未修改；
  - 后端：`127.0.0.1:8080`（本次证据 PID 10944）——继续仅监听回环；
  - Caddy：`[::]:8081`（本次证据 PID 16128）——双栈通配监听，属正常形态，实际可达性受防火墙（Private+LocalSubnet+限定 caddy.exe）约束；
  - 5173：无监听（Vite 开发服务器未运行）。
- HTTP 检查（重启后均 HTTP 200）：后端直连 `http://127.0.0.1:8080/health`、Caddy 首页 `http://127.0.0.1:8081/`、Caddy 代理 `http://127.0.0.1:8081/health`。
- 跨设备访问在真实整机重启后再次通过 Windows 移动热点验证（手机重新连接电脑热点后网页正常访问）。原 CMCC-4cXx 网络仍疑似存在客户端/AP 隔离，未通过、未处理（见 §16）。
- 至此"真实整机重启自启验收"从待办中移除；剩余待办见 §15。
