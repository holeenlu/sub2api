import type { CreateAccountRequest } from '@/types'

// Some platforms use explicit identity mappings to override built-in aliases.
// Those entries still control routing, never permissions in fixed/follow mode.
export function modelRoutingAliases(platform: string, mapping: Record<string, string>): Record<string, string> {
  if (['antigravity', 'grok', 'gemini'].includes(platform)) return { ...mapping }
  return Object.fromEntries(Object.entries(mapping).filter(([from, to]) => from !== to))
}

// All account creation paths (including OAuth batch import) use this boundary.
// An explicit old-style allowlist becomes a fixed policy; empty/unrestricted and
// platform-default mappings keep their old semantics until explicitly changed.
export function withCreatedAccountPolicy(input: CreateAccountRequest): CreateAccountRequest {
  if (input.model_catalog_policy || input.platform === 'antigravity' ||
      (input.platform === 'openai' && (input.extra?.openai_passthrough === true || input.extra?.openai_oauth_passthrough === true ||
        (input.type === 'oauth' && input.credentials.model_mapping_mode === 'aliases')))) return input
  const raw = input.credentials.model_mapping
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return input
  const entries = Object.entries(raw).filter((entry): entry is [string, string] => typeof entry[1] === 'string' && entry[0].trim() !== '' && entry[1].trim() !== '')
  if (entries.length === 0) return input
  const mapping = Object.fromEntries(entries)
  return {
    ...input,
    model_catalog_policy: { mode: 'fixed', models: Object.keys(mapping), excluded: [] },
    credentials: { ...input.credentials, model_mapping: modelRoutingAliases(input.platform, mapping) }
  }
}
