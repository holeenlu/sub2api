import type { CreateAccountRequest } from '@/types'

// Some platforms use explicit identity mappings to override built-in aliases.
// Those entries still control routing, never permissions when a model selection is saved.
export function modelRoutingAliases(platform: string, mapping: Record<string, string>): Record<string, string> {
  if (['antigravity', 'grok', 'gemini'].includes(platform)) return { ...mapping }
  return Object.fromEntries(Object.entries(mapping).filter(([from, to]) => from !== to))
}

// All account creation paths (including OAuth batch import) use this boundary.
// An explicit old-style allowlist becomes the account selection. Empty or
// routing-only mappings create an unrestricted selection, just like editing.
export function withCreatedAccountPolicy(input: CreateAccountRequest): CreateAccountRequest {
  if (input.model_catalog_policy) return input
  const unrestricted: CreateAccountRequest = {
    ...input,
    model_catalog_policy: { models: [] }
  }
  if (input.platform === 'antigravity' ||
      (input.platform === 'openai' && (input.extra?.openai_passthrough === true || input.extra?.openai_oauth_passthrough === true ||
        (input.type === 'oauth' && input.credentials.model_mapping_mode === 'aliases')))) return unrestricted
  const raw = input.credentials.model_mapping
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return unrestricted
  const entries = Object.entries(raw).filter((entry): entry is [string, string] => typeof entry[1] === 'string' && entry[0].trim() !== '' && entry[1].trim() !== '')
  if (entries.length === 0) return unrestricted
  const mapping = Object.fromEntries(entries)
  return {
    ...input,
    model_catalog_policy: { models: Object.keys(mapping) },
    credentials: { ...input.credentials, model_mapping: modelRoutingAliases(input.platform, mapping) }
  }
}
