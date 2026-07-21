import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 第 6 阶段：用户与账号页面的 WPS 表头筛选。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')

const appSource = read('src/App.vue')
const clientSource = read('src/api/client.ts')
const dateFilterSource = read('src/components/ColumnDateFilter.vue')
const columnFiltersSource = read('src/filters/columnFilters.ts')

// 只取 /admin/users 的模板块：其他页面各有各的筛选，全文匹配会张冠李戴。
const usersTemplate = (() => {
  const start = appSource.indexOf(`routeName === 'admin-users'`)
  const end = appSource.indexOf(`routeName === 'admin-user-detail'`)
  assert.ok(start > 0 && end > start, 'could not locate the /admin/users template block')
  return appSource.slice(start, end)
})()

// 用户主表的列，顺序必须完全一致。「管理员身份」列仅苏归可见（v-if="isOwner"），
// 在源码里始终存在，位于「创建时间」与「查看详情」之间。
const USER_COLUMNS = [
  'CN',
  '用户名称',
  '查询权限',
  '查询码',
  '恢复邮箱',
  '订单数量',
  '总金额',
  '已付金额',
  '未付金额',
  '最后登录时间',
  '创建时间',
  '管理员身份',
  '查看详情',
]

function userHeaderLabels() {
  const thead = usersTemplate.slice(usersTemplate.indexOf('<thead>'), usersTemplate.indexOf('</thead>'))
  return [...thead.matchAll(/<th\b[^>]*>([\s\S]*?)<\/th>/g)].map(([, cell]) => {
    const filterLabel = cell.match(/label="([^"]+)"/)
    if (filterLabel) return filterLabel[1]
    const plain = cell.match(/column-header__label">([^<]+)</)
    return plain ? plain[1] : ''
  })
}

test('旧的顶部筛选表单已从用户页移除', () => {
  assert.doesNotMatch(usersTemplate, /<form/)
  assert.doesNotMatch(usersTemplate, /order-filters/)
  assert.doesNotMatch(usersTemplate, /adminUserFilters\./)
  assert.doesNotMatch(usersTemplate, /placeholder="CN 或显示名"/)
  assert.doesNotMatch(usersTemplate, /高级筛选/)
  // 旧的单值筛选状态本身也已删除。
  assert.doesNotMatch(appSource, /const adminUserFilters = ref\(\{ cn: '', status: '' \}\)/)
})

test('用户主表列顺序一致（含苏归专属「管理员身份」列）', () => {
  const labels = userHeaderLabels()
  assert.deepEqual(labels, USER_COLUMNS)
  assert.ok(labels.every((label) => label.trim() !== ''), '不允许空表头占位列')

  // 行的单元格数与表头一致（含 v-if="isOwner" 的管理员身份单元格）。
  const bodyRow = usersTemplate.slice(usersTemplate.indexOf(':key="user.id"'))
  const cells = [...bodyRow.slice(0, bodyRow.indexOf('</tr>')).matchAll(/<td\b/g)]
  assert.equal(cells.length, 13)
})

test('值筛选列接入 WPS 漏斗组件', () => {
  for (const [label, column] of [
    ['CN', 'cn'],
    ['用户名称', 'name'],
    ['查询权限', 'status'],
    ['查询码', 'has_query_code'],
    ['恢复邮箱', 'has_recovery_email'],
  ]) {
    assert.match(
      usersTemplate,
      new RegExp(`<ColumnValueFilter[^>]*label="${label}"[^>]*column="${column}"`),
      `${label} 列缺少值筛选入口`,
    )
  }
})

test('范围与日期筛选列接入对应组件，查看详情无漏斗', () => {
  for (const label of ['订单数量', '总金额', '已付金额', '未付金额']) {
    assert.match(usersTemplate, new RegExp(`<ColumnRangeFilter[^>]*label="${label}"`), `${label} 列缺少范围筛选`)
  }
  assert.match(usersTemplate, /<ColumnDateFilter[^>]*label="最后登录时间"/)
  assert.match(usersTemplate, /<ColumnDateFilter[^>]*label="创建时间"/)

  // 查看详情列只有纯文本表头。
  assert.match(usersTemplate, /<span class="column-header__label">查看详情<\/span>/)
  assert.doesNotMatch(usersTemplate, /label="查看详情"[^>]*:load-facets/)
})

test('最后登录时间支持「从未登录」空白筛选', () => {
  assert.match(usersTemplate, /<ColumnDateFilter[^>]*label="最后登录时间"[^>]*allow-blank[^>]*blank-label="从未登录"/)
  // 创建时间没有空白选项（每个用户都有创建时间）。
  const created = usersTemplate.match(/<ColumnDateFilter[^>]*label="创建时间"[^>]*\/>/)
  assert.ok(created && !created[0].includes('allow-blank'), '创建时间不应有空白选项')

  // 组件按需渲染复选框，并与日期区间互斥。
  assert.match(dateFilterSource, /v-if="props\.allowBlank"/)
  assert.match(dateFilterSource, /:disabled="draft\.blank"/)
  // 空白选择通过专用 <column>_blank=1 传递。
  assert.match(columnFiltersSource, /if \(dates\.blank\) params\.set\(`\$\{column\}_blank`, '1'\)/)
  // 无登录记录时显示「从未登录」。
  assert.match(usersTemplate, /user\.last_login_at \? formatDate\(user\.last_login_at\) : '从未登录'/)
})

test('多值与空白参数经共用编码器生成', () => {
  assert.match(
    appSource,
    /function adminUserFilterParams\(\)\s*\{\s*\n\s*return buildFilterParams\(adminUserFilterState\.value, ADMIN_USER_RANGE_PARAMS, ADMIN_USER_DATE_PARAMS\)/,
  )
  // 值列的 key 即 API 参数名（重复参数）。
  assert.match(
    appSource,
    /valueColumns: \['cn', 'name', 'status', 'has_query_code', 'has_recovery_email'\]/,
  )
  // 范围与日期列各自映射到两个参数名。
  assert.match(appSource, /order_count: \['order_count_min', 'order_count_max'\]/)
  assert.match(appSource, /total: \['total_min', 'total_max'\]/)
  assert.match(appSource, /paid: \['paid_min', 'paid_max'\]/)
  assert.match(appSource, /unpaid: \['unpaid_min', 'unpaid_max'\]/)
  assert.match(appSource, /last_login: \['last_login_from', 'last_login_to'\]/)
  assert.match(appSource, /created: \['created_from', 'created_to'\]/)
})

test('facets 请求带当前筛选、列名与候选分页', () => {
  const loader = appSource.slice(appSource.indexOf('async function loadAdminUserFacets'), appSource.indexOf('function applyAdminUserFilters'))
  assert.match(loader, /const params = adminUserFilterParams\(\)/)
  assert.match(loader, /params\.set\('column', request\.column\)/)
  assert.match(loader, /params\.set\('facet_page', String\(request\.page\)\)/)
  assert.match(loader, /\/api\/admin\/users\/facets\?/)
  // 后端用 facet_page/facet_page_size 命名分页，这里适配成浮层读取的形状。
  assert.match(loader, /page: response\.facet_page/)
  assert.match(loader, /page_size: response\.facet_page_size/)
})

test('筛选变化重置页码，翻页保留筛选', () => {
  assert.match(appSource, /function applyAdminUserFilters\(\)\s*\{\s*\n\s*adminUserPage\.value = 1\s*\n\s*void loadAdminUsers\(\)/)
  assert.match(usersTemplate, /@update:model-value="applyAdminUserFilters"/)
  // 翻页只改页码，不动筛选状态。
  const goTo = appSource.slice(appSource.indexOf('function goToAdminUserPage'), appSource.indexOf('function changeAdminUserPageSize'))
  assert.match(goTo, /adminUserPage\.value = page\s*\n\s*void loadAdminUsers\(\)/)
  assert.doesNotMatch(goTo, /clearColumnFilters|adminUserFilterState\.value =/)
  // 每页数量切换也回到第 1 页。
  assert.match(appSource, /function changeAdminUserPageSize\(\)\s*\{\s*\n\s*adminUserPage\.value = 1/)
})

test('结果总数与分页来自后端响应', () => {
  assert.match(appSource, /adminUserTotal\.value = response\.total/)
  assert.match(appSource, /adminUserTotalPages\.value = response\.total_pages/)
  assert.match(usersTemplate, /结果：共 \{\{ adminUserTotal \}\} 位用户/)
  // 用的是筛选后的总数，而不是当前页行数。
  assert.doesNotMatch(usersTemplate, /结果：共 \{\{ adminUsers\.length \}\}/)
  // 每页可选 25/50/100/200。
  for (const size of [25, 50, 100, 200]) {
    assert.match(usersTemplate, new RegExp(`:value="${size}"`), `缺少每页 ${size} 条`)
  }
})

test('清空全部筛选一次性重置所有列', () => {
  assert.match(appSource, /function resetAdminUserFilters\(\)[\s\S]{0,200}?clearColumnFilters\(adminUserFilterState\.value\)\s*\n\s*applyAdminUserFilters\(\)/)
  assert.match(usersTemplate, /:disabled="adminUserActiveFilterCount === 0"/)
  assert.match(appSource, /const adminUserActiveFilterCount = computed\(\(\) => activeFilterCount\(adminUserFilterState\.value\)\)/)
})

test('导出携带完整筛选、不带分页', () => {
  assert.match(appSource, /void downloadExport\('\/api\/admin\/export\/users\.xlsx', adminUserFilterParams\(\)\)/)
  assert.match(appSource, /void downloadExport\('\/api\/admin\/export\/users\.csv', adminUserFilterParams\(\)\)/)
})

test('主表不显示查询码原文、邮箱或任何技术字段', () => {
  // 两个安全列只显示状态文案。
  assert.match(usersTemplate, /user\.has_query_code \? '已设置' : '未设置'/)
  assert.match(usersTemplate, /user\.has_recovery_email \? '已绑定' : '未绑定'/)
  for (const secret of [
    'query_code_hash', 'encrypted_email', 'email_lookup_hash',
    'masked_email', 'recovery_token', 'bind_token', 'session_id', 'verification_code',
  ]) {
    assert.ok(!usersTemplate.includes(secret), `主表不应出现 ${secret}`)
  }
  // 查询码本身绝不渲染。has_query_code（布尔状态）是允许的，所以这里查的是
  // 对 user.query_code 的直接访问，而不是宽松的子串。
  assert.doesNotMatch(usersTemplate, /user\.query_code\b/)
  assert.doesNotMatch(usersTemplate, /user\.email\b/)
  // 内部 ID 只用于 key 与详情导航，不渲染。
  assert.doesNotMatch(usersTemplate, /\{\{ user\.id \}\}/)
  assert.match(usersTemplate, /:key="user\.id"/)
  assert.match(usersTemplate, /navigate\('\/admin\/users\/' \+ user\.id\)/)
})

test('列表 DTO 的两个安全列是布尔状态', () => {
  const dto = clientSource.slice(clientSource.indexOf('export type AdminUserListItem'), clientSource.indexOf('export type AdminUserListSummary'))
  assert.match(dto, /has_query_code: boolean/)
  assert.match(dto, /has_recovery_email: boolean/)
  for (const secret of ['query_code_hash', 'encrypted_email', 'email_lookup_hash', 'password']) {
    assert.ok(!dto.includes(secret), `AdminUserListItem 不应包含 ${secret}`)
  }
})

test('页面标题为「用户与账号」，顶部三行结构', () => {
  const heading = usersTemplate.indexOf('page-heading')
  const actions = usersTemplate.indexOf('page-actions')
  const resultbar = usersTemplate.indexOf('page-resultbar')
  assert.ok(heading > 0 && actions > heading && resultbar > actions, '顶部应为 标题 / 操作 / 结果 三行')
  assert.match(usersTemplate.slice(heading, actions), /<h2>用户与账号<\/h2>/)
  assert.doesNotMatch(usersTemplate.slice(heading, actions), /导出 Excel/)
})

test('表格只在容器内部横向滚动，金额等宽两位小数', () => {
  const css = read('src/style.css')
  assert.match(usersTemplate, /class="table-scroll history-table user-table"/)
  assert.match(css, /\.user-table table \{ min-width: 1080px; \}/)
  assert.match(css, /\.table-scroll \{[\s\S]*?max-width: 100%;[\s\S]*?overflow: auto;/)
  for (const field of ['total_amount', 'paid_amount', 'remaining_amount']) {
    assert.match(usersTemplate, new RegExp(`formatMoney\\(user\\.${field}\\)`), `${field} 应走统一金额格式`)
  }
  assert.match(css, /\.numeric-column \{ font-variant-numeric: tabular-nums; \}/)
})

test('状态显示中文', () => {
  assert.match(usersTemplate, /userStatusLabel\(user\.status\)/)
  assert.match(appSource, /active: '正常'/)
  assert.match(appSource, /disabled: '已停用'/)
  assert.match(appSource, /merged: '已合并'/)
})

test('汇总方框口径与结果数一致（由后端对完整筛选结果聚合）', () => {
  // 前端不再自行加总，直接用后端 summary。
  assert.match(appSource, /adminUsersSummary\.value = response\.summary \?\? null/)
  assert.match(usersTemplate, /adminUsersSummary\.user_count/)
})

test('阶段 4F 订单页契约未被本阶段破坏', () => {
  // 共用组件与编码器改动后，订单页仍是明细行 + 13 列 + 同一套参数入口。
  assert.match(appSource, /function orderFilterParams\(\)\s*\{\s*\n\s*return buildFilterParams\(orderFilterState\.value, ORDER_RANGE_PARAMS, ORDER_DATE_PARAMS\)/)
  assert.match(appSource, /valueColumns: \['cn', 'item', 'series', 'role', 'status', 'payment_status'\]/)
})
