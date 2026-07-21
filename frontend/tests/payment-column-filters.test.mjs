import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 第 7 阶段：付款记录的 WPS 表头筛选。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')

const appSource = read('src/App.vue')
const clientSource = read('src/api/client.ts')

// 只取 /admin/finance/payments 的模板块。用 <template v-else-if= 前缀锚定，
// 避免匹配到顶部子导航按钮里同名的 routeName 判断。
const paymentsTemplate = (() => {
  const start = appSource.indexOf(`<template v-else-if="routeName === 'admin-payments'">`)
  const end = appSource.indexOf(`<template v-else-if="routeName === 'admin-payment-detail'">`)
  assert.ok(start > 0 && end > start, 'could not locate the admin-payments template block')
  return appSource.slice(start, end)
})()

// 付款记录主表的 10 列，顺序必须完全一致。
const PAYMENT_COLUMNS = [
  'CN',
  '本金',
  '手续费',
  '实付金额',
  '付款方式',
  '付款状态',
  '付款时间',
  '录入管理员',
  '撤销时间',
  '查看详情',
]

// 付款记录表是页面里最后一个表格（前面还有"录入付款"的待付明细表），
// 所以从最后一个 <thead> 取表头。
function paymentHeaderLabels() {
  const theadStart = paymentsTemplate.lastIndexOf('<thead>')
  const thead = paymentsTemplate.slice(theadStart, paymentsTemplate.indexOf('</thead>', theadStart))
  return [...thead.matchAll(/<th\b[^>]*>([\s\S]*?)<\/th>/g)].map(([, cell]) => {
    const filterLabel = cell.match(/label="([^"]+)"/)
    if (filterLabel) return filterLabel[1]
    const plain = cell.match(/column-header__label">([^<]+)</)
    return plain ? plain[1] : ''
  })
}

test('旧的顶部筛选表单与高级筛选已从付款记录页移除', () => {
  assert.doesNotMatch(paymentsTemplate, /class="filter-form"/)
  assert.doesNotMatch(paymentsTemplate, /filter-advanced/)
  assert.doesNotMatch(paymentsTemplate, /高级筛选/)
  assert.doesNotMatch(paymentsTemplate, /paymentFilters\./)
  assert.doesNotMatch(paymentsTemplate, /placeholder="CN 或显示名"/)
  // 旧状态本身也已删除。
  assert.doesNotMatch(appSource, /const paymentAdvancedOpen = ref/)
  assert.doesNotMatch(appSource, /function paymentQueryString/)
})

test('重复的「是否撤销」控件已删除，撤销由付款状态表达', () => {
  // 旧页面同时有"付款状态"和"是否撤销"两个下拉，都写同一个 status 字段，
  // 互相覆盖。现在只保留状态列，另有独立的撤销时间列。
  assert.doesNotMatch(paymentsTemplate, /是否撤销/)
  assert.doesNotMatch(paymentsTemplate, /仅已撤销/)
  assert.match(paymentsTemplate, /<ColumnValueFilter[^>]*column="status"/)
  assert.match(paymentsTemplate, /<ColumnDateFilter[^>]*label="撤销时间"/)
})

test('付款记录主表严格为 10 列且顺序一致', () => {
  const labels = paymentHeaderLabels()
  assert.deepEqual(labels, PAYMENT_COLUMNS)
  assert.ok(labels.every((label) => label.trim() !== ''), '不允许空表头占位列')

  // 行的单元格数与表头一致。
  const bodyRow = paymentsTemplate.slice(paymentsTemplate.indexOf(':key="payment.id"'))
  const cells = [...bodyRow.slice(0, bodyRow.indexOf('</tr>')).matchAll(/<td\b/g)]
  assert.equal(cells.length, 10)
})

test('值筛选列接入 WPS 漏斗组件', () => {
  for (const [label, column] of [
    ['CN', 'cn'],
    ['付款方式', 'payment_method'],
    ['付款状态', 'status'],
    ['录入管理员', 'created_by'],
  ]) {
    assert.match(
      paymentsTemplate,
      new RegExp(`<ColumnValueFilter[^>]*label="${label}"[^>]*column="${column}"`),
      `${label} 列缺少值筛选入口`,
    )
  }
})

test('范围与日期筛选列接入对应组件，查看详情无漏斗', () => {
  for (const label of ['本金', '手续费', '实付金额']) {
    assert.match(paymentsTemplate, new RegExp(`<ColumnRangeFilter[^>]*label="${label}"`), `${label} 列缺少范围筛选`)
  }
  assert.match(paymentsTemplate, /<ColumnDateFilter[^>]*label="付款时间"/)
  assert.match(paymentsTemplate, /<ColumnDateFilter[^>]*label="撤销时间"/)

  assert.match(paymentsTemplate, /<span class="column-header__label">查看详情<\/span>/)
  assert.doesNotMatch(paymentsTemplate, /label="查看详情"[^>]*:load-facets/)
})

test('撤销时间支持「未撤销」空白筛选', () => {
  assert.match(paymentsTemplate, /<ColumnDateFilter[^>]*label="撤销时间"[^>]*allow-blank[^>]*blank-label="未撤销"/)
  // 付款时间没有空白选项（每条付款都有时间）。
  const paid = paymentsTemplate.match(/<ColumnDateFilter[^>]*label="付款时间"[^>]*\/>/)
  assert.ok(paid && !paid[0].includes('allow-blank'), '付款时间不应有空白选项')
  // 未撤销的行显示「未撤销」。
  assert.match(paymentsTemplate, /payment\.voided_at \? formatDate\(payment\.voided_at\) : '未撤销'/)
})

test('多值与空白参数经共用编码器生成', () => {
  assert.match(
    appSource,
    /function paymentFilterParams\(\)\s*\{\s*\n\s*return buildFilterParams\(paymentFilterState\.value, PAYMENT_RANGE_PARAMS, PAYMENT_DATE_PARAMS\)/,
  )
  assert.match(appSource, /valueColumns: \['cn', 'payment_method', 'status', 'created_by'\]/)
  assert.match(appSource, /principal: \['principal_min', 'principal_max'\]/)
  assert.match(appSource, /fee: \['fee_min', 'fee_max'\]/)
  assert.match(appSource, /payable: \['payable_min', 'payable_max'\]/)
  assert.match(appSource, /paid: \['paid_from', 'paid_to'\]/)
  assert.match(appSource, /voided: \['voided_from', 'voided_to'\]/)
})

test('facets 请求带当前筛选、列名与候选分页', () => {
  const loader = appSource.slice(appSource.indexOf('async function loadPaymentFacets'), appSource.indexOf('function applyPaymentFilters'))
  assert.match(loader, /const params = paymentFilterParams\(\)/)
  assert.match(loader, /params\.set\('column', request\.column\)/)
  assert.match(loader, /params\.set\('facet_page', String\(request\.page\)\)/)
  assert.match(loader, /\/api\/admin\/payments\/facets\?/)
  assert.match(loader, /page: response\.facet_page/)
  assert.match(loader, /page_size: response\.facet_page_size/)
})

test('筛选变化重置页码，翻页保留筛选', () => {
  assert.match(appSource, /function applyPaymentFilters\(\)\s*\{\s*\n\s*paymentPage\.value = 1\s*\n\s*void loadPaymentRecords\(\)/)
  assert.match(paymentsTemplate, /@update:model-value="applyPaymentFilters"/)
  const goTo = appSource.slice(appSource.indexOf('function goToPaymentPage'), appSource.indexOf('function changePaymentPageSize'))
  assert.match(goTo, /paymentPage\.value = page\s*\n\s*void loadPaymentRecords\(\)/)
  assert.doesNotMatch(goTo, /clearColumnFilters|paymentFilterState\.value =/)
  assert.match(appSource, /function changePaymentPageSize\(\)\s*\{\s*\n\s*paymentPage\.value = 1/)
})

test('结果总数与分页来自后端响应', () => {
  assert.match(appSource, /paymentTotal\.value = response\.total/)
  assert.match(appSource, /paymentTotalPages\.value = response\.total_pages/)
  assert.match(paymentsTemplate, /结果：共 \{\{ paymentTotal \}\} 条付款记录/)
  assert.doesNotMatch(paymentsTemplate, /结果：\{\{ paymentRecords\.length \}\}/)
  for (const size of [25, 50, 100, 200]) {
    assert.match(paymentsTemplate, new RegExp(`:value="${size}"`), `缺少每页 ${size} 条`)
  }
})

test('清空全部筛选一次性重置所有列', () => {
  assert.match(appSource, /function resetPaymentFilters\(\)[\s\S]{0,240}?clearColumnFilters\(paymentFilterState\.value\)\s*\n\s*applyPaymentFilters\(\)/)
  assert.match(paymentsTemplate, /:disabled="paymentActiveFilterCount === 0"/)
  assert.match(appSource, /const paymentActiveFilterCount = computed\(\(\) => activeFilterCount\(paymentFilterState\.value\)\)/)
})

test('导出携带完整筛选、不带分页', () => {
  assert.match(appSource, /void downloadExport\('\/api\/admin\/export\/payments\.xlsx', paymentFilterParams\(\)\)/)
  assert.match(appSource, /void downloadExport\('\/api\/admin\/export\/payments\.csv', paymentFilterParams\(\)\)/)
  // 旧写法只传 cn/method/status/paid_*，会丢掉金额范围。
  assert.doesNotMatch(appSource, /payments\.xlsx', \{/)
  assert.doesNotMatch(appSource, /payments\.csv', \{/)
})

test('金额两位小数、等宽数字，且读取的是落库口径', () => {
  const css = read('src/style.css')
  // 本金/手续费/实付金额都走统一金额格式。
  assert.match(paymentsTemplate, /formatMoney\(payment\.principal_amount \?\? payment\.amount\)/)
  assert.match(paymentsTemplate, /formatMoney\(payment\.fee_amount\)/)
  assert.match(paymentsTemplate, /formatMoney\(payment\.payable_amount \?\? payment\.total_amount\)/)
  assert.match(css, /\.numeric-column \{ font-variant-numeric: tabular-nums; \}/)
  // 前端不得对已落库付款重算金额（例如按当前费率推手续费）。
  assert.doesNotMatch(paymentsTemplate, /fee_rate|\* 0\.006|payment\.principal_amount \*/)
})

test('付款方式与状态显示中文', () => {
  assert.match(paymentsTemplate, /paymentMethodLabel\(payment\.payment_method \|\| ''\)/)
  assert.match(paymentsTemplate, /paymentStatusLabel\(payment\.status\)/)
})

test('主表无技术字段', () => {
  for (const technical of ['idempotency', 'screenshot', 'user_id', 'order_item_id', 'order_id']) {
    assert.ok(!paymentsTemplate.includes(`payment.${technical}`), `主表不应出现 ${technical}`)
  }
  // 内部 ID 只用于 key 与详情导航，不渲染。
  assert.doesNotMatch(paymentsTemplate, /\{\{ payment\.id \}\}/)
  assert.match(paymentsTemplate, /:key="payment\.id"/)
  assert.match(paymentsTemplate, /navigate\('\/admin\/payments\/' \+ payment\.id\)/)
  // 备注留在详情页，不挤进主表。
  assert.doesNotMatch(paymentsTemplate, /\{\{ payment\.note \|\| '-' \}\}/)
})

test('列表 DTO 带分页字段', () => {
  const dto = clientSource.slice(clientSource.indexOf('export type PaymentListResponse'), clientSource.indexOf('export type PaymentFacetResponse'))
  for (const field of ['page: number', 'page_size: number', 'total: number', 'total_pages: number']) {
    assert.ok(dto.includes(field), `PaymentListResponse 缺少 ${field}`)
  }
})

test('页面标题与顶部三行结构', () => {
  const heading = paymentsTemplate.indexOf('page-heading')
  const actions = paymentsTemplate.indexOf('page-actions')
  const resultbar = paymentsTemplate.indexOf('page-resultbar')
  assert.ok(heading > 0 && actions > heading && resultbar > actions, '顶部应为 标题 / 操作 / 结果 三行')
  assert.match(paymentsTemplate.slice(heading, actions), /<h2>付款记录<\/h2>/)
  assert.doesNotMatch(paymentsTemplate.slice(heading, actions), /导出付款 Excel/)
})

test('宽表只在容器内部横向滚动', () => {
  const css = read('src/style.css')
  assert.match(paymentsTemplate, /class="table-scroll history-table payment-records-table"/)
  assert.match(css, /\.payment-records-table table \{ min-width: 1000px; \}/)
  assert.match(css, /\.table-scroll \{[\s\S]*?max-width: 100%;[\s\S]*?overflow: auto;/)
})

test('阶段 4F/6 页面契约未被本阶段破坏', () => {
  // 共用组件与编码器改动后，订单页与用户页仍是同一套入口。
  assert.match(appSource, /function orderFilterParams\(\)\s*\{\s*\n\s*return buildFilterParams\(orderFilterState\.value, ORDER_RANGE_PARAMS, ORDER_DATE_PARAMS\)/)
  assert.match(appSource, /function adminUserFilterParams\(\)\s*\{\s*\n\s*return buildFilterParams\(adminUserFilterState\.value, ADMIN_USER_RANGE_PARAMS, ADMIN_USER_DATE_PARAMS\)/)
  assert.match(appSource, /valueColumns: \['cn', 'item', 'series', 'group', 'role', 'status', 'payment_status'\]/)
  assert.match(appSource, /valueColumns: \['cn', 'name', 'status', 'has_query_code', 'has_recovery_email'\]/)
})
