import { onScopeDispose, ref } from 'vue'
import { duplicate, type ChannelMonitor } from '../api/adminChannelMonitor'
import { useOperationsServices, type OperationsServices } from '../services'
import { useOperationsRequest } from '../queries/useOperationsRequest'
type DuplicateState = { status: 'idle' } | { status: 'pending'; monitorId: number }
  | { status: 'success'; monitor: ChannelMonitor } | { status: 'error'; error: unknown }

/** The mounted consumer owns commands. Pending keys survive reload only in the same logical scope. */
export function useMonitorDuplicate(services: OperationsServices = useOperationsServices()) {
  const requests = useOperationsRequest(services)
  const state = ref<DuplicateState>({ status: 'idle' })
  const pendingKeys = new Map<string, string>()
  onScopeDispose(() => { state.value = { status: 'idle' }; pendingKeys.clear() })
  requests.onScopeChange(() => { state.value = { status: 'idle' }; pendingKeys.clear() })
  async function duplicateMonitor(id: number): Promise<ChannelMonitor | undefined> {
    const scope = services.scopeKey.value
    if (scope === null || state.value.status === 'pending') return
    const request = requests.begin('duplicate')
    const storageKey = 'sub2api:admin:channel-monitor-duplicate:scope:' + encodeURIComponent(scope) + ':' + id
    let key = pendingKeys.get(storageKey)
    try { key ||= sessionStorage.getItem(storageKey) || undefined } catch { /* Memory retains ambiguous commands when storage is blocked. */ }
    key ||= 'channel-monitor-duplicate-' + id + '-' + (globalThis.crypto?.randomUUID?.() || Date.now() + '-' + Math.random().toString(36).slice(2))
    pendingKeys.set(storageKey, key)
    try { sessionStorage.setItem(storageKey, key) } catch { /* The same mounted command still retains the key. */ }
    state.value = { status: 'pending', monitorId: id }
    try {
      const monitor = await request.wait(duplicate(id, { signal: request.signal, idempotencyKey: key }))
      pendingKeys.delete(storageKey)
      try { sessionStorage.removeItem(storageKey) } catch { /* Stale entries retain the same idempotent operation, never a new mutation. */ }
      state.value = { status: 'success', monitor }
      return monitor
    } catch (error) {
      if (!request.isCurrent()) return
      state.value = { status: 'error', error }
      throw error
    }
  }
  return { state, duplicateMonitor }
}
