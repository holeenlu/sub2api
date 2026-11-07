<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import PaymentStatusPanel from '../components/PaymentStatusPanel.vue'
import { paymentAPI } from '../api/payment'
import type { PaymentOrder } from '../api/types'
import { usePaymentsServices } from '../services'
import { usePaymentRequest } from '../queries/usePaymentRequest'
import { extractPaymentErrorMessage } from '@/features/payments/model/paymentError'
const { t } = useI18n(), route = useRoute(), router = useRouter(), services = usePaymentsServices()
function query(key: string): string { const raw = route.query[key]; return typeof raw === 'string' ? raw : '' }
const orderId = computed(() => Number(query('order_id')) || 0)
const request = usePaymentRequest(() => services.scopeKey.value === null ? null : services.scopeKey.value + ':' + orderId.value)
const order = ref<PaymentOrder | null>(null), loading = ref(false), error = ref('')
const expiresAt = computed(() => query('expires_at') || order.value?.expires_at || '')
async function load(): Promise<void> {
  order.value = null; error.value = ''
  const ticket = request.begin(), id = orderId.value, userId = services.user.value?.id
  if (!ticket.isCurrent() || !id) return
  loading.value = true
  try {
    const response = await paymentAPI.getOrder(id, ticket)
    if (!ticket.isCurrent()) return
    if (response.data.id === id && response.data.user_id === userId) order.value = response.data
    else error.value = t('payment.result.failed')
  } catch (cause) { if (ticket.isCurrent()) error.value = extractPaymentErrorMessage(cause, t, t('common.error')) }
  finally { if (ticket.isCurrent()) loading.value = false }
}
function showResult(): void { void router.push({ path: '/payment/result', query: { order_id: String(orderId.value), resume_token: query('resume_token') || undefined } }) }
watch(() => [services.scopeKey.value, orderId.value], () => { loading.value = false; void load() }, { flush: 'sync' })
onMounted(() => { void load() })
</script>
<template>
  <div class="mx-auto max-w-md space-y-6 py-8">
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <p v-else-if="error" role="alert">{{ error }}</p>
    <PaymentStatusPanel v-else-if="order" :key="order.id" :order-id="order.id" :amount="order.amount" :pay-amount="order.pay_amount"
      :qr-code="query('qr')" :pay-url="query('pay_url')" :expires-at="expiresAt" :payment-type="query('payment_type') || order.payment_type"
      :order-type="order.order_type" :currency="order.currency" :out-trade-no="order.out_trade_no"
      @success="showResult" @done="router.push('/purchase')" />
  </div>
</template>
