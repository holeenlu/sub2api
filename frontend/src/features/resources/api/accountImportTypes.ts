import type { Account } from './accountTypes'
import type { AccountPlatform, AccountType } from './platformTypes'
import type { ProxyProtocol } from './proxyTypes'

export interface AdminDataPayload {
  type?: string
  version?: number
  exported_at: string
  proxies: AdminDataProxy[]
  accounts: AdminDataAccount[]
  // 导出时被排除的 spark 影子账号数量(影子不持凭据、其调度配置不在备份范围)。
  skipped_shadows?: number
}

export interface AdminDataProxy {
  proxy_key: string
  name: string
  protocol: ProxyProtocol
  host: string
  port: number
  username?: string | null
  password?: string | null
  status: 'active' | 'inactive'
  expires_at?: number | null
  fallback_mode?: 'none' | 'proxy' | 'direct'
  backup_proxy_name?: string
  backup_proxy_key?: string
  expiry_warn_days?: number
}

export interface AdminDataAccountGroup {
  name: string
  platform: string
  priority: number
}

export interface AdminDataAccount {
  name: string
  notes?: string | null
  platform: AccountPlatform
  type: AccountType
  credentials: Record<string, unknown>
  bps_credentials?: Record<string, unknown>
  status?: Account['status']
  schedulable?: boolean
  load_factor?: number | null
  groups?: AdminDataAccountGroup[]
  bps_403_target_group?: AdminDataAccountGroup
  extra?: Record<string, unknown>
  proxy_key?: string | null
  concurrency: number
  priority: number
  rate_multiplier?: number | null
  expires_at?: number | null
  auto_pause_on_expired?: boolean
}

export interface AdminDataImportError {
  kind: 'proxy' | 'account'
  name?: string
  proxy_key?: string
  message: string
}

export interface AdminDataImportResult {
  proxy_created: number
  proxy_reused: number
  proxy_failed: number
  account_created: number
  account_failed: number
  errors?: AdminDataImportError[]
}

export interface CodexSessionImportRequest {
  content?: string
  contents?: string[]
  name?: string
  notes?: string | null
  group_ids?: number[]
  proxy_id?: number | null
  concurrency?: number
  priority?: number
  rate_multiplier?: number
  load_factor?: number | null
  expires_at?: number | null
  auto_pause_on_expired?: boolean
  credential_extras?: Record<string, unknown>
  extra?: Record<string, unknown>
  update_existing?: boolean
  skip_default_group_bind?: boolean
  confirm_mixed_channel_risk?: boolean
}

export interface OpenAICodexPATCreateRequest {
  access_token: string
  name?: string
  notes?: string | null
  group_ids?: number[]
  proxy_id?: number | null
  concurrency?: number
  priority?: number
  rate_multiplier?: number
  load_factor?: number | null
  expires_at?: number | null
  auto_pause_on_expired?: boolean
  credential_extras?: Record<string, unknown>
  extra?: Record<string, unknown>
  skip_default_group_bind?: boolean
  confirm_mixed_channel_risk?: boolean
}

export interface CodexSessionImportMessage {
  index: number
  name?: string
  message: string
}

export interface CodexSessionImportItem {
  index: number
  name?: string
  action: 'created' | 'updated' | 'skipped' | 'failed'
  account_id?: number
  message?: string
}

export interface CodexSessionImportResult {
  total: number
  created: number
  updated: number
  skipped: number
  failed: number
  items?: CodexSessionImportItem[]
  warnings?: CodexSessionImportMessage[]
  errors?: CodexSessionImportMessage[]
}
