import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const appSource = readFileSync(resolve(frontendRoot, 'src/App.vue'), 'utf8')
const styleSource = readFileSync(resolve(frontendRoot, 'src/style.css'), 'utf8')

function sourceBetween(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker)
  const end = source.indexOf(endMarker, start + startMarker.length)
  assert.notEqual(start, -1, `missing start marker: ${startMarker}`)
  assert.notEqual(end, -1, `missing end marker: ${endMarker}`)
  return source.slice(start, end)
}

const queryOrdersView = sourceBetween(
  appSource,
  `<template v-if="routeName === 'query-orders' && queryOrders">`,
  `<section v-if="routeName === 'query-payments'"`,
)
const queryOrderFilterView = sourceBetween(
  queryOrdersView,
  '<div class="query-order-filter-grid"',
  '<div class="query-order-filter-actions">',
)

test('我的订单顶部使用四个独立汇总框，不再拼接为一串订单级汇总', () => {
  assert.equal(
    (queryOrdersView.match(/<article class="metric-tile query-order-overview__tile/g) ?? []).length,
    4,
  )
  for (const label of ['总金额', '共多少件', '已付金额', '未付金额']) {
    assert.match(queryOrdersView, new RegExp(`<span>${label}</span>`))
  }
  assert.doesNotMatch(queryOrdersView, /query-order-summary/)
  assert.doesNotMatch(queryOrdersView, /<span><em>总金额/)
})

test('筛选仅包含四个普通用户业务字段，并提供结果数量和清空', () => {
  assert.equal((queryOrderFilterView.match(/<label>/g) ?? []).length, 4)
  for (const label of ['谷子种类', '角色', '系列', '付款状态']) {
    assert.match(queryOrderFilterView, new RegExp(`<span>${label}</span>`))
  }
  for (const forbidden of ['订单号', '项目', 'SKU', 'SHA', '批次', '来源']) {
    assert.equal(queryOrderFilterView.includes(forbidden), false, `filter contains ${forbidden}`)
  }
  assert.match(queryOrdersView, /筛选结果：\{\{ filteredQueryOrderItemCount \}\} 项谷子明细/)
  assert.match(queryOrdersView, /@click="clearQueryOrderFilters">清空筛选/)
})

test('桌面表格严格使用要求的业务字段顺序且金额统一两位格式', () => {
  assert.match(
    queryOrdersView,
    /<tr><th>谷子名称<\/th><th>角色<\/th><th>谷子种类<\/th><th>系列<\/th><th>数量<\/th><th>总金额<\/th><th>已付金额<\/th><th>未付金额<\/th><th>付款状态<\/th><\/tr>/,
  )
  for (const expression of [
    'formatMoney(item.amount)',
    'formatMoney(item.paid_amount)',
    'formatMoney(item.remaining_amount)',
    'formatMoney(queryOrders.total_amount)',
    'formatMoney(queryOrders.paid_amount)',
    'formatMoney(queryOrders.remaining_amount)',
  ]) {
    assert.equal(queryOrdersView.includes(expression), true, `missing ${expression}`)
  }
  assert.doesNotMatch(queryOrdersView, /item\.unit_price/)
  assert.doesNotMatch(queryOrdersView, /<th>单价<\/th>|<th>小计<\/th>/)
})

test('普通用户订单模板不渲染技术字段，也不接入管理员 WPS 筛选组件', () => {
  for (const forbidden of [
    'order_no',
    'project_name',
    'project_id',
    'import_batch_id',
    'source_sheet',
    'source_row_key',
    'filename',
    'sku',
    'sha',
    'ColumnFilterButton',
    'ColumnValueFilter',
    'ColumnRangeFilter',
    'ColumnDateFilter',
  ]) {
    assert.equal(queryOrdersView.toLowerCase().includes(forbidden.toLowerCase()), false, `query orders contains ${forbidden}`)
  }
})

test('桌面表格与移动卡片同时存在，付款状态使用中文映射', () => {
  assert.match(queryOrdersView, /class="table-scroll detail-table query-order-desktop-table"/)
  assert.match(queryOrdersView, /class="query-order-mobile-items"/)
  assert.match(queryOrdersView, /class="query-order-mobile-item"/)
  assert.match(queryOrdersView, /queryPaymentStatusLabel\(item\.payment_status\)/)
  for (const status of ['未付款', '部分付款', '已付款']) {
    assert.match(queryOrdersView, new RegExp(status))
  }
})

test('320px 使用单列汇总和移动卡片，页面级横向滚动受约束', () => {
  assert.match(styleSource, /html,\s*\nbody,\s*\n#app\s*\{[\s\S]*?overflow-x: clip;/)
  assert.match(styleSource, /\.query-order-desktop-table table\s*\{[\s\S]*?min-width: 900px;/)
  assert.match(styleSource, /@media \(max-width: 520px\)[\s\S]*?\.query-order-desktop-table\s*\{[\s\S]*?display: none;/)
  assert.match(styleSource, /@media \(max-width: 520px\)[\s\S]*?\.query-order-mobile-items\s*\{[\s\S]*?display: grid;/)
  assert.match(styleSource, /@media \(max-width: 400px\)[\s\S]*?\.query-order-overview,[\s\S]*?grid-template-columns: minmax\(0, 1fr\);/)
  assert.match(styleSource, /\.query-order-name\s*\{[\s\S]*?white-space: normal;[\s\S]*?overflow-wrap: anywhere;/)
})
