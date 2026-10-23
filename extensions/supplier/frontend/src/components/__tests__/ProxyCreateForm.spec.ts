import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ProxyCreateForm from '../ProxyCreateForm.vue'
import { SupplierClient, ApiError } from '../../api/client'
import zh from '../../i18n/locales/zh'
import en from '../../i18n/locales/en'
import type { OwnProxy } from '../../api/types'
const ok = (data:unknown) => new Response(JSON.stringify({code:0,data}),{status:200})
describe('private proxy credential submission',()=>{
 it('clears authentication-bearing input immediately and emits only safe returned fields',async()=>{
  let resolve:(response:Response)=>void=()=>undefined
  const f=vi.fn<typeof fetch>().mockResolvedValueOnce(ok({principal:{kind:'supplier',id:'fixture',login_account:'vendor_fixture'},supplier:{id:'fixture',name:'Fixture',portal_status:'active',version:1},csrf_token:'fixture-csrf'})).mockReturnValueOnce(new Promise<Response>(r=>{resolve=r}))
  const client=new SupplierClient(()=>undefined,f);await client.portalMe()
  const w=mount(ProxyCreateForm,{props:{client},global:{plugins:[createI18n({legacy:false,locale:'en',messages:{zh,en}})]}})
  await w.findAll('input')[0].setValue('Fixture proxy');await w.findAll('input')[1].setValue('http://user:secret@8.8.8.8:8080');await w.get('select').setValue('SG')
  const storage=vi.spyOn(Storage.prototype,'setItem')
  await w.get('form').trigger('submit')
  expect((w.findAll('input')[1].element as HTMLInputElement).value).toBe('')
  expect(new Headers(f.mock.calls[1][1]?.headers).get('Idempotency-Key')).toBeTruthy()
  expect(new Headers(f.mock.calls[1][1]?.headers).get('X-CSRF-Token')).toBe('fixture-csrf')
  resolve(ok({id:'proxy-fixture',name:'Fixture proxy',address:'http://8.8.8.8:8080',region:'SG',status:'active',capacity:1,accounts:0,held:0,version:2,password:'unexpected-server-secret'}));await flushPromises()
  expect(w.emitted('created')).toHaveLength(1);expect(JSON.stringify(w.emitted('created'))).not.toContain('password');expect(w.text()).not.toContain('secret');expect(storage).not.toHaveBeenCalled();storage.mockRestore();w.unmount()
 })
})

describe('proxy connectivity result', () => {
 it('shows a rejected connectivity check without emitting creation or saving a row',async()=>{
  const client={addProxy:vi.fn().mockRejectedValue(new ApiError(422,'PROXY_CHECK_FAILED'))}
  const w=mount(ProxyCreateForm,{props:{client:client as unknown as SupplierClient},global:{plugins:[createI18n({legacy:false,locale:'en',messages:{zh,en}})]}})
  await w.findAll('input')[0].setValue('Fixture');await w.findAll('input')[1].setValue('http://8.8.8.8:8080');await w.get('select').setValue('SG')
  await w.get('form').trigger('submit');await flushPromises()
  expect(w.emitted('created')).toBeUndefined();expect(w.emitted('saved')).toBeUndefined();expect(w.get('[role=alert]').text()).toContain('was not added')
  expect((w.findAll('input')[1].element as HTMLInputElement).value).toBe('');w.unmount()
 })
 const proxy: OwnProxy = {id:'fixture-proxy',name:'Fixture',address:'http://8.8.8.8:8080',region:'SG',status:'disabled',capacity:1,accounts:0,held:0,version:2}
 async function failedForm(status: OwnProxy['status'] = 'disabled') {
  const client = {addProxy:vi.fn().mockResolvedValue({...proxy,status}),testOwnProxy:vi.fn()}
  const w = mount(ProxyCreateForm,{props:{client:client as unknown as SupplierClient},global:{plugins:[createI18n({legacy:false,locale:'en',messages:{zh,en}})]}})
  await w.findAll('input')[0].setValue('Fixture');await w.findAll('input')[1].setValue('http://fixture-user:fixture-secret@8.8.8.8:8080');await w.get('select').setValue('SG')
  await w.get('form').trigger('submit');await flushPromises()
  return {w,client}
 }
 it('keeps a failed check visible and retests the saved ID without creating twice', async () => {
  const {w,client} = await failedForm()
  expect(w.emitted('created')).toBeUndefined();expect(w.emitted('saved')).toHaveLength(1)
  expect(w.get('[role=alert]').text()).toContain('saved as invalid')
  expect(w.text()).not.toContain('fixture-secret')
  expect(w.get('button').text()).toBe('Retest saved proxy')
  client.testOwnProxy.mockResolvedValueOnce(proxy).mockResolvedValueOnce({...proxy,status:'active',version:3})
  await w.get('form').trigger('submit');await flushPromises()
  expect(w.emitted('created')).toBeUndefined();expect(w.find('[role=alert]').exists()).toBe(true)
  await w.get('form').trigger('submit');await flushPromises()
  expect(client.addProxy).toHaveBeenCalledTimes(1);expect(client.testOwnProxy).toHaveBeenCalledTimes(2)
  expect(client.testOwnProxy).toHaveBeenLastCalledWith(proxy.id);expect(w.emitted('created')).toHaveLength(1)
  w.unmount()
 })
 it('preserves the failed record and warning after a retest request error', async () => {
  const {w,client} = await failedForm()
  client.testOwnProxy.mockRejectedValueOnce(new Error('fixture-secret'))
  await w.get('form').trigger('submit');await flushPromises()
  expect(w.emitted('created')).toBeUndefined();expect(client.addProxy).toHaveBeenCalledTimes(1)
  expect(w.text()).toContain('saved as invalid');expect(w.text()).not.toContain('fixture-secret')
  expect(w.get('button').attributes('disabled')).toBeUndefined();w.unmount()
 })
 it.each(['needs_attention','provisioning'] as const)('does not retry creation for an uncertain %s result', async status => {
  const {w,client} = await failedForm(status)
  expect(w.get('[role=alert]').text()).toContain('result is unconfirmed')
  expect(w.emitted('created')).toBeUndefined();expect(w.get('button').attributes('disabled')).toBeDefined()
  await w.get('form').trigger('submit');await flushPromises()
  expect(client.addProxy).toHaveBeenCalledTimes(1);expect(client.testOwnProxy).not.toHaveBeenCalled();w.unmount()
 })
})
