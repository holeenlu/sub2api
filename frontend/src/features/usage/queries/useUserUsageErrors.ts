import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SelectOption } from '@/shared/ui/Select.vue'
import { COMMON_ERROR_STATUS_CODES } from '@/features/usage/errorPresentation'
import { usageAPI } from '../api/usage'
import type { UserErrorRequest } from '../api/types'
import { useUsageServices } from '../services'
import { useUsageRequest } from './useUsageRequest'
export function useUserUsageErrors(options: { apiKeys: () => Array<{ id: number; name: string }>; range: () => { start: string; end: string } }) {
const services = useUsageServices()
const request = useUsageRequest(services.scopeKey)
const { t } = useI18n()
const errorRows = ref<UserErrorRequest[]>([])
const errorLoading = ref(false)
const errorPage = ref(1)
const errorPageSize = ref(20)
const errorSortBy = ref('created_at')
const errorSortOrder = ref<'asc' | 'desc'>('desc')
const errorTotal = ref(0)
const errorFilter = ref<{ model: string | null; category: string; api_key_id: number | null; status_code: number | null }>({
  model: '',
  category: '',
  api_key_id: null,
  status_code: null,
})

const errorKeyOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allKeys') },
  ...options.apiKeys().map((k) => ({ value: k.id, label: k.name })),
])

// 模型候选取自当前已加载错误中出现过的模型；creatable 允许输入任意片段做后端模糊。
const errorModelOptions = computed<SelectOption[]>(() => {
  const seen = new Set<string>()
  const opts: SelectOption[] = []
  for (const r of errorRows.value) {
    if (r.model && !seen.has(r.model)) {
      seen.add(r.model)
      opts.push({ value: r.model, label: r.model })
    }
  }
  return opts
})

const errorCategoryCodes = ['auth', 'rate_limit', 'quota', 'invalid_request', 'service_unavailable', 'upstream', 'internal', 'cyber']

const errorCategoryOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('usage.errors.allCategories') },
  ...errorCategoryCodes.map((c) => ({ value: c, label: t('usage.errors.categories.' + c) })),
])

// 状态码候选用固定常用列表(与管理端 UsageFilters 共用常量),不受当前页数据限制:
// 后端 status_code 过滤对全量生效,若只列当前页出现过的码,用户就选不到仅在后续页的码。
const errorStatusOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allStatuses') },
  ...COMMON_ERROR_STATUS_CODES.map((c) => ({ value: c, label: String(c) })),
])

const applyErrorFilters = () => {
  errorPage.value = 1
  void loadErrors()
}

const loadErrors = async () => {
  const ticket = request.begin()
  if (!ticket.isCurrent()) return
  errorLoading.value = true
  try {
    const resp = await usageAPI.listMyErrorRequests({
      page: errorPage.value,
      page_size: errorPageSize.value,
      start_date: options.range().start,
      end_date: options.range().end,
      model: (errorFilter.value.model ?? '').trim() || undefined,
      category: errorFilter.value.category || undefined,
      api_key_id: errorFilter.value.api_key_id ?? undefined,
      status_code: errorFilter.value.status_code ?? undefined,
      sort_by: errorSortBy.value,
      sort_order: errorSortOrder.value,
    }, ticket)
    if (!ticket.isCurrent()) return
    errorRows.value = resp.items
    errorTotal.value = resp.total
  } catch (error) {
    if (!ticket.isCurrent()) return
    services.notifyError(t('usage.errors.failedToLoad'))
  } finally {
    if (ticket.isCurrent()) errorLoading.value = false
  }
}

const onErrorSort = (sortBy: string, sortOrder: 'asc' | 'desc') => {
  errorSortBy.value = sortBy
  errorSortOrder.value = sortOrder
  errorPage.value = 1
  void loadErrors()
}

const onErrorPage = (page: number) => {
  errorPage.value = page
  void loadErrors()
}

const onErrorPageSize = (pageSize: number) => {
  errorPageSize.value = pageSize
  errorPage.value = 1
  void loadErrors()
}


function clear(): void { request.cancel(); errorRows.value = []; errorTotal.value = 0; errorPage.value = 1; errorLoading.value = false }
watch(services.scopeKey, clear, { flush: 'sync' })
return { errorRows, errorLoading, errorPage, errorPageSize, errorTotal, errorFilter, errorKeyOptions, errorModelOptions, errorCategoryOptions, errorStatusOptions, loadErrors, applyErrorFilters, onErrorSort, onErrorPage, onErrorPageSize, clear }
}
