// Public panel loaders retain the real Vue component props, events and slots.
export const loadCreateAccountModal = (): Promise<typeof import('./components/CreateAccountModal.vue')> => import('./components/CreateAccountModal.vue')
export const loadEditAccountModal = (): Promise<typeof import('./components/EditAccountModal.vue')> => import('./components/EditAccountModal.vue')
export const loadBulkEditAccountModal = (): Promise<typeof import('./components/BulkEditAccountModal.vue')> => import('./components/BulkEditAccountModal.vue')
export const loadModelWhitelistSelector = (): Promise<typeof import('./components/ModelWhitelistSelector.vue')> => import('./components/ModelWhitelistSelector.vue')
export const loadProxyPicker = (): Promise<typeof import('./components/ProxyPicker.vue')> => import('./components/ProxyPicker.vue')
