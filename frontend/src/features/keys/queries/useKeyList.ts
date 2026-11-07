import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getPersistedPageSize } from '@/shared/lib/usePersistedPageSize'
import { keysAPI, type ApiKeyFilters } from '../api/keys'
import type { ApiKey } from '../api/types'
import { useKeysServices, type KeyUsageSummary } from '../services'
import { useKeysRequest } from './useKeysRequest'

export function useKeyList() {
  const services = useKeysServices()
  const { t } = useI18n()
  const request = useKeysRequest(services)
  const apiKeys = ref<ApiKey[]>([])
  const selectedIds = ref<number[]>([])
  const usageStats = ref<Record<string, KeyUsageSummary>>({})
  const state = ref<'idle' | 'loading' | 'success' | 'error'>('idle')
  const usageState = ref<'idle' | 'loading' | 'success' | 'error'>('idle')
  const loading = computed(() => state.value === 'loading')
  const pagination = ref({ page: 1, page_size: getPersistedPageSize(), total: 0, pages: 0 })
  const sortState = ref({ sort_by: 'created_at', sort_order: 'desc' as 'asc' | 'desc' })
  const filterSearch = ref('')
  const filterStatus = ref('')
  const filterGroupId = ref<string | number>('')
  const filters = computed<ApiKeyFilters>(() => ({
    search: filterSearch.value || undefined, status: filterStatus.value || undefined,
    group_id: filterGroupId.value === '' ? undefined : filterGroupId.value, ...sortState.value
  }))
  const selectedApiKeys = computed(() => apiKeys.value.filter(key => selectedIds.value.includes(key.id)))
  function handleSelectionChange(ids: Array<string | number>): void {
    const visible = new Set(apiKeys.value.map(key => key.id))
    selectedIds.value = [...new Set(ids.map(Number))].filter(id => visible.has(id))
  }
  async function loadApiKeys(): Promise<void> {
    const ticket = request.begin()
    if (!ticket.isCurrent()) return
    const page = pagination.value.page
    const pageSize = pagination.value.page_size
    const capturedFilters = { ...filters.value }
    state.value = 'loading'
    usageStats.value = {}
    usageState.value = 'idle'
    try {
      const response = await keysAPI.list(page, pageSize, capturedFilters, ticket)
      if (!ticket.isCurrent()) return
      apiKeys.value = response.items
      handleSelectionChange(selectedIds.value)
      pagination.value.total = response.total
      pagination.value.pages = response.pages
      state.value = 'success'
      if (!response.items.length) return
      usageState.value = 'loading'
      try {
        const usage = await services.getKeyUsage(response.items.map(key => key.id), ticket)
        if (!ticket.isCurrent()) return
        usageStats.value = usage.stats
        usageState.value = 'success'
      } catch {
        if (ticket.isCurrent()) usageState.value = 'error'
      }
    } catch {
      if (!ticket.isCurrent()) return
      state.value = 'error'
      services.notifyError(t('keys.failedToLoad'))
    }
  }
  function onFilterChange(): void { selectedIds.value = []; pagination.value.page = 1; void loadApiKeys() }
  function onGroupFilterChange(value: string | number | boolean | null): void {
    filterGroupId.value = typeof value === 'number' || typeof value === 'string' ? value : ''
    onFilterChange()
  }
  function onStatusFilterChange(value: string | number | boolean | null): void {
    filterStatus.value = typeof value === 'string' ? value : ''
    onFilterChange()
  }
  function handlePageChange(page: number): void { selectedIds.value = []; pagination.value.page = page; void loadApiKeys() }
  function handlePageSizeChange(size: number): void { pagination.value.page_size = size; onFilterChange() }
  function handleSort(key: string, order: 'asc' | 'desc'): void { sortState.value = { sort_by: key, sort_order: order }; onFilterChange() }
  function handleBulkUpdated(ids: number[]): void {
    selectedIds.value = selectedIds.value.filter(id => !ids.includes(id)); void loadApiKeys()
  }
  function applyUpdatedKey(updated: ApiKey): void {
    apiKeys.value = apiKeys.value.map(key => key.id === updated.id ? { ...key, ...updated } : key)
    // A list started before the command may still hold the pre-update row.
    if (loading.value) void loadApiKeys()
  }
  watch(services.scopeKey, () => {
    apiKeys.value = []; selectedIds.value = []; usageStats.value = {}
    pagination.value = { ...pagination.value, page: 1, total: 0, pages: 0 }
    state.value = 'idle'; usageState.value = 'idle'
    if (services.scopeKey.value !== null) void loadApiKeys()
  }, { immediate: true, flush: 'sync' })
  return { apiKeys, selectedIds, selectedApiKeys, usageStats, state, usageState, loading,
    pagination, filters, filterSearch, filterStatus, filterGroupId, loadApiKeys,
    handleSelectionChange, handleBulkUpdated, applyUpdatedKey, onFilterChange, onGroupFilterChange,
    onStatusFilterChange, handlePageChange, handlePageSizeChange, handleSort }
}
