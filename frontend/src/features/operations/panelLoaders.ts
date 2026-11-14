// Public panel loaders retain the real Vue component props, events and slots.
export const loadOpsErrorLogTable = (): Promise<typeof import('./dashboard/components/OpsErrorLogTable.vue')> => import('./dashboard/components/OpsErrorLogTable.vue')
export const loadOpsErrorDetailModal = (): Promise<typeof import('./dashboard/components/OpsErrorDetailModal.vue')> => import('./dashboard/components/OpsErrorDetailModal.vue')
