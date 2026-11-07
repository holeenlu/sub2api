import { inject, type Component, type InjectionKey, type Ref } from 'vue'
export interface SettingsWorkspaceServices {
  scope: Readonly<Ref<string | null>>
  readonly isSuperAdmin: boolean
  notifications: { showError(message: string): unknown; showSuccess(message: string): unknown }
  DelegatedSettings: Component
  refreshSettings(): Promise<unknown>
}
export const settingsWorkspaceServicesKey: InjectionKey<SettingsWorkspaceServices> = Symbol('settings.workspace')
export function useSettingsWorkspaceServices(): SettingsWorkspaceServices {
  const value = inject(settingsWorkspaceServicesKey)
  if (!value) throw new Error('Settings workspace services are not provided')
  return value
}
