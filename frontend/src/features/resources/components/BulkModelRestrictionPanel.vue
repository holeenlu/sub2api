<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AccountPlatform } from '../api/platformTypes'
import type { BulkCredentialDraft } from '../model/bulkCredentialPatch'
import type { ModelMappingEntry } from '../model/modelRestrictions'
import type { ResourceEditorServices } from '../services'
import ModelWhitelistSelector from './ModelWhitelistSelector.vue'

type ModelRestriction = NonNullable<BulkCredentialDraft['modelRestriction']>

const props = defineProps<{
  selected: boolean
  mode: ModelRestriction['mode']
  models: readonly string[]
  mappings: readonly Readonly<ModelMappingEntry>[]
  platforms: readonly AccountPlatform[]
  presets: ReadonlyArray<Readonly<ReturnType<ResourceEditorServices['modelPresets']>[number]>>
  mixedReferences?: boolean
  disabledByPassthrough: boolean
}>()
const emit = defineEmits<{
  'update:selected': [value: boolean]
  'update:mode': [value: ModelRestriction['mode']]
  'update:models': [value: string[]]
  'update:mappings': [value: ModelMappingEntry[]]
  info: [message: string]
}>()
const { t } = useI18n()
const canEdit = computed(() => props.selected && !props.disabledByPassthrough && !props.mixedReferences)
// The existing selector accepts mutable rows; keep the parent's draft read-only.
const selectorModels = computed(() => [...props.models])
const selectorPlatforms = computed(() => [...props.platforms])

function updateMode(value: ModelRestriction['mode']) {
  if (canEdit.value) emit('update:mode', value)
}

function updateModels(value: string[]) {
  if (canEdit.value) emit('update:models', [...value])
}

function updateMapping(index: number, field: keyof ModelMappingEntry, event: Event) {
  if (!canEdit.value || !(event.target instanceof HTMLInputElement)) return
  const value = event.target.value
  emit('update:mappings', props.mappings.map((mapping, row) =>
    row === index ? { ...mapping, [field]: value } : { ...mapping }))
}

function addModelMapping() {
  if (!canEdit.value) return
  emit('update:mappings', [...props.mappings, { from: '', to: '' }])
}

function removeModelMapping(index: number) {
  if (!canEdit.value) return
  emit('update:mappings', props.mappings.filter((_, row) => row !== index))
}

function addPresetMapping(from: string, to: string) {
  if (!canEdit.value) return
  if (props.mappings.some(mapping => mapping.from === from)) {
    emit('info', t('admin.accounts.mappingExists', { model: from }))
    return
  }
  emit('update:mappings', [...props.mappings, { from, to }])
}
</script>

<template>
  <!-- Model restriction -->
  <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
    <div class="mb-3 flex items-center justify-between">
      <label
        id="bulk-edit-model-restriction-label"
        class="input-label mb-0"
        for="bulk-edit-model-restriction-enabled"
      >
        {{ t('admin.accounts.modelRestriction') }}
      </label>
      <input
        :checked="selected"
        :disabled="mixedReferences"
        @change="emit('update:selected', !selected)"
        id="bulk-edit-model-restriction-enabled"
        type="checkbox"
        aria-controls="bulk-edit-model-restriction-body"
        class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
      />
    </div>

    <p v-if="mixedReferences" class="mb-2 text-xs text-gray-500">{{ t('admin.accounts.modelReference.mixedPlatforms') }}</p>
    <fieldset
      id="bulk-edit-model-restriction-body"
      :disabled="!canEdit"
      :class="!selected && 'pointer-events-none opacity-50'"
      role="group"
      aria-labelledby="bulk-edit-model-restriction-label"
    >
      <div
        v-if="disabledByPassthrough"
        class="rounded-lg bg-amber-50 p-3 dark:bg-amber-900/20"
      >
        <p class="text-xs text-amber-700 dark:text-amber-400">
          {{ t('admin.accounts.openai.modelRestrictionDisabledByPassthrough') }}
        </p>
      </div>

      <template v-else>
        <!-- Mode Toggle -->
        <div class="mb-4 flex gap-2">
          <button
            type="button"
            :class="[
              'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
              mode === 'whitelist'
                ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
            ]"
            @click="updateMode('whitelist')"
          >
            <svg
              class="mr-1.5 inline h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            {{ t('admin.accounts.modelWhitelist') }}
          </button>
          <button
            type="button"
            :class="[
              'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
              mode === 'mapping'
                ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
            ]"
            @click="updateMode('mapping')"
          >
            <svg
              class="mr-1.5 inline h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4"
              />
            </svg>
            {{ t('admin.accounts.modelMapping') }}
          </button>
        </div>

        <!-- Whitelist Mode -->
        <div v-if="mode === 'whitelist'">
          <div class="mb-3 rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
            <p class="text-xs text-blue-700 dark:text-blue-400">
              <svg
                class="mr-1 inline h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                />
              </svg>
              {{ t('admin.accounts.selectAllowedModels') }}
            </p>
          </div>

          <ModelWhitelistSelector
            :disabled="!canEdit"
            :model-value="selectorModels"
            @update:model-value="updateModels"
            :platforms="selectorPlatforms"
          />

          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.selectedModels', { count: models.length }) }}
            <span v-if="models.length === 0">{{
              t('admin.accounts.supportsAllModels')
            }}</span>
          </p>
        </div>

        <!-- Mapping Mode -->
        <div v-else>
          <div class="mb-3 rounded-lg bg-purple-50 p-3 dark:bg-purple-900/20">
            <p class="text-xs text-purple-700 dark:text-purple-400">
              <svg
                class="mr-1 inline h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                />
              </svg>
              {{ t('admin.accounts.mapRequestModels') }}
            </p>
          </div>

          <!-- Model Mapping List -->
          <div v-if="mappings.length > 0" class="mb-3 space-y-2">
            <div
              v-for="(mapping, index) in mappings"
              :key="index"
              class="flex items-center gap-2"
            >
              <input
                :value="mapping.from"
                :aria-label="t('admin.accounts.requestModel')"
                @input="updateMapping(index, 'from', $event)"
                type="text"
                class="input flex-1"
                :placeholder="t('admin.accounts.requestModel')"
              />
              <svg
                class="h-4 w-4 flex-shrink-0 text-gray-400"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M14 5l7 7m0 0l-7 7m7-7H3"
                />
              </svg>
              <input
                :value="mapping.to"
                :aria-label="t('admin.accounts.actualModel')"
                @input="updateMapping(index, 'to', $event)"
                type="text"
                class="input flex-1"
                :placeholder="t('admin.accounts.actualModel')"
              />
              <button
                type="button"
                class="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                :aria-label="t('common.delete')"
                data-testid="bulk-model-mapping-remove"
                @click="removeModelMapping(index)"
              >
                <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                  />
                </svg>
              </button>
            </div>
          </div>

          <button
            type="button"
            class="mb-3 w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-gray-600 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400 dark:hover:text-gray-300"
            data-testid="bulk-model-mapping-add"
            @click="addModelMapping"
          >
            <svg
              class="mr-1 inline h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M12 4v16m8-8H4"
              />
            </svg>
            {{ t('admin.accounts.addMapping') }}
          </button>

          <!-- Quick Add Buttons -->
          <div class="flex flex-wrap gap-2">
            <button
              v-for="preset in presets"
              :key="preset.label"
              type="button"
              :class="['rounded-lg px-3 py-1 text-xs transition-colors', preset.color]"
              data-testid="bulk-model-mapping-preset"
              @click="addPresetMapping(preset.from, preset.to)"
            >
              + {{ preset.label }}
            </button>
          </div>
        </div>
      </template>
    </fieldset>
  </div>
</template>
