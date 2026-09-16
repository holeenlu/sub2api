import { docsModelCatalog } from './modelCatalog'

export const DOCS_PRICING_RATE = 0.5
export const DOCS_PRICING_VERIFIED_AT = '2026-09-15'
export const docsPricingModels = docsModelCatalog.filter((model) => model.tokenPricing || model.imagePricing)

export function halfOfficialPrice(value: number | undefined): number | undefined {
  return value == null ? undefined : value * DOCS_PRICING_RATE
}

export function formatPerMillion(value: number | undefined): string {
  if (value == null) return '—'
  return `$${value.toLocaleString('en-US', { minimumFractionDigits: 0, maximumFractionDigits: 3 })}`
}
