import { onScopeDispose, watch } from 'vue'
import { readApiError } from '@/shared/lib/apiError'
import type { AnnouncementServices } from '../announcementPorts'

/** Page-owned admin observations; invalidated by actor or management permission changes. */
export function useAnnouncementRequests(services: Pick<AnnouncementServices, 'scopeKey' | 'canManage'>) {
  const lanes = new Map<string, AbortController>()
  const resets: Array<() => void> = []
  let disposed = false
  function cancel(lane?: string) {
    if (lane !== undefined) { lanes.get(lane)?.abort(); lanes.delete(lane); return }
    for (const controller of lanes.values()) controller.abort()
    lanes.clear()
  }
  function begin(lane: string) {
    cancel(lane)
    const scope = services.scopeKey.value
    const controller = new AbortController()
    lanes.set(lane, controller)
    const isCurrent = () => !disposed && scope !== null && scope === services.scopeKey.value
      && !controller.signal.aborted && lanes.get(lane) === controller
    async function wait<T>(pending: Promise<T>): Promise<T> {
      const result = await pending
      if (!isCurrent()) throw new DOMException('Announcement request cancelled', 'AbortError')
      return result
    }
    return { signal: controller.signal, isCurrent, wait }
  }
  watch([services.scopeKey, services.canManage], () => { cancel(); for (const reset of resets) reset() }, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; cancel() })
  return { begin, cancel, onScopeChange: (reset: () => void) => resets.push(reset) }
}

export function isAnnouncementCancellation(error: unknown): boolean {
  const detail = readApiError(error)
  return detail.code === 'STEP_UP_CANCELLED' || detail.code === 'ERR_CANCELED' || detail.name === 'AbortError'
}
