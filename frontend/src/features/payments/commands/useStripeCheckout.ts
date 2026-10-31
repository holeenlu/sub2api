import { ref, onMounted, onScopeDispose, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import type { Stripe, StripeElements, StripePaymentElement } from '@stripe/stripe-js'
import { usePaymentsServices } from '../services'
import { usePaymentConfig } from '../queries/usePaymentConfig'
import { useCheckoutRecovery, type CheckoutRecoveryTarget } from '../queries/useCheckoutRecovery'
import { useOrderMonitor } from '../queries/useOrderMonitor'
import type { PaymentOrder } from '../api/types'
import { isSuccessStatus, normalizeOrderStatus } from '../model/orderResult'
import { stripeConfirmationMayBeUnknown } from '../model/paymentRetryPolicy'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
import { isMobileDevice } from '@/shared/lib/device'
export function useStripeCheckout(target: () => CheckoutRecoveryTarget & { method: string }) {
  const services = usePaymentsServices(), { t } = useI18n(), router = useRouter()
  const config = usePaymentConfig(), recovery = useCheckoutRecovery(target, false)
  const loading = ref(true), initError = ref(''), stripeError = ref(''), stripeSubmitting = ref(false)
  const stripeSuccess = ref(false), stripeReady = ref(false), order = ref<PaymentOrder | null>(null)
  const wechatQrUrl = ref(''), redirecting = ref(false), showPaymentElement = ref(false), awaitingConfirmation = ref(false)
  let stripe: Stripe | null = null, elements: StripeElements | null = null, element: StripePaymentElement | null = null
  let ticket: { signal: AbortSignal; isCurrent: () => boolean } | undefined
  let closeTimer: ReturnType<typeof setTimeout> | undefined
  let invalidated = false, submitted = false
  function current(): boolean { return !invalidated && !!ticket?.isCurrent() }
  function returnUrl(): string {
    const url = new URL('/payment/result', window.location.origin)
    url.searchParams.set('order_id', String(target().orderId))
    if (target().resumeToken) url.searchParams.set('resume_token', target().resumeToken || '')
    return url.toString()
  }
  function scheduleClose(): void {
    clearTimeout(closeTimer)
    if (!current()) return
    closeTimer = setTimeout(() => {
      if (!current()) return
      if (window.opener) window.close()
      else void router.push({ path: '/payment/result', query: { order_id: String(target().orderId), resume_token: target().resumeToken } })
    }, 2000)
  }
  const monitor = useOrderMonitor({ orderId: () => target().orderId, recoverPending: () => false,
    isTerminal: () => stripeSuccess.value || invalidated,
    onOrder(value) {
      if (!current()) return
      order.value = value
      if (isSuccessStatus(value.status)) { stripeSuccess.value = true; awaitingConfirmation.value = false; wechatQrUrl.value = ''; monitor.stop(); scheduleClose() }
      else if (['FAILED', 'CANCELLED', 'EXPIRED'].includes(value.status)) { monitor.stop(); stripeError.value = t('payment.result.failed') }
    },
  })
  function waitForBackend(): void { if (current()) { awaitingConfirmation.value = true; monitor.start(); void monitor.poll() } }
  function invalidate(): void {
    invalidated = true; recovery.cancel(); monitor.stop(); clearTimeout(closeTimer)
    element?.destroy(); element = null; elements = null; stripe = null
    order.value = null; wechatQrUrl.value = ''; stripeReady.value = false; stripeSuccess.value = false
  }
  watch(() => [services.scopeKey.value, JSON.stringify(target())], invalidate, { flush: 'sync' })
  onScopeDispose(invalidate)
  async function handleGenericPay(): Promise<void> {
    if (!current() || !stripe || !elements || stripeSubmitting.value || submitted) return
    submitted = true; stripeSubmitting.value = true; stripeError.value = ''
    try {
      const response = await stripe.confirmPayment({ elements, confirmParams: { return_url: returnUrl() }, redirect: 'if_required' })
      if (!current()) return
      if (response.error) {
        stripeError.value = response.error.message || t('payment.result.failed')
        if (!stripeConfirmationMayBeUnknown(response.error)) submitted = false
        else waitForBackend()
      }
      else waitForBackend()
    } catch (error) {
      if (current()) { stripeError.value = extractPaymentErrorMessage(error, t, t('payment.result.processing')); waitForBackend() }
    } finally { if (current()) stripeSubmitting.value = false }
  }
  onMounted(async () => {
    try {
      const restored = await recovery.load()
      if (!restored) { if (!invalidated) initError.value = t('payment.stripeMissingParams'); return }
      ticket = restored.ticket
      if (!current()) return
      order.value = restored.order
      if (isSuccessStatus(restored.status)) { stripeSuccess.value = true; scheduleClose(); return }
      if (normalizeOrderStatus(restored.status) !== 'PENDING') { await router.push(returnUrl().replace(window.location.origin, '')); return }
      const paymentConfig = await config.load()
      if (!current()) return
      if (!paymentConfig?.stripe_publishable_key) { initError.value = t('payment.stripeNotConfigured'); return }
      const sdk = await import('@stripe/stripe-js/pure')
      if (!current()) return
      stripe = await sdk.loadStripe(paymentConfig.stripe_publishable_key)
      if (!current()) return
      if (!stripe) { initError.value = t('payment.stripeLoadFailed'); return }
      const secret = restored.snapshot.clientSecret, method = target().method
      loading.value = false
      if (method === 'alipay') {
        submitted = true; redirecting.value = true
        const response = await stripe.confirmAlipayPayment(secret, { return_url: returnUrl() })
        if (!current()) return
        if (response.error) {
          redirecting.value = false; stripeError.value = response.error.message || t('payment.result.failed')
          if (stripeConfirmationMayBeUnknown(response.error)) waitForBackend()
        }
        else waitForBackend()
      } else if (method === 'wechat_pay') {
        submitted = true
        const confirmWechat = stripe.confirmWechatPayPayment.bind(stripe) as (secret: string, options: { payment_method_options: { wechat_pay: { client: 'mobile_web' | 'web' } } }) => ReturnType<Stripe['confirmWechatPayPayment']>
        const response = await confirmWechat(secret, { payment_method_options: { wechat_pay: { client: isMobileDevice() ? 'mobile_web' : 'web' } } })
        if (!current()) return
        if (response.error) {
          stripeError.value = response.error.message || t('payment.result.failed')
          if (stripeConfirmationMayBeUnknown(response.error)) waitForBackend()
        }
        else { wechatQrUrl.value = response.paymentIntent?.next_action?.wechat_pay_display_qr_code?.image_data_url || ''; waitForBackend() }
      } else {
        showPaymentElement.value = true; await nextTick()
        if (!current()) return
        elements = stripe.elements({ clientSecret: secret, appearance: { theme: document.documentElement.classList.contains('dark') ? 'night' : 'stripe', variables: { borderRadius: '8px' } } })
        element = elements.create('payment', { layout: 'tabs', paymentMethodOrder: ['alipay', 'wechat_pay', 'card', 'link'] })
        element.mount('#stripe-payment-element')
        element.on('ready', () => { if (current()) stripeReady.value = true })
      }
    } catch (error) {
      if (!invalidated && (!ticket || current())) {
        initError.value = extractPaymentErrorMessage(error, t, t('payment.stripeLoadFailed'))
        if (submitted) waitForBackend()
      }
    } finally { if (!invalidated && (!ticket || current())) loading.value = false }
  })
  return { loading, initError, stripeError, stripeSubmitting, stripeSuccess, stripeReady, order, wechatQrUrl, redirecting, showPaymentElement, awaitingConfirmation, handleGenericPay }
}
