import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AccountsView from '../AccountsView.vue'
import { session } from '../../composables/useSession'
import { identity, success } from '../../api/__tests__/fixtures'
import zh from '../../i18n/locales/zh'
import en from '../../i18n/locales/en'

const page = (items: unknown[] = []) => ({ items, total: items.length, page: 1, page_size: 20, pages: items.length ? 1 : 0 })
const account = { id: '10000000-0000-4000-8000-000000000001', name: 'Fixture Claude', admission_state: 'pending', desired_state: 'paused', observed_at: null, version: 2 }
const mountView = (review = false) => mount(AccountsView, { props: { review }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { zh, en } })] } })
async function authenticated(transport: ReturnType<typeof vi.fn<typeof fetch>>) {
  vi.stubGlobal('fetch', transport)
  await session.refreshIdentity()
}
afterEach(() => { session.clear(); vi.unstubAllGlobals() })

describe('Claude account pool', () => {
  it('shows a real empty state without invented accounts or usage', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity())).mockResolvedValueOnce(success(page()))
    await authenticated(transport)
    const wrapper = mountView(); await flushPromises()
    expect(wrapper.text()).toContain('No accounts yet')
    expect(wrapper.find('tbody').exists()).toBe(false)
    expect(transport.mock.calls[1][0]).toBe('/api/supplier/v1/accounts?page=1&page_size=20&include_metrics=1')
    expect(wrapper.text()).not.toMatch(/Account review \(platform\)|Approve admission/)
    wrapper.unmount()
  })

  it('displays pending accounts as paused without an enable button', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity())).mockResolvedValueOnce(success(page([account])))
    await authenticated(transport)
    const wrapper = mountView(); await flushPromises()
    expect(wrapper.get('tbody').text()).toContain('Awaiting review')
    expect(wrapper.get('tbody').text()).toContain('Paused')
    expect(wrapper.find('tbody button').exists()).toBe(false)
    wrapper.unmount()
  })

  it('opens official authorization and submits the code only to the local completion API', async () => {
    const auth = { id: '20000000-0000-4000-8000-000000000001', authorization_url: 'https://claude.com/cai/oauth/authorize?state=fixture-state', expires_at: new Date(Date.now() + 600_000).toISOString() }
    const op = { id: '30000000-0000-4000-8000-000000000001', state: 'succeeded', step: 'awaiting_review', action: 'oauth.complete', account_id: account.id }
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity())).mockResolvedValueOnce(success(page()))
      .mockResolvedValueOnce(success(auth)).mockResolvedValueOnce(success(op)).mockResolvedValueOnce(success(page([account])))
    await authenticated(transport)
    const persist = vi.spyOn(Storage.prototype, 'setItem')
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('#account-name').setValue('Fixture Claude')
    await wrapper.get('form.account-add').trigger('submit'); await flushPromises()
    expect(wrapper.get('a').attributes('href')).toBe(auth.authorization_url)
    expect(wrapper.get('a').attributes('rel')).toContain('noopener')
    expect(wrapper.get('#authorization-code').attributes('type')).toBe('password')
    await wrapper.get('#authorization-code').setValue('fixture-code#fixture-state')
    await wrapper.get('form.oauth-flow').trigger('submit'); await flushPromises()
    const [url, options] = transport.mock.calls[3]
    expect(url).toBe(`/api/supplier/v1/oauth/sessions/${auth.id}/complete`)
    expect(JSON.parse(String(options?.body))).toEqual({ authorization_code: 'fixture-code#fixture-state' })
    expect(new Headers(options?.headers).get('Idempotency-Key')).toMatch(/^[a-f0-9-]{36}$/i)
    expect(wrapper.find('#authorization-code').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('fixture-code')
    expect(wrapper.get('tbody').text()).toContain('Awaiting review')
    expect(persist).not.toHaveBeenCalled(); persist.mockRestore(); wrapper.unmount()
  })

  it('retains the same completion key after an ambiguous network failure', async () => {
    const auth = { id: '20000000-0000-4000-8000-000000000001', authorization_url: 'https://claude.com/cai/oauth/authorize?state=fixture-state', expires_at: new Date(Date.now() + 600_000).toISOString() }
    const op = { id: '30000000-0000-4000-8000-000000000001', state: 'needs_attention', step: 'authorization_result_unknown', action: 'oauth.complete' }
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity())).mockResolvedValueOnce(success(page()))
      .mockResolvedValueOnce(success(auth)).mockRejectedValueOnce(new Error('timeout')).mockResolvedValueOnce(success(op)).mockResolvedValueOnce(success(page()))
    await authenticated(transport)
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('#account-name').setValue('Fixture')
    await wrapper.get('form.account-add').trigger('submit'); await flushPromises()
    await wrapper.get('#authorization-code').setValue('fixture-code#fixture-state')
    await wrapper.get('form.oauth-flow').trigger('submit'); await flushPromises()
    await wrapper.get('form.oauth-flow').trigger('submit'); await flushPromises()
    expect(new Headers(transport.mock.calls[3][1]?.headers).get('Idempotency-Key')).toBe(new Headers(transport.mock.calls[4][1]?.headers).get('Idempotency-Key'))
    expect(wrapper.get('[role="status"]').text()).toContain('manual reconciliation')
    wrapper.unmount()
  })

  it('platform review sends an explicit resource version and idempotency key', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity())).mockResolvedValueOnce(success(page([account])))
      .mockResolvedValueOnce(success({ id: '30000000-0000-4000-8000-000000000001', state: 'succeeded', step: 'confirmed', action: 'account.approve' })).mockResolvedValueOnce(success(page()))
    await authenticated(transport)
    const wrapper = mountView(true); await flushPromises()
    await wrapper.get('#review-reason').setValue('Verified fixture')
    await wrapper.get('tbody button').trigger('click'); await flushPromises()
    expect(transport.mock.calls[2][0]).toBe(`/api/supplier/v1/review/accounts/${account.id}/approve`)
    const headers = new Headers(transport.mock.calls[2][1]?.headers)
    expect(headers.get('If-Match')).toBe('2')
    expect(headers.get('X-CSRF-Token')).toBe(identity().csrf_token)
    expect(headers.get('Idempotency-Key')).toMatch(/^[a-f0-9-]{36}$/i)
    expect(wrapper.find('tbody').exists()).toBe(false)
    wrapper.unmount()
  })

  it('a failed refresh clears the old account list rather than retaining stale data', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity())).mockResolvedValueOnce(success(page([account])))
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: 503, reason: 'UPSTREAM_UNAVAILABLE' }), { status: 503 }))
    await authenticated(transport)
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('.section-heading button').trigger('click'); await flushPromises()
    expect(wrapper.find('tbody').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain('upstream service is unavailable')
    wrapper.unmount()
  })
})
