/** Supplier module types (mirror backend/internal/supplier/model.go). */

export type SupplierStatus = 'active' | 'disabled'
export type ReviewStatus = 'pending' | 'approved' | 'rejected'
export type SettlementStatus = 'draft' | 'confirmed' | 'paid' | 'void'
export type AuthMode = 'oauth' | 'setup-token'
export type ModelFamily = 'opus' | 'fable' | 'sonnet' | 'haiku' | 'other'

export interface Supplier {
  id: number
  user_id: number
  user_email?: string
  name: string
  status: SupplierStatus
  default_concurrency: number
  max_accounts: number
  notes: string
  account_count: number
  proxy_count: number
  created_at: string
  updated_at: string
}

export interface SupplierAccount {
  id: number
  name: string
  platform: string
  type: string
  email?: string
  status: string
  schedulable: boolean
  error_message?: string
  proxy_id?: number | null
  concurrency: number
  review_status: ReviewStatus
  review_note?: string
  rate_limit_reset_at?: string
  overload_until?: string
  temp_unschedulable_until?: string
  temp_unschedulable_reason?: string
  session_window_end?: string
  session_window_status?: string
  expires_at?: string
  last_used_at?: string
  created_at: string
}

export interface AdminSupplierAccount extends SupplierAccount {
  supplier_id: number
  priority: number
  rate_multiplier?: number
  group_ids: number[]
}

export interface SupplierProxy {
  id: number
  name: string
  protocol: string
  host: string
  port: number
  username?: string
  has_password: boolean
  status: string
  expires_at?: string
  account_count: number
  created_at: string
}

export interface ProxyPayload {
  name?: string
  protocol?: string
  host?: string
  port?: number
  username?: string
  password?: string
}

export interface ProxyTestResult {
  success: boolean
  message: string
  latency_ms?: number
  ip_address?: string
  country?: string
  city?: string
}

export interface UsageTotals {
  request_count: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  amount: number
}

export interface UsageRow extends UsageTotals {
  date?: string
  account_id?: number
  account_name?: string
  model?: string
  family?: ModelFamily
}

export interface UsageSummary {
  start: string
  end: string
  totals: UsageTotals
  by_family: UsageRow[]
  by_model: UsageRow[]
}

export interface UsageParams {
  start?: string
  end?: string
  account_id?: number
  model?: string
}

export interface SettlementItem extends UsageTotals {
  account_id: number
  account_name: string
  model: string
  family: ModelFamily
}

export interface Settlement extends UsageTotals {
  id: number
  supplier_id: number
  supplier_name?: string
  period_start: string
  period_end: string
  status: SettlementStatus
  notes: string
  confirmed_at?: string
  paid_at?: string
  voided_at?: string
  created_at: string
  updated_at: string
  items?: SettlementItem[]
}

export interface AuthUrlResult {
  auth_url: string
  session_id: string
}

export interface Paginated<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  pages: number
}
