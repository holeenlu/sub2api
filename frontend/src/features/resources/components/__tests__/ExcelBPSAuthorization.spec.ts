import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive, ref } from 'vue'
import { editorTestServices } from '../../__tests__/editorTestServices'
import { resourceEditorServicesKey } from '../../services'
import ExcelBPSAuthorization from '../ExcelBPSAuthorization.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), generateUrl: vi.fn(), exchangeCode: vi.fn(), save: vi.fn(), revoke: vi.fn() }))
vi.mock('@/features/resources/accounts', () => ({ default: { exchangeCode: api.exchangeCode } }))
vi.mock('vue-i18n', async original => ({
  ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key })
}))

function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Deferred not initialized') }
  let reject: (reason: unknown) => void = () => { throw new Error('Deferred not initialized') }
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
function harness(initialScope = 'operator-a') {
  const actor = ref(initialScope)
  const access = reactive({ canAuthorize: true, isSuperAdmin: true, isSimpleMode: true })
  const services = editorTestServices()
  services.scopeKey = () => actor.value
  services.access = access
  services.excelAuthorization = { get: api.get, generateUrl: api.generateUrl, save: api.save, revoke: api.revoke }
  const wrapper = mount(ExcelBPSAuthorization, {
    props: { accountId: 1, proxyId: 11 },
    global: { provide: { [resourceEditorServicesKey as symbol]: services } }
  })
  return { wrapper, actor, access }
}
type Harness = ReturnType<typeof harness>
const changes = ['account', 'proxy', 'scope', 'empty-scope', 'canAuthorize', 'isSuperAdmin', 'unmount'] as const
type Change = typeof changes[number]
async function changeScope(h: Harness, change: Change) {
  const reads = api.get.mock.calls.length
  if (change === 'account') await h.wrapper.setProps({ accountId: 2 })
  if (change === 'proxy') await h.wrapper.setProps({ proxyId: 22 })
  if (change === 'scope') h.actor.value = 'operator-b'
  if (change === 'empty-scope') h.actor.value = ''
  if (change === 'canAuthorize' || change === 'isSuperAdmin') h.access[change] = false
  if (change === 'unmount') h.wrapper.unmount()
  await flushPromises()
  if (change === 'empty-scope') expect(api.get).toHaveBeenCalledTimes(reads)
}
async function click(h: Harness, key: string) {
  const button = h.wrapper.findAll('button').find(item => item.text() === key)
  if (!button) throw new Error('Missing button: ' + key)
  await button.trigger('click')
}
async function startAuthorization(h: Harness) {
  await click(h, 'autoBPSOps.authorization.authorize')
  await flushPromises()
  await h.wrapper.get('input').setValue('https://bps.openai.com/callback?code=synthetic-code&state=synthetic-state')
}
function expectNoGrantActions(h: Harness) {
  expect(h.wrapper.find('details').exists()).toBe(false)
  expect(h.wrapper.find('input').exists()).toBe(false)
  expect(h.wrapper.text()).not.toContain('authorization.authorize')
  expect(h.wrapper.text()).not.toContain('authorization.revoke')
}
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.resetAllMocks()
  api.get.mockResolvedValue({ authorized: false })
  api.generateUrl.mockResolvedValue({ auth_url: 'https://example.test/synthetic-login', session_id: 'synthetic-session' })
  api.exchangeCode.mockResolvedValue({ access_token: 'synthetic-access', refresh_token: 'synthetic-refresh' })
  api.save.mockResolvedValue(undefined)
  api.revoke.mockResolvedValue(undefined)
})

describe('Excel BPS authorization status', () => {
  it('shows reauthorization before primary guidance when the primary access token is absent', async () => {
    api.get.mockResolvedValue({ authorized: false, source: 'primary' })
    const h = harness(); await flushPromises()
    expect(h.wrapper.get('[role="status"]').text()).toContain('authorization.required')
    expect(h.wrapper.text()).toContain('authorization.managePrimary')
    expectNoGrantActions(h)
    expect(h.wrapper.findAll('button')).toHaveLength(1)
  })
  it('manages authorized primary Excel credentials through the existing account OAuth UI', async () => {
    api.get.mockResolvedValue({ authorized: true, source: 'primary', refresh_required: true })
    const h = harness(); await flushPromises()
    expect(h.wrapper.get('[role="status"]').text()).toContain('authorization.primary')
    expect(h.wrapper.text()).toContain('authorization.managePrimary')
    expect(h.wrapper.text()).toContain('authorization.refreshRequired')
    expectNoGrantActions(h)
    expect(api.get).toHaveBeenCalledTimes(1)
    expect(api.generateUrl).not.toHaveBeenCalled()
    expect(api.save).not.toHaveBeenCalled()
  })
  it('offers no grant actions before the first status is known', () => {
    api.get.mockReturnValueOnce(deferred<unknown>().promise)
    expectNoGrantActions(harness())
  })
  it('offers only status retry after a failed read', async () => {
    api.get.mockRejectedValueOnce(new Error('status unavailable'))
    const h = harness(); await flushPromises()
    expect(h.wrapper.get('[role="alert"]').text()).toBe('status unavailable')
    expectNoGrantActions(h)
    await click(h, 'autoBPSOps.refresh'); await flushPromises()
    expect(h.wrapper.find('textarea').exists()).toBe(true)
    expect(api.get).toHaveBeenCalledTimes(2)
  })
  it('does not retain secondary actions after a refresh read fails', async () => {
    const h = harness(); await flushPromises(); await startAuthorization(h)
    api.get.mockRejectedValueOnce(new Error('status unavailable'))
    await click(h, 'autoBPSOps.refresh'); await flushPromises()
    expectNoGrantActions(h)
  })
  it.each([{}, { authorized: false, source: 'future-source' }])('does not infer a secondary grant from unknown status %j', async status => {
    api.get.mockResolvedValue(status)
    const h = harness(); await flushPromises()
    expectNoGrantActions(h)
  })
  it('clears an obsolete standalone OAuth draft when a refreshed status is primary', async () => {
    const h = harness(); await flushPromises(); await startAuthorization(h)
    api.get.mockResolvedValue({ authorized: false, source: 'primary' })
    await click(h, 'autoBPSOps.refresh'); await flushPromises()
    expectNoGrantActions(h)
  })
})

describe('Excel BPS authorization task lifetime', () => {
  it('does not request or allow refresh when mounted with an empty scope and a lagging capability', async () => {
    const h = harness(''); await flushPromises()
    expect(h.access.canAuthorize).toBe(true)
    expect(api.get).not.toHaveBeenCalled()
    expectNoGrantActions(h)
    const refresh = h.wrapper.get('button')
    expect(refresh.element.disabled).toBe(true)
    // A queued/programmatic event must also be rejected by the action guard.
    refresh.element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    expect(api.get).not.toHaveBeenCalled()
    expect(api.generateUrl).not.toHaveBeenCalled()
    expect(api.exchangeCode).not.toHaveBeenCalled()
    expect(api.save).not.toHaveBeenCalled()
    expect(api.revoke).not.toHaveBeenCalled()
    h.actor.value = 'operator-a'; await flushPromises()
    expect(api.get).toHaveBeenCalledTimes(1)
    expect(api.get).toHaveBeenCalledWith(1)
    expect(h.wrapper.get('textarea').element.value).toBe('')
  })
  it('clears drafts without requests on an empty scope and reloads only after a valid scope returns', async () => {
    const h = harness(); await flushPromises(); await startAuthorization(h)
    await h.wrapper.get('textarea').setValue('{"refresh_token":"synthetic-draft"}')
    const reads = api.get.mock.calls.length
    h.actor.value = ''; await flushPromises()
    expect(h.access.canAuthorize).toBe(true)
    expect(api.get).toHaveBeenCalledTimes(reads)
    expectNoGrantActions(h)
    expect(h.wrapper.find('a').exists()).toBe(false)
    expect(h.wrapper.find('[role="status"]').exists()).toBe(false)
    expect(h.wrapper.get('button').element.disabled).toBe(true)
    h.wrapper.get('button').element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await h.wrapper.setProps({ accountId: 2, proxyId: 22 })
    await flushPromises()
    expect(api.get).toHaveBeenCalledTimes(reads)
    expect(api.generateUrl).toHaveBeenCalledTimes(1)
    expect(api.exchangeCode).not.toHaveBeenCalled()
    expect(api.save).not.toHaveBeenCalled()
    expect(api.revoke).not.toHaveBeenCalled()
    h.actor.value = 'operator-a'; await flushPromises()
    expect(api.get).toHaveBeenCalledTimes(reads + 1)
    expect(api.get).toHaveBeenLastCalledWith(2)
    expect(h.wrapper.get('textarea').element.value).toBe('')
    expect(h.wrapper.find('input').exists()).toBe(false)
    expect(h.wrapper.find('a').exists()).toBe(false)
  })
  it.each(changes)('discards a late status after %s changes', async change => {
    const pending = deferred<unknown>(); api.get.mockReturnValueOnce(pending.promise)
    const h = harness(); await changeScope(h, change)
    const current = h.wrapper.html()
    pending.resolve({ authorized: true, source: 'primary' }); await flushPromises()
    expect(h.wrapper.html()).toBe(current)
    if (change === 'account') expect(api.get).toHaveBeenLastCalledWith(2)
  })
  it.each(changes)('discards a late authorization URL after %s changes', async change => {
    const pending = deferred<unknown>(); api.generateUrl.mockReturnValueOnce(pending.promise)
    const h = harness(); await flushPromises()
    await click(h, 'autoBPSOps.authorization.authorize')
    expect(api.generateUrl).toHaveBeenCalledWith(11)
    await changeScope(h, change)
    const current = h.wrapper.html()
    pending.resolve({ auth_url: 'https://example.test/obsolete-login', session_id: 'obsolete-session' }); await flushPromises()
    expect(h.wrapper.html()).toBe(current)
  })
  it.each(changes)('does not save an exchanged grant after %s changes', async change => {
    const pending = deferred<unknown>(); api.exchangeCode.mockReturnValueOnce(pending.promise)
    const h = harness(); await flushPromises(); await startAuthorization(h)
    await click(h, 'autoBPSOps.authorization.save')
    expect(api.exchangeCode).toHaveBeenCalledTimes(1)
    await changeScope(h, change)
    pending.resolve({ access_token: 'obsolete-access' }); await flushPromises()
    expect(api.save).not.toHaveBeenCalled()
  })
  for (const operation of ['exchange', 'import', 'revoke'] as const) {
    it.each(changes)(operation + ' does not refresh or update state after %s changes during the write', async change => {
      const pending = deferred<unknown>(); api.get.mockResolvedValue({ authorized: true })
      const command = operation === 'revoke' ? api.revoke : api.save
      command.mockReturnValueOnce(pending.promise)
      const h = harness(); await flushPromises()
      if (operation === 'exchange') {
        await startAuthorization(h)
        await click(h, 'autoBPSOps.authorization.save')
      } else if (operation === 'import') {
        await h.wrapper.get('textarea').setValue('{"refresh_token":"synthetic-refresh"}')
        await click(h, 'autoBPSOps.authorization.save')
      } else {
        await click(h, 'autoBPSOps.authorization.revoke')
      }
      await flushPromises()
      expect(command).toHaveBeenCalledTimes(1)
      await changeScope(h, change)
      const reads = api.get.mock.calls.length, current = h.wrapper.html()
      pending.resolve(undefined); await flushPromises()
      expect(api.get).toHaveBeenCalledTimes(reads)
      expect(h.wrapper.html()).toBe(current)
    })
  }
  it.each(changes.filter(change => change !== 'unmount'))('clears sensitive drafts when %s changes', async change => {
    const h = harness(); await flushPromises(); await startAuthorization(h)
    await h.wrapper.get('textarea').setValue('{"refresh_token":"synthetic-draft"}')
    await changeScope(h, change)
    expect(h.wrapper.find('a').exists()).toBe(false)
    expect(h.wrapper.find('input').exists()).toBe(false)
    const textarea = h.wrapper.find('textarea')
    if (textarea.exists()) expect(textarea.element.value).toBe('')
    if (change === 'canAuthorize') {
      expectNoGrantActions(h)
      h.access.canAuthorize = true; await flushPromises()
      expect(h.wrapper.get('textarea').element.value).toBe('')
    }
  })
  it.each(['canAuthorize', 'isSuperAdmin'] as const)('does not revive an old task when %s is revoked and restored in the same session', async capability => {
    const pending = deferred<unknown>(); api.exchangeCode.mockReturnValueOnce(pending.promise)
    const h = harness(); await flushPromises(); await startAuthorization(h)
    await click(h, 'autoBPSOps.authorization.save')
    h.access[capability] = false; h.access[capability] = true
    await flushPromises()
    pending.resolve({ access_token: 'obsolete-access' }); await flushPromises()
    expect(api.save).not.toHaveBeenCalled()
    expect(h.wrapper.find('a').exists()).toBe(false)
  })
  it('does not let a late rejected task clear a newer busy operation or show an obsolete error', async () => {
    const old = deferred<unknown>(), current = deferred<unknown>()
    api.generateUrl.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const h = harness(); await flushPromises()
    await click(h, 'autoBPSOps.authorization.authorize')
    await h.wrapper.setProps({ proxyId: 22 }); await flushPromises()
    await click(h, 'autoBPSOps.authorization.authorize')
    old.reject(new Error('obsolete failure')); await flushPromises()
    expect(h.wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(h.wrapper.findAll('button').every(button => button.element.disabled)).toBe(true)
    current.resolve({ auth_url: 'https://example.test/current-login', session_id: 'current-session' }); await flushPromises()
    expect(h.wrapper.get('a').attributes('href')).toBe('https://example.test/current-login')
  })
})

describe('Excel BPS authorization commands', () => {
  it('exchanges and saves once, then reads the authoritative status and clears the callback', async () => {
    const h = harness(); await flushPromises(); await startAuthorization(h)
    api.get.mockResolvedValue({ authorized: true })
    await click(h, 'autoBPSOps.authorization.save'); await flushPromises()
    expect(api.exchangeCode).toHaveBeenCalledWith('/admin/openai/exchange-code', {
      session_id: 'synthetic-session', code: 'synthetic-code', state: 'synthetic-state'
    })
    expect(api.save).toHaveBeenCalledTimes(1)
    expect(api.save).toHaveBeenCalledWith(1, { access_token: 'synthetic-access', refresh_token: 'synthetic-refresh' })
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(h.wrapper.get('[role="status"]').text()).toContain('authorization.ready')
    expect(h.wrapper.find('input').exists()).toBe(false)
  })
  it.each(['null', '[]', '"text"', '42', 'true', '{'])('rejects non-object or malformed credential JSON %s before saving', async value => {
    const h = harness(); await flushPromises()
    await h.wrapper.get('textarea').setValue(value)
    await click(h, 'autoBPSOps.authorization.save'); await flushPromises()
    expect(api.save).not.toHaveBeenCalled()
    expect(h.wrapper.get('[role="alert"]').text()).toContain('authorization.invalidCredentials')
    expect(h.wrapper.get('textarea').element.value).toBe(value)
  })
  it('passes an object unchanged to server validation and preserves it on rejection', async () => {
    const credentials = { refresh_token: 'synthetic-refresh', extension: { value: 7 } }
    api.save.mockRejectedValueOnce(new Error('server credential validation failed'))
    const h = harness(); await flushPromises()
    await h.wrapper.get('textarea').setValue(JSON.stringify(credentials))
    await click(h, 'autoBPSOps.authorization.save'); await flushPromises()
    expect(api.save).toHaveBeenCalledWith(1, credentials)
    expect(h.wrapper.get('[role="alert"]').text()).toBe('server credential validation failed')
    expect(h.wrapper.get('textarea').element.value).toBe(JSON.stringify(credentials))
    expect(api.get).toHaveBeenCalledTimes(1)
  })
  it('removes only a known independent grant and reloads its status', async () => {
    api.get.mockResolvedValueOnce({ authorized: true }).mockResolvedValue({ authorized: false })
    const h = harness(); await flushPromises()
    await click(h, 'autoBPSOps.authorization.revoke'); await flushPromises()
    expect(api.revoke).toHaveBeenCalledTimes(1)
    expect(api.revoke).toHaveBeenCalledWith(1)
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(h.wrapper.text()).toContain('authorization.required')
  })
})
