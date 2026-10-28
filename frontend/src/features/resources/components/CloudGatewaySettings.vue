<template>
  <div class="space-y-3 border-t border-gray-200 py-4 dark:border-dark-600" data-testid="cloud-gateway-settings">
    <div class="flex items-center justify-between gap-4">
      <div class="min-w-0">
        <label class="input-label mb-0" :for="`cloud-enabled-${accountId}`">{{ t('admin.accounts.openai.cloud.enable') }}</label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.cloud.nativeHint') }}</p>
      </div>
      <Toggle :id="`cloud-enabled-${accountId}`" :model-value="enabled" :disabled="!canEdit"
        :aria-label="t('admin.accounts.openai.cloud.enable')" data-testid="cloud-enabled" @update:model-value="emit('update:enabled', $event)" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'
defineProps<{ accountId: number; enabled: boolean; canEdit: boolean }>()
const emit = defineEmits<{ 'update:enabled': [value: boolean] }>()
const { t } = useI18n()
</script>
