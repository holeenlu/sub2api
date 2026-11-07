// Public panel loaders retain the real Vue component props, events and slots.
export const loadPlazaGroupSection = (): Promise<typeof import('./components/PlazaGroupSection.vue')> => import('./components/PlazaGroupSection.vue')
