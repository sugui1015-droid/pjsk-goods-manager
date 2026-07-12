# 付款流程（当前实现）

本文档描述当前代码库中实际生效的付款流程，对应 [backend/internal/payments/handler.go](../backend/internal/payments/handler.go)。

历史上曾规划过“用户上传截图 + submitted/approved/rejected 审核流”的方案，但该方案**尚未实现**。当前正式上线的流程是管理员核对后手动录入付款，见下文。

## 当前正式流程

1. 管理员通过 `GET /api/admin/payments/unpaid?cn=xxx` 查询某个 CN 尚未结清的订单明细（`GET /api/admin/payments/cn?cn=xxx` 返回该 CN 的全部明细，包括已结清项）。
2. 管理员在明细中勾选本次要付款的一条或多条 `order_items`，逐项填写本次付款金额。
3. 管理员填写付款方式（`wechat` / `alipay` / `bank` / `cash` / `other`）、付款时间、备注，并通过 `POST /api/admin/payments` 提交。
4. 若付款方式为 `wechat`，后端按业务规则自动计算手续费（见下文），其他方式手续费为 `0`。
5. 付款一旦创建，状态直接是 `approved`，不存在 `submitted` / `rejected` 的人工审核步骤。
6. 一笔付款可以同时覆盖同一 CN 名下的一条或多条订单明细（`payment_items` 表记录每条明细的分摊金额）。
7. 支持部分付款：单条订单明细可以分多次付清，每条明细的已付/剩余金额会实时重新计算，订单和明细的 `payment_status` 会同步更新为 `unpaid` / `partial` / `paid`。
8. 创建请求必须携带 `idempotency_key`；重复提交同一个 key 会直接返回原有付款记录（`duplicate: true`），不会重复扣款。

## 金额字段说明

以下字段出现在 `PaymentRecord`、`PaymentListItem`、`PaymentDetail` 等响应结构中：

- `principal_amount`：本次付款的本金（用户/CN 实际需要承担的商品款）。
- `fee_amount`：手续费，仅微信付款可能非零。
- `payable_amount`：`principal_amount + fee_amount`，即需要收款方实际支付的金额，直接对应数据库列 `payable_amount`。
- `total_amount`：与 `payable_amount` 数值相同，是接口额外提供的别名字段，供前端使用更直观的命名。
- `amount`：与 `principal_amount` 数值相同，对应数据库列 `submitted_amount`，是较早期的字段命名，前端部分位置仍在读取，保留以维持接口兼容，不建议在新代码里继续使用。

以上字段均为兼容保留，未来如需精简，需要同步更新前端 [frontend/src/api/client.ts](../frontend/src/api/client.ts) 和 [frontend/src/App.vue](../frontend/src/App.vue) 中的对应引用。

## 微信手续费规则

规则和实现见 [backend/internal/payments/handler.go](../backend/internal/payments/handler.go) 中的 `calculateFee`，并有单元测试 `TestCalculateFeeWechat` 覆盖：

- 手续费率为本金的 `0.001`（即 0.1%）。
- 计算时先把本金换算成“分”为单位的整数（`baseCents`），避免浮点误差。
- 手续费 = `ceil(baseCents / 1000)` 分，即只要小数点后第 3 位及以后存在非 0 数字，就向上进 1 分。
- **不使用四舍五入**。
- 支付宝、银行转账、现金、其他方式的手续费固定为 `0`。

示例：本金 `36.80` 元 → `baseCents = 3680` → `feeCents = ceil(3680 / 1000) = 4` → 手续费 `0.04` 元，应付 `36.84` 元。

## 付款更正方式

- **已创建的付款记录不允许直接修改**金额、付款方式或关联的订单明细。
- 如果发现录入错误，正确做法是：先通过 `POST /api/admin/payments/{id}/void` 撤销原付款，再重新调用 `POST /api/admin/payments` 录入一笔新的正确付款。
- 撤销时必须填写 `reason`（撤销原因），否则请求会被拒绝（`ErrVoidReasonRequired`）。
- 只有状态为 `approved` 的付款可以被撤销；已经是 `voided` 的付款再次撤销会返回冲突错误（`ErrPaymentAlreadyVoid`）。
- 撤销成功后：
  - 付款状态变为 `voided`，并记录 `voided_at`（撤销时间）、`voided_by_admin_id`（操作管理员）、`void_reason`（撤销原因）。
  - 该付款不再计入相关订单明细的“已付金额”，明细和订单的 `payment_status` / `status` 会重新计算回撤销前的状态（例如从 `paid` 回退到 `partial` 或 `unpaid`）。
  - 撤销记录本身不会被删除，作为审计留痕永久保留，可通过 `GET /api/admin/payments/{id}` 查看撤销信息。

## 尚未实现的内容（后续可选功能）

以下内容仍停留在早期规划阶段，代码中**没有**对应实现，不应被当作已完成能力：

- 用户自行上传付款截图。
- 文件或对象存储（如 Supabase Storage）接入。
- `submitted / approved / rejected` 的用户付款审核流转。
- 用户端自主发起付款申请（当前付款只能由管理员在后台手动录入）。

如果未来需要恢复这条路线，应在 `payments` 表结构和本文档基础上重新设计，而不是假定它已经存在。
