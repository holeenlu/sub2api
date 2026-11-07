export type TempUnschedulableScope = 'account' | 'model'

export interface TempUnschedulableState {
  until_unix: number
  triggered_at_unix: number
  status_code: number
  matched_keyword: string
  scope?: TempUnschedulableScope
  model?: string
  rule_index: number
  error_message: string
  trigger_count?: number
  trigger_threshold?: number
  trigger_window_minutes?: number
}

export interface TempUnschedulableStatus {
  active: boolean
  state?: TempUnschedulableState
}

export interface UpstreamBillingData {
  object: 'sub2api.key_billing'
  schema_version: 1
  billing_scope: 'token'
  group_rate_multiplier: number
  user_rate_multiplier?: number
  resolved_rate_multiplier: number
  peak_rate_enabled: boolean
  peak_start?: string
  peak_end?: string
  peak_rate_multiplier?: number
  applied_peak_multiplier?: number
  effective_rate_multiplier: number
  timezone?: string
  observed_at: string
}

export type UpstreamBillingProbeStatus = 'ok' | 'unsupported' | 'failed'

export interface UpstreamBillingProbeSnapshot {
  status: UpstreamBillingProbeStatus
  data?: UpstreamBillingData
  received_at?: string
  fresh_until?: string
  last_attempt_at: string
  next_probe_at: string
  failure_count?: number
  http_status?: number
  last_error?: string
  // Value this probe wrote into the account rate multiplier; absent when the
  // probe did not sync a rate.
  synced_rate_multiplier?: number
}

export interface UpstreamBillingProbeSettings {
  enabled: boolean
  interval_minutes: number
}

export interface UpstreamBillingProbeResult {
  account_id: number
  snapshot?: UpstreamBillingProbeSnapshot
  error?: string
}

export interface UpstreamBillingRateSnapshotItem {
  account_id: number
  snapshot?: UpstreamBillingProbeSnapshot | null
}

export interface UpstreamBillingRatesResponse {
  items: UpstreamBillingRateSnapshotItem[]
  total: number
  page: number
  page_size: number
}

export type OllamaCloudUsageStatus = 'ok' | 'unauthorized' | 'failed'

export interface OllamaCloudUsageWindow {
  used_percent: number
  reset_at?: string
  reset_text?: string
}

export interface OllamaCloudUsageModel {
  model: string
  window: 'five_hour' | 'seven_day'
  requests: number
}

export interface OllamaCloudUsageData {
  plan?: string
  five_hour?: OllamaCloudUsageWindow
  seven_day?: OllamaCloudUsageWindow
  balance?: string
  models?: OllamaCloudUsageModel[]
}

export interface OllamaCloudUsageSnapshot {
  status: OllamaCloudUsageStatus
  data?: OllamaCloudUsageData
  fetched_at?: string
  last_attempt_at: string
  next_refresh_at: string
  failure_count?: number
  http_status?: number
  last_error?: string
}

export interface OllamaCloudUsageState {
  account_id: number
  eligible: boolean
  configured: boolean
  auto_refresh_enabled: boolean
  encryption_key_configured: boolean
  snapshot?: OllamaCloudUsageSnapshot
}

export interface OllamaCloudUsageSettings {
  enabled: boolean
  /** Max wait while model requests keep arriving (minutes). */
  interval_minutes: number
  /** Trailing quiet period after the latest model request (minutes). */
  debounce_minutes: number
}

export type OpenCodeGoUsageStatus = 'ok' | 'unauthorized' | 'failed'

export interface OpenCodeGoUsageWindow {
  status?: string
  percent: number
  resets_at?: string
}

export interface OpenCodeGoUsageData {
  rolling?: OpenCodeGoUsageWindow
  weekly?: OpenCodeGoUsageWindow
  monthly?: OpenCodeGoUsageWindow
}

export interface OpenCodeGoUsageSnapshot {
  status: OpenCodeGoUsageStatus
  data?: OpenCodeGoUsageData
  fetched_at?: string
  last_attempt_at?: string
  next_refresh_at?: string
  failure_count?: number
  http_status?: number
  last_error?: string
}

export interface OpenCodeGoUsageState {
  account_id: number
  eligible: boolean
  auto_refresh_enabled: boolean
  snapshot?: OpenCodeGoUsageSnapshot
}

export interface OpenCodeGoUsageSettings {
  enabled: boolean
  /** Max wait while model requests keep arriving (minutes). */
  interval_minutes: number
  /** Trailing quiet period after the latest model request (minutes). */
  debounce_minutes: number
}

export interface AnthropicAccountStatus {
  error_code?: 'authentication' | 'revoked' | 'disabled'
  rate_limited?: boolean
  rate_limit_reset_at?: string | null
  model_rate_limits?: Record<string, string | null>
}

export interface AccountSchedulerGroupScore {
  group_id?: number | null
  group_name?: string
  group_priority?: number | null
  base_score: number
}

export interface CodexUsageSnapshot {
  // Legacy fields (kept for backwards compatibility)
  // NOTE: The naming is ambiguous - actual window type is determined by window_minutes value
  codex_primary_used_percent?: number // Usage percentage (check window_minutes for actual window type)
  codex_primary_reset_after_seconds?: number // Seconds until reset
  codex_primary_window_minutes?: number // Window in minutes
  codex_secondary_used_percent?: number // Usage percentage (check window_minutes for actual window type)
  codex_secondary_reset_after_seconds?: number // Seconds until reset
  codex_secondary_window_minutes?: number // Window in minutes
  codex_primary_over_secondary_percent?: number // Overflow ratio

  // Canonical fields (normalized by backend, use these preferentially)
  codex_5h_used_percent?: number // 5-hour window usage percentage
  codex_5h_reset_after_seconds?: number // Seconds until 5h window reset
  codex_5h_reset_at?: string // 5-hour window absolute reset time (RFC3339)
  codex_5h_window_minutes?: number // 5h window in minutes (should be ~300)
  codex_7d_used_percent?: number // 7-day window usage percentage
  codex_7d_reset_after_seconds?: number // Seconds until 7d window reset
  codex_7d_reset_at?: string // 7-day window absolute reset time (RFC3339)
  codex_7d_window_minutes?: number // 7d window in minutes (should be ~10080)

  codex_usage_updated_at?: string // Last update timestamp
}

/** Read-only account projection from ExcelBPS403RecoveryStatus; states remain open for future values. */
export interface ExcelBPS403RecoveryStatus {
  state: string
  reason?: string
  interval_minutes: number
  disabled_at?: string
  last_probe_at?: string
  next_probe_at?: string
  last_result_current_cycle?: boolean
  last_result?: {
    disabled_at?: string
    attempted_at?: string
    upstream_request_id?: string
    error_code?: string
    error_type?: string
    status: string
    stage: string
    reason: string
    http_status?: number
    duration_ms: number
    completed_at: string
  }
}
