import { readonly, ref } from 'vue'
import { ApiError, SupplierClient } from '../api/client'
import type { Identity, LoginInput, LoginResult } from '../api/types'

export function createSession(transport?: typeof fetch) {
  const identity = ref<Identity | null>(null)
  const challenge = ref<Extract<LoginResult, { requires_2fa: true }> | null>(null)
  let generation = 0
  const clear = () => {
    generation += 1
    identity.value = null
    challenge.value = null
    client.clearSession()
  }
  const client = new SupplierClient(clear, transport)

  async function refreshIdentity(): Promise<void> {
    const expected = generation
    try {
      const result = await client.me()
      if (generation === expected) identity.value = result
      else client.clearSession()
    } catch (error) {
      if (error instanceof ApiError && ([401, 403].includes(error.status) || error.reason === 'INVALID_RESPONSE')) clear()
      throw error
    }
  }

  async function acceptLogin(result: LoginResult): Promise<void> {
    if (result.requires_2fa) {
      identity.value = null
      challenge.value = result
    } else {
      challenge.value = null
      await refreshIdentity()
    }
  }

  return {
    identity: readonly(identity), challenge: readonly(challenge), client,
    refreshIdentity, clear,
    async login(input: LoginInput) {
      clear()
      await acceptLogin(await client.login(input))
    },
    async completeTwoFactor(code: string) {
      const current = challenge.value
      if (!current || Date.parse(current.expires_at) <= Date.now()) {
        clear()
        throw new ApiError(401, 'CHALLENGE_EXPIRED')
      }
      await acceptLogin(await client.completeTwoFactor(current.challenge_id, code))
    },
    async logout() {
      await client.logout()
      clear()
    }
  }
}

export const session = createSession()
