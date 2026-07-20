# 阶段 2G 正式维护窗口执行与切流中止记录（2026-07-20）

> 本日志承接 `docs/development-logs/2026-07-19-tencent-cloud-production-cutover.md`（该文件阶段 2G-1B-Final-Review 起若干段落存在历史编码损坏，本轮不修改该文件，仅在此交叉引用）。
> 安全边界：本文不记录任何密码、密钥、HMAC、DSN 或凭据正文；仅记录公开哈希、路径、大小与时间。

## 提交补记（阶段 2G-1B 收尾）

- 2G-1B 批准的提交已完成并推送：`98f8fe1e7eb6336e842c1e9e336e0bbf0698a3ce`（`feat(db): add atomic PostgreSQL snapshot coordinator`）。
- 本窗口开始与结束时，本地 `HEAD` 与 `origin/main` 均为该提交，工作树干净。

## 一、维护窗口执行摘要（2026-07-20，北京时间）

### 1. 开窗复核（约 08:30–08:37）

- 本地 Git 干净，`HEAD` = `origin/main` = `98f8fe1e7eb6336e842c1e9e336e0bbf0698a3ce`。
- 本地库 `19 / 0019`，`0020/0021` 均 0 条；`pg_prepared_xacts` 为 0。
- 云端 `current.next` → `/opt/pjsk/releases/98f8fe1e7eb6`，release 的 `REVISION` 与完整提交 SHA 逐字一致（注：`REVISION` 与 `MANIFEST.sha256` 为 CRLF 行尾，校验时需剥离 `\r`）；旧 release `14d339e56677` 保留；`current`、正式 `backend.env`、正式 unit 均不存在。
- 云端 PG16:5432 / PG18:5433 均 online，Caddy active，根盘余 51 GB；本地 D: 余 46.8 GB。

### 2. 停写（管理员执行，约 08:35）

- 依序停止 `pjsk-caddy`、`pjsk-backend`；`postgresql-x64-18` 保持 Running；StartType 均保持 Automatic 未改。
- 复核：本地 8080 无监听；`pg_stat_activity` 中 `pjsk` 库 0 连接、无写事务。

### 3. final dump（08:38:13）

- 文件：`D:\PJSK-Archive\migration\pjsk-final-98f8fe1e7eb6-20260720-083813.dump`。
- 121,804 bytes；SHA-256 `52DEA9125E2B72E8BD77B153F993876438F2EDB1C826D9555B26644C133335CA`；PG18 `pg_restore --list` TOC 174 项；导出前后本地库均为 `19 / 0019`。
- 本地基线（用于云端对比）：users=45、orders=44、order_items=120、payments=7、payment_items=19、products=69、import_batches=1、admins=2、admin_sessions=10、admin_auth_audit_events=220、query_sessions=3、projects=1，其余表 0；orders_total=6318.44、order_items_total=6318.44、payment_items_applied=1145.40。

### 4. 上传与云端签收（08:39 起）

- 中转目录 `/home/ubuntu/.pjsk-upload-20260720T003925Z`（0700）；云端 `sha256sum -c` 返回 OK，哈希逐字一致。
- postgres 身份签收至 `/var/lib/pjsk/backups/final-20260720/`（postgres:postgres 0600），签收后再次 SHA 校验与 TOC=174 复核通过。

### 5. 正式角色/库创建与恢复（08:41–08:42）

- 创建前断言 PG18:5433 中 `pjsk_app` 与 `pjsk` 均不存在。
- `pjsk_app`：LOGIN-only（superuser/createdb/createrole/replication 均 false），SCRAM-SHA-256 存储；密码服务器本地随机生成，未输出、未进入命令行，临时 SQL/pgpass 均 0600 并即时删除。
- `pjsk` 库：owner `pjsk_app`、UTF8、template0；恢复前 public 0 表、无 pgcrypto、无 schema_migrations。
- `pg_restore`（PG18.4，`127.0.0.1:5433`，`--no-owner --no-privileges --role=pjsk_app --exit-on-error --single-transaction`）：00:42:45 UTC 开始，耗时 181ms，退出 0。
- 中途插曲（无副作用）：首个执行脚本因两处自写断言缺陷中止（TOC 统计正则把 `;` 注释行计入得 189；角色布尔拼接文本为 `true|false` 与预期短格式 `t|f` 不符）。均为脚本断言问题，服务器状态正确，修正断言后从校验点续跑。

### 6. 恢复后只读核验（全部通过）

- 迁移严格 `19 / 0019`，19 条列表与仓库逐行一致；`0020/0021` 记录与 `payment_qr_codes`/`payment_submissions` 表均不存在。
- 全部 22 项表行数、三项金额聚合与本地基线完全一致；库/表/序列 owner 均为 `pjsk_app`；pgcrypto extension owner 为 `pjsk_app`，37 个 postgres 属函数全部落在 `pg_depend deptype='e'` 批准例外内。

### 7. backend.env（08:47）

- `/etc/pjsk/backend.env`：原子安装，root:pjsk 0640（既有方案批准值：pjsk 可读不可写）。
- 15 个变量与代码白名单精确匹配；`DATABASE_*` 指向 `127.0.0.1:5433`/`pjsk_app`/`pjsk`；监听 `127.0.0.1:8080`；`RECOVERY_EMAIL_SENDER_MODE=disabled`（基线加密恢复邮箱数为 0，按方案不配置 AES/HMAC 迁移密钥）；`QUERY_CODE_RECOVERY_HMAC_KEY` 服务器本地独立生成，解码 32 字节。
- 验收含 pjsk 可读/不可写、密码与角色一致性（哈希比对）、`pjsk_app` TCP 实连 `select 1`；全程零回显；root 临时密码文件验收后销毁。

### 8. unit、current 切换与迁移（08:49）

- unit 以 release 绝对路径副本通过 `systemd-analyze verify`，最终文件仅差两处 `current` 路径，安装后 daemon-reload、未启动。
- release 门禁（REVISION/MANIFEST/可执行位/current.next 指向）通过后，`current` 原子指向 `/opt/pjsk/releases/98f8fe1e7eb6`；旧 release 保留。
- `systemctl enable --now pjsk-backend`：日志按序输出 `0020_payment_qr_codes.sql`、`0021_payment_submissions.sql` 后 `backend listening on 127.0.0.1:8080`。
- 数据库变为 `21 / 0021`，`0020`、`0021` 各恰好 1 条；新两表存在、0 行、owner `pjsk_app`；全部聚合与行数不变。

### 9. 云端本机验收与 Caddy 验证（08:50–08:54）

- `127.0.0.1:8080/health` HTTP 200（database=connected）；`/api/config` 200 且五模块 ready；未认证管理端点正确 401；`current/frontend` 静态文件齐备。
- Caddy：原默认 Caddyfile 备份至 `/root/caddy-backup-20260720T005220Z/`（哈希核对一致）；新配置为**仅匹配 `127.0.0.1` Host 的生产同构站点**（`/api/*`+`/health` 反代、SPA fallback、安全头、25MB 上限、JSON access log），`caddy validate` 通过后 reload。
- 经 Caddy 验收：`/health`、`/api/config`、首页、SPA fallback 均 200；以公网 IP 作 Host 的请求不命中站点（空响应）。
- 权限小修：caddy 用户不在 adm 组无法穿越 `root:adm 0750` 的 `/var/log/pjsk`，access.log 写入被拒；将该目录改为 0751（仅加穿越位）后日志正常落盘。已知偏差：access.log 实际 0644（配置 `mode 0640` 在 Caddy 2.6.2 未生效），留待后续阶段处理。
- 终态：backend NRestarts=0、日志零 error、failed units=0；外部实测公网仅 80 可达，8080/5433 不可达；UFW 仍仅 22/80/443。

## 二、切流中止与本地恢复

1. **本次正式迁移在切流前中止：正式域名、DNS 与 HTTPS 尚未就绪**（既有方案 H/I 节明确：无域名只能完成本机验收；`ADMIN_COOKIE_SECURE=true` 下明文 HTTP 不能作为正式入口）。停止点即"云端本机验收与 Caddy 验证完成、未切公网"。
2. 人工决定后执行本地恢复：管理员依序启动 `pjsk-backend`（health 200 确认）→ `pjsk-caddy`；`postgresql-x64-18` 全程未停。**本地 pjsk-backend、pjsk-caddy、PostgreSQL 现均已恢复运行。**
3. **本地正式访问入口为 `http://127.0.0.1:8081/`**（及局域网 8081）。
4. **端口无冲突**：IIS/W3SVC 占用 80（未动），Caddy 监听 8081，后端监听 `127.0.0.1:8080`，三者互不冲突。
5. 恢复后只读复核：**本地库仍为 `19 / 0019`，`0020/0021` 不存在**。本地服务后端为 2026-07-16 构建（迁移 `go:embed` 嵌入，仅含至 `0019`），重启不会触发新迁移。
6. **本地 8081 当前提供 2026-07-16 旧前端快照**（`D:\PJSK-Deploy\frontend`，index.html SHA-256 `B1B9F1C1F49B90CE2DC98451D06939BC93CA7DF7D69CC15D5A166EF3D55590EE`，与 HTTP 实际返回字节一致），显示"谷子管理工作台"与 foundation 页面**属于预期，不是代码丢失**。
7. **最新版源码与构建产物完好**：新版文案（用户中心/谷子管理中心/返回系统主页）存在于 `frontend/src/App.vue`；`frontend/dist`（2026-07-19 17:22 构建，index.html SHA-256 `962C9AEA32A1B6506749B9A41DF68A0103EA497DAC94BF6869E36CDB6A9E89CC`）与云端 release 同源；`HEAD` 与 `origin/main` 均为 `98f8fe1e7eb6336e842c1e9e336e0bbf0698a3ce`。
8. **人工决定采用选项 A，不采用仅替换前端的方案**：新版前端依赖 `0020/0021` 对应的新后端 API，单独替换静态文件会造成付款二维码、凭证提交等功能残缺；而本地重建后端会因嵌入迁移在启动时破坏 `19/0019` 基线，同样禁止。本地保持旧前端、旧后端、`19/0019` 数据库不变。

## 三、云端待命状态与下次迁移约束

9. **云端保持待命**：`current` → `/opt/pjsk/releases/98f8fe1e7eb6`，数据库 `21 / 0021`，后端 active 且仅监听回环 `127.0.0.1:8080`，Caddy 仅 `127.0.0.1` 作用域站点、**未配置正式公网站点**；公网无法访问业务。
10. **2026-07-20 final dump 降级为历史迁移快照**，不得用于下一次直接切流。
11. **本地重新开放使用后，云端数据库即视为过期副本**。下次迁移必须重新执行：停写 → 重新生成 final dump → 重新校验 → 重建或清空后恢复云端正式库（云端库已含 `0020/0021` 结构，不能直接在其上恢复仅到 `0019` 的新 dump）。
12. **所有本地与云端回滚材料继续保留，不得删除**：本地现役库、final dump 及 `.sha256`、`D:\PJSK-Deploy\frontend` 旧前端、旧后端 exe；云端旧 release `14d339e56677`、原 Caddyfile 备份、签收 dump 副本、中转目录执行脚本。

## 四、本轮未执行事项

- 未切换公网流量，未配置域名/DNS/HTTPS，未修改 UFW 或腾讯云安全组。
- 未修改本地程序、服务配置、Caddyfile、部署目录或数据库内容；本地库保持 `19 / 0019`。
- 未删除任何本地或云端回滚材料。
- 本日志为本轮唯一仓库改动；未提交、未推送，等待人工确认。
