export type ChannelMonitorMode = 'v1' | 'v2'

/** Missing/unknown configuration keeps the current default; v1 is an explicit selection. */
export function resolveChannelMonitorMode(settings?: { readonly channel_monitor_mode?: unknown } | null): ChannelMonitorMode {
  return settings?.channel_monitor_mode === 'v1' ? 'v1' : 'v2'
}
