const apiBaseUrl = (import.meta.env.VITE_API_BASE_URL as string | undefined)?.replace(/\/$/, '') ?? ''

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
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

async function parseResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let message = response.statusText
    try {
      const payload = (await response.json()) as { error?: string }
      message = payload.error ?? message
    } catch {
      // Keep the status text when the backend did not return JSON.
    }
    throw new ApiError(response.status, message)
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
  character_name?: string
  category?: string
  series_code?: string
  display_name?: string
  sku?: string
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
  id: string
  cn_code: string
  display_name?: string
}

export type QueryLoginResponse = {
  user: QueryUser
}

export type QueryOrderItem = {
  id: string
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
  import_batch_id?: string
  import_filename?: string
  source_sheet?: string
}

export type QueryOrder = {
  id: string
  order_no: string
  status: string
  project_name: string
  total_quantity: number
  total_amount: number
  paid_amount: number
  remaining_amount: number
  created_at: string
  import_filenames: string[]
  items: QueryOrderItem[]
}

export type QueryPaymentRecord = {
  id: string
  principal_amount: number
  fee_amount: number
  total_amount: number
  payment_method?: string
  status: string
  paid_at: string
  voided_at?: string
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
