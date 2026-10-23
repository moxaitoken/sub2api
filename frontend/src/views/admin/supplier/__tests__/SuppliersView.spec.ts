import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import SuppliersView from '../SuppliersView.vue'


const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), create: vi.fn(), setStatus: vi.fn(), resetPassword: vi.fn(), accounts: vi.fn() }))
vi.mock('@/api/admin/suppliers', () => ({ supplierAdminAPI: api }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
const supplier = { id: 'fixture-supplier', name: 'Fixture supplier', login_account: 'fixture_vendor', portal_status: 'active', version: 3 }
const page = { items: [supplier], total: 1, page: 1, page_size: 20, pages: 1 }
const stubs = {
  AppLayout: { template: '<main><slot /></main>' },
  TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
  DataTable: defineComponent({ props: ['columns', 'data', 'loading'], template: '<div><article v-for="row in data" :key="row.id"><div v-for="col in columns" :key="col.key"><slot :name="`cell-${col.key}`" :row="row">{{ row[col.key] }}</slot></div></article></div>' }),
  BaseDialog: { props: ['show', 'title'], template: '<section v-if="show" role="dialog"><h2>{{ title }}</h2><slot /></section>' },
  RouterLink: { props:['to'], template:'<a :href="to"><slot /></a>' }, Pagination: { template: '<div />' }, Icon: true
}
const options = { global: { stubs } }
async function button(wrapper: ReturnType<typeof mount>, label: string) {
  const found = wrapper.findAll('button').find(item => item.text() === label)
  expect(found, label).toBeDefined()
  await found!.trigger('click'); await flushPromises()
}
beforeEach(() => {
  vi.resetAllMocks()
  api.list.mockResolvedValue(page); api.detail.mockResolvedValue(supplier)
  api.create.mockResolvedValue(supplier); api.setStatus.mockResolvedValue({ ...supplier, portal_status: 'disabled', version: 4 })
  api.resetPassword.mockResolvedValue({ ...supplier, version: 4 })
  api.accounts.mockResolvedValue({ ...page, items: [{ id: 'owned-account', name: 'Owned Claude', admission_state: 'approved', desired_state: 'paused' }] })
})
describe('main supplier administration', () => {
  it.each([
    ['INVALID_SUPPLIER_NAME','invalidName'], ['INVALID_SUPPLIER_LOGIN','invalidLogin'],
    ['INVALID_SUPPLIER_PASSWORD','invalidPassword'], ['INVALID_SUPPLIER_INPUT','invalid']
  ])('shows safe field diagnosis for %s', async (reason,key) => {
    const wrapper=mount(SuppliersView,options);await flushPromises()
    await button(wrapper,'supplierAdmin.create')
    api.create.mockRejectedValue({status:400,reason,message:'private-password-error'})
    const inputs=wrapper.get('[role="dialog"]').findAll('input')
    await inputs[0]!.setValue('Vendor');await inputs[1]!.setValue('vendor_01');await inputs[2]!.setValue('FixturePassword!2026')
    await wrapper.get('[role="dialog"] form').trigger('submit');await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe(`supplierAdmin.${key}`)
    expect(wrapper.text()).not.toContain('private-password-error');wrapper.unmount()
  })
  it('loads real suppliers and searches/reset without a second login form', async () => {
    const wrapper = mount(SuppliersView, options); await flushPromises()
    expect(api.list).toHaveBeenCalledWith('', 1)
    expect(wrapper.text()).toContain('Fixture supplier')
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    await wrapper.get('input').setValue('vendor')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.list).toHaveBeenLastCalledWith('vendor', 1)
    await button(wrapper, 'common.reset')
    expect(api.list).toHaveBeenLastCalledWith('', 1)
    wrapper.unmount()
  })
  it('creates through the existing business API and clears the password', async () => {
    const wrapper = mount(SuppliersView, options); await flushPromises()
    await button(wrapper, 'supplierAdmin.create')
    const dialog = wrapper.get('[role="dialog"]')
    const inputs = dialog.findAll('input')
    await inputs[0]!.setValue('New vendor'); await inputs[1]!.setValue('new_vendor'); await inputs[2]!.setValue('  Synthetic!@#$%2026  ')
    await dialog.get('form').trigger('submit'); await flushPromises()
    expect(api.create).toHaveBeenCalledWith({ name: 'New vendor', login_account: 'new_vendor', initial_password: '  Synthetic!@#$%2026  ' }, expect.any(String))
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('  Synthetic!@#$%2026  ')
    wrapper.unmount()
  })
  it('reads current details and resets with the current version', async () => {
    const wrapper = mount(SuppliersView, options); await flushPromises()
    await button(wrapper, 'supplierAdmin.details')
    expect(api.detail).toHaveBeenCalledWith(supplier.id)
    await button(wrapper, 'supplierAdmin.resetPassword')
    await wrapper.get('input[type="password"]').setValue('  Replacement!@#$%2026  ')
    await wrapper.get('[role="dialog"] form').trigger('submit'); await flushPromises()
    expect(api.resetPassword).toHaveBeenCalledWith(supplier, '  Replacement!@#$%2026  ', expect.any(String))
    wrapper.unmount()
  })
  it('requires a status confirmation and scopes account reads to the selected supplier', async () => {
    const wrapper = mount(SuppliersView, options); await flushPromises()
    await button(wrapper, 'supplierAdmin.disable')
    expect(api.setStatus).not.toHaveBeenCalled()
    await wrapper.get('[role="dialog"] form').trigger('submit'); await flushPromises()
    expect(api.setStatus).toHaveBeenCalledWith(supplier, false, expect.any(String))
    expect(wrapper.findAll('a').map(a => a.attributes('href'))).toEqual(['/admin/suppliers/fixture-supplier/accounts','/admin/suppliers/fixture-supplier/proxies','/admin/suppliers/fixture-supplier/usage'])
    wrapper.unmount()
  })
  it('clears protected rows after permission loss and does not show raw upstream errors', async () => {
    const wrapper = mount(SuppliersView, options); await flushPromises()
    api.list.mockRejectedValue({ status: 403, message: 'private upstream body' })
    await wrapper.get('button[aria-label="common.refresh"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('supplierAdmin.denied')
    expect(wrapper.text()).not.toContain('Fixture supplier')
    expect(wrapper.text()).not.toContain('private upstream body')
    wrapper.unmount()
  })
  it('keeps failed writes visible and reuses the idempotency key on retry', async () => {
    const wrapper = mount(SuppliersView, options); await flushPromises()
    await button(wrapper, 'supplierAdmin.disable')
    api.setStatus.mockRejectedValueOnce({ status: 503 })
    await wrapper.get('[role="dialog"] form').trigger('submit'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('supplierAdmin.failed')
    const first = api.setStatus.mock.calls[0]
    await wrapper.get('[role="dialog"] form').trigger('submit'); await flushPromises()
    expect(api.setStatus.mock.calls[1]).toEqual(first)
    wrapper.unmount()
  })
  it('disables settlement for a zero balance', async () => {
    api.list.mockResolvedValue({...page,items:[{...supplier,unsettled_usd:'0.00',settled_usd:'0.00',total_consumption_usd:'0.00'}]})
    const wrapper = mount(SuppliersView, options); await flushPromises()
    const settle = wrapper.findAll('button').find(b => b.text() === 'supplierAdmin.settle')
    expect(settle?.attributes('disabled')).toBeDefined(); expect(wrapper.text()).toContain('0.00'); wrapper.unmount()
  })
})
