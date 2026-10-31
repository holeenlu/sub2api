<script setup lang="ts">
import AccountTableFilters from '../../AccountTableFilters.vue'
import type { AccountTableFilterValues } from '@/features/resources/managementPanels'
const filters: Readonly<AccountTableFilterValues> = {
  platform: 'openai', type: '', status: '', privacy_mode: null, group: '42'
}
const updateFilters = (value: AccountTableFilterValues): string | null => value.group
const wrongGroup = (value: { group: number }): number => value.group
const text = (value: string): string => value
const number = (value: number): number => value
</script>
<template>
  <AccountTableFilters :filters="filters" search-query=""
    @update:filters="updateFilters" @update:search-query="text" />
  <!-- @vue-expect-error group IDs are query strings, not numeric entity IDs -->
  <AccountTableFilters :filters="{ ...filters, group: 42 }" search-query="" />
  <!-- @vue-expect-error unrelated boolean Select fields cannot become account status -->
  <AccountTableFilters :filters="{ ...filters, status: false }" search-query="" />
  <!-- @vue-expect-error update payloads retain nullable string group IDs -->
  <AccountTableFilters :filters="filters" search-query="" @update:filters="wrongGroup" />
  <!-- @vue-expect-error search events contain text -->
  <AccountTableFilters :filters="filters" search-query="" @update:search-query="number" />
</template>
