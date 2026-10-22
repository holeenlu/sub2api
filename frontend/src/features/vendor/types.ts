import type { UsageAuthorizationMode } from '@/types'
import type { GroupPlatform, ProxyQualityCheckResult } from '@/types'
export type VendorPlatform = 'anthropic' | 'openai'
export type OpenAIAuthorizationMode = 'native' | 'bps'
export type AccountAuthorizationAction = 'create' | 'attach' | 'reauthorize'
export interface AccountAuthorizationPatch {
  native_enabled?: boolean
  bps_enabled?: boolean
  bps_all_models?: boolean
  bps_models?: string[]
}
export interface AccountAuthorization {
  mode: OpenAIAuthorizationMode
  authorized: boolean
  enabled: boolean
  status: 'unconfigured' | 'active' | 'expired' | 'disabled' | 'policy_revoked' | 'error'
  expires_at?: string
  can_refresh: boolean
}
export interface VendorGroup {
  id: number
  name: string
  platform?: GroupPlatform
  is_default?: boolean
}
export interface Vendor {
  account_defaults: { concurrency: number; priority: number }
  id: number
  user_id: number
  tier?: 1 | 2
  parent_vendor_id?: number | null
  username?: string
  name: string
  status: string
  policy_version: number
  supported_auth_methods?: Partial<Record<VendorPlatform, string[]>>
  openai_authorization_modes?: OpenAIAuthorizationMode[]
  proxy_protocols?: string[]
  platforms: VendorPlatform[]
  group_ids: number[]
  groups: VendorGroup[]
  account_limit: number
  proxy_limit: number
  allow_direct: boolean
  contact_name: string
  settlement_email: string
  binance_id: string
  default_coefficient: string
  group_coefficients: Record<string, string>
  contract_version: number
  account_count?: number
  proxy_count?: number
  account_tree_count?: number
  proxy_tree_count?: number
  email?: string
  created_at?: string
}
export interface Page<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}
export type Query = Record<string, string | number | boolean | undefined>
export interface ResourceScope {
  admin: boolean
  vendorId?: number
  readOnly?: boolean
  children?: boolean
}
export interface AccountOverview {
  today_usage?: Metrics | null
  all_time_usage?: Metrics | null
  abnormal?: number
  high_quota?: number
  quota_near_limit_percent?: number
  quota_full_percent?: number
  quota_distribution?: Record<
    string,
    { normal: number; near_limit: number; full: number; unknown: number }
  >
  failures?: Record<string, number>
  recent_minute?: Metrics
  rpm?: number
  tpm?: number
  tpm_with_cache?: number
  current_concurrency?: number | null
  maximum_concurrency?: number
  concurrency_at?: string
  timezone?: string
  total: number
  available: number
  paused: number
  cooling: number
  disabled: number
  collected_at: string
}
export interface AccountPage extends Page<VendorAccount> {
  overview?: AccountOverview
}
export interface VendorAccount {
  vendor_id?: number
  vendor_name?: string
  failure_category?: string
  today_usage?: Metrics | null
  all_time_usage?: Metrics | null
  recent_minute?: Metrics | null
  rpm?: number | null
  tpm?: number | null
  tpm_with_cache?: number | null
  model_limits?: { scope: string; resets_at: string }[] | null
  temp_unschedulable_until?: string | null
  plan_type?: string
  subscription_type?: string
  subscription_expires_at?: string
  authorization_expires_at?: string
  can_refresh?: boolean | null
  current_concurrency?: number | null
  statistics_at?: string
  timezone?: string
  id: number
  name: string
  platform: VendorPlatform
  type: string
  method?: string
  authorization_modes?: OpenAIAuthorizationMode[]
  authorizations?: AccountAuthorization[]
  bps_models?: string[] | null
  vendor_notes?: string
  email?: string
  proxy_id: number | null
  proxy?: {
    id: number
    name: string
    protocol: string
    host?: string
    port?: number
    endpoint?: string
    country_code?: string
    city?: string
    region_source?: string
  } | null
  group_ids: number[]
  groups: VendorGroup[]
  concurrency: number
  priority: number
  status: string
  archived: boolean
  archived_at: string | null
  schedulable: boolean
  vendor_paused_at?: string | null
  blockers?: string[] | null
  rate_limit_reset_at?: string | null
  overload_until?: string | null
  expires_at?: string | null
  created_at: string
  updated_at?: string
  quota?: Record<string, unknown>
}
export type ProxyQuality = ProxyQualityCheckResult
export interface VendorProxy {
  id: number
  name: string
  protocol: string
  host: string
  port: number
  endpoint?: string
  has_username?: boolean
  has_password?: boolean
  status: string
  effective_status?: string
  vendor_id?: number
  vendor_name?: string
  created_by?: number
  vendor_notes?: string
  region_label_country_code?: string
  region_label_city?: string
  country_code?: string
  city?: string
  region_source?: string
  config_version: number
  fallback_mode?: 'none' | 'direct' | 'proxy'
  backup_proxy_id?: number | null
  expiry_warn_days?: number
  expires_at?: string
  created_at?: string
  updated_at?: string
  account_count?: number
  account_ids?: number[]
  latency_ms?: number
  tested_at?: string
  quality?: ProxyQuality
  references?: {
    accounts: number[]
    backups: number[]
    oauth: number
    inflight: number
    total: number
  }
}
export interface ProxyPage extends Page<VendorProxy> {
  capabilities?: {
    protocols?: string[]
    max_import_rows?: number
    max_import_bytes?: number
    max_batch_size?: number
    max_scope_rows?: number
    probe_concurrency?: number
  }
  summary?: {
    total: number
    occupied: number
    occupied_unavailable: number
    unoccupied: number
  }
  snapshot_at?: string
  timezone?: string
}
export interface OAuthLease {
  auth_url: string
  session_id: string
  expires_at: string
}
export interface ImportRow {
  proxy?: VendorProxy
  id?: number
  index?: number
  line?: number
  status: string
  code?: string
  message?: string
  account_id?: number
  name?: string
  platform?: string
  proxy_id?: number | null
  direct?: boolean
  group_ids?: number[]
}
export interface ImportResult {
  preview_id?: string
  expires_at?: string
  items?: ImportRow[]
  rows?: ImportRow[]
  valid_count?: number
  duplicate_count?: number
  invalid_count?: number
  input_digest?: string
}
export interface Metrics {
  requests: number | null
  successful_requests?: number | null
  failed_requests?: number | null
  input_tokens: number | null
  output_tokens: number | null
  cache_creation_tokens: number | null
  cache_read_tokens: number | null
  total_tokens?: number | null
  cache_tokens?: number | null
  cost_usd: string | null
  opus_cost_usd?: string | null
  fable_cost_usd?: string | null
  cache_hit_rate: number | null
  success_rate?: number | null
  avg_duration_ms?: number | null
  avg_first_token_ms?: number | null
}
export interface UsageRow extends Metrics {
  authorization_mode?: UsageAuthorizationMode | 'unknown' | 'mixed' | null
  usage_log_id?: number
  vendor_id?: number
  vendor_name?: string
  account_id?: number
  account_name?: string
  group_id?: number
  group_name?: string
  requested_model?: string
  upstream_model?: string
  date?: string
  account_deleted?: boolean
  account_archived: boolean
  account_status?: string
  created_at?: string
  platform?: string
  status?: string
  model?: string
  result?: string
}
export interface UsageResponse extends Page<UsageRow> {
  summary: Metrics
  account_archived?: boolean
  dimension?: string
  timezone: string
  snapshot_at: string
  watermark?: number
  start_at?: string
  end_at?: string
  source?: string
  unknown_fields?: string[]
  trends?: UsageRow[]
  models?: UsageRow[]
  model_total?: number
  model_page?: number
  model_page_size?: number
  results?: UsageRow[]
  fixed_windows?: Record<string, Metrics>
  recent_minute?: Metrics
}
export interface SettlementPayment {
  id: number
  amount: string
  paid_at: string
  channel: string
  reference: string
  reversed_amount?: string
  reason?: string
}
export interface SettlementLine {
  group_id?: number
  group_name?: string
  account_name?: string
  cost_usd: string
  coefficient?: string
  amount?: string
  total?: string
}
export interface Statement {
  id: number
  number: string
  vendor_id: number
  payer_vendor_id?: number | null
  status: string
  version: number
  start_at: string
  end_at: string
  timezone: string
  snapshot_at: string
  watermark: number
  contract_version: number
  payee: {
    vendor_name: string
    contact_name: string
    settlement_email: string
    binance_id: string
  }
  exchange_rate: string
  exchange_rate_source: string
  coefficient_unit: string
  settlement_currency: string
  cost_usd: string
  subtotal: string
  adjustment: string
  adjustment_reason: string
  total: string
  credited?: string
  payment_total?: number
  paid: string
  unpaid: string
  snapshot_hash: string
  parent_statement_id?: number
  created_at: string
  confirmed_at?: string
  lines?: SettlementLine[]
  payments?: SettlementPayment[]
  allocations?: UsageRow[]
  allocation_total?: number
  page?: number
  page_size?: number
  late_usage?: number | Record<string, unknown>
}
export interface SettlementPreview {
  vendor_id: number
  preview_token: string
  expires_at: string
  error?: string
  statement?: Statement
  available_count: number
  occupied_count: number
}
export interface AuditEvent {
  id: number
  created_at: string
  action: string
  actor_user_id?: number | null
  actor_role: string
  vendor_id: number
  object_id: string
  result: string
}

export interface IdentityReconciliationRow {
  account_id: number
  status: string
  message?: string
}
export interface IdentityReconciliationResult {
  rows: IdentityReconciliationRow[]
  next_after_id: number
  done: boolean
}
