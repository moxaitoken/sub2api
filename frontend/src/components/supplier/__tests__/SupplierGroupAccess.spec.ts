import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
const { get, set } = vi.hoisted(() => ({ get: vi.fn(), set: vi.fn() }))
vi.mock('@/api/admin/suppliers', () => ({ supplierGroupAPI: { get, set } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
import SupplierGroupAccess from '../SupplierGroupAccess.vue'
beforeEach(() => { vi.clearAllMocks(); get.mockResolvedValue({ group_id: 2, enabled: false, version: 7 }); set.mockResolvedValue({ group_id: 2, enabled: true, version: 8 }) })
describe('supplier group admission setting', () => {
  it('loads and independently saves with optimistic version; no group or pricing writes', async () => {
    const w = mount(SupplierGroupAccess, { props: { groupId: 2, platform: 'anthropic', status: 'active' } })
    await flushPromises(); await w.get('input').setValue(true); await w.get('button').trigger('click'); await flushPromises()
    expect(set).toHaveBeenCalledWith(2, true, 7); expect(w.find('[role="status"]').exists()).toBe(true)
  })
  it('does not turn a failed read into a disabled saved policy', async () => {
    get.mockRejectedValue(new Error('unavailable'))
    const w = mount(SupplierGroupAccess, { props: { groupId: 2, platform: 'anthropic', status: 'active' } })
    await flushPromises(); expect(w.get('input').attributes('disabled')).toBeDefined(); expect(w.find('[role="alert"]').exists()).toBe(true); expect(set).not.toHaveBeenCalled()
  })
  it('disallows enabling inactive groups but allows disabling existing admission', async () => {
    const w = mount(SupplierGroupAccess, { props: { groupId: 2, platform: 'anthropic', status: 'inactive' } })
    await flushPromises(); expect(w.get('input').attributes('disabled')).toBeDefined()
    get.mockResolvedValue({ group_id: 3, enabled: true, version: 2 }); await w.setProps({ groupId: 3 }); await flushPromises()
    expect(w.get('input').attributes('disabled')).toBeUndefined()
    await w.get('input').setValue(false); await w.get('button').trigger('click'); expect(set).toHaveBeenCalledWith(3, false, 2)
  })
  it('shows conflicts and keeps unsaved choice without retrying a write', async () => {
    set.mockRejectedValue(new Error('conflict'))
    const w = mount(SupplierGroupAccess, { props: { groupId: 2, platform: 'anthropic', status: 'active' } })
    await flushPromises(); await w.get('input').setValue(true); await w.get('button').trigger('click'); await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true); expect(set).toHaveBeenCalledTimes(1)
  })
})
