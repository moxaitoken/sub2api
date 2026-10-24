import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AccountOnboardingView from '../AccountOnboardingView.vue'
import { SupplierClient } from '../../api/client'
import zh from '../../i18n/locales/zh'
import en from '../../i18n/locales/en'
const proxy = {id:'10000000-0000-4000-8000-000000000001',name:'Fixture proxy',address:'http://8.8.8.8:8080',region:'SG',status:'active',capacity:1,accounts:0,held:0,version:2}
const options = {groups:[{id:2,name:'Configured group',platform:'anthropic',status:'active'}],proxies:[proxy]}
const authorization = {id:'20000000-0000-4000-8000-000000000001',authorization_url:'https://claude.com/cai/oauth/authorize?state=fixture',expires_at:'2099-01-01T00:00:00Z'}
const identity = {principal:{kind:'supplier',id:'fixture',login_account:'vendor_fixture'},supplier:{id:'fixture',name:'Fixture',portal_status:'active',version:1},csrf_token:'fixture-csrf'}
const ok = (data:unknown) => new Response(JSON.stringify({code:0,data}),{status:200})
async function view(extra: Response[] = [], config:unknown = options) {
  const fetcher = vi.fn<typeof fetch>().mockResolvedValueOnce(ok(identity)).mockResolvedValueOnce(ok(config))
  for (const response of extra) fetcher.mockResolvedValueOnce(response)
  const client = new SupplierClient(()=>undefined,fetcher)
  await client.portalMe()
  const wrapper = mount(AccountOnboardingView,{props:{client},global:{plugins:[createI18n({legacy:false,locale:'en',messages:{zh,en}})]}})
  await flushPromises();return {wrapper,fetcher,client}
}
afterEach(()=>{vi.useRealTimers();vi.restoreAllMocks();vi.unstubAllGlobals()})
describe('manual supplier account onboarding',()=>{
 it('keeps a failed new proxy in its form without selecting it or allowing OAuth',async()=>{
  const {wrapper:w,fetcher:f}=await view([ok({...proxy,status:'disabled'})],{...options,proxies:[]})
  await w.findAll('select')[0].setValue('new')
  await w.findAll('input')[0].setValue('Fixture');await w.findAll('input')[1].setValue('http://8.8.8.8:8080')
  await w.get('form').trigger('submit');await flushPromises()
  expect(w.get('[role=alert]').text()).toContain('saved as invalid')
  expect(w.findAll('select')[0].element.value).toBe('new')
  expect(w.findAll('button').find(b=>b.text()==='Generate authorization link')!.attributes('disabled')).toBeDefined()
  expect(w.emitted('created')).toBeUndefined();expect(f).toHaveBeenCalledTimes(3);w.unmount()
 })
 it('loads only configured groups and authorized proxy choices; no fake defaults',async()=>{
  const {wrapper:w}=await view()
  expect(w.text()).toContain('Configured group');expect(w.text()).toContain('Fixture proxy')
  expect(w.findAll('input[type=password]')).toHaveLength(0)
  const generate=w.findAll('button').find(b=>b.text()==='Generate authorization link')!
  expect(generate.attributes('disabled')).toBeDefined()
  await w.findAll('select')[1].setValue(proxy.id)
  expect(generate.attributes('disabled')).toBeUndefined()
  w.unmount()
 })
 it('freezes proxy/group, opens an official URL, clears submitted code immediately and reports successfully validated and enabled',async()=>{
  const {wrapper:w,fetcher:f}=await view([ok(authorization),ok({id:'operation-fixture',action:'oauth.complete',state:'succeeded',step:'created_paused',account_id:'account-fixture'})])
  await w.findAll('select')[1].setValue(proxy.id)
  await w.findAll('button').find(b=>b.text()==='Generate authorization link')!.trigger('click');await flushPromises()
  expect(JSON.parse(String(f.mock.calls[2][1]?.body))).toEqual({name:'',proxy_id:proxy.id,group_ids:[2],settings:{concurrency:100,max_sessions:100,session_idle_timeout_minutes:5,base_rpm:15,rpm_strategy:'tiered'}})
  expect(new Headers(f.mock.calls[2][1]?.headers).get('Idempotency-Key')).toBeTruthy()
  expect(w.get('a').attributes('href')).toBe(authorization.authorization_url)
  expect(w.get('a').attributes('rel')).toBe('noopener noreferrer')
  expect(w.find('select').exists()).toBe(false)
  await w.get('textarea:not([readonly])').setValue('secret-code#fixture')
  const storage=vi.spyOn(Storage.prototype,'setItem')
  await w.get('form').trigger('submit')
  expect((w.get('textarea:not([readonly])').element as HTMLTextAreaElement).value).toBe('')
  await flushPromises();expect(w.text()).toContain('Account added');expect(w.text()).toContain('enabled after validation');expect(w.emitted('created')).toHaveLength(1);expect(storage).not.toHaveBeenCalled();w.unmount()
 })
 it('cancels an unconsumed authorization when the user closes the dialog',async()=>{
  const {wrapper:w,fetcher:f}=await view([ok(authorization),ok({cancelled:true})])
  await w.findAll('select')[1].setValue(proxy.id)
  await w.findAll('button').find(b=>b.text()==='Generate authorization link')!.trigger('click');await flushPromises()
  await w.get('.dialog-footer button').trigger('click');await flushPromises()
  expect(f.mock.calls[3][0]).toBe(`/api/supplier/v1/oauth/sessions/${authorization.id}`)
  expect(f.mock.calls[3][1]?.method).toBe('DELETE');expect(new Headers(f.mock.calls[3][1]?.headers).get('X-CSRF-Token')).toBe('fixture-csrf');expect(w.emitted('close')).toHaveLength(1);w.unmount()
 })
 it('rejects a credential-bearing or non-official authorization URL',async()=>{
  const {wrapper:w}=await view([ok({...authorization,authorization_url:'https://evil.invalid/steal?state=fixture'})])
  await w.findAll('select')[1].setValue(proxy.id);await w.findAll('button').find(b=>b.text()==='Generate authorization link')!.trigger('click');await flushPromises()
  expect(w.find('a').exists()).toBe(false);expect(w.find('[role=alert]').exists()).toBe(true);w.unmount()
 })
 it('does not offer a full or invalid proxy, and never generates an authorization without capacity',async()=>{
  const {wrapper:w,fetcher:f}=await view([],{...options,proxies:[{...proxy,held:1},{...proxy,id:'invalid',status:'disabled'}]})
  expect(w.text()).toContain('No valid proxy with spare capacity')
  expect(w.findAll('select')[1].findAll('option')).toHaveLength(1)
  expect(w.findAll('button').find(b=>b.text()==='Generate authorization link')!.attributes('disabled')).toBeDefined()
  expect(f).toHaveBeenCalledTimes(2);w.unmount()
 })
 it('keeps an unknown outcome visible without generating a new session or replaying a code',async()=>{
  const {wrapper:w,fetcher:f}=await view([ok(authorization),ok({id:'operation-unknown',action:'oauth.complete',state:'needs_attention',step:'account_create_unknown'})])
  await w.findAll('select')[1].setValue(proxy.id);await w.findAll('button').find(b=>b.text()==='Generate authorization link')!.trigger('click');await flushPromises()
  await w.get('textarea:not([readonly])').setValue('secret-code#fixture');await w.get('form').trigger('submit');await flushPromises()
  expect(w.text()).toContain('Result needs checking');expect(w.emitted('created')).toBeUndefined();expect(w.find('textarea:not([readonly])').exists()).toBe(false)
  expect(f).toHaveBeenCalledTimes(4);w.unmount()
 })
})

describe('copy the existing official authorization link across browsers', () => {
 async function authorizedView() {
  const result = await view([ok(authorization)])
  await result.wrapper.findAll('select')[1].setValue(proxy.id)
  await result.wrapper.findAll('button').find(b => b.text() === 'Generate authorization link')!.trigger('click')
  await flushPromises()
  return result
 }
 it('shows the complete readonly URL and copies exactly that URL without creating a new session', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  vi.stubGlobal('navigator', { clipboard: { writeText } })
  const {wrapper:w,fetcher:f} = await authorizedView()
  const link = w.get('textarea[readonly]')
  expect((link.element as HTMLTextAreaElement).value).toBe(authorization.authorization_url)
  expect(w.text()).toContain('browser where your Claude account is signed in')
  const requests = f.mock.calls.length
  await w.findAll('button').find(b => b.text() === 'Copy authorization link')!.trigger('click')
  await flushPromises()
  expect(writeText).toHaveBeenCalledWith(authorization.authorization_url)
  expect(w.text()).toContain('Authorization link copied')
  expect(w.get('a').attributes('href')).toBe(authorization.authorization_url)
  expect(f).toHaveBeenCalledTimes(requests)
  w.unmount()
 })
 it.each(['missing', 'denied'])('offers manual copying when clipboard access is %s', async mode => {
  const writeText = vi.fn().mockRejectedValue(new Error('denied'))
  vi.stubGlobal('navigator', mode === 'missing' ? {} : { clipboard: { writeText } })
  const {wrapper:w,fetcher:f} = await authorizedView()
  await w.findAll('button').find(b => b.text() === 'Copy authorization link')!.trigger('click')
  await flushPromises()
  expect(w.text()).toContain('Select or long-press the link to copy it manually')
  expect(w.text()).not.toContain('Authorization link copied')
  expect((w.get('textarea[readonly]').element as HTMLTextAreaElement).value).toBe(authorization.authorization_url)
  expect(f).toHaveBeenCalledTimes(3)
  w.unmount()
 })
})

describe('supplier-selected account settings and group picker', () => {
 it('uses Escape for the open menu without breaking Escape on the enclosing dialog', async () => {
  const {wrapper:w}=await view()
  const trigger=w.get('.group-picker-trigger')
  await trigger.trigger('click');await trigger.trigger('keydown',{key:'Escape'})
  expect(w.find('[role="listbox"]').exists()).toBe(false)
  expect(w.emitted('close')).toBeUndefined()
  await trigger.trigger('keydown',{key:'Escape'});await flushPromises()
  expect(w.emitted('close')).toHaveLength(1);w.unmount()
 })
 it('cannot start OAuth with no configured groups or after deselecting every group', async () => {
  const empty=await view([],{...options,groups:[]})
  await empty.wrapper.findAll('select')[1].setValue(proxy.id)
  expect(empty.wrapper.get('.group-picker-trigger').attributes('disabled')).toBeDefined()
  expect(empty.wrapper.findAll('button').find(b=>b.text()==='Generate authorization link')!.attributes('disabled')).toBeDefined()
  expect(empty.fetcher).toHaveBeenCalledTimes(2);empty.wrapper.unmount()
  const {wrapper:w,fetcher:f}=await view()
  await w.findAll('select')[1].setValue(proxy.id)
  await w.get('.group-picker-trigger').trigger('click')
  await w.get('[role="option"]').trigger('click')
  expect(w.get('[role="option"]').attributes('aria-selected')).toBe('false')
  expect(w.get('.group-picker-trigger').text()).toContain('Choose groups')
  expect(w.findAll('button').find(b=>b.text()==='Generate authorization link')!.attributes('disabled')).toBeDefined()
  expect(f).toHaveBeenCalledTimes(2);w.unmount()
 })
 async function editable() {
  const result = await view([ok(authorization)])
  await result.wrapper.findAll('select')[1].setValue(proxy.id)
  await result.wrapper.findAll('button').find(b => b.text().startsWith('Advanced settings'))!.trigger('click')
  return result
 }
 it('shows editable defaults and submits values above 100 unchanged', async () => {
  const {wrapper:w,fetcher:f} = await editable()
  for (const [id,value] of [['account-concurrency','100'],['max-sessions','100'],['session-idle','5'],['base-rpm','15']]) {
    expect((w.get(`[data-testid="${id}"]`).element as HTMLInputElement).value).toBe(value)
  }
  await w.get('[data-testid="account-concurrency"]').setValue('120')
  await w.get('[data-testid="max-sessions"]').setValue('150')
  await w.get('[data-testid="session-idle"]').setValue('8')
  await w.get('[data-testid="base-rpm"]').setValue('23')
  await w.get('[data-testid="rpm-strategy"]').setValue('sticky_exempt')
  await w.get('[data-testid="window-cost-enabled"]').setValue(true)
  await w.get('[data-testid="window-cost-limit"]').setValue('12.34')
  await w.get('[data-testid="window-cost-reserve"]').setValue('0')
  await w.findAll('button').find(b => b.text() === 'Generate authorization link')!.trigger('click')
  await flushPromises()
  expect(JSON.parse(String(f.mock.calls[2][1]?.body)).settings).toEqual({concurrency:120,max_sessions:150,session_idle_timeout_minutes:8,base_rpm:23,rpm_strategy:'sticky_exempt',window_cost_limit:12.34,window_cost_sticky_reserve:0})
  expect(w.find('[data-testid="account-concurrency"]').exists()).toBe(false)
  w.unmount()
 })
 it('restores blank fields to visible defaults and distinguishes disabled limits', async () => {
  const {wrapper:w,fetcher:f} = await editable()
  for (const [id,value] of [['account-concurrency','100'],['max-sessions','100'],['session-idle','5'],['base-rpm','15']]) {
    const input=w.get(`[data-testid="${id}"]`)
    await input.setValue('');await input.trigger('blur')
    expect((input.element as HTMLInputElement).value).toBe(value)
  }
  await w.get('[data-testid="session-limit-enabled"]').setValue(false)
  await w.get('[data-testid="rpm-limit-enabled"]').setValue(false)
  await w.findAll('button').find(b => b.text() === 'Generate authorization link')!.trigger('click');await flushPromises()
  expect(JSON.parse(String(f.mock.calls[2][1]?.body)).settings).toEqual({concurrency:100,max_sessions:0,base_rpm:0})
  w.unmount()
 })
 it('rejects invalid values without silently clamping them or starting OAuth', async () => {
  const {wrapper:w,fetcher:f} = await editable()
  const generate=w.findAll('button').find(b=>b.text()==='Generate authorization link')!
  for (const [id,value] of [['account-concurrency','-1'],['max-sessions','1.2'],['base-rpm','10001']]) {
    const input=w.get(`[data-testid="${id}"]`)
    await input.setValue(value)
    expect(generate.attributes('disabled')).toBeDefined()
    expect((input.element as HTMLInputElement).value).toBe(value)
    await input.setValue(id==='base-rpm' ? '15' : '100')
  }
  await w.get('[data-testid="window-cost-enabled"]').setValue(true)
  expect(generate.attributes('disabled')).toBeDefined()
  expect(f).toHaveBeenCalledTimes(2);w.unmount()
 })
 it('deduplicates names and IDs, toggles multiple groups, and closes with Escape or outside click', async () => {
  const groups=[{...options.groups[0],name:'coding',group_ids:[2,3]}, {...options.groups[0],id:4,name:'coding',group_ids:[3,4]}, {...options.groups[0],id:5,name:'distill',group_ids:[5]}]
  const {wrapper:w,fetcher:f}=await view([ok(authorization)],{...options,groups})
  const trigger=w.get('.group-picker-trigger')
  await trigger.trigger('click')
  expect(w.findAll('[role="option"]')).toHaveLength(2)
  await w.findAll('[role="option"]')[1].trigger('click')
  expect(trigger.text()).toContain('coding, distill')
  expect(w.findAll('[aria-selected="true"]')).toHaveLength(2)
  await trigger.trigger('keydown',{key:'Escape'})
  expect(w.find('[role="listbox"]').exists()).toBe(false)
  await trigger.trigger('click');document.body.dispatchEvent(new Event('pointerdown',{bubbles:true}));await flushPromises()
  expect(w.find('[role="listbox"]').exists()).toBe(false)
  await w.findAll('select')[1].setValue(proxy.id)
  await w.findAll('button').find(b=>b.text()==='Generate authorization link')!.trigger('click');await flushPromises()
  expect(JSON.parse(String(f.mock.calls[2][1]?.body)).group_ids).toEqual([2,3,4,5])
  w.unmount()
 })
})
