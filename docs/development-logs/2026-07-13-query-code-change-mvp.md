# 2026-07-13 普通用户已登录修改查询码 MVP

## 范围

- 本轮仅实现普通用户已登录后修改查询码 MVP。
- 未开发首次绑定查询码、忘记查询码、邮箱、手机号、验证码或密保问题。
- 未创建数据库迁移，沿用既有 `users.query_code_hash`、`users.query_code_updated_at` 和 `query_sessions`。
- 未修改订单、付款、Excel 导入、CN 合并等无关业务模块。

## 修改文件

- `backend/internal/query/handler.go`
- `backend/internal/query/handler_test.go`
- `backend/internal/querycode/querycode.go`
- `backend/internal/users/handler.go`
- `backend/internal/users/query_account_integration_test.go`
- `backend/internal/api/router.go`
- `frontend/src/api/client.ts`
- `frontend/src/App.vue`
- `frontend/src/style.css`

## 后端变化

- 新增 `POST /api/query/change-code`，只接受有效普通用户 `pjsk_query_session`。
- 修改查询码前校验旧查询码、新查询码、确认查询码和格式；格式规则抽到 `backend/internal/querycode`，供管理员设置/重置和普通用户修改复用。
- 旧查询码校验失败按 IP 与当前用户共同限流。
- 成功时使用 bcrypt 保存新哈希，并在同一事务内更新 `users.query_code_hash`、`users.query_code_updated_at`、`users.updated_at`，同时删除该用户全部 `query_sessions`。
- 事务提交后清除当前普通用户查询 Cookie；成功后不自动创建新会话，用户需用新查询码重新登录。
- API 响应不返回查询码明文、查询码哈希、内部用户 ID 或管理员信息。

## 前端变化

- 普通用户查询结果页新增“账号安全”区域，包含旧查询码、新查询码、确认新查询码和“修改查询码”按钮。
- 三个输入框均为密码类型，提交时前端先做必填和确认一致校验。
- 修改成功后清空输入、清除普通用户查询状态并回到普通用户登录界面，提示需使用新查询码重新登录。
- 管理员登录状态和管理员页面不受该操作影响。

## 验证

- 已执行 `go fmt ./...`。
- 已执行 `go test ./...`，后端测试通过。
- 已执行 `pnpm.cmd run build`，前端构建通过。
- 自动化测试覆盖未登录、旧查询码错误、空新查询码、确认不一致、格式非法、新旧相同、成功修改、Cookie 清除、响应不泄露敏感字段、错误限流、存储失败不清 Cookie、禁用/合并用户、过期会话、全部查询会话失效、旧查询码失效、新查询码可登录、其他用户会话不受影响等场景。
- 未执行真实浏览器人工验收：本轮未创建临时用户，也未使用真实业务数据；需要隔离测试账号后再人工验证。

## 安全说明

- 查询码明文仅用于本次请求内校验和 bcrypt 计算，不写入日志、响应、开发日志或数据库其他字段。
- 开发日志不记录任何查询码明文、查询码哈希、Cookie、数据库密码或会话令牌。
## 接手复核与隔离验收(同日,接手会话追加)

### 代码复核结果

接手后逐项复核 12 项安全要求,全部满足,未发现需要修复的问题:响应只含 `message` 字段;错误日志只记 error 不记查询码;`pjsk_query_session` 与 `pjsk_admin_session` Cookie 及会话表完全隔离,管理员会话无法调用该接口;`querycode.Validate` 由管理员端(`users.validateQueryCode`)与用户端共用;哈希更新与全量删除会话在同一事务;Cookie 仅在事务成功后清除(有 `TestChangeCodeStoreFailureDoesNotClearSessionCookie` 兜底);删除会话 SQL 按 `user_id` 限定;`FindUserBySession` 要求 `status='active'` 且未过期,disabled/merged/过期会话天然被拒;成功后不建新会话;前端成功路径只清普通用户状态;diff 未触碰订单/付款/导入/合并。

### 自动化验证

`go fmt`(无输出)、`go test ./...`(8 包全部通过)、`pnpm run build`(通过)、`git diff --check`(仅 CRLF 换行提示,无空白错误,未重写换行符)。

### 隔离浏览器人工验收

发现并处理一个环境问题:8080 端口跑着**旧编译的 backend.exe**(无 change-code 路由,POST 返回 404),确认无 Git 进程后停止旧进程并用当前代码重启,`/health` 正常、change-code 未登录返回 401。

临时账号:`TEST_QUERY_CODE_CHANGE_20260713`(显示名"查询码修改验收临时用户")与 `..._B` 两个 active 用户,随机查询码仅存放于仓库外临时文件,未写入日志/Git/汇报。

- **4.1 页面显示**:登录前无"账号安全"区域;登录后区域含旧/新/确认三个密码型输入框与提交按钮;页面无内部 ID/哈希/token/管理员字段;桌面与 375px 手机宽度均无横向溢出;订单区正常。
- **4.2 表单校验**:空字段由三个 `required` 密码框原生拦截;两次不一致提示"两次输入的新查询码不一致。";格式非法(过短/含空格)→ 400"查询码格式不正确";新旧相同 → 400;旧码错误 → 401"旧查询码不正确";用户 B 连续 5 次错误后第 6 次起 429"尝试次数过多,请稍后再试";全部为统一中文提示,无 SQL/bcrypt/panic/存在性泄露。
- **4.3 成功修改**:提交期间按钮禁用("修改中");成功提示与规范一致;回到登录页;刷新后仍是登录页;旧码登录 401、新码登录 200;控制台零报错;接口响应仅 `{"message":...}`。
- **4.4 多会话**:修改前用户 A 存在浏览器会话 + 独立 curl 会话两个 Cookie 环境;修改后另一会话请求 401,数据库核对 A 的旧会话全部删除(修改后重新登录产生的 1 条除外);用户 B 的会话全程 200 不受影响。
- **4.5 状态**:A 置为 `disabled`/`merged` 后 change-code 均 401,merged 后亦无法重新登录;随后测试用户直接删除。

### 临时数据清理

删除前先核对两个测试用户 orders=0、payments=0(工具内置计数不为零即中止);删除 2 条会话、2 个用户;只读回查:测试 CN 与测试会话均为 0,未删除任何真实用户,未修改订单/付款/导入/合并数据;仓库外的测试码与 Cookie 临时文件已删除。

### 结论

**MVP 1 验收通过**:未发现缺陷,零代码修复。未记录任何查询码明文;未访问或修改真实业务数据。
