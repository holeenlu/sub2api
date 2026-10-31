import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type BillingModeDraft = Pick<SettingsDraft, 'subscription_enabled' | 'payment_balance_disabled'>
export const billingModeFields = ['subscription_enabled', 'payment_balance_disabled'] as const
export function projectBillingMode(settings: SettingsDraft): BillingModeDraft { return { subscription_enabled: structuredClone(toRaw(settings.subscription_enabled !== false)), payment_balance_disabled: structuredClone(toRaw(settings.payment_balance_disabled === true)) } }
