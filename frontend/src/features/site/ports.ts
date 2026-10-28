import { inject, type InjectionKey, type Ref, type Component } from 'vue'
import type { ModelIcon } from '@/features/catalog/modelIcon'
import type { PublicSettings } from '@/features/site/api/publicSettingsTypes';
import type { ModelPlazaResponse, ModelPlazaGroup, ModelPlazaAvailability } from '@/features/catalog'

export type SitePublicSettings = Pick<PublicSettings,
  | 'site_name' | 'site_logo' | 'site_subtitle' | 'api_base_url' | 'doc_url' | 'contact_info'
  | 'home_content' | 'compact_home_enabled' | 'model_plaza_enabled' | 'model_plaza_require_auth'
  | 'login_agreement_documents' | 'login_agreement_updated_at'>
export interface SiteSettings {
  readonly current: SitePublicSettings | null
  readonly loaded: boolean
  load(): Promise<SitePublicSettings | null>
}
export interface SiteActor {
  readonly isAuthenticated: boolean
  readonly adminLandingPath: string
  readonly user: { readonly email?: string } | null
}
export interface SiteModels {
  availability: Readonly<Ref<ModelPlazaAvailability>>
  data: Ref<ModelPlazaResponse | null>
  loading: Ref<boolean>
  loadFailed: Ref<boolean>
  load(): Promise<void>
}
export interface SitePresentation {
  ModelIcon: typeof ModelIcon
  GroupPricingPanel: Component<{ group: ModelPlazaGroup }>
  readonly brand: { name: string; logo: string; apiBaseUrl: string }
  createModelQuery(): SiteModels
  copy(text: string): Promise<boolean>
}
export const siteSettingsKey: InjectionKey<SiteSettings> = Symbol('site.settings')
export const siteActorKey: InjectionKey<SiteActor> = Symbol('site.actor')
export const sitePresentationKey: InjectionKey<SitePresentation> = Symbol('site.presentation')
function required<T>(key: InjectionKey<T>): T {
  const value = inject(key)
  if (!value) throw new Error('Site port is not provided')
  return value
}
export const useSiteSettings = () => required(siteSettingsKey)
export const useSiteActor = () => required(siteActorKey)
export const useSitePresentation = () => required(sitePresentationKey)
