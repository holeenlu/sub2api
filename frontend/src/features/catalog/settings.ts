import { defineAsyncComponent } from 'vue'
export const AvailableChannelsSettingsPanel = defineAsyncComponent(() => import('./settings/AvailableChannelsSettingsPanel.vue'))
export { projectAvailableChannels, availableChannelsFields } from './settings/availableChannelsModel'
export type { AvailableChannelsDraft } from './settings/availableChannelsModel'
export const ModelPlazaSettingsPanel = defineAsyncComponent(() => import('./settings/ModelPlazaSettingsPanel.vue'))
export { projectModelPlaza, modelPlazaFields } from './settings/modelPlazaModel'
export type { ModelPlazaDraft } from './settings/modelPlazaModel'
