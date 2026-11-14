<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ManagedModel, ModelMappings, ModelPolicy } from '../../api/managedModelTypes'
import ClaudeDefaultsFields from './ClaudeDefaultsFields.vue'
import ManagedModelDialog from './ManagedModelDialog.vue'
import ModelMappingsEditor from './ModelMappingsEditor.vue'
import { cloneDraft } from './draft'
import type { MappingDraftState } from './draft'
import { useManagedModelsI18n } from './useManagedModelsI18n'

const props = defineProps<{
  policy: ModelPolicy
  mappings: ModelMappings | null
  canManage: boolean
  canSync: boolean
  busy: boolean
  /** Enabled chat IDs from the parent's saved catalog, never the policy draft. */
  mappingTargets?: readonly string[]
}>()
const emit = defineEmits<{
  'update:policy': [ModelPolicy]
  'update:mappings': [ModelMappings]
  'mapping-draft-state': [MappingDraftState]
  'save-policy': []
  'save-mappings': []
  'reset-policy': []
  'open-sync': []
}>()
const { t, locale } = useManagedModelsI18n()
const draft = ref(cloneDraft(props.policy))
const search = ref('')
const editingKey = ref<string | null>(null)
const disabled = computed(() => !props.canManage || props.busy)
watch(() => props.policy, value => { draft.value = cloneDraft(value) }, { deep: true })
watch(() => props.policy.platform, () => { editingKey.value = null; search.value = '' })
const cards = computed(() => {
  const query = search.value.trim().toLowerCase()
  return Object.entries(draft.value.models)
    .filter(([, model]) => `${model.id} ${model.display_name} ${model.aliases.join(' ')}`.toLowerCase().includes(query))
    .sort(([, a], [, b]) => a.sort - b.sort || a.id.localeCompare(b.id))
})
const chatCards = computed(() => cards.value.filter(([, model]) => model.kind !== 'image'))
const imageCards = computed(() => cards.value.filter(([, model]) => model.kind === 'image'))
const editing = computed(() => editingKey.value == null ? undefined : draft.value.models[editingKey.value])
// The parent owns the saved snapshot. Missing candidates must not be inferred
// from a v-model policy that may already contain unsaved enabled changes.
const mappingTargets = computed(() => props.mappingTargets ?? [])

function publishPolicy(value: ModelPolicy) {
  if (disabled.value) return
  draft.value = cloneDraft(value)
  emit('update:policy', cloneDraft(value))
}
function updateModel(key: string, edit: (model: ManagedModel) => void) {
  if (disabled.value || !draft.value.models[key]) return
  const value = cloneDraft(draft.value)
  edit(value.models[key])
  publishPolicy(value)
}
function replaceModel(model: ManagedModel) {
  if (editingKey.value == null) return
  const value = cloneDraft(draft.value)
  value.models[editingKey.value] = cloneDraft(model)
  publishPolicy(value)
}
function publishMappings(value: ModelMappings) {
  if (!disabled.value && props.policy.platform === 'openai') emit('update:mappings', cloneDraft(value))
}
function action(event: 'save-policy' | 'save-mappings' | 'reset-policy' | 'open-sync') {
  if (disabled.value || (event === 'open-sync' && !props.canSync)) return
  if (event === 'save-policy') emit('save-policy')
  else if (event === 'save-mappings') emit('save-mappings')
  else if (event === 'reset-policy') emit('reset-policy')
  else emit('open-sync')
}
function contextOf(model: ManagedModel): string {
  const value = Number(model.catalog?.context_window ?? model.capabilities?.context_window)
  return Number.isFinite(value) && value > 0
    ? new Intl.NumberFormat(locale.value, { notation: 'compact', maximumFractionDigits: 1 }).format(value)
    : '—'
}
</script>

<template>
  <section class="min-w-0 space-y-5" :aria-label="t('managedModels.title')" :aria-busy="busy">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div class="min-w-0">
        <h2 class="text-lg font-semibold">{{ t('managedModels.title') }} · {{ policy.platform === 'anthropic' ? 'Claude' : 'GPT' }}</h2>
        <p class="mt-1 break-words text-xs text-gray-500">{{ t('managedModels.modelCount', { count: cards.length }) }} · {{ t('managedModels.source') }}: {{ policy.source || '—' }}</p>
        <p v-if="policy.updated_at" class="mt-1 text-xs text-gray-500">{{ t('managedModels.updated') }}: <time :datetime="policy.updated_at">{{ policy.updated_at }}</time></p>
      </div>
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-secondary" :disabled="disabled || !canSync" data-testid="open-sync" @click="action('open-sync')">{{ t('managedModels.syncCatalog') }}</button>
        <button type="button" class="btn btn-secondary" :disabled="disabled" data-testid="reset-policy" @click="action('reset-policy')">{{ t('managedModels.reset') }}</button>
        <button type="button" class="btn btn-primary" :disabled="disabled" data-testid="save-policy" @click="action('save-policy')">{{ t('managedModels.savePolicy') }}</button>
      </div>
    </header>
    <label class="flex w-full flex-col gap-1 text-sm sm:max-w-md">
      {{ t('managedModels.search') }}
      <input v-model="search" type="search" class="input w-full" data-testid="model-search" />
    </label>
    <p v-if="!cards.length" role="status" class="rounded-xl bg-gray-50 p-5 text-sm text-gray-500 dark:bg-dark-800">{{ t('managedModels.empty') }}</p>
    <section v-for="group in [{ kind: 'chat', rows: chatCards }, { kind: 'image', rows: imageCards }]" v-show="group.rows.length" :key="group.kind" class="space-y-3">
      <h3 class="font-semibold">{{ t(`managedModels.${group.kind}`) }}</h3>
      <div class="grid min-w-0 gap-4 sm:grid-cols-2 xl:grid-cols-3">
        <article v-for="[key, model] in group.rows" :key="key" class="flex min-w-0 flex-col gap-3 rounded-2xl border border-gray-200 p-4 dark:border-dark-700" :data-model-id="model.id">
          <div class="flex flex-wrap items-start justify-between gap-2">
            <div class="min-w-0 flex-1">
              <h4 class="break-words font-semibold">{{ model.display_name || model.id }}</h4>
              <code class="break-all text-xs text-gray-500">{{ model.id }}</code>
            </div>
            <label class="flex shrink-0 items-center gap-2 text-sm">
              <input type="checkbox" :checked="model.enabled" :disabled="disabled" data-testid="model-enabled" @change="updateModel(key, value => { value.enabled = ($event.target as HTMLInputElement).checked })" />
              <span class="sr-only">{{ model.display_name || model.id }} </span>{{ t('managedModels.enabled') }}
            </label>
          </div>
          <dl class="flex flex-wrap gap-3 text-xs text-gray-500">
            <div><dt class="inline">{{ t('managedModels.context') }}: </dt><dd class="inline">{{ contextOf(model) }}</dd></div>
            <div v-if="model.params.default_effort"><dt class="inline">{{ t('managedModels.effort') }}: </dt><dd class="inline">{{ model.params.default_effort }}</dd></div>
          </dl>
          <button type="button" class="btn btn-secondary mt-auto self-start" :aria-label="`${t('managedModels.details')} ${model.id}`" data-testid="model-details-open" @click="editingKey = key">{{ t('managedModels.details') }}</button>
        </article>
      </div>
    </section>
    <ClaudeDefaultsFields v-if="policy.platform === 'anthropic'" :policy="draft" :disabled="disabled" @update:policy="publishPolicy" />
    <ModelMappingsEditor v-if="policy.platform === 'openai' && mappings" :mappings="mappings" :targets="mappingTargets" :disabled="disabled" @update:mappings="publishMappings" @draft-state="emit('mapping-draft-state', $event)" @save="action('save-mappings')" />
    <ManagedModelDialog v-if="editing" :model="editing" :platform="policy.platform" :disabled="disabled" @update:model="replaceModel" @close="editingKey = null" />
  </section>
</template>
