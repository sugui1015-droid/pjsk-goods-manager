import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 阶段 10：收款二维码管理与付款中心中轴统一。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')

const appSource = read('src/App.vue')
const css = read('src/style.css')

function routeTemplate(name) {
  const start = appSource.indexOf(`<template v-else-if="routeName === '${name}'">`)
  const alt = appSource.indexOf(`<template v-if="routeName === '${name}'">`)
  const s = start >= 0 ? start : alt
  assert.ok(s > 0, `找不到路由模板 ${name}`)
  const end = appSource.indexOf('<template v-else-if="', s + 20)
  return appSource.slice(s, end > s ? end : undefined)
}

test('管理员二维码：支付宝与微信卡片等宽、等高', () => {
  // 两列等宽网格 + 拉伸对齐 = 等宽等高。
  assert.match(css, /\.qr-admin-grid \{[\s\S]*?grid-template-columns: repeat\(2, minmax\(0, 1fr\)\);[\s\S]*?align-items: stretch;/)
})

test('管理员二维码：图片容器固定尺寸并居中，空状态占同样高度', () => {
  // 预览容器固定 min-height 且居中；图片与空状态共用同一容器。
  assert.match(css, /\.qr-card__preview \{[\s\S]*?min-height: 180px;[\s\S]*?align-items: center;[\s\S]*?justify-content: center;/)
  assert.match(css, /\.qr-empty \{[\s\S]*?text-align: center;/)
})

test('管理员二维码：标题、方式标题、状态、按钮居中', () => {
  assert.match(css, /\.qr-card__head \{[\s\S]*?align-items: center;[\s\S]*?text-align: center;/)
})

test('管理员二维码：SHA-256 仅在默认收起的技术标识区', () => {
  const tpl = routeTemplate('admin-qr')
  const technical = tpl.indexOf('qr-technical')
  assert.ok(technical > 0, '应有二维码技术区')
  const block = tpl.slice(technical, technical + 400)
  assert.doesNotMatch(block, /<details[^>]*\bopen\b/) // 默认收起
  assert.match(block, /技术标识/)
  assert.match(block, /仅供技术排查/)
  assert.match(block, /SHA-256/)
  // 主体元数据（格式/大小/更新管理员/更新时间）不在技术区，普通展示。
  assert.match(tpl, /qr-meta-list/)
})

test('管理员二维码：320px 单列', () => {
  assert.match(css, /@media \(max-width: 560px\) \{\s*\.qr-admin-grid \{\s*grid-template-columns: minmax\(0, 1fr\);/)
})

test('用户付款中心：支付宝/微信按钮等宽并居中', () => {
  assert.match(css, /\.query-method-button \{[\s\S]*?flex: 1;[\s\S]*?align-items: center;[\s\S]*?justify-content: center;/)
})

test('用户付款中心：本金/手续费/本次应付三块等宽', () => {
  assert.match(css, /\.query-amount-grid \{ grid-template-columns: repeat\(3, minmax\(0, 1fr\)\); \}/)
  // 320px 单列。
  assert.match(css, /\.query-amount-grid \{ grid-template-columns: minmax\(0, 1fr\); \}/)
})

test('用户付款中心：二维码标题与图片同轴，空状态与图片占位一致', () => {
  const tpl = routeTemplate('query-payment')
  // 二维码块居中中轴。
  assert.match(tpl, /class="query-pay-block query-pay-block--qr"/)
  assert.match(css, /\.query-pay-block--qr \{\s*align-items: center;/)
  // 固定高度占位槽，图片与空状态占同样高度。
  assert.match(tpl, /class="query-qr-slot"/)
  assert.match(css, /\.query-qr-slot \{[\s\S]*?min-height: 260px;[\s\S]*?align-items: center;[\s\S]*?justify-content: center;/)
  // 图片与空状态都在这个槽内。
  const slot = tpl.slice(tpl.indexOf('query-qr-slot'))
  assert.match(slot, /query-qr-figure/)
  assert.match(slot, /qr-empty/)
})

test('用户付款中心：图片本身居中', () => {
  assert.match(css, /\.query-qr-figure \{[\s\S]*?align-items: center;/)
})
