<template>
  <div class="flex min-h-screen items-center justify-center bg-gray-50 px-4 dark:bg-dark-900">
    <div class="w-full max-w-md space-y-6">
      <!-- Loading -->
      <div v-if="loading" class="flex items-center justify-center py-20">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary-500 border-t-transparent"></div>
      </div>
      <template v-else>
        <!-- Status Icon -->
        <div class="text-center">
          <div v-if="isSuccess"
            class="mx-auto flex h-20 w-20 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30">
            <svg class="h-10 w-10 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"
              stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
            </svg>
          </div>
          <div v-else-if="isPending"
            class="mx-auto flex h-20 w-20 items-center justify-center rounded-full bg-yellow-100 dark:bg-yellow-900/30">
            <div class="h-10 w-10 animate-spin rounded-full border-4 border-yellow-500 border-t-transparent"></div>
          </div>
          <div v-else
            class="mx-auto flex h-20 w-20 items-center justify-center rounded-full bg-red-100 dark:bg-red-900/30">
            <svg class="h-10 w-10 text-red-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </div>
          <h2 class="mt-4 text-2xl font-bold text-gray-900 dark:text-white">
            {{ statusTitle }}
          </h2>
          <p v-if="isPending" class="mt-2 text-sm text-gray-500 dark:text-gray-400">
            {{ t('payment.result.processingHint') }}
          </p>
        </div>
        <!-- Order Info -->
        <div v-if="order" class="rounded-xl bg-white p-5 shadow-sm dark:bg-dark-800">
          <div class="space-y-3 text-sm">
            <div v-if="hasOrderId(order)" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderId') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">#{{ order.id }}</span>
            </div>
            <div v-if="order.out_trade_no" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderNo') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ order.out_trade_no }}</span>
            </div>
            <div v-if="hasAmountFields(order)" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.baseAmount') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ formatGatewayAmount(baseAmount) }}</span>
            </div>
            <div v-if="hasAmountFields(order) && order.fee_rate > 0" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.fee') }} ({{ order.fee_rate }}%)</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ formatGatewayAmount(feeAmount) }}</span>
            </div>
            <div v-if="hasAmountFields(order)" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.payAmount') }}</span>
              <span class="font-bold text-primary-600 dark:text-primary-400">{{ formatGatewayAmount(order.pay_amount) }}</span>
            </div>
            <div v-if="hasAmountFields(order) && orderBonusAmount(order) > 0" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.bonusAmount') }}</span>
              <span class="font-medium text-amber-600 dark:text-amber-400">+${{ orderBonusAmount(order).toFixed(2) }}</span>
            </div>
            <div v-if="hasAmountFields(order) && (order.amount !== order.pay_amount || orderBonusAmount(order) > 0)" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.creditedAmount') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ order.order_type === 'balance' ? '$' + order.amount.toFixed(2) : formatGatewayAmount(order.amount) }}</span>
            </div>
            <div v-if="hasPaymentType(order)" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.paymentMethod') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ t(paymentMethodI18nKey(order.payment_type), normalizedOrderPaymentType(order.payment_type)) }}</span>
            </div>
            <div class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.status') }}</span>
              <OrderStatusBadge :status="displayOrderStatus(order.status)" />
            </div>
          </div>
        </div>
        <!-- EasyPay return info (when no order loaded) -->
        <div v-else-if="returnInfo" class="rounded-xl bg-white p-5 shadow-sm dark:bg-dark-800">
          <div class="space-y-3 text-sm">
            <div v-if="returnInfo.outTradeNo" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderId') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ returnInfo.outTradeNo }}</span>
            </div>
            <div v-if="returnInfo.money" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.payAmount') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ formatReturnAmount(returnInfo.money) }}</span>
            </div>
            <div v-if="returnInfo.type" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.paymentMethod') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ t(paymentMethodI18nKey(returnInfo.type), normalizedOrderPaymentType(returnInfo.type)) }}</span>
            </div>
          </div>
        </div>
        <!-- Actions -->
        <div class="flex gap-3">
          <button class="btn btn-secondary flex-1" @click="router.push('/purchase')">{{ t('payment.result.backToRecharge') }}</button>
          <button class="btn btn-primary flex-1" @click="router.push('/orders')">{{ t('payment.result.viewOrders') }}</button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, unref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import OrderStatusBadge from '../components/OrderStatusBadge.vue'
import { useOrderResult } from '../queries/useOrderResult'
import { hasOrderId, hasAmountFields, hasPaymentType, orderBonusAmount, displayOrderStatus, isSuccessStatus, isPendingStatus } from '../model/orderResult'
import { formatPaymentAmount, normalizePaymentCurrency } from '../model/currency'
import { normalizePaymentMethodForDisplay, paymentMethodI18nKey } from '../model/paymentUx'
const i18n = useI18n(), { t } = i18n, route = useRoute(), router = useRouter()
function query(key: string): string { const raw = route.query[key]; return typeof raw === 'string' ? raw : Array.isArray(raw) ? raw[0] ?? '' : '' }
const { order, loading } = useOrderResult(() => ({ orderId: Number(query('order_id')) || 0, resumeToken: query('resume_token'), outTradeNo: query('out_trade_no'), legacyReturn: !!query('trade_status') }))
const isSuccess = computed(() => isSuccessStatus(order.value?.status))
const hasLookup = computed(() => !!query('resume_token') || Number(query('order_id')) > 0 || (!!query('out_trade_no') && !!query('trade_status')))
const isPending = computed(() => order.value ? isPendingStatus(order.value.status) : hasLookup.value)
const statusTitle = computed(() => t(isSuccess.value ? 'payment.result.success' : isPending.value ? 'payment.result.processing' : 'payment.result.failed'))
const currency = computed(() => normalizePaymentCurrency(order.value && 'currency' in order.value ? order.value.currency : 'CNY'))
const baseAmount = computed(() => {
  if (!hasAmountFields(order.value)) return null
  const rate = order.value.fee_rate || 0
  return rate <= 0 ? order.value.pay_amount : Math.round((order.value.pay_amount / (1 + rate / 100)) * 100) / 100
})
const feeAmount = computed(() => !hasAmountFields(order.value) || baseAmount.value === null ? null : Math.round((order.value.pay_amount - baseAmount.value) * 100) / 100)
function formatGatewayAmount(value: number | null): string { return value === null ? '—' : formatPaymentAmount(value, currency.value, unref(i18n.locale)) }
function formatReturnAmount(value: string): string { const amount = Number(value); return Number.isFinite(amount) ? formatGatewayAmount(amount) : '—' }
function normalizedOrderPaymentType(value: string): string { return normalizePaymentMethodForDisplay(value) || value }
const returnInfo = computed(() => !order.value && query('out_trade_no') && query('trade_status') ? { outTradeNo: query('out_trade_no'), money: query('money'), type: query('type') } : null)
</script>
