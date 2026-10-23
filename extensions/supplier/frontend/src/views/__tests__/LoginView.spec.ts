import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import LoginView from '../LoginView.vue'
import { session } from '../../composables/useSession'
import zh from '../../i18n/locales/zh'
import en from '../../i18n/locales/en'
import { identity, success } from '../../api/__tests__/fixtures'

afterEach(() => { session.clear(); vi.unstubAllGlobals() })

describe('login form', () => {
  it('clears password immediately after submit, never persists it, and renders safe failures', async () => {
    let resolve: (response: Response) => void = () => undefined
    const transport = vi.fn<typeof fetch>().mockReturnValue(new Promise<Response>(done => { resolve = done }))
    vi.stubGlobal('fetch', transport)
    const persistence = vi.spyOn(Storage.prototype, 'setItem')
    const wrapper = mount(LoginView, { global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { zh, en } })] } })
    await wrapper.get('#email').setValue('fixture@example.test')
    await wrapper.get('#password').setValue('fixture-password-never-persist')
    await wrapper.get('form').trigger('submit')
    expect((wrapper.get('#password').element as HTMLInputElement).value).toBe('')
    expect(persistence).not.toHaveBeenCalled()
    resolve(new Response(JSON.stringify({ code: 401, message: 'upstream-secret-do-not-show', reason: 'INVALID_CREDENTIALS' }), { status: 401 }))
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('credentials are invalid')
    expect(wrapper.text()).not.toContain('upstream-secret')
    expect(wrapper.text()).not.toContain('fixture-password')
    wrapper.unmount()
  })

  it('only asks for 2FA when upstream requests it, without any enrollment controls', async () => {
    const transport = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(success({ requires_2fa: true, challenge_id: 'challenge-fixture', expires_at: '2099-01-01T00:00:00Z' }))
      .mockResolvedValueOnce(success({ requires_2fa: false }))
      .mockResolvedValueOnce(success(identity()))
    vi.stubGlobal('fetch', transport)
    const wrapper = mount(LoginView, { global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { zh, en } })] } })
    expect(wrapper.find('#totp').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/Enable|Configure|Enroll|Two-factor/)
    await wrapper.get('#email').setValue('fixture@example.test')
    await wrapper.get('#password').setValue('fixture-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('#password').exists()).toBe(false)
    expect(wrapper.find('#totp').exists()).toBe(true)
    expect(session.identity.value).toBeNull()
    await wrapper.get('#totp').setValue('123456')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(session.identity.value?.supplier.name).toBe('Fixture supplier')
    expect(transport.mock.calls.map(call => call[0])).toEqual(['/api/supplier/v1/auth/login', '/api/supplier/v1/auth/login/2fa', '/api/supplier/v1/me'])
    expect(JSON.parse(String(transport.mock.calls[1][1]?.body))).toEqual({ challenge_id: 'challenge-fixture', totp_code: '123456' })
    wrapper.unmount()
  })
})
