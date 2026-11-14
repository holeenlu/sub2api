<template>
  <!-- 窄屏两列网格铺满宽度，sm 起恢复单行流式排布 -->
  <div class="grid w-full grid-cols-2 gap-3 sm:flex sm:w-auto sm:flex-wrap sm:items-center">
    <SearchInput
      :model-value="searchQuery"
      :placeholder="t('admin.accounts.searchAccounts')"
      class="col-span-2 sm:w-52"
      @update:model-value="$emit('update:searchQuery', $event)"
      @search="$emit('change')"
    />
    <Select :model-value="filters.platform" class="min-w-0 sm:w-40" :options="pOpts" @update:model-value="updatePlatform" @change="$emit('change')" />
    <Select :model-value="filters.type" class="min-w-0 sm:w-40" :options="tOpts" @update:model-value="updateType" @change="$emit('change')" />
    <Select :model-value="filters.status" class="min-w-0 sm:w-40" :options="sOpts" @update:model-value="updateStatus" @change="$emit('change')" />
    <Select :model-value="filters.privacy_mode" class="min-w-0 sm:w-48" :options="privacyOpts" @update:model-value="updatePrivacyMode" @change="$emit('change')" />
    <Select :model-value="filters.group" class="col-span-2 min-w-0 sm:w-40" :options="gOpts" @update:model-value="updateGroup" @change="$emit('change')" />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/shared/ui/Select.vue'
import type { SelectOption } from '@/shared/ui/Select.vue'
import SearchInput from '@/shared/ui/SearchInput.vue'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes'
import type { AccountTableFilterValues } from '../model/accountTableFilters'
import { useResourceEditorServices } from '../services'

const props = defineProps<{
  searchQuery: string
  filters: Readonly<AccountTableFilterValues>
  groups?: readonly Pick<AdminGroup, 'id' | 'name'>[]
}>()
const emit = defineEmits<{
  'update:searchQuery': [value: string]
  'update:filters': [value: AccountTableFilterValues]
  change: []
}>()
const { t } = useI18n()

// Select is shared with numeric/boolean controls. This list's text filters must
// not turn those unrelated values into query strings or boolean flags.
const updateTextFilter = (field: keyof AccountTableFilterValues, value: SelectOption['value']): void => {
  if (value === null || typeof value === 'string') {
    emit('update:filters', { ...props.filters, [field]: value })
  }
}
const updatePlatform = (value: SelectOption['value']): void => { updateTextFilter('platform', value) }
const updateType = (value: SelectOption['value']): void => { updateTextFilter('type', value) }
const updateStatus = (value: SelectOption['value']): void => { updateTextFilter('status', value) }
const updatePrivacyMode = (value: SelectOption['value']): void => { updateTextFilter('privacy_mode', value) }
const updateGroup = (value: SelectOption['value']): void => { updateTextFilter('group', value) }
const services = useResourceEditorServices()
const platformOptions = computed(() => services.catalog.listPlatforms().map(spec => ({ value: spec.id, label: spec.display_name })))
const pOpts = computed(() => [{ value: '', label: t('admin.accounts.allPlatforms') }, ...platformOptions.value])
const tOpts = computed(() => [{ value: '', label: t('admin.accounts.allTypes') }, { value: 'oauth', label: t('admin.accounts.oauthType') }, { value: 'setup-token', label: t('admin.accounts.setupToken') }, { value: 'apikey', label: t('admin.accounts.apiKey') }, { value: 'bedrock', label: 'AWS Bedrock' }])
const sOpts = computed(() => [{ value: '', label: t('admin.accounts.allStatus') }, { value: 'active', label: t('admin.accounts.status.active') }, { value: 'inactive', label: t('admin.accounts.status.inactive') }, { value: 'error', label: t('admin.accounts.status.error') }, { value: 'rate_limited', label: t('admin.accounts.status.rateLimited') }, { value: 'temp_unschedulable', label: t('admin.accounts.status.tempUnschedulable') }, { value: 'unschedulable', label: t('admin.accounts.status.unschedulable') }])
const privacyOpts = computed(() => [
  { value: '', label: t('admin.accounts.allPrivacyModes') },
  { value: '__unset__', label: t('admin.accounts.privacyUnset') },
  { value: 'training_off', label: 'Privacy' },
  { value: 'training_set_cf_blocked', label: 'CF' },
  { value: 'training_set_failed', label: 'Fail' }
])
const gOpts = computed(() => [
  { value: '', label: t('admin.accounts.allGroups') },
  { value: 'ungrouped', label: t('admin.accounts.ungroupedGroup') },
  ...(props.groups || []).map(g => ({ value: String(g.id), label: g.name }))
])
</script>
