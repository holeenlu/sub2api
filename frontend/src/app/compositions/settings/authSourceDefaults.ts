import { AUTH_SOURCE_TYPES, projectAuthSourceGrants, prepareAuthSourceGrants } from '@/features/identity/settings'
import type { AuthDefaultsDraft, IdentitySettingsServices } from '@/features/identity/settings'
import { normalizePlatformQuotasMap, sanitizePlatformQuotasMap } from '@/features/access-policy/settings'
import { normalizeDefaultSubscriptionSettings, prepareDefaultSubscriptionSettings } from '@/features/subscriptions/settings'
import type { AuthSourceDefaultsState, PlatformType } from '@/features/system/api'
import type { SettingsPatch } from './transaction'

/** Authentication grants compose identity, subscription and quota policy in the app. */
export function createAuthSourceDefaultsServices(
  platformIds: () => readonly PlatformType[],
): IdentitySettingsServices['authDefaults'] {
  return {
    build(settings) {
      return AUTH_SOURCE_TYPES.reduce((state, source) => {
        const subscriptions = settings[`auth_source_default_${source}_subscriptions`]
        state[source] = {
          ...projectAuthSourceGrants({
            balance: settings[`auth_source_default_${source}_balance`],
            concurrency: settings[`auth_source_default_${source}_concurrency`],
            grant_on_signup: settings[`auth_source_default_${source}_grant_on_signup`],
            grant_on_first_bind: settings[`auth_source_default_${source}_grant_on_first_bind`],
          }),
          subscriptions: normalizeDefaultSubscriptionSettings(Array.isArray(subscriptions) ? subscriptions : []),
          platform_quotas: normalizePlatformQuotasMap(platformIds(), settings[`auth_source_default_${source}_platform_quotas`]),
        }
        return state
      }, {} as AuthSourceDefaultsState)
    },
    append(payload, state) {
      const target = payload as Record<string, unknown>
      for (const source of AUTH_SOURCE_TYPES) {
        const current = state[source]
        for (const [field, value] of Object.entries(prepareAuthSourceGrants(current))) {
          target['auth_source_default_' + source + '_' + field] = value
        }
        target['auth_source_default_' + source + '_subscriptions'] = normalizeDefaultSubscriptionSettings(current.subscriptions)
        target['auth_source_default_' + source + '_platform_quotas'] = sanitizePlatformQuotasMap(platformIds(), current.platform_quotas)
      }
      return payload
    },
  }
}

export function prepareAuthSourceSubscriptions(patch: SettingsPatch): SettingsPatch {
  const result: SettingsPatch = {}
  const authDefaults: Partial<AuthDefaultsDraft> = patch
  for (const [key, value] of Object.entries(authDefaults)) {
    if (!key.startsWith('auth_source_default_') || !key.endsWith('_subscriptions') || !Array.isArray(value)) continue
    Object.assign(result, { [key]: prepareDefaultSubscriptionSettings(value, key) })
  }
  return result
}
