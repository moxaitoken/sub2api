import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SupplierWorkspaceView from '../SupplierWorkspaceView.vue'
import zh from '../../i18n/locales/zh'
import en from '../../i18n/locales/en'
import type { SupplierAccount } from '../../api/types'

const first: SupplierAccount = { id: 'fixture-1', name: 'Alpha Claude', admission_state: 'approved', desired_state: 'paused', observed_at: null, version: 1 }
const second: SupplierAccount = { ...first, id: 'fixture-2', name: 'Beta Claude', admission_state: 'blocked', desired_state: 'running' }
const view = (props = {}) => mount(SupplierWorkspaceView, { props: { tab: 'accounts', accounts: [first, second], total: 42, page: 1, pages: 3, loading: false, failed: false, updatedAt: '', ...props }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { zh, en } })] } })

describe('supplier prototype business pages', () => {
  it('uses the server total, never converts sparse account states into quota or health metrics', () => {
    const w = view()
    expect(w.get('.account-count strong').text()).toBe('42')
    expect(w.findAll('.overview-metric strong').map(node => node.text())).toEqual(['—', '—', '—', '—', '—'])
    expect(w.text()).not.toMatch(/1,293|31,180|All platforms|Max 20x|Stripe/)
    expect(w.findAll('tbody tr')).toHaveLength(2)
    expect(w.findAll('tbody tr')[0].text()).toContain('Paused')
    w.unmount()
  })
  it('filters only the loaded page and preserves the server total', async () => {
    const w = view()
    await w.get('input[type=text],input:not([type])').setValue('Alpha')
    expect(w.findAll('tbody tr')).toHaveLength(1)
    expect(w.get('tbody').text()).toContain('Alpha Claude')
    expect(w.get('.account-count strong').text()).toBe('42')
    expect(w.get('.workspace-pagination').text()).toContain('1 matches on this page')
    await w.get(`select[aria-label="${en.accounts.state}"]`).setValue('blocked')
    expect(w.get('tbody').text()).toContain('No matching accounts')
    await w.findAll('button').find(b => b.text() === 'Reset')!.trigger('click')
    expect(w.findAll('tbody tr')).toHaveLength(2)
    w.unmount()
  })
  it('requests the next real page, not local demo pagination', async () => {
    const w = view()
    expect(w.get('button[aria-label="Previous"]').attributes('disabled')).toBeDefined()
    await w.get('button[aria-label="Next"]').trigger('click')
    expect(w.emitted('refresh')).toEqual([[2]])
    w.unmount()
  })
  it('shows loading and failed states without a false zero or empty-success claim', () => {
    const loading = view({ accounts: [], total: null, loading: true })
    expect(loading.get('tbody').text()).toContain('Loading accounts')
    loading.unmount()
    const failed = view({ accounts: [], total: null, failed: true })
    expect(failed.get('.account-count strong').text()).toBe('—')
    expect(failed.get('tbody').text()).toContain('Could not load accounts')
    expect(failed.get('tbody').text()).not.toContain('No accounts')
    failed.unmount()
  })
  it('shows actual account details without credentials and closes with Escape', async () => {
    const w = view()
    await w.get('button[aria-label="Account details Alpha Claude"]').trigger('click')
    expect(w.get('[role=dialog]').text()).toContain('fixture-1')
    expect(w.get('[role=dialog]').text()).toContain('Paused')
    expect(w.get('[role=dialog]').find('input').exists()).toBe(false)
    await w.get('[role=dialog]').trigger('keydown', { key: 'Escape' })
    expect(w.find('[role=dialog]').exists()).toBe(false)
    w.unmount()
  })
  it('supports column selection and overview collapse without changing server data', async () => {
    const w = view()
    await w.findAll('button').find(b => b.text() === 'Columns')!.trigger('click')
    const checkbox = w.findAll('.column-options label').find(label => label.text() === 'Today’s cost')!.get('input')
    await checkbox.setValue(false)
    expect(w.findAll('thead th').map(h => h.text())).not.toContain('Today’s cost')
    await w.findAll('button').find(b => b.text().startsWith('Collapse'))!.trigger('click')
    expect(w.find('.overview-metrics').exists()).toBe(false)
    expect(w.emitted('refresh')).toBeUndefined()
    w.unmount()
  })
  it('clears dialogs when switching supplier business pages', async () => {
    const w = view()
    await w.get('button[aria-label="Statistics scope"]').trigger('click')
    expect(w.find('[role=dialog]').exists()).toBe(true)
    await w.setProps({ tab: 'proxies' })
    expect(w.find('[role=dialog]').exists()).toBe(false)
    expect(w.get('h1').text()).toBe('Proxy management')
    w.unmount()
  })
  it('does not enable or simulate unconnected proxy/usage services', async () => {
    const w = view({ tab: 'proxies' })
    expect(w.get('tbody').text()).toContain('Proxy service not connected')
    expect(w.findAll('button').every(b => b.attributes('disabled') !== undefined)).toBe(true)
    await w.setProps({ tab: 'usage' })
    expect(w.findAll('.usage-card strong').map(node => node.text())).toEqual(['—', '—', '—', '—'])
    expect(w.findAll('select').every(s => s.attributes('disabled') !== undefined)).toBe(true)
    expect(w.text()).not.toContain('$0.00')
    w.unmount()
  })
  it('traps keyboard focus inside the account dialog and restores it on close', async () => {
    const w = view()
    const focus = vi.spyOn(HTMLElement.prototype, 'focus')
    await w.get('button[aria-label="Account details Alpha Claude"]').trigger('click')
    expect(focus).toHaveBeenCalled()
    await w.get('[role=dialog]').trigger('keydown', { key: 'Tab' })
    await w.get('[role=dialog]').trigger('keydown', { key: 'Escape' })
    expect(focus.mock.calls.length).toBeGreaterThan(1)
    focus.mockRestore(); w.unmount()
  })
})
