import { inject, type InjectionKey, type Ref } from 'vue'
import type { AdminGroup, GroupPlatform } from '@/features/access-policy/api/groupTypes';
export interface PricingNotifications { showError(message: string): void; showSuccess(message: string): void }
export interface PricingPermissions {
  readonly isSuperAdmin: boolean
  can(permission: string): boolean
  canEditAdminField(schema: string, field: string): boolean
  editableAdminFields<T extends object>(schema: string, payload: T): T
}
export interface PricingAccountOption { id: number; name: string; platform: string }
export interface PricingDirectory {
  listGroups(signal: AbortSignal): Promise<AdminGroup[]>
  searchAccounts(platform: string, keyword: string, signal: AbortSignal): Promise<PricingAccountOption[]>
  getAccount(id: number, signal: AbortSignal): Promise<PricingAccountOption>
}
export interface PricingCatalog {
 platformLabel(platform: string): string
  listPlatformIds(): GroupPlatform[]
  supportsPricingModelSync(platform: string): boolean
}
export const pricingNotificationsKey: InjectionKey<PricingNotifications> = Symbol('pricing.notifications')
export const pricingPermissionsKey: InjectionKey<PricingPermissions> = Symbol('pricing.permissions')
export const pricingScopeKey: InjectionKey<Readonly<Ref<string>>> = Symbol('pricing.scope')
export const pricingDirectoryKey: InjectionKey<PricingDirectory> = Symbol('pricing.directory')
export const pricingCatalogKey: InjectionKey<PricingCatalog> = Symbol('pricing.catalog')
export const pricingWebSearchKey: InjectionKey<(signal: AbortSignal) => Promise<boolean>> = Symbol('pricing.webSearch')
export function requirePricingPort<T>(key: InjectionKey<T>): T {
  const value = inject(key)
  if (value === undefined) throw new Error('Missing pricing app binding: ' + key.description)
  return value
}
