<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/shared/ui/Select.vue'
import {
  OPENAI_WS_MODE_OFF, OPENAI_WS_MODE_CTX_POOL, OPENAI_WS_MODE_PASSTHROUGH,
  OPENAI_WS_MODE_HTTP_BRIDGE, resolveOpenAIWSModeHintKey
} from '../model/openaiWsMode'
import type { OpenAIWSMode } from '../model/openaiWsMode'

const props = defineProps<{
  prefix: 'bulk-edit-openai-ws-mode' | 'bulk-edit-openai-apikey-ws-mode'
  selected: boolean
  mode: OpenAIWSMode
}>()
const emit = defineEmits<{
  'update:selected': [value: boolean]
  'update:mode': [value: OpenAIWSMode]
}>()
const { t } = useI18n()
const modeOptions = computed<Array<{ value: OpenAIWSMode; label: string }>>(() => [
  { value: OPENAI_WS_MODE_OFF, label: t('admin.accounts.openai.wsModeOff') },
  { value: OPENAI_WS_MODE_CTX_POOL, label: t('admin.accounts.openai.wsModeCtxPool') },
  { value: OPENAI_WS_MODE_PASSTHROUGH, label: t('admin.accounts.openai.wsModePassthrough') },
  { value: OPENAI_WS_MODE_HTTP_BRIDGE, label: t('admin.accounts.openai.wsModeHttpBridge') }
])
const hintKey = computed(() => resolveOpenAIWSModeHintKey(props.mode))

function updateMode(value: unknown) {
  const option = modeOptions.value.find(option => option.value === value)
  if (props.selected && option) emit('update:mode', option.value)
}
</script>

<template>
  <!-- The parent selects the eligible credential kind; prefix only names the controls. -->
  <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
    <div class="mb-3 flex items-center justify-between">
      <label
        :id="prefix + '-label'"
        class="input-label mb-0"
        :for="prefix + '-enabled'"
      >
        {{ t('admin.accounts.openai.wsMode') }}
      </label>
      <input
        :checked="selected"
        @change="emit('update:selected', !selected)"
        :id="prefix + '-enabled'"
        type="checkbox"
        :aria-controls="prefix"
        class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
      />
    </div>
    <div
      :id="prefix"
      :class="!selected && 'pointer-events-none opacity-50'"
    >
      <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.openai.wsModeDesc') }}
      </p>
      <p v-if="hintKey" class="mb-3 text-xs text-gray-500 dark:text-gray-400">
        {{ t(hintKey) }}
      </p>
      <Select
        :model-value="mode"
        :disabled="!selected"
        @update:model-value="updateMode"
        :data-testid="prefix + '-select'"
        :options="modeOptions"
        :aria-labelledby="prefix + '-label'"
      />
    </div>
  </div>
</template>
