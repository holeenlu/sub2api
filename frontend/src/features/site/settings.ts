import { defineAsyncComponent } from 'vue'
export const AppearanceSettingsPanel = defineAsyncComponent(() => import('./settings/AppearanceSettingsPanel.vue'))
export { projectAppearance, appearanceFields } from './settings/appearanceModel'
export type { AppearanceDraft } from './settings/appearanceModel'
export const NavigationSettingsPanel = defineAsyncComponent(() => import('./settings/NavigationSettingsPanel.vue'))
export { projectNavigation, navigationFields } from './settings/navigationModel'
export type { NavigationDraft } from './settings/navigationModel'
export const AgreementsSettingsPanel = defineAsyncComponent(() => import('./settings/AgreementsSettingsPanel.vue'))
export { projectAgreements, agreementsFields } from './settings/agreementsModel'
export type { AgreementsDraft } from './settings/agreementsModel'
export { prepareSiteSettings } from './settings/preparePatch'
