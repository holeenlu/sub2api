import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OpsErrorLog } from '@/features/operations'
import type { AdminUsageQueryParams } from '../api/adminUsage'
import { useUsageServices } from '../services'
import { useUsageRequest } from './useUsageRequest'
export function useAdminUsageErrors(options: { filters: () => AdminUsageQueryParams }) {
const services = useUsageServices()
const request = useUsageRequest(services.scopeKey)
const { t } = useI18n()
const errRows = ref<OpsErrorLog[]>([])
const errLoading = ref(false)
const errPage = ref(1)
const errPageSize = ref(20)
const errTotal = ref(0)
const errSortBy = ref('created_at')
const errSortOrder = ref<'asc' | 'desc'>('desc')
const showErrorModal = ref(false)
const selectedErrorId = ref<number | null>(null)

// 注意：'YYYY-MM-DDT00:00:00' 无时区后缀，按本地时区解析后再转 UTC——与页面其它日期处理语义一致，刻意如此，勿改成 'T00:00:00Z'
const toRFC3339 = (d: string | undefined, endOfDay = false): string | undefined =>
  d ? new Date(d + (endOfDay ? 'T23:59:59.999' : 'T00:00:00')).toISOString() : undefined

const loadAdminErrors = async () => {
  const ticket = request.begin()
  if (!ticket.isCurrent()) return
  errLoading.value = true
  try {
    const resp = await services.getAdminErrors({
      page: errPage.value,
      page_size: errPageSize.value,
      view: 'all',
      start_time: toRFC3339(options.filters().start_date),
      end_time: toRFC3339(options.filters().end_date, true),
      user_id: options.filters().user_id ?? undefined,
      api_key_id: options.filters().api_key_id ?? undefined,
      account_id: services.permissions.value.isSuperAdmin ? options.filters().account_id ?? undefined : undefined,
      group_id: options.filters().group_id ?? undefined,
      model: options.filters().model || undefined,
      phase: options.filters().error_phase || undefined,
      category: options.filters().error_category || undefined,
      status_codes: options.filters().status_code != null ? String(options.filters().status_code) : undefined,
      sort_by: errSortBy.value,
      sort_order: errSortOrder.value,
    }, ticket)
    if (!ticket.isCurrent()) return
    errRows.value = resp.items
    errTotal.value = resp.total
  } catch (error) {
    if (!ticket.isCurrent()) return
    services.notifyError(t('usage.errors.failedToLoad'))
  } finally {
    if (ticket.isCurrent()) errLoading.value = false
  }
}

const onErrSort = (sortBy: string, sortOrder: 'asc' | 'desc') => {
  errSortBy.value = sortBy
  errSortOrder.value = sortOrder
  errPage.value = 1
  loadAdminErrors()
}
const onErrPage = (p: number) => { errPage.value = p; loadAdminErrors() }
const onErrPageSize = (s: number) => { errPageSize.value = s; errPage.value = 1; loadAdminErrors() }
const openError = (id: number) => { selectedErrorId.value = id; showErrorModal.value = true }


watch(services.scopeKey, () => { errRows.value = []; errLoading.value = false; errPage.value = 1; errTotal.value = 0; showErrorModal.value = false; selectedErrorId.value = null }, { flush: 'sync' })
return { errRows, errLoading, errPage, errPageSize, errTotal, showErrorModal, selectedErrorId, loadAdminErrors, onErrSort, onErrPage, onErrPageSize, openError }
}
