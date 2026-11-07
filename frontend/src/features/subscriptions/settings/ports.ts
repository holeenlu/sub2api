import type { GroupBadge, GroupOptionItem } from '@/features/access-policy/presentation'
import { inject, type InjectionKey } from 'vue'


import type { AccountPlatform } from '@/features/resources/api/platformTypes';
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';

export interface SubscriptionsSettingsServices {
  readonly components: { GroupBadge: typeof GroupBadge; GroupOptionItem: typeof GroupOptionItem }
  groups: { getAll(): Promise<AdminGroup[]> }
  platformIds(): AccountPlatform[]
}
export const subscriptionsSettingsServicesKey: InjectionKey<SubscriptionsSettingsServices> = Symbol('subscriptions.settings')
export function useSubscriptionsSettingsServices(): SubscriptionsSettingsServices {
  const value = inject(subscriptionsSettingsServicesKey)
  if (!value) throw new Error('Subscriptions settings services are not provided')
  return value
}
