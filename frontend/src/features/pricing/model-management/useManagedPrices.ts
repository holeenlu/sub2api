import { computed, onScopeDispose, ref, shallowRef, watch } from 'vue'
import { InvalidPriceData, managedPricesAPI, requirePriceWrite } from './api'
import { clonePrice } from './types'
import type { ModelPrice, ModelPrices, PriceEntry, PricingContext } from './types'

export type PriceError = 'conflict' | 'denied' | 'unavailable' | 'invalid' | 'request' | 'refresh'
type QueryState = { status: 'idle' | 'loading' | 'success' } | { status: 'error'; error: PriceError }
type CommandState = { status: 'idle' | 'success' } | { status: 'pending'; kind: 'save' | 'sync' } | { status: 'error'; error: PriceError }
function errorKind(error: unknown): PriceError {
  if (error instanceof InvalidPriceData) return 'invalid'
  const status = typeof error === 'object' && error !== null && 'status' in error ? error.status : undefined
  if (status === 409) return 'conflict'
  if (status === 401 || status === 403) return 'denied'
  if (status === 404 || status === 501) return 'unavailable'
  return 'request'
}

export function useManagedPrices(context: () => PricingContext) {
  const snapshot = shallowRef<ModelPrices | null>(null)
  const query = ref<QueryState>({ status: 'idle' })
  const command = ref<CommandState>({ status: 'idle' })
  const editing = shallowRef<{ entry: PriceEntry; version: number } | null>(null)
  const needsRead = ref(false)
  let generation = 0
  let controller: AbortController | undefined
  let disposed = false
  let minimumVersion = 0
  const busy = computed(() => query.value.status === 'loading' || command.value.status === 'pending')
  const writable = computed(() => !!context().scope.trim() && context().canRead && context().canManage && query.value.status === 'success' && !!snapshot.value && !needsRead.value && !busy.value)
  const draftStale = computed(() => !!editing.value && editing.value.version !== snapshot.value?.version)

  function current(epoch: number, signal: AbortSignal) {
    return !disposed && generation === epoch && !signal.aborted
  }
  async function fetchPrices(signal: AbortSignal, epoch: number) {
    query.value = { status: 'loading' }
    try {
      const data = await managedPricesAPI.prices(context().platform, signal)
      if (!current(epoch, signal)) return false
      if (data.version < minimumVersion) throw new InvalidPriceData()
      minimumVersion = data.version
      snapshot.value = data
      query.value = { status: 'success' }
      needsRead.value = false
      return true
    } catch (error: unknown) {
      if (current(epoch, signal)) query.value = { status: 'error', error: errorKind(error) }
      return false
    }
  }
  async function load() {
    if (disposed || !context().scope.trim() || !context().canRead || busy.value) return
    const pending = new AbortController()
    controller = pending
    await fetchPrices(pending.signal, generation)
  }
  function open(entry: PriceEntry) {
    if (disposed || !writable.value || !snapshot.value) return
    editing.value = { entry: clonePrice(entry), version: snapshot.value.version }
    command.value = { status: 'idle' }
  }
  function close() {
    if (busy.value) return
    editing.value = null
  }
  async function execute(kind: 'save' | 'sync', price: ModelPrice | null = null) {
    if (disposed || !writable.value || !snapshot.value || (kind === 'save' && (!editing.value || draftStale.value))) return
    const selected = editing.value
    const expectedVersion = snapshot.value.version
    const platform = context().platform
    const epoch = generation
    const pending = new AbortController()
    controller = pending
    command.value = { status: 'pending', kind }
    try {
      if (kind === 'save' && price !== null) requirePriceWrite(price)
      const receipt = kind === 'sync'
        ? await managedPricesAPI.syncPrices(platform, expectedVersion, pending.signal)
        : selected && await managedPricesAPI.savePrice(platform, selected.entry.id, selected.version, clonePrice(price), pending.signal)
      if (!current(epoch, pending.signal) || !receipt) return
      // Command may already be committed. Never allow a blind repeat after a failed refresh.
      needsRead.value = true
      minimumVersion = Math.max(minimumVersion, receipt.version)
      if (await fetchPrices(pending.signal, epoch)) {
        if (current(epoch, pending.signal)) {
          command.value = { status: 'success' }
          if (kind === 'save') editing.value = null
        }
      } else if (current(epoch, pending.signal)) command.value = { status: 'error', error: 'refresh' }
    } catch (error: unknown) {
      if (current(epoch, pending.signal)) {
        needsRead.value = true
        command.value = { status: 'error', error: errorKind(error) }
      }
    }
  }
  // One synchronous owner invalidates both commands and queries on any authorization/input change.
  watch(() => [context().platform, context().scope, context().canRead, context().canManage] as const, () => {
    ++generation
    controller?.abort()
    controller = undefined
    snapshot.value = null
    editing.value = null
    query.value = { status: 'idle' }
    command.value = { status: 'idle' }
    needsRead.value = false
    minimumVersion = 0
    void load()
  }, { immediate: true, flush: 'sync' })
  onScopeDispose(() => { disposed = true; ++generation; controller?.abort() })
  return { snapshot, query, command, editing, busy, writable, draftStale, needsRead, load, open, close,
    save: (price: ModelPrice | null) => execute('save', price), sync: () => execute('sync') }
}
