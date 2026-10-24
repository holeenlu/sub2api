<template>
  <div class="flex items-center gap-3">
    <button class="btn btn-secondary" :disabled="disabled || exporting" data-test="export-keys" @click="showExport = true">
      <Icon name="download" size="md" class="mr-2" />
      {{ t(selectedIds.length ? 'keys.transfer.exportSelected' : 'keys.transfer.exportFiltered') }}
    </button>
    <button class="btn btn-secondary" data-test="import-keys" @click="showImport = true">
      <Icon name="upload" size="md" class="mr-2" />
      {{ t('keys.transfer.import') }}
    </button>
  </div>
  <ConfirmDialog
    :show="showExport"
    :title="t('keys.transfer.exportTitle')"
    :message="t('keys.transfer.exportWarning')"
    :confirm-text="t('keys.transfer.exportConfirm')"
    @confirm="exportKeys"
    @cancel="showExport = false"
  >
    <p class="text-sm text-gray-500 dark:text-gray-400">
      {{ t(selectedIds.length ? 'keys.transfer.selection' : 'keys.transfer.filtered', { count: selectedIds.length }) }}
    </p>
  </ConfirmDialog>
  <BaseDialog
    :show="showImport"
    :title="t('keys.transfer.import')"
    width="normal"
    :close-on-escape="!importing"
    :show-close-button="!importing"
    @close="closeImport"
  >
    <div class="space-y-4">
      <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('keys.transfer.importHint') }}</p>
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('keys.transfer.limits') }}</p>
      <label class="block text-sm font-medium">
        {{ t('keys.transfer.file') }}
        <input type="file" accept=".json,application/json" class="input mt-2 w-full" :disabled="importing" data-test="key-data-file" @change="readFile" />
      </label>
      <p v-if="fileName" class="break-all text-sm text-gray-500">{{ fileName }}</p>
      <p v-if="data" class="text-sm" data-test="key-data-preview">{{ t('keys.transfer.preview', { count: data.keys.length }) }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <div v-if="result" role="status" class="space-y-2 text-sm" data-test="key-data-result">
        <p>{{ t('keys.transfer.result', result) }}</p>
        <ul v-if="result.errors.length" class="max-h-48 space-y-1 overflow-auto text-red-600 dark:text-red-400">
          <li v-for="item in result.errors" :key="item.index">{{ t('keys.transfer.rowError', item) }}</li>
        </ul>
      </div>
    </div>
    <template #footer>
      <button class="btn btn-secondary" :disabled="importing" @click="closeImport">{{ t('common.close') }}</button>
      <button class="btn btn-primary" :disabled="!data || reading || importing || result?.failed === 0" data-test="submit-key-import" @click="importKeys">
        {{ t(importing ? 'keys.transfer.importing' : 'keys.transfer.startImport') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import { keysAPI } from '@/api'
import type { ApiKeyData, ApiKeyFilters, ApiKeyImportResult } from '@/api/keys'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{ selectedIds: number[]; filters: ApiKeyFilters; disabled?: boolean }>()
const emit = defineEmits<{ imported: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const showExport = ref(false)
const exporting = ref(false)
const showImport = ref(false)
const importing = ref(false)
const reading = ref(false)
const fileName = ref('')
const data = ref<ApiKeyData | null>(null)
const error = ref('')
const result = ref<ApiKeyImportResult | null>(null)
let readVersion = 0

const exportKeys = async () => {
  if (exporting.value) return
  showExport.value = false
  exporting.value = true
  try {
    const backup = await keysAPI.exportData(props.filters, props.selectedIds)
    if (!backup.keys.length) {
      appStore.showError(t('keys.transfer.empty'))
      return
    }
    const blob = new Blob([JSON.stringify(backup, null, 2)], { type: 'application/json' })
    if (blob.size > 5 * 1024 * 1024) throw new Error('size')
    saveAs(blob, `api-keys-${new Date().toISOString().replace(/[:.]/g, '-')}.json`)
    appStore.showSuccess(t('keys.transfer.exported', { count: backup.keys.length }))
  } catch {
    appStore.showError(t('keys.transfer.exportFailed'))
  } finally {
    exporting.value = false
  }
}

const closeImport = () => {
  if (importing.value) return
  readVersion++
  showImport.value = false
  reading.value = false
  data.value = null
  result.value = null
  error.value = ''
  fileName.value = ''
}

const readFile = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  input.value = ''
  const version = ++readVersion
  fileName.value = file.name
  data.value = null
  result.value = null
  error.value = ''
  reading.value = true
  try {
    if (file.size > 5 * 1024 * 1024) throw new Error('size')
    const parsed = JSON.parse((await file.text()).replace(/^\uFEFF/, ''))
    if (version !== readVersion) return
    if (parsed?.type !== 'sub2api-keys' || parsed.version !== 1 || !Array.isArray(parsed.keys) || !parsed.keys.length || parsed.keys.length > 1000) throw new Error('format')
    data.value = parsed
  } catch {
    if (version === readVersion) error.value = t('keys.transfer.invalidFile')
  } finally {
    if (version === readVersion) reading.value = false
  }
}

const importKeys = async () => {
  if (!data.value || importing.value) return
  importing.value = true
  error.value = ''
  result.value = null
  try {
    result.value = await keysAPI.importData(data.value)
    if (result.value.created) emit('imported')
  } catch {
    error.value = t('keys.transfer.importFailed')
  } finally {
    importing.value = false
  }
}
</script>
