import { inject, type Component, type Ref, type InjectionKey } from 'vue'
import type { StepUpController } from '@/features/identity'
import type { backupAPI } from './api/backup'
import type { pluginsAPI } from './api/plugins'
export interface SystemServices {
  scope: Readonly<Ref<string | null>>
  notifications: { showError(message: string): unknown; showSuccess(message: string): unknown; showWarning(message: string): unknown; showInfo(message: string): unknown }
  backup: typeof backupAPI
  plugins: typeof pluginsAPI
  createStepUp(): StepUpController
  StepUpDialog: Component
  isStepUpCancelled(error: unknown): boolean
  isStepUpBlocked(error: unknown): boolean
  stepUpBlockReason(error: unknown): string
}
export const systemServicesKey: InjectionKey<SystemServices> = Symbol('system.services')
export function useSystemServices(): SystemServices {
  const value = inject(systemServicesKey)
  if (!value) throw new Error('System services are not provided')
  return value
}
