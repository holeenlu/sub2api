import type { SchedulingQuotaProtection } from '@/features/operations/model/schedulingQuota'
/**
 * Admin Ops API endpoints (vNext)
 * - Error logs list/detail
 * - Dashboard overview (raw path)
 */

import { apiClient } from '@/shared/http'
import type { PaginatedResponse } from '@/shared/http/requestTypes';

export type OpsQueryMode = 'auto' | 'raw' | 'preagg'

export interface OpsRequestOptions {
  signal?: AbortSignal
}

export type OpsUpstreamErrorEvent = {
  at_unix_ms?: number
  platform?: string
  account_id?: number
  account_name?: string
  upstream_status_code?: number
  upstream_request_id?: string
  kind?: string
  message?: string
  detail?: string
}

export interface OpsDashboardOverview {
  start_time: string
  end_time: string
  platform: string
  group_id?: number | null

  health_score?: number

  system_metrics?: OpsSystemMetricsSnapshot | null
  system_metrics_nodes?: OpsNodeMetricsSnapshot[] | null
  system_metrics_unavailable?: boolean
  job_heartbeats?: OpsJobHeartbeat[] | null

  success_count: number
  error_count_total: number
  business_limited_count: number
  error_count_sla: number
  request_count_total: number
  request_count_sla: number

  token_consumed: number

  sla: number
  error_rate: number
  upstream_error_rate: number
  upstream_error_count_excl_429_529: number
  upstream_429_count: number
  upstream_529_count: number

  qps: {
    current: number
    peak: number
    avg: number
  }
  tps: {
    current: number
    peak: number
    avg: number
  }

  output_tps?: {
    p5: number | null
    p10: number | null
    p50: number | null
    avg: number | null
    sample_count: number
  } | null

  duration: OpsPercentiles
  ttft: OpsPercentiles
}

export interface OpsPercentiles {
  p50_ms?: number | null
  p90_ms?: number | null
  p95_ms?: number | null
  p99_ms?: number | null
  avg_ms?: number | null
  max_ms?: number | null
}

export interface OpsThroughputTrendPoint {
  bucket_start: string
  request_count: number
  token_consumed: number
  switch_count?: number
  qps: number
  tps: number
}

export interface OpsThroughputPlatformBreakdownItem {
  platform: string
  request_count: number
  token_consumed: number
}

export interface OpsThroughputGroupBreakdownItem {
  group_id: number
  group_name: string
  request_count: number
  token_consumed: number
}

export interface OpsThroughputTrendResponse {
  bucket: string
  points: OpsThroughputTrendPoint[]
  by_platform?: OpsThroughputPlatformBreakdownItem[]
  top_groups?: OpsThroughputGroupBreakdownItem[]
}

export type OpsRequestKind = 'success' | 'error'
export type OpsRequestDetailsKind = OpsRequestKind | 'all'
export type OpsRequestDetailsSort = 'created_at_desc' | 'duration_desc' | 'ttft_desc'

export interface OpsRequestDetail {
  kind: OpsRequestKind
  created_at: string
  request_id: string

  platform?: string
  model?: string
  duration_ms?: number | null
  first_token_ms?: number | null
  status_code?: number | null

  error_id?: number | null
  phase?: string
  severity?: string
  message?: string

  user_id?: number | null
  api_key_id?: number | null
  account_id?: number | null
  group_id?: number | null

  stream?: boolean
}

export interface OpsRequestDetailsParams {
  time_range?: '5m' | '30m' | '1h' | '6h' | '24h'
  start_time?: string
  end_time?: string

  kind?: OpsRequestDetailsKind

  platform?: string
  group_id?: number | null

  user_id?: number
  api_key_id?: number
  account_id?: number

  model?: string
  request_id?: string
  q?: string

  min_duration_ms?: number
  max_duration_ms?: number

  sort?: OpsRequestDetailsSort

  page?: number
  page_size?: number
}

export type OpsRequestDetailsResponse = PaginatedResponse<OpsRequestDetail>

export interface OpsLatencyHistogramBucket {
  range: string
  count: number
}

export interface OpsLatencyHistogramResponse {
  start_time: string
  end_time: string
  platform: string
  group_id?: number | null

  total_requests: number
  buckets: OpsLatencyHistogramBucket[]
}

export interface OpsErrorTrendPoint {
  bucket_start: string
  error_count_total: number
  business_limited_count: number
  error_count_sla: number
  upstream_error_count_excl_429_529: number
  upstream_429_count: number
  upstream_529_count: number
}

export interface OpsErrorTrendResponse {
  bucket: string
  points: OpsErrorTrendPoint[]
}

export interface OpsErrorDistributionItem {
  status_code: number
  total: number
  sla: number
  business_limited: number
}

export interface OpsErrorDistributionResponse {
  total: number
  items: OpsErrorDistributionItem[]
}

export interface OpsDashboardSnapshotV2Response {
  generated_at: string
  overview: OpsDashboardOverview
  throughput_trend: OpsThroughputTrendResponse
  error_trend: OpsErrorTrendResponse
}

export type OpsOpenAITokenStatsTimeRange = '30m' | '1h' | '1d' | '15d' | '30d'

export interface OpsOpenAITokenStatsItem {
  model: string
  request_count: number
  avg_tokens_per_sec?: number | null
  avg_first_token_ms?: number | null
  total_output_tokens: number
  avg_duration_ms: number
  requests_with_first_token: number
}

export interface OpsOpenAITokenStatsResponse {
  time_range: OpsOpenAITokenStatsTimeRange
  start_time: string
  end_time: string
  platform?: string
  group_id?: number | null
  items: OpsOpenAITokenStatsItem[]
  total: number
  page?: number
  page_size?: number
  top_n?: number | null
}

export interface OpsOpenAITokenStatsParams {
  time_range?: OpsOpenAITokenStatsTimeRange
  platform?: string
  group_id?: number | null
  page?: number
  page_size?: number
  top_n?: number
}

export interface OpsNodeMetricsSnapshot extends OpsSystemMetricsSnapshot {
  node_id: string
  node_name: string
  cpu_scope: 'host' | 'container' | 'unknown'
  memory_scope: 'host' | 'container' | 'unknown'
  cpu_capacity: number
  last_seen_at: string
  age_seconds: number
  stale: boolean
}

export interface OpsSystemMetricsSnapshot {
  id: number
  created_at: string
  window_minutes: number

  cpu_usage_percent?: number | null
  memory_used_mb?: number | null
  memory_total_mb?: number | null
  memory_usage_percent?: number | null

  db_ok?: boolean | null
  redis_ok?: boolean | null

  // Config-derived limits (best-effort) for rendering "current vs max".
  db_max_open_conns?: number | null
  redis_pool_size?: number | null

  redis_conn_total?: number | null
  redis_conn_idle?: number | null

  db_conn_active?: number | null
  db_conn_idle?: number | null
  db_conn_waiting?: number | null

  goroutine_count?: number | null
  concurrency_queue_depth?: number | null
  account_switch_count?: number | null
}

export interface OpsJobHeartbeat {
  job_name: string
  expected_interval_seconds?: number
  health_status?: 'healthy' | 'failed' | 'stale' | 'unknown'
  last_run_at?: string | null
  last_success_at?: string | null
  last_error_at?: string | null
  last_error?: string | null
  last_duration_ms?: number | null
  last_result?: string | null
  updated_at: string
}

export interface PlatformConcurrencyInfo {
  platform: string
  current_in_use: number
  max_capacity: number
  load_percentage: number
  waiting_in_queue: number
}

export interface GroupConcurrencyInfo {
  group_id: number
  group_name: string
  platform: string
  current_in_use: number
  max_capacity: number
  load_percentage: number
  waiting_in_queue: number
}

export interface AccountConcurrencyInfo {
  account_id: number
  account_name?: string
  platform: string
  group_id: number
  group_name: string
  current_in_use: number
  max_capacity: number
  load_percentage: number
  waiting_in_queue: number
}

export interface OpsConcurrencyStatsResponse {
  enabled: boolean
  platform: Record<string, PlatformConcurrencyInfo>
  group: Record<string, GroupConcurrencyInfo>
  account: Record<string, AccountConcurrencyInfo>
  timestamp?: string
}

export interface UserConcurrencyInfo {
  user_id: number
  user_email: string
  username: string
  current_in_use: number
  max_capacity: number
  load_percentage: number
  waiting_in_queue: number
}

export interface OpsUserConcurrencyStatsResponse {
  enabled: boolean
  user: Record<string, UserConcurrencyInfo>
  timestamp?: string
}

export async function getConcurrencyStats(platform?: string, groupId?: number | null, options: { signal?: AbortSignal } = {}): Promise<OpsConcurrencyStatsResponse> {
  const params: { platform?: string; group_id?: number } = {}
  if (platform) {
    params.platform = platform
  }
  if (typeof groupId === 'number' && groupId > 0) {
    params.group_id = groupId
  }

  const { data } = await apiClient.get<OpsConcurrencyStatsResponse>('/admin/ops/concurrency', { signal: options.signal, params })
  return data
}

export async function getUserConcurrencyStats(options: { signal?: AbortSignal } = {}): Promise<OpsUserConcurrencyStatsResponse> {
  const { data } = await apiClient.get<OpsUserConcurrencyStatsResponse>('/admin/ops/user-concurrency', { signal: options.signal })
  return data
}

export interface PlatformAvailability {
  platform: string
  total_accounts: number
  available_count: number
  rate_limit_count: number
  error_count: number
}

export interface GroupAvailability {
  group_id: number
  group_name: string
  platform: string
  total_accounts: number
  available_count: number
  rate_limit_count: number
  error_count: number
}

export interface AccountAvailability {
  account_id: number
  account_name: string
  platform: string
  group_id: number
  group_name: string
  status: string
  is_available: boolean
  is_rate_limited: boolean
  rate_limit_reset_at?: string
  rate_limit_remaining_sec?: number
  is_overloaded: boolean
  overload_until?: string
  overload_remaining_sec?: number
  has_error: boolean
  error_message?: string
}

export interface OpsAccountAvailabilityStatsResponse {
  enabled: boolean
  platform: Record<string, PlatformAvailability>
  group: Record<string, GroupAvailability>
  account: Record<string, AccountAvailability>
  timestamp?: string
}

export async function getAccountAvailabilityStats(platform?: string, groupId?: number | null, options: { signal?: AbortSignal } = {}): Promise<OpsAccountAvailabilityStatsResponse> {
  const params: { platform?: string; group_id?: number } = {}
  if (platform) {
    params.platform = platform
  }
  if (typeof groupId === 'number' && groupId > 0) {
    params.group_id = groupId
  }
  const { data } = await apiClient.get<OpsAccountAvailabilityStatsResponse>('/admin/ops/account-availability', { signal: options.signal, params })
  return data
}

export interface OpsRateSummary {
  current: number
  peak: number
  avg: number
}

export interface OpsRealtimeTrafficSummary {
  window: string
  start_time: string
  end_time: string
  platform: string
  group_id?: number | null
  qps: OpsRateSummary
  tps: OpsRateSummary
}

export interface OpsRealtimeTrafficSummaryResponse {
  enabled: boolean
  summary: OpsRealtimeTrafficSummary | null
  timestamp?: string
}

export async function getRealtimeTrafficSummary(
  window: string,
  platform?: string,
  groupId?: number | null, options: { signal?: AbortSignal } = {}
): Promise<OpsRealtimeTrafficSummaryResponse> {
  const params: { window: string; platform?: string; group_id?: number } = { window }
  if (platform) {
    params.platform = platform
  }
  if (typeof groupId === 'number' && groupId > 0) {
    params.group_id = groupId
  }

  const { data } = await apiClient.get<OpsRealtimeTrafficSummaryResponse>('/admin/ops/realtime-traffic', { signal: options.signal, params })
  return data
}

export type OpsSeverity = string
export type OpsPhase = string

export type AlertSeverity = 'critical' | 'warning' | 'info'
export type ThresholdMode = 'count' | 'percentage' | 'both'
export type MetricType =
  | 'success_rate'
  | 'error_rate'
  | 'upstream_error_rate'
  | 'cpu_usage_percent'
  | 'memory_usage_percent'
  | 'concurrency_queue_depth'
  | 'group_available_accounts'
  | 'group_available_ratio'
  | 'group_rate_limit_ratio'
  | 'account_rate_limited_count'
  | 'account_error_count'
  | 'account_error_ratio'
  | 'account_temp_unscheduled_count'
  | 'overload_account_count'
export type Operator = '>' | '>=' | '<' | '<=' | '==' | '!='

export interface AlertRule {
  id?: number
  name: string
  description?: string
  enabled: boolean
  metric_type: MetricType
  operator: Operator
  threshold: number
  window_minutes: number
  sustained_minutes: number
  severity: OpsSeverity
  cooldown_minutes: number
  notify_email: boolean
  filters?: Record<string, unknown>
  created_at?: string
  updated_at?: string
  last_triggered_at?: string | null
}

export interface AlertEvent {
  id: number
  rule_id: number
  severity: OpsSeverity | string
  status: 'firing' | 'resolved' | 'manual_resolved' | string
  title?: string
  description?: string
  metric_value?: number
  threshold_value?: number
  dimensions?: Record<string, unknown>
  fired_at: string
  resolved_at?: string | null
  email_sent: boolean
  created_at: string
}

export interface EmailNotificationConfig {
  alert: {
    enabled: boolean
    recipients: string[]
    min_severity: AlertSeverity | ''
    rate_limit_per_hour: number
    batching_window_seconds: number
    include_resolved_alerts: boolean
  }
  report: {
    enabled: boolean
    recipients: string[]
    daily_summary_enabled: boolean
    daily_summary_schedule: string
    weekly_summary_enabled: boolean
    weekly_summary_schedule: string
    error_digest_enabled: boolean
    error_digest_schedule: string
    error_digest_min_count: number
    account_health_enabled: boolean
    account_health_schedule: string
    account_health_error_rate_threshold: number
  }
}

export interface OpsMetricThresholds {
  sla_percent_min?: number | null                 // SLA低于此值变红
  ttft_p99_ms_max?: number | null                 // TTFT P99高于此值变红
  request_error_rate_percent_max?: number | null  // 请求错误率高于此值变红
  upstream_error_rate_percent_max?: number | null // 上游错误率高于此值变红
}

export interface OpsDistributedLockSettings {
  enabled: boolean
  key: string
  ttl_seconds: number
}

export interface OpsAlertRuntimeSettings {
  evaluation_interval_seconds: number
  distributed_lock: OpsDistributedLockSettings
  silencing: {
    enabled: boolean
    global_until_rfc3339: string
    global_reason: string
    entries?: Array<{
      rule_id?: number
      severities?: Array<OpsSeverity | string>
      until_rfc3339: string
      reason: string
    }>
  }
  thresholds: OpsMetricThresholds // 指标阈值配置
}

export interface OpsOpenAIAccountQuotaAutoPauseSettings {
  default_threshold_5h: number // 0~1，0 表示不启用全局默认 5h 阈值
  default_threshold_7d: number // 0~1，0 表示不启用全局默认 7d 阈值
}

export interface OpsAdvancedSettings {
  quota_protection: SchedulingQuotaProtection
  account_telemetry_sending_enabled: boolean
  account_telemetry_upload_interval_seconds: number
  data_retention: OpsDataRetentionSettings
  aggregation: OpsAggregationSettings
  openai_account_quota_auto_pause: OpsOpenAIAccountQuotaAutoPauseSettings
  ignore_count_tokens_errors: boolean
  ignore_context_canceled: boolean
  ignore_no_available_accounts: boolean
  ignore_invalid_api_key_errors: boolean
  ignore_insufficient_balance_errors: boolean
  display_openai_token_stats: boolean
  display_alert_events: boolean
  auto_refresh_enabled: boolean
  auto_refresh_interval_seconds: number
}

export interface OpsDataRetentionSettings {
  cleanup_enabled: boolean
  cleanup_schedule: string
  error_log_retention_days: number
  minute_metrics_retention_days: number
  hourly_metrics_retention_days: number
}

export interface OpsAggregationSettings {
  aggregation_enabled: boolean
}

export interface OpsRuntimeLogConfig {
  level: 'debug' | 'info' | 'warn' | 'error'
  persist_access_logs: boolean
  enable_sampling: boolean
  sampling_initial: number
  sampling_thereafter: number
  caller: boolean
  stacktrace_level: 'none' | 'error' | 'fatal'
  retention_days: number
  request_retention_days: number
  source?: string
  updated_at?: string
  updated_by_user_id?: number
}

export interface OpsSystemLog {
  id: number
  created_at: string
  host: string
  level: string
  component: string
  message: string
  request_id?: string
  client_request_id?: string
  user_id?: number | null
  api_key_id?: number | null
  account_id?: number | null
  platform?: string
  model?: string
  extra?: Record<string, unknown>
}

export type OpsSystemLogListResponse = PaginatedResponse<OpsSystemLog>

export interface OpsSystemLogQuery {
  page?: number
  page_size?: number
  time_range?: '5m' | '30m' | '1h' | '6h' | '24h' | '7d' | '30d'
  start_time?: string
  end_time?: string
  host?: string
  level?: string
  component?: string
  request_id?: string
  client_request_id?: string
  user_id?: number | null
  api_key_id?: number | null
  account_id?: number | null
  platform?: string
  model?: string
  q?: string
}

export interface OpsSystemLogCleanupRequest {
  start_time?: string
  end_time?: string
  host?: string
  level?: string
  component?: string
  request_id?: string
  client_request_id?: string
  user_id?: number | null
  api_key_id?: number | null
  account_id?: number | null
  platform?: string
  model?: string
  q?: string
}

export interface OpsSystemLogSinkHealth {
  queue_depth: number
  queue_capacity: number
  dropped_count: number
  write_failed_count: number
  written_count: number
  avg_write_delay_ms: number
  last_error?: string
}

export interface OpsErrorLog {
  id: number
  created_at: string

  // Standardized classification
  phase: OpsPhase
  type: string
  error_owner: 'client' | 'provider' | 'platform' | string
  error_source: 'client_request' | 'upstream_http' | 'gateway' | string

  severity: OpsSeverity
  status_code: number
  request_status_code?: number | null
  platform: string
  model: string

  resolved: boolean
  resolved_at?: string | null
  resolved_by_user_id?: number | null

  client_request_id: string
  request_id: string
  message: string

  user_id?: number | null
  user_email: string
  api_key_id?: number | null
  // 关联 api_key 名称（后端 LEFT JOIN api_keys；软删保留 name，故已删 key 仍有原名）。
  api_key_name?: string
  api_key_deleted?: boolean
  account_id?: number | null
  account_name: string
  group_id?: number | null
  group_name: string

  client_ip?: string | null
  request_path?: string
  stream?: boolean

  // Error observability context (endpoint + model mapping)
  inbound_endpoint?: string
  upstream_endpoint?: string
  requested_model?: string
  upstream_model?: string
  request_type?: number | null
  user_agent?: string

}

export interface OpsErrorDetail extends OpsErrorLog {
  error_body: string

  // Upstream context (optional; enriched by gateway services)
  upstream_status_code?: number | null
  upstream_error_message?: string
  upstream_error_detail?: string
  upstream_errors?: string

  auth_latency_ms?: number | null
  routing_latency_ms?: number | null
  upstream_latency_ms?: number | null
  response_latency_ms?: number | null
  time_to_first_token_ms?: number | null

  is_business_limited: boolean

  // Bound (non-deleted) key prefix, snapshotted at error time
  api_key_prefix?: string | null
}

export interface OpsErrorDiagnostics extends OpsErrorDetail {
  // Correlated records are separate from the unchanged raw upstream_errors string.
  correlated_upstream_errors: OpsErrorDetail[]
}

export type OpsErrorLogsResponse = PaginatedResponse<OpsErrorLog>

export async function getDashboardOverview(
  params: {
    time_range?: '5m' | '30m' | '1h' | '6h' | '24h'
    start_time?: string
    end_time?: string
    platform?: string
    group_id?: number | null
    mode?: OpsQueryMode
  },
  options: OpsRequestOptions = {}
): Promise<OpsDashboardOverview> {
  const { data } = await apiClient.get<OpsDashboardOverview>('/admin/ops/dashboard/overview', {
    params,
    signal: options.signal
  })
  return data
}

export async function getDashboardSnapshotV2(
  params: {
    time_range?: '5m' | '30m' | '1h' | '6h' | '24h'
    start_time?: string
    end_time?: string
    platform?: string
    group_id?: number | null
    mode?: OpsQueryMode
  },
  options: OpsRequestOptions = {}
): Promise<OpsDashboardSnapshotV2Response> {
  const { data } = await apiClient.get<OpsDashboardSnapshotV2Response>('/admin/ops/dashboard/snapshot-v2', {
    params,
    signal: options.signal
  })
  return data
}

export async function getThroughputTrend(
  params: {
    time_range?: '5m' | '30m' | '1h' | '6h' | '24h'
    start_time?: string
    end_time?: string
    platform?: string
    group_id?: number | null
    mode?: OpsQueryMode
  },
  options: OpsRequestOptions = {}
): Promise<OpsThroughputTrendResponse> {
  const { data } = await apiClient.get<OpsThroughputTrendResponse>('/admin/ops/dashboard/throughput-trend', {
    params,
    signal: options.signal
  })
  return data
}

export async function getLatencyHistogram(
  params: {
    time_range?: '5m' | '30m' | '1h' | '6h' | '24h'
    start_time?: string
    end_time?: string
    platform?: string
    group_id?: number | null
    mode?: OpsQueryMode
  },
  options: OpsRequestOptions = {}
): Promise<OpsLatencyHistogramResponse> {
  const { data } = await apiClient.get<OpsLatencyHistogramResponse>('/admin/ops/dashboard/latency-histogram', {
    params,
    signal: options.signal
  })
  return data
}

export async function getErrorTrend(
  params: {
    time_range?: '5m' | '30m' | '1h' | '6h' | '24h'
    start_time?: string
    end_time?: string
    platform?: string
    group_id?: number | null
    mode?: OpsQueryMode
  },
  options: OpsRequestOptions = {}
): Promise<OpsErrorTrendResponse> {
  const { data } = await apiClient.get<OpsErrorTrendResponse>('/admin/ops/dashboard/error-trend', {
    params,
    signal: options.signal
  })
  return data
}

export async function getErrorDistribution(
  params: {
    time_range?: '5m' | '30m' | '1h' | '6h' | '24h'
    start_time?: string
    end_time?: string
    platform?: string
    group_id?: number | null
    mode?: OpsQueryMode
  },
  options: OpsRequestOptions = {}
): Promise<OpsErrorDistributionResponse> {
  const { data } = await apiClient.get<OpsErrorDistributionResponse>('/admin/ops/dashboard/error-distribution', {
    params,
    signal: options.signal
  })
  return data
}

export async function getOpenAITokenStats(
  params: OpsOpenAITokenStatsParams,
  options: OpsRequestOptions = {}
): Promise<OpsOpenAITokenStatsResponse> {
  const { data } = await apiClient.get<OpsOpenAITokenStatsResponse>('/admin/ops/dashboard/openai-token-stats', {
    params,
    signal: options.signal
  })
  return data
}

export type OpsErrorListView = 'errors' | 'excluded' | 'all'

export type OpsErrorListQueryParams = {
  page?: number
  page_size?: number
  time_range?: string
  start_time?: string
  end_time?: string
  platform?: string
  group_id?: number | null
  account_id?: number | null
  user_id?: number
  api_key_id?: number
  // 模型过滤：后端以 COALESCE(requested_model, model) 精确匹配（admin 路径）。
  model?: string

  phase?: string
  // 分类(用户侧粗分类码,如 auth/rate_limit/upstream),后端反查为 phase/type ANY 条件
  category?: string
  error_owner?: string
  error_source?: string
  resolved?: string
  view?: OpsErrorListView

  q?: string
  status_codes?: string
  status_codes_other?: string

  // 服务端排序,列白名单见后端 opsErrorLogsOrderBy(created_at/model/status_code)
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

// Request error history remains the published usage/operations contract.
export async function listErrorLogs(params: OpsErrorListQueryParams, options: { signal?: AbortSignal } = {}): Promise<OpsErrorLogsResponse> {
  const { data } = await apiClient.get<OpsErrorLogsResponse>('/admin/ops/errors', { signal: options.signal, params })
  return data
}

export async function getErrorLogDetail(id: number, options: { signal?: AbortSignal } = {}): Promise<OpsErrorDetail> {
  const { data } = await apiClient.get<OpsErrorDetail>(`/admin/ops/errors/${id}`, { signal: options.signal })
  return data
}

export async function getErrorDiagnostics(id: number, errorType: 'request' | 'upstream' = 'request', options: OpsRequestOptions = {}): Promise<OpsErrorDiagnostics> {
  options.signal?.throwIfAborted()
  const detail = await (errorType === 'upstream' ? getUpstreamErrorDetail(id, options) : getRequestErrorDetail(id, options))
  options.signal?.throwIfAborted()
  const correlatedUpstreamErrors: OpsErrorDetail[] = []
  if (errorType === 'request') {
    let page = 1
    let pages = 1
    let total: number | undefined
    const attemptIds = new Set<number>()
    do {
      options.signal?.throwIfAborted()
      const response = await listRequestErrorUpstreamErrors(id, { page, page_size: 100, view: 'all' }, { include_detail: true, signal: options.signal })
      options.signal?.throwIfAborted()
      if (total !== undefined && response.total !== total) {
        throw new Error('Upstream error count changed during export')
      }
      total = response.total
      for (const attempt of response.items) {
        if (attemptIds.has(attempt.id)) throw new Error('Duplicate upstream error during export')
        attemptIds.add(attempt.id)
      }
      correlatedUpstreamErrors.push(...response.items)
      pages = response.pages
      page += 1
    } while (page <= pages)
    // The detail endpoint can skip records whose individual detail lookup failed.
    if (correlatedUpstreamErrors.length !== total) throw new Error('Incomplete upstream error details')
  }
  return { ...detail, correlated_upstream_errors: correlatedUpstreamErrors }
}

export async function updateErrorResolved(errorId: number, resolved: boolean, options: { signal?: AbortSignal } = {}): Promise<void> {
  await apiClient.put(`/admin/ops/errors/${errorId}/resolve`, { resolved }, { signal: options.signal })
}

// New split endpoints
export async function listRequestErrors(params: OpsErrorListQueryParams, options: { signal?: AbortSignal } = {}): Promise<OpsErrorLogsResponse> {
  const { data } = await apiClient.get<OpsErrorLogsResponse>('/admin/ops/request-errors', { signal: options.signal, params })
  return data
}

export async function listUpstreamErrors(params: OpsErrorListQueryParams, options: { signal?: AbortSignal } = {}): Promise<OpsErrorLogsResponse> {
  const { data } = await apiClient.get<OpsErrorLogsResponse>('/admin/ops/upstream-errors', { signal: options.signal, params })
  return data
}

export async function getRequestErrorDetail(id: number, options: { signal?: AbortSignal } = {}): Promise<OpsErrorDetail> {
  const { data } = await apiClient.get<OpsErrorDetail>(`/admin/ops/request-errors/${id}`, { signal: options.signal })
  return data
}

export async function getUpstreamErrorDetail(id: number, options: { signal?: AbortSignal } = {}): Promise<OpsErrorDetail> {
  const { data } = await apiClient.get<OpsErrorDetail>(`/admin/ops/upstream-errors/${id}`, { signal: options.signal })
  return data
}

export async function updateRequestErrorResolved(errorId: number, resolved: boolean, options: { signal?: AbortSignal } = {}): Promise<void> {
  await apiClient.put(`/admin/ops/request-errors/${errorId}/resolve`, { resolved }, { signal: options.signal })
}

export async function updateUpstreamErrorResolved(errorId: number, resolved: boolean, options: { signal?: AbortSignal } = {}): Promise<void> {
  await apiClient.put(`/admin/ops/upstream-errors/${errorId}/resolve`, { resolved }, { signal: options.signal })
}

export async function listRequestErrorUpstreamErrors(
  id: number,
  params: OpsErrorListQueryParams = {},
  options: { include_detail?: boolean; signal?: AbortSignal } = {}
): Promise<PaginatedResponse<OpsErrorDetail>> {
  const query: OpsErrorListQueryParams & { include_detail?: '1' } = { ...params }
  if (options.include_detail) query.include_detail = '1'
  const { data } = await apiClient.get<PaginatedResponse<OpsErrorDetail>>(`/admin/ops/request-errors/${id}/upstream-errors`, { signal: options.signal, params: query })
  return data
}

export async function listRequestDetails(params: OpsRequestDetailsParams, options: { signal?: AbortSignal } = {}): Promise<OpsRequestDetailsResponse> {
  const { data } = await apiClient.get<OpsRequestDetailsResponse>('/admin/ops/requests', { signal: options.signal, params })
  return data
}

// Alert rules
export async function listAlertRules(options: { signal?: AbortSignal } = {}): Promise<AlertRule[]> {
  const { data } = await apiClient.get<AlertRule[]>('/admin/ops/alert-rules', { signal: options.signal })
  return data
}

export async function createAlertRule(rule: AlertRule, options: { signal?: AbortSignal } = {}): Promise<AlertRule> {
  const { data } = await apiClient.post<AlertRule>('/admin/ops/alert-rules', rule, { signal: options.signal })
  return data
}

export async function updateAlertRule(id: number, rule: Partial<AlertRule>, options: { signal?: AbortSignal } = {}): Promise<AlertRule> {
  const { data } = await apiClient.put<AlertRule>(`/admin/ops/alert-rules/${id}`, rule, { signal: options.signal })
  return data
}

export async function deleteAlertRule(id: number, options: { signal?: AbortSignal } = {}): Promise<void> {
  await apiClient.delete(`/admin/ops/alert-rules/${id}`, { signal: options.signal })
}

export interface AlertEventsQuery {
  limit?: number
  status?: string
  severity?: string
  email_sent?: boolean
  time_range?: string
  start_time?: string
  end_time?: string
  before_fired_at?: string
  before_id?: number
  platform?: string
  group_id?: number
}

export async function listAlertEvents(params: AlertEventsQuery = {}, options: { signal?: AbortSignal } = {}): Promise<AlertEvent[]> {
  const { data } = await apiClient.get<AlertEvent[]>('/admin/ops/alert-events', { signal: options.signal, params })
  return data
}

export async function getAlertEvent(id: number, options: { signal?: AbortSignal } = {}): Promise<AlertEvent> {
  const { data } = await apiClient.get<AlertEvent>(`/admin/ops/alert-events/${id}`, { signal: options.signal })
  return data
}

export async function updateAlertEventStatus(id: number, status: 'resolved' | 'manual_resolved', options: { signal?: AbortSignal } = {}): Promise<void> {
  await apiClient.put(`/admin/ops/alert-events/${id}/status`, { status }, { signal: options.signal })
}

export async function createAlertSilence(payload: {
  rule_id: number
  platform: string
  group_id?: number | null
  region?: string | null
  until: string
  reason?: string
}, options: { signal?: AbortSignal } = {}): Promise<void> {
  await apiClient.post('/admin/ops/alert-silences', payload, { signal: options.signal })
}

// Email notification config
export async function getEmailNotificationConfig(options: { signal?: AbortSignal } = {}): Promise<EmailNotificationConfig> {
  const { data } = await apiClient.get<EmailNotificationConfig>('/admin/ops/email-notification/config', { signal: options.signal })
  return data
}

export async function updateEmailNotificationConfig(config: EmailNotificationConfig, options: { signal?: AbortSignal } = {}): Promise<EmailNotificationConfig> {
  const { data } = await apiClient.put<EmailNotificationConfig>('/admin/ops/email-notification/config', config, { signal: options.signal })
  return data
}

// Runtime settings (DB-backed)
export async function getAlertRuntimeSettings(options: { signal?: AbortSignal } = {}): Promise<OpsAlertRuntimeSettings> {
  const { data } = await apiClient.get<OpsAlertRuntimeSettings>('/admin/ops/runtime/alert', { signal: options.signal })
  return data
}

export async function updateAlertRuntimeSettings(config: OpsAlertRuntimeSettings, options: { signal?: AbortSignal } = {}): Promise<OpsAlertRuntimeSettings> {
  const { data } = await apiClient.put<OpsAlertRuntimeSettings>('/admin/ops/runtime/alert', config, { signal: options.signal })
  return data
}

export async function getRuntimeLogConfig(options: { signal?: AbortSignal } = {}): Promise<OpsRuntimeLogConfig> {
  const { data } = await apiClient.get<OpsRuntimeLogConfig>('/admin/ops/runtime/logging', { signal: options.signal })
  return data
}

export async function updateRuntimeLogConfig(config: OpsRuntimeLogConfig, options: { signal?: AbortSignal } = {}): Promise<OpsRuntimeLogConfig> {
  const { data } = await apiClient.put<OpsRuntimeLogConfig>('/admin/ops/runtime/logging', config, { signal: options.signal })
  return data
}

export async function resetRuntimeLogConfig(options: { signal?: AbortSignal } = {}): Promise<OpsRuntimeLogConfig> {
  const { data } = await apiClient.post<OpsRuntimeLogConfig>('/admin/ops/runtime/logging/reset', undefined, { signal: options.signal })
  return data
}

export async function listSystemLogs(params: OpsSystemLogQuery, options: { signal?: AbortSignal } = {}): Promise<OpsSystemLogListResponse> {
  const { data } = await apiClient.get<OpsSystemLogListResponse>('/admin/ops/system-logs', { signal: options.signal, params })
  return data
}

export async function cleanupSystemLogs(payload: OpsSystemLogCleanupRequest, options: { signal?: AbortSignal } = {}): Promise<{ deleted: number }> {
  const { data } = await apiClient.post<{ deleted: number }>('/admin/ops/system-logs/cleanup', payload, { signal: options.signal })
  return data
}

export async function getSystemLogSinkHealth(options: { signal?: AbortSignal } = {}): Promise<OpsSystemLogSinkHealth> {
  const { data } = await apiClient.get<OpsSystemLogSinkHealth>('/admin/ops/system-logs/health', { signal: options.signal })
  return data
}

// Advanced settings (DB-backed)
export async function getAdvancedSettings(options: { signal?: AbortSignal } = {}): Promise<OpsAdvancedSettings> {
  const { data } = await apiClient.get<OpsAdvancedSettings>('/admin/ops/advanced-settings', { signal: options.signal })
  return data
}

export async function updateAdvancedSettings(config: OpsAdvancedSettings, options: { signal?: AbortSignal } = {}): Promise<OpsAdvancedSettings> {
  const { data } = await apiClient.put<OpsAdvancedSettings>('/admin/ops/advanced-settings', config, { signal: options.signal })
  return data
}

// ==================== Metric Thresholds ====================

async function getMetricThresholds(options: { signal?: AbortSignal } = {}): Promise<OpsMetricThresholds> {
  const { data } = await apiClient.get<OpsMetricThresholds>('/admin/ops/settings/metric-thresholds', { signal: options.signal })
  return data
}

async function updateMetricThresholds(thresholds: OpsMetricThresholds, options: { signal?: AbortSignal } = {}): Promise<void> {
  await apiClient.put('/admin/ops/settings/metric-thresholds', thresholds, { signal: options.signal })
}

export const opsAPI = {
  getDashboardSnapshotV2,
  getDashboardOverview,
  getThroughputTrend,
  getLatencyHistogram,
  getErrorTrend,
  getErrorDistribution,
  getOpenAITokenStats,
  getConcurrencyStats,
  getUserConcurrencyStats,
  getAccountAvailabilityStats,
  getRealtimeTrafficSummary,

  // Request error history remains the published usage/operations contract.
  listErrorLogs,
  getErrorLogDetail,
  getErrorDiagnostics,
  updateErrorResolved,

  // New split endpoints
  listRequestErrors,
  listUpstreamErrors,
  getRequestErrorDetail,
  getUpstreamErrorDetail,
  updateRequestErrorResolved,
  updateUpstreamErrorResolved,
  listRequestErrorUpstreamErrors,

  listRequestDetails,
  listAlertRules,
  createAlertRule,
  updateAlertRule,
  deleteAlertRule,
  listAlertEvents,
  getAlertEvent,
  updateAlertEventStatus,
  createAlertSilence,
  getEmailNotificationConfig,
  updateEmailNotificationConfig,
  getAlertRuntimeSettings,
  updateAlertRuntimeSettings,
  getRuntimeLogConfig,
  updateRuntimeLogConfig,
  resetRuntimeLogConfig,
  getAdvancedSettings,
  updateAdvancedSettings,
  getMetricThresholds,
  updateMetricThresholds,
  listSystemLogs,
  cleanupSystemLogs,
  getSystemLogSinkHealth
}

export default opsAPI
