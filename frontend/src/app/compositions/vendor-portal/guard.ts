import { isVendorOrigin, safePortalRedirect } from '@/features/supply'
export interface VendorPortalLocation { path: string; fullPath: string; query: Record<string, unknown> }
export type VendorPortalDecision = { handled: false } | { handled: true; redirect?: string | { path: string; query?: Record<string, string> } }
export interface VendorPortalGuardPorts {
  settings: {
    loaded(): boolean
    load(): Promise<unknown>
    read(): { vendor_portal_origin?: string; vendor_console_enabled?: boolean } | null | undefined
  }
  identity: {
    authenticated(): boolean
    sessionKey(): string | null
    scopeKey(): string | null
    role(): string | undefined
    refresh(): Promise<unknown>
  }
  supply: { refresh(): Promise<unknown>; clear(): void }
  supplierBuild(): boolean
  origin(): string
}
/** Root router delegates this one guard; it remains the sole owner of route registration. */
export function createVendorPortalGuard(ports: VendorPortalGuardPorts) {
  return async (to: VendorPortalLocation): Promise<VendorPortalDecision> => {
    const initialSession = ports.identity.sessionKey()
    const deny = (): VendorPortalDecision => ({ handled: true, redirect: '/vendor-unavailable' })
    if (!ports.settings.loaded()) {
      try { await ports.settings.load() } catch { /* Supplier build/origin checks below still fail closed. */ }
    }
    const settings = ports.settings.read()
    const supplierOrigin = isVendorOrigin(settings, ports.origin())
    if (to.path === '/vendor-unavailable') return { handled: true }
    if (!supplierOrigin && !ports.supplierBuild()) return to.path.startsWith('/vendor/') ? deny() : { handled: false }
    if (!supplierOrigin || settings?.vendor_console_enabled !== true) return deny()
    const publicPath = ['/login', '/forgot-password', '/reset-password'].includes(to.path) || to.path.startsWith('/legal/')
    if (publicPath) return to.path === '/login' && ports.identity.authenticated()
      ? { handled: true, redirect: safePortalRedirect(to.query.redirect) } : { handled: true }
    if (initialSession !== ports.identity.sessionKey()) return deny()
    if (!ports.identity.authenticated()) return {
      handled: true, redirect: { path: '/login', query: { redirect: safePortalRedirect(to.fullPath) } },
    }
    const session = ports.identity.sessionKey()
    const current = () => session !== null && session === ports.identity.sessionKey() && ports.identity.authenticated()
    try {
      await ports.identity.refresh()
      if (!current()) return deny()
      // Accept a newly fetched policy in the same logical session, then bind
      // the supplier lookup to exactly that freshly verified authorization.
      const authorization = ports.identity.scopeKey()
      const role = ports.identity.role()
      if (role === 'super_admin') {
        ports.supply.clear()
        if (to.path !== '/vendor/platform' && to.path !== '/vendor/profile') {
          const tab = ['accounts', 'proxies', 'usage', 'settlements'].find(value => to.path === '/vendor/' + value)
          return { handled: true, redirect: { path: '/vendor/platform', query: tab ? { tab } : {} } }
        }
      } else if (role === 'vendor_level1' || role === 'vendor_level2') {
        await ports.supply.refresh()
        if (!current() || authorization === null || authorization !== ports.identity.scopeKey()) return deny()
        const level1Only = ['/vendor/children', '/vendor/child-accounts', '/vendor/child-proxies', '/vendor/child-usage', '/vendor/payables']
        if (!to.path.startsWith('/vendor/') || to.path === '/vendor/platform' || (role === 'vendor_level2' && level1Only.includes(to.path))) {
          return { handled: true, redirect: '/vendor/accounts' }
        }
      } else return deny()
    } catch { return deny() }
    return { handled: true }
  }
}
