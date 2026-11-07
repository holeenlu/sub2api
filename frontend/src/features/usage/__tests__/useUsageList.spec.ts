import { describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { useUsageList } from '../queries/useUsageList'
import type { UsageLog } from '../api/types'
import type { PaginatedResponse } from '@/shared/http/requestTypes';
function deferred<T>() {
  let resolve: (value: T) => void = () => undefined
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
const result = (cursor: string): PaginatedResponse<UsageLog> => ({ items: [], total: 0, page: 1, page_size: 20, pages: 1, next_cursor: cursor, total_exact: true })
function setup(fetch = vi.fn()) {
  const scope = ref<string | null>('user:1|session:1')
  const states: ReturnType<typeof useUsageList<UsageLog>>[] = []
  const onError = vi.fn()
  const wrapper = mount(defineComponent({ setup() {
    states.push(useUsageList({ scopeKey: scope, pageSize: 20, filters: () => ({ group_id: 0, native_compaction_v2: false }), fetch, onError }))
    return () => null
  } }))
  return { wrapper, state: states[0], scope, fetch, onError }
}
describe('usage cursor queries bind identity and request parameters', () => {
  it('uses the requested page for cursor acceptance even when a later load finishes first', async () => {
    const older = deferred<PaginatedResponse<UsageLog>>(), newer = deferred<PaginatedResponse<UsageLog>>()
    const fetch = vi.fn().mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise).mockResolvedValue(result('last'))
    const { wrapper, state } = setup(fetch)
    const first = state.load(), second = state.load()
    newer.resolve(result('current-next')); await second; older.resolve(result('stale-next')); await first
    state.pagination.page = 2; await state.load()
    expect(fetch.mock.calls[2][0]).toMatchObject({ page: 2, cursor: 'current-next', group_id: 0, native_compaction_v2: false })
    wrapper.unmount()
  })
  it('clears rows/cursors on scope replacement and ignores transport responses that ignored abort', async () => {
    const old = deferred<PaginatedResponse<UsageLog>>()
    const fetch = vi.fn().mockReturnValueOnce(old.promise).mockResolvedValue(result('new-next'))
    const { wrapper, state, scope } = setup(fetch)
    const pending = state.load(); scope.value = 'user:2|session:2'
    expect(fetch.mock.calls[0][1].signal.aborted).toBe(true)
    old.resolve({ ...result('old-next'), total: 99 }); await pending; await flushPromises()
    expect(state.pagination.total).toBe(0); expect(state.pagination.page).toBe(1)
    expect(state.state.value).toBe('success'); wrapper.unmount()
  })
  it('cancels on disposal and never accepts a late completion', async () => {
    const pending = deferred<PaginatedResponse<UsageLog>>()
    const { wrapper, state, fetch } = setup(vi.fn().mockReturnValue(pending.promise))
    const loading = state.load(); wrapper.unmount()
    expect(fetch.mock.calls[0][1].signal.aborted).toBe(true)
    pending.resolve({ ...result('ignored'), total: 99 }); await loading
    expect(state.pagination.total).toBe(0)
  })
})
