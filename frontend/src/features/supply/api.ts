export interface SupplyRequestOptions { signal?: AbortSignal }
import { scopeQuery, mutationScope } from './scope'
import { apiClient } from '@/shared/http'
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
export function resourceAPI(scopeInput: ResourceScope) {
  const scope = Object.freeze({ ...scopeInput })
  const root = scope.admin ? '/admin' : '/vendor'
  const config = () => {
    if (scope.readOnly) throw new Error('Supplier resources are read-only')
    return { params: mutationScope(scope), timeout: 90000 }
  }
  return {
    accounts: (q: Query, signal?: AbortSignal) =>
      get<AccountPage>(`${root}/accounts`, scopeQuery(scope, q), signal),
    account: (id: number, options: SupplyRequestOptions = {}) =>
      get<VendorAccount>(`${root}/accounts/${id}`, scopeQuery(scope), options.signal),
    updateAccount: async (id: number, body: unknown, options: SupplyRequestOptions = {}) =>
      (
        await apiClient.request<VendorAccount>({
          signal: options.signal,
          method: scope.admin ? 'PUT' : 'PATCH',
          url: `${root}/accounts/${id}`,
          data: body,
          ...config(),
        })
      ).data,
    createAccount: (body: unknown, options: SupplyRequestOptions = {}) =>
      post<VendorAccount>(`${root}/accounts`, body, { ...config(), signal: options.signal }),
    accountAction: (
      id: number,
      action:
        | 'pause'
        | 'resume'
        | 'refresh'
        | 'usage-refresh'
        | 'probe'
        | 'sync',
      authorizationMode?: OpenAIAuthorizationMode, options: SupplyRequestOptions = {}
    ) =>
      post<Record<string, unknown>>(
        `${root}/accounts/${id}/${action}`,
        {},
        {
          signal: options.signal,
          ...config(),
          params: { ...mutationScope(scope), ...(action === 'refresh' && authorizationMode ? { authorization_mode: authorizationMode } : {}) },
        },
      ),
    deleteAccount: (id: number, options: SupplyRequestOptions = {}) =>
      apiClient.delete(`${root}/accounts/${id}`, { ...config(), signal: options.signal }),
    revokeAuthorization: (id: number, mode: OpenAIAuthorizationMode, options: SupplyRequestOptions = {}) =>
      apiClient.delete(`${root}/accounts/${id}/authorizations/${mode}`, { ...config(), signal: options.signal }),
    proxies: (q: Query, signal?: AbortSignal) =>
      get<ProxyPage>(`${root}/proxies`, scopeQuery(scope, q), signal),
    proxy: (id: number, options: SupplyRequestOptions = {}) =>
      get<VendorProxy>(`${root}/proxies/${id}`, scopeQuery(scope), options.signal),
    createProxy: (body: unknown, idempotencyKey?: string, options: SupplyRequestOptions = {}) =>
      post<VendorProxy>(`${root}/proxies`, body, {
        signal: options.signal,
        ...config(),
        headers: idempotencyKey
          ? { 'Idempotency-Key': idempotencyKey }
          : undefined,
      }),
    updateProxy: async (id: number, body: unknown, idempotencyKey?: string, options: SupplyRequestOptions = {}) =>
      (
        await apiClient.request<VendorProxy>({
          signal: options.signal,
          method: scope.admin ? 'PUT' : 'PATCH',
          url: `${root}/proxies/${id}`,
          headers: idempotencyKey
            ? { 'Idempotency-Key': idempotencyKey }
            : undefined,
          data: body,
          ...config(),
        })
      ).data,
    deleteProxies: (ids: number[], options: SupplyRequestOptions = {}) =>
      post<ImportResult>(`${root}/proxies/batch-delete`, { ids }, { ...config(), signal: options.signal }),
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
    proxyImport: (body: unknown, preview: boolean, idempotencyKey?: string, options: SupplyRequestOptions = {}) =>
      post<ImportResult>(
        `${root}/proxies/import${preview ? '/preview' : ''}`,
        body,
        {
          signal: options.signal,
          ...config(),
          timeout: 90000,
          headers: idempotencyKey
            ? { 'Idempotency-Key': idempotencyKey }
            : undefined,
        },
      ),
    proxyExport: (body: unknown, options: SupplyRequestOptions = {}) =>
      post<Blob>(`${root}/proxies/export`, body, {
        signal: options.signal,
        ...config(),
        responseType: 'blob',
        timeout: 90000,
      }),
  }
}
export const vendorAPI = {
  reconcileIdentities: (body: { after_id: number; limit: number }, options: SupplyRequestOptions = {}) =>
    post<IdentityReconciliationResult>(
      '/admin/vendors/identity-reconciliation',
      body,
      { signal: options.signal, timeout: 780000 },
    ),
  me: (signal?: AbortSignal) => get<Vendor>('/vendor/me', undefined, signal),
  list: (q: Query = {}, signal?: AbortSignal) =>
    get<Page<Vendor>>('/admin/vendors', q, signal),
  detail: (id: number, options: SupplyRequestOptions = {}) => get<Vendor>(`/admin/vendors/${id}`, undefined, options.signal),
  create: (body: unknown, options: SupplyRequestOptions = {}) => post<Vendor>('/admin/vendors', body, { signal: options.signal }),
  update: async (id: number, body: unknown, options: SupplyRequestOptions = {}) =>
    (await apiClient.patch<Vendor>(`/admin/vendors/${id}`, body, { signal: options.signal })).data,
  startOAuth: (
    platform: string,
    body: unknown,
    scope: ResourceScope = { admin: false }, options: SupplyRequestOptions = {}
  ) =>
    post<OAuthLease>(
      `${scope.admin ? '/admin/vendors' : '/vendor'}/oauth/${platform}/sessions`,
      body,
      { signal: options.signal, params: mutationScope(scope) },
    ),
  completeOAuth: (
    platform: string,
    session: string,
    code: string,
    scope: ResourceScope = { admin: false }, options: SupplyRequestOptions = {}
  ) =>
    post<VendorAccount>(
      `${scope.admin ? '/admin/vendors' : '/vendor'}/oauth/${platform}/sessions/${encodeURIComponent(session)}/complete`,
      { code },
      { signal: options.signal, params: mutationScope(scope), timeout: 90000 },
    ),
  rebindOAuth: async (
    platform: string,
    session: string,
    body: unknown,
    scope: ResourceScope = { admin: false }, options: SupplyRequestOptions = {}
  ) =>
    (
      await apiClient.patch(
        `${scope.admin ? '/admin/vendors' : '/vendor'}/oauth/${platform}/sessions/${encodeURIComponent(session)}/proxy`,
        body,
        { signal: options.signal, params: mutationScope(scope) },
      )
    ).data,
  importAccounts: (
    body: unknown,
    preview: boolean,
    scope: ResourceScope = { admin: false }, options: SupplyRequestOptions = {}
  ) =>
    post<ImportResult>(
      `${scope.admin ? '/admin' : '/vendor'}/accounts/import${preview ? '/preview' : ''}`,
      body,
      { signal: options.signal, params: mutationScope(scope), timeout: 120000 },
    ),
  exportAccounts: (ids: number[], scope: ResourceScope = { admin: false }, options: SupplyRequestOptions = {}) =>
    post<Blob>(
      `${scope.admin ? '/admin' : '/vendor'}/accounts/export`,
      { ids, sensitive_confirmed: true },
      { signal: options.signal, params: mutationScope(scope), responseType: 'blob', timeout: 90000 },
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
  exportUsage: async (q: Query, options: SupplyRequestOptions = {}) =>
    (
      await apiClient.get<Blob>('/admin/vendors/usage/export', {
        params: queryParams(q),
        responseType: 'blob',
        timeout: 90000,
        signal: options.signal,
      })
    ).data,
  settlements: (admin: boolean, q: Query, signal?: AbortSignal, payer = false) =>
    get<Page<Statement>>(
      admin ? '/admin/vendors/settlements' : payer ? '/vendor/payables' : '/vendor/settlements',
      q,
      signal,
    ),
  statement: (admin: boolean, id: number, q: Query = {}, payer = false, options: SupplyRequestOptions = {}) =>
    get<Statement>(
      (admin ? '/admin/vendors/settlements' : payer ? '/vendor/payables' : '/vendor/settlements') + '/' + id,
      q, options.signal
    ),
  preview: (body: unknown, payer = false, options: SupplyRequestOptions = {}) =>
    post<{ items: SettlementPreview[] } | SettlementPreview[]>(
      payer ? '/vendor/payables/preview' : '/admin/vendors/settlements/preview',
      body, { signal: options.signal }
    ),
  drafts: (drafts: { preview_token: string; idempotency_key: string }[], payer = false, options: SupplyRequestOptions = {}) =>
    post<{ vendor_id: number; statement?: Statement; error?: string }[]>(
      payer ? '/vendor/payables' : '/admin/vendors/settlements',
      { drafts }, { signal: options.signal }
    ),
  statementAction: (id: number, action: string, body: unknown, payer = false, options: SupplyRequestOptions = {}) =>
    post<Statement>((payer ? '/vendor/payables' : '/admin/vendors/settlements') + '/' + id + '/' + action, body, { signal: options.signal }),
  exportStatement: async (admin: boolean, id: number, payer = false, options: SupplyRequestOptions = {}) =>
    (
      await apiClient.get<Blob>(
        (admin ? '/admin/vendors/settlements' : payer ? '/vendor/payables' : '/vendor/settlements') + '/' + id + '/export',
        { responseType: 'blob', timeout: 90000, signal: options.signal }
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
    detail: (id: number, options: SupplyRequestOptions = {}) => get<Vendor>('/vendor/vendors/' + id, undefined, options.signal),
    create: (body: unknown, options: SupplyRequestOptions = {}) => post<Vendor>('/vendor/vendors', body, { signal: options.signal }),
    update: async (id: number, body: unknown, options: SupplyRequestOptions = {}) => (await apiClient.patch<Vendor>('/vendor/vendors/' + id, body, { signal: options.signal })).data,
  }
}
