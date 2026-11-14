import { ref, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { paymentAPI } from '../api/payment'
import type { PaymentOrder } from '../api/types'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from './usePaymentRequest'
import { canUserRequestRefund } from '../model/refundEligibility'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
export function useUserOrders() {
  const services = usePaymentsServices(), { t } = useI18n()
  const listRequest = usePaymentRequest(() => services.scopeKey.value)
  const eligibilityRequest = usePaymentRequest(() => services.scopeKey.value)
  const loading = ref(false), orders = ref<PaymentOrder[]>([]), currentFilter = ref('')
  const refundEligibleProviders = ref(new Set<string>())
  const pagination = reactive({ page: 1, page_size: 20, total: 0 })
  async function fetchOrders(): Promise<void> {
    const ticket = listRequest.begin()
    if (!ticket.isCurrent()) return
    const params = { page: pagination.page, page_size: pagination.page_size, status: currentFilter.value || undefined }
    loading.value = true
    try {
      const response = await paymentAPI.getMyOrders(params, ticket)
      if (!ticket.isCurrent()) return
      orders.value = response.data.items ?? []; pagination.total = response.data.total ?? 0
    } catch (error) {
      if (ticket.isCurrent()) services.notifyError(extractPaymentErrorMessage(error, t, t('common.error')))
    } finally { if (ticket.isCurrent()) loading.value = false }
  }
  async function loadRefundEligibility(): Promise<void> {
    const ticket = eligibilityRequest.begin()
    if (!ticket.isCurrent()) return
    try {
      const response = await paymentAPI.getRefundEligibleProviders(ticket)
      if (ticket.isCurrent()) refundEligibleProviders.value = new Set(response.data.provider_instance_ids ?? [])
    } catch { /* No eligibility response means no refund capability is inferred. */ }
  }
  function canRequestRefund(order: PaymentOrder): boolean {
    return canUserRequestRefund(order, refundEligibleProviders.value)
  }
  function handlePageChange(page: number): void { pagination.page = page; void fetchOrders() }
  function handlePageSizeChange(size: number): void { pagination.page_size = size; pagination.page = 1; void fetchOrders() }
  watch(services.scopeKey, () => {
    orders.value = []; refundEligibleProviders.value = new Set(); pagination.page = 1; pagination.total = 0; loading.value = false
    if (services.scopeKey.value !== null) { void fetchOrders(); void loadRefundEligibility() }
  }, { flush: 'sync' })
  return { loading, orders, currentFilter, pagination, fetchOrders, loadRefundEligibility, canRequestRefund, handlePageChange, handlePageSizeChange }
}
