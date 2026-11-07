import type { ResourceAccountTestRequest } from './api/editorOperations'
import type { GroupBadge } from '@/features/access-policy/presentation'
import type { ModelIcon } from '@/features/catalog/modelIcon'
import { inject, type InjectionKey, type Component } from 'vue'
import type { ModelStat, EndpointStat } from '@/features/usage/api/types';
import type * as Catalog from '@/features/catalog'
import type grokAPI from '@/features/resources/grok'
import type geminiAPI from '@/features/resources/gemini'
import type antigravityAPI from '@/features/resources/antigravity'
import type cnProvidersAPI from '@/features/resources/cnProviders'
import type * as ClaudeCredits from '@/features/resources/claudeResetCredits'

export interface ResourceManagedModel { readonly id: string; readonly enabled: boolean; readonly display_name: string }

/** App-owned projections only. scopeKey includes session epoch, portal and business scope; never a token. */
export interface ResourceEditorServices {
  readonly components: { GroupBadge: typeof GroupBadge; ModelIcon: typeof ModelIcon }
  scopeKey: () => string
  /** App supplies usage-owned read-only presentations; resource snapshots remain the data source. */
  statisticsCharts: {
    ModelDistributionChart: Component<{ modelStats: ModelStat[]; loading?: boolean }>
    EndpointDistributionChart: Component<{ endpointStats: EndpointStat[]; loading?: boolean; title?: string }>
  }
  notices: {
    showError(message: string): unknown
    showSuccess(message: string): unknown
    showInfo(message: string): unknown
    showWarning(message: string): unknown
  }
  access: {
    readonly isSuperAdmin: boolean
    readonly isSimpleMode: boolean
    readonly canAuthorize: boolean
    /** App-owned selector write permission (accounts.manage for account consumers). */
    readonly canManage: boolean
    /** Consumer-owned catalog read permission; falls back to canManage for existing bindings. */
    readonly canReadModelCatalog?: boolean
    /** Existing server-owned request field grants; independent allowlist CAS does not require credentials. */
    canEditAdminField?(schema: string, field: string): boolean
    /** Explicit app permission projection for proxy diagnostics; absent means no permission. */
    readonly canTestProxies?: boolean
  }
  catalog: Pick<typeof Catalog, 'listPlatforms' | 'getProviderProfile' | 'getProviderMode' | 'supportsUpstreamModelSync'> & {
    readonly builtin: typeof Catalog.BUILTIN_PLATFORM_CATALOG
  }
  readonly quotaNotificationEnabled: boolean
  managedModelCatalog(platform: string, options?: { signal?: AbortSignal }): Promise<readonly ResourceManagedModel[]>
  modelPresets(platform: string): { label: string; from: string; to: string; color: string }[]
  tlsProfiles(): Promise<{ id: number; name: string }[]>
  webSearchConfig(): Promise<{ enabled: boolean; providers: string[] }>
  /** Resource credential APIs, using the existing shared/http client. Never identity login OAuth. */
  credentialAuthorization: {
    grok: Pick<typeof grokAPI, 'generateAuthUrl' | 'exchangeCode' | 'refreshGrokToken' | 'validateSSOToken' | 'authorizePassword' | 'createFromSSO' | 'getCapabilities' | 'queryQuota'>
    gemini: Pick<typeof geminiAPI, 'generateAuthUrl' | 'exchangeCode' | 'getCapabilities'>
    antigravity: Pick<typeof antigravityAPI, 'generateAuthUrl' | 'exchangeCode' | 'refreshAntigravityToken'>
  }
  providerObservations: Pick<typeof cnProvidersAPI, 'queryQuota' | 'queryBalance'>
  excelAuthorization: {
    generateUrl(proxyID?: number | null): Promise<{ auth_url: string; session_id: string }>
    get(accountID: number): Promise<{ authorized: boolean; refresh_required?: boolean; source?: string }>
    save(accountID: number, credentials: Record<string, unknown>): Promise<unknown>
    revoke(accountID: number): Promise<unknown>
  }
  claudeCredits: Pick<typeof ClaudeCredits, 'getClaudeResetCredits' | 'redeemClaudeResetCredit'>
  openAccountTest(accountID: number, request: ResourceAccountTestRequest, signal: AbortSignal): Promise<Response>
}

export const resourceEditorServicesKey: InjectionKey<ResourceEditorServices> = Symbol('resource-editor-services')

export function useResourceEditorServices(): ResourceEditorServices {
  const services = inject(resourceEditorServicesKey)
  if (!services) throw new Error('Resource editor services must be provided by the application composition')
  return services
}

export { createProviderCredentialOptions } from './model/providerCredentialOptions'
