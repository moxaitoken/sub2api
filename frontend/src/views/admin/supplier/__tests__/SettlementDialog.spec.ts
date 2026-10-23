import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import SettlementDialog from '../SettlementDialog.vue'
const { detail, settle } = vi.hoisted(() => ({ detail:vi.fn(), settle:vi.fn() }))
vi.mock('@/api/admin/suppliers', () => ({supplierAdminAPI:{detail},supplierFinanceAPI:{settle}}))
vi.mock('vue-i18n', () => ({useI18n:()=>({t:(s:string)=>s})}))
const owner = {id:'fixture',name:'Fixture',login_account:'fixture',portal_status:'active' as const,version:1,unsettled_usd:'100.00',settled_usd:'0.00',total_consumption_usd:'100.00'}
const config = {props:{supplier:owner},global:{stubs:{BaseDialog:{props:['show'],template:'<section v-if="show"><slot /></section>'}}}}
beforeEach(()=> { vi.resetAllMocks(); detail.mockResolvedValue(owner) })
it('rejects excess precision, zero and overspend before any write',async()=>{
  const w=mount(SettlementDialog,config);await flushPromises()
  for(const value of ['0','100.01','0.001','1e2']){await w.get('input').setValue(value);await w.get('form').trigger('submit');await flushPromises();expect(settle).not.toHaveBeenCalled()}
  w.unmount()
})
it('freezes uncertain submissions and retries the same target, exact payload and key',async()=>{
  settle.mockRejectedValueOnce({status:503}).mockResolvedValueOnce({id:'settlement',before_usd:'100.00',after_usd:'99.99',amount_usd:'0.01'})
  const w=mount(SettlementDialog,config);await flushPromises();await w.get('input').setValue('0.01');await w.get('form').trigger('submit');await flushPromises()
  expect(w.get('fieldset').attributes('disabled')).toBeDefined();expect(w.text()).toContain('supplierAdmin.uncertain')
  const first=settle.mock.calls[0];await w.get('form').trigger('submit');await flushPromises();expect(settle.mock.calls[1]).toEqual(first);expect(w.emitted('completed')).toHaveLength(1);w.unmount()
})
it('cancel closes without a settlement request',async()=>{
  const w=mount(SettlementDialog,config);await flushPromises();await w.findAll('button').find(b=>b.text()==='common.cancel')!.trigger('click');expect(settle).not.toHaveBeenCalled();expect(w.emitted('close')).toHaveLength(1);w.unmount()
})
