<script setup lang="ts">
import { computed } from 'vue'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import type { ManagedModel, ManagedModelPlatform } from '../../api/managedModelTypes'
import { cloneDraft, commaList } from './draft'
import { useManagedModelsI18n } from './useManagedModelsI18n'

const props = defineProps<{ model: ManagedModel; platform: ManagedModelPlatform; disabled: boolean }>()
const emit = defineEmits<{ 'update:model': [ManagedModel]; close: [] }>()
const { t } = useManagedModelsI18n()
const numericFields = ['max_tokens_default', 'max_tokens_cap', 'thinking_fallback_budget'] as const
const thinkingFields = ['on_adaptive', 'on_enabled'] as const
const betaLists = ['required', 'drop'] as const
const betaMode = computed(() => props.model.betas.pass_context_1m == null ? 'inherit' : props.model.betas.pass_context_1m ? 'pass' : 'strip')
const facts = computed(() => {
  const c = props.model.catalog
  if (!c) return []
  return [
    { key: 'context', value: c.context_window },
    { key: 'maxContext', value: c.max_context_window },
    { key: 'maxOutput', value: c.max_output_tokens },
    { key: 'effort', value: c.default_reasoning_level },
    { key: 'reasoningLevels', value: c.supported_reasoning_levels?.join(', ') },
    { key: 'modalities', value: c.input_modalities?.join(', ') },
    { key: 'toolCapabilities', value: c.codex_tool_capabilities == null ? undefined : JSON.stringify(c.codex_tool_capabilities) },
  ]
})

function update(edit: (draft: ManagedModel) => void) {
  if (props.disabled) return
  const draft = cloneDraft(props.model)
  edit(draft)
  emit('update:model', draft)
}
function numberField(field: typeof numericFields[number] | 'context_window', event: Event) {
  const input = event.target as HTMLInputElement
  if (input.value === '' || !input.validity.valid || !Number.isFinite(input.valueAsNumber)) return
  update(draft => {
    if (field === 'context_window') draft.capabilities = { ...draft.capabilities, context_window: input.valueAsNumber }
    else draft.params[field] = input.valueAsNumber
  })
}
function setBeta(event: Event) {
  const value = (event.target as HTMLSelectElement).value
  update(draft => {
    if (value === 'inherit') delete draft.betas.pass_context_1m
    else draft.betas.pass_context_1m = value === 'pass'
  })
}
</script>

<template>
  <BaseDialog :show="true" :title="model.display_name || model.id" width="wide" @close="emit('close')">
    <div class="space-y-5" data-testid="model-details">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <code class="break-all text-xs text-gray-500">{{ model.id }}</code>
        <label class="flex items-center gap-2 text-sm">
          <input type="checkbox" :checked="model.enabled" :disabled="disabled" data-testid="detail-enabled" @change="update(draft => { draft.enabled = ($event.target as HTMLInputElement).checked })" />
          {{ t('managedModels.enabled') }}
        </label>
      </div>
      <div v-if="platform === 'anthropic'" class="grid gap-4 sm:grid-cols-2">
        <label class="model-field">
          {{ t('managedModels.displayName') }}
          <input class="input w-full" :value="model.display_name" :disabled="disabled" data-testid="display-name" @input="update(draft => { draft.display_name = ($event.target as HTMLInputElement).value })" />
        </label>
        <label class="model-field">
          {{ t('managedModels.context') }}
          <input class="input w-full" type="number" min="1" step="1" :value="model.capabilities?.context_window" :disabled="disabled" data-testid="context-window" @input="numberField('context_window', $event)" />
        </label>
        <label v-for="field in numericFields" :key="field" class="model-field">
          {{ t(`managedModels.${field}`) }}
          <input type="number" step="1" :min="field === 'thinking_fallback_budget' ? 1024 : 1" class="input w-full" :value="model.params[field]" :disabled="disabled" :data-testid="`param-${field}`" @input="numberField(field, $event)" />
        </label>
        <label class="model-field">
          {{ t('managedModels.effort') }}
          <select class="input w-full" :value="model.params.default_effort || ''" :disabled="disabled || !model.capabilities?.supports_effort" data-testid="default-effort" @change="update(draft => { draft.params.default_effort = ($event.target as HTMLSelectElement).value })">
            <option value="">{{ t('managedModels.inherit') }}</option>
            <option v-for="effort in ['low', 'medium', 'high', 'xhigh', 'max']" :key="effort" :value="effort">{{ effort }}</option>
          </select>
        </label>
        <label v-for="field in thinkingFields" :key="field" class="model-field">
          {{ t(`managedModels.${field}`) }}
          <select class="input w-full" :value="model.params[field]" :disabled="disabled" :data-testid="`param-${field}`" @change="update(draft => { draft.params[field] = ($event.target as HTMLSelectElement).value })">
            <option v-for="action in ['passthrough', 'strip', field === 'on_adaptive' ? 'convert_to_enabled' : 'convert_to_adaptive']" :key="action" :value="action">{{ t(`managedModels.${action}`) }}</option>
          </select>
        </label>
        <label class="model-field sm:col-span-2">
          {{ t('managedModels.aliases') }}
          <input class="input w-full font-mono text-xs" :value="model.aliases.join(', ')" :disabled="disabled" data-testid="model-aliases" @change="update(draft => { draft.aliases = commaList(($event.target as HTMLInputElement).value) })" />
        </label>
        <label class="model-field">
          {{ t('managedModels.beta1m') }}
          <select class="input w-full" :value="betaMode" :disabled="disabled" data-testid="beta-1m" @change="setBeta">
            <option value="inherit">{{ t('managedModels.inherit') }}</option>
            <option value="pass">{{ t('managedModels.passthrough') }}</option>
            <option value="strip">{{ t('managedModels.strip') }}</option>
          </select>
        </label>
        <label class="flex items-center gap-2 text-sm">
          <input type="checkbox" :checked="model.betas.allow_client" :disabled="disabled" data-testid="beta-allow-client" @change="update(draft => { draft.betas.allow_client = ($event.target as HTMLInputElement).checked })" />
          {{ t('managedModels.allowClientBetas') }}
        </label>
        <label v-for="field in betaLists" :key="field" class="model-field sm:col-span-2">
          {{ t(`managedModels.beta_${field}`) }}
          <input class="input w-full font-mono text-xs" :value="model.betas[field]?.join(', ') || ''" :disabled="disabled" :data-testid="`beta-${field}`" @change="update(draft => { draft.betas[field] = commaList(($event.target as HTMLInputElement).value) })" />
        </label>
      </div>
      <dl v-else-if="facts.length" class="grid min-w-0 gap-3 text-sm sm:grid-cols-2">
        <div v-for="fact in facts" :key="fact.key" class="min-w-0 rounded-xl bg-gray-50 p-3 dark:bg-dark-800">
          <dt class="text-gray-500">{{ t(`managedModels.${fact.key}`) }}</dt>
          <dd class="mt-1 break-words font-mono text-xs">{{ fact.value ?? '—' }}</dd>
        </div>
      </dl>
      <p v-else class="text-sm text-gray-500">{{ t('managedModels.noFacts') }}</p>
    </div>
    <template #footer>
      <div class="flex w-full flex-wrap items-center justify-between gap-3">
        <p class="text-xs text-gray-500">{{ t('managedModels.draftHint') }}</p>
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('managedModels.done') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<style scoped>
.model-field { @apply flex min-w-0 flex-col gap-1 text-sm; }
</style>
