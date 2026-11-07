import { onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import type { Page, Query } from './types'
import { errorText } from './helpers'
import { useSupplyServices } from './services'

/** Instance-owned supplier page; identity/scope changes cancel and clear private projections. */
export function useSupplyPage<T>(fetcher: (q: Query, signal: AbortSignal) => Promise<Page<T>>,
  scopeKey: Readonly<Ref<string | null>> = useSupplyServices().scopeKey) {
  const items = shallowRef<T[]>([]), page = ref(1), pageSize = ref(20), total = ref(0)
  const loading = ref(false), error = ref(''), selected = ref(new Set<number>())
  let controller: AbortController | undefined
  let generation = 0
  let disposed = false
  function clear() {
    generation++
    controller?.abort()
    items.value = []; total.value = 0; selected.value = new Set(); loading.value = false; error.value = ''
  }
  async function load(query: Query = {}, reset = false, preserveRows = false) {
    controller?.abort()
    const request = new AbortController()
    controller = request
    const current = ++generation
    const scope = scopeKey.value
    const isCurrent = () => !disposed && scope !== null && scope === scopeKey.value && current === generation && !request.signal.aborted
    if (!isCurrent()) { clear(); return }
    if (reset) { page.value = 1; selected.value = new Set() }
    pageSize.value = Math.max(1, Math.min(100, pageSize.value))
    loading.value = true; error.value = ''
    if (!preserveRows || reset) items.value = []
    try {
      const result = await fetcher({ ...query, page: page.value, page_size: pageSize.value }, request.signal)
      if (!isCurrent()) return
      items.value = result.items || []
      total.value = result.total
      return result
    } catch (failure) {
      if (isCurrent()) error.value = errorText(failure)
    } finally { if (current === generation) loading.value = false }
  }
  watch(scopeKey, clear, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; clear() })
  return { items, page, pageSize, total, loading, error, selected, load, clear }
}
