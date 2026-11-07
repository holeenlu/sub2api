<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { useKeyTransfer, type KeyTransferProps } from '../queries/useKeyTransfer'
const props = defineProps<KeyTransferProps>()
const emit = defineEmits<{ imported: [] }>()
const { t } = useI18n()
const { showExport, exporting, showImport, importing, reading, fileName, data, error, result,
  exportKeys, closeImport, readFile, importKeys } = useKeyTransfer(props, () => emit('imported'))
</script>
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

