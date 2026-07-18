import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const appSource = readFileSync(resolve(frontendRoot, 'src/App.vue'), 'utf8')

test('three-level routing exists for both roles', () => {
  for (const route of [
    "path === '/admin'", "path === '/admin/data'", "path === '/admin/data/import'",
    "path === '/admin/data/history'", "path === '/admin/finance'",
    "path === '/admin/finance/payments'", "path === '/admin/finance/qr-codes'",
    "path === '/query/orders'", "path === '/query/payment'",
    "path === '/query/payments'", "path === '/query/security'",
  ]) {
    assert.equal(appSource.includes(route), true, `missing route: ${route}`)
  }
})

test('old admin URLs are redirected to the new module structure', () => {
  assert.match(appSource, /'\/admin\/imports': '\/admin\/data\/import'/)
  assert.match(appSource, /'\/admin\/imports\/history': '\/admin\/data\/history'/)
  assert.match(appSource, /'\/admin\/payments': '\/admin\/finance\/payments'/)
  assert.match(appSource, /'\/admin\/payment-qr': '\/admin\/finance\/qr-codes'/)
})

test('admin portal shows module cards and NO Excel file picker', () => {
  const portal = appSource.slice(
    appSource.indexOf(`<template v-else-if="routeName === 'admin'">`),
    appSource.indexOf(`<template v-else>`, appSource.indexOf(`<template v-else-if="routeName === 'admin'">`)),
  )
  for (const card of ['数据导入中心', '订单管理', '用户与账号', '收付款管理']) {
    assert.equal(portal.includes(card), true, `admin portal missing card ${card}`)
  }
  // The portal itself must not contain a file input.
  assert.doesNotMatch(portal, /type="file"/)
})

test('user portal shows module cards, not the full business content', () => {
  const portal = appSource.slice(
    appSource.indexOf(`<template v-else-if="routeName === 'query'">`),
    appSource.indexOf(`<template v-else-if="isUserRoute">`),
  )
  for (const card of ['我的订单', '付款中心', '付款记录', '账户安全']) {
    assert.equal(portal.includes(card), true, `user portal missing card ${card}`)
  }
})

test('reusable presentational components are extracted', () => {
  assert.match(appSource, /import ModuleCard from '\.\/components\/ModuleCard\.vue'/)
  assert.match(appSource, /import PortalStatusBar from '\.\/components\/PortalStatusBar\.vue'/)
})

test('no reference-site branding leaks', () => {
  assert.equal(appSource.includes('音游窝'), false)
})
