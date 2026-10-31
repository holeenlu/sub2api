import { inject, type InjectionKey } from 'vue'

import type { settingsAPI } from '@/features/system/api'


export interface SettingsUserOption { id: number; email: string; deleted?: boolean }
export interface AccessPolicySettingsServices {
  users: { search(query: string): Promise<SettingsUserOption[]>; get(id: number): Promise<{ id: number; email: string; deleted_at?: string | null }> }
  notifications: { showError(message: string): unknown; showSuccess(message: string): unknown; showWarning(message: string): unknown }
  control: Pick<typeof settingsAPI, 'getBetaPolicySettings' | 'getPanelRateLimitSettings' | 'updateBetaPolicySettings' | 'updatePanelRateLimitSettings'>
}
export const accesspolicySettingsServicesKey: InjectionKey<AccessPolicySettingsServices> = Symbol('access-policy.settings')
export function useAccessPolicySettingsServices(): AccessPolicySettingsServices {
  const value = inject(accesspolicySettingsServicesKey)
  if (!value) throw new Error('AccessPolicy settings services are not provided')
  return value
}
