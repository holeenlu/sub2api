import type { AccountPlatform, AccountType } from './platformTypes'

export type ProxyProtocol = 'http' | 'https' | 'socks5' | 'socks5h'

export interface Proxy {
  id: number
  name: string
  protocol: ProxyProtocol
  host: string
  port: number
  username: string | null
  has_password?: boolean
  status: 'active' | 'inactive' | 'expired'
  account_count?: number // Number of accounts using this proxy
  latency_ms?: number
  latency_status?: 'success' | 'failed'
  latency_message?: string
  ip_address?: string
  country?: string
  country_code?: string
  region?: string
  city?: string
  quality_status?: 'healthy' | 'warn' | 'challenge' | 'failed'
  quality_score?: number
  quality_grade?: string
  quality_summary?: string
  quality_checked?: number
  expires_at: string | null
  fallback_mode: 'none' | 'proxy' | 'direct'
  backup_proxy_id?: number | null
  expiry_warn_days: number
  created_at: string
  updated_at: string
}

export interface ProxyAccountSummary {
  id: number
  name: string
  platform: AccountPlatform
  type: AccountType
  notes?: string | null
}

export interface ProxyQualityCheckItem {
  target: string
  status: 'pass' | 'warn' | 'fail' | 'challenge'
  http_status?: number
  latency_ms?: number
  message?: string
  cf_ray?: string
}

export interface ProxyQualityCheckResult {
  proxy_id: number
  score: number
  grade: string
  summary: string
  exit_ip?: string
  country?: string
  country_code?: string
  base_latency_ms?: number
  passed_count: number
  warn_count: number
  failed_count: number
  challenge_count: number
  checked_at: number
  items: ProxyQualityCheckItem[]
}

export interface CreateProxyRequest {
  name: string
  protocol: ProxyProtocol
  host: string
  port: number
  username?: string | null
  password?: string | null
  expires_at?: number | null   // unix 秒；null/0 = 永不过期
  fallback_mode?: 'none' | 'proxy' | 'direct'
  backup_proxy_id?: number | null
  expiry_warn_days?: number
}

export interface UpdateProxyRequest {
  name?: string
  protocol?: ProxyProtocol
  host?: string
  port?: number
  username?: string | null
  password?: string | null
  status?: 'active' | 'inactive'
  expires_at?: number | null   // unix 秒；null/0 = 永不过期
  fallback_mode?: 'none' | 'proxy' | 'direct'
  backup_proxy_id?: number | null
  expiry_warn_days?: number
}
