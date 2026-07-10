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
  level: 'error' | 'warning' | 'notice'
  code: string
  message: string
  sheet_name?: string
  batch_id?: string
  row_number?: number
  column?: string
}

export type ImportDetail = {
  sheet_name: string
  batch_name: string
  category?: string
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
  sheet_name: string
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
  file: {
    original_filename: string
    sha256: string
    size_bytes: number
    sheet_count: number
    duplicate_file: boolean
    filename_conflict: boolean
  }
  sheets: Array<{
    name: string
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
