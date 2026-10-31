import { onScopeDispose, watch, type Ref } from 'vue'
import { readApiError } from '@/shared/lib/apiError'

export function isSystemRequestCancelled(error: unknown): boolean {
  const detail = readApiError(error)
  return detail.code === 'ERR_CANCELED' || detail.code === 'STEP_UP_CANCELLED' || detail.name === 'AbortError'
}

/** Mounted system controls own their requests, including any deferred step-up retry. */
export function useSystemRequests(scope: Readonly<Ref<string | null>>) {
  const lanes = new Map<string, AbortController>()
  const resets: Array<() => void> = []
  let disposed = false
  function cancel(lane?: string) {
    if (lane !== undefined) { lanes.get(lane)?.abort(); lanes.delete(lane); return }
    for (const controller of lanes.values()) controller.abort()
    lanes.clear()
  }
  function begin(lane: string, target: () => unknown = () => undefined) {
    cancel(lane)
    const actor = scope.value
    const selected = JSON.stringify(target())
    const controller = new AbortController()
    lanes.set(lane, controller)
    const isCurrent = () => !disposed && actor !== null && actor === scope.value
      && lanes.get(lane) === controller && !controller.signal.aborted && selected === JSON.stringify(target())
    function check() { if (!isCurrent()) throw new DOMException('System control scope changed', 'AbortError') }
    async function wait<T>(action: (signal: AbortSignal) => Promise<T>): Promise<T> {
      check()
      const result = await action(controller.signal)
      check()
      return result
    }
    return { signal: controller.signal, isCurrent, check, wait }
  }
  function invalidate() { cancel(); for (const reset of resets) reset() }
  watch(() => scope.value, invalidate, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; invalidate() })
  return { begin, cancel, onInvalidate: (reset: () => void) => { resets.push(reset) } }
}
