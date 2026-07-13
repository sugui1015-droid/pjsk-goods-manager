# 开发日志:付款录入页重排、状态改名、时区修正与独立筛选(2026-07-13)

## 本轮需求

人工验收后提出的新一轮界面/查询/导出调整,共十二个部分:付款录入页重排(方式限定为支付宝/微信、流程分三步、金额三卡片)、付款状态"已生效"改为"已交肾"、时间筛选文案、全链路中国时区复核、CN/谷子系列/谷子种类/谷子角色独立筛选、普通用户页精简、付款记录页信息优先级、两类导出字段顺序、CN"柴"查询码只读排查。本轮不提交、不推送、不执行 CN 合并、不创建或撤销真实付款。

## 问题原因(逐项核对后的真实发现)

1. **付款方式选择区**:此前(上一轮)已经把 5 种方式做成统一高度的按钮组,但用户本轮明确要求录入页只保留支付宝/微信两种,且两者必须严格等宽等高、蓝/绿配色区分,原有 5 按钮布局不满足"不能再根据文字长度自动改变按钮宽度"和颜色区分的要求。
2. **流程顺序不清楚**:原布局把付款方式、金额预览、备注、保存按钮全部塞进一个 4 列 CSS Grid 同一行,阅读顺序和视觉顺序不一致,"保存付款"按钮和明细数量、金额挤在一起。
3. **金额信息混在统计区**:本次已选本金/微信手续费/本次实付总额和 CN/订单总额/已付/剩余等统计指标混在同一个 `summary-grid` 里,造成"实付金额"这个最高优先级数字被稀释,而且和下方 `.payment-fee-preview` 又重复展示了一遍本金/手续费/实付金额——同一组数字在页面上出现了两次。
4. **付款状态措辞**:`paymentStatusLabel` 的 `approved` 映射是上一轮定的"已生效",本轮用户明确改为"已交肾",且要求区分"表头(交肾状态)"与"状态值(已交肾)"两种措辞,不能机械替换到所有"状态"字样(订单/明细的"已付款"不能动)。
5. **时区**:导出层(`export/labels.go`)在上一轮已经用固定 UTC+8 处理,但发现两个真实缺口:
   - 前端 `formatDate`/`localDateTimeInputValue` 用 `toLocaleString('zh-CN')`/`getTimezoneOffset()`,没有显式指定 `timeZone: 'Asia/Shanghai'`,如果管理员电脑本地时区不是中国时区,页面显示和"录入付款时间"默认值都会跟着错。
   - 后端 `parsePaymentTime` 用 `time.Local`(操作系统进程时区)解析管理员填写的付款时间,`paid_from`/`paid_to` 筛选参数直接原样拼进 `::timestamptz`,由 Postgres 会话时区决定含义——这两处都不是"固定中国时区",而是依赖运行环境,不符合"不要根据当前电脑或浏览器所在地自动切换"的要求。
6. **谷子系列/种类/角色混在一个输入框**:管理员订单列表原来只有一个"谷子种类"输入框,后端 `orders.List` 的 `item` 参数用同一个关键词去匹配 `product.name`/`category`/`series_code`/`character_name` 四个字段,不是"三个独立条件"。付款明细导出接口也没有这三个筛选参数。
7. **普通用户页仍暴露技术字段**:`/query` 页面模板本身没有渲染 `import_filename`/`import_batch_id`/`id` 等字段(上一轮已确认),但**后端 API 响应本身仍然把这些字段序列化进 JSON**(`OrderItem.ID`/`ImportBatchID`/`ImportFilename`/`SourceSheet`、`Order.ID`/`ImportFilenames`、`User.ID`、`PaymentRecord.ID` 全部在 `/api/query/orders` 响应里),只是前端不渲染——这不满足"后端也要做防护,不能只依赖前端隐藏"的原则,也不满足"普通用户不需要看到来源文件名"(`<p>来源：...</p>` 这行此前直接把 Excel 文件名展示给用户)。
8. **CN"柴"查询码**:数据库 `users` 表只有 `query_code_hash`(bcrypt),没有任何明文查询码列,已用只读方式在管理员用户列表确认"柴"的查询码状态为"已设置"。

## 设计决策

1. **付款录入页改为纵向三步流程**:`选择付款方式 → 核对金额 → 确认付款` 作为一段常驻提示文字,三个区块各占一行(`.payment-form.payment-flow` 用高特异性选择器把原有 4 列 Grid 覆盖成单列纵向布局)。方式区块只渲染 `alipay`/`wechat` 两个选项,用 `grid-template-columns: 1fr 1fr` 保证严格等宽,配色用两组独立的浅/深蓝、浅/深绿类名(`--alipay`/`--wechat` 修饰符),未选中态和选中态(整块变实心色 + 白字)对比明显,原生 `<input type="radio">` 保留(键盘可操作),窄屏降级为单列纵向堆叠。**后端可选枚举没有删除**(`payment_method must be one of wechat, alipay, bank, cash, other` 校验逻辑、`payments` 表的历史 bank/cash/other 记录、筛选下拉框里的 5 个选项都原样保留),只是录入页的选择器改为只出现两个按钮。
2. **金额卡片独立于统计区**:从 `summary-grid` 里删掉"本次已选本金/微信手续费/本次实付总额"三项(保留 CN/订单总额/有效已付总额/剩余待付总额/待付明细数),改为在"2. 核对金额"步骤下用新的 `.amount-card-row`(3 等宽卡片)展示本金/手续费/实付金额,`.amount-card--payable` 用浅红背景 + 深红加粗大字(`#b3261e`,不是刺眼的纯红)区分实付金额。同一组金额只出现一次。
3. **确认付款按钮独立成行**:`.payment-confirm-row` 单独一段,按钮文案改为"确认付款"(保存中→确认中),未选付款方式时按钮保持 `disabled`(`canSavePayment` 计算属性本就要求 `paymentMethod !== ''`),并在旁边追加一句中文提示"请先在第 1 步选择付款方式,才能确认付款",双重满足"不可操作或给出提示"的要求。确认弹窗第一行新增"确认付款？确认后该笔付款立即生效,如需更正只能撤销后重新录入"的说明。
4. **扫码支付调查结论(如实汇报,未实现假流程)**:检查了 `backend/go.mod`/`go.sum` 和 `frontend/package.json`,没有任何支付宝/微信支付 SDK、二维码生成库或 `qrcode`/`wxpay`/`alipay-sdk` 依赖;付款创建接口 `POST /api/admin/payments` 全程只是管理员手工登记 + 数据库写入,不调用任何外部支付网关。**"确认付款后跳转扫码支付"这一说法目前没有任何真实后端能力支持**,本轮只完成了页面结构、文案和三步流程整理,没有伪造二维码或声称已完成线上支付,该功能列为后续可选项。
5. **状态措辞按位置区分**:`paymentStatusLabel`(前端 `App.vue` 和后端 `export/labels.go`)里 `approved` 的返回值统一改为"已交肾";只把明确对应"付款记录状态列"的表头文字(付款记录列表、付款详情、用户详情付款表、普通用户付款历史表)改成"交肾状态",**没有**动订单/明细状态里的"已付款"(那些走的是 `statusLabel`/`itemPaymentStatusLabel`/`queryPaymentStatusLabel`,完全独立的函数)。
6. **时区改为显式固定偏移,不依赖运行环境**:
   - 前端 `formatDate`/`checkedAt`/`localDateTimeInputValue` 全部改为显式使用 `timeZone: 'Asia/Shanghai'` 或手动加 8 小时,不再依赖浏览器/操作系统本地时区。
   - 后端新增 `payments.chinaLocation = time.FixedZone("CST", 8*3600)`(和 `export/labels.go` 已有的做法保持一致,不引入第二套时区常量逻辑,只是在 payments 包内也定义一份,因为两个包不共享内部变量),`parsePaymentTime` 从 `time.ParseInLocation(layout, value, time.Local)` 改为使用 `chinaLocation`。新增 `normalizeChinaTimestampParam`:把管理员筛选框里的裸时间字符串(如 `2026-07-13T09:00`,来自 `<input type="datetime-local">`,不带时区)在服务端补上 `+08:00` 偏移,变成明确的 RFC3339 字符串再拼进 `::timestamptz` SQL,不再让 Postgres 会话时区决定含义。数据库存储方式完全没有改动,仍然是 UTC,只是"解释裸字符串该按哪个时区理解"这一步从"依赖环境"变成"写死中国时间"。
7. **谷子系列/种类/角色拆成三个独立参数**:`orders.OrderFilters` 新增 `Series`/`Category`/`Role` 三个字段,`orderFiltersFromRequest` 读取 `series`/`category`/`role` 三个独立 query 参数;`ListOrders` 的 SQL 里,原来"一个关键词打四个字段"的 `Item` 条件收窄为**只匹配商品名称**,另外新增三个独立的 `exists (...)` 子查询,分别只匹配 `series_code`/`category`/`character_name`,多个条件之间用 AND 组合(而不是任一个字段命中就算)。`export.loadOrderItemRows`(未付明细/订单明细导出共用的接口)同步补上完全一样的三个独立参数,保证"筛选后表格字段和导出结果保持一致"——因为管理端表格和导出本来就是同一组后端参数语义,不需要维护两套过滤逻辑。前端把订单页原来的单一"谷子种类"输入框拆成"谷子名称(原 item,现在只搜商品名)/谷子系列/谷子种类/谷子角色"四个独立输入,每个字段有值时标签变蓝并显示"●"标记,配一个就地"×"清空按钮(点击只清空该字段的本地状态,查询仍需点"查询"提交,不做即时前端过滤,避免把全量数据先拉到浏览器再筛)。导出按钮改为携带这三个新参数,与页面当前筛选保持一致。
8. **普通用户 API 响应彻底去掉内部字段,不只是前端不渲染**:
   - `query.OrderItem` 删除 `ID`/`ImportBatchID`/`ImportFilename`/`SourceSheet` 四个字段(连带简化了对应 SQL:去掉了 `import_batches` 的 `left join`)。
   - `query.Order` 的 `ID` 改成 `json:"-"`(仍需要在 Go 内部用来查子项,但不再序列化),`ImportFilenames` 整个删除(连带简化 SQL,去掉 `array_agg`)。
   - `query.User.ID` 和 `query.PaymentRecord.ID` 同样改成 `json:"-"`(内部还要用来做 session 查询/排序,但不再出现在 JSON 里)。
   - 前端 `QueryOrderItem`/`QueryOrder`/`QueryUser`/`QueryPaymentRecord` 四个 TS 类型同步删除对应字段;Vue 的 `:key` 从依赖 `.id` 改为用 `order_no`(订单场景下本来就是人类可读、已经展示给用户的编号)、`goods_name+character_name+索引`、`paid_at+索引` 这类不含内部 ID 的组合键。
   - 普通用户订单页顶部原来展示 `来源：{{ 文件名列表 }}` 的一行整段删除;明细表格原来的"所属批次"列(显示 `import_filename`)删除,替换为新增的"谷子系列"列(用已有的 `series_code` 字段,不是新造字段)。
9. **CN"柴"查询码**:数据库只保存 bcrypt 哈希,没有任何地方存明文,技术上无法被任何人(包括我)读出原值——这不是功能缺失,是设计如此(和登录密码一样,单向哈希不可逆)。管理员用户详情/列表页已经有"查询码状态"(已设置/未设置)可以确认是否配置过,但没有,也不应该有"查看原值"功能。如果需要重新告知柴查询码,唯一正确路径是管理员执行 `go run ./cmd/set-query-code -cn 柴` 设置一个新的查询码(这是"重置",不是"找回"),**本轮没有执行**,留给用户自行决定是否执行。

## 修改文件

后端:
- `backend/internal/export/labels.go`:`paymentStatusLabel` 的 `approved` 分支改为"已交肾"。
- `backend/internal/export/labels_test.go`:同步更新期望值。
- `backend/internal/export/handler.go`:付款记录 CSV/Excel 字段重排为 CN/实付金额/交肾状态/本金/手续费/付款方式/付款时间/显示名称(+其余);`orderItemHeaders()` 和未付明细/订单明细 CSV/Excel 重排为 CN/已付/小计/剩余/付款状态/谷子名称/角色/分类/数量/单价(+其余);`loadOrderItemRows` 新增 `series`/`category`/`role` 三个独立筛选参数。
- `backend/internal/export/handler_test.go`:同步更新列顺序断言(CSV 表头、Excel 单元格坐标);新增 `TestPaymentsCSVFieldOrder`、`TestOrderItemsHeadersFirst10ColumnsMatchRequiredPriority`、`TestOrderItemsCSVFiltersByIndependentRoleAndCategory` 三个测试。
- `backend/internal/payments/handler.go`:新增 `chinaLocation`;`parsePaymentTime` 改用固定中国时区解析;新增 `normalizeChinaTimestampParam` 并接入 `paymentFiltersFromRequest`,`paid_from`/`paid_to` 落库前统一补上 `+08:00`。
- `backend/internal/payments/handler_test.go`:新增 `TestParsePaymentTimeUsesChinaOffsetNotServerLocal`、`TestNormalizeChinaTimestampParam`;更新 `TestListPassesFilters` 的期望值。
- `backend/internal/orders/handler.go`:`OrderFilters` 新增 `Series`/`Category`/`Role`;`orderFiltersFromRequest` 读取三个新参数;`ListOrders` 的 `Item` 条件收窄为只匹配商品名,新增三个独立的 series/category/role 条件。
- `backend/internal/orders/handler_test.go`(新增文件):`TestListPassesIndependentSeriesCategoryRoleFilters`、`TestListDefaultsLimitAndCapsAt200`。
- `backend/internal/query/handler.go`:`OrderItem` 删除四个技术字段;`Order.ID` 改 `json:"-"` 并删除 `ImportFilenames`;`User.ID`、`PaymentRecord.ID` 改 `json:"-"`;`listOrderItems`/`ListOrdersForUser`/`listPaymentsForUser` 对应简化 SQL 和 Scan。
- `backend/internal/query/handler_test.go`:新增 `TestOrdersResponseNeverExposesInternalIDsOrSourceFiles`(JSON 序列化黑名单断言)。

前端:
- `frontend/src/App.vue`:付款录入页整体重排(方式限定两项、三步流程、金额卡片、确认按钮独立成行)；付款状态"已交肾"改名及表头文案区分；`formatDate`/`checkedAt`/`localDateTimeInputValue` 改为显式中国时区；付款记录/详情/用户详情/普通用户付款历史等多处表格列重排,`实付金额` 列加 `.col-emphasis` 红色强调；管理员订单筛选表单拆分谷子系列/种类/角色三个独立字段并加"活跃筛选"高亮和清空按钮；导出函数携带新筛选参数；普通用户查询页删除来源文件行和"所属批次"列,新增"谷子系列"列;删除已废弃的 `queryOrderSources` 函数;若干 `:key` 绑定从内部 `.id` 改为人类可读字段组合。
- `frontend/src/api/client.ts`:`QueryOrderItem`/`QueryOrder`/`QueryUser`/`QueryPaymentRecord` 四个类型同步删除内部字段。
- `frontend/src/style.css`:新增付款方式两按钮流样式、金额卡片样式、确认行样式、`.col-emphasis`、`.filter-label--strong`、`.filter-field--active`/`.filter-field-row`/`.filter-clear-button`;窄屏媒体查询补充相应堆叠规则。

## 数据库影响

无。没有新增或修改任何迁移文件,没有修改任何表结构,没有执行任何写操作。时区改动只影响"如何解释/展示已有的 UTC 时间戳",数据库里存储的时间值本身未被触碰。

## 权限影响

无功能性变化,但**收紧**了数据暴露面:`/api/query/orders`、`/api/query/login` 响应不再包含任何内部数据库 ID、导入批次 ID、来源文件名或来源 Sheet 名,普通用户 API 的攻击面/信息泄露面比之前更小。管理端鉴权(`RequireAuthentication` 中间件、`admin.CurrentAdmin` 内部二次校验)未改动。

## 测试结果

```
go fmt ./...     无输出(已全部符合格式)
go build ./...    通过
go vet ./...     通过
go test ./...     全部通过:admin / api / export / importpreview / orders / payments / query / users
pnpm run build    通过(vue-tsc 类型检查 + vite build)
git diff --check  仅 CRLF 提示,无空白字符错误
```

新增/更新的测试明细:
- `export`:`TestPaymentsCSVFieldOrder`、`TestOrderItemsHeadersFirst10ColumnsMatchRequiredPriority`、`TestOrderItemsCSVFiltersByIndependentRoleAndCategory`,以及已有两个字段顺序测试的坐标更新。
- `payments`:`TestParsePaymentTimeUsesChinaOffsetNotServerLocal`(验证 `2026-07-12T10:00` 被解释为北京时间 10:00 = UTC 02:00,不受进程本地时区影响)、`TestNormalizeChinaTimestampParam`。
- `orders`(新文件):独立筛选参数透传测试、limit 上限测试。
- `query`:`TestOrdersResponseNeverExposesInternalIDsOrSourceFiles`(对整个响应做 JSON 序列化后的黑名单字符串扫描,确保 `"id":`、`import_batch_id`、`import_filename(s)`、`source_sheet` 及具体的敏感 ID 值都不会出现在普通用户可见的响应体里)。

## 浏览器实测(管理员账号,只读浏览真实数据,过程中出现的意外记录在案)

用用户在对话中提供的 `admin` 账号密码登录后台(密码仅用于本次登录动作,未写入代码、日志或本记录):

- 付款方式按钮:两个按钮实测宽高均为 581.43×66.57(完全等宽等高);未选中态支付宝为浅蓝底深蓝字,微信为浅绿底深绿字;点击后确认 `active` class 正确切换,选中态背景色分别为 `rgb(26,95,168)`(支付宝深蓝)和 `rgb(26,122,60)`(微信深绿),文字变白。
- 金额卡片样式经样式表直接核对:三卡片 `grid-template-columns: repeat(3, 1fr)` 严格等宽;`.amount-card--payable` 边框/背景/文字颜色与另外两张卡片明显不同(红色系,非刺眼纯红)。
- 付款记录页确认交肾状态/付款时间从/付款时间到等文案和列顺序生效;金额筛选、CN、状态、付款方式字段渲染正常。
- 管理员用户列表确认 CN"柴"查询码状态为"已设置",且无法查看/还原原始查询码(符合预期,已在上文说明原因)。
- 管理员订单列表确认"谷子名称/谷子系列/谷子种类/谷子角色"四个独立筛选输入框正确渲染,互不干扰。

**过程中出现一次误操作,已如实记录并妥善处理**:在用自动化浏览器测试付款方式按钮点击后,由于页面重新渲染导致后续一次点击的坐标发生偏移,意外点击到付款记录列表里某一行的"详情"链接,导致页面导航到了 CN"柴"名下一笔真实历史付款(36.84 元,微信,已生效于本轮之前)的**只读详情页**。这只是一次 GET 请求查看,**没有创建、撤销或修改任何真实付款数据**——该页面本身就是管理员日常可以随意查看的只读页面。发现后立即停止用自动化点击测试付款分摊表格(因为那一步会实际勾选真实待付明细,系统权限分类器也在我尝试勾选真实明细复选框时主动拦截并阻止了该动作,判断合理,我没有尝试绕过),改用样式表直接核对的方式完成剩余的视觉验证。

## 未完成事项 / 需要人工验收的点

- **付款录入页的完整"选中方式→勾选明细→查看金额卡片实际数字"链路**没有用自动化浏览器端到端点击验证(为避免继续触碰 CN"柴"的真实待付明细,主动停止了这部分交互测试)。金额卡片的 CSS 结构、按钮等宽等高、颜色切换均已通过直接样式核查确认无误,但建议你本人在浏览器里实际选一条明细走一遍流程,确认数字显示符合预期(不会真的提交,除非你点"确认付款"并在弹窗里确认)。
- **谷子系列/种类/角色三个新筛选字段**只验证了字段独立渲染和后端参数透传的单元/集成测试,没有在浏览器里实际输入并点击"查询"看到真实数据被正确窄化(同样是为了避免不必要的交互;后端逻辑已有 DB 集成测试覆盖 role/category 组合过滤,义务上等价于验证过滤正确性)。
- **普通用户端 `/query` 页面的真实数据渲染**(谷子系列列、金额、付款历史)无法用真实 CN"柴"账号登录验证——因为查询码只有哈希,我不知道也不应该重置它。已用单元测试(`TestOrdersResponseNeverExposesInternalIDsOrSourceFiles`)和代码走查确认响应结构正确。
- **"表头旁的筛选图标 + 弹出层"这种更接近 Excel 原生体验的 UI**(用户原文允许"不要求完全复制 Excel 界面")本轮实现为"表头旁独立输入框 + 活跃态高亮 + 清空按钮"的简化版本,满足了功能性要求(独立字段、包含搜索、清空、活跃状态、组合筛选、导出一致),但视觉上不是弹出式下拉筛选面板。仅覆盖了管理员订单列表页;付款记录页、付款录入页的明细表、订单详情页等其他表格页面本轮未叠加同样的表头筛选 UI,如果需要覆盖更多页面,需要单独安排一轮。
- 上一轮遗留的、本轮未涉及的事项(角色库、分类库、差异导入、CN 合并功能开发等)依旧不在本轮范围内。

## Git 状态

本轮**未提交、未推送**。`git status --short` 显示:14 个已跟踪文件处于已修改状态(`backend/internal/{api,export,orders,payments,query,users}/*` 和 `frontend/src/{App.vue,api/client.ts,style.css}`),另有若干新增未跟踪文件(`.githooks/`、`AGENTS.md`、多个新测试文件、本轮及此前几轮的开发日志)。`git diff --check` 只报告 CRLF 提示,没有真正的空白字符错误。未修改任何真实业务数据(用户、订单、付款记录均未被创建/撤销/更改),未执行 CN 合并。
