import { inject, type InjectionKey } from 'vue'

import type { settingsAPI } from '@/features/system/api'

import type { accountsAPI } from '@/features/resources'
import type { proxiesAPI } from '@/features/resources'

export interface ResourcesSettingsServices {
  notifications: { showError(message: string): unknown; showSuccess(message: string): unknown; showWarning(message: string): unknown }
  control: Pick<typeof settingsAPI, 'getOverloadCooldownSettings' | 'getRateLimit429CooldownSettings' | 'getRectifierSettings' | 'getStreamTimeoutSettings' | 'getWebSearchEmulationConfig' | 'resetWebSearchUsage' | 'testWebSearchEmulation' | 'updateOverloadCooldownSettings' | 'updateRateLimit429CooldownSettings' | 'updateRectifierSettings' | 'updateStreamTimeoutSettings' | 'updateWebSearchEmulationConfig'>
  accounts: Pick<typeof accountsAPI, 'getOllamaCloudUsageSettings' | 'getOpenCodeGoUsageSettings' | 'getUpstreamBillingProbeSettings' | 'updateOllamaCloudUsageSettings' | 'updateOpenCodeGoUsageSettings' | 'updateUpstreamBillingProbeSettings'>
  proxies: Pick<typeof proxiesAPI, 'list'>
}
export const resourcesSettingsServicesKey: InjectionKey<ResourcesSettingsServices> = Symbol('resources.settings')
export function useResourcesSettingsServices(): ResourcesSettingsServices {
  const value = inject(resourcesSettingsServicesKey)
  if (!value) throw new Error('Resources settings services are not provided')
  return value
}
