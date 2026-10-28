import { effectScope, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useManagedModels, validModelMappings } from '../queries/managedModels'
import type { ManagedModelPlatform, ModelPolicy } from '../api/managedModelTypes'

const api = vi.hoisted(() => ({ policy: vi.fn(), mappings: vi.fn(), save: vi.fn(), saveMappings: vi.fn(), reset: vi.fn(), sources: vi.fn(), sync: vi.fn() }))
vi.mock('../api/managedModels', () => ({ managedModelsAPI: api }))
const scopes: ReturnType<typeof effectScope>[] = []
function policy(platform: ManagedModelPlatform = 'openai', version = 7): ModelPolicy {
  return { version, platform, source: 'builtin', catalog_mode: 'policy_only', defaults: { enabled: false, max_tokens: 8192, thinking_fallback_budget: 1024, strip_context_1m: false, context_1m_whitelist: [], normalize_thinking: true }, models: { example: { id: 'example', display_name: 'Example', kind: 'chat', enabled: true, sort: 1, aliases: [], params: { max_tokens_default: 8192, max_tokens_cap: 16000, thinking_fallback_budget: 1024, on_adaptive: 'passthrough', on_enabled: 'passthrough' }, betas: { allow_client: true } } } }
}
function setup(read = true, write = true) {
  const context = { scope: ref('user:1:epoch:1:console'), platform: ref<ManagedModelPlatform>('openai'), canRead: ref(read), canManage: ref(write), canSync: ref(write) }
  const scope = effectScope()
  scopes.push(scope)
  const state = scope.run(() => useManagedModels(context))
  if (!state) throw new Error('Scope unavailable')
  return { ...context, state, stop: () => scope.stop() }
}
function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('not initialized') }
  let reject: (value: unknown) => void = () => { throw new Error('not initialized') }
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
async function settle() { await Promise.resolve(); await Promise.resolve(); await nextTick(); await Promise.resolve() }
function edit(state: ReturnType<typeof setup>['state']) {
  if (!state.draft.value) throw new Error('Missing draft')
  state.updatePolicy({ ...state.draft.value, catalog_mode: 'worker_only', version: 0 })
}
beforeEach(() => {
  vi.resetAllMocks()
  api.policy.mockImplementation((platform: ManagedModelPlatform) => Promise.resolve(policy(platform)))
  api.mappings.mockResolvedValue({ version: 13, mappings: { 'codex-auto-review': 'example', gpt: 'example' } })
  api.sources.mockResolvedValue([{ id: 4, name: 'Source', platform: 'openai', type: 'oauth' }])
})
afterEach(() => { for (const scope of scopes.splice(0)) scope.stop() })

describe('managed model draft and version boundaries', () => {
  it('loads policy and mappings independently without querying billing', async () => {
    const { state } = setup()
    await settle()
    expect(state.draft.value?.version).toBe(7)
    expect(state.mappings.value?.version).toBe(13)
    expect(state.dirty.value).toBe(false)
    expect(state.loading.value).toBe(false)
    expect(api.policy.mock.calls[0][1].signal).toBeInstanceOf(AbortSignal)
  })
  it('does not query without read permission or a private scope', async () => {
    const context = setup(false)
    await settle()
    expect(api.policy).not.toHaveBeenCalled()
    context.scope.value = ''
    context.canRead.value = true
    await settle()
    expect(api.policy).not.toHaveBeenCalled()
  })
  it('preserves a readable policy when mapping loading fails', async () => {
    api.mappings.mockRejectedValue(new Error('Mapping unavailable'))
    const { state } = setup()
    await settle()
    expect(state.draft.value?.platform).toBe('openai')
    expect(state.error.value).toBeNull()
    expect(state.mappingError.value).toBeInstanceOf(Error)
    expect(state.loading.value).toBe(false)
  })

  it('allows the first CAS write when the loaded policy and mapping versions are zero', async () => {
    api.policy.mockResolvedValue(policy('openai', 0))
    api.mappings.mockResolvedValue({ version: 0, mappings: { gpt: 'example' } })
    const { state } = setup()
    await settle()
    edit(state)
    api.save.mockResolvedValue(policy('openai', 1))
    expect(await state.savePolicy()).toBe(true)
    expect(api.save.mock.calls[0][0].version).toBe(0)
    state.updateMappings({ version: 9, mappings: { gpt: 'another' } })
    api.saveMappings.mockResolvedValue({ version: 1, mappings: { gpt: 'another' } })
    expect(await state.saveMappings()).toBe(true)
    expect(api.saveMappings.mock.calls[0][0].version).toBe(0)
  })
  it('does not offer targets enabled only in an unsaved policy draft', async () => {
    const { state } = setup()
    await settle()
    if (!state.draft.value) throw new Error('Missing draft')
    state.draft.value.models.example.enabled = false
    expect(state.mappingTargets.value).toEqual(['example'])
  })
  it('uses the loaded policy CAS version, ignoring an edited version', async () => {
    const { state } = setup()
    await settle()
    edit(state)
    api.save.mockResolvedValue({ ...policy(), version: 8, catalog_mode: 'worker_only' })
    expect(await state.savePolicy()).toBe(true)
    expect(api.save.mock.calls[0][0].version).toBe(7)
    expect(state.draft.value?.version).toBe(8)
    expect(state.dirty.value).toBe(false)
  })
  it('keeps the dirty draft on 409 conflict and never retries the write', async () => {
    const { state } = setup()
    await settle()
    edit(state)
    api.save.mockRejectedValue({ status: 409, code: 409, reason: 'MODEL_POLICY_CONFLICT' })
    expect(await state.savePolicy()).toBe(false)
    expect(state.conflict.value).toBe(true)
    expect(state.dirty.value).toBe(true)
    expect(state.draft.value?.catalog_mode).toBe('worker_only')
    expect(api.save).toHaveBeenCalledTimes(1)
  })
  it('refuses reload and reset over dirty data until explicitly discarded', async () => {
    const { state } = setup()
    await settle()
    edit(state)
    expect(await state.reload()).toBe(false)
    expect(await state.resetPolicy()).toBe(false)
    expect(api.reset).not.toHaveBeenCalled()
    api.reset.mockResolvedValue(policy('openai', 8))
    expect(await state.resetPolicy(true)).toBe(true)
    expect(api.reset.mock.calls[0].slice(0, 2)).toEqual(['openai', 7])
    expect(state.dirty.value).toBe(false)
  })
  it('submits mapping CAS independently from policy and never with zero', async () => {
    const { state } = setup()
    await settle()
    edit(state)
    state.updateMappings({ version: 0, mappings: { 'codex-auto-review': 'example', gpt: 'another' } })
    api.saveMappings.mockResolvedValue({ version: 14, mappings: { 'codex-auto-review': 'example', gpt: 'another' } })
    expect(await state.saveMappings()).toBe(true)
    expect(api.saveMappings.mock.calls[0][0].version).toBe(13)
    expect(state.mappingsDirty.value).toBe(false)
    expect(state.dirty.value).toBe(true)
  })
  it('rejects removal of the mandatory review alias', async () => {
    const { state } = setup()
    await settle()
    state.updateMappings({ version: 13, mappings: { gpt: 'example' } })
    expect(state.mappings.value?.mappings).toHaveProperty('codex-auto-review')
    expect(state.mappingsDirty.value).toBe(false)
  })
  it.each([
    { mappings: { '': 'example' }, version: 1 },
    { mappings: { 'bad alias': 'example' }, version: 1 },
    { mappings: { good: '' }, version: 1 },
    { mappings: { good: '   ' }, version: 1 },
    { mappings: { GPT: 'example', gpt: 'another' }, version: 1 }
  ])('rejects invalid aliases or empty targets: %j', value => {
    expect(validModelMappings(value)).toBe(false)
  })

  it('blocks another write after a conflict until the user reviews the latest snapshot', async () => {
    const { state } = setup()
    await settle()
    edit(state)
    api.save.mockRejectedValue({ status: 409, reason: 'MODEL_POLICY_CONFLICT' })
    await state.savePolicy()
    expect(state.needsRead.value).toBe(true)
    expect(await state.savePolicy()).toBe(false)
    api.policy.mockResolvedValue({ ...policy('openai', 9), source: 'synced' })
    await state.compareLatest()
    expect(state.review.value?.server.version).toBe(9)
    expect(state.draft.value?.version).toBe(7)
    // No local selections means adopt the reviewed server state; this never sends a command.
    state.mergeReviewed([])
    expect(state.draft.value?.version).toBe(9)
    expect(state.draft.value?.catalog_mode).toBe('policy_only')
    expect(state.needsRead.value).toBe(false)
    expect(state.dirty.value).toBe(false)
    expect(api.save).toHaveBeenCalledTimes(1)
  })
  it('keeps an uncertain write draft and never sends another command before a read', async () => {
    const { state } = setup()
    await settle()
    edit(state)
    api.save.mockRejectedValue(new Error('Connection lost'))
    await state.savePolicy()
    expect(state.needsRead.value).toBe(true)
    expect(state.dirty.value).toBe(true)
    expect(await state.resetPolicy(true)).toBe(false)
    expect(api.reset).not.toHaveBeenCalled()
  })
  it('rejects stale write receipts and preserves the unconfirmed draft', async () => {
    const { state } = setup()
    await settle()
    edit(state)
    api.save.mockResolvedValue(policy('openai', 7))
    expect(await state.savePolicy()).toBe(false)
    expect(state.needsRead.value).toBe(true)
    expect(state.dirty.value).toBe(true)
  })
  it('does not classify unrelated 412 responses as model version conflicts', async () => {
    const { state } = setup()
    await settle()
    edit(state)
    api.save.mockRejectedValue({ status: 412 })
    await state.savePolicy()
    expect(state.conflict.value).toBe(false)
  })
  it('keeps drafts when comparison fails and allows another explicit comparison', async () => {
    const { state } = setup()
    await settle()
    edit(state)
    api.save.mockRejectedValue({ status: 409 })
    await state.savePolicy()
    api.policy.mockRejectedValueOnce(new Error('Read unavailable')).mockResolvedValue(policy('openai', 10))
    await state.compareLatest()
    expect(state.reviewError.value).toBeInstanceOf(Error)
    expect(state.dirty.value).toBe(true)
    expect(state.needsRead.value).toBe(true)
    await state.compareLatest()
    expect(state.review.value?.server.version).toBe(10)
    expect(state.reviewError.value).toBeNull()
  })
  it('discards a late comparison after the session changes', async () => {
    const { scope, state } = setup()
    await settle()
    edit(state)
    api.save.mockRejectedValue({ status: 409 })
    await state.savePolicy()
    const pending = deferred<ModelPolicy>()
    api.policy.mockReturnValueOnce(pending.promise)
    const compare = state.compareLatest()
    scope.value = ''
    pending.resolve(policy('openai', 10))
    await compare
    expect(state.review.value).toBeNull()
    expect(state.draft.value).toBeNull()
  })
  it('uses the mapping conflict domain and preserves an independent policy draft', async () => {
    const { state } = setup()
    await settle()
    edit(state)
    state.updateMappings({ version: 13, mappings: { 'codex-auto-review': 'example', gpt: 'another' } })
    api.saveMappings.mockRejectedValue({ status: 409 })
    await state.saveMappings()
    api.mappings.mockResolvedValue({ version: 14, mappings: { 'codex-auto-review': 'example', gpt: 'server' } })
    await state.compareLatest()
    expect(state.review.value?.kind).toBe('mappings')
    state.mergeReviewed([])
    expect(state.mappings.value?.version).toBe(14)
    expect(state.mappings.value?.mappings.gpt).toBe('server')
    expect(state.dirty.value).toBe(true)
  })
  it('does not mutate the loaded response when editing drafts', async () => {
    const source = policy()
    api.policy.mockResolvedValue(source)
    const { state } = setup()
    await settle()
    if (!state.draft.value) throw new Error('Missing draft')
    state.draft.value.models.example.enabled = false
    expect(source.models.example.enabled).toBe(true)
    expect(state.dirty.value).toBe(true)
  })
})

describe('managed model asynchronous ownership', () => {
  it('ignores late OpenAI responses after switching to Anthropic', async () => {
    const stale = deferred<ModelPolicy>()
    api.policy.mockImplementation((platform: ManagedModelPlatform) => platform === 'openai' ? stale.promise : Promise.resolve(policy('anthropic', 22)))
    const { platform, state } = setup()
    const oldSignal = api.policy.mock.calls[0][1].signal
    platform.value = 'anthropic'
    await settle()
    stale.resolve(policy('openai', 9))
    await settle()
    expect(oldSignal.aborted).toBe(true)
    expect(state.draft.value?.platform).toBe('anthropic')
    expect(state.draft.value?.version).toBe(22)
    expect(state.mappings.value).toBeNull()
  })
  it('clears drafts at logout and ignores a pending write result', async () => {
    const { scope, state } = setup()
    await settle()
    edit(state)
    const pending = deferred<ModelPolicy>()
    api.save.mockReturnValue(pending.promise)
    const result = state.savePolicy()
    scope.value = ''
    expect(api.save.mock.calls[0][1].signal.aborted).toBe(true)
    expect(state.draft.value).toBeNull()
    pending.resolve(policy('openai', 8))
    expect(await result).toBe(false)
    expect(state.draft.value).toBeNull()
    expect(state.busy.value).toBe(false)
  })
  it('rejects commands after write permission is revoked', async () => {
    const { canManage, state } = setup()
    await settle()
    edit(state)
    canManage.value = false
    await settle()
    expect(await state.savePolicy()).toBe(false)
    expect(await state.saveMappings()).toBe(false)
    expect(await state.resetPolicy(true)).toBe(false)
    expect(api.save).not.toHaveBeenCalled()
    expect(api.reset).not.toHaveBeenCalled()
  })
  it('rejects a second command while one is pending', async () => {
    const { state } = setup()
    await settle()
    edit(state)
    const pending = deferred<ModelPolicy>()
    api.save.mockReturnValue(pending.promise)
    const first = state.savePolicy()
    expect(await state.savePolicy()).toBe(false)
    expect(api.save).toHaveBeenCalledTimes(1)
    pending.resolve(policy('openai', 8))
    expect(await first).toBe(true)
  })
  it('disposes requests and does not save on unmount', async () => {
    const { state, stop } = setup()
    await settle()
    edit(state)
    const signal = api.policy.mock.calls[0][1].signal
    stop()
    expect(signal.aborted).toBe(true)
    expect(api.save).not.toHaveBeenCalled()
    expect(await state.reload(true)).toBe(false)
    expect(state.draft.value).toBeNull()
  })

  it('requires account read permission for sources and sync without blocking policy edits', async () => {
    const { state, canSync } = setup()
    await settle()
    canSync.value = false
    await settle()
    await state.loadSources()
    expect(api.sources).not.toHaveBeenCalled()
    expect(await state.syncPolicy([4], true)).toBe(false)
    edit(state)
    api.save.mockResolvedValue(policy('openai', 8))
    expect(await state.savePolicy()).toBe(true)
  })
  it('uses selected source IDs and the loaded policy version for synchronization', async () => {
    const { state } = setup()
    await settle()
    await state.loadSources()
    api.sync.mockResolvedValue(policy('openai', 8))
    expect(await state.syncPolicy([])).toBe(false)
    expect(await state.syncPolicy([999])).toBe(false)
    expect(await state.syncPolicy([4, 4])).toBe(true)
    expect(api.sync.mock.calls[0].slice(0, 3)).toEqual(['openai', 7, [4]])
  })
  it('keeps drafts when source loading fails and releases the loading state', async () => {
    const { state } = setup()
    await settle()
    edit(state)
    api.sources.mockRejectedValue(new Error('Unavailable'))
    await state.loadSources()
    expect(state.sourceError.value).toBeInstanceOf(Error)
    expect(state.loadingSources.value).toBe(false)
    expect(state.dirty.value).toBe(true)
    expect(await state.syncPolicy([4], true)).toBe(false)
  })
})
