<template>
  <div v-if="contextValid" class="space-y-4">
    <p v-if="cancellationUnknown && outcome === null" role="status" class="text-sm text-amber-700 dark:text-amber-300">
      {{ t('payment.result.processing') }} — {{ t('payment.result.processingHint') }}
    </p>
    <!-- ═══ Terminal States: show result, user clicks to return ═══ -->

    <!-- Success -->
    <template v-if="outcome === 'success'">
      <div class="card p-6">
        <div class="flex flex-col items-center space-y-4 py-4">
          <div class="flex h-16 w-16 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30">
            <Icon name="check" size="lg" class="text-green-500" />
          </div>
          <p class="text-lg font-bold text-gray-900 dark:text-white">{{ props.orderType === 'subscription' ? t('payment.result.subscriptionSuccess') : t('payment.result.success') }}</p>
          <div v-if="paidOrder" class="w-full rounded-xl bg-gray-50 p-4 dark:bg-dark-800">
            <div class="space-y-2 text-sm">
              <div class="flex justify-between">
                <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderId') }}</span>
                <span class="font-medium text-gray-900 dark:text-white">#{{ paidOrder.id }}</span>
              </div>
              <div v-if="paidOrder.out_trade_no" class="flex justify-between">
                <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderNo') }}</span>
                <span class="font-medium text-gray-900 dark:text-white">{{ paidOrder.out_trade_no }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.amount') }}</span>
                <span class="font-medium text-gray-900 dark:text-white">
                  {{ creditedAmountSymbol }}{{ paidOrder.amount.toFixed(2) }}
                  <span v-if="(paidOrder.bonus_amount ?? 0) > 0" class="ml-1 text-xs font-normal text-amber-600 dark:text-amber-400">
                    ({{ t('payment.orders.bonusIncluded', { amount: creditedAmountSymbol + (paidOrder.bonus_amount ?? 0).toFixed(2) }) }})
                  </span>
                </span>
              </div>
              <div class="flex justify-between">
                <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.payAmount') }}</span>
                <span class="font-medium text-gray-900 dark:text-white">{{ formatGatewayAmount(paidOrder.pay_amount, paidOrder.currency) }}</span>
              </div>
            </div>
          </div>
          <button class="btn btn-primary" @click="handleDone">{{ t('common.confirm') }}</button>
        </div>
      </div>
    </template>

    <!-- Cancelled -->
    <template v-else-if="outcome === 'cancelled'">
      <div class="card p-6">
        <div class="flex flex-col items-center space-y-4 py-4">
          <div class="flex h-16 w-16 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700">
            <svg class="h-8 w-8 text-gray-400 dark:text-gray-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </div>
          <p class="text-lg font-bold text-gray-900 dark:text-white">{{ t('payment.qr.cancelled') }}</p>
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.cancelledDesc') }}</p>
          <button class="btn btn-primary" @click="handleDone">{{ t('common.confirm') }}</button>
        </div>
      </div>
    </template>

    <!-- Expired / Failed -->
    <template v-else-if="outcome === 'expired'">
      <div class="card p-6">
        <div class="flex flex-col items-center space-y-4 py-4">
          <div class="flex h-16 w-16 items-center justify-center rounded-full bg-orange-100 dark:bg-orange-900/30">
            <svg class="h-8 w-8 text-orange-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </div>
          <p class="text-lg font-bold text-gray-900 dark:text-white">{{ t('payment.qr.expired') }}</p>
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.expiredDesc') }}</p>
          <button class="btn btn-primary" @click="handleDone">{{ t('common.confirm') }}</button>
        </div>
      </div>
    </template>

    <!-- ═══ Active States: QR or Popup waiting ═══ -->

    <!-- Mobile Alipay app handoff. The QR fallback stays hidden until launch timeout. -->
    <template v-else-if="isMobileAlipayDeepLink">
      <template v-if="!deepLinkFallbackVisible">
        <div class="card p-6">
          <div class="flex flex-col items-center space-y-4 py-4 text-center">
            <div
              v-if="deepLinkState === 'launching'"
              class="h-10 w-10 animate-spin rounded-full border-4 border-[#00AEEF] border-t-transparent"
            ></div>
            <div
              v-else
              class="flex h-12 w-12 items-center justify-center rounded-full bg-blue-50 dark:bg-blue-950/30"
            >
              <Icon name="checkCircle" size="lg" class="text-[#00AEEF]" />
            </div>
            <p class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ deepLinkState === 'backgrounded' ? t('payment.qr.alipayContinueInApp') : t('payment.qr.alipayOpening') }}
            </p>
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.alipayWaitingHint') }}</p>
            <button
              v-if="deepLinkState === 'backgrounded'"
              data-test="reopen-alipay"
              class="btn btn-alipay inline-flex items-center gap-2 text-sm"
              @click="reopenAlipay"
            >
              <Icon name="externalLink" size="sm" />
              {{ t('payment.qr.reopenAlipay') }}
            </button>
          </div>
        </div>
        <div class="card p-4 text-center">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.expiresIn') }}</p>
          <p class="mt-1 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{{ countdownDisplay }}</p>
          <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">{{ t('payment.qr.waitingPayment') }}</p>
        </div>
      </template>
      <template v-else>
        <div data-test="alipay-qr-fallback" class="card p-6">
          <div class="flex flex-col items-center space-y-4">
            <div class="text-center">
              <p class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('payment.qr.alipayFallbackTitle') }}</p>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.alipayFallbackHint') }}</p>
            </div>
            <div class="w-full space-y-2 border-y border-gray-100 py-3 text-sm dark:border-dark-600">
              <div class="flex items-start justify-between gap-4">
                <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.payAmount') }}</span>
                <span class="font-semibold text-gray-900 dark:text-white">{{ displayPaymentAmount }}</span>
              </div>
              <div class="flex items-start justify-between gap-4">
                <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderNo') }}</span>
                <span class="max-w-[70%] break-all text-right font-mono text-xs text-gray-900 dark:text-white">
                  {{ displayOrderNumber }}
                </span>
              </div>
              <div class="flex items-start justify-between gap-4">
                <span class="text-gray-500 dark:text-gray-400">{{ t('payment.qr.expiresIn') }}</span>
                <span class="font-semibold tabular-nums text-gray-900 dark:text-white">{{ countdownDisplay }}</span>
              </div>
            </div>
            <div :class="['relative rounded-lg border-2 p-4', qrBorderClass]">
              <canvas ref="qrCanvas" class="mx-auto"></canvas>
              <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
                <span :class="['rounded-full p-2 shadow ring-2 ring-white', qrLogoBgClass]">
                  <img :src="qrLogoIcon" alt="" class="h-5 w-5 brightness-0 invert" />
                </span>
              </div>
            </div>
            <p class="text-center text-sm leading-6 text-gray-600 dark:text-gray-300">
              {{ t('payment.qr.alipaySaveAndScanHint') }}
            </p>
            <div class="grid w-full gap-2 sm:grid-cols-2">
              <button
                data-test="reopen-alipay"
                class="btn btn-alipay inline-flex items-center justify-center gap-2"
                @click="reopenAlipay"
              >
                <Icon name="externalLink" size="sm" />
                {{ t('payment.qr.reopenAlipay') }}
              </button>
              <button
                data-test="save-alipay-qr"
                class="btn btn-secondary inline-flex items-center justify-center gap-2"
                @click="saveQRCode"
              >
                <Icon name="download" size="sm" />
                {{ t('payment.qr.saveQRCode') }}
              </button>
            </div>
            <button class="btn btn-secondary w-full" @click="handleDone">
              {{ t('payment.result.backToRecharge') }}
            </button>
          </div>
        </div>
      </template>
    </template>

    <!-- QR Code Mode -->
    <template v-else-if="showQRCode">
      <div class="card p-6">
        <div class="flex flex-col items-center space-y-4">
          <p class="text-lg font-semibold text-gray-900 dark:text-white">{{ scanTitle }}</p>
          <div :class="['relative rounded-lg border-2 p-4', qrBorderClass]">
            <canvas ref="qrCanvas" class="mx-auto"></canvas>
            <!-- Brand logo overlay -->
            <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
              <span :class="['rounded-full p-2 shadow ring-2 ring-white', qrLogoBgClass]">
                <img :src="qrLogoIcon" alt="" class="h-5 w-5 brightness-0 invert" />
              </span>
            </div>
          </div>
          <p v-if="scanHint" class="text-center text-sm text-gray-500 dark:text-gray-400">{{ scanHint }}</p>
          <button v-if="payUrl" class="btn btn-secondary text-sm" @click="reopenPopup">
            {{ t('payment.qr.openPayWindow') }}
          </button>
        </div>
      </div>
      <div class="card p-4 text-center">
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.expiresIn') }}</p>
        <p class="mt-1 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{{ countdownDisplay }}</p>
        <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">{{ t('payment.qr.waitingPayment') }}</p>
      </div>
      <button class="btn btn-secondary w-full" :disabled="cancelBlocked" @click="handleCancel">
        {{ cancelling ? t('common.processing') : t('payment.qr.cancelOrder') }}
      </button>
    </template>

    <!-- Waiting for Popup/Redirect Mode -->
    <template v-else>
      <div class="card p-6">
        <div class="flex flex-col items-center space-y-4 py-4">
          <div class="h-10 w-10 animate-spin rounded-full border-4 border-primary-500 border-t-transparent"></div>
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.payInNewWindowHint') }}</p>
          <button v-if="payUrl" class="btn btn-secondary text-sm" @click="reopenPopup">
            {{ t('payment.qr.openPayWindow') }}
          </button>
        </div>
      </div>
      <div class="card p-4 text-center">
        <p class="mt-1 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{{ countdownDisplay }}</p>
        <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">{{ t('payment.qr.waitingPayment') }}</p>
      </div>
      <button class="btn btn-secondary w-full" :disabled="cancelBlocked" @click="handleCancel">
        {{ cancelling ? t('common.processing') : t('payment.qr.cancelOrder') }}
      </button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useOrderMonitor } from '../queries/useOrderMonitor'
import { usePaymentRequest } from '../queries/usePaymentRequest'
import { paymentSubmissionMayBeUnknown } from '../model/paymentRetryPolicy'
import { usePaymentsServices } from '../services'
import { paymentAPI } from '@/features/payments/api/payment'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
import { getPaymentPopupFeatures, isBuiltInAlipayMethod, isBuiltInWxpayMethod } from '@/features/payments/model/providerConfig'
import { currencySymbol, formatPaymentAmount, normalizePaymentCurrency } from '@/features/payments/model/currency'
import type { PaymentOrder } from '@/features/payments/api/types'
import Icon from '@/shared/ui/icons/Icon.vue'
import QRCode from 'qrcode'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'
import paymentIcon from '@/assets/icons/payment.svg'
import {
  createAlipayDeepLinkLauncher,
  type AlipayDeepLinkLauncher,
  type AlipayDeepLinkState,
} from '../model/alipayDeepLink'

const props = defineProps<{
  orderId: number
  amount?: number
  payAmount?: number
  qrCode: string
  expiresAt: string
  paymentType: string
  payUrl?: string
  orderType?: string
  currency?: string
  outTradeNo?: string
  mobileAlipayDeepLink?: boolean
}>()

type PaymentOutcome = 'success' | 'cancelled' | 'expired'

const emit = defineEmits<{ done: []; success: []; settled: [outcome: PaymentOutcome] }>()

const i18n = useI18n()
const { t } = i18n
const services = usePaymentsServices()
const initialScope = services.scopeKey.value, initialOrderId = props.orderId
const contextValid = computed(() => initialScope !== null && initialScope === services.scopeKey.value && initialOrderId === props.orderId)
const command = usePaymentRequest(() => services.scopeKey.value ? services.scopeKey.value + '|order:' + props.orderId : null)

const qrCanvas = ref<HTMLCanvasElement | null>(null)
const qrUrl = ref('')
const remainingSeconds = ref(0)
const cancelling = ref(false)
const cancellationUnknown = ref(false)
const cancelBlocked = computed(() => cancelling.value || cancellationUnknown.value)
const paidOrder = ref<PaymentOrder | null>(null)
const deepLinkState = ref<AlipayDeepLinkState>('idle')
const deepLinkFallbackVisible = ref(false)
const paymentCurrency = computed(() => normalizePaymentCurrency(props.currency))
const creditedAmountSymbol = currencySymbol('USD')
const localeCode = computed(() => {
  const raw = i18n.locale as unknown
  if (typeof raw === 'string') return raw
  if (raw && typeof raw === 'object' && 'value' in raw) {
    return String((raw as { value?: string }).value || '')
  }
  return undefined
})

// Terminal outcome: null = still active, 'success' | 'cancelled' | 'expired'
const outcome = ref<PaymentOutcome | null>(null)

let countdownTimer: ReturnType<typeof setInterval> | null = null
let alipayLauncher: AlipayDeepLinkLauncher | null = null


const isAlipay = computed(() => isBuiltInAlipayMethod(props.paymentType))
const isWxpay = computed(() => isBuiltInWxpayMethod(props.paymentType))
const isMobileAlipayDeepLink = computed(() => props.mobileAlipayDeepLink === true && isAlipay.value && !!qrUrl.value)
const showQRCode = computed(() => !!qrUrl.value && (!isMobileAlipayDeepLink.value || deepLinkFallbackVisible.value))

const qrBorderClass = computed(() => {
  if (isAlipay.value) return 'border-[#00AEEF] bg-blue-50 dark:border-[#00AEEF]/70 dark:bg-blue-950/20'
  if (isWxpay.value) return 'border-[#2BB741] bg-green-50 dark:border-[#2BB741]/70 dark:bg-green-950/20'
  return 'border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-800'
})

const qrLogoBgClass = computed(() => {
  if (isAlipay.value) return 'bg-[#00AEEF]'
  if (isWxpay.value) return 'bg-[#2BB741]'
  return 'bg-gray-400'
})

const qrLogoIcon = computed(() => {
  if (isAlipay.value) return alipayIcon
  if (isWxpay.value) return wxpayIcon
  return paymentIcon
})

const scanTitle = computed(() => {
  if (isAlipay.value) return t('payment.qr.scanAlipay')
  if (isWxpay.value) return t('payment.qr.scanWxpay')
  return t('payment.qr.scanToPay')
})

const scanHint = computed(() => {
  if (isAlipay.value) return t('payment.qr.scanAlipayHint')
  if (isWxpay.value) return t('payment.qr.scanWxpayHint')
  return ''
})

const countdownDisplay = computed(() => {
  const m = Math.floor(remainingSeconds.value / 60)
  const s = remainingSeconds.value % 60
  return m.toString().padStart(2, '0') + ':' + s.toString().padStart(2, '0')
})

const displayPaymentAmount = computed(() => formatGatewayAmount(props.payAmount || props.amount || 0))
const displayOrderNumber = computed(() => props.outTradeNo || `#${props.orderId}`)

function formatGatewayAmount(value: number, currency?: string | null): string {
  return formatPaymentAmount(value, currency || paymentCurrency.value, localeCode.value)
}

function isSuccessStatus(status: string | null | undefined): boolean {
  return status === 'COMPLETED' || status === 'PAID' || status === 'RECHARGING'
}

function reopenPopup() {
  if (props.payUrl) {
    const win = window.open(props.payUrl, 'paymentPopup', getPaymentPopupFeatures())
    if (!win || win.closed) {
      window.location.href = props.payUrl
    }
  }
}

function setOutcome(next: PaymentOutcome): void {
  if (outcome.value !== null) return
  // A terminal observation owns the result; older commands must not replace it.
  command.cancel()
  cancelling.value = false
  cancellationUnknown.value = false
  outcome.value = next
  emit('settled', next)
}

async function renderQR() {
  await nextTick()
  if (!showQRCode.value || !qrCanvas.value || !qrUrl.value) return
  await QRCode.toCanvas(qrCanvas.value, qrUrl.value, {
    width: 220, margin: 2,
    errorCorrectionLevel: 'M',
  })
}

function updateDeepLinkState(state: AlipayDeepLinkState) {
  deepLinkState.value = state
  if (state === 'fallback') {
    deepLinkFallbackVisible.value = true
    renderQR()
  } else if (state === 'backgrounded') {
    deepLinkFallbackVisible.value = false
  }
}

function reopenAlipay() {
  alipayLauncher?.launch()
}

function saveQRCode() {
  const canvas = qrCanvas.value
  if (!canvas) return
  const link = document.createElement('a')
  link.href = canvas.toDataURL('image/png')
  link.download = `alipay-${props.outTradeNo || props.orderId}.png`
  document.body.appendChild(link)
  link.click()
  link.remove()
}

const monitor = useOrderMonitor({
  orderId: () => props.orderId,
  recoverPending: () => isWxpay.value || isAlipay.value,
  isTerminal: () => outcome.value !== null,
  onOrder: order => {
    if (isSuccessStatus(order.status)) { cleanup(); paidOrder.value = order; setOutcome('success'); emit('success') }
    else if (order.status === 'CANCELLED') { cleanup(); setOutcome('cancelled') }
    else if (order.status === 'EXPIRED' || order.status === 'FAILED') { cleanup(); setOutcome('expired') }
  },
  onInvalidate: () => { cleanup(); paidOrder.value = null; qrUrl.value = ''; outcome.value = null; cancelling.value = false; cancellationUnknown.value = false; remainingSeconds.value = 0 }
})

function startCountdown(seconds: number) {
  remainingSeconds.value = Math.max(0, seconds)
  const deadline = Date.now() + remainingSeconds.value * 1000
  if (remainingSeconds.value <= 0) { setOutcome('expired'); return }
  countdownTimer = setInterval(() => {
    remainingSeconds.value = Math.max(0, Math.floor((deadline - Date.now()) / 1000))
    if (remainingSeconds.value <= 0) { setOutcome('expired'); cleanup() }
  }, 1000)
}

async function handleCancel(): Promise<void> {
  if (!props.orderId || !contextValid.value || cancelBlocked.value || outcome.value !== null) return
  const ticket = command.begin()
  if (!ticket.isCurrent()) return
  const orderId = props.orderId
  cancelling.value = true
  try {
    await paymentAPI.cancelOrder(orderId, ticket)
    if (!ticket.isCurrent()) return
    cleanup()
    setOutcome('cancelled')
  } catch (err: unknown) {
    if (!ticket.isCurrent()) return
    cancellationUnknown.value = paymentSubmissionMayBeUnknown(err)
    services.notifyError(extractPaymentErrorMessage(err, t, t('common.error')))
  } finally {
    if (ticket.isCurrent()) cancelling.value = false
  }
}

function handleDone() { cleanup(); emit('done') }

function cleanup() {
  monitor.stop()
  if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
  alipayLauncher?.dispose()
  alipayLauncher = null
}

// Initialize on mount
qrUrl.value = props.qrCode
let seconds = 30 * 60
if (props.expiresAt) {
  seconds = Math.floor((new Date(props.expiresAt).getTime() - Date.now()) / 1000)
}
if (contextValid.value) { startCountdown(seconds); monitor.start(); void renderQR() }

watch([() => qrUrl.value, showQRCode], () => renderQR())
onMounted(() => {
  if (!contextValid.value || !isMobileAlipayDeepLink.value) return
  alipayLauncher = createAlipayDeepLinkLauncher({
    qrCode: qrUrl.value,
    document,
    lifecycleTarget: window,
    userAgent: window.navigator.userAgent,
    assignLocation: (url) => window.location.assign(url),
    onStateChange: updateDeepLinkState,
  })
  alipayLauncher.launch()
})
onUnmounted(() => cleanup())
</script>
