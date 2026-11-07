import { computed, type App } from 'vue'
import { systemServicesKey, backupAPI, pluginsAPI } from '@/features/system'
import { useAuthStore, useStepUp, TotpStepUpDialog, isStepUpCancelled, isStepUpBlocked, stepUpBlockReason } from '@/features/identity'
import { useAppStore } from '@/app/model/app'

export function installSystemServices(app: App): void {
  const notices = useAppStore()
  const auth = useAuthStore()
  app.provide(systemServicesKey, {
    scope: computed(() => auth.isAuthenticated ? auth.identityScopeKey : null),
    notifications: { showError: notices.showError, showSuccess: notices.showSuccess, showWarning: notices.showWarning, showInfo: notices.showInfo },
    backup: backupAPI, plugins: pluginsAPI, createStepUp: useStepUp, StepUpDialog: TotpStepUpDialog,
    isStepUpCancelled, isStepUpBlocked, stepUpBlockReason
  })
}
