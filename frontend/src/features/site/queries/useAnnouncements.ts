import { defineStore } from 'pinia'
import { computed, onScopeDispose, readonly, ref } from 'vue'
import { announcementsAPI } from '../api/announcements'
import type { UserAnnouncement } from '../model/announcement'

const THROTTLE_MS = 20 * 60 * 1000
const POPUP_DELAY_MS = 300

/** App supplies the authenticated, non-supplier scope before scheduling any loads. */
export const useAnnouncementStore = defineStore('announcements', () => {
  const scope = ref<string | null>(null)
  const announcements = ref<UserAnnouncement[]>([])
  const currentPopup = ref<UserAnnouncement | null>(null)
  const fetching = ref(false)
  const markingAll = ref(false)
  const loading = computed(() => fetching.value || markingAll.value)
  const unreadCount = computed(() => announcements.value.filter(item => !item.read_at).length)
  let lastFetchTime = 0
  let scopeVersion = 0
  let fetchGeneration = 0
  let fetchController: AbortController | null = null
  let popupTimer: ReturnType<typeof setTimeout> | null = null
  let popupQueue: UserAnnouncement[] = []
  const shownPopupIds = new Set<number>()
  const readRequests = new Map<number, { controller: AbortController; promise: Promise<void> }>()

  function isCurrent(version: number, key: string | null) {
    return key !== null && scope.value === key && scopeVersion === version
  }

  function reset() {
    ++scopeVersion
    ++fetchGeneration
    fetchController?.abort()
    fetchController = null
    for (const request of readRequests.values()) request.controller.abort()
    readRequests.clear()
    if (popupTimer !== null) clearTimeout(popupTimer)
    popupTimer = null
    announcements.value = []
    currentPopup.value = null
    popupQueue = []
    shownPopupIds.clear()
    lastFetchTime = 0
    fetching.value = false
    markingAll.value = false
  }

  function setScope(next: string | null) {
    if (next === scope.value) return
    reset()
    scope.value = next
  }

  function showNextPopup() {
    currentPopup.value = popupQueue.shift() ?? null
    if (currentPopup.value) shownPopupIds.add(currentPopup.value.id)
  }

  function enqueueNewPopups() {
    const eligible = announcements.value.filter(item => item.notify_mode === 'popup' && !item.read_at)
    const eligibleIds = new Set(eligible.map(item => item.id))
    // A newer server projection is authoritative for audience and publication status.
    popupQueue = popupQueue.filter(item => eligibleIds.has(item.id))
    if (currentPopup.value && !eligibleIds.has(currentPopup.value.id)) currentPopup.value = null
    for (const item of eligible) {
      if (!shownPopupIds.has(item.id) && !popupQueue.some(queued => queued.id === item.id)) popupQueue.push(item)
    }
    if (!currentPopup.value && popupTimer === null) showNextPopup()
  }

  async function fetchAnnouncements(force = false) {
    if (scope.value === null) return
    const now = Date.now()
    if (!force && (fetching.value || (lastFetchTime > 0 && now - lastFetchTime < THROTTLE_MS))) return
    lastFetchTime = now
    const version = scopeVersion
    const key = scope.value
    const generation = ++fetchGeneration
    fetchController?.abort()
    const controller = new AbortController()
    fetchController = controller
    fetching.value = true
    const current = () => isCurrent(version, key) && generation === fetchGeneration && !controller.signal.aborted
    try {
      const items = await announcementsAPI.list(false, { signal: controller.signal })
      if (!current()) return
      announcements.value = items.slice(0, 20)
      enqueueNewPopups()
    } catch {
      if (current()) lastFetchTime = 0
    } finally {
      if (current()) { fetching.value = false; fetchController = null }
    }
  }

  function markReadRequest(id: number): Promise<void> {
    const existing = readRequests.get(id)
    if (existing) return existing.promise
    const version = scopeVersion
    const key = scope.value
    const controller = new AbortController()
    const promise = (async () => {
      await announcementsAPI.markRead(id, { signal: controller.signal })
      if (!isCurrent(version, key) || controller.signal.aborted) return
      const item = announcements.value.find(item => item.id === id)
      if (item) item.read_at = new Date().toISOString()
    })().finally(() => {
      if (readRequests.get(id)?.controller === controller) readRequests.delete(id)
    })
    readRequests.set(id, { controller, promise })
    return promise
  }

  async function markAsRead(id: number): Promise<boolean> {
    if (scope.value === null) return false
    const version = scopeVersion
    const key = scope.value
    try {
      await markReadRequest(id)
      return isCurrent(version, key)
    } catch { return false }
  }

  async function dismissPopup() {
    if (!currentPopup.value || scope.value === null) return
    const id = currentPopup.value.id
    currentPopup.value = null
    void markAsRead(id)
    if (popupQueue.length > 0) {
      const version = scopeVersion
      const key = scope.value
      popupTimer = setTimeout(() => {
        popupTimer = null
        if (isCurrent(version, key)) showNextPopup()
      }, POPUP_DELAY_MS)
    }
  }

  async function markAllAsRead(): Promise<boolean> {
    if (scope.value === null || markingAll.value) return false
    const unread = announcements.value.filter(item => !item.read_at)
    if (unread.length === 0) return true
    const version = scopeVersion
    const key = scope.value
    markingAll.value = true
    try {
      const results = await Promise.allSettled(unread.map(item => markReadRequest(item.id)))
      if (!isCurrent(version, key)) return false
      const failure = results.find(result => result.status === 'rejected')
      if (failure?.status === 'rejected') throw failure.reason
      return true
    } finally {
      if (isCurrent(version, key)) markingAll.value = false
    }
  }

  onScopeDispose(reset)
  return { announcements, currentPopup, loading, unreadCount, scopeKey: readonly(scope),
    setScope, reset, fetchAnnouncements, dismissPopup, markAsRead, markAllAsRead }
})
