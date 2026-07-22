# 2026-07-22 付款审核状态提醒优化

## 范围

- 本批次仅修改前端，不修改数据库、后端接口、付款审核流程或付款状态机。
- 不新增通知表，不接入 SMTP，不改变 `payment_submissions` 与正式 `payments` 的业务边界。

## 实现

- `PortalStatusBar` 新增可选 `notice` 展示参数和 `notice` 点击事件；用户中心有新审核结果时显示“付款审核有更新”，点击进入付款中心。
- 新增纯函数 `getPaymentNoticeState`，使用现有付款提交的 `status`、`reviewed_at` 与本机最后查看时间判断提醒，不在模板中堆叠业务判断。
- 进入用户中心时复用现有 `loadUserSubmissions()` 请求，不复制接口调用。
- 进入付款中心且提交列表成功加载后，按 CN 隔离写入 `localStorage` 查看时间；加载失败时不清除提醒。浏览器禁用存储时退化为仅当前会话消除提醒，不影响业务数据。

## 安全与回滚

- 本地仅保存查看时间，不保存查询码、会话、付款图片、审核原因或其他业务内容。
- 回滚只需撤销本批前端文件与构建产物，不涉及数据库回滚或数据修复。

## 验证

- `node --test tests/payment-review-notice.test.mjs`：6/6 通过，覆盖审核通过、旧记录、多个提交、无效时间、入口复用与点击跳转。
- `node --test tests/admin-role-management.test.mjs tests/payment-review-notice.test.mjs`：36/36 通过，确认付款查看时间没有放宽“一次性临时密码不得进入浏览器存储”的既有安全保护。
- `pnpm.cmd test`：301/301 通过。
- `pnpm.cmd run build`：`vue-tsc -b` 与 Vite 生产构建通过；生成 JS gzip 91.24 kB、CSS gzip 10.79 kB，无 source map。
