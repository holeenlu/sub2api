import type { UpdateSettingsRequest } from '../api/settings'
import type { SettingsDraft } from './draft'
import { consoleFields, type ConsoleDraft } from './consoleModel'
export function prepareConsoleSettings(patch: Partial<ConsoleDraft>, draft: Pick<SettingsDraft, keyof ConsoleDraft>): Partial<UpdateSettingsRequest> {
  const origin = (draft.vendor_portal_origin || '').trim()
  if ('vendor_portal_origin' in patch || 'vendor_console_enabled' in patch) {
    if (draft.vendor_console_enabled || origin) {
      try {
        const url = new URL(origin)
        if (url.protocol !== 'https:' || url.origin !== origin || url.username || url.password || url.origin === window.location.origin) throw new Error('Invalid origin')
      } catch { throw { field: 'vendor_portal_origin', messageKey: 'vendor.invalidOrigin' } }
    }
  }
  for (const value of [patch.table_default_page_size, ...(patch.table_page_size_options || [])]) {
    if (value !== undefined && (!Number.isInteger(value) || value < 5 || value > 1000)) throw { field: 'table_default_page_size', messageKey: 'admin.settings.site.tableDefaultPageSizeRangeError', params: { min: 5, max: 1000 } }
  }
  const result: Partial<ConsoleDraft> = {}
  for (const key of consoleFields) if (key in patch) Object.assign(result, { [key]: key === 'vendor_portal_origin' ? origin : patch[key] })
  return result
}
