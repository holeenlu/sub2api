import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { catalogScopeKey, catalogUserGroupRatesKey } from '../ports'
import { useAvailableChannelsQuery } from '../queries/availableChannels'
import type { UserAvailableChannel } from '../api/types'
const getAvailableChannels = vi.hoisted(() => vi.fn())
vi.mock('../api/availableChannels', () => ({ getAvailableChannels }))
enableAutoUnmount(afterEach)
beforeEach(() => getAvailableChannels.mockReset())
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const channels = (name: string): UserAvailableChannel[] => [{ name, description: '', platforms: [] }]
function setup(rates = vi.fn().mockResolvedValue({})) {
  const scope = ref('epoch1:user:1')
  let query!: ReturnType<typeof useAvailableChannelsQuery>
  const wrapper = mount(defineComponent({ setup() { query = useAvailableChannelsQuery(); return () => null } }), {
    global: { provide: { [catalogScopeKey as symbol]: scope, [catalogUserGroupRatesKey as symbol]: rates } }
  })
  return { scope, query, wrapper, rates }
}

describe('available channel query ownership', () => {
  it('shares cancellation with optional rates and keeps a confirmed zero rate', async () => {
    getAvailableChannels.mockResolvedValue(channels('current'))
    const { query, rates } = setup(vi.fn().mockResolvedValue({ 7: 0 }))
    await flushPromises()
    expect(query.channels.value).toEqual(channels('current'))
    expect(query.userGroupRates.value).toEqual({ 7: 0 })
    expect(rates).toHaveBeenCalledWith(getAvailableChannels.mock.calls[0][0].signal)
    expect(query.loading.value).toBe(false)
  })
  it('keeps channel display when custom rates fail without treating them as authoritative zero', async () => {
    const failure = new Error('rates unavailable')
    getAvailableChannels.mockResolvedValue(channels('default-rate fallback'))
    const { query } = setup(vi.fn().mockRejectedValue(failure))
    await flushPromises()
    expect(query.channels.value).toEqual(channels('default-rate fallback'))
    expect(query.userGroupRates.value).toEqual({})
    expect(query.ratesError.value).toBe(failure)
    expect(query.error.value).toBeNull()
    expect(query.loading.value).toBe(false)
  })
  it.each(['resolve', 'reject'] as const)('discards a late private %s on scope change', async outcome => {
    const old = deferred<UserAvailableChannel[]>()
    const fresh = deferred<UserAvailableChannel[]>()
    const oldRates = deferred<Record<number, number>>()
    getAvailableChannels.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    const { scope, query } = setup(vi.fn().mockReturnValueOnce(oldRates.promise).mockResolvedValueOnce({ 8: 0 }))
    const signal = getAvailableChannels.mock.calls[0][0].signal as AbortSignal
    scope.value = 'epoch2:user:2'
    expect(signal.aborted).toBe(true)
    expect(query.channels.value).toEqual([])
    expect(query.userGroupRates.value).toEqual({})
    if (outcome === 'resolve') old.resolve(channels('private-old')); else old.reject(new Error('stale'))
    oldRates.reject(new Error('stale rates'))
    await flushPromises()
    expect(query.error.value).toBeNull()
    expect(query.ratesError.value).toBeNull()
    expect(query.loading.value).toBe(true)
    fresh.resolve(channels('new scope'))
    await flushPromises()
    expect(query.channels.value).toEqual(channels('new scope'))
    expect(query.userGroupRates.value).toEqual({ 8: 0 })
  })
  it('does not let an older refresh overwrite the latest result', async () => {
    const old = deferred<UserAvailableChannel[]>()
    getAvailableChannels.mockReturnValueOnce(old.promise).mockResolvedValueOnce(channels('latest'))
    const { query } = setup()
    const signal = getAvailableChannels.mock.calls[0][0].signal as AbortSignal
    await query.reload()
    expect(signal.aborted).toBe(true)
    old.resolve(channels('older'))
    await flushPromises()
    expect(query.channels.value).toEqual(channels('latest'))
  })
  it('preserves a same-scope snapshot on refresh error and clears it on successful empty response', async () => {
    const failure = new Error('channel offline')
    getAvailableChannels.mockResolvedValueOnce(channels('snapshot')).mockRejectedValueOnce(failure).mockResolvedValueOnce([])
    const { query } = setup()
    await flushPromises()
    await query.reload()
    expect(query.channels.value).toEqual(channels('snapshot'))
    expect(query.error.value).toBe(failure)
    expect(query.loading.value).toBe(false)
    await query.reload()
    expect(query.channels.value).toEqual([])
    expect(query.error.value).toBeNull()
  })
  it('clears data on disposal and refuses both late completion and later reload', async () => {
    const late = deferred<UserAvailableChannel[]>()
    getAvailableChannels.mockResolvedValueOnce(channels('private')).mockReturnValueOnce(late.promise)
    const { query, wrapper } = setup(vi.fn().mockResolvedValue({ 1: 0 }))
    await flushPromises()
    const pending = query.reload()
    const signal = getAvailableChannels.mock.calls[1][0].signal as AbortSignal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    expect(query.channels.value).toEqual([])
    expect(query.userGroupRates.value).toEqual({})
    late.resolve(channels('late'))
    await pending
    await query.reload()
    expect(getAvailableChannels).toHaveBeenCalledTimes(2)
    expect(query.channels.value).toEqual([])
    expect(query.loading.value).toBe(false)
  })
})
