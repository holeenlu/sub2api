import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { paymentAPI } from '../api/payment'
import type { PaymentOrder } from '../api/types'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from '../queries/usePaymentRequest'
import { paymentSubmissionMayBeUnknown } from '../model/paymentRetryPolicy'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
export function useUserOrderActions(reload: () => Promise<void>, canRequestRefund: (order: PaymentOrder) => boolean) {
  const services = usePaymentsServices(), { t } = useI18n()
  const request = usePaymentRequest(() => services.scopeKey.value)
  const pending = ref(false), uncertain = ref(false)
  const actionLoading = computed(() => pending.value || uncertain.value)
  const cancelTargetId = ref<number | null>(null), refundTarget = ref<PaymentOrder | null>(null), refundReason = ref('')
  function handleCancel(id: number): void { if (!actionLoading.value) cancelTargetId.value = id }
  function openRefundDialog(order: PaymentOrder): void {
    if (actionLoading.value || !canRequestRefund(order)) return
    refundTarget.value = order; refundReason.value = ''
  }
  async function run(kind: 'cancel' | 'refund'): Promise<void> {
    const id = kind === 'cancel' ? cancelTargetId.value : refundTarget.value?.id
    const reason = refundReason.value.trim()
    if (!id || actionLoading.value || (kind === 'refund' && !reason)) return
    if (kind === 'refund' && (!refundTarget.value || !canRequestRefund(refundTarget.value))) return
    const ticket = request.begin()
    if (!ticket.isCurrent()) return
    pending.value = true
    try {
      if (kind === 'cancel') await paymentAPI.cancelOrder(id, ticket)
      else await paymentAPI.requestRefund(id, { reason }, ticket)
      if (!ticket.isCurrent()) return
      services.notifySuccess(t('common.success'))
      cancelTargetId.value = null; refundTarget.value = null; refundReason.value = ''
      await reload()
    } catch (error) {
      if (!ticket.isCurrent()) return
      uncertain.value = paymentSubmissionMayBeUnknown(error)
      services.notifyError(extractPaymentErrorMessage(error, t, t('common.error')))
    } finally { if (ticket.isCurrent()) pending.value = false }
  }
  watch(services.scopeKey, () => {
    pending.value = false; uncertain.value = false; cancelTargetId.value = null; refundTarget.value = null; refundReason.value = ''
  }, { flush: 'sync' })
  return { actionLoading, cancelTargetId, refundTarget, refundReason, handleCancel, openRefundDialog,
    confirmCancel: () => run('cancel'), confirmRefund: () => run('refund') }
}
