import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')

const appSource = read('src/App.vue')
const valueFilterSource = read('src/components/ColumnValueFilter.vue')
const rangeFilterSource = read('src/components/ColumnRangeFilter.vue')
const dateFilterSource = read('src/components/ColumnDateFilter.vue')
const filterButtonSource = read('src/components/ColumnFilterButton.vue')

// The /admin/orders template only. Scoping matters: other pages legitimately
// still have their own 高级筛选 block, and asserting against the whole file
// would either fail on them or quietly pass on the wrong section.
const ordersTemplate = (() => {
  const start = appSource.indexOf(`routeName === 'admin-orders'`)
  const end = appSource.indexOf(`routeName === 'admin-order-detail'`)
  assert.ok(start > 0 && end > start, 'could not locate the /admin/orders template block')
  return appSource.slice(start, end)
})()

test('每个可筛选表头都有漏斗筛选入口，操作列没有', () => {
  for (const [label, column] of [
    ['CN', 'cn'],
    ['谷子名称', 'item'],
    ['系列', 'series'],
    ['团名', 'group'],
    ['谷子角色', 'role'],
    ['订单状态', 'status'],
    ['付款状态', 'payment_status'],
  ]) {
    assert.match(
      ordersTemplate,
      new RegExp(`<ColumnValueFilter[^>]*label="${label}"[^>]*column="${column}"`),
      `${label} 列缺少值筛选入口`,
    )
  }

  // 数量与金额列用范围筛选，创建时间用日期筛选。
  for (const label of ['数量', '明细总金额', '已付金额', '未付金额']) {
    assert.match(ordersTemplate, new RegExp(`<ColumnRangeFilter[^>]*label="${label}"`), `${label} 列缺少范围筛选`)
  }
  assert.match(ordersTemplate, /<ColumnDateFilter[^>]*label="创建时间"/)

  // 单价与查看详情列不设漏斗。
  assert.match(ordersTemplate, /<span class="column-header__label">单价<\/span>/)
  assert.match(ordersTemplate, /<span class="column-header__label">查看详情<\/span>/)
  assert.doesNotMatch(ordersTemplate, /label="单价"[^>]*:load-facets/)
  assert.doesNotMatch(ordersTemplate, /<ColumnRangeFilter[^>]*label="单价"/)
})

test('漏斗按钮显示在表头内部，且带已筛选状态', () => {
  // 漏斗是表头的一部分（column-header 内），不是独立的大按钮。
  assert.match(filterButtonSource, /<span class="column-header">/)
  assert.match(filterButtonSource, /class="column-filter-button"/)
  assert.match(filterButtonSource, /:class="\{ 'is-active': props\.active \}"/)
  assert.match(filterButtonSource, /:data-filtered="props\.active \? 'true' : 'false'"/)
  // 已启用筛选的视觉状态。
  const css = read('src/style.css')
  assert.match(css, /\.column-filter-button\.is-active/)
})

test('浮层可打开、可通过取消/外部点击/Esc 关闭', () => {
  assert.match(filterButtonSource, /function openPopover\(\)/)
  // 外部点击关闭。
  assert.match(filterButtonSource, /document\.addEventListener\('mousedown', onPointerDown, true\)/)
  assert.match(filterButtonSource, /if \(popoverRef\.value\?\.contains\(target\) \|\| buttonRef\.value\?\.contains\(target\)\) return\s*\n\s*cancel\(\)/)
  // Esc 关闭。
  assert.match(filterButtonSource, /if \(event\.key === 'Escape'\) cancel\(\)/)
  // 卸载时移除监听，避免泄漏。
  assert.match(filterButtonSource, /onBeforeUnmount\(\(\) => \{/)
  assert.match(filterButtonSource, /document\.removeEventListener\('mousedown', onPointerDown, true\)/)
})

test('浮层不超出视口：两轴夹紧并在下方空间不足时上翻', () => {
  assert.match(filterButtonSource, /const maxLeft = window\.innerWidth - width - VIEWPORT_MARGIN/)
  assert.match(filterButtonSource, /if \(left < VIEWPORT_MARGIN\) left = VIEWPORT_MARGIN/)
  assert.match(filterButtonSource, /top \+ height > window\.innerHeight - VIEWPORT_MARGIN/)
  // 窗口尺寸变化与容器滚动时重新定位。
  assert.match(filterButtonSource, /window\.addEventListener\('resize', reposition\)/)
  assert.match(filterButtonSource, /window\.addEventListener\('scroll', reposition, true\)/)
})

test('键盘可聚焦主要控件', () => {
  assert.match(filterButtonSource, /querySelector<HTMLElement>\(\s*'input, button, select, \[tabindex\]:not\(\[tabindex="-1"\]\)',\s*\)/)
  assert.match(filterButtonSource, /focusable\?\.focus\(\)/)
  // 关闭后焦点回到漏斗按钮。
  assert.match(filterButtonSource, /buttonRef\.value\?\.focus\(\)/)
})

test('值筛选浮层包含搜索、全选、候选数量与空白值', () => {
  // 搜索框（带防抖）。
  assert.match(valueFilterSource, /type="search"/)
  assert.match(valueFilterSource, /searchTimer = setTimeout\(\(\) => void fetchPage\(1, false\), 250\)/)
  // 全选（含半选状态）。
  assert.match(valueFilterSource, /function toggleAll\(\)/)
  assert.match(valueFilterSource, /:indeterminate\.prop="someLoadedSelected"/)
  assert.match(valueFilterSource, /全选/)
  // 每个候选值显示数量。
  assert.match(valueFilterSource, /column-filter-option__count">\{\{ candidate\.count \}\}/)
  // 空白值作为候选项（后端以 blank 标记，标签为 (空白)）。
  assert.match(valueFilterSource, /:data-blank="candidate\.blank \? 'true' : 'false'"/)
  assert.match(valueFilterSource, /'is-blank': candidate\.blank/)
})

test('全选与取消全选作用于已加载候选值', () => {
  // 已全选时再点击 = 取消全选（移除这些值），否则并入。
  assert.match(valueFilterSource, /if \(allLoadedSelected\.value\) \{\s*\n\s*draft\.value = draft\.value\.filter\(\(value\) => !loaded\.includes\(value\)\)/)
  assert.match(valueFilterSource, /draft\.value = \[\.\.\.new Set\(\[\.\.\.draft\.value, \.\.\.loaded\]\)\]/)
})

test('多选：切换单个值不影响其他已选值', () => {
  assert.match(valueFilterSource, /function toggleValue\(value: string\)/)
  assert.match(valueFilterSource, /if \(index === -1\) draft\.value = \[\.\.\.draft\.value, value\]/)
  assert.match(valueFilterSource, /else draft\.value = draft\.value\.filter\(\(candidate\) => candidate !== value\)/)
})

test('取消不生效、确定后才生效：草稿只在确定时提交', () => {
  for (const source of [valueFilterSource, rangeFilterSource, dateFilterSource]) {
    // 打开时把已应用值拷贝进草稿。
    assert.match(source, /function onOpen\(\)/)
    // 只有 confirm 触发 update:modelValue。
    const emits = source.match(/emit\('update:modelValue'/g) ?? []
    assert.equal(emits.length, 1, '只应有 confirm 一处提交筛选')
    assert.match(source, /function confirm\(close: \(\) => void\)/)
    // 取消直接走浮层的 cancel，不提交。
    assert.match(source, /@click="cancel"/)
    assert.match(source, /@click="confirm\(close\)"/)
  }
  // 草稿本身不与 modelValue 双向绑定。
  assert.match(valueFilterSource, /const draft = ref<string\[\]>\(\[\]\)/)
  assert.match(valueFilterSource, /draft\.value = \[\.\.\.props\.modelValue\]/)
})

test('数字输入确认时先转字符串，避免浏览器 number 值调用 trim 报错', () => {
  assert.match(rangeFilterSource, /String\(draft\.value\.min \?\? ''\)\.trim\(\)/)
  assert.match(rangeFilterSource, /String\(draft\.value\.max \?\? ''\)\.trim\(\)/)
})

test('候选加载中/无结果/接口失败可重试三种状态齐备', () => {
  assert.match(valueFilterSource, /候选值加载中…/)
  assert.match(valueFilterSource, /没有符合条件的候选值。/)
  assert.match(valueFilterSource, /errorMessage/)
  assert.match(valueFilterSource, /@click="retry"/)
  assert.match(valueFilterSource, />重试</)
  // 候选值分页，避免 CN 很多时一次拉全。
  assert.match(valueFilterSource, /加载更多候选值/)
  assert.match(valueFilterSource, /void fetchPage\(page\.value \+ 1, true\)/)
})

test('订单页顶部三行：标题与导出操作分行', () => {
  const heading = ordersTemplate.indexOf('page-heading')
  const actions = ordersTemplate.indexOf('page-actions')
  const resultbar = ordersTemplate.indexOf('page-resultbar')
  assert.ok(heading > 0 && actions > heading && resultbar > actions, '顶部应为 标题 / 操作 / 结果 三行')

  // 标题行内不得出现导出按钮。
  const headingBlock = ordersTemplate.slice(heading, actions)
  assert.match(headingBlock, /订单只读查询/)
  assert.doesNotMatch(headingBlock, /导出明细 Excel/)
  assert.doesNotMatch(headingBlock, /exportOrderItems/)

  // 操作行含导出与刷新。
  const actionsBlock = ordersTemplate.slice(actions, resultbar)
  assert.match(actionsBlock, /导出明细 Excel/)
  assert.match(actionsBlock, /CSV/)
  assert.match(actionsBlock, /刷新/)

  // 结果行含结果条数、分页信息与清空全部筛选。
  assert.match(ordersTemplate, /结果：共 \{\{ orderTotal \}\} 项谷子明细/)
  assert.match(ordersTemplate, /清空全部筛选/)
})

test('旧的顶部筛选表单与高级筛选已从订单页移除', () => {
  assert.doesNotMatch(ordersTemplate, /高级筛选/)
  assert.doesNotMatch(ordersTemplate, /filter-advanced/)
  assert.doesNotMatch(ordersTemplate, /filter-toolbar/)
  assert.doesNotMatch(ordersTemplate, /filter-technical/)
  assert.doesNotMatch(ordersTemplate, /<form/)
  // 旧的查询按钮与逐个输入框。
  assert.doesNotMatch(ordersTemplate, /查询中' : '查询/)
  assert.doesNotMatch(ordersTemplate, /placeholder="CN 或显示名"/)
  assert.doesNotMatch(ordersTemplate, /placeholder="商品名称"/)
  assert.doesNotMatch(ordersTemplate, /orderFilters\./)
  assert.doesNotMatch(ordersTemplate, /orderAdvancedOpen/)
  // 旧状态本身也已删除。
  assert.doesNotMatch(appSource, /const orderAdvancedOpen = ref/)
})

test('订单页不再出现导入批次 ID 等技术标识', () => {
  assert.doesNotMatch(ordersTemplate, /导入批次 ID/)
  assert.doesNotMatch(ordersTemplate, /import_batch_id/)
  assert.doesNotMatch(ordersTemplate, /importBatchID/)
  assert.doesNotMatch(ordersTemplate, /order\.sku/)
  assert.doesNotMatch(ordersTemplate, /file_hash/)
  // order_id 只用于 key 与详情导航，不作为可见列渲染。
  assert.doesNotMatch(ordersTemplate, /\{\{ item\.order_id \}\}/)
  assert.doesNotMatch(ordersTemplate, /\{\{ item\.item_id \}\}/)
})

test('订单详情的技术标识区位于底部、默认折叠并标注仅供技术排查', () => {
  const detailStart = appSource.indexOf(`routeName === 'admin-order-detail'`)
  const detailBlock = appSource.slice(detailStart)
  const technical = detailBlock.indexOf('orderDetailTechnicalIdentifiers(orderDetail).length > 0')
  assert.ok(technical > 0, '订单详情应保留技术标识区')
  // 默认折叠：<details> 且不带 open。
  assert.match(detailBlock.slice(technical, technical + 400), /<details>/)
  assert.doesNotMatch(detailBlock.slice(technical, technical + 400), /<details open/)
  assert.match(detailBlock.slice(technical, technical + 600), /仅供技术排查/)
})

test('筛选、分页与导出共用同一份筛选参数', () => {
  // 唯一的参数构造入口。
  assert.match(appSource, /function orderFilterParams\(\)\s*\{\s*\n\s*return buildFilterParams\(orderFilterState\.value, ORDER_RANGE_PARAMS, ORDER_DATE_PARAMS\)/)
  // 列表带上分页。
  assert.match(appSource, /params\.set\('page', String\(orderPage\.value\)\)/)
  assert.match(appSource, /params\.set\('page_size', String\(orderPageSize\.value\)\)/)
  // facets 带上当前筛选与列名。
  assert.match(appSource, /params\.set\('column', request\.column\)/)
  assert.match(appSource, /params\.set\('facet_page', String\(request\.page\)\)/)
  // 导出带完整筛选、且不带分页参数。
  assert.match(appSource, /void downloadExport\('\/api\/admin\/export\/order-items\.xlsx', orderFilterParams\(\)\)/)
  assert.match(appSource, /void downloadExport\('\/api\/admin\/export\/order-items\.csv', orderFilterParams\(\)\)/)
})

test('多值与空白筛选使用无歧义 query 参数', () => {
  const filterStateSource = read('src/filters/columnFilters.ts')
  assert.match(filterStateSource, /params\.append\(column, trimmed\)/)
  assert.match(filterStateSource, /params\.set\(`\$\{column\}_blank`, '1'\)/)
  assert.match(filterStateSource, /if \(trimmed !== ''\)/)
})

test('翻页保留筛选，改筛选回到第一页', () => {
  // 翻页只改页码后重新加载，不触碰筛选状态。
  assert.match(appSource, /function goToOrderPage\(page: number\)\s*\{[\s\S]*?orderPage\.value = page\s*\n\s*void loadOrders\(\)/)
  assert.doesNotMatch(
    appSource.slice(appSource.indexOf('function goToOrderPage'), appSource.indexOf('function goToOrderPage') + 300),
    /clearAllFilters|orderFilterState\.value =/,
  )
  // 改筛选回到第 1 页，避免停在已不存在的页码上。
  assert.match(appSource, /function applyOrderFilters\(\)\s*\{\s*\n\s*orderPage\.value = 1\s*\n\s*void loadOrders\(\)/)
  assert.match(ordersTemplate, /@update:model-value="applyOrderFilters"/)
})

test('清空全部筛选一次性重置所有列', () => {
  assert.match(appSource, /function resetOrderFilters\(\)\s*\{\s*\n\s*clearColumnFilters\(orderFilterState\.value\)\s*\n\s*applyOrderFilters\(\)/)
  assert.match(ordersTemplate, /:disabled="orderActiveFilterCount === 0"/)
})

test('列表分页与总数来自后端响应', () => {
  assert.match(appSource, /orderTotal\.value = response\.total/)
  assert.match(appSource, /orderTotalPages\.value = response\.total_pages/)
  // 用的是筛选后的明细总数，而不是当前页行数。
  assert.match(ordersTemplate, /结果：共 \{\{ orderTotal \}\} 项谷子明细/)
  assert.doesNotMatch(ordersTemplate, /结果：共 \{\{ orderItems\.length \}\}/)
})

test('可修改每页条数且会回到第一页', () => {
  assert.match(ordersTemplate, /v-model\.number="orderPageSize"/)
  assert.match(ordersTemplate, /@change="changeOrderPageSize"/)
  assert.match(appSource, /function changeOrderPageSize\(\)\s*\{\s*\n\s*orderPage\.value = 1\s*\n\s*void loadOrders\(\)/)
  for (const size of [25, 50, 100, 200]) assert.match(ordersTemplate, new RegExp(`:value="${size}"`))
})

test('facets 响应包含独立空白数量，失败提示为中文', () => {
  const clientSource = read('src/api/client.ts')
  assert.match(clientSource, /blank_count: number/)
  assert.match(valueFilterSource, /候选值加载失败：/)
  assert.match(appSource, /订单列表加载失败：/)
  assert.match(valueFilterSource, /response\.values\.filter\(\(candidate\) => !candidate\.blank\)/)
  assert.match(valueFilterSource, /response\.blank_count > 0/)
  assert.match(valueFilterSource, /value: '', label: '\(空白\)', count: response\.blank_count, blank: true/)
})

test('表格与页面滚动：只有表格容器内部横向滚动', () => {
  const css = read('src/style.css')
  assert.match(ordersTemplate, /class="table-scroll history-table order-table"/)
  assert.match(css, /\.order-table table \{ min-width: 1100px; \}/)
  assert.match(css, /\.table-scroll \{[\s\S]*?max-width: 100%;[\s\S]*?overflow: auto;/)
  assert.match(css, /html,[\s\S]*?body,[\s\S]*?#app \{[\s\S]*?max-width: 100%;[\s\S]*?overflow-x: clip;/)
})

test('金额列统一两位小数并使用等宽数字', () => {
  for (const field of ['unit_price', 'total_amount', 'paid_amount', 'unpaid_amount']) {
    assert.match(ordersTemplate, new RegExp(`formatMoney\\(item\\.${field}\\)`), `${field} 应走统一金额格式`)
  }
  assert.match(appSource, /function formatMoney\(value: number \| null \| undefined\) \{\s*\n\s*return Number\(value \?\? 0\)\.toFixed\(2\)/)
  const css = read('src/style.css')
  assert.match(css, /\.numeric-column \{ font-variant-numeric: tabular-nums; \}/)
})

test('状态列显示中文', () => {
  assert.match(ordersTemplate, /statusLabel\(item\.status\)/)
  assert.match(ordersTemplate, /queryPaymentStatusLabel\(item\.payment_status\)/)
})

test('长名称正常换行，不用省略号掩盖字段', () => {
  // 谷子名称/项目名称/用户名称换行显示，而不是截断成模糊省略号。
  for (const field of ['item_name', 'series_code', 'character_name']) {
    assert.match(ordersTemplate, new RegExp(`<span class="cell-wrap">\\{\\{ item\\.${field}`), `${field} 应换行展示`)
  }
  const css = read('src/style.css')
  assert.match(css, /\.cell-wrap \{[\s\S]*?white-space: normal;[\s\S]*?overflow-wrap: anywhere;/)
})

test('移动端浮层改为底部弹层且操作按钮始终可见', () => {
  const css = read('src/style.css')
  assert.match(css, /@media \(max-width: 560px\) \{[\s\S]*?\.column-filter-popover\.is-mobile/)
  assert.match(css, /max-width: calc\(100vw - 16px\)/)
  // 取消/确定 sticky 在底部。
  assert.match(css, /\.column-filter-popover\.is-mobile \.column-filter-actions \{\s*\n\s*position: sticky;/)
  assert.match(filterButtonSource, /const MOBILE_BREAKPOINT = 560/)
  assert.match(filterButtonSource, /mobile\.value = window\.innerWidth <= MOBILE_BREAKPOINT/)
})

test('表头文字与漏斗垂直居中，字号字重统一', () => {
  const css = read('src/style.css')
  assert.match(css, /\.column-header \{[\s\S]*?display: inline-flex;[\s\S]*?align-items: center;/)
  assert.match(css, /\.column-header__label \{[\s\S]*?font-size: var\(--fs-label\);[\s\S]*?font-weight: 700;/)
})

// ===== 阶段 4F：明细行粒度 =====

test('订单表一行 = 一项谷子明细，字段全部单值', () => {
  // 每行按明细 id 作 key，逐个字段都是单值，没有数组或拼接。
  assert.match(ordersTemplate, /v-for="\(item, index\) in orderItems"/)
  assert.match(ordersTemplate, /:key="item\.item_id"/)
  for (const field of ['cn_code', 'item_name', 'series_code', 'character_name']) {
    assert.match(ordersTemplate, new RegExp(`item\.${field}`), `${field} 应作为单值列渲染`)
  }
})

test('订单表不再出现任何聚合渲染逻辑', () => {
  // 旧的数组字段与拼接函数彻底消失——它们正是"筛角色仍显示其他角色"的来源。
  for (const aggregate of ['item_names', 'series_codes', 'categories', 'character_names', 'joinColumnValues']) {
    assert.doesNotMatch(ordersTemplate, new RegExp(aggregate), `模板不应再有聚合字段 ${aggregate}`)
  }
  // 辅助函数本身也已删除，不留下二次拼接的入口。
  assert.doesNotMatch(appSource, /function joinColumnValues/)
  assert.doesNotMatch(ordersTemplate, /\.join\(/)
  // 前端不得对聚合字符串做拆分筛选。
  assert.doesNotMatch(ordersTemplate, /\.split\(/)
})

test('表头包含数量、单价、明细总金额、已付、未付', () => {
  for (const label of ['数量', '单价', '明细总金额', '已付金额', '未付金额']) {
    assert.match(ordersTemplate, new RegExp(`>${label}<|label="${label}"`), `表头缺少 ${label}`)
  }
  // 数量与金额等宽数字。
  const css = read('src/style.css')
  assert.match(css, /\.numeric-column \{ font-variant-numeric: tabular-nums; \}/)
})

test('结果文案按谷子明细计数', () => {
  assert.match(ordersTemplate, /结果：共 \{\{ orderTotal \}\} 项谷子明细/)
  assert.match(ordersTemplate, /没有符合当前筛选条件的谷子明细。/)
})

test('同一 CN 多行不用 rowspan 合并，只做视觉分组', () => {
  // 合并单元格会破坏筛选后的行展示、分页与移动端，因此明确不允许。
  // 只禁属性用法，注释里说明"不使用 rowspan"是允许的。
  assert.doesNotMatch(ordersTemplate, /:?rowspan=/)
  assert.match(ordersTemplate, /'order-row--alt': isAlternateOrderRow\(index\)/)
  assert.match(appSource, /function isAlternateOrderRow\(index: number\)/)
  const css = read('src/style.css')
  assert.match(css, /\.order-row--alt td \{ background: #fafbfc; \}/)
})

test('角色等列仍使用 WPS 表头筛选组件', () => {
  for (const column of ['cn', 'item', 'series', 'role', 'status', 'payment_status']) {
    assert.match(ordersTemplate, new RegExp(`<ColumnValueFilter[^>]*column="${column}"`), `${column} 应仍走 WPS 值筛选组件`)
  }
})

test('每行详情入口指向所属订单', () => {
  assert.match(ordersTemplate, /navigate\(`\/admin\/orders\/\$\{item\.order_id\}`\)/)
})

test('技术字段仍不在主表', () => {
  for (const technical of ['import_batch_id', 'sku', 'file_hash', 'source_row_key', 'source_sheet', 'order_no']) {
    assert.doesNotMatch(ordersTemplate, new RegExp(`item\.${technical}`), `主表不应出现技术字段 ${technical}`)
  }
})

test('列表 DTO 为单值明细行，且带 item_id/order_id', () => {
  const client = read('src/api/client.ts')
  const dto = client.slice(client.indexOf('export type OrderListItem'), client.indexOf('export type OrderListResponse'))
  for (const field of ['item_id: string', 'order_id: string', 'item_name: string', 'series_code: string', 'category: string', 'character_name: string', 'quantity: number', 'unit_price: number']) {
    assert.ok(dto.includes(field), `OrderListItem 缺少 ${field}`)
  }
  // 不再有任何数组字段。
  assert.doesNotMatch(dto, /string\[\]/)
})

test('普通用户阶段 3 页面契约不变', () => {
  // 管理员 DTO 变更不得波及普通用户"我的订单"。
  assert.match(appSource, /const queryOrderFilters = ref\(\{ category: '', role: '', series: '', paymentStatus: '' \}\)/)
  assert.match(appSource, /v-for="\(order, orderIndex\) in filteredQueryOrders"/)
  // 普通用户页面不使用管理员 WPS 组件。
  const queryStart = appSource.indexOf(`routeName === 'query-orders'`)
  if (queryStart > 0) {
    const queryTemplate = appSource.slice(queryStart, appSource.indexOf('</template>', queryStart))
    assert.doesNotMatch(queryTemplate, /ColumnValueFilter/)
  }
})

// ===== 阶段 4F-8：主表列精简 =====

// 主表表头必须严格是这 14 列、且顺序完全一致。
// 「系列」来自模板 B2「分类(系列号)」的值，「团名」来自角色列头前缀
// （"25h miku" → "25h"）：两个独立字段，必须各占一列。
const ADMIN_ORDER_COLUMNS = [
  'CN',
  '谷子名称',
  '系列',
  '团名',
  '谷子角色',
  '数量',
  '单价',
  '明细总金额',
  '已付金额',
  '未付金额',
  '订单状态',
  '付款状态',
  '创建时间',
  '查看详情',
]

// 从 <thead> 里按出现顺序抽取每列的可见标题，无论它来自筛选组件的 label
// 还是纯文本表头。
function adminOrderHeaderLabels() {
  const thead = ordersTemplate.slice(ordersTemplate.indexOf('<thead>'), ordersTemplate.indexOf('</thead>'))
  return [...thead.matchAll(/<th\b[^>]*>([\s\S]*?)<\/th>/g)].map(([, cell]) => {
    const filterLabel = cell.match(/label="([^"]+)"/)
    if (filterLabel) return filterLabel[1]
    const plain = cell.match(/column-header__label">([^<]+)</)
    return plain ? plain[1] : ''
  })
}

test('订单主表严格为 14 列且顺序一致', () => {
  const labels = adminOrderHeaderLabels()
  assert.deepEqual(labels, ADMIN_ORDER_COLUMNS)
  assert.equal(labels.length, 14)
  // 空表头/占位列会让列数对不上，这里一并排除。
  assert.ok(labels.every((label) => label.trim() !== ''), '不允许空表头占位列')
})

test('主表不再包含用户名称、项目名称、谷子种类', () => {
  const labels = adminOrderHeaderLabels()
  for (const removed of ['用户名称', '项目名称', '谷子种类']) {
    assert.ok(!labels.includes(removed), `${removed} 列应已删除`)
  }
  // 不是靠 CSS 藏起来，而是整列不再渲染。
  for (const field of ['item.display_name', 'item.project_name', 'item.category']) {
    assert.doesNotMatch(ordersTemplate, new RegExp(field.replace('.', '\.')), `${field} 不应仍被渲染`)
  }
  // 行的单元格数量与表头一致。
  const bodyRow = ordersTemplate.slice(ordersTemplate.indexOf(':key="item.item_id"'))
  const cells = [...bodyRow.slice(0, bodyRow.indexOf('</tr>')).matchAll(/<td\b/g)]
  assert.equal(cells.length, 14)
})

test('订单页不再请求项目名称与谷子种类的 facets', () => {
  // 这两列已从筛选状态里移除，因此 buildFilterParams 不会带上它们，
  // 也不会有对应的 facets 请求。
  assert.match(
    appSource,
    /valueColumns: \['cn', 'item', 'series', 'group', 'role', 'status', 'payment_status'\]/,
  )
  assert.doesNotMatch(ordersTemplate, /column="project"/)
  assert.doesNotMatch(ordersTemplate, /column="category"/)
  assert.doesNotMatch(ordersTemplate, /orderFilterState\.values\.project/)
  assert.doesNotMatch(ordersTemplate, /orderFilterState\.values\.category/)
})

test('删列后重估表格宽度，未机械保留 1460px', () => {
  const css = read('src/style.css')
  assert.match(css, /\.order-table table \{ min-width: 1100px; \}/)
  assert.doesNotMatch(css, /min-width: 1460px/)
})

// ===== 系列 / 团名分列 =====

// 「系列」和「团名」是两个来源不同的字段，页面上必须各自绑定各自的数据，
// 不能共用同一个筛选状态或同一个单元格。
test('系列与团名各自绑定独立字段与独立筛选', () => {
  assert.match(ordersTemplate, /<ColumnValueFilter[^>]*orderFilterState\.values\.series[^>]*label="系列"[^>]*column="series"/)
  assert.match(ordersTemplate, /<ColumnValueFilter[^>]*orderFilterState\.values\.group[^>]*label="团名"[^>]*column="group"/)

  // 单元格：系列读 series_code，团名读 group_name，互不串列。
  assert.match(ordersTemplate, /\{\{ item\.series_code \|\| '-' \}\}/)
  assert.match(ordersTemplate, /\{\{ item\.group_name \|\| '-' \}\}/)

  // 旧文案「谷子系列」已不再出现在订单页。
  assert.ok(!ordersTemplate.includes('谷子系列'), '「谷子系列」应已改名为「团名」/「系列」')
})
