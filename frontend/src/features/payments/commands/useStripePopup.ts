import { ref, onMounted, onScopeDispose, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { paymentAPI } from '../api/payment'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from '../queries/usePaymentRequest'
import { useOrderMonitor } from '../queries/useOrderMonitor'
import { isSuccessStatus, normalizeOrderStatus } from '../model/orderResult'
import { formatPaymentAmount } from '../model/currency'
import { isMobileDevice } from '@/shared/lib/device'
import { stripeConfirmationMayBeUnknown } from '../model/paymentRetryPolicy'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
import type { Stripe } from '@stripe/stripe-js'
export function useStripePopup(target: () => { orderId: number; method: string }) {
  const services = usePaymentsServices(), { t } = useI18n()
  const request = usePaymentRequest(() => services.scopeKey.value === null ? null : services.scopeKey.value + ':' + JSON.stringify(target()))
  const error = ref(''), success = ref(false), hint = ref(t('payment.stripePopup.redirecting')), amount = ref('')
  let disposed = false, initialized = false
  let initTimer: ReturnType<typeof setTimeout> | undefined, closeTimer: ReturnType<typeof setTimeout> | undefined
  let active: ReturnType<typeof request.begin> | undefined
  const monitor = useOrderMonitor({ orderId: () => target().orderId, recoverPending: () => false,
    isTerminal: () => disposed || success.value,
    onOrder(order) {
      if (!active?.isCurrent()) return
      if (isSuccessStatus(order.status)) {
        success.value = true; monitor.stop()
        closeTimer = setTimeout(() => { if (active?.isCurrent() && !disposed) window.close() }, 2000)
      }
    },
  })
  async function initialize(clientSecret: string, publishableKey: string): Promise<void> {
    const ticket = request.begin(); active = ticket
    if (!ticket.isCurrent()) return
    let submitted = false
    try {
      const order = (await paymentAPI.getOrder(target().orderId, ticket)).data
      if (!ticket.isCurrent() || order.user_id !== services.user.value?.id || order.id !== target().orderId) return
      amount.value = formatPaymentAmount(order.pay_amount, order.currency)
      if (isSuccessStatus(order.status)) { monitor.start(); void monitor.poll(); return }
      if (normalizeOrderStatus(order.status) !== 'PENDING') {
        const statusKey = 'payment.status.' + normalizeOrderStatus(order.status).toLowerCase()
        const label = t(statusKey)
        error.value = label === statusKey ? t('payment.result.processing') : label
        return
      }
      const sdk = await import('@stripe/stripe-js/pure')
      if (!ticket.isCurrent()) return
      const stripe = await sdk.loadStripe(publishableKey)
      if (!ticket.isCurrent()) return
      if (!stripe) { error.value = t('payment.stripeLoadFailed'); return }
      const returnUrl = new URL('/payment/result', window.location.origin)
      returnUrl.searchParams.set('order_id', String(target().orderId))
      if (target().method === 'alipay') {
        submitted = true
        const result = await stripe.confirmAlipayPayment(clientSecret, { return_url: returnUrl.toString() })
        if (!ticket.isCurrent()) return
        if (result.error) {
          error.value = result.error.message || t('payment.result.failed')
          if (stripeConfirmationMayBeUnknown(result.error)) { monitor.start(); void monitor.poll() }
        }
        else { monitor.start(); void monitor.poll() }
      } else if (target().method === 'wechat_pay') {
        hint.value = t('payment.stripePopup.loadingQr')
        const confirmWechat = stripe.confirmWechatPayPayment.bind(stripe) as (secret: string, options: { payment_method_options: { wechat_pay: { client: 'mobile_web' | 'web' } } }) => ReturnType<Stripe['confirmWechatPayPayment']>
        submitted = true
        const result = await confirmWechat(clientSecret, { payment_method_options: { wechat_pay: { client: isMobileDevice() ? 'mobile_web' : 'web' } } })
        if (!ticket.isCurrent()) return
        if (result.error) {
          error.value = result.error.message || t('payment.result.failed')
          if (stripeConfirmationMayBeUnknown(result.error)) { monitor.start(); void monitor.poll() }
        }
        else { hint.value = t('payment.result.processing'); monitor.start(); void monitor.poll() }
      }
    } catch (cause) {
      if (!ticket.isCurrent()) return
      error.value = extractPaymentErrorMessage(cause, t, t('payment.stripeLoadFailed'))
      if (submitted) { monitor.start(); void monitor.poll() }
    }
  }
  function receive(event: MessageEvent): void {
    if (disposed || initialized || event.origin !== window.location.origin || !window.opener || event.source !== window.opener) return
    const data: unknown = event.data
    if (!data || typeof data !== 'object' || !('type' in data) || data.type !== 'STRIPE_POPUP_INIT') return
    if (!('clientSecret' in data) || typeof data.clientSecret !== 'string' || !data.clientSecret
      || !('publishableKey' in data) || typeof data.publishableKey !== 'string' || !data.publishableKey) { error.value = t('payment.stripeMissingParams'); return }
    initialized = true; clearTimeout(initTimer); window.removeEventListener('message', receive)
    void initialize(data.clientSecret, data.publishableKey)
  }
  function dispose(): void {
    disposed = true; amount.value = ''; success.value = false; request.cancel(); monitor.stop(); clearTimeout(initTimer); clearTimeout(closeTimer); window.removeEventListener('message', receive)
  }
  onMounted(() => {
    window.addEventListener('message', receive)
    window.opener?.postMessage({ type: 'STRIPE_POPUP_READY' }, window.location.origin)
    initTimer = setTimeout(() => { if (!disposed && !initialized) { error.value = t('payment.stripePopup.timeout'); window.removeEventListener('message', receive) } }, 15000)
  })
  watch(() => [services.scopeKey.value, JSON.stringify(target())], dispose, { flush: 'sync' })
  onScopeDispose(dispose)
  return { error, success, hint, amount, closeWindow: () => window.close() }
}
