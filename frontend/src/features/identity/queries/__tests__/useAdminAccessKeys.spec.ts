import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, ref, type EffectScope } from 'vue'
import { isCancel } from 'axios'
import { useAdminAccessKeys } from '../useAdminAccessKeys'

const api = vi.hoisted(() => ({ list: vi.fn() }))
vi.mock('@/features/identity/api/adminAccessKeys', () => api)
const scopes: EffectScope[] = []
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function setup() {
  const identity = ref('user7/session-a')
  const stepUp = { run: vi.fn(async <T>(action: () => Promise<T>) => action()), onCancel: vi.fn() }
  const effect = effectScope()
  scopes.push(effect)
  const state = effect.run(() => useAdminAccessKeys({ scope: () => identity.value, listGroups: async () => [], stepUp }))!
  return { identity, stepUp, effect, state }
}
beforeEach(() => { vi.resetAllMocks(); api.list.mockResolvedValue({ items: [], permissions: [] }) })
afterEach(() => { for (const scope of scopes.splice(0)) scope.stop() })

describe('management access-key request ownership', () => {
  it('clears one-time secrets and ignores a previous session list response', async () => {
    const old = deferred<{ items: { id: string }[]; permissions: [] }>()
    api.list.mockReturnValueOnce(old.promise)
    const { state, identity, stepUp } = setup()
    const request = state.load().catch(error => error)
    const signal = api.list.mock.calls[0][0].signal as AbortSignal
    state.secret.value = 'old-session-secret'
    identity.value = 'user7/session-b'
    expect(signal.aborted).toBe(true)
    expect(state.secret.value).toBe('')
    expect(stepUp.onCancel).toHaveBeenCalledOnce()
    old.resolve({ items: [{ id: 'old-key' }], permissions: [] })
    expect(isCancel(await request)).toBe(true)
    expect(state.items.value).toEqual([])
    expect(state.loaded.value).toBe(false)
    expect(state.loading.value).toBe(false)
  })

  it.each(['session change', 'unmount'])('never publishes a newly created secret after %s', async transition => {
    const pending = deferred<{ key: string }>()
    const { state, identity, effect } = setup()
    await state.load()
    const applied = vi.fn((result: { key: string }) => { state.secret.value = result.key })
    const mutation = state.mutate(() => pending.promise, applied).catch(error => error)
    if (transition === 'session change') identity.value = 'user7/session-b'
    else effect.stop()
    pending.resolve({ key: 'late-secret' })
    expect(isCancel(await mutation)).toBe(true)
    expect(applied).not.toHaveBeenCalled()
    expect(state.secret.value).toBe('')
    expect(state.busy.value).toBe(false)
  })

  it('rechecks identity after step-up before issuing the mutation', async () => {
    const approval = deferred<void>()
    const { state, identity, stepUp } = setup()
    await state.load()
    stepUp.run.mockImplementationOnce(async action => { await approval.promise; return action() })
    const send = vi.fn().mockResolvedValue({ key: 'must-not-exist' })
    const apply = vi.fn()
    const request = state.mutate(send, apply).catch(error => error)
    identity.value = 'user7/session-b'
    approval.resolve()
    expect(isCancel(await request)).toBe(true)
    expect(send).not.toHaveBeenCalled()
    expect(apply).not.toHaveBeenCalled()
  })

  it('keeps a newer load active when an earlier load settles', async () => {
    const first = deferred<unknown>(); const second = deferred<unknown>()
    api.list.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const { state } = setup()
    const old = state.load().catch(error => error)
    const current = state.load()
    first.resolve({ items: [{ id: 'old' }], permissions: [] })
    expect(isCancel(await old)).toBe(true)
    expect(state.loading.value).toBe(true)
    second.resolve({ items: [{ id: 'current' }], permissions: [] })
    await current
    expect(state.items.value.map(item => item.id)).toEqual(['current'])
    expect(state.loading.value).toBe(false)
  })

  it('retains current state on server rejection and does not reveal an old secret', async () => {
    const { state } = setup()
    await state.load()
    state.secret.value = 'previous-secret'
    const denied = new Error('Permission revoked')
    const apply = vi.fn()
    await expect(state.mutate(async () => { throw denied }, apply)).rejects.toBe(denied)
    expect(apply).not.toHaveBeenCalled()
    expect(state.secret.value).toBe('')
    expect(state.busy.value).toBe(false)
    expect(state.loaded.value).toBe(true)
  })
})
