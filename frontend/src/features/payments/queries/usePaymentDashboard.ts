import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminPaymentAPI } from '../api/adminPayment'
import type { DashboardStats } from '../api/types'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from './usePaymentRequest'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
export function usePaymentDashboard() {
  const services = usePaymentsServices(), { t } = useI18n()
  const request = usePaymentRequest(() => services.scopeKey.value)
  const days = ref(30), loading = ref(false), stats = ref<DashboardStats | null>(null)
  async function loadDashboard(): Promise<void> {
    const ticket = request.begin(), requestedDays = days.value
    if (!ticket.isCurrent()) return
    loading.value = true
    try {
      const response = await adminPaymentAPI.getDashboard(requestedDays, ticket)
      if (ticket.isCurrent()) stats.value = response.data
    } catch (error) { if (ticket.isCurrent()) services.notifyError(extractPaymentErrorMessage(error, t, t('common.error'))) }
    finally { if (ticket.isCurrent()) loading.value = false }
  }
  watch(days, () => { void loadDashboard() })
  watch(services.scopeKey, () => { stats.value = null; loading.value = false; if (services.scopeKey.value !== null) void loadDashboard() }, { flush: 'sync' })
  return { days, loading, stats, loadDashboard }
}
