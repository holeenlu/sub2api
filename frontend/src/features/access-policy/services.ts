import { inject, type InjectionKey, type Ref } from 'vue'
export interface AccessPolicyNotifications { showError(message: string): void; showSuccess(message: string): void }
export interface AccessPolicyPermissions {
 readonly isSuperAdmin: boolean
 readonly isSimpleMode: boolean
 can(permission: string): boolean
 canEditAdminField(schema: string, field: string): boolean
 editableAdminFields<T extends object>(schema: string, payload: T): T
}
/** Stable session epoch + portal + business scope; never an access token. */
export const accessPolicyScopeKey: InjectionKey<Readonly<Ref<string>>> = Symbol('access-policy.scope')
export const accessPolicyNotificationsKey: InjectionKey<AccessPolicyNotifications> = Symbol('access-policy.notifications')
export const accessPolicyPermissionsKey: InjectionKey<AccessPolicyPermissions> = Symbol('access-policy.permissions')
export function requireAccessPolicyService<T>(key: InjectionKey<T>): T {
 const value = inject(key)
 if (value === undefined) throw new Error('Missing access-policy binding: ' + key.description)
 return value
}

export interface GroupAccountOption { id: number; name: string }
export interface GroupAccountDirectory {
 search(keyword: string, options: { platform: string; group?: string }, signal: AbortSignal): Promise<GroupAccountOption[]>
 getById(id: number, signal?: AbortSignal): Promise<GroupAccountOption>
}
export interface GroupUserOption { id: number; email: string; username?: string; notes?: string; status?: string }
export interface GroupUserDirectory { search(keyword: string, signal: AbortSignal): Promise<GroupUserOption[]> }
export const accessPolicyAccountsKey: InjectionKey<GroupAccountDirectory> = Symbol('access-policy.accounts')
export const accessPolicyUsersKey: InjectionKey<GroupUserDirectory> = Symbol('access-policy.users')

export interface RiskControlMutationContext { readonly signal: AbortSignal; isCurrent(): boolean }
/** App must recheck this context after reading Settings and before its revision-conditional PUT. No blind conflict retry. */
export interface RiskControlSettings { enable(context: RiskControlMutationContext): Promise<void> }
export const riskControlSettingsKey: InjectionKey<RiskControlSettings> = Symbol('access-policy.risk-settings')
export const riskControlProxiesKey: InjectionKey<(signal: AbortSignal) => Promise<import('@/features/resources/api/proxyTypes').Proxy[]>> = Symbol('access-policy.risk-proxies')
export const accessPolicyModelSelectorKey: InjectionKey<import('vue').Component> = Symbol('access-policy.model-selector')

export const accessPolicyProxyPickerKey: InjectionKey<typeof import('@/features/resources/managementPanels').ProxyPicker> = Symbol('access-policy.proxy-picker')
