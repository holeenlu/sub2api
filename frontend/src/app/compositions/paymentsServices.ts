import { platformLabel } from '@/features/catalog'
import { peakRatePresentation } from '@/features/pricing'
import { computed, type App } from 'vue'
import { useAuthStore, getProfile } from '@/features/identity'
import { paymentsServicesKey, paymentPlansServicesKey, balanceHistoryAccountReaderKey, paymentAPI } from '@/features/payments'
import { groupsAPI } from '@/features/access-policy'
import { useSubscriptionStore } from '@/features/subscriptions'
import { useAppStore } from '@/app/model/app'
import { FeatureFlags, isFeatureFlagEnabled } from '@/app/navigation/featureFlags'

export function installPaymentsServices(app: App): void {
  const auth = useAuthStore()
  const settings = useAppStore()
  const subscriptions = useSubscriptionStore()
  app.provide(balanceHistoryAccountReaderKey, async options => {
    const { id, email, username, balance } = await getProfile(options)
    return { id, email, username, balance }
  })
  app.provide(paymentPlansServicesKey, {
    components: { GroupBadge },
    listGroups: options => groupsAPI.getAll(undefined, options),
    permissions: computed(() => ({
      canManagePlans: auth.can('plans.manage'), canUpdateRates: auth.can('billing.rates.update'), canReadPaymentConfig: auth.isSuperAdmin,
      createFields: auth.isSuperAdmin ? undefined : auth.user?.admin_write_fields?.['plan.create'] ?? [],
      updateFields: auth.isSuperAdmin ? undefined : auth.user?.admin_write_fields?.['plan.update'] ?? []
    }))
  })
  app.provide(paymentsServicesKey, {
    peakRate: peakRatePresentation,
    platformLabel,
    scopeKey: computed(() => auth.isAuthenticated ? auth.identityScopeKey : null),
    permissions: computed(() => ({ canManageOrders: auth.can('orders.manage'), canRefundOrders: auth.can('orders.refund') })),
    user: computed(() => auth.user ? { id: auth.user.id, username: auth.user.username, balance: auth.user.balance } : null),
    publicSettings: computed(() => settings.cachedPublicSettings),
    subscriptionEnabled: computed(() => isFeatureFlagEnabled(FeatureFlags.subscription)),
    activeSubscriptions: computed(() => subscriptions.activeSubscriptions),
    refreshUser: auth.refreshUser,
    refreshSubscriptions: subscriptions.fetchActiveSubscriptions,
    notifySuccess: settings.showSuccess, notifyError: settings.showError,
    notifyInfo: settings.showInfo, notifyWarning: settings.showWarning,
    async resolvePublicOrder(lookup, options) {
      const response = lookup.resumeToken !== undefined
        ? await paymentAPI.resolveOrderPublicByResumeToken(lookup.resumeToken, options)
        : await paymentAPI.verifyOrderPublic(lookup.outTradeNo, options)
      return response.data
    }
  })
}
import { GroupBadge } from '@/features/access-policy/presentation'
