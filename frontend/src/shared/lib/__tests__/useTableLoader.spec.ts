import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useTableLoader } from '@/shared/lib/useTableLoader'
import { onUnmounted } from 'vue'

// Mock Vue 的 onUnmounted（composable 外使用时会报错）
vi.mock('vue', async () => {
  const actual = await vi.importActual('vue')
  return {
    ...actual,
    onUnmounted: vi.fn(),
  }
})

const createMockFetchFn = (items: any[] = [], total = 0, pages = 1) => {
  return vi.fn().mockResolvedValue({ items, total, pages })
}

describe('useTableLoader', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('does not fetch when a pending filter debounce fires after unmount', async () => {
    const fetchFn = createMockFetchFn([{ id: 1 }], 1, 1)
    const loader = useTableLoader({ fetchFn })
    await loader.load()
    loader.debouncedReload()
    const cleanup = vi.mocked(onUnmounted).mock.calls.at(-1)?.[0]
    expect(cleanup).toBeTypeOf('function')
    cleanup!()
    await vi.runAllTimersAsync()
    expect(fetchFn).toHaveBeenCalledTimes(1)
    expect(fetchFn.mock.calls[0]?.[3].signal.aborted).toBe(true)
    expect(loader.items.value).toEqual([{ id: 1 }])
  })

  it('ignores an in-flight response that resolves after unmount', async () => {
    let resolveLoad!: (value: { items: { id: number }[]; total: number; pages: number }) => void
    const fetchFn = vi.fn(() => new Promise<{ items: { id: number }[]; total: number; pages: number }>(resolve => { resolveLoad = resolve }))
    const loader = useTableLoader({ fetchFn })
    const pending = loader.load()
    const cleanup = vi.mocked(onUnmounted).mock.calls.at(-1)?.[0]
    expect(cleanup).toBeTypeOf('function')
    cleanup!()
    resolveLoad({ items: [{ id: 99 }], total: 1, pages: 1 })
    await pending
    expect(loader.items.value).toEqual([])
    expect(loader.pagination.total).toBe(0)
  })

  // --- 基础加载 ---
  it('settles a canceled debounce without fetching and lets an explicit read supersede it', async () => {
    const fetchFn = createMockFetchFn()
    const loader = useTableLoader({ fetchFn })
    const pending = loader.debouncedReload()
    loader.cancel()
    await expect(pending).resolves.toBeUndefined()
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(500)
    expect(fetchFn).not.toHaveBeenCalled()
    const replaced = loader.debouncedReload()
    await loader.load()
    await replaced
    await vi.advanceTimersByTimeAsync(500)
    expect(fetchFn).toHaveBeenCalledTimes(1)
  })
  it('does not publish a stale network failure after replacement or disposal', async () => {
    let rejectFirst!: (error: Error) => void
    const fetchFn = createMockFetchFn([{ id: 2 }], 1, 1)
    fetchFn.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectFirst = reject }))
    const loader = useTableLoader({ fetchFn })
    const first = loader.load()
    await loader.load()
    rejectFirst(new Error('late disconnected request'))
    await expect(first).resolves.toBeUndefined()
    expect(loader.items.value).toEqual([{ id: 2 }])
    expect(loader.loading.value).toBe(false)

    fetchFn.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectFirst = reject }))
    const pending = loader.load()
    vi.mocked(onUnmounted).mock.calls.at(-1)![0]()
    expect(loader.loading.value).toBe(false)
    rejectFirst(new Error('failure after leaving the page'))
    await expect(pending).resolves.toBeUndefined()
  })

  it('captures query fields and returns page-change failures to their caller', async () => {
    const fetchFn = createMockFetchFn([{ id: 1 }], 10, 2)
    const loader = useTableLoader({ fetchFn, initialParams: { search: 'first' } })
    await loader.load()
    loader.params.search = 'second'
    expect(fetchFn.mock.calls[0][2]).toEqual({ search: 'first' })
    fetchFn.mockRejectedValueOnce(new Error('page rejected'))
    await expect(loader.handlePageChange(2)).rejects.toThrow('page rejected')
    fetchFn.mockRejectedValueOnce(new Error('size rejected'))
    await expect(loader.handlePageSizeChange(50)).rejects.toThrow('size rejected')
  })

  describe('基础加载', () => {
    it('load 执行 fetchFn 并更新 items', async () => {
      const mockItems = [{ id: 1, name: 'item1' }, { id: 2, name: 'item2' }]
      const fetchFn = createMockFetchFn(mockItems, 2, 1)

      const { items, loading, load, pagination } = useTableLoader({
        fetchFn,
      })

      expect(items.value).toHaveLength(0)

      await load()

      expect(items.value).toEqual(mockItems)
      expect(pagination.total).toBe(2)
      expect(pagination.pages).toBe(1)
      expect(loading.value).toBe(false)
    })

    it('load 期间 loading 为 true', async () => {
      let resolveLoad: (v: any) => void
      const fetchFn = vi.fn(
        () => new Promise((resolve) => { resolveLoad = resolve })
      )

      const { loading, load } = useTableLoader({ fetchFn })

      const p = load()
      expect(loading.value).toBe(true)

      resolveLoad!({ items: [], total: 0, pages: 0 })
      await p

      expect(loading.value).toBe(false)
    })

    it('使用默认 pageSize=20', async () => {
      const fetchFn = createMockFetchFn()
      const { load, pagination } = useTableLoader({ fetchFn })

      await load()

      expect(fetchFn).toHaveBeenCalledWith(
        1,
        20,
        expect.anything(),
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
      expect(pagination.page_size).toBe(20)
    })

    it('可自定义 pageSize', async () => {
      const fetchFn = createMockFetchFn()
      const { load } = useTableLoader({ fetchFn, pageSize: 50 })

      await load()

      expect(fetchFn).toHaveBeenCalledWith(
        1,
        50,
        expect.anything(),
        expect.anything()
      )
    })
  })

  // --- 分页 ---

  describe('分页', () => {
    it('handlePageChange 更新页码并加载', async () => {
      const fetchFn = createMockFetchFn([], 100, 5)
      const { handlePageChange, pagination, load } = useTableLoader({ fetchFn })

      await load() // 初始加载
      fetchFn.mockClear()

      handlePageChange(3)

      expect(pagination.page).toBe(3)
      // 等待 load 完成
      await vi.runAllTimersAsync()
      expect(fetchFn).toHaveBeenCalledWith(3, 20, expect.anything(), expect.anything())
    })

    it('handlePageSizeChange 重置到第1页并加载', async () => {
      const fetchFn = createMockFetchFn([], 100, 5)
      const { handlePageSizeChange, pagination, load } = useTableLoader({ fetchFn })

      await load()
      pagination.page = 3
      fetchFn.mockClear()

      handlePageSizeChange(50)

      expect(pagination.page).toBe(1)
      expect(pagination.page_size).toBe(50)
    })

    it('handlePageChange 限制页码范围', async () => {
      const fetchFn = createMockFetchFn([], 100, 5)
      const { handlePageChange, pagination, load } = useTableLoader({ fetchFn })

      await load()

      // 超出范围的页码被限制
      handlePageChange(999)
      expect(pagination.page).toBe(5) // 限制在 pages=5

      handlePageChange(0)
      expect(pagination.page).toBe(1) // 最小为 1
    })
  })

  // --- 搜索防抖 ---

  describe('搜索防抖', () => {
    it('debouncedReload 在 300ms 内多次调用只执行一次', async () => {
      const fetchFn = createMockFetchFn()
      const { debouncedReload } = useTableLoader({ fetchFn })

      // 快速连续调用
      debouncedReload()
      debouncedReload()
      debouncedReload()

      // 还没到 300ms，不应调用 fetchFn
      expect(fetchFn).not.toHaveBeenCalled()

      // 推进 300ms
      vi.advanceTimersByTime(300)

      // 等待异步完成
      await vi.runAllTimersAsync()

      expect(fetchFn).toHaveBeenCalledTimes(1)
    })

    it('reload 重置到第 1 页', async () => {
      const fetchFn = createMockFetchFn([], 100, 5)
      const { reload, pagination, load } = useTableLoader({ fetchFn })

      await load()
      pagination.page = 3

      await reload()

      expect(pagination.page).toBe(1)
    })
  })

  // --- 请求取消 ---

  describe('请求取消', () => {
    it('新请求取消前一个未完成的请求', async () => {
      let callCount = 0
      const fetchFn = vi.fn((_page, _size, _params, options) => {
        callCount++
        const currentCall = callCount
        return new Promise((resolve, reject) => {
          // 模拟监听 abort
          if (options?.signal) {
            options.signal.addEventListener('abort', () => {
              reject({ name: 'CanceledError', code: 'ERR_CANCELED' })
            })
          }
          // 异步解决
          setTimeout(() => {
            resolve({ items: [{ id: currentCall }], total: 1, pages: 1 })
          }, 1000)
        })
      })

      const { load } = useTableLoader({ fetchFn })

      // 第一次加载
      const p1 = load()
      // 第二次加载（应取消第一次）
      const p2 = load()

      // 推进时间让第二次完成
      vi.advanceTimersByTime(1000)
      await vi.runAllTimersAsync()

      // 等待两个 Promise settle
      await Promise.allSettled([p1, p2])

      // 第二次请求的结果生效
      expect(fetchFn).toHaveBeenCalledTimes(2)
    })
  })

  // --- 错误处理 ---

  describe('错误处理', () => {
    it('非取消错误会被抛出', async () => {
      const fetchFn = vi.fn().mockRejectedValue(new Error('Server error'))
      const { load } = useTableLoader({ fetchFn })

      await expect(load()).rejects.toThrow('Server error')
    })

    it('取消错误被静默处理', async () => {
      const fetchFn = vi.fn().mockRejectedValue({ name: 'CanceledError', code: 'ERR_CANCELED' })
      const { load } = useTableLoader({ fetchFn })

      // 不应抛出
      await load()
    })
  })
})
