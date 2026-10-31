import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type ChannelMonitorDraft = Pick<SettingsDraft, 'channel_monitor_enabled' | 'channel_monitor_mode' | 'channel_monitor_default_interval_seconds' | 'channel_monitor_hide_throughput' | 'channel_monitor_hide_user_ranking' | 'channel_monitor_show_quota'>
export const channelMonitorFields = ['channel_monitor_enabled', 'channel_monitor_mode', 'channel_monitor_default_interval_seconds', 'channel_monitor_hide_throughput', 'channel_monitor_hide_user_ranking', 'channel_monitor_show_quota'] as const
export function projectChannelMonitor(settings: SettingsDraft): ChannelMonitorDraft {
  return {
    channel_monitor_enabled: structuredClone(toRaw(settings.channel_monitor_enabled ?? (true))),
    channel_monitor_mode: structuredClone(toRaw(settings.channel_monitor_mode ?? ('v1' as 'v1' | 'v2'))),
    channel_monitor_default_interval_seconds: structuredClone(toRaw(settings.channel_monitor_default_interval_seconds ?? (60))),
    channel_monitor_hide_throughput: structuredClone(toRaw(settings.channel_monitor_hide_throughput ?? (false))),
    channel_monitor_hide_user_ranking: structuredClone(toRaw(settings.channel_monitor_hide_user_ranking ?? (false))),
    channel_monitor_show_quota: structuredClone(toRaw(settings.channel_monitor_show_quota ?? (false))),
  }
}
