import {mount,flushPromises} from '@vue/test-utils'
import {it,expect,vi} from 'vitest'
import SupplierFinanceView from '../SupplierFinanceView.vue'
const {ledger,list}=vi.hoisted(()=>({ledger:vi.fn(),list:vi.fn()}))
vi.mock('@/api/admin/suppliers',()=>({supplierFinanceAPI:{ledger},supplierAdminAPI:{list}}))
vi.mock('vue-router',async original=>({...await original<typeof import('vue-router')>(),useRoute:()=>({query:{supplier_id:'fixture'}})}))
vi.mock('vue-i18n',async original=>({...await original<typeof import('vue-i18n')>(),useI18n:()=>({t:(s:string)=>s})}))
it('loads the selected supplier and restricts keyword search to ledger requests',async()=>{
  ledger.mockResolvedValue({overview:{unsettled_usd:'123.45',settled_usd:'10.00',settlement_count:2,accrual_status:'not_configured'},items:[],total:0,page:1,page_size:20,pages:0});list.mockResolvedValue({items:[]})
  const w=mount(SupplierFinanceView,{global:{stubs:{AppLayout:{template:'<main><slot/></main>'},DataTable:true,Pagination:true,BaseDialog:true}}});await flushPromises()
  expect(ledger).toHaveBeenCalledWith('fixture','',1);expect(w.text()).toContain('123.45');expect(w.text()).toContain('supplierAdmin.accrualPending')
  await w.get('input[aria-label="supplierAdmin.ledgerSearch"]').setValue('receipt');await w.get('form').trigger('submit');await flushPromises();expect(ledger).toHaveBeenLastCalledWith('fixture','receipt',1);w.unmount()
})
