# 2026-07-21 苏归与管理员分级权限系统(R1 后端 / R2 前端 / R3 发布计划)

状态:**代码完成,已本地全量验证,准备制作 release 并部署到云端生产**。本文不含任何密码、密钥、恢复码、Cookie、Token、临时密码或连接串。

方案依据:`docs/admin-role-hierarchy-plan.md`(owner 已确认 13 项产品决策)。目标:技术角色保持 `owner`/`admin`,页面统一显示 owner→苏归、admin→管理员、普通客户→用户;苏归(唯一 owner)可从用户列表任命管理员、启停、撤销(软撤销)、重置临时密码;管理员权限高于用户、低于苏归;owner 转移仍只走 SSH CLI,不提供任何网页升级/转移入口。

## R1 后端(迁移 + 存储 + 接口 + 测试)

- 迁移 `0023_admin_management.sql`:全增量、逐条幂等——`admins.user_id`(可空唯一 FK users,删用户置空)、`admins.must_change_password`、软撤销 `revoked_at`/`revoked_by`、status 词表增 `revoked`、审计表增 `actor_admin_id`/`management_reason` 及索引、事件/原因词表扩展。零数据改写。
- 存储层 `repository_management.go`:列表/任命(含复聘复活原账号)/启停/软撤销/重置,每个变更单事务内完成"锁行拒绝 owner 目标 + 清目标会话 + 写审计";`must_change_password` 由改密清除。
- 接口 `management.go` + 路由:`/api/admin/owner/admins`(GET/POST)、`…/{id}`(GET)、`…/{id}/enable|disable|revoke|reset-password`(POST)、`…/{id}/audit`(GET);全部 `RequireAuthentication + RequireOwner`,写操作叠加 10 分钟 reauth;临时密码 `crypto/rand` 生成、响应中仅出现一次、bcrypt 入库、永不入日志。首登强制改密门禁在中间件层(除 me/logout/reauth/改密外一律 403 `password_change_required`)。
- 测试:管理生命周期集成测试、0023 幂等、路由守卫、全中间件链单元测试;0022 单 owner 约束回归通过;全新库 0001→0023 隔离迁移通过。

## R2 前端(角色显示 + 任命/管理页 + 首登改密)

- `roleDisplayName` 单一映射(client.ts):owner→苏归、admin→管理员、user→用户;全站禁裸角色技术值。
- 用户与账号页:苏归专属"管理员身份"列与"设为管理员/查看/重新任命"入口;苏归本人关联用户不显示危险操作。
- 苏归专属"管理员管理"页:列表/详情/启停/撤销/重置/相关审计;苏归本人显示"唯一苏归"、无危险按钮。
- 首登强制改密:全屏门禁,标志来自 `/api/admin/me`(刷新保持),改密成功后重取身份解除门禁;autocomplete 语义正确(current/new-password),临时密码只存内存 ref、关闭抹除、不入 storage/URL。
- 集中式 API 调用与错误映射(401/403/404/409/422/500/reauth_required/password_change_required),不回显后端堆栈。

## 门禁 A:本地旧生产自启处置

2026-07-21 08:28 Windows 重启后,Automatic 服务 `pjsk-backend`(连本地归档 `pjsk`,8080)与 `pjsk-caddy`(8081)被自动拉起。经管理员执行 `D:\PJSK-Archive\maintenance\disable-retired-local-services.ps1`,两服务已 **Stopped + StartType=Disabled**;`postgresql-x64-18` 保持 Running/Automatic 仅归档。冻结基线复核未变(19/0019、users 45、orders 44/6318.44、payments 7/1145.40/1145.84),无切换后新增写入。

## 门禁 B:R2 真实浏览器可视化验收

隔离 dev 栈(临时库 + 独立端口,不连冻结 pjsk / 云端)人工复验 10 项全通过。复验发现并修复两缺陷、补 8 项回归测试:
1. 任命/临时密码弹窗复用同层 overlay 遮挡 reauth → 分层 `.app-modal-overlay`(60)/`.reauth-overlay`(78)/`.first-pwd-overlay`(82),任命弹窗在 reauth 时隐藏、取消后带原用户名重现、成功后仅留临时密码弹窗;
2. 操作列把 `<td>` 设为 flex 致错位/行高不一 → 改行内 `.cell-actions`;附带修窄屏 `.admin-name-cell` 断行、非 owner 提前调 owner-only 接口。
验收后 dev 栈彻底拆除,无端口/进程/文件残留。

## 旧 release 兼容性回归测试(应用层回退保证)

`backend/internal/compat/oldrelease_compat_test.go`(默认 skip,需 `PJSK_RUN_DB_INTEGRATION_TESTS=1` + `PJSK_RUN_OLD_RELEASE_COMPAT_TEST=1`)。证明旧 release 提交 `95036a07911b`(内嵌迁移止于 0022)对已迁到 0023 的库保持回退兼容:用一次性隔离库(不连冻结 pjsk/云端)迁到 0023 并播种(owner/admin/user/0023 列/管理审计),从旧提交的临时 detached worktree 构建旧二进制并对该库运行——验证启动正常、`/health` 200、owner 登录 + `/api/admin/me`=owner、库仍 23/0023、0023 列与数据未被改动、0020–0023 各恰一条(未重跑/回退)。测试后删除临时库、旧二进制、worktree,无残留;旧二进制/临时密码/连接串/绝对路径不入仓库。

## R3 发布计划

先提交(`feat: add owner-managed administrator roles`,含 R1/R2 + 兼容性测试 + 本日志)→ 按新 commit SHA 制作 release(Linux x86-64 后端 + 前端 dist + 迁移 0001–0023 + REVISION + MANIFEST)→ 等人工确认后推送 → 云端只读门禁 → 上传签收新 release → 短维护窗口切换 current 并迁移 0023(终态 23/0023)→ 复用测试用户 `production_write_test_20260721` 走 P1–P6 受控验收(任命→临时密码登录→强制改密→停用→启用→重置→撤销→复聘)。不启用 HSTS、不改 Caddy、不开放真实客户;旧 release 与全部备份保留。

## R3-6 后续:P1 阻断与前端修复(身份显示 / 表格滚动 / reauth 体感)

release `4f2dda06b013` 切换、迁移 0023 完成后进入 R3-7 P1(苏归任命测试管理员),**被 owner 在生产浏览器阻断**,发现三处问题,P1 暂停、未产生任何管理员写入。均为前端问题,**不涉及后端/迁移**。

### 问题与准确根因
1. **顶部身份区显示"admin"而非"苏归"**。根因:身份区绑定 `admin.display_name ?? admin.username`;生产 owner 账号 `display_name = NULL`、`username = admin`、`role = owner`,故回落到登录用户名 "admin",`roleDisplayName` 从未接入身份区(此前只在管理页表格用到)。非缓存/Service Worker 问题(无 SW;新 bundle 本身即此绑定)。
2. **所有宽表横向滚动条只在表格底部**,行多时须滚到最底才能左右移动,右侧"管理员身份/详情"操作列难以触达。
3. **进入页面与 reauth 弹出偏慢**:实测后端处理亚毫秒~十几毫秒(reauth 80ms 为 bcrypt,设计如此)、静态 JS gzip 后约 89KB/0.34s;真正体感来源是 reauth 旧流程先发写请求等 403 才弹窗(1 个 WAN 往返)、用户页接口串行、静态资源缺 `Cache-Control immutable`。

### 修复(前端 only,未改后端/迁移)
- **身份与账号分离**:新增 computed `adminIdentityLabel = 身份：${roleDisplayName(role)}　账号：${username}`,两处 `PortalStatusBar :identity` 改绑之。角色身份只依赖 `roleDisplayName(role)`,与 `display_name` 无关 → owner 恒"苏归"、admin 恒"管理员",`display_name=NULL` 仍正确;不显示裸 owner;用户端 `CN：…` 不变。
- **统一双同步滚动条**:新增指令 `v-synced-scroll`(`src/directives/syncedScroll.ts`)——容器上方注入顶部轨道,顶/底 scrollLeft 用 compare-before-set 双向同步(不循环不抖动),`ResizeObserver` 跟随表格 `scrollWidth` 并在无溢出时隐藏顶部条,卸载时清理监听与注入节点。挂到全部 20 个 `.table-scroll`,窄表无溢出自动隐藏。
- **reauth 主动弹出 + 安全并行加载**:新增 `ensureReauth()`,写操作点击后在客户端 9 分钟新鲜窗口外**本地即时打开 reauth**(不等网络),取消则零写入并保留输入,成功记时间戳并执行原操作一次;保留 `execute()` 的服务端 403 兜底(校验强度不变)。用户页 `users` 与 `owner-admins` 由串行改 `Promise.all` 并行(身份已由 ensureAdmin 确认为 owner;普通管理员不请求 owner-only)。

### 验证
- 前端 **247/247 测试通过**(新增 15 条覆盖身份 display_name=NULL→苏归、admin→管理员、账号/角色分离、双滚动条双向同步、无溢出隐藏、Resize 同步、卸载清理、全宽表统一组件、reauth 本地即时/成功一次/取消不写入/403 兜底、并行加载、普通管理员不请求 owner-only);`vue-tsc`、`pnpm build`(无 source map、dist 泄漏扫描净)通过;后端回归(gofmt/vet/test/DB 集成/迁移隔离/旧-release 兼容性)全绿。
- **隔离 dev 栈(不连冻结 pjsk/云端)浏览器人工验收 10 项全通过**:owner(display_name=NULL)显示"身份：苏归 账号：admin";普通管理员显示"身份：管理员 账号：helper_demo"且看不到"管理员管理";顶部同步滚动条正常、拖动可达最右操作列、行列对齐;点击"确认任命"reauth 立即出现且不被遮挡;reauth 成功只任命一次、临时密码只显示一次(后端日志证 `reauth`→单次 `owner/admins` 无 403);管理中心卡片布局正常;无新增阻断视觉问题。验收后 dev 库/环境/日志/二进制全部删除。
- 安全:隔离 dev 截图中曾出现一次性临时密码,仅为**已销毁隔离环境**材料,**不得复用**;已确认其未进入源码、日志、Git、开发文档或构建产物。生产 P1 禁止截图或回传临时密码。

### 待办
- 本轮前端修复待人工复验后走新提交/新 release(**仅换前端 bundle,不迁移**,0023 已在库)/R3-5 签收/R3-6 切换,再回到 P1。
- **Caddy `/assets/*` 长期不可变缓存仍为后续独立阶段**(前端 release 部署验收后单独受控 reload 执行,不夹带在本次)。
- **生产 P1 尚未执行**,测试用户 `production_write_test_20260721` 目前仍无管理员身份;owner=1、testadmin disabled/admin 不变。
  - **【R3-7 事实更正,见下节】上一条"生产 P1 尚未执行"表述作废**:经生产只读核查,P1 实际已于 `2026-07-21 11:51:11 +08` 由 owner 浏览器操作成功提交。

## R3-7 前端修复 release 发布与收尾(release `62ffa2c28d64`)

R3-6 阻断发现的三处前端问题(身份显示 / 宽表滚动 / reauth 体感)已作为提交 `62ffa2c28d64…`(`fix: improve admin identity, table scrolling, and reauth flow`)修复,并按"仅换前端 bundle、不迁移"路径完成 R3-5 上传签收 → R3-6 切换 → R3-7 发布后验收。

### 正式上线版本
- release ID:`62ffa2c28d64`
- full revision:`62ffa2c28d647d15765905e78d5432408c00ad4f`
- backend SHA-256:`5052d70a0eb1fe0ea44c334171a4bc392980d67c9ae1810f1b6272c008d53ce7`(Linux x86-64 ELF;与上一 release `4f2dda06b013` 二进制差异仅 `vcs.revision`/`vcs.time`,`-trimpath`/`CGO_ENABLED=0`/`GOOS=linux`/`GOARCH=amd64` 全同,源码功能一致)
- transfer bundle SHA-256:`3d0958f5baa0356956d56174866111266cf06e4476260a561e737f4802b9f0f3`
- 前端 JS:`index-vCNHskDH.js`
- 前端 CSS:`index-s-C2Kh49.css`
- migrations:0001–0023,与 `4f2dda06b013` 逐字节一致,无 0024;库已 23/0023,本次发布**不产生新迁移**。

### 发布过程事实
- bundle 本地持久化路径:`D:\PJSK-Archive\transfer\pjsk-release-62ffa2c28d64.tar.gz`(顶层直接为 `bin/ frontend/ migrations/ REVISION MANIFEST.sha256`,无多余包裹层,32 文件,8,827,253 B)。
- 云端上传路径:`/home/ubuntu/pjsk-release-62ffa2c28d64.tar.gz`。
- staging:`/root/pjsk-upload-62ffa2c28d64-20260721T061808Z`(root:root 0700)。
- staging 中转签收:**32 文件、MANIFEST 30/30 OK**、后端 SHA 与 ELF x86-64 核验通过、无 source map、migrations 23 含 0023、泄漏扫描净。
- 安装到 `/opt/pjsk/releases/62ffa2c28d64`(镜像正式 release 权限:目录 755、`bin/pjsk-backend` 755、REVISION/MANIFEST/前端/迁移只读),目标目录独立复验 `RELEASE_INSTALL_PASS`。
- 切换采用同目录临时软链 + `mv -T` 原子替换,**未先删 current、未停后端、未 reload Caddy、未执行迁移**;`current` 与 `current.next` **最终均指向 `/opt/pjsk/releases/62ffa2c28d64`**。
- 发布后 R3-7 云端只读终态验收 `R3_7_POST_RELEASE_VERIFY_PASS`:current/current.next 均新 release;REVISION 全 SHA、files=32、MANIFEST 30/30、后端 SHA 匹配、前端 JS/CSS 由公网首页引用;PostgreSQL 18.4 / 5433 / `pjsk`,schema_migrations 23、max=`0023_admin_management.sql`、0023=1、prepared=0、idle-in-transaction=0;owner=1、testadmin=disabled/admin;pjsk-backend/caddy active、NRestarts=0;本地 `/health`、公网 `/health`、公网首页、`/assets/index-vCNHskDH.js`、`/assets/index-s-C2Kh49.css` 均 200。

### 事实更正:P1 实际已完成
- 经生产只读核查,P1 任命写入实际已于 **`2026-07-21 11:51:11 +08`** 由 owner 浏览器操作成功提交(在视觉阻断报告之前),属 owner 本人意外完成的合法写入,非漂移/外部写入。
- 测试用户:`production_write_test_20260721`;对应管理员用户名:**`123`**;actor:`admin`(唯一"苏归");`admin_appointed` **result=success**,审计恰 1 条,target=`123`;`123` 为 `admin`/`active`,`must_change_password=true`,`revoked_at`/`revoked_by` 为空;owner 仍恰 1,linked_admins=1。
- **此前"P1 未执行"表述作废。**
- 本次 R3-7 **不执行 P2、不重置密码、不创建第二个管理员**;账号 `123` 保持 `active` + `must_change_password=true`,不删除/不撤销/不重建/不改名;后续 P2 复用该账号,不新建第二个。

### 回滚策略
- 上一正式 release `4f2dda06b013` 保留为**直接回滚点**(纯符号链接翻回、无数据库改动、秒级);其余旧 release `95036a07911b`/`98f8fe1e7eb6`/`14d339e56677` 暂全部保留。
- 所有现有数据库 / Caddy / 迁移备份暂时保留;上传 bundle 与 staging 亦保留。
- **本轮不做任何清理**;后续清理必须另开阶段并重新审批。

### 人工验收
本轮为**人工验收通过**(非自动化覆盖全部页面):公网页面可正常访问且显示正常;新版身份显示("身份：苏归/管理员　账号：…")正常;宽表上下双同步横向滚动正常;主动 reauth 修复正常;页面整体可用。
