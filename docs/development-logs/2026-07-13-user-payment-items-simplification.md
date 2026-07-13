# 开发日志:普通用户端关联付款明细精简(2026-07-13)

## 本轮需求

只针对普通用户可见页面和普通用户专用接口中的"关联付款明细"做精简:用户查看某笔付款的关联内容时,只看谷子名称/角色/分类/数量/单价/小计/本次付款金额/当前付款状态八项;不展示订单号、项目名、内部 ID、导入批次、来源文件、来源 Sheet、管理员信息和审计撤销信息;英文数量文案改中文。管理员端付款详情、付款列表、订单详情、导出、审计撤销字段一律不动;普通用户正常订单列表的 order_no/project_name 也不许全局删除。

## 现状核对(修改前的真实发现)

按要求先读了 `2026-07-13-payment-page-redesign-and-filters.md`、`AGENTS.md` 和日志 README,再逐一核对代码,结论:

1. **普通用户 API(`/api/query/orders`)此前根本没有"关联付款明细"** —— `query.PaymentRecord` 只有本金/手续费/实付金额/方式/状态/时间,用户在付款历史里看不到某笔付款分摊到了哪些谷子上。所以本轮不存在"从现有结构里删字段"的工作,正确做法是按"优先新建或拆分普通用户付款关联明细的 DTO"的指示,**从零新建一个用户专用结构**,订单号/项目名/ID/审计字段从一开始就不进入。
2. 管理员端的付款关联明细走的是完全独立的 `payments.PaymentDetailItem`(含订单号、项目名、来源、内部 ID),与用户端结构零共享,不受本轮影响。
3. "N items" 英文数量文案只存在于**管理员**付款详情页和订单详情页两处页头(普通用户页面没有)。这是更早"管理员网页显示中文"轮次的遗漏。
4. 用户端 `PaymentRecord` 还序列化着 `voided_at`(审计时间戳,前端从未渲染)——属于"审计和撤销信息"泄露面,一并收掉;撤销这一生命周期信息用户仍能通过 `status`("已撤销")看到。

## 设计决策

1. **新建用户专用 DTO `query.PaymentItem`**(九个 JSON 字段:`goods_name`、`display_name`、`character_name`、`category`、`quantity`、`unit_price`、`amount`、`applied_amount`、`payment_status`),挂在 `PaymentRecord.items` 下。`display_name` 按项目既有规则在 SQL 里合成(分类为空或"默认分类"时不拼接,否则"名称-分类"),保证"谷子名称是用户可理解的业务名称";`amount` 是该明细自身小计,`applied_amount` 是本笔付款分摊到该明细的金额,两者严格分开,未混用"已付"。
2. **不提供"累计已付/剩余"**:用户字段清单里没有这两项;虽然技术上可以用 CTE 可靠计算,但放进"单笔付款的关联明细"里容易和"本次付款金额"混淆,按"如果只是临时推算就不要新增"的从严原则省略。
3. **数据加载**:`listPaymentsForUser` 之后新增一条按 `p.user_id` 限定的关联查询(`payment_items → order_items → products`),在 Go 内按内部 payment id(`json:"-"`,永不序列化)分组挂载;付款无明细时 `items` 为空数组而不是 null。与管理员付款详情一致,不过滤已撤销明细,保持付款历史完整。
4. **前端渲染**:付款历史表新增第 7 列"关联明细"(显示"共 N 条明细"),每笔付款下方跟一行 `<details>` 折叠区(默认收起,"▶ 查看关联明细(共 N 条)/▼ 收起关联明细",复用技术标识面板的开合模式),展开后是只含八列业务字段的嵌套小表。角色为空显示"—"。金额全部走 `formatMoney` 两位小数,状态走既有 `queryPaymentStatusLabel` 中文映射,没有新造状态规则。
5. **`voided_at` 从用户响应移除**:结构体字段删除、SQL 列不再查询、前端 TS 类型同步删除。
6. **管理员页头"N items"改为"共 N 条明细"**(付款详情、订单详情两处):这是完成更早轮次"管理员网页显示中文"的既定要求,纯文案修正,没有增删任何字段或数据,与本轮"不修改管理员页面字段"的边界不冲突。除此之外管理员端零改动。

## 修改文件

- `backend/internal/query/handler.go`:新增 `PaymentItem` 类型;`PaymentRecord` 增加 `Items`、删除 `VoidedAt`;`listPaymentsForUser` 挂载明细;新增 `listPaymentItemsForUser`。
- `backend/internal/query/handler_test.go`:新增 `TestPaymentItemsExposeOnlyUserFacingFields`(对序列化后的 JSON 做**字段白名单**断言:付款明细只许出现九个业务键;付款记录本身不得出现 id/order_no/order_number/project_name/import_batch_id/import_filename/source_sheet/created_by/voided_at/voided_by/void_reason/note;同时断言普通订单列表**仍保留** order_no 和 project_name)。
- `backend/internal/query/payment_items_integration_test.go`(新增文件):`TestPostgresUserPaymentsCarryMatchingItems` 真库集成测试 —— 前缀 `QUERY_PAYITEMS_TEST_<时间戳>` 的独立测试数据(用户/项目/两个商品/订单/两条明细/两笔付款),验证:一笔付款跨两条明细时金额与商品一一对应(A 分摊 4.00 于小计 10.00,部分付款;B 分摊 20.00 付清)、已撤销付款也带明细、`display_name` 按"名称-分类"规则合成、角色/分类字段正确。测试结束自动清理全部前缀数据。
- `frontend/src/api/client.ts`:新增 `QueryPaymentItem` 类型;`QueryPaymentRecord` 增加 `items`、删除 `voided_at`。
- `frontend/src/App.vue`:付款历史表增加"关联明细"列和可折叠明细行;管理员付款详情/订单详情页头 "N items" → "共 N 条明细"。
- `frontend/src/style.css`:新增 `.query-payment-items` 系列样式(折叠行浅灰底、summary 开合标签切换)。

## 普通用户关联付款明细最终字段顺序

谷子名称、角色、分类、数量、单价、小计、本次付款金额、当前付款状态。

## 后端停止下发的字段

- 新建的付款明细结构从未包含:订单号、项目名、商品/订单/付款内部 ID、导入批次、来源文件、来源 Sheet、SKU、管理员信息、审计撤销信息。
- 本轮额外移除:用户端 `PaymentRecord.voided_at`(审计时间戳)。
- **保留未动**:普通用户订单列表的 `order_no`、`project_name`(订单分组、卡片标题、Vue key 都依赖它们),有白名单测试专门断言它们仍然存在。

## 数据库影响

无。无迁移、无表结构变动、无真实数据写操作(集成测试只写入带独立前缀的测试数据并在结束时清理)。

## 权限影响

无功能变化;用户端 API 泄露面进一步缩小(付款明细以最小字段集下发,`voided_at` 不再出现)。管理员接口和鉴权零改动。

## 测试结果

```
go fmt ./...    无输出
go build ./...   通过
go vet ./...    通过
go test ./...    全部通过(admin/api/export/importpreview/orders/payments/query/users)
pnpm run build   通过(vue-tsc 类型检查 + vite build)
git diff --check 仅 CRLF 提示,无空白错误
```

管理员端不变性由既有测试继续保证:`payments` 包 `TestDetailReturnsFullItems`、`TestPostgresPaymentDetailReturnsAuditAmountsAndMethod`(订单号、项目名、来源、审计金额与撤销信息)本轮未改动且全部通过。

## 浏览器实测(只读)

- `/query` 登录页正常渲染(无控制台报错)。
- 管理员付款详情页(既有已撤销付款,直接 URL 导航只读查看):页头显示"共 12 条明细",订单号/项目名/来源/撤销时间/撤销管理员/撤销原因全部原样保留,确认管理员端未受影响。
- 新增的 `.query-payment-items` 全套 CSS 规则确认已加载进页面样式表。
- **限制**:普通用户付款历史的真实数据渲染无法验证——CN"柴"的查询码只存 bcrypt 哈希,无法登录用户端(与上一轮相同的限制,本轮同样未重置查询码)。数据正确性由真库集成测试覆盖;建议你人工验收时用真实查询码登录 `/query` 展开一笔付款的关联明细核对。

## 未完成事项

- 普通用户端真实浏览器验收(需要你用真实查询码登录)。
- 管理员端其他表格页的表头筛选 UI 等上轮遗留项,不在本轮范围。

## Git 状态

本轮**未提交、未推送**。工作区在上一轮基础上叠加:`backend/internal/query/handler.go`、`backend/internal/query/handler_test.go`、`frontend/src/App.vue`、`frontend/src/api/client.ts`、`frontend/src/style.css` 为已修改;`backend/internal/query/payment_items_integration_test.go` 与本日志为新增未跟踪文件。未修改任何真实业务数据,未创建/撤销付款,未动查询码,未执行 CN 合并,未删除或重命名任何历史日志。

## 追加记录:goods_name 去重(同日第二批次)

复查发现付款关联明细 DTO 里 `goods_name` 只被前端用作 `display_name` 的兜底和 Vue `:key` 的一部分,而 `display_name` 在 SQL 里由非空列 `product.name` 合成、**永远非空**,兜底永远不会触发 —— 属于无意义的重复下发。已按"接口最小化"原则删除:

- `backend/internal/query/handler.go`:`PaymentItem` 删除 `GoodsName` 字段,SQL 不再单独查询 `product.name` 原始列(合成的 `display_name` 保留)。
- `frontend/src/api/client.ts`:`QueryPaymentItem` 删除 `goods_name`。
- `frontend/src/App.vue`:付款明细单元格改为直接渲染 `display_name`,`:key` 改用 `display_name` 组合。
- 测试同步:白名单测试的允许/必需键列表去掉 `goods_name`;集成测试改按 `DisplayName` 断言(含"名称-分类"合成规则和空分类直出名称两种情形)。

**保留不动**:普通用户订单列表 `QueryOrderItem` 仍同时有 `goods_name` 和 `display_name`(按本轮指示不改订单列表);管理员接口、导出接口不受影响。

验证:`go build`、query 包三个相关测试(白名单/防泄露/真库集成)、`pnpm run build` 全部通过。
