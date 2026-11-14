import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type PluginFeatureDraft = Pick<SettingsDraft, 'plugin_management_enabled'>
export const pluginFeatureFields = ['plugin_management_enabled'] as const
export function projectPluginFeature(settings: SettingsDraft): PluginFeatureDraft {
  return {
    plugin_management_enabled: structuredClone(toRaw(settings.plugin_management_enabled ?? (false))),
  }
}
