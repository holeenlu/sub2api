import { describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { accessPolicyScopeKey } from '../../services'
import { useGroupListQuery } from '../groupList'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
const list = vi.hoisted(() => vi.fn())
vi.mock('../../api/groups', () => ({ groupsAPI: { list } }))
function deferred() { let resolve!: (value: { items: AdminGroup[]; total: number; pages: number }) => void; let reject!: (value: Error) => void; const promise = new Promise<{ items: AdminGroup[]; total: number; pages: number }>((yes, no) => { resolve = yes; reject = no }); return { promise, resolve, reject } }
describe('Group query scope', () => {
 it.each(['resolve', 'reject'] as const)('discards an old %s after scope changes and releases loading', async outcome => {
  list.mockReset(); const first = deferred(); const next = deferred(); list.mockReturnValueOnce(first.promise).mockReturnValueOnce(next.promise)
  const scope = ref('epoch1:platform'); let query!: ReturnType<typeof useGroupListQuery>
  const wrapper = mount(defineComponent({ setup() { query = useGroupListQuery(); return () => null } }), { global: { provide: { [accessPolicyScopeKey as symbol]: scope } } })
  const old = query.load(1, 20, {}); const signal = list.mock.calls[0][3].signal as AbortSignal
  scope.value = 'epoch2:supplier:7'; expect(signal.aborted).toBe(true); expect(query.loading.value).toBe(false)
  const current = query.load(1, 20, {})
  if (outcome === 'resolve') first.resolve({ items: [{ id: 1 } as AdminGroup], total: 1, pages: 1 }); else first.reject(new Error('stale failure'))
  await old; expect(query.groups.value).toEqual([]); expect(query.error.value).toBeNull(); expect(query.loading.value).toBe(true)
  next.resolve({ items: [{ id: 2 } as AdminGroup], total: 1, pages: 1 }); await current; expect(query.groups.value.map(row => row.id)).toEqual([2])
  wrapper.unmount(); expect(query.groups.value).toEqual([]); expect(query.loading.value).toBe(false)
 })
})
