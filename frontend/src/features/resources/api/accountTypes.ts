import type { Group } from '@/features/access-policy/api/groupTypes'
import type { OpenAICompactState } from './accountCredentialTypes'
import type { ExcelBPS403RecoveryStatus, AccountSchedulerGroupScore, AnthropicAccountStatus, CodexUsageSnapshot, OllamaCloudUsageState, OpenCodeGoUsageState, UpstreamBillingProbeSnapshot } from './accountObservationTypes'
import type { DiagnosticSummary } from './codexDiagnostics'
import type { OpenAIReferralEligibility } from './openaiReferralTypes'
import type { AccountPlatform, AccountType } from './platformTypes'
import type { Proxy } from './proxyTypes'

export interface ClaudeModel {
  id: string
  type: string
  display_name: string
  created_at: string
}

export interface Account {
  excel_bps_403_recovery?: ExcelBPS403RecoveryStatus | null
  authorization_modes?: ('native' | 'bps')[]
  vendor_id?: number | null
  created_by?: number | null
  created_by_name?: string
  created_by_role?: string

  anthropic_status?: AnthropicAccountStatus | null
  codex_diagnostic?: DiagnosticSummary | null
  id: number
  name: string
  notes?: string | null
  platform: AccountPlatform
  type: AccountType
  // 后端响应里 credentials 已脱敏：access_token / refresh_token / id_token /
  // api_key / session_key / cookie / aws_secret_access_key / aws_session_token /
  // service_account_json / service_account / private_key 不会出现，
  // 改为通过 credentials_status.has_<key> 暴露存在性。
  credentials?: Record<string, unknown>
  credentials_status?: Record<string, boolean>
  ollama_cloud_usage?: OllamaCloudUsageState
  opencode_go_usage?: OpenCodeGoUsageState
  // Extra fields including Codex usage, OpenAI compact capability, and model-level rate limits.
  extra?: (CodexUsageSnapshot & OpenAICompactState & {
    model_rate_limits?: Record<string, { rate_limited_at: string; rate_limit_reset_at: string }>
    antigravity_credits_overages?: Record<string, { activated_at: string; active_until: string }>
    upstream_billing_probe_enabled?: boolean
    upstream_billing_rate_sync_enabled?: boolean
    upstream_billing_probe?: UpstreamBillingProbeSnapshot
    codex_reset_credit_snapshot?: {
      available_count?: number
      credits?: { expires_at?: string }[]
    }
    codex_credits_snapshot?: {
      credits: { has_credits: boolean; unlimited: boolean; balance: string | null } | null
      fetched_at: number
    }
    codex_referral_snapshot?: OpenAIReferralEligibility | null
    auto_reset_credit_enabled?: boolean
    auto_reset_credit_5h_threshold?: number
    auto_reset_credit_7d_threshold?: number
    codex_auto_reset_credit_state?: {
      status?: 'checking' | 'available' | 'resetting' | 'success' | 'no_credit' | 'failed'
      trigger_window?: string
      available_count?: number
      checked_at?: string
      last_result_at?: string
      error_code?: string
    }
  } & Record<string, unknown>)
  proxy_id: number | null
  proxy_fallback_origin_id?: number | null
  proxy_fallback_origin_name?: string | null
  concurrency: number
  load_factor?: number | null
  current_concurrency?: number // Real-time concurrency count from Redis
  scheduler_score?: {
    base_score: number
  } | null
  scheduler_scores?: AccountSchedulerGroupScore[] | null
  priority: number
  rate_multiplier?: number // Account billing multiplier (>=0, 0 means free)
  status: 'active' | 'inactive' | 'error' | 'disabled'
  error_message: string | null
  last_used_at: string | null
  expires_at: number | null
  auto_pause_on_expired: boolean
  created_at: string
  updated_at: string
  proxy?: Proxy
  group_ids?: number[] // Groups this account belongs to
  groups?: Group[] // Preloaded group objects

  // Rate limit & scheduling fields
  schedulable: boolean
  rate_limited_at: string | null
  rate_limit_reset_at: string | null
  overload_until: string | null
  temp_unschedulable_until: string | null
  temp_unschedulable_reason: string | null

  // Session window fields (5-hour window)
  session_window_start: string | null
  session_window_end: string | null
  session_window_status: 'allowed' | 'allowed_warning' | 'rejected' | null

  // 5h窗口费用控制（仅 Anthropic OAuth/SetupToken 账号有效）
  window_cost_limit?: number | null
  window_cost_sticky_reserve?: number | null

  // 会话数量控制（仅 Anthropic OAuth/SetupToken 账号有效）
  max_sessions?: number | null
  session_idle_timeout_minutes?: number | null

  // RPM 限制（仅 Anthropic OAuth/SetupToken 账号有效）
  base_rpm?: number | null
  rpm_strategy?: string | null
  rpm_sticky_buffer?: number | null
  user_msg_queue_mode?: string | null  // "serialize" | "throttle" | null

  // TLS指纹伪装（仅 Anthropic OAuth/SetupToken 账号有效）
  enable_tls_fingerprint?: boolean | null
  tls_fingerprint_profile_id?: number | null

  // 会话ID伪装（仅 Anthropic OAuth/SetupToken 账号有效）
  // 启用后将在15分钟内固定 metadata.user_id 中的 session ID
  session_id_masking_enabled?: boolean | null

  // 缓存 TTL 强制替换（仅 Anthropic OAuth/SetupToken 账号有效）
  cache_ttl_override_enabled?: boolean | null
  cache_ttl_override_target?: string | null

  // 自定义 Base URL 中继转发（仅 Anthropic OAuth/SetupToken 账号有效）
  custom_base_url_enabled?: boolean | null
  custom_base_url?: string | null

  // API Key 账号配额限制
  quota_limit?: number | null
  quota_used?: number | null
  quota_daily_limit?: number | null
  quota_daily_used?: number | null
  quota_weekly_limit?: number | null
  quota_weekly_used?: number | null

  // 配额固定时间重置配置
  quota_daily_reset_mode?: 'rolling' | 'fixed' | null
  quota_daily_reset_hour?: number | null
  quota_weekly_reset_mode?: 'rolling' | 'fixed' | null
  quota_weekly_reset_day?: number | null
  quota_weekly_reset_hour?: number | null
  quota_reset_timezone?: string | null
  quota_daily_reset_at?: string | null
  quota_weekly_reset_at?: string | null

  // 运行时状态（仅当启用对应限制时返回）
  current_window_cost?: number | null // 当前窗口费用
  active_sessions?: number | null // 当前活跃会话数
  current_rpm?: number | null // 当前分钟 RPM 计数

  // 影子账号关系（spark 维度影子）
  parent_account_id?: number | null
  quota_dimension?: string
  // 影子账号回填的母账号信息（仅影子非空）
  parent_email?: string
  parent_plan_type?: string
  parent_privacy_mode?: string
  parent_subscription_expires_at?: string
  parent_chatgpt_account_id?: string
}

export type AccountListItem = Omit<Account, 'groups'>

export interface CreateAccountRequest {
  name: string
  notes?: string | null
  platform: AccountPlatform
  type: AccountType
  credentials: Record<string, unknown>
  extra?: Record<string, unknown>
  proxy_id?: number | null
  concurrency?: number
  load_factor?: number | null
  priority?: number
  rate_multiplier?: number // Account billing multiplier (>=0, 0 means free)
  group_ids?: number[]
  expires_at?: number | null
  auto_pause_on_expired?: boolean
  upstream_billing_probe_enabled?: boolean
  confirm_mixed_channel_risk?: boolean
}

export interface UpdateAccountRequest {
  name?: string
  notes?: string | null
  type?: AccountType
  credentials?: Record<string, unknown>
  extra?: Record<string, unknown>
  proxy_id?: number | null
  concurrency?: number
  load_factor?: number | null
  priority?: number
  rate_multiplier?: number // Account billing multiplier (>=0, 0 means free)
  schedulable?: boolean
  status?: 'active' | 'inactive' | 'error'
  group_ids?: number[]
  expires_at?: number | null
  auto_pause_on_expired?: boolean
  upstream_billing_probe_enabled?: boolean
  upstream_billing_rate_sync_enabled?: boolean
  confirm_mixed_channel_risk?: boolean
}

export interface CheckMixedChannelRequest {
  platform: AccountPlatform
  group_ids: number[]
  account_id?: number
}

export interface MixedChannelWarningDetails {
  group_id: number
  group_name: string
  current_platform: string
  other_platform: string
}

export interface CheckMixedChannelResponse {
  has_risk: boolean
  error?: string
  message?: string
  details?: MixedChannelWarningDetails
}
