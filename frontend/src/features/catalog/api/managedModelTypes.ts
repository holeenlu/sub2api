export type ManagedModelPlatform = 'anthropic' | 'openai'

export interface ManagedModel {
  id: string
  display_name: string
  kind: 'chat' | 'image'
  enabled: boolean
  sort: number
  aliases: string[]
  family?: string
  source_account_ids?: number[]
  capabilities?: Record<string, unknown>
  params: { max_tokens_default: number; max_tokens_cap: number; thinking_fallback_budget: number; on_adaptive: string; on_enabled: string; default_effort?: string }
  betas: { pass_context_1m?: boolean; required?: string[]; drop?: string[]; allow_client: boolean }
  catalog?: { id: string; display_name?: string; description?: string; reasoning?: boolean; context_window?: number; max_context_window?: number; max_output_tokens?: number; supported_reasoning_levels?: string[]; default_reasoning_level?: string; input_modalities?: string[]; codex_tool_capabilities?: Record<string, unknown> }
}
export interface ModelPolicy {
  version: number
  platform: ManagedModelPlatform
  source: string
  updated_at?: string
  catalog_mode: string
  models: Record<string, ManagedModel>
  defaults: { enabled: boolean; max_tokens: number; thinking_fallback_budget: number; strip_context_1m: boolean; context_1m_whitelist: string[]; normalize_thinking: boolean }
  aliases?: Record<string, string>
}
export interface ModelMappings { version: number; updated_at?: string; mappings: Record<string, string> }
export interface ModelSource { id: number; name: string; platform: string; type: string }

