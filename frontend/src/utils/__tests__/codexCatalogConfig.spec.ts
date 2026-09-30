import { describe, expect, it } from 'vitest'
import {
  findCodexCatalogModel,
  formatCodexReasoningEffortTomlLine,
  parseCodexCatalogModels,
  selectCodexConfigModel,
  selectCodexConfigReasoningEffort
} from '@/utils/codexCatalogConfig'

describe('codexCatalogConfig', () => {
  describe('selectCodexConfigModel', () => {
    const catalog = (...slugs: string[]) => slugs.map((slug) => ({ slug }))

    it.each([
      [['gpt-6-luna', 'gpt-6-sol', 'gpt-5.6-terra', 'gpt-6-astra'], 'gpt-6-astra'],
      [['gpt-5.6-terra', 'gpt-5.6-sol'], 'gpt-5.6-sol'],
      [['gpt-5.6-luna', 'gpt-5.6-terra'], 'gpt-5.6-terra'],
      [['gpt-8-luna', 'gpt-5.6-terra'], 'gpt-5.6-terra'],
      [['gpt-7-sol', 'gpt-6-astra'], 'gpt-6-astra'],
      [['gpt-6-astra', 'gpt-7.1-astra'], 'gpt-7.1-astra'],
      [['gpt-5.9-sol', 'gpt-5.10-sol'], 'gpt-5.10-sol'],
      [['gpt-5.6-sol', 'gpt-6-sol'], 'gpt-6-sol'],
      [['gpt-9-mini', 'gpt-5.5'], 'gpt-5.5'],
      [['gpt-9-nano', 'gpt-5.4-mini'], 'gpt-5.4-mini']
    ])('selects from the allowed subset %j', (slugs, expected) => {
      expect(selectCodexConfigModel(catalog(...slugs), 'gpt-6-astra')).toBe(expected)
    })

    it('recognizes known aliases and preserves the exact available ID', () => {
      expect(selectCodexConfigModel(catalog('gpt-6-sol', 'gpt-6'), 'gpt-6-astra')).toBe('gpt-6')
      expect(selectCodexConfigModel(catalog('gpt-5.6-terra', 'gpt-5.6'), 'gpt-6-astra')).toBe('gpt-5.6')
      expect(selectCodexConfigModel(catalog('gpt-6', 'gpt-6-astra'), 'gpt-6-astra')).toBe('gpt-6-astra')
    })

    it('retains provider prefixes and selects the newer snapshot within one version', () => {
      const slugs = ['openai/gpt-6-astra-2026-09-01', 'vendor/openai/gpt-6-astra-2026-09-20']
      expect(selectCodexConfigModel(catalog(...slugs), slugs[0])).toBe(slugs[1])
    })

    it('uses the published recommendation priority without mutating catalog order', () => {
      const models = [
        { slug: 'gpt-6-luna', priority: 0 },
        { slug: 'gpt-6-astra', priority: 99 },
        { slug: 'gpt-6-sol', priority: 1 }
      ]
      expect(selectCodexConfigModel(models, 'gpt-6-sol')).toBe('gpt-6-luna')
      expect(models.map((model) => model.slug)).toEqual(['gpt-6-luna', 'gpt-6-astra', 'gpt-6-sol'])
    })

    it('does not promote unknown or specialized GPT suffixes to flagship status', () => {
      expect(selectCodexConfigModel(catalog('gpt-99-experimental', 'gpt-8-codex-spark', 'gpt-6-sol'), 'gpt-99-experimental'))
        .toBe('gpt-6-sol')
    })

    it('preserves custom/non-OpenAI preference without inventing a cross-provider rank', () => {
      const models = catalog('claude-sonnet-5', 'company/research-model', 'gpt-99-experimental')
      expect(selectCodexConfigModel(models, 'company/research-model')).toBe('company/research-model')
      expect(selectCodexConfigModel(models, 'unavailable-model')).toBe('claude-sonnet-5')
    })

    it('excludes hidden models, wildcard entries and dedicated media/tool models', () => {
      const models = [
        { slug: 'gpt-8-astra', visibility: 'hide' },
        ...catalog('gpt-*', 'openai/gpt-image-2.5-flare', 'gpt-audio', 'gpt-4o-realtime-preview',
          'gemini-3-pro-image', 'text-embedding-3-large', 'omni-moderation-latest',
          'grok-imagine-video', 'codex-auto-review', 'gpt-6-sol')
      ]
      expect(selectCodexConfigModel(models, 'gpt-8-astra')).toBe('gpt-6-sol')
    })

    it('returns no default when the fetched catalog contains no selectable model', () => {
      expect(selectCodexConfigModel([], 'gpt-6-astra')).toBeNull()
      expect(selectCodexConfigModel([
        { slug: 'gpt-6-astra', visibility: 'hide' },
        ...catalog('', '  ', 'gpt-*', 'gpt-image-2.5-sunburst', 'whisper-1')
      ], 'gpt-6-astra')).toBeNull()
    })
  })

  it('parses catalog slugs and finds a model by id', () => {
    const content = JSON.stringify({
      models: [
        { slug: 'glm-5.3', default_reasoning_level: 'none', supported_reasoning_levels: [{ effort: 'none' }] },
        { slug: '  ', supported_reasoning_levels: [] }
      ]
    })
    expect(parseCodexCatalogModels(content).map((model) => model.slug)).toEqual(['glm-5.3'])
    expect(findCodexCatalogModel(content, 'glm-5.3')?.slug).toBe('glm-5.3')
    expect(findCodexCatalogModel(content, 'missing')).toBeUndefined()
  })

  it('omits effort when the descriptor only advertises none', () => {
    expect(selectCodexConfigReasoningEffort({
      slug: 'glm-5.3',
      default_reasoning_level: 'none',
      supported_reasoning_levels: [{ effort: 'none' }]
    })).toBeNull()
    expect(formatCodexReasoningEffortTomlLine(null)).toBe('')
  })

  it('does not emit an effort absent from supported_reasoning_levels', () => {
    expect(selectCodexConfigReasoningEffort({
      slug: 'glm-5.3',
      default_reasoning_level: 'xhigh',
      supported_reasoning_levels: [{ effort: 'none' }]
    })).toBeNull()
  })

  it('uses the catalog default when it is a supported non-none effort', () => {
    expect(selectCodexConfigReasoningEffort({
      slug: 'gpt-5.5',
      default_reasoning_level: 'medium',
      supported_reasoning_levels: [
        { effort: 'low' },
        { effort: 'medium' },
        { effort: 'high' },
        { effort: 'xhigh' }
      ]
    })).toBe('medium')
    expect(formatCodexReasoningEffortTomlLine('medium')).toBe('model_reasoning_effort = "medium"\n')
  })

  it('falls back to the first usable supported effort when default is missing', () => {
    expect(selectCodexConfigReasoningEffort({
      slug: 'custom',
      supported_reasoning_levels: [{ effort: 'none' }, { effort: 'high' }]
    })).toBe('high')
  })
})
