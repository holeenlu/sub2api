export interface AdminDashboardStats {
  // 用户统计
  total_users: number
  today_new_users: number // 今日新增用户数
  active_users: number // 今日有请求的用户数
  hourly_active_users: number // 当前小时活跃用户数（UTC）
  stats_updated_at: string // 统计更新时间（UTC RFC3339）
  stats_stale: boolean // 统计是否过期

  // API Key 统计
  total_api_keys: number
  active_api_keys: number // 状态为 active 的 API Key 数

  // 账户统计
  total_accounts: number
  normal_accounts: number // 正常账户数
  error_accounts: number // 异常账户数
  ratelimit_accounts: number // 限流账户数
  overload_accounts: number // 过载账户数

  // 累计 Token 使用统计
  total_requests: number
  total_input_tokens: number
  total_output_tokens: number
  total_cache_creation_tokens: number
  total_cache_read_tokens: number
  total_tokens: number
  total_cost: number // 累计标准计费
  total_actual_cost: number // 累计实际扣除
  total_account_cost: number // 累计账号成本

  // 今日 Token 使用统计
  today_requests: number
  today_input_tokens: number
  today_output_tokens: number
  today_cache_creation_tokens: number
  today_cache_read_tokens: number
  today_tokens: number
  today_cost: number // 今日标准计费
  today_actual_cost: number // 今日实际扣除
  today_account_cost: number // 今日账号成本

  // 系统运行统计
  average_duration_ms: number // 平均响应时间
  uptime: number // 系统运行时间(秒)

  // 性能指标
  rpm: number // 近5分钟平均每分钟请求数
  tpm: number // 近5分钟平均每分钟Token数
}
