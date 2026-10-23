import { afterEach, describe, expect, it, vi } from 'vitest'
import { createSession } from '../useSession'
import { identity, success } from '../../api/__tests__/fixtures'

afterEach(() => vi.unstubAllGlobals())

describe('portal session', () => {
  it('2FA challenge is not authenticated and me is only requested after 2FA success', async () => {
    const transport = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(success({ requires_2fa: true, challenge_id: 'challenge-fixture', expires_at: '2099-01-01T00:00:00Z' }))
      .mockResolvedValueOnce(success({ requires_2fa: false }))
      .mockResolvedValueOnce(success(identity()))
    const state = createSession(transport)
    await state.login({ email: 'fixture@example.test', password: 'fixture-password' })
    expect(state.identity.value).toBeNull()
    expect(state.challenge.value?.challenge_id).toBe('challenge-fixture')
    expect(transport).toHaveBeenCalledTimes(1)
    await state.completeTwoFactor('123456')
    expect(state.challenge.value).toBeNull()
    expect(state.identity.value?.user.id).toBe('9223372036854775806')
    expect(transport.mock.calls.map(call => call[0])).toEqual(['/api/supplier/v1/auth/login', '/api/supplier/v1/auth/login/2fa', '/api/supplier/v1/me'])
  })

  it('expired challenges fail closed without an upstream call', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(success({ requires_2fa: true, challenge_id: 'challenge-fixture', expires_at: '2000-01-01T00:00:00Z' }))
    const state = createSession(transport)
    await state.login({ email: 'fixture@example.test', password: 'fixture-password' })
    await expect(state.completeTwoFactor('123456')).rejects.toMatchObject({ reason: 'CHALLENGE_EXPIRED' })
    expect(state.challenge.value).toBeNull()
    expect(transport).toHaveBeenCalledTimes(1)
  })

  it.each([401, 403])('%s from logout clears established identity and CSRF', async (status) => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity()))
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: status, reason: 'UNAUTHORIZED' }), { status }))
    const state = createSession(transport)
    await state.refreshIdentity()
    await expect(state.logout()).rejects.toMatchObject({ status })
    expect(state.identity.value).toBeNull()
    await expect(state.client.logout()).rejects.toMatchObject({ reason: 'CSRF_INVALID' })
  })

  it('supplier rejection on me clears current access', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity()))
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: 403, reason: 'SUPPLIER_DISABLED' }), { status: 403 }))
    const state = createSession(transport)
    await state.refreshIdentity()
    await expect(state.refreshIdentity()).rejects.toMatchObject({ status: 403 })
    expect(state.identity.value).toBeNull()
  })

  it('invalid supplier response clears previously loaded supplier information', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity()))
      .mockResolvedValueOnce(success({ user: identity().user, csrf_token: identity().csrf_token }))
    const state = createSession(transport)
    await state.refreshIdentity()
    await expect(state.refreshIdentity()).rejects.toMatchObject({ reason: 'INVALID_RESPONSE' })
    expect(state.identity.value).toBeNull()
    expect(state.challenge.value).toBeNull()
    await expect(state.client.logout()).rejects.toMatchObject({ reason: 'CSRF_INVALID' })
  })

  it('missing identity response data also clears previously loaded supplier information', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity())).mockResolvedValueOnce(success())
    const state = createSession(transport)
    await state.refreshIdentity()
    await expect(state.refreshIdentity()).rejects.toMatchObject({ reason: 'INVALID_RESPONSE' })
    expect(state.identity.value).toBeNull()
    await expect(state.client.logout()).rejects.toMatchObject({ reason: 'CSRF_INVALID' })
  })

  it('a null response envelope cannot retain previously loaded supplier information', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity()))
      .mockResolvedValueOnce(new Response('null', { status: 200 }))
    const state = createSession(transport)
    await state.refreshIdentity()
    await expect(state.refreshIdentity()).rejects.toMatchObject({ reason: 'INVALID_RESPONSE' })
    expect(state.identity.value).toBeNull()
    await expect(state.client.logout()).rejects.toMatchObject({ reason: 'CSRF_INVALID' })
  })

  it('does not restore identity from an in-flight me after local clear', async () => {
    let resolve: (response: Response) => void = () => undefined
    const transport = vi.fn<typeof fetch>().mockReturnValue(new Promise<Response>(done => { resolve = done }))
    const state = createSession(transport)
    const request = state.refreshIdentity()
    state.clear()
    resolve(success(identity()))
    await request
    expect(state.identity.value).toBeNull()
    await expect(state.client.logout()).rejects.toMatchObject({ reason: 'CSRF_INVALID' })
  })

  it('uses no browser persistence for login credentials or CSRF', async () => {
    const local = vi.spyOn(Storage.prototype, 'setItem')
    const cookie = vi.spyOn(Document.prototype, 'cookie', 'set')
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success({ requires_2fa: false })).mockResolvedValueOnce(success(identity()))
    const state = createSession(transport)
    await state.login({ email: 'fixture@example.test', password: 'fixture-password' })
    expect(local).not.toHaveBeenCalled()
    expect(cookie).not.toHaveBeenCalled()
  })

  it('does not claim server logout succeeded on network error', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity())).mockRejectedValueOnce(new Error('network failure'))
    const state = createSession(transport)
    await state.refreshIdentity()
    await expect(state.logout()).rejects.toMatchObject({ reason: 'NETWORK_ERROR' })
    expect(state.identity.value).not.toBeNull()
  })
})
