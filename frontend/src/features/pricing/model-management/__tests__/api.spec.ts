import { beforeEach, describe, expect, it, vi } from 'vitest'
import { InvalidPriceData, managedPricesAPI } from '../api'
import { fullPrice, modelPrices } from './fixtures'
const http = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn() }))
vi.mock('@/shared/http', () => ({ apiClient: http }))
beforeEach(() => vi.resetAllMocks())

describe('managed pricing API uses the shared client and legacy CAS contract', () => {
  it('reads by platform with cancellation and preserves full nested fields and absent values', async () => {
    const payload = modelPrices()
    http.get.mockResolvedValue({ data: payload })
    const signal = new AbortController().signal
    expect(await managedPricesAPI.prices('anthropic', signal)).toEqual(payload)
    expect(http.get).toHaveBeenCalledWith('/admin/models/pricing', { params: { platform: 'anthropic' }, signal })
    expect(payload.entries[1]?.effective).not.toHaveProperty('cache_read')
  })
  it.each([null, fullPrice()])('saves reset/complete price with the captured version and signal', async price => {
    http.put.mockResolvedValue({ data: { version: 8 } })
    const signal = new AbortController().signal
    expect(await managedPricesAPI.savePrice('openai', 'test-image', 7, price, signal)).toEqual({ version: 8 })
    expect(http.put).toHaveBeenCalledWith('/admin/models/pricing', { platform: 'openai', model: 'test-image', version: 7, price }, { signal })
  })
  it('syncs with the expected version and the caller cancellation', async () => {
    http.post.mockResolvedValue({ data: { version: 8 } })
    const signal = new AbortController().signal
    await managedPricesAPI.syncPrices('anthropic', 7, signal)
    expect(http.post).toHaveBeenCalledWith('/admin/models/pricing/sync', { platform: 'anthropic', version: 7 }, { signal })
  })
  it.each([-1, 1.2, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1])('never sends invalid version %s', async version => {
    const signal = new AbortController().signal
    await expect(managedPricesAPI.savePrice('openai', 'x', version, null, signal)).rejects.toBeInstanceOf(InvalidPriceData)
    await expect(managedPricesAPI.syncPrices('openai', version, signal)).rejects.toBeInstanceOf(InvalidPriceData)
    expect(http.put).not.toHaveBeenCalled()
    expect(http.post).not.toHaveBeenCalled()
  })
  it.each([
    { version: -1, entries: [] }, { entries: [] }, { version: 1, entries: null },
    { ...modelPrices(), entries: [...modelPrices().entries, modelPrices().entries[0]] },
    { version: 1, entries: [{ id: 'x', source: 's', effective: { input: '0' }, override: null }] },
    { version: 1, entries: [{ id: 'x', source: 's', effective: { image: { output: -1 } }, override: null }] }
  ])('rejects malformed query DTO without inventing version or prices', async data => {
    http.get.mockResolvedValue({ data })
    await expect(managedPricesAPI.prices('openai', new AbortController().signal)).rejects.toBeInstanceOf(InvalidPriceData)
  })
  it.each([
    { input: -1 }, { output: NaN }, { cache_1h: Infinity },
    { tiers: [{ above: 0 }] }, { tiers: [{ above: 2 }, { above: 2 }] },
    { modes: { flex: { output: -1 } } }, { image: { cache_5m: -1 } }
  ])('rejects invalid nested outgoing prices before making a request', async price => {
    await expect(managedPricesAPI.savePrice('openai', 'x', 7, { ...fullPrice(), ...price }, new AbortController().signal)).rejects.toBeInstanceOf(InvalidPriceData)
    expect(http.put).not.toHaveBeenCalled()
  })
  it('rejects an invalid receipt rather than treating the command as a success', async () => {
    http.post.mockResolvedValue({ data: { version: -1 } })
    await expect(managedPricesAPI.syncPrices('openai', 7, new AbortController().signal)).rejects.toBeInstanceOf(InvalidPriceData)
  })
  it('supports a first authoritative GET 0 → PUT expected 0 → version 1', async () => {
    http.get.mockResolvedValueOnce({ data: modelPrices(0) }).mockResolvedValueOnce({ data: modelPrices(1) })
    http.put.mockResolvedValueOnce({ data: { version: 1 } })
    const signal = new AbortController().signal
    const seed = await managedPricesAPI.prices('openai', signal)
    const receipt = await managedPricesAPI.savePrice('openai', 'test-image', seed.version, fullPrice(), signal)
    expect(http.put.mock.calls[0]?.[1]).toMatchObject({ version: 0 })
    expect(receipt.version).toBe(1)
    expect((await managedPricesAPI.prices('openai', signal)).version).toBe(1)
  })
  it.each(['input', 'output', 'cache_read'] as const)('rejects null/missing %s writes but tolerates incomplete reads', async key => {
    for (const rate of [null, undefined]) {
      const price = { ...fullPrice(), [key]: rate }
      if (rate === undefined) delete price[key]
      http.get.mockResolvedValueOnce({ data: { version: 0, entries: [{ id: 'x', source: 'none', effective: price, override: null }] } })
      expect((await managedPricesAPI.prices('openai', new AbortController().signal)).version).toBe(0)
      await expect(managedPricesAPI.savePrice('openai', 'x', 0, price, new AbortController().signal)).rejects.toBeInstanceOf(InvalidPriceData)
    }
    expect(http.put).not.toHaveBeenCalled()
  })
  it.each(['input', 'output', 'cache_read', 'cache_5m', 'cache_1h'] as const)('enforces the 1e9 upper bound for %s', async key => {
    const signal = new AbortController().signal
    http.put.mockResolvedValue({ data: { version: 1 } })
    await managedPricesAPI.savePrice('openai', 'x', 0, { ...fullPrice(), [key]: 1e9 }, signal)
    await expect(managedPricesAPI.savePrice('openai', 'x', 0, { ...fullPrice(), [key]: 1e9 + 1 }, signal)).rejects.toBeInstanceOf(InvalidPriceData)
    expect(http.put).toHaveBeenCalledTimes(1)
  })
  it('preserves optional cache null/absence, explicit unpriced modes/tiers and image absence without filling rates in the browser', async () => {
    const price = { input: 0, output: 0, cache_read: 0, cache_5m: null, tiers: [{ above: 1, unpriced: true }], modes: { fast: null, flex: null, priority: null }, image: null }
    http.put.mockResolvedValueOnce({ data: { version: 1 } })
    await managedPricesAPI.savePrice('anthropic', 'x', 0, price, new AbortController().signal)
    expect(http.put.mock.calls[0]?.[1]).toMatchObject({ price })
    expect(http.put.mock.calls[0]?.[1].price).not.toHaveProperty('cache_1h')
  })
  it.each([
    { tiers: [{ above: 1, input: null, output: 0, cache_read: 0 }] },
    { modes: { flex: { input: 0, output: null, cache_read: 0 } } },
    { image: { input: 0, output: 0, cache_read: null } },
    { modes: { fast: { input: 1e9 + 1, output: 0, cache_read: 0 } } }
  ])('applies strict write rules to every priced tier/mode/image base', async nested => {
    await expect(managedPricesAPI.savePrice('openai', 'x', 0, { ...fullPrice(), ...nested }, new AbortController().signal)).rejects.toBeInstanceOf(InvalidPriceData)
    expect(http.put).not.toHaveBeenCalled()
  })
})
