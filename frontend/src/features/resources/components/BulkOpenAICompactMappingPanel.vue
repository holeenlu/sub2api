<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import type { ModelMappingEntry } from '../model/modelRestrictions'

const props = defineProps<{
  selected: boolean
  mappings: readonly Readonly<ModelMappingEntry>[]
}>()
const emit = defineEmits<{
  'update:selected': [value: boolean]
  'update:mappings': [value: ModelMappingEntry[]]
}>()
const { t } = useI18n()

function updateMapping(index: number, field: keyof ModelMappingEntry, event: Event) {
  if (!props.selected || !(event.target instanceof HTMLInputElement)) return
  const value = event.target.value
  emit('update:mappings', props.mappings.map((mapping, row) =>
    row === index ? { ...mapping, [field]: value } : { ...mapping }))
}

function addMapping() {
  if (props.selected) emit('update:mappings', [...props.mappings, { from: '', to: '' }])
}

function removeMapping(index: number) {
  if (props.selected) emit('update:mappings', props.mappings.filter((_, row) => row !== index))
}
</script>

<template>
  <!-- OpenAI Compact model mapping -->
  <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
    <div class="mb-3 flex items-center justify-between">
      <div class="flex-1 pr-4">
        <label
          id="bulk-edit-openai-compact-model-mapping-label"
          class="input-label mb-0"
          for="bulk-edit-openai-compact-model-mapping-enabled"
        >
          {{ t('admin.accounts.openai.compactModelMapping') }}
        </label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.openai.compactModelMappingDesc') }}
        </p>
      </div>
      <input
        :checked="selected"
        @change="emit('update:selected', !selected)"
        id="bulk-edit-openai-compact-model-mapping-enabled"
        type="checkbox"
        aria-controls="bulk-edit-openai-compact-model-mapping"
        class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
      />
    </div>
    <fieldset
      id="bulk-edit-openai-compact-model-mapping"
      :disabled="!selected"
      aria-labelledby="bulk-edit-openai-compact-model-mapping-label"
      :class="!selected && 'pointer-events-none opacity-50'"
    >
      <div v-if="mappings.length > 0" class="mb-3 space-y-2">
        <div
          v-for="(mapping, index) in mappings"
          :key="index"
          class="flex items-center gap-2"
        >
          <input
            :value="mapping.from"
            :aria-label="t('admin.accounts.fromModel')"
            @input="updateMapping(index, 'from', $event)"
            type="text"
            class="input flex-1"
            :placeholder="t('admin.accounts.fromModel')"
            data-testid="bulk-edit-openai-compact-model-mapping-input"
          />
          <span class="text-gray-400">→</span>
          <input
            :value="mapping.to"
            :aria-label="t('admin.accounts.toModel')"
            @input="updateMapping(index, 'to', $event)"
            type="text"
            class="input flex-1"
            :placeholder="t('admin.accounts.toModel')"
            data-testid="bulk-edit-openai-compact-model-mapping-input"
          />
          <button
            type="button"
            class="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
            :aria-label="t('common.delete')"
            data-testid="bulk-edit-openai-compact-model-mapping-remove"
            @click="removeMapping(index)"
          >
            <Icon name="trash" size="sm" />
          </button>
        </div>
      </div>
      <button
        type="button"
        class="mb-3 w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-gray-600 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400 dark:hover:text-gray-300"
        data-testid="bulk-edit-openai-compact-model-mapping-add"
        @click="addMapping"
      >
        + {{ t('admin.accounts.addMapping') }}
      </button>
    </fieldset>
  </div>
</template>
