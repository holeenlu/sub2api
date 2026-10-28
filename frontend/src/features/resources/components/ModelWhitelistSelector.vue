<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useResourceEditorServices } from '../services'
import { useModelReferences, type ModelReferenceProps } from '../queries/useModelReferences'
import { useClipboard } from '@/shared/lib/useClipboard'
import Icon from '@/shared/ui/icons/Icon.vue'

// Unknown legacy bindings must never serialize credentials onto the root element.
defineOptions({ inheritAttrs: false })
const props = defineProps<ModelReferenceProps>()
const emit = defineEmits<{
  'update:modelValue': [value: string[]]
  'busy-change': [busy: boolean]
  'draft-change': [dirty: boolean]
}>()
const { t } = useI18n()
const { ModelIcon } = useResourceEditorServices().components
const { copyToClipboard } = useClipboard()
const { value, options, selected, ready, saving, error, editable, draft, serverModels, refresh, toggle, inherit, reloadCurrent, adoptCurrent, mergeAndSave } = useModelReferences(props, models => emit('update:modelValue', models), key => t(`admin.accounts.modelReference.${key}`))
watch(saving, busy => emit('busy-change', busy), { immediate: true, flush: 'sync' })
watch(() => draft.value !== null, dirty => emit('draft-change', dirty), { immediate: true, flush: 'sync' })
onBeforeUnmount(() => {
  emit('busy-change', false)
  emit('draft-change', false)
})
const showDropdown = ref(false), searchQuery = ref('')
const filteredModels = computed(() => options.value.filter(model => `${model.id} ${model.display_name}`.toLowerCase().includes(searchQuery.value.trim().toLowerCase())))
</script>

<template>
  <div>
    <div class="relative mb-3">
      <div class="cursor-pointer rounded-lg border border-gray-300 bg-white px-3 py-2 dark:border-dark-500 dark:bg-dark-700" role="button" tabindex="0" :aria-expanded="showDropdown" @click="showDropdown = !showDropdown" @keydown.enter.prevent="showDropdown = !showDropdown" @keydown.space.prevent="showDropdown = !showDropdown">
        <div class="grid grid-cols-2 gap-1.5">
          <span v-for="model in value" :key="model" class="inline-flex min-w-0 items-center justify-between gap-1 rounded bg-gray-100 px-2 py-1 text-xs dark:bg-dark-600">
            <span class="flex min-w-0 items-center gap-1"><ModelIcon :model="model" size="14px" /><span class="truncate">{{ model }}</span></span>
            <button type="button" :aria-label="`${t('common.remove')} ${model}`" :disabled="saving || !ready || !editable" @click.stop="toggle(model)"><Icon name="x" size="xs" /></button>
          </span>
        </div>
        <span class="text-xs text-gray-500">{{ value.length || emptyMode === 'explicit' ? t('admin.accounts.modelCount', { count: value.length }) : t('admin.accounts.modelReference.inherit') }}</span>
      </div>
      <div v-if="showDropdown" class="absolute left-0 right-0 top-full z-50 mt-1 rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-600 dark:bg-dark-700">
        <input v-model="searchQuery" class="input w-full text-sm" :placeholder="t('admin.accounts.searchModels')" />
        <div class="max-h-52 overflow-auto">
          <div v-for="model in filteredModels" :key="model.id" data-testid="model-option" class="flex items-center">
            <button type="button" data-testid="select-model" :disabled="saving || !ready || !editable || (!model.enabled && !value.includes(model.id))" :aria-pressed="selected(model.id)" class="flex min-w-0 flex-1 items-center gap-2 px-3 py-2 text-left text-sm disabled:opacity-50" @click="toggle(model.id)">
              <span aria-hidden="true">{{ selected(model.id) ? '☑' : '☐' }}</span>
              <ModelIcon :model="model.id" size="18px" />
              <span class="min-w-0 break-all">{{ model.display_name || model.id }}<span v-if="model.display_name !== model.id" class="block text-xs text-gray-500">{{ model.id }}</span></span>
              <span v-if="!model.enabled" class="text-xs text-gray-500">{{ t('admin.accounts.modelReference.unavailable') }}</span>
            </button>
            <button type="button" data-testid="copy-model-id" class="mr-2 p-1.5" :aria-label="`${t('common.copy')} ${model.id}`" @click="copyToClipboard(model.id)"><Icon name="copy" size="sm" /></button>
          </div>
          <p v-if="!filteredModels.length" class="px-3 py-4 text-sm text-gray-500">{{ t('admin.accounts.noMatchingModels') }}</p>
        </div>
      </div>
    </div>
    <p class="mb-2 text-xs text-gray-500">{{ t(emptyMode === 'explicit' ? 'admin.accounts.modelReference.explicitHint' : 'admin.accounts.modelReference.hint') }}</p>
    <div class="flex gap-2">
      <button type="button" data-testid="refresh-catalog" :disabled="saving" class="btn btn-secondary" @click="refresh">{{ t('common.refresh') }}</button>
      <button v-if="emptyMode !== 'explicit'" type="button" data-testid="inherit-models" :disabled="saving || !ready || !editable" class="btn btn-secondary" @click="inherit">{{ t('admin.accounts.modelReference.inherit') }}</button>
    </div>
    <p v-if="error" role="alert" class="mt-2 text-sm text-red-600">{{ error }}</p>
    <button v-if="draft !== null && error" type="button" data-testid="reload-current-models" :disabled="saving || !editable" class="btn btn-secondary" @click="reloadCurrent">{{ t('admin.accounts.modelReference.reloadCurrent') }}</button>
    <div v-if="serverModels !== null" class="mt-2 space-y-2">
      <p class="text-sm">{{ t('admin.accounts.modelReference.reloadUnconfirmed') }}</p>
      <p data-testid="server-models" class="break-all text-xs">{{ t('admin.accounts.modelReference.serverModels') }}: {{ serverModels.length ? serverModels.join(', ') : t('admin.accounts.modelReference.inherit') }}</p>
      <button type="button" data-testid="adopt-current-models" :disabled="saving || !editable" class="btn btn-secondary" @click="adoptCurrent">{{ t('admin.accounts.modelReference.adoptCurrent') }}</button>
      <button type="button" data-testid="merge-model-draft" :disabled="saving || !ready || !editable" class="btn btn-secondary" @click="mergeAndSave">{{ t('admin.accounts.modelReference.mergeDraft') }}</button>
    </div>
  </div>
</template>
