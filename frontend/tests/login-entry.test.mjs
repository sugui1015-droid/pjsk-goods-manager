import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const appSource = readFileSync(resolve(frontendRoot, 'src/App.vue'), 'utf8')
const styleSource = readFileSync(resolve(frontendRoot, 'src/style.css'), 'utf8')

test('routes: /admin is the admin portal/login, / is the landing', () => {
  assert.match(appSource, /if \(path === '\/admin'\) return 'admin'/)
  // routeFromPath falls back to 'home' for '/'
  assert.match(appSource, /return 'home'/)
})

test('brand is PJSK 谷子系统 everywhere; no 音游窝 leaked from the reference image', () => {
  assert.equal(appSource.includes('音游窝'), false, 'App.vue must not contain 音游窝')
  // Landing + both login pages use the system brand.
  const brandCount = (appSource.match(/PJSK 谷子系统/g) || []).length
  assert.ok(brandCount >= 3, `expected PJSK 谷子系统 on landing + 2 login pages, found ${brandCount}`)
  assert.match(appSource, /请选择登录入口/)
  assert.match(appSource, /用户服务入口/)
  assert.match(appSource, /管理员入口/)
})

test('landing shows exactly two equal entry cards, no developer info', () => {
  const start = appSource.indexOf('<div class="entry-choices">')
  const end = appSource.indexOf('</div>', start)
  const choices = appSource.slice(start, end)
  assert.match(choices, /entry-choice--user/)
  assert.match(choices, /entry-choice--admin/)
  assert.match(choices, /普通用户入口/)
  assert.match(choices, /查询订单、付款信息及账户安全设置/)
  assert.match(choices, /导入数据、管理用户、订单及付款信息/)
  // No developer overview leaks onto the landing.
  assert.doesNotMatch(appSource, /Streamlit 管理端/)
  assert.doesNotMatch(appSource, /运行指标/)
  assert.doesNotMatch(appSource, /可用模块/)
})

test('no global admin nav; admin business routes are isolated behind the portal', () => {
  // The old always-on <nav class="tabs"> is gone; module sub-nav is scoped.
  assert.doesNotMatch(appSource, /<nav v-if="showAdminNav" class="tabs"/)
  assert.match(appSource, /<nav v-if="adminModule === 'data'" class="module-subnav"/)
  // isAdminRoute is admin *business* pages only (admin-*), not the portal 'admin'.
  assert.match(appSource, /const isAdminRoute = computed\(\(\) => routeName\.value\.startsWith\('admin-'\)\)/)
})

test('unauthenticated admin deep links are redirected to /admin and returned after login', () => {
  assert.match(appSource, /pendingAdminTarget\.value = window\.location\.pathname \+ window\.location\.search/)
  assert.match(appSource, /navigate\('\/admin'\)/)
  // login returns to the remembered target (or the admin default)
  assert.match(appSource, /const target = pendingAdminTarget\.value \|\| defaultAdminTarget/)
})

test('both login pages keep their existing auth flows', () => {
  // Admin login page still posts to the admin login handler via login().
  assert.match(appSource, /<form class="entry-form" @submit\.prevent="login">/)
  // User page retains CN + query code + recovery entries.
  assert.match(appSource, /首次设置查询码/)
  assert.match(appSource, /忘记查询码/)
})

test('entry CSS: equal cards, single column at 560px, back-to-home control', () => {
  assert.match(styleSource, /\.entry-choices\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2/)
  assert.match(styleSource, /@media \(max-width: 560px\)\s*\{[\s\S]*?\.entry-choices\s*\{[\s\S]*?minmax\(0, 1fr\)/)
  assert.match(appSource, /返回系统主页/)
})

// Naming is unified: 用户中心 / 谷子管理中心 / 系统主页 — no 工作台 / 服务台 / 入口选择.
test('portal and back-button naming is unified across the app', () => {
  for (const stale of ['用户服务台', '返回服务台', '谷子管理工作台', '返回工作台', '返回入口选择']) {
    assert.equal(appSource.includes(stale), false, `stale name still present: ${stale}`)
  }
  assert.match(appSource, /<h1 class="portal-hero__title">用户中心<\/h1>/)
  assert.match(appSource, /<h1 class="portal-hero__title">谷子管理中心<\/h1>/)
  assert.match(appSource, /back-label="← 返回用户中心"/)
  assert.match(appSource, /back-label="← 返回谷子管理中心"/)
  assert.match(appSource, /返回系统主页/)
})
