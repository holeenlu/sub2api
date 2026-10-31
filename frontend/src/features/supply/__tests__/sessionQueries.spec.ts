import { effectScope, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useSupplyPage } from '../useSupplyPage'
import { useSupplyRequest } from '../useSupplyRequest'
describe('supplier query scopes', () => {
  it('clears loaded data and rejects a late reply after scope replacement', async () => {
    const key = ref<string | null>('user:parent:children')
    let finish: (value: { items: { id: number }[]; total: number; page: number; page_size: number }) => void = () => { throw new Error('No request') }
    const fetch = vi.fn().mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const scope = effectScope()
    const page = scope.run(() => useSupplyPage<{ id: number }>(fetch, key))
    if (!page) throw new Error('No test scope')
    const work = page.load()
    key.value = 'user:other:readonly'
    expect(fetch.mock.calls[0][1].aborted).toBe(true)
    finish({ items: [{ id: 1 }], total: 1, page: 1, page_size: 20 })
    await work
    expect(page.items.value).toEqual([]); expect(page.loading.value).toBe(false)
    scope.stop()
  })
  it('does not let an old scope regain authority by returning to the same key', async () => {
    const key = ref<string | null>('a'), scope = effectScope()
    const owner = scope.run(() => useSupplyRequest(key))
    if (!owner) throw new Error('No owner')
    const old = owner.begin('mutation')
    key.value = 'b'; key.value = 'a'
    expect(old.signal.aborted).toBe(true)
    await expect(old.wait(Promise.resolve('late'))).rejects.toMatchObject({ name: 'AbortError' })
    scope.stop()
  })
})
