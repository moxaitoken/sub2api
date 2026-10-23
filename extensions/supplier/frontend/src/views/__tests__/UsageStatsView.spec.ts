import { it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import View from '../UsageStatsView.vue'
import { SupplierClient } from '../../api/client'
import { checkedUsage } from '../../api/usage'
import en from '../../i18n/locales/en'
const id = '10000000-0000-4000-8000-000000000001'
const metrics = { record_count: '2', input_tokens: '9007199254740993', output_tokens: '2', cache_read_tokens: '3', cache_creation_tokens: '4', tokens: '9007199254740995', cache_tokens: '7', total_cost_usd: '0.300000000000000001' }
const report = {
  period: 'today' as const, timezone: 'Asia/Shanghai' as const, account_id: '', start: '2026-10-22T00:00:00+08:00', end: '2026-10-23T00:00:00+08:00', observed_at: '2026-10-22T12:00:00+08:00',
  summary: metrics, status: 'complete' as const, request_attempts: null, total: 1, page: 1, page_size: 20, pages: 1,
  account_options: [{ id, name: 'Fixture account', archived: false }], daily: [{ date: '2026-10-22', ...metrics }],
  accounts: [{ id, name: 'Fixture account', archived: false, ...metrics, sync_state: 'complete' as const, synced_at: '2026-10-22T12:00:00+08:00' }]
}
async function setup() {
  const client = { usage: vi.fn().mockResolvedValue(report) }
  const w = mount(View, { props: { client: client as unknown as SupplierClient }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })] } })
  await flushPromises(); return { w, client }
}
it('shows exact consumption and large tokens, while keeping all request attempts unknown', async () => {
  const { w, client } = await setup()
  expect(client.usage).toHaveBeenCalledWith('today', '', 1)
  expect(w.get('[data-testid=usage-cost]').text()).toBe('$0.300000000000000001')
  expect(w.text()).toContain('9,007,199,254,740,995')
  expect(w.text()).toContain('All request attempts have no reliable source yet')
  expect(w.text()).toContain('Fixture account'); w.unmount()
})
it('scopes filters, resets pages, and removes stale totals on failure', async () => {
  const { w, client } = await setup()
  await w.get('select[aria-label=account]').setValue(id); await flushPromises()
  expect(client.usage).toHaveBeenLastCalledWith('today', id, 1)
  client.usage.mockRejectedValueOnce(new Error('fixture failure'))
  await w.get('select[aria-label=period]').setValue('7d'); await flushPromises()
  expect(client.usage).toHaveBeenLastCalledWith('7d', id, 1)
  expect(w.find('[role=alert]').exists()).toBe(true)
  expect(w.find('[data-testid=usage-cost]').exists()).toBe(false); w.unmount()
})
it('discards late responses after a new filter was selected', async () => {
  const { w, client } = await setup()
  let finish!: (v: typeof report) => void
  client.usage.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  await w.get('select[aria-label=period]').setValue('7d')
  client.usage.mockResolvedValueOnce({ ...report, status: 'partial' })
  await w.get('select[aria-label=period]').setValue('30d'); await flushPromises()
  finish(report); await flushPromises()
  expect(w.text()).toContain('Not fully reconciled'); w.unmount()
})
it('offers full history and resets account pagination when selected', async () => {
  const { w, client } = await setup()
  client.usage.mockResolvedValueOnce({ ...report, period: 'all' })
  await w.get('select[aria-label=period]').setValue('all'); await flushPromises()
  expect(client.usage).toHaveBeenLastCalledWith('all', '', 1)
  expect(w.text()).toContain('All history')
  expect(checkedUsage({ ...report, period: 'all' }).period).toBe('all')
  const longHistory = Array.from({ length: 1500 }, (_, i) => ({ ...report.daily[0], date: new Date(Date.UTC(2020, 0, 1 + i)).toISOString().slice(0, 10) }))
  expect(checkedUsage({ ...report, period: 'all', daily: longHistory }).daily).toHaveLength(1500)
  w.unmount()
})
it('allowlists usage DTO fields and rejects malformed numeric or sync values', () => {
  const safe = checkedUsage({ ...report, upstream_user: { email: 'never-leak' } })
  expect(JSON.stringify(safe)).not.toContain('never-leak')
  expect(safe.summary.total_cost_usd).toBe('0.300000000000000001')
  expect(() => checkedUsage({ ...report, summary: { ...metrics, total_cost_usd: 0.3 } })).toThrow()
  expect(() => checkedUsage({ ...report, request_attempts: '2' })).toThrow()
  expect(() => checkedUsage({ ...report, accounts: [{ ...report.accounts[0], sync_state: 'unknown' }] })).toThrow()
})
