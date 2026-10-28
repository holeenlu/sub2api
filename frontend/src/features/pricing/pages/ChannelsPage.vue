<template>
  <div>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-col justify-between gap-4 lg:flex-row lg:items-start">
          <!-- Left: Search + Filters -->
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
                :placeholder="t('admin.channels.searchChannels', 'Search channels...')"
                class="input pl-10"
                @input="handleSearch"
              />
            </div>

            <Select
              v-model="filters.status"
              :options="statusFilterOptions"
              :placeholder="t('admin.channels.allStatus', 'All Status')"
              class="w-40"
              @change="loadChannels"
            />
          </div>

          <!-- Right: Actions -->
          <div class="flex w-full flex-shrink-0 flex-wrap items-center justify-end gap-3 lg:w-auto">
            <button
              @click="loadChannels"
              :disabled="loading"
              class="btn btn-secondary"
              :title="t('common.refresh', 'Refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button v-if="rolePermissions.can('channels.manage')" @click="openCreateDialog" class="btn btn-primary">
              <Icon name="plus" size="md" class="mr-2" />
              {{ t('admin.channels.createChannel', 'Create Channel') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="channels"
          :loading="loading"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
        >
          <template #cell-name="{ value }">
            <span class="font-medium text-gray-900 dark:text-white">{{ value }}</span>
          </template>

          <template #cell-description="{ value }">
            <span class="text-sm text-gray-600 dark:text-gray-400">{{ value || '-' }}</span>
          </template>

          <template #cell-status="{ row }">
            <Toggle
              :disabled="!canChangeStatus || pendingCommands.has(row.id)" :modelValue="row.status === 'active'"
              @update:modelValue="toggleChannelStatus(row)"
            />
          </template>

          <template #cell-group_count="{ row }">
            <span
              class="inline-flex items-center rounded bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-800 dark:bg-dark-600 dark:text-gray-300"
            >
              {{ (row.group_ids || []).length }}
              {{ t('admin.channels.groupsUnit', 'groups') }}
            </span>
          </template>

          <template #cell-pricing_count="{ row }">
            <span
              class="inline-flex items-center rounded bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-800 dark:bg-dark-600 dark:text-gray-300"
            >
              {{ (row.model_pricing || []).length }}
              {{ t('admin.channels.pricingUnit', 'pricing rules') }}
            </span>
          </template>

          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-600 dark:text-gray-400">
              {{ formatDate(value) }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button
                v-if="rolePermissions.can('channels.manage')" @click="openEditDialog(row)"
                :disabled="pendingCommands.has(row.id)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
              >
                <Icon name="edit" size="sm" />
                <span class="text-xs">{{ t('common.edit', 'Edit') }}</span>
              </button>
              <button
                v-if="rolePermissions.can('channels.manage')" @click="handleDelete(row)"
                :disabled="pendingCommands.has(row.id) || deleting"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
              >
                <Icon name="trash" size="sm" />
                <span class="text-xs">{{ t('common.delete', 'Delete') }}</span>
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.channels.noChannelsYet', 'No Channels Yet')"
              :description="t('admin.channels.createFirstChannel', 'Create your first channel to manage model pricing')"
              :action-text="t('admin.channels.createChannel', 'Create Channel')"
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
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <ChannelEditorDialog v-if="showDialog" :channel="editingChannel" @close="showDialog = false" @saved="loadChannels" />
    <!-- Delete Confirmation -->
    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.channels.deleteChannel', 'Delete Channel')"
      :message="deleteConfirmMessage"
      :confirm-text="deleting ? t('common.processing') : t('common.delete', 'Delete')"
      :cancel-text="t('common.cancel', 'Cancel')"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="cancelDelete"
    />
  </div>
</template>

<script setup lang="ts">
import ChannelEditorDialog from '../components/ChannelEditorDialog.vue'
import { requirePricingPort, pricingNotificationsKey, pricingPermissionsKey } from '../ports'
import { usePricingRequest } from '../queries/requestScope'
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import channelsAPI from '../api/channels'
import type { Channel } from '../api/types'
import type { Column } from '@/shared/ui/tableTypes'

import TablePageLayout from '@/shared/ui/TablePageLayout.vue'
import DataTable from '@/shared/ui/DataTable.vue'
import Pagination from '@/shared/ui/Pagination.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import EmptyState from '@/shared/ui/EmptyState.vue'
import Select from '@/shared/ui/Select.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import Toggle from '@/shared/ui/Toggle.vue'
import { getPersistedPageSize } from '@/shared/lib/usePersistedPageSize'
const rolePermissions = requirePricingPort(pricingPermissionsKey)
const { t } = useI18n()
const notifications = requirePricingPort(pricingNotificationsKey)
// ── Table columns ──
const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.channels.columns.name', 'Name'), sortable: true },
  { key: 'description', label: t('admin.channels.columns.description', 'Description'), sortable: false },
  { key: 'status', label: t('admin.channels.columns.status', 'Status'), sortable: true },
  { key: 'group_count', label: t('admin.channels.columns.groups', 'Groups'), sortable: false },
  { key: 'pricing_count', label: t('admin.channels.columns.pricing', 'Pricing'), sortable: false },
  { key: 'created_at', label: t('admin.channels.columns.createdAt', 'Created'), sortable: true },
  { key: 'actions', label: t('admin.channels.columns.actions', 'Actions'), sortable: false }
])

const statusFilterOptions = computed(() => [
  { value: '', label: t('admin.channels.allStatus', 'All Status') },
  { value: 'active', label: t('admin.channels.statusActive', 'Active') },
  { value: 'disabled', label: t('admin.channels.statusDisabled', 'Disabled') }
])

// ── State ──
const channels = ref<Channel[]>([])
const loading = ref(false)
const searchQuery = ref('')
const filters = reactive({ status: '' })
const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0
})
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

// Dialog state
const showDialog = ref(false)
const editingChannel = ref<Channel | null>(null)
const showDeleteDialog = ref(false)
const deletingChannel = ref<Channel | null>(null)
const pendingCommands = reactive(new Map<number, symbol>())
const deleting = ref(false)
const canChangeStatus = computed(() => rolePermissions.can('channels.manage')
  && rolePermissions.canEditAdminField('channel.update', 'status'))

function formatDate(value: string): string {
  if (!value) return '-'
  return new Date(value).toLocaleDateString()
}

// ── Platform section helpers ──
const deleteConfirmMessage = computed(() => {
  const name = deletingChannel.value?.name || ''
  return t(
    'admin.channels.deleteConfirm',
    { name },
    `Are you sure you want to delete channel "${name}"? This action cannot be undone.`
  )
})

async function loadChannels() {
  const request = listRequest.begin()
  loading.value = true

  try {
    const response = await channelsAPI.list(pagination.page, pagination.page_size, {
      status: filters.status || undefined,
      search: searchQuery.value || undefined,
      sort_by: sortState.sort_by,
      sort_order: sortState.sort_order
    }, { signal: request.signal })

    if (!request.isCurrent()) return
    channels.value = response.items || []
    pagination.total = response.total
  } catch (error: unknown) {
    if (!request.isCurrent()) return
    const e = error as { name?: string; code?: string }
    if (!request.isCurrent() || e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    notifications.showError(extractApiErrorMessage(error, t('admin.channels.loadError', 'Failed to load channels')))
  } finally {
    if (request.isCurrent()) {
      loading.value = false
    }
  }
}

let searchTimeout: ReturnType<typeof setTimeout>
function handleSearch() {
  clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    pagination.page = 1
    loadChannels()
  }, 300)
}

function handlePageChange(page: number) {
  pagination.page = page
  loadChannels()
}

function handlePageSizeChange(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  loadChannels()
}

function handleSort(key: string, order: 'asc' | 'desc') {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  loadChannels()
}

// ── Dialog ──
async function toggleChannelStatus(channel: Channel) {
  if (!canChangeStatus.value) return
  const request = beginChannelCommand(channel.id)
  if (!request) return
  const newStatus = channel.status === 'active' ? 'disabled' : 'active'
  try {
    await channelsAPI.update(channel.id, { status: newStatus })
    if (!request.isCurrent()) return
    // Re-read the current filter; an older list must not overwrite the confirmed
    // command, nor may another row's command invalidate this row's completion.
    await loadChannels()
  } catch (error) {
    if (!request.isCurrent()) return
    notifications.showError(t('admin.channels.updateError', 'Failed to update channel'))
    console.error('Error toggling channel status:', error)
  } finally {
    request.finish()
  }
}

// ── Delete ──
function handleDelete(channel: Channel) {
  if (!rolePermissions.can('channels.manage') || deleting.value || pendingCommands.has(channel.id)) return
  deletingChannel.value = channel
  showDeleteDialog.value = true
}

function cancelDelete() {
  if (deleting.value) return
  showDeleteDialog.value = false
  deletingChannel.value = null
}

async function confirmDelete() {
  if (!rolePermissions.can('channels.manage') || deleting.value || !showDeleteDialog.value) return
  const target = deletingChannel.value
  if (!target) return
  const request = beginChannelCommand(target.id)
  if (!request) return
  deleting.value = true

  try {
    await channelsAPI.remove(target.id)
    if (!request.isCurrent()) return
    notifications.showSuccess(t('admin.channels.deleteSuccess', 'Channel deleted'))
    showDeleteDialog.value = false
    deletingChannel.value = null
    await loadChannels()
  } catch (error: unknown) {
    if (!request.isCurrent()) return
    notifications.showError(extractApiErrorMessage(error, t('admin.channels.deleteError', 'Failed to delete channel')))
  } finally {
    if (request.isCurrent()) deleting.value = false
    request.finish()
  }
}


const mutationRequest = usePricingRequest(() => { pendingCommands.clear(); deleting.value = false })
let mutationLifetime = mutationRequest.begin()
watch(mutationRequest.scope, () => { mutationLifetime = mutationRequest.begin() }, { flush: 'sync' })
function beginChannelCommand(id: number) {
  if (!mutationRequest.scope.value || !mutationLifetime.isCurrent() || pendingCommands.has(id)) return null
  const owner = mutationLifetime
  const token = Symbol('channel-command')
  pendingCommands.set(id, token)
  const isCurrent = () => owner.isCurrent() && pendingCommands.get(id) === token
  return { isCurrent, finish: () => { if (isCurrent()) pendingCommands.delete(id) } }
}
watch([() => rolePermissions.can('channels.manage'), canChangeStatus], () => {
  mutationRequest.cancel()
  mutationLifetime = mutationRequest.begin()
  showDialog.value = false
  editingChannel.value = null
  showDeleteDialog.value = false
  deletingChannel.value = null
}, { flush: 'sync' })
const listRequest = usePricingRequest(() => { channels.value = []; loading.value = false; pagination.total = 0; showDialog.value = false; editingChannel.value = null; showDeleteDialog.value = false; deletingChannel.value = null })
watch(listRequest.scope, () => { clearTimeout(searchTimeout); void loadChannels() })
function openCreateDialog() { if (!rolePermissions.can('channels.manage')) return; editingChannel.value = null; showDialog.value = true }
function openEditDialog(channel: Channel) { if (!rolePermissions.can('channels.manage') || pendingCommands.has(channel.id)) return; editingChannel.value = channel; showDialog.value = true }
onMounted(loadChannels)
onUnmounted(() => { clearTimeout(searchTimeout); listRequest.cancel() })
</script>
