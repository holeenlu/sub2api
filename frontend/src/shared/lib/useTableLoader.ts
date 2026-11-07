import { ref, shallowRef, reactive, onUnmounted, toRaw } from 'vue'
import type { BasePaginationResponse, FetchOptions } from '@/shared/http/requestTypes';
import { getPersistedPageSize, setPersistedPageSize } from '@/shared/lib/usePersistedPageSize'
import { readApiError } from './apiError'

interface PaginationState {
  page: number
  page_size: number
  total: number
  pages: number
}

interface TableLoaderOptions<T, P> {
  fetchFn: (page: number, pageSize: number, params: P, options?: FetchOptions) => Promise<BasePaginationResponse<T>>
  initialParams?: P
  pageSize?: number
  debounceMs?: number
}

/**
 * 通用表格数据加载 Composable
 * 统一处理分页、筛选、搜索防抖和请求取消
 */
export function useTableLoader<T, P extends object>(options: TableLoaderOptions<T, P>) {
  const { fetchFn, initialParams, pageSize, debounceMs = 300 } = options

  const items = ref<T[]>([])
  const loading = ref(false)
  const error = shallowRef<unknown>(null)
  const params = reactive<P>({ ...(initialParams || {}) } as P)
  const pagination = reactive<PaginationState>({
    page: 1,
    page_size: pageSize ?? getPersistedPageSize(),
    total: 0,
    pages: 0
  })

  let abortController: AbortController | null = null
  let disposed = false
  let pendingReload: { timer: ReturnType<typeof setTimeout>; resolve: () => void } | undefined
  const cancelDebouncedReload = (): void => {
    if (!pendingReload) return
    clearTimeout(pendingReload.timer)
    pendingReload.resolve()
    pendingReload = undefined
  }
  const cancel = (): void => {
    cancelDebouncedReload()
    abortController?.abort()
    abortController = null
    loading.value = false
    error.value = null
  }

  const isAbortError = (error: unknown): boolean => {
    const details = readApiError(error)
    return details.name === 'AbortError' || details.code === 'ERR_CANCELED' || details.name === 'CanceledError'
  }

  const load = async () => {
    if (disposed) return
    cancelDebouncedReload()
    if (abortController) {
      abortController.abort()
    }
    const currentController = new AbortController()
    abortController = currentController
    loading.value = true
    error.value = null

    try {
      const response = await fetchFn(
        pagination.page,
        pagination.page_size,
        { ...toRaw(params) } as P,
        { signal: currentController.signal }
      )

      if (disposed || currentController.signal.aborted) return
      items.value = response.items || []
      pagination.total = response.total || 0
      pagination.pages = response.pages || 0
    } catch (cause) {
      // A transport may reject after abort with an ordinary network error.
      // Only the current query may publish a failure to its page.
      if (disposed || currentController.signal.aborted) return
      if (!isAbortError(cause)) {
        error.value = cause
        throw cause
      }
    } finally {
      if (abortController === currentController) {
        loading.value = false
      }
    }
  }

  const reload = () => {
    pagination.page = 1
    return load()
  }

  // Own this timer so explicit cancellation/disposal releases both the queued
  // query and its callers. The installed debounce helper has no cancel API.
  const debouncedReload = (): Promise<void> => {
    if (disposed) return Promise.resolve()
    cancelDebouncedReload()
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        pendingReload = undefined
        void reload().then(resolve, reject)
      }, debounceMs)
      pendingReload = { timer, resolve }
    })
  }

  const handlePageChange = (page: number) => {
    // 确保页码在有效范围内
    const validPage = Math.max(1, Math.min(page, pagination.pages || 1))
    pagination.page = validPage
    return load()
  }

  const handlePageSizeChange = (size: number) => {
    pagination.page_size = size
    pagination.page = 1
    setPersistedPageSize(size)
    return load()
  }

  onUnmounted(() => {
    disposed = true
    cancel()
  })

  return {
    items,
    loading,
    error,
    params,
    pagination,
    load,
    cancel,
    reload,
    debouncedReload,
    handlePageChange,
    handlePageSizeChange
  }
}
