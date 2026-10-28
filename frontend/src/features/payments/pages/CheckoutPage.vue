<script setup lang="ts">
import { ref, computed, onMounted, onScopeDispose, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import '@/styles/announcement-markdown.css'
import type { SubscriptionPlan } from '@/features/subscriptions/api/subscriptionPolicyTypes';
import type { PeakRateFields } from '@/features/pricing'
import { platformAccentBarClass, platformBadgeLightClass } from '@/shared/styles/platformColors'
const { platformLabel: platformLabel } = usePaymentsServices()
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
import { formatRechargeBonusNumber } from '@/features/payments/model/rechargeBonus'
import AmountInput from '../components/AmountInput.vue'
import PaymentMethodSelector from '../components/PaymentMethodSelector.vue'
import SubscriptionPlanCard from '../components/SubscriptionPlanCard.vue'
import PaymentStatusPanel from '../components/PaymentStatusPanel.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { paymentAPI } from '../api/payment'
import { usePaymentsServices } from '../services'
import { useCheckoutData } from '../queries/useCheckoutData'
import { useCheckoutOrder } from '../commands/useCheckoutOrder'
import { useCheckoutQuote } from '../model/useCheckoutQuote'
import { METHOD_ORDER } from '../model/providerConfig'
import { PAYMENT_RECOVERY_STORAGE_KEY, normalizeVisibleMethod, readPaymentRecoverySnapshot, clearPaymentRecoverySnapshot } from '../model/paymentFlow'
import { hasWechatResumeQuery, parseWechatResumeRoute, stripWechatResumeQuery } from '../model/paymentWechatResume'
import { planValiditySuffix as validitySuffixOf } from '../model/validity'
const { t } = useI18n()
const route = useRoute(), router = useRouter()
const services = usePaymentsServices()
const user = services.user
const activeSubscriptions = services.activeSubscriptions
const data = useCheckoutData()
const checkout = data.checkout
const activeTab = ref<'recharge' | 'subscription'>('recharge'), amount = ref<number | null>(null), selectedMethod = ref('')
const selectedPlan = ref<SubscriptionPlan | null>(null), previewImage = ref('')
const initializing = ref(true)
const flow = useCheckoutOrder({ checkout, selectedMethod, selectedPlan })
const { submitting, submissionUnknown, errorMessage, paymentPhase, paymentState,
  onPaymentDone, onPaymentSuccess, onPaymentSettled } = flow
const loading = computed(() => data.loading.value || initializing.value)
const blocked = computed(() => flow.blocked.value || initializing.value)
const { renderedHelpText, renderedBonusNotice, subscriptionEnabled, tabs, enabledMethods, validAmount, balanceRechargeMultiplier, rechargeBonusTiers, rechargeBonusMode, planGridClass, globalMinAmount, globalMaxAmount, selectedCurrency, formatSelectedPaymentAmount, formatSelectedSubscriptionPaymentAmount, bonusQuote, discountAmount, creditedAmount, showBonusRow, showCreditedBalance, methodOptions, feeRate, feeAmount, totalAmount, showActualPay, amountError, canSubmit, subPaymentAmount, subFeeAmount, subTotalAmount, subMethodOptions, canSubmitSubscription, paymentButtonClass, planBadgeClass, planTextClass } = useCheckoutQuote({ checkout, activeTab, amount, selectedMethod, selectedPlan })
function getDaysRemaining(expiresAt: string | null): number {
  const diff = new Date(expiresAt || 0).getTime() - Date.now()
  return Math.max(0, Math.ceil(diff / (1000 * 60 * 60 * 24)))
}

function subscriptionHasPeakRate(sub: { group?: PeakRateFields | null }): boolean {
  return services.peakRate.hasPeakRate(sub.group)
}

function subscriptionPeakRateLabel(sub: { group?: PeakRateFields | null }): string {
  return services.peakRate.formatPeakRateWindow(sub.group, services.publicSettings.value?.server_utc_offset)
}

const showRenewalModal = ref(false)
const renewGroupId = ref<number | null>(null)
const renewalPlans = computed(() => {
  if (renewGroupId.value == null) return []
  return checkout.value.plans.filter(p => p.group_id === renewGroupId.value)
})

const planValiditySuffix = computed(() => {
  if (!selectedPlan.value) return ''
  return validitySuffixOf(selectedPlan.value, t)
})

function planHasPeakRate(plan: SubscriptionPlan): boolean {
  return services.peakRate.hasPeakRate(plan)
}

function planPeakRateLabel(plan: SubscriptionPlan): string {
  return services.peakRate.formatPeakRateWindow(plan, services.publicSettings.value?.server_utc_offset)
}

function selectPlan(plan: SubscriptionPlan) {
  selectedPlan.value = plan
  errorMessage.value = ''
}

function selectPlanFromModal(plan: SubscriptionPlan) {
  showRenewalModal.value = false
  renewGroupId.value = null
  selectedPlan.value = plan
  errorMessage.value = ''
}

function closeRenewalModal() {
  showRenewalModal.value = false
  renewGroupId.value = null
}

async function handleSubmitRecharge() {
  if (!canSubmit.value || blocked.value) return
  await flow.createOrder(validAmount.value, 'balance')
}

async function confirmSubscribe() {
  if (!selectedPlan.value || blocked.value) return
  await flow.createOrder(selectedPlan.value.price, 'subscription', selectedPlan.value.id)
}

async function resumeWechatPaymentFromQuery(ticket: { isCurrent: () => boolean }) {
  if (!ticket.isCurrent()) return
  const resume = parseWechatResumeRoute(route.query, checkout.value.plans, validAmount.value)
  if (!resume) {
    return
  }

  selectedMethod.value = resume.paymentType
  if (resume.orderType === 'balance' && resume.orderAmount > 0) {
    amount.value = resume.orderAmount
  }
  if (resume.orderType === 'subscription' && resume.planId) {
    selectedPlan.value = checkout.value.plans.find(plan => plan.id === resume.planId) ?? null
  }

  await router.replace({ path: route.path, query: stripWechatResumeQuery(route.query) })

  if (!ticket.isCurrent()) return
  if (resume.wechatResumeToken) {
    await flow.createOrder(0, resume.orderType, resume.planId, {
      wechatResumeToken: resume.wechatResumeToken,
      paymentType: resume.paymentType,
      isResume: true,
    })
    return
  }

  if (resume.orderAmount > 0 && resume.openid) {
    await flow.createOrder(resume.orderAmount, resume.orderType, resume.planId, {
      openid: resume.openid,
      paymentType: resume.paymentType,
      isResume: true,
    })
  }
}


let initialization = 0
async function initialize(): Promise<void> {
  const version = ++initialization, scope = services.scopeKey.value
  if (scope === null) { initializing.value = false; return }
  initializing.value = true
  try {
    const loaded = await data.load()
    if (!loaded || !loaded.ticket.isCurrent() || version !== initialization) return
    const { ticket } = loaded
    const order: readonly string[] = METHOD_ORDER
    selectedMethod.value = [...enabledMethods.value].sort((a, b) => {
      const ai = order.indexOf(a), bi = order.indexOf(b)
      return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi)
    })[0] ?? ''
    if (hasWechatResumeQuery(route.query)) clearPaymentRecoverySnapshot(window.localStorage)
    const token = typeof route.query.resume_token === 'string' ? route.query.resume_token
      : typeof route.query.wechat_resume_token === 'string' ? route.query.wechat_resume_token : undefined
    const raw = window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)
    const restored = readPaymentRecoverySnapshot(raw, { resumeToken: token })
    if (restored) {
      // A private checkout never trusts a previous login's browser snapshot.
      try {
        const verified = await paymentAPI.getOrder(restored.orderId, ticket)
        if (!ticket.isCurrent() || version !== initialization) return
        if (verified.data.user_id === services.user.value?.id) {
          flow.restore(restored)
          selectedMethod.value = normalizeVisibleMethod(restored.paymentType) || restored.paymentType || selectedMethod.value
        } else if (window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY) === raw) clearPaymentRecoverySnapshot(window.localStorage)
      } catch {
        if (!ticket.isCurrent()) return
        if (window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY) === raw) clearPaymentRecoverySnapshot(window.localStorage)
      }
    } else if (raw && window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY) === raw) clearPaymentRecoverySnapshot(window.localStorage)
    await resumeWechatPaymentFromQuery(ticket)
    if (!ticket.isCurrent() || version !== initialization) return
    if (route.query.tab === 'subscription' && subscriptionEnabled.value) {
      activeTab.value = 'subscription'
      if (route.query.group) {
        const groupPlans = checkout.value.plans.filter(plan => plan.group_id === Number(route.query.group))
        if (groupPlans.length === 1) selectedPlan.value = groupPlans[0]
        else if (groupPlans.length > 1) { renewGroupId.value = Number(route.query.group); showRenewalModal.value = true }
      }
    }
    if (subscriptionEnabled.value) void services.refreshSubscriptions().catch(() => {})
  } catch (error) {
    if (version === initialization && scope === services.scopeKey.value) services.notifyError(extractPaymentErrorMessage(error, t, t('common.error')))
  } finally { if (version === initialization && scope === services.scopeKey.value) initializing.value = false }
}
onMounted(() => { void initialize() })
watch(services.scopeKey, () => { selectedMethod.value = ''; amount.value = null; previewImage.value = ''; showRenewalModal.value = false; void initialize() }, { flush: 'sync' })
onScopeDispose(() => { initialization++ })
</script>
<template>
  <div v-if="submissionUnknown" role="status" class="mb-4 rounded-lg border border-amber-300 p-4 text-sm">{{ t('payment.result.processing') }} <router-link to="/orders" class="ml-2 underline">{{ t('nav.myOrders') }}</router-link></div>
    <div class="mx-auto max-w-4xl space-y-6">
      <div v-if="loading" class="flex items-center justify-center py-20">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary-500 border-t-transparent"></div>
      </div>
      <template v-else>
        <!-- Tab Switcher (hide during payment and subscription confirm) -->
        <div v-if="tabs.length > 1 && paymentPhase === 'select' && !selectedPlan" class="flex space-x-1 rounded-xl bg-gray-100 p-1 dark:bg-dark-800">
          <button v-for="tab in tabs" :key="tab.key"
            class="flex-1 rounded-lg px-4 py-2.5 text-sm font-medium transition-all"
            :class="activeTab === tab.key ? 'bg-white text-gray-900 shadow dark:bg-dark-700 dark:text-white' : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300'"
            @click="activeTab = tab.key">{{ tab.label }}</button>
        </div>
        <!-- Payment in progress (shared by recharge and subscription) -->
        <template v-if="paymentPhase === 'paying'">
          <PaymentStatusPanel
            :order-id="paymentState.orderId"
            :amount="paymentState.amount"
            :pay-amount="paymentState.payAmount"
            :qr-code="paymentState.qrCode"
            :expires-at="paymentState.expiresAt"
            :payment-type="paymentState.paymentType"
            :pay-url="paymentState.payUrl"
            :order-type="paymentState.orderType"
            :currency="paymentState.currency || selectedCurrency"
            :out-trade-no="paymentState.outTradeNo"
            :mobile-alipay-deep-link="paymentState.alipayMobilePrecreateDeepLink"
            @done="onPaymentDone"
            @success="onPaymentSuccess"
            @settled="onPaymentSettled"
          />
        </template>
        <!-- Tab content (select phase) -->
        <template v-else>
          <!-- Neither top-up nor subscriptions available (balance recharge disabled via API while subscriptions are off) -->
          <div v-if="tabs.length === 0" class="card py-16 text-center">
            <p class="text-gray-500 dark:text-gray-400">{{ t('payment.billingUnavailable') }}</p>
          </div>
          <!-- Top-up Tab -->
          <template v-else-if="activeTab === 'recharge'">
            <!-- Recharge Account Card -->
            <div class="card p-5">
              <p class="text-xs font-medium text-gray-400 dark:text-gray-500">{{ t('payment.rechargeAccount') }}</p>
              <p class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ user?.username || '' }}</p>
              <p class="mt-0.5 text-sm font-medium text-green-600 dark:text-green-400">{{ t('payment.currentBalance') }}: {{ user?.balance?.toFixed(2) ?? '—' }}</p>
            </div>
            <div v-if="enabledMethods.length === 0" class="card py-16 text-center">
              <p class="text-gray-500 dark:text-gray-400">{{ t('payment.notAvailable') }}</p>
            </div>
            <template v-else>
            <div class="card p-6">
              <!-- 充值赠送活动文案（后台 Markdown 配置，空则不渲染） -->
              <div
                v-if="renderedBonusNotice"
                class="mb-5 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-relaxed text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100 [&_a]:font-medium [&_a]:underline [&_h1]:text-base [&_h1]:font-semibold [&_h2]:text-base [&_h2]:font-semibold [&_h3]:text-sm [&_h3]:font-semibold [&_ol]:my-1 [&_ol]:ml-5 [&_ol]:list-decimal [&_p]:my-1 [&_strong]:font-semibold [&_ul]:my-1 [&_ul]:ml-5 [&_ul]:list-disc"
                data-testid="recharge-bonus-notice"
                v-html="renderedBonusNotice"
              ></div>
              <AmountInput
                v-model="amount"
                :amounts="[10, 20, 50, 100, 200, 500, 1000, 2000, 5000]"
                :min="globalMinAmount"
                :max="globalMaxAmount"
                :bonus-tiers="rechargeBonusTiers"
                :bonus-mode="rechargeBonusMode"
                :multiplier="balanceRechargeMultiplier"
                :currency="selectedCurrency"
              />
              <p v-if="amountError" class="mt-2 text-xs text-amber-600 dark:text-amber-300">{{ amountError }}</p>
            </div>
            <div v-if="enabledMethods.length >= 1" class="card p-6">
              <PaymentMethodSelector
                :methods="methodOptions"
                :selected="selectedMethod"
                @select="selectedMethod = $event"
              />
            </div>
            <div v-if="validAmount > 0" class="card p-6">
              <div class="space-y-2 text-sm">
                <div class="flex justify-between">
                  <span class="text-gray-500 dark:text-gray-400">{{ t('payment.paymentAmount') }}</span>
                  <span :class="discountAmount > 0 ? 'text-gray-400 line-through dark:text-gray-500' : 'text-gray-900 dark:text-white'">{{ formatSelectedPaymentAmount(validAmount) }}</span>
                </div>
                <div v-if="discountAmount > 0" class="flex justify-between" data-testid="recharge-discount-row">
                  <span class="text-gray-500 dark:text-gray-400">{{ t('payment.rechargeBonus.discountLabelWithPercent', { percent: formatRechargeBonusNumber(bonusQuote.percent) }) }}</span>
                  <span class="font-medium text-red-600 dark:text-red-400">-{{ formatSelectedPaymentAmount(discountAmount) }}</span>
                </div>
                <div v-if="feeRate > 0" class="flex justify-between">
                  <span class="text-gray-500 dark:text-gray-400">{{ t('payment.fee') }} ({{ feeRate }}%)</span>
                  <span class="text-gray-900 dark:text-white">{{ formatSelectedPaymentAmount(feeAmount) }}</span>
                </div>
                <div v-if="showActualPay" class="flex justify-between border-t border-gray-200 pt-2 dark:border-dark-600">
                  <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('payment.actualPay') }}</span>
                  <span class="text-lg font-bold text-primary-600 dark:text-primary-400">{{ formatSelectedPaymentAmount(totalAmount) }}</span>
                </div>
                <div v-if="showBonusRow" class="flex justify-between" :class="{ 'border-t border-gray-200 pt-2 dark:border-dark-600': !showActualPay }" data-testid="recharge-bonus-row">
                  <span class="text-gray-500 dark:text-gray-400">{{ t('payment.rechargeBonus.amountLabelWithPercent', { percent: formatRechargeBonusNumber(bonusQuote.percent) }) }}</span>
                  <span class="font-medium text-red-600 dark:text-red-400">+${{ bonusQuote.bonus.toFixed(2) }}</span>
                </div>
                <div v-if="showCreditedBalance" class="flex justify-between" :class="{ 'border-t border-gray-200 pt-2 dark:border-dark-600': !showActualPay && !showBonusRow }" data-testid="recharge-credited-row">
                  <span class="text-gray-500 dark:text-gray-400">{{ t('payment.creditedBalance') }}</span>
                  <span :class="bonusQuote.percent > 0 ? 'font-semibold text-gray-900 dark:text-white' : 'text-gray-900 dark:text-white'">${{ creditedAmount.toFixed(2) }}</span>
                </div>
                <p v-if="balanceRechargeMultiplier !== 1" class="border-t border-gray-200 pt-2 text-xs text-gray-500 dark:border-dark-600 dark:text-gray-400">
                  {{ t('payment.rechargeRatePreview', { currency: selectedCurrency, usd: balanceRechargeMultiplier.toFixed(2) }) }}
                </p>
              </div>
            </div>
            <button :class="['btn w-full py-3 text-base font-medium', paymentButtonClass]" :disabled="!canSubmit || blocked" @click="handleSubmitRecharge">
              <span v-if="submitting" class="flex items-center justify-center gap-2">
                <span class="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent"></span>
                {{ t('common.processing') }}
              </span>
              <span v-else>{{ t('payment.createOrder') }} {{ formatSelectedPaymentAmount(totalAmount) }}</span>
            </button>
            </template>
          </template>
          <!-- Subscribe Tab -->
          <template v-else-if="activeTab === 'subscription'">
            <!-- Subscription confirm (inline, replaces plan list) -->
            <template v-if="selectedPlan">
              <div class="card p-5">
                <!-- Header: platform badge + plan name -->
                <div class="mb-3 flex flex-wrap items-center gap-2">
                  <span :class="['rounded-md border px-2 py-0.5 text-xs font-medium', planBadgeClass]">
                    {{ platformLabel(selectedPlan.group_platform || '') }}
                  </span>
                  <h3 class="text-lg font-bold text-gray-900 dark:text-white">{{ selectedPlan.name }}</h3>
                </div>
                <!-- Price -->
                <div class="flex items-baseline gap-2">
                  <span v-if="selectedPlan.original_price" class="text-sm text-gray-400 line-through dark:text-gray-500">
                    {{ formatSelectedSubscriptionPaymentAmount(selectedPlan.original_price) }}
                  </span>
                  <span :class="['text-3xl font-bold', planTextClass]">{{ formatSelectedSubscriptionPaymentAmount(selectedPlan.price) }}</span>
                  <span class="text-sm text-gray-500 dark:text-gray-400">/ {{ planValiditySuffix }}</span>
                </div>
                <!-- Description -->
                <p v-if="selectedPlan.description" class="mt-2 text-sm leading-relaxed text-gray-500 dark:text-gray-400">
                  {{ selectedPlan.description }}
                </p>
                <!-- Rate + Limits grid -->
                <div class="mt-3 grid grid-cols-2 gap-3">
                  <div>
                    <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('payment.planCard.rate') }}</span>
                    <div class="flex items-baseline">
                      <span :class="['text-lg font-bold', planTextClass]">×{{ selectedPlan.rate_multiplier ?? 1 }}</span>
                    </div>
                  </div>
                  <div v-if="planHasPeakRate(selectedPlan)">
                    <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('payment.planCard.peakRate') }}</span>
                    <div class="text-sm font-semibold text-amber-700 dark:text-amber-300">
                      {{ planPeakRateLabel(selectedPlan) }}
                    </div>
                  </div>
                  <div v-if="selectedPlan.daily_limit_usd != null">
                    <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('payment.planCard.dailyLimit') }}</span>
                    <div class="text-lg font-semibold text-gray-800 dark:text-gray-200">${{ selectedPlan.daily_limit_usd }}</div>
                  </div>
                  <div v-if="selectedPlan.weekly_limit_usd != null">
                    <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('payment.planCard.weeklyLimit') }}</span>
                    <div class="text-lg font-semibold text-gray-800 dark:text-gray-200">${{ selectedPlan.weekly_limit_usd }}</div>
                  </div>
                  <div v-if="selectedPlan.monthly_limit_usd != null">
                    <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('payment.planCard.monthlyLimit') }}</span>
                    <div class="text-lg font-semibold text-gray-800 dark:text-gray-200">${{ selectedPlan.monthly_limit_usd }}</div>
                  </div>
                  <div v-if="selectedPlan.daily_limit_usd == null && selectedPlan.weekly_limit_usd == null && selectedPlan.monthly_limit_usd == null">
                    <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('payment.planCard.quota') }}</span>
                    <div class="text-lg font-semibold text-gray-800 dark:text-gray-200">{{ t('payment.planCard.unlimited') }}</div>
                  </div>
                </div>
              </div>
              <div v-if="enabledMethods.length >= 1" class="card p-6">
                <PaymentMethodSelector
                  :methods="subMethodOptions"
                  :selected="selectedMethod"
                  @select="selectedMethod = $event"
                />
              </div>
              <div v-if="feeRate > 0 && selectedPlan.price > 0" class="card p-6">
                <div class="space-y-2 text-sm">
                  <div class="flex justify-between">
                    <span class="text-gray-500 dark:text-gray-400">{{ t('payment.amountLabel') }}</span>
                    <span class="text-gray-900 dark:text-white">{{ formatSelectedPaymentAmount(subPaymentAmount) }}</span>
                  </div>
                  <div class="flex justify-between">
                    <span class="text-gray-500 dark:text-gray-400">{{ t('payment.fee') }} ({{ feeRate }}%)</span>
                    <span class="text-gray-900 dark:text-white">{{ formatSelectedPaymentAmount(subFeeAmount) }}</span>
                  </div>
                  <div class="flex justify-between border-t border-gray-200 pt-2 dark:border-dark-600">
                    <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('payment.actualPay') }}</span>
                    <span class="text-lg font-bold text-primary-600 dark:text-primary-400">{{ formatSelectedPaymentAmount(subTotalAmount) }}</span>
                  </div>
                </div>
              </div>
              <button :class="['btn w-full py-3 text-base font-medium', paymentButtonClass]" :disabled="!canSubmitSubscription || blocked" @click="confirmSubscribe">
                <span v-if="submitting" class="flex items-center justify-center gap-2">
                  <span class="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent"></span>
                  {{ t('common.processing') }}
                </span>
                <span v-else>{{ t('payment.createOrder') }} {{ formatSelectedPaymentAmount(subTotalAmount) }}</span>
              </button>
              <button class="btn btn-secondary w-full" @click="selectedPlan = null">{{ t('common.cancel') }}</button>
            </template>
            <!-- Plan list -->
            <template v-else>
              <div v-if="checkout.plans.length === 0" class="card py-16 text-center">
                <Icon name="gift" size="xl" class="mx-auto mb-3 text-gray-300 dark:text-dark-600" />
                <p class="text-gray-500 dark:text-gray-400">{{ t('payment.noPlans') }}</p>
              </div>
              <div v-else :class="planGridClass">
                <SubscriptionPlanCard v-for="plan in checkout.plans" :key="plan.id" :plan="plan" :active-subscriptions="activeSubscriptions" @select="selectPlan" />
              </div>
              <!-- Active subscriptions (compact, below plan list) -->
              <div v-if="activeSubscriptions.length > 0">
                <p class="mb-2 text-xs font-medium text-gray-400 dark:text-gray-500">{{ t('payment.activeSubscription') }}</p>
                <div class="space-y-2">
                  <div v-for="sub in activeSubscriptions" :key="sub.id"
                    class="flex items-center gap-3 rounded-xl border border-gray-100 bg-white px-3 py-2 dark:border-dark-700 dark:bg-dark-800">
                    <div :class="['h-6 w-1 shrink-0 rounded-full', platformAccentBarClass(sub.group?.platform || '')]" />
                    <div class="min-w-0 flex-1">
                      <div class="flex items-center gap-1.5">
                        <span class="truncate text-xs font-semibold text-gray-900 dark:text-white">{{ sub.group?.name || t('payment.groupFallback', { id: sub.group_id }) }}</span>
                        <span :class="['shrink-0 rounded-full px-1.5 py-0.5 text-[9px] font-medium', platformBadgeLightClass(sub.group?.platform || '')]">{{ platformLabel(sub.group?.platform || '') }}</span>
                      </div>
                      <div class="flex flex-wrap gap-x-3 text-[11px] text-gray-400 dark:text-gray-500">
                        <span>{{ t('payment.planCard.rate') }}: ×{{ sub.group?.rate_multiplier ?? 1 }}</span>
                        <span v-if="subscriptionHasPeakRate(sub)">{{ t('payment.planCard.peakRate') }}: {{ subscriptionPeakRateLabel(sub) }}</span>
                        <span v-if="sub.group?.daily_limit_usd == null && sub.group?.weekly_limit_usd == null && sub.group?.monthly_limit_usd == null">{{ t('payment.planCard.quota') }}: {{ t('payment.planCard.unlimited') }}</span>
                        <span v-if="sub.expires_at">{{ t('userSubscriptions.daysRemaining', { days: getDaysRemaining(sub.expires_at) }) }}</span>
                        <span v-else>{{ t('userSubscriptions.noExpiration') }}</span>
                      </div>
                    </div>
                    <span class="badge badge-success shrink-0 text-[10px]">{{ t('userSubscriptions.status.active') }}</span>
                  </div>
                </div>
              </div>
            </template>
          </template>
        </template>
        <div v-if="(checkout.help_text || checkout.help_image_url) && paymentPhase === 'select' && !selectedPlan" class="card p-4">
          <div class="flex flex-col items-center gap-3">
            <img v-if="checkout.help_image_url" :src="checkout.help_image_url" alt=""
              class="h-40 max-w-full cursor-pointer rounded-lg object-contain transition-opacity hover:opacity-80"
              @click="previewImage = checkout.help_image_url" />
            <div v-if="checkout.help_text" class="markdown-body w-full overflow-x-auto break-words" v-html="renderedHelpText"></div>
          </div>
        </div>
      </template>
    </div>
    <!-- Renewal Plan Selection Modal -->
    <Teleport to="body">
      <Transition name="modal">
        <div v-if="showRenewalModal" class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" @click.self="closeRenewalModal">
          <div class="relative flex max-h-full w-full max-w-lg flex-col rounded-2xl border border-gray-200 bg-white p-6 shadow-2xl dark:border-dark-700 dark:bg-dark-900">
            <!-- Close button -->
            <button class="absolute right-4 top-4 rounded-lg p-1 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-gray-200" @click="closeRenewalModal">
              <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
            </button>
            <h3 class="mb-4 shrink-0 text-lg font-semibold text-gray-900 dark:text-white">{{ t('payment.selectPlan') }}</h3>
            <div class="min-h-0 space-y-4 overflow-y-auto">
              <SubscriptionPlanCard v-for="plan in renewalPlans" :key="plan.id" :plan="plan" :active-subscriptions="activeSubscriptions" @select="selectPlanFromModal" />
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
    <!-- Image Preview Overlay -->
    <Teleport to="body">
      <Transition name="modal">
        <div v-if="previewImage" class="fixed inset-0 z-[60] flex items-center justify-center bg-black/70 backdrop-blur-sm" @click="previewImage = ''">
          <img :src="previewImage" alt="" class="max-h-[85vh] max-w-[90vw] rounded-xl object-contain shadow-2xl" />
        </div>
      </Transition>
    </Teleport>
</template>

