import { expect,it,vi } from 'vitest'
import { checkedOverview } from '../overview'
import { SupplierClient } from '../client'
import { success } from './fixtures'
const metric={value:'0',known:0,total:0,status:'ok'}
const value=()=>({observed_at:new Date().toISOString(),quota_fetched_at:null,rpm:{...metric},tpm:{...metric},uncached_tpm:{...metric},concurrency:{...metric},high_quota:{...metric},windows:{five_hour:{maximum:null,known:0,total:0},seven_day:{maximum:null,known:0,total:0},fable:{maximum:null,known:0,total:0}},account_quotas:{}})
it('validates exact counts, percentages, timestamps and completeness boundaries',()=>{
  expect(checkedOverview(value()).rpm.value).toBe('0')
  const v=value();v.rpm.value='NaN';expect(()=>checkedOverview(v)).toThrow()
  const q=value();q.windows.five_hour.known=1;expect(()=>checkedOverview(q)).toThrow()
})
it('pins supplier and admin overview routes and rejects caller scope changes',async()=>{
  const transport=vi.fn<typeof fetch>().mockImplementation(async()=>success(value()))
  const client=new SupplierClient(vi.fn(),transport)
  await client.overview();expect(transport.mock.calls[0][0]).toBe('/api/supplier/v1/accounts/overview')
  await expect(client.overview('foreign')).rejects.toMatchObject({status:403})
  const admin=new SupplierClient(vi.fn(),transport,'admin');await admin.overview('owned')
  expect(transport.mock.calls[1][0]).toBe('/api/supplier/admin/v1/suppliers/owned/accounts/overview')
})
