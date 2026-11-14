<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { ModelMappings } from '../../api/managedModelTypes'
import { cloneDraft } from './draft'
import type { MappingDraftState } from './draft'
import { useManagedModelsI18n } from './useManagedModelsI18n'

interface MappingRow { key: number; alias: string; target: string; fixed: boolean }
const props = defineProps<{ mappings: ModelMappings; targets: readonly string[]; disabled: boolean }>()
const emit = defineEmits<{ 'update:mappings': [ModelMappings]; 'draft-state': [MappingDraftState]; save: [] }>()
const { t } = useManagedModelsI18n()
const rows = ref<MappingRow[]>([])
const fixedAlias = 'codex-auto-review'
let nextKey = 0
let lastPublished = ''
watch(() => props.mappings, value => {
  // Keep incomplete new rows and keyboard focus when the parent echoes v-model.
  // A different server snapshot replaces the local row drafts.
  if (JSON.stringify(value) === lastPublished) return
  rows.value = Object.entries(value.mappings).map(([alias, target]) => ({ key: nextKey++, alias, target, fixed: alias === fixedAlias }))
    .sort((a, b) => Number(b.fixed) - Number(a.fixed))
  lastPublished = ''
}, { deep: true, immediate: true })

const invalidAlias = computed(() => {
  const aliases = rows.value.map(row => row.alias.toLowerCase())
  return rows.value.some(row => !row.alias || /\s/.test(row.alias) || (!row.fixed && row.alias.toLowerCase() === fixedAlias))
    || new Set(aliases).size !== aliases.length
})
const invalidTarget = computed(() => rows.value.some(row => !row.target.trim()))
const invalid = computed(() => invalidAlias.value || invalidTarget.value)
// Compare the actual row list, retaining duplicate/empty aliases. Projecting it
// to an object here would hide exactly the local drafts the parent must guard.
function canonicalRows(entries: [string, string][]): string {
  return JSON.stringify(entries.sort(([a, at], [b, bt]) => a.localeCompare(b) || at.localeCompare(bt)))
}
const dirty = computed(() => canonicalRows(rows.value.map(row => [row.alias, row.target]))
  !== canonicalRows(Object.entries(props.mappings.mappings)))
watch([dirty, invalid], ([dirty, invalid]) => {
  emit('draft-state', { dirty, valid: !invalid })
}, { immediate: true })
onBeforeUnmount(() => emit('draft-state', { dirty: false, valid: true }))

function publish() {
  if (props.disabled || invalid.value) return
  const value = { ...cloneDraft(props.mappings), mappings: Object.fromEntries(rows.value.map(row => [row.alias, row.target])) }
  lastPublished = JSON.stringify(value)
  emit('update:mappings', value)
}
function edit(row: MappingRow, field: 'alias' | 'target', event: Event) {
  if (props.disabled || (row.fixed && field === 'alias')) return
  row[field] = (event.target as HTMLInputElement | HTMLSelectElement).value
  publish()
}
function add() {
  if (props.disabled) return
  rows.value.push({ key: nextKey++, alias: '', target: props.targets[0] || '', fixed: false })
}
function remove(row: MappingRow) {
  if (props.disabled || row.fixed) return
  rows.value = rows.value.filter(item => item.key !== row.key)
  publish()
}
function resetTarget(row: MappingRow) {
  if (props.disabled || !row.fixed) return
  row.target = 'gpt-6-luna'
  publish()
}
function save() {
  if (!props.disabled && !invalid.value) emit('save')
}
</script>

<template>
  <section class="min-w-0 space-y-4 rounded-2xl border border-gray-200 p-4 dark:border-dark-700 sm:p-5" data-testid="mappings-editor">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h3 class="font-semibold">{{ t('managedModels.mappingTitle') }}</h3>
      <button type="button" class="btn btn-primary" :disabled="disabled || invalid" data-testid="save-mappings" @click="save">{{ t('managedModels.saveMappings') }}</button>
    </div>
    <p class="text-xs text-gray-500">{{ t('managedModels.fixedAliasHint') }}</p>
    <div v-for="row in rows" :key="row.key" class="grid min-w-0 gap-2 rounded-xl bg-gray-50 p-3 dark:bg-dark-800 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end" :data-fixed="row.fixed" data-testid="mapping-row">
      <label class="flex min-w-0 flex-col gap-1 text-sm">
        {{ t('managedModels.alias') }}
        <input class="input w-full min-w-0 font-mono text-xs" :value="row.alias" :disabled="disabled || row.fixed" :aria-invalid="invalid || undefined" data-testid="mapping-alias" @input="edit(row, 'alias', $event)" />
      </label>
      <label class="flex min-w-0 flex-col gap-1 text-sm">
        {{ t('managedModels.target') }}
        <select class="input w-full min-w-0 font-mono text-xs" :value="row.target" :disabled="disabled" :aria-invalid="!row.target.trim() || undefined" data-testid="mapping-target" @change="edit(row, 'target', $event)">
          <option v-if="!targets.includes(row.target)" :value="row.target">{{ row.target || '—' }} — {{ t('managedModels.unavailable') }}</option>
          <option v-for="target in targets" :key="target" :value="target">{{ target }}</option>
        </select>
      </label>
      <button v-if="row.fixed" type="button" class="btn btn-secondary" :disabled="disabled" data-testid="reset-mapping-target" @click="resetTarget(row)">{{ t('managedModels.resetTarget') }}</button>
      <button v-else type="button" class="btn btn-secondary" :disabled="disabled" data-testid="remove-mapping" @click="remove(row)">{{ t('managedModels.remove') }}</button>
    </div>
    <p v-if="invalidAlias" role="alert" class="text-sm text-red-600">{{ t('managedModels.invalidAlias') }}</p>
    <p v-if="invalidTarget" role="alert" class="text-sm text-red-600">{{ t('managedModels.invalidTarget') }}</p>
    <button type="button" class="btn btn-secondary" :disabled="disabled" data-testid="add-mapping" @click="add">{{ t('managedModels.addMapping') }}</button>
  </section>
</template>
