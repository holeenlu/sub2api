import type { UserSubscription } from '@/features/subscriptions/api/types';

export type UserAuthProvider = 'email' | 'linuxdo' | 'oidc' | 'wechat' | 'github' | 'google' | 'dingtalk'

export interface NotifyEmailEntry {
  email: string
  disabled: boolean
  verified: boolean
}

export interface UserAuthBindingStatus {
  bound?: boolean
  bound_count?: number
  provider?: UserAuthProvider | string
  provider_key?: string | null
  provider_subject?: string | null
  issuer?: string | null
  label?: string | null
  provider_label?: string | null
  display_name?: string | null
  subject_hint?: string | null
  verified_at?: string | null
  bind_start_path?: string | null
  can_bind?: boolean
  can_unbind?: boolean
  note_key?: string | null
  note?: string | null
  metadata?: Record<string, unknown>
}

export interface UserProfileSourceContext {
  provider?: UserAuthProvider | string
  source?: string | null
  label?: string | null
  provider_label?: string | null
}

export interface User {
  id: number
  username: string
  email: string
  avatar_url?: string | null
  avatar_source?: string | UserProfileSourceContext | null
  username_source?: string | UserProfileSourceContext | null
  display_name_source?: string | UserProfileSourceContext | null
  nickname_source?: string | UserProfileSourceContext | null
  profile_sources?: {
    avatar?: string | UserProfileSourceContext | null
    username?: string | UserProfileSourceContext | null
    display_name?: string | UserProfileSourceContext | null
    nickname?: string | UserProfileSourceContext | null
  }
  auth_bindings?: Partial<Record<UserAuthProvider, boolean | UserAuthBindingStatus>>
  identity_bindings?: Partial<Record<UserAuthProvider, boolean | UserAuthBindingStatus>>
  email_bound?: boolean
  linuxdo_bound?: boolean
  oidc_bound?: boolean
  wechat_bound?: boolean
  role: 'super_admin' | 'admin' | 'user' | 'vendor_level1' | 'vendor_level2' // User role for authorization
  created_by?: number
  created_by_name?: string
  last_login_at?: string | null
  totp_enabled?: boolean
  policy_version?: number
  admin_write_fields?: Record<string, string[]>
  permissions?: string[]
  admin_pages?: string[]
  admin_features?: Record<string, boolean>
  balance: number // User balance for API usage
  frozen_balance?: number // Balance currently held by async batch jobs
  concurrency: number // Allowed concurrent requests
  rpm_limit?: number // User-level RPM cap (0 = unlimited); effective as fallback when group has no rpm_limit
  status: 'active' | 'disabled' // Account status
  allowed_groups: number[] | null // Allowed group IDs (null = all non-exclusive groups)
  balance_notify_enabled: boolean
  balance_notify_threshold: number | null
  balance_notify_extra_emails: NotifyEmailEntry[]
  subscriptions?: UserSubscription[] // User's active subscriptions
  last_active_at?: string | null
  created_at: string
  updated_at: string
  deleted_at?: string | null
}

export interface AdminUser extends User {
  // 管理员备注（普通用户接口不返回）
  notes: string
  last_used_at?: string | null
  // 用户专属分组倍率配置 (group_id -> rate_multiplier)
  group_rates?: Record<number, number>
  // 为 true 时该用户仅可使用 allowed_groups 中列出的公开分组。
  // 管理侧权限开关，普通用户接口不返回。
  restrict_public_groups?: boolean
  // 当前并发数（仅管理员列表接口返回）
  current_concurrency?: number
}

export interface UpdateUserRequest {
  // Existing security command; actor/target authorization and step-up stay server-enforced.
  reset_totp?: boolean
  email?: string
  password?: string
  username?: string
  notes?: string
  role?: User['role']
  balance?: number
  concurrency?: number
  rpm_limit?: number
  status?: 'active' | 'disabled'
  allowed_groups?: number[] | null
  restrict_public_groups?: boolean
  // 用户专属分组倍率配置 (group_id -> rate_multiplier | null)
  // null 表示删除该分组的专属倍率
  group_rates?: Record<number, number | null>
}

export interface ChangePasswordRequest {
  old_password: string
  new_password: string
}
