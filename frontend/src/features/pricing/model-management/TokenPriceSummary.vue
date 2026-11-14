<template>
  <p v-if="value === null" class="text-xs">{{ t('unpriced') }}</p>
  <p v-else-if="value === undefined" class="text-xs">{{ t('unknown') }}</p>
  <dl v-else class="grid grid-cols-2 gap-x-4 gap-y-1 text-xs sm:grid-cols-3">
    <div v-for="field in tokenFields" :key="field"><dt class="text-gray-500">{{ t(field) }}</dt><dd class="tabular-nums">{{ rate(value[field], field) }}</dd></div>
  </dl>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { formatManagedRate } from './formatRate'
import { messages } from './locales'
import { tokenFields } from './types'
import type { TokenField, TokenPrice } from './types'
defineProps<{ value: TokenPrice | null | undefined }>()
const { t, locale } = useI18n({ useScope: 'local', messages })
function rate(value: number | null | undefined, field: TokenField) {
  return value === undefined ? t('unknown') : value === null ? t(field === 'cache_5m' || field === 'cache_1h' ? 'fallback' : 'unpriced') : formatManagedRate(value, locale.value)
}
</script>
