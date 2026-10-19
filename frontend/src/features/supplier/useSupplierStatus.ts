import { computed, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { getMe } from './api'
import type { Supplier } from './types'

// Shared across components: the sidebar and the console page read the same state.
const supplier = ref<Supplier | null>(null)
const checkedForUser = ref<number | null>(null)
let inflight: Promise<void> | null = null

// A negative answer is remembered for the browser session so non-suppliers do not hit
// /supplier/me (and log a 403) on every page load. Becoming a supplier mid-session needs
// a new session or opening /supplier directly, which always asks the server.
const NOT_SUPPLIER_KEY = 'supplier_status_negative_user'

async function check(userId: number, force = false): Promise<void> {
  try {
    if (!force && sessionStorage.getItem(NOT_SUPPLIER_KEY) === String(userId)) {
      supplier.value = null
      return
    }
    supplier.value = await getMe()
    sessionStorage.removeItem(NOT_SUPPLIER_KEY)
  } catch {
    // 403 means "not a supplier"; any other failure also just hides the entry.
    supplier.value = null
    sessionStorage.setItem(NOT_SUPPLIER_KEY, String(userId))
  } finally {
    checkedForUser.value = userId
  }
}

/**
 * Whether the signed-in user is an active supplier. The check runs once per user and is
 * re-run when the user changes (login / logout / switch).
 */
export function useSupplierStatus() {
  const authStore = useAuthStore()

  watch(
    () => (authStore.isAuthenticated ? authStore.user?.id ?? null : null),
    (userId) => {
      if (userId == null) {
        supplier.value = null
        checkedForUser.value = null
        return
      }
      if (checkedForUser.value === userId || inflight) return
      inflight = check(userId).finally(() => {
        inflight = null
      })
    },
    { immediate: true },
  )

  return {
    supplier,
    isSupplier: computed(() => supplier.value?.status === 'active'),
    refresh: () => (authStore.user?.id ? check(authStore.user.id, true) : Promise.resolve()),
  }
}
