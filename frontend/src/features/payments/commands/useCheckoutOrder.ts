import { computed, ref, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import type { SubscriptionPlan } from '@/features/subscriptions/api/subscriptionPolicyTypes';
import type { CheckoutInfoResponse, OrderType } from '../api/types'
import { paymentAPI } from '../api/payment'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from '../queries/usePaymentRequest'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
import { isMobileDevice } from '@/shared/lib/device'
import { getPaymentPopupFeatures } from '../model/providerConfig'
import { PAYMENT_RECOVERY_STORAGE_KEY, buildCreateOrderPayload, clearPaymentRecoverySnapshot, decidePaymentLaunch,
  normalizeVisibleMethod, writePaymentRecoverySnapshot, type PaymentRecoverySnapshot } from '../model/paymentFlow'
import { invokeWechatJsapiPayment } from '../model/wechatBridge'
import { buildWechatOAuthAuthorizeUrl } from '../model/wechatAuthorizeUrl'
import { canFallbackToDesktopQr, paymentSubmissionMayBeUnknown } from '../model/paymentRetryPolicy'
import { buildPaymentErrorToastMessage, describePaymentScenarioError } from '../model/paymentUx'
interface CreateOrderOptions { openid?: string; wechatResumeToken?: string; paymentType?: string; isResume?: boolean; mobileQrFallbackAttempted?: boolean }
export function useCheckoutOrder(options: { checkout: Ref<CheckoutInfoResponse>; selectedMethod: Ref<string>; selectedPlan: Ref<SubscriptionPlan | null> }) {
const { checkout, selectedMethod, selectedPlan } = options
const services = usePaymentsServices()
const request = usePaymentRequest(() => services.scopeKey.value)
const router = useRouter()
const { t } = useI18n()
const submissionState = ref<'idle' | 'submitting' | 'unknown'>('idle')
const submitting = computed(() => submissionState.value === 'submitting')
const blocked = computed(() => submissionState.value !== 'idle')
const submissionUnknown = computed(() => submissionState.value === 'unknown')
const errorMessage = ref(''), errorHintMessage = ref('')
const paymentPhase = ref<'select' | 'paying'>('select')
function emptyPaymentState(): PaymentRecoverySnapshot {
  return {
    orderId: 0,
    amount: 0,
    qrCode: '',
    expiresAt: '',
    paymentType: '',
    payUrl: '',
    outTradeNo: '',
    clientSecret: '',
    intentId: '',
    currency: '',
    countryCode: '',
    paymentEnv: '',
    payAmount: 0,
    orderType: '',
    paymentMode: '',
    resumeToken: '',
    alipayMobilePrecreateDeepLink: false,
    createdAt: 0,
  }
}

const paymentState = ref<PaymentRecoverySnapshot>(emptyPaymentState())

function persistRecoverySnapshot(snapshot: PaymentRecoverySnapshot) {
  if (typeof window === 'undefined' || !snapshot.orderId) return
  writePaymentRecoverySnapshot(window.localStorage, snapshot, PAYMENT_RECOVERY_STORAGE_KEY)
}

function removeRecoverySnapshot() {
  if (typeof window === 'undefined') return
  try {
    const raw = window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)
    const stored: unknown = raw ? JSON.parse(raw) : null
    if (stored && typeof stored === 'object' && 'orderId' in stored && stored.orderId === paymentState.value.orderId) clearPaymentRecoverySnapshot(window.localStorage)
  } catch { /* An unrelated/malformed recovery entry is not owned by this command. */ }
}

function resetPayment() {
  removeRecoverySnapshot()
  paymentPhase.value = 'select'
  paymentState.value = emptyPaymentState()
  submissionState.value = 'idle'
}

async function redirectToPaymentResult(state: PaymentRecoverySnapshot): Promise<void> {
  const query: Record<string, string | undefined> = {}
  if (state.orderId > 0) {
    query.order_id = String(state.orderId)
  }
  if (state.outTradeNo) {
    query.out_trade_no = state.outTradeNo
  }
  if (state.resumeToken) {
    query.resume_token = state.resumeToken
  }
  await router.push({
    path: '/payment/result',
    query,
  })
}

function onPaymentDone() {
  const wasSubscription = paymentState.value.orderType === 'subscription'
  resetPayment()
  selectedPlan.value = null
  if (wasSubscription) {
    services.refreshSubscriptions(true).catch(() => {})
  }
}

async function onPaymentSuccess() {
  const completedPayment = { ...paymentState.value }
  removeRecoverySnapshot()
  void services.refreshUser().catch(() => {})
  if (paymentState.value.orderType === 'subscription') {
    services.refreshSubscriptions(true).catch(() => {})
  }
  await redirectToPaymentResult(completedPayment)
}

function onPaymentSettled() {
  removeRecoverySnapshot()
}

async function createOrder(orderAmount: number, orderType: OrderType, planId?: number, options: CreateOrderOptions = {}) {
  if (submissionState.value !== 'idle') return
  const ticket = request.begin()
  if (!ticket.isCurrent()) return
  submissionState.value = 'submitting'
  errorMessage.value = ''
  errorHintMessage.value = ''
  const requestType = normalizeVisibleMethod(options.paymentType || selectedMethod.value) || options.paymentType || selectedMethod.value
  try {
    const payload = buildCreateOrderPayload({
      amount: orderAmount,
      paymentType: requestType,
      orderType,
      planId,
      origin: typeof window !== 'undefined' ? window.location.origin : '',
      isMobile: isMobileDevice(),
      isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
      forceQRCode: !!(checkout.value.alipay_force_qrcode && normalizeVisibleMethod(requestType) === 'alipay'),
      mobilePrecreateDeepLink: checkout.value.alipay_mobile_precreate_deep_link === true,
    })
    if (options.openid) {
      payload.openid = options.openid
    }
    if (options.wechatResumeToken) {
      payload.wechat_resume_token = options.wechatResumeToken
    }

    const result = (await paymentAPI.createOrder(payload, ticket)).data
    if (!ticket.isCurrent()) return
    const openWindow = (url: string) => {
      const win = window.open(url, 'paymentPopup', getPaymentPopupFeatures())
      if (!win || win.closed) {
        window.location.href = url
      }
    }
    const visibleMethod = normalizeVisibleMethod(requestType) || requestType
    // When user clicks the dedicated Stripe button, leave method blank so the
    // landing page renders Stripe's full Payment Element (card/link/alipay/wxpay).
    const stripeMethod = visibleMethod === 'stripe'
      ? ''
      : visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
    const stripeRouteUrl = result.client_secret && visibleMethod !== 'airwallex'
      ? router.resolve({
        path: '/payment/stripe',
        query: {
          order_id: String(result.order_id),
          method: stripeMethod || undefined,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const airwallexRouteUrl = result.client_secret && result.intent_id
      ? router.resolve({
        path: '/payment/airwallex',
        query: {
          order_id: String(result.order_id),
          out_trade_no: result.out_trade_no || undefined,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const decision = decidePaymentLaunch(result, {
      visibleMethod,
      orderType,
      isMobile: isMobileDevice(),
      isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
      forceQRCode: !!(checkout.value.alipay_force_qrcode && visibleMethod === 'alipay'),
      mobilePrecreateDeepLink: checkout.value.alipay_mobile_precreate_deep_link === true,
      stripePopupUrl: stripeRouteUrl,
      stripeRouteUrl,
      airwallexRouteUrl,
    })

    if (decision.kind === 'wechat_oauth' && decision.oauth?.authorize_url) {
      window.location.href = buildWechatOAuthAuthorizeUrl(decision.oauth.authorize_url, {
        paymentType: visibleMethod,
        orderType,
        planId,
        orderAmount,
      })
      return
    }

    if (decision.kind === 'unhandled') {
      submissionState.value = 'unknown'
      applyScenarioError({ reason: 'UNHANDLED_PAYMENT_SCENARIO' }, visibleMethod)
      return
    }

    paymentState.value = decision.paymentState
    paymentPhase.value = 'paying'
    persistRecoverySnapshot(decision.recovery)

    if (decision.kind === 'stripe_popup') {
      openWindow(decision.paymentState.payUrl)
      return
    }
    if (decision.kind === 'stripe_route') {
      window.location.href = decision.paymentState.payUrl
      return
    }
    if (decision.kind === 'airwallex_route') {
      window.location.href = decision.paymentState.payUrl
      return
    }
    if (decision.kind === 'wechat_jsapi' && decision.jsapi) {
      try {
        const jsapiResult = await invokeWechatJsapiPayment({ ...decision.jsapi }, ticket.signal)
        if (!ticket.isCurrent()) return
        const errMsg = String(jsapiResult.err_msg || '').toLowerCase()
        if (errMsg.includes('cancel')) {
          services.notifyInfo(t('payment.qr.cancelled'))
          resetPayment()
        } else if (errMsg && !errMsg.includes('ok')) {
          const fallbackApplied = await attemptMobileQrFallback(
            { reason: 'WECHAT_JSAPI_FAILED', message: errMsg },
            {
              orderAmount,
              orderType,
              planId,
              paymentType: visibleMethod,
              attempted: options.mobileQrFallbackAttempted === true,
            }, ticket)
          if (!fallbackApplied) {
            applyScenarioError({ reason: 'WECHAT_JSAPI_FAILED', message: errMsg }, visibleMethod)
          }
        } else {
          const resultState = { ...decision.paymentState }
          resetPayment()
          await redirectToPaymentResult(resultState)
        }
      } catch (err: unknown) {
        if (!ticket.isCurrent()) return
        // The known order remains available for read-only confirmation.

        const fallbackApplied = await attemptMobileQrFallback(err, {
          orderAmount,
          orderType,
          planId,
          paymentType: visibleMethod,
          attempted: options.mobileQrFallbackAttempted === true,
        }, ticket)
        if (!fallbackApplied) {
          throw err
        }
      }
      return
    }
    if (decision.kind === 'redirect_waiting' && decision.paymentState.payUrl) {
      if (isMobileDevice()) {
        window.location.href = decision.paymentState.payUrl
        return
      }
      openWindow(decision.paymentState.payUrl)
    }
  } catch (err: unknown) {
    if (!ticket.isCurrent()) return
    if (paymentSubmissionMayBeUnknown(err)) submissionState.value = 'unknown'
    const apiErr = typeof err === 'object' && err !== null ? err : {}
    if (('reason' in apiErr ? apiErr.reason : undefined) === 'TOO_MANY_PENDING') {
      const metadata = 'metadata' in apiErr && typeof apiErr.metadata === 'object' && apiErr.metadata !== null ? apiErr.metadata : undefined
      errorMessage.value = t('payment.errors.tooManyPending', { max: metadata && 'max' in metadata ? metadata.max : '' })
      errorHintMessage.value = ''
    } else if (('reason' in apiErr ? apiErr.reason : undefined) === 'CANCEL_RATE_LIMITED') {
      errorMessage.value = t('payment.errors.cancelRateLimited')
      errorHintMessage.value = ''
    } else if (await attemptMobileQrFallback(err, {
      orderAmount,
      orderType,
      planId,
      paymentType: requestType,
      attempted: options.mobileQrFallbackAttempted === true,
    }, ticket)) {
      return
    } else {
      const handled = applyScenarioError(
        err,
        normalizeVisibleMethod(options.paymentType || selectedMethod.value) || selectedMethod.value,
      )
      if (!handled) {
        errorMessage.value = extractPaymentErrorMessage(err, t, extractApiErrorMessage(err, t('payment.result.failed')))
        errorHintMessage.value = ''
      }
      if (handled) {
        return
      }
    }
    services.notifyError(buildPaymentErrorToastMessage(errorMessage.value, errorHintMessage.value))
  } finally {
    if (ticket.isCurrent() && submissionState.value !== 'unknown') submissionState.value = 'idle'
  }
}

interface MobileQrFallbackContext {
  orderAmount: number
  orderType: OrderType
  planId?: number
  paymentType: string
  attempted: boolean
}

async function attemptMobileQrFallback(err: unknown, context: MobileQrFallbackContext, ticket: ReturnType<typeof request.begin>): Promise<boolean> {
  if (!canFallbackToDesktopQr(err, context.paymentType, isMobileDevice(), context.attempted)) {
    return false
  }

  try {
    const visibleMethod = normalizeVisibleMethod(context.paymentType) || context.paymentType
    const payload = buildCreateOrderPayload({
      amount: context.orderAmount,
      paymentType: visibleMethod,
      orderType: context.orderType,
      planId: context.planId,
      origin: typeof window !== 'undefined' ? window.location.origin : '',
      isMobile: false,
      isWechatBrowser: false,
    })
    const result = (await paymentAPI.createOrder(payload, ticket)).data
    if (!ticket.isCurrent()) throw new DOMException('Payment context changed', 'AbortError')
    const stripeMethod = visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
    const stripeRouteUrl = result.client_secret
      ? router.resolve({
        path: '/payment/stripe',
        query: {
          order_id: String(result.order_id),
          method: stripeMethod,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const decision = decidePaymentLaunch(result, {
      visibleMethod,
      orderType: context.orderType,
      isMobile: false,
      isWechatBrowser: false,
      stripePopupUrl: stripeRouteUrl,
      stripeRouteUrl,
    })

    if (decision.kind !== 'qr_waiting' || !decision.paymentState.qrCode) {
      submissionState.value = 'unknown'
      return false
    }

    errorMessage.value = ''
    errorHintMessage.value = ''
    paymentState.value = decision.paymentState
    paymentPhase.value = 'paying'
    persistRecoverySnapshot(decision.recovery)
    services.notifyWarning(t('payment.errors.mobilePaymentFallbackToQr'))
    return true
  } catch (error) {
    if (!ticket.isCurrent()) throw new DOMException('Payment context changed', 'AbortError')
    if (paymentSubmissionMayBeUnknown(error)) submissionState.value = 'unknown'
    return false
  }
}

function applyScenarioError(err: unknown, paymentMethod: string): boolean {
  const descriptor = describePaymentScenarioError(err, {
    paymentMethod,
    isMobile: isMobileDevice(),
    isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
  })
  if (!descriptor) {
    errorMessage.value = ''
    errorHintMessage.value = ''
    return false
  }
  errorMessage.value = t(descriptor.messageKey)
  errorHintMessage.value = descriptor.hintKey ? t(descriptor.hintKey) : ''
  services.notifyError(buildPaymentErrorToastMessage(errorMessage.value, errorHintMessage.value))
  return true
}


function restore(snapshot: PaymentRecoverySnapshot): void { paymentState.value = snapshot; paymentPhase.value = 'paying' }
watch(services.scopeKey, () => { request.cancel(); resetPayment(); selectedPlan.value = null; errorMessage.value = ''; errorHintMessage.value = '' }, { flush: 'sync' })
return { submitting, blocked, submissionUnknown, errorMessage, errorHintMessage, paymentPhase, paymentState,
  createOrder, restore, resetPayment, removeRecoverySnapshot, onPaymentDone, onPaymentSuccess, onPaymentSettled }
}
