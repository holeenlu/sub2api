import type { SystemSettings, DefaultPlatformQuotasMap, AccountSchedulingThresholdsMap } from '../api/settings'
import type { RechargeBonusTierDraft, RechargeBonusMode } from '@/features/payments/settings'

export type SettingsDraft = Omit<
  SystemSettings,
  | "wechat_connect_open_enabled"
  | "wechat_connect_mp_enabled"
  | "wechat_connect_mobile_enabled"
  | "openai_oauth_scheduling_rate_multiplier"
> & {
  /** Form always binds a concrete boolean (SystemSettings marks this optional). */
  channel_monitor_hide_throughput: boolean;
  channel_monitor_show_quota: boolean;
  channel_monitor_hide_user_ranking: boolean;
  smtp_password: string;
  turnstile_secret_key: string;
  tencent_captcha_app_secret_key: string;
  tencent_captcha_cloud_secret_id: string;
  tencent_captcha_cloud_secret_key: string;
  aliyun_captcha_access_key_secret: string;
  linuxdo_connect_client_secret: string;
  dingtalk_connect_client_secret: string;
  wechat_connect_app_secret: string;
  wechat_connect_open_app_secret: string;
  wechat_connect_mp_app_secret: string;
  wechat_connect_mobile_app_secret: string;
  wechat_connect_open_enabled: boolean;
  wechat_connect_mp_enabled: boolean;
  wechat_connect_mobile_enabled: boolean;
  oidc_connect_client_secret: string;
  github_oauth_client_secret: string;
  google_oauth_client_secret: string;
  force_email_on_third_party_signup: boolean;
  openai_low_upstream_rate_priority_enabled: boolean;
  openai_oauth_scheduling_rate_multiplier: number | "" | null;
  openai_advanced_scheduler_subscription_priority_enabled: boolean;
  // 充值赠送阶梯编辑态：允许留空的行，提交时清洗为 RechargeBonusTier[]
  payment_recharge_bonus_tiers: RechargeBonusTierDraft[];
  payment_recharge_bonus_mode: RechargeBonusMode;
  payment_recharge_bonus_notice: string;
  // 系统全局平台限额 map；form 内始终归一化为全部平台对象（模板非空绑定依赖此不变量）
  default_platform_quotas: DefaultPlatformQuotasMap;
  account_scheduling_thresholds: AccountSchedulingThresholdsMap;
};
