import { defineAsyncComponent } from 'vue'
export const SettingsWorkspace = defineAsyncComponent(() => import('./SettingsWorkspace.vue'))
export { settingsWorkspaceServicesKey } from './ports'
export type { SettingsWorkspaceServices } from './ports'
export { installSettingsServices } from './installServices'
