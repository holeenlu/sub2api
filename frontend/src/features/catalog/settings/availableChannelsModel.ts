import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type AvailableChannelsDraft = Pick<SettingsDraft, 'available_channels_enabled'>
export const availableChannelsFields = ['available_channels_enabled'] as const
export function projectAvailableChannels(settings: SettingsDraft): AvailableChannelsDraft {
  return {
    available_channels_enabled: structuredClone(toRaw(settings.available_channels_enabled ?? (false))),
  }
}
