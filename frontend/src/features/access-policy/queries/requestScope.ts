import { onScopeDispose, watch } from 'vue';
import { accessPolicyScopeKey, requireAccessPolicyService } from '../services';
export function useAccessPolicyRequest(reset: () => void = () => {}) {
 const scope = requireAccessPolicyService(accessPolicyScopeKey)
 let sequence = 0
 let controller: AbortController | undefined
 let disposed = false
 function cancel() { ++sequence; controller?.abort(); controller = undefined; reset() }
 watch(scope, cancel, { flush: 'sync' })
 onScopeDispose(() => { disposed = true; cancel() })
 function begin() {
  controller?.abort()
  const pending = new AbortController(); controller = pending
  const current = ++sequence; const captured = scope.value
  return { signal: pending.signal, isCurrent: () => !disposed && !pending.signal.aborted && current === sequence && captured === scope.value }
 }
 return { begin, cancel, scope }
}
