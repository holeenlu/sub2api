import type { UpdateSettingsRequest, SettingsDraft } from '@/features/system/api'
import { sanitizeUrl } from '@/shared/lib/url'
import type { AppearanceDraft } from './appearanceModel'
import type { NavigationDraft } from './navigationModel'
import type { AgreementsDraft } from './agreementsModel'
import { appearanceFields } from './appearanceModel'
import { navigationFields } from './navigationModel'
import { agreementsFields } from './agreementsModel'
type SitePatch = Partial<AppearanceDraft & NavigationDraft & AgreementsDraft>
export function prepareSiteSettings(patch: SitePatch, draft: Pick<SettingsDraft, keyof SitePatch>): Partial<UpdateSettingsRequest> {
  const result: SitePatch = {}
  for (const key of [...appearanceFields, ...navigationFields, ...agreementsFields]) if (key in patch) Object.assign(result, { [key]: patch[key] })
  for (const field of ['site_logo', 'doc_url', 'api_base_url'] as const) {
    const value = patch[field]
    if (value && !sanitizeUrl(value, { allowRelative: field !== 'api_base_url', allowDataUrl: field === 'site_logo' })) throw { field, messageKey: 'siteSettings.invalidUrl' }
  }
  if ('custom_menu_items' in patch) for (const item of patch.custom_menu_items || []) {
    if (item.url && !sanitizeUrl(item.url, { allowRelative: true })) throw { field: 'custom_menu_items', messageKey: 'siteSettings.invalidUrl' }
  }
  if ('login_agreement_documents' in patch || 'login_agreement_enabled' in patch) {
    const docs = draft.login_agreement_documents.map(doc => ({ ...doc, id: doc.id.trim().toLowerCase().replace(/[^a-z0-9_-]+/g, '-').replace(/^-+|-+$/g, ''), title: doc.title.trim() }))
    if (draft.login_agreement_enabled && !docs.length) throw { field: 'login_agreement_documents', messageKey: 'ui.atLeastOneDocumentIsRequiredWhenLoginAgreement' }
    if (docs.some(doc => !doc.title)) throw { field: 'login_agreement_documents', messageKey: 'ui.loginAgreementDocumentTitleCannotBeEmpty' }
    const ids = new Set<string>()
    for (const doc of docs) {
      if (!doc.id || ids.has(doc.id)) throw { field: 'login_agreement_documents', messageKey: 'ui.duplicateAgreementRoute', params: { slug: doc.id } }
      ids.add(doc.id)
    }
    if ('login_agreement_documents' in patch) result.login_agreement_documents = docs
  }
  return result
}
