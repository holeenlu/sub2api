import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import VersionBadge from '../VersionBadge.vue'

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), backendUpdate: vi.fn(), root: true }))
vi.mock('@/api/client', () => ({ default: { get: mocks.get, post: mocks.post } }))
vi.mock('@/stores', () => ({ useAuthStore: () => ({ isSuperAdmin: mocks.root }), useAppStore: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/system', () => ({ performUpdate: mocks.backendUpdate, restartService: vi.fn(), getRollbackVersions: vi.fn(), rollback: vi.fn() }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() }) }))
let state: Record<string, unknown>
let wrapper: ReturnType<typeof mount>
beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  mocks.root = true
  state = { status: 'idle', portal_version: '0.2.17.12', backend_version: '0.2.17.13', mode: 'portal' }
  mocks.get.mockImplementation(async (url: string) => ({ data: url === '/portal-build.json'
    ? { version: '0.2.17.12' } : url === '/admin/system/version' ? { version: '0.2.17.13' } : { ...state } }))
  mocks.post.mockResolvedValue({ data: { status: 'running' } })
  wrapper = mount(VersionBadge, { props: { portal: true }, global: { stubs: { Icon: true } } })
})
afterEach(() => { wrapper.unmount(); vi.useRealTimers() })
async function open() { await flushPromises(); await wrapper.get('button').trigger('click'); await flushPromises() }
function updateButton() { return wrapper.findAll('button').find(b => b.text() === 'version.updateNow')! }

describe('shared version badge for the supplier portal', () => {
  it('uses the same version badge and update action while keeping the portal and backend versions separate', async () => {
    await open()
    expect(wrapper.get('button').text()).toBe('v0.2.17.12')
    expect(wrapper.text()).toContain('vendor.portalUpdate.portalVersion')
    expect(wrapper.text()).toContain('vendor.portalUpdate.backendVersion: v0.2.17.13')
    await updateButton().trigger('click')
    await flushPromises()
    expect(mocks.post).toHaveBeenCalledWith('/portal-updater/update', { version: '0.2.17.13' })
    expect(mocks.backendUpdate).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('version.composeUpdating')
    expect(updateButton().attributes('disabled')).toBeDefined()
  })
  it('does not claim a successful synchronization from backend version alone', async () => {
    await open()
    await updateButton().trigger('click')
    await flushPromises()
    state.status = 'succeeded'
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper.get('button').text()).toBe('v0.2.17.12')
    expect(wrapper.text()).not.toContain('vendor.portalUpdate.matched')
    expect(wrapper.text()).not.toContain('version.rollback')
  })
  it('keeps the running portal version and disables updates without the host updater', async () => {
    mocks.get.mockImplementation(async (url: string) => {
      if (url === '/portal-updater/status') throw new Error('502')
      return { data: { version: url === '/portal-build.json' ? '0.2.17.12' : '0.2.17.13' } }
    })
    await open()
    await wrapper.findAll('button').find(b => b.attributes('title') === 'version.refresh')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('button').text()).toBe('v0.2.17.12')
    expect(wrapper.text()).toContain('vendor.portalUpdate.setupRequired')
    expect(updateButton().attributes('disabled')).toBeDefined()
  })
  it.each(['portal', 'backend'])('does not claim matching versions when the %s version is unknown', async missing => {
    wrapper.unmount()
    await flushPromises()
    state.portal_version = missing === 'portal' ? '' : '0.2.17.12'
    state.backend_version = missing === 'backend' ? 'unknown' : '0.2.17.13'
    mocks.get.mockImplementation(async (url: string) => ({ data: url === '/portal-updater/status'
      ? { ...state } : { version: url === '/portal-build.json' ? state.portal_version : state.backend_version } }))
    wrapper = mount(VersionBadge, { props: { portal: true }, global: { stubs: { Icon: true } } })
    await open()
    expect(wrapper.text()).toContain('vendor.portalUpdate.versionUnavailable')
    expect(wrapper.text()).not.toContain('vendor.portalUpdate.matched')
    expect(updateButton()).toBeUndefined()
    expect(mocks.post).not.toHaveBeenCalled()
  })
  it('shows recovery failure and clears polling on unmount', async () => {
    await open()
    await updateButton().trigger('click')
    await flushPromises()
    state.status = 'failed'
    state.message = 'previous image restored for all managed services'
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper.text()).toContain(state.message)
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
  it('does not query privileged endpoints for non-super-admin users', async () => {
    wrapper.unmount()
    await flushPromises()
    mocks.get.mockClear()
    mocks.root = false
    wrapper = mount(VersionBadge, { props: { portal: true }, global: { stubs: { Icon: true } } })
    await flushPromises()
    expect(mocks.get).not.toHaveBeenCalled()
    expect(wrapper.find('button').exists()).toBe(false)
  })
})
