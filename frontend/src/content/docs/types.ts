export type DocsLocale = 'en' | 'zh' | 'zh-TW' | 'ja'
export type DocsModelKind = 'chat' | 'image'
export type DocsPlatform = 'openai' | 'anthropic'

export interface DocsTokenPricing {
  input: number
  cachedInput?: number
  cacheWrite?: number
  cacheWrite1h?: number
  output: number
  longContext?: {
    threshold: number
    input: number
    cachedInput?: number
    cacheWrite?: number
    output: number
  }
}

export interface DocsImagePricing {
  textInput: number
  cachedTextInput: number
  imageInput: number
  cachedImageInput: number
  imageOutput: number
}

export interface DocsModelCatalogEntry {
  id: string
  displayName: string
  platform: DocsPlatform
  kind: DocsModelKind
  summaryKey: string
  contextWindow?: number
  maxOutputTokens?: number
  cacheMinimumTokens?: number
  endpoints: string[]
  features: string[]
  sourceUrl: string
  tokenPricing?: DocsTokenPricing
  imagePricing?: DocsImagePricing
}

export interface DocsNavItem {
  path: string
  titleKey: string
  descriptionKey: string
  icon: 'home' | 'bolt' | 'key' | 'dollar' | 'exclamationTriangle' | 'server' | 'chat' | 'sparkles' | 'cube'
  articleId?: string
  pricingKind?: DocsModelKind
  exampleKind?: 'chat' | 'responses' | 'messages' | 'image' | 'models'
}

export interface DocsNavGroup {
  titleKey: string
  items: DocsNavItem[]
}

export interface DocsCodeExample {
  id: 'curl' | 'python' | 'node'
  label: string
  language: string
  code: string
}
