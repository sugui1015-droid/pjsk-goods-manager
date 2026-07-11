import type { ConfirmRules, ImportBatch, ImportDetail, ImportPreviewResponse } from './api/client'

export type NumberFilter = {
  eq: string
  min: string
  max: string
}

export type PreviewFilters = {
  sheet: string
  sheetTitle: string
  batch: string
  cn: string
  category: string
  role: string
  itemName: string
  source: string
  quantity: NumberFilter
  unitPrice: NumberFilter
  amount: NumberFilter
  excluded: '' | 'yes' | 'no'
  categoryChanged: '' | 'yes' | 'no'
}

export type PreviewDetailRow = {
  id: string
  batchId: string
  sheetId: string
  sheetName: string
  sheetTitle: string
  batchName: string
  goodsSeriesName: string
  category: string
  seriesCode: string
  displayName: string
  itemName: string
  role: string
  originalCN: string
  normalizedCN: string
  quantity: number
  unitPrice: number
  amount: number
  columnName: string
  rowNumber: number
  detail: ImportDetail
  batch: ImportBatch
}

export type PreviewSummary = {
  sheetCount: number
  batchCount: number
  cnCount: number
  itemTypeCount: number
  detailCount: number
  totalQuantity: number
  totalAmount: number
}

export type CNExclusionMap = Record<string, { sheet_id?: string; batch_id?: string; cn: string }>
export type CategoryMap = Record<string, string>

export function defaultPreviewFilters(): PreviewFilters {
  return {
    sheet: '',
    sheetTitle: '',
    batch: '',
    cn: '',
    category: '',
    role: '',
    itemName: '',
    source: '',
    quantity: { eq: '', min: '', max: '' },
    unitPrice: { eq: '', min: '', max: '' },
    amount: { eq: '', min: '', max: '' },
    excluded: '',
    categoryChanged: '',
  }
}

export function flattenPreviewDetails(preview: ImportPreviewResponse | null): PreviewDetailRow[] {
  if (!preview) return []
  const rows: PreviewDetailRow[] = []
  for (const batch of preview.batches) {
    for (const detail of batch.details ?? []) {
      rows.push({
        id: detail.id,
        batchId: batch.id,
        sheetId: batch.sheet_id || detail.sheet_id || batch.sheet_name,
        sheetName: batch.sheet_name,
        sheetTitle: batch.sheet_title || detail.sheet_title || '',
        batchName: batch.batch_name,
        goodsSeriesName: detail.goods_series_name ?? detail.sheet_title ?? '',
        category: detail.product_category ?? detail.category ?? '',
        seriesCode: detail.series_code ?? detail.series_name ?? '',
        displayName: detail.display_name ?? goodsDisplayName(detail.goods_series_name ?? detail.sheet_title ?? '', detail.product_category ?? detail.category ?? ''),
        itemName: detail.item_name,
        role: detail.character_name ?? extractRoleName(detail.item_name),
        originalCN: detail.original_cn,
        normalizedCN: detail.normalized_cn,
        quantity: detail.quantity,
        unitPrice: detail.unit_price,
        amount: detail.amount,
        columnName: detail.column_name,
        rowNumber: detail.row_number,
        detail,
        batch,
      })
    }
  }
  return rows
}

export function normalizeSearch(value: string) {
  return value.trim().replace(/\s+/g, ' ').toLocaleLowerCase()
}

export function cnRuleKey(row: Pick<PreviewDetailRow, 'sheetId' | 'batchId' | 'normalizedCN' | 'originalCN'>) {
  return JSON.stringify([row.sheetId, row.batchId, row.normalizedCN || normalizeSearch(row.originalCN)])
}

export function isRowExcluded(row: PreviewDetailRow, includedSheetIds: Set<string>, excludedCNRules: CNExclusionMap, excludedDetailIds: Set<string>) {
  return !includedSheetIds.has(row.sheetId) || excludedDetailIds.has(row.id) || Boolean(excludedCNRules[cnRuleKey(row)])
}

export function detailCategory(row: PreviewDetailRow, categoryOverrides: CategoryMap) {
  return categoryOverrides[row.id] ?? row.category
}

export function filterRows(rows: PreviewDetailRow[], filters: PreviewFilters, includedSheetIds: Set<string>, excludedCNRules: CNExclusionMap, excludedDetailIds: Set<string>, categoryOverrides: CategoryMap) {
  return rows.filter((row) => {
    const excluded = isRowExcluded(row, includedSheetIds, excludedCNRules, excludedDetailIds)
    const changed = Object.prototype.hasOwnProperty.call(categoryOverrides, row.id)
    const category = detailCategory(row, categoryOverrides)
    if (!textMatches(row.sheetName, filters.sheet)) return false
    if (!textMatches(row.sheetTitle, filters.sheetTitle)) return false
    if (!textMatches(row.batchName, filters.batch)) return false
    if (!textMatches(`${row.originalCN} ${row.normalizedCN}`, filters.cn)) return false
    if (!textMatches(category, filters.category)) return false
    if (!textMatches(row.role, filters.role)) return false
    if (!textMatches(`${row.displayName} ${row.itemName} ${row.seriesCode}`, filters.itemName)) return false
    if (!textMatches(row.sheetName + "!" + row.columnName + row.rowNumber, filters.source)) return false
    if (!numberMatches(row.quantity, filters.quantity)) return false
    if (!numberMatches(row.unitPrice, filters.unitPrice)) return false
    if (!numberMatches(row.amount, filters.amount)) return false
    if (filters.excluded === 'yes' && !excluded) return false
    if (filters.excluded === 'no' && excluded) return false
    if (filters.categoryChanged === 'yes' && !changed) return false
    if (filters.categoryChanged === 'no' && changed) return false
    return true
  })
}

export function summarizeRows(rows: PreviewDetailRow[], categoryOverrides: CategoryMap = {}): PreviewSummary {
  const sheets = new Set<string>()
  const batches = new Set<string>()
  const cns = new Set<string>()
  const items = new Set<string>()
  let totalQuantity = 0
  let totalAmount = 0
  for (const row of rows) {
    sheets.add(row.sheetId)
    batches.add(row.batchId)
    cns.add(row.normalizedCN || normalizeSearch(row.originalCN))
    items.add(`${row.goodsSeriesName}|${detailCategory(row, categoryOverrides)}|${row.seriesCode}|${row.itemName}|${row.columnName}|${row.unitPrice}`)
    totalQuantity += row.quantity
    totalAmount += row.amount
  }
  return {
    sheetCount: sheets.size,
    batchCount: batches.size,
    cnCount: cns.size,
    itemTypeCount: items.size,
    detailCount: rows.length,
    totalQuantity,
    totalAmount: Number(totalAmount.toFixed(2)),
  }
}

export function uniqueOptions(rows: PreviewDetailRow[], getter: (row: PreviewDetailRow) => string) {
  return Array.from(new Set(rows.map(getter).map((value) => value.trim()).filter(Boolean))).sort((a, b) => a.localeCompare(b, 'zh-CN'))
}

export function buildConfirmRules(preview: ImportPreviewResponse | null, includedSheetIds: Set<string>, excludedCNRules: CNExclusionMap, excludedDetailIds: Set<string>, categoryOverrides: CategoryMap): ConfirmRules {
  if (!preview) return {}
  const excludedSheetIDs = preview.sheets
    .map((sheet) => sheet.id || sheet.name)
    .filter((sheetID) => !includedSheetIds.has(sheetID))
  return {
    excluded_sheet_ids: excludedSheetIDs,
    excluded_cns: Object.values(excludedCNRules),
    excluded_item_ids: Array.from(excludedDetailIds),
    category_rules: Object.entries(categoryOverrides)
      .map(([detailID, category]) => ({ item_ids: [detailID], detail_ids: [detailID], category: category.trim() }))
      .filter((rule) => rule.category.length > 0),
  }
}

export function selectedCNSummary(rows: PreviewDetailRow[], selectedIds: Set<string>) {
  return summarizeRows(rows.filter((row) => selectedIds.has(row.id)))
}

export function cleanCategoryInput(value: string) {
  const clean = value.trim()
  if (!clean) return ''
  return Array.from(clean).slice(0, 40).join('')
}

export const textFilterSeparator = ' || '

export function textFilterTokens(filter: string) {
  return filter.split(textFilterSeparator).map((value) => value.trim()).filter(Boolean)
}

function textMatches(value: string, filter: string) {
  const tokens = textFilterTokens(filter)
  if (tokens.length === 0) return true
  const target = normalizeSearch(value)
  const compactTarget = target.replace(/\s+/g, '')
  return tokens.some((token) => {
    const normalized = normalizeSearch(token)
    return target.includes(normalized) || compactTarget.includes(normalized.replace(/\s+/g, ''))
  })
}

function numberMatches(value: number, filter: NumberFilter) {
  const eq = parseNumber(filter.eq)
  const min = parseNumber(filter.min)
  const max = parseNumber(filter.max)
  if (eq !== null && value !== eq) return false
  if (min !== null && value < min) return false
  if (max !== null && value > max) return false
  return true
}

function parseNumber(value: string) {
  const clean = value.trim()
  if (!clean) return null
  const parsed = Number(clean)
  return Number.isFinite(parsed) ? parsed : null
}
function extractRoleName(itemName: string) {
  const clean = itemName.trim().toLowerCase()
  if (allowedCharacterNames.has(clean)) return clean
  for (const name of characterNameOrder) {
    if (clean.startsWith(name)) {
      const next = clean.slice(name.length, name.length + 1)
      if (next && !/[a-z]/.test(next)) return name
    }
  }
  return ''
}

const characterNameOrder = ['meiko', 'kaito', 'miku', 'shiho', 'mnr', 'airi', 'khn', 'akt', 'toya', 'tks', 'emu', 'nene', 'rui', 'knd', 'mfy', 'ena', 'mzk', 'rin', 'len', 'luka', 'ick', 'saki', 'hnm', 'hrk', 'szk', 'an']
const allowedCharacterNames = new Set(['miku', 'rin', 'len', 'luka', 'meiko', 'kaito', 'ick', 'saki', 'hnm', 'shiho', 'mnr', 'hrk', 'airi', 'szk', 'khn', 'an', 'akt', 'toya', 'tks', 'emu', 'nene', 'rui', 'knd', 'mfy', 'ena', 'mzk'])







function goodsDisplayName(goodsSeriesName: string, productCategory: string) {
  const name = goodsSeriesName.trim()
  const category = productCategory.trim()
  if (!category || category === '默认分类') return name
  if (!name) return category
  return `${name}-${category}`
}
