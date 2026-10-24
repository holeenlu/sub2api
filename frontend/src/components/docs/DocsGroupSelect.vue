<template>
  <label class="block min-w-0">
    <span v-if="showLabel" class="mb-1.5 block text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('docs.chooseGroup') }}</span>
    <select
      :value="modelValue ?? ''"
      class="h-10 w-full rounded-md border border-gray-200 bg-white px-3 text-sm text-gray-800 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/15 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-100"
      @change="$emit('update:modelValue', Number(($event.target as HTMLSelectElement).value))"
    >
      <option v-if="groups.length === 0" value="" disabled>{{ t(loading ? 'docs.loadingCatalog' : 'docs.noVisibleGroups') }}</option>
      <option v-for="group in groups" :key="group.id" :value="group.id">
        {{ group.name }} · {{ effectiveRate(group).toFixed(2) }}x
      </option>
    </select>
  </label>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ModelPlazaGroup } from '@/api/modelPlaza'

withDefaults(defineProps<{ groups: ModelPlazaGroup[]; modelValue: number | null; showLabel?: boolean; loading?: boolean }>(), { showLabel: true, loading: false })
defineEmits<{ 'update:modelValue': [value: number] }>()
const { t } = useI18n()
function effectiveRate(group: ModelPlazaGroup) { return group.user_rate_multiplier ?? group.rate_multiplier }
</script>
