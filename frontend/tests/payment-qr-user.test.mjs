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

// The payment center is its own module page (/query/payment).
const payPanel = sourceBetween(
  appSource,
  `<template v-if="routeName === 'query-payment'">`,
  `<section v-if="routeName === 'query-security'"`,
)

test('payment center has the four-tile summary', () => {
  assert.match(payPanel, /付款汇总/)
  const summary = sourceBetween(payPanel, 'query-pay-summary', '</div>')
  for (const tile of ['总金额', '共件数', '已付金额', '未付金额']) {
    assert.equal(summary.includes(tile), true, `summary missing ${tile}`)
  }
})

test('BOTH 支付宝 and 微信 are always offered (not only configured methods)', () => {
  // Method buttons iterate the fixed [alipay, wechat] set, not availability.
  assert.match(payPanel, /v-for="method in queryPayMethods"/)
  assert.match(appSource, /const queryPayMethods: PaymentQRMethod\[\] = \['alipay', 'wechat'\]/)
  assert.match(payPanel, /query-method-button--alipay/)
  assert.match(payPanel, /query-method-button--wechat/)
  // Default selection is 支付宝 (no WeChat fee until the user picks 微信).
  assert.match(appSource, /if \(!queryQRMethod\.value\) queryQRMethod\.value = 'alipay'/)
})

test('amounts split into 本金 / 手续费 / 本次应付, computed in integer cents', () => {
  const payable = sourceBetween(payPanel, '本次应付', '收款二维码')
  for (const box of ['本金', '手续费', '本次应付']) {
    assert.equal(payable.includes(box), true, `amount grid missing ${box}`)
  }
  assert.match(payable, /queryBaseAmount/)
  assert.match(payable, /queryFeeAmount/)
  assert.match(payable, /queryPayableAmount/)
  // WeChat fee: ceil(base/1000) cents, integer math (no float rounding).
  assert.match(appSource, /queryFeeCents = computed\(\(\) => \(queryQRMethod\.value === 'wechat' \? Math\.floor\(\(queryBaseCents\.value \+ 999\) \/ 1000\) : 0\)\)/)
  // 本次应付 uses the emphasis (red) tile; no editable input for the amount.
  assert.match(payable, /query-payable-tile/)
  assert.doesNotMatch(payable, /<input/)
  assert.match(payable, /付款完成不代表系统已自动确认/)
})

test('QR follows the selected method, from the query endpoint, with empty + zoom', () => {
  assert.match(appSource, /\/api\/query\/payment-qr\/\$\{method\}\/image/)
  assert.match(payPanel, /openQueryQRZoom/)
  assert.match(payPanel, /onQueryQRImageError/)
  // Empty state when the selected method's QR is not configured.
  assert.match(payPanel, /queryMethodAvailable\(queryQRMethod\)/)
  assert.match(payPanel, /管理员暂未配置/)
  assert.doesNotMatch(payPanel, /data:image\//)
})

test('payment center leaks no technical identifiers', () => {
  for (const forbidden of ['sha256', 'byte_size', 'updated_by', 'mime_type', 'order_no', 'project_name', 'image_data']) {
    assert.equal(payPanel.includes(forbidden), false, `pay panel leaks ${forbidden}`)
  }
})

test('payment center CSS: equal method buttons and three-box amount grid', () => {
  assert.match(styleSource, /\.query-method-button\s*\{[\s\S]*?flex:\s*1/)
  assert.match(styleSource, /\.query-amount-grid\s*\{[\s\S]*?grid-template-columns:\s*repeat\(3/)
  assert.match(styleSource, /\.query-qr-image\s*\{[\s\S]*?cursor:\s*zoom-in/)
})
