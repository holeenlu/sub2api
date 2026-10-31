import { onScopeDispose, watch, type Ref } from 'vue'

/** A single independent usage request owner; there is no process-wide cache. */
export function useUsageRequest(scopeKey: Readonly<Ref<string | null>>) {
  let sequence = 0
  let controller: AbortController | undefined
  let disposed = false
  function cancel(): void { ++sequence; controller?.abort(); controller = undefined }
  function begin() {
    cancel()
    const request = new AbortController()
    controller = request
    const id = sequence
    const scope = scopeKey.value
    const isCurrent = () => !disposed && scope !== null && scope === scopeKey.value
      && id === sequence && !request.signal.aborted
    return { signal: request.signal, isCurrent }
  }
  watch(scopeKey, cancel, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; cancel() })
  return { begin, cancel }
}
