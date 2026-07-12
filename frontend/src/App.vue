<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  ApiError,
  getJSON,
  postForm,
  postJSON,
  type Admin,
  type AuthResponse,
  type ConfigResponse,
  type CNPaymentResponse,
  type CreatePaymentResponse,
  type HealthResponse,
  type ImportBatch,
  type ImportConfirmResponse,
  type ImportDetailResponse,
  type ImportHistoryItem,
  type ImportHistoryResponse,
  type ImportRevokeResponse,
  type ImportIssue,
  type ImportPreviewResponse,
  type OrderDetailResponse,
  type OrderListResponse,
  type OrderSummary,
  type PaymentDetailResponse,
  type PaymentItemRow,
  type PaymentListItem,
  type PaymentListResponse,
  type QueryLoginResponse,
  type QueryOrdersResponse,
  type QueryUser,
} from './api/client'
import {
  buildConfirmRules as buildImportConfirmRules,
  cleanCategoryInput,
  cnRuleKey,
  defaultPreviewFilters,
  detailCategory as adjustedDetailCategory,
  filterRows,
  flattenPreviewDetails,
  isRowExcluded,
  selectedCNSummary,
  summarizeRows,
  textFilterSeparator,
  textFilterTokens,
  uniqueOptions,
  type CategoryMap,
  type CNExclusionMap,
  type PreviewDetailRow,
} from './importPreviewTools'
const maxExcelSize = 20 * 1024 * 1024
const categoryPresets = ['吧唧', 'ep', '色纸', '立牌', '麻将', '亚克力']

type RouteName = 'home' | 'query' | 'admin-imports' | 'admin-import-history' | 'admin-import-detail' | 'admin-orders' | 'admin-order-detail' | 'admin-payments' | 'admin-payment-detail'
type IssueFilter = 'all' | 'row_error' | 'fatal_error' | 'warning' | 'notice'
type TextFilterKey = 'sheet' | 'sheetTitle' | 'batch' | 'cn' | 'category' | 'role' | 'itemName' | 'source'
type QuickFilterGroup = { key: TextFilterKey; label: string; options: string[] }

const fallbackConfig: ConfigResponse = {
  name: 'PJSK Goods Next',
  stage: 'local-shell',
  legacyAdminPort: '8512',
  legacyUserPort: '8513',
  frontendOrigins: ['http://localhost:5173', 'http://127.0.0.1:5173'],
  modules: [
    { key: 'frontend-shell', title: '前端工作台', status: 'ready', description: 'Vue 管理端已启动。' },
    { key: 'backend-core', title: 'Go 后端', status: 'queued', description: '等待 /health 与 /api/config。' },
    { key: 'excel-import', title: 'Excel 导入', status: 'queued', description: '管理员可预览、确认并查看历史。' },
  ],
}

const health = ref<HealthResponse | null>(null)
const config = ref<ConfigResponse>(fallbackConfig)
const errorMessage = ref('')
const loading = ref(true)
const checkedAt = ref('')
const activeView = ref<'overview' | 'ops' | 'legacy'>('overview')
const routeName = ref<RouteName>(routeFromPath(window.location.pathname))
const routeImportID = ref(importIDFromPath(window.location.pathname))
const routeOrderID = ref(orderIDFromPath(window.location.pathname))
const routePaymentID = ref(paymentIDFromPath(window.location.pathname))

const admin = ref<Admin | null>(null)
const authChecked = ref(false)
const authMessage = ref('')
const loginUsername = ref('admin')
const loginPassword = ref('')
const loginLoading = ref(false)

const selectedFile = ref<File | null>(null)
const uploadLoading = ref(false)
const uploadMessage = ref('')
const preview = ref<ImportPreviewResponse | null>(null)
const confirmResult = ref<ImportConfirmResponse | null>(null)
const confirmLoading = ref(false)
const confirmMessage = ref('')
const allowWarnings = ref(false)
const expandedBatchIds = ref<Set<string>>(new Set())
const issueFilter = ref<IssueFilter>('all')
const includedSheetIds = ref<Set<string>>(new Set())
const excludedCNRules = ref<CNExclusionMap>({})
const excludedDetailIds = ref<Set<string>>(new Set())
const categoryOverrides = ref<CategoryMap>({})
const customCategoryInputs = ref<Record<string, string>>({})
const previewFilters = ref(defaultPreviewFilters())
const selectedDetailIds = ref<Set<string>>(new Set())
const bulkCategoryPreset = ref('')
const bulkCustomCategory = ref('')
const bulkMessage = ref('')
const pendingBulkAction = ref('')
const openFilterKey = ref<string>('')
const filterSearches = ref<Record<string, string>>({})

const historyLoading = ref(false)
const historyMessage = ref('')
const importHistory = ref<ImportHistoryItem[]>([])
const detailLoading = ref(false)
const detailMessage = ref('')
const importDetail = ref<ImportDetailResponse | null>(null)
const revokeLoading = ref(false)
const revokeMessage = ref('')

const ordersLoading = ref(false)
const ordersMessage = ref('')
const orderItems = ref<OrderSummary[]>([])
const orderDetailLoading = ref(false)
const orderDetailMessage = ref('')
const orderDetail = ref<OrderDetailResponse | null>(null)
const orderFilters = ref({
  cn: '',
  project: '',
  item: '',
  importBatchID: '',
  status: '',
  createdFrom: '',
  createdTo: '',
})
const cnPayment = ref<CNPaymentResponse | null>(null)
const cnPaymentLoading = ref(false)
const cnPaymentMessage = ref('')
const selectedPaymentItemIds = ref<Set<string>>(new Set())
const paymentAmounts = ref<Record<string, string>>({})
const paymentMethod = ref('Alipay')
const paymentPaidAt = ref(localDateTimeInputValue())
const paymentNote = ref('')
const paymentSaving = ref(false)

const paymentRecordsLoading = ref(false)
const paymentRecordsMessage = ref('')
const paymentRecords = ref<PaymentListItem[]>([])
const paymentDetailLoading = ref(false)
const paymentDetailMessage = ref('')
const paymentDetail = ref<PaymentDetailResponse | null>(null)
const paymentFilters = ref({
  cn: '',
  paymentMethod: '',
  status: '',
  paidFrom: '',
  paidTo: '',
})

const queryCN = ref('')
const queryCode = ref('')
const queryUser = ref<QueryUser | null>(null)
const queryOrders = ref<QueryOrdersResponse | null>(null)
const queryLoading = ref(false)
const queryMessage = ref('')

const isBackendOnline = computed(() => health.value?.status === 'ok')
const readyCount = computed(() => config.value.modules.filter((item) => item.status === 'ready').length)
const queuedCount = computed(() => config.value.modules.filter((item) => item.status === 'queued').length)
const isAdminRoute = computed(() => routeName.value !== 'home' && routeName.value !== 'query')
const canUpload = computed(() => selectedFile.value !== null && !uploadLoading.value)
const fatalIssueCount = computed(() => (preview.value?.errors ?? []).filter((item) => item.level === 'fatal_error').length)
const rowErrorCount = computed(() => (preview.value?.errors ?? []).filter((item) => item.level !== 'fatal_error').length)
const canConfirm = computed(() => {
  if (!preview.value?.import_batch_id || confirmLoading.value || confirmResult.value) return false
  if (fatalIssueCount.value > 0) return false
  if ((preview.value.warnings?.length ?? 0) > 0 && !allowWarnings.value) return false
  return adjustedImportSummary.value.detailCount > 0
})
const selectedPaymentItems = computed(() => (cnPayment.value?.items ?? []).filter((item) => selectedPaymentItemIds.value.has(item.id)))
const selectedPaymentTotal = computed(() => selectedPaymentItems.value.reduce((sum, item) => roundMoney(sum + paymentAmountValue(item.id)), 0))
const hasInvalidPaymentAmount = computed(() => selectedPaymentItems.value.some(paymentAmountInvalid))
const canSavePayment = computed(() => selectedPaymentItems.value.length > 0 && selectedPaymentTotal.value > 0 && !hasInvalidPaymentAmount.value && !paymentSaving.value)

const templateCounts = computed(() => countTemplates(preview.value?.batches ?? []))
const previewRows = computed(() => flattenPreviewDetails(preview.value))
const filteredPreviewRows = computed(() => filterRows(previewRows.value, previewFilters.value, includedSheetIds.value, excludedCNRules.value, excludedDetailIds.value, categoryOverrides.value))
const adjustedImportSummary = computed(() => summarizeRows(previewRows.value.filter((row) => !isRowExcluded(row, includedSheetIds.value, excludedCNRules.value, excludedDetailIds.value)), categoryOverrides.value))
const filteredImportSummary = computed(() => summarizeRows(filteredPreviewRows.value, categoryOverrides.value))
const filteredPreviewRowIds = computed(() => new Set(filteredPreviewRows.value.map((row) => row.id)))
const filteredBatches = computed(() => (preview.value?.batches ?? []).filter((batch) => detailsForBatch(batch).length > 0 || (batch.template_type !== 'matrix' && batchMatchesCurrentFilters(batch))))
const excludedSheetCount = computed(() => preview.value?.sheets.filter((sheet) => !includedSheetIds.value.has(sheet.id || sheet.name)).length ?? 0)
const excludedCNCount = computed(() => Object.keys(excludedCNRules.value).length)
const excludedDetailCount = computed(() => excludedDetailIds.value.size)
const categoryChangeCount = computed(() => Object.keys(categoryOverrides.value).length)
const selectedImportSummary = computed(() => selectedCNSummary(previewRows.value, selectedDetailIds.value))
const filterOptions = computed(() => ({
  sheets: uniqueOptions(previewRows.value, (row) => row.sheetName),
  sheetTitles: uniqueOptions(previewRows.value, (row) => row.sheetTitle),
  batches: uniqueOptions(previewRows.value, (row) => row.batchName),
  cns: uniqueOptions(previewRows.value, (row) => row.originalCN),
  categories: uniqueOptions(previewRows.value, (row) => adjustedDetailCategory(row, categoryOverrides.value) || row.category),
  roles: uniqueOptions(previewRows.value, (row) => row.role),
  itemNames: uniqueOptions(previewRows.value, (row) => [row.displayName, row.itemName, row.seriesCode].filter(Boolean).join(' ')),
  sources: uniqueOptions(previewRows.value, (row) => row.sheetName + '!' + row.columnName + row.rowNumber),
}))
const quickFilterGroups = computed<QuickFilterGroup[]>(() => [
  { key: 'sheet', label: 'Sheet', options: filterOptions.value.sheets.slice(0, 80) },
  { key: 'sheetTitle', label: '大标题', options: filterOptions.value.sheetTitles.slice(0, 80) },
  { key: 'batch', label: '批次', options: filterOptions.value.batches.slice(0, 80) },
  { key: 'cn', label: 'CN', options: filterOptions.value.cns.slice(0, 120) },
  { key: 'category', label: '分类', options: filterOptions.value.categories.slice(0, 100) },
  { key: 'role', label: '角色', options: filterOptions.value.roles.slice(0, 60) },
  { key: 'itemName', label: '谷子名称/角色', options: filterOptions.value.itemNames.slice(0, 120) },
  { key: 'source', label: '来源', options: filterOptions.value.sources.slice(0, 120) },
])
const batchQuickFilterGroups = computed<QuickFilterGroup[]>(() => [
  { key: 'cn', label: 'CN', options: filterOptions.value.cns.slice(0, 120) },
  { key: 'itemName', label: '谷子名称/角色', options: filterOptions.value.itemNames.slice(0, 120) },
  { key: 'category', label: '分类', options: filterOptions.value.categories.slice(0, 100) },
  { key: 'role', label: '角色', options: filterOptions.value.roles.slice(0, 60) },
  { key: 'source', label: '来源', options: filterOptions.value.sources.slice(0, 120) },
])
const detailTemplateCounts = computed(() => countTemplates(importDetail.value?.preview?.batches ?? []))
const allIssues = computed(() => [
  ...(preview.value?.errors ?? []),
  ...(preview.value?.warnings ?? []),
  ...(preview.value?.notices ?? []),
])
const filteredIssues = computed(() => issueFilter.value === 'all' ? allIssues.value : allIssues.value.filter((item) => item.level === issueFilter.value))

function routeFromPath(path: string): RouteName {
  if (path === '/query') return 'query'
  if (path === '/admin/orders') return 'admin-orders'
  if (path.startsWith('/admin/orders/')) return 'admin-order-detail'
  if (path === '/admin/payments') return 'admin-payments'
  if (path.startsWith('/admin/payments/')) return 'admin-payment-detail'
  if (path === '/admin/imports/history') return 'admin-import-history'
  if (path.startsWith('/admin/imports/') && path !== '/admin/imports/preview') return 'admin-import-detail'
  if (path === '/admin/imports') return 'admin-imports'
  return 'home'
}

function importIDFromPath(path: string) {
  if (!path.startsWith('/admin/imports/')) return ''
  const id = decodeURIComponent(path.replace('/admin/imports/', '').replace(/\/$/, ''))
  return id === 'history' ? '' : id
}

function orderIDFromPath(path: string) {
  if (!path.startsWith('/admin/orders/')) return ''
  return decodeURIComponent(path.replace('/admin/orders/', '').replace(/\/$/, ''))
}

function paymentIDFromPath(path: string) {
  if (!path.startsWith('/admin/payments/')) return ''
  return decodeURIComponent(path.replace('/admin/payments/', '').replace(/\/$/, ''))
}

function navigate(path: string) {
  window.history.pushState(null, '', path)
  routeName.value = routeFromPath(path)
  routeImportID.value = importIDFromPath(path)
  routeOrderID.value = orderIDFromPath(path)
  routePaymentID.value = paymentIDFromPath(path)
  if (isAdminRoute.value) void handleRouteEntered()
  else void handlePublicRouteEntered()
}

async function handleRouteEntered() {
  if (!isAdminRoute.value) return
  await ensureAdmin()
  if (!admin.value) return
  if (routeName.value === 'admin-import-history') await loadHistory()
  if (routeName.value === 'admin-import-detail' && routeImportID.value) await loadDetail(routeImportID.value)
  if (routeName.value === 'admin-orders') await loadOrders()
  if (routeName.value === 'admin-order-detail' && routeOrderID.value) await loadOrderDetail(routeOrderID.value)
  if (routeName.value === 'admin-payments') await loadPaymentRecords()
  if (routeName.value === 'admin-payment-detail' && routePaymentID.value) await loadPaymentDetail(routePaymentID.value)
}

async function handlePublicRouteEntered() {
  if (routeName.value === 'query') await loadQueryOrders(false)
}

async function load() {
  loading.value = true
  errorMessage.value = ''
  try {
    const [healthResponse, configResponse] = await Promise.all([
      getJSON<HealthResponse>('/health'),
      getJSON<ConfigResponse>('/api/config'),
    ])
    health.value = healthResponse
    config.value = configResponse
  } catch (error) {
    health.value = null
    config.value = fallbackConfig
    errorMessage.value = error instanceof Error ? error.message : 'Backend unreachable'
  } finally {
    checkedAt.value = new Date().toLocaleString('zh-CN', { hour12: false })
    loading.value = false
  }
}

async function ensureAdmin() {
  authMessage.value = ''
  try {
    const response = await getJSON<AuthResponse>('/api/admin/me')
    admin.value = response.admin
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '请先登录管理员账号。'
    } else {
      authMessage.value = error instanceof Error ? error.message : '管理员状态检查失败'
    }
  } finally {
    authChecked.value = true
  }
}

async function login() {
  loginLoading.value = true
  authMessage.value = ''
  try {
    const response = await postJSON<AuthResponse>('/api/admin/login', {
      username: loginUsername.value,
      password: loginPassword.value,
    })
    admin.value = response.admin
    loginPassword.value = ''
    await handleRouteEntered()
  } catch (error) {
    authMessage.value = error instanceof Error ? error.message : '登录失败'
  } finally {
    loginLoading.value = false
  }
}

async function logout() {
  try {
    await postJSON<void>('/api/admin/logout', {})
  } catch (error) {
    if (!(error instanceof ApiError && error.status === 401)) {
      authMessage.value = error instanceof Error ? error.message : '退出失败'
      return
    }
  }
  admin.value = null
  preview.value = null
  confirmResult.value = null
  importHistory.value = []
  importDetail.value = null
  orderItems.value = []
  orderDetail.value = null
  paymentRecords.value = []
  paymentDetail.value = null
}

function onFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0] ?? null
  uploadMessage.value = ''
  preview.value = null
  confirmResult.value = null
  confirmMessage.value = ''
  allowWarnings.value = false
  resetPreviewAdjustments(null)

  if (!file) {
    selectedFile.value = null
    return
  }
  if (!file.name.toLowerCase().endsWith('.xlsx')) {
    selectedFile.value = null
    input.value = ''
    uploadMessage.value = '请选择 .xlsx 文件。'
    return
  }
  if (file.size > maxExcelSize) {
    selectedFile.value = null
    input.value = ''
    uploadMessage.value = '文件不能超过 20MB。'
    return
  }
  selectedFile.value = file
}

async function uploadPreview() {
  if (!selectedFile.value || uploadLoading.value) return
  uploadLoading.value = true
  uploadMessage.value = ''
  const form = new FormData()
  form.append('file', selectedFile.value)
  try {
    preview.value = await postForm<ImportPreviewResponse>('/api/admin/imports/preview', form)
    resetPreviewAdjustments(preview.value)
    confirmResult.value = null
    confirmMessage.value = ''
    allowWarnings.value = false
    expandedBatchIds.value = new Set()
    issueFilter.value = 'all'
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    uploadMessage.value = error instanceof Error ? error.message : '上传失败'
  } finally {
    uploadLoading.value = false
  }
}

async function confirmImport() {
  if (!preview.value?.import_batch_id || confirmLoading.value) return
  confirmLoading.value = true
  confirmMessage.value = ''
  try {
    confirmResult.value = await postJSON<ImportConfirmResponse>('/api/admin/imports/confirm', {
      import_batch_id: preview.value.import_batch_id,
      allow_warnings: allowWarnings.value,
      rules: buildConfirmRules(),
    })
    await loadHistory()
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    confirmMessage.value = error instanceof Error ? error.message : '确认导入失败'
  } finally {
    confirmLoading.value = false
  }
}

async function loadHistory() {
  historyLoading.value = true
  historyMessage.value = ''
  try {
    const response = await getJSON<ImportHistoryResponse>('/api/admin/imports')
    importHistory.value = response.items ?? []
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    historyMessage.value = error instanceof Error ? error.message : '导入历史加载失败'
  } finally {
    historyLoading.value = false
  }
}

async function loadDetail(id: string) {
  detailLoading.value = true
  detailMessage.value = ''
  importDetail.value = null
  try {
    importDetail.value = await getJSON<ImportDetailResponse>(`/api/admin/imports/${encodeURIComponent(id)}`)
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    detailMessage.value = error instanceof Error ? error.message : '导入详情加载失败'
  } finally {
    detailLoading.value = false
  }
}


async function revokeImport() {
  if (!importDetail.value || revokeLoading.value) return
  const item = importDetail.value.import
  const result = item.confirm_result
  const message = result
    ? `将撤销本次导入：CN ${result.cn_count} 个，明细 ${result.order_item_count} 条，总件数 ${result.total_quantity}，总金额 ${formatMoney(result.total_amount)}。确认继续吗？`
    : '将撤销本次导入产生的有效明细，确认继续吗？'
  if (!window.confirm(message)) return
  revokeLoading.value = true
  revokeMessage.value = ''
  try {
    const response = await postJSON<ImportRevokeResponse>(`/api/admin/imports/${encodeURIComponent(item.id)}/revert`, {})
    revokeMessage.value = `已撤销：影响 CN ${response.affected_cn_count} 个，明细 ${response.order_item_count} 条，总金额 ${formatMoney(response.total_amount)}。`
    await loadDetail(item.id)
    await loadHistory()
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    revokeMessage.value = error instanceof Error ? error.message : '撤销导入失败'
  } finally {
    revokeLoading.value = false
  }
}
function orderQueryString() {
  const query = new URLSearchParams()
  const filters = orderFilters.value
  if (filters.cn.trim()) query.set('cn', filters.cn.trim())
  if (filters.project.trim()) query.set('project', filters.project.trim())
  if (filters.item.trim()) query.set('item', filters.item.trim())
  if (filters.importBatchID.trim()) query.set('import_batch_id', filters.importBatchID.trim())
  if (filters.status.trim()) query.set('status', filters.status.trim())
  if (filters.createdFrom) query.set('created_from', filters.createdFrom)
  if (filters.createdTo) query.set('created_to', filters.createdTo)
  const encoded = query.toString()
  return encoded ? `?${encoded}` : ''
}

async function loadOrders() {
  ordersLoading.value = true
  ordersMessage.value = ''
  try {
    const response = await getJSON<OrderListResponse>(`/api/admin/orders${orderQueryString()}`)
    orderItems.value = response.items ?? []
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    ordersMessage.value = error instanceof Error ? error.message : '订单列表加载失败'
  } finally {
    ordersLoading.value = false
  }
}

function paymentQueryString() {
  const query = new URLSearchParams()
  const filters = paymentFilters.value
  if (filters.cn.trim()) query.set('cn', filters.cn.trim())
  if (filters.paymentMethod.trim()) query.set('payment_method', filters.paymentMethod.trim())
  if (filters.status.trim()) query.set('status', filters.status.trim())
  if (filters.paidFrom) query.set('paid_from', filters.paidFrom)
  if (filters.paidTo) query.set('paid_to', filters.paidTo)
  const encoded = query.toString()
  return encoded ? '?' + encoded : ''
}

async function loadPaymentRecords() {
  paymentRecordsLoading.value = true
  paymentRecordsMessage.value = ''
  try {
    const response = await getJSON<PaymentListResponse>('/api/admin/payments' + paymentQueryString())
    paymentRecords.value = response.items ?? []
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = 'Login expired. Please log in again.'
      return
    }
    paymentRecordsMessage.value = error instanceof Error ? error.message : 'Payment records failed to load'
  } finally {
    paymentRecordsLoading.value = false
  }
}

async function loadPaymentDetail(id: string) {
  paymentDetailLoading.value = true
  paymentDetailMessage.value = ''
  paymentDetail.value = null
  try {
    paymentDetail.value = await getJSON<PaymentDetailResponse>('/api/admin/payments/' + encodeURIComponent(id))
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = 'Login expired. Please log in again.'
      return
    }
    paymentDetailMessage.value = error instanceof Error ? error.message : 'Payment detail failed to load'
  } finally {
    paymentDetailLoading.value = false
  }
}

function resetPaymentFilters() {
  paymentFilters.value = {
    cn: '',
    paymentMethod: '',
    status: '',
    paidFrom: '',
    paidTo: '',
  }
  void loadPaymentRecords()
}

async function loadOrderDetail(id: string) {
  orderDetailLoading.value = true
  orderDetailMessage.value = ''
  orderDetail.value = null
  try {
    orderDetail.value = await getJSON<OrderDetailResponse>(`/api/admin/orders/${encodeURIComponent(id)}`)
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    orderDetailMessage.value = error instanceof Error ? error.message : '订单详情加载失败'
  } finally {
    orderDetailLoading.value = false
  }
}

async function loadCNPayment(preserveMessage = false) {
  const cn = orderFilters.value.cn.trim()
  if (!cn) {
    cnPaymentMessage.value = 'Enter one CN first.'
    return
  }
  cnPaymentLoading.value = true
  if (!preserveMessage) cnPaymentMessage.value = ''
  try {
    cnPayment.value = await getJSON<CNPaymentResponse>(`/api/admin/payments/cn?cn=${encodeURIComponent(cn)}`)
    resetPaymentDraft()
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = 'Login expired. Please log in again.'
      return
    }
    cnPayment.value = null
    cnPaymentMessage.value = error instanceof Error ? error.message : 'Payment details failed to load'
  } finally {
    cnPaymentLoading.value = false
  }
}

function resetPaymentDraft() {
  selectedPaymentItemIds.value = new Set()
  paymentAmounts.value = {}
  paymentPaidAt.value = localDateTimeInputValue()
  paymentNote.value = ''
}

function setPaymentItemSelected(item: PaymentItemRow, checked: boolean) {
  const next = new Set(selectedPaymentItemIds.value)
  if (checked) {
    next.add(item.id)
    paymentAmounts.value = { ...paymentAmounts.value, [item.id]: formatMoney(item.remaining_amount) }
  } else {
    next.delete(item.id)
  }
  selectedPaymentItemIds.value = next
}

function paymentAmountNumber(itemID: string) {
  const raw = String(paymentAmounts.value[itemID] ?? '').trim()
  if (raw === '') return null
  const value = Number(raw)
  return Number.isFinite(value) ? value : null
}

function paymentAmountValue(itemID: string) {
  const value = paymentAmountNumber(itemID)
  return value === null ? 0 : roundMoney(value)
}

function paymentAmountInvalid(item: PaymentItemRow) {
  if (!selectedPaymentItemIds.value.has(item.id)) return false
  const value = paymentAmountNumber(item.id)
  return value === null || value <= 0 || value - item.remaining_amount > 0.005
}

async function savePayment() {
  if (!cnPayment.value || !canSavePayment.value) return
  const invalidItem = selectedPaymentItems.value.find(paymentAmountInvalid)
  if (invalidItem) {
    cnPaymentMessage.value = 'Amount must be greater than 0 and not exceed remaining amount.'
    return
  }
  paymentSaving.value = true
  cnPaymentMessage.value = ''
  try {
    const submittedTotal = selectedPaymentTotal.value
    const response = await postJSON<CreatePaymentResponse>('/api/admin/payments', {
      cn: cnPayment.value.user.cn_code,
      payment_method: paymentMethod.value.trim(),
      paid_at: paymentPaidAt.value,
      note: paymentNote.value.trim(),
      idempotency_key: newIdempotencyKey(),
      items: selectedPaymentItems.value.map((item) => ({ order_item_id: item.id, amount: paymentAmountValue(item.id) })),
    })
    cnPayment.value = { ...cnPayment.value, summary: response.summary, items: response.items }
    await loadCNPayment(true)
    cnPaymentMessage.value = response.duplicate ? '检测到重复提交，未新增付款记录。' : `已保存付款 ${formatMoney(submittedTotal)}。`
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = 'Login expired. Please log in again.'
      return
    }
    cnPaymentMessage.value = error instanceof Error ? error.message : 'Payment save failed'
  } finally {
    paymentSaving.value = false
  }
}

function resetOrderFilters() {
  orderFilters.value = {
    cn: '',
    project: '',
    item: '',
    importBatchID: '',
    status: '',
    createdFrom: '',
    createdTo: '',
  }
  void loadOrders()
}


async function loginQuery() {
  queryLoading.value = true
  queryMessage.value = ''
  try {
    const response = await postJSON<QueryLoginResponse>('/api/query/login', {
      cn: queryCN.value,
      query_code: queryCode.value,
    })
    queryUser.value = response.user
    queryCode.value = ''
    await loadQueryOrders(true)
  } catch (error) {
    queryOrders.value = null
    queryUser.value = null
    queryMessage.value = error instanceof Error ? error.message : '查询登录失败'
  } finally {
    queryLoading.value = false
  }
}

async function loadQueryOrders(showMessage = true) {
  queryLoading.value = true
  if (showMessage) queryMessage.value = ''
  try {
    const response = await getJSON<QueryOrdersResponse>('/api/query/orders')
    queryOrders.value = response
    queryUser.value = response.user
    queryCN.value = response.user.cn_code
  } catch (error) {
    queryOrders.value = null
    queryUser.value = null
    if (showMessage || !(error instanceof ApiError && error.status === 401)) {
      queryMessage.value = error instanceof Error ? error.message : '查询订单失败'
    }
  } finally {
    queryLoading.value = false
  }
}

async function logoutQuery() {
  queryLoading.value = true
  queryMessage.value = ''
  try {
    await postJSON<void>('/api/query/logout', {})
    queryUser.value = null
    queryOrders.value = null
    queryCode.value = ''
    queryMessage.value = '已退出查询。'
  } catch (error) {
    queryMessage.value = error instanceof Error ? error.message : '退出失败'
  } finally {
    queryLoading.value = false
  }
}

function resetPreviewAdjustments(nextPreview: ImportPreviewResponse | null) {
  includedSheetIds.value = new Set((nextPreview?.sheets ?? []).map((sheet) => sheet.id || sheet.name))
  excludedCNRules.value = {}
  excludedDetailIds.value = new Set()
  categoryOverrides.value = {}
  customCategoryInputs.value = {}
  previewFilters.value = defaultPreviewFilters()
  selectedDetailIds.value = new Set()
  bulkCategoryPreset.value = ''
  bulkCustomCategory.value = ''
  bulkMessage.value = ''
  pendingBulkAction.value = ''
}

function sheetDisplayName(sheet: ImportPreviewResponse['sheets'][number]) {
  return sheet.title && sheet.title !== sheet.name ? `${sheet.name}（${sheet.title}）` : sheet.name
}

function batchSheetKey(batch: ImportBatch) {
  return batch.sheet_id || batch.sheet_name
}

function goodsDisplayName(goodsSeriesName: string, productCategory: string) {
  const name = goodsSeriesName.trim()
  const category = productCategory.trim()
  if (!category || category === '默认分类') return name
  if (!name) return category
  return `${name}-${category}`
}

function rowForDetail(batch: ImportBatch, detail: NonNullable<ImportBatch['details']>[number]): PreviewDetailRow {
  return {
    id: detail.id,
    batchId: batch.id,
    sheetId: batchSheetKey(batch),
    sheetName: batch.sheet_name,
    sheetTitle: batch.sheet_title || detail.sheet_title || '',
    batchName: batch.batch_name,
    goodsSeriesName: detail.goods_series_name ?? detail.sheet_title ?? '',
    category: detail.product_category ?? detail.category ?? '',
    seriesCode: detail.series_code ?? detail.series_name ?? '',
    displayName: detail.display_name ?? goodsDisplayName(detail.goods_series_name ?? detail.sheet_title ?? '', detail.product_category ?? detail.category ?? ''),
    itemName: detail.item_name,
    role: detail.character_name ?? detail.item_name,
    originalCN: detail.original_cn,
    normalizedCN: detail.normalized_cn,
    quantity: detail.quantity,
    unitPrice: detail.unit_price,
    amount: detail.amount,
    columnName: detail.column_name,
    rowNumber: detail.row_number,
    detail,
    batch,
  }
}

function isBatchIncluded(batch: ImportBatch) {
  return includedSheetIds.value.has(batchSheetKey(batch))
}

function setSheetIncluded(sheetId: string, checked: boolean) {
  const next = new Set(includedSheetIds.value)
  if (checked) next.add(sheetId)
  else next.delete(sheetId)
  includedSheetIds.value = next
}

function isDetailSelected(detailId: string) {
  return selectedDetailIds.value.has(detailId)
}

function setDetailSelected(detailId: string, checked: boolean) {
  const next = new Set(selectedDetailIds.value)
  if (checked) next.add(detailId)
  else next.delete(detailId)
  selectedDetailIds.value = next
}

function selectFilteredRows() {
  const next = new Set(selectedDetailIds.value)
  for (const row of filteredPreviewRows.value) next.add(row.id)
  selectedDetailIds.value = next
}

function unselectFilteredRows() {
  const filteredIds = new Set(filteredPreviewRows.value.map((row) => row.id))
  selectedDetailIds.value = new Set(Array.from(selectedDetailIds.value).filter((id) => !filteredIds.has(id)))
}

function selectAllRows() {
  selectedDetailIds.value = new Set(previewRows.value.map((row) => row.id))
}

function clearSelection() {
  selectedDetailIds.value = new Set()
}

function isCNExcluded(batch: ImportBatch, detail: NonNullable<ImportBatch['details']>[number]) {
  const row = rowForDetail(batch, detail)
  return Boolean(excludedCNRules.value[cnRuleKey(row)])
}

function isDetailExcluded(batch: ImportBatch, detail: NonNullable<ImportBatch['details']>[number]) {
  return isRowExcluded(rowForDetail(batch, detail), includedSheetIds.value, excludedCNRules.value, excludedDetailIds.value)
}

function setCNExcluded(batch: ImportBatch, detail: NonNullable<ImportBatch['details']>[number], checked: boolean) {
  const row = rowForDetail(batch, detail)
  const key = cnRuleKey(row)
  const next = { ...excludedCNRules.value }
  if (checked) next[key] = { sheet_id: row.sheetId, batch_id: row.batchId, cn: row.originalCN }
  else delete next[key]
  excludedCNRules.value = next
}

function setDetailExcluded(detailId: string, checked: boolean) {
  const next = new Set(excludedDetailIds.value)
  if (checked) next.add(detailId)
  else next.delete(detailId)
  excludedDetailIds.value = next
}

function detailCategory(detailOrRow: NonNullable<ImportBatch['details']>[number] | PreviewDetailRow) {
  if ('detail' in detailOrRow) return adjustedDetailCategory(detailOrRow, categoryOverrides.value)
  return categoryOverrides.value[detailOrRow.id] ?? detailOrRow.category ?? ''
}

function setDetailCategory(detailOrRow: NonNullable<ImportBatch['details']>[number] | PreviewDetailRow, value: string) {
  const id = detailOrRow.id
  const original = 'detail' in detailOrRow ? detailOrRow.category : detailOrRow.category ?? ''
  const next = { ...categoryOverrides.value }
  const clean = cleanCategoryInput(value)
  if (!clean || clean === original) delete next[id]
  else next[id] = clean
  categoryOverrides.value = next
}

function applyCustomCategory(detail: NonNullable<ImportBatch['details']>[number]) {
  setDetailCategory(detail, customCategoryInputs.value[detail.id] ?? '')
}

function applyBulkCategory(source: 'selected' | 'filtered') {
  const category = cleanCategoryInput(bulkCustomCategory.value || bulkCategoryPreset.value)
  if (!category) {
    bulkMessage.value = '请先选择或输入要批量设置的分类。'
    return
  }
  const rows = source === 'selected' ? previewRows.value.filter((row) => selectedDetailIds.value.has(row.id)) : filteredPreviewRows.value
  const next = { ...categoryOverrides.value }
  for (const row of rows) next[row.id] = category
  categoryOverrides.value = next
  bulkMessage.value = `已把 ${rows.length} 条明细批量设置为「${category}」。`
}

function restoreBulkCategory(source: 'selected' | 'filtered') {
  const rows = source === 'selected' ? previewRows.value.filter((row) => selectedDetailIds.value.has(row.id)) : filteredPreviewRows.value
  const next = { ...categoryOverrides.value }
  for (const row of rows) delete next[row.id]
  categoryOverrides.value = next
  bulkMessage.value = `已恢复 ${rows.length} 条明细到系统识别分类。`
}

function excludeSelectedCNs() {
  const rows = previewRows.value.filter((row) => selectedDetailIds.value.has(row.id))
  if (rows.length >= 20 && !window.confirm(`将按 Sheet 和批次范围排除 ${rows.length} 条已选明细涉及的 CN，继续吗？`)) return
  const next = { ...excludedCNRules.value }
  for (const row of rows) next[cnRuleKey(row)] = { sheet_id: row.sheetId, batch_id: row.batchId, cn: row.originalCN }
  excludedCNRules.value = next
  bulkMessage.value = `已按范围排除 ${rows.length} 条已选明细涉及的 CN。`
}

function excludeSelectedDetails() {
  const rows = previewRows.value.filter((row) => selectedDetailIds.value.has(row.id))
  if (rows.length >= 20 && !window.confirm(`将只排除 ${rows.length} 条具体明细，继续吗？`)) return
  const next = new Set(excludedDetailIds.value)
  for (const row of rows) next.add(row.id)
  excludedDetailIds.value = next
  bulkMessage.value = `已排除 ${rows.length} 条具体明细。`
}

function includeSelectedDetails() {
  const selected = new Set(selectedDetailIds.value)
  excludedDetailIds.value = new Set(Array.from(excludedDetailIds.value).filter((id) => !selected.has(id)))
  bulkMessage.value = '已取消已选明细的明细级排除。'
}


function cleanIntegerInput(value: string) {
  return value.replace(/[^0-9]/g, '')
}

function cleanDecimalInput(value: string) {
  const cleaned = value.replace(/[^0-9.]/g, '')
  const [integer, ...rest] = cleaned.split('.')
  const decimal = rest.join('').slice(0, 4)
  return rest.length > 0 ? `${integer}.${decimal}` : integer
}
function clearAllFilters() {
  previewFilters.value = defaultPreviewFilters()
}

function filterTokenKey(value: string) {
  return value.trim().replace(/\s+/g, ' ').toLocaleLowerCase()
}

function filterDropdownID(scope: string, key: TextFilterKey) {
  return `${scope}-${key}`
}

function filterSearch(scope: string, key: TextFilterKey) {
  return filterSearches.value[filterDropdownID(scope, key)] ?? ''
}

function setFilterSearch(scope: string, key: TextFilterKey, value: string) {
  filterSearches.value = { ...filterSearches.value, [filterDropdownID(scope, key)]: value }
}

function selectedFilterValues(key: TextFilterKey) {
  return textFilterTokens(previewFilters.value[key])
}

function selectedFilterCount(key: TextFilterKey) {
  return selectedFilterValues(key).length
}

function isFilterOpen(scope: string, key: TextFilterKey) {
  return openFilterKey.value === filterDropdownID(scope, key)
}

function toggleFilterMenu(scope: string, key: TextFilterKey) {
  const id = filterDropdownID(scope, key)
  openFilterKey.value = openFilterKey.value === id ? '' : id
}

function closeFilterMenu() {
  openFilterKey.value = ''
}

function filteredFilterOptions(group: QuickFilterGroup, scope: string) {
  const search = filterTokenKey(filterSearch(scope, group.key))
  if (!search) return group.options
  return group.options.filter((option) => filterTokenKey(option).includes(search))
}

function isFilterOptionActive(key: TextFilterKey, value: string) {
  const target = filterTokenKey(value)
  return selectedFilterValues(key).some((token) => filterTokenKey(token) === target)
}

function setFilterValues(key: TextFilterKey, values: string[]) {
  const deduped: string[] = []
  const seen = new Set<string>()
  for (const value of values) {
    const clean = value.trim()
    const normalized = filterTokenKey(clean)
    if (!clean || seen.has(normalized)) continue
    seen.add(normalized)
    deduped.push(clean)
  }
  previewFilters.value[key] = deduped.join(textFilterSeparator)
}

function toggleFilterOption(key: TextFilterKey, value: string) {
  const target = filterTokenKey(value)
  const tokens = selectedFilterValues(key)
  const exists = tokens.some((token) => filterTokenKey(token) === target)
  setFilterValues(key, exists ? tokens.filter((token) => filterTokenKey(token) !== target) : [...tokens, value])
}

function selectAllFilterOptions(group: QuickFilterGroup, scope: string) {
  const visible = filteredFilterOptions(group, scope)
  setFilterValues(group.key, [...selectedFilterValues(group.key), ...visible])
}

function invertFilterOptions(group: QuickFilterGroup, scope: string) {
  const visible = filteredFilterOptions(group, scope)
  const visibleKeys = new Set(visible.map(filterTokenKey))
  const current = selectedFilterValues(group.key)
  const currentKeys = new Set(current.map(filterTokenKey))
  const kept = current.filter((value) => !visibleKeys.has(filterTokenKey(value)))
  const added = visible.filter((value) => !currentKeys.has(filterTokenKey(value)))
  setFilterValues(group.key, [...kept, ...added])
}

function clearTextFilter(key: TextFilterKey) {
  previewFilters.value[key] = ''
}

function buildConfirmRules(): ReturnType<typeof buildImportConfirmRules> {
  return buildImportConfirmRules(preview.value, includedSheetIds.value, excludedCNRules.value, excludedDetailIds.value, categoryOverrides.value)
}

function batchMatchesCurrentFilters(batch: ImportBatch) {
  const rows = previewRows.value.filter((row) => row.batchId === batch.id)
  if (rows.length > 0) return rows.some((row) => filteredPreviewRowIds.value.has(row.id))
  const sheetText = `${batch.sheet_name} ${batch.sheet_title ?? ''}`.toLocaleLowerCase()
  const batchText = batch.batch_name.toLocaleLowerCase()
  const sheetFilter = previewFilters.value.sheet.trim().toLocaleLowerCase()
  const titleFilter = previewFilters.value.sheetTitle.trim().toLocaleLowerCase()
  const batchFilter = previewFilters.value.batch.trim().toLocaleLowerCase()
  return (!sheetFilter || sheetText.includes(sheetFilter)) && (!titleFilter || sheetText.includes(titleFilter)) && (!batchFilter || batchText.includes(batchFilter))
}

function detailsForBatch(batch: ImportBatch) {
  return (batch.details ?? []).filter((detail) => filteredPreviewRowIds.value.has(detail.id))
}
function queryOrderSources(order: { import_filenames?: string[] }) {
  return (order.import_filenames ?? []).join(' / ') || '-'
}

function orderSources(order: OrderSummary) {
  const filenames = order.import_filenames ?? []
  if (filenames.length > 0) return filenames.join(' / ')
  return (order.import_batch_ids ?? []).join(' / ') || '-'
}
function toggleBatch(batchId: string) {
  const next = new Set(expandedBatchIds.value)
  if (next.has(batchId)) next.delete(batchId)
  else next.add(batchId)
  expandedBatchIds.value = next
}

function isExpanded(batchId: string) {
  return expandedBatchIds.value.has(batchId)
}

function roundMoney(value: number) {
  return Math.round(value * 100) / 100
}

function localDateTimeInputValue() {
  const date = new Date()
  date.setMinutes(date.getMinutes() - date.getTimezoneOffset())
  return date.toISOString().slice(0, 16)
}

function newIdempotencyKey() {
  if (window.crypto?.randomUUID) return window.crypto.randomUUID()
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function formatMoney(value: number | null | undefined) {
  return Number(value ?? 0).toFixed(2)
}

function formatBytes(value: number) {
  if (value >= 1024 * 1024) return `${(value / 1024 / 1024).toFixed(2)} MB`
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${value} B`
}

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function countTemplates(batches: ImportBatch[]) {
  const counts = new Map<string, number>()
  for (const batch of batches) counts.set(batch.template_type, (counts.get(batch.template_type) ?? 0) + 1)
  return Array.from(counts.entries()).map(([name, count]) => ({ name, count }))
}


function statusLabel(status: string) {
  const labels: Record<string, string> = {
    previewed: '待确认',
    processing: '处理中',
    confirmed: '已确认',
    completed: '已完成',
    partial: '部分完成',
    failed: '失败',
    cancelled: '已取消',
    reverted: '已撤销',
    submitted: '已提交',
    draft: '草稿',
    paid: '已付款',
    partially_paid: '部分付款',
  }
  return labels[status] ?? status
}

function queryCharacterLabel(item: { character_name?: string; series_code?: string }) {
  if (!item.character_name) return '-'
  return item.series_code ? `${item.character_name}（${item.series_code}）` : item.character_name
}

function queryPaymentStatusLabel(status: string) {
  const labels: Record<string, string> = {
    unpaid: '未付款',
    partial: '部分付款',
    paid: '已付款',
  }
  return labels[status] ?? status
}

function issueLevelLabel(level: string) {
  const labels: Record<string, string> = {
    row_error: '行级错误',
    fatal_error: '致命错误',
    warning: '提醒',
    notice: '提示',
    error: '错误',
  }
  return labels[level] ?? level
}
function issueContext(issue: ImportIssue) {
  const parts = [
    issue.sheet_name ? `工作表 ${issue.sheet_name}` : '',
    issue.batch_id ? `批次 ${issue.batch_id}` : '',
    issue.row_number ? `第 ${issue.row_number} 行` : '',
    issue.column ? `列 ${issue.column}` : '',
  ].filter(Boolean)
  return parts.join(' / ') || '无位置上下文'
}

function priceTypeLabel(batch: ImportBatch) {
  return batch.calculation_price_type || (batch.price_types ?? []).map((item) => item.type).join(', ') || '无'
}

function historyTotalAmount(item: ImportHistoryItem) {
  return item.confirm_result?.total_amount ?? 0
}

window.addEventListener('popstate', () => {
  routeName.value = routeFromPath(window.location.pathname)
  routeImportID.value = importIDFromPath(window.location.pathname)
  routeOrderID.value = orderIDFromPath(window.location.pathname)
  routePaymentID.value = paymentIDFromPath(window.location.pathname)
  void handleRouteEntered()
})

onMounted(() => {
  void load()
  if (isAdminRoute.value) void handleRouteEntered()
  else {
    authChecked.value = true
    void handlePublicRouteEntered()
  }
})
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <div>
        <p class="product-label">PJSK Goods Next</p>
        <h1>谷子管理工作台</h1>
      </div>
      <div class="topbar__actions">
        <span v-if="admin" class="admin-chip">{{ admin.display_name ?? admin.username }}</span>
        <span class="connection-pill" :data-online="isBackendOnline">
          <span class="connection-dot" />
          {{ isBackendOnline ? '后端在线' : '本地前端模式' }}
        </span>
        <button class="icon-button" type="button" title="重新检查后端" @click="load" :disabled="loading">↻</button>
      </div>
    </header>

    <nav class="tabs" aria-label="工作台导航">
      <button :class="{ active: routeName === 'home' }" type="button" @click="navigate('/')">总览</button>
      <button :class="{ active: routeName === 'admin-imports' }" type="button" @click="navigate('/admin/imports')">Excel 导入预览</button>
      <button :class="{ active: routeName === 'admin-import-history' || routeName === 'admin-import-detail' }" type="button" @click="navigate('/admin/imports/history')">导入历史</button>
      <button :class="{ active: routeName === 'admin-orders' || routeName === 'admin-order-detail' }" type="button" @click="navigate('/admin/orders')">订单查询</button>
      <button :class="{ active: routeName === 'admin-payments' || routeName === 'admin-payment-detail' }" type="button" @click="navigate('/admin/payments')">付款记录</button>
    </nav>

    <main v-if="isAdminRoute" class="workspace">
      <section v-if="!authChecked" class="panel">
        <div class="panel__header"><h2>管理员状态</h2><span>checking</span></div>
        <p class="muted">正在检查登录状态。</p>
      </section>

      <section v-else-if="!admin" class="panel auth-panel">
        <div class="panel__header"><h2>管理员登录</h2><span>HttpOnly Cookie</span></div>
        <form class="login-form" @submit.prevent="login">
          <label><span>用户名</span><input v-model="loginUsername" autocomplete="username" required /></label>
          <label><span>密码</span><input v-model="loginPassword" type="password" autocomplete="current-password" required /></label>
          <button class="primary-button" type="submit" :disabled="loginLoading">{{ loginLoading ? '登录中' : '登录' }}</button>
        </form>
        <div v-if="authMessage" class="inline-alert">{{ authMessage }}</div>
      </section>

      <template v-else>
        <section class="panel admin-actions">
          <div class="panel__header">
            <div>
              <h2>管理员导入中心</h2>
              <p class="muted">当前只提供 Excel 预览、确认导入、历史与详情查询。</p>
            </div>
            <button class="secondary-button" type="button" @click="logout">退出</button>
          </div>
          <div class="action-row">
            <button class="secondary-button" type="button" @click="navigate('/admin/imports')">导入预览</button>
            <button class="secondary-button" type="button" @click="navigate('/admin/imports/history')">导入历史</button>
            <button class="secondary-button" type="button" @click="navigate('/admin/orders')">订单查询</button>
          </div>
        </section>

        <template v-if="routeName === 'admin-imports'">
          <section class="panel upload-panel">
            <div class="panel__header">
              <div>
                <h2>Excel 导入预览</h2>
                <p class="muted">仅解析预览；确认前不会写入正式订单或付款数据。</p>
              </div>
            </div>
            <div class="upload-row">
              <label class="file-picker">
                <span>选择 .xlsx 文件</span>
                <input type="file" accept=".xlsx" :disabled="uploadLoading" @change="onFileChange" />
              </label>
              <button class="primary-button" type="button" :disabled="!canUpload" @click="uploadPreview">{{ uploadLoading ? '解析中' : '上传并预览' }}</button>
              <a class="secondary-button template-download" href="/templates/pjsk-goods-import-template.xlsx" download>下载标准模板</a>
            </div>
            <p class="muted">文件大小限制 20MB；上传字段为 <code class="inline-code">file</code>。标准模板会识别为 <code class="inline-code">standard_import</code>。</p>
            <div v-if="selectedFile" class="file-line">{{ selectedFile.name }} / {{ formatBytes(selectedFile.size) }}</div>
            <div v-if="uploadMessage" class="inline-alert">{{ uploadMessage }}</div>
          </section>

          <section v-if="preview" class="summary-grid" aria-label="导入预览摘要">
            <article class="metric-tile wide-metric"><span>文件名</span><strong>{{ preview.file.original_filename }}</strong></article>
            <article class="metric-tile wide-metric"><span>SHA-256</span><strong>{{ preview.file.sha256 }}</strong></article>
            <article class="metric-tile"><span>工作表</span><strong>{{ preview.file.sheet_count }}</strong></article>
            <article class="metric-tile"><span>批次</span><strong>{{ preview.batches.length }}</strong></article>
            <article class="metric-tile"><span>行级错误</span><strong>{{ rowErrorCount }}</strong></article><article class="metric-tile"><span>致命错误</span><strong>{{ fatalIssueCount }}</strong></article>
            <article class="metric-tile"><span>Warnings</span><strong>{{ preview.warnings?.length ?? 0 }}</strong></article>
            <article class="metric-tile"><span>Notices</span><strong>{{ preview.notices?.length ?? 0 }}</strong></article>
            <article class="metric-tile wide-metric"><span>模板类型</span><strong>{{ templateCounts.map((item) => `${item.name} ${item.count}`).join(' / ') }}</strong></article>
          </section>


          <section v-if="preview" class="panel review-panel">
            <div class="panel__header">
              <div>
                <h2>导入前人工审核</h2>
                <p class="muted">先筛选，再多选和批量处理。确认导入时只提交规则，数量、单价和金额仍由后端重新计算。</p>
              </div>
              <span>最终预计 {{ adjustedImportSummary.detailCount }} 明细</span>
            </div>

            <div class="summary-grid compact-summary">
              <article class="metric-tile"><span>最终预计 CN</span><strong>{{ adjustedImportSummary.cnCount }}</strong></article>
              <article class="metric-tile"><span>最终预计明细</span><strong>{{ adjustedImportSummary.detailCount }}</strong></article>
              <article class="metric-tile"><span>最终预计件数</span><strong>{{ adjustedImportSummary.totalQuantity }}</strong></article>
              <article class="metric-tile"><span>最终预计金额</span><strong>{{ formatMoney(adjustedImportSummary.totalAmount) }}</strong></article>
              <article class="metric-tile"><span>当前筛选 CN</span><strong>{{ filteredImportSummary.cnCount }}</strong></article>
              <article class="metric-tile"><span>当前筛选明细</span><strong>{{ filteredImportSummary.detailCount }}</strong></article>
              <article class="metric-tile"><span>当前筛选件数</span><strong>{{ filteredImportSummary.totalQuantity }}</strong></article>
              <article class="metric-tile"><span>当前筛选金额</span><strong>{{ formatMoney(filteredImportSummary.totalAmount) }}</strong></article>
              <article class="metric-tile"><span>已选明细</span><strong>{{ selectedImportSummary.detailCount }}</strong></article>
              <article class="metric-tile"><span>已选 CN</span><strong>{{ selectedImportSummary.cnCount }}</strong></article>
              <article class="metric-tile"><span>排除 Sheet/CN</span><strong>{{ excludedSheetCount }} / {{ excludedCNCount }}</strong></article>
              <article class="metric-tile"><span>排除明细/分类修正</span><strong>{{ excludedDetailCount }} / {{ categoryChangeCount }}</strong></article>
            </div>

            <div class="excel-filter-bar">
              <div v-for="group in quickFilterGroups" :key="group.key" class="excel-filter">
                <button class="excel-filter-button" :class="{ active: selectedFilterCount(group.key) > 0 }" type="button" @click="toggleFilterMenu('review', group.key)">
                  <span>{{ group.label }}</span><strong v-if="selectedFilterCount(group.key) > 0">{{ selectedFilterCount(group.key) }}</strong><span>▾</span>
                </button>
                <div v-if="isFilterOpen('review', group.key)" class="excel-filter-menu">
                  <div class="excel-filter-menu__top"><strong>{{ group.label }}筛选</strong><button type="button" @click="closeFilterMenu">×</button></div>
                  <input class="excel-filter-search" :value="filterSearch('review', group.key)" placeholder="搜索，空格分隔关键词" @input="setFilterSearch('review', group.key, ($event.target as HTMLInputElement).value)" />
                  <div class="excel-filter-actions"><button type="button" @click="selectAllFilterOptions(group, 'review')">全选</button><button type="button" @click="invertFilterOptions(group, 'review')">反选</button><button type="button" @click="clearTextFilter(group.key)">清除</button></div>
                  <div class="excel-filter-options">
                    <label v-for="option in filteredFilterOptions(group, 'review')" :key="`${group.key}-${option}`" class="excel-filter-option"><input type="checkbox" :checked="isFilterOptionActive(group.key, option)" @change="toggleFilterOption(group.key, option)" /><span>{{ option }}</span></label>
                    <p v-if="filteredFilterOptions(group, 'review').length === 0" class="muted">没有匹配项。</p>
                  </div>
                </div>
              </div>
              <label><span>排除状态</span><select v-model="previewFilters.excluded"><option value="">全部</option><option value="no">未排除</option><option value="yes">已排除</option></select></label>
              <label><span>分类状态</span><select v-model="previewFilters.categoryChanged"><option value="">全部</option><option value="no">未修改</option><option value="yes">已修改</option></select></label>
              <button class="secondary-button" type="button" @click="clearAllFilters">清除全部筛选</button>
            </div>
            <div class="number-filter-grid">
              <label><span>数量 =</span><input v-model="previewFilters.quantity.eq" type="number" step="1" min="0" @input="previewFilters.quantity.eq = cleanIntegerInput(($event.target as HTMLInputElement).value)" /></label>
              <label><span>数量范围</span><div class="range-inputs"><input v-model="previewFilters.quantity.min" type="number" step="1" min="0" @input="previewFilters.quantity.min = cleanIntegerInput(($event.target as HTMLInputElement).value)" placeholder="最小" /><input v-model="previewFilters.quantity.max" type="number" step="1" min="0" @input="previewFilters.quantity.max = cleanIntegerInput(($event.target as HTMLInputElement).value)" placeholder="最大" /></div></label>
              <label><span>价格 =</span><input v-model="previewFilters.unitPrice.eq" type="number" step="0.0001" min="0" @input="previewFilters.unitPrice.eq = cleanDecimalInput(($event.target as HTMLInputElement).value)" /></label>
              <label><span>价格范围</span><div class="range-inputs"><input v-model="previewFilters.unitPrice.min" type="number" step="0.0001" min="0" @input="previewFilters.unitPrice.min = cleanDecimalInput(($event.target as HTMLInputElement).value)" placeholder="最小" /><input v-model="previewFilters.unitPrice.max" type="number" step="0.0001" min="0" @input="previewFilters.unitPrice.max = cleanDecimalInput(($event.target as HTMLInputElement).value)" placeholder="最大" /></div></label>
              <label><span>小计 =</span><input v-model="previewFilters.amount.eq" type="number" step="0.0001" min="0" @input="previewFilters.amount.eq = cleanDecimalInput(($event.target as HTMLInputElement).value)" /></label>
              <label><span>小计范围</span><div class="range-inputs"><input v-model="previewFilters.amount.min" type="number" step="0.0001" min="0" @input="previewFilters.amount.min = cleanDecimalInput(($event.target as HTMLInputElement).value)" placeholder="最小" /><input v-model="previewFilters.amount.max" type="number" step="0.0001" min="0" @input="previewFilters.amount.max = cleanDecimalInput(($event.target as HTMLInputElement).value)" placeholder="最大" /></div></label>
            </div>
            <div class="bulk-toolbar">
              <div><strong>已选择 {{ selectedImportSummary.detailCount }} 条明细，涉及 {{ selectedImportSummary.cnCount }} 个 CN</strong><small>筛选不会丢失已选内容。</small></div>
              <button class="secondary-button" type="button" @click="selectFilteredRows">选择当前筛选结果</button>
              <button class="secondary-button" type="button" @click="unselectFilteredRows">取消当前筛选选择</button>
              <button class="secondary-button" type="button" @click="selectAllRows">选择整个预览</button>
              <button class="secondary-button" type="button" @click="clearSelection">清空选择</button>
              <button class="secondary-button" type="button" @click="clearAllFilters">清除筛选</button>
            </div>

            <div class="bulk-toolbar bulk-toolbar--edit">
              <select v-model="bulkCategoryPreset"><option value="">选择预设分类</option><option v-for="preset in categoryPresets" :key="preset" :value="preset">{{ preset }}</option></select>
              <input v-model="bulkCustomCategory" maxlength="40" placeholder="或输入自定义分类" />
              <button class="primary-button" type="button" :disabled="selectedImportSummary.detailCount === 0" @click="applyBulkCategory('selected')">批量改已选分类</button>
              <button class="secondary-button" type="button" :disabled="filteredImportSummary.detailCount === 0" @click="applyBulkCategory('filtered')">批量改筛选结果</button>
              <button class="secondary-button" type="button" :disabled="selectedImportSummary.detailCount === 0" @click="restoreBulkCategory('selected')">恢复已选原分类</button>
              <button class="secondary-button" type="button" :disabled="selectedImportSummary.detailCount === 0" @click="excludeSelectedCNs">批量排除已选 CN</button>
              <button class="secondary-button" type="button" :disabled="selectedImportSummary.detailCount === 0" @click="excludeSelectedDetails">只排除已选明细</button>
              <button class="secondary-button" type="button" :disabled="selectedImportSummary.detailCount === 0" @click="includeSelectedDetails">取消已选明细排除</button>
            </div>
            <div v-if="bulkMessage" class="inline-alert">{{ bulkMessage }}</div>

            <div class="table-scroll compact-table">
              <table>
                <thead><tr><th>是否导入</th><th>Sheet</th><th>大标题</th><th>模板</th><th>批次</th><th>金额差额</th></tr></thead>
                <tbody>
                  <tr v-for="sheet in preview.sheets" :key="sheet.id || sheet.name">
                    <td><input type="checkbox" :checked="includedSheetIds.has(sheet.id || sheet.name)" @change="setSheetIncluded(sheet.id || sheet.name, ($event.target as HTMLInputElement).checked)" /></td>
                    <td>{{ sheetDisplayName(sheet) }}</td>
                    <td>{{ sheet.title || '-' }}</td>
                    <td>{{ sheet.template_type }}</td>
                    <td>{{ sheet.batch_count }}</td>
                    <td :class="{ danger: Math.abs(sheet.difference) > 0.01 }">{{ formatMoney(sheet.difference) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section v-if="preview" class="panel confirm-panel">
            <div class="panel__header"><div><h2>确认导入</h2><p class="muted">确认时使用服务器保存的预览结果，不信任前端明细。</p></div><span>{{ preview.import_batch_id }}</span></div>
            <div v-if="fatalIssueCount > 0" class="inline-alert">当前预览存在致命错误，无法确认导入。</div><div v-else-if="rowErrorCount > 0" class="inline-alert">发现 {{ rowErrorCount }} 条错误记录，确认导入时将自动跳过；其余 {{ adjustedImportSummary.detailCount }} 条有效明细可以继续导入。</div>
            <label v-if="(preview.warnings?.length ?? 0) > 0" class="confirm-check"><input v-model="allowWarnings" type="checkbox" /><span>我已人工检查 warnings，允许继续确认导入。</span></label>
            <div class="confirm-actions">
              <button class="primary-button" type="button" :disabled="!canConfirm" @click="confirmImport">{{ confirmLoading ? '确认中' : `确认导入 ${adjustedImportSummary.detailCount} 条有效明细` }}</button>
              <span class="muted">不会写入 payments / payment_items。</span>
            </div>
            <div v-if="confirmMessage" class="inline-alert">{{ confirmMessage }}</div>
            <div v-if="confirmResult" class="confirm-result">
              <strong>导入已确认</strong>
              <span>CN {{ confirmResult.cn_count }}</span>
              <span>商品 {{ confirmResult.product_count }}</span>
              <span>订单 {{ confirmResult.order_count }}</span>
              <span>明细 {{ confirmResult.order_item_count }}</span>
              <span>总件数 {{ confirmResult.total_quantity }}</span>
              <span>总金额 {{ formatMoney(confirmResult.total_amount) }}</span>
              <span>接受提醒 {{ confirmResult.warnings_accepted ? '是' : '否' }}</span><span>跳过错误 {{ confirmResult.skipped_error_count ?? 0 }}</span>
              <span>{{ formatDate(confirmResult.confirmed_at) }}</span>
            </div>
          </section>

          <section v-if="preview" class="panel">
            <div class="panel__header"><h2>批次列表</h2><span>{{ filteredBatches.length }} / {{ preview.batches.length }} batches</span></div>
            <div class="excel-filter-bar compact-excel-filter-bar">
              <div v-for="group in batchQuickFilterGroups" :key="group.key" class="excel-filter">
                <button class="excel-filter-button" :class="{ active: selectedFilterCount(group.key) > 0 }" type="button" @click="toggleFilterMenu('batch', group.key)">
                  <span>{{ group.label }}</span><strong v-if="selectedFilterCount(group.key) > 0">{{ selectedFilterCount(group.key) }}</strong><span>▾</span>
                </button>
                <div v-if="isFilterOpen('batch', group.key)" class="excel-filter-menu">
                  <div class="excel-filter-menu__top"><strong>{{ group.label }}筛选</strong><button type="button" @click="closeFilterMenu">×</button></div>
                  <input class="excel-filter-search" :value="filterSearch('batch', group.key)" placeholder="搜索，空格分隔关键词" @input="setFilterSearch('batch', group.key, ($event.target as HTMLInputElement).value)" />
                  <div class="excel-filter-actions"><button type="button" @click="selectAllFilterOptions(group, 'batch')">全选</button><button type="button" @click="invertFilterOptions(group, 'batch')">反选</button><button type="button" @click="clearTextFilter(group.key)">清除</button></div>
                  <div class="excel-filter-options">
                    <label v-for="option in filteredFilterOptions(group, 'batch')" :key="`${group.key}-${option}`" class="excel-filter-option"><input type="checkbox" :checked="isFilterOptionActive(group.key, option)" @change="toggleFilterOption(group.key, option)" /><span>{{ option }}</span></label>
                    <p v-if="filteredFilterOptions(group, 'batch').length === 0" class="muted">没有匹配项。</p>
                  </div>
                </div>
              </div>
              <label><span>排除状态</span><select v-model="previewFilters.excluded"><option value="">全部</option><option value="no">未排除</option><option value="yes">已排除</option></select></label>
              <label><span>分类状态</span><select v-model="previewFilters.categoryChanged"><option value="">全部</option><option value="no">未修改</option><option value="yes">已修改</option></select></label>
              <button class="secondary-button" type="button" @click="clearAllFilters">清除筛选</button>
            </div>
            <div class="number-filter-grid compact-number-filter-grid">
              <label><span>数量 =</span><input v-model="previewFilters.quantity.eq" type="number" step="1" min="0" @input="previewFilters.quantity.eq = cleanIntegerInput(($event.target as HTMLInputElement).value)" /></label>
              <label><span>数量范围</span><div class="range-inputs"><input v-model="previewFilters.quantity.min" type="number" step="1" min="0" @input="previewFilters.quantity.min = cleanIntegerInput(($event.target as HTMLInputElement).value)" placeholder="最小" /><input v-model="previewFilters.quantity.max" type="number" step="1" min="0" @input="previewFilters.quantity.max = cleanIntegerInput(($event.target as HTMLInputElement).value)" placeholder="最大" /></div></label>
              <label><span>价格 =</span><input v-model="previewFilters.unitPrice.eq" type="number" step="0.0001" min="0" @input="previewFilters.unitPrice.eq = cleanDecimalInput(($event.target as HTMLInputElement).value)" /></label>
              <label><span>价格范围</span><div class="range-inputs"><input v-model="previewFilters.unitPrice.min" type="number" step="0.0001" min="0" @input="previewFilters.unitPrice.min = cleanDecimalInput(($event.target as HTMLInputElement).value)" placeholder="最小" /><input v-model="previewFilters.unitPrice.max" type="number" step="0.0001" min="0" @input="previewFilters.unitPrice.max = cleanDecimalInput(($event.target as HTMLInputElement).value)" placeholder="最大" /></div></label>
              <label><span>小计 =</span><input v-model="previewFilters.amount.eq" type="number" step="0.0001" min="0" @input="previewFilters.amount.eq = cleanDecimalInput(($event.target as HTMLInputElement).value)" /></label>
              <label><span>小计范围</span><div class="range-inputs"><input v-model="previewFilters.amount.min" type="number" step="0.0001" min="0" @input="previewFilters.amount.min = cleanDecimalInput(($event.target as HTMLInputElement).value)" placeholder="最小" /><input v-model="previewFilters.amount.max" type="number" step="0.0001" min="0" @input="previewFilters.amount.max = cleanDecimalInput(($event.target as HTMLInputElement).value)" placeholder="最大" /></div></label>
            </div>
            <div class="batch-list">
              <article v-for="batch in filteredBatches" :key="batch.id" class="batch-card">
                <button class="batch-card__summary" type="button" @click="toggleBatch(batch.id)">
                  <span>{{ isExpanded(batch.id) ? '▾' : '▸' }}</span><strong>{{ batch.sheet_title ? `${batch.sheet_name}（${batch.sheet_title}）` : batch.sheet_name }} / {{ batch.batch_name }}</strong><span class="status-chip" data-state="draft">{{ batch.template_type }}</span><span v-if="!isBatchIncluded(batch)" class="simple-note">该 Sheet 已排除</span><span v-else-if="batch.template_type === 'simple_cn_amount'" class="simple-note">仅预览，不转换为订单项</span>
                </button>
                <div class="batch-metrics"><span>CN {{ batch.cn_count }}</span><span>种类 {{ batch.item_type_count }}</span><span>总件数 {{ batch.total_quantity }}</span><span>表格 {{ formatMoney(batch.table_amount) }}</span><span>程序 {{ formatMoney(batch.calculated_amount) }}</span><span :class="{ danger: Math.abs(batch.difference) > 0.01 }">差额 {{ formatMoney(batch.difference) }}</span><span>价格 {{ priceTypeLabel(batch) }}</span></div>
                <div v-if="isExpanded(batch.id)" class="batch-detail">
                                    <div class="table-scroll detail-table"><table><thead><tr><th>选择</th><th>导入</th><th>原始 CN</th><th>谷子名称</th><th>系列编号</th><th>角色/种类</th><th>分类修正</th><th>数量</th><th>价格</th><th>小计</th><th>来源</th></tr></thead><tbody><tr v-if="detailsForBatch(batch).length === 0"><td colspan="11">当前筛选下无订单项明细。</td></tr><tr v-for="detail in detailsForBatch(batch)" :key="detail.id || `${batch.id}-${detail.row_number}-${detail.column_name}-${detail.original_cn}`" :class="{ muted: isDetailExcluded(batch, detail) }"><td><input type="checkbox" :checked="isDetailSelected(detail.id)" @change="setDetailSelected(detail.id, ($event.target as HTMLInputElement).checked)" /></td><td><input type="checkbox" :disabled="!isBatchIncluded(batch)" :checked="isBatchIncluded(batch) && !isCNExcluded(batch, detail)" @change="setCNExcluded(batch, detail, !($event.target as HTMLInputElement).checked)" /></td><td><strong>{{ detail.original_cn }}</strong><small v-if="isCNExcluded(batch, detail)">已排除该范围内 CN</small><small v-if="excludedDetailIds.has(detail.id)">已排除此明细</small><button class="secondary-button tiny-button" type="button" @click="setDetailExcluded(detail.id, !excludedDetailIds.has(detail.id))">{{ excludedDetailIds.has(detail.id) ? '取消明细排除' : '只排除此明细' }}</button></td><td>{{ detail.display_name || detail.sheet_title || '-' }}</td><td>{{ detail.series_code || detail.series_name || '-' }}</td><td>{{ detail.item_name }}</td><td><div class="category-editor"><select :disabled="!isBatchIncluded(batch) || isCNExcluded(batch, detail)" :value="detailCategory(detail)" @change="setDetailCategory(detail, ($event.target as HTMLSelectElement).value)"><option value="">保持原分类</option><option v-for="preset in categoryPresets" :key="preset" :value="preset">{{ preset }}</option></select><div class="custom-category"><input v-model="customCategoryInputs[detail.id]" :disabled="!isBatchIncluded(batch) || isCNExcluded(batch, detail)" maxlength="40" placeholder="自定义制品" /><button class="secondary-button" type="button" :disabled="!isBatchIncluded(batch) || isCNExcluded(batch, detail)" @click="applyCustomCategory(detail)">应用</button></div><small>{{ detailCategory(detail) || detail.product_category || detail.category || '默认分类' }}</small></div></td><td>{{ detail.quantity }}</td><td>{{ formatMoney(detail.unit_price) }}</td><td>{{ formatMoney(detail.amount) }}</td><td>{{ detail.sheet_name }}!{{ detail.column_name }}{{ detail.row_number }}</td></tr></tbody></table></div>
                </div>
              </article>
            </div>
          </section>

          <section v-if="preview" class="panel">
            <div class="panel__header"><h2>问题列表</h2><div class="filter-buttons"><button :class="{ active: issueFilter === 'all' }" type="button" @click="issueFilter = 'all'">全部</button><button :class="{ active: issueFilter === 'row_error' }" type="button" @click="issueFilter = 'row_error'">行级错误</button><button :class="{ active: issueFilter === 'fatal_error' }" type="button" @click="issueFilter = 'fatal_error'">致命错误</button><button :class="{ active: issueFilter === 'warning' }" type="button" @click="issueFilter = 'warning'">提醒</button><button :class="{ active: issueFilter === 'notice' }" type="button" @click="issueFilter = 'notice'">提示</button></div></div>
            <div class="issue-list"><article v-if="filteredIssues.length === 0" class="issue-row">当前筛选下没有问题。</article><article v-for="issue in filteredIssues" :key="`${issue.level}-${issue.code}-${issue.sheet_name}-${issue.batch_id}-${issue.row_number}-${issue.column}`" class="issue-row" :data-level="issue.level"><strong>{{ issueLevelLabel(issue.level) }} / {{ issue.code }}</strong><span>{{ issue.message }}</span><small>{{ issue.code === 'image_formula_ignored' ? '图片公式已忽略 / ' : '' }}{{ issueContext(issue) }}</small></article></div>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-payments'">
          <section class="panel">
            <div class="panel__header"><div><h2>付款记录</h2><p class="muted">只读查看付款流水和关联明细；本页不提供删除、作废或冲正。</p></div><button class="secondary-button" type="button" :disabled="paymentRecordsLoading" @click="loadPaymentRecords">{{ paymentRecordsLoading ? '加载中' : '刷新' }}</button></div>
            <form class="order-filters" @submit.prevent="loadPaymentRecords"><label><span>CN</span><input v-model="paymentFilters.cn" placeholder="CN 或显示名" /></label><label><span>付款方式</span><input v-model="paymentFilters.paymentMethod" placeholder="Alipay / WeChat / Bank" /></label><label><span>付款状态</span><select v-model="paymentFilters.status"><option value="">全部</option><option value="approved">approved</option><option value="submitted">submitted</option><option value="rejected">rejected</option><option value="voided">voided</option></select></label><label><span>付款开始时间</span><input v-model="paymentFilters.paidFrom" type="datetime-local" /></label><label><span>付款结束时间</span><input v-model="paymentFilters.paidTo" type="datetime-local" /></label><div class="filter-actions"><button class="primary-button" type="submit" :disabled="paymentRecordsLoading">查询</button><button class="secondary-button" type="button" @click="resetPaymentFilters">重置</button></div></form>
            <div v-if="paymentRecordsMessage" class="inline-alert">{{ paymentRecordsMessage }}</div>
            <div class="table-scroll history-table"><table><thead><tr><th>付款时间</th><th>CN</th><th>付款金额</th><th>付款方式</th><th>状态</th><th>操作管理员</th><th>备注</th><th>关联明细数量</th><th></th></tr></thead><tbody><tr v-if="!paymentRecordsLoading && paymentRecords.length === 0"><td colspan="9">暂无付款记录。</td></tr><tr v-for="payment in paymentRecords" :key="payment.id"><td>{{ formatDate(payment.paid_at) }}</td><td><strong>{{ payment.cn_code }}</strong><small>{{ payment.display_name || '-' }}</small></td><td>{{ formatMoney(payment.amount) }}</td><td>{{ payment.payment_method || '-' }}</td><td><span class="status-chip" :data-state="payment.status">{{ statusLabel(payment.status) }}</span></td><td>{{ payment.created_by || '-' }}</td><td>{{ payment.note || '-' }}</td><td>{{ payment.payment_item_count }}</td><td><button class="secondary-button" type="button" @click="navigate('/admin/payments/' + payment.id)">详情</button></td></tr></tbody></table></div>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-payment-detail'">
          <section class="panel">
            <div class="panel__header"><div><h2>付款详情</h2><p class="muted">{{ routePaymentID }}</p></div><button class="secondary-button" type="button" @click="navigate('/admin/payments')">返回付款记录</button></div>
            <div v-if="paymentDetailMessage" class="inline-alert">{{ paymentDetailMessage }}</div><p v-if="paymentDetailLoading" class="muted">正在加载付款详情。</p>
            <template v-if="paymentDetail"><div class="summary-grid"><article class="metric-tile"><span>CN</span><strong>{{ paymentDetail.payment.cn_code }}</strong></article><article class="metric-tile"><span>付款金额</span><strong>{{ formatMoney(paymentDetail.payment.amount) }}</strong></article><article class="metric-tile"><span>付款方式</span><strong>{{ paymentDetail.payment.payment_method || '-' }}</strong></article><article class="metric-tile"><span>状态</span><strong>{{ statusLabel(paymentDetail.payment.status) }}</strong></article><article class="metric-tile"><span>操作管理员</span><strong>{{ paymentDetail.payment.created_by || '-' }}</strong></article><article class="metric-tile"><span>付款时间</span><strong>{{ formatDate(paymentDetail.payment.paid_at) }}</strong></article><article class="metric-tile"><span>关联明细</span><strong>{{ paymentDetail.payment.payment_item_count }}</strong></article><article class="metric-tile wide-metric"><span>备注</span><strong>{{ paymentDetail.payment.note || '-' }}</strong></article></div><section class="panel nested-panel"><div class="panel__header"><h2>关联付款明细</h2><span>{{ paymentDetail.payment.items.length }} items</span></div><div class="table-scroll detail-table"><table><thead><tr><th>订单号</th><th>项目名</th><th>谷子名称</th><th>本次分配金额</th><th>当前付款状态</th><th>来源</th></tr></thead><tbody><tr v-if="paymentDetail.payment.items.length === 0"><td colspan="6">无关联明细。</td></tr><tr v-for="item in paymentDetail.payment.items" :key="item.id"><td>{{ item.order_no }}</td><td>{{ item.project_name }}</td><td>{{ item.display_name || item.product_name }}<small>{{ item.category || item.character_name || item.series_code || '-' }}</small></td><td>{{ formatMoney(item.applied_amount) }}</td><td>{{ queryPaymentStatusLabel(item.payment_status) }}</td><td>{{ item.import_filename || '-' }}<small>{{ item.source_row_key || item.source_sheet || '' }}</small></td></tr></tbody></table></div></section></template>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-orders'">
          <section class="panel">
            <div class="panel__header">
              <div>
                <h2>订单只读查询</h2>
                <p class="muted">查看 Excel 确认导入后的正式订单数据；本页不允许修改、删除或撤销。</p>
              </div>
              <button class="secondary-button" type="button" :disabled="ordersLoading" @click="loadOrders">刷新</button>
            </div>

            <form class="order-filters" @submit.prevent="loadOrders">
              <label><span>CN</span><input v-model="orderFilters.cn" placeholder="CN 或显示名" /></label>
              <label><span>项目/批次</span><input v-model="orderFilters.project" placeholder="项目名称或编码" /></label>
              <label><span>谷子种类</span><input v-model="orderFilters.item" placeholder="商品、分类或角色" /></label>
              <label><span>导入批次 ID</span><input v-model="orderFilters.importBatchID" placeholder="import_batch_id" /></label>
              <label>
                <span>订单状态</span>
                <select v-model="orderFilters.status">
                  <option value="">全部</option>
                  <option value="draft">draft</option>
                  <option value="submitted">submitted</option>
                  <option value="partially_paid">partially_paid</option>
                  <option value="paid">paid</option>
                  <option value="cancelled">cancelled</option>
                </select>
              </label>
              <label><span>创建时间起</span><input v-model="orderFilters.createdFrom" type="date" /></label>
              <label><span>创建时间止</span><input v-model="orderFilters.createdTo" type="date" /></label>
              <div class="filter-actions">
                <button class="primary-button" type="submit" :disabled="ordersLoading">{{ ordersLoading ? '查询中' : '查询' }}</button>
                <button class="secondary-button" type="button" @click="resetOrderFilters">重置</button>
              </div>
            </form>

            <section class="panel nested-panel payment-entry-panel">
              <div class="panel__header"><div><h2>Payment entry</h2><p class="muted">Load one CN, select order items, and save full or partial payment.</p></div><button class="secondary-button" type="button" :disabled="cnPaymentLoading" @click="() => loadCNPayment()">{{ cnPaymentLoading ? 'Loading' : 'Load CN payments' }}</button></div>
              <div v-if="cnPaymentMessage" class="inline-alert">{{ cnPaymentMessage }}</div>
              <template v-if="cnPayment">
                <div class="summary-grid compact-summary payment-summary"><article class="metric-tile"><span>CN</span><strong>{{ cnPayment.user.cn_code }}</strong></article><article class="metric-tile"><span>Total</span><strong>{{ formatMoney(cnPayment.summary.total_amount) }}</strong></article><article class="metric-tile"><span>Paid</span><strong>{{ formatMoney(cnPayment.summary.paid_amount) }}</strong></article><article class="metric-tile"><span>Remaining</span><strong>{{ formatMoney(cnPayment.summary.remaining_amount) }}</strong></article><article class="metric-tile"><span>Items U/P/F</span><strong>{{ cnPayment.summary.unpaid_count }} / {{ cnPayment.summary.partial_count }} / {{ cnPayment.summary.paid_count }}</strong></article></div>
                <div class="payment-form"><label><span>Method</span><input v-model="paymentMethod" placeholder="Alipay / WeChat / Bank" /></label><label><span>Paid at</span><input v-model="paymentPaidAt" type="datetime-local" /></label><label class="payment-note"><span>Note</span><input v-model="paymentNote" maxlength="200" placeholder="Optional" /></label><div class="payment-actions"><span>{{ selectedPaymentItems.length }} items / {{ formatMoney(selectedPaymentTotal) }}</span><button class="primary-button" type="button" :disabled="!canSavePayment" @click="savePayment">{{ paymentSaving ? 'Saving' : 'Save payment' }}</button></div></div>
                <div class="table-scroll detail-table payment-table"><table><thead><tr><th>Select</th><th>Project / Order</th><th>Item</th><th>Qty</th><th>Due</th><th>Paid</th><th>Remain</th><th>This payment</th><th>Status</th><th>Source</th></tr></thead><tbody><tr v-if="cnPayment.items.length === 0"><td colspan="10">No payable order items.</td></tr><tr v-for="item in cnPayment.items" :key="item.id" :class="{ muted: item.remaining_amount <= 0 }"><td><input type="checkbox" :disabled="item.remaining_amount <= 0" :checked="selectedPaymentItemIds.has(item.id)" @change="setPaymentItemSelected(item, ($event.target as HTMLInputElement).checked)" /></td><td>{{ item.project_name }}<small>{{ item.order_no }}</small></td><td>{{ item.display_name || item.product_name }}<small>{{ item.category || item.character_name || '-' }}</small></td><td>{{ item.quantity }}</td><td>{{ formatMoney(item.amount) }}</td><td>{{ formatMoney(item.paid_amount) }}</td><td>{{ formatMoney(item.remaining_amount) }}</td><td><input class="amount-input" v-model="paymentAmounts[item.id]" :disabled="!selectedPaymentItemIds.has(item.id)" type="number" min="0.01" step="0.01" :max="item.remaining_amount" :class="{ invalid: paymentAmountInvalid(item) }" /></td><td>{{ queryPaymentStatusLabel(item.payment_status) }}</td><td>{{ item.import_filename || '-' }}<small>{{ item.source_row_key || item.source_sheet || '' }}</small></td></tr></tbody></table></div>
                <section class="panel nested-panel payment-history-panel"><div class="panel__header"><h2>Payment history</h2><span>{{ cnPayment.payments.length }} records</span></div><div class="table-scroll compact-table"><table><thead><tr><th>Amount</th><th>Method</th><th>Paid at</th><th>Admin</th><th>Note</th><th>Status</th></tr></thead><tbody><tr v-if="cnPayment.payments.length === 0"><td colspan="6">No payment records.</td></tr><tr v-for="payment in cnPayment.payments" :key="payment.id"><td>{{ formatMoney(payment.amount) }}</td><td>{{ payment.payment_method || '-' }}</td><td>{{ formatDate(payment.paid_at) }}</td><td>{{ payment.created_by || '-' }}</td><td>{{ payment.note || '-' }}</td><td>{{ payment.status }}</td></tr></tbody></table></div></section>
              </template>
            </section>

            <div v-if="ordersMessage" class="inline-alert">{{ ordersMessage }}</div>
            <div class="table-scroll history-table">
              <table>
                <thead>
                  <tr>
                    <th>CN</th>
                    <th>项目</th>
                    <th>状态</th>
                    <th>种类数</th>
                    <th>商品总数</th>
                    <th>订单总金额</th>
                    <th>来源导入批次</th>
                    <th>创建时间</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="!ordersLoading && orderItems.length === 0"><td colspan="9">暂无订单数据。</td></tr>
                  <tr v-for="order in orderItems" :key="order.id">
                    <td><strong>{{ order.cn_code }}</strong><small>{{ order.display_name || '-' }}</small></td>
                    <td>{{ order.project_name }}<small>{{ order.order_no }}</small></td>
                    <td><span class="status-chip" data-state="draft">{{ order.status }}</span></td>
                    <td>{{ order.item_type_count }}</td>
                    <td>{{ order.total_quantity }}</td>
                    <td>{{ formatMoney(order.total_amount) }}</td>
                    <td class="hash-cell">{{ orderSources(order) }}</td>
                    <td>{{ formatDate(order.created_at) }}</td>
                    <td><button class="secondary-button" type="button" @click="navigate(`/admin/orders/${order.id}`)">详情</button></td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-order-detail'">
          <section class="panel">
            <div class="panel__header">
              <div>
                <h2>订单详情</h2>
                <p class="muted">{{ routeOrderID }}</p>
              </div>
              <button class="secondary-button" type="button" @click="navigate('/admin/orders')">返回订单列表</button>
            </div>
            <div v-if="orderDetailMessage" class="inline-alert">{{ orderDetailMessage }}</div>
            <p v-if="orderDetailLoading" class="muted">正在加载订单详情。</p>

            <template v-if="orderDetail">
              <div class="summary-grid">
                <article class="metric-tile"><span>CN</span><strong>{{ orderDetail.order.cn_code }}</strong></article>
                <article class="metric-tile wide-metric"><span>项目</span><strong>{{ orderDetail.order.project_name }}</strong></article>
                <article class="metric-tile"><span>状态</span><strong>{{ orderDetail.order.status }}</strong></article>
                <article class="metric-tile"><span>明细数</span><strong>{{ orderDetail.order.item_count }}</strong></article>
                <article class="metric-tile"><span>商品总数</span><strong>{{ orderDetail.order.total_quantity }}</strong></article>
                <article class="metric-tile"><span>订单总额</span><strong>{{ formatMoney(orderDetail.order.total_amount) }}</strong></article>
                <article class="metric-tile wide-metric"><span>来源</span><strong>{{ orderSources(orderDetail.order) }}</strong></article>
                <article class="metric-tile"><span>创建时间</span><strong>{{ formatDate(orderDetail.order.created_at) }}</strong></article>
              </div>

              <section class="panel nested-panel">
                <div class="panel__header"><h2>谷子明细</h2><span>{{ orderDetail.order.items.length }} items</span></div>
                <div class="table-scroll detail-table">
                  <table>
                    <thead>
                      <tr>
                        <th>谷子种类</th>
                        <th>分类</th>
                        <th>SKU</th>
                        <th>数量</th>
                        <th>单价</th>
                        <th>小计</th>
                        <th>付款状态</th>
                        <th>来源 Excel / 批次</th>
                        <th>来源位置</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-if="orderDetail.order.items.length === 0"><td colspan="9">无明细。</td></tr>
                      <tr v-for="item in orderDetail.order.items" :key="item.id">
                        <td>{{ item.product_name }}</td>
                        <td>{{ item.category || item.character_name || '-' }}</td>
                        <td class="hash-cell">{{ item.sku || '-' }}</td>
                        <td>{{ item.quantity }}</td>
                        <td>{{ formatMoney(item.unit_price) }}</td>
                        <td>{{ formatMoney(item.amount) }}</td>
                        <td>{{ item.payment_status }}</td>
                        <td class="hash-cell">{{ item.import_filename || item.import_batch_id || '-' }}</td>
                        <td>{{ item.source_row_key || item.source_sheet || '-' }}</td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </section>
            </template>
          </section>
        </template>
        <template v-else-if="routeName === 'admin-import-history'">
          <section class="panel">
            <div class="panel__header"><div><h2>导入历史</h2><p class="muted">可查看导入记录，并按导入批次安全软撤销。</p></div><button class="secondary-button" type="button" :disabled="historyLoading" @click="loadHistory">刷新</button></div>
            <div v-if="historyMessage" class="inline-alert">{{ historyMessage }}</div>
            <div class="table-scroll history-table"><table><thead><tr><th>文件</th><th>SHA-256</th><th>状态</th><th>上传</th><th>确认</th><th>工作表/批次</th><th>问题</th><th>写入结果</th><th>总金额</th><th></th></tr></thead><tbody><tr v-if="!historyLoading && importHistory.length === 0"><td colspan="10">暂无导入记录。</td></tr><tr v-for="item in importHistory" :key="item.id"><td><strong>{{ item.original_filename }}</strong><small>{{ formatBytes(item.file_size) }}</small></td><td class="hash-cell">{{ item.file_hash }}</td><td><span class="status-chip" :data-state="item.status">{{ statusLabel(item.status) }}</span><small v-if="item.revoked_at">{{ formatDate(item.revoked_at) }}</small></td><td>{{ item.uploaded_by || '-' }}<small>{{ formatDate(item.created_at) }}</small></td><td>{{ item.confirmed_by || '-' }}<small>{{ formatDate(item.confirmed_at) }}</small></td><td>{{ item.sheet_count }} / {{ item.batch_count }}</td><td>E {{ item.error_count }} / W {{ item.warning_count }} / N {{ item.notice_count }}</td><td>{{ item.confirm_result ? `${item.confirm_result.order_count} 单 / ${item.confirm_result.order_item_count} 明细` : '-' }}<small v-if="item.revoke_result">已撤销 {{ item.revoke_result.order_item_count }} 明细</small></td><td>{{ formatMoney(historyTotalAmount(item)) }}</td><td><button class="secondary-button" type="button" @click="navigate(`/admin/imports/${item.id}`)">详情</button></td></tr></tbody></table></div>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-import-detail'">
          <section class="panel">
            <div class="panel__header"><div><h2>导入详情</h2><p class="muted">{{ routeImportID }}</p></div><button class="secondary-button" type="button" @click="navigate('/admin/imports/history')">返回历史</button></div>
            <div v-if="detailMessage" class="inline-alert">{{ detailMessage }}</div>
            <p v-if="detailLoading" class="muted">正在加载详情。</p>
            <template v-if="importDetail">
              <div class="summary-grid">
                <article class="metric-tile wide-metric"><span>文件名</span><strong>{{ importDetail.import.original_filename }}</strong></article>
                <article class="metric-tile wide-metric"><span>SHA-256</span><strong>{{ importDetail.import.file_hash }}</strong></article>
                <article class="metric-tile"><span>状态</span><strong>{{ statusLabel(importDetail.import.status) }}</strong></article>
                <article class="metric-tile"><span>工作表</span><strong>{{ importDetail.import.sheet_count }}</strong></article>
                <article class="metric-tile"><span>批次</span><strong>{{ importDetail.import.batch_count }}</strong></article>
                <article class="metric-tile"><span>问题</span><strong>E {{ importDetail.import.error_count }} / W {{ importDetail.import.warning_count }} / N {{ importDetail.import.notice_count }}</strong></article>
                <article class="metric-tile"><span>接受提醒</span><strong>{{ importDetail.import.warnings_accepted ? '是' : '否' }}</strong></article>
                <article class="metric-tile"><span>确认管理员</span><strong>{{ importDetail.import.confirmed_by || '-' }}</strong></article>
                <article class="metric-tile"><span>确认时间</span><strong>{{ formatDate(importDetail.import.confirmed_at) }}</strong></article><article class="metric-tile"><span>撤销管理员</span><strong>{{ importDetail.import.revoked_by || '-' }}</strong></article><article class="metric-tile"><span>撤销时间</span><strong>{{ formatDate(importDetail.import.revoked_at) }}</strong></article>
              </div>

              <section v-if="importDetail.import.confirm_result" class="confirm-result detail-result">
                <strong>写入结果</strong><span>CN {{ importDetail.import.confirm_result.cn_count }}</span><span>商品 {{ importDetail.import.confirm_result.product_count }}</span><span>订单 {{ importDetail.import.confirm_result.order_count }}</span><span>明细 {{ importDetail.import.confirm_result.order_item_count }}</span><span>跳过错误 {{ importDetail.import.confirm_result.skipped_error_count ?? 0 }}</span><span>总件数 {{ importDetail.import.confirm_result.total_quantity }}</span><span>总金额 {{ formatMoney(importDetail.import.confirm_result.total_amount) }}</span>
              </section>

              <section v-if="importDetail.import.revoke_result" class="confirm-result detail-result">
                <strong>撤销结果</strong><span>CN {{ importDetail.import.revoke_result.affected_cn_count }}</span><span>订单 {{ importDetail.import.revoke_result.order_count }}</span><span>明细 {{ importDetail.import.revoke_result.order_item_count }}</span><span>总件数 {{ importDetail.import.revoke_result.total_quantity }}</span><span>总金额 {{ formatMoney(importDetail.import.revoke_result.total_amount) }}</span><span>{{ formatDate(importDetail.import.revoke_result.revoked_at) }}</span>
              </section>

              <section v-if="importDetail.import.status === 'confirmed' && importDetail.import.confirm_result" class="panel nested-panel danger-panel">
                <div class="panel__header"><div><h2>安全撤销</h2><p class="muted">软撤销本次导入产生的订单明细，不影响其他导入批次里的相同 CN。</p></div><button class="secondary-button" type="button" :disabled="revokeLoading" @click="revokeImport">{{ revokeLoading ? '撤销中' : '撤销本次导入' }}</button></div>
                <p class="muted">将影响 CN {{ importDetail.import.confirm_result.cn_count }} 个，明细 {{ importDetail.import.confirm_result.order_item_count }} 条，总件数 {{ importDetail.import.confirm_result.total_quantity }}，总金额 {{ formatMoney(importDetail.import.confirm_result.total_amount) }}。</p>
                <div v-if="revokeMessage" class="inline-alert">{{ revokeMessage }}</div>
              </section>

              <section v-if="importDetail.preview" class="panel nested-panel">
                <div class="panel__header"><h2>解析摘要</h2><span>{{ detailTemplateCounts.map((item) => `${item.name} ${item.count}`).join(' / ') }}</span></div>
                <div class="table-scroll compact-table"><table><thead><tr><th>工作表</th><th>模板</th><th>批次</th><th>表格金额</th><th>程序金额</th><th>差额</th></tr></thead><tbody><tr v-for="sheet in importDetail.preview.sheets" :key="sheet.name"><td>{{ sheet.name }}</td><td>{{ sheet.template_type }}</td><td>{{ sheet.batch_count }}</td><td>{{ formatMoney(sheet.table_amount) }}</td><td>{{ formatMoney(sheet.calculated_amount) }}</td><td :class="{ danger: Math.abs(sheet.difference) > 0.01 }">{{ formatMoney(sheet.difference) }}</td></tr></tbody></table></div>
              </section>

              <section v-if="importDetail.preview" class="panel nested-panel">
                <div class="panel__header"><h2>问题</h2><span>{{ (importDetail.preview.errors?.length ?? 0) + (importDetail.preview.warnings?.length ?? 0) + (importDetail.preview.notices?.length ?? 0) }}</span></div>
                <div class="issue-list"><article v-for="issue in [...(importDetail.preview.errors ?? []), ...(importDetail.preview.warnings ?? []), ...(importDetail.preview.notices ?? [])]" :key="`${issue.level}-${issue.code}-${issue.sheet_name}-${issue.batch_id}-${issue.row_number}-${issue.column}`" class="issue-row" :data-level="issue.level"><strong>{{ issueLevelLabel(issue.level) }} / {{ issue.code }}</strong><span>{{ issue.message }}</span><small>{{ issueContext(issue) }}</small></article><article v-if="!((importDetail.preview.errors?.length ?? 0) + (importDetail.preview.warnings?.length ?? 0) + (importDetail.preview.notices?.length ?? 0))" class="issue-row">无问题。</article></div>
              </section>
            </template>
          </section>
        </template>
      </template>
    </main>

    <main v-else class="workspace">
      <template v-if="routeName === 'query'">
        <section class="panel query-panel">
          <div class="panel__header">
            <div>
              <h2>CN 查询</h2>
              <p class="muted">输入 CN 和查询码后，只能查看当前 CN 自己的订单明细。</p>
            </div>
            <button v-if="queryUser" class="secondary-button" type="button" :disabled="queryLoading" @click="logoutQuery">退出查询</button>
          </div>
          <form v-if="!queryUser" class="login-form query-login" @submit.prevent="loginQuery">
            <label><span>CN</span><input v-model="queryCN" autocomplete="username" required placeholder="输入自己的 CN" /></label>
            <label><span>查询码</span><input v-model="queryCode" type="password" autocomplete="current-password" required placeholder="管理员提供的查询码" /></label>
            <button class="primary-button" type="submit" :disabled="queryLoading">{{ queryLoading ? '查询中' : '查询订单' }}</button>
          </form>
          <div v-if="queryMessage" class="inline-alert">{{ queryMessage }}</div>
        </section>

        <template v-if="queryOrders">
          <section class="summary-grid">
            <article class="metric-tile"><span>CN</span><strong>{{ queryOrders.user.cn_code }}</strong></article>
            <article class="metric-tile"><span>订单数</span><strong>{{ queryOrders.orders.length }}</strong></article>
            <article class="metric-tile"><span>总件数</span><strong>{{ queryOrders.total_quantity }}</strong></article>
            <article class="metric-tile"><span>总金额</span><strong>{{ formatMoney(queryOrders.total_amount) }}</strong></article>
          </section>

          <section v-for="order in queryOrders.orders" :key="order.id" class="panel query-order-card">
            <div class="panel__header">
              <div>
                <h2>{{ order.project_name }}</h2>
                <p class="muted">{{ order.order_no }} / {{ formatDate(order.created_at) }}</p>
              </div>
              <div class="query-order-total"><strong>{{ formatMoney(order.total_amount) }}</strong><span>{{ order.total_quantity }} 件</span></div>
            </div>
            <p class="muted">来源：{{ queryOrderSources(order) }}</p>
            <div class="table-scroll detail-table">
              <table>
                <thead>
                  <tr><th>谷子名称</th><th>分类</th><th>角色</th><th>数量</th><th>单价</th><th>小计</th><th>付款状态</th><th>所属批次</th></tr>
                </thead>
                <tbody>
                  <tr v-for="item in order.items" :key="item.id">
                    <td>{{ item.display_name || item.goods_name }}</td>
                    <td>{{ item.category || '-' }}</td>
                    <td>{{ queryCharacterLabel(item) }}</td>
                    <td>{{ item.quantity }}</td>
                    <td>{{ formatMoney(item.unit_price) }}</td>
                    <td>{{ formatMoney(item.amount) }}</td>
                    <td>{{ queryPaymentStatusLabel(item.payment_status) }}</td>
                    <td>{{ item.import_filename || item.import_batch_id || '-' }}<small>{{ item.source_sheet || '' }}</small></td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
          <section v-if="queryOrders.orders.length === 0" class="panel"><p class="muted">当前 CN 暂无可查询订单。</p></section>
        </template>
      </template>

      <template v-else>
      <section class="metrics" aria-label="运行指标">
        <article class="metric-tile"><span>可用模块</span><strong>{{ readyCount }}</strong></article>
        <article class="metric-tile"><span>待接入模块</span><strong>{{ queuedCount }}</strong></article>
        <article class="metric-tile"><span>后端服务</span><strong>{{ health?.service ?? '未连接' }}</strong></article>
        <article class="metric-tile"><span>检查时间</span><strong>{{ checkedAt || '待检查' }}</strong></article>
      </section>
      <nav class="subtabs" aria-label="首页信息"><button :class="{ active: activeView === 'overview' }" type="button" @click="activeView = 'overview'">总览</button><button :class="{ active: activeView === 'ops' }" type="button" @click="activeView = 'ops'">接口</button><button :class="{ active: activeView === 'legacy' }" type="button" @click="activeView = 'legacy'">旧版</button></nav>
      <section v-if="activeView === 'overview'" class="panel"><div class="panel__header"><h2>模块状态</h2><span>{{ config.stage }}</span></div><div v-if="errorMessage" class="inline-alert">{{ errorMessage }}</div><div class="module-table"><div class="module-row module-row--head"><span>模块</span><span>状态</span><span>说明</span></div><div v-for="item in config.modules" :key="item.key" class="module-row"><strong>{{ item.title }}</strong><span class="status-chip" :data-state="item.status">{{ item.status }}</span><span>{{ item.description }}</span></div></div></section>
      <section v-else-if="activeView === 'ops'" class="workspace workspace--two"><div class="panel"><div class="panel__header"><h2>后端接口</h2><span>{{ isBackendOnline ? 'online' : 'offline' }}</span></div><div class="endpoint-list"><div><code>GET /health</code><span>{{ health?.status ?? 'waiting' }}</span></div><div><code>GET /api/config</code><span>{{ isBackendOnline ? 'ready' : 'waiting' }}</span></div><div><code>POST /api/admin/imports/preview</code><span>admin only</span></div><div><code>POST /api/admin/imports/confirm</code><span>admin only</span></div><div><code>GET /api/admin/imports</code><span>admin only</span></div></div></div><div class="panel"><div class="panel__header"><h2>下一步</h2><span>history first</span></div><ol class="task-list"><li>验收确认导入幂等保护。</li><li>查看导入历史和详情。</li><li>再进入订单只读管理。</li></ol></div></section>
      <section v-else class="workspace workspace--two"><div class="panel"><div class="panel__header"><h2>Streamlit 管理端</h2><span>port {{ config.legacyAdminPort }}</span></div><code>cd legacy-streamlit && python -m streamlit run main.py --server.port {{ config.legacyAdminPort }}</code></div><div class="panel"><div class="panel__header"><h2>Streamlit 用户端</h2><span>port {{ config.legacyUserPort }}</span></div><code>cd legacy-streamlit && python -m streamlit run user.py --server.port {{ config.legacyUserPort }}</code></div></section>
      </template>
    </main>
  </div>
</template>
