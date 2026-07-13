# 2026-07-13 管理员一次性绑定码 MVP

## 范围

- 允许尚未设置查询码的 `active` 用户,在管理员线下核实身份并生成一次性绑定码后,通过"CN + 绑定码"自行设置首个查询码。
- 未开发:邮箱绑定、手机号绑定、邮件/短信验证码、密保问题、忘记查询码找回、查看原查询码、订单信息校验找回、自动发送验证码。
- 未修改订单、付款、Excel 导入、CN 合并逻辑。
- 绑定码不能用于已设置查询码的用户,也不能用于忘记查询码重置。

## 迁移设计

新增 `backend/migrations/0015_query_code_bind_tokens.sql`(按现有编号顺延,未改历史迁移):

- 表 `user_query_code_bind_tokens`:`id`、`user_id→users`、`token_hash`、`expires_at`、`used_at`、`failed_attempts`(默认 0)、`created_by_admin_id→admins`、`created_at`、`invalidated_at`。
- 索引:`user_id`、`expires_at`、`created_by_admin_id`。
- "同一用户只有一个有效绑定码"由业务事务保证(生成前先在事务内把该用户全部未使用绑定码置 `invalidated_at`),不用唯一索引。
- 迁移不生成任何绑定码、不修改现有用户、不出现明文 token,由现有迁移系统按文件名管理(重启后端时已成功应用)。

## 绑定码生成与哈希(`backend/internal/querycode`,双端共用)

- `GenerateBindToken()`:`crypto/rand`,10 位,字母表 31 字符(大写字母+数字,**排除 0 O 1 I L**),约 49.5 bit 熵。未使用 math/rand、时间戳、用户 ID、CN、自增序号。
- `HashBindToken()`:SHA-256 hex。选择 SHA-256 而非 bcrypt 的理由:token 是高熵、短期(30 分钟)、有失败次数上限的随机值,不同于人选口令,SHA-256 已足够且验证开销低;数据库只存哈希。
- 明文只出现在管理员生成成功后的单次响应中;不写入服务日志、开发日志、错误日志、数据库、Git 测试文件、localStorage、sessionStorage。

## 管理员接口

`POST /api/admin/users/{id}/query-code-bind-token`(挂在既有 `Detail` 分发下,经 `RequireAuthentication` + 处理函数内二次 `CurrentAdmin` 校验):

- 事务内:锁用户行 → 校验存在、`active`、`query_code_hash` 为空 → 全部旧未使用绑定码置失效 → 生成新码 → 存哈希与过期时间(固定 30 分钟)→ 记录生成管理员。
- 响应:`{bind_token, expires_at, message:"绑定码仅显示一次，请安全交给用户。"}`;不返回 hash、旧码、查询码哈希或其他内部安全字段。
- 不满足条件时:404(用户不存在)、409(已有查询码 / 已停用或已合并)。
- 用户详情响应新增 `has_active_bind_token`、`bind_token_expires_at`(仅状态,无明文无哈希)。
- 管理员不能查看历史绑定码;重新生成即旧码全部失效。原有"设置/重置查询码""启停查询权限"能力不受影响。

## 普通用户绑定接口

`POST /api/query/bind-code`(匿名,强限流):

- 请求:`cn`、`bind_token`、`new_query_code`、`confirm_query_code`。
- 本地输入类错误给具体中文提示(缺项/两次不一致/格式不符,格式复用 `querycode.Validate`);**账户与 token 相关的一切失败(CN 不存在、已设置查询码、已停用/已合并、无有效 token、token 不匹配、过期、已用、已失效、token 不属于该 CN)统一返回 401「CN 或绑定码不正确」**,不泄露账户状态。
- 限流:复用 login 限流器,按 IP(20 次/分钟)+ IP+CN(10 分钟内失败 5 次封锁 10 分钟)双层;另有 token 自身维度 —— 单个绑定码累计错 5 次(`failed_attempts`)立即失效,失败计数在同一事务中提交(拒绝请求也持久化计数)。
- 哈希比对使用 `crypto/subtle.ConstantTimeCompare`。
- 成功时同一事务:更新 `users.query_code_hash`(bcrypt)/`query_code_updated_at`/`updated_at` → 标记该 token `used_at` → 该用户其他有效 token 全部失效 → 删除该用户全部旧 `query_sessions`(防异常遗留)。
- 成功响应仅 `{"message":"查询码设置成功，请使用新查询码登录。"}`,**不自动创建会话**,用户需用新查询码正常登录。

## 前端

- **管理员用户详情页**:`active` 且未设置查询码的用户在"查询权限"面板内显示"首次绑定码"区块与"生成一次性绑定码"按钮;点击有二次确认("生成新绑定码后,该用户以前未使用的绑定码将立即失效。是否继续?");生成成功后在独立提示框展示绑定码(等宽大字、"仅显示一次"警示、复制按钮、过期时间);token 只存于内存 ref,刷新即消失,不入 localStorage/sessionStorage;已有查询码或非 active 用户不显示按钮;面板另提示"已有未使用的绑定码(何时过期)"状态。
- **普通用户登录页**:新增"首次设置查询码"入口(不影响原 CN+查询码登录);绑定表单含 CN、一次性绑定码(密码型)、新查询码、确认新查询码、"设置查询码"与"返回登录"按钮;提交期间按钮禁用;成功后清空表单、返回登录页并提示使用新查询码登录;不自动登录;绑定码不保存,刷新后输入消失。

## 修改文件

- `backend/migrations/0015_query_code_bind_tokens.sql`(新增)
- `backend/internal/querycode/querycode.go`(新增 GenerateBindToken/HashBindToken)
- `backend/internal/users/bindtoken.go`(新增,管理员生成)
- `backend/internal/users/handler.go`(Store 接口、Detail 分发、DetailResponse 状态字段)
- `backend/internal/users/handler_test.go`(stub + 3 个单元测试)
- `backend/internal/users/bind_token_integration_test.go`(新增,2 个真库生命周期测试)
- `backend/internal/query/bindcode.go`(新增,用户绑定)
- `backend/internal/query/handler.go`(Store 接口)
- `backend/internal/query/handler_test.go`(stub + 2 个单元测试)
- `backend/internal/api/router.go`(注册 `/api/query/bind-code`)
- `backend/internal/api/payments_routes_test.go`(路由鉴权清单加绑定码生成接口)
- `frontend/src/api/client.ts`、`frontend/src/App.vue`、`frontend/src/style.css`

## 测试结果

- 单元测试:管理员生成(未登录 401、返回明文不返回 hash、404/409 映射);用户绑定(缺项/不一致/格式 400、统一 401 文案、限流 429)。
- 真库集成测试(前缀 `BIND_TOKEN_TEST_*`,自动清理):token 长度与字符表(无 0O1IL)、只存 SHA-256 哈希、记录生成管理员、过期约 30 分钟、详情状态字段、重新生成使旧码失效、错误 token 计数、失效/过期/已用/跨用户 token 均拒绝、连续 5 次错误后 token 失效且正确 token 也不再可用、成功绑定后 used_at 置位、其他 token 失效、新查询码可登录、已设置查询码用户不能再生成、disabled/merged 不能生成。
- 路由鉴权测试:`POST /api/admin/users/{id}/query-code-bind-token` 未登录 401。
- `go fmt`/`go build`/`go vet`/`go test ./...` 与 `pnpm run build`:全部通过(见下文汇总)。

## 安全说明

- 全程未记录任何真实绑定码或查询码明文;数据库、日志、开发日志、Git 文件中只存在哈希或占位测试值。
- 汇报与日志不包含数据库连接串或密码。

## 人工验收状态(如实记录)

- **API 级端到端验收已完成**(隔离测试用户 `TEST_BIND_TOKEN_UI_20260713`,管理员会话由用户提供的账号登录):管理员生成绑定码 200(响应仅 bind_token/expires_at/message 三字段,长度 10);错误绑定码与不存在的 CN 均返回统一 401「CN 或绑定码不正确」;正确绑定 200 并返回规范文案;同一绑定码复用 401;新查询码登录 200;已设置查询码后再生成 409。管理员用户详情只读核对:`has_active_bind_token` 状态字段正常,响应中无 token 明文或哈希。
- **浏览器 UI 交互验收未完成**:本会话的自动化浏览器交互工具(表单输入/导航/JS)在验收阶段因环境侧安全分类器持续不可用而无法操作,多次重试未恢复。UI 的类型正确性和构建已由 vue-tsc + vite 保证;交互表现(绑定码仅显示一次、刷新消失、复制按钮、登录页入口切换、控制台无报错)留待人工在浏览器中验收,或环境恢复后补验。
- **临时数据清理已完成**:删除 1 条绑定码记录与 1 个测试用户(删除前核对无订单/付款关联),只读回查 `TEST_` 前缀用户为 0;scratchpad 中的绑定码/会话临时文件已删除。未访问或修改任何真实业务数据,未记录任何真实绑定码或查询码明文。

## 浏览器 UI 人工验收补充（2026-07-14）

- 管理员用户详情页的“生成一次性绑定码”按钮工作正常，二次确认文案正常。
- 生成后正确显示 10 位绑定码，“仅显示一次”警告、复制按钮和过期时间均正常。
- 页面刷新后绑定码明文消失，仅保留有效绑定码状态，不泄露明文或哈希。
- 重新生成后旧绑定码立即失效。
- 普通用户登录页“首次设置查询码”入口正常；CN、绑定码、新查询码、确认查询码表单及密码型输入框均正常。
- 错误 CN、错误绑定码、失效绑定码和已使用绑定码统一提示“CN 或绑定码不正确”。
- 两次新查询码不一致和查询码格式非法时，页面提示正常。
- 设置成功后不自动登录并返回登录页；新查询码可以正常登录，已使用绑定码不能复用。
- 已设置查询码的用户不再显示首次绑定按钮。
- 桌面视口和约 375px 窄屏均无横向溢出，浏览器控制台无红色报错。
- 本轮临时测试用户、对应一次性绑定码和查询会话已清理；仓库外未发现本轮遗留的绑定码、查询码、Cookie 或会话临时文件。
- 清理前确认测试用户的订单、付款和 CN 合并关联均为 0；清理后回查用户、绑定码和查询会话数量均为 0。未修改任何真实业务数据。