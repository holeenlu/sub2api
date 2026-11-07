import { inject, type InjectionKey, type Ref } from 'vue'
export interface OperationsGroup { id: number; name: string; platform: string; status: 'active' | 'inactive' }
export interface OperationsPermissions { isSuperAdmin: boolean; canManageOps: boolean; canReadPromptContent: boolean }
export interface OperationsMonitoring { enabled: boolean; realtimeEnabled: boolean; queryModeDefault: 'auto' | 'raw' | 'preagg' }
/** App owns identity, public settings and cross-domain lookups. No domain store is exposed. */
export interface OperationsServices {
  errorPresentation: typeof import('@/features/usage/errorPresentation')
  scopeKey: Readonly<Ref<string | null>>
  permissions: Readonly<Ref<OperationsPermissions>>
  platformOptions: Readonly<Ref<ReadonlyArray<{ value: string; label: string }>>>
  monitoring: Readonly<Ref<OperationsMonitoring>>
  publicSettings: Readonly<Ref<{ channel_monitor_enabled?: boolean; channel_monitor_mode?: string } | null>>
  loadMonitoringSettings(options?: { signal?: AbortSignal }): Promise<unknown>
  disableRealtimeMonitoring(): void
  listGroups(options: { includeInactive: boolean; signal?: AbortSignal }): Promise<OperationsGroup[]>
  listAccountNames(page: number, pageSize: number, options: { signal?: AbortSignal }): Promise<{
    items: Array<{ id: number; name: string }>; total: number
  }>
  getTotpStatus(options?: { signal?: AbortSignal }): Promise<{ enabled: boolean }>
  notifySuccess(message: string): void
  notifyError(message: string): void
  notifyWarning(message: string): void
  copyText(text: string, successMessage?: string): Promise<boolean>
}
export const operationsServicesKey: InjectionKey<OperationsServices> = Symbol('OperationsServices')
export function useOperationsServices(): OperationsServices {
  const services = inject(operationsServicesKey)
  if (!services) throw new Error('OperationsServices must be provided by app composition')
  return services
}
