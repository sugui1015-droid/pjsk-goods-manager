import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 第 5 阶段：技术标识全站边界。
//
// 这些断言锁的是"技术标识只出现在管理员详情页的收起区里"这条边界：
// 普通用户完全看不到，管理员主视图也不显示，只有详情页底部默认收起的
// 「技术标识」区里才有，并标明仅供技术排查。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(frontendRoot, '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')
const readRepo = (path) => readFileSync(resolve(repoRoot, path), 'utf8')

const appSource = read('src/App.vue')
const clientSource = read('src/api/client.ts')
const queryHandler = readRepo('backend/internal/query/handler.go')

// 取某个路由的模板块。
function routeTemplate(name) {
  const start = appSource.indexOf(`routeName === '${name}'`)
  assert.ok(start > 0, `找不到路由模板 ${name}`)
  const next = appSource.indexOf('<template v-else-if="routeName ===', start + 10)
  return appSource.slice(start, next > start ? next : undefined)
}

// ===== 5B：普通用户边界 =====

test('普通用户 DTO 不序列化任何技术标识', () => {
  // 内部 ID 必须是 json:"-"：查询用得到，但绝不下发。
  assert.match(queryHandler, /ID\s+string\s+`json:"-"`/)
  assert.match(queryHandler, /QueryCodeHash \*string `json:"-"`/)

  // 普通用户的两个明细 DTO 里不得出现任何技术字段的 JSON tag。
  const forbidden = [
    'order_id', 'order_item_id', 'product_id', 'user_id', 'payment_id',
    'import_batch_id', 'sku', 'sha', 'file_hash', 'content_hash',
    'source_sheet', 'source_row_key', 'source_file', 'idempotency',
    'order_no', 'project_name', 'project_id',
  ]
  for (const dto of ['type OrderItem struct', 'type PaymentItem struct']) {
    const start = queryHandler.indexOf(dto)
    assert.ok(start > 0, `找不到 ${dto}`)
    const block = queryHandler.slice(start, queryHandler.indexOf('}', start))
    for (const field of forbidden) {
      assert.ok(!block.includes(`json:"${field}`), `${dto} 不应下发 ${field}`)
    }
  }
})

test('普通用户前端类型不含技术字段', () => {
  for (const typeName of ['QueryOrderItem', 'QueryPaymentItem']) {
    const start = clientSource.indexOf(`export type ${typeName} = {`)
    if (start < 0) continue
    const block = clientSource.slice(start, clientSource.indexOf('}', start))
    for (const field of ['id:', 'order_id', 'product_id', 'sku', 'import_batch_id', 'source_sheet', 'source_row_key', 'file_hash', 'order_no']) {
      assert.ok(!block.includes(field), `${typeName} 不应包含 ${field}`)
    }
  }
})

test('普通用户页面模板不渲染技术标识', () => {
  for (const route of ['query-orders', 'query-payment', 'query-security']) {
    let template
    try {
      template = routeTemplate(route)
    } catch {
      continue // 该路由不存在时跳过，不作为失败。
    }
    for (const technical of [
      'import_batch_id', '.sku', 'file_hash', 'source_row_key', 'source_sheet',
      'order_no', 'project_name', 'technical-section', 'addTechnicalIdentifier',
    ]) {
      assert.ok(!template.includes(technical), `${route} 不应出现技术标识 ${technical}`)
    }
  }
})

test('普通用户页面没有导出入口', () => {
  for (const route of ['query-orders', 'query-payment']) {
    let template
    try {
      template = routeTemplate(route)
    } catch {
      continue
    }
    assert.ok(!template.includes('downloadExport'), `${route} 不应提供导出`)
    assert.ok(!template.includes('.xlsx'), `${route} 不应提供导出`)
  }
})

// ===== 5C：管理员主视图边界 =====

test('管理员订单主表无技术标识', () => {
  const template = routeTemplate('admin-orders')
  for (const technical of [
    'import_batch_id', 'item.sku', 'file_hash', 'source_row_key', 'source_sheet',
    '{{ item.order_id }}', '{{ item.item_id }}', 'item.order_no',
  ]) {
    assert.ok(!template.includes(technical), `订单主表不应出现 ${technical}`)
  }
  // order_id 只用于详情导航。
  assert.match(template, /navigate\(`\/admin\/orders\/\$\{item\.order_id\}`\)/)
})

test('导入历史列表页不再挂技术标识区（同样的标识在导入详情里）', () => {
  const template = routeTemplate('admin-import-history')
  assert.ok(!template.includes('technical-section'), '列表页不应有技术标识区')
  assert.ok(!template.includes('importHistoryTechnicalIdentifiers'), '列表页不应收集技术标识')
  // 对应的收集函数已删除，不留死代码。
  assert.doesNotMatch(appSource, /function importHistoryTechnicalIdentifiers/)
  // 导入详情仍然保留这些标识。
  assert.match(appSource, /addTechnicalIdentifier\(rows, '导入记录 ID', detail\.import\.original_filename, detail\.import\.id\)/)
  assert.match(appSource, /addTechnicalIdentifier\(rows, '文件 SHA', detail\.import\.original_filename, detail\.import\.file_hash\)/)
})

// ===== 5D：管理员详情技术区 =====

test('每个技术标识区都默认收起、统一标题并标注仅供技术排查', () => {
  // 站内所有技术区共用同一套契约。
  const panels = [...appSource.matchAll(/class="[^"]*technical-(?:section|panel)[^"]*"/g)]
  assert.ok(panels.length >= 5, `技术区数量异常：${panels.length}`)

  // 统一标题：全部是「技术标识」，没有遗留的「查看技术标识」「技术详情」等文案。
  assert.doesNotMatch(appSource, /查看技术标识/)
  assert.doesNotMatch(appSource, /技术详情（仅管理员可见）/)
  const summaries = [...appSource.matchAll(/closed-label">▶ 技术标识/g)]
  assert.equal(summaries.length, 6, '应有 6 个统一标题的技术区（付款/用户/订单/导入详情 + 二维码卡片 + 收肾记录详情）')

  // 每个技术区都带「仅供技术排查」提示（数的是渲染出来的那行，
  // 而不是源码注释里顺带提到的字样）。
  const notes = [...appSource.matchAll(/<p class="muted technical-note">仅供技术排查/g)]
  assert.equal(notes.length, 6)

  // 默认收起：技术区的 <details> 一律不带 open（移动端同样默认收起）。
  assert.doesNotMatch(appSource, /<details[^>]*technical[^>]*\bopen\b/)
  assert.doesNotMatch(appSource, /class="technical-panel qr-technical" open/)
})

test('技术标识只在管理员详情页出现', () => {
  // 收集技术标识的函数只服务于四个详情页 + 二维码卡片。
  const collectors = [...appSource.matchAll(/function (\w*TechnicalIdentifiers)\(/g)]
    .map(([, name]) => name)
    // mergeTechnicalIdentifiers 是共用的去重/排版工具，不是某个页面的采集器。
    .filter((name) => name !== 'mergeTechnicalIdentifiers')
  assert.deepEqual(collectors.sort(), [
    'adminUserTechnicalIdentifiers',
    'importDetailTechnicalIdentifiers',
    'orderDetailTechnicalIdentifiers',
    'paymentDetailTechnicalIdentifiers',
  ])
  // 主列表路由里不得调用它们。
  for (const route of ['admin-orders', 'admin-users', 'admin-import-history']) {
    const template = routeTemplate(route)
    for (const collector of collectors) {
      assert.ok(!template.includes(collector), `${route} 是主视图，不应展示技术标识`)
    }
  }
})

test('订单详情技术区位于页面最底部', () => {
  const template = routeTemplate('admin-order-detail')
  const technical = template.indexOf('orderDetailTechnicalIdentifiers(orderDetail).length > 0')
  assert.ok(technical > 0)
  // 技术区之后不再有其它业务 section。
  const after = template.slice(technical)
  assert.ok(!after.includes('<section class="panel nested-panel">'), '技术区应是最后一块内容')
})

// 5.5 导出字段边界（表头顺序、无秘密）由 Go 侧断言，见
// backend/internal/export/technical_boundary_test.go —— 那里能直接调用
// orderItemHeaders()，比在前端正则匹配 Go 源码可靠得多。

// ===== 5.6 错误信息 =====

test('后端错误经 logsafe 分类，不把 SQL/路径/堆栈返回前端', () => {
  const ordersHandler = readRepo('backend/internal/orders/handler.go')
  // 未知故障一律返回固定文案，真实错误只进服务端日志。
  assert.match(ordersHandler, /log\.Printf\("[^"]+", logsafe\.Category\(err\)\)/)
  assert.match(ordersHandler, /writeError\(w, http\.StatusInternalServerError, "internal server error"\)/)
  // 不得把原始 error 直接写给客户端。
  assert.doesNotMatch(ordersHandler, /writeError\([^)]*err\.Error\(\)/)
  assert.doesNotMatch(ordersHandler, /http\.Error\([^)]*err\.Error\(\)/)
})

test('前端只展示中文业务错误，不回显后端原始错误对象', () => {
  // 列表/详情的错误分支统一走「中文兜底文案」。
  assert.match(appSource, /ordersMessage\.value = `订单列表加载失败：\$\{detail\}`/)
  // 不把整个 error 对象或堆栈塞进界面。
  assert.doesNotMatch(appSource, /\.value = String\(error\)/)
  assert.doesNotMatch(appSource, /error\.stack/)
})

// ===== 第 6 阶段：用户与账号的隐私边界 =====

test('用户详情：查询码只显示状态与更新时间，邮箱只显示脱敏结果', () => {
  const start = appSource.indexOf(`routeName === 'admin-user-detail'`)
  assert.ok(start > 0, '找不到用户详情模板')
  const next = appSource.indexOf('<template v-else-if="routeName ===', start + 10)
  const detail = appSource.slice(start, next > start ? next : undefined)

  // 查询码：只有「已设置 / 未设置」与更新时间，绝不显示码本身或哈希。
  assert.match(detail, /adminUserDetail\.user\.has_query_code \? '已设置' : '未设置'/)
  assert.match(detail, /adminUserDetail\.user\.query_code_updated_at/)
  assert.doesNotMatch(detail, /query_code_hash/)
  assert.doesNotMatch(detail, /user\.query_code\b/)

  // 恢复邮箱：只显示脱敏结果（指令允许详情页显示脱敏），不显示密文或盲索引。
  assert.match(detail, /adminRecoveryEmail\.masked_email/)
  for (const secret of ['encrypted_email', 'email_lookup_hash', 'verification_code', 'recovery_token']) {
    assert.ok(!detail.includes(secret), `用户详情不应出现 ${secret}`)
  }
})

test('用户列表接口不下发查询码哈希或邮箱密文', () => {
  const usersHandler = readRepo('backend/internal/users/handler.go')
  const dto = usersHandler.slice(usersHandler.indexOf('type ListItem struct'), usersHandler.indexOf('type queryCodeRequest'))
  for (const secret of ['query_code_hash', 'encrypted_email', 'email_lookup_hash', 'QueryCodeHash', 'EncryptedEmail']) {
    assert.ok(!dto.includes(secret), `ListItem 不应包含 ${secret}`)
  }
  // 两个安全列是布尔状态。
  assert.match(dto, /HasQueryCode\s+bool\s+`json:"has_query_code"`/)
  assert.match(dto, /HasRecoveryEmail\s+bool\s+`json:"has_recovery_email"`/)

  // 隐私在 SQL 源头收口：秘密字段根本不进 select。
  const filters = readRepo('backend/internal/users/filters.go')
  assert.match(filters, /case when coalesce\(u\.query_code_hash, ''\) <> '' then 'yes' else 'no' end as has_query_code/)
  assert.match(filters, /case when re\.user_id is not null then 'yes' else 'no' end as has_recovery_email/)
  for (const secret of ['encrypted_email', 'email_lookup_hash']) {
    assert.ok(!filters.includes(secret), `用户列表 SQL 不应触碰 ${secret}`)
  }
})
