import type { DocsModelCatalogEntry } from './types'

const openAIChatEndpoints = ['/v1/responses', '/v1/chat/completions', '/v1/messages']

export const docsModelCatalog: DocsModelCatalogEntry[] = [
  {
    id: 'gpt-6-astra', displayName: 'GPT-6 Astra', platform: 'openai', kind: 'chat',
    summaryKey: 'docs.models.summaries.astra', contextWindow: 1_050_000, maxOutputTokens: 128_000,
    endpoints: openAIChatEndpoints, features: ['reasoning', 'vision', 'tools', 'streaming', 'prompt-cache'],
    sourceUrl: 'https://developers.openai.com/api/docs/models/gpt-6-astra',
    tokenPricing: { input: 10, cachedInput: 1, cacheWrite: 12.5, output: 50, longContext: { threshold: 272_000, input: 20, cachedInput: 2, cacheWrite: 25, output: 75 } }
  },
  {
    id: 'gpt-5.6-sol', displayName: 'GPT-5.6 Sol', platform: 'openai', kind: 'chat',
    summaryKey: 'docs.models.summaries.sol', contextWindow: 1_050_000, maxOutputTokens: 128_000,
    endpoints: openAIChatEndpoints, features: ['reasoning', 'vision', 'tools', 'streaming', 'prompt-cache'],
    sourceUrl: 'https://developers.openai.com/api/docs/models/gpt-5.6-sol',
    tokenPricing: { input: 4, cachedInput: 0.4, cacheWrite: 5, output: 20, longContext: { threshold: 272_000, input: 8, cachedInput: 0.8, cacheWrite: 10, output: 30 } }
  },
  {
    id: 'gpt-5.6-terra', displayName: 'GPT-5.6 Terra', platform: 'openai', kind: 'chat',
    summaryKey: 'docs.models.summaries.terra', contextWindow: 1_050_000, maxOutputTokens: 128_000,
    endpoints: openAIChatEndpoints, features: ['reasoning', 'vision', 'tools', 'streaming', 'prompt-cache'],
    sourceUrl: 'https://developers.openai.com/api/docs/models/gpt-5.6-terra',
    tokenPricing: { input: 2, cachedInput: 0.2, cacheWrite: 2.5, output: 12, longContext: { threshold: 272_000, input: 4, cachedInput: 0.4, cacheWrite: 5, output: 18 } }
  },
  {
    id: 'gpt-5.6-luna', displayName: 'GPT-5.6 Luna', platform: 'openai', kind: 'chat',
    summaryKey: 'docs.models.summaries.luna', contextWindow: 1_050_000, maxOutputTokens: 128_000,
    endpoints: openAIChatEndpoints, features: ['reasoning', 'vision', 'tools', 'streaming', 'prompt-cache'],
    sourceUrl: 'https://developers.openai.com/api/docs/models/gpt-5.6-luna',
    tokenPricing: { input: 0.2, cachedInput: 0.02, cacheWrite: 0.25, output: 1.2, longContext: { threshold: 272_000, input: 0.4, cachedInput: 0.04, cacheWrite: 0.5, output: 1.8 } }
  },
  {
    id: 'gpt-5.5', displayName: 'GPT-5.5', platform: 'openai', kind: 'chat',
    summaryKey: 'docs.models.summaries.gpt55', contextWindow: 1_050_000, maxOutputTokens: 128_000,
    endpoints: openAIChatEndpoints, features: ['reasoning', 'vision', 'tools', 'streaming', 'prompt-cache'],
    sourceUrl: 'https://developers.openai.com/api/docs/models/gpt-5.5',
    tokenPricing: { input: 5, cachedInput: 0.5, output: 30, longContext: { threshold: 272_000, input: 10, cachedInput: 1, output: 45 } }
  },
  {
    id: 'claude-fable-5-1', displayName: 'Claude Fable 5.1', platform: 'anthropic', kind: 'chat',
    summaryKey: 'docs.models.summaries.fable51', contextWindow: 1_000_000, maxOutputTokens: 128_000, cacheMinimumTokens: 512,
    endpoints: ['/v1/messages'], features: ['reasoning', 'vision', 'tools', 'streaming', 'prompt-cache'],
    sourceUrl: 'https://platform.claude.com/docs/en/models/fable-5-1/overview',
    tokenPricing: { input: 10, cachedInput: 0.25, cacheWrite: 12.5, cacheWrite1h: 20, output: 50 }
  },
  {
    id: 'claude-fable-5', displayName: 'Claude Fable 5', platform: 'anthropic', kind: 'chat',
    summaryKey: 'docs.models.summaries.fable5', contextWindow: 1_000_000, maxOutputTokens: 128_000, cacheMinimumTokens: 512,
    endpoints: ['/v1/messages'], features: ['reasoning', 'vision', 'tools', 'streaming', 'prompt-cache'],
    sourceUrl: 'https://platform.claude.com/docs/en/about-claude/models/overview',
    tokenPricing: { input: 10, cachedInput: 1, cacheWrite: 12.5, cacheWrite1h: 20, output: 50 }
  },
  {
    id: 'claude-opus-5', displayName: 'Claude Opus 5', platform: 'anthropic', kind: 'chat',
    summaryKey: 'docs.models.summaries.opus5', contextWindow: 1_000_000, maxOutputTokens: 128_000, cacheMinimumTokens: 512,
    endpoints: ['/v1/messages'], features: ['reasoning', 'vision', 'tools', 'streaming', 'prompt-cache'],
    sourceUrl: 'https://platform.claude.com/docs/en/about-claude/models/overview',
    tokenPricing: { input: 5, cachedInput: 0.5, cacheWrite: 6.25, cacheWrite1h: 10, output: 25 }
  },
  {
    id: 'claude-opus-4-8', displayName: 'Claude Opus 4.8', platform: 'anthropic', kind: 'chat',
    summaryKey: 'docs.models.summaries.opus48', contextWindow: 1_000_000, maxOutputTokens: 128_000, cacheMinimumTokens: 1_024,
    endpoints: ['/v1/messages'], features: ['reasoning', 'vision', 'tools', 'streaming', 'prompt-cache'],
    sourceUrl: 'https://platform.claude.com/docs/en/about-claude/models/overview',
    tokenPricing: { input: 5, cachedInput: 0.5, cacheWrite: 6.25, cacheWrite1h: 10, output: 25 }
  },
  {
    id: 'claude-sonnet-5', displayName: 'Claude Sonnet 5', platform: 'anthropic', kind: 'chat',
    summaryKey: 'docs.models.summaries.sonnet5', contextWindow: 1_000_000, maxOutputTokens: 128_000, cacheMinimumTokens: 1_024,
    endpoints: ['/v1/messages'], features: ['reasoning', 'vision', 'tools', 'streaming', 'prompt-cache'],
    sourceUrl: 'https://platform.claude.com/docs/en/about-claude/models/overview',
    tokenPricing: { input: 2, cachedInput: 0.2, cacheWrite: 2.5, cacheWrite1h: 4, output: 10 }
  },
  {
    id: 'gpt-image-2.5-flare', displayName: 'GPT-Image-2.5 Flare', platform: 'openai', kind: 'image',
    summaryKey: 'docs.models.summaries.flare', endpoints: ['/v1/images/generations', '/v1/images/edits'],
    features: ['image-input', 'image-output', 'prompt-cache'],
    sourceUrl: 'https://developers.openai.com/api/docs/models/gpt-image-2.5-flare',
    imagePricing: { textInput: 5, cachedTextInput: 1.25, imageInput: 8, cachedImageInput: 2, imageOutput: 30 }
  },
  {
    id: 'gpt-image-2.5-sunburst', displayName: 'GPT-Image-2.5 Sunburst', platform: 'openai', kind: 'image',
    summaryKey: 'docs.models.summaries.sunburst', endpoints: ['/v1/images/generations', '/v1/images/edits'],
    features: ['image-input', 'image-output', 'prompt-cache'],
    sourceUrl: 'https://developers.openai.com/api/docs/models/gpt-image-2.5-sunburst',
    imagePricing: { textInput: 5, cachedTextInput: 1.25, imageInput: 8, cachedImageInput: 2, imageOutput: 30 }
  }
]

export const docsModelCatalogById = new Map(docsModelCatalog.map((model) => [model.id, model]))
