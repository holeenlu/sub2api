import { describe, it, expect, vi } from 'vitest'
import { effectScope, ref } from 'vue'
import { resourceAPI, vendorAPI } from '../api'
import { scopeQuery } from '../scope'
import {
  isVendorOrigin,
  safePortalRedirect,
  selectionForPage,
  proxyMetadata,
  proxyProbeOutcome,
  boundedQueue,
  resourceOwnerScope,
} from '../helpers'
import { useSupplyPage } from '../useSupplyPage'
const api = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  request: vi.fn(),
  patch: vi.fn(),
  delete: vi.fn(),
}))
vi.mock('@/shared/http', () => ({ apiClient: api }))
it('omits blank optional filters from reports and CSV while preserving zero and false', async () => {
  api.get.mockResolvedValue({ data: {} })
  const query = Object.freeze({
    start_date: '2026-10-01',
    end_date: '2026-10-15',
    vendor_id: 8,
    group_id: '',
    account_id: '',
    platform: '',
    search: '  ',
    status: '',
    watermark: 0,
    all_history: false,
    page: 1,
    page_size: 20,
    dimension: 'account',
    snapshot_at: undefined,
  })
  const expected = {
    start_date: '2026-10-01',
    end_date: '2026-10-15',
    vendor_id: 8,
    watermark: 0,
    all_history: false,
    page: 1,
    page_size: 20,
    dimension: 'account',
  }
  await vendorAPI.adminUsage(query)
  expect(api.get).toHaveBeenLastCalledWith(
    '/admin/vendors/usage',
    expect.objectContaining({ params: expected }),
  )
  await vendorAPI.exportUsage(query)
  expect(api.get).toHaveBeenLastCalledWith(
    '/admin/vendors/usage/export',
    expect.objectContaining({ params: expected, responseType: 'blob' }),
  )
  for (const kind of ['summary', 'trend', 'accounts'] as const) {
    await vendorAPI.usage(kind, {
      group_id: '',
      account_id: '',
      watermark: 0,
      all_history: false,
    })
    expect(api.get).toHaveBeenLastCalledWith(
      '/vendor/usage/' + kind,
      expect.objectContaining({ params: { watermark: 0, all_history: false } }),
    )
  }
  expect(query.group_id).toBe('')
})
it('normalizes optional blanks for other workbenches without dropping the safe aggregate scope', async () => {
  api.get.mockResolvedValue({ data: {} })
  const blank = {
    group_id: '',
    account_id: '',
    status: '',
    search: '',
    page: 1,
  }
  await resourceAPI({ admin: true, readOnly: true, vendorId: 0 }).accounts(blank)
  expect(api.get).toHaveBeenLastCalledWith(
    '/admin/accounts',
    expect.objectContaining({ params: { vendor_id: 0, page: 1 } }),
  )
  for (const kind of ['accounts', 'proxies'] as const) {
    await resourceAPI({ admin: true, vendorId: 8 })[kind](blank)
    expect(api.get).toHaveBeenLastCalledWith(
      '/admin/' + kind,
      expect.objectContaining({ params: { vendor_id: 8, page: 1 } }),
    )
  }
  await vendorAPI.settlements(true, blank)
  expect(api.get).toHaveBeenLastCalledWith(
    '/admin/vendors/settlements',
    expect.objectContaining({ params: { page: 1 } }),
  )
  await vendorAPI.accountUsage(12, blank, undefined, {
    admin: true,
    vendorId: 8,
  })
  expect(api.get).toHaveBeenLastCalledWith(
    '/admin/accounts/12/usage',
    expect.objectContaining({ params: { vendor_id: 8, page: 1 } }),
  )
})
it('reads the flat me projection and its capabilities from the native response data', async () => {
  const me = {
    id: 8,
    name: 'Supplier',
    supported_auth_methods: { openai: ['oauth', 'refresh_token'] },
    proxy_protocols: ['http', 'https', 'socks5'],
  }
  api.get.mockResolvedValueOnce({ data: me })
  expect(await vendorAPI.me()).toBe(me)
  expect(api.get).toHaveBeenLastCalledWith(
    '/vendor/me',
    expect.objectContaining({ params: undefined }),
  )
})
describe('supplier authorization and resource scope', () => {
  it('passes the requested refresh mode as a scoped query while preserving the legacy default', async () => {
    api.post.mockResolvedValue({ data: {} })
    const scoped = resourceAPI({ admin: true, vendorId: 8 })
    await scoped.accountAction(12, 'refresh', 'bps')
    expect(api.post).toHaveBeenLastCalledWith('/admin/accounts/12/refresh', {}, expect.objectContaining({ params: { vendor_id: 8, authorization_mode: 'bps' } }))
    await resourceAPI({ admin: false }).accountAction(12, 'refresh', 'native')
    expect(api.post).toHaveBeenLastCalledWith('/vendor/accounts/12/refresh', {}, expect.objectContaining({ params: { authorization_mode: 'native' } }))
    await scoped.accountAction(12, 'refresh')
    expect(api.post).toHaveBeenLastCalledWith('/admin/accounts/12/refresh', {}, expect.objectContaining({ params: { vendor_id: 8 } }))
  })
  it('revokes one grant in its owner scope and rejects parent read-only access', async () => {
    api.delete.mockResolvedValue({ data: {} })
    await resourceAPI({ admin: false }).revokeAuthorization(12, 'bps')
    expect(api.delete).toHaveBeenLastCalledWith('/vendor/accounts/12/authorizations/bps', expect.objectContaining({ params: {} }))
    await resourceAPI({ admin: true, vendorId: 8 }).revokeAuthorization(12, 'native')
    expect(api.delete).toHaveBeenLastCalledWith('/admin/accounts/12/authorizations/native', expect.objectContaining({ params: { vendor_id: 8 } }))
    for (const scope of [{ admin: false, vendorId: 8, readOnly: true }, { admin: false, children: true, readOnly: true }, { admin: true, vendorId: 0, readOnly: true }])
      expect(() => resourceAPI(scope).revokeAuthorization(12, 'bps')).toThrow('read-only')
  })
  it('uses explicit child inventory reads and narrows details to the row owner', async () => {
    const scope = { admin: false, children: true, readOnly: true }
    api.get.mockResolvedValue({ data: { items: [], total: 0 } })
    for (const kind of ['accounts', 'proxies'] as const) {
      await resourceAPI(scope)[kind]({ vendor_id: 99, scope: 'all', page: 2 })
      expect(api.get).toHaveBeenLastCalledWith('/vendor/' + kind,
        expect.objectContaining({ params: { scope: 'children', page: 2 } }))
    }
    expect(resourceOwnerScope(scope, 8)).toEqual({ admin: false, vendorId: 8, readOnly: true })
    expect(() => resourceOwnerScope(scope)).toThrow()
    expect(scopeQuery({ admin: false }, { scope: 'children', vendor_id: 8 })).toEqual({})
    for (const invalid of [{ ...scope, admin: true }, { ...scope, readOnly: false }, { ...scope, vendorId: 8 }])
      expect(() => scopeQuery(invalid)).toThrow()
    expect(() => resourceAPI(scope).createProxy({})).toThrow('read-only')
    expect(() => resourceAPI({ admin: true, readOnly: true, vendorId: 0 }).createAccount({})).toThrow('read-only')
  })
  it('matches only the exact configured origin, including scheme and port', () => {
    const settings = { vendor_portal_origin: 'https://supplier.example:8443' }
    expect(isVendorOrigin(settings, 'https://supplier.example:8443')).toBe(true)
    for (const origin of [
      'https://supplier.example',
      'http://supplier.example:8443',
      'https://supplier.example.evil:8443',
    ])
      expect(isVendorOrigin(settings, origin)).toBe(false)
    expect(
      isVendorOrigin(
        { vendor_portal_origin: 'https://supplier.example/path' },
        'https://supplier.example',
      ),
    ).toBe(false)
    expect(isVendorOrigin(undefined, 'https://supplier.example')).toBe(false)
    // Host casing, the default HTTPS port and a trailing slash do not change
    // an origin. They are accepted by the existing backend setting validator.
    for (const configured of [
      'https://SUPPLIER.example',
      'https://supplier.example:443',
      'https://supplier.example/',
    ]) {
      expect(
        isVendorOrigin(
          { vendor_portal_origin: configured },
          'https://supplier.example',
        ),
      ).toBe(true)
    }
    for (const configured of [
      'https://supplier.example/?redirect=admin',
      'https://supplier.example/#admin',
      'https://user:password@supplier.example',
    ]) {
      expect(
        isVendorOrigin(
          { vendor_portal_origin: configured },
          'https://supplier.example',
        ),
      ).toBe(false)
    }
  })
  it('accepts only supplier destinations after native login', () => {
    expect(safePortalRedirect('/vendor/usage?account=7')).toBe(
      '/vendor/usage?account=7',
    )
    for (const path of [
      'https://evil.test',
      '//evil.test',
      '/admin/vendors',
      '/vendor/../../admin',
      '/vendor\\admin',
      '/vendor/other',
      null,
    ])
      expect(safePortalRedirect(path)).toBe('/vendor/accounts')
  })
  it('rejects unspecified admin scope and cannot inherit a previous supplier filter', () => {
    expect(() => scopeQuery({ admin: true })).toThrow()
    expect(
      scopeQuery({ admin: true, vendorId: 8 }, { vendor_id: 4, search: 'x' }),
    ).toEqual({ vendor_id: 8, search: 'x' })
    expect(scopeQuery({ admin: false }, { vendor_id: 4, page: 2 })).toEqual({
      page: 2,
    })
  })
  it('applies admin vendor scope to writes, tests and list requests', async () => {
    api.get.mockResolvedValue({ data: { items: [], total: 0 } })
    api.request.mockResolvedValue({ data: { id: 1 } })
    api.post.mockResolvedValue({ data: {} })
    const scoped = resourceAPI({ admin: true, vendorId: 18 })
    await scoped.proxies({ page: 2, vendor_id: 99 })
    expect(api.get).toHaveBeenLastCalledWith(
      '/admin/proxies',
      expect.objectContaining({ params: { page: 2, vendor_id: 18 } }),
    )
    await scoped.updateProxy(7, {
      config_version: 2,
      credential_action: 'preserve',
    })
    expect(api.request).toHaveBeenLastCalledWith(
      expect.objectContaining({
        method: 'PUT',
        params: { vendor_id: 18 },
        data: { config_version: 2, credential_action: 'preserve' },
      }),
    )
    await scoped.testProxy(7, 2, false)
    expect(api.post).toHaveBeenLastCalledWith(
      '/admin/proxies/7/test',
      { config_version: 2 },
      expect.objectContaining({ params: { vendor_id: 18 } }),
    )
  })
  it('keeps cross-page selection without adding unselected IDs', () => {
    let selected = selectionForPage(new Set(), [1, 2], true)
    selected = selectionForPage(selected, [3, 4], true)
    selected = selectionForPage(selected, [1, 2], false)
    expect([...selected]).toEqual([3, 4])
  })
  it('drops stale responses and selection when supplier/filter scope changes', async () => {
    let first!: (v: any) => void
    let second!: (v: any) => void
    const fetcher = vi
      .fn()
      .mockImplementationOnce(() => new Promise((r) => (first = r)))
      .mockImplementationOnce(() => new Promise((r) => (second = r)))
    const scope = effectScope()
    const page = scope.run(() => useSupplyPage<{ id: number }>(fetcher, ref('session:vendor')))!
    page.selected.value = new Set([8])
    const old = page.load({ vendor_id: 1 })
    const next = page.load({ vendor_id: 2 }, true)
    second({ items: [{ id: 22 }], total: 1, page: 1, page_size: 20 })
    await next
    first({ items: [{ id: 11 }], total: 1, page: 1, page_size: 20 })
    await old
    expect(page.items.value).toEqual([{ id: 22 }])
    expect(page.selected.value.size).toBe(0)
    expect(fetcher.mock.calls[0][1].aborted).toBe(true)
    scope.stop()
  })
  it('whitelists proxy metadata and never serializes authentication or ownership', () => {
    const metadata = proxyMetadata({
      id: 1,
      name: 'proxy',
      host: 'example.test',
      port: 8080,
      protocol: 'http',
      status: 'active',
      config_version: 3,
      username: 'secret-user',
      password: 'secret-pass',
      credentials: { token: 'secret' },
      vendor_id: 99,
    } as any)
    expect(JSON.stringify(metadata)).not.toMatch(
      /secret|password|username|credentials|vendor_id|config_version/,
    )
  })
  it('bounds manual probes to two and stops dequeuing immediately on cancellation', async () => {
    const controller = new AbortController(),
      started: number[] = [],
      release: (() => void)[] = []
    const queue = boundedQueue([1, 2, 3, 4], controller.signal, async (id) => {
      started.push(id)
      await new Promise<void>((resolve) => release.push(resolve))
    })
    expect(started).toEqual([1, 2])
    controller.abort()
    release.forEach((r) => r())
    await queue
    expect(started).toEqual([1, 2])
  })
})

describe('native manual probe outcomes', () => {
  it('does not convert a failed or skipped native result into success', () => {
    expect(
      proxyProbeOutcome({
        status: 'failed',
        test: { success: false, message: 'Connection failed' },
      }).status,
    ).toBe('failed')
    expect(proxyProbeOutcome({ status: 'skipped' }).status).toBe('skipped')
    expect(proxyProbeOutcome({}).status).toBe('failed')
  })
  it('retains per-target quality results as a snapshot', () => {
    const quality = {
      grade: 'C',
      score: 61,
      checked_at: 123,
      items: [{ target: 'OpenAI', status: 'challenge' }],
    }
    expect(proxyProbeOutcome({ status: 'success', quality }).report).toEqual(
      quality,
    )
  })
})

describe('superadmin supplier native route contracts', () => {
  it('uses scoped admin OAuth sessions without impersonating the supplier', async () => {
    const scope = { admin: true, vendorId: 8 }
    api.post.mockResolvedValue({ data: { session_id: 'lease' } })
    api.patch.mockResolvedValue({ data: {} })
    await vendorAPI.startOAuth('openai', { name: 'Account' }, scope)
    expect(api.post).toHaveBeenLastCalledWith(
      '/admin/vendors/oauth/openai/sessions',
      { name: 'Account' },
      { params: { vendor_id: 8 } },
    )
    await vendorAPI.rebindOAuth('openai', 'lease/1', { proxy_id: 4 }, scope)
    expect(api.patch).toHaveBeenLastCalledWith(
      '/admin/vendors/oauth/openai/sessions/lease%2F1/proxy',
      { proxy_id: 4 },
      { params: { vendor_id: 8 } },
    )
    await vendorAPI.completeOAuth(
      'openai',
      'lease/1',
      'private-callback',
      scope,
    )
    expect(api.post).toHaveBeenLastCalledWith(
      '/admin/vendors/oauth/openai/sessions/lease%2F1/complete',
      { code: 'private-callback' },
      expect.objectContaining({ params: { vendor_id: 8 } }),
    )
  })
  it('keeps admin import receipts, sensitive exports and account history in their exact supplier scope', async () => {
    const scope = { admin: true, vendorId: 8 }
    api.post.mockResolvedValue({ data: {} })
    api.get.mockResolvedValue({ data: {} })
    await vendorAPI.importAccounts({ data: { accounts: [] } }, true, scope)
    expect(api.post).toHaveBeenLastCalledWith(
      '/admin/accounts/import/preview',
      expect.any(Object),
      expect.objectContaining({ params: { vendor_id: 8 } }),
    )
    await vendorAPI.importAccounts({ preview_id: 'receipt' }, false, scope)
    expect(api.post).toHaveBeenLastCalledWith(
      '/admin/accounts/import',
      { preview_id: 'receipt' },
      expect.objectContaining({ params: { vendor_id: 8 } }),
    )
    await vendorAPI.exportAccounts([3, 4], scope)
    expect(api.post).toHaveBeenLastCalledWith(
      '/admin/accounts/export',
      { ids: [3, 4], sensitive_confirmed: true },
      expect.objectContaining({
        params: { vendor_id: 8 },
        responseType: 'blob',
      }),
    )
    await vendorAPI.accountUsage(
      3,
      { vendor_id: 99, page: 2 },
      undefined,
      scope,
    )
    expect(api.get).toHaveBeenLastCalledWith(
      '/admin/accounts/3/usage',
      expect.objectContaining({ params: { vendor_id: 8, page: 2 } }),
    )
    await vendorAPI.audit(true, { vendor_id: 8 })
    expect(api.get).toHaveBeenLastCalledWith(
      '/admin/vendors/audit-logs',
      expect.objectContaining({ params: { vendor_id: 8 } }),
    )
  })
  it('rejects missing admin supplier scope before any OAuth or account request', async () => {
    api.post.mockClear()
    api.get.mockClear()
    const scope = { admin: true }
    expect(() => vendorAPI.startOAuth('openai', {}, scope)).toThrow()
    expect(() => vendorAPI.importAccounts({}, true, scope)).toThrow()
    expect(() => vendorAPI.exportAccounts([1], scope)).toThrow()
    expect(() => vendorAPI.accountUsage(1, {}, undefined, scope)).toThrow()
    expect(api.post).not.toHaveBeenCalled()
    expect(api.get).not.toHaveBeenCalled()
  })
})

it('explicitly selects the safe aggregate projection instead of falling through to native admin inventory', async () => {
  api.get.mockResolvedValue({ data: { items: [], total: 0 } })
  for (const kind of ['accounts', 'proxies'] as const) {
    await resourceAPI({ admin: true, readOnly: true, vendorId: 0 })[kind]({ vendor_id: 99, search: 'safe', page: 2 })
    expect(api.get).toHaveBeenLastCalledWith(
      '/admin/' + kind,
      expect.objectContaining({
        params: { vendor_id: 0, search: 'safe', page: 2 },
      }),
    )
  }
  for (const vendorId of [NaN, Infinity, 1.5])
    expect(() => scopeQuery({ admin: true, vendorId })).toThrow()
})

it('uses the child scope only for reads and blocks every resource mutation before HTTP', async () => {
  vi.clearAllMocks()
  api.get.mockResolvedValue({ data: { items: [], total: 0 } })
  const scope = { admin: false, readOnly: true, vendorId: 8 }
  const child = resourceAPI(scope)
  await child.accounts({ vendor_id: 99 })
  expect(api.get).toHaveBeenLastCalledWith('/vendor/accounts',
    expect.objectContaining({ params: { vendor_id: 8 } }))
  await child.proxies({})
  expect(api.get).toHaveBeenLastCalledWith('/vendor/proxies',
    expect.objectContaining({ params: { vendor_id: 8 } }))
  const writes = [
    () => child.createAccount({}), () => child.updateAccount(1, {}),
    () => child.deleteAccount(1), () => child.accountAction(1, 'probe'),
    () => child.createProxy({}), () => child.updateProxy(1, {}),
    () => child.deleteProxies([1]), () => child.testProxy(1, 1, false),
    () => child.proxyImport({}, true), () => child.proxyExport({}),
    () => vendorAPI.startOAuth('openai', {}, scope)
  ]
  for (const write of writes)
    await expect(Promise.resolve().then(write)).rejects.toThrow('read-only')
  expect(api.post).not.toHaveBeenCalled()
  expect(api.request).not.toHaveBeenCalled()
  expect(api.delete).not.toHaveBeenCalled()
})


it('keeps captured child inventory read-only even if the caller later mutates its scope object', async () => {
  vi.clearAllMocks()
  const scope = { admin: false, vendorId: 8, readOnly: true }
  const owned = resourceAPI(scope)
  scope.readOnly = false
  scope.vendorId = 99
  await expect(Promise.resolve().then(() => owned.createAccount({}))).rejects.toThrow('read-only')
  api.get.mockResolvedValueOnce({ data: { items: [], total: 0 } })
  await owned.accounts({ vendor_id: 99 })
  expect(api.get).toHaveBeenLastCalledWith('/vendor/accounts', expect.objectContaining({ params: { vendor_id: 8 } }))
  expect(api.post).not.toHaveBeenCalled()
})
