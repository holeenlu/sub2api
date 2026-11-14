<template>
  <div>
    <div class="w-full min-w-0 space-y-6 pb-8">
      <header
        class="page-header mb-0 rounded-3xl bg-white p-5 shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700 sm:p-6"
      >
        <h1 class="page-title flex items-center gap-2 text-xl font-black text-gray-900 dark:text-white">
          <span class="inline-flex h-8 w-8 items-center justify-center rounded-xl bg-blue-50 text-blue-500 dark:bg-blue-900/30 dark:text-blue-400">
            <Icon name="chart" size="sm" />
          </span>
          {{ t('admin.channelMonitor.title') }}
        </h1>
        <p class="page-description mt-1.5 text-xs text-gray-500 dark:text-gray-400">
          {{
            isV1Mode
              ? t('channelMonitorV2.admin.descriptionV1')
              : t('channelMonitorV2.admin.descriptionV2')
          }}
        </p>
        <div class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700">
          <div
            class="tabs inline-flex w-full max-w-xl flex-wrap sm:w-auto"
            role="tablist"
            :aria-label="t('channelMonitorV2.admin.tabAria')"
          >
            <button
              type="button"
              role="tab"
              class="tab flex-1 sm:flex-none"
              :class="adminMonitorTab === 'v2' ? 'tab-active' : ''"
              :aria-selected="adminMonitorTab === 'v2'"
              @click="adminMonitorTab = 'v2'"
            >
              {{ t('channelMonitorV2.admin.tabV2') }}
            </button>
            <button
              type="button"
              role="tab"
              class="tab flex-1 sm:flex-none"
              :class="adminMonitorTab === 'legacy' ? 'tab-active' : ''"
              :aria-selected="adminMonitorTab === 'legacy'"
              @click="adminMonitorTab = 'legacy'"
            >
              {{ isV1Mode ? t('channelMonitorV2.admin.tabV1Active') : t('channelMonitorV2.admin.tabV1History') }}
            </button>
          </div>
        </div>
      </header>

      <MonitorSettingsPanel v-if="adminMonitorTab === 'v2'" />

      <TablePageLayout v-else>
      <template #filters>
        <MonitorFiltersBar
          v-model:search="searchQuery"
          v-model:provider="providerFilter"
          v-model:enabled="enabledFilter"
          :loading="loading"
          @reload="reload"
          @create="openCreateDialog"
          @manage-templates="showTemplateManager = true"
          @search-input="handleSearch"
        />
      </template>

      <template #table>
        <DataTable :columns="columns" :data="monitors" :loading="loading">
          <template #cell-name="{ row, value }">
            <div class="flex items-center gap-1.5">
              <span class="font-medium text-gray-900 dark:text-white">{{ value }}</span>
              <HelpTooltip v-if="row.api_key_decrypt_failed" :content="t('admin.channelMonitor.apiKeyDecryptFailed')">
                <Icon name="exclamationTriangle" size="sm" class="text-red-500" />
              </HelpTooltip>
            </div>
          </template>

          <template #cell-provider="{ row }">
            <span class="inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium" :class="providerBadgeClass(row.provider)">
              {{ providerLabel(row.provider) }}
            </span>
            <!-- 三种检测模式并列展示，quota 系配额数据源与纯探活一眼可分 -->
            <span class="ml-1 inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium" :class="checkModeBadgeClass(row.check_mode)">
              {{ checkModeLabel(row.check_mode) }}
            </span>
          </template>

          <template #cell-primary_model="{ row }">
            <MonitorPrimaryModelCell :row="row" />
          </template>

          <template #cell-availability_7d="{ row }">
            <span class="text-sm text-gray-900 dark:text-gray-100">{{ formatAvailability(row) }}</span>
          </template>

          <template #cell-latency="{ row }">
            <span class="text-sm text-gray-900 dark:text-gray-100">{{ formatLatency(row.primary_latency_ms) }}</span>
          </template>

          <template #cell-enabled="{ row }">
            <Toggle :modelValue="row.enabled" @update:modelValue="toggleEnabled(row)" />
          </template>

          <template #cell-actions="{ row }">
            <MonitorActionsCell
              :row="row"
              :running="runningId === row.id"
              :duplicating="duplicatingIds.has(row.id)"
              @run="handleRunNow"
              @duplicate="handleDuplicate"
              @edit="openEditDialog"
              @delete="handleDelete"
            />
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.channelMonitor.noMonitorsYet')"
              :description="t('admin.channelMonitor.createFirstMonitor')"
              :action-text="t('admin.channelMonitor.createButton')"
              @action="openCreateDialog"
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
          @update:page="onPageChange"
          @update:pageSize="onPageSizeChange"
        />
      </template>
      </TablePageLayout>
    </div>

    <MonitorFormDialog
      :show="showDialog"
      :monitor="editing"
      @close="closeDialog"
      @saved="reload"
    />

    <MonitorTemplateManagerDialog
      :show="showTemplateManager"
      @close="showTemplateManager = false"
      @updated="reload"
    />

    <MonitorRunResultDialog
      :show="showRunResult"
      :results="runResults"
      @close="showRunResult = false"
    />

    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('common.delete')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { resolveChannelMonitorMode } from '../mode'
import { useOperationsRequest } from '@/features/operations/queries/useOperationsRequest'
import { useOperationsServices } from '@/features/operations/services'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { channelMonitorAPI } from '@/features/operations/api'
import { useMonitorDuplicate } from '@/features/operations'
import type {
  ChannelMonitor,
  CheckResult,
  ListParams,
  Provider,
} from '@/features/operations/api'
import type { Column } from '@/shared/ui/tableTypes'

import TablePageLayout from '@/shared/ui/TablePageLayout.vue'
import DataTable from '@/shared/ui/DataTable.vue'
import Pagination from '@/shared/ui/Pagination.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import EmptyState from '@/shared/ui/EmptyState.vue'
import HelpTooltip from '@/shared/ui/HelpTooltip.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import Toggle from '@/shared/ui/Toggle.vue'
import MonitorFiltersBar from '@/features/operations/monitor/admin/MonitorFiltersBar.vue'
import MonitorFormDialog from '@/features/operations/monitor/admin/MonitorFormDialog.vue'
import MonitorTemplateManagerDialog from '@/features/operations/monitor/admin/MonitorTemplateManagerDialog.vue'
import MonitorRunResultDialog from '@/features/operations/monitor/admin/MonitorRunResultDialog.vue'
import MonitorPrimaryModelCell from '@/features/operations/monitor/admin/MonitorPrimaryModelCell.vue'
import MonitorActionsCell from '@/features/operations/monitor/admin/MonitorActionsCell.vue'
import { getPersistedPageSize } from '@/shared/lib/usePersistedPageSize'
import { useChannelMonitorFormat } from '@/features/operations/monitor/useChannelMonitorFormat'
import MonitorSettingsPanel from '@/features/operations/monitor/MonitorSettingsPanel.vue'


const { t } = useI18n()
const services = useOperationsServices()
const requests = useOperationsRequest(services)
const isV1Mode = computed(() => services.publicSettings.value?.channel_monitor_enabled !== false && resolveChannelMonitorMode(services.publicSettings.value) === 'v1')
const adminMonitorTab = ref<'v2' | 'legacy'>(isV1Mode.value ? 'legacy' : 'v2')
const {
  providerLabel,
  providerBadgeClass,
  checkModeLabel,
  checkModeBadgeClass,
  formatLatency,
  formatAvailability,
} = useChannelMonitorFormat()

const monitors = ref<ChannelMonitor[]>([])
const loading = ref(false)
const runningId = ref<number | null>(null)
const searchQuery = ref('')
const providerFilter = ref<Provider | ''>('')
const enabledFilter = ref<'' | 'true' | 'false'>('')
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })

const showDialog = ref(false)
const showTemplateManager = ref(false)
const editing = ref<ChannelMonitor | null>(null)
const showDeleteDialog = ref(false)
const deleting = ref<ChannelMonitor | null>(null)
const showRunResult = ref(false)
const runResults = ref<CheckResult[]>([])
const duplicateCommand = useMonitorDuplicate(services)
const duplicatingIds = computed(() => duplicateCommand.state.value.status === 'pending' ? new Set([duplicateCommand.state.value.monitorId]) : new Set<number>())

let mounted = false
let searchTimeout: ReturnType<typeof setTimeout> | null = null

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.channelMonitor.columns.name'), sortable: false },
  { key: 'provider', label: t('admin.channelMonitor.columns.provider'), sortable: false },
  { key: 'primary_model', label: t('admin.channelMonitor.columns.primaryModel'), sortable: false },
  { key: 'availability_7d', label: t('admin.channelMonitor.columns.availability7d'), sortable: false },
  { key: 'latency', label: t('admin.channelMonitor.columns.latency'), sortable: false },
  { key: 'enabled', label: t('admin.channelMonitor.columns.enabled'), sortable: false },
  { key: 'actions', label: t('admin.channelMonitor.columns.actions'), sortable: false },
])

const deleteConfirmMessage = computed(() => {
  const name = deleting.value?.name || ''
  return t('admin.channelMonitor.deleteConfirm', { name })
})

async function reload() {
  if (services.scopeKey.value === null) return
  const operation = requests.begin('list')
  loading.value = true
  try {
    const params: ListParams = {
      page: pagination.page,
      page_size: pagination.page_size,
    }
    if (providerFilter.value) params.provider = providerFilter.value
    if (enabledFilter.value === 'true') params.enabled = true
    if (enabledFilter.value === 'false') params.enabled = false
    if (searchQuery.value.trim()) params.search = searchQuery.value.trim()

    const res = await operation.wait(channelMonitorAPI.list(params, { signal: operation.signal }))
    monitors.value = res.items || []
    pagination.total = res.total
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (!operation.isCurrent() || e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    services.notifyError(extractApiErrorMessage(err, t('admin.channelMonitor.loadError')))
  } finally {
    if (operation.isCurrent()) {
      loading.value = false
    }
  }
}

function handleSearch() {
  if (searchTimeout) clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    pagination.page = 1
    reload()
  }, 300)
}

function onPageChange(page: number) {
  pagination.page = page
  reload()
}

function onPageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  reload()
}

function openCreateDialog() {
  editing.value = null
  showDialog.value = true
}

function openEditDialog(row: ChannelMonitor) {
  editing.value = row
  showDialog.value = true
}

function closeDialog() {
  showDialog.value = false
  editing.value = null
}

async function toggleEnabled(row: ChannelMonitor) {
  const next = !row.enabled
  const operation = requests.begin('enabled:' + row.id)
  try {
    await operation.wait(channelMonitorAPI.update(row.id, { enabled: next }, { signal: operation.signal }))
    row.enabled = next
  } catch (err: unknown) {
    if (!operation.isCurrent()) return
    services.notifyError(extractApiErrorMessage(err, t('common.error')))
  }
}

async function handleRunNow(row: ChannelMonitor) {
  if (!isV1Mode.value) {
    services.notifyError(t('admin.channelMonitor.runFailed'))
    return
  }
  if (runningId.value != null) return
  runningId.value = row.id
  const operation = requests.begin('run')
  try {
    const res = await operation.wait(channelMonitorAPI.runNow(row.id, { signal: operation.signal }))
    runResults.value = res.results || []
    showRunResult.value = true
    services.notifySuccess(t('admin.channelMonitor.runSuccess'))
    // Refresh row to get latest status from backend
    void reload()
  } catch (err: unknown) {
    if (!operation.isCurrent()) return
    services.notifyError(extractApiErrorMessage(err, t('admin.channelMonitor.runFailed')))
  } finally {
    if (operation.isCurrent()) runningId.value = null
  }
}

async function handleDuplicate(row: ChannelMonitor) {
  if (row.api_key_decrypt_failed) {
    services.notifyError(t('admin.channelMonitor.duplicateKeyUnavailable'))
    return
  }
  if (duplicateCommand.state.value.status === 'pending') return
  const operation = requests.begin('duplicate-result')

  try {
    const duplicate = await duplicateCommand.duplicateMonitor(row.id)
    if (!duplicate || !operation.isCurrent()) return
    services.notifySuccess(t('admin.channelMonitor.duplicateSuccess', { name: duplicate.name }))
    await reload()
  } catch (err: unknown) {
    if (!operation.isCurrent()) return
    services.notifyError(extractApiErrorMessage(err, t('admin.channelMonitor.duplicateFailed')))
  }
}

function handleDelete(row: ChannelMonitor) {
  deleting.value = row
  showDeleteDialog.value = true
}

async function confirmDelete() {
  if (!deleting.value) return
  const operation = requests.begin('delete')
  try {
    await operation.wait(channelMonitorAPI.del(deleting.value.id, { signal: operation.signal }))
    services.notifySuccess(t('admin.channelMonitor.deleteSuccess'))
    showDeleteDialog.value = false
    deleting.value = null
    reload()
  } catch (err: unknown) {
    if (!operation.isCurrent()) return
    services.notifyError(extractApiErrorMessage(err, t('common.error')))
  }
}

watch(adminMonitorTab, (tab) => {
  if (tab === 'legacy' && monitors.value.length === 0) void reload()
})
requests.onScopeChange(() => {
  monitors.value = []; pagination.total = 0; loading.value = false; runningId.value = null
  showDialog.value = false; showTemplateManager.value = false; editing.value = null
  showDeleteDialog.value = false; deleting.value = null; showRunResult.value = false; runResults.value = []
  if (mounted && adminMonitorTab.value === 'legacy') void reload()
})
onMounted(() => {
  mounted = true
  if (adminMonitorTab.value === 'legacy') void reload()
})
onUnmounted(() => {
  if (searchTimeout) clearTimeout(searchTimeout)
  mounted = false; requests.cancel()
})
</script>
