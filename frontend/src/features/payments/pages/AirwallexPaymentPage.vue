<template>

    <div class="mx-auto max-w-lg space-y-6 py-8">
      <div v-if="loading" class="flex items-center justify-center py-20">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-emerald-500 border-t-transparent"></div>
      </div>

      <div v-else-if="errorMessage" class="card p-8 text-center">
        <div class="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-red-100 dark:bg-red-900/30">
          <Icon name="exclamationCircle" size="xl" class="text-red-500" />
        </div>
        <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('payment.airwallexLoadFailed') }}</h3>
        <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ errorMessage }}</p>
        <button class="btn btn-primary mt-6" @click="router.push('/purchase')">{{ t('payment.result.backToRecharge') }}</button>
      </div>

      <div v-else class="card p-6">
        <div class="flex flex-col items-center space-y-4 py-4">
          <div class="h-10 w-10 animate-spin rounded-full border-4 border-emerald-500 border-t-transparent"></div>
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.payInNewWindowHint') }}</p>
        </div>
      </div>
    </div>

</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import Icon from '@/shared/ui/icons/Icon.vue'
import { useAirwallexCheckout } from '../commands/useAirwallexCheckout'
const { t } = useI18n(), route = useRoute(), router = useRouter()
function query(key: string): string { const raw = route.query[key]; return typeof raw === 'string' ? raw : Array.isArray(raw) ? raw[0] ?? '' : '' }
const { loading, errorMessage } = useAirwallexCheckout(() => ({ orderId: Number(query('order_id')) || 0, outTradeNo: query('out_trade_no'), resumeToken: query('resume_token') || undefined }))
</script>
