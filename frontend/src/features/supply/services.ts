import { inject, type InjectionKey, type Ref } from 'vue'
import type { VendorGroup } from './types'
export interface SupplyIdentity { userId: number | null; role: string; isSuperAdmin: boolean }
/** Domain consumes verified identity/catalog/access projections; app owns their implementation. */
export interface SupplyServices {
  scopeKey: Readonly<Ref<string | null>>
  identity: Readonly<Ref<SupplyIdentity>>
  platformName(platform: string): string
  getAssignableGroups(options: { signal: AbortSignal }): Promise<VendorGroup[]>
}
export const supplyServicesKey: InjectionKey<SupplyServices> = Symbol('SupplyServices')
export function useSupplyServices(): SupplyServices {
  const services = inject(supplyServicesKey)
  if (!services) throw new Error('SupplyServices must be provided by the application')
  return services
}
