import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import PortalLoginView from '../PortalLoginView.vue'
import { SupplierClient } from '../../api/client'
import zh from '../../i18n/locales/zh'
import en from '../../i18n/locales/en'
const ok=(data:unknown)=>new Response(JSON.stringify({code:0,data}),{status:200})
const view=(transport:typeof fetch,administrator=false)=>mount(PortalLoginView,{props:{client:new SupplierClient(()=>undefined,transport,administrator?'admin':'supplier'),administrator},global:{plugins:[createI18n({legacy:false,locale:'en',messages:{zh,en}})]}})
afterEach(()=>vi.restoreAllMocks())
describe('R2 login forms',()=>{
 it.each(['zh','en'])('does not offer administrator sign-in on the supplier form (%s)',(locale)=>{
  const w=mount(PortalLoginView,{props:{client:new SupplierClient(()=>undefined,vi.fn()),administrator:false},global:{plugins:[createI18n({legacy:false,locale,messages:{zh,en}})]}})
  expect(w.find('a[href="/?portal=admin"]').exists()).toBe(false)
  expect(w.find('#login-account').exists()).toBe(true)
  expect(w.find('#login-password').exists()).toBe(true)
  w.unmount()
 })
 it('keeps the return-to-supplier link on the administrator form',()=>{
  const w=view(vi.fn(),true)
  expect(w.get('a[href="/?portal=supplier"]').text()).toBe(en.portal.supplierLogin)
  w.unmount()
 })
 it('submits a non-email local account and immediately clears the password',async()=>{
  let resolve:(r:Response)=>void=()=>undefined;const f=vi.fn<typeof fetch>().mockReturnValue(new Promise<Response>(r=>{resolve=r}));const w=view(f)
  await w.get('#login-account').setValue('vendor_01');await w.get('#login-password').setValue('FixturePassword!2026');await w.get('form').trigger('submit')
  expect((w.get('#login-password').element as HTMLInputElement).value).toBe('');expect(JSON.parse(String(f.mock.calls[0][1]?.body))).toEqual({login_account:'vendor_01',password:'FixturePassword!2026'});resolve(ok({requires_2fa:false}));await flushPromises();expect(w.emitted('authenticated')).toHaveLength(1);w.unmount()
 })
 it('admin completes only a real upstream challenge at the independent endpoint',async()=>{
  const f=vi.fn<typeof fetch>().mockResolvedValueOnce(ok({requires_2fa:true,challenge_id:'fixture',expires_at:'2099-01-01T00:00:00Z'})).mockResolvedValueOnce(ok({requires_2fa:false}));const w=view(f,true)
  await w.get('#login-account').setValue('admin@example.invalid');await w.get('#login-password').setValue('fixture');await w.get('form').trigger('submit');await flushPromises();expect(w.find('#login-code').exists()).toBe(true)
  await w.get('#login-code').setValue('123456');await w.get('form').trigger('submit');await flushPromises();expect(f.mock.calls.map(c=>c[0])).toEqual(['/api/supplier/admin/v1/auth/login','/api/supplier/admin/v1/auth/login/2fa']);expect(w.emitted('authenticated')).toHaveLength(1);w.unmount()
 })
 it('does not echo raw credential failures',async()=>{
  const f=vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({code:401,reason:'AUTHENTICATION_REQUIRED',message:'secret-in-upstream-error'}),{status:401}));const w=view(f)
  await w.get('#login-account').setValue('vendor_01');await w.get('#login-password').setValue('fixture');await w.get('form').trigger('submit');await flushPromises();expect(w.find('[role=alert]').exists()).toBe(true);expect(w.text()).not.toContain('secret-in-upstream-error');w.unmount()
 })
})
