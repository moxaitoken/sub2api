import { it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import View from '../OwnProxiesView.vue'
import { SupplierClient } from '../../api/client'
import en from '../../i18n/locales/en'
const proxy={id:'fixture',name:'Fixture',address:'http://8.8.8.8:8080',region:'SG',status:'disabled',accounts:0,held:0,capacity:1,version:2}
async function view(extra={}) {
 const client={ownProxies:vi.fn().mockResolvedValue({items:[{...proxy,...extra}]}),deleteOwnProxy:vi.fn().mockResolvedValue(undefined)}
 const w=mount(View,{props:{client:client as unknown as SupplierClient},global:{plugins:[createI18n({legacy:false,locale:'en',messages:{en}})]}})
 await flushPromises();return {w,client}
}
it('requires confirmation, deletes the selected ID and refreshes after success',async()=>{
 const {w,client}=await view();await w.findAll('button').find(b=>b.text()==='Delete')!.trigger('click')
 expect(client.deleteOwnProxy).not.toHaveBeenCalled();expect(w.get('[role=dialog]').text()).toContain('Fixture')
 client.ownProxies.mockResolvedValueOnce({items:[]})
 await w.findAll('button').find(b=>b.text()==='Confirm deletion')!.trigger('click');await flushPromises()
 expect(client.deleteOwnProxy).toHaveBeenCalledWith('fixture');expect(w.find('[role=dialog]').exists()).toBe(false);expect(w.text()).not.toContain('Fixture');w.unmount()
})
it.each([{accounts:1},{held:1}])('blocks deletion when in use %j',async extra=>{
 const {w,client}=await view(extra);expect(w.findAll('button').find(b=>b.text()==='Delete')!.attributes('disabled')).toBeDefined();expect(client.deleteOwnProxy).not.toHaveBeenCalled();w.unmount()
})
it('keeps confirmation and error visible on failure; retry uses the same record',async()=>{
 const {w,client}=await view();client.deleteOwnProxy.mockRejectedValueOnce(new Error('fixture'))
 await w.findAll('button').find(b=>b.text()==='Delete')!.trigger('click');await w.findAll('button').find(b=>b.text()==='Confirm deletion')!.trigger('click');await flushPromises()
 expect(w.find('[role=dialog]').exists()).toBe(true);expect(w.find('[role=alert]').exists()).toBe(true)
 await w.findAll('button').find(b=>b.text()==='Confirm deletion')!.trigger('click');await flushPromises();expect(client.deleteOwnProxy.mock.calls).toEqual([['fixture'],['fixture']]);w.unmount()
})
