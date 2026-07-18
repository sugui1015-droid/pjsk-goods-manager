import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 阶段 9：数据导入中心重排 + 导入历史 WPS 表头筛选。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')

const appSource = read('src/App.vue')
const clientSource = read('src/api/client.ts')

function routeTemplate(name) {
  const start = appSource.indexOf(`<template v-else-if="routeName === '${name}'">`)
  const alt = appSource.indexOf(`<template v-if="routeName === '${name}'">`)
  const s = start >= 0 ? start : alt
  assert.ok(s > 0, `找不到路由模板 ${name}`)
  const end = appSource.indexOf('<template v-else-if="', s + 20)
  return appSource.slice(s, end > s ? end : undefined)
}

const historyTemplate = routeTemplate('admin-import-history')

test('数据导入中心入口页只有模块卡片，无上传/预览/历史表格', () => {
  const dataTemplate = routeTemplate('admin-data')
  assert.match(dataTemplate, /ModuleCard title="Excel 导入"/)
  assert.match(dataTemplate, /ModuleCard title="导入历史"/)
  // 入口页不得出现文件选择框、预览表格或历史表格。
  assert.doesNotMatch(dataTemplate, /type="file"/)
  assert.doesNotMatch(dataTemplate, /<table/)
  assert.doesNotMatch(dataTemplate, /uploadPreview/)
})

test('导入历史无旧的顶部搜索或高级筛选', () => {
  assert.doesNotMatch(historyTemplate, /class="filter-form"/)
  assert.doesNotMatch(historyTemplate, /filter-advanced/)
  assert.doesNotMatch(historyTemplate, /高级筛选/)
})

const IMPORT_COLUMNS = ['文件名', '状态', '上传管理员', '工作表数', '问题数', '写入明细数', '总金额', '上传时间', '确认时间', '查看详情']

function historyHeaderLabels() {
  const thead = historyTemplate.slice(historyTemplate.indexOf('<thead>'), historyTemplate.indexOf('</thead>'))
  return [...thead.matchAll(/<th\b[^>]*>([\s\S]*?)<\/th>/g)].map(([, cell]) => {
    const filterLabel = cell.match(/label="([^"]+)"/)
    if (filterLabel) return filterLabel[1]
    const plain = cell.match(/column-header__label">([^<]+)</)
    return plain ? plain[1] : ''
  })
}

test('导入历史表头顺序正确（10 列）', () => {
  assert.deepEqual(historyHeaderLabels(), IMPORT_COLUMNS)
})

test('值筛选列接入 WPS 漏斗组件；范围/日期列到位', () => {
  for (const [label, column] of [['文件名', 'filename'], ['状态', 'status'], ['上传管理员', 'uploaded_by']]) {
    assert.match(historyTemplate, new RegExp(`<ColumnValueFilter[^>]*label="${label}"[^>]*column="${column}"`), `${label} 缺少值筛选`)
  }
  for (const label of ['工作表数', '问题数', '写入明细数', '总金额']) {
    assert.match(historyTemplate, new RegExp(`<ColumnRangeFilter[^>]*label="${label}"`), `${label} 缺少范围筛选`)
  }
  assert.match(historyTemplate, /<ColumnDateFilter[^>]*label="上传时间"/)
  // 确认时间支持"未确认"空白。
  assert.match(historyTemplate, /<ColumnDateFilter[^>]*label="确认时间"[^>]*allow-blank[^>]*blank-label="未确认"/)
  assert.match(historyTemplate, /item\.confirmed_at \? formatDate\(item\.confirmed_at\) : '未确认'/)
  // 查看详情列无漏斗。
  assert.match(historyTemplate, /<span class="column-header__label">查看详情<\/span>/)
})

test('参数编码、facets 与分页接线', () => {
  assert.match(appSource, /function importFilterParams\(\)\s*\{\s*\n\s*return buildFilterParams\(importFilterState\.value, IMPORT_RANGE_PARAMS, IMPORT_DATE_PARAMS\)/)
  assert.match(appSource, /valueColumns: \['filename', 'status', 'uploaded_by'\]/)
  assert.match(appSource, /sheet: \['sheet_min', 'sheet_max'\]/)
  assert.match(appSource, /written: \['written_min', 'written_max'\]/)
  assert.match(appSource, /amount: \['amount_min', 'amount_max'\]/)
  const loader = appSource.slice(appSource.indexOf('async function loadImportFacets'), appSource.indexOf('function applyImportFilters'))
  assert.match(loader, /params\.set\('column', request\.column\)/)
  assert.match(loader, /\/api\/admin\/imports\/facets\?/)
  assert.match(loader, /page: response\.facet_page/)
})

test('筛选变化重置页码；总数与分页来自后端', () => {
  assert.match(appSource, /function applyImportFilters\(\)\s*\{\s*\n\s*importPage\.value = 1/)
  assert.match(appSource, /importTotal\.value = response\.total/)
  assert.match(appSource, /importTotalPages\.value = response\.total_pages/)
  assert.match(historyTemplate, /结果：共 \{\{ importTotal \}\} 条导入记录/)
  assert.match(historyTemplate, /@update:model-value="applyImportFilters"/)
  for (const size of [25, 50, 100, 200]) {
    assert.match(historyTemplate, new RegExp(`:value="${size}"`), `缺少每页 ${size}`)
  }
})

test('清空全部筛选一次性重置', () => {
  assert.match(appSource, /function resetImportFilters\(\)[\s\S]{0,160}?clearColumnFilters\(importFilterState\.value\)\s*\n\s*applyImportFilters\(\)/)
  assert.match(historyTemplate, /:disabled="importActiveFilterCount === 0"/)
})

test('导入历史主表无技术字段（SHA / 批次 ID / 内部 id）', () => {
  for (const technical of ['file_hash', 'item.id }}', 'import_batch_id', 'batch_id', 'file_sha']) {
    assert.ok(!historyTemplate.includes(technical), `主表不应出现 ${technical}`)
  }
  // id 仅用于 key 与详情导航。
  assert.match(historyTemplate, /:key="item\.id"/)
  assert.match(historyTemplate, /navigate\(`\/admin\/imports\/\$\{item\.id\}`\)/)
})

test('导入详情技术区默认收起、标题"技术标识"、标注仅供技术排查', () => {
  const detail = routeTemplate('admin-import-detail')
  const technical = detail.indexOf('importDetailTechnicalIdentifiers(importDetail).length > 0')
  assert.ok(technical > 0, '导入详情应保留技术标识区')
  const block = detail.slice(technical, technical + 500)
  assert.match(block, /<details>/)
  assert.doesNotMatch(block, /<details open/)
  assert.match(block, /技术标识/)
  assert.match(block, /仅供技术排查/)
})

test('导入历史页遵循 320px 表格容器内部滚动', () => {
  const css = read('src/style.css')
  assert.match(historyTemplate, /class="table-scroll history-table import-history-table"/)
  assert.match(css, /\.import-history-table table \{ min-width: 1040px; \}/)
})

test('列表 DTO 带分页字段', () => {
  const dto = clientSource.slice(clientSource.indexOf('export type ImportHistoryResponse'), clientSource.indexOf('export type ImportFacetResponse'))
  for (const field of ['page: number', 'page_size: number', 'total: number', 'total_pages: number']) {
    assert.ok(dto.includes(field), `ImportHistoryResponse 缺少 ${field}`)
  }
})

test('顶部三行结构：标题独占一行', () => {
  const heading = historyTemplate.indexOf('page-heading')
  const actions = historyTemplate.indexOf('page-actions')
  const resultbar = historyTemplate.indexOf('page-resultbar')
  assert.ok(heading > 0 && actions > heading && resultbar > actions, '顶部应为 标题/操作/结果 三行')
  assert.match(historyTemplate.slice(heading, actions), /<h2>导入历史<\/h2>/)
})
