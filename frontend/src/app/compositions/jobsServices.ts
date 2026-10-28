import { computed, type App } from 'vue'
import { useAuthStore } from '@/features/identity'
import { keysAPI } from '@/features/keys'
import { jobsServicesKey, type JobsServices } from '@/features/jobs'
import { useAppStore } from '@/app/model/app'
import { isVendorOrigin } from '@/features/supply'

export function installJobsServices(app: App, copyText: JobsServices['copyText']): void {
  const auth = useAuthStore()
  const settings = useAppStore()
  app.provide(jobsServicesKey, {
    scopeKey: computed(() => auth.isAuthenticated ? JSON.stringify([auth.identityScopeKey,
      import.meta.env.VITE_VENDOR_PORTAL === 'true' || isVendorOrigin(settings.cachedPublicSettings) ? 'supplier' : 'console']) : null),
    publicSettings: computed(() => settings.cachedPublicSettings),
    listPersonalKeys(page, pageSize, filters, options) {
      const order = filters.sort_order
      if (order !== 'asc' && order !== 'desc') throw new Error('Invalid key sort order')
      return keysAPI.list(page, pageSize, { ...filters, sort_order: order }, options)
    },
    notifySuccess: settings.showSuccess, notifyError: settings.showError, copyText
  })
}
