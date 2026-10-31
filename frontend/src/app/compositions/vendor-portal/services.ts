import { computed, watch, type App } from 'vue'
import { useAuthStore } from '@/features/identity'
import { platformDisplayName } from '@/features/catalog'
import { groupsAPI } from '@/features/access-policy/groups'
import { useAppStore } from '@/app/model/app'
import { supplyServicesKey, useSupplyStore, isVendorOrigin, type SupplyServices } from '@/features/supply'

/** App lifetime owner; call once after Pinia and before guards refresh supplier identity. */
export function installSupplyServices(app: App): () => void {
  const auth = useAuthStore()
  const settings = useAppStore()
  const supply = useSupplyStore()
  const sessionScope = computed(() => auth.isAuthenticated
    ? JSON.stringify([auth.identityScopeKey, auth.user?.role,
    import.meta.env.VITE_VENDOR_PORTAL === 'true' || isVendorOrigin(settings.cachedPublicSettings) ? 'supplier' : 'console']) : null)
  const stop = watch(sessionScope, value => supply.setSessionScope(value), { immediate: true, flush: 'sync' })
  app.provide(supplyServicesKey, {
    scopeKey: computed(() => sessionScope.value === null ? null : JSON.stringify([
      sessionScope.value, supply.vendor?.id, supply.vendor?.policy_version, supply.vendor?.contract_version,
    ])),
    identity: computed(() => ({ userId: auth.user?.id ?? null, role: auth.user?.role ?? '', isSuperAdmin: auth.isSuperAdmin })),
    platformName: platformDisplayName,
    getAssignableGroups: options => groupsAPI.getAll(undefined, options),
  } satisfies SupplyServices)
  return () => { stop(); supply.setSessionScope(null) }
}
