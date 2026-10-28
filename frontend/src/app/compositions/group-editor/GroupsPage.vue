<template>

  <div>
    <TablePageLayout>
      <template #filters>
        <div
          class="flex flex-col justify-between gap-4 lg:flex-row lg:items-start"
        >
          <!-- Left: fuzzy search + filters (can wrap to multiple lines) -->
          <div class="flex flex-1 flex-wrap items-center gap-3">
            <div class="relative w-full sm:w-64">
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
              />
              <input
                v-model="searchQuery"
                type="text"
                :placeholder="t('admin.groups.searchGroups')"
                class="input pl-10"
                @input="handleSearch"
              />
            </div>
            <Select
              v-model="filters.platform"
              :options="platformFilterOptions"
              :placeholder="t('admin.groups.allPlatforms')"
              class="w-44"
              @change="loadGroups"
            />
            <Select
              v-model="filters.status"
              :options="statusOptions"
              :placeholder="t('admin.groups.allStatus')"
              class="w-40"
              @change="loadGroups"
            />
            <Select
              v-if="!authStore.isSimpleMode"
              v-model="filters.is_exclusive"
              :options="exclusiveOptions"
              :placeholder="t('admin.groups.allGroups')"
              class="w-44"
              @change="loadGroups"
            />
          </div>

          <!-- Right: actions -->
          <div
            class="flex w-full flex-shrink-0 flex-wrap items-center justify-end gap-3 lg:w-auto"
          >
            <button
              @click="loadGroups"
              :disabled="loading"
              class="btn btn-secondary"
              :title="t('common.refresh')"
            >
              <Icon
                name="refresh"
                size="md"
                :class="loading ? 'animate-spin' : ''"
              />
            </button>
            <div class="relative" ref="columnDropdownRef">
              <button
                @click="showColumnDropdown = !showColumnDropdown"
                class="btn btn-secondary"
                :title="t('admin.groups.columnSettings')"
              >
                <Icon name="grid" size="md" class="mr-2" />
                <span class="hidden md:inline">{{
                  t("admin.groups.columnSettings")
                }}</span>
              </button>
              <div
                v-if="showColumnDropdown"
                class="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 overflow-y-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-800"
              >
                <button
                  v-for="col in toggleableColumns"
                  :key="col.key"
                  @click="toggleColumn(col.key)"
                  class="flex w-full items-center justify-between px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
                >
                  <span>{{ col.label }}</span>
                  <Icon
                    v-if="isColumnVisible(col.key)"
                    name="check"
                    size="sm"
                    class="text-primary-500"
                    :stroke-width="2"
                  />
                </button>
              </div>
            </div>
            <button
              v-if="(!authStore.isSimpleMode) &amp;&amp; (rolePermissions.can('groups.manage'))"
              @click="openSortModal"
              class="btn btn-secondary"
              :title="t('admin.groups.sortOrder')"
            >
              <Icon name="arrowsUpDown" size="md" class="mr-2" />
              {{ t("admin.groups.sortOrder") }}
            </button>
            <button v-if="rolePermissions.can('groups.manage')"
              @click="openCreateModal"
              class="btn btn-primary"
              data-tour="groups-create-btn"
            >
              <Icon name="plus" size="md" class="mr-2" />
              {{ t("admin.groups.createGroup") }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="groups"
          :loading="loading"
          :server-side-sort="true"
          default-sort-key="sort_order"
          default-sort-order="asc"
          @sort="handleSort"
        >
          <template #cell-name="{ value }">
            <span class="font-medium text-gray-900 dark:text-white">{{
              value
            }}</span>
          </template>

          <template #cell-id="{ value }">
            <span class="font-mono text-xs text-gray-500 dark:text-gray-400"
              >#{{ value }}</span
            >
          </template>

          <template #cell-platform="{ value }">
            <span
              :class="[
                'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium',
                value === 'anthropic'
                  ? 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
                  : value === 'openai'
                    ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
                    : value === 'antigravity'
                      ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                      : value === 'grok'
                        ? 'bg-zinc-200 text-zinc-800 dark:bg-zinc-700 dark:text-zinc-100'
                        : value === 'kimi'
                          ? 'bg-pink-100 text-pink-700 dark:bg-pink-900/30 dark:text-pink-400'
                          : value === 'zhipu'
                            ? 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-400'
                            : value === 'deepseek'
                              ? 'bg-teal-100 text-teal-700 dark:bg-teal-900/30 dark:text-teal-400'
                              : value === 'minimax'
                                ? 'bg-rose-100 text-rose-700 dark:bg-rose-900/30 dark:text-rose-400'
                              : value === 'opencode_go'
                                ? 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300'
                                : 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
              ]"
            >
              <PlatformIcon :platform="value" size="xs" />
              {{ t("admin.groups.platforms." + value, platformLabel(value)) }}
            </span>
          </template>

          <template #cell-billing_type="{ row }">
            <div class="space-y-1">
              <!-- Type Badge -->
              <span
                :class="[
                  'inline-block rounded-full px-2 py-0.5 text-xs font-medium',
                  row.subscription_type === 'subscription'
                    ? 'bg-violet-100 text-violet-700 dark:bg-violet-900/30 dark:text-violet-400'
                    : 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300',
                ]"
              >
                {{
                  row.subscription_type === "subscription"
                    ? t("admin.groups.subscription.subscription")
                    : t("admin.groups.subscription.standard")
                }}
              </span>
              <!-- Subscription Limits - compact single line -->
              <div
                v-if="row.subscription_type === 'subscription'"
                class="space-y-0.5 text-xs text-gray-500 dark:text-gray-400"
              >
                <div
                  v-if="
                    row.daily_limit_usd ||
                    row.weekly_limit_usd ||
                    row.monthly_limit_usd
                  "
                  class="flex flex-wrap items-center gap-x-1 gap-y-0.5"
                >
                  <span v-if="row.daily_limit_usd" class="whitespace-nowrap">
                    <span
                      v-if="usageLoading"
                      class="font-medium text-gray-400 dark:text-gray-500"
                      >—</span
                    >
                    <span
                      v-else
                      :class="
                        getQuotaUsageClass(
                          usageMap.get(row.id)?.today_cost ?? 0,
                          row.daily_limit_usd
                        )
                      "
                      >{{
                        formatUsd(usageMap.get(row.id)?.today_cost ?? 0)
                      }}</span
                    >
                    <span class="text-gray-400 dark:text-gray-500">
                      / {{ formatUsd(row.daily_limit_usd) }}/{{
                        t("admin.groups.limitDay")
                      }}</span
                    >
                  </span>
                  <span
                    v-if="
                      row.daily_limit_usd &&
                      (row.weekly_limit_usd || row.monthly_limit_usd)
                    "
                    class="mx-1 text-gray-300 dark:text-gray-600"
                    >·</span
                  >
                  <span v-if="row.weekly_limit_usd" class="whitespace-nowrap"
                    >{{ formatUsd(row.weekly_limit_usd) }}/{{
                      t("admin.groups.limitWeek")
                    }}</span
                  >
                  <span
                    v-if="row.weekly_limit_usd && row.monthly_limit_usd"
                    class="mx-1 text-gray-300 dark:text-gray-600"
                    >·</span
                  >
                  <span v-if="row.monthly_limit_usd" class="whitespace-nowrap"
                    >{{ formatUsd(row.monthly_limit_usd) }}/{{
                      t("admin.groups.limitMonth")
                    }}</span
                  >
                </div>
                <span v-else class="text-gray-400 dark:text-gray-500">{{
                  t("admin.groups.subscription.noLimit")
                }}</span>
                <div class="text-gray-400 dark:text-gray-500">
                  {{ t("admin.groups.usageTotal") }}
                  <span class="ml-1 font-medium text-gray-600 dark:text-gray-300"
                    >{{
                      usageLoading
                        ? "—"
                        : formatUsd(usageMap.get(row.id)?.total_cost ?? 0)
                    }}</span
                  >
                </div>
              </div>
            </div>
          </template>

          <template #cell-rate_multiplier="{ value }">
            <span class="text-sm text-gray-700 dark:text-gray-300"
              >{{ value }}x</span
            >
          </template>

          <template #cell-is_exclusive="{ value }">
            <span :class="['badge', value ? 'badge-primary' : 'badge-gray']">
              {{
                value ? t("admin.groups.exclusive") : t("admin.groups.public")
              }}
            </span>
          </template>

          <template #cell-account_count="{ row }">
            <div class="space-y-0.5 text-xs">
              <div>
                <span class="text-gray-500 dark:text-gray-400">{{
                  t("admin.groups.accountsAvailable")
                }}</span>
                <span
                  class="ml-1 font-medium text-emerald-600 dark:text-emerald-400"
                  >{{ row.active_account_count || 0 }}</span
                >
                <span
                  class="ml-1 inline-flex items-center rounded bg-gray-100 px-1.5 py-0.5 font-medium text-gray-800 dark:bg-dark-600 dark:text-gray-300"
                  >{{ t("admin.groups.accountsUnit") }}</span
                >
              </div>
              <div v-if="row.rate_limited_account_count">
                <span class="text-gray-500 dark:text-gray-400">{{
                  t("admin.groups.accountsRateLimited")
                }}</span>
                <span
                  class="ml-1 font-medium text-amber-600 dark:text-amber-400"
                  >{{ row.rate_limited_account_count }}</span
                >
                <span
                  class="ml-1 inline-flex items-center rounded bg-gray-100 px-1.5 py-0.5 font-medium text-gray-800 dark:bg-dark-600 dark:text-gray-300"
                  >{{ t("admin.groups.accountsUnit") }}</span
                >
              </div>
              <div>
                <span class="text-gray-500 dark:text-gray-400">{{
                  t("admin.groups.accountsTotal")
                }}</span>
                <span
                  class="ml-1 font-medium text-gray-700 dark:text-gray-300"
                  >{{ row.account_count || 0 }}</span
                >
                <span
                  class="ml-1 inline-flex items-center rounded bg-gray-100 px-1.5 py-0.5 font-medium text-gray-800 dark:bg-dark-600 dark:text-gray-300"
                  >{{ t("admin.groups.accountsUnit") }}</span
                >
              </div>
            </div>
          </template>

          <template #cell-capacity="{ row }">
            <GroupCapacityBadge
              v-if="capacityMap.get(row.id)"
              :concurrency-used="capacityMap.get(row.id)!.concurrencyUsed"
              :concurrency-max="capacityMap.get(row.id)!.concurrencyMax"
              :sessions-used="capacityMap.get(row.id)!.sessionsUsed"
              :sessions-max="capacityMap.get(row.id)!.sessionsMax"
              :rpm-used="capacityMap.get(row.id)!.rpmUsed"
              :rpm-max="capacityMap.get(row.id)!.rpmMax"
            />
            <span v-else class="text-xs text-gray-400">—</span>
          </template>

          <template #cell-usage="{ row }">
            <div v-if="usageLoading" class="text-xs text-gray-400">—</div>
            <div v-else class="space-y-0.5 text-xs">
              <div class="text-gray-500 dark:text-gray-400">
                <span class="text-gray-400 dark:text-gray-500">{{
                  t("admin.groups.usageToday")
                }}</span>
                <span class="ml-1 font-medium text-gray-700 dark:text-gray-300"
                  >${{
                    formatCost(usageMap.get(row.id)?.today_cost ?? 0)
                  }}</span
                >
              </div>
              <div class="text-gray-500 dark:text-gray-400">
                <span class="text-gray-400 dark:text-gray-500">{{
                  t("admin.groups.usageYesterday")
                }}</span>
                <span class="ml-1 font-medium text-gray-700 dark:text-gray-300"
                  >${{
                    formatCost(usageMap.get(row.id)?.yesterday_cost ?? 0)
                  }}</span
                >
              </div>
              <div class="text-gray-500 dark:text-gray-400">
                <span class="text-gray-400 dark:text-gray-500">{{
                  t("admin.groups.usageTotal")
                }}</span>
                <span class="ml-1 font-medium text-gray-700 dark:text-gray-300"
                  >${{
                    formatCost(usageMap.get(row.id)?.total_cost ?? 0)
                  }}</span
                >
              </div>
            </div>
          </template>

          <template #cell-status="{ value }">
            <span
              :class="[
                'badge',
                value === 'active' ? 'badge-success' : 'badge-danger',
              ]"
            >
              {{ t("admin.accounts.status." + value) }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button v-if="rolePermissions.can('groups.manage')"
                @click="handleEdit(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
              >
                <Icon name="edit" size="sm" />
                <span class="text-xs">{{ t("common.edit") }}</span>
              </button>
              <button
                v-if="(!authStore.isSimpleMode) &amp;&amp; (rolePermissions.can('groups.manage'))"
                data-testid="group-duplicate"
                :title="
                  duplicatingGroupIds.has(row.id)
                    ? t('admin.groups.duplicating')
                    : t('admin.groups.duplicate')
                "
                :disabled="duplicatingGroupIds.has(row.id)"
                @click="handleDuplicate(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-dark-700 dark:hover:text-primary-400"
              >
                <Icon name="copy" size="sm" />
                <span class="text-xs">
                  {{
                    duplicatingGroupIds.has(row.id)
                      ? t("admin.groups.duplicating")
                      : t("admin.groups.duplicate")
                  }}
                </span>
              </button>
              <button
                v-if="!authStore.isSimpleMode && row.platform === 'composite'"
                data-testid="group-composite-routes"
                @click="handleCompositeRoutes(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-cyan-600 dark:hover:bg-dark-700 dark:hover:text-cyan-400"
              >
                <Icon name="swap" size="sm" />
                <span class="text-xs">{{
                  t("admin.groups.compositeRoutes.action")
                }}</span>
              </button>
              <button
                v-if="(!authStore.isSimpleMode) &amp;&amp; (rolePermissions.can('billing.rates.update'))"
                data-testid="group-rate-multipliers"
                @click="handleRateMultipliers(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-purple-600 dark:hover:bg-dark-700 dark:hover:text-purple-400"
              >
                <Icon name="dollar" size="sm" />
                <span class="text-xs">{{
                  t("admin.groups.rateMultipliers")
                }}</span>
              </button>
              <button
                v-if="(!authStore.isSimpleMode) &amp;&amp; (rolePermissions.can('groups.manage'))"
                data-testid="group-rpm-overrides"
                @click="handleRPMOverrides(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-orange-600 dark:hover:bg-dark-700 dark:hover:text-orange-400"
              >
                <Icon name="bolt" size="sm" />
                <span class="text-xs">{{
                  t("admin.groups.rpmOverrides")
                }}</span>
              </button>
              <button v-if="rolePermissions.can('groups.manage')"
                @click="handleDelete(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
              >
                <Icon name="trash" size="sm" />
                <span class="text-xs">{{ t("common.delete") }}</span>
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.groups.noGroupsYet')"
              :description="t('admin.groups.createFirstGroup')"
              :action-text="t('admin.groups.createGroup')"
              @action="openCreateModal"
            />
          </template>
        </DataTable>
      </template>

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

    <!-- Create Group Modal -->
 <GroupEditorDialog v-if="showCreateModal || showEditModal" :key="editingGroup?.id ?? 'create'" :group="editingGroup" @close="closeEditor" @saved="loadGroups" />
    <!-- Delete Confirmation Dialog -->
    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.groups.deleteGroup')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />

    

    <!-- Sort Order Modal -->
 <GroupSortDialog v-if="showSortModal" @close="showSortModal = false" @saved="loadGroups" />
 <CompositeRoutesDialog v-if="compositeRoutesGroup" :group="compositeRoutesGroup" @close="compositeRoutesGroup = null" />
<GroupRateMultipliersModal
      :show="showRateMultipliersModal"
      :group="rateMultipliersGroup"
      @close="showRateMultipliersModal = false"
      @success="loadGroups"
    />
<GroupRPMOverridesModal
      :show="showRPMOverridesModal"
      :group="rpmOverridesGroup"
      @close="showRPMOverridesModal = false"
      @success="loadGroups"
    />
</div>

</template>
<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import type { Column } from '@/shared/ui/tableTypes';
import { GROUP_PLATFORM_OPTIONS } from '@/features/catalog';
import { groupsAPI, useGroupListQuery, accessPolicyPermissionsKey, accessPolicyNotificationsKey } from '@/features/access-policy';
import { requireAccessPolicyService } from '@/features/access-policy';
import { useAccessPolicyRequest } from '@/features/access-policy';
import { GroupRateMultipliersModal, GroupRPMOverridesModal } from '@/features/access-policy/panels';
import { CompositeRoutesDialog } from '@/features/access-policy/panels';
import { GroupSortDialog } from '@/features/access-policy/panels';
import GroupEditorDialog from './GroupEditorDialog.vue';
import TablePageLayout from '@/shared/ui/TablePageLayout.vue';
import DataTable from '@/shared/ui/DataTable.vue';
import Pagination from '@/shared/ui/Pagination.vue';

import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue';
import EmptyState from '@/shared/ui/EmptyState.vue';
import Select from '@/shared/ui/Select.vue';
import PlatformIcon from '@/shared/ui/PlatformIcon.vue';
import { GroupCapacityBadge } from '@/features/access-policy/presentation';
import Icon from '@/shared/ui/icons/Icon.vue';
import { platformLabel as platformLabel } from '@/features/catalog';
import { getPersistedPageSize } from '@/shared/lib/usePersistedPageSize';
import { extractApiErrorMessage } from '@/shared/lib/apiError';
const { t } = useI18n()
const rolePermissions = requireAccessPolicyService(accessPolicyPermissionsKey)
const authStore = rolePermissions
const appStore = requireAccessPolicyService(accessPolicyNotificationsKey)
const ALWAYS_VISIBLE_COLUMNS = new Set(["name", "actions"]);
const DEFAULT_HIDDEN_COLUMNS = ["id"];
const HIDDEN_COLUMNS_KEY = "group-hidden-columns";
const COLUMN_SETTINGS_VERSION_KEY = "group-column-settings-version";
const COLUMN_SETTINGS_VERSION = 2;
const VERSION_NEW_HIDDEN_COLUMNS: Record<number, string[]> = {
  2: ["id"],
};
const allColumns = computed<Column[]>(() => {
  const basic: Column[] = [
    { key: "name", label: t("admin.groups.columns.name"), sortable: true },
    { key: "id", label: t("admin.groups.columns.id"), sortable: true },
    { key: "platform", label: t("admin.groups.columns.platform"), sortable: true },
    { key: "account_count", label: t("admin.groups.columns.accounts"), sortable: true },
    { key: "status", label: t("admin.groups.columns.status"), sortable: true },
    { key: "actions", label: t("admin.groups.columns.actions"), sortable: false },
  ];
  if (authStore.isSimpleMode) return basic;
  return [
    ...basic.slice(0, 3),
    { key: "billing_type", label: t("admin.groups.columns.billingType"), sortable: true },
    { key: "rate_multiplier", label: t("admin.groups.columns.rateMultiplier"), sortable: true },
    { key: "is_exclusive", label: t("admin.groups.columns.type"), sortable: true },
    basic[3],
    { key: "capacity", label: t("admin.groups.columns.capacity"), sortable: false },
    { key: "usage", label: t("admin.groups.columns.usage"), sortable: false },
    ...basic.slice(4),
  ];
});
const toggleableColumns = computed(() =>
  allColumns.value.filter((col) => !ALWAYS_VISIBLE_COLUMNS.has(col.key)),
);
const hiddenColumns = reactive<Set<string>>(new Set());
const showColumnDropdown = ref(false);
const columnDropdownRef = ref<HTMLElement | null>(null);
const getValidHiddenColumnKeys = () =>
  new Set(toggleableColumns.value.map((col) => col.key));
const loadSavedColumns = () => {
  hiddenColumns.clear();
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY);
    const validKeys = getValidHiddenColumnKeys();

    if (saved) {
      const parsed = JSON.parse(saved);
      if (Array.isArray(parsed)) {
        parsed
          .filter(
            (key): key is string =>
              typeof key === "string" && validKeys.has(key),
          )
          .forEach((key) => hiddenColumns.add(key));
      }

      // Existing admins: auto-hide columns newly added as default-hidden.
      const storedVersion = Number(
        localStorage.getItem(COLUMN_SETTINGS_VERSION_KEY) ?? "1",
      );
      if (storedVersion < COLUMN_SETTINGS_VERSION) {
        let mutated = false;
        for (let v = storedVersion + 1; v <= COLUMN_SETTINGS_VERSION; v++) {
          for (const key of VERSION_NEW_HIDDEN_COLUMNS[v] ?? []) {
            if (validKeys.has(key) && !hiddenColumns.has(key)) {
              hiddenColumns.add(key);
              mutated = true;
            }
          }
        }
        if (mutated) {
          saveColumnsToStorage();
        } else {
          localStorage.setItem(
            COLUMN_SETTINGS_VERSION_KEY,
            String(COLUMN_SETTINGS_VERSION),
          );
        }
      }
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
        if (validKeys.has(key)) hiddenColumns.add(key);
      });
      saveColumnsToStorage();
    }
  } catch (error) {
    console.error("Failed to load group column settings:", error);
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => hiddenColumns.add(key));
  }
};
const saveColumnsToStorage = () => {
  try {
    const validKeys = getValidHiddenColumnKeys();
    const keys = [...hiddenColumns].filter((key) => validKeys.has(key));
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify(keys));
    localStorage.setItem(
      COLUMN_SETTINGS_VERSION_KEY,
      String(COLUMN_SETTINGS_VERSION),
    );
  } catch (error) {
    console.error("Failed to save group column settings:", error);
  }
};
const isColumnVisible = (key: string) => !hiddenColumns.has(key);
const hasVisibleUsageSummaryConsumer = computed(
  () => !authStore.isSimpleMode && (isColumnVisible("usage") || isColumnVisible("billing_type")),
);
const hasVisibleCapacityColumn = computed(() => !authStore.isSimpleMode && isColumnVisible("capacity"));
const toggleColumn = (key: string) => {
  const validKeys = getValidHiddenColumnKeys();
  if (!validKeys.has(key)) return;

  const wasHidden = hiddenColumns.has(key);
  if (wasHidden) {
    hiddenColumns.delete(key);
  } else {
    hiddenColumns.add(key);
  }
  saveColumnsToStorage();

  if (wasHidden && (key === "usage" || key === "billing_type")) {
    loadUsageSummary();
  }
  if (wasHidden && key === "capacity") {
    loadCapacitySummary();
  }
};
const columns = computed<Column[]>(() =>
  allColumns.value.filter(
    (col) => ALWAYS_VISIBLE_COLUMNS.has(col.key) || !hiddenColumns.has(col.key),
  ),
);
const statusOptions = computed(() => [
  { value: "", label: t("admin.groups.allStatus") },
  { value: "active", label: t("admin.accounts.status.active") },
  { value: "inactive", label: t("admin.accounts.status.inactive") },
]);
const exclusiveOptions = computed(() => [
  { value: "", label: t("admin.groups.allGroups") },
  { value: "true", label: t("admin.groups.exclusive") },
  { value: "false", label: t("admin.groups.nonExclusive") },
]);
const platformFilterOptions = computed(() => [
  { value: "", label: t("admin.groups.allPlatforms") },
  ...GROUP_PLATFORM_OPTIONS,
]);
type GroupUsageSummary = {
  today_cost: number;
  yesterday_cost: number;
  total_cost: number;
};
const usageMap = ref<Map<number, GroupUsageSummary>>(new Map());
const usageLoading = ref(false);
const capacityMap = ref<
  Map<
    number,
    {
      concurrencyUsed: number;
      concurrencyMax: number;
      sessionsUsed: number;
      sessionsMax: number;
      rpmUsed: number;
      rpmMax: number;
    }
  >
>(new Map());
const searchQuery = ref("");
const filters = reactive({
  platform: "",
  status: "",
  is_exclusive: "",
});
const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0,
});
const sortState = reactive({
  sort_by: "sort_order",
  sort_order: "asc" as "asc" | "desc",
});
const showCreateModal = ref(false);
const showEditModal = ref(false);
const showDeleteDialog = ref(false);
const showSortModal = ref(false);
const editingGroup = ref<AdminGroup | null>(null);
const deletingGroup = ref<AdminGroup | null>(null);
const duplicatingGroupIds = reactive(new Map<number, symbol>());
const deletingGroupIds = reactive(new Map<number, symbol>());
const showRateMultipliersModal = ref(false);
const rateMultipliersGroup = ref<AdminGroup | null>(null);
const showRPMOverridesModal = ref(false);
const rpmOverridesGroup = ref<AdminGroup | null>(null);
const compositeRoutesGroup = ref<AdminGroup | null>(null);
const deleteConfirmMessage = computed(() => {
  if (!deletingGroup.value) {
    return "";
  }
  if (deletingGroup.value.subscription_type === "subscription") {
    return t("admin.groups.deleteConfirmSubscription", {
      name: deletingGroup.value.name,
    });
  }
  return t("admin.groups.deleteConfirm", { name: deletingGroup.value.name });
});
const formatCost = (cost: number): string => {
  if (cost >= 1000) return cost.toFixed(0);
  if (cost >= 100) return cost.toFixed(1);
  return cost.toFixed(2);
};
const formatUsd = (cost: number | null | undefined): string =>
  `$${formatCost(cost ?? 0)}`;
const getQuotaUsageClass = (
  used: number,
  limit: number | null | undefined,
): string => {
  if (!limit || limit <= 0) {
    return "font-medium text-gray-700 dark:text-gray-300";
  }
  const ratio = used / limit;
  if (ratio >= 1) {
    return "font-semibold text-red-600 dark:text-red-400";
  }
  if (ratio >= 0.8) {
    return "font-semibold text-amber-600 dark:text-amber-400";
  }
  return "font-medium text-gray-700 dark:text-gray-300";
};
const loadUsageSummary = async () => {
  const request = metricsRequest.begin();
  if (!hasVisibleUsageSummaryConsumer.value) {
    usageLoading.value = false;
    return;
  }
  usageLoading.value = true;
  try {
    const data = await groupsAPI.getUsageSummary();
    const map = new Map<number, GroupUsageSummary>();
    for (const item of data) {
      map.set(item.group_id, {
        today_cost: item.today_cost,
        yesterday_cost: item.yesterday_cost,
        total_cost: item.total_cost,
      });
    }
    if (request.isCurrent()) usageMap.value = map;
  } catch (error) {
    console.error("Error loading group usage summary:", error);
  } finally {
    if (request.isCurrent()) usageLoading.value = false;
  }
};
const loadCapacitySummary = async () => {
  const request = capacityRequest.begin();
  if (!hasVisibleCapacityColumn.value) {
    return;
  }
  try {
    const data = await groupsAPI.getCapacitySummary();
    const map = new Map<
      number,
      {
        concurrencyUsed: number;
        concurrencyMax: number;
        sessionsUsed: number;
        sessionsMax: number;
        rpmUsed: number;
        rpmMax: number;
      }
    >();
    for (const item of data) {
      map.set(item.group_id, {
        concurrencyUsed: item.concurrency_used,
        concurrencyMax: item.concurrency_max,
        sessionsUsed: item.sessions_used,
        sessionsMax: item.sessions_max,
        rpmUsed: item.rpm_used,
        rpmMax: item.rpm_max,
      });
    }
    if (request.isCurrent()) capacityMap.value = map;
  } catch (error) {
    console.error("Error loading group capacity summary:", error);
  }
};
let searchTimeout: ReturnType<typeof setTimeout>;
const handleSearch = () => {
  clearTimeout(searchTimeout);
  searchTimeout = setTimeout(() => {
    pagination.page = 1;
    loadGroups();
  }, 300);
};
const handlePageChange = (page: number) => {
  pagination.page = page;
  loadGroups();
};
const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize;
  pagination.page = 1;
  loadGroups();
};
const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key;
  sortState.sort_order = order;
  pagination.page = 1;
  loadGroups();
};
const handleRateMultipliers = (group: AdminGroup) => {
  rateMultipliersGroup.value = group;
  showRateMultipliersModal.value = true;
};
const handleRPMOverrides = (group: AdminGroup) => {
  rpmOverridesGroup.value = group;
  showRPMOverridesModal.value = true;
};
const handleDuplicate = async (group: AdminGroup) => {
  if (duplicatingGroupIds.has(group.id) || !rolePermissions.can('groups.manage')) return;
  const request = mutationContext;
  const token = Symbol('duplicate');
  duplicatingGroupIds.set(group.id, token);
  try {
    const duplicate = await groupsAPI.duplicate(group.id);
    if (!request.isCurrent() || !rolePermissions.can('groups.manage')) return;
    appStore.showSuccess(t('admin.groups.duplicateSuccess', { name: duplicate.name }));
    await loadGroups();
  } catch (error: unknown) {
    if (!request.isCurrent() || !rolePermissions.can('groups.manage')) return;
    appStore.showError(extractApiErrorMessage(error, t('admin.groups.duplicateFailed')));
  } finally {
    if (duplicatingGroupIds.get(group.id) === token) duplicatingGroupIds.delete(group.id);
  }
};
const handleDelete = (group: AdminGroup) => {
  deletingGroup.value = group;
  showDeleteDialog.value = true;
};
const confirmDelete = async () => {
  const targetId = deletingGroup.value?.id;
  if (targetId === undefined || deletingGroupIds.has(targetId) || !rolePermissions.can('groups.manage')) return;
  const request = mutationContext;
  const token = Symbol('delete');
  deletingGroupIds.set(targetId, token);
  try {
    await groupsAPI.delete(targetId);
    if (!request.isCurrent() || !rolePermissions.can('groups.manage')) return;
    appStore.showSuccess(t('admin.groups.groupDeleted'));
    if (deletingGroup.value?.id === targetId) { showDeleteDialog.value = false; deletingGroup.value = null; }
    await loadGroups();
  } catch (error: unknown) {
    if (request.isCurrent() && rolePermissions.can('groups.manage')) appStore.showError(extractApiErrorMessage(error, t('admin.groups.failedToDelete')));
  } finally {
    if (deletingGroupIds.get(targetId) === token) deletingGroupIds.delete(targetId);
  }
};
const list = useGroupListQuery()
const groups = list.groups
const loading = list.loading
const capacityRequest = useAccessPolicyRequest(() => { capacityMap.value = new Map() })
const metricsRequest = useAccessPolicyRequest(() => { usageMap.value = new Map(); usageLoading.value = false; capacityMap.value = new Map() })
const mutations = useAccessPolicyRequest(() => { closeEditor(); showSortModal.value = false; showRateMultipliersModal.value = false; showRPMOverridesModal.value = false; compositeRoutesGroup.value = null; showDeleteDialog.value = false; deletingGroup.value = null; duplicatingGroupIds.clear(); deletingGroupIds.clear() })
let mutationContext = mutations.begin()
watch(mutations.scope, () => { mutationContext = mutations.begin() }, { flush: 'sync' })
watch([() => rolePermissions.can('groups.manage'), () => rolePermissions.isSuperAdmin, () => rolePermissions.isSimpleMode], () => { mutations.cancel(); mutationContext = mutations.begin(); list.clear(); void loadGroups() }, { flush: 'sync' })
let groupListSequence = 0
async function loadGroups() {
 const sequence = ++groupListSequence
 const scopeAtStart = list.scope.value
 const result = await list.load(pagination.page, pagination.page_size, { platform: filters.platform || undefined, status: (filters.status || undefined) as 'active' | 'inactive' | undefined, is_exclusive: !authStore.isSimpleMode && filters.is_exclusive ? filters.is_exclusive === 'true' : undefined, search: searchQuery.value.trim() || undefined, sort_by: sortState.sort_by, sort_order: sortState.sort_order })
 if (sequence !== groupListSequence || scopeAtStart !== list.scope.value) return
 if (result) { pagination.total = result.total; pagination.pages = result.pages; if (hasVisibleUsageSummaryConsumer.value) void loadUsageSummary(); if (hasVisibleCapacityColumn.value) void loadCapacitySummary() }
 else if (list.error.value) appStore.showError(t('admin.groups.failedToLoad'))
}
function openCreateModal() { if (!rolePermissions.can('groups.manage')) return; editingGroup.value = null; showCreateModal.value = true }
function handleEdit(group: AdminGroup) {
 const current = groups.value.find(item => item.id === group.id)
 if (!current || !rolePermissions.can('groups.manage')) return
 editingGroup.value = current; showEditModal.value = true
}
function closeEditor() { showCreateModal.value = false; showEditModal.value = false; editingGroup.value = null }
function handleCompositeRoutes(group: AdminGroup) { compositeRoutesGroup.value = group }
function openSortModal() { showSortModal.value = true }
function handleClickOutside(event: MouseEvent) { if (columnDropdownRef.value && !columnDropdownRef.value.contains(event.target as Node)) showColumnDropdown.value = false }
watch(list.scope, () => { pagination.page = 1; pagination.total = 0; pagination.pages = 0; clearTimeout(searchTimeout); void loadGroups() })
onMounted(() => { loadSavedColumns(); void loadGroups(); document.addEventListener('click', handleClickOutside) })
onUnmounted(() => { clearTimeout(searchTimeout); document.removeEventListener('click', handleClickOutside) })

</script>
