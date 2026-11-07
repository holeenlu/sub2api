import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { CanceledError } from 'axios'
import ProfilePasskeyCard from '../ProfilePasskeyCard.vue'

const mocks = vi.hoisted(() => ({
  actor: { identityScopeKey: 'session-a', isAuthenticated: true },
  list: vi.fn(), register: vi.fn(), rename: vi.fn(), remove: vi.fn(),
  showSuccess: vi.fn(), showError: vi.fn()
}))
vi.mock('@/features/identity/model/auth', () => ({ useAuthStore: () => mocks.actor }))
vi.mock('@/features/identity/api/passkey', () => ({ passkeyAPI: { ...mocks, isSupported: () => true } }))
vi.mock('@/features/identity/ports', () => ({ useIdentityNotifications: () => mocks }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())

const credential = { id: 10, name: 'current-passkey', created_at: '2026-10-01', backup: false }
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
beforeEach(() => {
  vi.resetAllMocks()
  mocks.actor = reactive({ identityScopeKey: 'session-a', isAuthenticated: true })
  mocks.list.mockResolvedValue([{ ...credential }])
  vi.spyOn(window, 'prompt').mockReturnValue('renamed-key')
})
async function open() {
  const wrapper = mount(ProfilePasskeyCard, { props: { enabled: true }, global: { stubs: { Icon: true } } })
  await flushPromises()
  return wrapper
}
function button(wrapper: VueWrapper, text: string) {
  return wrapper.findAll('button').find(button => button.text() === text)!
}
type Operation = 'register' | 'rename' | 'remove'
async function act(wrapper: VueWrapper, operation: Operation) {
  if (operation === 'rename') await button(wrapper, 'common.edit').trigger('click')
  else if (operation === 'register') {
    await button(wrapper, 'profile.passkey.add').trigger('click')
    await wrapper.get('#passkey-name').setValue('new-passkey')
    await wrapper.get('#passkey-add-password').setValue('synthetic-password')
    await wrapper.get('form').trigger('submit.prevent')
  } else {
    await button(wrapper, 'common.delete').trigger('click')
    await wrapper.get('#passkey-delete-password').setValue('synthetic-password')
    await wrapper.get('form').trigger('submit.prevent')
  }
}

describe('passkey read ownership', () => {
  it('invalidates an old load when disabled and keeps the subsequent load authoritative', async () => {
    const old = deferred<unknown>()
    mocks.list.mockReturnValueOnce(old.promise)
    const wrapper = await open()
    const signal = mocks.list.mock.calls[0][0].signal as AbortSignal
    await wrapper.setProps({ enabled: false })
    expect(signal.aborted).toBe(true)
    await wrapper.setProps({ enabled: true })
    await flushPromises()
    expect(wrapper.text()).toContain('current-passkey')
    old.resolve([{ ...credential, name: 'obsolete-passkey' }])
    await flushPromises()
    expect(wrapper.text()).not.toContain('obsolete-passkey')
    expect(wrapper.text()).toContain('current-passkey')
  })

  it('silences load cancellation while surfacing real errors', async () => {
    mocks.list.mockRejectedValueOnce(new CanceledError('cancelled'))
    const wrapper = await open()
    expect(mocks.showError).not.toHaveBeenCalled()
    mocks.list.mockRejectedValueOnce(new Error('offline'))
    mocks.actor.identityScopeKey = 'session-b'
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('profile.passkey.loadFailed')
    expect(wrapper.text()).not.toContain('current-passkey')
  })
})

describe.each(['register', 'rename', 'remove'] as const)('passkey %s ownership', operation => {
  it.each(['session', 'disable', 'unmount'])('aborts and discards late success after %s', async transition => {
    const pending = deferred<unknown>()
    const command = mocks[operation]
    command.mockReturnValue(pending.promise)
    const wrapper = await open()
    await act(wrapper, operation)
    expect(command).toHaveBeenCalledOnce()
    if (operation !== 'rename') await wrapper.get('form').trigger('submit.prevent')
    expect(command).toHaveBeenCalledOnce()
    const args = command.mock.calls[0]
    const signal = args[args.length - 1].signal as AbortSignal
    if (transition === 'session') mocks.actor.identityScopeKey = 'session-b'
    else if (transition === 'disable') await wrapper.setProps({ enabled: false })
    else wrapper.unmount()
    await flushPromises()
    expect(signal.aborted).toBe(true)
    pending.resolve({ ...credential, name: 'obsolete-passkey' })
    await flushPromises()
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(command).toHaveBeenCalledOnce()
    if (transition !== 'unmount') {
      expect(wrapper.find('input[type="password"]').exists()).toBe(false)
      expect(wrapper.text()).not.toContain('obsolete-passkey')
      expect(wrapper.text()).not.toContain('renamed-key')
    }
  })

  it('ignores a late error after disposal', async () => {
    const pending = deferred<unknown>()
    mocks[operation].mockReturnValue(pending.promise)
    const wrapper = await open()
    await act(wrapper, operation)
    wrapper.unmount()
    pending.reject(new Error('late outcome'))
    await flushPromises()
    expect(mocks.showError).not.toHaveBeenCalled()
  })

  it('preserves a successful current-session action', async () => {
    mocks[operation].mockResolvedValue({ ...credential })
    const wrapper = await open()
    await act(wrapper, operation)
    await flushPromises()
    expect(mocks.showSuccess).toHaveBeenCalledOnce()
    expect(mocks.showError).not.toHaveBeenCalled()
    if (operation === 'remove') expect(wrapper.text()).not.toContain('current-passkey')
    if (operation === 'rename') expect(wrapper.text()).toContain('renamed-key')
    if (operation === 'register') expect(mocks.list).toHaveBeenCalledTimes(2)
  })
})
