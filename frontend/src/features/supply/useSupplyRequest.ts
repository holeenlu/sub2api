import { onScopeDispose, watch, type Ref } from 'vue'
import { useSupplyServices } from './services'
/** A mounted supplier workflow owns requests; neither parent scope nor late replies grant authority. */
export function useSupplyRequest(scopeKey: Readonly<Ref<string | null>> = useSupplyServices().scopeKey) {
  const lanes = new Map<string, AbortController>()
  let disposed = false
  function cancel() { for (const request of lanes.values()) request.abort(); lanes.clear() }
  function begin(lane: string) {
    lanes.get(lane)?.abort()
    const request = new AbortController()
    lanes.set(lane, request)
    const scope = scopeKey.value
    const isCurrent = () => !disposed && scope !== null && scopeKey.value === scope && lanes.get(lane) === request && !request.signal.aborted
    function check() { if (!isCurrent()) throw new DOMException('Supplier scope changed', 'AbortError') }
    async function wait<T>(pending: Promise<T>): Promise<T> { const value = await pending; check(); return value }
    return { signal: request.signal, isCurrent, check, wait }
  }
  watch(scopeKey, cancel, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; cancel() })
  return { begin, cancel }
}
