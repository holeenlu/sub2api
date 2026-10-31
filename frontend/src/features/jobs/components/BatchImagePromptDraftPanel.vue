<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'

// Display summaries deliberately exclude uploaded image data and submission credentials.
interface PromptSummary {
  readonly localId: string
  readonly customId: string
  readonly prompt: string
  readonly outputCount: number
  readonly referenceCount: number
}

const props = defineProps<{
  prompt: string
  customId: string
  outputCount: number
  referenceNames: readonly string[]
  rows: readonly PromptSummary[]
  maxOutputsPerItem: number
  maxOutputsPerJob: number
  referenceLimit: number
}>()

const emit = defineEmits<{
  'update:prompt': [value: string]
  'update:customId': [value: string]
  'update:outputCount': [value: number]
  'select-reference-files': [files: File[]]
  'remove-reference': [index: number]
  'add-prompt': []
  'remove-prompt': [localId: string]
}>()

const { t } = useI18n()
const promptId = useId()
const outputCountOptions = computed(() =>
  Array.from({ length: props.maxOutputsPerItem }, (_, index) => index + 1),
)

function selectReferenceFiles(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  // Reset the browser control so selecting the same file again still emits a change.
  // Reading, validation and session cancellation belong to the page.
  input.value = ''
  if (files.length) emit('select-reference-files', files)
}
</script>

<template>
  <section class="space-y-3">
    <div class="flex items-center justify-between gap-3">
      <label :for="promptId" class="input-label mb-0">Prompt</label>
      <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('batchImage.create.promptAdded', { count: rows.length }) }}</span>
    </div>
    <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
      <textarea
        :id="promptId"
        :value="prompt"
        rows="3"
        class="h-[76px] w-full resize-y rounded-md border border-gray-300 px-3 py-2 text-sm leading-5 outline-none focus:border-primary-500 focus:ring-2 focus:ring-primary-100 dark:border-dark-600 dark:bg-dark-900 dark:text-gray-100 dark:focus:border-primary-500 dark:focus:ring-primary-900/40"
        :placeholder="t('batchImage.create.promptPlaceholder')"
        @input="emit('update:prompt', ($event.target as HTMLTextAreaElement).value)"
      />
      <div class="mt-2 grid gap-2 md:grid-cols-[minmax(0,1fr)_112px_132px_112px] md:items-center">
        <input
          :value="customId"
          type="text"
          maxlength="255"
          class="input h-9 text-sm"
          :placeholder="t('batchImage.create.customIdPlaceholder')"
          :aria-label="t('batchImage.create.customIdPlaceholder')"
          @input="emit('update:customId', ($event.target as HTMLInputElement).value)"
        />
        <select
          :value="outputCount"
          class="batch-output-count-select input h-9 text-sm"
          :title="t('batchImage.create.outputCountPerPrompt')"
          :aria-label="t('batchImage.create.outputCountPerPrompt')"
          @change="emit('update:outputCount', Number(($event.target as HTMLSelectElement).value))"
        >
          <option v-for="count in outputCountOptions" :key="count" :value="count">
            {{ t('batchImage.create.outputCountOption', { n: count }, count) }}
          </option>
        </select>
        <label
          class="btn btn-secondary h-9 cursor-pointer justify-center text-sm focus-within:ring-2 focus-within:ring-primary-500/30"
          :class="referenceNames.length >= referenceLimit ? 'pointer-events-none opacity-60' : ''"
        >
          <Icon name="upload" size="sm" class="mr-1.5" />
          {{ t('batchImage.create.referenceImage') }}
          <input
            type="file"
            accept="image/png,image/jpeg,image/webp"
            multiple
            class="sr-only"
            :disabled="referenceNames.length >= referenceLimit"
            @change="selectReferenceFiles"
          />
        </label>
        <button type="button" class="btn btn-secondary h-9 justify-center whitespace-nowrap px-4 text-sm" :disabled="!prompt.trim()" @click="emit('add-prompt')">
          <Icon name="plus" size="sm" class="mr-1.5" />
          {{ t('common.add') }}
        </button>
      </div>
      <div v-if="referenceNames.length" class="mt-3 flex flex-wrap gap-2">
        <span
          v-for="(name, refIndex) in referenceNames"
          :key="`${name}-${refIndex}`"
          class="inline-flex max-w-full items-center gap-1 rounded-md border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-200"
        >
          <span class="max-w-[180px] truncate">{{ name }}</span>
          <button type="button" class="text-gray-400 hover:text-red-600" :title="t('batchImage.create.removeReferenceImage')" @click="emit('remove-reference', refIndex)">
            <Icon name="x" size="xs" />
          </button>
        </span>
      </div>
      <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
        {{ t('batchImage.create.limitsHint', { maxPerItem: maxOutputsPerItem, maxPerJob: maxOutputsPerJob, refLimit: referenceLimit }) }}
      </p>
    </div>
    <div v-if="rows.length" class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700">
      <div
        v-for="row in rows"
        :key="row.localId"
        class="flex items-center gap-3 border-b border-gray-100 px-3 py-2 last:border-b-0 dark:border-dark-700"
      >
        <span class="w-20 flex-shrink-0 font-mono text-xs text-gray-500 dark:text-gray-400">{{ row.customId }}</span>
        <p class="min-w-0 flex-1 truncate text-sm text-gray-800 dark:text-gray-100">{{ row.prompt }}</p>
        <span v-if="row.outputCount > 1" class="flex-shrink-0 text-xs text-gray-500 dark:text-gray-400">
          x{{ row.outputCount }}
        </span>
        <span v-if="row.referenceCount" class="flex-shrink-0 text-xs text-gray-500 dark:text-gray-400">
          {{ t('batchImage.create.referenceCount', { n: row.referenceCount }, row.referenceCount) }}
        </span>
        <button type="button" class="btn-ghost btn-icon flex-shrink-0 text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20" :title="t('common.delete')" @click="emit('remove-prompt', row.localId)">
          <Icon name="trash" size="sm" />
        </button>
      </div>
    </div>
    <div v-else class="rounded-lg border border-dashed border-gray-200 px-3 py-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
      {{ t('batchImage.create.noPrompts') }}
    </div>
  </section>
</template>

<style scoped>
.batch-output-count-select {
  height: 36px;
  min-height: 36px;
  padding-top: 0;
  padding-bottom: 0;
  padding-left: 14px;
  padding-right: 34px;
  line-height: 36px;
}
</style>
