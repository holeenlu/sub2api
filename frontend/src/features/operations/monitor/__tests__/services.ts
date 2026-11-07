import { config } from '@vue/test-utils'
import { ref } from 'vue'
import { GroupBadge, groupPresentationKey } from '@/features/access-policy/presentation'
import { peakRatePresentation } from '@/features/pricing'
import { operationsServicesKey, type OperationsServices } from '../../services'
import { createOperationsTestServices } from '../../__tests__/services'
import { monitorServicesKey, type MonitorServices } from '../ports'

export function installMonitorTestServices(operations: Partial<OperationsServices> = {}, monitor: Partial<MonitorServices> = {}) {
  config.global.provide[groupPresentationKey as symbol] ??= { peakRate: peakRatePresentation, serverUtcOffset: ref(undefined), isSimpleMode: ref(false), platformLabel: (platform: string) => platform }
  const services = createOperationsTestServices({ publicSettings: ref({ channel_monitor_enabled: true, channel_monitor_mode: 'v1' }), ...operations })
  const monitorServices: MonitorServices = {
    canReadAdmin: ref(true),
    displaySettings: ref({ hideThroughput: false, hideUserRanking: false, showQuota: false }),
    listLinkedAccounts: async () => [],
    getLinkedAccount: async () => { throw new Error('No fixture account') },
    listActiveKeys: async () => [], getUserGroupRates: async () => ({}),
    components: { GroupBadge, ModelTagInput: { props: ['modelValue'], emits: ['update:modelValue'], template: '<div />' }, UsageProgressBar: { template: '<div />' } },
    ...monitor,
  }
  config.global.provide = { ...config.global.provide, [operationsServicesKey as symbol]: services, [monitorServicesKey as symbol]: monitorServices }
  return { services, monitorServices }
}
