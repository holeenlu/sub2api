import { onScopeDispose, watch } from 'vue'
import { requireSubscriptionPort, subscriptionScopeKey } from '../ports'
/** One owner per independently loading subscription query or command. */
export function useSubscriptionRequest(reset: () => void = () => {}) {
  const scope = requireSubscriptionPort(subscriptionScopeKey)
  let sequence = 0
  let controller: AbortController | undefined
  let disposed = false
  function cancel() { ++sequence; controller?.abort(); controller = undefined; reset() }
  watch(scope, cancel, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; cancel() })
  function begin() {
    controller?.abort()
    const pending = new AbortController()
    controller = pending
    const current = ++sequence
    const captured = scope.value
    return { signal: pending.signal, isCurrent: () => !disposed && !pending.signal.aborted && current === sequence && captured === scope.value }
  }
  return { begin, cancel, scope }
}
