import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import VersionBadge from '../VersionBadge.vue'

const { appStore, authStore, systemAPI } = vi.hoisted(() => ({
  appStore: {
    versionLoading: false,
    currentVersion: '0.2.17',
    latestVersion: '0.2.18',
    hasUpdate: true,
    buildType: 'release',
    releaseInfo: { html_url: 'https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.18' },
    fetchVersion: vi.fn(),
    clearVersionCache: vi.fn()
  },
  authStore: { isAdmin: true },
  systemAPI: {
    performUpdate: vi.fn(),
    restartService: vi.fn(),
    getRollbackVersions: vi.fn(),
    rollback: vi.fn()
  }
}))

vi.mock('@/stores', () => ({ useAppStore: () => appStore, useAuthStore: () => authStore }))
vi.mock('@/api/admin/system', () => systemAPI)
vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() })
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

async function openBadge() {
  const wrapper = mount(VersionBadge, {
    props: { version: '0.2.17' },
    global: { stubs: { Icon: true, transition: false } }
  })
  await wrapper.get('button').trigger('click')
  return wrapper
}

describe('fork version badge', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    authStore.isAdmin = true
    appStore.buildType = 'release'
    appStore.hasUpdate = true
    appStore.versionLoading = false
    appStore.releaseInfo = {
      html_url: 'https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.18'
    }
  })

  it.each(['release', 'source'])('keeps %s builds read-only when updates exist', async (build) => {
    appStore.buildType = build
    const wrapper = await openBadge()
    const buttons = wrapper.findAll('button').map((button) => button.text()).join(' ')
    console.info(`build=${build} updateNowVisible=${buttons.includes('version.updateNow')}`)
    expect(buttons).not.toContain('version.updateNow')
    expect(wrapper.text()).toContain('version.forkUpgradeHint')
    expect(wrapper.text()).toContain('0.2.18')
    expect(wrapper.get('a').attributes('href')).toBe(appStore.releaseInfo.html_url)
    expect(wrapper.text()).not.toContain('version.sourceModeHint')
    expect(systemAPI.performUpdate).not.toHaveBeenCalled()
    expect(systemAPI.getRollbackVersions).not.toHaveBeenCalled()
    expect(systemAPI.restartService).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('does not offer official rollback or manual upstream replacement commands', async () => {
    appStore.hasUpdate = false
    const wrapper = await openBadge()
    const buttons = wrapper.findAll('button').map((button) => button.text()).join(' ')
    expect(buttons).not.toContain('version.rollback')
    expect(wrapper.find('code').exists()).toBe(false)
    expect(wrapper.text()).toContain('version.forkUpgradeHint')
    wrapper.unmount()
  })

  it('keeps refresh available without invoking a mutation', async () => {
    const wrapper = await openBadge()
    await wrapper.get('button[title="version.refresh"]').trigger('click')
    await flushPromises()
    expect(appStore.fetchVersion).toHaveBeenCalledWith(false)
    expect(appStore.fetchVersion).toHaveBeenCalledWith(true)
    expect(systemAPI.performUpdate).not.toHaveBeenCalled()
    expect(systemAPI.rollback).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('does not display invalid placeholder release links', async () => {
    appStore.releaseInfo = { html_url: '#' }
    const wrapper = await openBadge()
    expect(wrapper.find('a').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps non-admin version text static', () => {
    authStore.isAdmin = false
    const wrapper = mount(VersionBadge, { props: { version: '0.2.17' } })
    expect(wrapper.text()).toContain('v0.2.17')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(appStore.fetchVersion).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
