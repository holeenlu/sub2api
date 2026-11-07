import type { GroupBadge, GroupOptionItem } from '@/features/access-policy/presentation'
import type { PeakRatePresentation } from '@/features/pricing'
import { inject, type InjectionKey, type Ref } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
import type { Group, GroupPlatform } from '@/features/access-policy/api/groupTypes';
export interface SubscriptionNotifications { showError(message: string): void; showSuccess(message: string): void }
export interface SubscriptionUserOption { id: number; email: string; username?: string; deleted?: boolean }
/** Active assignment and historical (possibly deleted) users have different readers. */
export interface SubscriptionDirectory {
  listGroups(signal: AbortSignal): Promise<Group[]>
  searchAssignableUsers(keyword: string, signal: AbortSignal): Promise<SubscriptionUserOption[]>
  searchHistoryUsers(keyword: string, signal: AbortSignal): Promise<SubscriptionUserOption[]>
}
export interface SubscriptionNavigation {
  openKeys(): void
  openPurchase(groupId: number): void
  usageTarget(userId: number): RouteLocationRaw
  groupsTarget: RouteLocationRaw
}
export const subscriptionNotificationsKey: InjectionKey<SubscriptionNotifications> = Symbol('subscription.notifications')
export const subscriptionScopeKey: InjectionKey<Readonly<Ref<string>>> = Symbol('subscription.scope')
export const subscriptionDirectoryKey: InjectionKey<SubscriptionDirectory> = Symbol('subscription.directory')
export const subscriptionCanManageKey: InjectionKey<Readonly<Ref<boolean>>> = Symbol('subscription.canManage')
export const subscriptionNavigationKey: InjectionKey<SubscriptionNavigation> = Symbol('subscription.navigation')
export interface SubscriptionPresentation {
  readonly peakRate: PeakRatePresentation
  /** Catalog-owned platform order, including the composite option, supplied by app. */
  readonly groupPlatformOptions: Readonly<Ref<readonly Readonly<{ value: GroupPlatform; label: string }>[]>>
  readonly components: { GroupBadge: typeof GroupBadge; GroupOptionItem: typeof GroupOptionItem }
  serverUtcOffset: Readonly<Ref<string | undefined>>
  platformLabel(platform: string): string
}
export const subscriptionPresentationKey: InjectionKey<SubscriptionPresentation> = Symbol('subscription.presentation')
export function requireSubscriptionPort<T>(key: InjectionKey<T>): T {
  const value = inject(key)
  if (value === undefined) throw new Error('Missing subscription app binding: ' + key.description)
  return value
}
