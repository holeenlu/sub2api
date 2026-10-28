<template>
  <div class="grid gap-3 sm:grid-cols-3">
    <label v-for="field in tokenFields" :key="field" class="min-w-0 space-y-1 text-xs">
      <span>{{ t(field) }}{{ optional(field) ? '' : ' *' }}</span>
      <select :value="state(field)" class="input w-full" :disabled="disabled" :data-state="field" @change="setState(field, ($event.target as HTMLSelectElement).value)">
        <option value="unknown">{{ t('unknown') }}</option>
        <option value="unpriced">{{ t(optional(field) ? 'fallback' : 'unpriced') }}</option>
        <option value="priced">{{ t('priced') }}</option>
      </select>
      <input v-if="typeof modelValue[field] === 'number'" :value="modelValue[field]" :aria-label="t(field)" :data-rate="field" type="number" min="0" max="1000000000" step="any" required class="input w-full" :disabled="disabled" @input="setRate(field, ($event.target as HTMLInputElement).value)" />
    </label>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { messages } from './locales'
import { tokenFields } from './types'
import type { TokenField, TokenPrice } from './types'
const props = defineProps<{ modelValue: TokenPrice; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: TokenPrice] }>()
const { t } = useI18n({ useScope: 'local', messages })
function optional(field: TokenField) { return field === 'cache_5m' || field === 'cache_1h' }
function state(field: TokenField) {
  return props.modelValue[field] === undefined ? 'unknown' : props.modelValue[field] === null ? 'unpriced' : 'priced'
}
function setState(field: TokenField, state: string) {
  const next = { ...props.modelValue }
  if (state === 'unknown') delete next[field]
  else next[field] = state === 'unpriced' ? null : 0
  emit('update:modelValue', next)
}
function setRate(field: TokenField, value: string) {
  // Retain an invalid numeric value until form validation; never serialize NaN as null.
  emit('update:modelValue', { ...props.modelValue, [field]: value === '' ? NaN : Number(value) })
}
</script>
