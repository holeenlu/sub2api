import { mount, flushPromises } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import VersionBadge from '../VersionBadge.vue'
import { performUpdate } from '@/api/admin/system'

const state = vi.hoisted(() => ({ app: {} as any }))
vi.mock('@/stores', () => ({ useAuthStore: () => ({ isAdmin: true }), useAppStore: () => state.app }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/system', () => ({ performUpdate: vi.fn(), restartService: vi.fn(), getRollbackVersions: vi.fn(), rollback: vi.fn() }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() }) }))

describe('VersionBadge Compose updates', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    state.app = reactive({ currentVersion: '0.2.8', latestVersion: '0.2.8.1', hasUpdate: true,
      buildType: 'release', updateDisabled: false, updateMethod: 'compose', versionLoading: false,
      versionLoaded: true, containerUpdate: undefined, fetchVersion: vi.fn(), clearVersionCache: vi.fn() })
  })
  afterEach(() => { vi.useRealTimers(); vi.clearAllMocks() })
  async function open() {
    const wrapper = mount(VersionBadge, { global: { stubs: { Icon: true } } })
    await wrapper.find('button').trigger('click')
    return wrapper
  }
  it('disables online update for Docker deployments without the host updater', async () => {
    state.app.updateMethod = 'manual'
    const wrapper = await open()
    const update = wrapper.findAll('button').find(b => b.text().includes('version.updateNow'))!
    expect(update.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('version.composeSetupRequired')
    wrapper.unmount()
  })
  it('queues Compose update without offering a binary restart and polls host state', async () => {
    vi.mocked(performUpdate).mockResolvedValue({ message: 'accepted', need_restart: false })
    const wrapper = await open()
    await wrapper.findAll('button').find(b => b.text().includes('version.updateNow'))!.trigger('click')
    await flushPromises()
    expect(state.app.containerUpdate.status).toBe('running')
    expect(wrapper.text()).toContain('version.composeUpdating')
    expect(wrapper.text()).not.toContain('version.restartNow')
    await vi.advanceTimersByTimeAsync(5000)
    expect(state.app.clearVersionCache).toHaveBeenCalled()
    expect(state.app.fetchVersion).toHaveBeenCalledWith(false)
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
