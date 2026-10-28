import { ref, onMounted, onScopeDispose, watch, unref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { isChineseLocale } from '@/i18n/localeUtils'
import { useCheckoutRecovery, type CheckoutRecoveryTarget } from '../queries/useCheckoutRecovery'
import { usePaymentsServices } from '../services'
import { normalizeOrderStatus } from '../model/orderResult'
function airwallexLocale(locale: string): 'zh' | 'zh-HK' | 'ja' | 'en' {
  if (locale.toLowerCase().startsWith('ja')) return 'ja'
  if (!isChineseLocale(locale)) return 'en'
  return locale.toLowerCase().startsWith('zh-tw') ? 'zh-HK' : 'zh'
}
export function useAirwallexCheckout(target: () => CheckoutRecoveryTarget) {
  const { t, locale } = useI18n(), router = useRouter(), services = usePaymentsServices()
  const recovery = useCheckoutRecovery(target, true)
  const loading = ref(true), errorMessage = ref('')
  let invalidated = false
  function invalidate(): void { invalidated = true; recovery.cancel(); loading.value = false }
  watch(() => JSON.stringify(target()), invalidate, { flush: 'sync' })
  watch(services.scopeKey, () => { if (!recovery.publicCredential.value) invalidate() }, { flush: 'sync' })
  onScopeDispose(invalidate)
  onMounted(async () => {
    try {
      const restored = await recovery.load()
      if (invalidated) return
      if (!restored || restored.snapshot.paymentType !== 'airwallex' || !restored.snapshot.intentId) { errorMessage.value = t('payment.airwallexMissingParams'); return }
      const { snapshot, ticket } = restored
      const successUrl = new URL('/payment/result', window.location.origin)
      successUrl.searchParams.set('order_id', String(snapshot.orderId))
      if (snapshot.outTradeNo) successUrl.searchParams.set('out_trade_no', snapshot.outTradeNo)
      if (snapshot.resumeToken) successUrl.searchParams.set('resume_token', snapshot.resumeToken)
      // Recovery may query any order, but only a confirmed pending order may enter checkout.
      if (normalizeOrderStatus(restored.status) !== 'PENDING') { await router.push(successUrl.pathname + successUrl.search); return }
      const sdk = await import('@airwallex/components-sdk')
      if (!ticket.isCurrent() || invalidated) return
      const result = await sdk.init({ env: snapshot.paymentEnv === 'prod' ? 'prod' : 'demo', enabledElements: ['payments'], locale: airwallexLocale(unref(locale)) })
      if (!ticket.isCurrent() || invalidated) return
      if (!result.payments) throw new Error(t('payment.airwallexLoadFailed'))
      const redirect = result.payments.redirectToCheckout({ intent_id: snapshot.intentId, client_secret: snapshot.clientSecret, currency: snapshot.currency || 'CNY', country_code: snapshot.countryCode || 'CN', successUrl: successUrl.toString() })
      if (ticket.isCurrent() && !invalidated && typeof redirect === 'string' && redirect) window.location.assign(redirect)
    } catch (error) { if (!invalidated) errorMessage.value = error instanceof Error && error.message ? error.message : t('payment.airwallexLoadFailed') }
    finally { if (!invalidated) loading.value = false }
  })
  return { loading, errorMessage }
}
