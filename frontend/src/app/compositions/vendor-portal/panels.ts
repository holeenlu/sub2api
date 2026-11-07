import { inject, type Component, type InjectionKey } from 'vue'
/** Identity owns these concrete security cards; app provides their public exports once ready. */
export interface VendorPortalSecurityPanels {
  password: Component<{ embedded?: boolean }>
  totp: Component
  passkey: Component<{ enabled?: boolean }>
}
export const vendorPortalSecurityPanelsKey: InjectionKey<VendorPortalSecurityPanels> = Symbol('VendorPortalSecurityPanels')
export function useVendorPortalSecurityPanels(): VendorPortalSecurityPanels {
  const panels = inject(vendorPortalSecurityPanelsKey)
  if (!panels) throw new Error('Vendor portal identity security panels must be provided by app')
  return panels
}

// Use the resource owner's actual public component contracts. Vue's Component<P>
// constructor alternative describes a public instance, not a contravariant props input.
// Supplier DTOs remain distinct and are checked at the real component call sites.
import type { EditAccountModal, BulkEditAccountModal } from '@/features/resources/panels'
export interface VendorPortalResourcePanels {
  editAccount: typeof EditAccountModal
  bulkEditAccounts: typeof BulkEditAccountModal
}
export const vendorPortalResourcePanelsKey: InjectionKey<VendorPortalResourcePanels> = Symbol('VendorPortalResourcePanels')
export function useVendorPortalResourcePanels(): VendorPortalResourcePanels | undefined {
  return inject(vendorPortalResourcePanelsKey, undefined)
}
