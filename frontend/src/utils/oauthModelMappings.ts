import type { OAuthInitialModelMappings, OAuthModelMappingRule } from '@/api/admin/oauthInitialModelMappings'

export const defaultOAuthInitialModelMappings = (): OAuthInitialModelMappings => ({
  enabled: false,
  platform: 'openai',
  rules: [{ from: 'gpt-5.4', to: 'gpt-5.5' }]
})

export function oauthModelMappingsError(settings: OAuthInitialModelMappings): string | null {
  if (settings.platform !== 'openai') return 'autoBPSOps.oauthMappings.openAIOnly'
  if (settings.rules.length > 100) return 'autoBPSOps.oauthMappings.tooMany'
  const seen = new Set<string>()
  for (const rule of settings.rules) {
    const from = rule.from.trim()
    const to = rule.to.trim()
    if (!from || !to || new TextEncoder().encode(from).length > 256 || new TextEncoder().encode(to).length > 256) {
      return 'autoBPSOps.oauthMappings.invalidModel'
    }
    if (hasWhitespaceOrControl(from + to)) return 'autoBPSOps.oauthMappings.invalidModel'
    if (from.slice(0, -1).includes('*') || to.includes('*')) return 'autoBPSOps.oauthMappings.invalidWildcard'
    if (seen.has(from)) return 'autoBPSOps.oauthMappings.duplicateSource'
    seen.add(from)
  }
  return null
}

function hasWhitespaceOrControl(value: string): boolean {
  for (const character of value) {
    if (/\s/u.test(character)) return true
    const code = character.codePointAt(0) || 0
    if (code < 0x20 || code === 0x7f) return true
  }
  return false
}

export function oauthModelMappingsPayload(settings: OAuthInitialModelMappings): OAuthInitialModelMappings {
  return {
    enabled: settings.enabled,
    platform: 'openai',
    rules: settings.rules.map((rule: OAuthModelMappingRule) => ({ from: rule.from.trim(), to: rule.to.trim() }))
  }
}
