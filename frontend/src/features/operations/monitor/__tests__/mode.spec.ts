import { describe, expect, it, vi } from 'vitest'
import { resolveChannelMonitorMode } from '../mode'
import { getChannelMonitorMode } from '@/app/navigation/featureFlags'
const { appStore } = vi.hoisted(() => ({ appStore: { cachedPublicSettings: null as { channel_monitor_mode?: string } | null | undefined } }))
vi.mock('@/app/model/app', () => ({ useAppStore: () => appStore }))
describe('monitor mode resolution', () => {
  it.each([undefined, null, {}, { channel_monitor_mode: undefined }, { channel_monitor_mode: '' }, { channel_monitor_mode: 'unknown' }, { channel_monitor_mode: 'v2' }])('keeps v2 for absent or non-v1 configuration: %j', settings => {
    appStore.cachedPublicSettings = settings
    expect(resolveChannelMonitorMode(settings)).toBe('v2')
    expect(getChannelMonitorMode()).toBe('v2')
  })
  it('keeps an explicitly configured v1 deployment', () => {
    appStore.cachedPublicSettings = { channel_monitor_mode: 'v1' }
    expect(resolveChannelMonitorMode(appStore.cachedPublicSettings)).toBe('v1')
    expect(getChannelMonitorMode()).toBe('v1')
  })
})
