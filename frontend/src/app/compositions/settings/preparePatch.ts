import { prepareIdentitySettings } from '@/features/identity/settings'
import type { SettingsDraft } from '@/features/system/api'
import { prepareSiteSettings } from '@/features/site/settings'
import { prepareConsoleSettings } from '@/features/system/settings'
import { prepareResourceSettings } from '@/features/resources/settings'
import { prepareSubscriptionSettings } from '@/features/subscriptions/settings'
import { preparePaymentSettings } from '@/features/payments/settings'
import { prepareAccessPolicySettings } from '@/features/access-policy/settings'
import type { SettingsPatch } from './transaction'
import { listPlatformIds } from '@/features/catalog'
import { prepareAuthSourceSubscriptions } from './authSourceDefaults'
/** Each domain validates and projects its modified fields; the composition performs one PUT. */
export function prepareSettingsPatch(patch: SettingsPatch, draft: SettingsDraft): SettingsPatch {
  return { ...patch, ...prepareIdentitySettings(patch, draft), ...prepareAuthSourceSubscriptions(patch), ...prepareSiteSettings(patch, draft), ...prepareConsoleSettings(patch, draft), ...prepareResourceSettings(patch, draft), ...prepareSubscriptionSettings(patch), ...preparePaymentSettings(patch, draft), ...prepareAccessPolicySettings(patch, listPlatformIds()) }
}
