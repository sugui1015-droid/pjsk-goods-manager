# 开发日志:付款详情、手续费与撤销审计(2026-07-12)

## 本轮完成内容

对应提交(按时间顺序):

- `702c720` feat: add admin payment management — 管理员按 CN 查询未付明细、手动录入付款
- `7504025` feat: add payment records management — 付款记录列表与筛选
- `a66f2f2` / `1486761` / `87504be` / `692a055` test: 付款时间、幂等键、未知 CN、handler 覆盖补全
- `994b93c` feat: support payment voiding and status rollback — 付款撤销与订单状态回滚
- `90bb159` feat: add manual payment entry and fee handling — 微信手续费(整数分计算)
- `ad6838e` feat: complete payment details and void audit — 付款详情页、撤销审计字段
- `9d56453` docs: align project status and handover — 文档对齐与交接文档

## 核心设计决策

1. **付款创建即 `approved`**:正式流程为管理员核对后手动录入,不经过 submitted/rejected 审核流(该审核流列为后续可选功能,未实现)。
2. **金额以整数分计算**:`safeCentsFromFloat64` → `calculateFee` → `centsToNumeric`,手续费计算全程无浮点运算,写库时以字符串形式传给 `numeric(12,2)`。
3. **微信手续费**:`feeCents = (baseCents + 999) / 1000`,即本金的 0.1% 向上进位到分,不四舍五入;其他付款方式手续费为 0。
4. **付款不可修改,只能撤销后重录**:撤销必须填写原因,记录 `voided_at` / `voided_by_admin_id` / `void_reason`,撤销后重算相关明细与订单的付款状态。
5. **幂等保护**:创建付款必须携带 `idempotency_key`,数据库层通过 `pg_advisory_xact_lock` + 唯一查询防止并发重复创建。

## 边界条件与测试覆盖

后端 `internal/payments` 共 40 个测试,覆盖 PDF 计划要求的全部边界:

| 边界条件 | 测试 |
|---|---|
| 未选明细 / 金额 ≤ 0 | `TestCreateMapsValidationErrors` |
| 超过剩余金额 | `TestCreateRejectsOverPayment`、`TestPostgresRejectsOverPaymentWithoutResidualRows` |
| 同一明细多次部分付款 | `TestPostgresPartialPaymentInstallmentsAndVoidRecovery` |
| 微信手续费自动计算 | `TestCalculateFeeWechat`(0.01 → 进 1 分等多组用例) |
| 非微信方式手续费为 0 | `TestCalculateFeeNonWechat` |
| 幂等键缺失 / 重复提交 | `TestCreateRejectsEmptyIdempotencyKey`、`TestPostgresDuplicatePaymentDoesNotDuplicateRows` |
| 已撤销付款不能再撤销 | `TestVoidMapsAlreadyVoided`、`TestPostgresVoidPaymentTwiceReturnsConflictError` |
| 并发撤销只成功一次 | `TestPostgresVoidPaymentConcurrentOnlyOneSucceeds` |
| 撤销后金额恢复 | `TestPostgresVoidPartialPaymentRollsBackToSubmitted`、`TestPostgresVoidFullPaymentRollsBackPaidStatus` |
| 撤销失败不产生部分写入 | `TestPostgresVoidFailureDoesNotPartiallyWrite` |

前端录入侧校验(App.vue):未选明细禁止提交;单条分配金额必须 > 0 且 ≤ 剩余应付;本金 = 各明细分配之和(由构造保证);微信手续费为计算展示值,无输入框,管理员不可修改;保存前弹出确认(CN、方式、本金、手续费、实付、明细清单);撤销需输入原因并二次确认(展示 CN、本金、手续费、实付总额、影响明细数)。

本轮补充:录入付款区和付款详情撤销区均增加"付款记录不可直接修改,请撤销后重新录入"的明显提示。

## 人工验收记录

自动化浏览器不支持原生 `window.prompt()`,撤销原因弹窗需人工在本地浏览器验收。验收清单(打开 `http://localhost:5173/admin/payments`):

- [ ] 点击付款详情正常显示本金 / 手续费 / 实付总额
- [ ] 点击撤销付款出现原因输入框
- [ ] 确认弹窗展示 CN、本金、手续费、实付总额、影响明细数量
- [ ] 撤销后状态变为"已撤销"
- [ ] 撤销原因、撤销管理员、撤销时间正常展示

(此清单由管理员在真实浏览器中逐项勾选;自动化部分已由集成测试覆盖。)

## 已知遗留

- `payments` 表保留了 submitted/rejected 相关列(`submitted_at`、`approved_at`、`approved_by`),当前创建时直接填充,为将来可能的审核流预留。
- 迁移编号存在两个 `0005_*`、缺 `0006_*` 的历史问题,已确认两者均已执行且互不冲突,新迁移从 `0013` 继续编号,历史文件不重命名。
