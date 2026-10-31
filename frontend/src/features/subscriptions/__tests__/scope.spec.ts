import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSubscriptionStore } from '../model/store'
import type { UserSubscription } from '../api/types'
const getActive = vi.hoisted(() => vi.fn())
vi.mock('../api/user', () => ({ default: { getActiveSubscriptions: getActive } }))
function deferred() {
 let resolve!: (value: UserSubscription[]) => void
 let reject!: (reason: Error) => void
 const promise = new Promise<UserSubscription[]>((yes, no) => { resolve = yes; reject = no })
 return { promise, resolve, reject }
}
beforeEach(() => { setActivePinia(createPinia()); getActive.mockReset() })
describe('subscription scope ownership', () => {
 it.each(['resolve', 'reject'] as const)('cancels and discards a late %s after scope change', async outcome => {
  const first = deferred(); const next = deferred()
  getActive.mockReturnValueOnce(first.promise).mockReturnValueOnce(next.promise)
  const store = useSubscriptionStore(); store.setScope('epoch1:user:10')
  const old = store.fetchActiveSubscriptions(); const signal = getActive.mock.calls[0][0] as AbortSignal
  store.setScope('epoch2:user:20')
  expect(signal.aborted).toBe(true); expect(store.loading).toBe(false)
  const current = store.fetchActiveSubscriptions()
  if (outcome === 'resolve') first.resolve([{ id: 1 } as UserSubscription]); else first.reject(new Error('old failure'))
  expect(await old).toEqual([]); expect(store.loading).toBe(true); expect(store.activeSubscriptions).toEqual([])
  next.resolve([{ id: 2 } as UserSubscription]); await current
  expect(store.activeSubscriptions.map(row => row.id)).toEqual([2])
 })
 it('keeps request generations local to each Pinia instance', async () => {
  const a = deferred(); const b = deferred(); getActive.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
  const first = useSubscriptionStore(createPinia()); const second = useSubscriptionStore(createPinia())
  const pa = first.fetchActiveSubscriptions(); const pb = second.fetchActiveSubscriptions()
  a.resolve([{ id: 1 } as UserSubscription]); b.resolve([{ id: 2 } as UserSubscription]); await Promise.all([pa, pb])
  expect(first.activeSubscriptions.map(row => row.id)).toEqual([1]); expect(second.activeSubscriptions.map(row => row.id)).toEqual([2])
 })
 it('disposes the Pinia effect scope and refuses to restart timers or requests', async () => {
  vi.useFakeTimers()
  try {
   const pending = deferred(); getActive.mockReturnValueOnce(pending.promise)
   const store = useSubscriptionStore(); store.startPolling(); const response = store.fetchActiveSubscriptions()
   const signal = getActive.mock.calls[0][0] as AbortSignal
   store.$dispose(); expect(signal.aborted).toBe(true); expect(store.loading).toBe(false)
   pending.resolve([{ id: 1 } as UserSubscription]); expect(await response).toEqual([])
   store.startPolling(); await store.fetchActiveSubscriptions(); await vi.advanceTimersByTimeAsync(600_000)
   expect(getActive).toHaveBeenCalledTimes(1); expect(store.activeSubscriptions).toEqual([])
  } finally { vi.useRealTimers() }
 })
})
