import { apiClient } from '@/api/client'
import type { AxiosRequestConfig } from 'axios'
import type {
  Page,
  AccountPage,
  Vendor,
  VendorAccount,
  VendorProxy,
  ProxyPage,
  Query,
  ResourceScope,
  OAuthLease,
  ImportResult,
  UsageResponse,
  Statement,
  SettlementPreview,
  AuditEvent,
  IdentityReconciliationResult,
  OpenAIAuthorizationMode,
} from './types'

function queryParams(query?: Query): Query | undefined {
  if (!query) return query
  // Optional blank filters must be absent: numeric query parsers reject "".
  // Zero watermarks/aggregate scope and false flags are meaningful values.
  return Object.fromEntries(
    Object.entries(query).filter(
      ([, value]) =>
        value != null && !(typeof value === 'string' && value.trim() === ''),
    ),
  )
}
export async function get<T>(
  url: string,
  params?: Query,
  signal?: AbortSignal,
): Promise<T> {
  return (await apiClient.get<T>(url, { params: queryParams(params), signal }))
    .data
}
export async function post<T>(
  url: string,
  body?: unknown,
  config?: AxiosRequestConfig,
): Promise<T> {
  return (await apiClient.post<T>(url, body, config)).data
}
export function scopeQuery(scope: ResourceScope, query: Query = {}): Query {
  const { vendor_id: _discard, scope: _scope, ...rest } = query
  if (scope.children) {
    if (scope.admin || !scope.readOnly || scope.vendorId !== undefined)
      throw new Error('Invalid child inventory scope')
    return { ...rest, scope: 'children' }
  }
  if (scope.admin && scope.readOnly && scope.vendorId === 0)
    return { ...rest, vendor_id: 0 }
  if (
    (scope.admin || scope.readOnly) &&
    (!Number.isSafeInteger(scope.vendorId) ||
      !scope.vendorId ||
      scope.vendorId <= 0)
  )
    throw new Error('A supplier scope is required')
  // Scope is written last; a caller cannot accidentally retain the previous supplier.
  return scope.admin || scope.readOnly ? { ...rest, vendor_id: scope.vendorId } : rest
}
function mutationScope(scope: ResourceScope): Query {
  if (scope.readOnly) throw new Error('Supplier resources are read-only')
  return scopeQuery(scope)
}
export function resourceAPI(scope: ResourceScope) {
  const root = scope.admin ? '/admin' : '/vendor'
  const config = () => {
    if (scope.readOnly) throw new Error('Supplier resources are read-only')
    return { params: mutationScope(scope), timeout: 90000 }
  }
  return {
    accounts: (q: Query, signal?: AbortSignal) =>
      get<AccountPage>(`${root}/accounts`, scopeQuery(scope, q), signal),
    account: (id: number) =>
      get<VendorAccount>(`${root}/accounts/${id}`, scopeQuery(scope)),
    updateAccount: async (id: number, body: unknown) =>
      (
        await apiClient.request<VendorAccount>({
          method: scope.admin ? 'PUT' : 'PATCH',
          url: `${root}/accounts/${id}`,
          data: body,
          ...config(),
        })
      ).data,
    createAccount: (body: unknown) =>
      post<VendorAccount>(`${root}/accounts`, body, config()),
    accountAction: (
      id: number,
      action:
        | 'pause'
        | 'resume'
        | 'refresh'
        | 'usage-refresh'
        | 'probe'
        | 'sync',
      authorizationMode?: OpenAIAuthorizationMode,
    ) =>
      post<Record<string, unknown>>(
        `${root}/accounts/${id}/${action}`,
        {},
        {
          ...config(),
          params: { ...mutationScope(scope), ...(action === 'refresh' && authorizationMode ? { authorization_mode: authorizationMode } : {}) },
        },
      ),
    deleteAccount: (id: number) =>
      apiClient.delete(`${root}/accounts/${id}`, config()),
    revokeAuthorization: (id: number, mode: OpenAIAuthorizationMode) =>
      apiClient.delete(`${root}/accounts/${id}/authorizations/${mode}`, config()),
    proxies: (q: Query, signal?: AbortSignal) =>
      get<ProxyPage>(`${root}/proxies`, scopeQuery(scope, q), signal),
    proxy: (id: number) =>
      get<VendorProxy>(`${root}/proxies/${id}`, scopeQuery(scope)),
    createProxy: (body: unknown, idempotencyKey?: string) =>
      post<VendorProxy>(`${root}/proxies`, body, {
        ...config(),
        headers: idempotencyKey
          ? { 'Idempotency-Key': idempotencyKey }
          : undefined,
      }),
    updateProxy: async (id: number, body: unknown, idempotencyKey?: string) =>
      (
        await apiClient.request<VendorProxy>({
          method: scope.admin ? 'PUT' : 'PATCH',
          url: `${root}/proxies/${id}`,
          headers: idempotencyKey
            ? { 'Idempotency-Key': idempotencyKey }
            : undefined,
          data: body,
          ...config(),
        })
      ).data,
    deleteProxies: (ids: number[]) =>
      post<ImportResult>(`${root}/proxies/batch-delete`, { ids }, config()),
    testProxy: (
      id: number,
      version: number,
      quality: boolean,
      signal?: AbortSignal,
    ) =>
      post<Record<string, unknown>>(
        `${root}/proxies/${id}/${quality ? 'quality-check' : 'test'}`,
        { config_version: version },
        { ...config(), signal, timeout: 90000 },
      ),
    proxyImport: (body: unknown, preview: boolean, idempotencyKey?: string) =>
      post<ImportResult>(
        `${root}/proxies/import${preview ? '/preview' : ''}`,
        body,
        {
          ...config(),
          timeout: 90000,
          headers: idempotencyKey
            ? { 'Idempotency-Key': idempotencyKey }
            : undefined,
        },
      ),
    proxyExport: (body: unknown) =>
      post<Blob>(`${root}/proxies/export`, body, {
        ...config(),
        responseType: 'blob',
        timeout: 90000,
      }),
  }
}
export const vendorAPI = {
  reconcileIdentities: (body: { after_id: number; limit: number }) =>
    post<IdentityReconciliationResult>(
      '/admin/vendors/identity-reconciliation',
      body,
      { timeout: 780000 },
    ),
  me: () => get<Vendor>('/vendor/me'),
  list: (q: Query = {}, signal?: AbortSignal) =>
    get<Page<Vendor>>('/admin/vendors', q, signal),
  detail: (id: number) => get<Vendor>(`/admin/vendors/${id}`),
  create: (body: unknown) => post<Vendor>('/admin/vendors', body),
  update: async (id: number, body: unknown) =>
    (await apiClient.patch<Vendor>(`/admin/vendors/${id}`, body)).data,
  startOAuth: (
    platform: string,
    body: unknown,
    scope: ResourceScope = { admin: false },
  ) =>
    post<OAuthLease>(
      `${scope.admin ? '/admin/vendors' : '/vendor'}/oauth/${platform}/sessions`,
      body,
      { params: mutationScope(scope) },
    ),
  completeOAuth: (
    platform: string,
    session: string,
    code: string,
    scope: ResourceScope = { admin: false },
  ) =>
    post<VendorAccount>(
      `${scope.admin ? '/admin/vendors' : '/vendor'}/oauth/${platform}/sessions/${encodeURIComponent(session)}/complete`,
      { code },
      { params: mutationScope(scope), timeout: 90000 },
    ),
  rebindOAuth: async (
    platform: string,
    session: string,
    body: unknown,
    scope: ResourceScope = { admin: false },
  ) =>
    (
      await apiClient.patch(
        `${scope.admin ? '/admin/vendors' : '/vendor'}/oauth/${platform}/sessions/${encodeURIComponent(session)}/proxy`,
        body,
        { params: mutationScope(scope) },
      )
    ).data,
  importAccounts: (
    body: unknown,
    preview: boolean,
    scope: ResourceScope = { admin: false },
  ) =>
    post<ImportResult>(
      `${scope.admin ? '/admin' : '/vendor'}/accounts/import${preview ? '/preview' : ''}`,
      body,
      { params: mutationScope(scope), timeout: 120000 },
    ),
  exportAccounts: (ids: number[], scope: ResourceScope = { admin: false }) =>
    post<Blob>(
      `${scope.admin ? '/admin' : '/vendor'}/accounts/export`,
      { ids, sensitive_confirmed: true },
      { params: mutationScope(scope), responseType: 'blob', timeout: 90000 },
    ),
  usage: (
    kind: 'summary' | 'trend' | 'accounts',
    q: Query,
    signal?: AbortSignal,
  ) => get<UsageResponse>(`/vendor/usage/${kind}`, q, signal),
  adminUsage: (q: Query, signal?: AbortSignal) =>
    get<UsageResponse>('/admin/vendors/usage', q, signal),
  accountUsage: (
    id: number,
    q: Query,
    signal?: AbortSignal,
    scope: ResourceScope = { admin: false },
  ) =>
    get<UsageResponse>(
      `${scope.admin ? '/admin' : '/vendor'}/accounts/${id}/usage`,
      scopeQuery(scope, q),
      signal,
    ),
  exportUsage: async (q: Query) =>
    (
      await apiClient.get<Blob>('/admin/vendors/usage/export', {
        params: queryParams(q),
        responseType: 'blob',
        timeout: 90000,
      })
    ).data,
  settlements: (admin: boolean, q: Query, signal?: AbortSignal, payer = false) =>
    get<Page<Statement>>(
      admin ? '/admin/vendors/settlements' : payer ? '/vendor/payables' : '/vendor/settlements',
      q,
      signal,
    ),
  statement: (admin: boolean, id: number, q: Query = {}, payer = false) =>
    get<Statement>(
      (admin ? '/admin/vendors/settlements' : payer ? '/vendor/payables' : '/vendor/settlements') + '/' + id,
      q,
    ),
  preview: (body: unknown, payer = false) =>
    post<{ items: SettlementPreview[] } | SettlementPreview[]>(
      payer ? '/vendor/payables/preview' : '/admin/vendors/settlements/preview',
      body,
    ),
  drafts: (drafts: { preview_token: string; idempotency_key: string }[], payer = false) =>
    post<{ vendor_id: number; statement?: Statement; error?: string }[]>(
      payer ? '/vendor/payables' : '/admin/vendors/settlements',
      { drafts },
    ),
  statementAction: (id: number, action: string, body: unknown, payer = false) =>
    post<Statement>((payer ? '/vendor/payables' : '/admin/vendors/settlements') + '/' + id + '/' + action, body),
  exportStatement: async (admin: boolean, id: number, payer = false) =>
    (
      await apiClient.get<Blob>(
        (admin ? '/admin/vendors/settlements' : payer ? '/vendor/payables' : '/vendor/settlements') + '/' + id + '/export',
        { responseType: 'blob', timeout: 90000 },
      )
    ).data,
  audit: (admin: boolean, q: Query, signal?: AbortSignal) =>
    get<Page<AuditEvent>>(
      admin ? '/admin/vendors/audit-logs' : '/vendor/audit-logs',
      q,
      signal,
    ),
}

export function vendorManagementAPI(parent?: Vendor) {
  if (!parent) return vendorAPI
  return {
    list: (q: Query = {}, signal?: AbortSignal) => get<Page<Vendor>>('/vendor/vendors', q, signal),
    detail: (id: number) => get<Vendor>('/vendor/vendors/' + id),
    create: (body: unknown) => post<Vendor>('/vendor/vendors', body),
    update: async (id: number, body: unknown) => (await apiClient.patch<Vendor>('/vendor/vendors/' + id, body)).data,
  }
}
