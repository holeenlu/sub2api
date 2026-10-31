import { ref, onBeforeUnmount, type Ref } from 'vue'

export interface UseAutoRefreshOptions {
  storageKey: string
  intervals?: readonly number[]
  defaultInterval?: number
  defaultEnabled?: boolean
  onRefresh: () => Promise<void> | void
  /** Skip tick when this returns true (e.g. modal open, document hidden). */
  shouldPause?: () => boolean
}

export function useAutoRefresh(options: UseAutoRefreshOptions) {
  const {
    storageKey,
    intervals = [5, 10, 15, 30] as const,
    defaultInterval,
    onRefresh,
    shouldPause,
  } = options

  const enabled = ref(options.defaultEnabled ?? false)
  const intervalSeconds = ref(defaultInterval ?? intervals[intervals.length - 1])
  const countdown = ref(0)
  const fetching = ref(false)

  let timerId: number | undefined

  function loadFromStorage() {
    try {
      const saved = localStorage.getItem(storageKey)
      if (!saved) return
      const parsed: unknown = JSON.parse(saved)
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return
      enabled.value = 'enabled' in parsed && parsed.enabled === true
      const iv = 'interval_seconds' in parsed ? parsed.interval_seconds : undefined
      if (typeof iv === 'number' && Number.isFinite(iv) && intervals.some(interval => interval === iv)) {
        intervalSeconds.value = iv
      }
    } catch { /* ignore */ }
  }

  function saveToStorage() {
    try {
      localStorage.setItem(storageKey, JSON.stringify({
        enabled: enabled.value,
        interval_seconds: intervalSeconds.value,
      }))
    } catch { /* ignore */ }
  }

  async function tick() {
    if (!enabled.value) return
    if (shouldPause?.()) return
    if (fetching.value) return

    if (countdown.value <= 1) {
      countdown.value = intervalSeconds.value
      fetching.value = true
      try { await onRefresh() } finally { fetching.value = false }
      return
    }
    countdown.value -= 1
  }

  function start() {
    if (timerId !== undefined) return
    timerId = window.setInterval(tick, 1000)
  }

  function stop() {
    if (timerId !== undefined) {
      clearInterval(timerId)
      timerId = undefined
    }
  }

  function setEnabled(value: boolean) {
    enabled.value = value
    saveToStorage()
    if (value) {
      countdown.value = intervalSeconds.value
      start()
    } else {
      stop()
      countdown.value = 0
    }
  }

  function setInterval_(seconds: number) {
    intervalSeconds.value = seconds
    saveToStorage()
    if (enabled.value) countdown.value = seconds
  }

  function resetCountdown() {
    countdown.value = intervalSeconds.value
  }

  loadFromStorage()

  onBeforeUnmount(stop)

  return {
    enabled: enabled as Ref<boolean>,
    intervalSeconds: intervalSeconds as Ref<number>,
    countdown: countdown as Ref<number>,
    fetching: fetching as Ref<boolean>,
    intervals,
    setEnabled,
    setInterval: setInterval_,
    resetCountdown,
    start,
    stop,
  }
}
