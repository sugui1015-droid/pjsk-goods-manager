import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const appSource = readFileSync(resolve(frontendRoot, 'src/App.vue'), 'utf8')
const clientSource = readFileSync(resolve(frontendRoot, 'src/api/client.ts'), 'utf8')
const styleSource = readFileSync(resolve(frontendRoot, 'src/style.css'), 'utf8')

function sourceBetween(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker)
  const end = source.indexOf(endMarker, start + startMarker.length)
  assert.notEqual(start, -1, `missing start marker: ${startMarker}`)
  assert.notEqual(end, -1, `missing end marker: ${endMarker}`)
  return source.slice(start, end)
}

const queryView = sourceBetween(
  appSource,
  `<template v-else-if="isUserRoute">`,
  `<template v-else-if="routeName === 'admin'">`,
)

test('regular-user order template never renders source-derived technical identifiers', () => {
  for (const forbidden of [
    'order.order_no',
    'order.project_name',
    'order.created_at',
    'import_batch_id',
    'source_sheet',
    'source_row_key',
    '.xlsx',
    'IMP-',
  ]) {
    assert.equal(queryView.includes(forbidden), false, `query view contains ${forbidden}`)
  }

  assert.match(queryView, /订单明细/)
  assert.match(queryView, /query-order-mobile-items/)
  assert.match(queryView, /谷子名称|display_name/)
  assert.match(queryView, /付款状态/)
})

test('regular-user QueryOrder DTO contains only aggregate and business item fields', () => {
  const queryOrderType = sourceBetween(
    clientSource,
    'export type QueryOrder = {',
    '// QueryPaymentItem',
  )

  for (const forbidden of ['order_no', 'project_name', 'created_at', 'status', 'import_', 'source_', 'sku', 'sha']) {
    assert.equal(queryOrderType.includes(forbidden), false, `QueryOrder type contains ${forbidden}`)
  }
  for (const required of ['total_quantity', 'total_amount', 'paid_amount', 'remaining_amount', 'items']) {
    assert.equal(queryOrderType.includes(required), true, `QueryOrder type misses ${required}`)
  }
})

test('payment history always has loading, failure, empty, and populated states', () => {
  assert.match(queryView, /<section v-if="routeName === 'query-payments'" class="panel query-payments-card">/)
  assert.doesNotMatch(queryView, /<section v-if="queryOrders\.payments\.length > 0" class="panel query-payments-card">/)
  assert.match(queryView, /正在加载付款历史。/)
  assert.match(queryView, /queryOrdersError/)
  assert.match(queryView, /暂无付款记录/)
  assert.match(queryView, /queryOrders\.payments/)
})

test('320px CSS uses wrapped navigation and mobile order cards without horizontal detail scrolling', () => {
  assert.match(styleSource, /@media \(max-width: 560px\)[\s\S]*?\.tabs\s*{[\s\S]*?display: grid;[\s\S]*?grid-template-columns: repeat\(3, minmax\(0, 1fr\)\);[\s\S]*?overflow-x: visible;/)
  assert.match(styleSource, /\.tabs button\s*{[\s\S]*?min-width: 0;[\s\S]*?white-space: normal;/)
  assert.match(styleSource, /@media \(max-width: 520px\)[\s\S]*?\.query-order-desktop-table\s*{[\s\S]*?display: none;/)
  assert.match(styleSource, /@media \(max-width: 520px\)[\s\S]*?\.query-order-mobile-items\s*{[\s\S]*?display: grid;[\s\S]*?min-width: 0;[\s\S]*?width: 100%;/)
})
