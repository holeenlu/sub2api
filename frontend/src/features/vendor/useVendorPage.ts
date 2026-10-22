import { onScopeDispose, ref, shallowRef } from 'vue'
import type { Page, Query } from './types'
import { errorText } from './helpers'

// One owner for cancellation, page state and stale-response suppression in both portals.
export function useVendorPage<T>(fetcher: (q: Query, signal: AbortSignal) => Promise<Page<T>>) {
  const items = shallowRef<T[]>([]),
    page = ref(1),
    pageSize = ref(20),
    total = ref(0)
  const loading = ref(false),
    error = ref(''),
    selected = ref(new Set<number>())
  let controller: AbortController | undefined,
    generation = 0
  async function load(query: Query = {}, reset = false, preserveRows = false) {
    controller?.abort()
    controller = new AbortController()
    const current = ++generation
    if (reset) {
      page.value = 1
      selected.value = new Set()
    }
    pageSize.value = Math.max(1, Math.min(100, pageSize.value))
    loading.value = true
    error.value = ''
    if (!preserveRows || reset) items.value = []
    try {
      const result = await fetcher(
        { ...query, page: page.value, page_size: pageSize.value },
        controller.signal
      )
      if (current !== generation) return
      items.value = result.items || []
      total.value = result.total
      return result
    } catch (e) {
      if (current === generation && !controller.signal.aborted) error.value = errorText(e)
    } finally {
      if (current === generation) loading.value = false
    }
  }
  onScopeDispose(() => {
    generation++
    controller?.abort()
  })
  return { items, page, pageSize, total, loading, error, selected, load }
}
