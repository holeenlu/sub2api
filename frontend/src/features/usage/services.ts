import { inject, type InjectionKey, type Ref, type Component } from 'vue'
import type { Account } from '@/features/resources/api/accountTypes';
import type { AdminUser } from '@/features/identity/api/profileTypes';
import type { ApiKey } from '@/features/keys/api/types';
import type { Group } from '@/features/access-policy/api/groupTypes';
import type { PaginatedResponse } from '@/shared/http/requestTypes';
import type { PublicSettings } from '@/features/site/api/publicSettingsTypes';
import type { DashboardSnapshotV2Params, DashboardSnapshotV2Response, ModelStatsParams, ModelStatsResponse,
  UserBreakdownParams, UserBreakdownResponse, APIKeyBreakdownResponse } from '@/features/usage/adminDashboard'
import type { OpsErrorListQueryParams, OpsErrorLogsResponse } from '@/features/operations'

export interface UsageRequestOptions { signal?: AbortSignal }
export interface UsagePermissions { isSuperAdmin: boolean; canExport: boolean; canReadBillingCost: boolean }
export interface UsagePanels {
  /** show/user/hideActions props; close event. */
  balanceHistory: Component
  /** Existing rows/loading/paging/column props and user/detail/sort events. */
  adminErrorsTable: Component
  /** show/errorId/errorType props; update:show event. */
  adminErrorDetail: Component
}
export interface UsageServices {
  panels: UsagePanels
  /** Actual identity epoch, portal and business scope; null means no private query. */
  scopeKey: Readonly<Ref<string | null>>
  permissions: Readonly<Ref<UsagePermissions>>
  publicSettings: Readonly<Ref<Pick<PublicSettings, 'allow_user_view_error_requests'> | null>>
  subscriptionEnabled: Readonly<Ref<boolean>>
  notifySuccess(message: string): void
  notifyError(message: string): void
  notifyInfo(message: string): void
  notifyWarning(message: string): void
  copyText(text: string, successMessage?: string): Promise<boolean>
  listUserKeys(page: number, pageSize: number, options?: UsageRequestOptions): Promise<PaginatedResponse<Pick<ApiKey, 'id' | 'name'>>>
  getUserGroups(options?: UsageRequestOptions): Promise<Group[]>
  getAdminUser(id: number, options?: UsageRequestOptions): Promise<AdminUser>
  listAdminGroups(options?: UsageRequestOptions): Promise<Group[]>
  searchAdminAccounts(keyword: string, options?: UsageRequestOptions): Promise<Array<Pick<Account, 'id' | 'name'>>>
  getAdminSnapshot(params: DashboardSnapshotV2Params, options?: UsageRequestOptions): Promise<DashboardSnapshotV2Response>
  getAdminModelStats(params: ModelStatsParams, options?: UsageRequestOptions): Promise<ModelStatsResponse>
  getAdminUserBreakdown(params: UserBreakdownParams, options?: UsageRequestOptions): Promise<UserBreakdownResponse>
  getAdminKeyBreakdown(params: UserBreakdownParams, options?: UsageRequestOptions): Promise<APIKeyBreakdownResponse>
  getAdminErrors(params: OpsErrorListQueryParams, options?: UsageRequestOptions): Promise<OpsErrorLogsResponse>
}
export const usageServicesKey: InjectionKey<UsageServices> = Symbol('UsageServices')
export function useUsageServices(): UsageServices {
  const services = inject(usageServicesKey)
  if (!services) throw new Error('UsageServices must be provided by the application')
  return services
}
