import { apiClient } from '@/shared/http'
import { serviceModes, tokenFields } from './types'
import type { ManagedPricePlatform, ModelPrice, ModelPrices, PriceVersion } from './types'

export class InvalidPriceData extends Error {
  constructor() { super('Invalid managed pricing data') }
}
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
export function requirePriceVersion(value: unknown): asserts value is number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) throw new InvalidPriceData()
}
function requireTokens(value: unknown): asserts value is Record<string, unknown> {
  if (!record(value)) throw new InvalidPriceData()
  for (const key of tokenFields) {
    if (!(key in value)) continue
    const rate = value[key]
    if (rate !== null && (typeof rate !== 'number' || !Number.isFinite(rate) || rate < 0)) throw new InvalidPriceData()
  }
}
export function requireModelPrice(value: unknown): asserts value is ModelPrice {
  requireTokens(value)
  if ('tiers' in value) {
    if (!Array.isArray(value.tiers)) throw new InvalidPriceData()
    const thresholds = new Set<number>()
    for (const tier of value.tiers) {
      requireTokens(tier)
      const above = tier.above
      if (typeof above !== 'number' || !Number.isSafeInteger(above) || above <= 0 || thresholds.has(above)) throw new InvalidPriceData()
      thresholds.add(above)
      if ('unpriced' in tier && typeof tier.unpriced !== 'boolean') throw new InvalidPriceData()
    }
  }
  if ('modes' in value) {
    if (!record(value.modes)) throw new InvalidPriceData()
    for (const mode of serviceModes) {
      if (mode in value.modes && value.modes[mode] !== null) requireTokens(value.modes[mode])
    }
  }
  if ('image' in value && value.image !== null) requireTokens(value.image)
}
function requirePricedTokens(value: Record<string, unknown>) {
  for (const key of ['input', 'output', 'cache_read']) {
    const rate = value[key]
    if (typeof rate !== 'number' || !Number.isFinite(rate) || rate < 0 || rate > 1e9) throw new InvalidPriceData()
  }
  for (const key of ['cache_5m', 'cache_1h']) {
    const rate = value[key]
    if (typeof rate === 'number' && rate > 1e9) throw new InvalidPriceData()
  }
}
/** Reads may be incomplete; writes must satisfy the legacy server's priced-base contract. */
export function requirePriceWrite(value: unknown): asserts value is ModelPrice {
  requireModelPrice(value)
  // requireModelPrice has already validated the object shape and finite optional rates.
  requirePricedTokens({ ...value })
  if ((value.tiers?.length ?? 0) > 32) throw new InvalidPriceData()
  for (const tier of value.tiers ?? []) if (!tier.unpriced) requirePricedTokens({ ...tier })
  for (const [mode, price] of Object.entries(value.modes ?? {})) {
    if (!serviceModes.some(known => known === mode)) throw new InvalidPriceData()
    if (price !== null) requirePricedTokens({ ...price })
  }
  if (value.image != null) requirePricedTokens({ ...value.image })
}
function requirePrices(value: unknown): asserts value is ModelPrices {
  if (!record(value)) throw new InvalidPriceData()
  requirePriceVersion(value.version)
  if (value.synced_at !== undefined && typeof value.synced_at !== 'string') throw new InvalidPriceData()
  if (!Array.isArray(value.entries)) throw new InvalidPriceData()
  const ids = new Set<string>()
  for (const entry of value.entries) {
    if (!record(entry) || typeof entry.id !== 'string' || !entry.id.trim() || ids.has(entry.id) || typeof entry.source !== 'string') throw new InvalidPriceData()
    ids.add(entry.id)
    if (entry.effective !== null) requireModelPrice(entry.effective)
    if (entry.override !== null) requireModelPrice(entry.override)
  }
  // Validated without projecting fields: nested tiers/modes/image remain intact.
}
function version(value: unknown): PriceVersion {
  if (!record(value)) throw new InvalidPriceData()
  requirePriceVersion(value.version)
  return { version: value.version }
}

/** Legacy 7771bc4b9 contract, pending matching backend routes and server RBAC/CAS. */
export const managedPricesAPI = {
  async prices(platform: ManagedPricePlatform, signal: AbortSignal): Promise<ModelPrices> {
    const { data } = await apiClient.get<unknown>('/admin/models/pricing', { params: { platform }, signal })
    requirePrices(data)
    return data
  },
  async savePrice(platform: ManagedPricePlatform, model: string, expectedVersion: number, price: ModelPrice | null, signal: AbortSignal): Promise<PriceVersion> {
    requirePriceVersion(expectedVersion)
    if (!model.trim()) throw new InvalidPriceData()
    if (price !== null) requirePriceWrite(price)
    return version((await apiClient.put<unknown>('/admin/models/pricing', { platform, model, version: expectedVersion, price }, { signal })).data)
  },
  async syncPrices(platform: ManagedPricePlatform, expectedVersion: number, signal: AbortSignal): Promise<PriceVersion> {
    requirePriceVersion(expectedVersion)
    return version((await apiClient.post<unknown>('/admin/models/pricing/sync', { platform, version: expectedVersion }, { signal })).data)
  }
}
