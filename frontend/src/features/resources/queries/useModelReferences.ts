import { computed, onBeforeUnmount, ref, watch, type ComputedRef, type Ref } from 'vue'
import { useResourceEditorServices, type ResourceManagedModel } from '../services'
import { saveAccountModels } from '../api/accountModels'
import { accountsAPI } from '../accounts'
import { readAccountModelReferences } from '../model/accountModelReferences'
import { extractApiErrorMessage } from '@/shared/lib/apiError'

export interface ModelReferenceProps {
  modelValue: string[]
  platform?: string
  platforms?: string[]
  accountId?: number
  /** Account forms save their independent reference in the atomic account use case. */
  saveMode?: 'immediate' | 'draft'
  /** BPS opt-in lists have an explicitly empty selection, unlike native references. */
  emptyMode?: 'inherit' | 'explicit'
  disabled?: boolean
}

export interface ModelReferencesState {
  readonly value: ComputedRef<string[]>
  readonly options: ComputedRef<readonly ResourceManagedModel[]>
  readonly selected: (id: string) => boolean
  readonly ready: Ref<boolean>
  readonly saving: Ref<boolean>
  readonly error: Ref<string>
  readonly editable: ComputedRef<boolean>
  readonly draft: Ref<string[] | null>
  readonly serverModels: Ref<string[] | null>
  refresh(): Promise<void>
  toggle(id: string): void
  reloadCurrent(): Promise<void>
  adoptCurrent(): void
  mergeAndSave(): void
  inherit(): Promise<void>
}

export function useModelReferences(props: ModelReferenceProps, update: (models: string[]) => void, message: (key: string) => string): ModelReferencesState {
  const services = useResourceEditorServices()
  const catalog = ref<readonly ResourceManagedModel[]>([])
  const ready = ref(false), saving = ref(false), error = ref('')
  // An unsuccessful CAS keeps a local draft, but never publishes success to its parent.
  const draft = ref<string[] | null>(null)
  const expected = ref<string[]>([])
  const serverModels = ref<string[] | null>(null)
  let generation = 0, disposed = false
  let controller: AbortController | undefined
  const platforms = computed(() => [...new Set(props.platforms?.length ? props.platforms : [props.platform || 'anthropic'])])
  const canReadCatalog = () => services.access.canReadModelCatalog ?? services.access.canManage
  const scope = () => JSON.stringify([services.scopeKey(), services.access.canAuthorize, services.access.canManage, canReadCatalog(), services.access.isSuperAdmin, props.accountId, platforms.value, props.saveMode, props.emptyMode, props.disabled])
  const current = (version: number) => !disposed && version === generation
  const editable = computed(() => !!services.scopeKey() && services.access.canManage && !props.disabled)
  const enabledIDs = computed(() => catalog.value.filter(model => model.enabled).map(model => model.id))
  const value = computed(() => draft.value ?? props.modelValue)
  const effectiveIDs = computed(() => value.value.length || props.emptyMode === 'explicit' ? value.value : enabledIDs.value)
  const selected = (id: string) => effectiveIDs.value.includes(id)
  const options = computed(() => {
    const known = new Set(catalog.value.map(model => model.id))
    return [...catalog.value, ...value.value.filter(id => !known.has(id)).map(id => ({ id, display_name: id, enabled: false }))]
  })
  async function refresh() {
    // A cancelled write may already have committed remotely. Never unlock it via directory refresh.
    if (saving.value) return
    controller?.abort()
    controller = new AbortController()
    const signal = controller.signal, version = ++generation
    ready.value = false
    if (!services.scopeKey() || !canReadCatalog()) return
    try {
      const rows = await Promise.all(platforms.value.map(platform => services.managedModelCatalog(platform, { signal })))
      if (!current(version)) return
      // A bulk selection can add only models enabled on every target platform.
      const merged = new Map<string, ResourceManagedModel>()
      for (const row of rows.flat()) merged.set(row.id, { ...row, enabled: rows.every(items => items.some(item => item.id === row.id && item.enabled)) })
      catalog.value = [...merged.values()]
      ready.value = true
    } catch (failure) {
      if (!current(version) || signal.aborted) return
      error.value = extractApiErrorMessage(failure, message('loadFailed'))
      services.notices.showError(error.value)
    }
  }
  async function persist(models: string[], confirmedRecovery = false) {
    if (!editable.value || !ready.value || saving.value) return
    if (!props.accountId || props.saveMode === 'draft' || props.emptyMode === 'explicit') { update(models); return }
    if (draft.value !== null && error.value && !confirmedRecovery) { draft.value = [...models]; return }
    const signal = controller?.signal
    if (!signal || signal.aborted) return
    const version = generation, accountID = props.accountId
    draft.value = [...models]
    saving.value = true
    error.value = ''
    try {
      const result = await saveAccountModels(accountID, expected.value, models, { signal })
      if (!current(version) || signal.aborted) return
      expected.value = [...(result.models ?? [])]
      draft.value = null
      serverModels.value = null
      update([...expected.value])
    } catch (failure) {
      if (!current(version) || signal.aborted) return
      error.value = extractApiErrorMessage(failure, message('saveFailed'))
      services.notices.showError(error.value)
    } finally { if (current(version)) saving.value = false }
  }
  async function reloadCurrent() {
    const accountID = props.accountId, signal = controller?.signal, version = generation
    if (!accountID || !signal || signal.aborted || saving.value || !editable.value || !canReadCatalog() || draft.value === null) return
    saving.value = true
    try {
      const account = await accountsAPI.getById(accountID, { signal })
      if (!current(version) || signal.aborted) return
      if (account.id !== accountID || account.platform !== platforms.value[0]) throw new Error(message('scopeMismatch'))
      serverModels.value = readAccountModelReferences(account.platform, account.credentials?.model_mapping, account.credentials?.model_allowlist).allowedModels
      services.notices.showInfo(message('reloadUnconfirmed'))
    } catch (failure) {
      if (!current(version) || signal.aborted) return
      error.value = extractApiErrorMessage(failure, message('loadFailed'))
      services.notices.showError(error.value)
    } finally { if (current(version)) saving.value = false }
  }
  function adoptCurrent() {
    if (serverModels.value === null || saving.value || !editable.value) return
    expected.value = [...serverModels.value]
    draft.value = null; serverModels.value = null; error.value = ''
    update([...expected.value])
  }
  function mergeAndSave() {
    if (serverModels.value === null || draft.value === null || saving.value || !editable.value) return
    // Replay only this editor's additions/removals, retaining independent server edits.
    const before = expected.value.length ? expected.value : enabledIDs.value
    const local = draft.value.length ? draft.value : enabledIDs.value
    const added = local.filter(id => !before.includes(id))
    const removed = new Set(before.filter(id => !local.includes(id)))
    if (added.some(id => !enabledIDs.value.includes(id))) { error.value = message('unavailable'); return }
    const remote = serverModels.value.length ? serverModels.value : enabledIDs.value
    const merged = draft.value.length ? [...new Set([...remote.filter(id => !removed.has(id)), ...added])] : []
    expected.value = [...serverModels.value]
    serverModels.value = null
    void persist(merged, true)
  }
  function toggle(id: string) {
    if (!editable.value || !ready.value || saving.value) return
    const ids = effectiveIDs.value
    if (!ids.includes(id) && !enabledIDs.value.includes(id)) return
    void persist(ids.includes(id) ? ids.filter(item => item !== id) : [...ids, id])
  }
  watch(() => props.modelValue, models => { if (!saving.value && draft.value === null) expected.value = [...models] }, { immediate: true, deep: true })
  watch(scope, () => {
    controller?.abort(); generation++; saving.value = false
    draft.value = null; serverModels.value = null; error.value = ''; expected.value = [...props.modelValue]; catalog.value = []
    void refresh()
  }, { immediate: true, flush: 'sync' })
  onBeforeUnmount(() => { disposed = true; generation++; controller?.abort() })
  return { value, options, selected, ready, saving, error, editable, draft, serverModels, refresh, toggle, reloadCurrent, adoptCurrent, mergeAndSave, inherit: () => persist([]) }
}
