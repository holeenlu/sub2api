import { onScopeDispose, watch } from 'vue'
import { useOperationsServices, type OperationsServices } from '../services'
/** One component owns its in-flight observations and commands; there is no global query cache. */
export function useOperationsRequest(services: Pick<OperationsServices, 'scopeKey'> & Partial<Pick<OperationsServices, 'permissions'>> = useOperationsServices()) {
  const lanes = new Map<string, AbortController>()
  let disposed = false
  const resets: Array<() => void> = []
  function reset() { cancel(); for (const clear of resets) clear() }
  function onScopeChange(clear: () => void) { resets.push(clear) }
  function cancel(lane?: string) {
    if (lane !== undefined) { lanes.get(lane)?.abort(); lanes.delete(lane); return }
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
    async function wait<T>(pending: Promise<T>): Promise<T> {
      const value = await pending
      if (!isCurrent()) throw new DOMException('Observation wait cancelled', 'AbortError')
      return value
    }
    return { signal: controller.signal, isCurrent, wait }
  }
  watch([services.scopeKey, () => services.permissions?.value.isSuperAdmin,
  () => services.permissions?.value.canManageOps, () => services.permissions?.value.canReadPromptContent],
    () => reset(), { flush: 'sync' })
  onScopeDispose(() => { disposed = true; cancel() })
  return { begin, cancel, reset, onScopeChange, scopeKey: services.scopeKey }
}
