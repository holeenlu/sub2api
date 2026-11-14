import { defineAsyncComponent } from 'vue'
export const RegistrationSettingsPanel = defineAsyncComponent(() => import('./settings/RegistrationSettingsPanel.vue'))
export { projectRegistration, registrationFields } from './settings/registrationModel'
export type { RegistrationDraft } from './settings/registrationModel'
export const CaptchaSettingsPanel = defineAsyncComponent(() => import('./settings/CaptchaSettingsPanel.vue'))
export { projectCaptcha, captchaFields } from './settings/captchaModel'
export type { CaptchaDraft } from './settings/captchaModel'
export const LinuxDoSettingsPanel = defineAsyncComponent(() => import('./settings/LinuxDoSettingsPanel.vue'))
export { projectLinuxDo, linuxDoFields } from './settings/linuxDoModel'
export type { LinuxDoDraft } from './settings/linuxDoModel'
export const EmailOAuthSettingsPanel = defineAsyncComponent(() => import('./settings/EmailOAuthSettingsPanel.vue'))
export { projectEmailOAuth, emailOAuthFields } from './settings/emailOAuthModel'
export type { EmailOAuthDraft } from './settings/emailOAuthModel'
export const WeChatSettingsPanel = defineAsyncComponent(() => import('./settings/WeChatSettingsPanel.vue'))
export { projectWeChat, weChatFields } from './settings/weChatModel'
export type { WeChatDraft } from './settings/weChatModel'
export const DingTalkSettingsPanel = defineAsyncComponent(() => import('./settings/DingTalkSettingsPanel.vue'))
export { projectDingTalk, dingTalkFields } from './settings/dingTalkModel'
export type { DingTalkDraft } from './settings/dingTalkModel'
export const OidcSettingsPanel = defineAsyncComponent(() => import('./settings/OidcSettingsPanel.vue'))
export { projectOidc, oidcFields } from './settings/oidcModel'
export type { OidcDraft } from './settings/oidcModel'
export const AuthDefaultsSettingsPanel = defineAsyncComponent(() => import('./settings/AuthDefaultsSettingsPanel.vue'))
export { projectAuthDefaults, authDefaultsFields } from './settings/authDefaultsModel'
export type { AuthDefaultsDraft } from './settings/authDefaultsModel'
export { identitySettingsServicesKey } from './settings/ports'
export type { IdentitySettingsServices } from './settings/ports'
export const AdminAccessKeysSettings = defineAsyncComponent(() => import('./settings/AdminAccessKeysSettings.vue'))
export const UserDefaultsSettingsPanel = defineAsyncComponent(() => import('./settings/UserDefaultsSettingsPanel.vue'))
export { projectUserDefaults } from './settings/userDefaultsModel'
export { prepareIdentitySettings } from './settings/preparePatch'
export { getWeChatConnectModeOptions, normalizeWeChatConnectMode, defaultWeChatConnectScopesForMode, resolveWeChatConnectModeCapabilities, deriveWeChatConnectStoredMode } from './settings/weChatConnect'
export { AUTH_SOURCE_TYPES, projectAuthSourceGrants, prepareAuthSourceGrants } from './settings/authSourceGrants'
