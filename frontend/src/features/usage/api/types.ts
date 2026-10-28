import type { User } from '@/features/identity/api/profileTypes';
import type { ApiKey } from '@/features/keys/api/types';
import type { Group } from '@/features/access-policy/api/groupTypes';
import type { UserSubscription } from '@/features/subscriptions/api/types';

// ==================== Usage & Redeem Types ====================

export type UsageRequestType = 'unknown' | 'sync' | 'stream' | 'ws_v2' | 'cyber' | 'live'

export type ImageSizeSource = 'output' | 'input' | 'default' | 'legacy'

export type ImageSizeBreakdown = Record<string, number>

export type UsageAuthorizationMode = 'native' | 'bps' | 'cloud'

export interface UsageSamplingDetails {
  version: number
  scope: 'cloud_turn' | 'cloud_response'
  pricing_mode: string
  hosted_cost: number
  hosted_actual_cost: number
  samples: Array<{ input_tokens: number; output_tokens: number; cache_read_tokens: number; cache_creation_tokens: number; reasoning_tokens: number; total_cost: number | null; actual_cost: number | null }>
}

export interface UsageLatencyMetrics {
  version: 1
  first_event_ms: number | null
  first_content_ms: number | null
  first_text_ms: number | null
  duration_ms: number
}

export interface UsageLog {
  billing_status?: 'unknown' | 'pending' | 'pending_pricing' | 'settled' | 'free' | 'lower_bound' | 'unmetered'
  id: number
  user_id: number
  api_key_id: number
  request_id: string
  model: string
  service_tier?: string | null
  reasoning_effort?: string | null
  inbound_endpoint?: string | null
  upstream_endpoint?: string | null

  group_id: number | null
  subscription_id: number | null

  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  cache_creation_5m_tokens: number
  cache_creation_1h_tokens: number
  cache_creation_30m_tokens: number
  cache_creation_12h_tokens: number

  input_cost: number
  output_cost: number
  cache_creation_cost: number
  cache_read_cost: number
  total_cost: number
  actual_cost: number
  rate_multiplier: number
  long_context_billing_applied: boolean
  billing_type: number

  request_type?: UsageRequestType
  stream: boolean
  openai_ws_mode?: boolean
  native_compaction_v2: boolean
  duration_ms: number | null
  first_token_ms: number | null
  latency_metrics?: UsageLatencyMetrics | null
  sampling_details?: UsageSamplingDetails | null

  // 图片生成字段
  image_count: number
  image_size: string | null
  image_input_size: string | null
  image_output_size: string | null
  image_size_source: ImageSizeSource | null
  image_size_breakdown: ImageSizeBreakdown | null
  image_input_tokens: number
  image_input_cost: number
  image_output_tokens: number
  image_output_cost: number

  // User-Agent
  user_agent: string | null
  ip_address?: string | null

  // Cache TTL Override
  cache_ttl_overridden: boolean

  // 计费模式
  billing_mode?: string | null

  created_at: string

  user?: User
  api_key?: ApiKey
  group?: Group
  subscription?: UserSubscription
}

export interface UsageLogAccountSummary {
  id: number
  name: string
}

export interface AdminUsageLog extends UsageLog {
  // Upstream account identity is returned only to super administrators.
  account_id?: number | null
  authorization_mode?: UsageAuthorizationMode | null
  upstream_model?: string | null
  upstream_reasoning_effort?: string | null
  upstream_response_model?: string | null
  upstream_model_mismatch?: boolean | null
  model_mapping_chain?: string | null
  upstream_request_id?: string | null

  // 账号计费倍率（仅管理员可见）
  account_rate_multiplier?: number | null
  // 自定义定价规则计算的账号统计费用（nil 时使用 total_cost * multiplier）
  account_stats_cost?: number | null

  // 渠道 ID 和计费等级（仅管理员可见）
  channel_id?: number | null
  billing_tier?: string | null

  // 最小账号信息（仅超级管理员可见）
  account?: UsageLogAccountSummary
}

export interface UsageCleanupFilters {
  start_time: string
  end_time: string
  user_id?: number
  api_key_id?: number
  account_id?: number
  group_id?: number
  model?: string | null
  request_type?: UsageRequestType | null
  stream?: boolean | null
  billing_type?: number | null
}

export interface UsageCleanupTask {
  id: number
  status: string
  filters: UsageCleanupFilters
  created_by: number
  deleted_rows: number
  error_message?: string | null
  canceled_by?: number | null
  canceled_at?: string | null
  started_at?: string | null
  finished_at?: string | null
  created_at: string
  updated_at: string
}

export interface UsageStatsResponse {
  period?: string
  total_requests: number
  total_input_tokens: number
  total_output_tokens: number
  total_cache_tokens: number
  total_cache_read_tokens: number
  total_cache_creation_tokens: number
  total_tokens: number
  total_cost: number // 标准计费
  total_actual_cost: number // 实际扣除
  average_duration_ms: number
  models?: Record<string, number>
  endpoints?: EndpointStat[]
  upstream_endpoints?: EndpointStat[]
  endpoint_paths?: EndpointStat[]
}

// ==================== Trend & Chart Types ====================

export interface TrendDataPoint {
  date: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  cost: number // 标准计费
  actual_cost: number // 实际扣除
}

export interface ModelStat {
  model: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  cost: number // 标准计费
  actual_cost: number // 实际扣除
  account_cost?: number // 账号成本（仅管理员接口返回）
}

export interface EndpointStat {
  endpoint: string
  requests: number
  total_tokens: number
  cost: number
  actual_cost: number
}

export interface GroupStat {
  group_id: number
  group_name: string
  requests: number
  total_tokens: number
  cost: number // 标准计费
  actual_cost: number // 实际扣除
  account_cost?: number // 账号成本（仅管理员接口返回）
}

export interface UserBreakdownItem {
  user_id: number
  email: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
  account_cost: number
}

/** 与 UserBreakdownItem 同构，聚合维度换成 api_key_id。 */
export interface APIKeyBreakdownItem {
  api_key_id: number
  /** Key 已被物理删除时为空 */
  key_name: string
  /** Key 已软删除或已不存在；历史用量仍会统计进来，只是标记出来 */
  key_deleted: boolean
  user_id: number
  email: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
  account_cost: number
}

export interface UserUsageTrendPoint {
  date: string
  user_id: number
  email: string
  username: string
  requests: number
  tokens: number
  cost: number // 标准计费
  actual_cost: number // 实际扣除
}

export interface UserSpendingRankingItem {
  user_id: number
  email: string
  username: string
  actual_cost: number
  requests: number
  tokens: number
}

export interface UserSpendingRankingResponse {
  ranking: UserSpendingRankingItem[]
  total_actual_cost: number
  total_requests: number
  total_tokens: number
  start_date: string
  end_date: string
}

export interface ApiKeyUsageTrendPoint {
  date: string
  api_key_id: number
  key_name: string
  requests: number
  tokens: number
}

// ==================== Query Parameters ====================

export interface UserErrorRequest {
  id: number
  created_at: string
  model: string
  inbound_endpoint: string
  status_code: number
  category: string
  platform: string
  message: string
  key_name: string
  key_deleted: boolean
  client_ip?: string
  group_name?: string
  request_type?: number
  stream?: boolean
  user_agent?: string
}

export interface UserErrorRequestDetail extends UserErrorRequest {
  error_body: string
  upstream_status_code?: number
}

export interface UserErrorListParams {
  page?: number
  page_size?: number
  start_date?: string
  end_date?: string
  timezone?: string
  model?: string
  status_code?: number
  category?: string
  api_key_id?: number
  // 服务端排序,列白名单见后端 opsErrorLogsOrderBy(created_at/model/status_code)
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export interface UsageQueryParams {
  cursor?: string
  exact_total?: boolean
  page?: number
  page_size?: number
  api_key_id?: number
  user_id?: number
  account_id?: number
  group_id?: number
  model?: string
  request_type?: UsageRequestType
  stream?: boolean
  native_compaction_v2?: boolean | null
  billing_type?: number | null
  billing_mode?: string | null
  start_date?: string
  end_date?: string
  timezone?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}
