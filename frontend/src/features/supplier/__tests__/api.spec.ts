import { beforeEach, describe, expect, it, vi } from 'vitest'

const client = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn(), delete: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))

import * as supplierAPI from '../api'
import { adminSupplierAPI } from '../api'

describe('supplier API', () => {
  beforeEach(() => Object.values(client).forEach((mock) => mock.mockReset()))

  it('keeps the console under /supplier', async () => {
    client.get.mockResolvedValue({ data: [] })
    await supplierAPI.listAccounts()
    await supplierAPI.listProxies()
    await supplierAPI.getUsageSummary({ start: '2026-10-01', end: '2026-10-07', account_id: 3 })
    expect(client.get.mock.calls.map((c) => c[0])).toEqual([
      '/supplier/accounts',
      '/supplier/proxies',
      '/supplier/usage/summary',
    ])
    expect(client.get).toHaveBeenLastCalledWith('/supplier/usage/summary', {
      params: { start: '2026-10-01', end: '2026-10-07', account_id: 3 },
    })
  })

  it('omits an empty proxy selection instead of sending 0', async () => {
    client.post.mockResolvedValue({ data: { auth_url: 'u', session_id: 's' } })
    await supplierAPI.generateAuthUrl('oauth', 0)
    expect(client.post).toHaveBeenCalledWith('/supplier/accounts/auth-url', { mode: 'oauth', proxy_id: undefined })

    client.post.mockResolvedValue({ data: {} })
    await supplierAPI.cookieAuth({ mode: 'setup-token', session_key: 'sk', name: 'a', proxy_id: null })
    expect(client.post).toHaveBeenLastCalledWith('/supplier/accounts/cookie-auth', {
      mode: 'setup-token',
      session_key: 'sk',
      name: 'a',
      proxy_id: undefined,
    })
  })

  it('uses dedicated admin namespaces that do not collide with /admin/suppliers/:id', async () => {
    client.get.mockResolvedValue({ data: { items: [], total: 0 } })
    client.post.mockResolvedValue({ data: {} })
    await adminSupplierAPI.listAccounts(2, 'pending')
    await adminSupplierAPI.review(9, { action: 'approve', group_ids: [1] })
    await adminSupplierAPI.listSettlements(1, 20, 0, '')
    await adminSupplierAPI.transitionSettlement(5, 'mark-paid')

    expect(client.get).toHaveBeenCalledWith('/admin/supplier-accounts', {
      params: { supplier_id: 2, review_status: 'pending' },
    })
    expect(client.post).toHaveBeenCalledWith('/admin/supplier-accounts/9/review', { action: 'approve', group_ids: [1] })
    expect(client.get).toHaveBeenCalledWith('/admin/supplier-settlements', {
      params: { page: 1, page_size: 20, supplier_id: undefined, status: undefined },
    })
    expect(client.post).toHaveBeenCalledWith('/admin/supplier-settlements/5/mark-paid')
  })
})
