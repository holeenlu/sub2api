import { describe, expect, it } from 'vitest'
import { docsModelCatalog } from '../modelCatalog'
import { DOCS_PRICING_RATE, halfOfficialPrice } from '../pricingSnapshot'

describe('API docs pricing snapshot', () => {
  it('contains the TapModels launch catalog exactly once', () => {
    const ids = docsModelCatalog.map((model) => model.id)

    expect(new Set(ids).size).toBe(ids.length)
    expect(ids).toEqual([
      'gpt-6-astra', 'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna', 'gpt-5.5',
      'claude-fable-5-1', 'claude-fable-5', 'claude-opus-5', 'claude-opus-4-8', 'claude-sonnet-5',
      'gpt-image-2.5-flare', 'gpt-image-2.5-sunburst'
    ])
  })

  it('applies the official 0.5x multiplier per price field', () => {
    expect(DOCS_PRICING_RATE).toBe(0.5)
    expect(halfOfficialPrice(20)).toBe(10)
    expect(halfOfficialPrice(0.4)).toBe(0.2)
    expect(halfOfficialPrice(undefined)).toBeUndefined()
  })

  it('keeps cache and image price categories separate', () => {
    const sol = docsModelCatalog.find((model) => model.id === 'gpt-5.6-sol')!
    const fable = docsModelCatalog.find((model) => model.id === 'claude-fable-5-1')!
    const flare = docsModelCatalog.find((model) => model.id === 'gpt-image-2.5-flare')!

    expect(sol.tokenPricing).toMatchObject({ input: 4, cachedInput: 0.4, cacheWrite: 5, output: 20 })
    expect(fable.tokenPricing).toMatchObject({ cachedInput: 0.25, cacheWrite: 12.5, cacheWrite1h: 20 })
    expect(flare.imagePricing).toEqual({ textInput: 5, cachedTextInput: 1.25, imageInput: 8, cachedImageInput: 2, imageOutput: 30 })
    expect(halfOfficialPrice(flare.imagePricing!.imageOutput)).toBe(15)
  })
})
