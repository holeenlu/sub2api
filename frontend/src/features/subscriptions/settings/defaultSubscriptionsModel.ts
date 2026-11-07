import { toRaw } from 'vue'
import { normalizeDefaultSubscriptionSettings } from './defaultSubscriptionSettings'
import type { SettingsDraft } from '@/features/system/api'

export type DefaultSubscriptionsDraft = Pick<SettingsDraft, 'default_subscriptions'>
export const defaultSubscriptionsFields = ['default_subscriptions'] as const
export function projectDefaultSubscriptions(settings: SettingsDraft): DefaultSubscriptionsDraft { return { default_subscriptions: structuredClone(toRaw(normalizeDefaultSubscriptionSettings(settings.default_subscriptions))) } }
