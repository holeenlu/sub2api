<template>
  <div class="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-6">
    <label v-for="field in fields" :key="field" class="min-w-0 text-xs font-medium text-gray-600 dark:text-gray-400" :class="field === 'cache_5m' || field === 'cache_1h' ? 'sm:col-span-3' : 'sm:col-span-2'">
      <span class="mb-1.5 block min-h-8 sm:min-h-4">{{ t(`modelManagement.${field}`) }}<span v-if="requiredField(field)" class="ml-0.5 text-red-500" aria-hidden="true">*</span></span>
      <span class="relative block">
        <span class="pointer-events-none absolute inset-y-0 left-3 flex items-center text-sm text-gray-400" aria-hidden="true">$</span>
        <input :value="modelValue[field] ?? ''" type="number" inputmode="decimal" min="0" step="any" class="input w-full pl-7 text-right tabular-nums" :placeholder="requiredField(field) ? '0' : t('modelManagement.inherit')" :required="requiredField(field)" :disabled="disabled" :data-price-field="field" @input="change(field, ($event.target as HTMLInputElement).value)" />
      </span>
    </label>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TokenPrice } from '@/api/admin/models'
const props = defineProps<{ modelValue: TokenPrice; image?: boolean; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: TokenPrice] }>()
const { t } = useI18n()
const fields = computed<(keyof TokenPrice)[]>(() => props.image ? ['input', 'output', 'cache_read'] : ['input', 'output', 'cache_read', 'cache_5m', 'cache_1h'])
const imageEntered = computed(() => props.image && ['input', 'output', 'cache_read'].some(field => props.modelValue[field as keyof TokenPrice] != null))
const requiredField = (field: keyof TokenPrice) => ['input', 'output', 'cache_read'].includes(field) && (!props.image || !!imageEntered.value)
function change(field: keyof TokenPrice, value: string) { emit('update:modelValue', { ...props.modelValue, [field]: value === '' ? null : Number(value) }) }
</script>
