import type { GroupBadge, GroupOptionItem } from '@/features/access-policy/presentation'
import { inject, type InjectionKey, type Ref } from 'vue'
import type { CustomEndpoint } from '@/features/site/api/publicSettingsTypes';
import type { Group } from '@/features/access-policy/api/groupTypes';

export type KeysGroup = Pick<Group, 'id' | 'name' | 'description' | 'platform' | 'subscription_type' |
  'rate_multiplier' | 'peak_rate_enabled' | 'peak_start' | 'peak_end' | 'peak_rate_multiplier'>
export interface KeysPublicSettings { api_base_url?: string; custom_endpoints?: CustomEndpoint[] }
export interface KeyUsageSummary { api_key_id: number; today_actual_cost?: number | null; total_actual_cost?: number | null }
export interface KeyModelsManifest { content: string; modelCount: number; responseBytes: number }

/** App supplies reactive identity epoch + portal + the effective business scope.
 * Include policy revision only when the existing server projection exposes it.
 * null means no authenticated scope. Never use the access token as this key.
 * Providers forward the signal; the feature also rejects ignored/late responses.
 */
export interface KeysServices {
  readonly components: { GroupBadge: typeof GroupBadge; GroupOptionItem: typeof GroupOptionItem }
  scopeKey: Readonly<Ref<string | null>>
  publicSettings: Readonly<Ref<KeysPublicSettings | null>>
  getAvailableGroups(options: { signal: AbortSignal }): Promise<KeysGroup[]>
  getUserGroupRates(options: { signal: AbortSignal }): Promise<Record<number, number>>
  getKeyUsage(ids: number[], options: { signal: AbortSignal }): Promise<{ stats: Record<string, KeyUsageSummary> }>
  notifySuccess(message: string): void
  notifyError(message: string): void
  copyText(text: string, successMessage?: string): Promise<boolean>
  /** App retains the existing tour-step predicate and 500ms advance. */
  onKeyCreated(): void
  /** Existing public-key Codex catalog adapter; never the management JWT. */
  codexModels: {
    buildCatalogUrl(baseUrl: string): string
    fetchManifest(baseUrl: string, apiKey: string, signal?: AbortSignal): Promise<KeyModelsManifest>
  }
}
export const keysServicesKey: InjectionKey<KeysServices> = Symbol('KeysServices')
export function useKeysServices(): KeysServices {
  const services = inject(keysServicesKey)
  if (!services) throw new Error('KeysServices must be provided by the application')
  return services
}
