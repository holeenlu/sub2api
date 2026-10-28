import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAnnouncementStore } from '../useAnnouncements'
import type { UserAnnouncement } from '../../model/announcement'
const api = vi.hoisted(() => ({ list: vi.fn(), markRead: vi.fn() }))
vi.mock('../../api/announcements', () => ({ announcementsAPI: api }))
const notice = (id: number): UserAnnouncement => ({ id, title: 'Notice ' + id, content: 'Content', notify_mode: 'popup', created_at: '', updated_at: '' })
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => { resolve = res })
  return { promise, resolve }
}
beforeEach(() => { setActivePinia(createPinia()); vi.resetAllMocks(); vi.useFakeTimers(); api.markRead.mockResolvedValue({ message: 'ok' }) })
afterEach(() => { useAnnouncementStore().$dispose(); vi.useRealTimers() })

describe('announcement actor scope and popup timing', () => {
  it('does not send private requests before app sets a scope or in the supplier/anonymous scope', async () => {
    const store = useAnnouncementStore()
    await store.fetchAnnouncements(true)
    expect(await store.markAsRead(1)).toBe(false)
    expect(await store.markAllAsRead()).toBe(false)
    expect(api.list).not.toHaveBeenCalled()
    expect(api.markRead).not.toHaveBeenCalled()
  })

  it('retains the 20 item cap, throttle and session popup deduplication', async () => {
    const store = useAnnouncementStore()
    store.setScope('actor-a:user')
    api.list.mockResolvedValue(Array.from({ length: 25 }, (_, i) => notice(i + 1)))
    await store.fetchAnnouncements()
    expect(store.announcements).toHaveLength(20)
    expect(store.currentPopup?.id).toBe(1)
    await store.fetchAnnouncements()
    expect(api.list).toHaveBeenCalledTimes(1)
    await store.dismissPopup()
    expect(store.currentPopup).toBeNull()
    await vi.advanceTimersByTimeAsync(299)
    expect(store.currentPopup).toBeNull()
    await vi.advanceTimersByTimeAsync(1)
    expect(store.currentPopup?.id).toBe(2)
    await store.fetchAnnouncements(true)
    expect(store.currentPopup?.id).toBe(2)
    store.setScope('actor-a:user')
    await store.fetchAnnouncements()
    expect(api.list).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(20 * 60 * 1000)
    await store.fetchAnnouncements()
    expect(api.list).toHaveBeenCalledTimes(3)
  })

  it('cancels both a delayed popup and in-flight read when the actor changes', async () => {
    const store = useAnnouncementStore()
    store.setScope('actor-a:user')
    api.list.mockResolvedValue([notice(1), notice(2)])
    await store.fetchAnnouncements()
    const old = deferred<{ message: string }>()
    api.markRead.mockReturnValueOnce(old.promise)
    await store.dismissPopup()
    const signal = api.markRead.mock.calls[0][1].signal as AbortSignal
    store.setScope('actor-b:user')
    api.list.mockResolvedValue([notice(1)])
    await store.fetchAnnouncements()
    old.resolve({ message: 'ok' })
    await vi.advanceTimersByTimeAsync(500)
    expect(signal.aborted).toBe(true)
    expect(store.currentPopup?.id).toBe(1)
    expect(store.announcements[0].read_at).toBeUndefined()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('does not restore private rows after an old fetch resolves for the new actor', async () => {
    const store = useAnnouncementStore()
    store.setScope('actor-a:user')
    const old = deferred<UserAnnouncement[]>()
    api.list.mockReturnValueOnce(old.promise).mockResolvedValueOnce([notice(2)])
    const pending = store.fetchAnnouncements()
    const signal = api.list.mock.calls[0][1].signal as AbortSignal
    store.setScope('actor-b:user')
    await store.fetchAnnouncements()
    old.resolve([notice(1)])
    await pending
    expect(signal.aborted).toBe(true)
    expect(store.announcements.map(item => item.id)).toEqual([2])
  })

  it('does not apply old mark-all results to same-number announcements in another scope', async () => {
    const store = useAnnouncementStore()
    store.setScope('actor-a:user')
    store.announcements = [notice(1)]
    const old = deferred<{ message: string }>()
    api.markRead.mockReturnValueOnce(old.promise)
    const pending = store.markAllAsRead()
    store.setScope('actor-b:user')
    store.announcements = [notice(1)]
    old.resolve({ message: 'ok' })
    expect(await pending).toBe(false)
    expect(store.announcements[0].read_at).toBeUndefined()
    expect(store.loading).toBe(false)
  })

  it('drops queued popups withdrawn from the server audience projection', async () => {
    const store = useAnnouncementStore()
    store.setScope('actor-a:user')
    api.list.mockResolvedValueOnce([notice(1), notice(2)]).mockResolvedValueOnce([])
    await store.fetchAnnouncements()
    await store.dismissPopup()
    await store.fetchAnnouncements(true)
    await vi.advanceTimersByTimeAsync(300)
    expect(store.currentPopup).toBeNull()
    expect(store.announcements).toEqual([])
  })
})
