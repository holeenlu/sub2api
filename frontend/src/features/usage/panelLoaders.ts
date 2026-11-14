// Public panel loaders retain the real Vue component props, events and slots.
export const loadModelDistributionChart = (): Promise<typeof import('./components/ModelDistributionChart.vue')> => import('./components/ModelDistributionChart.vue')
export const loadEndpointDistributionChart = (): Promise<typeof import('./components/EndpointDistributionChart.vue')> => import('./components/EndpointDistributionChart.vue')
