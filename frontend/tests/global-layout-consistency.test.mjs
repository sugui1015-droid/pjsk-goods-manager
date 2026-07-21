import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 阶段 8：全站字体层级、居中、名称统一、导出与标题分行。
//
// 这些是源码断言：锁定字体令牌的大小关系、无斜体/歪斜、旧名称为 0、
// 导出页标题与导出区分处不同结构行、以及阶段 3～7 的页面契约不回退。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')

const appSource = read('src/App.vue')
const css = read('src/style.css')

// 从 :root 里读出某个字号令牌的像素值。
function fontToken(name) {
  const match = css.match(new RegExp(`--${name}:\\s*(\\d+)px`))
  assert.ok(match, `字体令牌 --${name} 未定义`)
  return Number(match[1])
}

test('字体层级：页面主标题 > 区块标题 > 内容 > 标签 > 提示', () => {
  const pageTitle = fontToken('fs-page-title')
  const sectionTitle = fontToken('fs-section-title')
  const value = fontToken('fs-value')
  const label = fontToken('fs-label')
  const hint = fontToken('fs-hint')

  assert.ok(pageTitle > sectionTitle, `页面主标题(${pageTitle}) 必须大于区块标题(${sectionTitle})`)
  assert.ok(sectionTitle > value, `区块标题(${sectionTitle}) 必须大于内容(${value})`)
  // 关键：内容不得小于标签。
  assert.ok(value > label, `内容(${value}) 必须大于标签(${label})`)
  assert.ok(label >= hint, `标签(${label}) 不应小于提示(${hint})`)
})

test('WPS 管理页主标题使用页面主标题字号（明显大于内容）', () => {
  // .page-heading h2 必须绑定 --fs-page-title，而不是区块标题字号。
  assert.match(css, /\.page-heading h2 \{[^}]*font-size:\s*var\(--fs-page-title\)/)
  // 裸块标题被纳入区块标题令牌，避免浏览器默认字号失控。
  assert.match(css, /\.panel h2[^{]*\{[^}]*font-size:\s*var\(--fs-section-title\)/)
})

test('全站无斜体或歪斜关键规则', () => {
  assert.doesNotMatch(css, /font-style:\s*italic/)
  assert.doesNotMatch(css, /font-style:\s*oblique/)
  assert.doesNotMatch(css, /transform:\s*skew/)
  assert.doesNotMatch(css, /skewX|skewY/)
  // 根节点关闭字体合成，避免伪粗/伪斜。
  assert.match(css, /font-synthesis:\s*none/)
})

test('表格表头与内容水平且垂直居中', () => {
  assert.match(css, /table th, table td \{ text-align: center; vertical-align: middle; \}/)
})

test('中轴容器存在：主体与页面标题共享居中中轴', () => {
  assert.match(css, /\.app-shell \{[\s\S]*?margin: 0 auto/)
  assert.match(css, /\.page-heading \{ text-align: center;/)
  assert.match(css, /\.module-header__title \{ font-size: var\(--fs-page-title\)/)
})

test('旧门户/返回名称全仓库为 0', () => {
  for (const oldName of ['用户服务台', '返回服务台', '谷子管理工作台', '返回工作台', '返回入口选择']) {
    assert.ok(!appSource.includes(oldName), `旧名称 ${oldName} 不应出现`)
  }
  // 目标名称存在。
  assert.match(appSource, /返回谷子管理中心/)
  assert.match(appSource, /返回系统主页/)
  assert.match(appSource, /返回用户中心/)
})

test('返回名称同层级一致：用户子页统一「返回用户中心」', () => {
  // 用户子页的 PortalStatusBar 返回标签统一，不与「返回个人中心」混用。
  assert.doesNotMatch(appSource, /返回个人中心/) // 本项目选定「返回用户中心」口径
  const backLabels = [...appSource.matchAll(/back-label="← (返回[^"]+)"/g)].map((m) => m[1])
  // 允许的集合：返回谷子管理中心（管理员）、返回系统主页（顶层）、返回用户中心（用户子页）。
  for (const label of backLabels) {
    assert.ok(
      ['返回谷子管理中心', '返回系统主页', '返回用户中心'].includes(label),
      `未预期的返回标签：${label}`,
    )
  }
})

// 三个带 CSV/XLSX 导出按钮的 WPS 管理页：标题与导出区必须分处不同结构行。
const EXPORT_PAGES = [
  { name: 'admin-orders', label: '订单只读查询' },
  { name: 'admin-users', label: '用户与账号' },
  { name: 'admin-payments', label: '付款记录' },
]

function routeTemplate(name) {
  const start = appSource.indexOf(`<template v-else-if="routeName === '${name}'">`)
  assert.ok(start > 0, `找不到路由模板 ${name}`)
  const end = appSource.indexOf('<template v-else-if="', start + 20)
  return appSource.slice(start, end > start ? end : undefined)
}

test('所有导出页：主标题与导出按钮分处不同结构行', () => {
  for (const page of EXPORT_PAGES) {
    const tpl = routeTemplate(page.name)
    const heading = tpl.indexOf('page-heading')
    const actions = tpl.indexOf('page-actions')
    const resultbar = tpl.indexOf('page-resultbar')
    assert.ok(heading > 0 && actions > heading && resultbar > actions, `${page.name} 顶部应为 标题/操作/结果 三行`)

    // 标题行内不得出现导出按钮。
    const headingBlock = tpl.slice(heading, actions)
    assert.ok(headingBlock.includes(page.label), `${page.name} 标题缺失`)
    assert.doesNotMatch(headingBlock, /导出|export[A-Z]/i)

    // 导出按钮在操作行。
    const actionsBlock = tpl.slice(actions, resultbar)
    assert.match(actionsBlock, /导出/)
  }
})

test('导出页样式：page-actions 与 page-resultbar 居中', () => {
  assert.match(css, /\.page-actions \{[\s\S]*?justify-content: center;/)
  assert.match(css, /\.page-resultbar \{[\s\S]*?justify-content: center;/)
})

test('320px 页面级防横滚：表格仅容器内部滚动', () => {
  // 宽表容器有横向滚动，页面本身不横滚。
  assert.match(css, /\.table-scroll \{[\s\S]*?max-width: 100%;[\s\S]*?overflow: auto;/)
  assert.match(appSource, /class="table-scroll history-table order-table"/)
  assert.match(appSource, /class="table-scroll history-table user-table"/)
  assert.match(appSource, /class="table-scroll history-table payment-records-table"/)
})

test('阶段 3～7 页面契约不回退', () => {
  // 订单页（4F）：明细行 + 同一套筛选参数入口。
  assert.match(appSource, /function orderFilterParams\(\)/)
  assert.match(appSource, /valueColumns: \['cn', 'item', 'series', 'group', 'role', 'status', 'payment_status'\]/)
  // 用户页（6）。
  assert.match(appSource, /function adminUserFilterParams\(\)/)
  assert.match(appSource, /valueColumns: \['cn', 'name', 'status', 'has_query_code', 'has_recovery_email'\]/)
  // 付款页（7）。
  assert.match(appSource, /function paymentFilterParams\(\)/)
  assert.match(appSource, /valueColumns: \['cn', 'payment_method', 'status', 'created_by'\]/)
  // 普通用户"我的订单"（3）。
  assert.match(appSource, /const queryOrderFilters = ref\(\{ category: '', role: '', series: '', paymentStatus: '' \}\)/)
})
