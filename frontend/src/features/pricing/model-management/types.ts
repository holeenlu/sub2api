export type ManagedPricePlatform = 'openai' | 'anthropic'
export const tokenFields = ['input', 'output', 'cache_read', 'cache_5m', 'cache_1h'] as const
export type TokenField = typeof tokenFields[number]
// Read DTO: missing means unknown; zero is known free. Null is unavailable for base
// fields, server fallback for optional cache writes, and is not a saveable base.
export type TokenPrice = Partial<Record<TokenField, number | null>>
export const serviceModes = ['fast', 'flex', 'priority'] as const
export type ServiceMode = typeof serviceModes[number]
export interface PriceTier extends TokenPrice { above: number; unpriced?: boolean }
export interface ModelPrice extends TokenPrice {
  tiers?: PriceTier[]
  modes?: Partial<Record<ServiceMode, TokenPrice | null>>
  image?: TokenPrice | null
}
export interface PriceEntry {
  id: string
  source: string
  effective: ModelPrice | null
  override: ModelPrice | null
}
export interface ModelPrices { version: number; synced_at?: string; entries: PriceEntry[] }
export interface PriceVersion { version: number }
export interface PricingContext {
  platform: ManagedPricePlatform
  scope: string
  /** App combines models.read AND billing.cost.read. */
  canRead: boolean
  /** App combines models.manage AND billing.rates.update. */
  canManage: boolean
}
export function clonePrice<T>(value: T): T {
  return JSON.parse(JSON.stringify(value))
}
