export interface Supplier {
  id: string
  name: string
  portal_status: 'active' | 'disabled'
  version: number
}

export interface Identity {
  user: { id: string; email: string; role: string; status: string }
  supplier: Supplier
  csrf_token: string
}

export interface LoginInput {
  email: string
  password: string
  turnstile_token?: string
  tencent_captcha_ticket?: string
  tencent_captcha_randstr?: string
}

export type LoginResult =
  | { requires_2fa: false }
  | { requires_2fa: true; challenge_id: string; expires_at: string }

export interface SupplierAccount {
  id: string
  name: string
  admission_state: 'provisioning' | 'identity_pending' | 'pending' | 'approved' | 'rejected' | 'blocked' | 'deleting' | 'deleted' | 'needs_attention'
  desired_state: 'paused' | 'running'
  observed_at: string | null
  version: number
}
export interface Page<T> { items: T[]; total: number; page: number; page_size: number; pages: number }
export interface Authorization { id: string; authorization_url: string; expires_at: string }
export interface AccountOperation {
  id: string
  action: string
  state: 'running' | 'pending' | 'succeeded' | 'failed' | 'needs_attention'
  step: string
  account_id?: string
}

export interface PortalIdentity {
 principal: {kind: 'supplier' | 'admin'; id: string; login_account?: string; email?: string}
 supplier?: Supplier
 csrf_token: string
}
export interface ManagedSupplier extends Supplier {
 login_account: string
 unsettled_usd: string
 settled_usd: string
 total_consumption_usd: string
}

export interface OwnProxy { id: string; name: string; address: string; region: string; status: 'provisioning' | 'active' | 'disabled' | 'needs_attention' | 'deleting'; capacity: number; accounts: number; held: number; version: number }
export interface ProxyInput { name: string; address: string; region: string }
export interface AccountGroup { id: number; name: string; platform: string; status: string; group_ids?: number[] }
export interface OnboardingOptions { groups: AccountGroup[]; proxies: OwnProxy[] }

export type UsagePeriod = 'today' | '7d' | '30d' | 'all'
export interface UsageMetrics {
  record_count: string; input_tokens: string; output_tokens: string
  cache_read_tokens: string; cache_creation_tokens: string
  tokens: string; cache_tokens: string; total_cost_usd: string
}
export interface UsageAccount { id: string; name: string; archived: boolean }
export interface UsageReport {
  period: UsagePeriod; timezone: 'Asia/Shanghai'; account_id: string; start: string; end: string; observed_at: string
  summary: UsageMetrics; status: 'complete' | 'partial'; request_attempts: null
  total: number; page: number; page_size: number; pages: number
  account_options: UsageAccount[]
  daily: (UsageMetrics & { date: string })[]
  accounts: (UsageMetrics & UsageAccount & { sync_state: 'complete' | 'partial'; synced_at: string | null })[]
}
