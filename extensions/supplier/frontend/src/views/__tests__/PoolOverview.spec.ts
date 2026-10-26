import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, expect, it, vi } from 'vitest'
import View from '../SupplierWorkspaceView.vue'
import en from '../../i18n/locales/en'
import type { SupplierClient } from '../../api/client'
import type { PoolOverview } from '../../api/overview'

function fixture(): PoolOverview {
  const metric = (value: string) => ({value,known:25,total:25,status:'ok' as const})
  return {observed_at:new Date().toISOString(),rpm:metric('7'),tpm:metric('9007199254740993'),uncached_tpm:metric('3'),concurrency:metric('2'),high_quota:metric('1'),quota_fetched_at:new Date().toISOString(),windows:{five_hour:{maximum:95,known:24,total:25},seven_day:{maximum:null,known:0,total:25},fable:{maximum:0,known:25,total:25}},account_quotas:{}}
}
function setup() {
  const overview=vi.fn().mockResolvedValue(fixture())
  const w=mount(View,{props:{client:{overview} as unknown as SupplierClient,tab:'accounts',accounts:[],total:25,page:2,pages:2,loading:false,failed:false,updatedAt:''},global:{plugins:[createI18n({legacy:false,locale:'en',messages:{en}})]}})
  return {w,overview}
}
afterEach(()=>{vi.useRealTimers();vi.restoreAllMocks()})
it('renders supplier-wide real metrics and quota maximum/coverage rather than a current-page sum',async()=>{
  vi.useFakeTimers();const {w,overview}=setup();await flushPromises()
  expect(overview).toHaveBeenCalledWith(undefined)
  expect(w.findAll('.overview-metric strong').map(n=>n.text())).toEqual(['7','9,007,199,254,740,993','3','2','1'])
  expect(w.text()).toContain('Current calendar minute')
  expect(w.get('.quota-windows').text()).toContain('95%')
  expect(w.get('.quota-windows').text()).toContain('Observed 24/25 accounts')
  expect(w.get('.quota-windows').text()).toContain('0%')
  expect(w.get('.quota-windows').text()).toContain('No valid quota snapshot')
  await w.get('input[type=text],input:not([type])').setValue('missing')
  expect(w.findAll('.overview-metric strong')[0].text()).toBe('7')
  w.unmount()
})
it('polls only while visible, does not keep stale values after failure, and cleans up',async()=>{
  vi.useFakeTimers();const visible=vi.spyOn(document,'visibilityState','get').mockReturnValue('visible')
  const {w,overview}=setup();await flushPromises()
  await vi.advanceTimersByTimeAsync(5000);await flushPromises();expect(overview).toHaveBeenCalledTimes(2)
  visible.mockReturnValue('hidden');document.dispatchEvent(new Event('visibilitychange'))
  await vi.advanceTimersByTimeAsync(15000);expect(overview).toHaveBeenCalledTimes(2)
  overview.mockRejectedValueOnce(new Error('fixture failure'));visible.mockReturnValue('visible');document.dispatchEvent(new Event('visibilitychange'));await flushPromises()
  expect(w.findAll('.overview-metric strong').every(n=>n.text()==='—')).toBe(true)
  expect(w.text()).toContain('Unavailable / not enabled')
  w.unmount();const calls=overview.mock.calls.length;await vi.advanceTimersByTimeAsync(10000);expect(overview).toHaveBeenCalledTimes(calls)
})
it('ignores a slow response after the supplier/client changes',async()=>{
  vi.useFakeTimers();const {w,overview}=setup();await flushPromises()
  let resolve!: (v:PoolOverview)=>void
  overview.mockReturnValueOnce(new Promise(r=>{resolve=r}));await vi.advanceTimersByTimeAsync(5000)
  const next=vi.fn().mockResolvedValue({...fixture(),rpm:{value:'1',known:1,total:1,status:'ok'}})
  await w.setProps({client:{overview:next} as unknown as SupplierClient});await flushPromises()
  resolve(fixture());await flushPromises();expect(w.findAll('.overview-metric strong')[0].text()).toBe('1');w.unmount()
})
