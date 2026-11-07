import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import type { AdminUser } from '@/features/identity'
import GroupReplaceModal from '../GroupReplaceModal.vue'
import UserPlatformQuotaModal from '../UserPlatformQuotaModal.vue'
import UserAllowedGroupsModal from '../UserAllowedGroupsModal.vue'
import UserApiKeysModal from '../UserApiKeysModal.vue'

const mocks = vi.hoisted(() => ({
  actor: { identityScopeKey: 'session-a', isAdmin: true },
  quotas: vi.fn(), saveQuotas: vi.fn(), resetQuota: vi.fn(), replace: vi.fn(),
  groups: vi.fn(), allGroups: vi.fn(), updateUser: vi.fn(), keys: vi.fn(), updateKey: vi.fn(),
  showSuccess: vi.fn(), showError: vi.fn()
}))
vi.mock('@/features/identity/model/auth', () => ({ useAuthStore: () => mocks.actor }))
vi.mock('@/features/identity/api/users', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/identity/api/users')>()
  return { ...actual, usersAPI: { ...actual.usersAPI, update: mocks.updateUser } }
})
vi.mock('@/features/access-policy', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/access-policy')>()
  return { ...actual, groupsAPI: { ...actual.groupsAPI, list: mocks.groups, getAll: mocks.allGroups } }
})
vi.mock('@/features/access-policy/userAccess', () => ({
  getPlatformQuotas: mocks.quotas, updatePlatformQuotas: mocks.saveQuotas,
  resetPlatformQuotaWindow: mocks.resetQuota, replaceGroup: mocks.replace
}))
vi.mock('@/features/keys/userKeys', () => ({ getUserApiKeys: mocks.keys }))
vi.mock('@/features/keys/admin', () => ({ updateApiKeyGroup: mocks.updateKey }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const user = (id: number): AdminUser => ({ id, email: 'fixture@example.test', allowed_groups: [7], group_rates: { 7: 0.5 } } as AdminUser)
const group = { id: 7, name: 'current-group', platform: 'openai', is_exclusive: true, status: 'active', subscription_type: 'standard', rate_multiplier: 1 }
const quota = (limit: number) => ({ platform_quotas: [{ platform: 'openai', daily_limit_usd: limit, weekly_limit_usd: null, monthly_limit_usd: null }] })
const key = { id: 5, name: 'current-key', key: 'synthetic-not-a-real-key', group_id: null, status: 'active', created_at: '2026-10-01' }
const stubs = {
  BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  Icon: true, PlatformIcon: true, GroupBadge: true,
  GroupOptionItem: { props: ['name'], template: '<span>{{ name }}</span>' }, Teleport: true
}
beforeEach(() => {
  vi.resetAllMocks()
  mocks.actor = reactive({ identityScopeKey: 'session-a', isAdmin: true })
  mocks.quotas.mockResolvedValue(quota(10))
  mocks.groups.mockResolvedValue({ items: [group] })
  mocks.allGroups.mockResolvedValue([group])
  mocks.keys.mockResolvedValue({ items: [key] })
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
type Panel = 'replace' | 'quotaSave' | 'quotaReset' | 'allowed' | 'keyGroup'
const commands = () => ({ replace: mocks.replace, quotaSave: mocks.saveQuotas, quotaReset: mocks.resetQuota, allowed: mocks.updateUser, keyGroup: mocks.updateKey })
async function open(panel: Panel) {
  const component = panel === 'replace' ? GroupReplaceModal : panel.startsWith('quota') ? UserPlatformQuotaModal : panel === 'allowed' ? UserAllowedGroupsModal : UserApiKeysModal
  const wrapper = mount(component, {
    props: { show: true, user: user(1), ...(panel === 'replace' ? { oldGroup: { id: 1, name: 'Old' }, allGroups: [group] } : {}) },
    global: { stubs }
  })
  await flushPromises()
  return wrapper
}
async function submit(wrapper: VueWrapper, panel: Panel) {
  if (panel === 'replace') await wrapper.get('input[type="radio"]').setValue()
  if (panel === 'keyGroup') {
    await wrapper.get('button').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'current-group')!.trigger('click')
  } else if (panel === 'quotaReset') {
    await wrapper.findAll('button').find(button => button.text() === '↻' && button.attributes('disabled') === undefined)!.trigger('click')
  } else await wrapper.get('button.btn-primary').trigger('click')
}
async function invalidate(wrapper: VueWrapper, transition: string) {
  if (transition === 'target') await wrapper.setProps({ user: user(2) })
  else if (transition === 'actor') mocks.actor.identityScopeKey = 'same-user-new-session'
  else if (transition === 'revoke') mocks.actor.isAdmin = false
  else if (transition === 'close') await wrapper.setProps({ show: false })
  else wrapper.unmount()
  await flushPromises()
}

describe.each(['replace', 'quotaSave', 'quotaReset', 'allowed', 'keyGroup'] as const)('%s command ownership', panel => {
  it.each(['target', 'actor', 'revoke', 'close', 'unmount'])('ignores a late success after %s without replay', async transition => {
    const pending = deferred<unknown>()
    const command = commands()[panel]
    command.mockReturnValueOnce(pending.promise)
    const wrapper = await open(panel)
    await submit(wrapper, panel)
    expect(command).toHaveBeenCalledOnce()
    expect(command.mock.calls[0][0]).toBe(panel === 'keyGroup' ? 5 : 1)
    const args = command.mock.calls[0]
    const signal = args[args.length - 1].signal as AbortSignal
    expect(signal).toBeInstanceOf(AbortSignal)
    expect(signal.aborted).toBe(false)
    await invalidate(wrapper, transition)
    expect(signal.aborted).toBe(true)
    pending.resolve({ ...quota(999), migrated_keys: 10, api_key: { ...key, name: 'obsolete-key' } })
    await flushPromises()
    expect(command).toHaveBeenCalledOnce()
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    if (transition === 'target' || transition === 'actor') expect(wrapper.text()).not.toContain('obsolete-key')
  })

  it('ignores a late failure after disposal', async () => {
    const pending = deferred<unknown>()
    commands()[panel].mockReturnValueOnce(pending.promise)
    const wrapper = await open(panel)
    await submit(wrapper, panel)
    wrapper.unmount()
    pending.reject(new Error('old command outcome'))
    await flushPromises()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(console.error).not.toHaveBeenCalled()
  })
})

describe('directory read ownership and submission guards', () => {
  it.each(['quotaSave', 'allowed', 'keyGroup'] as const)('%s clears old data and rejects late reads after a new session', async panel => {
    const pending = deferred<unknown>()
    const api = panel === 'quotaSave' ? mocks.quotas : panel === 'allowed' ? mocks.groups : mocks.allGroups
    api.mockReturnValueOnce(pending.promise)
    const wrapper = await open(panel)
    const args = api.mock.calls[0]
    const signal = args[args.length - 1].signal as AbortSignal
    mocks.actor.identityScopeKey = 'session-b'
    await flushPromises()
    expect(signal.aborted).toBe(true)
    pending.resolve(panel === 'quotaSave' ? quota(999) : panel === 'allowed' ? { items: [{ ...group, name: 'obsolete-group' }] } : [{ ...group, name: 'obsolete-group' }])
    await flushPromises()
    expect(wrapper.text()).not.toContain('obsolete-group')
    if (panel === 'quotaSave') expect(wrapper.findAll('input').map(input => input.element.value)).not.toContain('999')
    if (panel === 'keyGroup') {
      await wrapper.get('button').trigger('click')
      expect(wrapper.text()).toContain('current-group')
      expect(wrapper.text()).not.toContain('obsolete-group')
    }
  })

  it('does not save an unread quota configuration after load failure', async () => {
    mocks.quotas.mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = await open('quotaSave')
    expect(wrapper.get('button.btn-primary').attributes('disabled')).toBeDefined()
    await wrapper.get('button.btn-primary').trigger('click')
    expect(mocks.saveQuotas).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith('admin.users.platformQuota.loadFailed')
  })

  it.each(['replace', 'quotaSave', 'quotaReset', 'allowed'] as const)('%s refuses duplicate clicks while the first command is pending', async panel => {
    const pending = deferred<unknown>()
    const command = commands()[panel]
    command.mockReturnValue(pending.promise)
    const wrapper = await open(panel)
    await submit(wrapper, panel)
    const buttons = wrapper.findAll('button')
    const submitButton = panel === 'quotaReset' ? buttons.find(button => button.text() === '↻')! : wrapper.get('button.btn-primary')
    await submitButton.trigger('click')
    expect(command).toHaveBeenCalledOnce()
  })
})
