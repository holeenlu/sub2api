<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformDisplayName } from '@/constants/platformCatalog'
import { platformBadgeLightClass } from '@/utils/platformColors'
import Pagination from '@/components/common/Pagination.vue'
import DataTable from '@/components/common/DataTable.vue'
import Icon from '@/components/icons/Icon.vue'
import { vendorManagementAPI } from '../api'
import { useVendorPage } from '../useVendorPage'
import { display, errorText } from '../helpers'
import type { Vendor } from '../types'
import RequestState from './RequestState.vue'
import VendorEditor from './VendorEditor.vue'
import IdentityReconciliation from './IdentityReconciliation.vue'
const props = defineProps<{ parent?: Vendor }>()
const api = computed(() => vendorManagementAPI(props.parent))
const emit = defineEmits<{
    changed: []
    navigate: [
      tab: 'accounts' | 'proxies' | 'usage' | 'settlements',
      vendorId: number,
    ]
  }>(),
  { t } = useI18n()
const columns = computed(() =>
  [
    'name',
    'status',
    'supplierTier',
    'platform',
    'groups',
    'accounts',
    'proxies',
    'contact',
    'binanceId',
    'actions',
  ].map((key) => ({
    key,
    label: t('vendor.' + key),
  })),
)
const filters = reactive({ search: '', status: '' }),
  applied = ref({ ...filters }),
  editing = ref<Vendor>(),
  showForm = ref(false),
  errorAction = ref('')
const list = useVendorPage<Vendor>((q, s) => api.value.list(q, s)),
  { items, total, page, pageSize, loading, error } = list
const activeCount = computed(() => items.value.filter((item) => item.status === 'active').length)
const accountCount = computed(() => items.value.reduce((sum, item) => sum + (item.account_count || 0), 0))
const proxyCount = computed(() => items.value.reduce((sum, item) => sum + (item.proxy_count || 0), 0))
function load(reset = false) {
  if (reset) applied.value = { ...filters }
  return list.load(applied.value, reset)
}
async function edit(v: Vendor) {
  try {
    editing.value = await api.value.detail(v.id)
    showForm.value = true
  } catch (e) {
    errorAction.value = errorText(e)
  }
}
function created() {
  showForm.value = false
  load()
  emit('changed')
}
onMounted(() => load())
</script>
<template>
  <section class="space-y-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <p class="text-xs font-semibold uppercase tracking-[0.16em] text-primary-600 dark:text-primary-400">{{ t('vendor.supplierManagement') }}</p>
        <h2 class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ t('vendor.suppliers') }}</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('vendor.supplierManagementHelp') }}</p>
      </div>
      <button
        class="btn btn-primary shrink-0"
        data-testid="vendor-create"
        @click="editing = undefined; showForm = true"
      >
        <Icon name="plus" size="sm" aria-hidden="true" />{{ t('vendor.create') }}
      </button>
    </div>
    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <div class="rounded-xl border border-gray-200 bg-white px-4 py-3 shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('vendor.totalSuppliers') }}</p>
        <p class="mt-1 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ total }}</p>
        <p class="mt-1 text-xs text-gray-400">{{ t('vendor.currentList') }}</p>
      </div>
      <div class="rounded-xl border border-gray-200 bg-white px-4 py-3 shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('vendor.activeSuppliers') }}</p>
        <p class="mt-1 text-2xl font-semibold tabular-nums text-emerald-600 dark:text-emerald-400">{{ activeCount }}</p>
        <p class="mt-1 text-xs text-gray-400">{{ t('vendor.currentPage') }}</p>
      </div>
      <div class="rounded-xl border border-gray-200 bg-white px-4 py-3 shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('vendor.accounts') }}</p>
        <p class="mt-1 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ accountCount }}</p>
        <p class="mt-1 text-xs text-gray-400">{{ t('vendor.currentPage') }}</p>
      </div>
      <div class="rounded-xl border border-gray-200 bg-white px-4 py-3 shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('vendor.proxies') }}</p>
        <p class="mt-1 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ proxyCount }}</p>
        <p class="mt-1 text-xs text-gray-400">{{ t('vendor.currentPage') }}</p>
      </div>
    </div>
    <div
      class="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800"
    >
      <form
        class="grid flex-1 items-center gap-2 sm:grid-cols-[minmax(160px,1fr)_160px_auto]"
        @submit.prevent="load(true)"
      >
        <input
          v-model="filters.search"
          :aria-label="t('vendor.search')"
          :placeholder="t('vendor.search')"
          class="input"
        /><Select
          v-model="filters.status"
          :options="[
            { value: '', label: t('vendor.all') },
            ...['active', 'disabled'].map((value) => ({
              value,
              label: t('vendor.' + value),
            })),
          ]"
          :aria-label="t('vendor.status')"
          class="min-w-0"
        /><button class="btn btn-secondary">
          <Icon name="search" size="sm" aria-hidden="true" />{{
            t('vendor.search')
          }}
        </button>
      </form>
    </div>
    <p v-if="errorAction" role="alert" class="text-red-600">
      {{ errorAction }}
    </p>
    <RequestState
      :loading="loading"
      :error="error"
      :empty="!items.length"
      @retry="load()"
    >
      <DataTable
        :columns="columns"
        :data="items"
        row-key="id"
        :expandable-actions="false"
      >
        <template #cell-name="{ row }">
          <div class="min-w-40">
            <button
              class="font-semibold text-primary-600 hover:text-primary-700 dark:text-primary-400"
              @click="edit(row)"
            >
              {{ row.name }}
            </button>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              #{{ row.id }} · {{ t('vendor.userId') }} #{{ row.user_id }}
            </p>
          </div>
        </template>
        <template #cell-supplierTier="{ row }"><span class="badge badge-gray">{{ t(row.tier === 2 ? 'vendor.tier2' : 'vendor.tier1') }}</span><p v-if="row.parent_vendor_id" class="mt-1 text-xs text-gray-500">{{ t('vendor.parentVendor') }} #{{ row.parent_vendor_id }}</p></template>
        <template #cell-status="{ row }">
          <StatusBadge
            :status="row.status"
            :label="t('vendor.' + row.status)"
          />
        </template>
        <template #cell-platform="{ row }">
          <div class="flex flex-wrap gap-1">
            <span
              v-for="platform in row.platforms"
              :key="platform"
              class="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium"
              :class="platformBadgeLightClass(platform)"
            >
              <PlatformIcon :platform="platform" size="xs" />{{
                platformDisplayName(platform)
              }}
            </span>
          </div>
        </template>
        <template #cell-groups="{ row }">
          <div class="flex max-w-56 flex-wrap gap-1">
            <span
              v-for="group in row.groups"
              :key="group.id"
              class="badge badge-gray"
              >{{ group.name }}</span
            >
            <span v-if="!row.groups?.length" class="text-xs text-gray-500">{{
              t('vendor.noGroups')
            }}</span>
          </div>
        </template>
        <template #cell-accounts="{ row }">
          <button
            class="whitespace-nowrap text-left tabular-nums hover:text-primary-600"
            :aria-label="t('vendor.accounts') + ' · ' + row.name"
            @click="emit('navigate', 'accounts', row.id)"
          >
            <span
              class="font-semibold"
              :class="
                (row.account_tree_count ?? row.account_count ?? 0) >= row.account_limit
                  ? 'text-amber-600 dark:text-amber-400'
                  : ''
              "
              >{{ display(row.account_count) }}</span
            >
            <span v-if="row.tier !== 1" class="text-gray-500">
              / {{ display(row.account_limit) }}</span
            >
            <p class="mt-1 text-xs text-gray-500">
              {{ row.tier === 1 ? t('vendor.resourceTreeUsage', { used: display(row.account_tree_count), limit: display(row.account_limit) }) : t('vendor.accountLimit') }}
            </p>
          </button>
        </template>
        <template #cell-proxies="{ row }">
          <button
            class="whitespace-nowrap text-left tabular-nums hover:text-primary-600"
            :aria-label="t('vendor.proxies') + ' · ' + row.name"
            @click="emit('navigate', 'proxies', row.id)"
          >
            <span
              class="font-semibold"
              :class="
                (row.proxy_tree_count ?? row.proxy_count ?? 0) >= row.proxy_limit
                  ? 'text-amber-600 dark:text-amber-400'
                  : ''
              "
              >{{ display(row.proxy_count) }}</span
            >
            <span v-if="row.tier !== 1" class="text-gray-500"> / {{ display(row.proxy_limit) }}</span>
            <p class="mt-1 text-xs text-gray-500">
              {{ row.tier === 1 ? t('vendor.resourceTreeUsage', { used: display(row.proxy_tree_count), limit: display(row.proxy_limit) }) : t('vendor.proxyLimit') }}
            </p>
          </button>
        </template>
        <template #cell-contact="{ row }">
          <div class="max-w-64 whitespace-normal">
            <p class="font-medium">{{ display(row.contact_name) }}</p>
            <p
              v-if="row.settlement_email"
              class="mt-1 break-all text-xs text-gray-500"
              :title="t('vendor.settlementEmail')"
            >
              {{ row.settlement_email }}
            </p>
          </div>
        </template>
        <template #cell-binanceId="{ row }">
          <span class="font-mono text-xs">{{
            row.binance_id ? '••••' + row.binance_id.slice(-4) : '—'
          }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="flex flex-wrap items-center gap-2">
            <button class="btn btn-secondary btn-sm" @click="edit(row)">
              <Icon name="edit" size="sm" aria-hidden="true" />{{
                t('vendor.details')
              }}
              / {{ t('vendor.edit') }}
            </button>
            <button
              class="btn btn-secondary btn-sm"
              @click="emit('navigate', 'usage', row.id)"
            >
              <Icon name="chartBar" size="sm" />{{ t('vendor.usage') }}
            </button>
            <button
              class="btn btn-secondary btn-sm"
              @click="emit('navigate', 'settlements', row.id)"
            >
              <Icon name="dollar" size="sm" />{{ t('vendor.settlements') }}
            </button>
          </div>
        </template>
      </DataTable> </RequestState
    ><Pagination
      v-if="total"
      :page="page"
      :page-size="pageSize"
      :total="total"
      @update:page="
        page = $event
        load();
      "
      @update:page-size="
        pageSize = $event
        page = 1
        load();
      "
    />
    <details v-if="!parent"
      class="rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800"
    >
      <summary
        class="cursor-pointer px-4 py-3 text-sm font-medium text-gray-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-gray-300"
      >
        <span class="ml-1 inline-flex items-center gap-2"
          ><Icon name="shield" size="sm" aria-hidden="true" />{{
            t('vendor.reconcileTitle')
          }}</span
        >
      </summary>
      <div class="border-t border-gray-200 p-4 dark:border-dark-700">
        <IdentityReconciliation v-if="!parent" />
      </div>
    </details>
  </section>
  <VendorEditor
    v-if="showForm"
    :vendor="editing" :parent="parent"
    @close="showForm = false"
    @saved="created"
  />
</template>
