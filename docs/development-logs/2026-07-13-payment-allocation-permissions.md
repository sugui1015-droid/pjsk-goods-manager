# 开发日志:付款分摊命名与权限边界(2026-07-13,批次 1/5)

## 本轮需求

用户提出人工验收问题清单(共 8 部分)。本条日志覆盖第一部分:"本次分配"措辞澄清 + 管理员/普通用户权限边界确认与补测。本轮不提交、不推送、不执行 CN 合并、不撤销真实付款。

## 问题原因

- 付款录入表格列名"本次分配"容易被误读为修改商品原价,而不是"本次付款要分摊到该明细的金额"。
- 需要用测试而非目测确认:付款创建/分摊/撤销、CN 合并等接口在后端层面确实要求管理员会话,不是只靠前端隐藏按钮。

## 现状复核(修改前)

先读了 `AGENTS.md` 和 `docs/development-logs/README.md`,两者规则一致(日志统一放 `docs/development-logs/`,不得另建根目录日志文件夹,不得删除/重命名/覆盖历史日志),不存在冲突,按此规则继续。

复核代码后确认以下内容**已经是现状**,未发现需要新增的缺口:

- `frontend/src/App.vue` 的 `/query`（普通用户查询）页面本来就是纯只读表格,没有可编辑输入框、没有"修改付款金额""修改已付金额""撤销付款"按钮,也没有任何管理员操作入口。
- 后端 `backend/internal/api/router.go` 中,`/api/admin/payments*`、`/api/admin/users*`(含 `merge`、`merge-preview`)全部经过 `adminHandler.RequireAuthentication` 包裹。
- `payments.Handler.Void` 内部还额外做了一次 `admin.CurrentAdmin` 校验(双重保护,不完全依赖路由中间件)。
- 已保存的付款记录没有任何"编辑"入口,只有"撤销"入口,符合"录错先撤销再重录"的既定规则。
- 分摊金额校验(`paymentAmountInvalid`、后端 `CreatePayment` 中的 `ErrInvalidAmount`/`ErrOverPayment`)已经要求金额 > 0 且不超过剩余应付;"分摊合计必须等于本次付款本金"这一条在当前实现里是**结构性成立**的 —— 本金 `total` 就是逐条明细分摊金额累加得出,不存在"本金"和"分摊合计"分开填写导致不一致的可能。金额计算全程使用 `round2`/`safeCentsFromFloat64`/`calculateFee` 的整数分运算,未改动。

## 设计决策

1. 统一采用"**本次分摊金额**"作为付款录入表格列名和付款详情历史表格列名,弃用"本次分配"。理由:"分摊"比"分配"更贴近"把这笔钱按明细拆开"的语义,不会被误读为修改商品价格;录入页和详情页统一措辞,减少认知负担。
2. 在录入表格上方新增常驻说明文字,并给表头加 `title` 悬浮提示,双重提醒该金额含义和操作边界。
3. 权限边界本轮判定为**已达标**,不新增前端改动,而是把验证责任转移到自动化测试上,避免以后重构时无声破坏权限保护。

## 修改文件

- `frontend/src/App.vue`:
  - 付款录入校验提示文案"本次分配金额" → "本次分摊金额"。
  - 付款录入表格表头"本次分配" → "本次分摊金额"(带 `title` 提示)。
  - 付款详情历史表格表头"本次分配金额" → "本次分摊金额"。
  - 录入表格上方新增 `<p class="payment-allocation-hint">` 说明文字。
- `frontend/src/style.css`:新增 `.payment-allocation-hint` 样式(与既有 `.payment-immutable-hint` 视觉体系一致,浅蓝底提示,和"不可直接修改"的浅橙底提示区分开)。
- `backend/internal/payments/handler_test.go`:新增 `TestVoidRequiresAdmin`(确认 `Void` 处理函数在没有管理员上下文时直接返回 401,不依赖路由中间件)、`TestUnpaidRequiresGetButNotAdminAtHandlerLevel`(明确记录 `Unpaid`/`CN` 处理函数本身不做二次校验,完全依赖路由层 `RequireAuthentication`,防止未来重构误删中间件时无测试兜底)。
- `backend/internal/api/payments_routes_test.go`(新增文件):`TestAdminPaymentAndUserRoutesRequireAuth` 用真实路由 + 真实数据库连接,逐条验证 `/api/admin/payments`、`/api/admin/payments/cn`、`/api/admin/payments/unpaid`、`/api/admin/payments/{id}`、`/api/admin/payments/{id}/void`、`/api/admin/users`、`/api/admin/users/{id}`、`/api/admin/users/merge-preview`、`/api/admin/users/merge` 在未登录时全部返回 401,且路由确实存在(排除误判 404 为"安全"的假阳性)。另加 `TestPublicQueryRoutesRejectAdminOnlyPayloads` 确认 `/api/query/*` 命名空间下不存在任何创建/撤销付款的路由。

## 数据库影响

无。未新建、未修改任何迁移文件,未执行任何写操作。

## 权限影响

无功能性变化 —— 本轮确认现状已满足"后端独立于前端做权限保护"的要求,新增的是测试覆盖,不是新的权限逻辑。

## 测试结果

```
go build ./...                                   通过
go test ./internal/payments/...                  ok  (3.7s，含新增 2 个测试)
go test ./internal/api/...                        ok  (1.1s，含新增 2 个测试)
pnpm run build                                    通过(vue-tsc 类型检查 + vite build)
```

## 未完成事项

- 技术标识区域重排版(批次 2)、中文化审计(批次 3)、Excel 字段顺序与列宽(批次 4)、前端整体视觉整理(批次 5)尚未开始,将分别追加新日志。
- 本条日志涉及的前端文案改动尚未做真实浏览器人工验收截图确认(将在批次 5 结束后统一做一轮完整人工验收并记录)。

## Git 状态

本轮不提交、不推送。改动仍在工作区,`git status` 会显示 `frontend/src/App.vue`、`frontend/src/style.css`、`backend/internal/payments/handler_test.go` 为已修改,`backend/internal/api/payments_routes_test.go` 及本日志文件为新增未跟踪文件。
