<template>
  <div class="space-y-3 border-t border-gray-200 py-4 dark:border-dark-600" data-testid="cloud-gateway-settings">
    <div class="flex items-center justify-between gap-4">
      <div class="min-w-0">
        <label class="input-label mb-0" :for="`cloud-enabled-${accountId}`">{{ t('admin.accounts.openai.cloud.enable') }}</label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.cloud.nativeHint') }}</p>
      </div>
      <Toggle :id="`cloud-enabled-${accountId}`" :model-value="enabled" :disabled="!canEdit || (!enabled && status?.runtime_blocked)"
        :aria-label="t('admin.accounts.openai.cloud.enable')" data-testid="cloud-enabled" @update:model-value="emit('update:enabled', $event)" />
    </div>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <CloudReconciliation v-if="status?.runtime_blocked" :key="accountId" :account-id="accountId" @refreshed="loadStatus()" />
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getCloudStatus, type OpenAICloudStatus } from '@/api/admin/accounts'
import { extractApiErrorMessage } from '@/utils/apiError'
import Toggle from '@/components/common/Toggle.vue'
import CloudReconciliation from './CloudReconciliation.vue'
const props = defineProps<{ accountId: number; enabled: boolean; canEdit: boolean }>()
const emit = defineEmits<{ 'update:enabled': [value: boolean]; status: [value: OpenAICloudStatus | null] }>()
const { t } = useI18n()
const status = ref<OpenAICloudStatus | null>(null)
const error = ref('')
let generation = 0
async function loadStatus(reset = false) {
  const version = ++generation
  error.value = ''
  if (reset) { status.value = null; emit('status', null) }
  try {
    const value = await getCloudStatus(props.accountId)
    if (version !== generation) return
    status.value = value; emit('status', value)
  } catch (cause) {
    if (version === generation) error.value = extractApiErrorMessage(cause, t('admin.accounts.openai.cloud.failed'))
  }
}
watch(() => props.accountId, () => loadStatus(true), { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>
