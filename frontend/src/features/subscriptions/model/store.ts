import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref } from 'vue'
import subscriptionsAPI from '../api/user'
import type { UserSubscription } from '../api/types'

const CACHE_TTL_MS = 60_000

/** App calls setScope on session/portal/business-scope changes and clear on logout. */
export const useSubscriptionStore = defineStore('subscriptions', () => {
  const activeSubscriptions = ref<UserSubscription[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const lastFetchedAt = ref<number | null>(null)
  let scopeKey: string | null = null
  let generation = 0
  let disposed = false
  let controller: AbortController | null = null
  let pending: Promise<UserSubscription[]> | null = null
  let poller: ReturnType<typeof setInterval> | null = null
  const hasActiveSubscriptions = computed(() => activeSubscriptions.value.length > 0)

  function stopPolling() {
    if (poller !== null) clearInterval(poller)
    poller = null
  }
  function clear() {
    ++generation
    controller?.abort()
    controller = null
    pending = null
    loading.value = false
    loaded.value = false
    activeSubscriptions.value = []
    lastFetchedAt.value = null
    stopPolling()
  }
  function setScope(key: string | null) {
    if (scopeKey === key) return
    clear()
    scopeKey = key
  }
  async function fetchActiveSubscriptions(force = false): Promise<UserSubscription[]> {
    if (disposed) return []
    if (!force && loaded.value && lastFetchedAt.value !== null && Date.now() - lastFetchedAt.value < CACHE_TTL_MS) return activeSubscriptions.value
    if (!force && pending) return pending
    controller?.abort()
    const request = new AbortController()
    controller = request
    const current = ++generation
    const scope = scopeKey
    const isCurrent = () => !disposed && current === generation && scope === scopeKey
    loading.value = true
    const promise = subscriptionsAPI.getActiveSubscriptions(request.signal).then(data => {
      if (!isCurrent()) return []
      activeSubscriptions.value = data
      loaded.value = true
      lastFetchedAt.value = Date.now()
      return data
    }).catch((error: unknown) => {
      if (!isCurrent() || request.signal.aborted) return []
      throw error
    }).finally(() => {
      if (isCurrent()) { loading.value = false; pending = null; controller = null }
    })
    pending = promise
    return promise
  }
  function startPolling() {
    if (disposed || poller !== null) return
    poller = setInterval(() => {
      void fetchActiveSubscriptions(true).catch((error: unknown) => { console.error('Subscription polling failed:', error) })
    }, 5 * 60 * 1000)
  }
  function invalidateCache() { lastFetchedAt.value = null }
  function dispose() { clear(); disposed = true }
  onScopeDispose(dispose)
  return { activeSubscriptions, loading, hasActiveSubscriptions, fetchActiveSubscriptions, startPolling, stopPolling, clear, setScope, invalidateCache, dispose }
})
