# 阶段 2H-2B：系统所有者账号与安全恢复机制（2026-07-20）

> 安全边界：本文不含任何密码、密码哈希、恢复码明文、HMAC/AES 密钥值、会话 token 或 DSN。
> 部署边界：本阶段全部改动仅进入仓库。未部署到本地旧生产（8081/8080/19-0019）与云端待命环境；本地 `pjsk` 数据库只读复核仍为 `19 / 0019_admin_auth_audit_events.sql`。

## 一、目标账号定位（"苏归"）

- 本地现役库只读调查（未输出敏感列）：`admins` 表共 2 条——`admin`（active，最后登录 2026-07-16 20:34）与 `testadmin`（disabled）。**不存在名为"苏归"的记录，两者 `display_name` 均为空。**
- "苏归"即日常主用账号 `admin`，稳定标识：用户名 `admin`（登录名规范化唯一索引）+ 主键 UUID `08aca962-9c62-4ec6-a5b1-8684ba612343`。
- 后续初始化通过 `pjsk-backend promote-owner --username admin` 完成：显示目标的 id/username/display_name/role/status 非敏感信息，要求人工逐字重输用户名确认；本阶段**未**在任何数据库执行角色升级。

## 二、迁移 0022（真实对象名）

文件：`backend/migrations/0022_admin_owner_security.sql`

| 类别 | 名称 |
|---|---|
| 约束 | `admins_role_check`（role ∈ admin/owner）；`admin_auth_audit_events_event_type_check` / `admin_auth_audit_events_reason_code_check`（drop 后扩展重建） |
| 索引 | `admins_single_owner_unique`（`(true) where role='owner'` 部分唯一索引，最多一个 owner 的硬保证）；`admin_recovery_email_codes_admin_purpose_index`；`admin_recovery_codes_admin_index` |
| 触发器 | `admins_protect_last_owner_trigger`（**DEFERRED 约束触发器** + 函数 `admins_protect_last_owner()`）：提交时校验"曾为活跃 owner 的行被删/停用/降级后必须仍存在活跃 owner"，因此单事务内"先降旧、再升新"的 owner 转移可提交，且不产生外部可见的 0/2 owner 状态 |
| 新表 | `admin_recovery_emails`（AES-GCM 密文 + HMAC 查找哈希，独立 AAD `pjsk:admin-recovery-email:v1`）；`admin_recovery_email_codes`（6 位码 HMAC hex，purpose bind/reset，`attempt_count` 上限 5）；`admin_recovery_codes`（**bytea 原始 32 字节 HMAC**，`octet_length=32` check，batch_id/used_at/revoked_at） |
| 列 | `admin_sessions.reauth_at` |

审计事件新增 12 个：`admin_reauth_succeeded/failed`、`admin_password_changed`、`admin_recovery_email_bound`、`admin_recovery_email_bind_failed`、`admin_recovery_codes_generated`、`admin_recovery_code_reset_succeeded/failed`、`admin_recovery_email_reset_succeeded/failed`、`owner_promoted`、`owner_cli_password_reset`；reason 新增 8 个（invalid_recovery_code、invalid_verification_code、verification_code_expired、recovery_email_not_configured、email_delivery_disabled、weak_password、not_owner、validation_failed）。Go 常量与 SQL 枚举有静态一致性测试锁定。

## 三、后端实现

- `internal/admin/`：新增 `security.go`（改密/恢复邮箱/审计摘要）、`reauth.go`（reauth + `RequireRecentReauth`/`RequireRecentReauthWhen`/`RequireOwner`/`MutatingSuffixMatch`/`MutatingMatch`）、`recoverycodes.go`（码生成/规范化/HMAC/owner 端点/未认证 code-reset）、`recovery.go`（未认证邮箱找回）、`repository_security.go`（`SecurityStore` + CLI 存储）；`handler.go` 扩展可选安全能力字段。
- 新包 `internal/admincli`：`promote-owner`（仅 owner 数为 0 时；事务内复核+审计）与 `reset-owner-password`（TTY 强制、`x/term.ReadPassword` 双输无回显、密码不进参数/标准输出/日志、事务内改密+撤销全部会话+审计 `owner_cli_password_reset`、失败零副作用）；两者启动前校验目标库已应用 0022，**因此对本地 19/0019 库拒绝执行**；`--env-file` 显式加载 `/etc/pjsk/backend.env`。`main.go` 有子命令即走 CLI，不启动 HTTP、不跑迁移。
- 配置：新增 `ADMIN_RECOVERY_CODE_HMAC_KEY`（base64≥32B）。production 缺失→拒绝启动；解码失败/过短→拒绝；与 `QUERY_CODE_RECOVERY_HMAC_KEY` 常量时间比较相同→拒绝（`crypto/subtle`）。开发环境可缺省（恢复码功能显式 503）。
- API：`POST /api/admin/reauth`；`POST /api/admin/security/password`；`GET /api/admin/security/recovery-email` + `/request`（fresh reauth）+ `/confirm`；`GET /api/admin/security/audit-summary`；`GET|POST /api/admin/owner/recovery-codes`（owner-only，POST 需 fresh reauth）；未认证单步 `POST /api/admin/recovery/code-reset|email-request|email-reset`。
- **恢复流程不签发会话**：code-reset/email-reset 是单步"验证凭据+设新密码"，成功仅返回 204，事务内删除该管理员全部 `admin_sessions`（reauth 状态随会话一并清除），用户必须用新密码重新登录。防枚举：未知用户/停用账号/错码统一同一 401 文案；均限速（IP+用户名桶，5 次失败封禁 10 分钟）并写审计。
- **高风险 reauth 门禁（10 分钟）**已接入：导入撤销（POST …/revert）、付款作废（POST …/void）、用户合并（/api/admin/users/merge）、收款二维码修改（/api/admin/payment-qr* 非读方法）、恢复码生成、恢复邮箱绑定请求。读操作不受影响；过期返回 403 `reauth_required`。
- 恢复码：每批 10 张，`ABCDE-FGHJK-MNPQR-STVWX` 形（30 字符无易混淆字母表、拒绝采样均匀、约 98 bit 熵）；域分离 HMAC（`pjsk:admin-recovery-code:v1:`）；重新生成整批作废（`revoked_at`），使用即焚（`used_at`），库中永远只有原始 32 字节摘要。

## 四、前端实现

- `api/client.ts`：全部请求助手经 `execute` 包装；403+`reauth_required` → 调起注册的重验证弹窗 → 成功后自动重试一次；新增 9 个安全 API 函数。
- `App.vue`：管理门户新增"账户安全"模块卡与 `/admin/security` 路由（改密、owner 恢复码区、恢复邮箱区、最近 20 条安全事件表）；全局重验证弹窗（隐藏 username + `current-password`）；登录页新增"使用恢复码重置"入口（`one-time-code` + 双 `new-password`）。
- 密码管理器语义：登录表单原有 `username`/`current-password` 保留；改密表单含隐藏 username（`.visually-hidden`）+ `current-password` + 双 `new-password`；恢复表单 `one-time-code`/`new-password`。
- SMTP disabled 表现：后端相关端点显式 503 `email recovery is not enabled`、绝不伪装发送；前端恢复邮箱区显示"邮箱恢复尚未启用：系统当前未配置邮件发送服务…"，不渲染绑定表单。邮箱地址在接口与界面只以掩码出现；日志只记 admin id。
- 恢复码 UI：仅生成响应展示一次，复制按钮 + "我已妥善保存"确认（提示关闭后不可再查看）；重新生成前确认旧批作废。

## 五、Passkey 第二阶段（本阶段仅记录扩展点）

- 明确留待 `pjskgoods.cloud` HTTPS 稳定上线后：WebAuthn 注册/断言端点、`admin_webauthn_credentials` 表、rpID=`pjskgoods.cloud`、Windows Hello/设备 PIN/安全密钥；密码保留为备用。现有扩展点：`ConfigureSecurity` 能力注入模式、审计枚举可再扩展、账户安全页可加"通行密钥"区块。本阶段未实现任何 WebAuthn 代码。

## 六、测试与门禁结果

- 后端：`go test ./...`（含 `PJSK_RUN_DB_INTEGRATION_TESTS=1` 全量集成）**525 通过 / 0 失败**；`go build`、`go vet`、`gofmt -l` 全清洁。
- 新增测试：`internal/admin/owner_security_test.go`（码格式/规范化/域分离、密码策略、reauth 成功/失败/限速、10 分钟新鲜期三态、owner 门禁、改密成功撤销其他会话与弱密码拒绝、恢复码生成批次哈希一致且仅存摘要、code-reset 成功不发 Cookie/错码统一文案+审计/未知用户同答/限速、SMTP disabled 显式 503、0022 枚举与对象名一致性）；`owner_integration_test.go`（真实库：0022 已应用、bootstrap 升级仅一次、直写 SQL 触发 `admins_single_owner_unique`、最后 owner 降级/停用/删除在提交时被拒、单事务转移可提交、恢复码生命周期含会话全撤销与审计计数、reauth 读写往返）；`internal/config/admin_recovery_code_test.go`（production 必填、编码/长度、防复用）。
- 迁移静态检查：`Invoke-MigrationFactsTests.ps1` **27/27**（识别 22 个迁移、最高 `0022_admin_owner_security.sql` 命名合法）。
- 前端：`pnpm test` **202 通过 / 0 失败**（新增 `tests/admin-owner-security.test.mjs` 9 项）；`pnpm run build`（vue-tsc + vite）通过。
- 敏感扫描：改动文件仅测试内合成占位密码（`correct-password` 等）命中，无真实秘密；`git diff --check` 通过。
- 本地 `pjsk` 库复核仍为 `19 / 0019`；集成测试仅使用 `pjsk_integration_test_*` 一次性数据库。

## 七、完成项与未完成项

完成：0022 迁移及全部约束/触发器；owner 角色与唯一性/最后 owner 保护；owner 初始化命令（未执行升级）；恢复邮箱数据结构与 disabled 状态；恢复码全生命周期；10 分钟 reauth 与高风险门禁接入；恢复成功全会话撤销；审计扩展；CLI 紧急重置；表单 autocomplete；账户安全页面；全部测试与门禁。

未完成 / 后续阶段：owner 实际升级（待下次部署窗口用 `promote-owner` 执行）；SMTP 启用与邮箱恢复实际开通（独立阶段，需另配 `RECOVERY_EMAIL_*` 密钥与 SMTP 凭据）；Passkey/WebAuthn（HTTPS 后）；owner 转移的 CLI 封装（数据库语义已支持单事务转移，暂无命令入口）；`backend.env` 增补 `ADMIN_RECOVERY_CODE_HMAC_KEY` 属下次部署窗口操作。

## 八、部署限制

- 本轮改动只进仓库，未提交（停在提交前等待人工审核）、未推送、未构建 release、未部署。
- 本地旧生产二进制（2026-07-16，嵌入迁移至 0019）与 `D:\PJSK-Deploy` 前端未动；云端 `current`、数据库、`backend.env`、Caddy、服务状态未动。
- 下次云端部署时 0020→0022 将由新后端按序自动执行；`backend.env` 必须新增 `ADMIN_RECOVERY_CODE_HMAC_KEY`（服务器本地生成，且不得与 `QUERY_CODE_RECOVERY_HMAC_KEY` 相同，后端会拒绝启动）。
