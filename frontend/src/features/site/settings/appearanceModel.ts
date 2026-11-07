import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type AppearanceDraft = Pick<SettingsDraft, 'site_name' | 'site_subtitle' | 'api_base_url' | 'custom_endpoints' | 'contact_info' | 'doc_url' | 'site_logo' | 'home_content' | 'compact_home_enabled'>
export const appearanceFields = ['site_name', 'site_subtitle', 'api_base_url', 'custom_endpoints', 'contact_info', 'doc_url', 'site_logo', 'home_content', 'compact_home_enabled'] as const
export function projectAppearance(settings: SettingsDraft): AppearanceDraft {
  return {
    site_name: structuredClone(toRaw(settings.site_name ?? ('sub2api'))),
    site_subtitle: structuredClone(toRaw(settings.site_subtitle ?? (''))),
    api_base_url: structuredClone(toRaw(settings.api_base_url ?? (""))),
    custom_endpoints: structuredClone(toRaw(settings.custom_endpoints ?? ([] as Array<{
    name: string;
    endpoint: string;
    description: string;
  }>))),
    contact_info: structuredClone(toRaw(settings.contact_info ?? (""))),
    doc_url: structuredClone(toRaw(settings.doc_url ?? (""))),
    site_logo: structuredClone(toRaw(settings.site_logo ?? (""))),
    home_content: structuredClone(toRaw(settings.home_content ?? (""))),
    compact_home_enabled: structuredClone(toRaw(settings.compact_home_enabled ?? (false))),
  }
}
