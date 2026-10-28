import { reactive, ref, watch, onScopeDispose } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminPaymentAPI } from '../api/adminPayment'
import type { PaymentOrder } from '../api/types'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from './usePaymentRequest'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
export interface OrderAuditLog { id: number; action: string; detail: string | null; operator: string | null; created_at: string }
export function useAdminOrders() {
  const services = usePaymentsServices(), { t } = useI18n()
  const list = usePaymentRequest(() => services.scopeKey.value), detail = usePaymentRequest(() => services.scopeKey.value)
  const ordersLoading = ref(false), orders = ref<PaymentOrder[]>([]), orderSearch = ref('')
  const orderFilters = reactive({ status: '', payment_type: '', order_type: '' })
  const orderPagination = reactive({ page: 1, page_size: 20, total: 0 })
  const selectedOrder = ref<PaymentOrder | null>(null), showDetailDialog = ref(false), orderAuditLogs = ref<OrderAuditLog[]>([])
  let debounceTimer: ReturnType<typeof setTimeout> | undefined
  async function loadOrders(): Promise<void> {
    clearTimeout(debounceTimer)
    const ticket = list.begin()
    if (!ticket.isCurrent()) return
    const params = { page: orderPagination.page, page_size: orderPagination.page_size, keyword: orderSearch.value || undefined,
      status: orderFilters.status || undefined, payment_type: orderFilters.payment_type || undefined, order_type: orderFilters.order_type || undefined }
    ordersLoading.value = true
    try {
      const response = await adminPaymentAPI.getOrders(params, ticket)
      if (ticket.isCurrent()) { orders.value = response.data.items ?? []; orderPagination.total = response.data.total ?? 0 }
    } catch (error) { if (ticket.isCurrent()) services.notifyError(extractPaymentErrorMessage(error, t, t('common.error'))) }
    finally { if (ticket.isCurrent()) ordersLoading.value = false }
  }
  function debounceLoadOrders(): void { clearTimeout(debounceTimer); debounceTimer = setTimeout(() => { void loadOrders() }, 300) }
  async function showOrderDetail(order: PaymentOrder): Promise<void> {
    const ticket = detail.begin()
    if (!ticket.isCurrent()) return
    selectedOrder.value = order; showDetailDialog.value = true; orderAuditLogs.value = []
    try {
      const response = await adminPaymentAPI.getOrder(order.id, ticket)
      if (!ticket.isCurrent() || selectedOrder.value?.id !== order.id || !showDetailDialog.value) return
      const data = response.data as PaymentOrder & { order?: PaymentOrder; auditLogs?: OrderAuditLog[]; audit_logs?: OrderAuditLog[] }
      selectedOrder.value = data.order ?? data
      orderAuditLogs.value = data.auditLogs ?? data.audit_logs ?? []
    } catch { /* Keep the currently selected cached row on detail read failure. */ }
  }
  function handleOrderPageChange(page: number): void { orderPagination.page = page; void loadOrders() }
  function handleOrderPageSizeChange(size: number): void { orderPagination.page_size = size; orderPagination.page = 1; void loadOrders() }
  watch(showDetailDialog, show => { if (!show) detail.cancel() }, { flush: 'sync' })
  watch(services.scopeKey, () => {
    clearTimeout(debounceTimer); orders.value = []; ordersLoading.value = false; orderPagination.page = 1; orderPagination.total = 0
    selectedOrder.value = null; showDetailDialog.value = false; orderAuditLogs.value = []
    if (services.scopeKey.value !== null) void loadOrders()
  }, { flush: 'sync' })
  onScopeDispose(() => clearTimeout(debounceTimer))
  return { ordersLoading, orders, orderSearch, orderFilters, orderPagination, selectedOrder, showDetailDialog, orderAuditLogs,
    loadOrders, debounceLoadOrders, showOrderDetail, handleOrderPageChange, handleOrderPageSizeChange }
}
