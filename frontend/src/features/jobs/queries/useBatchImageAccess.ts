import { computed, onScopeDispose, ref, watch } from 'vue'
import { useJobsServices, type JobsServices } from '../services'
import { useJobsRequest } from './useJobsRequest'
export function useBatchImageAccess(services: JobsServices = useJobsServices()) {
  const state = ref<'idle' | 'loading' | 'ready' | 'error'>('idle')
  const allowed = ref(false)
  const requests = useJobsRequest(services)
  let pending: Promise<boolean> | undefined
  onScopeDispose(() => { state.value = 'idle'; allowed.value = false; pending = undefined })
  watch(services.scopeKey, () => { state.value = 'idle'; allowed.value = false; pending = undefined }, { flush: 'sync' })
  function refreshBatchImageAccess(force = false): Promise<boolean> {
    if (services.scopeKey.value === null) return Promise.resolve(false)
    if (state.value === 'ready' && !force) return Promise.resolve(allowed.value)
    if (pending && !force) return pending
    const request = requests.begin('access')
    state.value = 'loading'
    const work = (async () => {
      try {
        for (let page = 1;;page++) {
          request.check()
          const result = await request.wait(services.listPersonalKeys(page, 100, {
            status: 'active', sort_by: 'created_at', sort_order: 'desc'
          }, { signal: request.signal }))
          if (result.items.some(key => key.status === 'active' && key.group?.platform === 'gemini'
            && key.group.allow_batch_image_generation === true)) {
            allowed.value = true; state.value = 'ready'; return true
          }
          if (page >= result.pages || result.items.length === 0) {
            allowed.value = false; state.value = 'ready'; return false
          }
        }
      } catch {
        if (request.isCurrent()) { state.value = 'error'; allowed.value = false }
        return false
      } finally { if (request.isCurrent()) pending = undefined }
    })()
    pending = work
    return work
  }
  return {
    canUseBatchImage: computed(() => allowed.value),
    batchImageAccessLoaded: computed(() => state.value === 'ready'),
    batchImageAccessLoading: computed(() => state.value === 'loading'),
    refreshBatchImageAccess
  }
}
