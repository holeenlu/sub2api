import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore } from '@/app/model/app'
import { useAdminSettingsStore } from '@/app/model/adminSettings'
import { useAuthStore } from '@/features/identity'
import { resolveRouteMetaKeys } from '@/app/router/title'
import { FeatureFlags, isFeatureFlagEnabled } from './featureFlags'
import { resolveSiteBillingMode } from './siteBillingMode'
import type { NavigationContext } from './routeContributions'

/** Share the menu's existing policy without starting another access query.
 * Sidebar owns batch-image access; other consumers cannot invent that grant.
 */
export function useNavigationContext(batchImageAccess: () => boolean = () => false): NavigationContext {
  const { t } = useI18n()
  const route = useRoute()
  const router = useRouter()
  const app = useAppStore()
  const auth = useAuthStore()
  const admin = useAdminSettingsStore()
  return {
    get simpleMode() { return auth.isSimpleMode },
    translate: key => t(key),
    canAccessAdminPage: path => auth.canAccessAdminPage(path),
    featureEnabled(feature) {
      if (feature === 'opsMonitoring') return admin.opsMonitoringEnabled
      if (feature === 'adminPayment') return admin.paymentEnabled
      if (feature === 'batchImageAccess') return batchImageAccess()
      return isFeatureFlagEnabled(FeatureFlags[feature])
    },
    titleKey: contribution => resolveRouteMetaKeys(contribution, {
      billingMode: resolveSiteBillingMode(app.cachedPublicSettings),
    }).titleKey,
    resolvePath: contribution => contribution.name
      ? router.resolve({ name: contribution.name, params: route.params }).path
      : undefined,
  }
}
