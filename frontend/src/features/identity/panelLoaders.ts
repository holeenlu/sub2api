// Public panel loaders retain the real Vue component props, events and slots.
export const loadProfilePasswordForm = (): Promise<typeof import('./components/profile/ProfilePasswordForm.vue')> => import('./components/profile/ProfilePasswordForm.vue')
export const loadProfileTotpCard = (): Promise<typeof import('./components/profile/ProfileTotpCard.vue')> => import('./components/profile/ProfileTotpCard.vue')
export const loadProfilePasskeyCard = (): Promise<typeof import('./components/profile/ProfilePasskeyCard.vue')> => import('./components/profile/ProfilePasskeyCard.vue')
export const loadAdminComplianceDialog = (): Promise<typeof import('./components/AdminComplianceDialog.vue')> => import('./components/AdminComplianceDialog.vue')
