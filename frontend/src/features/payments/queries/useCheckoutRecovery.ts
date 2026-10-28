import { computed, ref } from 'vue'
import { paymentAPI } from '../api/payment'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from './usePaymentRequest'
import { PAYMENT_RECOVERY_STORAGE_KEY, readPaymentRecoverySnapshot } from '../model/paymentFlow'
export interface CheckoutRecoveryTarget { orderId: number; resumeToken?: string; outTradeNo?: string }
export function useCheckoutRecovery(target: () => CheckoutRecoveryTarget, allowPublic: boolean) {
  const services = usePaymentsServices()
  const snapshotToken = ref('')
  const publicCredential = computed(() => allowPublic ? target().resumeToken || snapshotToken.value : '')
  const request = usePaymentRequest(() => publicCredential.value
    ? 'public-checkout:' + publicCredential.value + ':' + JSON.stringify(target()) : services.scopeKey.value === null ? null : services.scopeKey.value + ':' + JSON.stringify(target()))
  async function load() {
    request.cancel()
    snapshotToken.value = ''
    const expected = { ...target() }
    const snapshot = readPaymentRecoverySnapshot(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY), { resumeToken: expected.resumeToken })
    if (!snapshot || !snapshot.clientSecret || (expected.orderId > 0 && snapshot.orderId !== expected.orderId)
      || (expected.outTradeNo && snapshot.outTradeNo !== expected.outTradeNo)) return null
    snapshotToken.value = allowPublic ? snapshot.resumeToken : ''
    const ticket = request.begin()
    if (!ticket.isCurrent()) return null
    const resumeToken = publicCredential.value
    if (resumeToken) {
      const verified = await services.resolvePublicOrder({ resumeToken }, ticket)
      if (!ticket.isCurrent() || verified.out_trade_no !== snapshot.outTradeNo) return null
      return { snapshot, order: null, status: verified.status, ticket }
    }
    const userId = services.user.value?.id
    const verified = await paymentAPI.getOrder(snapshot.orderId, ticket)
    if (!ticket.isCurrent() || verified.data.id !== snapshot.orderId || verified.data.user_id !== userId) return null
    return { snapshot, order: verified.data, status: verified.data.status, ticket }
  }
  return { load, cancel: request.cancel, publicCredential }
}
