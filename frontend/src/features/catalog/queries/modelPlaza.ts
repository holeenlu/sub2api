import { computed, onScopeDispose, ref, watch, type ComputedRef, type Ref } from 'vue'
import { getModelPlaza } from '../api/modelPlaza'
import type { ModelPlazaResponse } from '../api/types'
import { catalogScopeKey, catalogPresentationKey, requireCatalogPort } from '../ports'
import { readApiError } from '@/shared/lib/apiError'
export type ModelPlazaAvailability = 'unknown' | 'disabled' | 'authentication_required' | 'available'
export interface ModelPlazaQuery {
  data: Ref<ModelPlazaResponse | null>
  loading: Ref<boolean>
  loadFailed: ComputedRef<boolean>
  errorStatus: Ref<number | null>
  availability: ComputedRef<ModelPlazaAvailability>
  reload(): Promise<void>
}
export function useModelPlazaQuery(): ModelPlazaQuery {
  const scope = requireCatalogPort(catalogScopeKey)
  const presentation = requireCatalogPort(catalogPresentationKey)
  const availability = computed<ModelPlazaAvailability>(() => {
    if (!presentation.value.settingsLoaded || !presentation.value.settings) return 'unknown'
    if (presentation.value.settings.model_plaza_enabled !== true) return 'disabled'
    if (presentation.value.settings.model_plaza_require_auth === true && !presentation.value.authenticated) return 'authentication_required'
    return 'available'
  })
  const data = ref<ModelPlazaResponse | null>(null)
  const loading = ref(false)
  const errorStatus = ref<number | null>(null)
  const loadFailed = computed(() => errorStatus.value !== null)
  let generation = 0
  let disposed = false
  let controller: AbortController | undefined
  let pending: Promise<void> | undefined
  function clear(): void { ++generation; controller?.abort(); data.value = null; loading.value = false; errorStatus.value = null }
  async function fetchAvailable(): Promise<void> {
    if (disposed) return
    clear()
    if (availability.value !== 'available') return
    const current = generation
    const captured = scope.value
    const request = new AbortController()
    controller = request
    const isCurrent = () => !disposed && availability.value === 'available' && current === generation && scope.value === captured && !request.signal.aborted
    loading.value = true
    try {
      const response = await getModelPlaza({ signal: request.signal })
      if (isCurrent()) data.value = response
    } catch (error: unknown) {
      if (isCurrent()) {
        const failure = readApiError(error)
        errorStatus.value = failure.status ?? failure.response?.status ?? 0
      }
    } finally {
      if (isCurrent()) loading.value = false
    }
  }
  async function reload(): Promise<void> {
    if (disposed) return
    const captured = scope.value
    const before = availability.value
    if (before === 'unknown' || [401, 403, 404].includes(errorStatus.value ?? 0)) {
      try { await presentation.value.refreshSettings() } catch { /* Keep configuration unknown; never infer access. */ }
      if (disposed || captured !== scope.value) return
      // A changed policy has already invalidated the old query through watch.
      if (before !== availability.value) { await pending; return }
    }
    pending = fetchAvailable()
    await pending
  }
  watch([scope, availability], () => { pending = fetchAvailable() }, { immediate: true, flush: 'sync' })
  onScopeDispose(() => { disposed = true; clear() })
  return { data, loading, loadFailed, errorStatus, availability, reload }
}
