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
