import type { GroupBadge } from '@/features/access-policy/presentation'
import { inject, type Component, type InjectionKey, type Ref } from 'vue'
import type { ApiKey } from '@/features/keys/api/types';
import type { Group } from '@/features/access-policy/api/groupTypes';
import type { Provider } from '@/features/operations/api'

export type MonitorKey = Pick<ApiKey, 'id' | 'name' | 'key' | 'status' | 'expires_at'> & {
  group?: Pick<Group, 'id' | 'name' | 'platform' | 'subscription_type' | 'rate_multiplier'> | null
}
export interface MonitorLinkedAccount { id: number; name: string; platform: string }
export interface MonitorDisplaySettings {
  defaultIntervalSeconds?: number
  hideThroughput: boolean
  hideUserRanking: boolean
  showQuota: boolean
}
/** Cross-domain reads and presentation only; monitoring commands stay in operations API. */
export interface MonitorServices {
  canReadAdmin: Readonly<Ref<boolean>>
  displaySettings: Readonly<Ref<MonitorDisplaySettings>>
  listLinkedAccounts(query: { provider: Provider; search: string; signal?: AbortSignal }): Promise<MonitorLinkedAccount[]>
  getLinkedAccount(id: number, options?: { signal?: AbortSignal }): Promise<MonitorLinkedAccount>
  listActiveKeys(options?: { signal?: AbortSignal }): Promise<MonitorKey[]>
  getUserGroupRates(options?: { signal?: AbortSignal }): Promise<Record<number, number>>
  components: { ModelTagInput: Component; UsageProgressBar: Component; GroupBadge: typeof GroupBadge }
}
export const monitorServicesKey: InjectionKey<MonitorServices> = Symbol('operations.monitor')
export function useMonitorServices(): MonitorServices {
  const value = inject(monitorServicesKey)
  if (!value) throw new Error('MonitorServices must be provided by app composition')
  return value
}
