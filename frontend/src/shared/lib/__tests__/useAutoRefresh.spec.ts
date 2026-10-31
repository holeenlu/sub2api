import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { useAutoRefresh, type UseAutoRefreshOptions } from '../useAutoRefresh'

let unmount: (() => void) | undefined
function setup(options: Partial<UseAutoRefreshOptions> = {}) {
  const onRefresh = vi.fn()
  let refresh: ReturnType<typeof useAutoRefresh> | undefined
  const wrapper = mount(defineComponent({
    setup() {
      refresh = useAutoRefresh({ storageKey: 'auto-refresh-test', onRefresh, ...options })
      return () => null
    },
  }))
  unmount = () => wrapper.unmount()
  if (!refresh) throw new Error('Refresh composable was not mounted')
  return { refresh, onRefresh }
}

beforeEach(() => { localStorage.clear(); vi.useFakeTimers() })
afterEach(() => { unmount?.(); unmount = undefined; localStorage.clear(); vi.restoreAllMocks(); vi.useRealTimers() })

describe('useAutoRefresh stored preferences', () => {
  it.each([5, 10, 15, 30])('loads allowed interval %s without starting a timer', interval => {
    const startTimer = vi.spyOn(window, 'setInterval')
    localStorage.setItem('auto-refresh-test', JSON.stringify({ enabled: true, interval_seconds: interval }))
    const { refresh } = setup()
    expect(refresh.enabled.value).toBe(true)
    expect(refresh.intervalSeconds.value).toBe(interval)
    expect(startTimer).not.toHaveBeenCalled()
  })

  it('validates custom allowed intervals by value', () => {
    localStorage.setItem('auto-refresh-test', JSON.stringify({ enabled: false, interval_seconds: 120 }))
    const { refresh } = setup({ intervals: [30, 60, 120], defaultInterval: 60, defaultEnabled: true })
    expect(refresh.enabled.value).toBe(false)
    expect(refresh.intervalSeconds.value).toBe(120)
  })

  it.each(['10', '', false, true, null, [], [10], {}, 0, -5, 11])('ignores invalid interval %j instead of coercing it', interval => {
    localStorage.setItem('auto-refresh-test', JSON.stringify({ enabled: true, interval_seconds: interval }))
    const { refresh } = setup()
    expect(refresh.intervalSeconds.value).toBe(30)
    expect(refresh.enabled.value).toBe(true)
  })

  it.each(['null', '[]', 'true', '10', '"10"', '{broken'])('preserves defaults for malformed preference %s', saved => {
    localStorage.setItem('auto-refresh-test', saved)
    const { refresh } = setup({ defaultEnabled: true, defaultInterval: 15 })
    expect(refresh.enabled.value).toBe(true)
    expect(refresh.intervalSeconds.value).toBe(15)
  })

  it('rejects non-finite stored numbers even if listed in intervals', () => {
    localStorage.setItem('auto-refresh-test', '{"enabled":true,"interval_seconds":1e400}')
    const { refresh } = setup({ intervals: [10, Infinity], defaultInterval: 10 })
    expect(refresh.intervalSeconds.value).toBe(10)
  })
})

describe('useAutoRefresh lifecycle', () => {
  it('refreshes at the selected interval, persists preferences and stops on unmount', async () => {
    const startTimer = vi.spyOn(window, 'setInterval')
    const stopTimer = vi.spyOn(globalThis, 'clearInterval')
    const { refresh, onRefresh } = setup({ intervals: [5, 10], defaultInterval: 10 })
    refresh.setEnabled(true)
    refresh.start()
    expect(startTimer).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(9000)
    expect(onRefresh).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1000)
    expect(onRefresh).toHaveBeenCalledTimes(1)
    refresh.setInterval(5)
    expect(JSON.parse(localStorage.getItem('auto-refresh-test') ?? '{}')).toEqual({ enabled: true, interval_seconds: 5 })
    await vi.advanceTimersByTimeAsync(5000)
    expect(onRefresh).toHaveBeenCalledTimes(2)
    unmount?.()
    unmount = undefined
    expect(stopTimer).toHaveBeenCalledWith(startTimer.mock.results[0]?.value)
    await vi.advanceTimersByTimeAsync(10000)
    expect(onRefresh).toHaveBeenCalledTimes(2)
  })

  it('pauses without consuming the countdown and never overlaps a pending refresh', async () => {
    let paused = true
    let finish: (() => void) | undefined
    const pending = new Promise<void>(resolve => { finish = resolve })
    const onRefresh = vi.fn(() => pending)
    const { refresh } = setup({ defaultInterval: 5, shouldPause: () => paused, onRefresh })
    refresh.setEnabled(true)
    await vi.advanceTimersByTimeAsync(10000)
    expect(refresh.countdown.value).toBe(5)
    paused = false
    await vi.advanceTimersByTimeAsync(5000)
    expect(onRefresh).toHaveBeenCalledTimes(1)
    expect(refresh.fetching.value).toBe(true)
    await vi.advanceTimersByTimeAsync(10000)
    expect(onRefresh).toHaveBeenCalledTimes(1)
    finish?.()
    await pending
    expect(refresh.fetching.value).toBe(false)
    refresh.setEnabled(false)
    expect(refresh.countdown.value).toBe(0)
    await vi.advanceTimersByTimeAsync(10000)
    expect(onRefresh).toHaveBeenCalledTimes(1)
  })
})
