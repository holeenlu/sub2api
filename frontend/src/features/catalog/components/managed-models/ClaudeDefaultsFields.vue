<script setup lang="ts">
import type { ModelPolicy } from '../../api/managedModelTypes'
import { cloneDraft, commaList } from './draft'
import { useManagedModelsI18n } from './useManagedModelsI18n'

const props = defineProps<{ policy: ModelPolicy; disabled: boolean }>()
const emit = defineEmits<{ 'update:policy': [ModelPolicy] }>()
const { t } = useManagedModelsI18n()
const numericFields = ['max_tokens', 'thinking_fallback_budget'] as const
const booleanFields = ['enabled', 'normalize_thinking', 'strip_context_1m'] as const

function update(edit: (draft: ModelPolicy) => void) {
  if (props.disabled) return
  const draft = cloneDraft(props.policy)
  edit(draft)
  emit('update:policy', draft)
}
function numberField(field: typeof numericFields[number], event: Event) {
  const input = event.target as HTMLInputElement
  if (input.value === '' || !input.validity.valid || !Number.isFinite(input.valueAsNumber)) return
  update(draft => { draft.defaults[field] = input.valueAsNumber })
}
</script>

<template>
  <details class="rounded-2xl border border-gray-200 p-4 dark:border-dark-700 sm:p-5">
    <summary class="cursor-pointer font-semibold">{{ t('managedModels.advanced') }}</summary>
    <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <label class="flex min-w-0 flex-col gap-1 text-sm">
        {{ t('managedModels.catalogMode') }}
        <select class="input w-full" :value="policy.catalog_mode" :disabled="disabled" data-testid="catalog-mode" @change="update(draft => { draft.catalog_mode = ($event.target as HTMLSelectElement).value })">
          <option value="policy_only">{{ t('managedModels.policyCatalog') }}</option>
          <option value="worker_intersect_policy">{{ t('managedModels.policyCatalogLegacy') }}</option>
          <option value="worker_only">{{ t('managedModels.upstreamCatalog') }}</option>
        </select>
      </label>
      <label v-for="field in numericFields" :key="field" class="flex min-w-0 flex-col gap-1 text-sm">
        {{ t(`managedModels.default_${field}`) }}
        <input type="number" step="1" :min="field === 'thinking_fallback_budget' ? 1024 : 1" class="input w-full" :value="policy.defaults[field]" :disabled="disabled" :data-testid="`default-${field}`" @input="numberField(field, $event)" />
      </label>
      <label v-for="field in booleanFields" :key="field" class="flex items-center gap-2 text-sm">
        <input type="checkbox" :checked="policy.defaults[field]" :disabled="disabled" :data-testid="`default-${field}`" @change="update(draft => { draft.defaults[field] = ($event.target as HTMLInputElement).checked })" />
        {{ t(`managedModels.default_${field}`) }}
      </label>
      <label class="flex min-w-0 flex-col gap-1 text-sm sm:col-span-2 lg:col-span-3">
        {{ t('managedModels.contextWhitelist') }}
        <input class="input w-full font-mono text-xs" :value="policy.defaults.context_1m_whitelist.join(', ')" :disabled="disabled" data-testid="context-whitelist" @change="update(draft => { draft.defaults.context_1m_whitelist = commaList(($event.target as HTMLInputElement).value) })" />
      </label>
    </div>
  </details>
</template>
