import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, reactive, type EffectScope } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { useUserDirectory } from '../useUserDirectory'

const mocks = vi.hoisted(() => ({
  list: vi.fn(), definitions: vi.fn(), attributes: vi.fn(), usage: vi.fn(), quotas: vi.fn(), showError: vi.fn(),
  actor: { identityScopeKey: 'user7-session1', isAuthenticated: true, user: { id: 7 }, permissions: ['usage.read'] }
}))
vi.mock('../../api/users', () => ({ usersAPI: { list: mocks.list } }))
vi.mock('../../api/attributes', () => ({ userAttributesAPI: { listEnabledDefinitions: mocks.definitions, getBatchUserAttributes: mocks.attributes } }))
vi.mock('../../model/auth', () => ({ useAuthStore: () => ({
  get identityScopeKey() { return reactive(mocks.actor).identityScopeKey },
  get isAuthenticated() { return reactive(mocks.actor).isAuthenticated },
  can: (permission: string) => reactive(mocks.actor).permissions.includes(permission)
}) }))
vi.mock('../../ports', () => ({ useIdentityNotifications: () => ({ showError: mocks.showError }) }))
vi.mock('../../userPorts', () => ({ useIdentityUserDirectoryData: () => ({ loadUsage: mocks.usage, loadPlatformQuotas: mocks.quotas }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const scopes: EffectScope[] = []
const page = (email: string) => ({ items: [{ id: 7, email }], total: 1, pages: 1 })
function createDirectory() {
  const scope = effectScope()
  scopes.push(scope)
  const directory = scope.run(() => useUserDirectory({ pageSize: 20, filters: () => ({}),
    needsUsage: () => true, needsAttributes: () => false, needsPlatformQuotas: () => false }))!
  return { scope, directory }
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  Object.assign(reactive(mocks.actor), { identityScopeKey: 'user7-session1', isAuthenticated: true, permissions: ['usage.read'] })
  mocks.list.mockReset().mockResolvedValue(page('current@example.test'))
  mocks.definitions.mockReset().mockResolvedValue([])
  mocks.attributes.mockResolvedValue({ attributes: {} })
  mocks.usage.mockReset().mockResolvedValue({ stats: {} })
  mocks.quotas.mockResolvedValue({ platform_quotas: [] })
})
afterEach(() => { scopes.splice(0).forEach(scope => scope.stop()); vi.clearAllTimers(); vi.useRealTimers() })

describe('user-directory request ownership', () => {
  it('cancels the previous query and ignores its late response even if the transport ignores AbortSignal', async () => {
    let finish!: (value: ReturnType<typeof page>) => void
    mocks.list.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const { directory } = createDirectory()
    const old = directory.loadUsers()
    const firstSignal = mocks.list.mock.calls[0][3].signal as AbortSignal
    directory.pagination.page = 2
    await directory.loadUsers()
    expect(firstSignal.aborted).toBe(true)
    finish(page('old@example.test'))
    await old
    expect(directory.users.value[0].email).toBe('current@example.test')
    expect(directory.pagination.page).toBe(2)
    expect(directory.loading.value).toBe(false)
  })

  it('clears the old scope and reloads for a fresh session of the same user', async () => {
    let finish!: (value: ReturnType<typeof page>) => void
    mocks.list.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const { directory } = createDirectory()
    const old = directory.loadUsers()
    reactive(mocks.actor).identityScopeKey = 'user7-session2'
    expect(directory.users.value).toEqual([])
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(2)
    finish(page('session1@example.test'))
    await old
    expect(directory.users.value[0].email).toBe('current@example.test')
    expect(mocks.showError).not.toHaveBeenCalled()
  })

  it('does not apply usage returned after permission revocation', async () => {
    let finish!: (value: unknown) => void
    mocks.usage.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const { directory } = createDirectory()
    await directory.loadUsers()
    await vi.advanceTimersByTimeAsync(50)
    expect(mocks.usage).toHaveBeenCalledOnce()
    reactive(mocks.actor).permissions = []
    reactive(mocks.actor).identityScopeKey = 'user7-policy2'
    await flushPromises()
    finish({ stats: { '7': { user_id: 7, total_actual_cost: 100, today_actual_cost: 10 } } })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(50)
    expect(directory.usageStats.value).toEqual({})
    expect(mocks.usage).toHaveBeenCalledOnce()
  })

  it('cancels deferred enrichment and releases its timer on disposal', async () => {
    const { scope, directory } = createDirectory()
    await directory.loadUsers()
    expect(vi.getTimerCount()).toBe(1)
    scope.stop()
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(1000)
    expect(mocks.usage).not.toHaveBeenCalled()
    expect(directory.users.value).toEqual([])
  })

  it('ignores errors after disposal and clears loading without a failure toast', async () => {
    let fail!: (error: Error) => void
    mocks.list.mockReturnValueOnce(new Promise((_resolve, reject) => { fail = reject }))
    const { scope, directory } = createDirectory()
    const pending = directory.loadUsers()
    scope.stop()
    fail(new Error('late response'))
    await pending
    expect(directory.loading.value).toBe(false)
    expect(mocks.showError).not.toHaveBeenCalled()
  })

  it('never starts a replacement-scope query from an obsolete attribute-load continuation', async () => {
    let finish!: (value: unknown[]) => void
    mocks.definitions.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const { directory } = createDirectory()
    reactive(mocks.actor).identityScopeKey = 'user7-session2'
    reactive(mocks.actor).identityScopeKey = 'user7-session3'
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledOnce()
    finish([])
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledOnce()
    expect(directory.users.value[0].email).toBe('current@example.test')
  })
})
