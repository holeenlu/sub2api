/** Fixed model descriptions and official links; displayed prices come from channel data. */
export const PRICING_VERIFIED_AT = '2026-09-09'

export interface OfficialModel {
  model: string
  descriptionKey: string
  source: string
}

export const OFFICIAL_MODEL_GROUPS: readonly {
  provider: string
  platform: 'anthropic' | 'openai'
  pricingSource: string
  noteKey: string
  models: readonly OfficialModel[]
}[] = [
  {
    provider: 'Anthropic', platform: 'anthropic',
    pricingSource: 'https://platform.claude.com/docs/en/about-claude/pricing',
    noteKey: 'anthropicNote',
    models: [
      { model: 'claude-fable-5-1', descriptionKey: 'fable51',
        source: 'https://platform.claude.com/docs/en/models/fable-5-1/overview' },
      { model: 'claude-fable-5', descriptionKey: 'fable5',
        source: 'https://www.anthropic.com/news/claude-fable-5-mythos-5' },
      { model: 'claude-opus-5', descriptionKey: 'opus5',
        source: 'https://platform.claude.com/docs/en/models/opus-5/overview' },
      { model: 'claude-opus-4-8', descriptionKey: 'opus48',
        source: 'https://www.anthropic.com/claude/opus?via=free' },
      { model: 'claude-sonnet-5', descriptionKey: 'sonnet5',
        source: 'https://platform.claude.com/docs/en/models/sonnet-5/overview' }
    ]
  },
  {
    provider: 'OpenAI', platform: 'openai',
    pricingSource: 'https://developers.openai.com/api/docs/pricing',
    noteKey: 'openaiNote',
    models: [
      { model: 'gpt-6-astra', descriptionKey: 'astra',
        source: 'https://developers.openai.com/api/docs/models/gpt-6-astra' },
      { model: 'gpt-5.6-sol', descriptionKey: 'sol',
        source: 'https://developers.openai.com/api/docs/models/gpt-5.6-sol' },
      { model: 'gpt-5.6-terra', descriptionKey: 'terra',
        source: 'https://developers.openai.com/api/docs/models/gpt-5.6-terra' },
      { model: 'gpt-5.6-luna', descriptionKey: 'luna',
        source: 'https://developers.openai.com/api/docs/models/gpt-5.6-luna' }
    ]
  }
]
