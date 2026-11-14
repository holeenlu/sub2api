import type { GroupBadge } from '@/features/access-policy/presentation'
import { inject, type Component, type InjectionKey } from 'vue'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import type { BatchUserUsageStats } from '@/features/usage/adminDashboard'
import type { PlatformQuotasResponse } from '@/features/access-policy/api/platformQuotaTypes';

/** Read-only external projections used by the user directory; no provider API is imported at runtime. */
export interface IdentityUserDirectoryData {
  listGroups(includeInactive?: boolean): Promise<AdminGroup[]>
  buildApiKeyGroupOptions(groups: AdminGroup[], labels: {
    all: string; exclusive: string; public: string; subscription: string; disabled: string
  }): Array<{ value: number | null; label: string; kind?: 'group'; disabled?: boolean }>
  loadUsage(userIds: number[]): Promise<{ stats: Record<string, BatchUserUsageStats> }>
  loadPlatformQuotas(userId: number): Promise<PlatformQuotasResponse>
}

/** App compositions retain each existing panel's props/events and server-authorized workflow. */
export interface IdentityUserPanels {
  GroupBadge: typeof GroupBadge
  UserApiKeysModal: Component
  UserAllowedGroupsModal: Component
  GroupReplaceModal: Component
  UserBalanceModal: Component
  UserBalanceHistoryModal: Component
  UserPlatformQuotaModal: Component
  PlatformUsageBreakdown: Component
  PlatformCostCell: Component
  UserPlatformQuotaCell: Component
}

export const identityUserDirectoryDataKey: InjectionKey<IdentityUserDirectoryData> = Symbol('identity.userDirectoryData')
export const identityUserPanelsKey: InjectionKey<IdentityUserPanels> = Symbol('identity.userPanels')

export function useIdentityUserDirectoryData(): IdentityUserDirectoryData {
  const data = inject(identityUserDirectoryDataKey)
  if (!data) throw new Error('User directory external data is not provided')
  return data
}

export function useIdentityUserPanels(): IdentityUserPanels {
  const panels = inject(identityUserPanelsKey)
  if (!panels) throw new Error('User management app compositions are not provided')
  return panels
}
