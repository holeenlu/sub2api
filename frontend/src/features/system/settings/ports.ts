import { inject, type Component, type Ref, type InjectionKey } from 'vue'
import type { StepUpController } from '@/features/identity'
import type { settingsAPI } from '@/features/system/api'


export interface SystemSettingsServices {
  scope: Readonly<Ref<string | null>>
  notifications: { showError(message: string): unknown; showSuccess(message: string): unknown; showWarning(message: string): unknown }
  createStepUp(): StepUpController
  StepUpDialog: Component
  isStepUpCancelled(error: unknown): boolean
  control: Pick<typeof settingsAPI, 'deleteAdminApiKey' | 'getAdminApiKey' | 'regenerateAdminApiKey' | 'sendTestEmail' | 'testSmtpConnection' | 'getEmailTemplate' | 'getEmailTemplates' | 'updateEmailTemplate' | 'previewEmailTemplate' | 'restoreOfficialEmailTemplate'>
}
export const systemSettingsServicesKey: InjectionKey<SystemSettingsServices> = Symbol('system.settings')
export function useSystemSettingsServices(): SystemSettingsServices {
  const value = inject(systemSettingsServicesKey)
  if (!value) throw new Error('System settings services are not provided')
  return value
}
