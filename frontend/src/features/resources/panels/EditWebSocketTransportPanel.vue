<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/shared/ui/Select.vue'
import { type OpenAIWSMode } from '@/features/resources/model/openaiWsMode'
import { type UnwrapRef, type ComputedRef, type WritableComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { Props } from '../model/editEditorTypes'


const props = defineProps<{
  account: NonNullable<Props['account']>
  openAIWSModeHintKey: UnwrapRef<ComputedRef<"admin.accounts.openai.wsModeCtxPoolHint" | "admin.accounts.openai.wsModePassthroughHint" | "admin.accounts.openai.wsModeHttpBridgeHint" | null>>
  openaiResponsesWebSocketV2Mode: UnwrapRef<WritableComputedRef<OpenAIWSMode, OpenAIWSMode>>
  openAIWSModeOptions: UnwrapRef<ComputedRef<{ value: string; label: string }[]>>
}>()

const emit = defineEmits<{
  'update:openaiResponsesWebSocketV2Mode': [value: UnwrapRef<WritableComputedRef<OpenAIWSMode, OpenAIWSMode>>]
}>()

const { t } = useI18n()

const account = computed(() => props.account)

const openAIWSModeHintKey = computed(() => props.openAIWSModeHintKey)

const openaiResponsesWebSocketV2Mode = usePanelField(() => props.openaiResponsesWebSocketV2Mode, value => emit('update:openaiResponsesWebSocketV2Mode', value))

const openAIWSModeOptions = computed(() => props.openAIWSModeOptions)
</script>

<template>
<template v-if="account">
<div
        v-if="account?.platform === 'openai' && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.wsMode') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.wsModeDesc') }}
            </p>
            <p v-if="openAIWSModeHintKey" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t(openAIWSModeHintKey) }}
            </p>
          </div>
          <div class="w-52">
            <Select v-model="openaiResponsesWebSocketV2Mode" data-testid="edit-openai-ws-mode-select" :options="openAIWSModeOptions" />
          </div>
        </div>
      </div>
</template>
</template>
