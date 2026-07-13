<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  ApiError,
  apiUrl,
  bindQueryCode,
  changeQueryCode,
  createQueryCodeBindToken,
  getJSON,
  postForm,
  postJSON,
  patchJSON,
  type Admin,
  type AdminUserDetailResponse,
  type AdminUserListItem,
  type AdminUserListResponse,
  type AdminUserListSummary,
  type AdminUserMergePreviewResponse,
  type AdminUserMergeResponse,
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

type RouteName = 'home' | 'query' | 'admin-imports' | 'admin-import-history' | 'admin-import-detail' | 'admin-orders' | 'admin-order-detail' | 'admin-payments' | 'admin-payment-detail' | 'admin-users' | 'admin-user-detail'
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
const routeUserID = ref(userIDFromPath(window.location.pathname))

const adminUsers = ref<AdminUserListItem[]>([])
const adminUsersSummary = ref<AdminUserListSummary | null>(null)
const adminUsersLoading = ref(false)
const adminUsersMessage = ref('')
const adminUserFilters = ref({ cn: '', status: '' })
const adminUserDetail = ref<AdminUserDetailResponse | null>(null)
const adminUserDetailLoading = ref(false)
const adminUserDetailMessage = ref('')
const mergeTargetCN = ref('')
const mergeReason = ref('')
const mergePreview = ref<AdminUserMergePreviewResponse | null>(null)
const mergePreviewLoading = ref(false)
const mergeSaving = ref(false)
const mergeMessage = ref('')
const queryCodeDraft = ref('')
const queryCodeSaving = ref(false)
const queryAccessSaving = ref(false)
const queryAccountMessage = ref('')
// One-time bind token result lives only in this in-memory ref: it is never
// written to localStorage/sessionStorage and disappears on refresh.
const bindTokenGenerating = ref(false)
const bindTokenResult = ref<{ token: string; expiresAt: string } | null>(null)
const bindTokenMessage = ref('')

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
  series: '',
  category: '',
  role: '',
  importBatchID: '',
  status: '',
  createdFrom: '',
  createdTo: '',
})
const paymentEntryCN = ref('')
const cnPayment = ref<CNPaymentResponse | null>(null)
const cnPaymentLoading = ref(false)
const cnPaymentMessage = ref('')
const selectedPaymentItemIds = ref<Set<string>>(new Set())
const paymentAmounts = ref<Record<string, string>>({})
type PaymentMethod = 'alipay' | 'wechat' | 'bank' | 'cash' | 'other' | ''
const paymentMethod = ref<PaymentMethod>('')
const paymentPaidAt = ref(localDateTimeInputValue())
const paymentNote = ref('')
const paymentSaving = ref(false)
const paymentDraftIdempotencyKey = ref('')
const paymentCreatedID = ref('')

const paymentRecordsLoading = ref(false)
const paymentRecordsMessage = ref('')
const paymentRecords = ref<PaymentListItem[]>([])
const paymentDetailLoading = ref(false)
const paymentDetailMessage = ref('')
const paymentDetail = ref<PaymentDetailResponse | null>(null)
const paymentVoiding = ref(false)
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
const queryOldCode = ref('')
const queryNewCode = ref('')
const queryConfirmCode = ref('')
const queryCodeChanging = ref(false)
const querySecurityMessage = ref('')
// First-time bind flow on the user login page. The bind token is held only
// in this in-memory form state — never persisted, gone on refresh.
const queryView = ref<'login' | 'bind'>('login')
const bindCN = ref('')
const bindTokenInput = ref('')
const bindNewCode = ref('')
const bindConfirmCode = ref('')
const bindSubmitting = ref(false)
const bindMessage = ref('')

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
const selectedPaymentBaseCents = computed(() => selectedPaymentItems.value.reduce((sum, item) => sum + paymentAmountCents(item.id), 0))
const selectedPaymentTotal = computed(() => selectedPaymentBaseCents.value / 100)
const hasInvalidPaymentAmount = computed(() => selectedPaymentItems.value.some(paymentAmountInvalid))
const canSavePayment = computed(() => selectedPaymentItems.value.length > 0 && selectedPaymentBaseCents.value > 0 && paymentMethod.value !== '' && !hasInvalidPaymentAmount.value && !paymentSaving.value)

const paymentBaseCents = computed(() => selectedPaymentBaseCents.value)
const paymentFeeCents = computed(() => {
  if (paymentMethod.value === 'alipay') return 0
  if (paymentMethod.value === 'wechat') return Math.floor((paymentBaseCents.value + 999) / 1000)
  return 0
})
const paymentFeeAmount = computed(() => paymentFeeCents.value / 100)
const paymentPayableAmount = computed(() => (paymentBaseCents.value + paymentFeeCents.value) / 100)

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
  if (path === '/admin/users') return 'admin-users'
  if (path.startsWith('/admin/users/')) return 'admin-user-detail'
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

function userIDFromPath(path: string) {
  if (!path.startsWith('/admin/users/')) return ''
  return decodeURIComponent(path.replace('/admin/users/', '').replace(/\/$/, ''))
}

function navigate(path: string) {
  window.history.pushState(null, '', path)
  routeName.value = routeFromPath(path)
  routeImportID.value = importIDFromPath(path)
  routeOrderID.value = orderIDFromPath(path)
  routePaymentID.value = paymentIDFromPath(path)
  routeUserID.value = userIDFromPath(path)
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
  if (routeName.value === 'admin-users') await loadAdminUsers()
  if (routeName.value === 'admin-user-detail' && routeUserID.value) await loadAdminUserDetail(routeUserID.value)
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
    checkedAt.value = new Date().toLocaleString('zh-CN', { hour12: false, timeZone: 'Asia/Shanghai' })
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
  if (filters.series.trim()) query.set('series', filters.series.trim())
  if (filters.category.trim()) query.set('category', filters.category.trim())
  if (filters.role.trim()) query.set('role', filters.role.trim())
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

async function voidPayment() {
  if (!paymentDetail.value || paymentDetail.value.payment.status !== 'approved' || paymentVoiding.value) return
  const payment = paymentDetail.value.payment
  const principal = payment.principal_amount ?? payment.amount
  const fee = payment.fee_amount ?? 0
  const total = payment.total_amount ?? payment.payable_amount ?? roundMoney(principal + fee)
  const reason = window.prompt('请输入撤销原因')?.trim()
  if (!reason) {
    paymentDetailMessage.value = '撤销原因不能为空。'
    return
  }
  const confirmMessage = [
    '确认撤销这笔付款？撤销后将回滚关联订单明细付款状态。',
    `CN：${payment.cn_code}`,
    `本金：${formatMoney(principal)}`,
    `手续费：${formatMoney(fee)}`,
    `实付总额：${formatMoney(total)}`,
    `影响明细：${payment.payment_item_count} 条`,
  ].join('\n')
  if (!window.confirm(confirmMessage)) return
  paymentVoiding.value = true
  paymentDetailMessage.value = ''
  try {
    paymentDetail.value = await postJSON<PaymentDetailResponse>('/api/admin/payments/' + encodeURIComponent(payment.id) + '/void', { reason })
    paymentDetailMessage.value = '付款已撤销，订单状态已回滚。'
    await loadPaymentRecords()
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = 'Login expired. Please log in again.'
      return
    }
    paymentDetailMessage.value = error instanceof Error ? error.message : 'Payment void failed'
  } finally {
    paymentVoiding.value = false
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

async function loadAdminUsers() {
  adminUsersLoading.value = true
  adminUsersMessage.value = ''
  try {
    const params = new URLSearchParams()
    if (adminUserFilters.value.cn.trim()) params.set('cn', adminUserFilters.value.cn.trim())
    if (adminUserFilters.value.status) params.set('status', adminUserFilters.value.status)
    const suffix = params.toString() ? `?${params.toString()}` : ''
    const response = await getJSON<AdminUserListResponse>('/api/admin/users' + suffix)
    adminUsers.value = response.items ?? []
    adminUsersSummary.value = response.summary ?? null
    if (adminUsers.value.length === 0) adminUsersMessage.value = '没有符合条件的用户。'
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    adminUsersSummary.value = null
    adminUsersMessage.value = error instanceof Error ? error.message : '用户列表加载失败'
  } finally {
    adminUsersLoading.value = false
  }
}

function resetAdminUserFilters() {
  adminUserFilters.value = { cn: '', status: '' }
  void loadAdminUsers()
}

async function downloadExport(path: string, params: Record<string, string>) {
  const searchParams = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value.trim()) searchParams.set(key, value.trim())
  }
  const suffix = searchParams.toString() ? `?${searchParams.toString()}` : ''
  const response = await fetch(apiUrl(path + suffix), { credentials: 'include' })
  if (!response.ok) {
    if (response.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      navigate('/admin')
      return
    }
    window.alert(`导出失败：HTTP ${response.status}`)
    return
  }
  const blob = await response.blob()
  const disposition = response.headers.get('Content-Disposition') ?? ''
  const utf8Name = disposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1]
  const quotedName = disposition.match(/filename="?([^";]+)"?/i)?.[1]
  const fallbackName = path.split('/').pop() || 'export.xlsx'
  const filename = utf8Name ? decodeURIComponent(utf8Name) : (quotedName || fallbackName)
  const objectURL = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = objectURL
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(objectURL)
}

function exportAdminUsersExcel() {
  void downloadExport('/api/admin/export/users.xlsx', {
    cn: adminUserFilters.value.cn,
    status: adminUserFilters.value.status,
  })
}

function exportAdminUsersCSV() {
  void downloadExport('/api/admin/export/users.csv', {
    cn: adminUserFilters.value.cn,
    status: adminUserFilters.value.status,
  })
}

function exportPaymentsExcel() {
  void downloadExport('/api/admin/export/payments.xlsx', {
    cn: paymentFilters.value.cn,
    payment_method: paymentFilters.value.paymentMethod,
    status: paymentFilters.value.status,
    paid_from: paymentFilters.value.paidFrom,
    paid_to: paymentFilters.value.paidTo,
  })
}

function exportPaymentsCSV() {
  void downloadExport('/api/admin/export/payments.csv', {
    cn: paymentFilters.value.cn,
    payment_method: paymentFilters.value.paymentMethod,
    status: paymentFilters.value.status,
    paid_from: paymentFilters.value.paidFrom,
    paid_to: paymentFilters.value.paidTo,
  })
}

function exportUnpaidItemsExcel() {
  void downloadExport('/api/admin/export/order-items.xlsx', { unpaid_only: '1' })
}

function exportUnpaidItemsCSV() {
  void downloadExport('/api/admin/export/order-items.csv', { unpaid_only: '1' })
}

function exportOrderItemsExcel() {
  void downloadExport('/api/admin/export/order-items.xlsx', {
    cn: orderFilters.value.cn,
    project: orderFilters.value.project,
    series: orderFilters.value.series,
    category: orderFilters.value.category,
    role: orderFilters.value.role,
  })
}

function exportOrderItemsCSV() {
  void downloadExport('/api/admin/export/order-items.csv', {
    cn: orderFilters.value.cn,
    project: orderFilters.value.project,
    series: orderFilters.value.series,
    category: orderFilters.value.category,
    role: orderFilters.value.role,
  })
}

async function copyIdentifier(value?: string) {
  const text = String(value ?? '').trim()
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.style.position = 'fixed'
    textarea.style.left = '-9999px'
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    document.body.removeChild(textarea)
  }
}


type TechnicalIdentifier = {
  type: string
  context: string
  value: string
}

function addTechnicalIdentifier(rows: TechnicalIdentifier[], type: string, context: string, value?: string) {
  const normalized = String(value ?? '').trim()
  if (!normalized) return
  rows.push({ type, context, value: normalized })
}

// Multiple order items can share the same order/user ID. Merge rows that
// have the same type and value so a 12-item payment does not repeat the
// same order ID 12 times; the merged context still lists every item so the
// order-item-to-product correspondence is not lost.
function mergeTechnicalIdentifiers(rows: TechnicalIdentifier[]) {
  const order: string[] = []
  const byKey = new Map<string, TechnicalIdentifier & { contexts: Set<string> }>()
  for (const row of rows) {
    const key = row.type + ' ' + row.value
    const existing = byKey.get(key)
    if (existing) {
      existing.contexts.add(row.context)
      continue
    }
    order.push(key)
    byKey.set(key, { ...row, contexts: new Set([row.context]) })
  }
  return order.map((key) => {
    const entry = byKey.get(key)!
    const contexts = [...entry.contexts]
    const context = contexts.length > 3 ? `${contexts.slice(0, 3).join('、')} 等 ${contexts.length} 项` : contexts.join('、')
    return { type: entry.type, context, value: entry.value }
  })
}

function itemBusinessLabel(item: { display_name?: string; product_name?: string; order_no?: string; project_name?: string }) {
  const name = item.display_name || item.product_name || '明细'
  const order = item.order_no ? item.order_no + ' / ' : ''
  return order + name
}

function paymentDetailTechnicalIdentifiers(detail: PaymentDetailResponse | null) {
  const rows: TechnicalIdentifier[] = []
  if (!detail) return rows
  addTechnicalIdentifier(rows, '付款 ID', detail.payment.cn_code || '付款记录', detail.payment.id)
  for (const item of detail.payment.items) {
    const label = itemBusinessLabel(item)
    addTechnicalIdentifier(rows, '订单 ID', label, item.order_id)
    addTechnicalIdentifier(rows, '订单明细 ID', label, item.order_item_id)
    addTechnicalIdentifier(rows, '商品 ID', label, item.product_id)
    addTechnicalIdentifier(rows, '商品 SKU', label, item.sku)
    addTechnicalIdentifier(rows, '系列编码', label, item.series_code)
    addTechnicalIdentifier(rows, '来源 Sheet', label, item.source_sheet)
    addTechnicalIdentifier(rows, '来源位置', label, item.source_row_key)
  }
  return rows
}

function adminUserTechnicalIdentifiers(detail: AdminUserDetailResponse | null) {
  const rows: TechnicalIdentifier[] = []
  if (!detail) return rows
  addTechnicalIdentifier(rows, '用户 ID', detail.user.cn_code, detail.user.id)
  for (const order of detail.orders) {
    addTechnicalIdentifier(rows, '订单 ID', order.order_no, order.id)
    for (const item of order.items) {
      const label = itemBusinessLabel({ ...item, order_no: order.order_no })
      addTechnicalIdentifier(rows, '订单明细 ID', label, item.id)
      addTechnicalIdentifier(rows, '商品 ID', label, item.product_id)
      addTechnicalIdentifier(rows, '商品 SKU', label, item.sku)
      addTechnicalIdentifier(rows, '系列编码', label, item.series_code)
      addTechnicalIdentifier(rows, '来源 Sheet', label, item.source_sheet)
      addTechnicalIdentifier(rows, '来源位置', label, item.source_row_key)
    }
  }
  for (const payment of detail.payments) {
    addTechnicalIdentifier(rows, '付款 ID', payment.paid_at || detail.user.cn_code, payment.id)
  }
  for (const merge of detail.merges) {
    addTechnicalIdentifier(rows, '合并记录 ID', merge.other_cn, merge.id)
  }
  return rows
}

function orderDetailTechnicalIdentifiers(detail: OrderDetailResponse | null) {
  const rows: TechnicalIdentifier[] = []
  if (!detail) return rows
  addTechnicalIdentifier(rows, '订单 ID', detail.order.order_no, detail.order.id)
  addTechnicalIdentifier(rows, '项目 ID', detail.order.project_name, detail.order.project_id)
  for (const batchID of detail.order.import_batch_ids ?? []) {
    addTechnicalIdentifier(rows, '导入批次 ID', detail.order.order_no, batchID)
  }
  for (const item of detail.order.items) {
    const label = itemBusinessLabel({ ...item, order_no: detail.order.order_no })
    addTechnicalIdentifier(rows, '订单明细 ID', label, item.id)
    addTechnicalIdentifier(rows, '商品 ID', label, item.product_id)
    addTechnicalIdentifier(rows, '商品 SKU', label, item.sku)
    addTechnicalIdentifier(rows, '系列编码', label, item.series_code)
    addTechnicalIdentifier(rows, '导入批次 ID', label, item.import_batch_id)
    addTechnicalIdentifier(rows, '来源 Sheet', label, item.source_sheet)
    addTechnicalIdentifier(rows, '来源位置', label, item.source_row_key)
  }
  return rows
}

function importHistoryTechnicalIdentifiers(items: ImportHistoryItem[]) {
  const rows: TechnicalIdentifier[] = []
  for (const item of items) {
    addTechnicalIdentifier(rows, '导入记录 ID', item.original_filename, item.id)
    addTechnicalIdentifier(rows, '文件 SHA', item.original_filename, item.file_hash)
  }
  return rows
}

function importDetailTechnicalIdentifiers(detail: ImportDetailResponse | null) {
  const rows: TechnicalIdentifier[] = []
  if (!detail) return rows
  addTechnicalIdentifier(rows, '导入记录 ID', detail.import.original_filename, detail.import.id)
  addTechnicalIdentifier(rows, '文件 SHA', detail.import.original_filename, detail.import.file_hash)
  addTechnicalIdentifier(rows, '预览批次 ID', detail.import.original_filename, detail.preview?.import_batch_id)
  for (const batch of detail.preview?.batches ?? []) {
    addTechnicalIdentifier(rows, '解析批次 ID', batch.batch_name || batch.sheet_name, batch.id)
    addTechnicalIdentifier(rows, '工作表 ID', batch.sheet_name, batch.sheet_id)
    addTechnicalIdentifier(rows, '内容 Hash', batch.batch_name || batch.sheet_name, batch.content_hash)
  }
  return rows
}

async function loadAdminUserDetail(id: string) {
  adminUserDetailLoading.value = true
  adminUserDetailMessage.value = ''
  adminUserDetail.value = null
  mergeTargetCN.value = ''
  mergeReason.value = ''
  mergePreview.value = null
  mergeMessage.value = ''
  queryCodeDraft.value = ''
  queryAccountMessage.value = ''
  bindTokenResult.value = null
  bindTokenMessage.value = ''
  try {
    adminUserDetail.value = await getJSON<AdminUserDetailResponse>('/api/admin/users/' + encodeURIComponent(id))
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    adminUserDetailMessage.value = error instanceof Error ? error.message : '用户详情加载失败'
  } finally {
    adminUserDetailLoading.value = false
  }
}

function updateAdminUserInList(user: AdminUserListItem) {
  adminUsers.value = adminUsers.value.map((item) => (item.id === user.id ? user : item))
  if (adminUserDetail.value?.user.id === user.id) {
    adminUserDetail.value = { ...adminUserDetail.value, user }
  }
}

async function saveQueryCode() {
  if (!adminUserDetail.value || queryCodeSaving.value) return
  const code = queryCodeDraft.value.trim()
  if (code.length < 6 || code.length > 32) {
    queryAccountMessage.value = '查询码需为 6-32 位，可使用字母、数字及 - _ @ # .。'
    return
  }
  const user = adminUserDetail.value.user
  const action = user.has_query_code ? '重置' : '设置'
  const message = user.has_query_code
    ? `确认重置 ${user.cn_code} 的查询码？旧查询码和已登录查询会话会立即失效，管理员不能查看原查询码。`
    : `确认为 ${user.cn_code} 设置查询码？明文只会用于本次保存，页面不会显示或记录。`
  if (!window.confirm(message)) return
  queryCodeSaving.value = true
  queryAccountMessage.value = ''
  try {
    const response = await postJSON<{ user: AdminUserListItem }>(`/api/admin/users/${encodeURIComponent(user.id)}/query-code`, { query_code: code })
    updateAdminUserInList(response.user)
    queryCodeDraft.value = ''
    queryAccountMessage.value = `${action}查询码成功，旧查询会话已失效。`
    await loadAdminUsers()
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    queryAccountMessage.value = error instanceof Error ? error.message : `${action}查询码失败`
  } finally {
    queryCodeSaving.value = false
  }
}

async function generateBindToken() {
  if (!adminUserDetail.value || bindTokenGenerating.value) return
  const user = adminUserDetail.value.user
  if (!window.confirm('生成新绑定码后，该用户以前未使用的绑定码将立即失效。是否继续？')) return
  bindTokenGenerating.value = true
  bindTokenMessage.value = ''
  bindTokenResult.value = null
  try {
    const response = await createQueryCodeBindToken(user.id)
    bindTokenResult.value = { token: response.bind_token, expiresAt: response.expires_at }
    await loadAdminUserDetailStatusOnly(user.id)
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    bindTokenMessage.value = error instanceof Error ? error.message : '生成绑定码失败'
  } finally {
    bindTokenGenerating.value = false
  }
}

// Refresh only the bind-token status flags without wiping the one-time
// token that generateBindToken just put on screen.
async function loadAdminUserDetailStatusOnly(id: string) {
  try {
    const refreshed = await getJSON<AdminUserDetailResponse>('/api/admin/users/' + encodeURIComponent(id))
    if (adminUserDetail.value && adminUserDetail.value.user.id === id) {
      adminUserDetail.value = refreshed
    }
  } catch {
    // Status refresh is cosmetic; the generated token stays visible.
  }
}

async function copyBindToken() {
  if (!bindTokenResult.value) return
  try {
    await navigator.clipboard.writeText(bindTokenResult.value.token)
    bindTokenMessage.value = '绑定码已复制到剪贴板。'
  } catch {
    bindTokenMessage.value = '复制失败，请手动选择并复制绑定码。'
  }
}

async function setQueryAccessStatus(status: 'active' | 'disabled') {
  if (!adminUserDetail.value || queryAccessSaving.value) return
  const user = adminUserDetail.value.user
  const disabling = status === 'disabled'
  const message = disabling
    ? `确认停用 ${user.cn_code} 的查询权限？该用户将无法登录，当前查询会话会立即失效。`
    : `确认启用 ${user.cn_code} 的查询权限？原查询码将继续有效，除非已被重置。`
  if (!window.confirm(message)) return
  queryAccessSaving.value = true
  queryAccountMessage.value = ''
  try {
    const response = await patchJSON<{ user: AdminUserListItem }>(`/api/admin/users/${encodeURIComponent(user.id)}/status`, { status })
    updateAdminUserInList(response.user)
    queryAccountMessage.value = disabling ? '查询权限已停用，旧查询会话已失效。' : '查询权限已启用。'
    await loadAdminUsers()
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    queryAccountMessage.value = error instanceof Error ? error.message : '查询权限更新失败'
  } finally {
    queryAccessSaving.value = false
  }
}
async function previewCNMerge() {
  if (!adminUserDetail.value) return
  const target = mergeTargetCN.value.trim()
  if (!target) {
    mergeMessage.value = '请输入要合并到的目标 CN。'
    return
  }
  mergePreviewLoading.value = true
  mergeMessage.value = ''
  queryCodeDraft.value = ''
  queryAccountMessage.value = ''
  mergePreview.value = null
  try {
    mergePreview.value = await getJSON<AdminUserMergePreviewResponse>(
      `/api/admin/users/merge-preview?source_id=${encodeURIComponent(adminUserDetail.value.user.id)}&target_cn=${encodeURIComponent(target)}`,
    )
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    mergeMessage.value = error instanceof Error ? error.message : '合并预览失败'
  } finally {
    mergePreviewLoading.value = false
  }
}

async function confirmCNMerge() {
  if (!adminUserDetail.value || !mergePreview.value || mergeSaving.value) return
  const reason = mergeReason.value.trim()
  if (!reason) {
    mergeMessage.value = '请填写合并原因。'
    return
  }
  const preview = mergePreview.value
  const confirmMessage = [
    '确认合并 CN？此操作不可撤销。',
    `被合并（将标记为已合并）：${preview.source.cn_code}`,
    `合并到：${preview.target.cn_code}`,
    `迁移订单：${preview.move_order_count} 单`,
    `迁移付款：${preview.move_payment_count} 笔`,
    `原因：${reason}`,
  ].join('\n')
  if (!window.confirm(confirmMessage)) return
  mergeSaving.value = true
  mergeMessage.value = ''
  queryCodeDraft.value = ''
  queryAccountMessage.value = ''
  try {
    const response = await postJSON<AdminUserMergeResponse>('/api/admin/users/merge', {
      source_user_id: preview.source.id,
      target_user_id: preview.target.id,
      reason,
    })
    mergeMessage.value = `合并完成：迁移订单 ${response.moved_order_count} 单、付款 ${response.moved_payment_count} 笔。`
    mergePreview.value = null
    await loadAdminUserDetail(preview.target.id)
    navigate('/admin/users/' + preview.target.id)
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    mergeMessage.value = error instanceof Error ? error.message : 'CN 合并失败'
  } finally {
    mergeSaving.value = false
  }
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
  const cn = paymentEntryCN.value.trim()
  if (!cn) {
    cnPaymentMessage.value = '请先输入 CN。'
    return
  }
  cnPaymentLoading.value = true
  if (!preserveMessage) cnPaymentMessage.value = ''
  if (!preserveMessage) paymentCreatedID.value = ''
  try {
    cnPayment.value = await getJSON<CNPaymentResponse>(`/api/admin/payments/unpaid?cn=${encodeURIComponent(cn)}`)
    resetPaymentDraft()
    if (cnPayment.value.items.length === 0) cnPaymentMessage.value = '该 CN 暂无待付款明细。'
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = 'Login expired. Please log in again.'
      return
    }
    cnPayment.value = null
    cnPaymentMessage.value = error instanceof Error ? error.message : '付款明细加载失败'
  } finally {
    cnPaymentLoading.value = false
  }
}

function resetPaymentDraft() {
  selectedPaymentItemIds.value = new Set()
  paymentAmounts.value = {}
  paymentPaidAt.value = localDateTimeInputValue()
  paymentNote.value = ''
  paymentDraftIdempotencyKey.value = newIdempotencyKey()
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

function moneyToCents(value: number) {
  return Math.round(value * 100)
}

function moneyInputToCents(rawValue: string) {
  const raw = rawValue.trim()
  if (raw === '') return null
  if (!/^\d+(?:\.\d{0,2})?$/.test(raw)) return null
  const [whole, fraction = ''] = raw.split('.')
  const wholeCents = Number(whole) * 100
  const fractionCents = Number((fraction + '00').slice(0, 2))
  if (!Number.isFinite(wholeCents) || !Number.isFinite(fractionCents)) return null
  return wholeCents + fractionCents
}

function paymentAmountCents(itemID: string) {
  return moneyInputToCents(String(paymentAmounts.value[itemID] ?? '')) ?? 0
}

function paymentAmountValue(itemID: string) {
  return paymentAmountCents(itemID) / 100
}

function paymentAmountInvalid(item: PaymentItemRow) {
  if (!selectedPaymentItemIds.value.has(item.id)) return false
  const cents = moneyInputToCents(String(paymentAmounts.value[item.id] ?? ''))
  return cents === null || cents <= 0 || cents > moneyToCents(item.remaining_amount)
}

async function savePayment() {
  if (!cnPayment.value) return
  if (paymentMethod.value === '') {
    cnPaymentMessage.value = '请选择付款方式。'
    return
  }
  if (selectedPaymentItems.value.length === 0) {
    cnPaymentMessage.value = '请先选择待付款明细。'
    return
  }
  const invalidItem = selectedPaymentItems.value.find(paymentAmountInvalid)
  if (invalidItem) {
    cnPaymentMessage.value = '本次分摊金额必须大于 0，且不能超过剩余应付金额。'
    return
  }
  if (!canSavePayment.value) return
  const submittedTotal = selectedPaymentTotal.value
  const submittedFee = paymentFeeAmount.value
  const submittedPayable = paymentPayableAmount.value
  const methodLabel = paymentMethodLabel(paymentMethod.value)
  const confirmLines = [
    '确认付款？确认后该笔付款立即生效，如需更正只能撤销后重新录入。',
    `CN：${cnPayment.value.user.cn_code}`,
    `付款方式：${methodLabel}`,
    `本金：${formatMoney(submittedTotal)}`,
    `手续费：${formatMoney(submittedFee)}`,
    `实付金额：${formatMoney(submittedPayable)}`,
    `关联明细数量：${selectedPaymentItems.value.length}`,
    '',
    ...selectedPaymentItems.value.map((item) => `${item.order_no} / ${item.display_name || item.product_name}：${formatMoney(paymentAmountValue(item.id))}`),
  ]
  if (!window.confirm(confirmLines.join('\n'))) return
  paymentSaving.value = true
  cnPaymentMessage.value = ''
  paymentCreatedID.value = ''
  try {
    if (!paymentDraftIdempotencyKey.value) paymentDraftIdempotencyKey.value = newIdempotencyKey()
    const response = await postJSON<CreatePaymentResponse>('/api/admin/payments', {
      cn: cnPayment.value.user.cn_code,
      payment_method: paymentMethod.value,
      paid_at: paymentPaidAt.value,
      note: paymentNote.value.trim(),
      idempotency_key: paymentDraftIdempotencyKey.value,
      items: selectedPaymentItems.value.map((item) => ({ order_item_id: item.id, amount: paymentAmountValue(item.id) })),
    })
    paymentCreatedID.value = response.payment_id
    await loadCNPayment(true)
    await loadPaymentRecords()
    cnPaymentMessage.value = response.duplicate ? '检测到重复提交，未新增付款记录。' : `付款已确认，本金 ${formatMoney(submittedTotal)}，实付 ${formatMoney(submittedPayable)}。`
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = 'Login expired. Please log in again.'
      return
    }
    cnPaymentMessage.value = error instanceof Error ? error.message : '付款确认失败'
  } finally {
    paymentSaving.value = false
  }
}

function resetOrderFilters() {
  orderFilters.value = {
    cn: '',
    project: '',
    item: '',
    series: '',
    category: '',
    role: '',
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
    queryOldCode.value = ''
    queryNewCode.value = ''
    queryConfirmCode.value = ''
    querySecurityMessage.value = ''
    queryMessage.value = '已退出查询。'
  } catch (error) {
    queryMessage.value = error instanceof Error ? error.message : '退出失败'
  } finally {
    queryLoading.value = false
  }
}

function openBindView() {
  queryView.value = 'bind'
  bindMessage.value = ''
  queryMessage.value = ''
}

function closeBindView() {
  queryView.value = 'login'
  bindCN.value = ''
  bindTokenInput.value = ''
  bindNewCode.value = ''
  bindConfirmCode.value = ''
  bindMessage.value = ''
}

async function submitBindCode() {
  if (bindSubmitting.value) return
  bindMessage.value = ''
  const cn = bindCN.value.trim()
  const token = bindTokenInput.value.trim()
  const newCode = bindNewCode.value.trim()
  const confirmCode = bindConfirmCode.value.trim()
  if (!cn || !token || !newCode || !confirmCode) {
    bindMessage.value = '请完整输入 CN、绑定码和新查询码。'
    return
  }
  if (newCode !== confirmCode) {
    bindMessage.value = '两次输入的新查询码不一致。'
    return
  }
  bindSubmitting.value = true
  try {
    const response = await bindQueryCode({
      cn,
      bind_token: token,
      new_query_code: newCode,
      confirm_query_code: confirmCode,
    })
    closeBindView()
    queryMessage.value = response.message
  } catch (error) {
    bindMessage.value = error instanceof Error ? error.message : '查询码设置失败'
  } finally {
    bindSubmitting.value = false
  }
}

async function submitQueryCodeChange() {
  if (queryCodeChanging.value) return
  querySecurityMessage.value = ''
  const oldCode = queryOldCode.value.trim()
  const newCode = queryNewCode.value.trim()
  const confirmCode = queryConfirmCode.value.trim()
  if (!oldCode || !newCode || !confirmCode) {
    querySecurityMessage.value = '请完整输入旧查询码、新查询码和确认查询码。'
    return
  }
  if (newCode !== confirmCode) {
    querySecurityMessage.value = '两次输入的新查询码不一致。'
    return
  }
  queryCodeChanging.value = true
  try {
    const response = await changeQueryCode({
      old_query_code: oldCode,
      new_query_code: newCode,
      confirm_query_code: confirmCode,
    })
    queryOldCode.value = ''
    queryNewCode.value = ''
    queryConfirmCode.value = ''
    queryUser.value = null
    queryOrders.value = null
    queryCode.value = ''
    queryMessage.value = response.message
  } catch (error) {
    querySecurityMessage.value = error instanceof Error ? error.message : '查询码修改失败'
  } finally {
    queryCodeChanging.value = false
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
function orderSources(order: OrderSummary) {
  return (order.import_filenames ?? []).join(' / ') || '-'
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

// China timezone requirement: default the "paid at" input to Asia/Shanghai
// (UTC+8) wall-clock time, not the browser's local timezone. Using a fixed
// offset (rather than the machine's own zone) keeps this correct even if an
// admin's computer is set to a different timezone.
function localDateTimeInputValue() {
  const chinaOffsetMinutes = 8 * 60
  const date = new Date(Date.now() + chinaOffsetMinutes * 60000)
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

// China timezone requirement: always render Asia/Shanghai (UTC+8) regardless
// of the admin's local computer/browser timezone. toLocaleString without an
// explicit timeZone falls back to the local system zone, which would silently
// show the wrong time on a machine set to e.g. Asia/Tokyo.
function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false, timeZone: 'Asia/Shanghai' })
}

function countTemplates(batches: ImportBatch[]) {
  const counts = new Map<string, number>()
  for (const batch of batches) counts.set(batch.template_type, (counts.get(batch.template_type) ?? 0) + 1)
  return Array.from(counts.entries()).map(([name, count]) => ({ name: templateTypeLabel(name), count }))
}


function paymentMethodLabel(method: string) {
  const labels: Record<string, string> = {
    alipay: '支付宝',
    wechat: '微信',
    bank: '银行转账',
    cash: '现金',
    other: '其他',
  }
  return labels[method] ?? method
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

function paymentStatusLabel(status: string) {
  const labels: Record<string, string> = {
    approved: '已交肾',
    voided: '已撤销',
    submitted: '待处理',
    rejected: '已驳回',
    cancelled: '已取消',
  }
  return labels[status] ?? status
}

function userStatusLabel(status: string) {
  const labels: Record<string, string> = {
    active: '正常',
    disabled: '已停用',
    merged: '已合并',
  }
  return labels[status] ?? status
}

function templateTypeLabel(type: string) {
  const labels: Record<string, string> = {
    matrix: '矩阵汇总表',
    standard_import: '明细表',
    simple_cn_amount: '仅 CN 金额表',
    unknown: '未识别',
  }
  return labels[type] ?? type
}

function queryCharacterLabel(item: { character_name?: string }) {
  return item.character_name || '-'
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
  routeUserID.value = userIDFromPath(window.location.pathname)
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
      <button :class="{ active: routeName === 'admin-users' || routeName === 'admin-user-detail' }" type="button" @click="navigate('/admin/users')">用户管理</button>
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
            <button class="secondary-button" type="button" @click="navigate('/admin/payments')">付款记录</button>
            <button class="secondary-button" type="button" @click="navigate('/admin/users')">用户管理</button>
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
            <article class="metric-tile wide-metric"><span>文件校验</span><strong>已记录</strong></article>
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
                    <td>{{ templateTypeLabel(sheet.template_type) }}</td>
                    <td>{{ sheet.batch_count }}</td>
                    <td :class="{ danger: Math.abs(sheet.difference) > 0.01 }">{{ formatMoney(sheet.difference) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section v-if="preview" class="panel confirm-panel">
            <div class="panel__header"><div><h2>确认导入</h2><p class="muted">确认时使用服务器保存的预览结果，不信任前端明细。</p></div><span>预览已生成</span></div>
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
                  <span>{{ isExpanded(batch.id) ? '▾' : '▸' }}</span><strong>{{ batch.sheet_title ? `${batch.sheet_name}（${batch.sheet_title}）` : batch.sheet_name }} / {{ batch.batch_name }}</strong><span class="status-chip" data-state="draft">{{ templateTypeLabel(batch.template_type) }}</span><span v-if="!isBatchIncluded(batch)" class="simple-note">该 Sheet 已排除</span><span v-else-if="batch.template_type === 'simple_cn_amount'" class="simple-note">仅预览，不转换为订单项</span>
                </button>
                <div class="batch-metrics"><span>CN {{ batch.cn_count }}</span><span>种类 {{ batch.item_type_count }}</span><span>总件数 {{ batch.total_quantity }}</span><span>表格 {{ formatMoney(batch.table_amount) }}</span><span>程序 {{ formatMoney(batch.calculated_amount) }}</span><span :class="{ danger: Math.abs(batch.difference) > 0.01 }">差额 {{ formatMoney(batch.difference) }}</span><span>价格 {{ priceTypeLabel(batch) }}</span></div>
                <div v-if="isExpanded(batch.id)" class="batch-detail">
                                    <div class="table-scroll detail-table"><table><thead><tr><th>选择</th><th>导入</th><th>原始 CN</th><th>谷子名称</th><th>角色/种类</th><th>分类修正</th><th>数量</th><th>价格</th><th>小计</th><th>来源</th></tr></thead><tbody><tr v-if="detailsForBatch(batch).length === 0"><td colspan="10">当前筛选下无订单项明细。</td></tr><tr v-for="detail in detailsForBatch(batch)" :key="detail.id || `${batch.id}-${detail.row_number}-${detail.column_name}-${detail.original_cn}`" :class="{ muted: isDetailExcluded(batch, detail) }"><td><input type="checkbox" :checked="isDetailSelected(detail.id)" @change="setDetailSelected(detail.id, ($event.target as HTMLInputElement).checked)" /></td><td><input type="checkbox" :disabled="!isBatchIncluded(batch)" :checked="isBatchIncluded(batch) && !isCNExcluded(batch, detail)" @change="setCNExcluded(batch, detail, !($event.target as HTMLInputElement).checked)" /></td><td><strong>{{ detail.original_cn }}</strong><small v-if="isCNExcluded(batch, detail)">已排除该范围内 CN</small><small v-if="excludedDetailIds.has(detail.id)">已排除此明细</small><button class="secondary-button tiny-button" type="button" @click="setDetailExcluded(detail.id, !excludedDetailIds.has(detail.id))">{{ excludedDetailIds.has(detail.id) ? '取消明细排除' : '只排除此明细' }}</button></td><td>{{ detail.display_name || detail.sheet_title || '-' }}</td><td>{{ detail.item_name }}</td><td><div class="category-editor"><select :disabled="!isBatchIncluded(batch) || isCNExcluded(batch, detail)" :value="detailCategory(detail)" @change="setDetailCategory(detail, ($event.target as HTMLSelectElement).value)"><option value="">保持原分类</option><option v-for="preset in categoryPresets" :key="preset" :value="preset">{{ preset }}</option></select><div class="custom-category"><input v-model="customCategoryInputs[detail.id]" :disabled="!isBatchIncluded(batch) || isCNExcluded(batch, detail)" maxlength="40" placeholder="自定义制品" /><button class="secondary-button" type="button" :disabled="!isBatchIncluded(batch) || isCNExcluded(batch, detail)" @click="applyCustomCategory(detail)">应用</button></div><small>{{ detailCategory(detail) || detail.product_category || detail.category || '默认分类' }}</small></div></td><td>{{ detail.quantity }}</td><td>{{ formatMoney(detail.unit_price) }}</td><td>{{ formatMoney(detail.amount) }}</td><td>{{ detail.sheet_name }}!{{ detail.column_name }}{{ detail.row_number }}</td></tr></tbody></table></div>
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
            <div class="panel__header"><div><h2>付款记录</h2><p class="muted">只读查看付款流水和关联明细；本页不提供删除、作废或冲正。</p></div><div class="header-actions"><button class="secondary-button" type="button" @click="exportPaymentsExcel">导出付款 Excel</button><button class="secondary-button" type="button" @click="exportUnpaidItemsExcel">导出未付明细 Excel</button><button class="secondary-button ghost-button" type="button" @click="exportPaymentsCSV">付款 CSV</button><button class="secondary-button ghost-button" type="button" @click="exportUnpaidItemsCSV">未付 CSV</button><button class="secondary-button" type="button" :disabled="paymentRecordsLoading" @click="loadPaymentRecords">{{ paymentRecordsLoading ? '加载中' : '刷新' }}</button></div></div>
            <section class="panel nested-panel payment-entry-panel">
              <div class="panel__header">
                <div><h2>录入付款</h2><p class="muted">按 CN 加载尚未付清的订单明细，支持全额或部分付款。</p></div>
                <button class="secondary-button" type="button" :disabled="cnPaymentLoading" @click="() => loadCNPayment()">{{ cnPaymentLoading ? '加载中' : '加载待付明细' }}</button>
              </div>
              <form class="payment-cn-form" @submit.prevent="loadCNPayment()">
                <label><span>CN</span><input v-model="paymentEntryCN" placeholder="输入 CN" /></label>
                <button class="primary-button" type="submit" :disabled="cnPaymentLoading">查询</button>
              </form>
              <p class="payment-immutable-hint">付款记录保存后不可直接修改。如金额、付款方式或关联明细录入错误，请先在付款详情页撤销原付款，再重新录入。</p>
              <div v-if="cnPaymentMessage" class="inline-alert">{{ cnPaymentMessage }}</div>
              <template v-if="cnPayment">
                <div class="summary-grid compact-summary payment-summary">
                  <article class="metric-tile"><span>CN</span><strong>{{ cnPayment.user.cn_code }}</strong></article>
                  <article class="metric-tile"><span>订单总额</span><strong>{{ formatMoney(cnPayment.summary.total_amount) }}</strong></article>
                  <article class="metric-tile"><span>有效已付总额</span><strong>{{ formatMoney(cnPayment.summary.paid_amount) }}</strong></article>
                  <article class="metric-tile"><span>剩余待付总额</span><strong>{{ formatMoney(cnPayment.summary.remaining_amount) }}</strong></article>
                  <article class="metric-tile"><span>待付明细数</span><strong>{{ cnPayment.items.length }}</strong></article>
                </div>
                <div class="payment-form payment-flow">
                  <p class="payment-flow-steps">选择付款方式 → 核对金额 → 确认付款</p>
                  <div class="payment-method-group payment-method-group--flow">
                    <span class="payment-method-label">1. 选择付款方式</span>
                    <div class="payment-method-options payment-method-options--flow">
                      <label class="payment-method-option payment-method-option--alipay" :class="{ active: paymentMethod === 'alipay' }">
                        <input type="radio" value="alipay" v-model="paymentMethod" />
                        <span>支付宝</span>
                      </label>
                      <label class="payment-method-option payment-method-option--wechat" :class="{ active: paymentMethod === 'wechat' }">
                        <input type="radio" value="wechat" v-model="paymentMethod" />
                        <span>微信</span>
                      </label>
                    </div>
                    <span v-if="paymentMethod === ''" class="payment-method-hint">请选择付款方式后再核对金额</span>
                  </div>
                  <div class="payment-amount-section">
                    <span class="payment-method-label">2. 核对金额</span>
                    <div v-if="paymentMethod !== '' && selectedPaymentItems.length > 0" class="amount-card-row">
                      <div class="amount-card"><span>本金</span><strong>{{ formatMoney(selectedPaymentTotal) }}</strong></div>
                      <div class="amount-card"><span>手续费</span><strong>{{ formatMoney(paymentFeeAmount) }}</strong></div>
                      <div class="amount-card amount-card--payable"><span>实付金额</span><strong>{{ formatMoney(paymentPayableAmount) }}</strong></div>
                    </div>
                    <p v-else class="muted">请先选择付款方式并勾选待付明细，金额会在这里显示。</p>
                  </div>
                  <label class="payment-note"><span>备注</span><input v-model="paymentNote" maxlength="200" placeholder="可选" /></label>
                  <div class="payment-confirm-row">
                    <span class="payment-method-label">3. 确认付款</span>
                    <div class="payment-actions">
                      <span class="muted">{{ selectedPaymentItems.length }} 条明细</span>
                      <button class="primary-button payment-confirm-button" type="button" :disabled="!canSavePayment" @click="savePayment">{{ paymentSaving ? '确认中' : '确认付款' }}</button>
                      <button v-if="paymentCreatedID" class="secondary-button" type="button" @click="navigate('/admin/payments/' + paymentCreatedID)">查看付款详情</button>
                    </div>
                    <span v-if="paymentMethod === '' && selectedPaymentItems.length > 0" class="payment-method-hint">请先在第 1 步选择付款方式，才能确认付款。</span>
                  </div>
                </div>
                <p class="payment-allocation-hint">填写本次付款分摊到该明细的金额，不能超过剩余应付金额。分摊金额只能由管理员在本页操作，付款保存后不可直接修改。</p>
                <div class="table-scroll detail-table payment-table"><table><thead><tr><th>选择</th><th>订单号</th><th>项目名</th><th>谷子名称</th><th>角色</th><th>分类</th><th>原始应付</th><th>已付</th><th>剩余应付</th><th title="填写本次付款分摊到该明细的金额，不能超过剩余应付金额">本次分摊金额</th><th>状态</th><th>来源</th></tr></thead><tbody><tr v-if="cnPayment.items.length === 0"><td colspan="12">暂无待付款明细。</td></tr><tr v-for="item in cnPayment.items" :key="item.id"><td><input type="checkbox" :disabled="item.remaining_amount <= 0" :checked="selectedPaymentItemIds.has(item.id)" @change="setPaymentItemSelected(item, ($event.target as HTMLInputElement).checked)" /></td><td>{{ item.order_no }}</td><td><span class="cell-clip" :title="item.project_name">{{ item.project_name }}</span></td><td><span class="cell-clip" :title="item.display_name || item.product_name">{{ item.display_name || item.product_name }}</span></td><td>{{ item.character_name || '-' }}</td><td>{{ item.category || '-' }}</td><td>{{ formatMoney(item.amount) }}</td><td>{{ formatMoney(item.paid_amount) }}</td><td>{{ formatMoney(item.remaining_amount) }}</td><td><input class="amount-input" v-model="paymentAmounts[item.id]" :disabled="!selectedPaymentItemIds.has(item.id)" type="number" min="0.01" step="0.01" :max="item.remaining_amount" :class="{ invalid: paymentAmountInvalid(item) }" /></td><td>{{ queryPaymentStatusLabel(item.payment_status) }}</td><td>{{ item.import_filename || '-' }}</td></tr></tbody></table></div>
              </template>
            </section>
            <form class="order-filters payment-filters-priority" @submit.prevent="loadPaymentRecords"><label class="filter-label--strong"><span>付款时间从</span><input v-model="paymentFilters.paidFrom" type="datetime-local" /></label><label class="filter-label--strong"><span>付款时间到</span><input v-model="paymentFilters.paidTo" type="datetime-local" /></label><label><span>CN</span><input v-model="paymentFilters.cn" placeholder="CN 或显示名" /></label><label><span>交肾状态</span><select v-model="paymentFilters.status"><option value="">全部</option><option value="approved">已交肾</option><option value="submitted">待处理</option><option value="rejected">已驳回</option><option value="voided">已撤销</option></select></label><label><span>付款方式</span><select v-model="paymentFilters.paymentMethod"><option value="">全部</option><option value="alipay">支付宝</option><option value="wechat">微信</option><option value="bank">银行转账</option><option value="cash">现金</option><option value="other">其他</option></select></label><div class="filter-actions"><button class="primary-button" type="submit" :disabled="paymentRecordsLoading">查询</button><button class="secondary-button" type="button" @click="resetPaymentFilters">重置</button></div></form>
            <div v-if="paymentRecordsMessage" class="inline-alert">{{ paymentRecordsMessage }}</div>
            <div class="table-scroll history-table"><table><thead><tr><th>付款时间</th><th>CN</th><th class="col-emphasis">实付金额</th><th>交肾状态</th><th>本金</th><th>手续费</th><th>付款方式</th><th>操作管理员</th><th>备注</th><th>关联明细数量</th><th></th></tr></thead><tbody><tr v-if="!paymentRecordsLoading && paymentRecords.length === 0"><td colspan="11">暂无付款记录。</td></tr><tr v-for="payment in paymentRecords" :key="payment.id"><td>{{ formatDate(payment.paid_at) }}</td><td><strong>{{ payment.cn_code }}</strong><small>{{ payment.display_name || '-' }}</small></td><td class="col-emphasis">{{ formatMoney(payment.payable_amount) }}</td><td><span class="status-chip" :data-state="payment.status">{{ paymentStatusLabel(payment.status) }}</span></td><td>{{ formatMoney(payment.amount) }}</td><td>{{ formatMoney(payment.fee_amount) }}</td><td>{{ paymentMethodLabel(payment.payment_method || '') }}</td><td>{{ payment.created_by || '-' }}</td><td>{{ payment.note || '-' }}</td><td>{{ payment.payment_item_count }}</td><td><button class="secondary-button" type="button" @click="navigate('/admin/payments/' + payment.id)">详情</button></td></tr></tbody></table></div>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-payment-detail'">
          <section class="panel">
            <div class="panel__header"><div><h2>付款详情</h2><p class="muted">付款流水与关联明细</p></div><button class="secondary-button" type="button" @click="navigate('/admin/payments')">返回付款记录</button></div>
            <div v-if="paymentDetailMessage" class="inline-alert">{{ paymentDetailMessage }}</div><p v-if="paymentDetailLoading" class="muted">正在加载付款详情。</p>
            <template v-if="paymentDetail">
              <div class="summary-grid">
                <article class="metric-tile"><span>CN</span><strong>{{ paymentDetail.payment.cn_code }}</strong></article>
                <article class="metric-tile metric-tile--emphasis"><span>实付金额</span><strong>{{ formatMoney(paymentDetail.payment.total_amount ?? paymentDetail.payment.payable_amount) }}</strong></article>
                <article class="metric-tile"><span>交肾状态</span><strong>{{ paymentStatusLabel(paymentDetail.payment.status) }}</strong></article>
                <article class="metric-tile"><span>本金</span><strong>{{ formatMoney(paymentDetail.payment.principal_amount ?? paymentDetail.payment.amount) }}</strong></article>
                <article class="metric-tile"><span>手续费</span><strong>{{ formatMoney(paymentDetail.payment.fee_amount) }}</strong></article>
                <article class="metric-tile"><span>付款方式</span><strong>{{ paymentMethodLabel(paymentDetail.payment.payment_method || '') }}</strong></article>
                <article class="metric-tile"><span>付款时间</span><strong>{{ formatDate(paymentDetail.payment.paid_at) }}</strong></article>
                <article class="metric-tile"><span>操作管理员</span><strong>{{ paymentDetail.payment.created_by || '-' }}</strong></article>
                <article class="metric-tile"><span>关联明细</span><strong>{{ paymentDetail.payment.payment_item_count }}</strong></article>
                <article class="metric-tile wide-metric"><span>备注</span><strong>{{ paymentDetail.payment.note || '-' }}</strong></article>
                <article v-if="paymentDetail.payment.voided_at" class="metric-tile"><span>撤销时间</span><strong>{{ formatDate(paymentDetail.payment.voided_at) }}</strong></article>
                <article v-if="paymentDetail.payment.voided_by" class="metric-tile"><span>撤销管理员</span><strong>{{ paymentDetail.payment.voided_by }}</strong></article>
                <article v-if="paymentDetail.payment.void_reason" class="metric-tile wide-metric"><span>撤销原因</span><strong>{{ paymentDetail.payment.void_reason }}</strong></article>
              </div>
              <section v-if="paymentDetail.payment.status === 'approved'" class="panel nested-panel danger-panel">
                <div class="panel__header">
                  <div><h2>撤销付款</h2><p class="muted">付款记录不可直接修改。如金额、方式或明细错误，请撤销本笔付款后重新录入；撤销后该付款不再计入有效已付款金额，相关订单状态会重新计算。</p></div>
                  <button class="danger-button" type="button" :disabled="paymentVoiding" @click="voidPayment">{{ paymentVoiding ? '撤销中' : '撤销付款' }}</button>
                </div>
              </section>
              <section v-if="paymentDetail.payment.status === 'voided'" class="panel nested-panel danger-panel">
                <div class="panel__header"><h2>已撤销</h2><span>{{ formatDate(paymentDetail.payment.voided_at) }}</span></div>
                <p class="muted">{{ paymentDetail.payment.voided_by || '-' }} / {{ paymentDetail.payment.void_reason || '-' }}</p>
              </section>
              <section class="panel nested-panel">
                <div class="panel__header"><h2>关联付款明细</h2><span>共 {{ paymentDetail.payment.items.length }} 条明细</span></div>
                <div class="table-scroll detail-table"><table><thead><tr><th>订单号</th><th>项目名</th><th>谷子名称</th><th>角色</th><th>分类</th><th>数量</th><th>单价</th><th>小计</th><th>已付</th><th>剩余</th><th>本次分摊金额</th><th>当前付款状态</th><th>来源</th></tr></thead><tbody><tr v-if="paymentDetail.payment.items.length === 0"><td colspan="13">无关联明细。</td></tr><tr v-for="item in paymentDetail.payment.items" :key="item.id"><td>{{ item.order_no }}</td><td>{{ item.project_name }}</td><td>{{ item.display_name || item.product_name }}</td><td>{{ item.character_name || '-' }}</td><td>{{ item.category || '-' }}</td><td>{{ item.quantity }}</td><td>{{ formatMoney(item.unit_price) }}</td><td>{{ formatMoney(item.amount) }}</td><td>{{ formatMoney(item.paid_amount) }}</td><td>{{ formatMoney(item.remaining_amount) }}</td><td>{{ formatMoney(item.applied_amount) }}</td><td>{{ queryPaymentStatusLabel(item.payment_status) }}</td><td>{{ item.import_filename || '-' }}</td></tr></tbody></table></div>
              </section>
              <section v-if="paymentDetailTechnicalIdentifiers(paymentDetail).length > 0" class="panel nested-panel technical-section">
                <details>
                  <summary><span class="closed-label">▶ 查看技术标识</span><span class="open-label">▼ 收起技术标识</span></summary>
                  <div class="technical-list"><article v-for="identifier in mergeTechnicalIdentifiers(paymentDetailTechnicalIdentifiers(paymentDetail))" :key="identifier.type + '-' + identifier.value" class="technical-item"><div class="technical-item__head"><span class="technical-item__type">{{ identifier.type }}</span><span class="technical-item__context">{{ identifier.context }}</span><button type="button" class="copy-button" @click="copyIdentifier(identifier.value)">复制</button></div><code class="technical-item__value">{{ identifier.value }}</code></article></div>
                </details>
              </section>
            </template>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-users'">
          <section class="panel">
            <div class="panel__header"><div><h2>用户管理</h2><p class="muted">查看用户订单和付款汇总；本页只读，不提供删除。</p></div><div class="header-actions"><button class="secondary-button" type="button" @click="exportAdminUsersExcel">导出 Excel</button><button class="secondary-button ghost-button" type="button" @click="exportAdminUsersCSV">CSV</button><button class="secondary-button" type="button" :disabled="adminUsersLoading" @click="loadAdminUsers">{{ adminUsersLoading ? '加载中' : '刷新' }}</button></div></div>
            <form class="order-filters" @submit.prevent="loadAdminUsers">
              <label><span>CN</span><input v-model="adminUserFilters.cn" placeholder="CN 或显示名" /></label>
              <label><span>状态</span><select v-model="adminUserFilters.status"><option value="">全部</option><option value="active">正常</option><option value="disabled">已停用</option><option value="merged">已合并</option></select></label>
              <div class="filter-actions"><button class="primary-button" type="submit" :disabled="adminUsersLoading">查询</button><button class="secondary-button" type="button" @click="resetAdminUserFilters">重置</button></div>
            </form>
            <div v-if="adminUsersMessage" class="inline-alert">{{ adminUsersMessage }}</div>
            <div v-if="adminUsersSummary" class="summary-grid compact-summary">
              <article class="metric-tile"><span>用户总数</span><strong>{{ adminUsersSummary.user_count }}</strong></article>
              <article class="metric-tile"><span>有订单用户数</span><strong>{{ adminUsersSummary.users_with_orders }}</strong></article>
              <article class="metric-tile"><span>订单总额</span><strong>{{ formatMoney(adminUsersSummary.total_amount) }}</strong></article>
              <article class="metric-tile"><span>有效已付总额</span><strong>{{ formatMoney(adminUsersSummary.paid_amount) }}</strong></article>
              <article class="metric-tile"><span>剩余待付总额</span><strong>{{ formatMoney(adminUsersSummary.remaining_amount) }}</strong></article>
            </div>
            <div class="table-scroll history-table"><table><thead><tr><th>CN</th><th>查询权限</th><th>查询码</th><th>创建时间</th><th>最后登录</th><th>订单数</th><th>订单总金额</th><th>已付金额</th><th>剩余金额</th><th></th></tr></thead><tbody><tr v-if="!adminUsersLoading && adminUsers.length === 0"><td colspan="10">暂无用户。</td></tr><tr v-for="user in adminUsers" :key="user.id"><td><strong>{{ user.cn_code }}</strong><small>{{ user.display_name || '-' }}</small></td><td><span class="status-chip" :data-state="user.status">{{ userStatusLabel(user.status) }}</span></td><td>{{ user.has_query_code ? '已设置' : '未设置' }}<small v-if="user.query_code_updated_at">{{ formatDate(user.query_code_updated_at) }}</small></td><td>{{ formatDate(user.created_at) }}</td><td>{{ user.last_login_at ? formatDate(user.last_login_at) : '-' }}</td><td>{{ user.order_count }}</td><td>{{ formatMoney(user.total_amount) }}</td><td>{{ formatMoney(user.paid_amount) }}</td><td :class="{ danger: user.remaining_amount > 0 }">{{ formatMoney(user.remaining_amount) }}</td><td><button class="secondary-button" type="button" @click="navigate('/admin/users/' + user.id)">详情</button></td></tr></tbody></table></div>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-user-detail'">
          <section class="panel">
            <div class="panel__header"><div><h2>用户详情</h2><p class="muted">用户订单、付款与合并记录</p></div><button class="secondary-button" type="button" @click="navigate('/admin/users')">返回用户列表</button></div>
            <div v-if="adminUserDetailMessage" class="inline-alert">{{ adminUserDetailMessage }}</div><p v-if="adminUserDetailLoading" class="muted">正在加载用户详情。</p>
            <template v-if="adminUserDetail">
              <div class="summary-grid">
                <article class="metric-tile"><span>CN</span><strong>{{ adminUserDetail.user.cn_code }}</strong></article>
                <article class="metric-tile"><span>显示名称</span><strong>{{ adminUserDetail.user.display_name || '-' }}</strong></article>
                <article class="metric-tile"><span>查询码</span><strong>{{ adminUserDetail.user.has_query_code ? '已设置' : '未设置' }}</strong></article>
                <article class="metric-tile"><span>状态</span><strong>{{ userStatusLabel(adminUserDetail.user.status) }}</strong></article><article class="metric-tile"><span>查询码更新时间</span><strong>{{ adminUserDetail.user.query_code_updated_at ? formatDate(adminUserDetail.user.query_code_updated_at) : '-' }}</strong></article><article class="metric-tile"><span>最后登录</span><strong>{{ adminUserDetail.user.last_login_at ? formatDate(adminUserDetail.user.last_login_at) : '-' }}</strong></article>
                <article class="metric-tile"><span>订单数</span><strong>{{ adminUserDetail.user.order_count }}</strong></article>
                <article class="metric-tile"><span>订单总金额</span><strong>{{ formatMoney(adminUserDetail.user.total_amount) }}</strong></article>
                <article class="metric-tile"><span>已付金额</span><strong>{{ formatMoney(adminUserDetail.user.paid_amount) }}</strong></article>
                <article class="metric-tile"><span>剩余金额</span><strong :class="{ danger: adminUserDetail.user.remaining_amount > 0 }">{{ formatMoney(adminUserDetail.user.remaining_amount) }}</strong></article>
              </div>
              <p v-if="adminUserDetail.import_filenames.length > 0" class="muted">导入来源：{{ adminUserDetail.import_filenames.join('、') }}</p>              <section v-if="adminUserDetail.user.status !== 'merged'" class="panel nested-panel query-account-panel">
                <div class="panel__header"><div><h2>查询权限</h2><p class="muted">管理员只能设置或重置查询码；页面不会显示原查询码、明文或哈希。设置、重置和停用会立即清除该用户已登录的查询会话。</p></div><span class="status-chip" :data-state="adminUserDetail.user.status">{{ userStatusLabel(adminUserDetail.user.status) }}</span></div>
                <div class="query-account-grid">
                  <label><span>{{ adminUserDetail.user.has_query_code ? '新查询码' : '查询码' }}</span><input v-model="queryCodeDraft" type="password" autocomplete="new-password" minlength="6" maxlength="32" placeholder="6-32 位" /></label>
                  <button class="primary-button" type="button" :disabled="queryCodeSaving || queryCodeDraft.trim().length === 0" @click="saveQueryCode">{{ queryCodeSaving ? '保存中' : (adminUserDetail.user.has_query_code ? '重置查询码' : '设置查询码') }}</button>
                  <button v-if="adminUserDetail.user.status === 'active'" class="danger-button" type="button" :disabled="queryAccessSaving" @click="setQueryAccessStatus('disabled')">{{ queryAccessSaving ? '处理中' : '停用查询权限' }}</button>
                  <button v-else class="secondary-button" type="button" :disabled="queryAccessSaving" @click="setQueryAccessStatus('active')">{{ queryAccessSaving ? '处理中' : '启用查询权限' }}</button>
                </div>
                <div v-if="queryAccountMessage" class="inline-alert">{{ queryAccountMessage }}</div>
                <div v-if="adminUserDetail.user.status === 'active' && !adminUserDetail.user.has_query_code" class="bind-token-block">
                  <div class="panel__header"><div><h3>首次绑定码</h3><p class="muted">线下核实身份后，为该用户生成一次性绑定码；用户在登录页“首次设置查询码”中使用。绑定码 30 分钟内有效，仅显示一次，重新生成会使旧码全部失效。</p></div></div>
                  <div class="query-account-grid">
                    <button class="secondary-button" type="button" :disabled="bindTokenGenerating" @click="generateBindToken">{{ bindTokenGenerating ? '生成中' : '生成一次性绑定码' }}</button>
                    <span v-if="adminUserDetail.has_active_bind_token && !bindTokenResult" class="muted">已有未使用的绑定码（{{ formatDate(adminUserDetail.bind_token_expires_at) }} 过期）；重新生成将使其失效。</span>
                  </div>
                  <div v-if="bindTokenResult" class="bind-token-result">
                    <p class="bind-token-result__notice">绑定码仅显示一次，请立即安全交给用户，刷新页面后无法再次查看。</p>
                    <div class="bind-token-result__row">
                      <code class="bind-token-result__token">{{ bindTokenResult.token }}</code>
                      <button class="primary-button" type="button" @click="copyBindToken">复制</button>
                    </div>
                    <p class="muted">过期时间：{{ formatDate(bindTokenResult.expiresAt) }}</p>
                  </div>
                  <div v-if="bindTokenMessage" class="inline-alert">{{ bindTokenMessage }}</div>
                </div>
              </section>
              <section v-if="adminUserDetail.user.status === 'active'" class="panel nested-panel danger-panel">
                <div class="panel__header"><div><h2>CN 合并</h2><p class="muted">将当前 CN 的订单、付款和查询记录全部迁移到目标 CN，当前 CN 标记为“已合并”。此操作不可撤销，请先预览影响范围。</p></div></div>
                <div class="payment-cn-form">
                  <label><span>合并到目标 CN</span><input v-model="mergeTargetCN" placeholder="输入目标 CN" /></label>
                  <button class="secondary-button" type="button" :disabled="mergePreviewLoading" @click="previewCNMerge">{{ mergePreviewLoading ? '预览中' : '预览合并影响' }}</button>
                </div>
                <div v-if="mergeMessage" class="inline-alert">{{ mergeMessage }}</div>
                <template v-if="mergePreview">
                  <div class="summary-grid compact-summary">
                    <article class="metric-tile"><span>被合并 CN</span><strong>{{ mergePreview.source.cn_code }}</strong></article>
                    <article class="metric-tile"><span>目标 CN</span><strong>{{ mergePreview.target.cn_code }}</strong></article>
                    <article class="metric-tile"><span>迁移订单</span><strong>{{ mergePreview.move_order_count }} 单</strong></article>
                    <article class="metric-tile"><span>迁移付款</span><strong>{{ mergePreview.move_payment_count }} 笔</strong></article>
                    <article class="metric-tile"><span>迁移查询会话</span><strong>{{ mergePreview.move_query_session_count }} 条</strong></article>
                    <article class="metric-tile"><span>目标现有订单</span><strong>{{ mergePreview.target.order_count }} 单</strong></article>
                    <article class="metric-tile"><span>目标剩余应付</span><strong>{{ formatMoney(mergePreview.target.remaining_amount) }}</strong></article>
                  </div>
                  <div class="payment-cn-form">
                    <label><span>合并原因（必填）</span><input v-model="mergeReason" maxlength="200" placeholder="例如：同一用户重复 CN" /></label>
                    <button class="danger-button" type="button" :disabled="mergeSaving" @click="confirmCNMerge">{{ mergeSaving ? '合并中' : '确认合并' }}</button>
                  </div>
                </template>
              </section>
              <section v-if="adminUserDetail.merges.length > 0" class="panel nested-panel">
                <div class="panel__header"><h2>CN 合并历史</h2><span>{{ adminUserDetail.merges.length }} 条</span></div>
                <div class="table-scroll history-table"><table><thead><tr><th>方向</th><th>相关 CN</th><th>原因</th><th>操作管理员</th><th>时间</th></tr></thead><tbody><tr v-for="entry in adminUserDetail.merges" :key="entry.id"><td>{{ entry.direction === 'merged_into' ? '本 CN 被合并到' : '接收合并自' }}</td><td>{{ entry.other_cn }}</td><td>{{ entry.reason }}</td><td>{{ entry.merged_by || '-' }}</td><td>{{ formatDate(entry.merged_at) }}</td></tr></tbody></table></div>
              </section>
              <section class="panel nested-panel">
                <div class="panel__header"><h2>订单及明细</h2><span>{{ adminUserDetail.orders.length }} 单</span></div>
                <p v-if="adminUserDetail.orders.length === 0" class="muted">暂无订单。</p>
                <article v-for="order in adminUserDetail.orders" :key="order.id" class="user-order-card">
                  <div class="panel__header compact-header"><div><h3>{{ order.order_no }}</h3><p class="muted">{{ order.project_name }} / {{ statusLabel(order.status) }} / {{ formatDate(order.created_at) }}</p></div><div class="query-order-total"><strong>{{ formatMoney(order.total_amount) }}</strong><span>已付 {{ formatMoney(order.paid_amount) }} / 剩余 {{ formatMoney(order.remaining_amount) }}</span></div><button class="secondary-button" type="button" @click="navigate('/admin/orders/' + order.id)">订单详情</button></div>
                  <div class="table-scroll detail-table"><table><thead><tr><th>谷子名称</th><th>角色</th><th>分类</th><th>数量</th><th>单价</th><th>小计</th><th>已付</th><th>剩余</th><th>状态</th><th>来源</th></tr></thead><tbody><tr v-if="order.items.length === 0"><td colspan="10">无明细。</td></tr><tr v-for="item in order.items" :key="item.id"><td>{{ item.display_name || item.product_name }}</td><td>{{ item.character_name || '-' }}</td><td>{{ item.category || '-' }}</td><td>{{ item.quantity }}</td><td>{{ formatMoney(item.unit_price) }}</td><td>{{ formatMoney(item.amount) }}</td><td>{{ formatMoney(item.paid_amount) }}</td><td :class="{ danger: item.remaining_amount > 0 }">{{ formatMoney(item.remaining_amount) }}</td><td>{{ queryPaymentStatusLabel(item.payment_status) }}</td><td>{{ item.import_filename || '-' }}</td></tr></tbody></table></div>
                </article>
              </section>
              <section class="panel nested-panel">
                <div class="panel__header"><h2>付款记录</h2><span>{{ adminUserDetail.payments.length }} 笔</span></div>
                <div class="table-scroll history-table"><table><thead><tr><th>付款时间</th><th class="col-emphasis">实付金额</th><th>交肾状态</th><th>本金</th><th>手续费</th><th>付款方式</th><th>操作管理员</th><th>撤销信息</th><th></th></tr></thead><tbody><tr v-if="adminUserDetail.payments.length === 0"><td colspan="9">暂无付款记录。</td></tr><tr v-for="payment in adminUserDetail.payments" :key="payment.id" :class="{ 'voided-row': payment.status === 'voided' }"><td>{{ formatDate(payment.paid_at) }}</td><td class="col-emphasis">{{ formatMoney(payment.total_amount) }}</td><td><span class="status-chip" :data-state="payment.status">{{ paymentStatusLabel(payment.status) }}</span></td><td>{{ formatMoney(payment.principal_amount) }}</td><td>{{ formatMoney(payment.fee_amount) }}</td><td>{{ paymentMethodLabel(payment.payment_method || '') }}</td><td>{{ payment.created_by || '-' }}</td><td>{{ payment.voided_at ? `${payment.voided_by || '-'} / ${payment.void_reason || '-'}` : '-' }}</td><td><button class="secondary-button" type="button" @click="navigate('/admin/payments/' + payment.id)">详情</button></td></tr></tbody></table></div>
              </section>
              <section v-if="adminUserTechnicalIdentifiers(adminUserDetail).length > 0" class="panel nested-panel technical-section">
                <details>
                  <summary><span class="closed-label">▶ 查看技术标识</span><span class="open-label">▼ 收起技术标识</span></summary>
                  <div class="technical-list"><article v-for="identifier in mergeTechnicalIdentifiers(adminUserTechnicalIdentifiers(adminUserDetail))" :key="identifier.type + '-' + identifier.value" class="technical-item"><div class="technical-item__head"><span class="technical-item__type">{{ identifier.type }}</span><span class="technical-item__context">{{ identifier.context }}</span><button type="button" class="copy-button" @click="copyIdentifier(identifier.value)">复制</button></div><code class="technical-item__value">{{ identifier.value }}</code></article></div>
                </details>
              </section>
            </template>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-orders'">
          <section class="panel">
            <div class="panel__header">
              <div>
                <h2>订单只读查询</h2>
                <p class="muted">查看 Excel 确认导入后的正式订单数据；本页不允许修改、删除或撤销。</p>
              </div>
              <div class="header-actions"><button class="secondary-button" type="button" @click="exportOrderItemsExcel">导出明细 Excel</button><button class="secondary-button ghost-button" type="button" @click="exportOrderItemsCSV">CSV</button><button class="secondary-button" type="button" :disabled="ordersLoading" @click="loadOrders">刷新</button></div>
            </div>

            <form class="order-filters" @submit.prevent="loadOrders">
              <label :class="{ 'filter-field--active': orderFilters.cn }"><span>CN{{ orderFilters.cn ? ' ●' : '' }}</span><span class="filter-field-row"><input v-model="orderFilters.cn" placeholder="CN 或显示名，支持部分匹配" /><button v-if="orderFilters.cn" type="button" class="filter-clear-button" title="清空此项筛选" @click="orderFilters.cn = ''">×</button></span></label>
              <label :class="{ 'filter-field--active': orderFilters.project }"><span>项目/批次{{ orderFilters.project ? ' ●' : '' }}</span><span class="filter-field-row"><input v-model="orderFilters.project" placeholder="项目名称或编码" /><button v-if="orderFilters.project" type="button" class="filter-clear-button" title="清空此项筛选" @click="orderFilters.project = ''">×</button></span></label>
              <label :class="{ 'filter-field--active': orderFilters.item }"><span>谷子名称{{ orderFilters.item ? ' ●' : '' }}</span><span class="filter-field-row"><input v-model="orderFilters.item" placeholder="商品名称，支持部分匹配" /><button v-if="orderFilters.item" type="button" class="filter-clear-button" title="清空此项筛选" @click="orderFilters.item = ''">×</button></span></label>
              <label :class="{ 'filter-field--active': orderFilters.series }"><span>谷子系列{{ orderFilters.series ? ' ●' : '' }}</span><span class="filter-field-row"><input v-model="orderFilters.series" placeholder="独立筛选，不与种类/角色合并" /><button v-if="orderFilters.series" type="button" class="filter-clear-button" title="清空此项筛选" @click="orderFilters.series = ''">×</button></span></label>
              <label :class="{ 'filter-field--active': orderFilters.category }"><span>谷子种类{{ orderFilters.category ? ' ●' : '' }}</span><span class="filter-field-row"><input v-model="orderFilters.category" placeholder="独立筛选，不与系列/角色合并" /><button v-if="orderFilters.category" type="button" class="filter-clear-button" title="清空此项筛选" @click="orderFilters.category = ''">×</button></span></label>
              <label :class="{ 'filter-field--active': orderFilters.role }"><span>谷子角色{{ orderFilters.role ? ' ●' : '' }}</span><span class="filter-field-row"><input v-model="orderFilters.role" placeholder="独立筛选，不与系列/种类合并" /><button v-if="orderFilters.role" type="button" class="filter-clear-button" title="清空此项筛选" @click="orderFilters.role = ''">×</button></span></label>
              <label :class="{ 'filter-field--active': orderFilters.importBatchID }"><span>导入批次 ID{{ orderFilters.importBatchID ? ' ●' : '' }}</span><input v-model="orderFilters.importBatchID" placeholder="import_batch_id" /></label>
              <label>
                <span>订单状态</span>
                <select v-model="orderFilters.status">
                  <option value="">全部</option>
                  <option value="draft">草稿</option>
                  <option value="submitted">已提交</option>
                  <option value="partially_paid">部分付款</option>
                  <option value="paid">已付款</option>
                  <option value="cancelled">已取消</option>
                </select>
              </label>
              <label><span>创建时间起</span><input v-model="orderFilters.createdFrom" type="date" /></label>
              <label><span>创建时间止</span><input v-model="orderFilters.createdTo" type="date" /></label>
              <div class="filter-actions">
                <button class="primary-button" type="submit" :disabled="ordersLoading">{{ ordersLoading ? '查询中' : '查询' }}</button>
                <button class="secondary-button" type="button" @click="resetOrderFilters">重置</button>
              </div>
            </form>

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

                    <th>创建时间</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="!ordersLoading && orderItems.length === 0"><td colspan="8">暂无订单数据。</td></tr>
                  <tr v-for="order in orderItems" :key="order.id">
                    <td><strong>{{ order.cn_code }}</strong><small>{{ order.display_name || '-' }}</small></td>
                    <td><span class="cell-clip" :title="order.project_name">{{ order.project_name }}</span><small>{{ order.order_no }}</small></td>
                    <td><span class="status-chip" :data-state="order.status">{{ statusLabel(order.status) }}</span></td>
                    <td>{{ order.item_type_count }}</td>
                    <td>{{ order.total_quantity }}</td>
                    <td>{{ formatMoney(order.total_amount) }}</td>

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
                <p class="muted">订单与明细信息</p>
              </div>
              <button class="secondary-button" type="button" @click="navigate('/admin/orders')">返回订单列表</button>
            </div>
            <div v-if="orderDetailMessage" class="inline-alert">{{ orderDetailMessage }}</div>
            <p v-if="orderDetailLoading" class="muted">正在加载订单详情。</p>

            <template v-if="orderDetail">
              <div class="summary-grid">
                <article class="metric-tile"><span>CN</span><strong>{{ orderDetail.order.cn_code }}</strong></article>
                <article class="metric-tile wide-metric"><span>项目</span><strong>{{ orderDetail.order.project_name }}</strong></article>
                <article class="metric-tile"><span>状态</span><strong>{{ statusLabel(orderDetail.order.status) }}</strong></article>
                <article class="metric-tile"><span>明细数</span><strong>{{ orderDetail.order.item_count }}</strong></article>
                <article class="metric-tile"><span>商品总数</span><strong>{{ orderDetail.order.total_quantity }}</strong></article>
                <article class="metric-tile"><span>订单总额</span><strong>{{ formatMoney(orderDetail.order.total_amount) }}</strong></article>
                <article class="metric-tile wide-metric"><span>来源</span><strong>{{ orderSources(orderDetail.order) }}</strong></article>
                <article class="metric-tile"><span>创建时间</span><strong>{{ formatDate(orderDetail.order.created_at) }}</strong></article>
              </div>

              <section class="panel nested-panel">
                <div class="panel__header"><h2>谷子明细</h2><span>共 {{ orderDetail.order.items.length }} 条明细</span></div>
                <div class="table-scroll detail-table">
                  <table>
                    <thead>
                      <tr>
                        <th>谷子名称</th>
                        <th>角色</th>
                        <th>分类</th>
                        <th>数量</th>
                        <th>单价</th>
                        <th>小计</th>
                        <th>已付</th>
                        <th>剩余</th>
                        <th>付款状态</th>
                        <th>来源 Excel</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-if="orderDetail.order.items.length === 0"><td colspan="10">无明细。</td></tr>
                      <tr v-for="item in orderDetail.order.items" :key="item.id">
                        <td>{{ item.display_name || item.product_name }}</td>
                        <td>{{ item.character_name || '-' }}</td>
                        <td>{{ item.category || '-' }}</td>
                        <td>{{ item.quantity }}</td>
                        <td>{{ formatMoney(item.unit_price) }}</td>
                        <td>{{ formatMoney(item.amount) }}</td>
                        <td>{{ formatMoney(item.paid_amount) }}</td>
                        <td :class="{ danger: item.remaining_amount > 0 }">{{ formatMoney(item.remaining_amount) }}</td>
                        <td>{{ queryPaymentStatusLabel(item.payment_status) }}</td>
                        <td>{{ item.import_filename || '-' }}</td>

                      </tr>
                    </tbody>
                  </table>
                </div>
              </section>
              <section v-if="orderDetailTechnicalIdentifiers(orderDetail).length > 0" class="panel nested-panel technical-section">
                <details>
                  <summary><span class="closed-label">▶ 查看技术标识</span><span class="open-label">▼ 收起技术标识</span></summary>
                  <div class="technical-list"><article v-for="identifier in mergeTechnicalIdentifiers(orderDetailTechnicalIdentifiers(orderDetail))" :key="identifier.type + '-' + identifier.value" class="technical-item"><div class="technical-item__head"><span class="technical-item__type">{{ identifier.type }}</span><span class="technical-item__context">{{ identifier.context }}</span><button type="button" class="copy-button" @click="copyIdentifier(identifier.value)">复制</button></div><code class="technical-item__value">{{ identifier.value }}</code></article></div>
                </details>
              </section>
            </template>
          </section>
        </template>
        <template v-else-if="routeName === 'admin-import-history'">
          <section class="panel">
            <div class="panel__header"><div><h2>导入历史</h2><p class="muted">可查看导入记录，并按导入批次安全软撤销。</p></div><button class="secondary-button" type="button" :disabled="historyLoading" @click="loadHistory">刷新</button></div>
            <div v-if="historyMessage" class="inline-alert">{{ historyMessage }}</div>
            <div class="table-scroll history-table"><table><thead><tr><th>文件</th><th>状态</th><th>上传</th><th>确认</th><th>工作表/批次</th><th>问题</th><th>写入结果</th><th>总金额</th><th></th></tr></thead><tbody><tr v-if="!historyLoading && importHistory.length === 0"><td colspan="9">暂无导入记录。</td></tr><tr v-for="item in importHistory" :key="item.id"><td><strong>{{ item.original_filename }}</strong><small>{{ formatBytes(item.file_size) }}</small></td><td><span class="status-chip" :data-state="item.status">{{ statusLabel(item.status) }}</span><small v-if="item.revoked_at">{{ formatDate(item.revoked_at) }}</small></td><td>{{ item.uploaded_by || '-' }}<small>{{ formatDate(item.created_at) }}</small></td><td>{{ item.confirmed_by || '-' }}<small>{{ formatDate(item.confirmed_at) }}</small></td><td>{{ item.sheet_count }} / {{ item.batch_count }}</td><td>E {{ item.error_count }} / W {{ item.warning_count }} / N {{ item.notice_count }}</td><td>{{ item.confirm_result ? `${item.confirm_result.order_count} 单 / ${item.confirm_result.order_item_count} 明细` : '-' }}<small v-if="item.revoke_result">已撤销 {{ item.revoke_result.order_item_count }} 明细</small></td><td>{{ formatMoney(historyTotalAmount(item)) }}</td><td><button class="secondary-button" type="button" @click="navigate(`/admin/imports/${item.id}`)">详情</button></td></tr></tbody></table></div>
            <section v-if="importHistoryTechnicalIdentifiers(importHistory).length > 0" class="panel nested-panel technical-section">
              <details>
                <summary><span class="closed-label">▶ 查看技术标识</span><span class="open-label">▼ 收起技术标识</span></summary>
                <div class="technical-list"><article v-for="identifier in mergeTechnicalIdentifiers(importHistoryTechnicalIdentifiers(importHistory))" :key="identifier.type + '-' + identifier.value" class="technical-item"><div class="technical-item__head"><span class="technical-item__type">{{ identifier.type }}</span><span class="technical-item__context">{{ identifier.context }}</span><button type="button" class="copy-button" @click="copyIdentifier(identifier.value)">复制</button></div><code class="technical-item__value">{{ identifier.value }}</code></article></div>
              </details>
            </section>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-import-detail'">
          <section class="panel">
            <div class="panel__header"><div><h2>导入详情</h2><p class="muted">导入文件、预览与写入结果</p></div><button class="secondary-button" type="button" @click="navigate('/admin/imports/history')">返回历史</button></div>
            <div v-if="detailMessage" class="inline-alert">{{ detailMessage }}</div>
            <p v-if="detailLoading" class="muted">正在加载详情。</p>
            <template v-if="importDetail">
              <div class="summary-grid">
                <article class="metric-tile wide-metric"><span>文件名</span><strong>{{ importDetail.import.original_filename }}</strong></article>
                <article class="metric-tile wide-metric"><span>文件校验</span><strong>{{ importDetail.import.file_hash ? '已记录' : '-' }}</strong></article>
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
              <section v-if="importDetailTechnicalIdentifiers(importDetail).length > 0" class="panel nested-panel technical-section">
                <details>
                  <summary><span class="closed-label">▶ 查看技术标识</span><span class="open-label">▼ 收起技术标识</span></summary>
                  <div class="technical-list"><article v-for="identifier in mergeTechnicalIdentifiers(importDetailTechnicalIdentifiers(importDetail))" :key="identifier.type + '-' + identifier.value" class="technical-item"><div class="technical-item__head"><span class="technical-item__type">{{ identifier.type }}</span><span class="technical-item__context">{{ identifier.context }}</span><button type="button" class="copy-button" @click="copyIdentifier(identifier.value)">复制</button></div><code class="technical-item__value">{{ identifier.value }}</code></article></div>
                </details>
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
          <template v-if="!queryUser && queryView === 'login'">
            <form class="login-form query-login" @submit.prevent="loginQuery">
              <label><span>CN</span><input v-model="queryCN" autocomplete="username" required placeholder="输入自己的 CN" /></label>
              <label><span>查询码</span><input v-model="queryCode" type="password" autocomplete="current-password" required placeholder="管理员提供的查询码" /></label>
              <button class="primary-button" type="submit" :disabled="queryLoading">{{ queryLoading ? '查询中' : '查询订单' }}</button>
            </form>
            <p class="query-bind-entry"><button class="link-button" type="button" @click="openBindView">首次设置查询码</button><span class="muted">（需要管理员提供的一次性绑定码）</span></p>
          </template>
          <template v-else-if="!queryUser && queryView === 'bind'">
            <form class="login-form query-login query-bind-form" @submit.prevent="submitBindCode">
              <label><span>CN</span><input v-model="bindCN" autocomplete="username" required placeholder="输入自己的 CN" /></label>
              <label><span>一次性绑定码</span><input v-model="bindTokenInput" type="password" autocomplete="one-time-code" required placeholder="管理员提供的绑定码" /></label>
              <label><span>新查询码</span><input v-model="bindNewCode" type="password" autocomplete="new-password" required minlength="6" maxlength="32" placeholder="6-32 位" /></label>
              <label><span>确认新查询码</span><input v-model="bindConfirmCode" type="password" autocomplete="new-password" required minlength="6" maxlength="32" placeholder="再次输入新查询码" /></label>
              <button class="primary-button" type="submit" :disabled="bindSubmitting">{{ bindSubmitting ? '设置中' : '设置查询码' }}</button>
              <button class="secondary-button" type="button" :disabled="bindSubmitting" @click="closeBindView">返回登录</button>
            </form>
            <div v-if="bindMessage" class="inline-alert">{{ bindMessage }}</div>
          </template>
          <div v-if="queryMessage" class="inline-alert">{{ queryMessage }}</div>
        </section>

        <template v-if="queryOrders">
          <section class="panel query-security-panel">
            <div class="panel__header">
              <div>
                <h2>账号安全</h2>
                <p class="muted">修改查询码后，当前查询登录会失效，请使用新查询码重新登录。</p>
              </div>
            </div>
            <form class="query-security-form" @submit.prevent="submitQueryCodeChange">
              <label><span>旧查询码</span><input v-model="queryOldCode" type="password" autocomplete="current-password" required placeholder="输入当前查询码" /></label>
              <label><span>新查询码</span><input v-model="queryNewCode" type="password" autocomplete="new-password" required minlength="6" maxlength="32" placeholder="6-32 位" /></label>
              <label><span>确认新查询码</span><input v-model="queryConfirmCode" type="password" autocomplete="new-password" required minlength="6" maxlength="32" placeholder="再次输入新查询码" /></label>
              <button class="primary-button" type="submit" :disabled="queryCodeChanging">{{ queryCodeChanging ? '修改中' : '修改查询码' }}</button>
            </form>
            <div v-if="querySecurityMessage" class="inline-alert">{{ querySecurityMessage }}</div>
          </section>

          <section class="summary-grid">
            <article class="metric-tile"><span>CN</span><strong>{{ queryOrders.user.cn_code }}</strong></article>
            <article class="metric-tile"><span>订单数</span><strong>{{ queryOrders.orders.length }}</strong></article>
            <article class="metric-tile"><span>总件数</span><strong>{{ queryOrders.total_quantity }}</strong></article>
            <article class="metric-tile"><span>总金额</span><strong>{{ formatMoney(queryOrders.total_amount) }}</strong></article>
            <article class="metric-tile"><span>已付金额</span><strong>{{ formatMoney(queryOrders.paid_amount) }}</strong></article>
            <article class="metric-tile"><span>未付金额</span><strong class="danger">{{ formatMoney(queryOrders.remaining_amount) }}</strong></article>
          </section>

          <section v-for="order in queryOrders.orders" :key="order.order_no" class="panel query-order-card">
            <div class="panel__header">
              <div>
                <h2>{{ order.project_name }}</h2>
                <p class="muted">{{ order.order_no }} / {{ formatDate(order.created_at) }}</p>
              </div>
              <div class="query-order-summary">
                <span><em>总金额</em><strong>{{ formatMoney(order.total_amount) }}</strong></span>
                <span><em>共</em><strong>{{ order.total_quantity }}</strong><em>件</em></span>
                <span><em>已付</em><strong>{{ formatMoney(order.paid_amount) }}</strong></span>
                <span class="is-unpaid"><em>未付</em><strong>{{ formatMoney(order.remaining_amount) }}</strong></span>
              </div>
            </div>
            <div class="table-scroll detail-table">
              <table>
                <thead>
                  <tr><th>谷子名称</th><th>谷子系列</th><th>分类</th><th>角色</th><th>数量</th><th>单价</th><th>小计</th><th>已付</th><th>剩余未付</th><th>付款状态</th></tr>
                </thead>
                <tbody>
                  <tr v-for="(item, itemIndex) in order.items" :key="`${item.goods_name}-${item.character_name}-${itemIndex}`">
                    <td>{{ item.display_name || item.goods_name }}</td>
                    <td>{{ item.series_code || '-' }}</td>
                    <td>{{ item.category || '-' }}</td>
                    <td>{{ queryCharacterLabel(item) }}</td>
                    <td>{{ item.quantity }}</td>
                    <td>{{ formatMoney(item.unit_price) }}</td>
                    <td>{{ formatMoney(item.amount) }}</td>
                    <td>{{ formatMoney(item.paid_amount) }}</td>
                    <td :class="{ danger: item.remaining_amount > 0 }">{{ formatMoney(item.remaining_amount) }}</td>
                    <td>{{ queryPaymentStatusLabel(item.payment_status) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
          <section v-if="queryOrders.orders.length === 0" class="panel"><p class="muted">当前 CN 暂无可查询订单。</p></section>

          <section v-if="queryOrders.payments.length > 0" class="panel query-payments-card">
            <div class="panel__header"><div><h2>付款历史</h2><p class="muted">已撤销的付款不计入有效已付款金额。展开关联明细可查看每笔付款分摊到了哪些谷子上。</p></div></div>
            <div class="table-scroll history-table">
              <table>
                <thead>
                  <tr><th>付款时间</th><th class="col-emphasis">实付金额</th><th>交肾状态</th><th>本金</th><th>手续费</th><th>付款方式</th><th>关联明细</th></tr>
                </thead>
                <tbody>
                  <template v-for="(payment, paymentIndex) in queryOrders.payments" :key="`${payment.paid_at}-${paymentIndex}`">
                    <tr :class="{ 'voided-row': payment.status === 'voided' }">
                      <td>{{ formatDate(payment.paid_at) }}</td>
                      <td class="col-emphasis">{{ formatMoney(payment.total_amount) }}</td>
                      <td><span class="status-chip" :data-state="payment.status">{{ paymentStatusLabel(payment.status) }}</span></td>
                      <td>{{ formatMoney(payment.principal_amount) }}</td>
                      <td>{{ formatMoney(payment.fee_amount) }}</td>
                      <td>{{ paymentMethodLabel(payment.payment_method || '') }}</td>
                      <td>共 {{ payment.items.length }} 条明细</td>
                    </tr>
                    <tr v-if="payment.items.length > 0" class="query-payment-items-row">
                      <td colspan="7">
                        <details class="query-payment-items">
                          <summary><span class="closed-label">▶ 查看关联明细（共 {{ payment.items.length }} 条）</span><span class="open-label">▼ 收起关联明细</span></summary>
                          <div class="table-scroll detail-table">
                            <table>
                              <thead>
                                <tr><th>谷子名称</th><th>角色</th><th>分类</th><th>数量</th><th>单价</th><th>小计</th><th>本次付款金额</th><th>当前付款状态</th></tr>
                              </thead>
                              <tbody>
                                <tr v-for="(item, itemIndex) in payment.items" :key="`${item.display_name}-${item.character_name}-${itemIndex}`">
                                  <td>{{ item.display_name }}</td>
                                  <td>{{ item.character_name || '—' }}</td>
                                  <td>{{ item.category || '—' }}</td>
                                  <td>{{ item.quantity }}</td>
                                  <td>{{ formatMoney(item.unit_price) }}</td>
                                  <td>{{ formatMoney(item.amount) }}</td>
                                  <td>{{ formatMoney(item.applied_amount) }}</td>
                                  <td>{{ queryPaymentStatusLabel(item.payment_status) }}</td>
                                </tr>
                              </tbody>
                            </table>
                          </div>
                        </details>
                      </td>
                    </tr>
                  </template>
                </tbody>
              </table>
            </div>
          </section>
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
