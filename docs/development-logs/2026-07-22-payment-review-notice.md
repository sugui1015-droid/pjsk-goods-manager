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

## 提交与生产部署

- 功能提交：`219451b572bf7d00173d41fb1ce54e3ff97bfdfc`（`feat(frontend): add payment review update notice`）。
- 推送 `origin/main` 后，`git rev-list --left-right --count origin/main...HEAD` 返回 `0 0`。
- 按标准 release 链部署，未直接修改线上文件：构建 release `219451b572bf`，上传后校验归档 SHA-256 为 `4a65baac03fdd198a03ab507c4489ba35cd9dc1db8fa698fed30100e7bc7cc15`。
- 候选 release 校验通过后原子切换 `/opt/pjsk/current`：旧 release 为 `382e0729e75c`，新 release 为 `219451b572bf`；旧 release 保留为直接回滚点。
- 本次无数据库迁移，生产 `schema_migrations` 仍为 25 条，最高版本为 `0025_payment_submission_request_id.sql`。
- 切换后 `pjsk-backend` 与 `caddy` 均为 `active`，后端 `NRestarts=0`；本机和公网 `/health` 均返回数据库已连接，新首页已引用本 release 的 JS/CSS 且资源响应正常。
- 最终只读复核时，`/opt/pjsk/current` 仍指向 `219451b572bf`，服务与公网健康检查保持正常。

## 生产人工验收状态

- 自动化部署验收已通过，生产部署历史记录为 `health_pass_manual_pending`。
- 已尝试使用现有浏览器会话执行用户侧定向验收，但用户侧标签页无法稳定返回可交互页面；在没有可确认的测试用户会话和测试付款数据时，没有对真实付款记录执行审核写操作。
- 因此“无更新、审核通过、审核驳回、查看后消失、两 CN 隔离”五项生产人工场景仍待具备测试账号会话后补验；当前不把自动健康检查等同于业务人工验收通过。

## 部署过程中的非破坏性修正

- 首次只读远程检查中，PowerShell 提前解释了远端命令替换表达式；仅导致显示命令失败，未产生服务器写入。
- 磁盘空间预检的两种命令格式与远端环境不兼容，改用兼容格式复核通过后才继续发布。
- 本地 release 元数据正则首次遗漏等号，产生一次假失败；经 Go 构建元数据只读核对并修正校验表达式后确认 `vcs.revision` 正确、`vcs.modified=false`，随后才上传制品。
- `stat` 显示格式的引号在远端拆分，但独立 SHA-256 校验通过，未影响制品完整性或发布内容。
