import { defineAsyncComponent } from 'vue'
export const ErrorVisibilitySettingsPanel = defineAsyncComponent(() => import('./settings/ErrorVisibilitySettingsPanel.vue'))
export { projectErrorVisibility, errorVisibilityFields } from './settings/errorVisibilityModel'
export type { ErrorVisibilityDraft } from './settings/errorVisibilityModel'
export const ChannelMonitorSettingsPanel = defineAsyncComponent(() => import('./settings/ChannelMonitorSettingsPanel.vue'))
export { projectChannelMonitor, channelMonitorFields } from './settings/channelMonitorModel'
export type { ChannelMonitorDraft } from './settings/channelMonitorModel'
