import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const appStoreState = {
  versionLoading: false,
  currentVersion: '1.2.3',
  latestVersion: '',
  hasUpdate: false,
  releaseInfo: null as unknown,
  buildType: 'release',
  upstreamVersion: 'v9.9.9',
  buildCommit: '2776c84a5ebcc94d2fba1f551c200ab1d9ffe149',
  updateDisabled: true,
  fetchVersion: vi.fn(),
  clearVersionCache: vi.fn()
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, values?: { commit: string }) => values ? `${key}: [${values.commit}]` : key
  })
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({ isAdmin: true }),
  useAppStore: () => appStoreState
}))

vi.mock('@/api/admin/system', () => ({
  performUpdate: vi.fn(),
  restartService: vi.fn(),
  getRollbackVersions: vi.fn().mockResolvedValue({ versions: [] }),
  rollback: vi.fn()
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() })
}))

import VersionBadge from '../VersionBadge.vue'

const stubs = {
  Icon: { template: '<span />' }
}

async function mountOpened() {
  const wrapper = mount(VersionBadge, { global: { stubs } })
  await wrapper.find('button').trigger('click')
  await nextTick()
  return wrapper
}

describe('VersionBadge', () => {
  it('shows only the version when online update check is disabled', async () => {
    appStoreState.updateDisabled = true
    appStoreState.hasUpdate = false
    const wrapper = await mountOpened()

    const badgeButton = wrapper.find('button')
    expect(badgeButton.text()).toBe('v1.2.3')
    expect(badgeButton.attributes('title')).toBeUndefined()
    // no update dot / ping animation
    expect(wrapper.find('.animate-ping').exists()).toBe(false)

    const text = wrapper.text()
    // header + version only
    expect(text).toContain('version.currentVersion')
    expect(text).toContain('v1.2.3')

    for (const hidden of [
      'version.refresh',
      'version.checkDisabled',
      'version.upToDate',
      'version.latestVersion',
      'version.updateAvailable',
      'version.updateNow',
      'version.viewRelease',
      'version.viewChangelog',
      'version.rollback'
    ]) {
      expect(text, `${hidden} should be hidden`).not.toContain(hidden)
    }
    expect(wrapper.get('[data-testid="build-commit"]').text()).toBe('version.buildCommit: [2776c84a5]')
    expect(wrapper.get('[data-testid="build-commit"]').attributes('title')).toBe(appStoreState.buildCommit)
    expect(text).not.toContain('9.9.9')
    // only the badge button itself remains clickable in the popover
    expect(wrapper.findAll('button')).toHaveLength(1)
  })

  it('does not flag an update while the check is disabled even if the store says so', async () => {
    appStoreState.updateDisabled = true
    appStoreState.hasUpdate = true
    const wrapper = await mountOpened()

    expect(wrapper.find('.animate-ping').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('version.updateAvailable')
  })

  it('keeps the update-check UI when online update check is enabled', async () => {
    appStoreState.updateDisabled = false
    appStoreState.hasUpdate = false
    const wrapper = await mountOpened()

    const text = wrapper.text()
    expect(text).toContain('version.currentVersion')
    expect(text).toContain('version.upToDate')
    expect(text).toContain('version.rollback')
    // refresh button is back
    expect(wrapper.findAll('button').length).toBeGreaterThan(1)
    expect(text).toContain('version.buildCommit: [2776c84a5]')
    expect(text).not.toContain('9.9.9')
  })

  it.each(['', '   ', 'unknown', 'docker', '123xyz7'])('hides an invalid build commit (%j)', async (commit) => {
    appStoreState.buildCommit = commit
    const wrapper = await mountOpened()
    expect(wrapper.find('[data-testid="build-commit"]').exists()).toBe(false)
    appStoreState.buildCommit = '2776c84a5ebcc94d2fba1f551c200ab1d9ffe149'
  })

  it('accepts a legacy short build commit', async () => {
    appStoreState.buildCommit = ' ABCDEF0 '
    const wrapper = await mountOpened()
    expect(wrapper.get('[data-testid="build-commit"]').text()).toBe('version.buildCommit: [abcdef0]')
    appStoreState.buildCommit = '2776c84a5ebcc94d2fba1f551c200ab1d9ffe149'
  })
})
