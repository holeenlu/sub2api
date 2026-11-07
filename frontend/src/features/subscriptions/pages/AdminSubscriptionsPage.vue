<template>
  <div>
    <TablePageLayout>
      <template #filters>
        <!-- Top Toolbar: Left (search + filters) / Right (actions) -->
        <div class="flex flex-wrap items-start justify-between gap-4">
          <!-- Left: Fuzzy user search + filters (wrap to multiple lines) -->
          <div class="flex flex-1 flex-wrap items-center gap-3">
            <!-- User Search -->
            <div
              class="relative w-full sm:w-64"
              data-filter-user-search
            >
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
              />
              <input
                v-model="filterUserKeyword"
                type="text"
                :placeholder="t('admin.users.searchUsers')"
                class="input pl-10 pr-8"
                @input="debounceSearchFilterUsers"
                @focus="showFilterUserDropdown = true"
              />
              <button
                v-if="selectedFilterUser"
                @click="clearFilterUser"
                type="button"
                class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                :title="t('common.clear')"
              >
                <Icon name="x" size="sm" :stroke-width="2" />
              </button>

              <!-- User Dropdown -->
              <div
                v-if="showFilterUserDropdown && (filterUserResults.length > 0 || filterUserKeyword)"
                class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800"
              >
                <div
                  v-if="filterUserLoading"
                  class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400"
                >
                  {{ t('common.loading') }}
                </div>
                <div
                  v-else-if="filterUserResults.length === 0 && filterUserKeyword"
                  class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400"
                >
                  {{ t('common.noOptionsFound') }}
                </div>
                <button
                  v-for="user in filterUserResults"
                  :key="user.id"
                  type="button"
                  @click="selectFilterUser(user)"
                  class="w-full px-4 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-dark-700"
                >
                  <span class="font-medium text-gray-900 dark:text-white">{{ user.email }}</span>
                  <span class="ml-2 text-gray-500 dark:text-gray-400">#{{ user.id }}</span>
                </button>
              </div>
            </div>

            <!-- Filters -->
            <div class="w-full sm:w-40">
              <Select
                v-model="filters.status"
                :options="statusOptions"
                :placeholder="t('admin.subscriptions.allStatus')"
                @change="applyFilters"
              />
            </div>
            <div class="w-full sm:w-48">
              <Select
                v-model="filters.group_id"
                :options="groupOptions"
                :placeholder="t('admin.subscriptions.allGroups')"
                @change="applyFilters"
              />
            </div>
            <div class="w-full sm:w-40">
              <Select
                v-model="filters.platform"
                :options="platformFilterOptions"
                :placeholder="t('admin.subscriptions.allPlatforms')"
                @change="applyFilters"
              />
            </div>
          </div>

          <!-- Right: Actions -->
          <div class="ml-auto flex flex-wrap items-center justify-end gap-3">
            <button
              @click="loadSubscriptions"
              :disabled="loading"
              class="btn btn-secondary"
              :title="t('common.refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <!-- Column Settings Dropdown -->
            <div class="relative" ref="columnDropdownRef">
              <button
                @click="showColumnDropdown = !showColumnDropdown"
                class="btn btn-secondary px-2 md:px-3"
                :title="t('admin.users.columnSettings')"
              >
                <svg class="h-4 w-4 md:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M9 4.5v15m6-15v15m-10.875 0h15.75c.621 0 1.125-.504 1.125-1.125V5.625c0-.621-.504-1.125-1.125-1.125H4.125C3.504 4.5 3 5.004 3 5.625v12.75c0 .621.504 1.125 1.125 1.125z" />
                </svg>
                <span class="hidden md:inline">{{ t('admin.users.columnSettings') }}</span>
              </button>
              <!-- Dropdown menu -->
              <div
                v-if="showColumnDropdown"
                class="absolute right-0 z-50 mt-2 w-48 origin-top-right rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800"
              >
                <div class="p-2">
                  <!-- User column mode selection -->
                  <div class="mb-2 border-b border-gray-200 pb-2 dark:border-dark-700">
                    <div class="px-3 py-1 text-xs font-medium text-gray-500 dark:text-gray-400">
                      {{ t('admin.subscriptions.columns.user') }}
                    </div>
                    <button
                      @click="setUserColumnMode('email')"
                      class="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
                    >
                      <span>{{ t('admin.users.columns.email') }}</span>
                      <Icon v-if="userColumnMode === 'email'" name="check" size="sm" class="text-primary-500" />
                    </button>
                    <button
                      @click="setUserColumnMode('username')"
                      class="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
                    >
                      <span>{{ t('admin.users.columns.username') }}</span>
                      <Icon v-if="userColumnMode === 'username'" name="check" size="sm" class="text-primary-500" />
                    </button>
                  </div>
                  <!-- Other columns toggle -->
                  <button
                    v-for="col in toggleableColumns"
                    :key="col.key"
                    @click="toggleColumn(col.key)"
                    class="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
                  >
                    <span>{{ col.label }}</span>
                    <Icon v-if="isColumnVisible(col.key)" name="check" size="sm" class="text-primary-500" />
                  </button>
                </div>
              </div>
            </div>
            <button
              @click="showGuideModal = true"
              class="btn btn-secondary"
              :title="t('admin.subscriptions.guide.showGuide')"
            >
              <Icon name="questionCircle" size="md" />
            </button>
            <button v-if="canManage" @click="showAssignModal = true" class="btn btn-primary">
              <Icon name="plus" size="md" class="mr-2" />
              {{ t('admin.subscriptions.assignSubscription') }}
            </button>
          </div>
        </div>
        <div
          v-if="selectedCount > 0"
          class="mt-3 space-y-2 rounded-xl border border-primary-200 bg-primary-50 p-3 dark:border-primary-800 dark:bg-primary-900/20"
          data-test="subscription-bulk-actions"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="mr-2 text-sm font-medium text-primary-800 dark:text-primary-200">
              {{ t('admin.subscriptions.bulk.selected', { count: selectedCount }) }}
            </span>
            <button v-if="canManage"
              v-for="action in bulkActions"
              :key="action"
              type="button"
              :class="action === 'revoke' ? 'btn btn-danger btn-sm' : 'btn btn-secondary btn-sm'"
              :data-test="`bulk-${action}`"
              :disabled="loading || bulkTargets[action].length === 0"
              @click="openBulkAction(action)"
            >
              {{ t(`admin.subscriptions.bulk.${action}`) }} ({{ bulkTargets[action].length }})
            </button>
            <button type="button" class="btn btn-secondary btn-sm" @click="clearSelection">
              {{ t('admin.subscriptions.bulk.clearSelection') }}
            </button>
          </div>
          <p class="text-xs text-gray-600 dark:text-gray-400">{{ t('admin.subscriptions.bulk.selectionHint') }}</p>
        </div>
      </template>

      <!-- Subscriptions Table -->
      <template #table>
        <DataTable
          :columns="columns"
          :data="subscriptions"
          :loading="loading"
          row-key="id"
          selectable
          :selected-keys="selectedIds"
          :selection-label="getSubscriptionSelectionLabel"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
          @update:selected-keys="handleSelectedKeysUpdate"
        >
          <template #cell-user="{ row }">
            <div class="flex items-center gap-2">
              <div
                class="flex h-8 w-8 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30"
              >
                <span class="text-sm font-medium text-primary-700 dark:text-primary-300">
                  {{ userColumnMode === 'email'
                    ? (row.user?.email?.charAt(0).toUpperCase() || '?')
                    : (row.user?.username?.charAt(0).toUpperCase() || '?')
                  }}
                </span>
              </div>
              <RouterLink
                :to="navigation.usageTarget(row.user_id)"
                class="rounded font-medium text-gray-900 hover:text-primary-600 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:text-white dark:hover:text-primary-400 dark:focus-visible:ring-offset-dark-800"
              >
                {{ userColumnMode === 'email'
                  ? (row.user?.email || t('admin.users.userPrefix', { id: row.user_id }))
                  : (row.user?.username || t('admin.users.userPrefix', { id: row.user_id }))
                }}
              </RouterLink>
            </div>
          </template>

          <template #cell-group="{ row }">
            <GroupBadge
              v-if="row.group"
              :name="row.group.name"
              :platform="row.group.platform"
              :subscription-type="row.group.subscription_type"
              :rate-multiplier="row.group.rate_multiplier"
              :show-rate="false"
            />
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>

          <template #cell-usage="{ row }"><SubscriptionQuotaCell :subscription="row" /></template>

          <template #cell-expires_at="{ value }">
            <div v-if="value">
              <span
                class="text-sm"
                :class="
                  isExpiringSoon(value)
                    ? 'text-orange-600 dark:text-orange-400'
                    : 'text-gray-700 dark:text-gray-300'
                "
              >
                {{ formatDateTimeToMinute(value) }}
              </span>
              <template
                v-for="remainingExpiry in [formatRemainingExpiry(value)]"
                :key="remainingExpiry ?? 'expired'"
              >
                <div v-if="remainingExpiry" class="text-xs text-gray-500">
                  {{ remainingExpiry }}
                </div>
              </template>
            </div>
            <span v-else class="text-sm text-gray-500">{{
              t('admin.subscriptions.noExpiration')
            }}</span>
          </template>

          <template #cell-status="{ value }">
            <span
              :class="[
                'badge',
                value === 'active'
                  ? 'badge-success'
                  : value === 'expired'
                    ? 'badge-warning'
                    : 'badge-danger'
              ]"
            >
              {{ t(`admin.subscriptions.status.${value}`) }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button
                v-if="(row.status === 'active' || row.status === 'expired') &amp;&amp; (canManage)"
                @click="actionDialog?.open('extend', row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-blue-50 hover:text-blue-600 dark:hover:bg-blue-900/20 dark:hover:text-blue-400"
              >
                <Icon name="calendar" size="sm" />
                <span class="text-xs">{{ t('admin.subscriptions.adjust') }}</span>
              </button>
              <button
                v-if="(row.status === 'active') &amp;&amp; (canManage)"
                @click="actionDialog?.open('reset_quota', row)"
                :disabled="actionDialog?.busy"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-orange-50 hover:text-orange-600 dark:hover:bg-orange-900/20 dark:hover:text-orange-400 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Icon name="refresh" size="sm" />
                <span class="text-xs">{{ t('admin.subscriptions.resetQuota') }}</span>
              </button>
              <button
                v-if="(row.status === 'active') &amp;&amp; (canManage)"
                @click="actionDialog?.open('revoke', row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
              >
                <Icon name="ban" size="sm" />
                <span class="text-xs">{{ t('admin.subscriptions.revoke') }}</span>
              </button>
              <button
                v-if="(row.status === 'revoked') &amp;&amp; (canManage)"
                @click="actionDialog?.open('restore', row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-green-50 hover:text-green-600 dark:hover:bg-green-900/20 dark:hover:text-green-400"
              >
                <Icon name="refresh" size="sm" />
                <span class="text-xs">{{ t('admin.subscriptions.restore') }}</span>
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.subscriptions.noSubscriptionsYet')"
              :description="t('admin.subscriptions.assignFirstSubscription')"
              :action-text="t('admin.subscriptions.assignSubscription')"
              @action="showAssignModal = true"
            />
          </template>
        </DataTable>
      </template>

      <!-- Pagination -->
      <template #pagination>
      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />
      </template>
    </TablePageLayout>

    <BulkSubscriptionActionDialog
      v-if="bulkAction !== null"
      :show="true"
      :action="bulkAction"
      :subscriptions="bulkSubscriptions"
      @close="bulkAction = null"
      @completed="handleBulkCompleted"
    />

    <AssignSubscriptionDialog v-if="showAssignModal" :groups="groups" @close="showAssignModal = false" @completed="loadSubscriptions" />

    <SubscriptionActionsDialog ref="actionDialog" @completed="loadSubscriptions" />
    <SubscriptionGuideDialog :show="showGuideModal" @close="showGuideModal = false" />
  </div>
</template>

<script setup lang="ts">

import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { requireSubscriptionPort, subscriptionNotificationsKey, subscriptionDirectoryKey, subscriptionCanManageKey, subscriptionNavigationKey } from '../ports'
import { useSubscriptionRequest } from '../queries/requestScope'
import SubscriptionQuotaCell from '../components/SubscriptionQuotaCell.vue'
import SubscriptionActionsDialog from '../components/SubscriptionActionsDialog.vue'
import SubscriptionGuideDialog from '../components/SubscriptionGuideDialog.vue'
import AssignSubscriptionDialog from '../components/AssignSubscriptionDialog.vue'
import { subscriptionsAPI } from '../api/admin'
import type { Group } from '@/features/access-policy/api/groupTypes';
import type { UserSubscription } from '../api/types'
import type { SubscriptionUserOption as SimpleUser } from '../ports'
import type { SubscriptionBulkAction, SubscriptionBulkActionResult } from '../api/admin'
import { useTableSelection } from '@/shared/lib/useTableSelection'
import BulkSubscriptionActionDialog from '../components/BulkSubscriptionActionDialog.vue'
import type { Column } from '@/shared/ui/tableTypes'
import { formatDateTimeToMinute } from '@/shared/lib/format'
import { getPersistedPageSize } from '@/shared/lib/usePersistedPageSize'
import TablePageLayout from '@/shared/ui/TablePageLayout.vue'
import DataTable from '@/shared/ui/DataTable.vue'
import Pagination from '@/shared/ui/Pagination.vue'
import EmptyState from '@/shared/ui/EmptyState.vue'
import Select from '@/shared/ui/Select.vue'
import { subscriptionPresentationKey } from '../ports'
const { components: { GroupBadge }, groupPlatformOptions } = requireSubscriptionPort(subscriptionPresentationKey)
import Icon from '@/shared/ui/icons/Icon.vue'
import {
  getRemainingExpiryDuration
} from '@/features/subscriptions/model/subscriptionQuota'

const { t } = useI18n()
const notifications = requireSubscriptionPort(subscriptionNotificationsKey)
const directory = requireSubscriptionPort(subscriptionDirectoryKey)
const canManage = requireSubscriptionPort(subscriptionCanManageKey)
const navigation = requireSubscriptionPort(subscriptionNavigationKey)

// Guide modal state
const showGuideModal = ref(false)

// User column display mode: 'email' or 'username'
const userColumnMode = ref<'email' | 'username'>('email')
const USER_COLUMN_MODE_KEY = 'subscription-user-column-mode'

const loadUserColumnMode = () => {
  try {
    const saved = localStorage.getItem(USER_COLUMN_MODE_KEY)
    if (saved === 'email' || saved === 'username') {
      userColumnMode.value = saved
    }
  } catch (e) {
    console.error('Failed to load user column mode:', e)
  }
}

const saveUserColumnMode = () => {
  try {
    localStorage.setItem(USER_COLUMN_MODE_KEY, userColumnMode.value)
  } catch (e) {
    console.error('Failed to save user column mode:', e)
  }
}

const setUserColumnMode = (mode: 'email' | 'username') => {
  userColumnMode.value = mode
  saveUserColumnMode()
}

// All available columns
const allColumns = computed<Column[]>(() => [
  {
    key: 'user',
    label: userColumnMode.value === 'email'
      ? t('admin.subscriptions.columns.user')
      : t('admin.users.columns.username'),
    sortable: false
  },
  { key: 'group', label: t('admin.subscriptions.columns.group'), sortable: false },
  { key: 'usage', label: t('admin.subscriptions.columns.usage'), sortable: false },
  { key: 'expires_at', label: t('admin.subscriptions.columns.expires'), sortable: true },
  { key: 'status', label: t('admin.subscriptions.columns.status'), sortable: true },
  { key: 'actions', label: t('admin.subscriptions.columns.actions'), sortable: false }
])

// Columns that can be toggled (exclude user and actions which are always visible)
const toggleableColumns = computed(() =>
  allColumns.value.filter(col => col.key !== 'user' && col.key !== 'actions')
)

// Hidden columns set
const hiddenColumns = reactive<Set<string>>(new Set())

// Default hidden columns
const DEFAULT_HIDDEN_COLUMNS: string[] = []

// localStorage key
const HIDDEN_COLUMNS_KEY = 'subscription-hidden-columns'

// Load saved column settings
const loadSavedColumns = () => {
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (saved) {
      const parsed = JSON.parse(saved) as string[]
      parsed.forEach(key => hiddenColumns.add(key))
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach(key => hiddenColumns.add(key))
    }
  } catch (e) {
    console.error('Failed to load saved columns:', e)
    DEFAULT_HIDDEN_COLUMNS.forEach(key => hiddenColumns.add(key))
  }
}

// Save column settings to localStorage
const saveColumnsToStorage = () => {
  try {
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
  } catch (e) {
    console.error('Failed to save columns:', e)
  }
}

// Toggle column visibility
const toggleColumn = (key: string) => {
  if (hiddenColumns.has(key)) {
    hiddenColumns.delete(key)
  } else {
    hiddenColumns.add(key)
  }
  saveColumnsToStorage()
}

// Check if column is visible
const isColumnVisible = (key: string) => !hiddenColumns.has(key)

// Filtered columns for display
const columns = computed<Column[]>(() =>
  allColumns.value.filter(col =>
    col.key === 'user' || col.key === 'actions' || !hiddenColumns.has(col.key)
  )
)

// Column dropdown state
const showColumnDropdown = ref(false)
const columnDropdownRef = ref<HTMLElement | null>(null)

// Filter options
const statusOptions = computed(() => [
  { value: '', label: t('admin.subscriptions.allStatus') },
  { value: 'active', label: t('admin.subscriptions.status.active') },
  { value: 'expired', label: t('admin.subscriptions.status.expired') },
  { value: 'revoked', label: t('admin.subscriptions.status.revoked') }
])

const subscriptions = ref<UserSubscription[]>([])
const groups = ref<Group[]>([])
const loading = ref(false)


const { selectedIds, selectedCount, setSelectedIds, clear: clearSelection, removeMany: removeSelectedIds } =
  useTableSelection<UserSubscription>({ rows: subscriptions, getId: (subscription) => subscription.id })
const bulkActions: SubscriptionBulkAction[] = ['extend', 'reset_quota', 'revoke', 'restore']
const bulkAction = ref<SubscriptionBulkAction | null>(null)
const bulkSubscriptions = ref<UserSubscription[]>([])
const bulkTargets = computed(() => {
  const selected = subscriptions.value.filter((subscription) => selectedIds.value.includes(subscription.id))
  return {
    extend: selected.filter((subscription) => ['active', 'expired'].includes(subscription.status)),
    reset_quota: selected.filter((subscription) => subscription.status === 'active'),
    revoke: selected.filter((subscription) => subscription.status === 'active'),
    restore: selected.filter((subscription) => subscription.status === 'revoked')
  }
})
const getSubscriptionSelectionLabel = (subscription: UserSubscription) =>
  t('admin.subscriptions.bulk.selectSubscription', { id: subscription.id })
const handleSelectedKeysUpdate = (keys: Array<string | number>) => {
  const visibleIds = new Set(subscriptions.value.map((subscription) => subscription.id))
  setSelectedIds(keys.filter((key): key is number => typeof key === 'number' && visibleIds.has(key)))
}
const openBulkAction = (action: SubscriptionBulkAction) => {
  if (loading.value || bulkTargets.value[action].length === 0) return
  bulkSubscriptions.value = [...bulkTargets.value[action]]
  bulkAction.value = action
}
const handleBulkCompleted = async (result: SubscriptionBulkActionResult) => {
  removeSelectedIds(result.results.filter((item) => item.success).map((item) => item.subscription_id))
  await loadSubscriptions()
}

// Toolbar user filter (fuzzy search -> select user_id)
const filterUserKeyword = ref('')
const filterUserResults = ref<SimpleUser[]>([])
const filterUserLoading = ref(false)
const showFilterUserDropdown = ref(false)
const selectedFilterUser = ref<SimpleUser | null>(null)
let filterUserSearchTimeout: ReturnType<typeof setTimeout> | null = null

const filters = reactive({
  status: 'active',
  group_id: '',
  platform: '',
  user_id: null as number | null
})

// Sorting state
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})

const actionDialog = ref<InstanceType<typeof SubscriptionActionsDialog> | null>(null)
const showAssignModal = ref(false)
// Group options for filter (all groups)
const groupOptions = computed(() => [
  { value: '', label: t('admin.subscriptions.allGroups') },
  ...groups.value.map((g) => ({ value: g.id.toString(), label: g.name }))
])

const platformFilterOptions = computed(() => [
  { value: '', label: t('admin.subscriptions.allPlatforms') },
  ...groupPlatformOptions.value
])

const applyFilters = () => {
  clearSelection()
  pagination.page = 1
  loadSubscriptions()
}

const loadSubscriptions = async () => {
  const request = listRequest.begin()
  const { signal } = request

  loading.value = true
  try {
    const response = await subscriptionsAPI.list(
      pagination.page,
      pagination.page_size,
      {
        status: (filters.status as UserSubscription['status']) || undefined,
        group_id: filters.group_id ? parseInt(filters.group_id) : undefined,
        platform: filters.platform || undefined,
        user_id: filters.user_id || undefined,
        sort_by: sortState.sort_by,
        sort_order: sortState.sort_order
      },
      {
        signal
      }
    )
    if (!request.isCurrent()) return
    subscriptions.value = response.items
    const visibleIds = new Set(response.items.map((subscription) => subscription.id))
    setSelectedIds(selectedIds.value.filter((id) => visibleIds.has(id)))
    pagination.total = response.total
    pagination.pages = response.pages
  } catch (error: unknown) {
    if (!request.isCurrent()) {
      return
    }
    notifications.showError(t('admin.subscriptions.failedToLoad'))
    console.error('Error loading subscriptions:', error)
  } finally {
    if (request.isCurrent()) {
      loading.value = false
    }
  }
}

const loadGroups = async () => {
  const request = groupsRequest.begin()
  try {
    const data = await directory.listGroups(request.signal)
    if (request.isCurrent()) groups.value = data
  } catch (error) {
    console.error('Error loading groups:', error)
  }
}

// Toolbar user filter search with debounce
const debounceSearchFilterUsers = () => {
  filterRequest.cancel()
  if (filterUserSearchTimeout) {
    clearTimeout(filterUserSearchTimeout)
  }
  filterUserSearchTimeout = setTimeout(searchFilterUsers, 300)
}

const searchFilterUsers = async () => {
  const keyword = filterUserKeyword.value.trim()

  // Clear active user filter if user modified the search keyword
  if (selectedFilterUser.value && keyword !== selectedFilterUser.value.email) {
    selectedFilterUser.value = null
    filters.user_id = null
    applyFilters()
  }

  if (!keyword) {
    filterUserResults.value = []
    return
  }

  const request = filterRequest.begin()
  filterUserLoading.value = true
  try {
    const data = await directory.searchHistoryUsers(keyword, request.signal)
    if (request.isCurrent()) filterUserResults.value = data
  } catch (error) {
    if (!request.isCurrent()) return
    console.error('Failed to search users:', error)
    filterUserResults.value = []
  } finally {
    if (request.isCurrent()) filterUserLoading.value = false
  }
}

const selectFilterUser = (user: SimpleUser) => {
  selectedFilterUser.value = user
  filterUserKeyword.value = user.email
  showFilterUserDropdown.value = false
  filters.user_id = user.id
  applyFilters()
}

const clearFilterUser = () => {
  selectedFilterUser.value = null
  filterUserKeyword.value = ''
  filterUserResults.value = []
  showFilterUserDropdown.value = false
  filters.user_id = null
  applyFilters()
}

const handlePageChange = (page: number) => {
  clearSelection()
  pagination.page = page
  loadSubscriptions()
}

const handlePageSizeChange = (pageSize: number) => {
  clearSelection()
  pagination.page_size = pageSize
  pagination.page = 1
  loadSubscriptions()
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  clearSelection()
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  loadSubscriptions()
}

// Helper functions
const getDaysRemaining = (expiresAt: string): number | null => {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  if (diff < 0) return null
  return Math.ceil(diff / (1000 * 60 * 60 * 24))
}

const formatRemainingExpiry = (expiresAt: string): string | null => {
  const duration = getRemainingExpiryDuration(expiresAt)
  if (!duration) return null
  if (duration.unit === 'days') {
    return t('admin.subscriptions.daysRemaining', { days: duration.days })
  }
  if (duration.hours) {
    return t('admin.subscriptions.hoursMinutesRemaining', {
      hours: duration.hours,
      minutes: duration.minutes
    })
  }
  return t('admin.subscriptions.minutesRemaining', { minutes: duration.minutes })
}

const isExpiringSoon = (expiresAt: string): boolean => {
  const days = getDaysRemaining(expiresAt)
  return days !== null && days <= 7
}

const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as HTMLElement
  if (!target.closest('[data-filter-user-search]')) showFilterUserDropdown.value = false
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(target)) {
    showColumnDropdown.value = false
  }
}


const listRequest = useSubscriptionRequest(() => { subscriptions.value = []; loading.value = false; pagination.total = 0; pagination.pages = 0; clearSelection() })
const groupsRequest = useSubscriptionRequest(() => { groups.value = [] })
const filterRequest = useSubscriptionRequest(() => { filterUserResults.value = []; filterUserLoading.value = false })
watch(listRequest.scope, () => { showAssignModal.value = false; bulkAction.value = null; bulkSubscriptions.value = []; showGuideModal.value = false; filters.user_id = null; filterUserKeyword.value = ''; selectedFilterUser.value = null; if (filterUserSearchTimeout) clearTimeout(filterUserSearchTimeout); void loadSubscriptions(); void loadGroups() })

onMounted(() => {
  loadUserColumnMode()
  loadSavedColumns()
  loadSubscriptions()
  loadGroups()
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  if (filterUserSearchTimeout) {
    clearTimeout(filterUserSearchTimeout)
  }

})
</script>
