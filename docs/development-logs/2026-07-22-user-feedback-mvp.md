# 2026-07-22 用户意见反馈 MVP

## 范围

- 新增普通用户纯文本意见反馈入口，以及管理员反馈列表、状态筛选和状态修改。
- 不修改付款、订单、安全流程；不接入消息中心、SMTP、附件、回复或富文本。
- 不修改 `PortalStatusBar`，不改变用户中心既有四卡布局。

## 数据库

- 新增 `0026_feedbacks.sql`，创建 `feedbacks` 表，仅包含 `id`、`user_id`、`content`、`created_at`、`status` 五个业务字段。
- 数据库约束正文 trim 后长度为 1–1000，状态仅允许 `new`、`processed`。
- 新增 `(status, created_at desc)` 与 `(user_id, created_at desc)` 索引。
- 隔离 PostgreSQL 测试库从 `0001` 到 `0026` 全量迁移成功；空正文、1001 字正文均被数据库约束拒绝，测试库完成后已删除。

## 后端

- 新增独立 `internal/feedback` 包，包含 handler、store、types，不复用或改写付款模块。
- `POST /api/query/feedbacks` 使用 `RequireSessionUser`，身份只取服务端 session；JSON 限制 8 KiB，未知字段、空正文和超过 1000 字均拒绝。
- 同一用户 60 秒内提交相同 trim 后正文时，通过事务内用户级 advisory lock 原子拦截，返回 `409 Conflict`。
- `GET /api/admin/feedbacks` 支持 `status`、`page`、`page_size`；`PATCH /api/admin/feedbacks/{id}/status` 只接受 `new` 或 `processed`。两个管理员接口均使用管理员会话鉴权，普通管理员可用。
- 日志只记录反馈 ID、用户 ID、状态、结果和安全错误类别，不记录反馈正文；自动化测试覆盖成功与失败日志。

## 前端

- 用户中心四卡下方新增低强调度“提交意见反馈”入口，进入 `/query/feedback`。
- 用户页提供 1000 字纯文本输入、字数显示、提交中禁用和成功后清空；不加载或展示历史反馈，退出登录会清除当前草稿。
- 谷子管理中心新增“意见反馈”卡片，进入 `/admin/feedbacks`；页面复用现有 panel、page-heading、page-actions、表格、分页、status-chip 和统一宽表滚动指令。
- 管理员可按状态筛选、查看用户与时间、展开纯文本正文，并在“新反馈 / 已处理”之间修改状态；没有详情路由，正文使用 Vue 文本插值，不使用 `v-html`。

## 验证

- `go test ./...`：后端全仓测试通过。
- `PJSK_RUN_DB_INTEGRATION_TESTS=1 go test -count=1 -run TestFeedbackStoreLifecycleIntegration -v ./internal/feedback`：隔离 PostgreSQL 全迁移、反馈生命周期、重复拦截与数据库内容约束通过。
- `pnpm.cmd test`：305/305 通过，包含 4 项反馈路由、表单、API、管理员交互约束测试。
- `pnpm.cmd run build`：`vue-tsc -b` 与 Vite 生产构建通过；生成 JS gzip 93.03 kB、CSS gzip 11.05 kB，无 source map。
- `git diff --check`：通过。
- 本地 mock 环境完成人工页面验收：用户中心四卡与低强调度入口间距正常，反馈输入框、字数提示、提交按钮禁用/启用、成功提示和清空行为正常；桌面与 390px 手机宽度均无横向溢出。
- 管理员反馈列表标题、状态筛选、内容摘要、正文展开和状态操作布局正常；手机宽度下宽表保持在横向滚动容器内，长正文按任意位置换行，不撑破页面。
- 截图验收使用的 `C:\tmp\pjsk-feedback-ui-mock.mjs` 仅为临时本地辅助文件，未进入仓库，验收后已删除。

## 部署与提交状态

- 本批次未部署。
- 人工页面验收已完成；本日志与阶段 2B 功能将纳入同一个提交，提交后仍不推送、不部署。
