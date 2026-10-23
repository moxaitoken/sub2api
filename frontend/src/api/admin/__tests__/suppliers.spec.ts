import { beforeEach, describe, expect, it, vi } from 'vitest'
const { request, requireAcknowledgement } = vi.hoisted(() => ({ request: vi.fn(), requireAcknowledgement: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { request } }))
vi.mock('@/stores/adminCompliance', () => ({ useAdminComplianceStore: () => ({ requireAcknowledgement }) }))
import { supplierResourceClient, supplierFinanceAPI, supplierAdminAPI } from '../suppliers'
beforeEach(() => { vi.resetAllMocks(); request.mockResolvedValue({ data: {} }) })
describe('supplier main client integration', () => {
  it('uses the main client at a fixed same-origin path and keeps mutation controls', async () => {
    const supplier = { id: 'fixture', name: 'Fixture', login_account: 'fixture', portal_status: 'active' as const, version: 7, unsettled_usd:'0.00',settled_usd:'0.00',total_consumption_usd:'0.00' }
    await supplierAdminAPI.setStatus(supplier, false, 'fixture-key')
    expect(request).toHaveBeenCalledWith(expect.objectContaining({ baseURL: '/', url: '/api/supplier/admin/v1/suppliers/fixture/status', method: 'PUT', headers: { 'Idempotency-Key': 'fixture-key', 'If-Match': '7' }, data: { enabled: false } }))
  })
  it('opens the existing compliance dialog on a blocked admin request without accepting it', async () => {
    const blocked = { status: 423, reason: 'ADMIN_COMPLIANCE_ACK_REQUIRED' }
    request.mockRejectedValue(blocked)
    await expect(supplierAdminAPI.list()).rejects.toEqual(blocked)
    expect(requireAcknowledgement).toHaveBeenCalledOnce()
  })
})

it('locks all resource calls to one supplier using the main authenticated transport', async () => {
  const id='30000000-0000-0000-0000-000000000001'
  request.mockResolvedValue({data:{items:[],total:0,page:1,pages:1,page_size:20}})
  await supplierResourceClient(id).accounts()
  expect(request).toHaveBeenLastCalledWith(expect.objectContaining({url:`/api/supplier/admin/v1/suppliers/${id}/accounts?page=1&page_size=20`,baseURL:'/'}))
  await expect(supplierResourceClient(id).portalMe()).rejects.toMatchObject({status:403})
})
it('keeps exact decimal settlement input and its retry key',async()=>{
  const input={amount_usd:'0.01',payment_method:'usd_transfer',reference:'fixture',remark:''}
  await supplierFinanceAPI.settle('fixture',input,'stable-key')
  expect(request).toHaveBeenCalledWith(expect.objectContaining({url:'/api/supplier/admin/v1/suppliers/fixture/settlements',data:input,headers:{'Idempotency-Key':'stable-key'}}))
})

it('adapts main automatic timezone without weakening strict supplier queries',async()=>{
  await supplierFinanceAPI.ledger()
  const config=request.mock.calls[0]![0]
  expect(config.paramsSerializer.serialize({timezone:'Asia/Shanghai',page:2})).toBe('page=2')
})
