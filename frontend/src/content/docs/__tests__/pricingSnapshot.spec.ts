import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { docsModelCatalog } from '../modelCatalog'
import { formatPerMillion } from '../pricingSnapshot'

describe('API docs pricing snapshot', () => {
  it('contains each documented model exactly once', () => {
    const ids = docsModelCatalog.map((model) => model.id)

    expect(new Set(ids).size).toBe(ids.length)
    expect(ids).toEqual([
      'gpt-6-astra', 'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna', 'gpt-5.5',
      'claude-fable-5-1', 'claude-fable-5', 'claude-opus-5', 'claude-opus-4-8', 'claude-sonnet-5',
      'gpt-image-2.5-flare', 'gpt-image-2.5-sunburst'
    ])
  })

  it('formats prices without applying a fixed marketing discount', () => {
    expect(formatPerMillion(20)).toBe('$20')
    expect(formatPerMillion(0)).toBe('$0')
    expect(formatPerMillion(undefined)).toBe('—')
  })

  it.each(['zh', 'zh-TW', 'en', 'ja'])('keeps %s marketing copy independent of configured group discounts', (locale) => {
    const sources = [
      resolve(`src/content/docs/${locale}/pricing.md`),
      resolve(`src/i18n/locales/${locale}/docs.ts`),
      resolve(`src/i18n/locales/${locale}/landing.ts`),
    ]
    const copy = sources.map((source) => readFileSync(source, 'utf8')).join('\n')
    expect(copy).not.toMatch(/(?:[×*]\s*0\.[58]|0\.[58]\s*x|五折|半价|半價|半額|half[- ]price|halved prices|50% off)/i)
  })

  it('keeps cache and image price categories separate', () => {
    const sol = docsModelCatalog.find((model) => model.id === 'gpt-5.6-sol')!
    const fable = docsModelCatalog.find((model) => model.id === 'claude-fable-5-1')!
    const flare = docsModelCatalog.find((model) => model.id === 'gpt-image-2.5-flare')!

    expect(sol.tokenPricing).toMatchObject({ input: 4, cachedInput: 0.4, cacheWrite: 5, output: 20 })
    expect(fable.tokenPricing).toMatchObject({ cachedInput: 0.25, cacheWrite: 12.5, cacheWrite1h: 20 })
    expect(flare.imagePricing).toEqual({ textInput: 5, cachedTextInput: 1.25, imageInput: 8, cachedImageInput: 2, imageOutput: 30 })
  })
})
