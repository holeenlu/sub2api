import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'

export type ConsoleDraft = Pick<SettingsDraft, 'vendor_console_enabled' | 'vendor_portal_origin' | 'backend_mode_enabled' | 'table_default_page_size' | 'table_page_size_options'>
export const consoleFields = ['vendor_console_enabled', 'vendor_portal_origin', 'backend_mode_enabled', 'table_default_page_size', 'table_page_size_options'] as const
export function projectConsole(settings: SettingsDraft): ConsoleDraft { return { vendor_console_enabled: structuredClone(toRaw(settings.vendor_console_enabled ?? false)), vendor_portal_origin: structuredClone(toRaw(settings.vendor_portal_origin ?? '')), backend_mode_enabled: structuredClone(toRaw(settings.backend_mode_enabled ?? false)), table_default_page_size: structuredClone(toRaw(settings.table_default_page_size ?? 20)), table_page_size_options: structuredClone(toRaw(settings.table_page_size_options ?? [10, 20, 50, 100])) } }
