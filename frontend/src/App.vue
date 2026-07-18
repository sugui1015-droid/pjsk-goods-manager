<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import {
  ApiError,
  apiUrl,
  bindQueryCode,
  changeQueryCode,
  createQueryCodeBindToken,
  deleteAdminRecoveryEmail,
  getAdminRecoveryEmail,
  getQueryRecoveryEmail,
  putAdminRecoveryEmail,
  requestQueryCodeRecovery,
  resetRecoveredQueryCode,
  sendRecoveryEmailVerification,
  verifyRecoveryEmail,
  verifyQueryCodeRecovery,
  getJSON,
  postForm,
  postJSON,
  patchJSON,
  getPaymentQRAdminStatuses,
  uploadPaymentQR,
  disablePaymentQR,
  getPaymentQRAvailability,
  listUserPaymentSubmissions,
  submitPaymentSubmission,
  listAdminPaymentSubmissions,
  getAdminPaymentSubmissionFacets,
  getAdminPaymentSubmissionDetail,
  rejectPaymentSubmission,
  approvePaymentSubmission,
  type PaymentQRMethod,
  type PaymentQRAdminStatus,
  type PaymentQRAvailability,
  type UserPaymentSubmission,
  type AdminPaymentSubmissionListItem,
  type AdminPaymentSubmissionDetailResponse,
  type PaymentSubmissionFacetResponse,
  type Admin,
  type AdminUserDetailResponse,
  type AdminUserListItem,
  type AdminUserFacetResponse,
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
  type ImportFacetResponse,
  type ImportHistoryResponse,
  type ImportRevokeResponse,
  type ImportIssue,
  type ImportPreviewResponse,
  type OrderDetailResponse,
  type ColumnFacetResponse,
  type OrderListItem,
  type OrderListResponse,
  type OrderSummary,
  type PaymentDetailResponse,
  type PaymentItemRow,
  type PaymentListItem,
  type PaymentFacetResponse,
  type PaymentListResponse,
  type QueryLoginResponse,
  type QueryOrdersResponse,
  type RecoveryEmailState,
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
import ModuleCard from './components/ModuleCard.vue'
import PortalStatusBar from './components/PortalStatusBar.vue'
import ColumnValueFilter from './components/ColumnValueFilter.vue'
import ColumnRangeFilter from './components/ColumnRangeFilter.vue'
import ColumnDateFilter from './components/ColumnDateFilter.vue'
import {
  activeFilterCount,
  buildFilterParams,
  clearAllFilters as clearColumnFilters,
  createFilterState,
} from './filters/columnFilters'
const maxExcelSize = 20 * 1024 * 1024
const categoryPresets = ['吧唧', 'ep', '色纸', '立牌', '麻将', '亚克力']

type RouteName =
  | 'home'
  | 'query' | 'query-orders' | 'query-payment' | 'query-payments' | 'query-security'
  | 'admin' | 'admin-data' | 'admin-import' | 'admin-import-history' | 'admin-import-detail'
  | 'admin-orders' | 'admin-order-detail'
  | 'admin-users' | 'admin-user-detail'
  | 'admin-finance' | 'admin-payments' | 'admin-payment-detail' | 'admin-qr'
  | 'admin-submissions' | 'admin-submission-detail'
type IssueFilter = 'all' | 'row_error' | 'fatal_error' | 'warning' | 'notice'
type TextFilterKey = 'sheet' | 'sheetTitle' | 'batch' | 'cn' | 'category' | 'role' | 'itemName' | 'source'
type QuickFilterGroup = { key: TextFilterKey; label: string; options: string[] }

const fallbackConfig: ConfigResponse = {
  name: 'PJSK Goods Next',
  stage: 'local-shell',
  legacyAdminPort: '8512',
  legacyUserPort: '8513',
  frontendOrigins: ['http://localhost:5173', 'http://127.0.0.1:5173'],
  emailDeliveryEnabled: false,
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
const routeName = ref<RouteName>(routeFromPath(window.location.pathname))
const routeImportID = ref(importIDFromPath(window.location.pathname))
const routeOrderID = ref(orderIDFromPath(window.location.pathname))
const routePaymentID = ref(paymentIDFromPath(window.location.pathname))
const routeUserID = ref(userIDFromPath(window.location.pathname))
const routeSubmissionID = ref(submissionIDFromPath(window.location.pathname))

const adminUsers = ref<AdminUserListItem[]>([])
const adminUsersSummary = ref<AdminUserListSummary | null>(null)
const adminUsersLoading = ref(false)
const adminUsersMessage = ref('')

// User table filters live entirely in the column headers — there is no
// top-of-page filter form. The value-column keys double as the API's parameter
// names.
const adminUserFilterState = ref(
  createFilterState({
    valueColumns: ['cn', 'name', 'status', 'has_query_code', 'has_recovery_email'],
    rangeColumns: ['order_count', 'total', 'paid', 'unpaid'],
    dateColumns: ['last_login', 'created'],
  }),
)
const ADMIN_USER_RANGE_PARAMS: Record<string, [string, string]> = {
  order_count: ['order_count_min', 'order_count_max'],
  total: ['total_min', 'total_max'],
  paid: ['paid_min', 'paid_max'],
  unpaid: ['unpaid_min', 'unpaid_max'],
}
const ADMIN_USER_DATE_PARAMS: Record<string, [string, string]> = {
  last_login: ['last_login_from', 'last_login_to'],
  created: ['created_from', 'created_to'],
}
const adminUserPage = ref(1)
const adminUserPageSize = ref(50)
const adminUserTotal = ref(0)
const adminUserTotalPages = ref(0)
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
const adminRecoveryEmail = ref<RecoveryEmailState | null>(null)
const adminRecoveryEmailLoading = ref(false)
const adminRecoveryEmailSaving = ref(false)
const adminRecoveryEmailDraft = ref('')
const adminRecoveryEmailReason = ref('')
const adminRecoveryEmailMessage = ref('')
const admin = ref<Admin | null>(null)
const authChecked = ref(false)
const authMessage = ref('')
const loginUsername = ref('admin')
const loginPassword = ref('')
const loginLoading = ref(false)
// When an unauthenticated request hits a protected deep link, we remember it
// here and return to it after a successful login (per role).
const pendingAdminTarget = ref('')
const pendingQueryTarget = ref('')
const defaultAdminTarget = '/admin'

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

// Import-history WPS filters live entirely in the column headers. The value
// column keys double as the API's parameter names.
const importFilterState = ref(
  createFilterState({
    valueColumns: ['filename', 'status', 'uploaded_by'],
    rangeColumns: ['sheet', 'issue', 'written', 'amount'],
    dateColumns: ['created', 'confirmed'],
  }),
)
const IMPORT_RANGE_PARAMS: Record<string, [string, string]> = {
  sheet: ['sheet_min', 'sheet_max'],
  issue: ['issue_min', 'issue_max'],
  written: ['written_min', 'written_max'],
  amount: ['amount_min', 'amount_max'],
}
const IMPORT_DATE_PARAMS: Record<string, [string, string]> = {
  created: ['created_from', 'created_to'],
  confirmed: ['confirmed_from', 'confirmed_to'],
}
const importPage = ref(1)
const importPageSize = ref(50)
const importTotal = ref(0)
const importTotalPages = ref(0)
const importActiveFilterCount = computed(() => activeFilterCount(importFilterState.value))
const detailLoading = ref(false)
const detailMessage = ref('')
const importDetail = ref<ImportDetailResponse | null>(null)
const revokeLoading = ref(false)
const revokeMessage = ref('')

const ordersLoading = ref(false)
const ordersMessage = ref('')
const orderItems = ref<OrderListItem[]>([])
const orderDetailLoading = ref(false)
const orderDetailMessage = ref('')
const orderDetail = ref<OrderDetailResponse | null>(null)

// Order table filters live entirely in the column headers now — there is no
// top-of-page filter form and no "高级筛选" drawer. The keys double as the API's
// parameter names for value columns.
//
// 'project' and 'category' are deliberately absent: those columns were removed
// from the table, and a filter the table cannot show has nowhere to display its
// active state. The backend still accepts both (the export and other callers
// rely on them); this page simply never sends or faceting them.
const orderFilterState = ref(
  createFilterState({
    valueColumns: ['cn', 'item', 'series', 'role', 'status', 'payment_status'],
    rangeColumns: ['quantity', 'amount', 'paid', 'unpaid'],
    dateColumns: ['created'],
  }),
)
const ORDER_RANGE_PARAMS: Record<string, [string, string]> = {
  quantity: ['quantity_min', 'quantity_max'],
  amount: ['amount_min', 'amount_max'],
  paid: ['paid_min', 'paid_max'],
  unpaid: ['unpaid_min', 'unpaid_max'],
}
const ORDER_DATE_PARAMS: Record<string, [string, string]> = {
  created: ['created_from', 'created_to'],
}
const orderPage = ref(1)
const orderPageSize = ref(50)
const orderTotal = ref(0)
const orderTotalPages = ref(0)
const paymentEntryCN = ref('')
const cnPayment = ref<CNPaymentResponse | null>(null)
const cnPaymentLoading = ref(false)
const cnPaymentMessage = ref('')
// Display-only filters for the loaded CN's unpaid items. They narrow which rows
// are shown; selection (selectedPaymentItemIds) and amounts (paymentAmounts)
// always operate on the full cnPayment.items, so a selected row stays selected
// and counted even while filtered out of view.
const cnPaymentItemFilters = ref({ category: '', role: '', series: '' })
const cnPaymentFilterOptions = computed(() => {
  const items = cnPayment.value?.items ?? []
  return {
    categories: [...new Set(items.map((i) => (i.category ?? '').trim()).filter((v) => v !== ''))].sort(),
    roles: [...new Set(items.map((i) => (i.character_name ?? '').trim()).filter((v) => v !== ''))].sort(),
    series: [...new Set(items.map((i) => (i.series_code ?? '').trim()).filter((v) => v !== ''))].sort(),
  }
})
const cnPaymentFiltersActive = computed(() => {
  const f = cnPaymentItemFilters.value
  return f.category !== '' || f.role !== '' || f.series !== ''
})
const filteredCnPaymentItems = computed(() => {
  const items = cnPayment.value?.items ?? []
  const f = cnPaymentItemFilters.value
  if (!cnPaymentFiltersActive.value) return items
  return items.filter((item) =>
    (f.category === '' || (item.category ?? '') === f.category) &&
    (f.role === '' || (item.character_name ?? '') === f.role) &&
    (f.series === '' || (item.series_code ?? '') === f.series),
  )
})
function clearCnPaymentItemFilters() {
  cnPaymentItemFilters.value = { category: '', role: '', series: '' }
}
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
// Payment table filters live entirely in the column headers now — there is no
// top-of-page filter form and no "高级筛选" drawer. The value-column keys double
// as the API's parameter names.
//
// There is deliberately no separate "是否撤销" column: voiding is already carried
// by status=voided, and the old page had both controls writing to the same
// status field, where they overwrote each other. 撤销时间 filters the void
// timestamp itself, with a blank option meaning "not voided".
const paymentFilterState = ref(
  createFilterState({
    valueColumns: ['cn', 'payment_method', 'status', 'created_by'],
    rangeColumns: ['principal', 'fee', 'payable'],
    dateColumns: ['paid', 'voided'],
  }),
)
const PAYMENT_RANGE_PARAMS: Record<string, [string, string]> = {
  principal: ['principal_min', 'principal_max'],
  fee: ['fee_min', 'fee_max'],
  payable: ['payable_min', 'payable_max'],
}
const PAYMENT_DATE_PARAMS: Record<string, [string, string]> = {
  paid: ['paid_from', 'paid_to'],
  voided: ['voided_from', 'voided_to'],
}
const paymentPage = ref(1)
const paymentPageSize = ref(50)
const paymentTotal = ref(0)
const paymentTotalPages = ref(0)
const paymentActiveFilterCount = computed(() => activeFilterCount(paymentFilterState.value))

// --- Payment proof submissions ("收肾记录") ---
// Regular-user side: the user's own submission history on the payment center,
// plus a single pending upload (transient object URL preview, revoked on clear).
const userSubmissions = ref<UserPaymentSubmission[]>([])
const userSubmissionsLoading = ref(false)
const userSubmissionsMessage = ref('')
const submissionAcceptedTypes = ['image/png', 'image/jpeg', 'image/webp']
const submissionMaxBytes = 10 * 1024 * 1024
const submissionFile = ref<File | null>(null)
const submissionPreviewURL = ref('')
const submissionUploading = ref(false)
const submissionUploadMessage = ref('')
const canSubmitProof = computed(() => submissionFile.value !== null && !submissionUploading.value)

// Admin side: the WPS proof table. Value/range/date column keys double as the
// API parameter names, exactly like the other admin tables.
const paymentSubmissions = ref<AdminPaymentSubmissionListItem[]>([])
const paymentSubmissionsLoading = ref(false)
const paymentSubmissionsMessage = ref('')
const submissionFilterState = ref(
  createFilterState({
    valueColumns: ['cn', 'payment_method', 'status', 'reviewed_by'],
    rangeColumns: ['principal', 'fee', 'payable'],
    dateColumns: ['submitted', 'reviewed'],
  }),
)
const SUBMISSION_RANGE_PARAMS: Record<string, [string, string]> = {
  principal: ['principal_min', 'principal_max'],
  fee: ['fee_min', 'fee_max'],
  payable: ['payable_min', 'payable_max'],
}
const SUBMISSION_DATE_PARAMS: Record<string, [string, string]> = {
  submitted: ['submitted_from', 'submitted_to'],
  reviewed: ['reviewed_from', 'reviewed_to'],
}
const submissionPage = ref(1)
const submissionPageSize = ref(50)
const submissionTotal = ref(0)
const submissionTotalPages = ref(0)
const submissionActiveFilterCount = computed(() => activeFilterCount(submissionFilterState.value))

// Admin submission detail + review (approve reuses the record-payment allocation).
const submissionDetail = ref<AdminPaymentSubmissionDetailResponse | null>(null)
const submissionDetailLoading = ref(false)
const submissionDetailMessage = ref('')
const submissionActionMessage = ref('')
const submissionRejectReason = ref('')
const submissionRejecting = ref(false)
const submissionApproving = ref(false)
const submissionApproveNote = ref('')
const submissionImageReloadKey = ref(0)

// Payment collection QR management (admin). Image bytes never live in app
// state: the current image is loaded from the backend image endpoint, and the
// local pre-upload preview uses a transient object URL that is revoked on clear.
const qrMethods: PaymentQRMethod[] = ['alipay', 'wechat']
const qrAcceptedTypes = ['image/png', 'image/jpeg', 'image/webp']
const qrMaxBytes = 5 * 1024 * 1024
const paymentQRStatuses = ref<PaymentQRAdminStatus[]>([])
const paymentQRLoading = ref(false)
const paymentQRMessage = ref('')
// Bumped after every successful upload/disable so <img> src changes and the
// browser refetches instead of showing a stale cached image.
const paymentQRReloadKey = ref(0)
const qrSelectedFile = ref<Record<PaymentQRMethod, File | null>>({ alipay: null, wechat: null })
const qrPreviewURL = ref<Record<PaymentQRMethod, string>>({ alipay: '', wechat: '' })
const qrUploading = ref<Record<PaymentQRMethod, boolean>>({ alipay: false, wechat: false })
const qrDisabling = ref<Record<PaymentQRMethod, boolean>>({ alipay: false, wechat: false })
const paymentQRStatusByMethod = computed<Record<PaymentQRMethod, PaymentQRAdminStatus>>(() => {
  const map: Record<PaymentQRMethod, PaymentQRAdminStatus> = {
    alipay: { payment_method: 'alipay', configured: false },
    wechat: { payment_method: 'wechat', configured: false },
  }
  for (const status of paymentQRStatuses.value) {
    map[status.payment_method] = status
  }
  return map
})

const queryCN = ref('')
const queryCode = ref('')
const queryUser = ref<QueryUser | null>(null)
const queryOrders = ref<QueryOrdersResponse | null>(null)
const queryLoading = ref(false)
const queryOrdersError = ref('')
const queryMessage = ref('')
// Regular-user payment QR: which methods are usable, the selected method, and a
// click-to-enlarge lightbox. The user never sees any technical field; the image
// is loaded from the query image endpoint and never held as bytes in state.
const queryQRAvailability = ref<PaymentQRAvailability[]>([])
const queryQRLoading = ref(false)
const queryQRError = ref('')
const queryQRMethod = ref<PaymentQRMethod | ''>('')
const queryQRZoom = ref(false)
const queryQRReloadKey = ref(0)
// Both methods are always offered in the payment center.
const queryPayMethods: PaymentQRMethod[] = ['alipay', 'wechat']
// Fee is computed in integer cents, mirroring the backend rule exactly:
// alipay → 0; wechat → ceil(base/1000) cents (0.1%, any remainder rounds up).
// No floating-point rounding is used for the fee.
const queryBaseCents = computed(() => Math.round((queryOrders.value?.remaining_amount ?? 0) * 100))
const queryFeeCents = computed(() => (queryQRMethod.value === 'wechat' ? Math.floor((queryBaseCents.value + 999) / 1000) : 0))
const queryBaseAmount = computed(() => queryBaseCents.value / 100)
const queryFeeAmount = computed(() => queryFeeCents.value / 100)
const queryPayableAmount = computed(() => (queryBaseCents.value + queryFeeCents.value) / 100)
// Regular-user client-side filters. Data volume is bounded (a single CN), so
// filtering happens in the browser over the already-loaded response. Order
// filters and payment-history filters are independent: each controls only its
// own list, and neither changes the "付款汇总" totals (those stay whole-data).
const queryOrderFilters = ref({ category: '', role: '', series: '', paymentStatus: '' })
const queryPaymentFilters = ref({ method: '', status: '', dateFrom: '', dateTo: '' })

function uniqueSorted(values: (string | undefined)[]): string[] {
  return [...new Set(values.map((v) => (v ?? '').trim()).filter((v) => v !== ''))].sort()
}

const queryOrderFilterOptions = computed(() => {
  const items = (queryOrders.value?.orders ?? []).flatMap((order) => order.items)
  return {
    categories: uniqueSorted(items.map((i) => i.category)),
    roles: uniqueSorted(items.map((i) => i.character_name)),
    series: uniqueSorted(items.map((i) => i.series_code)),
  }
})

const queryOrderFiltersActive = computed(() => {
  const f = queryOrderFilters.value
  return f.category !== '' || f.role !== '' || f.series !== '' || f.paymentStatus !== ''
})

// Orders with their items filtered; orders with no matching item are dropped.
// The original queryOrders is never mutated.
const filteredQueryOrders = computed(() => {
  const orders = queryOrders.value?.orders ?? []
  const f = queryOrderFilters.value
  if (!queryOrderFiltersActive.value) return orders
  return orders
    .map((order) => ({
      ...order,
      items: order.items.filter((item) =>
        (f.category === '' || (item.category ?? '') === f.category) &&
        (f.role === '' || (item.character_name ?? '') === f.role) &&
        (f.series === '' || (item.series_code ?? '') === f.series) &&
        (f.paymentStatus === '' || item.payment_status === f.paymentStatus),
      ),
    }))
    .filter((order) => order.items.length > 0)
})

const filteredQueryOrderItemCount = computed(() =>
  filteredQueryOrders.value.reduce((total, order) => total + order.items.length, 0),
)

function clearQueryOrderFilters() {
  queryOrderFilters.value = { category: '', role: '', series: '', paymentStatus: '' }
}

const queryPaymentFiltersActive = computed(() => {
  const f = queryPaymentFilters.value
  return f.method !== '' || f.status !== '' || f.dateFrom !== '' || f.dateTo !== ''
})

const filteredQueryPayments = computed(() => {
  const payments = queryOrders.value?.payments ?? []
  const f = queryPaymentFilters.value
  if (!queryPaymentFiltersActive.value) return payments
  const fromTime = f.dateFrom ? new Date(f.dateFrom).getTime() : null
  const toTime = f.dateTo ? new Date(f.dateTo).getTime() : null
  return payments.filter((p) => {
    if (f.method !== '' && (p.payment_method ?? '') !== f.method) return false
    if (f.status !== '' && p.status !== f.status) return false
    const paidTime = p.paid_at ? new Date(p.paid_at).getTime() : NaN
    if (fromTime !== null && !(paidTime >= fromTime)) return false
    if (toTime !== null && !(paidTime <= toTime)) return false
    return true
  })
})

function clearQueryPaymentFilters() {
  queryPaymentFilters.value = { method: '', status: '', dateFrom: '', dateTo: '' }
}
const queryOldCode = ref('')
const queryNewCode = ref('')
const queryConfirmCode = ref('')
const queryCodeChanging = ref(false)
const querySecurityMessage = ref('')
const queryRecoveryEmail = ref<RecoveryEmailState | null>(null)
const queryRecoveryEmailLoading = ref(false)
const queryRecoveryEmailMessage = ref('')
const queryRecoveryVerificationCode = ref('')
const queryRecoverySending = ref(false)
const queryRecoveryVerifying = ref(false)
const queryRecoveryCooldownUntil = ref(0)
const queryRecoveryExpiresAt = ref('')
const queryRecoveryClock = ref(Date.now())
let queryRecoveryClockTimer: number | undefined// First-time bind flow on the user login page. The bind token is held only
// in this in-memory form state — never persisted, gone on refresh.
const queryView = ref<'login' | 'bind' | 'recovery'>('login')
const bindCN = ref('')
const bindTokenInput = ref('')
const bindNewCode = ref('')
const bindConfirmCode = ref('')
const bindSubmitting = ref(false)
const bindMessage = ref('')
// Anonymous recovery secrets remain only in Vue memory. They are never put
// in the URL or browser storage and disappear on refresh or flow exit.
const anonymousRecoveryStep = ref<'request' | 'verify' | 'reset'>('request')
const anonymousRecoveryCN = ref('')
const anonymousRecoveryCode = ref('')
const anonymousRecoveryResetToken = ref('')
const anonymousRecoveryTokenExpiresAt = ref('')
const anonymousRecoveryNewCode = ref('')
const anonymousRecoveryConfirmCode = ref('')
const anonymousRecoveryLoading = ref(false)
const anonymousRecoveryMessage = ref('')

const isBackendOnline = computed(() => health.value?.status === 'ok')
const queryRecoveryCooldownSeconds = computed(() => Math.max(0, Math.ceil((queryRecoveryCooldownUntil.value - queryRecoveryClock.value) / 1000)))
const queryRecoveryCanSend = computed(() => config.value.emailDeliveryEnabled && queryRecoveryEmail.value?.status === 'pending' && queryRecoveryCooldownSeconds.value === 0 && !queryRecoverySending.value)
// Admin *business* routes (data/orders/users/finance pages) require an admin
// session. The admin portal/login page (`/admin`, routeName 'admin') and the
// role entry / user pages are excluded — they render their own chrome.
const isAdminRoute = computed(() => routeName.value.startsWith('admin-'))
// User *business* module pages require a query session; the user portal/login
// page (`/query`, routeName 'query') renders login-or-portal itself.
const isUserRoute = computed(() => routeName.value.startsWith('query-'))
// Whether the current route belongs to the admin surface at all (portal + pages).
const isAdminSurface = computed(() => routeName.value === 'admin' || isAdminRoute.value)
// The admin module the current business route belongs to, and its display name.
const adminModule = computed(() => {
  const r = routeName.value
  if (r === 'admin-data' || r === 'admin-import' || r === 'admin-import-history' || r === 'admin-import-detail') return 'data'
  if (r === 'admin-orders' || r === 'admin-order-detail') return 'orders'
  if (r === 'admin-users' || r === 'admin-user-detail') return 'users'
  if (r === 'admin-finance' || r === 'admin-payments' || r === 'admin-payment-detail' || r === 'admin-qr' || r === 'admin-submissions' || r === 'admin-submission-detail') return 'finance'
  return ''
})
const adminModuleTitle = computed(() => (({ data: '数据导入中心', orders: '订单管理', users: '用户与账号', finance: '收付款管理' }) as Record<string, string>)[adminModule.value] ?? '')
// The user module the current route belongs to, and its display name.
const userModule = computed(() => {
  const r = routeName.value
  if (r === 'query-orders') return 'orders'
  if (r === 'query-payment') return 'payment'
  if (r === 'query-payments') return 'payments'
  if (r === 'query-security') return 'security'
  return ''
})
const userModuleTitle = computed(() => (({ orders: '我的订单', payment: '付款中心', payments: '付款记录', security: '账户安全' }) as Record<string, string>)[userModule.value] ?? '')
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

// Old bookmarked admin URLs are transparently redirected to the new module
// structure. Backend API paths are unchanged — only front-end URLs move.
function canonicalPath(path: string): string {
  const map: Record<string, string> = {
    '/admin/imports': '/admin/data/import',
    '/admin/imports/history': '/admin/data/history',
    '/admin/payments': '/admin/finance/payments',
    '/admin/payment-qr': '/admin/finance/qr-codes',
  }
  if (map[path]) return map[path]
  if (path.startsWith('/admin/imports/') && path !== '/admin/imports/preview') {
    return '/admin/data/history/' + path.slice('/admin/imports/'.length)
  }
  if (path.startsWith('/admin/payments/')) {
    return '/admin/finance/payments/' + path.slice('/admin/payments/'.length)
  }
  return path
}

function routeFromPath(path: string): RouteName {
  if (path === '/query') return 'query'
  if (path === '/query/orders') return 'query-orders'
  if (path === '/query/payment') return 'query-payment'
  if (path === '/query/payments') return 'query-payments'
  if (path === '/query/security') return 'query-security'
  if (path === '/admin') return 'admin'
  if (path === '/admin/data') return 'admin-data'
  if (path === '/admin/data/import') return 'admin-import'
  if (path === '/admin/data/history') return 'admin-import-history'
  if (path.startsWith('/admin/data/history/')) return 'admin-import-detail'
  if (path === '/admin/orders') return 'admin-orders'
  if (path.startsWith('/admin/orders/')) return 'admin-order-detail'
  if (path === '/admin/users') return 'admin-users'
  if (path.startsWith('/admin/users/')) return 'admin-user-detail'
  if (path === '/admin/finance') return 'admin-finance'
  if (path === '/admin/finance/payments') return 'admin-payments'
  if (path.startsWith('/admin/finance/payments/')) return 'admin-payment-detail'
  if (path === '/admin/finance/qr-codes') return 'admin-qr'
  if (path === '/admin/finance/submissions') return 'admin-submissions'
  if (path.startsWith('/admin/finance/submissions/')) return 'admin-submission-detail'
  return 'home'
}

function importIDFromPath(path: string) {
  if (!path.startsWith('/admin/data/history/')) return ''
  return decodeURIComponent(path.slice('/admin/data/history/'.length).replace(/\/$/, ''))
}

function orderIDFromPath(path: string) {
  if (!path.startsWith('/admin/orders/')) return ''
  return decodeURIComponent(path.slice('/admin/orders/'.length).replace(/\/$/, ''))
}

function paymentIDFromPath(path: string) {
  if (!path.startsWith('/admin/finance/payments/')) return ''
  return decodeURIComponent(path.slice('/admin/finance/payments/'.length).replace(/\/$/, ''))
}

function userIDFromPath(path: string) {
  if (!path.startsWith('/admin/users/')) return ''
  return decodeURIComponent(path.slice('/admin/users/'.length).replace(/\/$/, ''))
}

function submissionIDFromPath(path: string) {
  if (!path.startsWith('/admin/finance/submissions/')) return ''
  return decodeURIComponent(path.slice('/admin/finance/submissions/'.length).replace(/\/$/, ''))
}

function applyRoute(path: string) {
  const canonical = canonicalPath(path)
  if (canonical !== path) window.history.replaceState(null, '', canonical)
  routeName.value = routeFromPath(canonical)
  routeImportID.value = importIDFromPath(canonical)
  routeOrderID.value = orderIDFromPath(canonical)
  routePaymentID.value = paymentIDFromPath(canonical)
  routeUserID.value = userIDFromPath(canonical)
  routeSubmissionID.value = submissionIDFromPath(canonical)
}

function navigate(path: string) {
  const canonical = canonicalPath(path)
  window.history.pushState(null, '', canonical)
  applyRoute(canonical)
  void handleRouteChange()
}

async function handleRouteChange() {
  if (isAdminRoute.value) return handleAdminRouteEntered()
  if (isUserRoute.value) return handleUserRouteEntered()
  if (routeName.value === 'admin') return handleAdminPortalEntered()
  if (routeName.value === 'query') return handleQueryPortalEntered()
}

async function handleAdminRouteEntered() {
  await ensureAdmin()
  if (!admin.value) {
    pendingAdminTarget.value = window.location.pathname + window.location.search
    navigate('/admin')
    return
  }
  if (routeName.value === 'admin-import-history') await loadHistory()
  if (routeName.value === 'admin-import-detail' && routeImportID.value) await loadDetail(routeImportID.value)
  if (routeName.value === 'admin-orders') await loadOrders()
  if (routeName.value === 'admin-order-detail' && routeOrderID.value) await loadOrderDetail(routeOrderID.value)
  if (routeName.value === 'admin-payments') await loadPaymentRecords()
  if (routeName.value === 'admin-payment-detail' && routePaymentID.value) await loadPaymentDetail(routePaymentID.value)
  if (routeName.value === 'admin-qr') await loadPaymentQRStatuses()
  if (routeName.value === 'admin-users') await loadAdminUsers()
  if (routeName.value === 'admin-user-detail' && routeUserID.value) await loadAdminUserDetail(routeUserID.value)
  if (routeName.value === 'admin-submissions') await loadPaymentSubmissions()
  if (routeName.value === 'admin-submission-detail' && routeSubmissionID.value) await loadSubmissionDetail(routeSubmissionID.value)
}

async function handleAdminPortalEntered() {
  await ensureAdmin()
  if (admin.value && pendingAdminTarget.value) {
    const target = pendingAdminTarget.value
    pendingAdminTarget.value = ''
    navigate(target)
    return
  }
  if (!admin.value) authMessage.value = ''
}

async function handleUserRouteEntered() {
  if (!queryUser.value) await loadQueryOrders(false)
  if (!queryUser.value) {
    pendingQueryTarget.value = window.location.pathname + window.location.search
    navigate('/query')
    return
  }
  if (routeName.value === 'query-orders' || routeName.value === 'query-payment' || routeName.value === 'query-payments') {
    if (!queryOrders.value) await loadQueryOrders(false)
  }
  if (routeName.value === 'query-payment') await loadUserSubmissions()
}

async function handleQueryPortalEntered() {
  if (queryUser.value && !queryOrders.value) await loadQueryOrders(false)
  if (queryUser.value && pendingQueryTarget.value) {
    const target = pendingQueryTarget.value
    pendingQueryTarget.value = ''
    navigate(target)
  }
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
    // Return to the originally requested admin page, or the admin default.
    const target = pendingAdminTarget.value || defaultAdminTarget
    pendingAdminTarget.value = ''
    navigate(target)
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
  pendingAdminTarget.value = ''
  // Leave the admin surface entirely: back to the admin login page.
  navigate('/admin')
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

// The one place the import filter state becomes API parameters — shared by the
// list and the facet popovers so they never disagree about what is filtered.
function importFilterParams() {
  return buildFilterParams(importFilterState.value, IMPORT_RANGE_PARAMS, IMPORT_DATE_PARAMS)
}

async function loadImportFacets(request: { column: string; search: string; page: number }): Promise<ColumnFacetResponse> {
  const params = importFilterParams()
  params.set('column', request.column)
  if (request.search) params.set('search', request.search)
  params.set('facet_page', String(request.page))
  const response = await getJSON<ImportFacetResponse>(`/api/admin/imports/facets?${params.toString()}`)
  return {
    column: response.column,
    values: response.values ?? [],
    total: response.total,
    blank_count: response.blank_count,
    page: response.facet_page,
    page_size: response.facet_page_size,
    has_more: response.has_more,
  }
}

function applyImportFilters() {
  importPage.value = 1
  void loadHistory()
}

function goToImportPage(page: number) {
  if (page < 1 || (importTotalPages.value > 0 && page > importTotalPages.value)) return
  importPage.value = page
  void loadHistory()
}

function changeImportPageSize() {
  importPage.value = 1
  void loadHistory()
}

function resetImportFilters() {
  clearColumnFilters(importFilterState.value)
  applyImportFilters()
}

async function loadHistory() {
  historyLoading.value = true
  historyMessage.value = ''
  try {
    const params = importFilterParams()
    params.set('page', String(importPage.value))
    params.set('page_size', String(importPageSize.value))
    const response = await getJSON<ImportHistoryResponse>(`/api/admin/imports?${params.toString()}`)
    importHistory.value = response.items ?? []
    importPage.value = response.page
    importPageSize.value = response.page_size
    importTotal.value = response.total
    importTotalPages.value = response.total_pages
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    const detail = error instanceof Error ? error.message : '未知错误'
    historyMessage.value = `导入历史加载失败：${detail}`
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
// The one place the order filter state becomes API parameters. The list, the
// facet popovers and the export all go through it, which is what keeps the
// three from ever disagreeing about what is filtered.
function orderFilterParams() {
  return buildFilterParams(orderFilterState.value, ORDER_RANGE_PARAMS, ORDER_DATE_PARAMS)
}

// Count of filtered columns (for the "清空全部筛选" control).
const orderActiveFilterCount = computed(() => activeFilterCount(orderFilterState.value))

// Facet loader handed to every value popover: it sends the current filter state
// alongside the requested column, and the backend drops that column's own
// selection when computing candidates.
async function loadOrderFacets(request: { column: string; search: string; page: number }): Promise<ColumnFacetResponse> {
  const params = orderFilterParams()
  params.set('column', request.column)
  if (request.search) params.set('search', request.search)
  params.set('facet_page', String(request.page))
  return await getJSON<ColumnFacetResponse>(`/api/admin/orders/facets?${params.toString()}`)
}

// Applying a filter returns to page 1: staying on page 7 of a result set that
// just shrank to two pages would show an empty table.
function applyOrderFilters() {
  orderPage.value = 1
  void loadOrders()
}

function goToOrderPage(page: number) {
  if (page < 1 || (orderTotalPages.value > 0 && page > orderTotalPages.value)) return
  orderPage.value = page
  void loadOrders()
}

function changeOrderPageSize() {
  orderPage.value = 1
  void loadOrders()
}

async function loadOrders() {
  ordersLoading.value = true
  ordersMessage.value = ''
  try {
    const params = orderFilterParams()
    params.set('page', String(orderPage.value))
    params.set('page_size', String(orderPageSize.value))
    const response = await getJSON<OrderListResponse>(`/api/admin/orders?${params.toString()}`)
    orderItems.value = response.items ?? []
    orderPage.value = response.page
    orderPageSize.value = response.page_size
    orderTotal.value = response.total
    orderTotalPages.value = response.total_pages
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    const detail = error instanceof Error ? error.message : '未知错误'
    ordersMessage.value = `订单列表加载失败：${detail}`
  } finally {
    ordersLoading.value = false
  }
}

// The one place the payment filter state becomes API parameters. The list, the
// facet popovers and the export all go through it, which is what keeps the
// three from ever disagreeing about what is filtered.
function paymentFilterParams() {
  return buildFilterParams(paymentFilterState.value, PAYMENT_RANGE_PARAMS, PAYMENT_DATE_PARAMS)
}

// Facet loader handed to every value popover. The backend drops the requested
// column's own selection when computing candidates, and names its pager
// facet_page/facet_page_size — adapted here into the shape the popover reads.
async function loadPaymentFacets(request: { column: string; search: string; page: number }): Promise<ColumnFacetResponse> {
  const params = paymentFilterParams()
  params.set('column', request.column)
  if (request.search) params.set('search', request.search)
  params.set('facet_page', String(request.page))
  const response = await getJSON<PaymentFacetResponse>(`/api/admin/payments/facets?${params.toString()}`)
  return {
    column: response.column,
    values: response.values ?? [],
    total: response.total,
    blank_count: response.blank_count,
    page: response.facet_page,
    page_size: response.facet_page_size,
    has_more: response.has_more,
  }
}

// Applying a filter returns to page 1: staying on page 7 of a result set that
// just shrank to two pages would show an empty table.
function applyPaymentFilters() {
  paymentPage.value = 1
  void loadPaymentRecords()
}

function goToPaymentPage(page: number) {
  if (page < 1 || (paymentTotalPages.value > 0 && page > paymentTotalPages.value)) return
  paymentPage.value = page
  void loadPaymentRecords()
}

function changePaymentPageSize() {
  paymentPage.value = 1
  void loadPaymentRecords()
}

async function loadPaymentRecords() {
  paymentRecordsLoading.value = true
  paymentRecordsMessage.value = ''
  try {
    const params = paymentFilterParams()
    params.set('page', String(paymentPage.value))
    params.set('page_size', String(paymentPageSize.value))
    const response = await getJSON<PaymentListResponse>(`/api/admin/payments?${params.toString()}`)
    paymentRecords.value = response.items ?? []
    paymentPage.value = response.page
    paymentPageSize.value = response.page_size
    paymentTotal.value = response.total
    paymentTotalPages.value = response.total_pages
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    const detail = error instanceof Error ? error.message : '未知错误'
    paymentRecordsMessage.value = `付款记录加载失败：${detail}`
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
  // clearColumnFilters: the shared helper is aliased because this file already
  // has its own clearAllFilters for the user-facing query pages.
  clearColumnFilters(paymentFilterState.value)
  applyPaymentFilters()
}

// --- Payment proof submissions: regular-user side ---

async function loadUserSubmissions() {
  userSubmissionsLoading.value = true
  userSubmissionsMessage.value = ''
  try {
    const response = await listUserPaymentSubmissions()
    userSubmissions.value = response.items ?? []
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) return
    userSubmissionsMessage.value = error instanceof Error ? `收肾记录加载失败：${error.message}` : '收肾记录加载失败'
  } finally {
    userSubmissionsLoading.value = false
  }
}

function clearSubmissionSelection() {
  if (submissionPreviewURL.value) URL.revokeObjectURL(submissionPreviewURL.value)
  submissionFile.value = null
  submissionPreviewURL.value = ''
}

function onSubmissionFileChange(event: Event) {
  submissionUploadMessage.value = ''
  const input = event.target as HTMLInputElement
  const file = input.files?.[0] ?? null
  if (!file) {
    clearSubmissionSelection()
    return
  }
  if (!submissionAcceptedTypes.includes(file.type)) {
    submissionUploadMessage.value = '仅支持 PNG、JPEG 或 WebP 图片。'
    input.value = ''
    clearSubmissionSelection()
    return
  }
  if (file.size > submissionMaxBytes) {
    submissionUploadMessage.value = '图片超过 10 MiB 大小限制。'
    input.value = ''
    clearSubmissionSelection()
    return
  }
  if (submissionPreviewURL.value) URL.revokeObjectURL(submissionPreviewURL.value)
  submissionFile.value = file
  submissionPreviewURL.value = URL.createObjectURL(file)
}

async function submitUserProof() {
  const file = submissionFile.value
  if (!file || submissionUploading.value) return
  submissionUploading.value = true
  submissionUploadMessage.value = ''
  try {
    const form = new FormData()
    form.append('file', file)
    // The method comes from the user's current payment-center selection; the CN,
    // user id and amounts are all resolved server-side from the session.
    form.append('payment_method', queryQRMethod.value)
    const response = await submitPaymentSubmission(form)
    clearSubmissionSelection()
    const input = document.getElementById('submission-file-input') as HTMLInputElement | null
    if (input) input.value = ''
    submissionUploadMessage.value = `已交肾（待管理员核对）。本次应付 ${formatMoney(response.submission.payable_amount)}。`
    await loadUserSubmissions()
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      submissionUploadMessage.value = '登录已过期，请重新登录后再提交。'
      return
    }
    submissionUploadMessage.value = error instanceof Error ? error.message : '提交失败，请重试。'
  } finally {
    submissionUploading.value = false
  }
}

function submissionStatusLabel(status: string) {
  if (status === 'submitted') return '已交肾（待管理员核对）'
  if (status === 'approved') return '已核对通过'
  if (status === 'rejected') return '已驳回'
  return status
}

function submissionAdminStatusLabel(status: string) {
  if (status === 'submitted') return '待核对'
  if (status === 'approved') return '已通过'
  if (status === 'rejected') return '已驳回'
  return status
}

// --- Payment proof submissions: admin side ---

function submissionFilterParams() {
  return buildFilterParams(submissionFilterState.value, SUBMISSION_RANGE_PARAMS, SUBMISSION_DATE_PARAMS)
}

async function loadSubmissionFacets(request: { column: string; search: string; page: number }): Promise<ColumnFacetResponse> {
  const params = submissionFilterParams()
  params.set('column', request.column)
  if (request.search) params.set('search', request.search)
  params.set('facet_page', String(request.page))
  const response: PaymentSubmissionFacetResponse = await getAdminPaymentSubmissionFacets(params.toString())
  return {
    column: response.column,
    values: response.values ?? [],
    total: response.total,
    blank_count: response.blank_count,
    page: response.facet_page,
    page_size: response.facet_page_size,
    has_more: response.has_more,
  }
}

function applySubmissionFilters() {
  submissionPage.value = 1
  void loadPaymentSubmissions()
}

function goToSubmissionPage(page: number) {
  if (page < 1 || (submissionTotalPages.value > 0 && page > submissionTotalPages.value)) return
  submissionPage.value = page
  void loadPaymentSubmissions()
}

function changeSubmissionPageSize() {
  submissionPage.value = 1
  void loadPaymentSubmissions()
}

function resetSubmissionFilters() {
  clearColumnFilters(submissionFilterState.value)
  applySubmissionFilters()
}

async function loadPaymentSubmissions() {
  paymentSubmissionsLoading.value = true
  paymentSubmissionsMessage.value = ''
  try {
    const params = submissionFilterParams()
    params.set('page', String(submissionPage.value))
    params.set('page_size', String(submissionPageSize.value))
    const response = await listAdminPaymentSubmissions(params.toString())
    paymentSubmissions.value = response.items ?? []
    submissionPage.value = response.page
    submissionPageSize.value = response.page_size
    submissionTotal.value = response.total
    submissionTotalPages.value = response.total_pages
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    paymentSubmissionsMessage.value = error instanceof Error ? `收肾记录加载失败：${error.message}` : '收肾记录加载失败'
  } finally {
    paymentSubmissionsLoading.value = false
  }
}

async function loadSubmissionDetail(id: string) {
  submissionDetailLoading.value = true
  submissionDetailMessage.value = ''
  submissionActionMessage.value = ''
  submissionRejectReason.value = ''
  submissionApproveNote.value = ''
  submissionDetail.value = null
  cnPayment.value = null
  try {
    submissionDetail.value = await getAdminPaymentSubmissionDetail(id)
    submissionImageReloadKey.value += 1
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    submissionDetailMessage.value = error instanceof Error ? error.message : '收肾记录详情加载失败'
  } finally {
    submissionDetailLoading.value = false
  }
}

// adminSubmissionImageURL builds the admin image URL with a cache-busting query
// (the sha, when present, otherwise the reload counter) so a fresh open reloads.
function adminSubmissionImageURL(id: string) {
  const version = submissionDetail.value?.submission.sha256 || String(submissionImageReloadKey.value)
  return apiUrl(`/api/admin/payment-submissions/${encodeURIComponent(id)}/image`) + `?v=${encodeURIComponent(version)}`
}

async function rejectSubmission() {
  if (!submissionDetail.value || submissionRejecting.value) return
  const id = submissionDetail.value.submission.id
  const reason = submissionRejectReason.value.trim()
  if (!reason) {
    submissionActionMessage.value = '请填写驳回原因。'
    return
  }
  submissionRejecting.value = true
  submissionActionMessage.value = ''
  try {
    submissionDetail.value = await rejectPaymentSubmission(id, reason)
    submissionActionMessage.value = '已驳回该收肾记录。'
    await loadPaymentSubmissions()
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    submissionActionMessage.value = error instanceof Error ? error.message : '驳回失败，请重试。'
  } finally {
    submissionRejecting.value = false
  }
}

// Select-all over the loaded unpaid items. Deliberately NOT a new allocation
// path: both directions walk the rows and call the same per-row
// setPaymentItemSelected the checkboxes use, so a "select all" is exactly N
// manual ticks — same default amounts, same over-payment validation, nothing
// bypassed. Rows with no remaining balance are skipped, as their checkbox is
// disabled in manual use too.
const selectableCnPaymentItems = computed(() => (cnPayment.value?.items ?? []).filter((item) => item.remaining_amount > 0))
const allCnPaymentItemsSelected = computed(
  () => selectableCnPaymentItems.value.length > 0 && selectableCnPaymentItems.value.every((item) => selectedPaymentItemIds.value.has(item.id)),
)

function selectAllCnPaymentItems() {
  for (const item of selectableCnPaymentItems.value) {
    if (!selectedPaymentItemIds.value.has(item.id)) setPaymentItemSelected(item, true)
  }
}

function clearAllCnPaymentItemSelection() {
  for (const item of selectableCnPaymentItems.value) {
    if (selectedPaymentItemIds.value.has(item.id)) setPaymentItemSelected(item, false)
  }
}

// loadSubmissionAllocation opens the record-payment allocation for this proof's
// CN. It reuses the exact same unpaid-item table, selection and amount inputs as
// 录入付款 — the admin picks明细 and applied_amount there, then confirms below.
async function loadSubmissionAllocation() {
  if (!submissionDetail.value) return
  paymentEntryCN.value = submissionDetail.value.submission.cn_code
  submissionActionMessage.value = ''
  await loadCNPayment(true)
}

async function approveSubmission() {
  if (!submissionDetail.value || submissionApproving.value) return
  if (!cnPayment.value) {
    submissionActionMessage.value = '请先加载该 CN 的未付明细。'
    return
  }
  const sub = submissionDetail.value.submission
  if (selectedPaymentItems.value.length === 0) {
    submissionActionMessage.value = '请先勾选本次付款对应的未付明细。'
    return
  }
  const invalidItem = selectedPaymentItems.value.find(paymentAmountInvalid)
  if (invalidItem) {
    submissionActionMessage.value = '本次分摊金额必须大于 0，且不能超过剩余应付金额。'
    return
  }
  const confirmLines = [
    '确认核对通过？通过后会按下方分配创建一条正式付款，并计入有效已付金额。',
    `CN：${sub.cn_code}`,
    `付款方式：${paymentMethodLabel(sub.payment_method)}`,
    `关联明细数量：${selectedPaymentItems.value.length}`,
    '',
    ...selectedPaymentItems.value.map((item) => `${item.order_no} / ${item.display_name || item.product_name}：${formatMoney(paymentAmountValue(item.id))}`),
  ]
  if (!window.confirm(confirmLines.join('\n'))) return
  submissionApproving.value = true
  submissionActionMessage.value = ''
  try {
    submissionDetail.value = await approvePaymentSubmission(sub.id, {
      items: selectedPaymentItems.value.map((item) => ({ order_item_id: item.id, amount: paymentAmountValue(item.id) })),
      note: submissionApproveNote.value.trim(),
    })
    cnPayment.value = null
    submissionActionMessage.value = '已核对通过并创建正式付款。'
    await loadPaymentSubmissions()
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    submissionActionMessage.value = error instanceof Error ? error.message : '核对通过失败，请重试。'
  } finally {
    submissionApproving.value = false
  }
}

async function loadPaymentQRStatuses() {
  paymentQRLoading.value = true
  paymentQRMessage.value = ''
  try {
    const response = await getPaymentQRAdminStatuses()
    paymentQRStatuses.value = response.items
  } catch (error) {
    paymentQRMessage.value = error instanceof Error ? error.message : '收款二维码状态加载失败'
  } finally {
    paymentQRLoading.value = false
  }
}

// adminQRImageURL builds the admin preview URL for a method's current image,
// with a cache-busting query so a replaced image reloads. It uses the content
// hash when available, otherwise the reload counter.
function adminQRImageURL(method: PaymentQRMethod) {
  const status = paymentQRStatusByMethod.value[method]
  const version = status.sha256 || String(paymentQRReloadKey.value)
  return apiUrl(`/api/admin/payment-qr/${method}/image`) + `?v=${encodeURIComponent(version)}`
}

function clearQRSelection(method: PaymentQRMethod) {
  const existing = qrPreviewURL.value[method]
  if (existing) URL.revokeObjectURL(existing)
  qrSelectedFile.value = { ...qrSelectedFile.value, [method]: null }
  qrPreviewURL.value = { ...qrPreviewURL.value, [method]: '' }
}

function onQRFileChange(method: PaymentQRMethod, event: Event) {
  paymentQRMessage.value = ''
  const input = event.target as HTMLInputElement
  const file = input.files?.[0] ?? null
  if (!file) {
    clearQRSelection(method)
    return
  }
  if (!qrAcceptedTypes.includes(file.type)) {
    paymentQRMessage.value = '仅支持 PNG、JPEG 或 WebP 图片。'
    input.value = ''
    clearQRSelection(method)
    return
  }
  if (file.size > qrMaxBytes) {
    paymentQRMessage.value = '图片超过 5 MiB 大小限制。'
    input.value = ''
    clearQRSelection(method)
    return
  }
  const previous = qrPreviewURL.value[method]
  if (previous) URL.revokeObjectURL(previous)
  qrSelectedFile.value = { ...qrSelectedFile.value, [method]: file }
  qrPreviewURL.value = { ...qrPreviewURL.value, [method]: URL.createObjectURL(file) }
}

async function uploadPaymentQRImage(method: PaymentQRMethod) {
  const file = qrSelectedFile.value[method]
  if (!file || qrUploading.value[method]) return
  const label = paymentMethodLabel(method)
  if (paymentQRStatusByMethod.value[method].configured) {
    if (!window.confirm(`确认用新图片替换当前生效的${label}收款二维码？旧二维码会被停用并保留为历史。`)) return
  }
  qrUploading.value = { ...qrUploading.value, [method]: true }
  paymentQRMessage.value = ''
  try {
    const form = new FormData()
    form.append('file', file)
    const response = await uploadPaymentQR(method, form)
    paymentQRStatuses.value = response.items
    clearQRSelection(method)
    const input = document.getElementById(`qr-file-${method}`) as HTMLInputElement | null
    if (input) input.value = ''
    paymentQRReloadKey.value += 1
    paymentQRMessage.value = `${label}收款二维码已更新。`
  } catch (error) {
    paymentQRMessage.value = error instanceof Error ? error.message : '上传失败'
  } finally {
    qrUploading.value = { ...qrUploading.value, [method]: false }
  }
}

async function disablePaymentQRImage(method: PaymentQRMethod) {
  if (qrDisabling.value[method]) return
  const label = paymentMethodLabel(method)
  if (!window.confirm(`确认停用当前生效的${label}收款二维码？停用后普通用户将无法查看该二维码，历史记录仍会保留。`)) return
  qrDisabling.value = { ...qrDisabling.value, [method]: true }
  paymentQRMessage.value = ''
  try {
    const response = await disablePaymentQR(method)
    paymentQRStatuses.value = response.items
    paymentQRReloadKey.value += 1
    paymentQRMessage.value = `${label}收款二维码已停用。`
  } catch (error) {
    paymentQRMessage.value = error instanceof Error ? error.message : '停用失败'
  } finally {
    qrDisabling.value = { ...qrDisabling.value, [method]: false }
  }
}

// The one place the user filter state becomes API parameters. The list, the
// facet popovers and the export all go through it, which is what keeps the
// three from ever disagreeing about what is filtered.
function adminUserFilterParams() {
  return buildFilterParams(adminUserFilterState.value, ADMIN_USER_RANGE_PARAMS, ADMIN_USER_DATE_PARAMS)
}

const adminUserActiveFilterCount = computed(() => activeFilterCount(adminUserFilterState.value))

// Facet loader handed to every value popover. The backend drops the requested
// column's own selection when computing candidates, and names its pager
// facet_page/facet_page_size — adapted here into the shape the popover reads.
async function loadAdminUserFacets(request: { column: string; search: string; page: number }): Promise<ColumnFacetResponse> {
  const params = adminUserFilterParams()
  params.set('column', request.column)
  if (request.search) params.set('search', request.search)
  params.set('facet_page', String(request.page))
  const response = await getJSON<AdminUserFacetResponse>(`/api/admin/users/facets?${params.toString()}`)
  return {
    column: response.column,
    values: response.values ?? [],
    total: response.total,
    blank_count: response.blank_count,
    page: response.facet_page,
    page_size: response.facet_page_size,
    has_more: response.has_more,
  }
}

// Applying a filter returns to page 1: staying on page 7 of a result set that
// just shrank to two pages would show an empty table.
function applyAdminUserFilters() {
  adminUserPage.value = 1
  void loadAdminUsers()
}

function goToAdminUserPage(page: number) {
  if (page < 1 || (adminUserTotalPages.value > 0 && page > adminUserTotalPages.value)) return
  adminUserPage.value = page
  void loadAdminUsers()
}

function changeAdminUserPageSize() {
  adminUserPage.value = 1
  void loadAdminUsers()
}

async function loadAdminUsers() {
  adminUsersLoading.value = true
  adminUsersMessage.value = ''
  try {
    const params = adminUserFilterParams()
    params.set('page', String(adminUserPage.value))
    params.set('page_size', String(adminUserPageSize.value))
    const response = await getJSON<AdminUserListResponse>(`/api/admin/users?${params.toString()}`)
    adminUsers.value = response.items ?? []
    adminUsersSummary.value = response.summary ?? null
    adminUserPage.value = response.page
    adminUserPageSize.value = response.page_size
    adminUserTotal.value = response.total
    adminUserTotalPages.value = response.total_pages
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    adminUsersSummary.value = null
    const detail = error instanceof Error ? error.message : '未知错误'
    adminUsersMessage.value = `用户列表加载失败：${detail}`
  } finally {
    adminUsersLoading.value = false
  }
}

function resetAdminUserFilters() {
  // clearColumnFilters: the shared helper is aliased because this file already
  // has its own clearAllFilters for the user-facing query pages.
  clearColumnFilters(adminUserFilterState.value)
  applyAdminUserFilters()
}

// Accepts URLSearchParams so a caller can pass repeated multi-value filter
// parameters, which a Record<string, string> cannot express.
async function downloadExport(path: string, params: Record<string, string> | URLSearchParams) {
  let searchParams: URLSearchParams
  if (params instanceof URLSearchParams) {
    searchParams = params
  } else {
    searchParams = new URLSearchParams()
    for (const [key, value] of Object.entries(params)) {
      if (value.trim()) searchParams.set(key, value.trim())
    }
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

// Exports carry the complete filter state and no pagination, so a download is
// the whole filtered result set rather than the page on screen.
function exportAdminUsersExcel() {
  void downloadExport('/api/admin/export/users.xlsx', adminUserFilterParams())
}

function exportAdminUsersCSV() {
  void downloadExport('/api/admin/export/users.csv', adminUserFilterParams())
}

// Exports carry the complete filter state and no pagination, so a download is
// the whole filtered result set rather than the page on screen. The old version
// passed only cn/method/status/paid dates and silently dropped the amount
// ranges, so an amount-filtered table exported more rows than it showed.
function exportPaymentsExcel() {
  void downloadExport('/api/admin/export/payments.xlsx', paymentFilterParams())
}

function exportPaymentsCSV() {
  void downloadExport('/api/admin/export/payments.csv', paymentFilterParams())
}

function exportUnpaidItemsExcel() {
  void downloadExport('/api/admin/export/order-items.xlsx', { unpaid_only: '1' })
}

function exportUnpaidItemsCSV() {
  void downloadExport('/api/admin/export/order-items.csv', { unpaid_only: '1' })
}

// Exports carry the complete filter state and no pagination, so a download is
// the whole filtered result set rather than the page on screen.
function exportOrderItemsExcel() {
  void downloadExport('/api/admin/export/order-items.xlsx', orderFilterParams())
}

function exportOrderItemsCSV() {
  void downloadExport('/api/admin/export/order-items.csv', orderFilterParams())
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
  adminRecoveryEmail.value = null
  adminRecoveryEmailDraft.value = ''
  adminRecoveryEmailReason.value = ''
  adminRecoveryEmailMessage.value = ''
  try {
    adminUserDetail.value = await getJSON<AdminUserDetailResponse>('/api/admin/users/' + encodeURIComponent(id))
    await loadAdminRecoveryEmail(id)
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

async function loadAdminRecoveryEmail(userID: string) {
  adminRecoveryEmailLoading.value = true
  adminRecoveryEmailMessage.value = ''
  try {
    adminRecoveryEmail.value = await getAdminRecoveryEmail(userID)
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    adminRecoveryEmail.value = null
    adminRecoveryEmailMessage.value = error instanceof Error ? error.message : '找回邮箱状态加载失败'
  } finally {
    adminRecoveryEmailLoading.value = false
  }
}

async function saveAdminRecoveryEmail() {
  if (!adminUserDetail.value || adminRecoveryEmailSaving.value || adminUserDetail.value.user.status === 'merged') return
  const email = adminRecoveryEmailDraft.value.trim()
  const reason = adminRecoveryEmailReason.value.trim()
  if (!email) {
    adminRecoveryEmailMessage.value = '请输入新的找回邮箱。'
    return
  }
  if (!reason) {
    adminRecoveryEmailMessage.value = '请填写操作原因。'
    return
  }
  const replacing = adminRecoveryEmail.value?.has_recovery_email === true
  const maskedCurrent = adminRecoveryEmail.value?.masked_email || '当前邮箱'
  const confirmation = replacing
    ? `确认替换 ${maskedCurrent}？旧记录将立即失效，新邮箱状态为待验证。`
    : '确认登记新的找回邮箱？保存后页面只显示脱敏邮箱，状态为待验证。'
  if (!window.confirm(confirmation)) return

  adminRecoveryEmailSaving.value = true
  adminRecoveryEmailMessage.value = ''
  try {
    adminRecoveryEmail.value = await putAdminRecoveryEmail(adminUserDetail.value.user.id, email, reason)
    adminRecoveryEmailDraft.value = ''
    adminRecoveryEmailReason.value = ''
    adminRecoveryEmailMessage.value = adminRecoveryEmail.value.message || (replacing ? '找回邮箱已替换。' : '找回邮箱已登记。')
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    adminRecoveryEmailMessage.value = error instanceof Error ? error.message : '找回邮箱保存失败'
  } finally {
    adminRecoveryEmailSaving.value = false
  }
}

async function unbindAdminRecoveryEmail() {
  if (!adminUserDetail.value || !adminRecoveryEmail.value?.has_recovery_email || adminRecoveryEmailSaving.value || adminUserDetail.value.user.status === 'merged') return
  const reason = adminRecoveryEmailReason.value.trim()
  if (!reason) {
    adminRecoveryEmailMessage.value = '请填写解绑原因。'
    return
  }
  const maskedCurrent = adminRecoveryEmail.value.masked_email || '当前脱敏邮箱'
  if (!window.confirm(`确认解绑 ${maskedCurrent}？历史记录会保留用于安全审计，但该邮箱不再是当前找回邮箱。`)) return

  adminRecoveryEmailSaving.value = true
  adminRecoveryEmailMessage.value = ''
  try {
    adminRecoveryEmail.value = await deleteAdminRecoveryEmail(adminUserDetail.value.user.id, reason)
    adminRecoveryEmailDraft.value = ''
    adminRecoveryEmailReason.value = ''
    adminRecoveryEmailMessage.value = adminRecoveryEmail.value.message || '找回邮箱已解绑。'
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    adminRecoveryEmailMessage.value = error instanceof Error ? error.message : '找回邮箱解绑失败'
  } finally {
    adminRecoveryEmailSaving.value = false
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
  clearColumnFilters(orderFilterState.value)
  applyOrderFilters()
}


async function loginQuery() {
  queryLoading.value = true
  queryOrdersError.value = ''
  queryMessage.value = ''
  try {
    const response = await postJSON<QueryLoginResponse>('/api/query/login', {
      cn: queryCN.value,
      query_code: queryCode.value,
    })
    resetQueryRecoveryVerification()
    resetAnonymousQueryRecovery()
    queryView.value = 'login'
    queryUser.value = response.user
    queryCode.value = ''
    await loadQueryOrders(true)
    // Enter the user module portal (or the originally requested module page).
    const target = pendingQueryTarget.value || '/query'
    pendingQueryTarget.value = ''
    navigate(target)
  } catch (error) {
    queryOrders.value = null
    queryUser.value = null
    queryOrdersError.value = ''
    queryMessage.value = error instanceof Error ? error.message : '查询登录失败'
  } finally {
    queryLoading.value = false
  }
}

async function loadQueryOrders(showMessage = true) {
  queryLoading.value = true
  queryOrdersError.value = ''
  if (showMessage) queryMessage.value = ''
  try {
    const response = await getJSON<QueryOrdersResponse>('/api/query/orders')
    queryOrders.value = response
    queryUser.value = response.user
    queryCN.value = response.user.cn_code
    await loadQueryQRAvailability()
    await loadQueryRecoveryEmail()
  } catch (error) {
    queryOrders.value = null
    if (error instanceof ApiError && error.status === 401) {
      queryUser.value = null
    } else {
      queryOrdersError.value = '付款历史加载失败，请稍后重试。'
    }
    if (showMessage || !(error instanceof ApiError && error.status === 401)) {
      queryMessage.value = error instanceof ApiError && error.status === 401
        ? error.message
        : '订单与付款信息加载失败，请稍后重试。'
    }
  } finally {
    queryLoading.value = false
  }
}

async function loadQueryQRAvailability() {
  queryQRLoading.value = true
  queryQRError.value = ''
  try {
    const response = await getPaymentQRAvailability()
    queryQRAvailability.value = response.items
    // Both 支付宝 and 微信 are always offered as payment methods; QR
    // configuration only affects whether an image or an empty state shows.
    // Default to 支付宝 so an unselected page never implies a WeChat fee.
    if (!queryQRMethod.value) queryQRMethod.value = 'alipay'
  } catch (error) {
    queryQRAvailability.value = []
    queryQRMethod.value = 'alipay'
    if (error instanceof ApiError && error.status === 401) {
      queryUser.value = null
      queryOrders.value = null
      queryMessage.value = error.message
      return
    }
    queryQRError.value = '收款二维码信息加载失败，请稍后重试。'
  } finally {
    queryQRLoading.value = false
  }
}

function selectQueryQRMethod(method: PaymentQRMethod) {
  // Both methods are always selectable; QR availability only changes the QR
  // display, not whether the user may choose the method.
  queryQRMethod.value = method
  queryQRError.value = ''
}

// Whether a method's QR image is currently configured/enabled.
function queryMethodAvailable(method: PaymentQRMethod): boolean {
  return queryQRAvailability.value.some((item) => item.payment_method === method && item.available)
}

// queryQRImageURL builds the regular-user image URL for the selected method.
// The reload counter lets a manual refresh re-fetch; no technical identifier
// (sha, size, admin) is ever placed in the URL or shown to the user.
function queryQRImageURL(method: PaymentQRMethod) {
  return apiUrl(`/api/query/payment-qr/${method}/image`) + `?v=${queryQRReloadKey.value}`
}

function onQueryQRImageError() {
  queryQRError.value = '二维码加载失败，请刷新页面或重新登录后再试。'
}

function openQueryQRZoom() {
  if (queryQRMethod.value) queryQRZoom.value = true
}

function closeQueryQRZoom() {
  queryQRZoom.value = false
}

async function loadQueryRecoveryEmail() {
  queryRecoveryEmailLoading.value = true
  queryRecoveryEmailMessage.value = ''
  try {
    queryRecoveryEmail.value = await getQueryRecoveryEmail()
    if (queryRecoveryEmail.value.status !== 'pending') resetQueryRecoveryVerification()
  } catch (error) {
    queryRecoveryEmail.value = null
    if (error instanceof ApiError && error.status === 401) {
      queryUser.value = null
      queryOrders.value = null
      queryOrdersError.value = ''
      queryMessage.value = error.message
      return
    }
    queryRecoveryEmailMessage.value = error instanceof Error ? error.message : '找回邮箱状态加载失败'
  } finally {
    queryRecoveryEmailLoading.value = false
  }
}
function resetQueryRecoveryVerification() {
  queryRecoveryVerificationCode.value = ''
  queryRecoveryCooldownUntil.value = 0
  queryRecoveryExpiresAt.value = ''
  queryRecoverySending.value = false
  queryRecoveryVerifying.value = false
}

function clearExpiredQuerySession(error: unknown) {
  if (!(error instanceof ApiError) || error.status !== 401) return false
  resetQueryRecoveryVerification()
  queryUser.value = null
  queryOrders.value = null
  queryOrdersError.value = ''
  queryRecoveryEmail.value = null
  queryRecoveryEmailMessage.value = ''
  queryMessage.value = error.message
  return true
}

async function sendQueryRecoveryVerification() {
  if (!queryRecoveryCanSend.value) return
  queryRecoverySending.value = true
  queryRecoveryEmailMessage.value = ''
  try {
    const response = await sendRecoveryEmailVerification()
    queryRecoveryCooldownUntil.value = Date.now() + (response.retry_after_seconds ?? 60) * 1000
    queryRecoveryExpiresAt.value = response.expires_at ?? ''
    queryRecoveryEmail.value = {
      ...(queryRecoveryEmail.value ?? { has_recovery_email: true }),
      has_recovery_email: true,
      status: 'pending',
      masked_email: response.masked_email ?? queryRecoveryEmail.value?.masked_email,
    }
    queryRecoveryEmailMessage.value = response.message
  } catch (error) {
    if (clearExpiredQuerySession(error)) return
    if (error instanceof ApiError && error.status === 429 && error.retryAfterSeconds > 0) {
      queryRecoveryCooldownUntil.value = Date.now() + error.retryAfterSeconds * 1000
      queryRecoveryEmailMessage.value = `${error.message} 请等待 ${error.retryAfterSeconds} 秒。`
    } else {
      queryRecoveryEmailMessage.value = error instanceof Error ? error.message : '验证码发送失败'
    }
  } finally {
    queryRecoverySending.value = false
  }
}

async function verifyQueryRecoveryEmail() {
  if (queryRecoveryVerifying.value) return
  const code = queryRecoveryVerificationCode.value.trim()
  if (!/^\d{6}$/.test(code)) {
    queryRecoveryEmailMessage.value = '请输入 6 位数字验证码。'
    return
  }
  queryRecoveryVerifying.value = true
  queryRecoveryEmailMessage.value = ''
  try {
    const response = await verifyRecoveryEmail(code)
    queryRecoveryVerificationCode.value = ''
    queryRecoveryCooldownUntil.value = 0
    queryRecoveryExpiresAt.value = ''
    queryRecoveryEmail.value = {
      ...(queryRecoveryEmail.value ?? { has_recovery_email: true }),
      has_recovery_email: true,
      status: 'verified',
      masked_email: response.masked_email ?? queryRecoveryEmail.value?.masked_email,
      verified_at: response.verified_at,
    }
    queryRecoveryEmailMessage.value = response.message
  } catch (error) {
    if (clearExpiredQuerySession(error)) return
    queryRecoveryEmailMessage.value = error instanceof Error ? error.message : '找回邮箱验证失败'
  } finally {
    queryRecoveryVerifying.value = false
  }
}
async function logoutQuery() {
  resetQueryRecoveryVerification()
  queryLoading.value = true
  queryMessage.value = ''
  try {
    await postJSON<void>('/api/query/logout', {})
    queryUser.value = null
    queryOrders.value = null
    queryOrdersError.value = ''
    queryCode.value = ''
    queryOldCode.value = ''
    queryNewCode.value = ''
    queryConfirmCode.value = ''
    querySecurityMessage.value = ''
    queryRecoveryEmail.value = null
    queryRecoveryEmailMessage.value = ''
    queryQRAvailability.value = []
    queryQRMethod.value = ''
    queryQRError.value = ''
    queryQRZoom.value = false
    pendingQueryTarget.value = ''
    queryMessage.value = '已退出查询。'
    navigate('/query')
  } catch (error) {
    queryMessage.value = error instanceof Error ? error.message : '退出失败'
  } finally {
    queryLoading.value = false
  }
}

function resetAnonymousQueryRecovery(clearCN = true) {
  anonymousRecoveryStep.value = 'request'
  if (clearCN) anonymousRecoveryCN.value = ''
  anonymousRecoveryCode.value = ''
  anonymousRecoveryResetToken.value = ''
  anonymousRecoveryTokenExpiresAt.value = ''
  anonymousRecoveryNewCode.value = ''
  anonymousRecoveryConfirmCode.value = ''
  anonymousRecoveryLoading.value = false
  anonymousRecoveryMessage.value = ''
}

function openRecoveryView() {
  if (!config.value.emailDeliveryEnabled) {
    queryMessage.value = '邮箱找回功能暂不可用。'
    return
  }
  resetAnonymousQueryRecovery()
  anonymousRecoveryCN.value = queryCN.value.trim()
  queryView.value = 'recovery'
  queryMessage.value = ''
}

function closeRecoveryView() {
  resetAnonymousQueryRecovery()
  queryView.value = 'login'
}

async function requestAnonymousQueryRecovery() {
  if (!config.value.emailDeliveryEnabled) {
    anonymousRecoveryMessage.value = '邮箱找回功能暂不可用。'
    return
  }
  if (anonymousRecoveryLoading.value) return
  const cn = anonymousRecoveryCN.value.trim()
  if (!cn) {
    anonymousRecoveryMessage.value = '请输入 CN。'
    return
  }
  anonymousRecoveryLoading.value = true
  anonymousRecoveryMessage.value = ''
  try {
    const response = await requestQueryCodeRecovery(cn)
    anonymousRecoveryCN.value = cn
    anonymousRecoveryCode.value = ''
    anonymousRecoveryStep.value = 'verify'
    anonymousRecoveryMessage.value = response.message
  } catch (error) {
    anonymousRecoveryMessage.value = error instanceof Error ? error.message : '请求失败，请稍后再试'
  } finally {
    anonymousRecoveryLoading.value = false
  }
}

async function verifyAnonymousQueryRecovery() {
  if (anonymousRecoveryLoading.value) return
  const code = anonymousRecoveryCode.value.trim()
  if (!/^\d{6}$/.test(code)) {
    anonymousRecoveryMessage.value = '请输入 6 位数字验证码。'
    return
  }
  anonymousRecoveryLoading.value = true
  anonymousRecoveryMessage.value = ''
  try {
    const response = await verifyQueryCodeRecovery(anonymousRecoveryCN.value, code)
    anonymousRecoveryCode.value = ''
    anonymousRecoveryResetToken.value = response.reset_token
    anonymousRecoveryTokenExpiresAt.value = response.expires_at
    anonymousRecoveryStep.value = 'reset'
    anonymousRecoveryMessage.value = response.message
  } catch (error) {
    anonymousRecoveryMessage.value = error instanceof Error ? error.message : '验证码确认失败'
  } finally {
    anonymousRecoveryLoading.value = false
  }
}

async function submitRecoveredQueryCode() {
  if (anonymousRecoveryLoading.value) return
  const newCode = anonymousRecoveryNewCode.value.trim()
  const confirmCode = anonymousRecoveryConfirmCode.value.trim()
  if (!newCode || !confirmCode) {
    anonymousRecoveryMessage.value = '请完整输入新查询码和确认值。'
    return
  }
  if (newCode !== confirmCode) {
    anonymousRecoveryMessage.value = '两次输入的新查询码不一致。'
    return
  }
  if (!/^[A-Za-z0-9_@#.-]{6,32}$/.test(newCode)) {
    anonymousRecoveryMessage.value = '查询码需为 6-32 位，可使用字母、数字及 - _ @ # .。'
    return
  }
  anonymousRecoveryLoading.value = true
  anonymousRecoveryMessage.value = ''
  const retainedCN = anonymousRecoveryCN.value
  try {
    const response = await resetRecoveredQueryCode(anonymousRecoveryResetToken.value, newCode, confirmCode)
    resetAnonymousQueryRecovery()
    queryView.value = 'login'
    queryCN.value = retainedCN
    queryCode.value = ''
    queryMessage.value = response.message
  } catch (error) {
    anonymousRecoveryMessage.value = error instanceof Error ? error.message : '查询码重置失败'
  } finally {
    anonymousRecoveryLoading.value = false
  }
}
function openBindView() {
  resetAnonymousQueryRecovery()
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
    queryOrdersError.value = ''
    queryRecoveryEmail.value = null
    queryRecoveryEmailMessage.value = ''
    resetQueryRecoveryVerification()
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

// Alternating tint per order, so consecutive detail rows of the same order read
// as a group. This is presentation only: rows stay separate and independently
// filterable, which rowspan merging would destroy.
const orderRowTints = computed(() => {
  const tints: boolean[] = []
  let tinted = false
  let previousOrderID = ''
  for (const [index, item] of orderItems.value.entries()) {
    if (index > 0 && item.order_id !== previousOrderID) tinted = !tinted
    previousOrderID = item.order_id
    tints.push(tinted)
  }
  return tints
})

function isAlternateOrderRow(index: number) {
  return orderRowTints.value[index] ?? false
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

function recoveryEmailStatusLabel(status?: string) {
  if (status === 'pending') return '待验证'
  if (status === 'verified') return '已验证'
  if (status === 'disabled') return '已停用'
  return '未登记'
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
  applyRoute(window.location.pathname)
  void handleRouteChange()
})

onMounted(() => {
  queryRecoveryClockTimer = window.setInterval(() => {
    queryRecoveryClock.value = Date.now()
  }, 1000)
  void load()
  applyRoute(window.location.pathname)
  if (!isAdminSurface.value) authChecked.value = true
  void handleRouteChange()
})

onUnmounted(() => {
  if (queryRecoveryClockTimer !== undefined) window.clearInterval(queryRecoveryClockTimer)
  if (submissionPreviewURL.value) URL.revokeObjectURL(submissionPreviewURL.value)
})
</script>

<template>
  <div class="app-shell">
    <main v-if="isAdminRoute" class="workspace admin-surface">
      <PortalStatusBar
        :identity="admin ? (admin.display_name ?? admin.username) : undefined"
        :online="isBackendOnline"
        :online-text="isBackendOnline ? '后端在线' : '本地前端模式'"
        :show-refresh="true"
        back-label="← 返回谷子管理中心"
        @back="navigate('/admin')"
        @refresh="load"
        @logout="logout"
      />

      <section v-if="!admin" class="panel module-redirect">
        <p class="muted">正在前往管理员登录页…</p>
        <button class="primary-button" type="button" @click="navigate('/admin')">前往管理员登录</button>
      </section>

      <template v-else>
        <div class="module-header">
          <h2 class="module-header__title">{{ adminModuleTitle }}</h2>
          <nav v-if="adminModule === 'data'" class="module-subnav" aria-label="数据导入中心">
            <button :class="{ active: routeName === 'admin-import' }" type="button" @click="navigate('/admin/data/import')">Excel 导入</button>
            <button :class="{ active: routeName === 'admin-import-history' || routeName === 'admin-import-detail' }" type="button" @click="navigate('/admin/data/history')">导入历史</button>
          </nav>
          <nav v-else-if="adminModule === 'finance'" class="module-subnav" aria-label="收付款管理">
            <button :class="{ active: routeName === 'admin-payments' || routeName === 'admin-payment-detail' }" type="button" @click="navigate('/admin/finance/payments')">付款记录</button>
            <button :class="{ active: routeName === 'admin-submissions' || routeName === 'admin-submission-detail' }" type="button" @click="navigate('/admin/finance/submissions')">收肾记录</button>
            <button :class="{ active: routeName === 'admin-qr' }" type="button" @click="navigate('/admin/finance/qr-codes')">收款二维码</button>
          </nav>
        </div>

        <template v-if="routeName === 'admin-data'">
          <section class="module-portal">
            <div class="module-grid">
              <ModuleCard title="Excel 导入" description="上传并预览 Excel，确认后写入订单与付款" meta="含标准模板下载" accent="blue" cta="进入导入" @enter="navigate('/admin/data/import')" />
              <ModuleCard title="导入历史" description="查看历史导入批次、状态与详情" accent="neutral" cta="查看历史" @enter="navigate('/admin/data/history')" />
            </div>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-finance'">
          <section class="module-portal">
            <div class="module-grid">
              <ModuleCard title="付款记录" description="录入付款、查看付款流水与详情、撤销" accent="blue" cta="进入付款" @enter="navigate('/admin/finance/payments')" />
              <ModuleCard title="收肾记录" description="核对用户提交的付款凭证，通过后创建正式付款或驳回" accent="green" cta="进入核对" @enter="navigate('/admin/finance/submissions')" />
              <ModuleCard title="收款二维码" description="维护支付宝 / 微信静态收款二维码" accent="neutral" cta="进入二维码" @enter="navigate('/admin/finance/qr-codes')" />
            </div>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-import'">
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
            <!-- Row 1: title only. Export actions live on their own row below,
                 with the result scope after them. -->
            <div class="page-heading">
              <h2>付款记录</h2>
              <p>只读查看付款流水和关联明细。筛选入口在每列表头的漏斗图标中。</p>
              <p class="muted">本页不提供删除、作废或冲正。</p>
            </div>
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
                <div class="list-filter-bar" aria-label="未付明细筛选">
                  <span class="list-filter-context">当前 CN：<strong>{{ cnPayment.user.cn_code }}</strong></span>
                  <label><span>分类</span><select v-model="cnPaymentItemFilters.category"><option value="">全部</option><option v-for="opt in cnPaymentFilterOptions.categories" :key="opt" :value="opt">{{ opt }}</option></select></label>
                  <label><span>角色</span><select v-model="cnPaymentItemFilters.role"><option value="">全部</option><option v-for="opt in cnPaymentFilterOptions.roles" :key="opt" :value="opt">{{ opt }}</option></select></label>
                  <label><span>系列</span><select v-model="cnPaymentItemFilters.series"><option value="">全部</option><option v-for="opt in cnPaymentFilterOptions.series" :key="opt" :value="opt">{{ opt }}</option></select></label>
                  <button class="secondary-button" type="button" :disabled="!cnPaymentFiltersActive" @click="clearCnPaymentItemFilters">清空筛选</button>
                </div>
                <p class="muted list-filter-note">筛选只影响下方明细的显示，不影响已勾选项与合计；已勾选但被筛选隐藏的明细仍会计入本次付款。</p>
                <div class="table-scroll detail-table payment-table"><table><thead><tr><th>选择</th><th>订单号</th><th>项目名</th><th>谷子名称</th><th>角色</th><th>分类</th><th>原始应付</th><th>已付</th><th>剩余应付</th><th title="填写本次付款分摊到该明细的金额，不能超过剩余应付金额">本次分摊金额</th><th>状态</th><th>来源</th></tr></thead><tbody><tr v-if="cnPayment.items.length === 0"><td colspan="12">暂无待付款明细。</td></tr><tr v-else-if="filteredCnPaymentItems.length === 0"><td colspan="12">没有符合当前筛选条件的明细。</td></tr><tr v-for="item in filteredCnPaymentItems" :key="item.id"><td><input type="checkbox" :disabled="item.remaining_amount <= 0" :checked="selectedPaymentItemIds.has(item.id)" @change="setPaymentItemSelected(item, ($event.target as HTMLInputElement).checked)" /></td><td>{{ item.order_no }}</td><td><span class="cell-clip" :title="item.project_name">{{ item.project_name }}</span></td><td><span class="cell-clip" :title="item.display_name || item.product_name">{{ item.display_name || item.product_name }}</span></td><td>{{ item.character_name || '-' }}</td><td>{{ item.category || '-' }}</td><td>{{ formatMoney(item.amount) }}</td><td>{{ formatMoney(item.paid_amount) }}</td><td>{{ formatMoney(item.remaining_amount) }}</td><td><input class="amount-input" v-model="paymentAmounts[item.id]" :disabled="!selectedPaymentItemIds.has(item.id)" type="number" min="0.01" step="0.01" :max="item.remaining_amount" :class="{ invalid: paymentAmountInvalid(item) }" /></td><td>{{ queryPaymentStatusLabel(item.payment_status) }}</td><td>{{ item.import_filename || '-' }}</td></tr></tbody></table></div>
              </template>
            </section>
            <!-- Row 2: actions. -->
            <div class="page-actions">
              <button class="secondary-button" type="button" @click="exportPaymentsExcel">导出付款 Excel</button>
              <button class="secondary-button ghost-button" type="button" @click="exportPaymentsCSV">付款 CSV</button>
              <button class="secondary-button" type="button" @click="exportUnpaidItemsExcel">导出未付明细 Excel</button>
              <button class="secondary-button ghost-button" type="button" @click="exportUnpaidItemsCSV">未付 CSV</button>
              <button class="secondary-button" type="button" :disabled="paymentRecordsLoading" @click="loadPaymentRecords">{{ paymentRecordsLoading ? '加载中' : '刷新' }}</button>
            </div>

            <!-- Row 3: result scope and the single global reset. -->
            <div class="page-resultbar">
              <span class="filter-result-count">结果：共 {{ paymentTotal }} 条付款记录</span>
              <span class="muted">第 {{ paymentPage }} / {{ Math.max(paymentTotalPages, 1) }} 页</span>
              <label class="page-size-control">
                <span>每页</span>
                <select v-model.number="paymentPageSize" :disabled="paymentRecordsLoading" @change="changePaymentPageSize">
                  <option :value="25">25 条</option>
                  <option :value="50">50 条</option>
                  <option :value="100">100 条</option>
                  <option :value="200">200 条</option>
                </select>
              </label>
              <button class="secondary-button ghost-button" type="button" :disabled="paymentActiveFilterCount === 0" @click="resetPaymentFilters">清空全部筛选<template v-if="paymentActiveFilterCount > 0">（{{ paymentActiveFilterCount }}）</template></button>
            </div>

            <div v-if="paymentRecordsMessage" class="inline-alert">{{ paymentRecordsMessage }}</div>

            <div class="table-scroll history-table payment-records-table">
              <table>
                <thead>
                  <tr>
                    <th><ColumnValueFilter v-model="paymentFilterState.values.cn" label="CN" column="cn" :load-facets="loadPaymentFacets" @update:model-value="applyPaymentFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="paymentFilterState.ranges.principal" label="本金" @update:model-value="applyPaymentFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="paymentFilterState.ranges.fee" label="手续费" @update:model-value="applyPaymentFilters" /></th>
                    <th class="numeric-column col-emphasis"><ColumnRangeFilter v-model="paymentFilterState.ranges.payable" label="实付金额" @update:model-value="applyPaymentFilters" /></th>
                    <th><ColumnValueFilter v-model="paymentFilterState.values.payment_method" label="付款方式" column="payment_method" :load-facets="loadPaymentFacets" @update:model-value="applyPaymentFilters" /></th>
                    <th><ColumnValueFilter v-model="paymentFilterState.values.status" label="付款状态" column="status" :load-facets="loadPaymentFacets" @update:model-value="applyPaymentFilters" /></th>
                    <th><ColumnDateFilter v-model="paymentFilterState.dates.paid" label="付款时间" @update:model-value="applyPaymentFilters" /></th>
                    <th><ColumnValueFilter v-model="paymentFilterState.values.created_by" label="录入管理员" column="created_by" :load-facets="loadPaymentFacets" @update:model-value="applyPaymentFilters" /></th>
                    <th><ColumnDateFilter v-model="paymentFilterState.dates.voided" label="撤销时间" allow-blank blank-label="未撤销" @update:model-value="applyPaymentFilters" /></th>
                    <th><span class="column-header"><span class="column-header__label">查看详情</span></span></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="paymentRecordsLoading"><td colspan="10">加载中…</td></tr>
                  <tr v-else-if="paymentRecords.length === 0"><td colspan="10">没有符合当前筛选条件的付款记录。</td></tr>
                  <!-- payment.id keys the row and drives the detail link; it is
                       never rendered as a column. A voided payment keeps its
                       original amounts — only 付款状态 says it no longer counts.
                       备注 stays on the detail page rather than crowding the table. -->
                  <tr v-for="payment in paymentRecords" v-else :key="payment.id">
                    <td><span class="cell-wrap"><strong>{{ payment.cn_code }}</strong></span><small>{{ payment.display_name || '-' }}</small></td>
                    <td class="numeric-column">{{ formatMoney(payment.principal_amount ?? payment.amount) }}</td>
                    <td class="numeric-column">{{ formatMoney(payment.fee_amount) }}</td>
                    <td class="numeric-column col-emphasis">{{ formatMoney(payment.payable_amount ?? payment.total_amount) }}</td>
                    <td>{{ paymentMethodLabel(payment.payment_method || '') }}</td>
                    <td><span class="status-chip" :data-state="payment.status">{{ paymentStatusLabel(payment.status) }}</span></td>
                    <td>{{ formatDate(payment.paid_at) }}</td>
                    <td><span class="cell-wrap">{{ payment.created_by || '-' }}</span></td>
                    <td>{{ payment.voided_at ? formatDate(payment.voided_at) : '未撤销' }}</td>
                    <td><button class="secondary-button" type="button" @click="navigate('/admin/payments/' + payment.id)">详情</button></td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div v-if="paymentTotalPages > 1" class="pagination">
              <button class="secondary-button" type="button" :disabled="paymentRecordsLoading || paymentPage <= 1" @click="goToPaymentPage(paymentPage - 1)">上一页</button>
              <span class="muted">第 {{ paymentPage }} / {{ paymentTotalPages }} 页</span>
              <button class="secondary-button" type="button" :disabled="paymentRecordsLoading || paymentPage >= paymentTotalPages" @click="goToPaymentPage(paymentPage + 1)">下一页</button>
            </div>
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
                  <summary><span class="closed-label">▶ 技术标识</span><span class="open-label">▼ 技术标识</span></summary>
                  <p class="muted technical-note">仅供技术排查，日常对账与查询无需使用。</p>
                  <div class="technical-list"><article v-for="identifier in mergeTechnicalIdentifiers(paymentDetailTechnicalIdentifiers(paymentDetail))" :key="identifier.type + '-' + identifier.value" class="technical-item"><div class="technical-item__head"><span class="technical-item__type">{{ identifier.type }}</span><span class="technical-item__context">{{ identifier.context }}</span><button type="button" class="copy-button" @click="copyIdentifier(identifier.value)">复制</button></div><code class="technical-item__value">{{ identifier.value }}</code></article></div>
                </details>
              </section>
            </template>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-submissions'">
          <section class="panel">
            <div class="page-heading">
              <h2>收肾记录</h2>
              <p>核对用户提交的付款凭证。筛选入口在每列表头的漏斗图标中。</p>
              <p class="muted">提交本身不改变已付金额；只有核对通过并完成明细分配后才会创建正式付款。</p>
            </div>

            <div class="page-actions">
              <button class="secondary-button" type="button" :disabled="paymentSubmissionsLoading" @click="loadPaymentSubmissions">{{ paymentSubmissionsLoading ? '加载中' : '刷新' }}</button>
            </div>

            <div class="page-resultbar">
              <span class="filter-result-count">结果：共 {{ submissionTotal }} 条收肾记录</span>
              <span class="muted">第 {{ submissionPage }} / {{ Math.max(submissionTotalPages, 1) }} 页</span>
              <label class="page-size-control">
                <span>每页</span>
                <select v-model.number="submissionPageSize" :disabled="paymentSubmissionsLoading" @change="changeSubmissionPageSize">
                  <option :value="25">25 条</option>
                  <option :value="50">50 条</option>
                  <option :value="100">100 条</option>
                  <option :value="200">200 条</option>
                </select>
              </label>
              <button class="secondary-button ghost-button" type="button" :disabled="submissionActiveFilterCount === 0" @click="resetSubmissionFilters">清空全部筛选<template v-if="submissionActiveFilterCount > 0">（{{ submissionActiveFilterCount }}）</template></button>
            </div>

            <div v-if="paymentSubmissionsMessage" class="inline-alert">{{ paymentSubmissionsMessage }}</div>

            <div class="table-scroll history-table submission-records-table">
              <table>
                <thead>
                  <tr>
                    <th><ColumnValueFilter v-model="submissionFilterState.values.cn" label="CN" column="cn" :load-facets="loadSubmissionFacets" @update:model-value="applySubmissionFilters" /></th>
                    <th><ColumnValueFilter v-model="submissionFilterState.values.payment_method" label="付款方式" column="payment_method" :load-facets="loadSubmissionFacets" @update:model-value="applySubmissionFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="submissionFilterState.ranges.principal" label="本金" @update:model-value="applySubmissionFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="submissionFilterState.ranges.fee" label="手续费" @update:model-value="applySubmissionFilters" /></th>
                    <th class="numeric-column col-emphasis"><ColumnRangeFilter v-model="submissionFilterState.ranges.payable" label="本次应付" @update:model-value="applySubmissionFilters" /></th>
                    <th><ColumnValueFilter v-model="submissionFilterState.values.status" label="提交状态" column="status" :load-facets="loadSubmissionFacets" @update:model-value="applySubmissionFilters" /></th>
                    <th><ColumnDateFilter v-model="submissionFilterState.dates.submitted" label="提交时间" @update:model-value="applySubmissionFilters" /></th>
                    <th><ColumnDateFilter v-model="submissionFilterState.dates.reviewed" label="核对时间" allow-blank blank-label="未核对" @update:model-value="applySubmissionFilters" /></th>
                    <th><ColumnValueFilter v-model="submissionFilterState.values.reviewed_by" label="核对管理员" column="reviewed_by" :load-facets="loadSubmissionFacets" @update:model-value="applySubmissionFilters" /></th>
                    <th><span class="column-header"><span class="column-header__label">查看详情</span></span></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="paymentSubmissionsLoading"><td colspan="10">加载中…</td></tr>
                  <tr v-else-if="paymentSubmissions.length === 0"><td colspan="10">没有符合当前筛选条件的收肾记录。</td></tr>
                  <!-- submission.id keys the row and drives the detail link; it is
                       never rendered as a column. SHA / internal ids stay on the
                       detail page's collapsed 技术标识 section. -->
                  <tr v-for="submission in paymentSubmissions" v-else :key="submission.id">
                    <td><span class="cell-wrap"><strong>{{ submission.cn_code }}</strong></span><small>{{ submission.display_name || '-' }}</small></td>
                    <td>{{ paymentMethodLabel(submission.payment_method) }}</td>
                    <td class="numeric-column">{{ formatMoney(submission.principal_amount) }}</td>
                    <td class="numeric-column">{{ formatMoney(submission.fee_amount) }}</td>
                    <td class="numeric-column col-emphasis">{{ formatMoney(submission.payable_amount) }}</td>
                    <td><span class="status-chip" :data-state="submission.status">{{ submissionAdminStatusLabel(submission.status) }}</span></td>
                    <td>{{ formatDate(submission.submitted_at) }}</td>
                    <td>{{ submission.reviewed_at ? formatDate(submission.reviewed_at) : '未核对' }}</td>
                    <td><span class="cell-wrap">{{ submission.reviewed_by || '-' }}</span></td>
                    <td><button class="secondary-button" type="button" @click="navigate('/admin/finance/submissions/' + submission.id)">详情</button></td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div v-if="submissionTotalPages > 1" class="pagination">
              <button class="secondary-button" type="button" :disabled="paymentSubmissionsLoading || submissionPage <= 1" @click="goToSubmissionPage(submissionPage - 1)">上一页</button>
              <span class="muted">第 {{ submissionPage }} / {{ submissionTotalPages }} 页</span>
              <button class="secondary-button" type="button" :disabled="paymentSubmissionsLoading || submissionPage >= submissionTotalPages" @click="goToSubmissionPage(submissionPage + 1)">下一页</button>
            </div>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-submission-detail'">
          <section class="panel">
            <div class="panel__header"><div><h2>收肾记录详情</h2><p class="muted">核对用户提交的付款凭证</p></div><button class="secondary-button" type="button" @click="navigate('/admin/finance/submissions')">返回收肾记录</button></div>
            <div v-if="submissionDetailMessage" class="inline-alert">{{ submissionDetailMessage }}</div>
            <p v-if="submissionDetailLoading" class="muted">正在加载收肾记录详情。</p>
            <template v-if="submissionDetail">
              <div class="summary-grid">
                <article class="metric-tile"><span>CN</span><strong>{{ submissionDetail.submission.cn_code }}</strong></article>
                <article class="metric-tile"><span>付款方式</span><strong>{{ paymentMethodLabel(submissionDetail.submission.payment_method) }}</strong></article>
                <article class="metric-tile"><span>提交状态</span><strong>{{ submissionAdminStatusLabel(submissionDetail.submission.status) }}</strong></article>
                <article class="metric-tile"><span>本金</span><strong>{{ formatMoney(submissionDetail.submission.principal_amount) }}</strong></article>
                <article class="metric-tile"><span>手续费</span><strong>{{ formatMoney(submissionDetail.submission.fee_amount) }}</strong></article>
                <article class="metric-tile metric-tile--emphasis"><span>本次应付</span><strong>{{ formatMoney(submissionDetail.submission.payable_amount) }}</strong></article>
                <article class="metric-tile"><span>提交时间</span><strong>{{ formatDate(submissionDetail.submission.submitted_at) }}</strong></article>
                <article v-if="submissionDetail.submission.reviewed_at" class="metric-tile"><span>核对时间</span><strong>{{ formatDate(submissionDetail.submission.reviewed_at) }}</strong></article>
                <article v-if="submissionDetail.submission.reviewed_by" class="metric-tile"><span>核对管理员</span><strong>{{ submissionDetail.submission.reviewed_by }}</strong></article>
                <article v-if="submissionDetail.submission.reject_reason" class="metric-tile wide-metric"><span>驳回原因</span><strong>{{ submissionDetail.submission.reject_reason }}</strong></article>
              </div>

              <section class="panel nested-panel">
                <div class="panel__header"><h2>付款凭证图片</h2></div>
                <div class="submission-image-slot"><img :src="adminSubmissionImageURL(submissionDetail.submission.id)" alt="付款凭证" class="submission-image" /></div>
              </section>

              <div v-if="submissionActionMessage" class="inline-alert">{{ submissionActionMessage }}</div>

              <template v-if="submissionDetail.submission.status === 'submitted'">
                <section class="panel nested-panel danger-panel">
                  <div class="panel__header"><div><h2>驳回</h2><p class="muted">驳回后用户可重新提交新的凭证，旧记录会完整保留。</p></div></div>
                  <label class="reject-reason-field"><span>驳回原因（必填）</span><textarea v-model="submissionRejectReason" maxlength="500" rows="2" placeholder="请说明驳回原因，例如：图片不清晰 / 金额不符"></textarea></label>
                  <div class="page-actions"><button class="danger-button" type="button" :disabled="submissionRejecting || submissionRejectReason.trim() === ''" @click="rejectSubmission">{{ submissionRejecting ? '驳回中' : '驳回' }}</button></div>
                </section>

                <section class="panel nested-panel">
                  <div class="panel__header"><div><h2>核对通过并创建付款</h2><p class="muted">通过将复用现有付款录入流程：先加载该 CN 的未付明细，勾选并填写本次分摊金额，确认后创建一条正式付款并计入已付。</p></div><button class="secondary-button" type="button" :disabled="cnPaymentLoading" @click="loadSubmissionAllocation">{{ cnPaymentLoading ? '加载中' : '加载该 CN 未付明细' }}</button></div>
                  <p class="payment-allocation-hint">本次应付参考：{{ formatMoney(submissionDetail.submission.payable_amount) }}（本金 {{ formatMoney(submissionDetail.submission.principal_amount) }} + 手续费 {{ formatMoney(submissionDetail.submission.fee_amount) }}）。手续费由后端按实际分配金额与该付款方式规则重新计算。</p>
                  <div v-if="cnPaymentMessage" class="inline-alert">{{ cnPaymentMessage }}</div>
                  <template v-if="cnPayment">
                    <!-- 一键全选只是逐行调用与复选框相同的 setPaymentItemSelected，
                         默认分摊金额与手工逐条勾选完全一致，不引入新的分配算法。 -->
                    <div class="page-actions submission-select-actions">
                      <button class="secondary-button" type="button" :disabled="selectableCnPaymentItems.length === 0 || allCnPaymentItemsSelected" @click="selectAllCnPaymentItems">全选待付明细</button>
                      <button class="secondary-button ghost-button" type="button" :disabled="selectedPaymentItems.length === 0" @click="clearAllCnPaymentItemSelection">取消全选</button>
                      <span class="muted">已选 {{ selectedPaymentItems.length }} / {{ selectableCnPaymentItems.length }} 条</span>
                    </div>
                    <div class="table-scroll detail-table"><table><thead><tr><th>选择</th><th>订单号</th><th>谷子名称</th><th>角色</th><th>原始应付</th><th>已付</th><th>剩余应付</th><th>本次分摊金额</th></tr></thead><tbody>
                      <tr v-if="cnPayment.items.length === 0"><td colspan="8">该 CN 暂无待付款明细。</td></tr>
                      <tr v-for="item in cnPayment.items" v-else :key="item.id"><td><input type="checkbox" :disabled="item.remaining_amount <= 0" :checked="selectedPaymentItemIds.has(item.id)" @change="setPaymentItemSelected(item, ($event.target as HTMLInputElement).checked)" /></td><td>{{ item.order_no }}</td><td><span class="cell-wrap">{{ item.display_name || item.product_name }}</span></td><td>{{ item.character_name || '-' }}</td><td>{{ formatMoney(item.amount) }}</td><td>{{ formatMoney(item.paid_amount) }}</td><td>{{ formatMoney(item.remaining_amount) }}</td><td><input class="amount-input" v-model="paymentAmounts[item.id]" :disabled="!selectedPaymentItemIds.has(item.id)" type="number" min="0.01" step="0.01" :max="item.remaining_amount" :class="{ invalid: paymentAmountInvalid(item) }" /></td></tr>
                    </tbody></table></div>
                    <label class="payment-note"><span>备注</span><input v-model="submissionApproveNote" maxlength="200" placeholder="可选" /></label>
                    <div class="page-actions"><button class="primary-button" type="button" :disabled="submissionApproving || selectedPaymentItems.length === 0" @click="approveSubmission">{{ submissionApproving ? '通过中' : '确认通过并创建付款' }}</button></div>
                  </template>
                </section>
              </template>

              <section v-else-if="submissionDetail.submission.status === 'approved'" class="panel nested-panel">
                <div class="panel__header"><h2>已核对通过</h2></div>
                <p class="muted">已创建正式付款并计入有效已付金额。撤销正式付款不会改写本凭证的历史状态。</p>
                <div v-if="submissionDetail.submission.linked_payment_id" class="page-actions"><button class="secondary-button" type="button" @click="navigate('/admin/finance/payments/' + submissionDetail.submission.linked_payment_id)">查看关联付款</button></div>
              </section>

              <section v-else-if="submissionDetail.submission.status === 'rejected'" class="panel nested-panel">
                <div class="panel__header"><h2>已驳回</h2></div>
                <p class="muted">驳回原因：{{ submissionDetail.submission.reject_reason || '-' }}。用户可重新提交新的凭证。</p>
              </section>

              <section class="panel nested-panel technical-section">
                <details>
                  <summary><span class="closed-label">▶ 技术标识</span><span class="open-label">▼ 技术标识</span></summary>
                  <p class="muted technical-note">仅供技术排查，日常对账与查询无需使用。</p>
                  <div class="technical-list">
                    <article class="technical-item"><div class="technical-item__head"><span class="technical-item__type">凭证 ID</span><button type="button" class="copy-button" @click="copyIdentifier(submissionDetail.submission.id)">复制</button></div><code class="technical-item__value">{{ submissionDetail.submission.id }}</code></article>
                    <article class="technical-item"><div class="technical-item__head"><span class="technical-item__type">SHA-256</span><button type="button" class="copy-button" @click="copyIdentifier(submissionDetail.submission.sha256)">复制</button></div><code class="technical-item__value">{{ submissionDetail.submission.sha256 }}</code></article>
                    <article class="technical-item"><div class="technical-item__head"><span class="technical-item__type">图片格式 / 大小</span></div><code class="technical-item__value">{{ submissionDetail.submission.mime_type }} / {{ submissionDetail.submission.byte_size }} B</code></article>
                    <article v-if="submissionDetail.submission.linked_payment_id" class="technical-item"><div class="technical-item__head"><span class="technical-item__type">关联付款 ID</span><button type="button" class="copy-button" @click="copyIdentifier(submissionDetail.submission.linked_payment_id)">复制</button></div><code class="technical-item__value">{{ submissionDetail.submission.linked_payment_id }}</code></article>
                  </div>
                </details>
              </section>
            </template>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-qr'">
          <section class="panel">
            <div class="panel__header">
              <div><h2>收款二维码</h2><p class="muted">上传、替换或停用支付宝与微信的静态收款二维码。普通用户登录后可查看当前生效的二维码。</p></div>
              <div class="header-actions"><button class="secondary-button" type="button" :disabled="paymentQRLoading" @click="loadPaymentQRStatuses">{{ paymentQRLoading ? '加载中' : '刷新' }}</button></div>
            </div>
            <div v-if="paymentQRMessage" class="inline-alert">{{ paymentQRMessage }}</div>
            <div class="qr-admin-grid">
              <article v-for="method in qrMethods" :key="method" class="qr-card" :class="method === 'alipay' ? 'qr-card--alipay' : 'qr-card--wechat'">
                <header class="qr-card__head">
                  <h3>{{ paymentMethodLabel(method) }}收款码</h3>
                  <span class="status-chip" :data-state="paymentQRStatusByMethod[method].configured ? 'active' : 'disabled'">{{ paymentQRStatusByMethod[method].configured ? '已配置' : '未配置' }}</span>
                </header>
                <div class="qr-card__preview">
                  <img v-if="paymentQRStatusByMethod[method].configured" :src="adminQRImageURL(method)" :alt="paymentMethodLabel(method) + '收款二维码'" class="qr-image" />
                  <p v-else class="qr-empty muted">未配置该收款二维码</p>
                </div>
                <p v-if="paymentQRStatusByMethod[method].configured" class="qr-updated muted">更新时间：{{ formatDate(paymentQRStatusByMethod[method].updated_at) }}</p>

                <div class="qr-card__upload">
                  <label class="file-picker qr-file-picker">
                    <span>选择图片（PNG / JPEG / WebP，≤ 5 MiB）</span>
                    <input :id="`qr-file-${method}`" type="file" accept="image/png,image/jpeg,image/webp" @change="onQRFileChange(method, $event)" />
                  </label>
                  <div v-if="qrSelectedFile[method]" class="qr-selected">
                    <img :src="qrPreviewURL[method]" alt="待上传预览" class="qr-image qr-image--preview" />
                    <p class="qr-file-meta">{{ qrSelectedFile[method]!.name }} · {{ qrSelectedFile[method]!.type || '未知格式' }} · {{ formatBytes(qrSelectedFile[method]!.size) }}</p>
                    <div class="qr-actions">
                      <button class="primary-button" type="button" :disabled="qrUploading[method]" @click="uploadPaymentQRImage(method)">{{ qrUploading[method] ? '上传中' : (paymentQRStatusByMethod[method].configured ? '替换二维码' : '上传二维码') }}</button>
                      <button class="secondary-button" type="button" :disabled="qrUploading[method]" @click="clearQRSelection(method)">取消</button>
                    </div>
                  </div>
                </div>

                <div v-if="paymentQRStatusByMethod[method].configured" class="qr-card__danger">
                  <button class="secondary-button danger-button" type="button" :disabled="qrDisabling[method]" @click="disablePaymentQRImage(method)">{{ qrDisabling[method] ? '停用中' : '停用二维码' }}</button>
                </div>

                <!-- Same contract as every other technical area: last thing in
                     the card, collapsed by default, titled 技术标识, marked
                     仅供技术排查. 更新管理员/更新时间 moved out of it — those are
                     ordinary audit facts an operator reads day to day, not
                     troubleshooting internals. -->
                <dl v-if="paymentQRStatusByMethod[method].configured" class="qr-meta-list">
                  <div><dt>格式</dt><dd>{{ paymentQRStatusByMethod[method].mime_type || '-' }}</dd></div>
                  <div><dt>大小</dt><dd>{{ paymentQRStatusByMethod[method].byte_size ? formatBytes(paymentQRStatusByMethod[method].byte_size ?? 0) : '-' }}</dd></div>
                  <div><dt>更新管理员</dt><dd>{{ paymentQRStatusByMethod[method].updated_by || '-' }}</dd></div>
                  <div><dt>更新时间</dt><dd>{{ formatDate(paymentQRStatusByMethod[method].updated_at) }}</dd></div>
                </dl>
                <details v-if="paymentQRStatusByMethod[method].configured" class="technical-panel qr-technical">
                  <summary><span class="closed-label">▶ 技术标识</span><span class="open-label">▼ 技术标识</span></summary>
                  <p class="muted technical-note">仅供技术排查，日常对账与查询无需使用。</p>
                  <dl class="qr-tech-list">
                    <div class="qr-tech-sha"><dt>SHA-256</dt><dd><code>{{ paymentQRStatusByMethod[method].sha256 || '-' }}</code></dd></div>
                  </dl>
                </details>
              </article>
            </div>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-users'">
          <section class="panel">
            <!-- Row 1: title only. -->
            <div class="page-heading">
              <h2>用户与账号</h2>
              <p>查看用户订单与付款汇总、查询码与恢复邮箱状态。筛选入口在每列表头的漏斗图标中。</p>
              <p class="muted">本页只读，不提供删除。</p>
            </div>

            <!-- Row 2: actions. -->
            <div class="page-actions">
              <button class="secondary-button" type="button" @click="exportAdminUsersExcel">导出 Excel</button>
              <button class="secondary-button ghost-button" type="button" @click="exportAdminUsersCSV">CSV</button>
              <button class="secondary-button" type="button" :disabled="adminUsersLoading" @click="loadAdminUsers">{{ adminUsersLoading ? '加载中' : '刷新' }}</button>
            </div>

            <!-- Row 3: result scope and the single global reset. -->
            <div class="page-resultbar">
              <span class="filter-result-count">结果：共 {{ adminUserTotal }} 位用户</span>
              <span class="muted">第 {{ adminUserPage }} / {{ Math.max(adminUserTotalPages, 1) }} 页</span>
              <label class="page-size-control">
                <span>每页</span>
                <select v-model.number="adminUserPageSize" :disabled="adminUsersLoading" @change="changeAdminUserPageSize">
                  <option :value="25">25 条</option>
                  <option :value="50">50 条</option>
                  <option :value="100">100 条</option>
                  <option :value="200">200 条</option>
                </select>
              </label>
              <button class="secondary-button ghost-button" type="button" :disabled="adminUserActiveFilterCount === 0" @click="resetAdminUserFilters">清空全部筛选<template v-if="adminUserActiveFilterCount > 0">（{{ adminUserActiveFilterCount }}）</template></button>
            </div>

            <div v-if="adminUsersMessage" class="inline-alert">{{ adminUsersMessage }}</div>

            <!-- The tiles aggregate over the whole filtered set (computed in
                 SQL, not summed from the page), so they agree with the result
                 count above rather than describing only the visible rows. -->
            <div v-if="adminUsersSummary" class="summary-grid compact-summary">
              <article class="metric-tile"><span>用户总数</span><strong>{{ adminUsersSummary.user_count }}</strong></article>
              <article class="metric-tile"><span>有订单用户数</span><strong>{{ adminUsersSummary.users_with_orders }}</strong></article>
              <article class="metric-tile"><span>订单总额</span><strong>{{ formatMoney(adminUsersSummary.total_amount) }}</strong></article>
              <article class="metric-tile"><span>有效已付总额</span><strong>{{ formatMoney(adminUsersSummary.paid_amount) }}</strong></article>
              <article class="metric-tile"><span>剩余待付总额</span><strong>{{ formatMoney(adminUsersSummary.remaining_amount) }}</strong></article>
            </div>

            <div class="table-scroll history-table user-table">
              <table>
                <thead>
                  <tr>
                    <th><ColumnValueFilter v-model="adminUserFilterState.values.cn" label="CN" column="cn" :load-facets="loadAdminUserFacets" @update:model-value="applyAdminUserFilters" /></th>
                    <th><ColumnValueFilter v-model="adminUserFilterState.values.name" label="用户名称" column="name" :load-facets="loadAdminUserFacets" @update:model-value="applyAdminUserFilters" /></th>
                    <th><ColumnValueFilter v-model="adminUserFilterState.values.status" label="查询权限" column="status" :load-facets="loadAdminUserFacets" @update:model-value="applyAdminUserFilters" /></th>
                    <th><ColumnValueFilter v-model="adminUserFilterState.values.has_query_code" label="查询码" column="has_query_code" :load-facets="loadAdminUserFacets" @update:model-value="applyAdminUserFilters" /></th>
                    <th><ColumnValueFilter v-model="adminUserFilterState.values.has_recovery_email" label="恢复邮箱" column="has_recovery_email" :load-facets="loadAdminUserFacets" @update:model-value="applyAdminUserFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="adminUserFilterState.ranges.order_count" label="订单数量" step="1" @update:model-value="applyAdminUserFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="adminUserFilterState.ranges.total" label="总金额" @update:model-value="applyAdminUserFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="adminUserFilterState.ranges.paid" label="已付金额" @update:model-value="applyAdminUserFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="adminUserFilterState.ranges.unpaid" label="未付金额" @update:model-value="applyAdminUserFilters" /></th>
                    <th><ColumnDateFilter v-model="adminUserFilterState.dates.last_login" label="最后登录时间" allow-blank blank-label="从未登录" @update:model-value="applyAdminUserFilters" /></th>
                    <th><ColumnDateFilter v-model="adminUserFilterState.dates.created" label="创建时间" @update:model-value="applyAdminUserFilters" /></th>
                    <th><span class="column-header"><span class="column-header__label">查看详情</span></span></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="adminUsersLoading"><td colspan="12">加载中…</td></tr>
                  <tr v-else-if="adminUsers.length === 0"><td colspan="12">没有符合当前筛选条件的用户。</td></tr>
                  <!-- user.id keys the row and drives the detail link; it is
                       never rendered as a column. -->
                  <tr v-for="user in adminUsers" v-else :key="user.id">
                    <td><span class="cell-wrap"><strong>{{ user.cn_code }}</strong></span></td>
                    <td><span class="cell-wrap">{{ user.display_name || '-' }}</span></td>
                    <td><span class="status-chip" :data-state="user.status">{{ userStatusLabel(user.status) }}</span></td>
                    <td>{{ user.has_query_code ? '已设置' : '未设置' }}<small v-if="user.query_code_updated_at">{{ formatDate(user.query_code_updated_at) }}</small></td>
                    <td>{{ user.has_recovery_email ? '已绑定' : '未绑定' }}</td>
                    <td class="numeric-column">{{ user.order_count }}</td>
                    <td class="numeric-column">{{ formatMoney(user.total_amount) }}</td>
                    <td class="numeric-column">{{ formatMoney(user.paid_amount) }}</td>
                    <td class="numeric-column" :class="{ danger: user.remaining_amount > 0 }">{{ formatMoney(user.remaining_amount) }}</td>
                    <td>{{ user.last_login_at ? formatDate(user.last_login_at) : '从未登录' }}</td>
                    <td>{{ formatDate(user.created_at) }}</td>
                    <td><button class="secondary-button" type="button" @click="navigate('/admin/users/' + user.id)">详情</button></td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div v-if="adminUserTotalPages > 1" class="pagination">
              <button class="secondary-button" type="button" :disabled="adminUsersLoading || adminUserPage <= 1" @click="goToAdminUserPage(adminUserPage - 1)">上一页</button>
              <span class="muted">第 {{ adminUserPage }} / {{ adminUserTotalPages }} 页</span>
              <button class="secondary-button" type="button" :disabled="adminUsersLoading || adminUserPage >= adminUserTotalPages" @click="goToAdminUserPage(adminUserPage + 1)">下一页</button>
            </div>
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
              <section class="panel nested-panel recovery-email-panel">
                <div class="panel__header">
                  <div><h2>找回邮箱</h2><p class="muted">管理员只能登记新邮箱或查看脱敏状态，不能读取、回填或复制完整邮箱。邮箱验证由已登录的普通用户完成，管理员没有代验证入口。</p></div>
                  <span class="status-chip" :data-state="adminRecoveryEmail?.status || 'disabled'">{{ recoveryEmailStatusLabel(adminRecoveryEmail?.status) }}</span>
                </div>
                <p v-if="adminRecoveryEmailLoading" class="muted">正在加载找回邮箱状态。</p>
                <div v-else class="recovery-email-state">
                  <div><span>当前邮箱</span><strong class="recovery-email-masked">{{ adminRecoveryEmail?.has_recovery_email ? (adminRecoveryEmail.masked_email || '-') : '未登记' }}</strong></div>
                  <div><span>更新时间</span><strong>{{ adminRecoveryEmail?.updated_at ? formatDate(adminRecoveryEmail.updated_at) : '-' }}</strong></div>
                </div>
                <p v-if="!config.emailDeliveryEnabled" class="inline-alert">邮件服务未配置；当前只能管理邮箱记录，用户无法接收验证码。</p>
                <p v-if="adminUserDetail.user.status === 'merged'" class="inline-alert">已合并用户不能新增、替换或解绑找回邮箱。</p>
                <form v-else class="recovery-email-form" @submit.prevent="saveAdminRecoveryEmail">
                  <label><span>{{ adminRecoveryEmail?.has_recovery_email ? '新的找回邮箱' : '找回邮箱' }}</span><input v-model="adminRecoveryEmailDraft" type="email" autocomplete="off" maxlength="254" placeholder="重新输入完整新邮箱" /></label>
                  <label><span>操作原因（必填）</span><input v-model="adminRecoveryEmailReason" maxlength="200" placeholder="说明登记、替换或解绑原因" /></label>
                  <div class="recovery-email-actions">
                    <button class="primary-button" type="submit" :disabled="adminRecoveryEmailSaving || !adminRecoveryEmailDraft.trim() || !adminRecoveryEmailReason.trim()">{{ adminRecoveryEmailSaving ? '保存中' : (adminRecoveryEmail?.has_recovery_email ? '替换邮箱' : '登记邮箱') }}</button>
                    <button v-if="adminRecoveryEmail?.has_recovery_email" class="danger-button" type="button" :disabled="adminRecoveryEmailSaving || !adminRecoveryEmailReason.trim()" @click="unbindAdminRecoveryEmail">{{ adminRecoveryEmailSaving ? '处理中' : '解绑邮箱' }}</button>
                  </div>
                </form>
                <div v-if="adminRecoveryEmailMessage" class="inline-alert">{{ adminRecoveryEmailMessage }}</div>
              </section>              <section v-if="adminUserDetail.user.status === 'active'" class="panel nested-panel danger-panel">
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
                  <summary><span class="closed-label">▶ 技术标识</span><span class="open-label">▼ 技术标识</span></summary>
                  <p class="muted technical-note">仅供技术排查，日常对账与查询无需使用。</p>
                  <div class="technical-list"><article v-for="identifier in mergeTechnicalIdentifiers(adminUserTechnicalIdentifiers(adminUserDetail))" :key="identifier.type + '-' + identifier.value" class="technical-item"><div class="technical-item__head"><span class="technical-item__type">{{ identifier.type }}</span><span class="technical-item__context">{{ identifier.context }}</span><button type="button" class="copy-button" @click="copyIdentifier(identifier.value)">复制</button></div><code class="technical-item__value">{{ identifier.value }}</code></article></div>
                </details>
              </section>
            </template>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-orders'">
          <section class="panel">
            <!-- Row 1: the title stands alone. Keeping the export buttons off
                 this row is what stops the heading competing with a cluster of
                 controls. -->
            <div class="page-heading">
              <h2>订单只读查询</h2>
              <p>每行对应一项谷子明细，同一 CN 或同一订单可出现多行。筛选只保留符合条件的明细，不会把其他谷子合并显示。</p>
              <p class="muted">本页只读，不允许修改、删除或撤销。筛选入口在每列表头的漏斗图标中。</p>
            </div>

            <!-- Row 2: actions. -->
            <div class="page-actions">
              <button class="secondary-button" type="button" @click="exportOrderItemsExcel">导出明细 Excel</button>
              <button class="secondary-button ghost-button" type="button" @click="exportOrderItemsCSV">CSV</button>
              <button class="secondary-button" type="button" :disabled="ordersLoading" @click="loadOrders">刷新</button>
            </div>

            <!-- Row 3: result scope and the single global reset. -->
            <div class="page-resultbar">
              <span class="filter-result-count">结果：共 {{ orderTotal }} 项谷子明细</span>
              <span class="muted">第 {{ orderPage }} / {{ Math.max(orderTotalPages, 1) }} 页</span>
              <label class="page-size-control">
                <span>每页</span>
                <select v-model.number="orderPageSize" :disabled="ordersLoading" @change="changeOrderPageSize">
                  <option :value="25">25 条</option>
                  <option :value="50">50 条</option>
                  <option :value="100">100 条</option>
                  <option :value="200">200 条</option>
                </select>
              </label>
              <button class="secondary-button ghost-button" type="button" :disabled="orderActiveFilterCount === 0" @click="resetOrderFilters">清空全部筛选<template v-if="orderActiveFilterCount > 0">（{{ orderActiveFilterCount }}）</template></button>
            </div>

            <div v-if="ordersMessage" class="inline-alert">{{ ordersMessage }}</div>
            <div class="table-scroll history-table order-table">
              <table>
                <thead>
                  <tr>
                    <!-- Exactly 13 columns. 用户名称 / 项目名称 / 谷子种类 were
                         removed outright rather than hidden: no empty <th>, no
                         placeholder cell, no CSS-hidden column. 单价 and 操作
                         carry no funnel. -->
                    <th><ColumnValueFilter v-model="orderFilterState.values.cn" label="CN" column="cn" :load-facets="loadOrderFacets" @update:model-value="applyOrderFilters" /></th>
                    <th><ColumnValueFilter v-model="orderFilterState.values.item" label="谷子名称" column="item" :load-facets="loadOrderFacets" @update:model-value="applyOrderFilters" /></th>
                    <th><ColumnValueFilter v-model="orderFilterState.values.series" label="谷子系列" column="series" :load-facets="loadOrderFacets" @update:model-value="applyOrderFilters" /></th>
                    <th><ColumnValueFilter v-model="orderFilterState.values.role" label="谷子角色" column="role" :load-facets="loadOrderFacets" @update:model-value="applyOrderFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="orderFilterState.ranges.quantity" label="数量" step="1" @update:model-value="applyOrderFilters" /></th>
                    <th class="numeric-column"><span class="column-header"><span class="column-header__label">单价</span></span></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="orderFilterState.ranges.amount" label="明细总金额" @update:model-value="applyOrderFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="orderFilterState.ranges.paid" label="已付金额" @update:model-value="applyOrderFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="orderFilterState.ranges.unpaid" label="未付金额" @update:model-value="applyOrderFilters" /></th>
                    <th><ColumnValueFilter v-model="orderFilterState.values.status" label="订单状态" column="status" :load-facets="loadOrderFacets" @update:model-value="applyOrderFilters" /></th>
                    <th><ColumnValueFilter v-model="orderFilterState.values.payment_status" label="付款状态" column="payment_status" :load-facets="loadOrderFacets" @update:model-value="applyOrderFilters" /></th>
                    <th><ColumnDateFilter v-model="orderFilterState.dates.created" label="创建时间" @update:model-value="applyOrderFilters" /></th>
                    <th><span class="column-header"><span class="column-header__label">查看详情</span></span></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="ordersLoading"><td colspan="13">加载中…</td></tr>
                  <tr v-else-if="orderItems.length === 0"><td colspan="13">没有符合当前筛选条件的谷子明细。</td></tr>
                  <!-- One row per goods line. Rows are never merged with rowspan:
                       the same CN legitimately repeats, and merging would break
                       filtering, paging and the mobile layout. Consecutive rows
                       of one order are only tinted, never combined. -->
                  <tr v-for="(item, index) in orderItems" v-else :key="item.item_id" :class="{ 'order-row--alt': isAlternateOrderRow(index) }">
                    <td><strong>{{ item.cn_code }}</strong></td>
                    <td><span class="cell-wrap">{{ item.item_name }}</span></td>
                    <td><span class="cell-wrap">{{ item.series_code || '-' }}</span></td>
                    <td><span class="cell-wrap">{{ item.character_name || '-' }}</span></td>
                    <td class="numeric-column">{{ item.quantity }}</td>
                    <td class="numeric-column">{{ formatMoney(item.unit_price) }}</td>
                    <td class="numeric-column">{{ formatMoney(item.total_amount) }}</td>
                    <td class="numeric-column">{{ formatMoney(item.paid_amount) }}</td>
                    <td class="numeric-column" :class="{ danger: item.unpaid_amount > 0 }">{{ formatMoney(item.unpaid_amount) }}</td>
                    <td><span class="status-chip" :data-state="item.status">{{ statusLabel(item.status) }}</span></td>
                    <td><span class="status-chip" :data-state="item.payment_status">{{ queryPaymentStatusLabel(item.payment_status) }}</span></td>
                    <td>{{ formatDate(item.created_at) }}</td>
                    <td><button class="secondary-button" type="button" @click="navigate(`/admin/orders/${item.order_id}`)">详情</button></td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div v-if="orderTotalPages > 1" class="pagination">
              <button class="secondary-button" type="button" :disabled="ordersLoading || orderPage <= 1" @click="goToOrderPage(orderPage - 1)">上一页</button>
              <span class="muted">第 {{ orderPage }} / {{ orderTotalPages }} 页</span>
              <button class="secondary-button" type="button" :disabled="ordersLoading || orderPage >= orderTotalPages" @click="goToOrderPage(orderPage + 1)">下一页</button>
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
              <!-- Technical identifiers live here and nowhere else: last
                   section of the detail page, collapsed by default. The order
                   list deliberately carries none of them. -->
              <section v-if="orderDetailTechnicalIdentifiers(orderDetail).length > 0" class="panel nested-panel technical-section">
                <details>
                  <summary><span class="closed-label">▶ 技术标识</span><span class="open-label">▼ 技术标识</span></summary>
                  <p class="muted technical-note">仅供技术排查，日常对账与查询无需使用。</p>
                  <div class="technical-list"><article v-for="identifier in mergeTechnicalIdentifiers(orderDetailTechnicalIdentifiers(orderDetail))" :key="identifier.type + '-' + identifier.value" class="technical-item"><div class="technical-item__head"><span class="technical-item__type">{{ identifier.type }}</span><span class="technical-item__context">{{ identifier.context }}</span><button type="button" class="copy-button" @click="copyIdentifier(identifier.value)">复制</button></div><code class="technical-item__value">{{ identifier.value }}</code></article></div>
                </details>
              </section>
            </template>
          </section>
        </template>
        <template v-else-if="routeName === 'admin-import-history'">
          <section class="panel">
            <!-- Row 1: title only. -->
            <div class="page-heading">
              <h2>导入历史</h2>
              <p>查看导入记录，可进入详情按导入批次安全软撤销。筛选入口在每列表头的漏斗图标中。</p>
              <p class="muted">技术标识（导入记录 ID、文件 SHA）仅在导入详情页底部的技术标识区显示。</p>
            </div>

            <!-- Row 2: actions (no CSV/XLSX export here; refresh only). -->
            <div class="page-actions">
              <button class="secondary-button" type="button" :disabled="historyLoading" @click="loadHistory">{{ historyLoading ? '加载中' : '刷新' }}</button>
            </div>

            <!-- Row 3: result scope and the single global reset. -->
            <div class="page-resultbar">
              <span class="filter-result-count">结果：共 {{ importTotal }} 条导入记录</span>
              <span class="muted">第 {{ importPage }} / {{ Math.max(importTotalPages, 1) }} 页</span>
              <label class="page-size-control">
                <span>每页</span>
                <select v-model.number="importPageSize" :disabled="historyLoading" @change="changeImportPageSize">
                  <option :value="25">25 条</option>
                  <option :value="50">50 条</option>
                  <option :value="100">100 条</option>
                  <option :value="200">200 条</option>
                </select>
              </label>
              <button class="secondary-button ghost-button" type="button" :disabled="importActiveFilterCount === 0" @click="resetImportFilters">清空全部筛选<template v-if="importActiveFilterCount > 0">（{{ importActiveFilterCount }}）</template></button>
            </div>

            <div v-if="historyMessage" class="inline-alert">{{ historyMessage }}</div>

            <div class="table-scroll history-table import-history-table">
              <table>
                <thead>
                  <tr>
                    <th><ColumnValueFilter v-model="importFilterState.values.filename" label="文件名" column="filename" :load-facets="loadImportFacets" @update:model-value="applyImportFilters" /></th>
                    <th><ColumnValueFilter v-model="importFilterState.values.status" label="状态" column="status" :load-facets="loadImportFacets" @update:model-value="applyImportFilters" /></th>
                    <th><ColumnValueFilter v-model="importFilterState.values.uploaded_by" label="上传管理员" column="uploaded_by" :load-facets="loadImportFacets" @update:model-value="applyImportFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="importFilterState.ranges.sheet" label="工作表数" step="1" @update:model-value="applyImportFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="importFilterState.ranges.issue" label="问题数" step="1" @update:model-value="applyImportFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="importFilterState.ranges.written" label="写入明细数" step="1" @update:model-value="applyImportFilters" /></th>
                    <th class="numeric-column"><ColumnRangeFilter v-model="importFilterState.ranges.amount" label="总金额" @update:model-value="applyImportFilters" /></th>
                    <th><ColumnDateFilter v-model="importFilterState.dates.created" label="上传时间" @update:model-value="applyImportFilters" /></th>
                    <th><ColumnDateFilter v-model="importFilterState.dates.confirmed" label="确认时间" allow-blank blank-label="未确认" @update:model-value="applyImportFilters" /></th>
                    <th><span class="column-header"><span class="column-header__label">查看详情</span></span></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="historyLoading"><td colspan="10">加载中…</td></tr>
                  <tr v-else-if="importHistory.length === 0"><td colspan="10">没有符合当前筛选条件的导入记录。</td></tr>
                  <!-- item.id keys the row and drives the detail link; the main
                       table renders no technical identifiers (SHA / batch id). -->
                  <tr v-for="item in importHistory" v-else :key="item.id">
                    <td><span class="cell-wrap"><strong>{{ item.original_filename }}</strong></span><small>{{ formatBytes(item.file_size) }}</small></td>
                    <td><span class="status-chip" :data-state="item.status">{{ statusLabel(item.status) }}</span></td>
                    <td><span class="cell-wrap">{{ item.uploaded_by || '-' }}</span></td>
                    <td class="numeric-column">{{ item.sheet_count }}</td>
                    <td class="numeric-column">{{ item.error_count + item.warning_count + item.notice_count }}</td>
                    <td class="numeric-column">{{ item.confirm_result ? item.confirm_result.order_item_count : '-' }}</td>
                    <td class="numeric-column">{{ formatMoney(historyTotalAmount(item)) }}</td>
                    <td>{{ formatDate(item.created_at) }}</td>
                    <td>{{ item.confirmed_at ? formatDate(item.confirmed_at) : '未确认' }}</td>
                    <td><button class="secondary-button" type="button" @click="navigate(`/admin/imports/${item.id}`)">详情</button></td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div v-if="importTotalPages > 1" class="pagination">
              <button class="secondary-button" type="button" :disabled="historyLoading || importPage <= 1" @click="goToImportPage(importPage - 1)">上一页</button>
              <span class="muted">第 {{ importPage }} / {{ importTotalPages }} 页</span>
              <button class="secondary-button" type="button" :disabled="historyLoading || importPage >= importTotalPages" @click="goToImportPage(importPage + 1)">下一页</button>
            </div>
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
                  <summary><span class="closed-label">▶ 技术标识</span><span class="open-label">▼ 技术标识</span></summary>
                  <p class="muted technical-note">仅供技术排查，日常对账与查询无需使用。</p>
                  <div class="technical-list"><article v-for="identifier in mergeTechnicalIdentifiers(importDetailTechnicalIdentifiers(importDetail))" :key="identifier.type + '-' + identifier.value" class="technical-item"><div class="technical-item__head"><span class="technical-item__type">{{ identifier.type }}</span><span class="technical-item__context">{{ identifier.context }}</span><button type="button" class="copy-button" @click="copyIdentifier(identifier.value)">复制</button></div><code class="technical-item__value">{{ identifier.value }}</code></article></div>
                </details>
              </section>
            </template>
          </section>
        </template>
      </template>
    </main>

    <main v-else class="workspace">
      <template v-if="routeName === 'query' && !queryUser">
        <div class="entry-page">
          <header class="entry-brand">
            <button class="entry-back" type="button" @click="navigate('/')">← 返回系统主页</button>
            <h1>PJSK 谷子系统</h1>
            <p class="entry-subtitle">用户服务入口</p>
          </header>

          <section v-if="queryView === 'login'" class="entry-card">
            <h2 class="entry-card__title">用户登录</h2>
            <form class="entry-form" @submit.prevent="loginQuery">
              <label><span>CN</span><input v-model="queryCN" autocomplete="username" required placeholder="输入自己的 CN" /></label>
              <label><span>查询码</span><input v-model="queryCode" type="password" autocomplete="current-password" required placeholder="管理员提供的查询码" /></label>
              <button class="primary-button entry-submit" type="submit" :disabled="queryLoading">{{ queryLoading ? '查询中' : '查询订单' }}</button>
            </form>
            <div class="entry-aux">
              <p v-if="config.emailDeliveryEnabled"><button class="link-button" type="button" @click="openRecoveryView">忘记查询码</button><span class="muted">通过已验证找回邮箱重置</span></p>
              <p v-else class="muted">邮箱找回功能暂不可用（邮件服务未配置）。</p>
              <p><button class="link-button" type="button" @click="openBindView">首次设置查询码</button><span class="muted">尚未设置查询码，需管理员提供一次性绑定码</span></p>
            </div>
            <div v-if="queryMessage" class="inline-alert">{{ queryMessage }}</div>
          </section>

          <section v-else-if="queryView === 'bind'" class="entry-card">
            <h2 class="entry-card__title">首次设置查询码</h2>
            <form class="entry-form" @submit.prevent="submitBindCode">
              <label><span>CN</span><input v-model="bindCN" autocomplete="username" required placeholder="输入自己的 CN" /></label>
              <label><span>一次性绑定码</span><input v-model="bindTokenInput" type="password" autocomplete="one-time-code" required placeholder="管理员提供的绑定码" /></label>
              <label><span>新查询码</span><input v-model="bindNewCode" type="password" autocomplete="new-password" required minlength="6" maxlength="32" placeholder="6-32 位" /></label>
              <label><span>确认新查询码</span><input v-model="bindConfirmCode" type="password" autocomplete="new-password" required minlength="6" maxlength="32" placeholder="再次输入新查询码" /></label>
              <button class="primary-button entry-submit" type="submit" :disabled="bindSubmitting">{{ bindSubmitting ? '设置中' : '设置查询码' }}</button>
              <button class="secondary-button entry-submit" type="button" :disabled="bindSubmitting" @click="closeBindView">返回登录</button>
            </form>
            <div v-if="bindMessage" class="inline-alert">{{ bindMessage }}</div>
          </section>

          <section v-else-if="queryView === 'recovery'" class="entry-card">
            <h2 class="entry-card__title">忘记查询码</h2>
            <p class="muted entry-card__hint">通过管理员登记且已经验证的找回邮箱重置查询码。</p>
            <ol class="query-recovery-steps" aria-label="查询码找回步骤">
              <li :data-active="anonymousRecoveryStep === 'request'">输入 CN</li>
              <li :data-active="anonymousRecoveryStep === 'verify'">邮箱验证</li>
              <li :data-active="anonymousRecoveryStep === 'reset'">设置新查询码</li>
            </ol>
            <form v-if="anonymousRecoveryStep === 'request'" class="entry-form" @submit.prevent="requestAnonymousQueryRecovery">
              <label><span>CN</span><input v-model="anonymousRecoveryCN" autocomplete="username" required placeholder="输入自己的 CN" /></label>
              <p class="muted query-recovery-notice">无论账号是否存在或是否符合找回条件，页面都会显示相同提示，不会展示邮箱或账户状态。</p>
              <button class="primary-button entry-submit" type="submit" :disabled="anonymousRecoveryLoading">{{ anonymousRecoveryLoading ? '请求中' : '发送邮箱验证码' }}</button>
            </form>
            <form v-else-if="anonymousRecoveryStep === 'verify'" class="entry-form" @submit.prevent="verifyAnonymousQueryRecovery">
              <label><span>CN</span><input :value="anonymousRecoveryCN" autocomplete="username" disabled /></label>
              <label><span>邮箱验证码</span><input v-model="anonymousRecoveryCode" type="text" inputmode="numeric" autocomplete="one-time-code" maxlength="6" pattern="[0-9]{6}" required placeholder="输入 6 位数字验证码" /></label>
              <p class="muted query-recovery-notice">验证码有效期为 10 分钟；页面不会显示邮箱地址。</p>
              <button class="primary-button entry-submit" type="submit" :disabled="anonymousRecoveryLoading || anonymousRecoveryCode.trim().length !== 6">{{ anonymousRecoveryLoading ? '验证中' : '确认邮箱验证码' }}</button>
            </form>
            <form v-else class="entry-form" @submit.prevent="submitRecoveredQueryCode">
              <label><span>新查询码</span><input v-model="anonymousRecoveryNewCode" type="password" autocomplete="new-password" required minlength="6" maxlength="32" placeholder="6-32 位" /></label>
              <label><span>确认新查询码</span><input v-model="anonymousRecoveryConfirmCode" type="password" autocomplete="new-password" required minlength="6" maxlength="32" placeholder="再次输入新查询码" /></label>
              <p v-if="anonymousRecoveryTokenExpiresAt" class="muted query-recovery-notice">本次重置授权将在 {{ formatDate(anonymousRecoveryTokenExpiresAt) }} 过期；刷新页面后需要重新开始。</p>
              <button class="primary-button entry-submit" type="submit" :disabled="anonymousRecoveryLoading">{{ anonymousRecoveryLoading ? '重置中' : '重置查询码' }}</button>
            </form>
            <div class="entry-aux">
              <button class="link-button" type="button" :disabled="anonymousRecoveryLoading" @click="closeRecoveryView">返回查询登录</button>
            </div>
            <div v-if="anonymousRecoveryMessage" class="inline-alert">{{ anonymousRecoveryMessage }}</div>
          </section>
        </div>
      </template>

      <template v-else-if="routeName === 'query'">
        <PortalStatusBar :identity="queryUser ? ('CN：' + queryUser.cn_code) : undefined" back-label="← 返回系统主页" @back="navigate('/')" @logout="logoutQuery" />
        <section class="portal-hero">
          <h1 class="portal-hero__title">用户中心</h1>
          <p class="portal-hero__subtitle">当前登录 CN：{{ queryUser?.cn_code }}</p>
        </section>
        <div v-if="queryOrders" class="summary-grid portal-summary">
          <article class="metric-tile"><span>总金额</span><strong>{{ formatMoney(queryOrders.total_amount) }}</strong></article>
          <article class="metric-tile"><span>已付金额</span><strong>{{ formatMoney(queryOrders.paid_amount) }}</strong></article>
          <article class="metric-tile"><span>未付金额</span><strong class="danger">{{ formatMoney(queryOrders.remaining_amount) }}</strong></article>
          <article class="metric-tile"><span>订单数</span><strong>{{ queryOrders.orders.length }}</strong></article>
        </div>
        <section class="module-portal">
          <div class="module-grid">
            <ModuleCard title="我的订单" description="订单汇总、商品明细与分类/角色/系列筛选" :meta="queryOrders ? ('共 ' + queryOrders.orders.length + ' 个订单') : ''" accent="blue" cta="查看订单" @enter="navigate('/query/orders')" />
            <ModuleCard title="付款中心" description="选择支付宝 / 微信、查看应付金额与收款二维码" :meta="queryOrders ? ('未付 ' + formatMoney(queryOrders.remaining_amount)) : ''" accent="green" cta="去付款" @enter="navigate('/query/payment')" />
            <ModuleCard title="付款记录" description="历史付款流水与可展开的关联明细" :meta="queryOrders ? (queryOrders.payments.length + ' 条记录') : ''" accent="neutral" cta="查看记录" @enter="navigate('/query/payments')" />
            <ModuleCard title="账户安全" description="修改查询码、找回邮箱验证" accent="neutral" cta="账户设置" @enter="navigate('/query/security')" />
          </div>
        </section>
      </template>

      <template v-else-if="isUserRoute">
        <PortalStatusBar :identity="queryUser ? ('CN：' + queryUser.cn_code) : undefined" back-label="← 返回用户中心" @back="navigate('/query')" @logout="logoutQuery" />
        <div class="module-header"><h2 class="module-header__title">{{ userModuleTitle }}</h2></div>

        <template v-if="routeName === 'query-payment'">
          <section class="panel query-pay-panel" aria-label="付款">
            <div class="panel__header"><div><h2>付款汇总</h2><p class="muted">以下金额由管理员核对录入，付款完成仍以管理员最终录入结果为准。</p></div></div>
            <div class="summary-grid query-pay-summary">
              <article class="metric-tile"><span>总金额</span><strong>{{ formatMoney(queryOrders?.total_amount ?? 0) }}</strong></article>
              <article class="metric-tile"><span>共件数</span><strong>{{ queryOrders?.total_quantity ?? 0 }}</strong></article>
              <article class="metric-tile"><span>已付金额</span><strong>{{ formatMoney(queryOrders?.paid_amount ?? 0) }}</strong></article>
              <article class="metric-tile"><span>未付金额</span><strong class="danger">{{ formatMoney(queryOrders?.remaining_amount ?? 0) }}</strong></article>
            </div>

            <div class="query-pay-block">
              <h3>选择付款方式</h3>
              <div class="query-method-options">
                <button
                  v-for="method in queryPayMethods"
                  :key="method"
                  type="button"
                  class="query-method-button"
                  :class="[method === 'alipay' ? 'query-method-button--alipay' : 'query-method-button--wechat', { active: queryQRMethod === method }]"
                  @click="selectQueryQRMethod(method)"
                >{{ paymentMethodLabel(method) }}</button>
              </div>
            </div>

            <div class="query-pay-block">
              <h3>本次应付</h3>
              <div class="summary-grid query-amount-grid">
                <article class="metric-tile"><span>本金</span><strong>{{ formatMoney(queryBaseAmount) }}</strong></article>
                <article class="metric-tile"><span>手续费</span><strong>{{ formatMoney(queryFeeAmount) }}</strong></article>
                <article class="metric-tile metric-tile--emphasis query-payable-tile"><span>本次应付</span><strong>{{ formatMoney(queryPayableAmount) }}</strong></article>
              </div>
              <p class="muted query-payable-note">{{ queryQRMethod === 'wechat' ? '微信付款含 0.1% 手续费（向上取整到分）。' : '支付宝付款无手续费。' }}请按「本次应付」金额付款；付款完成不代表系统已自动确认，最终以管理员录入结果为准。</p>
            </div>

            <div class="query-pay-block query-pay-block--qr">
              <h3>{{ queryQRMethod ? paymentMethodLabel(queryQRMethod) + '收款二维码' : '收款二维码' }}</h3>
              <p v-if="queryQRError" class="inline-alert">{{ queryQRError }}</p>
              <!-- Fixed-height, centered slot: the configured image and the
                   empty state occupy the same footprint, so switching between
                   支付宝/微信 (or configured/unconfigured) never jumps the layout.
                   Title and image share this same center axis. -->
              <div class="query-qr-slot">
                <template v-if="queryQRMethod && queryMethodAvailable(queryQRMethod)">
                  <figure class="query-qr-figure">
                    <img
                      :key="queryQRMethod + '-' + queryQRReloadKey"
                      :src="queryQRImageURL(queryQRMethod)"
                      :alt="paymentMethodLabel(queryQRMethod) + '收款二维码'"
                      class="query-qr-image"
                      @click="openQueryQRZoom"
                      @error="onQueryQRImageError"
                    />
                    <figcaption class="muted">点击二维码可放大</figcaption>
                  </figure>
                </template>
                <p v-else class="qr-empty muted">管理员暂未配置{{ queryQRMethod ? paymentMethodLabel(queryQRMethod) : '该付款方式' }}的收款二维码。</p>
              </div>
            </div>
          </section>

          <div v-if="queryQRZoom && queryQRMethod && queryMethodAvailable(queryQRMethod)" class="qr-zoom-overlay" role="dialog" aria-modal="true" @click="closeQueryQRZoom">
            <div class="qr-zoom-inner" @click.stop>
              <button class="qr-zoom-close" type="button" aria-label="关闭" @click="closeQueryQRZoom">×</button>
              <img :src="queryQRImageURL(queryQRMethod)" :alt="paymentMethodLabel(queryQRMethod) + '收款二维码'" class="qr-zoom-image" @error="onQueryQRImageError" />
              <p class="qr-zoom-caption">{{ paymentMethodLabel(queryQRMethod) }}收款码</p>
            </div>
          </div>

          <section class="panel query-submission-panel">
            <div class="panel__header"><div><h2>提交收肾记录</h2><p class="muted">用微信或支付宝付款后，在这里上传付款截图作为凭证。提交后状态为「已交肾（待管理员核对）」，管理员核对通过后才会计入已付金额。</p></div></div>
            <div class="query-submission-grid">
              <div class="query-submission-upload">
                <label class="file-field">
                  <span>选择付款截图（PNG / JPEG / WebP，≤10 MiB）</span>
                  <input id="submission-file-input" type="file" accept="image/png,image/jpeg,image/webp" @change="onSubmissionFileChange" />
                </label>
                <div class="submission-preview-slot">
                  <img v-if="submissionPreviewURL" :src="submissionPreviewURL" alt="待提交付款截图预览" class="submission-preview-image" />
                  <p v-else class="qr-empty muted">尚未选择图片</p>
                </div>
              </div>
              <div class="query-submission-meta">
                <div class="query-amount-grid query-submission-amounts">
                  <article class="metric-tile"><span>付款方式</span><strong>{{ queryQRMethod ? paymentMethodLabel(queryQRMethod) : '请先选择' }}</strong></article>
                  <article class="metric-tile"><span>本金</span><strong>{{ formatMoney(queryBaseAmount) }}</strong></article>
                  <article class="metric-tile"><span>手续费</span><strong>{{ formatMoney(queryFeeAmount) }}</strong></article>
                  <article class="metric-tile metric-tile--emphasis"><span>本次应付</span><strong>{{ formatMoney(queryPayableAmount) }}</strong></article>
                </div>
                <p v-if="submissionUploadMessage" class="inline-alert">{{ submissionUploadMessage }}</p>
                <button class="primary-button query-submission-button" type="button" :disabled="!canSubmitProof || queryQRMethod === ''" @click="submitUserProof">{{ submissionUploading ? '提交中…' : '提交收肾记录' }}</button>
                <p v-if="queryQRMethod === ''" class="muted">请先在上方选择付款方式，再提交收肾记录。</p>
              </div>
            </div>

            <div class="query-submission-history">
              <h3>我的收肾记录</h3>
              <p v-if="userSubmissionsLoading" class="muted">正在加载我的收肾记录。</p>
              <p v-else-if="userSubmissionsMessage" class="inline-alert">{{ userSubmissionsMessage }}</p>
              <p v-else-if="userSubmissions.length === 0" class="muted">暂无收肾记录。上传付款截图后会显示在这里。</p>
              <ul v-else class="submission-history-list">
                <li v-for="submission in userSubmissions" :key="submission.id" class="submission-history-item" :data-state="submission.status">
                  <div class="submission-history-head">
                    <span class="status-chip" :data-state="submission.status">{{ submissionStatusLabel(submission.status) }}</span>
                    <span class="muted submission-history-time">{{ formatDate(submission.submitted_at) }}</span>
                  </div>
                  <div class="submission-history-body">
                    <span>{{ paymentMethodLabel(submission.payment_method) }}</span>
                    <span>本次应付 {{ formatMoney(submission.payable_amount) }}</span>
                  </div>
                  <p v-if="submission.status === 'rejected' && submission.reject_reason" class="submission-reject-reason">驳回原因：{{ submission.reject_reason }}。可重新选择图片后再次提交。</p>
                </li>
              </ul>
            </div>
          </section>
        </template>

        <section v-if="routeName === 'query-security'" class="panel query-security-panel">
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
            <div class="query-recovery-email-readonly">
              <div class="panel__header">
                <div><h3>找回邮箱</h3><p class="muted">验证管理员登记的当前邮箱；普通用户不能在此新增、替换或解绑邮箱。</p></div>
                <span class="status-chip" :data-state="queryRecoveryEmail?.status || 'disabled'">{{ recoveryEmailStatusLabel(queryRecoveryEmail?.status) }}</span>
              </div>
              <p v-if="queryRecoveryEmailLoading" class="muted">正在加载找回邮箱状态。</p>
              <template v-else-if="!queryRecoveryEmail?.has_recovery_email">
                <p class="recovery-email-empty">尚未登记找回邮箱，请联系管理员登记。</p>
              </template>
              <template v-else>
                <div class="recovery-email-state">
                  <div><span>当前邮箱</span><strong class="recovery-email-masked">{{ queryRecoveryEmail.masked_email || '-' }}</strong></div>
                  <div v-if="queryRecoveryEmail.status === 'verified'"><span>验证时间</span><strong>{{ queryRecoveryEmail.verified_at ? formatDate(queryRecoveryEmail.verified_at) : '-' }}</strong></div>
                  <div v-else><span>更新时间</span><strong>{{ queryRecoveryEmail.updated_at ? formatDate(queryRecoveryEmail.updated_at) : '-' }}</strong></div>
                </div>
                <div v-if="queryRecoveryEmail.status === 'pending' && config.emailDeliveryEnabled" class="recovery-email-verification">
                  <div class="recovery-email-verification__send">
                    <button class="secondary-button" type="button" :disabled="!queryRecoveryCanSend" @click="sendQueryRecoveryVerification">
                      {{ queryRecoverySending ? '发送中' : (queryRecoveryCooldownSeconds > 0 ? `${queryRecoveryCooldownSeconds} 秒后可重发` : '发送验证码') }}
                    </button>
                    <span class="muted">验证码有效期为 10 分钟；倒计时仅用于页面提示，发送限制由服务器执行。</span>
                  </div>
                  <form class="recovery-email-verification__form" @submit.prevent="verifyQueryRecoveryEmail">
                    <label><span>邮箱验证码</span><input v-model="queryRecoveryVerificationCode" type="text" inputmode="numeric" autocomplete="one-time-code" maxlength="6" pattern="[0-9]{6}" placeholder="输入 6 位数字验证码" /></label>
                    <button class="primary-button" type="submit" :disabled="queryRecoveryVerifying || queryRecoveryVerificationCode.trim().length !== 6">{{ queryRecoveryVerifying ? '验证中' : '确认验证' }}</button>
                  </form>
                  <p v-if="queryRecoveryExpiresAt" class="muted">本次验证码将在 {{ formatDate(queryRecoveryExpiresAt) }} 过期。</p>
                </div>
                <p v-else-if="queryRecoveryEmail.status === 'pending'" class="inline-alert">邮件服务暂未启用，无法发送验证码。</p>
              </template>
              <div v-if="queryRecoveryEmailMessage" class="inline-alert">{{ queryRecoveryEmailMessage }}</div>
            </div>
          </section>

        <template v-if="routeName === 'query-orders' && queryOrders">
          <section class="summary-grid query-order-overview" aria-label="订单汇总">
            <article class="metric-tile query-order-overview__tile">
              <span>总金额</span>
              <strong class="query-order-overview__amount">{{ formatMoney(queryOrders.total_amount) }}</strong>
            </article>
            <article class="metric-tile query-order-overview__tile">
              <span>共多少件</span>
              <strong class="query-order-overview__quantity">{{ queryOrders.total_quantity }}</strong>
            </article>
            <article class="metric-tile query-order-overview__tile query-order-overview__tile--paid">
              <span>已付金额</span>
              <strong class="query-order-overview__amount">{{ formatMoney(queryOrders.paid_amount) }}</strong>
            </article>
            <article class="metric-tile query-order-overview__tile query-order-overview__tile--unpaid">
              <span>未付金额</span>
              <strong class="query-order-overview__amount">{{ formatMoney(queryOrders.remaining_amount) }}</strong>
            </article>
          </section>

          <section v-if="queryOrders.orders.length > 0" class="panel query-orders-panel">
            <div class="query-orders-heading">
              <h2>订单明细</h2>
              <span class="muted query-order-result-count">筛选结果：{{ filteredQueryOrderItemCount }} 项谷子明细（{{ filteredQueryOrders.length }} 个订单）</span>
            </div>
            <div class="query-order-filter-grid" aria-label="订单筛选">
              <label><span>谷子种类</span><select v-model="queryOrderFilters.category"><option value="">全部</option><option v-for="opt in queryOrderFilterOptions.categories" :key="opt" :value="opt">{{ opt }}</option></select></label>
              <label><span>角色</span><select v-model="queryOrderFilters.role"><option value="">全部</option><option v-for="opt in queryOrderFilterOptions.roles" :key="opt" :value="opt">{{ opt }}</option></select></label>
              <label><span>系列</span><select v-model="queryOrderFilters.series"><option value="">全部</option><option v-for="opt in queryOrderFilterOptions.series" :key="opt" :value="opt">{{ opt }}</option></select></label>
              <label><span>付款状态</span><select v-model="queryOrderFilters.paymentStatus"><option value="">全部</option><option value="unpaid">未付款</option><option value="partial">部分付款</option><option value="paid">已付款</option></select></label>
            </div>
            <div class="query-order-filter-actions">
              <span class="muted">筛选仅改变下方明细，汇总金额始终采用后端完整结果。</span>
              <button class="secondary-button" type="button" :disabled="!queryOrderFiltersActive" @click="clearQueryOrderFilters">清空筛选</button>
            </div>
          </section>

          <section v-if="queryOrders.orders.length > 0 && filteredQueryOrders.length === 0" class="panel"><p class="muted">没有符合当前筛选条件的订单明细。<button class="link-button" type="button" @click="clearQueryOrderFilters">清空筛选</button></p></section>

          <section v-for="(order, orderIndex) in filteredQueryOrders" :key="orderIndex" class="panel query-order-card">
            <div class="query-order-card__heading">
              <h2>订单 {{ orderIndex + 1 }}</h2>
              <span class="muted">{{ order.items.length }} 项谷子明细</span>
            </div>
            <div class="table-scroll detail-table query-order-desktop-table">
              <table>
                <thead>
                  <tr><th>谷子名称</th><th>角色</th><th>谷子种类</th><th>系列</th><th>数量</th><th>总金额</th><th>已付金额</th><th>未付金额</th><th>付款状态</th></tr>
                </thead>
                <tbody>
                  <tr v-for="(item, itemIndex) in order.items" :key="`${item.goods_name}-${item.character_name}-${itemIndex}`">
                    <td class="query-order-name">{{ item.display_name || item.goods_name }}</td>
                    <td>{{ queryCharacterLabel(item) }}</td>
                    <td>{{ item.category || '-' }}</td>
                    <td>{{ item.series_code || '-' }}</td>
                    <td class="query-order-number">{{ item.quantity }}</td>
                    <td class="query-order-number">{{ formatMoney(item.amount) }}</td>
                    <td class="query-order-number">{{ formatMoney(item.paid_amount) }}</td>
                    <td class="query-order-number" :class="{ danger: item.remaining_amount > 0 }">{{ formatMoney(item.remaining_amount) }}</td>
                    <td><span class="status-chip" :data-state="item.payment_status">{{ queryPaymentStatusLabel(item.payment_status) }}</span></td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div class="query-order-mobile-items" aria-label="订单明细卡片">
              <article v-for="(item, itemIndex) in order.items" :key="`${item.goods_name}-${item.character_name}-${itemIndex}`" class="query-order-mobile-item">
                <div class="query-order-mobile-item__heading">
                  <h3>{{ item.display_name || item.goods_name }}</h3>
                  <span class="status-chip" :data-state="item.payment_status">{{ queryPaymentStatusLabel(item.payment_status) }}</span>
                </div>
                <dl>
                  <div><dt>角色</dt><dd>{{ queryCharacterLabel(item) }}</dd></div>
                  <div><dt>谷子种类</dt><dd>{{ item.category || '—' }}</dd></div>
                  <div><dt>系列</dt><dd>{{ item.series_code || '—' }}</dd></div>
                  <div><dt>数量</dt><dd class="query-order-number">{{ item.quantity }}</dd></div>
                  <div><dt>总金额</dt><dd class="query-order-number">{{ formatMoney(item.amount) }}</dd></div>
                  <div><dt>已付金额</dt><dd class="query-order-number">{{ formatMoney(item.paid_amount) }}</dd></div>
                  <div><dt>未付金额</dt><dd class="query-order-number" :class="{ danger: item.remaining_amount > 0 }">{{ formatMoney(item.remaining_amount) }}</dd></div>
                </dl>
              </article>
            </div>
          </section>
          <section v-if="queryOrders.orders.length === 0" class="panel"><p class="muted">当前 CN 暂无可查询订单。</p></section>
        </template>

          <section v-if="routeName === 'query-payments'" class="panel query-payments-card">
            <div class="panel__header"><div><h2>付款历史</h2><p class="muted">已撤销的付款不计入有效已付款金额。展开关联明细可查看每笔付款分摊到了哪些谷子上。</p></div></div>
            <div v-if="queryOrders && queryOrders.payments.length > 0" class="list-filter-bar" aria-label="付款历史筛选">
              <label><span>付款方式</span><select v-model="queryPaymentFilters.method"><option value="">全部</option><option value="alipay">支付宝</option><option value="wechat">微信</option><option value="bank">银行转账</option><option value="cash">现金</option><option value="other">其他</option></select></label>
              <label><span>状态</span><select v-model="queryPaymentFilters.status"><option value="">全部</option><option value="approved">已交肾</option><option value="voided">已撤销</option><option value="submitted">待处理</option><option value="rejected">已驳回</option></select></label>
              <label><span>时间从</span><input v-model="queryPaymentFilters.dateFrom" type="datetime-local" /></label>
              <label><span>时间到</span><input v-model="queryPaymentFilters.dateTo" type="datetime-local" /></label>
              <button class="secondary-button" type="button" :disabled="!queryPaymentFiltersActive" @click="clearQueryPaymentFilters">清空筛选</button>
            </div>
            <p v-if="queryLoading" class="query-payment-state muted">正在加载付款历史。</p>
            <p v-else-if="queryOrdersError" class="query-payment-state inline-alert">{{ queryOrdersError }}</p>
            <p v-else-if="!queryOrders || queryOrders.payments.length === 0" class="query-payment-state muted">暂无付款记录</p>
            <p v-else-if="filteredQueryPayments.length === 0" class="query-payment-state muted">没有符合当前筛选条件的付款记录。<button class="link-button" type="button" @click="clearQueryPaymentFilters">清空筛选</button></p>
            <div v-else class="table-scroll history-table">
              <table>
                <thead>
                  <tr><th>付款时间</th><th class="col-emphasis">实付金额</th><th>交肾状态</th><th>本金</th><th>手续费</th><th>付款方式</th><th>关联明细</th></tr>
                </thead>
                <tbody>
                  <template v-for="(payment, paymentIndex) in filteredQueryPayments" :key="`${payment.paid_at}-${paymentIndex}`">
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

      <template v-else-if="routeName === 'admin'">
        <template v-if="admin">
          <PortalStatusBar
            :identity="admin.display_name ?? admin.username"
            :online="isBackendOnline"
            :online-text="isBackendOnline ? '后端在线' : '本地前端模式'"
            :show-refresh="true"
            back-label="← 返回系统主页"
            @back="navigate('/')"
            @refresh="load"
            @logout="logout"
          />
          <section class="portal-hero">
            <h1 class="portal-hero__title">谷子管理中心</h1>
            <p class="portal-hero__subtitle">请选择要进入的管理模块</p>
          </section>
          <section class="module-portal">
            <div class="module-grid">
              <ModuleCard title="数据导入中心" description="Excel 导入预览、确认导入、导入历史与详情" accent="blue" cta="进入模块" @enter="navigate('/admin/data')" />
              <ModuleCard title="订单管理" description="订单查询、明细筛选、订单详情与导出" accent="neutral" cta="进入模块" @enter="navigate('/admin/orders')" />
              <ModuleCard title="用户与账号" description="用户管理、查询码与恢复邮箱状态" accent="neutral" cta="进入模块" @enter="navigate('/admin/users')" />
              <ModuleCard title="收付款管理" description="付款记录、撤销、未付明细与收款二维码" accent="green" cta="进入模块" @enter="navigate('/admin/finance')" />
            </div>
          </section>
        </template>
        <div v-else class="entry-page">
          <header class="entry-brand">
            <button class="entry-back" type="button" @click="navigate('/')">← 返回系统主页</button>
            <h1>PJSK 谷子系统</h1>
            <p class="entry-subtitle">管理员入口</p>
          </header>
          <section class="entry-card">
            <h2 class="entry-card__title">管理员登录</h2>
            <form class="entry-form" @submit.prevent="login">
              <label><span>管理员用户名</span><input v-model="loginUsername" autocomplete="username" required placeholder="输入管理员用户名" /></label>
              <label><span>密码</span><input v-model="loginPassword" type="password" autocomplete="current-password" required placeholder="输入密码" /></label>
              <button class="primary-button entry-submit" type="submit" :disabled="loginLoading">{{ loginLoading ? '登录中' : '登录' }}</button>
            </form>
            <div v-if="authMessage" class="inline-alert">{{ authMessage }}</div>
          </section>
        </div>
      </template>

      <template v-else>
        <div class="entry-page">
          <header class="entry-brand">
            <h1>PJSK 谷子系统</h1>
            <p class="entry-subtitle">请选择登录入口</p>
          </header>
          <div class="entry-choices">
            <button class="entry-choice entry-choice--user" type="button" @click="navigate('/query')">
              <span class="entry-choice__title">普通用户入口</span>
              <span class="entry-choice__desc">查询订单、付款信息及账户安全设置</span>
              <span class="entry-choice__cta">进入 →</span>
            </button>
            <button class="entry-choice entry-choice--admin" type="button" @click="navigate('/admin')">
              <span class="entry-choice__title">管理员入口</span>
              <span class="entry-choice__desc">导入数据、管理用户、订单及付款信息</span>
              <span class="entry-choice__cta">进入 →</span>
            </button>
          </div>
        </div>
      </template>
    </main>
  </div>
</template>
