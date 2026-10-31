import { describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { catalogScopeKey, catalogPresentationKey } from '../ports'
import { useModelPlazaQuery } from '../queries/modelPlaza'
import { catalogPresentationFixture } from './presentation'
import type { ModelPlazaResponse } from '../api/types'
const getModelPlaza = vi.hoisted(() => vi.fn())
vi.mock('../api/modelPlaza', () => ({ getModelPlaza }))
function deferred() {
 let resolve!: (value: ModelPlazaResponse) => void
 let reject!: (reason: Error) => void
 const promise = new Promise<ModelPlazaResponse>((yes, no) => { resolve = yes; reject = no })
 return { promise, resolve, reject }
}
describe('model plaza public and private query isolation', () => {
 it('cannot restart a disposed query through a retained reload callback', async () => {
  getModelPlaza.mockReset(); getModelPlaza.mockResolvedValue({ description: 'public', groups: [] })
  let query!: ReturnType<typeof useModelPlazaQuery>
  const wrapper = mount(defineComponent({ setup() { query = useModelPlazaQuery(); return () => null } }), { global: { provide: { [catalogPresentationKey as symbol]: ref(catalogPresentationFixture()), [catalogScopeKey as symbol]: ref('public') } } })
  await flushPromises()
  const reload = query.reload
  wrapper.unmount()
  await reload()
  expect(getModelPlaza).toHaveBeenCalledTimes(1)
  expect(query.data.value).toBeNull(); expect(query.loading.value).toBe(false)
 })

 it.each(['resolve', 'reject'] as const)('ignores a late private %s after switching to anonymous', async outcome => {
  getModelPlaza.mockReset(); const first = deferred(); const next = deferred()
  getModelPlaza.mockReturnValueOnce(first.promise).mockReturnValueOnce(next.promise)
  const scope = ref('epoch1:user:1'); let query!: ReturnType<typeof useModelPlazaQuery>
  const wrapper = mount(defineComponent({ setup() { query = useModelPlazaQuery(); return () => null } }), { global: { provide: { [catalogPresentationKey as symbol]: ref(catalogPresentationFixture()), [catalogScopeKey as symbol]: scope } } })
  const signal = getModelPlaza.mock.calls[0][0].signal as AbortSignal
  scope.value = 'public:anonymous'; expect(signal.aborted).toBe(true); expect(query.data.value).toBeNull()
  if (outcome === 'resolve') first.resolve({ description: 'private pricing', groups: [] }); else first.reject(new Error('stale private failure'))
  await flushPromises(); expect(query.data.value).toBeNull(); expect(query.loadFailed.value).toBe(false); expect(query.loading.value).toBe(true)
  next.resolve({ description: 'public', groups: [] }); await flushPromises(); expect(query.data.value?.description).toBe('public')
  wrapper.unmount(); expect(query.data.value).toBeNull()
 })
 it('discards an uncancellable completion after unmount', async () => {
  getModelPlaza.mockReset(); const pending = deferred(); getModelPlaza.mockReturnValue(pending.promise)
  let query!: ReturnType<typeof useModelPlazaQuery>
  const wrapper = mount(defineComponent({ setup() { query = useModelPlazaQuery(); return () => null } }), { global: { provide: { [catalogPresentationKey as symbol]: ref(catalogPresentationFixture()), [catalogScopeKey as symbol]: ref('public') } } })
  wrapper.unmount(); pending.resolve({ description: 'late', groups: [] }); await flushPromises()
  expect(query.data.value).toBeNull(); expect(query.loading.value).toBe(false)
 })
})
