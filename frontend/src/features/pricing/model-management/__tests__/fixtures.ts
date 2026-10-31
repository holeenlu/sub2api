import type { ModelPrice, ModelPrices } from '../types'
export function fullPrice(): ModelPrice {
  return {
    input: 0, output: 3.5, cache_read: 0, cache_5m: 1, cache_1h: 2,
    tiers: [
      { above: 200000, input: 5, output: 6, cache_read: 0, cache_5m: 7, cache_1h: 8 },
      { above: 400000, unpriced: true, input: 9, output: 10, cache_read: null }
    ],
    modes: { fast: { input: 0, output: 2, cache_read: 1, cache_5m: 2, cache_1h: 3 }, flex: null, priority: { input: 1, output: 5, cache_read: 0 } },
    image: { input: 1, output: 0, cache_read: 2, cache_5m: 4, cache_1h: 5 }
  }
}
export function modelPrices(version = 7): ModelPrices {
  return { version, synced_at: '2026-10-27T00:00:00Z', entries: [
    { id: 'test-image', source: 'custom', effective: fullPrice(), override: fullPrice() },
    { id: 'unknown-model', source: 'upstream-source', effective: { input: 0, output: null }, override: null },
    { id: 'unpriced-model', source: 'none', effective: null, override: null }
  ] }
}
export function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Uninitialized deferred') }
  let reject: (reason: unknown) => void = () => { throw new Error('Uninitialized deferred') }
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
