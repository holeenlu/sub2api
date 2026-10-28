import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type BpsImagesDraft = Pick<SettingsDraft, 'excel_bps_image_relay_enabled' | 'excel_bps_enabled' | 'excel_bps_image_mode' | 'excel_bps_image_base_url' | 'excel_bps_image_limit_policy' | 'excel_bps_image_warning_remaining' | 'excel_bps_image_compact_reserve' | 'excel_bps_image_body_limit_mib' | 'excel_bps_image_budget_mib' | 'excel_bps_image_max_requests' | 'excel_bps_image_max_images' | 'excel_bps_image_max_image_mib' | 'excel_bps_image_max_total_mib' | 'excel_bps_image_storage_mib' | 'excel_bps_image_storage_entries' | 'excel_bps_image_ttl_minutes'>
export const bpsImagesFields = ['excel_bps_image_relay_enabled', 'excel_bps_enabled', 'excel_bps_image_mode', 'excel_bps_image_base_url', 'excel_bps_image_limit_policy', 'excel_bps_image_warning_remaining', 'excel_bps_image_compact_reserve', 'excel_bps_image_body_limit_mib', 'excel_bps_image_budget_mib', 'excel_bps_image_max_requests', 'excel_bps_image_max_images', 'excel_bps_image_max_image_mib', 'excel_bps_image_max_total_mib', 'excel_bps_image_storage_mib', 'excel_bps_image_storage_entries', 'excel_bps_image_ttl_minutes'] as const
export function projectBpsImages(settings: SettingsDraft): BpsImagesDraft {
  return {
    excel_bps_image_relay_enabled: structuredClone(toRaw(settings.excel_bps_image_relay_enabled ?? (true))),
    excel_bps_enabled: structuredClone(toRaw(settings.excel_bps_enabled ?? (true))),
    excel_bps_image_mode: structuredClone(toRaw(settings.excel_bps_image_mode ?? ('native' as 'relay' | 'native'))),
    excel_bps_image_base_url: structuredClone(toRaw(settings.excel_bps_image_base_url ?? (''))),
    excel_bps_image_limit_policy: structuredClone(toRaw(settings.excel_bps_image_limit_policy ?? ("auto_compact" as "off" | "auto_compact" | "warn"))),
    excel_bps_image_warning_remaining: structuredClone(toRaw(settings.excel_bps_image_warning_remaining ?? (8))),
    excel_bps_image_compact_reserve: structuredClone(toRaw(settings.excel_bps_image_compact_reserve ?? (3))),
    excel_bps_image_body_limit_mib: structuredClone(toRaw(settings.excel_bps_image_body_limit_mib ?? (128))),
    excel_bps_image_budget_mib: structuredClone(toRaw(settings.excel_bps_image_budget_mib ?? (1024))),
    excel_bps_image_max_requests: structuredClone(toRaw(settings.excel_bps_image_max_requests ?? (128))),
    excel_bps_image_max_images: structuredClone(toRaw(settings.excel_bps_image_max_images ?? (20))),
    excel_bps_image_max_image_mib: structuredClone(toRaw(settings.excel_bps_image_max_image_mib ?? (32))),
    excel_bps_image_max_total_mib: structuredClone(toRaw(settings.excel_bps_image_max_total_mib ?? (64))),
    excel_bps_image_storage_mib: structuredClone(toRaw(settings.excel_bps_image_storage_mib ?? (1024))),
    excel_bps_image_storage_entries: structuredClone(toRaw(settings.excel_bps_image_storage_entries ?? (512))),
    excel_bps_image_ttl_minutes: structuredClone(toRaw(settings.excel_bps_image_ttl_minutes ?? (30))),
  }
}
