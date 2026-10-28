import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, reactive } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { useManagedPrices } from '../useManagedPrices'
import { deferred, fullPrice, modelPrices } from './fixtures'
import type { ModelPrices, PriceVersion, PricingContext } from '../types'
const api = vi.hoisted(() => ({ prices: vi.fn(), savePrice: vi.fn(), syncPrices: vi.fn() }))
vi.mock('../api', async original => ({ ...await original<typeof import('../api')>(), managedPricesAPI: api }))
const scopes: ReturnType<typeof effectScope>[] = []
beforeEach(() => {
  vi.resetAllMocks()
  api.prices.mockResolvedValue(modelPrices())
  api.savePrice.mockResolvedValue({ version: 8 })
  api.syncPrices.mockResolvedValue({ version: 8 })
})
afterEach(() => scopes.splice(0).forEach(scope => scope.stop()))
async function create(overrides: Partial<PricingContext> = {}) {
  const context = reactive<PricingContext>({ platform: 'openai', scope: 'actor:1:policy:7', canRead: true, canManage: true, ...overrides })
  const scope = effectScope()
  scopes.push(scope)
  const state = scope.run(() => useManagedPrices(() => context))
  if (!state) throw new Error('Missing active scope')
  await flushPromises()
  return { context, state, scope }
}
function firstEntry(state: ReturnType<typeof useManagedPrices>) {
  const entry = state.snapshot.value?.entries[0]
  if (!entry) throw new Error('Missing price entry')
  return entry
}

describe('managed price query and command ownership', () => {
  it.each(['', '   '])('never reads or writes with empty scope %j even while permissions remain true', async scope => {
    const { state } = await create({ scope })
    await state.load(); await state.sync(); await state.save(fullPrice())
    expect(api.prices).not.toHaveBeenCalled()
    expect(api.syncPrices).not.toHaveBeenCalled()
    expect(api.savePrice).not.toHaveBeenCalled()
    expect(state.writable.value).toBe(false)
  })
  it('clears and aborts an authorized scope before transient permissions have been revoked', async () => {
    const pending = deferred<PriceVersion>()
    api.syncPrices.mockReturnValueOnce(pending.promise)
    const { context, state } = await create()
    const command = state.sync()
    const signal = api.syncPrices.mock.calls[0]?.[2] as AbortSignal
    context.scope = ''
    expect(context.canRead).toBe(true)
    expect(context.canManage).toBe(true)
    expect(state.snapshot.value).toBeNull()
    expect(state.writable.value).toBe(false)
    await state.load(); await state.sync()
    pending.resolve({ version: 8 }); await command
    expect(signal.aborted).toBe(true)
    expect(api.prices).toHaveBeenCalledTimes(1)
    expect(api.syncPrices).toHaveBeenCalledTimes(1)
  })
  it('loads seed version 0 and saves expected version 0 before accepting version 1', async () => {
    api.prices.mockResolvedValueOnce(modelPrices(0))
    const { state } = await create()
    expect(state.snapshot.value?.version).toBe(0)
    expect(state.writable.value).toBe(true)
    state.open(firstEntry(state))
    api.savePrice.mockResolvedValueOnce({ version: 1 })
    api.prices.mockResolvedValueOnce(modelPrices(1))
    await state.save(fullPrice())
    expect(api.savePrice.mock.calls[0]?.slice(0, 4)).toEqual(['openai', 'test-image', 0, fullPrice()])
    expect(state.snapshot.value?.version).toBe(1)
    expect(state.command.value.status).toBe('success')
  })
  it('reloads the global pricing version after switching platforms', async () => {
    const { context, state } = await create()
    api.prices.mockResolvedValueOnce(modelPrices(42))
    context.platform = 'anthropic'
    await flushPromises()
    expect(api.prices.mock.calls[1]?.[0]).toBe('anthropic')
    expect(state.snapshot.value?.version).toBe(42)
    api.syncPrices.mockResolvedValueOnce({ version: 43 })
    api.prices.mockResolvedValueOnce(modelPrices(43))
    await state.sync()
    expect(api.syncPrices.mock.calls[0]?.slice(0, 2)).toEqual(['anthropic', 42])
  })
  it.each([412, 428, 502])('does not invent a conflict reason from sync HTTP %s', async status => {
    api.syncPrices.mockRejectedValueOnce({ status })
    const { state } = await create()
    await state.sync()
    expect(state.command.value).toEqual({ status: 'error', error: 'request' })
    expect(state.needsRead.value).toBe(true)
  })
  it('never reads or writes without billing.cost.read even with write permissions', async () => {
    const { state } = await create({ canRead: false })
    await state.load(); await state.sync(); await state.save(fullPrice())
    expect(api.prices).not.toHaveBeenCalled()
    expect(api.syncPrices).not.toHaveBeenCalled()
    expect(api.savePrice).not.toHaveBeenCalled()
    expect(state.query.value.status).toBe('idle')
  })
  it('read-only actors can load but cannot open an editor or execute direct commands', async () => {
    const { state } = await create({ canManage: false })
    state.open(firstEntry(state)); await state.sync(); await state.save(fullPrice())
    expect(api.prices).toHaveBeenCalledTimes(1)
    expect(state.editing.value).toBeNull()
    expect(api.syncPrices).not.toHaveBeenCalled()
    expect(api.savePrice).not.toHaveBeenCalled()
  })
  it('serializes all commands and reloads before allowing more writes', async () => {
    const pending = deferred<PriceVersion>()
    const refresh = deferred<ModelPrices>()
    api.savePrice.mockReturnValue(pending.promise)
    const { state } = await create()
    state.open(firstEntry(state))
    const draft = fullPrice()
    const save = state.save(draft)
    draft.input = 999
    await state.save(null); await state.sync(); await state.load(); state.close()
    expect(api.savePrice).toHaveBeenCalledTimes(1)
    expect(api.savePrice.mock.calls[0]?.slice(0, 4)).toEqual(['openai', 'test-image', 7, fullPrice()])
    expect(api.syncPrices).not.toHaveBeenCalled()
    expect(state.editing.value).not.toBeNull()
    api.prices.mockReturnValueOnce(refresh.promise)
    pending.resolve({ version: 8 }); await flushPromises()
    expect(state.busy.value).toBe(true)
    await state.save(null)
    refresh.resolve(modelPrices(8)); await save
    expect(state.command.value.status).toBe('success')
    expect(state.snapshot.value?.version).toBe(8)
    expect(state.editing.value).toBeNull()
    expect(state.busy.value).toBe(false)
  })
  it('resets with null and syncs with the next freshly read version', async () => {
    const { state } = await create()
    state.open(firstEntry(state))
    api.prices.mockResolvedValueOnce(modelPrices(8))
    await state.save(null)
    expect(api.savePrice.mock.calls[0]?.slice(0, 4)).toEqual(['openai', 'test-image', 7, null])
    api.syncPrices.mockResolvedValueOnce({ version: 9 })
    api.prices.mockResolvedValueOnce(modelPrices(9))
    await state.sync()
    expect(api.syncPrices.mock.calls[0]?.slice(0, 2)).toEqual(['openai', 8])
  })
  it('keeps the captured editor/version on conflict and never silently rebases it', async () => {
    const { state } = await create()
    state.open(firstEntry(state))
    const editor = state.editing.value
    api.savePrice.mockRejectedValueOnce({ status: 409 })
    await state.save(fullPrice())
    expect(state.command.value).toEqual({ status: 'error', error: 'conflict' })
    expect(state.editing.value).toBe(editor)
    await state.save(fullPrice())
    expect(api.savePrice).toHaveBeenCalledTimes(1)
    api.prices.mockResolvedValueOnce(modelPrices(8))
    await state.load()
    expect(state.draftStale.value).toBe(true)
    await state.save(fullPrice())
    expect(api.savePrice).toHaveBeenCalledTimes(1)
  })
  it('preserves drafts on ambiguous failure and requires an explicit reload', async () => {
    const { state } = await create()
    state.open(firstEntry(state))
    api.savePrice.mockRejectedValueOnce({ status: 0 })
    await state.save(fullPrice()); await state.save(fullPrice())
    expect(state.editing.value?.version).toBe(7)
    expect(state.needsRead.value).toBe(true)
    expect(api.savePrice).toHaveBeenCalledTimes(1)
    await state.load()
    expect(state.writable.value).toBe(true)
  })
  it('does not retry a committed command after its refresh failed or returned a stale version', async () => {
    const { state } = await create()
    state.open(firstEntry(state))
    api.prices.mockRejectedValueOnce({ status: 500 })
    await state.save(fullPrice())
    expect(state.command.value).toEqual({ status: 'error', error: 'refresh' })
    expect(state.editing.value).not.toBeNull()
    await state.save(fullPrice())
    await state.load() // The default mock still returns the pre-command version 7.
    expect(state.query.value).toEqual({ status: 'error', error: 'invalid' })
    expect(state.writable.value).toBe(false)
    expect(api.savePrice).toHaveBeenCalledTimes(1)
    api.prices.mockResolvedValueOnce(modelPrices(8)); await state.load()
    expect(state.draftStale.value).toBe(true)
  })
  it.each(['scope', 'platform', 'canRead', 'canManage'] as const)('aborts and rejects a late query after %s changes', async key => {
    const pending = deferred<ModelPrices>()
    api.prices.mockReturnValueOnce(pending.promise)
    const { context, state } = await create()
    const signal = api.prices.mock.calls[0]?.[1] as AbortSignal
    if (key === 'scope') context.scope = 'actor:2'
    else if (key === 'platform') context.platform = 'anthropic'
    else context[key] = false
    await flushPromises()
    pending.resolve(modelPrices(99)); await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(state.snapshot.value?.version).not.toBe(99)
    if (key === 'canRead') expect(state.snapshot.value).toBeNull()
  })
  it.each(['scope', 'platform', 'canRead', 'canManage'] as const)('rejects an old command across %s changes and cannot clear a new pending command', async key => {
    const old = deferred<PriceVersion>(), fresh = deferred<PriceVersion>()
    api.syncPrices.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    const { context, state } = await create()
    const original = state.sync()
    const signal = api.syncPrices.mock.calls[0]?.[2] as AbortSignal
    if (key === 'scope') context.scope = 'actor:2'
    else if (key === 'platform') context.platform = 'anthropic'
    else { context[key] = false; context[key] = true }
    await flushPromises()
    const next = state.sync()
    const reads = api.prices.mock.calls.length
    old.resolve({ version: 99 }); await original
    expect(signal.aborted).toBe(true)
    expect(api.prices).toHaveBeenCalledTimes(reads)
    expect(state.command.value.status).toBe('pending')
    api.prices.mockResolvedValueOnce(modelPrices(8))
    fresh.resolve({ version: 8 }); await next
    expect(state.command.value.status).toBe('success')
  })
  it('cancels commands on disposal and ignores both late success and failure', async () => {
    const pending = deferred<PriceVersion>()
    api.syncPrices.mockReturnValue(pending.promise)
    const { state, scope } = await create()
    const command = state.sync()
    const signal = api.syncPrices.mock.calls[0]?.[2] as AbortSignal
    scope.stop()
    pending.reject({ status: 500 }); await command
    expect(signal.aborted).toBe(true)
    expect(api.prices).toHaveBeenCalledTimes(1)
    await state.load(); await state.sync()
    expect(api.syncPrices).toHaveBeenCalledTimes(1)
  })
  it('reports an unavailable backend without creating a writable empty snapshot', async () => {
    api.prices.mockRejectedValueOnce({ status: 404 })
    const { state } = await create()
    expect(state.query.value).toEqual({ status: 'error', error: 'unavailable' })
    expect(state.snapshot.value).toBeNull()
    await state.sync()
    expect(api.syncPrices).not.toHaveBeenCalled()
  })
})
