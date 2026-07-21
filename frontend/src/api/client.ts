const configuredApiBaseUrl = (import.meta.env.VITE_API_BASE_URL as string | undefined)?.replace(/\/$/, '') ?? ''
const apiBaseUrl = import.meta.env.DEV ? '' : configuredApiBaseUrl

export class ApiError extends Error {
  status: number
  retryAfterSeconds: number

  constructor(status: number, message: string, retryAfterSeconds = 0) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.retryAfterSeconds = retryAfterSeconds
  }
}

export type Admin = {
  id: string
  username: string
  display_name?: string
  role: string
  // must_change_password is set for a system-generated temporary password
  // (appointment or owner reset). While true the backend locks every admin
  // capability except identity/logout/reauth/password-change.
  must_change_password?: boolean
}

// roleDisplayName is the single source of truth for how a technical role value
// is shown to people. The database and API keep the technical values
// ('owner'/'admin'); the UI never renders those raw. Callers pass the surface
// they are in: an admin context shows 'admin' as 「管理员」, while the customer
// surface shows an account holder as 「用户」.
export function roleDisplayName(role: string | null | undefined): string {
  switch (role) {
    case 'owner':
      return '苏归'
    case 'admin':
      return '管理员'
    case 'user':
      return '用户'
    default:
      return '用户'
  }
}

export type AuthResponse = {
  admin: Admin
}

export type HealthResponse = {
  service: string
  status: string
  database?: string
  time: string
}

export type ModuleInfo = {
  key: string
  title: string
  status: 'ready' | 'queued' | 'draft'
  description: string
}

export type ConfigResponse = {
  name: string
  stage: string
  legacyAdminPort: string
  legacyUserPort: string
  frontendOrigins: string[]
  emailDeliveryEnabled: boolean
  modules: ModuleInfo[]
}

export type ImportIssue = {
  level: 'row_error' | 'fatal_error' | 'warning' | 'notice'
  code: string
  message: string
  sheet_name?: string
  batch_id?: string
  row_number?: number
  column?: string
}

export type ImportDetail = {
  id: string
  sheet_id: string
  sheet_name: string
  sheet_title?: string
  batch_name: string
  goods_series_name?: string
  product_category?: string
  series_code?: string
  group_name?: string
  display_name?: string
  character_name?: string
  category?: string
  series_name?: string
  item_name: string
  column_index: number
  column_name: string
  row_number: number
  original_cn: string
  normalized_cn: string
  quantity: number
  price_type: string
  unit_price: number
  amount: number
  table_row_amount: number
}

export type ImportBatch = {
  id: string
  sheet_id: string
  sheet_name: string
  sheet_title?: string
  batch_name: string
  template_type: string
  start_row: number
  end_row: number
  content_hash: string
  duplicate_in_file: boolean
  calculation_price_type: string
  price_types: Array<{
    type: string
    row: number
    unit_count: number
    values?: number[]
  }> | null
  cn_count: number
  item_type_count: number
  total_quantity: number
  table_amount: number
  calculated_amount: number
  difference: number
  details: ImportDetail[] | null
  errors: ImportIssue[] | null
  warnings: ImportIssue[] | null
  notices: ImportIssue[] | null
}

export type ImportPreviewResponse = {
  import_batch_id?: string
file: {
    original_filename: string
    sha256: string
    size_bytes: number
    sheet_count: number
    duplicate_file: boolean
    filename_conflict: boolean
  }
  sheets: Array<{
    id: string
    name: string
    title: string
    index: number
    template_type: string
    batch_count: number
    row_count: number
    column_count: number
    table_amount: number
    calculated_amount: number
    difference: number
  }>
  batches: ImportBatch[]
  errors: ImportIssue[] | null
  warnings: ImportIssue[] | null
  notices: ImportIssue[] | null
}

function endpoint(path: string) {
  return apiBaseUrl ? `${apiBaseUrl}${path}` : path
}

export function apiUrl(path: string) {
  return endpoint(path)
}

// The backend marks high-risk endpoints that need a fresh re-authentication
// with HTTP 403 and this exact error string. It rides on 403 (not 401) so the
// app's "session expired" handling never fires for it.
export const REAUTH_REQUIRED = 'reauth_required'

type ReauthHandler = () => Promise<boolean>
let reauthHandler: ReauthHandler | null = null

// setReauthHandler registers the app's re-authentication dialog. When a
// request is rejected with reauth_required, the handler runs (prompting for
// the password and calling /api/admin/reauth); a true result retries the
// original request once.
export function setReauthHandler(handler: ReauthHandler | null) {
  reauthHandler = handler
}

async function execute<T>(run: () => Promise<Response>): Promise<T> {
  try {
    return await parseResponse<T>(await run())
  } catch (error) {
    if (error instanceof ApiError && error.status === 403 && error.message === REAUTH_REQUIRED && reauthHandler) {
      const confirmed = await reauthHandler()
      if (confirmed) {
        return parseResponse<T>(await run())
      }
    }
    throw error
  }
}

async function parseResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let message = response.statusText
    let retryAfterSeconds = 0
    try {
      const payload = (await response.json()) as { error?: string; message?: string; retry_after_seconds?: number }
      message = payload.error ?? payload.message ?? message
      retryAfterSeconds = payload.retry_after_seconds ?? 0
    } catch {
      // Keep the status text when the backend did not return JSON.
    }
    throw new ApiError(response.status, message, retryAfterSeconds)
  }
  if (response.status === 204) {
    return undefined as T
  }
  return (await response.json()) as T
}

export async function getJSON<T>(path: string): Promise<T> {
  return execute<T>(() => fetch(endpoint(path), {
    credentials: 'include',
  }))
}

export async function postJSON<T>(path: string, body: unknown): Promise<T> {
  return execute<T>(() => fetch(endpoint(path), {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  }))
}

export async function patchJSON<T>(path: string, body: unknown): Promise<T> {
  return execute<T>(() => fetch(endpoint(path), {
    method: 'PATCH',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  }))
}
export async function putJSON<T>(path: string, body: unknown): Promise<T> {
  return execute<T>(() => fetch(endpoint(path), {
    method: 'PUT',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  }))
}

export async function deleteJSON<T>(path: string, body: unknown): Promise<T> {
  return execute<T>(() => fetch(endpoint(path), {
    method: 'DELETE',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  }))
}
export async function postForm<T>(path: string, body: FormData): Promise<T> {
  return execute<T>(() => fetch(endpoint(path), {
    method: 'POST',
    credentials: 'include',
    body,
  }))
}


export type CNExclusionRule = {
  sheet_id?: string
  batch_id?: string
  cn: string
}

export type CategoryCorrectionRule = {
  sheet_id?: string
  batch_id?: string
  detail_ids?: string[]
  item_ids?: string[]
  category: string
}

export type ConfirmRules = {
  excluded_sheet_ids?: string[]
  excluded_cns?: CNExclusionRule[]
  excluded_item_ids?: string[]
  category_rules?: CategoryCorrectionRule[]
}
export type ImportConfirmResponse = {
  import_batch_id: string
  project_id: string
  status: string
  cn_count: number
  product_count: number
  order_count: number
  order_item_count: number
  total_quantity: number
  total_amount: number
  warnings_accepted: boolean
  confirmed_at: string
  skipped_error_count: number
}

export type ImportRevokeResponse = {
  import_batch_id: string
  status: string
  affected_cn_count: number
  order_count: number
  order_item_count: number
  total_quantity: number
  total_amount: number
  revoked_by: string
  revoked_at: string
}

export type ImportHistoryItem = {
  id: string
  original_filename: string
  file_hash: string
  file_size: number
  sheet_count: number
  batch_count: number
  status: string
  uploaded_by?: string
  confirmed_by?: string
  revoked_by?: string
  created_at: string
  started_at?: string
  confirmed_at?: string
  completed_at?: string
  revoked_at?: string
  error_count: number
  warning_count: number
  notice_count: number
  warnings_accepted: boolean
  confirm_result?: ImportConfirmResponse
  revoke_result?: ImportRevokeResponse
}

// Counts are import records. total is every import matching the filters.
export type ImportHistoryResponse = {
  items: ImportHistoryItem[]
  page: number
  page_size: number
  total: number
  total_pages: number
}

// The import facets endpoint names its pager facet_page/facet_page_size; the
// loader adapts it into the ColumnFacetResponse the popover consumes.
export type ImportFacetResponse = {
  column: string
  values: ColumnFacetValue[]
  total: number
  blank_count: number
  facet_page: number
  facet_page_size: number
  total_pages: number
  has_more: boolean
}

export type ImportDetailResponse = {
  import: ImportHistoryItem
  preview?: ImportPreviewResponse
}
export type OrderSummary = {
  id: string
  order_no: string
  status: string
  cn_code: string
  display_name?: string
  project_id: string
  project_name: string
  item_type_count: number
  item_count: number
  total_quantity: number
  total_amount: number
  import_batch_ids: string[]
  import_filenames: string[]
  created_at: string
  updated_at: string
}

export type OrderItem = {
  id: string
  product_id: string
  product_name: string
  character_name?: string
  category?: string
  series_code?: string
  group_name?: string
  display_name?: string
  sku?: string
  quantity: number
  unit_price: number
  amount: number
  paid_amount: number
  remaining_amount: number
  payment_status: string
  import_batch_id?: string
  import_filename?: string
  source_sheet?: string
  source_row_key?: string
  created_at: string
}

export type OrderDetail = OrderSummary & {
  items: OrderItem[]
}

// OrderListItem is one row of the admin order table: a single goods line of a
// single CN's order — never a whole order.
//
// Every product field is single-valued. The list has no name/series/category/
// role arrays on purpose: an aggregated row cannot say which of its values
// matched a filter. An item with quantity 3 is one row whose quantity is 3.
//
// order_id is for navigating to the order's detail page and for keying rows —
// not a column to display. It carries no technical identifiers (import batch
// id, SKU, file hash); those stay in the detail page's technical section.
export type OrderListItem = {
  item_id: string
  order_id: string
  order_no: string
  status: string
  payment_status: string
  cn_code: string
  display_name?: string
  project_name: string
  item_name: string
  series_code: string
  group_name: string
  category: string
  character_name: string
  quantity: number
  unit_price: number
  total_amount: number
  paid_amount: number
  unpaid_amount: number
  created_at: string
}

// Counts are in detail rows: total is every goods line matching the filters,
// page_size is goods lines per page.
export type OrderListResponse = {
  items: OrderListItem[]
  page: number
  page_size: number
  total: number
  total_pages: number
}

// Shared by every WPS column filter (orders, users, …). Not order-specific.
export type ColumnFacetValue = {
  value: string
  label: string
  count: number
  blank: boolean
}

// The shape ColumnValueFilter consumes. Endpoints whose wire format names the
// pager differently (the user facets use facet_page/facet_page_size) adapt into
// this shape in their loader.
export type ColumnFacetResponse = {
  column: string
  values: ColumnFacetValue[]
  total: number
  blank_count: number
  page: number
  page_size: number
  has_more: boolean
}

export type OrderDetailResponse = {
  order: OrderDetail
}
export type PaymentSummary = {
  total_amount: number
  paid_amount: number
  remaining_amount: number
  item_count: number
  unpaid_count: number
  partial_count: number
  paid_count: number
}

export type PaymentItemRow = {
  id: string
  order_item_id: string
  order_id: string
  order_no: string
  project_name: string
  product_name: string
  product_id?: string
  character_name?: string
  category?: string
  series_code?: string
  group_name?: string
  display_name?: string
  sku?: string
  quantity: number
  unit_price: number
  amount: number
  paid_amount: number
  remaining_amount: number
  payment_status: string
  import_filename?: string
  source_sheet?: string
  source_row_key?: string
}

export type PaymentRecord = {
  id: string
  amount: number
  principal_amount?: number
  fee_amount?: number
  payable_amount?: number
  total_amount?: number
  payment_method?: string
  note?: string
  status: string
  paid_at: string
  created_by?: string
  created_at: string
  voided_at?: string
  voided_by?: string
  void_reason?: string
}

export type CNPaymentResponse = {
  user: QueryUser
  summary: PaymentSummary
  items: PaymentItemRow[]
  payments: PaymentRecord[]
}

export type CreatePaymentResponse = {
  payment_id: string
  status: string
  duplicate: boolean
  summary: PaymentSummary
  items: PaymentItemRow[]
}


export type PaymentListItem = {
  id: string
  cn_code: string
  display_name?: string
  amount: number
  principal_amount?: number
  fee_amount?: number
  payable_amount?: number
  total_amount?: number
  payment_method?: string
  status: string
  paid_at: string
  created_by?: string
  note?: string
  payment_item_count: number
  created_at: string
  voided_at?: string
  voided_by?: string
  void_reason?: string
}

// Counts are payment records. total is every payment matching the filters,
// never just this page.
export type PaymentListResponse = {
  items: PaymentListItem[]
  page: number
  page_size: number
  total: number
  total_pages: number
}

// The payment facets endpoint names its pager facet_page/facet_page_size; the
// loader adapts it into the ColumnFacetResponse the popover consumes.
export type PaymentFacetResponse = {
  column: string
  values: ColumnFacetValue[]
  total: number
  blank_count: number
  facet_page: number
  facet_page_size: number
  total_pages: number
  has_more: boolean
}

export type PaymentDetailItem = {
  id: string
  order_item_id: string
  order_id: string
  order_no: string
  project_name: string
  product_name: string
  product_id?: string
  character_name?: string
  category?: string
  series_code?: string
  group_name?: string
  display_name?: string
  sku?: string
  quantity: number
  unit_price: number
  amount: number
  paid_amount: number
  remaining_amount: number
  applied_amount: number
  payment_status: string
  import_filename?: string
  source_sheet?: string
  source_row_key?: string
}

export type PaymentDetail = PaymentListItem & {
  user_id: string
  items: PaymentDetailItem[]
}

export type PaymentDetailResponse = {
  payment: PaymentDetail
}

export type QueryUser = {
  cn_code: string
  display_name?: string
}

export type QueryLoginResponse = {
  user: QueryUser
}

export type ChangeQueryCodeRequest = {
  old_query_code: string
  new_query_code: string
  confirm_query_code: string
}

export type ChangeQueryCodeResponse = {
  message: string
}

export function changeQueryCode(request: ChangeQueryCodeRequest): Promise<ChangeQueryCodeResponse> {
  return postJSON<ChangeQueryCodeResponse>('/api/query/change-code', request)
}

export type BindQueryCodeRequest = {
  cn: string
  bind_token: string
  new_query_code: string
  confirm_query_code: string
}

export type BindQueryCodeResponse = {
  message: string
}

// First-time query code setup with an admin-issued one-time bind token.
// The token is submitted once and never persisted client-side.
export function bindQueryCode(request: BindQueryCodeRequest): Promise<BindQueryCodeResponse> {
  return postJSON<BindQueryCodeResponse>('/api/query/bind-code', request)
}
export type QueryCodeRecoveryRequestResponse = {
  success: boolean
  message: string
}

export type QueryCodeRecoveryVerifyResponse = {
  success: boolean
  message: string
  reset_token: string
  expires_at: string
}

export type QueryCodeRecoveryResetResponse = {
  success: boolean
  message: string
}

export function requestQueryCodeRecovery(cn: string): Promise<QueryCodeRecoveryRequestResponse> {
  return postJSON<QueryCodeRecoveryRequestResponse>('/api/query/recovery/request', { cn })
}

export function verifyQueryCodeRecovery(cn: string, code: string): Promise<QueryCodeRecoveryVerifyResponse> {
  return postJSON<QueryCodeRecoveryVerifyResponse>('/api/query/recovery/verify', { cn, code })
}

export function resetRecoveredQueryCode(resetToken: string, newQueryCode: string, confirmQueryCode: string): Promise<QueryCodeRecoveryResetResponse> {
  return postJSON<QueryCodeRecoveryResetResponse>('/api/query/recovery/reset', {
    reset_token: resetToken,
    new_query_code: newQueryCode,
    confirm_query_code: confirmQueryCode,
  })
}

export type BindTokenResponse = {
  bind_token: string
  expires_at: string
  message: string
}

// Admin-only: issue a one-time bind token for a user without a query code.
// The plaintext token exists only in this single response.
export function createQueryCodeBindToken(userID: string): Promise<BindTokenResponse> {
  return postJSON<BindTokenResponse>(`/api/admin/users/${encodeURIComponent(userID)}/query-code-bind-token`, {})
}

export type BulkBindTokenPreview = {
  eligible_count: number
  existing_unused_count: number
  max_batch_size: number
  valid_days: number
}

// Counts what a bulk bind-code issue would affect, without issuing anything.
// The caller shows these numbers for confirmation before the batch runs.
export function fetchBulkBindTokenPreview(params: URLSearchParams): Promise<BulkBindTokenPreview> {
  const suffix = params.toString() ? `?${params.toString()}` : ''
  return getJSON<BulkBindTokenPreview>(`/api/admin/users/bind-token-batch-preview${suffix}`)
}

export type RecoveryEmailState = {
  has_recovery_email: boolean
  status?: 'pending' | 'verified' | 'disabled'
  masked_email?: string
  verified_at?: string
  updated_at?: string
  message?: string
}

export function getAdminRecoveryEmail(userID: string): Promise<RecoveryEmailState> {
  return getJSON<RecoveryEmailState>(`/api/admin/users/${encodeURIComponent(userID)}/recovery-email`)
}

export function putAdminRecoveryEmail(userID: string, email: string, reason: string): Promise<RecoveryEmailState> {
  return putJSON<RecoveryEmailState>(`/api/admin/users/${encodeURIComponent(userID)}/recovery-email`, { email, reason })
}

export function deleteAdminRecoveryEmail(userID: string, reason: string): Promise<RecoveryEmailState> {
  return deleteJSON<RecoveryEmailState>(`/api/admin/users/${encodeURIComponent(userID)}/recovery-email`, { reason })
}

export function getQueryRecoveryEmail(): Promise<RecoveryEmailState> {
  return getJSON<RecoveryEmailState>('/api/query/recovery-email')
}

export type RecoveryEmailVerificationResponse = {
  success: boolean
  message: string
  status?: 'pending' | 'verified'
  masked_email?: string
  expires_at?: string
  verified_at?: string
  retry_after_seconds?: number
}

export function sendRecoveryEmailVerification(): Promise<RecoveryEmailVerificationResponse> {
  return postJSON<RecoveryEmailVerificationResponse>('/api/query/recovery-email/send-verification', {})
}

export function verifyRecoveryEmail(code: string): Promise<RecoveryEmailVerificationResponse> {
  return postJSON<RecoveryEmailVerificationResponse>('/api/query/recovery-email/verify', { code })
}
// QueryOrderItem is the regular-user-facing shape: no internal ids, no
// import batch/source-file tracking fields. Those exist only on the admin
// side (see the "技术标识" panels).
export type QueryOrderItem = {
  goods_name: string
  category?: string
  character_name?: string
  series_code?: string
  group_name?: string
  display_name: string
  quantity: number
  unit_price: number
  amount: number
  paid_amount: number
  remaining_amount: number
  payment_status: string
}

export type QueryOrder = {
  total_quantity: number
  total_amount: number
  paid_amount: number
  remaining_amount: number
  items: QueryOrderItem[]
}

// QueryPaymentItem is the regular-user-facing view of how one payment was
// split across order items: business fields only, no order numbers, project
// names, internal ids, import/source tracking, or admin/audit info.
// display_name is the composed business name and is always non-empty;
// amount is the item's own subtotal (小计); applied_amount is the portion of
// THIS payment allocated to the item (本次付款金额).
export type QueryPaymentItem = {
  display_name: string
  character_name?: string
  category?: string
  quantity: number
  unit_price: number
  amount: number
  applied_amount: number
  payment_status: string
}

export type QueryPaymentRecord = {
  principal_amount: number
  fee_amount: number
  total_amount: number
  payment_method?: string
  status: string
  paid_at: string
  items: QueryPaymentItem[]
}

export type QueryOrdersResponse = {
  user: QueryUser
  orders: QueryOrder[]
  payments: QueryPaymentRecord[]
  total_quantity: number
  total_amount: number
  paid_amount: number
  remaining_amount: number
}

// One row of the admin user table.
//
// The two account-security columns are booleans by construction: the backend
// reduces the query code to "is one set" and the recovery email to "is one
// bound", and never selects the hash, the ciphertext or the lookup hash. id is
// for keying rows and navigating to the detail page — never a column to render.
export type AdminUserListItem = {
  id: string
  cn_code: string
  display_name?: string
  has_query_code: boolean
  has_recovery_email: boolean
  status: string
  order_count: number
  total_amount: number
  paid_amount: number
  remaining_amount: number
  created_at: string
  query_code_updated_at?: string
  last_login_at?: string
}

export type AdminUserListSummary = {
  user_count: number
  users_with_orders: number
  total_amount: number
  paid_amount: number
  remaining_amount: number
}

// Counts are users. total is every user matching the filters, and summary
// aggregates over that same full set — never just this page.
export type AdminUserListResponse = {
  items: AdminUserListItem[]
  summary: AdminUserListSummary
  page: number
  page_size: number
  total: number
  total_pages: number
}

// The user facets endpoint names its pager facet_page/facet_page_size; the
// loader adapts it into the ColumnFacetResponse the popover consumes.
export type AdminUserFacetResponse = {
  column: string
  values: ColumnFacetValue[]
  total: number
  blank_count: number
  facet_page: number
  facet_page_size: number
  total_pages: number
  has_more: boolean
}

export type AdminUserDetailOrderItem = {
  id: string
  product_name: string
  product_id?: string
  character_name?: string
  category?: string
  series_code?: string
  group_name?: string
  display_name?: string
  sku?: string
  quantity: number
  unit_price: number
  amount: number
  paid_amount: number
  remaining_amount: number
  payment_status: string
  import_filename?: string
  source_sheet?: string
  source_row_key?: string
}

export type AdminUserDetailOrder = {
  id: string
  order_no: string
  status: string
  project_name: string
  item_count: number
  total_amount: number
  paid_amount: number
  remaining_amount: number
  created_at: string
  items: AdminUserDetailOrderItem[]
}

export type AdminUserDetailPayment = {
  id: string
  principal_amount: number
  fee_amount: number
  total_amount: number
  payment_method?: string
  status: string
  paid_at: string
  created_by?: string
  voided_at?: string
  voided_by?: string
  void_reason?: string
}

export type AdminUserDetailResponse = {
  user: AdminUserListItem
  orders: AdminUserDetailOrder[]
  payments: AdminUserDetailPayment[]
  import_filenames: string[]
  merges: AdminUserMergeLogEntry[]
  // Bind-token status only — the token plaintext or hash never appears here.
  has_active_bind_token: boolean
  bind_token_expires_at?: string
}

export type AdminUserMergeLogEntry = {
  id: string
  direction: 'merged_into' | 'absorbed'
  other_cn: string
  reason: string
  merged_by?: string
  merged_at: string
}

export type AdminUserMergePreviewResponse = {
  source: AdminUserListItem
  target: AdminUserListItem
  move_order_count: number
  move_payment_count: number
  move_query_session_count: number
}

export type AdminUserMergeResponse = {
  source_user_id: string
  target_user_id: string
  moved_order_count: number
  moved_payment_count: number
  merged_at: string
}

// --- Payment collection QR codes ---
// Static QR codes (Alipay / WeChat) that admins configure and logged-in users
// scan. Image bytes are never carried in JSON: admins/users load the image via
// the dedicated image endpoints (see apiUrl) so bytes stay out of app state.
export type PaymentQRMethod = 'alipay' | 'wechat'

// Admin-only technical status for one method.
export type PaymentQRAdminStatus = {
  payment_method: PaymentQRMethod
  configured: boolean
  mime_type?: string
  byte_size?: number
  sha256?: string
  updated_at?: string
  updated_by?: string
}

export type PaymentQRAdminStatusResponse = {
  items: PaymentQRAdminStatus[]
}

// Regular-user view: only whether a method is usable. No technical fields.
export type PaymentQRAvailability = {
  payment_method: PaymentQRMethod
  available: boolean
}

export type PaymentQRAvailabilityResponse = {
  items: PaymentQRAvailability[]
}

export function getPaymentQRAdminStatuses(): Promise<PaymentQRAdminStatusResponse> {
  return getJSON<PaymentQRAdminStatusResponse>('/api/admin/payment-qr')
}

export function uploadPaymentQR(method: PaymentQRMethod, form: FormData): Promise<PaymentQRAdminStatusResponse> {
  return postForm<PaymentQRAdminStatusResponse>(`/api/admin/payment-qr/${method}`, form)
}

export function disablePaymentQR(method: PaymentQRMethod): Promise<PaymentQRAdminStatusResponse> {
  return postJSON<PaymentQRAdminStatusResponse>(`/api/admin/payment-qr/${method}/disable`, {})
}

export function getPaymentQRAvailability(): Promise<PaymentQRAvailabilityResponse> {
  return getJSON<PaymentQRAvailabilityResponse>('/api/query/payment-qr')
}

// --- Payment proof submissions ("收肾记录") ---
// A logged-in user uploads a screenshot of a completed payment. A submission is
// EVIDENCE ONLY: it never changes any paid amount by itself. Image bytes are
// never carried in JSON; the image is loaded via the dedicated image endpoints
// (see apiUrl) which set nosniff. The regular-user shape carries no sha, no
// internal ids, no admin identity, no paths — only the user's own history.
export type PaymentSubmissionMethod = 'alipay' | 'wechat'
export type PaymentSubmissionStatus = 'submitted' | 'approved' | 'rejected'

export type UserPaymentSubmission = {
  id: string
  payment_method: string
  principal_amount: number
  fee_amount: number
  payable_amount: number
  status: PaymentSubmissionStatus
  submitted_at: string
  reviewed_at?: string
  reject_reason?: string
  // Set when the server replayed an existing submission because the request
  // carried an already-seen idempotency key: the retry landed, but no second
  // record was created.
  deduplicated?: boolean
}

export type UserPaymentSubmissionListResponse = { items: UserPaymentSubmission[] }
export type UserPaymentSubmissionCreateResponse = { submission: UserPaymentSubmission }

export function listUserPaymentSubmissions(): Promise<UserPaymentSubmissionListResponse> {
  return getJSON<UserPaymentSubmissionListResponse>('/api/query/payment-submissions')
}

// The proof upload deliberately has NO helper here. It goes through
// src/api/upload.ts (XMLHttpRequest) because it needs upload progress, a
// timeout and cancellation, none of which fetch can provide - and a 1.25 MB
// proof takes ~10 s on a phone uplink, so silent waiting is not acceptable.
// UserPaymentSubmissionCreateResponse below is the shape that endpoint returns.

// One row of the admin proof table. Business fields only — the sha, byte size,
// mime type, internal user id and linked payment id belong to the detail view's
// collapsed 技术标识 section (see AdminPaymentSubmissionDetail), never the table.
export type AdminPaymentSubmissionListItem = {
  id: string
  cn_code: string
  display_name?: string
  payment_method: string
  principal_amount: number
  fee_amount: number
  payable_amount: number
  status: PaymentSubmissionStatus
  submitted_at: string
  reviewed_at?: string
  reviewed_by?: string
  reject_reason?: string
}

// Counts are submissions. total is every proof matching the filters, not the page.
export type AdminPaymentSubmissionListResponse = {
  items: AdminPaymentSubmissionListItem[]
  page: number
  page_size: number
  total: number
  total_pages: number
}

export type AdminPaymentSubmissionDetail = AdminPaymentSubmissionListItem & {
  mime_type: string
  byte_size: number
  sha256: string
  original_filename_safe?: string
  user_id: string
  linked_payment_id?: string
}

export type AdminPaymentSubmissionDetailResponse = { submission: AdminPaymentSubmissionDetail }

// The proof facets endpoint names its pager facet_page/facet_page_size; the
// loader adapts it into the ColumnFacetResponse the popover consumes.
export type PaymentSubmissionFacetResponse = {
  column: string
  values: ColumnFacetValue[]
  total: number
  blank_count: number
  facet_page: number
  facet_page_size: number
  total_pages: number
  has_more: boolean
}

export function listAdminPaymentSubmissions(query: string): Promise<AdminPaymentSubmissionListResponse> {
  const suffix = query ? `?${query}` : ''
  return getJSON<AdminPaymentSubmissionListResponse>(`/api/admin/payment-submissions${suffix}`)
}

export function getAdminPaymentSubmissionFacets(query: string): Promise<PaymentSubmissionFacetResponse> {
  return getJSON<PaymentSubmissionFacetResponse>(`/api/admin/payment-submissions/facets?${query}`)
}

export function getAdminPaymentSubmissionDetail(id: string): Promise<AdminPaymentSubmissionDetailResponse> {
  return getJSON<AdminPaymentSubmissionDetailResponse>(`/api/admin/payment-submissions/${encodeURIComponent(id)}`)
}

export function rejectPaymentSubmission(id: string, reason: string): Promise<AdminPaymentSubmissionDetailResponse> {
  return postJSON<AdminPaymentSubmissionDetailResponse>(`/api/admin/payment-submissions/${encodeURIComponent(id)}/reject`, { reason })
}

export type ApprovePaymentSubmissionRequest = {
  items: Array<{ order_item_id: string; amount: number }>
  paid_at?: string
  note?: string
}

export function approvePaymentSubmission(id: string, request: ApprovePaymentSubmissionRequest): Promise<AdminPaymentSubmissionDetailResponse> {
  return postJSON<AdminPaymentSubmissionDetailResponse>(`/api/admin/payment-submissions/${encodeURIComponent(id)}/approve`, request)
}

// --- Admin owner/security (stage 2H-2B) ---

export type AdminSecurityRecoveryEmail = {
  storage_configured: boolean
  delivery_enabled: boolean
  has_recovery_email: boolean
  status?: string
  masked_email?: string
  verified_at?: string
}

export type AdminAuditEvent = {
  event_type: string
  occurred_at: string
  client_ip: string
  result: string
  reason_code: string
}

export type OwnerRecoveryCodesStatus = {
  enabled: boolean
  remaining_codes: number
  generated_at?: string
}

export type OwnerRecoveryCodesGenerated = {
  codes: string[]
  generated_at: string
}

export function adminReauth(password: string): Promise<void> {
  return postJSON<void>('/api/admin/reauth', { password })
}

export function changeAdminPassword(currentPassword: string, newPassword: string): Promise<void> {
  return postJSON<void>('/api/admin/security/password', {
    current_password: currentPassword,
    new_password: newPassword,
  })
}

export function getAdminSecurityRecoveryEmail(): Promise<AdminSecurityRecoveryEmail> {
  return getJSON<AdminSecurityRecoveryEmail>('/api/admin/security/recovery-email')
}

export function requestAdminRecoveryEmailBind(email: string): Promise<{ masked_email: string }> {
  return postJSON<{ masked_email: string }>('/api/admin/security/recovery-email/request', { email })
}

export function confirmAdminRecoveryEmailBind(code: string): Promise<void> {
  return postJSON<void>('/api/admin/security/recovery-email/confirm', { code })
}

export function getAdminAuditSummary(): Promise<{ events: AdminAuditEvent[] }> {
  return getJSON<{ events: AdminAuditEvent[] }>('/api/admin/security/audit-summary')
}

export function getOwnerRecoveryCodesStatus(): Promise<OwnerRecoveryCodesStatus> {
  return getJSON<OwnerRecoveryCodesStatus>('/api/admin/owner/recovery-codes')
}

export function generateOwnerRecoveryCodes(): Promise<OwnerRecoveryCodesGenerated> {
  return postJSON<OwnerRecoveryCodesGenerated>('/api/admin/owner/recovery-codes', {})
}

export function adminRecoveryCodeReset(username: string, recoveryCode: string, newPassword: string): Promise<void> {
  return postJSON<void>('/api/admin/recovery/code-reset', {
    username,
    recovery_code: recoveryCode,
    new_password: newPassword,
  })
}

// ===== Owner-managed administrator accounts (系统所有者 = 苏归) =====
//
// Every call below hits an owner-only endpoint (RequireAuthentication +
// RequireOwner). The mutations additionally require a fresh re-authentication;
// the shared execute() wrapper transparently opens the reauth dialog and
// retries once on reauth_required, so callers never handle that themselves.

export type ManagedAdmin = {
  id: string
  username: string
  display_name?: string
  role: string
  status: string
  user_id?: string
  user_cn?: string
  must_change_password: boolean
  created_at: string
  last_login_at?: string
  revoked_at?: string
}

export type ManagedAdminListResponse = {
  admins: ManagedAdmin[]
}

// ManagedAdminResponse carries the row after a mutation. temp_password is
// present exactly once — on appointment and on password reset — and is never
// persisted anywhere by the client.
export type ManagedAdminResponse = {
  admin: ManagedAdmin
  temp_password?: string
}

export type AppointAdminRequest = {
  user_id: string
  username: string
  display_name?: string
}

export function listOwnerAdmins(): Promise<ManagedAdminListResponse> {
  return getJSON<ManagedAdminListResponse>('/api/admin/owner/admins')
}

export function getOwnerAdmin(id: string): Promise<ManagedAdminResponse> {
  return getJSON<ManagedAdminResponse>(`/api/admin/owner/admins/${encodeURIComponent(id)}`)
}

export function appointOwnerAdmin(request: AppointAdminRequest): Promise<ManagedAdminResponse> {
  return postJSON<ManagedAdminResponse>('/api/admin/owner/admins', request)
}

export function enableOwnerAdmin(id: string, reason: string): Promise<ManagedAdminResponse> {
  return postJSON<ManagedAdminResponse>(`/api/admin/owner/admins/${encodeURIComponent(id)}/enable`, { reason })
}

export function disableOwnerAdmin(id: string, reason: string): Promise<ManagedAdminResponse> {
  return postJSON<ManagedAdminResponse>(`/api/admin/owner/admins/${encodeURIComponent(id)}/disable`, { reason })
}

export function revokeOwnerAdmin(id: string, reason: string): Promise<ManagedAdminResponse> {
  return postJSON<ManagedAdminResponse>(`/api/admin/owner/admins/${encodeURIComponent(id)}/revoke`, { reason })
}

export function resetOwnerAdminPassword(id: string, reason: string): Promise<ManagedAdminResponse> {
  return postJSON<ManagedAdminResponse>(`/api/admin/owner/admins/${encodeURIComponent(id)}/reset-password`, { reason })
}

export function getOwnerAdminAudit(id: string): Promise<{ events: AdminAuditEvent[] }> {
  return getJSON<{ events: AdminAuditEvent[] }>(`/api/admin/owner/admins/${encodeURIComponent(id)}/audit`)
}
