import type { UpdateSettingsRequest } from '@/features/system/api'
import { prepareDefaultSubscriptionSettings } from './defaultSubscriptionSettings'
export function prepareSubscriptionSettings(patch: Pick<UpdateSettingsRequest, 'default_subscriptions'>): Partial<UpdateSettingsRequest> {
  if (!('default_subscriptions' in patch)) return {}
  return { default_subscriptions: prepareDefaultSubscriptionSettings(patch.default_subscriptions, 'default_subscriptions') }
}
