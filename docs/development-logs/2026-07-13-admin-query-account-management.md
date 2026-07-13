# 开发日志:管理员查询码账户管理基础(2026-07-13)

## 本阶段目标

让管理员能够查看普通用户查询权限状态、查询码设置状态、创建时间和最后登录时间,并能设置/重置查询码、启用/停用查询权限。本阶段不开发密保问题、邮箱验证码、手机号验证码或用户自助绑定查询码。

## 只读调查结论

- `users` 已有 `query_code_hash text`,用于保存 bcrypt 查询码哈希;接口此前已经通过 `has_query_code` 布尔值暴露是否设置,没有返回哈希。
- `users` 已有 `status text`,初始迁移支持 `active/disabled`,后续 `0013_cn_merge.sql` 扩展为 `active/disabled/merged`。
- `users` 已有 `created_at/updated_at`,但没有查询码更新时间或普通用户最后登录时间。
- `query_sessions` 已有 `user_id/token_hash/expires_at/last_used_at/created_at`,并按 `user_id` 和 `expires_at` 建索引。
- 普通用户登录接口使用 `bcrypt.CompareHashAndPassword` 校验 `query_code_hash`,成功后写入 `query_sessions`;会话校验会要求用户仍为 `active`。
- 管理员用户管理已有 `/api/admin/users` 列表、`/api/admin/users/{id}` 详情、CN 合并预览/执行接口,本阶段复用现有用户管理入口,不重复创建用户管理模块。
- 真实用户 `succ` 只读核对:状态为 `active`,已设置查询码;本阶段未输出、修改或清除其查询码哈希。

## 数据库变更

新增迁移 `backend/migrations/0014_user_query_account_admin.sql`:

- `users.query_code_updated_at timestamptz`:记录管理员设置或重置查询码的时间。
- `users.last_query_login_at timestamptz`:记录普通用户查询登录成功时间。

无查询码明文列,无查询码找回字段,无邮箱/手机号/密保字段。

## 后端实现

- `backend/internal/query/handler.go`:普通用户登录创建 `query_sessions` 时,同一条 CTE 更新 `users.last_query_login_at`;未设置查询码、停用、错误查询码统一返回通用登录失败文案,避免泄露账号状态。
- `backend/internal/users/handler.go`:扩展现有 `/api/admin/users/{id}` 路由子操作:
  - `POST /api/admin/users/{id}/query-code`:设置或重置查询码。后端按当前 `has_query_code` 判断是设置还是重置;使用 bcrypt 保存哈希;成功后删除该用户全部 `query_sessions`;响应只返回用户 DTO,不返回明文或哈希。
  - `PATCH /api/admin/users/{id}/status`:启用或停用普通用户查询权限。停用时删除该用户全部 `query_sessions`;启用不改变原查询码。
- 管理员用户列表和详情 DTO 增加 `query_code_updated_at`、`last_login_at`;继续只返回 `has_query_code` 布尔值。
- 管理员接口仍由既有 `RequireAuthentication` 保护;普通用户 session cookie 不能访问管理员接口。

## 前端实现

- `frontend/src/api/client.ts`:新增 `patchJSON`;管理员用户类型增加 `query_code_updated_at`、`last_login_at`。
- `frontend/src/App.vue`:用户列表展示 CN、查询权限、查询码状态、创建时间、最后登录时间以及既有订单/金额汇总;用户详情页新增查询权限面板,支持设置/重置查询码、启用/停用查询权限。设置和重置使用二次确认,重置与停用文案明确提示旧查询码或旧会话会立即失效。
- `frontend/src/style.css`:补充查询权限面板布局和 `active/disabled/merged` 状态样式;窄屏下操作区单列堆叠。

## 安全边界

- 明文查询码只用于本次请求计算 bcrypt 哈希,不写入响应、日志、开发日志或数据库其他字段。
- API 响应不返回 `query_code_hash`,也不返回明文查询码。
- 停用用户后普通用户登录失败,旧查询会话立即失效;重新启用后原查询码仍可登录,除非管理员执行了重置。
- 本阶段未修改付款金额、付款分配、订单金额或 Excel 导入逻辑。
- 本阶段未修改或删除真实用户 `succ`,未创建真实订单或付款测试数据。

## 测试

新增/更新后端测试覆盖:

- 未登录管理员访问新用户管理操作返回 401。
- 管理员为未设置查询码用户设置成功。
- 管理员重置查询码成功,旧查询码登录失败,新查询码登录成功。
- 查询码明文和哈希不出现在接口响应中。
- 停用用户后登录失败,停用和重置均清理 `query_sessions`,旧查询会话失效。
- 重新启用后可再次用当前查询码登录。
- 不存在用户返回 404,非法查询码格式返回 400。
- 普通用户查询会话不能访问管理员接口。

测试数据均使用 `QUERY_ACCOUNT_TEST_` 或 `QUERY_ACCOUNT_ROUTE_TEST_` 前缀,测试结束按前缀清理用户和查询会话;未清理或修改真实用户 `succ`。

## 验证结果

已执行:

```
cd D:\pjsk\backend
go fmt ./...
go test ./...

cd D:\pjsk\frontend
pnpm.cmd run build
```

结果:后端全部测试通过;前端 `vue-tsc -b && vite build` 通过。

待最终收尾验证:按任务要求继续执行 `go build ./...`、`go vet ./...`、`git diff --check`、`git status --short`,并做浏览器人工验收记录。

## 2026-07-13 本地前端 API 主机名修正

- 调查发现 `frontend/.env` 和 `frontend/.env.local` 均不存在;前端默认 API 基址来自 `frontend/src/api/client.ts` 的 `VITE_API_BASE_URL`,Vite 开发代理仍指向 `http://localhost:8080`。
- 调整开发环境 API 访问策略:开发模式下前端统一使用相对 `/api` 和 `/health` 请求,不读取外部 `VITE_API_BASE_URL`;生产构建仍保留 `VITE_API_BASE_URL` 覆盖能力。
- 调整 Vite 开发服务监听 `127.0.0.1:5173`,并将 `/api`、`/health` 代理目标统一为 `http://127.0.0.1:8080`,避免页面使用 `127.0.0.1` 时浏览器请求混入 `localhost:8080`。
- 复核管理员 Cookie 设置:未固定 Domain,Path 为 `/`,SameSite 为 Lax,Secure 默认 false,适用于当前本地 HTTP 开发方式。
- 本次未修改管理员密码、普通用户、真实用户 `succ`、订单、付款或查询码数据;未记录 Cookie、session token、数据库密码或任何明文查询码。
## 2026-07-13 管理员用户详情窄屏验收修正

- 人工验收 320px 宽度时发现页面级横向溢出来自 `body` 最小宽度和查询权限操作区三列布局。
- 调整窄屏 CSS:允许页面按实际视口收缩,查询权限操作区在小屏单列堆叠,表格滚动容器不撑开页面。
- 本次仅修复验收发现的前端布局问题,未修改管理员密码、普通用户真实数据、订单、付款、查询码计算或导入逻辑。
## 2026-07-13 本地运行示例同步

- 同步 `.env.example` 和 `docs/run-new-stack.md` 的本地前端 API 说明:开发环境使用相对 `/api`、`/health`,由 Vite 代理到 `http://127.0.0.1:8080`。
- 避免示例配置重新引入 `localhost:8080` 与 `127.0.0.1:5173` 混用。
## 2026-07-13 本地运行 README 同步

- 同步 `README.md`、`backend/README.md` 和 `HANDOVER.md` 的本地访问说明为 `127.0.0.1` 开发方式。
- 明确前端开发模式使用相对 `/api` 与 `/health`,生产构建才使用 `VITE_API_BASE_URL`。