import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getPublicKeyUsage, type PublicKeyUsageParams, type PublicKeyUsageResponse } from '../api/publicKeyUsage'
import { usePublicKeyUsageServices } from '../publicKeyServices'
import { useUsageRequest } from './useUsageRequest'
import { formatDateLocalInput } from '@/shared/lib/format'
export type PublicUsageDateRange = 'today' | '7d' | '30d' | 'custom'
export function usePublicKeyUsage() {
  const services = usePublicKeyUsageServices(), { t } = useI18n()
  const apiKey = ref(''), isQuerying = ref(false), showResults = ref(false), showDatePicker = ref(false)
  const resultData = ref<PublicKeyUsageResponse | null>(null)
  const currentRange = ref<PublicUsageDateRange>('today'), customStartDate = ref(''), customEndDate = ref(''), dailyUsageDays = ref<7 | 30 | 90>(30)
  const scope = computed(() => apiKey.value.trim() ? JSON.stringify([apiKey.value.trim(), currentRange.value, customStartDate.value, customEndDate.value, dailyUsageDays.value]) : null)
  const request = useUsageRequest(scope)
  function params(): PublicKeyUsageParams {
    let timezone = 'UTC'
    try { timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC' } catch { /* Browser fallback preserves the existing UTC default. */ }
    const value: PublicKeyUsageParams = { days: dailyUsageDays.value, timezone }
    if (currentRange.value === 'custom') {
      if (customStartDate.value && customEndDate.value) { value.start_date = customStartDate.value; value.end_date = customEndDate.value }
    } else {
      const now = new Date(), days = currentRange.value === 'today' ? 0 : currentRange.value === '7d' ? 7 : 30
      value.end_date = formatDateLocalInput(now); value.start_date = formatDateLocalInput(new Date(now.getTime() - days * 86400000))
    }
    return value
  }
  async function queryKey(): Promise<void> {
    const key = apiKey.value.trim()
    if (!key) { services.notifyInfo(t('keyUsage.enterApiKey')); return }
    const ticket = request.begin(), query = params()
    if (!ticket.isCurrent()) return
    isQuerying.value = true; showResults.value = true; resultData.value = null
    try {
      const value = await getPublicKeyUsage(key, query, ticket)
      if (!ticket.isCurrent()) return
      resultData.value = value; showDatePicker.value = true; services.notifySuccess(t('keyUsage.querySuccess'))
    } catch (error) {
      if (!ticket.isCurrent()) return
      showResults.value = false
      const message = error && typeof error === 'object' && 'message' in error && typeof error.message === 'string' ? error.message : t('keyUsage.queryFailedRetry')
      services.notifyError(message)
    } finally { if (ticket.isCurrent()) isQuerying.value = false }
  }
  function setDateRange(value: PublicUsageDateRange): void { currentRange.value = value; if (value !== 'custom') void queryKey() }
  function setDailyUsageDays(value: 7 | 30 | 90): void {
    if (dailyUsageDays.value === value) return
    const reload = showDatePicker.value && !!apiKey.value.trim(); dailyUsageDays.value = value
    if (reload) void queryKey()
  }
  watch(scope, () => { resultData.value = null; showResults.value = false; isQuerying.value = false }, { flush: 'sync' })
  return { apiKey, isQuerying, showLoading: isQuerying, showResults, showDatePicker, resultData, currentRange, customStartDate, customEndDate, dailyUsageDays, queryKey, setDateRange, setDailyUsageDays }
}
