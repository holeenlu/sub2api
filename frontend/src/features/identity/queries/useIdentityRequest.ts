import { onScopeDispose, watch } from 'vue'

export interface IdentityRequestTicket {
  readonly signal: AbortSignal
  isCurrent(): boolean
}

export interface IdentityRequestOwner {
  begin(): IdentityRequestTicket
  cancel(): void
}

/** Actor/session plus the caller's concrete target. Cancellation never implies server rollback. */
export function useIdentityRequest(readContext: () => unknown): IdentityRequestOwner {
  let generation = 0
  let controller: AbortController | undefined
  let disposed = false
  const context = () => JSON.stringify(readContext())

  function cancel(): void {
    generation++
    controller?.abort()
    controller = undefined
  }

  function begin(): IdentityRequestTicket {
    cancel()
    const request = new AbortController()
    controller = request
    const version = generation
    const scope = context()
    if (disposed) request.abort()
    return {
      signal: request.signal,
      isCurrent: () => !disposed && !request.signal.aborted && version === generation && scope === context()
    }
  }

  watch(context, cancel, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; cancel() })
  return { begin, cancel }
}
