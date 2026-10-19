import { apiClient } from '@/api/client'
import type {
  AdminSupplierAccount,
  AuthMode,
  AuthUrlResult,
  Paginated,
  ProxyPayload,
  ProxyTestResult,
  ReviewStatus,
  Settlement,
  SettlementStatus,
  Supplier,
  SupplierAccount,
  SupplierProxy,
  UsageParams,
  UsageRow,
  UsageSummary,
} from './types'

// ---------------------------------------------------------------------------
// Supplier console (/supplier)
// ---------------------------------------------------------------------------

export async function getMe(): Promise<Supplier> {
  const { data } = await apiClient.get<Supplier>('/supplier/me')
  return data
}

export async function listProxies(): Promise<SupplierProxy[]> {
  const { data } = await apiClient.get<SupplierProxy[]>('/supplier/proxies')
  return data
}

export async function createProxy(payload: ProxyPayload): Promise<SupplierProxy> {
  const { data } = await apiClient.post<SupplierProxy>('/supplier/proxies', payload)
  return data
}

export async function updateProxy(id: number, payload: ProxyPayload): Promise<SupplierProxy> {
  const { data } = await apiClient.put<SupplierProxy>(`/supplier/proxies/${id}`, payload)
  return data
}

export async function deleteProxy(id: number): Promise<void> {
  await apiClient.delete(`/supplier/proxies/${id}`)
}

export async function testProxy(id: number): Promise<ProxyTestResult> {
  const { data } = await apiClient.post<ProxyTestResult>(`/supplier/proxies/${id}/test`)
  return data
}

export async function listAccounts(): Promise<SupplierAccount[]> {
  const { data } = await apiClient.get<SupplierAccount[]>('/supplier/accounts')
  return data
}

export async function generateAuthUrl(mode: AuthMode, proxyId?: number | null): Promise<AuthUrlResult> {
  const { data } = await apiClient.post<AuthUrlResult>('/supplier/accounts/auth-url', {
    mode,
    proxy_id: proxyId || undefined,
  })
  return data
}

export interface ExchangeCodePayload {
  session_id: string
  code: string
  name?: string
  proxy_id?: number | null
  account_id?: number
}

export async function exchangeCode(payload: ExchangeCodePayload): Promise<SupplierAccount> {
  const { data } = await apiClient.post<SupplierAccount>('/supplier/accounts/exchange-code', {
    ...payload,
    proxy_id: payload.proxy_id || undefined,
  })
  return data
}

export interface CookieAuthPayload {
  mode: AuthMode
  session_key: string
  name?: string
  proxy_id?: number | null
  account_id?: number
}

export async function cookieAuth(payload: CookieAuthPayload): Promise<SupplierAccount> {
  const { data } = await apiClient.post<SupplierAccount>('/supplier/accounts/cookie-auth', {
    ...payload,
    proxy_id: payload.proxy_id || undefined,
  })
  return data
}

export async function updateAccount(
  id: number,
  payload: { name?: string; proxy_id?: number | null; clear_proxy?: boolean },
): Promise<SupplierAccount> {
  const { data } = await apiClient.put<SupplierAccount>(`/supplier/accounts/${id}`, payload)
  return data
}

export async function deleteAccount(id: number): Promise<void> {
  await apiClient.delete(`/supplier/accounts/${id}`)
}

export async function getUsageSummary(params: UsageParams): Promise<UsageSummary> {
  const { data } = await apiClient.get<UsageSummary>('/supplier/usage/summary', { params })
  return data
}

export async function getUsageDaily(params: UsageParams): Promise<UsageRow[]> {
  const { data } = await apiClient.get<UsageRow[]>('/supplier/usage/daily', { params })
  return data
}

export async function getUsageByAccount(params: UsageParams): Promise<UsageRow[]> {
  const { data } = await apiClient.get<UsageRow[]>('/supplier/usage/accounts', { params })
  return data
}

export async function listSettlements(page = 1, pageSize = 20): Promise<Paginated<Settlement>> {
  const { data } = await apiClient.get<Paginated<Settlement>>('/supplier/settlements', {
    params: { page, page_size: pageSize },
  })
  return data
}

export async function getSettlement(id: number): Promise<Settlement> {
  const { data } = await apiClient.get<Settlement>(`/supplier/settlements/${id}`)
  return data
}

// ---------------------------------------------------------------------------
// Admin (/admin/suppliers, /admin/supplier-accounts, /admin/supplier-settlements)
// ---------------------------------------------------------------------------

export interface SupplierPayload {
  user_id?: number
  email?: string
  name: string
  status?: string
  default_concurrency: number
  max_accounts: number
  notes?: string
}

export const adminSupplierAPI = {
  async list(page = 1, pageSize = 20, search = ''): Promise<Paginated<Supplier>> {
    const { data } = await apiClient.get<Paginated<Supplier>>('/admin/suppliers', {
      params: { page, page_size: pageSize, search: search || undefined },
    })
    return data
  },

  async create(payload: SupplierPayload): Promise<Supplier> {
    const { data } = await apiClient.post<Supplier>('/admin/suppliers', payload)
    return data
  },

  async update(id: number, payload: SupplierPayload): Promise<Supplier> {
    const { data } = await apiClient.put<Supplier>(`/admin/suppliers/${id}`, payload)
    return data
  },

  async attachAccounts(id: number, accountIds: number[]): Promise<void> {
    await apiClient.post(`/admin/suppliers/${id}/attach-accounts`, { account_ids: accountIds })
  },

  async usageSummary(id: number, params: UsageParams): Promise<UsageSummary> {
    const { data } = await apiClient.get<UsageSummary>(`/admin/suppliers/${id}/usage/summary`, { params })
    return data
  },

  async listAccounts(supplierId?: number, reviewStatus?: ReviewStatus | ''): Promise<AdminSupplierAccount[]> {
    const { data } = await apiClient.get<AdminSupplierAccount[]>('/admin/supplier-accounts', {
      params: { supplier_id: supplierId || undefined, review_status: reviewStatus || undefined },
    })
    return data
  },

  async review(
    accountId: number,
    payload: {
      action: 'approve' | 'reject'
      note?: string
      group_ids?: number[]
      concurrency?: number
      priority?: number
      rate_multiplier?: number
    },
  ): Promise<void> {
    await apiClient.post(`/admin/supplier-accounts/${accountId}/review`, payload)
  },

  async listSettlements(
    page = 1,
    pageSize = 20,
    supplierId?: number,
    status?: SettlementStatus | '',
  ): Promise<Paginated<Settlement>> {
    const { data } = await apiClient.get<Paginated<Settlement>>('/admin/supplier-settlements', {
      params: { page, page_size: pageSize, supplier_id: supplierId || undefined, status: status || undefined },
    })
    return data
  },

  async getSettlement(id: number): Promise<Settlement> {
    const { data } = await apiClient.get<Settlement>(`/admin/supplier-settlements/${id}`)
    return data
  },

  async previewSettlement(payload: { supplier_id: number; period_start: string; period_end: string }): Promise<Settlement> {
    const { data } = await apiClient.post<Settlement>('/admin/supplier-settlements/preview', payload)
    return data
  },

  async createSettlement(payload: {
    supplier_id: number
    period_start: string
    period_end: string
    notes?: string
  }): Promise<Settlement> {
    const { data } = await apiClient.post<Settlement>('/admin/supplier-settlements', payload)
    return data
  },

  async transitionSettlement(id: number, action: 'confirm' | 'mark-paid' | 'void'): Promise<Settlement> {
    const { data } = await apiClient.post<Settlement>(`/admin/supplier-settlements/${id}/${action}`)
    return data
  },
}
