import type { GroupBadge, GroupOptionItem } from '@/features/access-policy/presentation'
import { inject, type InjectionKey } from 'vue'
import type { AuthSourceDefaultsState } from '@/features/system/api'
import type { AuthDefaultsDraft } from './authDefaultsModel'


import type { AccountPlatform } from '@/features/resources/api/platformTypes';
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';

export interface IdentitySettingsServices {
  readonly authDefaults: {
    build(settings: Partial<AuthDefaultsDraft>): AuthSourceDefaultsState
    append(payload: Partial<AuthDefaultsDraft>, state: AuthSourceDefaultsState): Partial<AuthDefaultsDraft>
  }
  readonly components: { GroupBadge: typeof GroupBadge; GroupOptionItem: typeof GroupOptionItem }
  copy(value: string, successMessage?: string): Promise<boolean>
  groups: { getAll(): Promise<AdminGroup[]>; getAllIncludingInactive(): Promise<AdminGroup[]> }
  readonly platformOptions: { value: string; label: string }[]
  platformIds(): AccountPlatform[]
}
export const identitySettingsServicesKey: InjectionKey<IdentitySettingsServices> = Symbol('identity.settings')
export function useIdentitySettingsServices(): IdentitySettingsServices {
  const value = inject(identitySettingsServicesKey)
  if (!value) throw new Error('Identity settings services are not provided')
  return value
}
