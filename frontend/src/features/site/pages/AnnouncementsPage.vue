<template>
  <div>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <!-- Left: Search + Filters -->
          <div class="flex-1 sm:max-w-64">
            <input
              v-model="searchQuery"
              type="text"
              :placeholder="t('admin.announcements.searchAnnouncements')"
              class="input"
              @input="handleSearch"
            />
          </div>
          <Select
            v-model="filters.status"
            :options="statusFilterOptions"
            class="w-40"
            @change="handleStatusChange"
          />

          <!-- Right: Action buttons -->
          <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
            <button
              @click="loadAnnouncements"
              :disabled="loading"
              class="btn btn-secondary"
              :title="t('common.refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button v-if="canManage" @click="openCreateDialog" class="btn btn-primary">
              <Icon name="plus" size="md" class="mr-1" />
              {{ t('admin.announcements.createAnnouncement') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="announcements"
          :loading="loading"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
        >
          <template #cell-title="{ value, row }">
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <span class="truncate font-medium text-gray-900 dark:text-white">{{ value }}</span>
              </div>
              <div class="mt-1 flex items-center gap-2 text-xs text-gray-500 dark:text-dark-400">
                <span>#{{ row.id }}</span>
                <span class="text-gray-300 dark:text-dark-700">·</span>
                <span>{{ formatDateTime(row.created_at) }}</span>
              </div>
            </div>
          </template>

          <template #cell-status="{ value }">
            <span
              :class="[
                'badge',
                value === 'active'
                  ? 'badge-success'
                  : value === 'draft'
                    ? 'badge-gray'
                    : 'badge-warning'
              ]"
            >
              {{ statusLabel(value) }}
            </span>
          </template>

          <template #cell-notify_mode="{ row }">
            <span
              :class="[
                'badge',
                row.notify_mode === 'popup'
                  ? 'badge-warning'
                  : 'badge-gray'
              ]"
            >
              {{ row.notify_mode === 'popup' ? t('admin.announcements.notifyModeLabels.popup') : t('admin.announcements.notifyModeLabels.silent') }}
            </span>
          </template>

          <template #cell-targeting="{ row }">
            <span class="text-sm text-gray-600 dark:text-gray-300">
              {{ targetingSummary(row.targeting) }}
            </span>
          </template>

          <template #cell-timeRange="{ row }">
            <div class="text-sm text-gray-600 dark:text-gray-300">
              <div>
                <span class="font-medium">{{ t('admin.announcements.form.startsAt') }}:</span>
                <span class="ml-1">{{ row.starts_at ? formatDateTime(row.starts_at) : t('admin.announcements.timeImmediate') }}</span>
              </div>
              <div class="mt-0.5">
                <span class="font-medium">{{ t('admin.announcements.form.endsAt') }}:</span>
                <span class="ml-1">{{ row.ends_at ? formatDateTime(row.ends_at) : t('admin.announcements.timeNever') }}</span>
              </div>
            </div>
          </template>

          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center space-x-1">
              <button
                @click="openPreview(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-blue-50 hover:text-blue-600 dark:hover:bg-blue-900/20 dark:hover:text-blue-400"
                :title="t('admin.announcements.preview')"
              >
                <Icon name="eye" size="sm" />
              </button>
              <button
                @click="openReadStatus(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-blue-50 hover:text-blue-600 dark:hover:bg-blue-900/20 dark:hover:text-blue-400"
                :title="t('admin.announcements.readStatus')"
              >
                <Icon name="chartBar" size="sm" />
              </button>
              <button v-if="canManage"
                @click="openEditDialog(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-600 dark:hover:text-gray-300"
                :title="t('common.edit')"
              >
                <Icon name="edit" size="sm" />
              </button>
              <button v-if="canManage"
                @click="handleDelete(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                :title="t('common.delete')"
              >
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('empty.noData')"
              :description="t('admin.announcements.createFirstAnnouncement')"
              :action-text="t('admin.announcements.createAnnouncement')"
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

    <!-- Create/Edit Dialog -->
    <BaseDialog
      :show="showEditDialog"
      :title="isEditing ? t('admin.announcements.editAnnouncement') : t('admin.announcements.createAnnouncement')"
      width="wide"
      @close="closeEdit"
    >
      <form v-if="canManage" id="announcement-form" @submit.prevent="handleSave" class="space-y-4">
        <div>
          <label class="input-label">{{ t('admin.announcements.form.title') }}</label>
          <input v-model="form.title" type="text" class="input" required />
        </div>

        <div>
          <label class="input-label">{{ t('admin.announcements.form.content') }}</label>
          <textarea v-model="form.content" rows="6" class="input" required></textarea>
        </div>

        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.announcements.form.status') }}</label>
            <Select v-model="form.status" :options="statusOptions" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.announcements.form.notifyMode') }}</label>
            <Select v-model="form.notify_mode" :options="notifyModeOptions" />
            <p class="input-hint">{{ t('admin.announcements.form.notifyModeHint') }}</p>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.announcements.form.startsAt') }}</label>
            <input v-model="form.starts_at_str" type="datetime-local" max="9999-12-31T23:59" class="input" />
            <p class="input-hint">{{ t('admin.announcements.form.startsAtHint') }}</p>
          </div>
          <div>
            <label class="input-label">{{ t('admin.announcements.form.endsAt') }}</label>
            <input v-model="form.ends_at_str" type="datetime-local" max="9999-12-31T23:59" class="input" />
            <p class="input-hint">{{ t('admin.announcements.form.endsAtHint') }}</p>
          </div>
        </div>

        <AnnouncementTargetingEditor
          v-model="form.targeting"
          :groups="subscriptionGroups"
        />
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" @click="closeEdit" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" form="announcement-form" :disabled="saving" class="btn btn-primary">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Delete Confirmation -->
    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.announcements.deleteAnnouncement')"
      :message="t('admin.announcements.deleteConfirm')"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmDelete"
      @cancel="closeDelete"
    />

    <!-- Read Status Dialog -->
    <AnnouncementReadStatusDialog
      :show="showReadStatusDialog"
      :announcement-id="readStatusAnnouncementId"
      @close="showReadStatusDialog = false"
    />

    <AnnouncementPopup
      :announcement="previewAnnouncement"
      preview
      @close="previewAnnouncement = null"
    />
  </div>
</template>

<script setup lang="ts">

import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAnnouncementServices, type AnnouncementAudienceGroup } from '../announcementPorts'
import { useAnnouncementRequests, isAnnouncementCancellation } from '../queries/useAnnouncementRequests'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { createAnnouncementDraft, announcementDraftFrom, createAnnouncementPayload, updateAnnouncementPayload } from '../model/announcementDraft'
import { getPersistedPageSize } from '@/shared/lib/usePersistedPageSize'
import { adminAnnouncementsAPI } from '../api/adminAnnouncements'
import { formatDateTime } from '@/shared/lib/format'
import type { Announcement, AnnouncementTargeting } from '@/features/site/model/announcement'
import type { Column } from '@/shared/ui/tableTypes'

import TablePageLayout from '@/shared/ui/TablePageLayout.vue'
import DataTable from '@/shared/ui/DataTable.vue'
import Pagination from '@/shared/ui/Pagination.vue'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import Select from '@/shared/ui/Select.vue'
import EmptyState from '@/shared/ui/EmptyState.vue'
import Icon from '@/shared/ui/icons/Icon.vue'

import AnnouncementTargetingEditor from '@/features/site/components/announcements/AnnouncementTargetingEditor.vue'
import AnnouncementReadStatusDialog from '@/features/site/components/announcements/AnnouncementReadStatusDialog.vue'
import AnnouncementPopup from '@/features/site/components/announcements/AnnouncementPopup.vue'

const { t } = useI18n()
const services = useAnnouncementServices()
const canManage = services.canManage
const requests = useAnnouncementRequests(services)
let mounted = false

const announcements = ref<Announcement[]>([])
const loading = ref(false)

const filters = reactive({
  status: '',
})
const searchQuery = ref('')

const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})

const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

const statusFilterOptions = computed(() => [
  { value: '', label: t('admin.announcements.allStatus') },
  { value: 'draft', label: t('admin.announcements.statusLabels.draft') },
  { value: 'active', label: t('admin.announcements.statusLabels.active') },
  { value: 'archived', label: t('admin.announcements.statusLabels.archived') }
])

const statusOptions = computed(() => [
  { value: 'draft', label: t('admin.announcements.statusLabels.draft') },
  { value: 'active', label: t('admin.announcements.statusLabels.active') },
  { value: 'archived', label: t('admin.announcements.statusLabels.archived') }
])

const notifyModeOptions = computed(() => [
  { value: 'silent', label: t('admin.announcements.notifyModeLabels.silent') },
  { value: 'popup', label: t('admin.announcements.notifyModeLabels.popup') }
])

const columns = computed<Column[]>(() => [
  { key: 'title', label: t('admin.announcements.columns.title'), sortable: true },
  { key: 'status', label: t('admin.announcements.columns.status'), sortable: true },
  { key: 'notify_mode', label: t('admin.announcements.columns.notifyMode'), sortable: true },
  { key: 'targeting', label: t('admin.announcements.columns.targeting') },
  { key: 'timeRange', label: t('admin.announcements.columns.timeRange') },
  { key: 'created_at', label: t('admin.announcements.columns.createdAt'), sortable: true },
  { key: 'actions', label: t('admin.announcements.columns.actions') }
])

const statusLabel = (status: string) => {
  if (status === 'draft') return t('admin.announcements.statusLabels.draft')
  if (status === 'active') return t('admin.announcements.statusLabels.active')
  if (status === 'archived') return t('admin.announcements.statusLabels.archived')
  return status
}

const targetingSummary = (targeting: AnnouncementTargeting) => {
  const anyOf = targeting?.any_of ?? []
  if (!anyOf || anyOf.length === 0) return t('admin.announcements.targetingSummaryAll')
  return t('admin.announcements.targetingSummaryCustom', { groups: anyOf.length })
}

// ===== CRUD / list =====
async function loadAnnouncements() {
  if (services.scopeKey.value === null) return
  const request = requests.begin('list')
  loading.value = true
  try {
    const result = await request.wait(adminAnnouncementsAPI.list(pagination.page, pagination.page_size, {
      status: filters.status || undefined, search: searchQuery.value || undefined,
      sort_by: sortState.sort_by, sort_order: sortState.sort_order,
    }, { signal: request.signal }))
    announcements.value = result.items
    pagination.total = result.total
    pagination.pages = result.pages
    pagination.page = result.page
    pagination.page_size = result.page_size
  } catch (error: unknown) {
    if (request.isCurrent() && !isAnnouncementCancellation(error)) services.notifyError(extractApiErrorMessage(error, t('admin.announcements.failedToLoad')))
  } finally { if (request.isCurrent()) loading.value = false }
}

function handlePageChange(page: number) {
  pagination.page = page
  loadAnnouncements()
}

function handlePageSizeChange(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  loadAnnouncements()
}

function handleStatusChange() {
  pagination.page = 1
  loadAnnouncements()
}

function handleSort(key: string, order: 'asc' | 'desc') {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  loadAnnouncements()
}

let searchDebounceTimer: number | null = null
function handleSearch() {
  if (searchDebounceTimer) window.clearTimeout(searchDebounceTimer)
  searchDebounceTimer = window.setTimeout(() => {
    pagination.page = 1
    loadAnnouncements()
  }, 300)
}

// ===== Create/Edit dialog =====
const showEditDialog = ref(false)
const saving = ref(false)
const editingAnnouncement = ref<Announcement | null>(null)

const isEditing = computed(() => !!editingAnnouncement.value)

const form = reactive(createAnnouncementDraft())
const subscriptionGroups = ref<AnnouncementAudienceGroup[]>([])

async function loadSubscriptionGroups() {
  if (services.scopeKey.value === null) return
  const request = requests.begin('audience-groups')
  try {
    subscriptionGroups.value = await request.wait(services.listSubscriptionGroups({ signal: request.signal }))
  } catch { /* The server validates targeting; unavailable options do not discard saved targeting. */ }
}

function resetForm() { Object.assign(form, createAnnouncementDraft()) }
function fillFormFromAnnouncement(value: Announcement) { Object.assign(form, announcementDraftFrom(value)) }

function openCreateDialog() {
  if (!canManage.value || services.scopeKey.value === null) return
  closeEdit()
  editingAnnouncement.value = null
  resetForm()
  showEditDialog.value = true
}

function openEditDialog(row: Announcement) {
  if (!canManage.value || services.scopeKey.value === null) return
  closeEdit()
  editingAnnouncement.value = row
  fillFormFromAnnouncement(row)
  showEditDialog.value = true
}

function closeEdit() {
  requests.cancel('save')
  saving.value = false
  resetForm()
  showEditDialog.value = false
  editingAnnouncement.value = null
}

async function handleSave() {
  if (!canManage.value || services.scopeKey.value === null || saving.value) return
  const anyOf = form.targeting?.any_of ?? []
  if (anyOf.length > 50 || anyOf.some(group => (group.all_of ?? []).length > 50)) {
    services.notifyError(t('admin.announcements.failedToCreate'))
    return
  }
  saving.value = true
  const original = editingAnnouncement.value
  const request = requests.begin('save')
  try {
    if (original) {
      await request.wait(adminAnnouncementsAPI.update(original.id, updateAnnouncementPayload(form, original), { signal: request.signal }))
    } else {
      await request.wait(adminAnnouncementsAPI.create(createAnnouncementPayload(form), { signal: request.signal }))
    }
    services.notifySuccess(t('common.success'))
    closeEdit()
    await loadAnnouncements()
  } catch (error: unknown) {
    if (request.isCurrent() && !isAnnouncementCancellation(error)) services.notifyError(extractApiErrorMessage(error, t(original ? 'admin.announcements.failedToUpdate' : 'admin.announcements.failedToCreate')))
  } finally { if (request.isCurrent()) saving.value = false }
}

// ===== Delete =====
const showDeleteDialog = ref(false)
const deletingAnnouncement = ref<Announcement | null>(null)

function handleDelete(row: Announcement) {
  if (!canManage.value || services.scopeKey.value === null) return
  closeDelete()
  deletingAnnouncement.value = row
  showDeleteDialog.value = true
}

const deleting = ref(false)
function closeDelete() {
  requests.cancel('delete')
  deleting.value = false
  showDeleteDialog.value = false
  deletingAnnouncement.value = null
}
async function confirmDelete() {
  if (!canManage.value || services.scopeKey.value === null || !deletingAnnouncement.value || deleting.value) return
  deleting.value = true
  const request = requests.begin('delete')
  try {
    await request.wait(adminAnnouncementsAPI.delete(deletingAnnouncement.value.id, { signal: request.signal }))
    services.notifySuccess(t('common.success'))
    showDeleteDialog.value = false
    deletingAnnouncement.value = null
    await loadAnnouncements()
  } catch (error: unknown) {
    if (request.isCurrent() && !isAnnouncementCancellation(error)) services.notifyError(extractApiErrorMessage(error, t('admin.announcements.failedToDelete')))
  } finally { if (request.isCurrent()) deleting.value = false }
}

// ===== Read status =====
const showReadStatusDialog = ref(false)
const readStatusAnnouncementId = ref<number | null>(null)
const previewAnnouncement = ref<Announcement | null>(null)

function openPreview(row: Announcement) {
  previewAnnouncement.value = row
}

function openReadStatus(row: Announcement) {
  readStatusAnnouncementId.value = row.id
  showReadStatusDialog.value = true
}

requests.onScopeChange(() => {
  if (searchDebounceTimer !== null) window.clearTimeout(searchDebounceTimer)
  searchDebounceTimer = null
  announcements.value = []; subscriptionGroups.value = []
  pagination.page = 1; pagination.total = 0; pagination.pages = 0
  closeEdit(); showDeleteDialog.value = false; deletingAnnouncement.value = null
  showReadStatusDialog.value = false; readStatusAnnouncementId.value = null; previewAnnouncement.value = null
  loading.value = false; deleting.value = false
  if (mounted && services.scopeKey.value !== null) { void loadSubscriptionGroups(); void loadAnnouncements() }
})
onMounted(() => { mounted = true; void loadSubscriptionGroups(); void loadAnnouncements() })
onUnmounted(() => {
  mounted = false
  if (searchDebounceTimer !== null) window.clearTimeout(searchDebounceTimer)
  requests.cancel()
})
</script>
