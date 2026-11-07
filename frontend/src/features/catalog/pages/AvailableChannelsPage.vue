<template>
  <TablePageLayout>
    <template #filters>
      <div class="flex flex-col justify-between gap-4 lg:flex-row lg:items-start">
        <div class="flex flex-1 flex-wrap items-center gap-3">
          <div class="relative w-full sm:w-80">
            <Icon
              name="search"
              size="md"
              class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
            />
            <input
              v-model="searchQuery"
              type="text"
              :placeholder="t('availableChannels.searchPlaceholder')"
              class="input pl-10"
            />
          </div>
        </div>

        <div class="flex w-full flex-shrink-0 flex-wrap items-center justify-end gap-3 lg:w-auto">
          <button
            @click="reload()"
            :disabled="loading"
            class="btn btn-secondary"
            :title="t('common.refresh', 'Refresh')"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </div>
    </template>

    <template #table>
      <AvailableChannelsTable
        :columns="columnLabels"
        :rows="filteredChannels"
        :loading="loading"
        :user-group-rates="userGroupRates"
        :server-utc-offset="presentation.settings?.server_utc_offset"
        pricing-key-prefix="availableChannels.pricing"
        :no-pricing-label="t('availableChannels.noPricing')"
        :no-models-label="t('availableChannels.noModels')"
        :empty-label="t('availableChannels.empty')"
      />
    </template>
  </TablePageLayout>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import TablePageLayout from '@/shared/ui/TablePageLayout.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import AvailableChannelsTable from '../components/AvailableChannelsTable.vue'
import { useAvailableChannelsQuery } from '../queries/availableChannels'
import { filterAvailableChannels } from '../model/availableChannels'
import { catalogNotificationsKey, catalogPresentationKey, catalogScopeKey, requireCatalogPort } from '../ports'

const { t } = useI18n()
const presentation = requireCatalogPort(catalogPresentationKey)
const notifications = requireCatalogPort(catalogNotificationsKey)
const scope = requireCatalogPort(catalogScopeKey)
const searchQuery = ref('')
const { channels, userGroupRates, loading, error, reload } = useAvailableChannelsQuery()
const columnLabels = computed(() => ({
  name: t('availableChannels.columns.name'),
  description: t('availableChannels.columns.description'),
  platform: t('availableChannels.columns.platform'),
  groups: t('availableChannels.columns.groups'),
  supportedModels: t('availableChannels.columns.supportedModels'),
}))
const filteredChannels = computed(() => filterAvailableChannels(channels.value, searchQuery.value))
watch(scope, () => { searchQuery.value = '' }, { flush: 'sync' })
watch(error, failure => {
  if (failure !== null) notifications.showError(extractApiErrorMessage(failure, t('common.error')))
})
</script>
