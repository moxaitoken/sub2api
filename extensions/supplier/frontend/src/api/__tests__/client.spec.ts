import { describe, expect, it, vi } from 'vitest'
import { ApiError, SupplierClient } from '../client'
import { errorKey } from '../errorText'
import { identity, success } from './fixtures'

describe('supplier API client', () => {
  it.each([['PROXY_CHECK_FAILED',422,'errors.proxyCheckFailed'],['PROXY_CLEANUP_PENDING',503,'errors.proxyCleanupPending'],['PROXY_ADDRESS_REJECTED',400,'errors.proxyAddress'],['PROXY_DNS_REJECTED',400,'errors.proxyDns']] as const)('preserves safe proxy diagnosis %s',async(reason,status,key)=>{
    const transport=vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({code:status,reason,message:'private-secret'}),{status}))
    try { await new SupplierClient(vi.fn(),transport).me(); expect.unreachable() }
    catch(error) { expect(error).toBeInstanceOf(ApiError);expect(errorKey(error)).toBe(key);expect(String(error)).not.toContain('private-secret') }
  })
  it('only sends credentials to same-origin fixed API; login needs no CSRF', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(success({ requires_2fa: false }))
    const client = new SupplierClient(vi.fn(), transport)
    await client.login({ email: 'fixture@example.test', password: 'fixture-password', turnstile_token: 'fixture-proof' })
    const [url, options] = transport.mock.calls[0]
    expect(url).toBe('/api/supplier/v1/auth/login')
    expect(options).toMatchObject({ method: 'POST', credentials: 'include', cache: 'no-store', redirect: 'error' })
    expect(new Headers(options?.headers).has('X-CSRF-Token')).toBe(false)
    expect(JSON.parse(String(options?.body))).toMatchObject({ turnstile_token: 'fixture-proof' })
  })

  it('rejects writes without CSRF before transport; me provides token to writes', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity())).mockResolvedValueOnce(success())
    const client = new SupplierClient(vi.fn(), transport)
    await expect(client.logout()).rejects.toMatchObject({ reason: 'CSRF_INVALID' })
    expect(transport).not.toHaveBeenCalled()
    await client.me()
    await client.logout()
    expect(new Headers(transport.mock.calls[1][1]?.headers).get('X-CSRF-Token')).toBe(identity().csrf_token)
    await expect(client.logout()).rejects.toMatchObject({ reason: 'CSRF_INVALID' })
    expect(transport).toHaveBeenCalledTimes(2)
  })

  it.each([401, 403])('%s invalidates identity even when body is not JSON', async (status) => {
    const clear = vi.fn()
    const client = new SupplierClient(clear, vi.fn<typeof fetch>().mockResolvedValue(new Response('unavailable', { status })))
    await expect(client.me()).rejects.toMatchObject({ status })
    expect(clear).toHaveBeenCalledOnce()
  })

  it.each([401, 403])('an envelope %s invalidates identity', async (code) => {
    const clear = vi.fn()
    const transport = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ code }), { status: 200 }))
    await expect(new SupplierClient(clear, transport).me()).rejects.toMatchObject({ status: 200 })
    expect(clear).toHaveBeenCalledOnce()
  })

  it('has no member actions and supplier clients cannot provision accounts', async () => {
    const client = new SupplierClient(vi.fn())
    expect(client).not.toHaveProperty('members')
    await expect(client.createSupplier('Fixture', 'vendor_01', 'FixturePassword!2026', 'fixture-key')).rejects.toMatchObject({status: 403, reason: 'ACCESS_DENIED'})
    expect(client).not.toHaveProperty('addMember')
  })

  it('never propagates raw server error message or arbitrary reason/request ID', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ code: 502, reason: 'private-upstream-token', message: 'private-upstream-password', request_id: 'another-secret' }), { status: 502 }))
    const client = new SupplierClient(vi.fn(), transport)
    try { await client.me(); expect.unreachable() } catch (error) {
      expect(error).toBeInstanceOf(ApiError)
      expect(String(error)).not.toContain('private-')
      expect(error).toMatchObject({ reason: 'REQUEST_FAILED', requestId: undefined })
      expect(errorKey(error)).toBe('errors.generic')
    }
  })

  it('preserves safe operational error and request ID', async () => {
    const client = new SupplierClient(vi.fn(), vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ code: 423, reason: 'UPSTREAM_ACTION_REQUIRED', request_id: '11111111-1111-4111-8111-111111111111' }), { status: 423 })))
    try { await client.me(); expect.unreachable() } catch (error) {
      expect(error).toMatchObject({ reason: 'UPSTREAM_ACTION_REQUIRED', requestId: '11111111-1111-4111-8111-111111111111' })
      expect(errorKey(error)).toBe('errors.actionRequired')
    }
  })

  it('sends exact 2FA contract without upstream temp token', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValue(success({ requires_2fa: false }))
    const client = new SupplierClient(vi.fn(), transport)
    await client.completeTwoFactor('challenge-fixture', '123456')
    expect(transport.mock.calls[0][0]).toBe('/api/supplier/v1/auth/login/2fa')
    expect(JSON.parse(String(transport.mock.calls[0][1]?.body))).toEqual({ challenge_id: 'challenge-fixture', totp_code: '123456' })
  })

  it('accepts the minimal supplier identity and preserves bigint IDs as strings', async () => {
    const profile = identity()
    profile.supplier.id = '9223372036854775805'
    const transport = vi.fn<typeof fetch>().mockResolvedValue(success(profile))
    const client = new SupplierClient(vi.fn(), transport)
    expect(await client.me()).toEqual(profile)
    expect(transport.mock.calls.map(call => call[0])).toEqual(['/api/supplier/v1/me'])
  })

  it.each([
    ['missing supplier', undefined],
    ['null supplier', null],
    ['empty supplier ID', { ...identity().supplier, id: '' }],
    ['numeric supplier ID', { ...identity().supplier, id: 42 }],
    ['blank supplier name', { ...identity().supplier, name: ' ' }],
    ['invalid supplier status', { ...identity().supplier, portal_status: 'unknown' }],
    ['invalid supplier version', { ...identity().supplier, version: 0 }]
  ])('rejects %s and clears stale identity and CSRF', async (_description, supplier) => {
    const clear = vi.fn()
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success(identity()))
      .mockResolvedValueOnce(success({ ...identity(), supplier }))
    const client = new SupplierClient(clear, transport)
    await client.me()
    await expect(client.me()).rejects.toMatchObject({ reason: 'INVALID_RESPONSE' })
    expect(clear).toHaveBeenCalledOnce()
    await expect(client.logout()).rejects.toMatchObject({ reason: 'CSRF_INVALID' })
    expect(transport).toHaveBeenCalledTimes(2)
  })

  it('maps network faults to a safe error', async () => {
    const client = new SupplierClient(vi.fn(), vi.fn<typeof fetch>().mockRejectedValue(new Error('sensitive transport details')))
    await expect(client.me()).rejects.toMatchObject({ status: 0, reason: 'NETWORK_ERROR', message: 'Supplier request failed' })
  })

  it('rejects malformed login and 2FA expiry responses without authenticating', async () => {
    const transport = vi.fn<typeof fetch>().mockResolvedValueOnce(success({})).mockResolvedValueOnce(success({ requires_2fa: true, challenge_id: 'fixture', expires_at: 'invalid' }))
    const client = new SupplierClient(vi.fn(), transport)
    await expect(client.login({ email: 'fixture@example.test', password: 'fixture-password' })).rejects.toMatchObject({ reason: 'INVALID_RESPONSE' })
    await expect(client.login({ email: 'fixture@example.test', password: 'fixture-password' })).rejects.toMatchObject({ reason: 'INVALID_RESPONSE' })
    expect(transport).toHaveBeenCalledTimes(2)
  })
})


describe('owned proxy response safety',()=>{
 it('accepts explicit standard HTTP/HTTPS ports while discarding unexpected secrets',async()=>{
  for(const address of ['http://8.8.8.8:80','https://8.8.8.8:443']){
   const transport=vi.fn<typeof fetch>().mockResolvedValue(success({items:[{id:'fixture-proxy',name:'Fixture',address,region:'SG',status:'active',capacity:1,accounts:0,held:0,version:2,password:'must-not-propagate'}]}));const client=new SupplierClient(()=>undefined,transport)
   const result=await client.ownProxies();expect(result.items[0].address).toBe(address);expect(JSON.stringify(result)).not.toContain('must-not-propagate')
  }
 })
 it('rejects an authentication-bearing address from the server',async()=>{
  const transport=vi.fn<typeof fetch>().mockResolvedValue(success({items:[{id:'fixture-proxy',name:'Fixture',address:'http://user:secret@8.8.8.8:8080',region:'SG',status:'active',capacity:1,accounts:0,held:0,version:2}]}));const client=new SupplierClient(()=>undefined,transport)
  await expect(client.ownProxies()).rejects.toMatchObject({reason:'INVALID_RESPONSE'})
 })
})
