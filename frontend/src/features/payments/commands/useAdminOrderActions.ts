import { ref, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminPaymentAPI } from '../api/adminPayment'
import type { PaymentOrder } from '../api/types'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from '../queries/usePaymentRequest'
import { paymentSubmissionMayBeUnknown } from '../model/paymentRetryPolicy'
import { canRefund } from '../model/refundEligibility'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
export function useAdminOrderActions(selectedOrder: Ref<PaymentOrder | null>, reload: () => Promise<void>) {
  const services = usePaymentsServices(), { t } = useI18n()
  const request = usePaymentRequest(() => services.scopeKey.value + ':' + JSON.stringify(services.permissions.value))
  const busy = ref(false), uncertain = ref(new Set<number>())
  const showRefundDialog = ref(false), refundSubmitting = ref(false), refundRequireForce = ref(false), refundWarning = ref('')
  const refundQueryingIds = ref(new Set<number>())
  function closeRefundDialog(): void { showRefundDialog.value = false; refundRequireForce.value = false; refundWarning.value = '' }
  function openRefundDialog(order: PaymentOrder): void {
    if (!services.permissions.value.canRefundOrders || !canRefund(order.status) || busy.value || uncertain.value.has(order.id)) return
    selectedOrder.value = order; refundRequireForce.value = false; refundWarning.value = ''; showRefundDialog.value = true
  }
  async function run(order: PaymentOrder, kind: 'cancel' | 'retry' | 'refund' | 'query', data?: { amount: number; reason: string; deduct_balance: boolean; force: boolean }): Promise<void> {
    if (busy.value || !services.scopeKey.value || (kind !== 'query' && uncertain.value.has(order.id))) return
    if (kind === 'cancel' || kind === 'retry') { if (!services.permissions.value.canManageOrders) return }
    else if (!services.permissions.value.canRefundOrders) return
    if (kind === 'refund' && (!showRefundDialog.value || !canRefund(order.status))) return
    const ticket = request.begin()
    if (!ticket.isCurrent()) return
    busy.value = true; refundSubmitting.value = kind === 'refund'
    if (kind === 'query') refundQueryingIds.value = new Set([order.id])
    try {
      if (kind === 'cancel' || kind === 'retry') {
        if (kind === 'cancel') await adminPaymentAPI.cancelOrder(order.id, ticket)
        else await adminPaymentAPI.retryRecharge(order.id, ticket)
        if (!ticket.isCurrent()) return
        services.notifySuccess(t(kind === 'cancel' ? 'payment.admin.orderCancelled' : 'payment.admin.retrySuccess'))
      } else {
        if (kind === 'refund' && !data) return
        const response = kind === 'query' ? await adminPaymentAPI.queryRefund(order.id, ticket)
          : data ? await adminPaymentAPI.refundOrder(order.id, { ...data }, ticket) : null
        if (!response) return
        if (!ticket.isCurrent()) return
        const result = response.data
        if (result.success || /pending|处理中|待/.test(String(result.warning || '').toLowerCase())) {
          services.notifySuccess(t(result.success ? 'payment.admin.refundSuccess' : 'payment.admin.refundPending'))
          if (kind === 'refund' && selectedOrder.value?.id === order.id) closeRefundDialog()
          uncertain.value.delete(order.id)
        } else if (result.require_force && kind === 'refund' && selectedOrder.value?.id === order.id) {
          refundRequireForce.value = true; refundWarning.value = result.warning || ''; return
        } else { services.notifyError(result.warning || t('common.error')); return }
      }
      await reload()
    } catch (error) {
      if (!ticket.isCurrent()) return
      if (paymentSubmissionMayBeUnknown(error)) uncertain.value = new Set(uncertain.value).add(order.id)
      services.notifyError(extractPaymentErrorMessage(error, t, t('common.error')))
    } finally { if (ticket.isCurrent()) { busy.value = false; refundSubmitting.value = false; refundQueryingIds.value = new Set() } }
  }
  watch(() => [services.scopeKey.value, services.permissions.value.canManageOrders, services.permissions.value.canRefundOrders], () => {
    request.cancel(); busy.value = false; refundSubmitting.value = false; refundQueryingIds.value = new Set(); closeRefundDialog()
  }, { flush: 'sync' })
  watch(services.scopeKey, () => { uncertain.value = new Set() }, { flush: 'sync' })
  return { busy, uncertain, showRefundDialog, refundSubmitting, refundRequireForce, refundWarning, refundQueryingIds, openRefundDialog, closeRefundDialog,
    handleCancelOrder: (order: PaymentOrder) => run(order, 'cancel'), handleRetryOrder: (order: PaymentOrder) => run(order, 'retry'), handleQueryRefund: (order: PaymentOrder) => run(order, 'query'),
    handleRefund: (data: { amount: number; reason: string; deduct_balance: boolean; force: boolean }) => selectedOrder.value ? run(selectedOrder.value, 'refund', data) : Promise.resolve() }
}
