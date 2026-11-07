import { ref, watch, onScopeDispose } from 'vue'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import { keysAPI, type ApiKeyFilters, type ApiKeyImportData, type ApiKeyImportResult } from '../api/keys'
import { parseKeyImport, MAX_KEY_IMPORT_BYTES } from '../model/keyTransfer'
import { useKeysServices } from '../services'
import { useKeysRequest } from './useKeysRequest'
export interface KeyTransferProps { selectedIds: number[]; filters: ApiKeyFilters; disabled?: boolean }
export function useKeyTransfer(props: KeyTransferProps, imported: () => void) {
  const { t } = useI18n()
  const services = useKeysServices()
  const exportRequest = useKeysRequest(services)
  const importRequest = useKeysRequest(services)
  const fileRequest = useKeysRequest(services)
  const showExport = ref(false), exporting = ref(false), showImport = ref(false), importing = ref(false), reading = ref(false)
  const fileName = ref(''), error = ref('')
  const data = ref<ApiKeyImportData | null>(null)
  const result = ref<ApiKeyImportResult | null>(null)
  const exportTarget = () => JSON.stringify([
    [...props.selectedIds].sort((left, right) => left - right),
    Object.entries(props.filters).sort(([left], [right]) => left.localeCompare(right))
  ])
  watch(exportTarget, () => {
    exportRequest.cancel(); showExport.value = false; exporting.value = false
  }, { flush: 'sync' })
  async function exportKeys(): Promise<void> {
    if (!showExport.value || exporting.value || props.disabled) return
    const ticket = exportRequest.begin()
    if (!ticket.isCurrent()) return
    showExport.value = false; exporting.value = true
    try {
      const backup = await keysAPI.exportData({ ...props.filters }, [...props.selectedIds], ticket)
      if (!ticket.isCurrent()) return
      if (!backup.keys.length) { services.notifyError(t('keys.transfer.empty')); return }
      const blob = new Blob([JSON.stringify(backup, null, 2)], { type: 'application/json' })
      if (blob.size > MAX_KEY_IMPORT_BYTES) throw new Error('size')
      saveAs(blob, 'api-keys-' + new Date().toISOString().replace(/[:.]/g, '-') + '.json')
      services.notifySuccess(t('keys.transfer.exported', { count: backup.keys.length }))
    } catch { if (ticket.isCurrent()) services.notifyError(t('keys.transfer.exportFailed')) }
    finally { if (ticket.isCurrent()) exporting.value = false }
  }
  function clearImport(): void {
    fileRequest.cancel(); importRequest.cancel(); showImport.value = false; importing.value = false
    reading.value = false; data.value = null; result.value = null; error.value = ''; fileName.value = ''
  }
  function closeImport(): void { if (!importing.value) clearImport() }
  async function readFile(event: Event): Promise<void> {
    if (!(event.target instanceof HTMLInputElement)) return
    const file = event.target.files?.[0]
    if (!file) return
    event.target.value = ''
    const ticket = fileRequest.begin()
    if (!ticket.isCurrent()) return
    fileName.value = file.name; data.value = null; result.value = null; error.value = ''; reading.value = true
    try {
      if (file.size > MAX_KEY_IMPORT_BYTES) throw new Error('size')
      const parsed = parseKeyImport(await file.text())
      if (ticket.isCurrent()) data.value = parsed
    } catch { if (ticket.isCurrent()) error.value = t('keys.transfer.invalidFile') }
    finally { if (ticket.isCurrent()) reading.value = false }
  }
  async function importKeys(): Promise<void> {
    if (!data.value || importing.value) return
    const ticket = importRequest.begin()
    if (!ticket.isCurrent()) return
    const payload = data.value
    importing.value = true; error.value = ''; result.value = null
    try {
      const response = await keysAPI.importData(payload, ticket)
      if (!ticket.isCurrent()) return
      result.value = response
      if (response.created) imported()
    } catch { if (ticket.isCurrent()) error.value = t('keys.transfer.importFailed') }
    finally { if (ticket.isCurrent()) importing.value = false }
  }
  watch(services.scopeKey, () => { clearImport(); showExport.value = false; exporting.value = false }, { flush: 'sync' })
  onScopeDispose(clearImport)
  return { showExport, exporting, showImport, importing, reading, fileName, data, error, result, exportKeys, closeImport, readFile, importKeys }
}
