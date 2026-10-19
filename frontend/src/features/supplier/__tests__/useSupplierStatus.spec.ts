import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, reactive } from 'vue'

const getMe = vi.hoisted(() => vi.fn())
vi.mock('../api', () => ({ getMe }))

const auth = vi.hoisted(() => ({ store: null as null | { isAuthenticated: boolean; user: { id: number } | null } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth.store }))

const flush = () => new Promise((resolve) => setTimeout(resolve, 0))

describe('useSupplierStatus', () => {
  beforeEach(() => {
    vi.resetModules()
    getMe.mockReset()
    sessionStorage.clear()
    auth.store = reactive({ isAuthenticated: true, user: { id: 7 } })
  })

  it('shows the entry for an active supplier', async () => {
    getMe.mockResolvedValue({ id: 1, status: 'active' })
    const { useSupplierStatus } = await import('../useSupplierStatus')
    const { isSupplier } = useSupplierStatus()
    await flush()
    expect(isSupplier.value).toBe(true)
  })

  it('remembers a 403 for the session instead of asking on every page load', async () => {
    getMe.mockRejectedValue({ status: 403 })
    let mod = await import('../useSupplierStatus')
    expect(mod.useSupplierStatus().isSupplier.value).toBe(false)
    await flush()
    expect(getMe).toHaveBeenCalledTimes(1)

    // A full page reload re-evaluates the module; the cached answer avoids another request.
    vi.resetModules()
    mod = await import('../useSupplierStatus')
    expect(mod.useSupplierStatus().isSupplier.value).toBe(false)
    await flush()
    expect(getMe).toHaveBeenCalledTimes(1)
  })

  it('a forced refresh bypasses the cache, and a different user is checked again', async () => {
    getMe.mockRejectedValueOnce({ status: 403 })
    const { useSupplierStatus } = await import('../useSupplierStatus')
    const status = useSupplierStatus()
    await flush()
    expect(status.isSupplier.value).toBe(false)

    getMe.mockResolvedValueOnce({ id: 1, status: 'active' })
    await status.refresh()
    expect(status.isSupplier.value).toBe(true)

    getMe.mockRejectedValueOnce({ status: 403 })
    auth.store!.user = { id: 8 }
    await nextTick()
    await flush()
    expect(getMe).toHaveBeenCalledTimes(3)
    expect(status.isSupplier.value).toBe(false)
  })
})
