import { onScopeDispose, watch } from 'vue'
import type { KeysServices } from '../services'

/** One owner per independent Key query or editor operation; no shared cache. */
export function useKeysRequest(services: KeysServices) {
  let sequence = 0
  let controller: AbortController | undefined
  let disposed = false
  function cancel(): void { ++sequence; controller?.abort(); controller = undefined }
  function begin() {
    cancel()
    const request = new AbortController()
    controller = request
    const id = sequence
    const scope = services.scopeKey.value
    const isCurrent = () => !disposed && scope !== null && scope === services.scopeKey.value
      && id === sequence && !request.signal.aborted
    return { signal: request.signal, isCurrent }
  }
  watch(services.scopeKey, cancel, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; cancel() })
  return { begin, cancel }
}
export function keyErrorMessage(error: unknown, fallback: string): string {
  if (typeof error !== 'object' || error === null) return fallback
  if ('response' in error && typeof error.response === 'object' && error.response !== null
    && 'data' in error.response && typeof error.response.data === 'object' && error.response.data !== null
    && 'detail' in error.response.data && typeof error.response.data.detail === 'string') return error.response.data.detail
  return 'message' in error && typeof error.message === 'string' ? error.message : fallback
}
