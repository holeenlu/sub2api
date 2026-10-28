import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import AdminRolePermissions from '../AdminRolePermissions.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import type { AdminPermission, AdminRolePermissionsResponse } from '../../api/roles'
import en from '../../locales/en'
import ja from '../../locales/ja'
import zh from '../../locales/zh'
import zhTW from '../../locales/zh-TW'

const mocks = vi.hoisted(() => ({
  onBeforeRouteLeave: vi.fn<(guard: () => Promise<boolean>) => void>(),
  root: { isSuperAdmin: true, identityScopeKey: 'session-a' }, get: vi.fn(), update: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(),
}))
vi.mock('vue-router', () => ({ onBeforeRouteLeave: mocks.onBeforeRouteLeave }))
vi.mock('@/features/identity/model/auth', () => ({ useAuthStore: () => mocks.root }))

vi.mock('@/features/identity/api/roles', () => ({ getAdminRolePermissions: mocks.get, updateAdminRolePermissions: mocks.update }))
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ t: (key: string) => key }) }))
const catalogue: AdminPermission[] = [
  { key: 'accounts.read', module: 'accounts', group:'resources', dependencies: [], sensitive: false, label: '', aliases: [] },
  { key: 'accounts.manage', module: 'accounts', group:'resources', dependencies: ['accounts.read'], sensitive: false, label: '', aliases: [] },
  { key: 'accounts.authorize', module: 'accounts', group:'resources', dependencies: ['accounts.read', 'accounts.manage'], sensitive: true, label: '', aliases: [] },
]
const response = (version = 7, permissions: string[] = [], items = catalogue): AdminRolePermissionsResponse => ({
  policy: { version, permissions, updated_at: '', updated_by: 1 }, catalogue: items, step_up_enabled: true, affected_admin_count: 3
})
const wrappers: ReturnType<typeof mount<typeof AdminRolePermissions>>[] = []
const render = () => {
  const wrapper = mount(AdminRolePermissions, { attachTo: document.body, global: { stubs: { Teleport: true } } })
  wrappers.push(wrapper)
  return wrapper
}
const openReview = async (wrapper: ReturnType<typeof render>) => { await wrapper.get('[data-testid=review]').trigger('click') }
const level = (wrapper: ReturnType<typeof render>, module = 'accounts') => (wrapper.get(`[data-module="${module}"]`).element as HTMLSelectElement).value
const checked = (wrapper: ReturnType<typeof render>, key: string) => (wrapper.get(`[data-permission="${key}"]`).element as HTMLInputElement).checked
function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Not initialized') }
  let reject: (reason: unknown) => void = () => { throw new Error('Not initialized') }
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
async function edit(wrapper: ReturnType<typeof render>, reason = 'Allow operations visibility') {
  await wrapper.get('[data-module=accounts]').setValue('read')
  await openReview(wrapper)
  await wrapper.get('textarea').setValue(reason)
}
async function createConflict(wrapper: ReturnType<typeof render>) {
  await edit(wrapper, 'Keep this reason')
  mocks.update.mockRejectedValueOnce({ status: 409, reason: 'ADMIN_POLICY_CONFLICT' })
  await wrapper.get('[data-testid=save]').trigger('click')
  await flushPromises()
}
beforeEach(() => {
  vi.resetAllMocks()
  mocks.root = reactive({ isSuperAdmin: true, identityScopeKey: 'session-a' })
  mocks.get.mockResolvedValue(response())
  mocks.update.mockResolvedValue(response(8))
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })
describe('one shared administrator policy', () => {
  it('aborts an old policy read and ignores it after the same user starts a new session', async () => {
    let finish: (value: ReturnType<typeof response>) => void = () => { throw new Error('Not initialized') }
    mocks.get.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = render()
    const oldSignal = mocks.get.mock.calls[0]?.[0]?.signal as AbortSignal | undefined
    mocks.root.identityScopeKey = 'session-b'
    await flushPromises()
    try {
      expect(oldSignal?.aborted).toBe(true)
      expect(mocks.get).toHaveBeenCalledTimes(2)
      finish({ ...response(1), policy: { ...response(1).policy, permissions: ['accounts.read'] } })
      await flushPromises()
      expect((wrapper.get('[data-module="accounts"]').element as HTMLSelectElement).value).toBe('off')
      expect(mocks.showError).not.toHaveBeenCalled()
    } finally { wrapper.unmount() }
  })

  it.each(['unmount', 'revoke'] as const)('silences a late policy error after %s', async boundary => {
    let reject: (reason: Error) => void = () => { throw new Error('Not initialized') }
    mocks.get.mockReturnValueOnce(new Promise((_resolve, fail) => { reject = fail }))
    const wrapper = render()
    if (boundary === 'unmount') wrapper.unmount()
    else mocks.root.isSuperAdmin = false
    reject(new Error('obsolete failure'))
    await flushPromises()
    expect(mocks.showError).not.toHaveBeenCalled()
    if (boundary !== 'unmount') wrapper.unmount()
  })

  it('does not reload a new session for an old save conflict', async () => {
    let reject: (reason: unknown) => void = () => { throw new Error('Not initialized') }
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-module="accounts"]').setValue('read')
    await openReview(wrapper)
    await wrapper.get('textarea').setValue('Change only this session policy')
    mocks.update.mockReturnValueOnce(new Promise((_resolve, fail) => { reject = fail }))
    await wrapper.get('[data-testid=save]').trigger('click')
    const signal = mocks.update.mock.calls[0]?.[3]?.signal as AbortSignal | undefined
    mocks.root.identityScopeKey = 'session-b'
    await flushPromises()
    const reads = mocks.get.mock.calls.length
    reject({ reason: 'ADMIN_POLICY_CONFLICT' }); await flushPromises()
    expect(signal?.aborted).toBe(true)
    expect(mocks.get).toHaveBeenCalledTimes(reads)
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('uses server dependencies and submits the loaded version plus a reason', async () => {
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-permission="accounts.authorize"]').setValue(true)
    expect((wrapper.get('[data-module="accounts"]').element as HTMLSelectElement).value).toBe('manage')
    await openReview(wrapper)
    const save = wrapper.get('[data-testid=save]')
    expect(save.attributes('disabled')).toBeDefined()
    await wrapper.get('textarea').setValue('Enable the authorized operations team')
    await save.trigger('click'); await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(7, ['accounts.authorize', 'accounts.manage', 'accounts.read'], 'Enable the authorized operations team', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    wrapper.unmount()
  })
  it('removing a prerequisite also removes its dependent grants', async () => {
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-permission="accounts.authorize"]').setValue(true)
    await wrapper.get('[data-module="accounts"]').setValue('off')
    expect(wrapper.findAll('input:checked')).toHaveLength(0)
    wrapper.unmount()
  })
  it('allows explicit empty-policy recovery without default grants', async () => {
    mocks.get.mockResolvedValue(response(0))
    const wrapper = render(); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.rolePermissions.recovery')
    await openReview(wrapper)
    await wrapper.get('textarea').setValue('Recover without granting any operational access')
    await wrapper.get('[data-testid=save]').trigger('click'); await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(0, [], 'Recover without granting any operational access', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    wrapper.unmount()
  })
  it('keeps grants depending only on read when manage is downgraded', async () => {
    const extra = {label:'',aliases:[],key:'accounts.diagnostics',module:'accounts',group:'resources',dependencies:['accounts.read'],sensitive:false}
    mocks.get.mockResolvedValue({...response(),catalogue:[...catalogue,extra],policy:{...response().policy,permissions:['accounts.read','accounts.manage','accounts.authorize','accounts.diagnostics']}})
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-module="accounts"]').setValue('read')
    expect((wrapper.get('[data-permission="accounts.diagnostics"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-permission="accounts.authorize"]').element as HTMLInputElement).checked).toBe(false)
    await wrapper.get('[data-module="accounts"]').setValue('off')
    expect((wrapper.get('[data-permission="accounts.diagnostics"]').element as HTMLInputElement).checked).toBe(false)
    wrapper.unmount()
  })
  it('keeps staff management and balance adjustment when user management becomes read-only', async () => {
    const grants = [
      {label:'',aliases:[],key:'users.read',module:'users',group:'personnel',dependencies:[],sensitive:false},
      {label:'',aliases:[],key:'users.update',module:'users',group:'personnel',dependencies:['users.read'],sensitive:false},
      {label:'',aliases:[],key:'staff.manage',module:'staff',group:'personnel',dependencies:['users.read'],sensitive:true},
      {label:'',aliases:[],key:'billing.balance.adjust',module:'billing',group:'finance',dependencies:['users.read'],sensitive:true}
    ]
    mocks.get.mockResolvedValue({...response(),catalogue:grants,policy:{...response().policy,permissions:grants.map(p=>p.key)}})
    const wrapper = render();await flushPromises()
    await wrapper.get('[data-module="users"]').setValue('read')
    expect((wrapper.get('[data-permission="staff.manage"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-permission="billing.balance.adjust"]').element as HTMLInputElement).checked).toBe(true)
    wrapper.unmount()
  })
  it('preserves the draft and reason on conflict until explicit reconciliation and review', async () => {
    const wrapper = render(); await flushPromises()
    await createConflict(wrapper)
    expect(mocks.get).toHaveBeenCalledTimes(1)
    expect(mocks.showError).toHaveBeenCalledWith('admin.rolePermissions.conflict')
    expect(level(wrapper)).toBe('read')
    expect(wrapper.get('[data-testid=review]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('textarea').exists()).toBe(false)
    mocks.get.mockResolvedValueOnce(response(9))
    await wrapper.get('[data-testid=reconcile]').trigger('click'); await flushPromises()
    expect(mocks.get).toHaveBeenCalledTimes(2)
    expect(mocks.get).toHaveBeenLastCalledWith({ signal: expect.any(AbortSignal) })
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('Keep this reason')
    expect(mocks.update).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid=permission-preview]').text()).toContain('admin.rolePermissions.added')
    await wrapper.get('[data-testid=save]').trigger('click'); await flushPromises()
    expect(mocks.update).toHaveBeenLastCalledWith(9, ['accounts.read'], 'Keep this reason', { signal: expect.any(AbortSignal) })
  })
  it.each([true, false])('saves directly without TOTP when site step-up is %s', async (enabled) => {
    mocks.get.mockResolvedValue({ ...response(), step_up_enabled: enabled })
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-module="accounts"]').setValue('read')
    await openReview(wrapper)
    await wrapper.get('textarea').setValue('Allow operations visibility')
    await wrapper.get('[data-testid=save]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(7, ['accounts.read'], 'Allow operations visibility', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(mocks.showSuccess).toHaveBeenCalledWith('admin.rolePermissions.saved')
    expect(mocks.showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('never loads or renders the role editor for a limited administrator', async () => {
    mocks.root.isSuperAdmin = false
    const wrapper = render(); await flushPromises()
    expect(mocks.get).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="admin-role-permissions"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

vi.mock('@/features/identity/ports', () => ({
  useIdentityNotifications: () => mocks
}))

describe('permission review and reconciliation', () => {
  it('shows partial CRUD as custom, preserves concurrent and unlisted server grants, and never auto-submits', async () => {
    const grants: AdminPermission[] = [
      ...['read', 'create', 'update', 'delete'].map(action => ({
        key: `users.${action}`, module: 'users', group: 'personnel', dependencies: action === 'read' ? [] : ['users.read'], sensitive: false, label: '', aliases: []
      })),
      { key: 'billing.cost.read', module: 'billing', group: 'finance', dependencies: [], sensitive: false, label: '', aliases: [] }
    ]
    mocks.get.mockResolvedValueOnce(response(7, ['users.read', 'users.update'], grants))
    const wrapper = render(); await flushPromises()
    expect(level(wrapper, 'users')).toBe('custom')
    await wrapper.get('[data-permission="users.create"]').setValue(true)
    expect(checked(wrapper, 'users.delete')).toBe(false)
    await openReview(wrapper)
    await wrapper.get('textarea').setValue('Allow creating users')
    mocks.update.mockRejectedValueOnce({ reason: 'ADMIN_POLICY_CONFLICT' })
    await wrapper.get('[data-testid=save]').trigger('click'); await flushPromises()
    mocks.get.mockResolvedValueOnce(response(9, ['users.read', 'users.update', 'billing.cost.read', 'future.grant'], grants))
    await wrapper.get('[data-testid=reconcile]').trigger('click'); await flushPromises()
    expect(checked(wrapper, 'billing.cost.read')).toBe(true)
    expect(level(wrapper, 'users')).toBe('custom')
    const preview = wrapper.get('[data-testid=permission-preview]').text()
    expect(preview).toContain('actions.create')
    expect(preview).not.toContain('cost_read')
    expect(preview).not.toContain('future.grant')
    expect(mocks.update).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid=save]').trigger('click'); await flushPromises()
    expect(mocks.update).toHaveBeenLastCalledWith(9,
      ['billing.cost.read', 'future.grant', 'users.create', 'users.read', 'users.update'],
      'Allow creating users', { signal: expect.any(AbortSignal) })
  })

  it('reviews dependency removals against the latest catalogue and retains independent concurrent grants', async () => {
    const diagnostics: AdminPermission = { key: 'accounts.diagnostics', module: 'accounts', group: 'resources', dependencies: ['accounts.read'], sensitive: false, label: '', aliases: [] }
    mocks.get.mockResolvedValueOnce(response(7, ['accounts.read', 'accounts.manage']))
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-module=accounts]').setValue('read')
    await openReview(wrapper); await wrapper.get('textarea').setValue('Remove write access')
    mocks.update.mockRejectedValueOnce({ reason: 'ADMIN_POLICY_CONFLICT' })
    await wrapper.get('[data-testid=save]').trigger('click'); await flushPromises()
    mocks.get.mockResolvedValueOnce(response(9, ['accounts.read', 'accounts.manage', 'accounts.authorize', diagnostics.key], [...catalogue, diagnostics]))
    await wrapper.get('[data-testid=reconcile]').trigger('click'); await flushPromises()
    expect(checked(wrapper, 'accounts.authorize')).toBe(false)
    expect(checked(wrapper, diagnostics.key)).toBe(true)
    expect(wrapper.get('[data-testid=permission-preview]').text()).toContain('actions.authorize')
    expect(wrapper.get('[data-testid=permission-preview]').text()).toContain('actions.manage')
    expect(mocks.update).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid=save]').trigger('click'); await flushPromises()
    expect(mocks.update).toHaveBeenLastCalledWith(9, [diagnostics.key, 'accounts.read'], 'Remove write access', { signal: expect.any(AbortSignal) })
  })

  it('does not carry a removed catalogue grant into the new draft or fill in full management', async () => {
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-module=accounts]').setValue('manage')
    await openReview(wrapper); await wrapper.get('textarea').setValue('Use known permissions only')
    mocks.update.mockRejectedValueOnce({ reason: 'ADMIN_POLICY_CONFLICT' })
    await wrapper.get('[data-testid=save]').trigger('click'); await flushPromises()
    const revised = catalogue.filter(permission => permission.key !== 'accounts.manage' && permission.key !== 'accounts.authorize')
    mocks.get.mockResolvedValueOnce(response(9, [], revised))
    await wrapper.get('[data-testid=reconcile]').trigger('click'); await flushPromises()
    expect(level(wrapper)).toBe('read')
    expect(mocks.update).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid=save]').trigger('click'); await flushPromises()
    expect(mocks.update).toHaveBeenLastCalledWith(9, ['accounts.read'], 'Use known permissions only', { signal: expect.any(AbortSignal) })
  })

  it('keeps a failed reconciliation draft, reason and conflict so a later GET can be retried', async () => {
    const wrapper = render(); await flushPromises()
    await createConflict(wrapper)
    mocks.get.mockRejectedValueOnce(new Error('offline'))
    await wrapper.get('[data-testid=reconcile]').trigger('click'); await flushPromises()
    expect(level(wrapper)).toBe('read')
    expect(wrapper.get('[data-testid=review]').attributes('disabled')).toBeDefined()
    expect(mocks.showError).toHaveBeenLastCalledWith('admin.rolePermissions.loadFailed')
    expect(mocks.update).toHaveBeenCalledTimes(1)
    mocks.get.mockResolvedValueOnce(response(9))
    await wrapper.get('[data-testid=reconcile]').trigger('click'); await flushPromises()
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('Keep this reason')
  })

  it('keeps the new baseline on a second conflict and still requires manual reconciliation', async () => {
    const wrapper = render(); await flushPromises()
    await createConflict(wrapper)
    mocks.get.mockResolvedValueOnce(response(9))
    await wrapper.get('[data-testid=reconcile]').trigger('click'); await flushPromises()
    mocks.update.mockRejectedValueOnce({ reason: 'ADMIN_POLICY_CONFLICT' })
    await wrapper.get('[data-testid=save]').trigger('click'); await flushPromises()
    expect(mocks.get).toHaveBeenCalledTimes(2)
    expect(mocks.update).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid=reconcile]').exists()).toBe(true)
    expect(level(wrapper)).toBe('read')
  })

  it('retains a failed save draft and reason for review', async () => {
    const wrapper = render(); await flushPromises()
    await edit(wrapper)
    mocks.update.mockRejectedValueOnce(new Error('offline'))
    await wrapper.get('[data-testid=save]').trigger('click'); await flushPromises()
    expect(level(wrapper)).toBe('read')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('Allow operations visibility')
    expect(wrapper.get('[data-testid=save]').attributes('disabled')).toBeUndefined()
    expect(mocks.showSuccess).not.toHaveBeenCalled()
  })

  it('switches categories and expands details without modifying grants', async () => {
    const users: AdminPermission = { key: 'users.read', module: 'users', group: 'personnel', dependencies: [], sensitive: false, label: '', aliases: [] }
    mocks.get.mockResolvedValueOnce(response(7, [], [...catalogue, users]))
    const wrapper = render(); await flushPromises()
    expect(wrapper.get('[data-permission="accounts.authorize"]').isVisible()).toBe(false)
    await wrapper.get('[data-expand=accounts]').trigger('click')
    expect(wrapper.get('[data-expand=accounts]').attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('[data-permission="accounts.authorize"]').isVisible()).toBe(true)
    await wrapper.get('[data-module=accounts]').setValue('read')
    await wrapper.get('[data-category=personnel]').trigger('click')
    await wrapper.get('[data-module=users]').setValue('read')
    await wrapper.get('[data-category=resources]').trigger('click')
    expect(level(wrapper)).toBe('read')
    expect(level(wrapper, 'users')).toBe('read')
    expect(checked(wrapper, 'accounts.manage')).toBe(false)
    expect(mocks.update).not.toHaveBeenCalled()
  })
})

describe('identity scope and interaction barriers', () => {
  const boundaries = ['session', 'same-session-return', 'revoke', 'unmount'] as const
  function invalidate(boundary: typeof boundaries[number], wrapper: ReturnType<typeof render>) {
    if (boundary === 'unmount') wrapper.unmount()
    else if (boundary === 'revoke') mocks.root.isSuperAdmin = false
    else {
      mocks.root.identityScopeKey = 'session-b'
      if (boundary === 'same-session-return') mocks.root.identityScopeKey = 'session-a'
    }
  }
  it.each(boundaries)('aborts reconciliation and ignores its late success after %s', async boundary => {
    const wrapper = render(); await flushPromises()
    await createConflict(wrapper)
    const pending = deferred<AdminRolePermissionsResponse>()
    mocks.get.mockReturnValueOnce(pending.promise)
    await wrapper.get('[data-testid=reconcile]').trigger('click')
    const signal = mocks.get.mock.lastCall?.[0].signal as AbortSignal
    expect(wrapper.get('[data-testid=reconcile]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-module=accounts]').attributes('disabled')).toBeDefined()
    expect(await wrapper.vm.confirmLeave()).toBe(false)
    invalidate(boundary, wrapper)
    await flushPromises()
    mocks.showError.mockClear()
    pending.resolve(response(99, ['accounts.read', 'accounts.manage']))
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(mocks.update).toHaveBeenCalledTimes(1)
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    if (boundary === 'session' || boundary === 'same-session-return') {
      expect(level(wrapper)).toBe('off')
      expect(wrapper.find('textarea').exists()).toBe(false)
      expect(wrapper.find('[data-testid=reconcile]').exists()).toBe(false)
    }
  })
  it.each(boundaries)('silences a late reconciliation failure after %s', async boundary => {
    const wrapper = render(); await flushPromises()
    await createConflict(wrapper)
    const pending = deferred<AdminRolePermissionsResponse>()
    mocks.get.mockReturnValueOnce(pending.promise)
    await wrapper.get('[data-testid=reconcile]').trigger('click')
    invalidate(boundary, wrapper); await flushPromises()
    mocks.showError.mockClear()
    pending.reject(new Error('old reconciliation failure')); await flushPromises()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(mocks.update).toHaveBeenCalledTimes(1)
  })
  it.each(boundaries)('cancels a pending leave confirmation on %s', async boundary => {
    const wrapper = render(); await flushPromises()
    await edit(wrapper)
    const result = wrapper.vm.confirmLeave()
    await flushPromises()
    const dialog = wrapper.getComponent(ConfirmDialog)
    invalidate(boundary, wrapper)
    dialog.vm.$emit('confirm')
    expect(await result).toBe(false)
    await flushPromises()
    expect(mocks.update).not.toHaveBeenCalled()
  })
  it('does not let an accepted discard continuation clear a new session draft', async () => {
    const wrapper = render(); await flushPromises()
    await edit(wrapper)
    wrapper.getComponent(BaseDialog).vm.$emit('close'); await flushPromises()
    await wrapper.get('[data-testid=discard]').trigger('click')
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    // Invalidate in the same turn, before the async confirmation/discard continuations.
    mocks.root.identityScopeKey = 'session-b'
    await flushPromises()
    await edit(wrapper, 'New session reason')
    expect(level(wrapper)).toBe('read')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('New session reason')
    expect(mocks.update).not.toHaveBeenCalled()
  })
  it('blocks edit, discard, close, duplicate submit and leave while saving', async () => {
    const wrapper = render(); await flushPromises()
    await edit(wrapper)
    const pending = deferred<AdminRolePermissionsResponse>()
    mocks.update.mockReturnValueOnce(pending.promise)
    await wrapper.get('[data-testid=save]').trigger('click')
    for (const selector of ['[data-testid=save]', '[data-testid=discard]', '[data-testid=review]', '[data-module=accounts]', '[data-permission="accounts.manage"]', 'textarea', '[data-expand=accounts]']) {
      expect(wrapper.get(selector).attributes('disabled')).toBeDefined()
    }
    await wrapper.get('[data-testid=save]').trigger('click')
    wrapper.getComponent(BaseDialog).vm.$emit('close'); await flushPromises()
    expect(wrapper.find('textarea').exists()).toBe(true)
    expect(await wrapper.vm.confirmLeave()).toBe(false)
    expect(mocks.update).toHaveBeenCalledTimes(1)
    const event = new Event('beforeunload', { cancelable: true }); window.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    pending.resolve(response(8, ['accounts.read'])); await flushPromises()
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(await wrapper.vm.confirmLeave()).toBe(true)
  })
  it.each(boundaries)('aborts a pending save and silences its late success on %s', async boundary => {
    const wrapper = render(); await flushPromises()
    await edit(wrapper)
    const pending = deferred<AdminRolePermissionsResponse>()
    mocks.update.mockReturnValueOnce(pending.promise)
    await wrapper.get('[data-testid=save]').trigger('click')
    const signal = mocks.update.mock.lastCall?.[3].signal as AbortSignal
    invalidate(boundary, wrapper); await flushPromises()
    pending.resolve(response(8, ['accounts.read'])); await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
    if (boundary === 'session' || boundary === 'same-session-return') expect(level(wrapper)).toBe('off')
  })
  it('guards routes and unload, preserves a cancelled draft, and clears only after explicit discard', async () => {
    const wrapper = render(); await flushPromises()
    const guard = mocks.onBeforeRouteLeave.mock.calls[0]?.[0]
    expect(guard).toBeTypeOf('function')
    expect(await guard?.()).toBe(true)
    await edit(wrapper)
    const event = new Event('beforeunload', { cancelable: true }); window.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    const result = guard?.(); await flushPromises()
    wrapper.getComponent(ConfirmDialog).vm.$emit('cancel')
    expect(await result).toBe(false)
    expect(level(wrapper)).toBe('read')
    wrapper.getComponent(BaseDialog).vm.$emit('close'); await flushPromises()
    await wrapper.get('[data-testid=discard]').trigger('click')
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm'); await flushPromises()
    expect(level(wrapper)).toBe('off')
    expect(await guard?.()).toBe(true)
    const clean = new Event('beforeunload', { cancelable: true }); window.dispatchEvent(clean)
    expect(clean.defaultPrevented).toBe(false)
    wrapper.unmount()
    const after = new Event('beforeunload', { cancelable: true }); window.dispatchEvent(after)
    expect(after.defaultPrevented).toBe(false)
  })
  it('still protects a reason-only draft after reverting all grants', async () => {
    const wrapper = render(); await flushPromises()
    await edit(wrapper)
    wrapper.getComponent(BaseDialog).vm.$emit('close'); await flushPromises()
    await wrapper.get('[data-module=accounts]').setValue('off')
    expect(wrapper.get('[data-testid=review]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid=discard]').attributes('disabled')).toBeUndefined()
    const leave = wrapper.vm.confirmLeave(); await flushPromises()
    wrapper.getComponent(ConfirmDialog).vm.$emit('cancel')
    expect(await leave).toBe(false)
  })
})

describe('identity role permission translations', () => {
  it('owns complete four-language review copy and removes replaced bulk-selection labels', () => {
    const locales = [en, ja, zh, zhTW].map(locale => locale.admin.rolePermissions)
    for (const locale of locales) {
      expect(Object.keys(locale).sort()).toEqual(Object.keys(locales[0]).sort())
      for (const key of ['custom', 'individual', 'categoriesLabel', 'module', 'basic', 'extra', 'collapse', 'details', 'notGranted', 'pending', 'unchanged', 'discard', 'review', 'reviewTitle', 'continueEditing', 'leaveWarning', 'conflict', 'reconcile']) {
        expect(locale).toHaveProperty(key, expect.any(String))
      }
      for (const key of ['selectAll', 'clear', 'version']) expect(locale).not.toHaveProperty(key)
    }
  })
})
