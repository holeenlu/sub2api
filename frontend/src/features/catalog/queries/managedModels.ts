import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { managedModelsAPI } from '../api/managedModels'
import { mergeModelConflict, type ManagedModelConflict } from '../model/managedModelConflict'
import type { ManagedModelPlatform, ModelMappings, ModelPolicy, ModelSource } from '../api/managedModelTypes'

export interface ManagedModelsContext {
  scope: Readonly<Ref<string>>
  platform: Readonly<Ref<ManagedModelPlatform>>
  canRead: Readonly<Ref<boolean>>
  canManage: Readonly<Ref<boolean>>
  canSync: Readonly<Ref<boolean>>
}

// These HTTP DTOs contain JSON values only. JSON copying also unwraps nested Vue proxies.
function clone<T extends ModelPolicy | ModelMappings>(value: T): T { return JSON.parse(JSON.stringify(value)) }

function equal(left: unknown, right: unknown): boolean { return JSON.stringify(left) === JSON.stringify(right) }

export function isModelVersionConflict(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  if ('status' in error && error.status === 409) return true
  if ('response' in error && error.response && typeof error.response === 'object' && 'status' in error.response) {
    return error.response.status === 409
  }
  return false
}

export function validModelMappings(value: ModelMappings): boolean {
  const aliases = Object.keys(value.mappings)
  return aliases.every(alias => alias.length > 0 && !/\s/.test(alias) && value.mappings[alias].trim().length > 0)
    && new Set(aliases.map(alias => alias.toLowerCase())).size === aliases.length
}

/** One editor lifetime owns drafts, requests and commands. No global private cache or automatic write retries. */
export function useManagedModels(context: ManagedModelsContext) {
  const saved = shallowRef<ModelPolicy | null>(null)
  const draft = ref<ModelPolicy | null>(null)
  const savedMappings = shallowRef<ModelMappings | null>(null)
  const mappings = ref<ModelMappings | null>(null)
  const sources = ref<ModelSource[]>([])
  const loading = ref(false)
  const loadingSources = ref(false)
  const busy = ref(false)
  const error = shallowRef<unknown>(null)
  const mappingError = shallowRef<unknown>(null)
  const sourceError = shallowRef<unknown>(null)
  const commandError = shallowRef<unknown>(null)
  const completedVersion = ref<number | null>(null)
  const dirty = computed(() => !equal(saved.value, draft.value))
  const mappingsDirty = computed(() => !equal(savedMappings.value, mappings.value))
  const needsRead = ref(false)
  const comparing = ref(false)
  const reviewError = shallowRef<unknown>(null)
  const review = shallowRef<ManagedModelConflict | null>(null)
  let failedDomain: 'policy' | 'mappings' | null = null
  const conflict = computed(() => isModelVersionConflict(commandError.value))
  const mappingTargets = computed(() => Object.values(saved.value?.models ?? {}).filter(model => model.enabled && model.kind === 'chat').map(model => model.id))
  let generation = 0
  let sourceGeneration = 0
  let controller = new AbortController()
  let disposed = false

  function current(expectedGeneration: number, scope: string, platform: ManagedModelPlatform): boolean {
    return !disposed && generation === expectedGeneration && context.scope.value === scope
      && context.platform.value === platform && context.canRead.value && !controller.signal.aborted
  }

  function clear(): void {
    generation++
    sourceGeneration++
    controller.abort()
    controller = new AbortController()
    saved.value = null
    draft.value = null
    savedMappings.value = null
    mappings.value = null
    sources.value = []
    loading.value = false
    loadingSources.value = false
    busy.value = false
    error.value = null
    mappingError.value = null
    sourceError.value = null
    commandError.value = null
    completedVersion.value = null
    needsRead.value = false
    comparing.value = false
    reviewError.value = null
    review.value = null
    failedDomain = null
  }

  function acceptPolicy(value: ModelPolicy): void {
    saved.value = clone(value)
    draft.value = clone(value)
  }

  function acceptMappings(value: ModelMappings): void {
    savedMappings.value = clone(value)
    mappings.value = clone(value)
  }

  async function load(): Promise<void> {
    if (disposed || !context.scope.value || !context.canRead.value) return
    const scope = context.scope.value
    const platform = context.platform.value
    const request = generation
    const options = { signal: controller.signal }
    loading.value = true
    await Promise.all([
      managedModelsAPI.policy(platform, options).then(value => {
        if (current(request, scope, platform)) acceptPolicy(value)
      }).catch((failure: unknown) => { if (current(request, scope, platform)) error.value = failure }),
      platform === 'openai' ? managedModelsAPI.mappings(options).then(value => {
        if (current(request, scope, platform)) acceptMappings(value)
      }).catch((failure: unknown) => { if (current(request, scope, platform)) mappingError.value = failure }) : Promise.resolve()
    ])
    if (current(request, scope, platform)) loading.value = false
  }

  async function reload(discard = false): Promise<boolean> {
    if (disposed || busy.value || (!discard && (dirty.value || mappingsDirty.value))) return false
    clear()
    await load()
    return true
  }

  function updatePolicy(value: ModelPolicy): void {
    if (!context.canManage.value || busy.value || comparing.value || needsRead.value || !saved.value || value.platform !== saved.value.platform) return
    draft.value = { ...clone(value), version: saved.value.version }
  }

  function updateMappings(value: ModelMappings): void {
    if (!context.canManage.value || busy.value || comparing.value || needsRead.value || !savedMappings.value) return
    // The mandatory review alias remains fixed even when another editor emits an incomplete draft.
    if ('codex-auto-review' in savedMappings.value.mappings && !('codex-auto-review' in value.mappings)) return
    mappings.value = { ...clone(value), version: savedMappings.value.version }
  }

  async function command<T extends { version: number }>(domain: 'policy' | 'mappings', run: (signal: AbortSignal) => Promise<T>, accept: (value: T) => void): Promise<boolean> {
    if (disposed || busy.value || loading.value || comparing.value || needsRead.value || !context.scope.value || !context.canRead.value || !context.canManage.value) return false
    const request = generation
    const scope = context.scope.value
    const platform = context.platform.value
    busy.value = true
    commandError.value = null
    completedVersion.value = null
    failedDomain = domain
    const expectedVersion = domain === 'policy' ? saved.value?.version : savedMappings.value?.version
    try {
      const value = await run(controller.signal)
      if (!current(request, scope, platform) || !context.canManage.value) return false
      if (!Number.isSafeInteger(value.version) || expectedVersion === undefined || value.version <= expectedVersion) throw new Error('Model command returned a stale version')
      accept(value)
      completedVersion.value = value.version
      return true
    } catch (failure: unknown) {
      if (current(request, scope, platform)) {
        commandError.value = failure
        const status = failure && typeof failure === 'object' && 'status' in failure ? failure.status : undefined
        needsRead.value = isModelVersionConflict(failure) || typeof status !== 'number' || status === 0 || status >= 500
      }
      return false
    } finally {
      if (current(request, scope, platform)) busy.value = false
    }
  }

  async function savePolicy(): Promise<boolean> {
    if (!draft.value || !saved.value || !dirty.value) return false
    const value = { ...clone(draft.value), version: saved.value.version }
    return command('policy', signal => managedModelsAPI.save(value, { signal }), acceptPolicy)
  }

  async function saveMappings(): Promise<boolean> {
    if (!mappings.value || !savedMappings.value || !mappingsDirty.value || !validModelMappings(mappings.value)) return false
    const value = { ...clone(mappings.value), version: savedMappings.value.version }
    return command('mappings', signal => managedModelsAPI.saveMappings(value, { signal }), acceptMappings)
  }

  async function resetPolicy(discard = false): Promise<boolean> {
    if (!saved.value || (dirty.value && !discard)) return false
    const { platform, version } = saved.value
    return command('policy', signal => managedModelsAPI.reset(platform, version, { signal }), acceptPolicy)
  }

  async function loadSources(): Promise<void> {
    if (disposed || !context.canManage.value || !context.canSync.value || !context.canRead.value || !context.scope.value) return
    const request = generation
    const sourceRequest = ++sourceGeneration
    const scope = context.scope.value
    const platform = context.platform.value
    loadingSources.value = true
    sourceError.value = null
    sources.value = []
    try {
      const value = await managedModelsAPI.sources(platform, { signal: controller.signal })
      if (current(request, scope, platform) && sourceRequest === sourceGeneration) sources.value = value
    } catch (failure: unknown) {
      if (current(request, scope, platform) && sourceRequest === sourceGeneration) sourceError.value = failure
    } finally {
      if (current(request, scope, platform) && sourceRequest === sourceGeneration) loadingSources.value = false
    }
  }

  async function syncPolicy(ids: number[], discard = false): Promise<boolean> {
    if (!context.canSync.value || !saved.value || !ids.length || loadingSources.value || sourceError.value || (dirty.value && !discard)) return false
    const available = new Set(sources.value.map(source => source.id))
    if (ids.some(id => !Number.isSafeInteger(id) || id <= 0 || !available.has(id))) return false
    const { platform, version } = saved.value
    return command('policy', signal => managedModelsAPI.sync(platform, version, [...new Set(ids)], { signal }), acceptPolicy)
  }


  async function compareLatest(): Promise<void> {
    if (disposed || comparing.value || busy.value || !needsRead.value || !failedDomain || !context.canManage.value || !context.scope.value) return
    const request = generation
    const scope = context.scope.value
    const platform = context.platform.value
    comparing.value = true
    reviewError.value = null
    try {
      if (failedDomain === 'policy' && saved.value && draft.value) {
        const baseline = clone(saved.value)
        const server = await managedModelsAPI.policy(platform, { signal: controller.signal })
        if (!current(request, scope, platform) || !draft.value) return
        if (server.version < baseline.version) throw new Error('Model comparison returned a stale version')
        review.value = { kind: 'policy', baseline, local: clone(draft.value), server: clone(server) }
      } else if (failedDomain === 'mappings' && savedMappings.value && mappings.value) {
        const baseline = clone(savedMappings.value)
        const server = await managedModelsAPI.mappings({ signal: controller.signal })
        if (!current(request, scope, platform) || !mappings.value) return
        if (server.version < baseline.version) throw new Error('Model comparison returned a stale version')
        review.value = { kind: 'mappings', baseline, local: clone(mappings.value), server: clone(server) }
      }
    } catch (failure: unknown) {
      if (current(request, scope, platform)) reviewError.value = failure
    } finally {
      if (current(request, scope, platform)) comparing.value = false
    }
  }

  function mergeReviewed(keepKeys: readonly string[]): void {
    if (!review.value || disposed || busy.value || comparing.value || !context.canManage.value) return
    const merged = mergeModelConflict(review.value, keepKeys)
    if (review.value.kind === 'policy' && 'platform' in merged) {
      saved.value = clone(review.value.server)
      draft.value = merged
    } else if (review.value.kind === 'mappings' && 'mappings' in merged) {
      savedMappings.value = clone(review.value.server)
      mappings.value = merged
    } else return
    review.value = null
    needsRead.value = false
    commandError.value = null
    failedDomain = null
  }

  watch([context.scope, context.platform, context.canRead, context.canManage, context.canSync], () => {
    clear()
    void load()
  }, { immediate: true, flush: 'sync' })
  onScopeDispose(() => { disposed = true; clear() })
  return { draft, mappings, sources, loading, loadingSources, busy, error, mappingError, sourceError, commandError,
    dirty, mappingsDirty, conflict, completedVersion, mappingTargets, needsRead, comparing, review, reviewError, compareLatest, mergeReviewed, updatePolicy, updateMappings, reload, savePolicy, saveMappings, resetPolicy, loadSources, syncPolicy }
}
