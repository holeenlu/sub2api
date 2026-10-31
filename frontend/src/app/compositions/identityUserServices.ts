import type { App } from 'vue'
import { lazyPanel } from './panelLoadingServices'
import { identityUserDirectoryDataKey, identityUserPanelsKey } from '@/features/identity'
import { groupsAPI } from '@/features/access-policy'
import { getBatchUsersUsage } from '@/features/usage/adminDashboard'
import { getPlatformQuotas } from "@/features/access-policy/userAccess"
import { buildApiKeyGroupFilterOptions } from '@/app/compositions/user-directory/apiKeyGroupFilterOptions'

export function installIdentityUserServices(app: App): void {
  app.provide(identityUserDirectoryDataKey, {
    listGroups: includeInactive => includeInactive ? groupsAPI.getAllIncludingInactive() : groupsAPI.getAll(),
    buildApiKeyGroupOptions: buildApiKeyGroupFilterOptions,
    loadUsage: getBatchUsersUsage, loadPlatformQuotas: getPlatformQuotas
  })
  app.provide(identityUserPanelsKey, {
    GroupBadge,
    UserApiKeysModal: lazyPanel(() => import('@/app/compositions/user-directory/UserApiKeysModal.vue')),
    UserAllowedGroupsModal: lazyPanel(() => import('@/app/compositions/user-directory/UserAllowedGroupsModal.vue')),
    GroupReplaceModal: lazyPanel(() => import('@/app/compositions/user-directory/GroupReplaceModal.vue')),
    UserBalanceModal: lazyPanel(() => import('@/app/compositions/user-directory/UserBalanceModal.vue')),
    UserBalanceHistoryModal: lazyPanel(() => import('@/app/compositions/user-directory/UserBalanceHistoryModal.vue')),
    UserPlatformQuotaModal: lazyPanel(() => import('@/app/compositions/user-directory/UserPlatformQuotaModal.vue')),
    PlatformUsageBreakdown: lazyPanel(() => import('@/app/compositions/user-directory/PlatformUsageBreakdown.vue')),
    PlatformCostCell: lazyPanel(() => import('@/app/compositions/user-directory/PlatformCostCell.vue')),
    UserPlatformQuotaCell: lazyPanel(() => import('@/app/compositions/user-directory/UserPlatformQuotaCell.vue'))
  })
}
import { GroupBadge } from '@/features/access-policy/presentation'
