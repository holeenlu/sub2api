<template>
  <div class="mb-4 space-y-2" data-testid="account-catalog-policy">
    <label class="text-xs font-medium text-gray-600 dark:text-gray-300" :for="selectId">
      {{ t('modelCatalog.accountPolicy') }}
    </label>
    <select
      :id="selectId"
      :value="modelValue.mode"
      class="input text-sm"
      @change="update({ mode: ($event.target as HTMLSelectElement).value as CatalogPolicyMode })"
    >
      <option v-if="allowLegacy !== false" value="legacy">{{ t('modelCatalog.legacy') }}</option>
      <option value="follow">{{ t('modelCatalog.follow') }}</option>
      <option value="fixed">{{ t('modelCatalog.fixed') }}</option>
    </select>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t(`modelCatalog.policyModeHint.${modelValue.mode}`) }}</p>
    <textarea
      v-if="modelValue.mode !== 'legacy'"
      :value="modelValue.excludedText"
      class="input font-mono text-xs"
      rows="2"
      :placeholder="t('modelCatalog.exclude')"
      :aria-label="t('modelCatalog.exclude')"
      @input="update({ excludedText: ($event.target as HTMLTextAreaElement).value })"
    />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CatalogPolicyMode } from '@/api/admin/modelCatalog'
import type { AccountCatalogPolicyForm } from './accountCatalogPolicy'

const props = defineProps<{ modelValue: AccountCatalogPolicyForm; selectId?: string; allowLegacy?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [AccountCatalogPolicyForm] }>()
const { t } = useI18n()
const selectId = props.selectId ?? 'account-catalog-policy-mode'

function update(patch: Partial<AccountCatalogPolicyForm>) {
  emit('update:modelValue', { ...props.modelValue, ...patch })
}
</script>
