import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import VersionBadge from '../VersionBadge.vue'

const mocks = vi.hoisted(() => ({ fetch: vi.fn(), clear: vi.fn(), copy: vi.fn(), portal: vi.fn() }))
const auth = reactive({ isSuperAdmin: true, isAdmin: true })
const app = reactive({
  versionLoaded: true, versionLoading: false, currentVersion: '1.2.3', upstreamVersion: 'v1.2.0',
  buildCommit: 'a'.repeat(40), fetchVersion: mocks.fetch, clearVersionCache: mocks.clear
})
vi.mock('@/stores', () => ({ useAuthStore: () => auth, useAppStore: () => app }))
vi.mock('@/features/vendor/version', () => ({ usePortalVersion: mocks.portal }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: false, copyToClipboard: mocks.copy }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.clearAllMocks()
  auth.isSuperAdmin = true
  auth.isAdmin = true
  app.versionLoaded = true
  app.versionLoading = false
  app.currentVersion = '1.2.3'
  app.upstreamVersion = 'v1.2.0'
  app.buildCommit = 'a'.repeat(40)
  mocks.fetch.mockResolvedValue({ version: '1.2.3' })
  mocks.copy.mockResolvedValue(true)
})

async function open(props: { version?: string; compact?: boolean; collapsed?: boolean } = {}) {
  const wrapper = mount(VersionBadge, { props, global: { stubs: { Icon: true } } })
  await flushPromises()
  await wrapper.get('button').trigger('click')
  return wrapper
}

describe('read-only version information', () => {
  it.each([false, true])('shows only the supplied public version to a non-super-admin (admin=%s)', async (isAdmin) => {
    auth.isSuperAdmin = false
    auth.isAdmin = isAdmin
    const wrapper = mount(VersionBadge, { props: { version: '0.9.0' } })
    await flushPromises()
    expect(wrapper.text()).toBe('v0.9.0')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(mocks.fetch).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain(app.buildCommit)
  })

  it('reads metadata and copies the full commit without deployment actions', async () => {
    const wrapper = await open()
    expect(mocks.fetch).toHaveBeenCalledOnce()
    expect(mocks.fetch).toHaveBeenCalledWith(false)
    expect(wrapper.get('[data-testid="current-version"]').text()).toBe('v1.2.3')
    expect(wrapper.get('[data-testid="upstream-version"]').text()).toBe('v1.2.0')
    expect(wrapper.get('[data-testid="build-commit"]').attributes('title')).toBe('a'.repeat(40))
    expect(wrapper.findAll('button').map(button => button.text())).toEqual(['v1.2.3', 'version.refresh', 'version.copyDetails'])
    const copy = wrapper.findAll('button').find(button => button.text() === 'version.copyDetails')
    expect(copy).toBeDefined()
    await copy?.trigger('click')
    expect(mocks.copy).toHaveBeenCalledWith('version.currentVersion: v1.2.3\nversion.upstreamVersion: v1.2.0\nversion.buildCommit: ' + 'a'.repeat(40))
    expect(wrapper.find('a').exists()).toBe(false)
    expect(wrapper.find('code').exists()).toBe(false)
  })

  it.each(['dev', 'source', 'build-abcdef', 'v1.2.3'])('preserves the actual build identifier %s', async (version) => {
    app.currentVersion = version
    const wrapper = await open()
    expect(wrapper.get('[data-testid="current-version"]').text()).toBe(version)
  })

  it('refreshes metadata only on explicit interaction', async () => {
    const wrapper = await open()
    const refresh = wrapper.findAll('button').find(button => button.text() === 'version.refresh')
    await refresh?.trigger('click')
    await flushPromises()
    expect(mocks.fetch.mock.calls).toEqual([[false], [true]])
  })

  it('preserves compact and collapsed presentation with an accessible details button', async () => {
    const wrapper = await open({ compact: true, collapsed: true })
    expect(wrapper.get('button').classes()).toContain('p-1')
    expect(wrapper.get('button').attributes('aria-label')).toBe('version.details')
    expect(wrapper.get('button span').classes()).toContain('sr-only')
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('true')
  })

  it.each(['', 'unknown', '  UNKNOWN  '])('does not fabricate a missing version (%j)', async (value) => {
    app.currentVersion = value
    app.upstreamVersion = ''
    app.buildCommit = ''
    const wrapper = await open()
    expect(wrapper.get('[data-testid="current-version"]').text()).toBe('—')
    const copy = wrapper.findAll('button').find(button => button.text() === 'version.copyDetails')
    expect(copy?.attributes('disabled')).toBeDefined()
  })

  it.each(['', 'unknown', 'docker', '123xyz7'])('omits an invalid commit (%j)', async (commit) => {
    app.buildCommit = commit
    const wrapper = await open()
    expect(wrapper.find('[data-testid="build-commit"]').exists()).toBe(false)
  })

  it('shows a read failure without claiming the running version is current', async () => {
    mocks.fetch.mockRejectedValue(new Error('unavailable'))
    const wrapper = await open()
    expect(wrapper.get('[role="status"]').text()).toBe('version.unavailable')
  })

  it('closes privileged details immediately when the role is revoked', async () => {
    const wrapper = await open({ version: '0.9.0' })
    auth.isSuperAdmin = false
    await flushPromises()
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.text()).toBe('v0.9.0')
    expect(mocks.copy).not.toHaveBeenCalled()
  })
})
