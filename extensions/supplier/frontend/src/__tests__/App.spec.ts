import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import App from '../App.vue'
import zh from '../i18n/locales/zh'
import en from '../i18n/locales/en'
const supplier = {id:'10000000-0000-0000-0000-000000000001',name:'Fixture vendor',portal_status:'active',version:1}
const local = {principal:{kind:'supplier',id:supplier.id,login_account:'vendor_01'},supplier,csrf_token:'fixture-csrf'}
const admin = {principal:{kind:'admin',id:'42',email:'admin@example.invalid'},csrf_token:'fixture-admin-csrf'}
const row = {...supplier,login_account:'vendor_01',unsettled_usd:'0.00',settled_usd:'0.00',total_consumption_usd:'0.00'}
const ok = (data:unknown) => new Response(JSON.stringify({code:0,data}),{status:200})
const page = (items:unknown[]) => ({items,total:items.length,page:1,page_size:20,pages:items.length ? 1 : 0})
const mountApp = () => mount(App,{global:{plugins:[createI18n({legacy:false,locale:'en',messages:{zh,en}})]}})
afterEach(() => {vi.unstubAllGlobals();vi.restoreAllMocks();window.history.replaceState(null,'','/')})
describe('R2 portal boundaries',()=>{
 it('uses Goccmax consistently in supplier navigation and footer',async()=>{
  vi.stubGlobal('fetch',vi.fn().mockResolvedValueOnce(ok(local)).mockResolvedValueOnce(ok(page([]))))
  const w=mountApp();await flushPromises()
  expect(w.get('.brand b').text()).toBe('Goccmax')
  expect(w.get('.breadcrumb').text()).toContain('Goccmax')
  expect(w.get('.supplier-footer').text()).toContain('Goccmax')
  expect(w.text()).not.toMatch(/MOXAI|Moxai/)
  w.unmount()
 })

 it('shows local account login on initial 401 without raw failure',async()=>{
  vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response('{}',{status:401})))
  const w=mountApp();await flushPromises();expect(w.get('#login-account').attributes('type')).toBe('text');expect(w.find('[role=alert]').exists()).toBe(false);w.unmount()
 })
 it('has exactly three supplier menu items and no member/review/finance controls',async()=>{
  const fetcher=vi.fn().mockResolvedValueOnce(ok(local)).mockResolvedValueOnce(ok(page([])));vi.stubGlobal('fetch',fetcher)
  const w=mountApp();await flushPromises();expect(w.findAll('nav button').map(b=>b.text())).toEqual(['Account pool','Proxy management','Usage statistics']);expect(w.text()).toContain('Fixture vendor');expect(w.text()).toContain('No accounts');expect(w.text()).not.toMatch(/Account review|Create supplier|Supplier information|Members/);w.unmount()
 })
 it('admits the real admin without inventing a supplier and uses only admin APIs',async()=>{
  window.history.replaceState(null,'','/?portal=admin');const f=vi.fn().mockResolvedValueOnce(ok(admin)).mockResolvedValueOnce(ok(page([row])));vi.stubGlobal('fetch',f)
  const w=mountApp();await flushPromises();expect(w.findAll('nav button').map(b=>b.text())).toEqual(['Supplier list','Supplier finance']);expect(w.text()).toContain('vendor_01');expect(f.mock.calls.map(c=>c[0])).toEqual(['/api/supplier/admin/v1/me','/api/supplier/admin/v1/suppliers?search=&page=1&page_size=20']);w.unmount()
 })
 it('supplier cookie cannot unlock admin UI with a forged identity body',async()=>{
  window.history.replaceState(null,'','/?portal=admin');vi.stubGlobal('fetch',vi.fn().mockResolvedValue(ok(local)))
  const w=mountApp();await flushPromises();expect(w.find('#login-password').exists()).toBe(true);expect(w.text()).not.toContain('Fixture vendor');w.unmount()
 })
 it('clears a create password immediately and sends one key, never stores it',async()=>{
  window.history.replaceState(null,'','/?portal=admin');let resolve:(r:Response)=>void=()=>undefined
  const f=vi.fn().mockResolvedValueOnce(ok(admin)).mockResolvedValueOnce(ok(page([]))).mockReturnValueOnce(new Promise<Response>(r=>{resolve=r})).mockResolvedValue(ok(page([row])))
  vi.stubGlobal('fetch',f);const storage=vi.spyOn(Storage.prototype,'setItem');const w=mountApp();await flushPromises()
  await w.findAll('button').find(b=>b.text()==='Create supplier account')!.trigger('click')
  await w.get('#supplier-name').setValue('Fixture vendor');await w.get('#supplier-login').setValue('vendor_01');await w.get('#supplier-password').setValue('FixturePassword!2026')
  await w.get('[role=dialog] form').trigger('submit');expect((w.get('#supplier-password').element as HTMLInputElement).value).toBe('');expect(storage).not.toHaveBeenCalled()
  const call=f.mock.calls[2];expect(call[0]).toBe('/api/supplier/admin/v1/suppliers');expect(new Headers(call[1].headers).get('Idempotency-Key')).toBeTruthy();expect(new Headers(call[1].headers).get('X-CSRF-Token')).toBe('fixture-admin-csrf');resolve(ok(row));await flushPromises();expect(w.find('[role=dialog]').exists()).toBe(false);w.unmount()
 })
 it('logout removes private data and sends the current identity logout route',async()=>{
  const f=vi.fn().mockResolvedValueOnce(ok(local)).mockResolvedValueOnce(ok(page([]))).mockResolvedValueOnce(ok({logged_out:true}));vi.stubGlobal('fetch',f)
  const w=mountApp();await flushPromises();await w.get('.toolbar button').trigger('click');await flushPromises();expect(w.find('#login-password').exists()).toBe(true);expect(w.text()).not.toContain('Fixture vendor');expect(f.mock.calls[2][0]).toBe('/api/supplier/v1/auth/logout');w.unmount()
 })
 it('admin business details lock the selected supplier and query scoped accounts',async()=>{
  window.history.replaceState(null,'','/?portal=admin');const f=vi.fn().mockResolvedValueOnce(ok(admin)).mockResolvedValueOnce(ok(page([row]))).mockResolvedValueOnce(ok(page([])));vi.stubGlobal('fetch',f)
  const w=mountApp();await flushPromises();await w.findAll('tbody button').find(b=>b.text()==='Account pool')!.trigger('click');await flushPromises();expect(f.mock.calls[2][0]).toBe(`/api/supplier/admin/v1/suppliers/${row.id}/accounts?page=1&page_size=20&include_metrics=1`);expect(w.text()).toContain('Back to supplier list');w.unmount()
 })
})


describe('supplier workspace navigation and paging', () => {
 it('keeps prototype sidebar selection and breadcrumb in sync', async () => {
  vi.stubGlobal('fetch',vi.fn().mockResolvedValueOnce(ok(local)).mockResolvedValueOnce(ok(page([]))))
  const w=mountApp();await flushPromises()
  expect(w.find('.supplier-sidebar').exists()).toBe(true)
  expect(w.get('nav button[aria-current="page"]').text()).toBe('Account pool')
  await w.get('nav button[aria-label="Proxy management"]').trigger('click')
  expect(w.get('nav button[aria-current="page"]').text()).toBe('Proxy management')
  expect(w.get('.breadcrumb b').text()).toBe('Proxy management')
  expect(w.get('main h1').text()).toBe('Proxy management')
  expect(w.text()).not.toMatch(/Create supplier|Supplier finance|Members/)
  w.unmount()
 })
 it('passes the requested page to the supplier-scoped API', async () => {
  const f=vi.fn().mockResolvedValueOnce(ok(local)).mockResolvedValueOnce(ok({...page([]),total:21,pages:2})).mockResolvedValueOnce(ok({...page([]),total:21,page:2,pages:2}));vi.stubGlobal('fetch',f)
  const w=mountApp();await flushPromises();await w.get('button[aria-label="Next"]').trigger('click');await flushPromises()
  expect(f.mock.calls[2][0]).toBe('/api/supplier/v1/accounts?page=2&page_size=20&include_metrics=1')
  expect(w.get('.workspace-pagination').text()).toContain('Page 2 / 2')
  w.unmount()
 })
 it('rejects a late account response after logout', async () => {
  let resolve:(r:Response)=>void=()=>undefined
  const f=vi.fn().mockResolvedValueOnce(ok(local)).mockReturnValueOnce(new Promise<Response>(r=>{resolve=r})).mockResolvedValueOnce(ok({logged_out:true}));vi.stubGlobal('fetch',f)
  const w=mountApp();await flushPromises();await w.get('.toolbar button').trigger('click');await flushPromises()
  resolve(ok(page([{id:'late-account',name:'Private late account',admission_state:'approved',desired_state:'running',version:1,observed_at:null}])));await flushPromises()
  expect(w.find('#login-password').exists()).toBe(true);expect(w.text()).not.toContain('Private late account');expect(w.text()).not.toContain('Fixture vendor')
  w.unmount()
 })
})


it('opens a real account onboarding dialog from the enabled add button',async()=>{
 const f=vi.fn().mockResolvedValueOnce(ok(local)).mockResolvedValueOnce(ok(page([]))).mockResolvedValueOnce(ok({groups:[{id:2,name:'Configured fixture group',platform:'anthropic',status:'active'}],proxies:[]}));vi.stubGlobal('fetch',f)
 const w=mountApp();await flushPromises();const add=w.findAll('button').find(b=>b.text()==='Add account')!;expect(add.attributes('disabled')).toBeUndefined();await add.trigger('click');await flushPromises();expect(w.get('[role=dialog]').text()).toContain('Manual OAuth');expect(w.get('[role=dialog]').text()).toContain('Configured fixture group');expect(f.mock.calls[2][0]).toBe('/api/supplier/v1/onboarding/options');w.unmount()
})
