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
  const response = await fetch(endpoint(path), {
    credentials: 'include',
  })
  return parseResponse<T>(response)
}

export async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(endpoint(path), {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })
  return parseResponse<T>(response)
}

export async function patchJSON<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(endpoint(path), {
    method: 'PATCH',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })
  return parseResponse<T>(response)
}
export async function putJSON<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(endpoint(path), {
    method: 'PUT',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })
  return parseResponse<T>(response)
}

export async function deleteJSON<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(endpoint(path), {
    method: 'DELETE',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })
  return parseResponse<T>(response)
}
export async function postForm<T>(path: string, body: FormData): Promise<T> {
  const response = await fetch(endpoint(path), {
    method: 'POST',
    credentials: 'include',
    body,
  })
  return parseResponse<T>(response)
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

export type ImportHistoryResponse = {
  items: ImportHistoryItem[]
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

export type OrderListResponse = {
  items: OrderSummary[]
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

export type PaymentListResponse = {
  items: PaymentListItem[]
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
  display_name: string
  quantity: number
  unit_price: number
  amount: number
  paid_amount: number
  remaining_amount: number
  payment_status: string
}

export type QueryOrder = {
  order_no: string
  status: string
  project_name: string
  total_quantity: number
  total_amount: number
  paid_amount: number
  remaining_amount: number
  created_at: string
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

export type AdminUserListItem = {
  id: string
  cn_code: string
  display_name?: string
  has_query_code: boolean
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

export type AdminUserListResponse = {
  items: AdminUserListItem[]
  summary: AdminUserListSummary
}

export type AdminUserDetailOrderItem = {
  id: string
  product_name: string
  product_id?: string
  character_name?: string
  category?: string
  series_code?: string
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
