export interface GeminiCredentials {
  // API Key authentication
  api_key?: string
  base_url?: string

  // OAuth authentication
  access_token?: string
  refresh_token?: string
  oauth_type?: 'code_assist' | 'google_one' | 'ai_studio' | string
  tier_id?:
    | 'google_one_free'
    | 'google_ai_pro'
    | 'google_ai_ultra'
    | 'gcp_standard'
    | 'gcp_enterprise'
    | 'aistudio_free'
    | 'aistudio_paid'
    | 'LEGACY'
    | 'PRO'
    | 'ULTRA'
    | string
  project_id?: string
  token_type?: string
  scope?: string
  expires_at?: string
  model_mapping?: Record<string, string>
}

export type OpenAICompactMode = 'auto' | 'force_on' | 'force_off'

export type OpenAIResponsesMode = 'auto' | 'force_responses' | 'force_chat_completions'

export type OpenAIEndpointCapability = 'chat_completions' | 'embeddings' | 'seedance'

export interface OpenAICompactState {
  openai_compact_mode?: OpenAICompactMode
  openai_compact_supported?: boolean
  openai_compact_checked_at?: string
  openai_compact_last_status?: number
  openai_compact_last_error?: string
}

export type GrokMediaEligibilityMode = 'auto' | 'enabled' | 'disabled'

export interface GrokMediaEligibilityState {
  account_id: number
  mode: GrokMediaEligibilityMode
  eligible: boolean
  reason: string
}
