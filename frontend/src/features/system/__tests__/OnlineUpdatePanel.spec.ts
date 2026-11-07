import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import OnlineUpdatePanel from '../components/OnlineUpdatePanel.vue'
import type { OnlineUpdateStatus } from '../api/update'

const mocks = vi.hoisted(() => ({ get: vi.fn(), check: vi.fn(), start: vi.fn(), cancel: vi.fn(), run: vi.fn() }))
const scope = ref<string | null>('admin:1')
const visible = ref(false)
vi.mock('../api/update', () => ({ getOnlineUpdate: mocks.get, checkOnlineUpdate: mocks.check, startOnlineUpdate: mocks.start }))
vi.mock('../ports', () => ({ useSystemServices: () => ({
  scope,
  createStepUp: () => ({ visible, run: mocks.run, onCancel: () => { visible.value = false; mocks.cancel() } }),
  StepUpDialog: defineComponent({ name: 'StepUpDialog', template: '<div />' }),
  isStepUpCancelled: (error: { code?: string }) => error.code === 'STEP_UP_CANCELLED',
  isStepUpBlocked: (error: { code?: string }) => ['STEP_UP_TOTP_NOT_ENABLED', 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'].includes(error.code ?? ''),
  stepUpBlockReason: (error: { code?: string }) => error.code,
}) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, args?: Record<string, unknown>) => args?.version ? key + ':' + args.version : key }) }))
const Confirm = defineComponent({
  name: 'ConfirmDialog', props: ['show', 'message'], emits: ['confirm', 'cancel'],
  template: '<div v-if="show" role="dialog"><p>{{ message }}</p><button data-testid="confirm-install" @click="$emit(\'confirm\')">confirm</button><button data-testid="cancel-install" @click="$emit(\'cancel\')">cancel</button></div>'
})
enableAutoUnmount(afterEach)
afterEach(() => { vi.useRealTimers() })

function status(overrides: Partial<OnlineUpdateStatus> = {}): OnlineUpdateStatus {
  return { enabled: true, repository: 'holeenlu/sub2api', branch: 'main', current: { version: '1.1.0', commit: 'a'.repeat(40) }, available: false, ...overrides }
}
function available(overrides: Partial<OnlineUpdateStatus> = {}): OnlineUpdateStatus {
  return status({ available: true, target: { version: '1.1.1', commit: 'b'.repeat(40), release_url: 'https://github.com/holeenlu/sub2api/releases/tag/NewAPI/latest', requires_manual_migration: false }, ...overrides })
}
function running(): OnlineUpdateStatus {
  return available({ job: { id: 'job-1', state: 'running', phase: 'pulling', message: 'working', target_commit: 'b'.repeat(40), started_at: '2026-11-08T00:00:00Z' } })
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
async function open(value = status()) {
  mocks.get.mockResolvedValueOnce(value)
  const wrapper = mount(OnlineUpdatePanel, { global: { stubs: { ConfirmDialog: Confirm } } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  scope.value = 'admin:1'
  visible.value = false
  mocks.run.mockImplementation((action: () => Promise<unknown>) => action())
})

describe('online update controls', () => {
  it('does not offer update actions for an unconfigured deployment', async () => {
    const wrapper = await open(status({ enabled: false }))
    expect(wrapper.text()).toContain('version.onlineUpdate.disabled')
    expect(wrapper.find('[data-testid="update-check"]').exists()).toBe(false)
    expect(mocks.check).not.toHaveBeenCalled()
  })

  it('checks explicitly and links only to the trusted product release', async () => {
    const wrapper = await open()
    mocks.check.mockResolvedValue(available())
    await wrapper.get('[data-testid="update-check"]').trigger('click')
    await flushPromises()
    expect(mocks.check).toHaveBeenCalledWith({ signal: expect.any(AbortSignal) })
    expect(wrapper.get('[data-testid="update-target"]').text()).toContain('1.1.1')
    expect(wrapper.get('a').attributes('href')).toBe('https://github.com/holeenlu/sub2api/releases/tag/NewAPI/latest')
    expect(mocks.start).not.toHaveBeenCalled()
  })

  it('blocks migration releases and suppresses untrusted links', async () => {
    const target = available().target!
    const wrapper = await open(available({ target: { ...target, requires_manual_migration: true, release_url: 'https://untrusted.invalid/release' } }))
    expect(wrapper.text()).toContain('version.onlineUpdate.manualMigration')
    expect(wrapper.get('[data-testid="update-start"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('a').exists()).toBe(false)
    expect(mocks.start).not.toHaveBeenCalled()
  })

  it('requires confirmation before dispatching through step-up and starts one job', async () => {
    const wrapper = await open(available())
    mocks.start.mockResolvedValue(running())
    await wrapper.get('[data-testid="update-start"]').trigger('click')
    expect(wrapper.get('[role="dialog"]').text()).toContain('version.onlineUpdate.confirmMessage')
    expect(mocks.start).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-install"]').trigger('click')
    await flushPromises()
    expect(mocks.run).toHaveBeenCalledOnce()
    expect(mocks.start).toHaveBeenCalledWith('b'.repeat(40), expect.any(String), { signal: expect.any(AbortSignal) })
    expect(wrapper.get('[data-testid="update-start"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('version.onlineUpdate.running')
  })

  it('cancels confirmation without submitting a request', async () => {
    const wrapper = await open(available())
    await wrapper.get('[data-testid="update-start"]').trigger('click')
    await wrapper.get('[data-testid="cancel-install"]').trigger('click')
    expect(mocks.run).not.toHaveBeenCalled()
    expect(mocks.start).not.toHaveBeenCalled()
  })

  it('keeps polling through a service restart, then emits completion once', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const wrapper = await open(running())
    mocks.get.mockRejectedValueOnce({ status: 502 })
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(wrapper.text()).toContain('version.onlineUpdate.reconnecting')
    expect(wrapper.text()).not.toContain('version.onlineUpdate.failed')
    mocks.get.mockResolvedValueOnce(status({ job: { ...running().job!, state: 'succeeded', phase: 'complete' } }))
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(wrapper.text()).toContain('version.onlineUpdate.succeeded')
    expect(wrapper.emitted('updated')).toHaveLength(1)
    const reads = mocks.get.mock.calls.length
    await vi.advanceTimersByTimeAsync(9000)
    expect(mocks.get).toHaveBeenCalledTimes(reads)
  })

  it('reconciles a lost update response without issuing another update', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const wrapper = await open(available())
    mocks.start.mockRejectedValue({ status: 502 })
    mocks.get.mockRejectedValueOnce({ status: 503 })
    await wrapper.get('[data-testid="update-start"]').trigger('click')
    await wrapper.get('[data-testid="confirm-install"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('version.onlineUpdate.reconnecting')
    expect(wrapper.get('[data-testid="update-start"]').attributes('disabled')).toBeDefined()
    mocks.get.mockResolvedValueOnce(running())
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(mocks.start).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('version.onlineUpdate.running')
  })

  it('aborts an old account check and discards its response on identity change', async () => {
    const wrapper = await open()
    const old = deferred<OnlineUpdateStatus>()
    mocks.check.mockReturnValueOnce(old.promise)
    await wrapper.get('[data-testid="update-check"]').trigger('click')
    const signal = mocks.check.mock.calls[0][0].signal as AbortSignal
    mocks.get.mockResolvedValueOnce(status({ enabled: false }))
    scope.value = 'admin:2'
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(mocks.cancel).toHaveBeenCalled()
    old.resolve(available())
    await flushPromises()
    expect(wrapper.text()).toContain('version.onlineUpdate.disabled')
    expect(wrapper.find('[data-testid="update-target"]').exists()).toBe(false)
  })

  it('does not dispatch a deferred step-up action after the session changes', async () => {
    const wrapper = await open(available())
    const verified = deferred<void>()
    mocks.run.mockImplementationOnce(async (action: () => Promise<unknown>) => { visible.value = true; await verified.promise; return action() })
    await wrapper.get('[data-testid="update-start"]').trigger('click')
    await wrapper.get('[data-testid="confirm-install"]').trigger('click')
    scope.value = null
    await flushPromises()
    verified.resolve()
    await flushPromises()
    expect(mocks.start).not.toHaveBeenCalled()
    expect(visible.value).toBe(false)
    expect(wrapper.find('[data-testid="update-start"]').exists()).toBe(false)
  })

  it('clears private job details after authorization is rejected', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const wrapper = await open(running())
    mocks.get.mockRejectedValueOnce({ status: 403 })
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(wrapper.find('[data-testid="update-target"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('version.onlineUpdate.running')
    expect(wrapper.text()).toContain('version.onlineUpdate.unavailable')
    const reads = mocks.get.mock.calls.length
    await vi.advanceTimersByTimeAsync(9000)
    expect(mocks.get).toHaveBeenCalledTimes(reads)
  })

  it('reports step-up restrictions without exposing host errors', async () => {
    const wrapper = await open(available())
    mocks.start.mockRejectedValue({ status: 403, code: 'STEP_UP_ADMIN_API_KEY_FORBIDDEN', message: 'sensitive host output' })
    await wrapper.get('[data-testid="update-start"]').trigger('click')
    await wrapper.get('[data-testid="confirm-install"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('stepUp.adminApiKeyForbidden')
    expect(wrapper.text()).not.toContain('sensitive host output')
  })

  it('shows successful image recovery and blocks checks during operator recovery', async () => {
    const wrapper = await open(available({ job: { ...running().job!, state: 'failed', phase: 'rolled_back' } }))
    expect(wrapper.get('[data-testid="update-phase"]').text()).toBe('version.onlineUpdate.phases.rolledBack')
    mocks.check.mockResolvedValueOnce(available({ job: { ...running().job!, state: 'manual_intervention_required', phase: 'recovery_required' } }))
    await wrapper.get('[data-testid="update-check"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="update-check"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="update-start"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="update-recovery-status"]').exists()).toBe(true)
  })

  it('requires another release check after the selected target changes', async () => {
    const wrapper = await open(available())
    mocks.start.mockRejectedValue({ status: 409, reason: 'UPDATE_TARGET_CHANGED' })
    await wrapper.get('[data-testid="update-start"]').trigger('click')
    await wrapper.get('[data-testid="confirm-install"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('version.onlineUpdate.targetChanged')
    expect(wrapper.find('[data-testid="update-start"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="update-check"]').attributes('disabled')).toBeUndefined()
  })

  it('stops polling and cancels pending requests when closed', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const wrapper = await open(running())
    const pending = deferred<OnlineUpdateStatus>()
    mocks.get.mockReturnValueOnce(pending.promise)
    await vi.advanceTimersByTimeAsync(3000)
    const signal = mocks.get.mock.calls[1][0].signal as AbortSignal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    pending.resolve(running())
    await flushPromises()
    await vi.advanceTimersByTimeAsync(9000)
    expect(mocks.get).toHaveBeenCalledTimes(2)
  })
})
