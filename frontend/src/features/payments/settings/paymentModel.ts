import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type PaymentDraft = Pick<SettingsDraft, 'payment_enabled' | 'payment_product_name_prefix' | 'payment_product_name_suffix' | 'payment_min_amount' | 'payment_max_amount' | 'payment_daily_limit' | 'payment_balance_recharge_multiplier' | 'payment_subscription_usd_to_cny_rate' | 'payment_recharge_fee_rate' | 'payment_order_timeout_minutes' | 'payment_max_pending_orders' | 'payment_load_balance_strategy' | 'payment_cancel_rate_limit_enabled' | 'payment_cancel_rate_limit_window_mode' | 'payment_cancel_rate_limit_window' | 'payment_cancel_rate_limit_unit' | 'payment_cancel_rate_limit_max' | 'payment_alipay_force_qrcode' | 'payment_alipay_mobile_precreate_deep_link' | 'payment_help_image_url' | 'payment_help_text' | 'payment_enabled_types'>
export const paymentFields = ['payment_enabled', 'payment_product_name_prefix', 'payment_product_name_suffix', 'payment_min_amount', 'payment_max_amount', 'payment_daily_limit', 'payment_balance_recharge_multiplier', 'payment_subscription_usd_to_cny_rate', 'payment_recharge_fee_rate', 'payment_order_timeout_minutes', 'payment_max_pending_orders', 'payment_load_balance_strategy', 'payment_cancel_rate_limit_enabled', 'payment_cancel_rate_limit_window_mode', 'payment_cancel_rate_limit_window', 'payment_cancel_rate_limit_unit', 'payment_cancel_rate_limit_max', 'payment_alipay_force_qrcode', 'payment_alipay_mobile_precreate_deep_link', 'payment_help_image_url', 'payment_help_text', 'payment_enabled_types'] as const
export function projectPayment(settings: SettingsDraft): PaymentDraft {
  return {
    payment_enabled: structuredClone(toRaw(settings.payment_enabled ?? (false))),
    payment_product_name_prefix: structuredClone(toRaw(settings.payment_product_name_prefix ?? (""))),
    payment_product_name_suffix: structuredClone(toRaw(settings.payment_product_name_suffix ?? (""))),
    payment_min_amount: structuredClone(toRaw(settings.payment_min_amount ?? (1))),
    payment_max_amount: structuredClone(toRaw(settings.payment_max_amount ?? (10000))),
    payment_daily_limit: structuredClone(toRaw(settings.payment_daily_limit ?? (50000))),
    payment_balance_recharge_multiplier: structuredClone(toRaw(settings.payment_balance_recharge_multiplier ?? (1))),
    payment_subscription_usd_to_cny_rate: structuredClone(toRaw(settings.payment_subscription_usd_to_cny_rate ?? (0))),
    payment_recharge_fee_rate: structuredClone(toRaw(settings.payment_recharge_fee_rate ?? (0))),
    payment_order_timeout_minutes: structuredClone(toRaw(settings.payment_order_timeout_minutes ?? (30))),
    payment_max_pending_orders: structuredClone(toRaw(settings.payment_max_pending_orders ?? (3))),
    payment_load_balance_strategy: structuredClone(toRaw(settings.payment_load_balance_strategy ?? ("round-robin"))),
    payment_cancel_rate_limit_enabled: structuredClone(toRaw(settings.payment_cancel_rate_limit_enabled ?? (false))),
    payment_cancel_rate_limit_window_mode: structuredClone(toRaw(settings.payment_cancel_rate_limit_window_mode ?? ("rolling"))),
    payment_cancel_rate_limit_window: structuredClone(toRaw(settings.payment_cancel_rate_limit_window ?? (1))),
    payment_cancel_rate_limit_unit: structuredClone(toRaw(settings.payment_cancel_rate_limit_unit ?? ("day"))),
    payment_cancel_rate_limit_max: structuredClone(toRaw(settings.payment_cancel_rate_limit_max ?? (10))),
    payment_alipay_force_qrcode: structuredClone(toRaw(settings.payment_alipay_force_qrcode ?? (false))),
    payment_alipay_mobile_precreate_deep_link: structuredClone(toRaw(settings.payment_alipay_mobile_precreate_deep_link ?? (false))),
    payment_help_image_url: structuredClone(toRaw(settings.payment_help_image_url ?? (""))),
    payment_help_text: structuredClone(toRaw(settings.payment_help_text ?? (""))),
    payment_enabled_types: structuredClone(toRaw(settings.payment_enabled_types ?? ([]))),
  }
}
