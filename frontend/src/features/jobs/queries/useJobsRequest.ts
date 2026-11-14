import { onScopeDispose, watch } from 'vue'
import type { JobsServices } from '../services'
/** Page-owned lanes: superseded queries and commands cannot publish into another scope. */
export function useJobsRequest(services: Pick<JobsServices, 'scopeKey'>) {
  const lanes = new Map<string, AbortController>()
  let disposed = false
  function cancel(lane?: string) {
    if (lane !== undefined) {
      for (const [key, controller] of lanes) if (key === lane || (lane.endsWith(':') && key.startsWith(lane))) { controller.abort(); lanes.delete(key) }
      return
    }
    for (const controller of lanes.values()) controller.abort()
    lanes.clear()
  }
  function begin(lane: string) {
    cancel(lane)
    const controller = new AbortController()
    lanes.set(lane, controller)
    const scope = services.scopeKey.value
    const isCurrent = () => !disposed && scope !== null && services.scopeKey.value === scope
      && lanes.get(lane) === controller && !controller.signal.aborted
    function check() { if (!isCurrent()) throw new DOMException('Job wait cancelled', 'AbortError') }
    async function wait<T>(work: Promise<T>): Promise<T> { const value = await work; check(); return value }
    return { signal: controller.signal, isCurrent, check, wait }
  }
  watch(services.scopeKey, () => cancel(), { flush: 'sync' })
  onScopeDispose(() => { disposed = true; cancel() })
  return { begin, cancel }
}
export function isJobWaitCancelled(error: unknown): boolean {
  return error instanceof Error && (error.name === 'AbortError' || error.name === 'CanceledError')
}
