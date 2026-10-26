import { SupplierClient, ApiError, type RequestOptions } from '../../../../extensions/supplier/frontend/src/api/client'
import { apiClient } from '@/api/client'
import { useAdminComplianceStore } from '@/stores/adminCompliance'

export interface ManagedSupplier {
  id: string
  name: string
  login_account: string
  portal_status: 'active' | 'disabled'
  version: number
  unsettled_usd: string
  settled_usd: string
  total_consumption_usd: string
}
export interface SupplierAccount {
  id: string
  name: string
  admission_state: string
  desired_state: 'paused' | 'running'
  version: number
}
export interface SupplierPage<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  pages: number
}

const base = '/api/supplier/admin/v1'
export interface SupplierGroupAccess { group_id: number; enabled: boolean; version: number; supplier_group_name?: string }
export const supplierGroupAPI = {
  list: () => request<{ items: SupplierGroupAccess[] }>('GET', '/groups/access'),
  get: (id: number) => request<SupplierGroupAccess>('GET', `/groups/${id}/access`),
  set: (id: number, enabled: boolean, version: number, supplierGroupName?: string) => request<SupplierGroupAccess>('PUT', `/groups/${id}/access`, { enabled, ...(supplierGroupName !== undefined ? { supplier_group_name: supplierGroupName } : {}) }, undefined, version)
}
async function request<T>(method: 'GET' | 'POST' | 'PUT' | 'DELETE', path: string, data?: unknown, key?: string, version?: number): Promise<T> {
  try {
    const response = await apiClient.request<T>({
      method, baseURL: '/', url: base + path, data,
      // Main GET interceptor adds timezone. Supplier periods have a fixed accounting
      // timezone and reject unknown query keys, so adapt that main-client default here.
      paramsSerializer: { serialize: params => {
        const query = new URLSearchParams()
        for (const [name, value] of Object.entries(params)) {
          if (name !== 'timezone' && value !== undefined && value !== null) query.append(name, String(value))
        }
        return query.toString()
      } },
      headers: {
        ...(key ? { 'Idempotency-Key': key } : {}),
        ...(version !== undefined ? { 'If-Match': String(version) } : {})
      }
    })
    return response.data
  } catch (error) {
    const failure = error as { status?: number; reason?: string }
    if (failure.status === 423 && failure.reason === 'ADMIN_COMPLIANCE_ACK_REQUIRED') {
      useAdminComplianceStore().requireAcknowledgement()
    }
    throw error
  }
}

export const supplierAdminAPI = {
  list: (search = '', page = 1) => request<SupplierPage<ManagedSupplier>>('GET', `/suppliers?${new URLSearchParams({ search, page: String(page), page_size: '20' })}`),
  detail: (id: string) => request<ManagedSupplier>('GET', `/suppliers/${encodeURIComponent(id)}`),
  create: (input: { name: string; login_account: string; initial_password: string }, key: string) => request<ManagedSupplier>('POST', '/suppliers', input, key),
  setStatus: (supplier: ManagedSupplier, enabled: boolean, key: string) => request<ManagedSupplier>('PUT', `/suppliers/${encodeURIComponent(supplier.id)}/status`, { enabled }, key, supplier.version),
  resetPassword: (supplier: ManagedSupplier, password: string, key: string) => request<ManagedSupplier>('POST', `/suppliers/${encodeURIComponent(supplier.id)}/credential-reset`, { new_password: password }, key, supplier.version),
  accounts: (id: string, page = 1) => request<SupplierPage<SupplierAccount>>('GET', `/suppliers/${encodeURIComponent(id)}/accounts?page=${page}&page_size=20`)
}

export interface SettlementInput { amount_usd: string; payment_method: string; reference: string; remark: string }
export interface Settlement extends SettlementInput {
  id: string; ledger_id: string; supplier_id: string; supplier_name: string; login_account: string
  before_usd: string; after_usd: string; settled_usd: string; actor_id: number; created_at: string
}
export interface FinanceReport extends SupplierPage<Settlement> {
  overview: { unsettled_usd: string; settled_usd: string; settlement_count: number; accrual_status: string }
}
export const supplierFinanceAPI = {
  ledger: (supplier = '', search = '', page = 1) => request<FinanceReport>('GET', `/finance/ledger?${new URLSearchParams({supplier_id:supplier,search,page:String(page),page_size:'20'})}`),
  detail: (id: string) => request<Settlement>('GET', `/finance/settlements/${encodeURIComponent(id)}`),
  settle: (id: string, input: SettlementInput, key: string) => request<Settlement>('POST', `/suppliers/${encodeURIComponent(id)}/settlements`, input, key)
}
// One immutable supplier scope; callers cannot redirect the main authenticated transport.
export function supplierResourceClient(id: string): SupplierClient {
  if (!/^[a-f0-9-]{36}$/i.test(id)) throw new ApiError(404,'RESOURCE_NOT_FOUND')
  const prefix = `/suppliers/${encodeURIComponent(id)}`
  return new SupplierClient(() => {}, undefined, 'supplier', async <T>(path: string, options: RequestOptions) => {
    if (!/^\/(accounts(?:\/overview$|\?|$)|usage\?|proxies(?:\/|$)|onboarding\/options$|oauth\/sessions(?:\/|$)|operations\/)/.test(path)) throw new ApiError(403,'ACCESS_DENIED')
    try { return await request<T>(options.method || 'GET', prefix + path, options.body, options.idempotencyKey, options.version) }
    catch (cause) { const e = cause as {status?:number;reason?:string;request_id?:string}; throw new ApiError(e.status || 0,e.reason || 'REQUEST_FAILED',e.request_id) }
  })
}
