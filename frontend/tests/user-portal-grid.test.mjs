import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 阶段 2H-2A：用户中心门户四卡布局。
//
// 源码断言：统计区与功能区共用同一容器（同 max-width、同 gap、同列宽），
// 断点为 ≥1200 四列 / 641–1199 两列 / ≤640 单列；作用域必须收敛在
// portal-summary、module-grid--user 与 query-pay-summary 三个显式类上，
// 管理端 .module-grid 与其余 .summary-grid 不得被改写。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')

const appSource = read('src/App.vue')
const css = read('src/style.css')
const moduleCard = read('src/components/ModuleCard.vue')

// 提取某个 max-width 媒体查询块的完整文本。
function mediaBlock(width) {
  const match = css.match(new RegExp(`@media \\(max-width: ${width}px\\) \\{[\\s\\S]*?\\n\\}`, 'g'))
  assert.ok(match && match.length > 0, `缺少 @media (max-width: ${width}px) 块`)
  return match.join('\n')
}

test('用户中心统计区与功能区共用同一容器与四列网格', () => {
  const shared = css.match(
    /\.summary-grid\.portal-summary,\s*\n\.module-grid\.module-grid--user \{[^}]*\}/
  )
  assert.ok(shared, '必须存在 portal-summary 与 module-grid--user 的共用规则')
  const block = shared[0]
  assert.match(block, /grid-template-columns: repeat\(4, minmax\(0, 1fr\)\)/, '桌面端必须四列等宽')
  assert.match(block, /max-width: 1040px/, '共用 max-width 必须一致')
  assert.match(block, /margin-left: auto/, '容器必须水平居中')
  assert.match(block, /margin-right: auto/, '容器必须水平居中')
  assert.match(block, /gap: 16px/, '上下网格 gap 必须一致')
})

test('641–1199px 两列、≤640px 单列，且两个网格同步降列', () => {
  const twoCol = mediaBlock(1199)
  assert.match(twoCol, /\.summary-grid\.portal-summary/, '1199px 断点必须覆盖统计区')
  assert.match(twoCol, /\.module-grid\.module-grid--user/, '1199px 断点必须覆盖功能区')
  assert.match(twoCol, /repeat\(2, minmax\(0, 1fr\)\)/, '1199px 断点必须为两列')

  const oneCol = mediaBlock(640)
  assert.match(oneCol, /\.summary-grid\.portal-summary/, '640px 断点必须覆盖统计区')
  assert.match(oneCol, /\.module-grid\.module-grid--user/, '640px 断点必须覆盖功能区')
  assert.match(oneCol, /grid-template-columns: minmax\(0, 1fr\)/, '640px 断点必须为单列')
})

test('作用域收敛：通用 module-grid 与 summary-grid 行为未被改写', () => {
  // 通用 .module-grid 仍保持 auto-fit（管理端 3 处依赖它）。
  assert.match(css, /\.module-grid \{ display: grid; grid-template-columns: repeat\(auto-fit/, '通用 .module-grid 不得被改写')
  // 通用 .summary-grid 的 5 列默认与既有媒体查询保持存在。
  assert.match(css, /\.summary-grid \{\n  grid-template-columns: repeat\(5, minmax\(0, 1fr\)\);\n\}/, '通用 .summary-grid 默认不得被改写')
  // 新规则不得使用裸 .module-grid 或裸 .summary-grid 选择器扩大影响。
  const section = css.slice(css.indexOf('用户中心门户四卡布局'))
  assert.ok(section.length > 0, '缺少阶段 2H-2A 样式段')
  for (const line of section.split('\n')) {
    if (/^\s*\.(summary-grid|module-grid)\s*[,{]/.test(line)) {
      assert.fail(`阶段 2H-2A 样式段出现未收敛的裸选择器：${line.trim()}`)
    }
  }
})

test('用户中心模板挂载专用类且四卡均保留 meta 槽位', () => {
  assert.match(appSource, /class="module-grid module-grid--user"/, '用户中心功能区必须使用 module-grid--user')
  // module-grid--user 全站仅一处，不影响其他门户。
  assert.equal(appSource.match(/module-grid--user/g).length, 1, 'module-grid--user 只允许用于用户中心')
  const gridStart = appSource.indexOf('module-grid module-grid--user')
  const gridEnd = appSource.indexOf('</div>', gridStart)
  const gridBlock = appSource.slice(gridStart, gridEnd)
  const cards = gridBlock.match(/<ModuleCard /g)
  assert.equal(cards?.length, 4, '用户中心必须恰好四张模块卡')
  assert.equal(gridBlock.match(/reserve-meta/g)?.length, 4, '四张卡都必须显式保留 meta 槽位')
  assert.match(gridBlock, /title="账户安全"[^>]*reserve-meta|reserve-meta[^>]*title="账户安全"|<ModuleCard title="账户安全"[^>]*reserve-meta/, '账户安全卡必须保留 meta 槽位')
})

test('ModuleCard 的 reserveMeta 为显式参数，默认行为不变', () => {
  assert.match(moduleCard, /reserveMeta\?: boolean/, 'reserveMeta 必须是可选 prop')
  assert.match(moduleCard, /v-if="meta \|\| reserveMeta"/, '未传 reserveMeta 且无 meta 时不渲染徽标（默认行为不变）')
  assert.match(moduleCard, /'module-card__meta--empty': !meta/, '空 meta 时必须挂占位类')
  assert.match(css, /\.module-card__meta--empty \{ visibility: hidden; \}/, '占位徽标必须不可见但占位')
})

test('卡片槽位对齐规则仅作用于用户中心', () => {
  assert.match(css, /\.module-grid--user \.module-card \{ justify-content: flex-start; \}/)
  assert.match(css, /\.module-grid--user \.module-card__desc \{ flex: 1;/)
  assert.match(css, /\.module-grid--user \.module-card__cta \{ margin-top: auto; \}/)
  // 不得出现改写全局 .module-card 对齐的裸规则（既有定义除外：justify-content: center）。
  const globalCard = css.match(/\n\.module-card \{[^}]*\}/)
  assert.ok(globalCard, '全局 .module-card 定义必须存在')
  assert.match(globalCard[0], /justify-content: center/, '全局 .module-card 默认对齐不得被改动')
})

test('付款汇总修正为四列且随断点降列', () => {
  assert.match(css, /\.summary-grid\.query-pay-summary \{ grid-template-columns: repeat\(4, minmax\(0, 1fr\)\); \}/)
  assert.match(mediaBlock(1199), /\.summary-grid\.query-pay-summary \{ grid-template-columns: repeat\(2, minmax\(0, 1fr\)\); \}/)
  assert.match(mediaBlock(640), /\.summary-grid\.query-pay-summary \{ grid-template-columns: minmax\(0, 1fr\); \}/)
})
