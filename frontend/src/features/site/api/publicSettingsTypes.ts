export interface CustomMenuItem {
  id: string
  label: string
  icon_svg: string
  url: string
  page_slug?: string
  hide_open_button?: boolean
  visibility: 'user' | 'admin'
  sort_order: number
}

export interface CustomEndpoint {
  name: string
  endpoint: string
  description: string
}

export interface LoginAgreementDocument {
  id: string
  title: string
  content_md: string
}

export interface PublicSettings {
  vendor_console_enabled?: boolean
  vendor_portal_origin?: string
  registration_enabled: boolean
  email_verify_enabled: boolean
  force_email_on_third_party_signup: boolean
  registration_email_suffix_whitelist: string[]
  registration_email_domain_quota_enabled?: boolean
  password_reset_enabled: boolean
  login_agreement_enabled?: boolean
  login_agreement_mode?: 'modal' | 'checkbox' | string
  login_agreement_updated_at?: string
  login_agreement_revision?: string
  login_agreement_documents?: LoginAgreementDocument[]
  turnstile_enabled: boolean
  tencent_captcha_enabled?: boolean
  tencent_captcha_app_id?: string
  tencent_captcha_region?: string
  passkey_enabled?: boolean
  turnstile_site_key: string
  aliyun_captcha_enabled?: boolean
  aliyun_captcha_scene_id?: string
  aliyun_captcha_prefix?: string
  aliyun_captcha_region?: string
  site_name: string
  site_logo: string
  site_subtitle: string
  api_base_url: string
  contact_info: string
  doc_url: string
  home_content: string
  compact_home_enabled: boolean
  payment_enabled: boolean
  risk_control_enabled: boolean
  table_default_page_size: number
  table_page_size_options: number[]
  custom_menu_items: CustomMenuItem[]
  custom_endpoints: CustomEndpoint[]
  linuxdo_oauth_enabled: boolean
  dingtalk_oauth_enabled?: boolean
  wechat_oauth_enabled: boolean
  wechat_oauth_open_enabled?: boolean
  wechat_oauth_mp_enabled?: boolean
  wechat_oauth_mobile_enabled?: boolean
  oidc_oauth_enabled: boolean
  oidc_oauth_provider_name: string
  github_oauth_enabled: boolean
  google_oauth_enabled: boolean
  backend_mode_enabled: boolean
  version: string
  // 服务器全局时区（IANA 名称与当前 UTC 偏移），高峰时段等服务端本地时间窗口的展示标注用；
  // 可选：注入的 __APP_CONFIG__ 旧缓存可能缺失
  server_timezone?: string
  server_utc_offset?: string
  balance_low_notify_enabled: boolean
  account_quota_notify_enabled: boolean
  balance_low_notify_threshold: number
  channel_monitor_enabled: boolean
  /** Exclusive mode: v1 active probes or v2 passive aggregation. Default v2. */
  channel_monitor_mode?: 'v1' | 'v2'
  channel_monitor_default_interval_seconds: number
  /** When true, user monitor hides RPM/TPM so scale cannot be reverse-estimated. */
  channel_monitor_hide_throughput?: boolean
  /** When true, user monitor shows account quota/balance snapshots (default off). */
  channel_monitor_show_quota?: boolean
  /** When true, user monitor hides the user ranking tab and /users payload. */
  channel_monitor_hide_user_ranking?: boolean
  available_channels_enabled: boolean
  /** When false, the whole user-facing subscription surface is hidden. Default true. */
  subscription_enabled: boolean
  /** Mirrors payment config BALANCE_PAYMENT_DISABLED; true = balance top-up closed (subscription-only site). */
  payment_balance_disabled: boolean
  model_plaza_enabled: boolean
  model_plaza_require_auth: boolean
  plugin_management_enabled: boolean
  service_quota_enabled: boolean
  allow_user_view_error_requests?: boolean
}
