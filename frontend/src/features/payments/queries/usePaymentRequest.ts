import { computed, onScopeDispose, watch } from 'vue'

/** Caller selects one identity: a user session OR an opaque public-order credential. */
export function usePaymentRequest(readContext: () => string | null) {
  const context = computed(readContext)
  let sequence = 0
  let controller: AbortController | undefined
  let disposed = false
  function cancel(): void { ++sequence; controller?.abort(); controller = undefined }
  function begin() {
    cancel()
    const captured = context.value, id = sequence
    const request = new AbortController(); controller = request
    const isCurrent = () => !disposed && captured !== null && captured === context.value && id === sequence && !request.signal.aborted
    return { signal: request.signal, isCurrent }
  }
  watch(context, cancel, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; cancel() })
  return { begin, cancel }
}
