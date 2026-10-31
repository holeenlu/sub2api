import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import type { UsageQueryParams, UsageLog, AdminUsageLog } from '../api/types'
import { consumeUsageExport, escapeUsageCSV } from '../api/usageExport'
import { userUsageExportHeaders, userUsageExportRows, adminUsageExportHeaders, adminUsageExportRows } from '../model/exportRows'
import { useUsageServices } from '../services'
import { useUsageRequest } from './useUsageRequest'

export function useUsageExport(options: { role: 'user' | 'admin'; params: () => UsageQueryParams; total: () => number }) {
  const services = useUsageServices()
  const { t } = useI18n()
  const request = useUsageRequest(services.scopeKey)
  const exporting = ref(false)
  const exportProgress = reactive({ show: false, progress: 0, current: 0, total: 0, estimatedTime: '' })
  function cancel(): void { request.cancel(); exporting.value = false; exportProgress.show = false }
  async function run(): Promise<void> {
    if (exporting.value || (options.role === 'admin' && !services.permissions.value.canExport)) return
    if (options.role === 'user' && options.total() === 0) { services.notifyWarning(t('usage.noDataToExport')); return }
    const ticket = request.begin()
    if (!ticket.isCurrent()) return
    const params = { ...options.params() }
    const permissions = { ...services.permissions.value }
    exporting.value = true
    exportProgress.show = options.role === 'admin'; exportProgress.current = 0; exportProgress.progress = 0; exportProgress.total = options.total()
    if (options.role === 'user') services.notifyInfo(t('usage.preparingExport'))
    try {
      if (options.role === 'user') {
        const chunks = ['\uFEFF' + userUsageExportHeaders(t).map(escapeUsageCSV).join(',') + '\n']
        await consumeUsageExport<UsageLog>('/usage/export', params, rows => {
          if (!ticket.isCurrent()) throw new DOMException('Usage export canceled', 'AbortError')
          chunks.push(userUsageExportRows(rows, t).map(row => row.join(',')).join('\n') + '\n')
        }, ticket.signal)
        if (!ticket.isCurrent()) return
        const url = URL.createObjectURL(new Blob(chunks, { type: 'text/csv;charset=utf-8;' }))
        try {
          const link = document.createElement('a'); link.href = url
          link.download = 'usage_' + params.start_date + '_to_' + params.end_date + '.csv'; link.click()
        } finally { URL.revokeObjectURL(url) }
      } else {
        const XLSX = await import('xlsx')
        if (!ticket.isCurrent()) return
        const sheet = XLSX.utils.aoa_to_sheet([adminUsageExportHeaders(t, permissions)])
        await consumeUsageExport<AdminUsageLog>('/admin/usage/export', params, rows => {
          if (!ticket.isCurrent()) throw new DOMException('Usage export canceled', 'AbortError')
          const values = adminUsageExportRows(rows, t, permissions)
          if (values.length) XLSX.utils.sheet_add_aoa(sheet, values, { origin: -1 })
          exportProgress.current += values.length
          exportProgress.progress = exportProgress.total > 0 ? Math.min(100, Math.round(exportProgress.current / exportProgress.total * 100)) : 0
        }, ticket.signal)
        if (!ticket.isCurrent()) return
        const workbook = XLSX.utils.book_new(); XLSX.utils.book_append_sheet(workbook, sheet, 'Usage')
        saveAs(new Blob([XLSX.write(workbook, { bookType: 'xlsx', type: 'array' })], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' }), 'usage_' + params.start_date + '_to_' + params.end_date + '.xlsx')
      }
      if (ticket.isCurrent()) services.notifySuccess(t('usage.exportSuccess'))
    } catch {
      if (ticket.isCurrent()) services.notifyError(t('usage.exportFailed'))
    } finally { if (ticket.isCurrent()) { exporting.value = false; exportProgress.show = false } }
  }
  watch(services.scopeKey, cancel, { flush: 'sync' })
  watch(() => [services.permissions.value.isSuperAdmin, services.permissions.value.canExport, services.permissions.value.canReadBillingCost], cancel, { flush: 'sync' })
  return { exporting, exportProgress, run, cancel }
}
