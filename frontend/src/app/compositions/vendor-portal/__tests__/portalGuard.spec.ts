import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createVendorPortalGuard } from '../guard'
const auth = { authenticated: true, role: 'vendor_level1', session: 'session-a', policy: 1 }
const settings = { vendor_portal_origin: 'https://supplier.test:8443', vendor_console_enabled: true }
const refreshUser = vi.fn(), refreshSupplier = vi.fn(), clearSupplier = vi.fn()
const ports = {
  settings: { loaded: () => true, load: vi.fn(), read: () => settings },
  identity: { authenticated: () => auth.authenticated, role: () => auth.role, sessionKey: () => auth.authenticated ? auth.session : null, scopeKey: () => auth.authenticated ? auth.role + ':' + auth.policy : null, refresh: refreshUser },
  supply: { refresh: refreshSupplier, clear: clearSupplier },
  supplierBuild: () => false, origin: () => 'https://supplier.test:8443',
}
const navigate = (path: string) => createVendorPortalGuard(ports)({ path, fullPath: path, query: {} })
beforeEach(() => {
  vi.clearAllMocks(); refreshUser.mockResolvedValue(undefined); refreshSupplier.mockResolvedValue({ id: 1, tier: 1 })
  auth.authenticated = true; auth.role = 'vendor_level1'; auth.session = 'session-a'; auth.policy = 1
  settings.vendor_console_enabled = true; settings.vendor_portal_origin = 'https://supplier.test:8443'
})
describe('supplier portal authorization guard', () => {
  it('refreshes identity and supplier authorization before allowing the portal', async () => {
    expect(await navigate('/vendor/accounts')).toEqual({ handled: true })
    expect(refreshUser).toHaveBeenCalledOnce(); expect(refreshSupplier).toHaveBeenCalledOnce()
    expect(refreshUser.mock.invocationCallOrder[0]).toBeLessThan(refreshSupplier.mock.invocationCallOrder[0])
  })
  it('allows superadministrators into platform without impersonating a supplier', async () => {
    auth.role = 'super_admin'
    expect(await navigate('/vendor/platform')).toEqual({ handled: true })
    expect(refreshSupplier).not.toHaveBeenCalled(); expect(clearSupplier).toHaveBeenCalled()
    expect(await navigate('/admin/vendors')).toEqual({ handled: true, redirect: { path: '/vendor/platform', query: {} } })
  })
  it.each(['user', 'admin'])('denies non-supplier role %s', async role => {
    auth.role = role
    expect(await navigate('/vendor/accounts')).toEqual({ handled: true, redirect: '/vendor-unavailable' })
  })
  it.each(['/vendor/children', '/vendor/child-accounts', '/vendor/child-proxies', '/vendor/child-usage', '/vendor/payables', '/vendor/platform'])('denies tier-2 child control at %s', async path => {
    auth.role = 'vendor_level2'
    expect(await navigate(path)).toEqual({ handled: true, redirect: '/vendor/accounts' })
  })
  it('restricts registration and admin paths on the supplier origin', async () => {
    for (const path of ['/register', '/admin/vendors', '/dashboard']) expect(await navigate(path)).toEqual({ handled: true, redirect: '/vendor/accounts' })
  })
  it('matches scheme and port, and supplier builds fail closed for a mismatch', async () => {
    settings.vendor_portal_origin = 'http://supplier.test:8443'
    expect(await navigate('/vendor/accounts')).toEqual({ handled: true, redirect: '/vendor-unavailable' })
    expect(await createVendorPortalGuard({ ...ports, supplierBuild: () => true })({ path: '/', fullPath: '/', query: {} })).toEqual({ handled: true, redirect: '/vendor-unavailable' })
    expect(refreshSupplier).not.toHaveBeenCalled()
  })
  it('denies a disabled portal, and redirects unauthenticated users to same-origin login', async () => {
    settings.vendor_console_enabled = false
    expect(await navigate('/login')).toEqual({ handled: true, redirect: '/vendor-unavailable' })
    settings.vendor_console_enabled = true; auth.authenticated = false
    expect(await navigate('/vendor/usage')).toEqual({ handled: true, redirect: { path: '/login', query: { redirect: '/vendor/usage' } } })
  })
  it('accepts a refreshed policy but rejects another policy change during supplier lookup', async () => {
    refreshUser.mockImplementationOnce(async () => { auth.policy = 2 })
    expect(await navigate('/vendor/accounts')).toEqual({ handled: true })
    refreshSupplier.mockImplementationOnce(async () => { auth.policy = 3 })
    expect(await navigate('/vendor/accounts')).toEqual({ handled: true, redirect: '/vendor-unavailable' })
  })
  it('denies a revoked supplier and a late old-session refresh', async () => {
    refreshSupplier.mockRejectedValueOnce({ status: 403 })
    expect(await navigate('/vendor/usage')).toEqual({ handled: true, redirect: '/vendor-unavailable' })
    refreshUser.mockImplementationOnce(async () => { auth.session = 'session-b' })
    expect(await navigate('/vendor/usage')).toEqual({ handled: true, redirect: '/vendor-unavailable' })
    expect(refreshSupplier).toHaveBeenCalledTimes(1)
  })
})
