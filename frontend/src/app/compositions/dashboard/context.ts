import { computed, inject } from 'vue'
import { useAuthStore } from '@/features/identity'
import { useBatchImageAccess } from '@/features/jobs'
import { usageServicesKey } from '@/features/usage'
/** Cross-domain composition consumes the domain public contracts only. */
export function useDashboardContext() {
  const usage = inject(usageServicesKey)
  if (!usage) throw new Error('UsageServices must be provided for dashboards')
  const identity = useAuthStore(), jobs = useBatchImageAccess()
  return { scopeKey: usage.scopeKey, notifyError: usage.notifyError, user: computed(() => identity.user), isSimple: computed(() => identity.isSimpleMode),
    refreshUser: () => identity.refreshUser(), canUseBatchImage: jobs.canUseBatchImage, refreshBatchImageAccess: jobs.refreshBatchImageAccess }
}
