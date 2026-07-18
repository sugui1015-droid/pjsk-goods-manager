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

const qrAdminView = sourceBetween(
  appSource,
  `<template v-else-if="routeName === 'admin-qr'">`,
  `<template v-else-if="routeName === 'admin-users'">`,
)

test('admin QR page has two method cards driven by a single loop (equal structure)', () => {
  assert.match(qrAdminView, /v-for="method in qrMethods"/)
  assert.match(qrAdminView, /qr-card--alipay/)
  assert.match(qrAdminView, /qr-card--wechat/)
  assert.match(qrAdminView, /收款码/)
})

test('admin QR page supports upload, replace, disable with confirmation', () => {
  assert.match(qrAdminView, /uploadPaymentQRImage\(method\)/)
  assert.match(qrAdminView, /disablePaymentQRImage\(method\)/)
  // Replace vs upload label depends on configured state.
  assert.match(qrAdminView, /替换二维码/)
  assert.match(qrAdminView, /上传二维码/)
  assert.match(qrAdminView, /停用二维码/)
  // Both destructive actions confirm in the handlers.
  assert.match(appSource, /function uploadPaymentQRImage[\s\S]*?window\.confirm/)
  assert.match(appSource, /function disablePaymentQRImage[\s\S]*?window\.confirm/)
})

test('admin QR page loads the image from the backend endpoint, never base64 in state', () => {
  assert.match(qrAdminView, /adminQRImageURL\(method\)/)
  assert.match(appSource, /\/api\/admin\/payment-qr\/\$\{method\}\/image/)
  // No data-URL / base64 image handling anywhere in the QR flow.
  assert.doesNotMatch(appSource, /data:image\//)
  assert.doesNotMatch(appSource, /toDataURL|FileReader/)
})

test('admin QR technical details are collapsed by default and admin-only', () => {
  assert.match(qrAdminView, /<details v-if="paymentQRStatusByMethod\[method\]\.configured" class="technical-panel qr-technical">/)
  // The <details> has no `open` attribute (collapsed by default).
  assert.doesNotMatch(qrAdminView, /class="technical-panel qr-technical" open/)
  // Same contract as every other technical area on the site.
  assert.match(qrAdminView, /closed-label">▶ 技术标识/)
  assert.match(qrAdminView, /仅供技术排查/)
  assert.match(qrAdminView, /SHA-256/)
  // Only the SHA is technical; the ordinary audit facts live outside the panel
  // so daily use does not require expanding a troubleshooting section.
  const panel = qrAdminView.slice(qrAdminView.indexOf('class="technical-panel qr-technical"'))
  assert.doesNotMatch(panel.slice(0, 500), /更新管理员/)
  assert.match(qrAdminView, /class="qr-meta-list"/)
})

test('admin QR upload is validated client-side (type + size) before hitting the network', () => {
  const onChange = sourceBetween(appSource, 'function onQRFileChange', 'async function uploadPaymentQRImage')
  assert.match(onChange, /qrAcceptedTypes\.includes/)
  assert.match(onChange, /qrMaxBytes/)
  assert.match(appSource, /const qrAcceptedTypes = \['image\/png', 'image\/jpeg', 'image\/webp'\]/)
  assert.match(appSource, /const qrMaxBytes = 5 \* 1024 \* 1024/)
})

test('client exposes the QR admin API helpers', () => {
  for (const symbol of ['getPaymentQRAdminStatuses', 'uploadPaymentQR', 'disablePaymentQR']) {
    assert.equal(clientSource.includes(symbol), true, `client missing ${symbol}`)
  }
})

test('QR admin CSS stacks to a single column at 320px and constrains the file input', () => {
  assert.match(styleSource, /\.qr-admin-grid\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2/)
  // At the narrow breakpoint the grid collapses to a single column.
  assert.match(styleSource, /@media \(max-width: 560px\)\s*\{[\s\S]*?\.qr-admin-grid\s*\{[\s\S]*?minmax\(0, 1fr\)/)
  // The bare file input is constrained so it cannot force horizontal overflow.
  assert.match(styleSource, /\.qr-file-picker input\s*\{[\s\S]*?min-width:\s*0/)
})
