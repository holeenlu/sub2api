import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useSupplyStore } from '../store'
const me = vi.hoisted(() => vi.fn())
vi.mock('../api', () => ({ vendorAPI: { me } }))
beforeEach(() => { setActivePinia(createPinia()); me.mockReset() })
describe('supplier session store', () => {
  it('rejects an old supplier response after identity replacement', async () => {
    let finish: (value: unknown) => void = () => { throw new Error('No request') }
    me.mockImplementationOnce(() => new Promise(resolve => { finish = resolve })).mockResolvedValue({ id: 2, policy_version: 5 })
    const store = useSupplyStore()
    store.setSessionScope('user-a:session')
    const first = store.refresh().catch(error => error)
    const signal = me.mock.calls[0][0] as AbortSignal
    store.setSessionScope('user-b:session')
    expect(signal.aborted).toBe(true)
    await store.refresh()
    finish({ id: 1, policy_version: 1 })
    expect(await first).toMatchObject({ name: 'AbortError' })
    expect(store.vendor?.id).toBe(2)
    expect(store.loading).toBe(false)
    store.$dispose()
  })
  it('does not request a supplier outside an active session and clears on disposal', async () => {
    const store = useSupplyStore()
    await expect(store.refresh()).rejects.toMatchObject({ name: 'AbortError' })
    expect(me).not.toHaveBeenCalled()
    store.setSessionScope('user-a')
    me.mockResolvedValue({ id: 1 })
    await store.refresh()
    store.$dispose()
    expect(store.vendor).toBeNull()
  })
  it('clears the cached policy on a current revocation rejection', async () => {
    const store = useSupplyStore()
    store.setSessionScope('user-a')
    me.mockResolvedValueOnce({ id: 1, policy_version: 1 }).mockRejectedValueOnce({ status: 403 })
    await store.refresh()
    await expect(store.refresh()).rejects.toMatchObject({ status: 403 })
    expect(store.vendor).toBeNull()
    expect(store.loading).toBe(false)
    store.$dispose()
  })
})
