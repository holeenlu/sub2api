import { computed, reactive, shallowRef, watch, type Ref } from 'vue'
import { createUsageCursorPager } from '@/features/usage/queries/usageCursor'
import type { PaginatedResponse } from '@/shared/http/requestTypes';
import type { UsageLog, UsageQueryParams } from '../api/types'
import { useUsageRequest } from './useUsageRequest'

export function useUsageList<T extends UsageLog, P extends UsageQueryParams = UsageQueryParams>(options: {
  scopeKey: Readonly<Ref<string | null>>
  pageSize: number
  filters: () => P
  fetch: (params: P, options: { signal: AbortSignal }) => Promise<PaginatedResponse<T>>
  onError: (error: unknown) => void
}) {
  const requests = useUsageRequest(options.scopeKey)
  const pager = createUsageCursorPager()
  const rows = shallowRef<T[]>([])
  const state = shallowRef<'idle' | 'loading' | 'success' | 'error'>('idle')
  const loading = computed(() => state.value === 'loading')
  const totalExact = shallowRef(false)
  const pagination = reactive({ page: 1, page_size: options.pageSize, total: 0 })
  async function load(): Promise<void> {
    const ticket = requests.begin()
    if (!ticket.isCurrent()) return
    const page = pagination.page
    const params = { ...options.filters(), page, page_size: pagination.page_size }
    state.value = 'loading'
    try {
      const response = await options.fetch(pager.prepare(params), ticket)
      if (!ticket.isCurrent()) return
      rows.value = response.items
      totalExact.value = response.total_exact ?? false
      pager.accept(page, response)
      pagination.total = response.total
      state.value = 'success'
    } catch (error) {
      if (!ticket.isCurrent()) return
      state.value = 'error'
      options.onError(error)
    }
  }
  watch(options.scopeKey, () => {
    rows.value = []; totalExact.value = false; pagination.page = 1; pagination.total = 0; state.value = 'idle'
    if (options.scopeKey.value !== null) void load()
  }, { flush: 'sync' })
  return { rows, state, loading, totalExact, pagination, load, cancel: requests.cancel }
}
