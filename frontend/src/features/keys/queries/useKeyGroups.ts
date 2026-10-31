import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useKeysServices, type KeysGroup } from '../services'
import { useKeysRequest } from './useKeysRequest'

export function useKeyGroups() {
  const services = useKeysServices()
  const { t } = useI18n()
  const request = useKeysRequest(services)
  const groups = ref<KeysGroup[]>([])
  const userGroupRates = ref<Record<number, number>>({})
  const groupPricingLoading = ref(false)
  async function loadGroupPricing(): Promise<void> {
    const ticket = request.begin()
    if (!ticket.isCurrent()) return
    groupPricingLoading.value = true
    try {
      const [available, rates] = await Promise.all([
        services.getAvailableGroups(ticket), services.getUserGroupRates(ticket)
      ])
      if (!ticket.isCurrent()) return
      groups.value = available
      userGroupRates.value = rates
    } catch {
      if (ticket.isCurrent()) services.notifyError(t('keys.failedToLoad'))
    } finally {
      if (ticket.isCurrent()) groupPricingLoading.value = false
    }
  }
  const groupOptions = computed(() => groups.value.map(group => ({
    value: group.id, label: group.name, description: group.description,
    rate: group.rate_multiplier, userRate: userGroupRates.value[group.id] ?? null,
    peakRateEnabled: group.peak_rate_enabled, peakStart: group.peak_start, peakEnd: group.peak_end,
    peakRateMultiplier: group.peak_rate_multiplier, subscriptionType: group.subscription_type, platform: group.platform
  })))
  watch(services.scopeKey, () => {
    groups.value = []; userGroupRates.value = {}; groupPricingLoading.value = false
    if (services.scopeKey.value !== null) void loadGroupPricing()
  }, { immediate: true, flush: 'sync' })
  return { groups, userGroupRates, groupOptions, groupPricingLoading, loadGroupPricing }
}
export type KeyGroupOption = ReturnType<typeof useKeyGroups>['groupOptions']['value'][number]
