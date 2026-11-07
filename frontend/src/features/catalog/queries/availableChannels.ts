import { onScopeDispose, ref, shallowRef, watch } from 'vue'
import { getAvailableChannels } from '../api/availableChannels'
import type { UserAvailableChannel } from '../api/types'
import { catalogScopeKey, catalogUserGroupRatesKey, requireCatalogPort } from '../ports'

/** Owns this page's data, request generation and disposal; no shared private cache. */
export function useAvailableChannelsQuery() {
  const scope = requireCatalogPort(catalogScopeKey)
  const getUserGroupRates = requireCatalogPort(catalogUserGroupRatesKey)
  const channels = ref<UserAvailableChannel[]>([])
  const userGroupRates = ref<Record<number, number>>({})
  const loading = ref(false)
  const error = shallowRef<unknown>(null)
  const ratesError = shallowRef<unknown>(null)
  let generation = 0
  let controller: AbortController | undefined
  let disposed = false

  function invalidate(clearData: boolean) {
    ++generation
    controller?.abort()
    controller = undefined
    loading.value = false
    error.value = null
    ratesError.value = null
    if (clearData) {
      channels.value = []
      userGroupRates.value = {}
    }
  }

  async function reload(clearData = false) {
    if (disposed) return
    invalidate(clearData)
    const current = generation
    const capturedScope = scope.value
    const request = new AbortController()
    controller = request
    const isCurrent = () => !disposed && current === generation && capturedScope === scope.value && !request.signal.aborted
    loading.value = true
    try {
      const [list, rates] = await Promise.all([
        getAvailableChannels({ signal: request.signal }),
        getUserGroupRates(request.signal).catch((failure: unknown) => {
          if (isCurrent()) ratesError.value = failure
          // Optional custom rates retain the existing default-rate fallback.
          return {} as Record<number, number>
        })
      ])
      if (!isCurrent()) return
      channels.value = list
      userGroupRates.value = rates
    } catch (failure: unknown) {
      if (isCurrent()) error.value = failure
    } finally {
      if (isCurrent()) loading.value = false
    }
  }

  watch(scope, () => { void reload(true) }, { immediate: true, flush: 'sync' })
  onScopeDispose(() => { disposed = true; invalidate(true) })
  return { channels, userGroupRates, loading, error, ratesError, reload }
}
