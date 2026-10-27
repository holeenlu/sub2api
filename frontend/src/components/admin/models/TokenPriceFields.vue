<template>
  <div class="grid grid-cols-2 gap-3 sm:grid-cols-3">
    <label v-for="field in fields" :key="field" class="min-w-0 text-xs font-medium text-gray-600 dark:text-gray-400">
      {{ t(`modelManagement.${field}`) }}<span v-if="!field.startsWith('cache_') || field === 'cache_read'" class="text-red-500"> *</span>
      <input :value="modelValue[field] ?? ''" type="number" min="0" step="any" class="input mt-1 w-full" :disabled="disabled" @input="change(field, ($event.target as HTMLInputElement).value)" />
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
function change(field: keyof TokenPrice, value: string) { emit('update:modelValue', { ...props.modelValue, [field]: value === '' ? null : Number(value) }) }
</script>
