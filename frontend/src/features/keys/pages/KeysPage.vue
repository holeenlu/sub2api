<script setup lang="ts">
import { ref, computed, watch, onMounted, onScopeDispose } from 'vue'
import { useI18n } from 'vue-i18n'
import TablePageLayout from '@/shared/ui/TablePageLayout.vue'
import Pagination from '@/shared/ui/Pagination.vue'
import Select from '@/shared/ui/Select.vue'
import SearchInput from '@/shared/ui/SearchInput.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { keysAPI } from '../api/keys'
import type { ApiKey, UpdateApiKeyRequest } from '../api/types'
import { useKeysServices } from '../services'
import { useKeysRequest, keyErrorMessage } from '../queries/useKeysRequest'
import { useKeyList } from '../queries/useKeyList'
import { useKeyGroups } from '../queries/useKeyGroups'
import { useKeyColumns } from '../model/useKeyColumns'
import KeysTable from '../components/KeysTable.vue'
import KeyEditorDialog from '../components/KeyEditorDialog.vue'
import BulkEditKeysModal from '../components/BulkEditKeysModal.vue'
import KeyDataTransfer from '../components/KeyDataTransfer.vue'
import EndpointPopover from '../components/EndpointPopover.vue'
import UseKeyModal from '../components/UseKeyModal.vue'

const { t } = useI18n()
const services = useKeysServices()
const publicSettings = services.publicSettings
const request = useKeysRequest(services)
const { apiKeys, selectedIds, selectedApiKeys, usageStats, state, loading, pagination, filters,
  filterSearch, filterStatus, filterGroupId, loadApiKeys, handleSelectionChange, handleBulkUpdated, applyUpdatedKey,
  onFilterChange, onGroupFilterChange, onStatusFilterChange, handlePageChange, handlePageSizeChange, handleSort } = useKeyList()
const { groups, userGroupRates, groupOptions, groupPricingLoading, loadGroupPricing } = useKeyGroups()
const { columns, toggleableColumns, toggleColumn, isColumnVisible } = useKeyColumns()
const groupFilterOptions = computed(() => [
  { value: '', label: t('keys.allGroups') }, { value: 0, label: t('keys.noGroup') }, ...groups.value.map(group => ({ value: group.id, label: group.name }))
])
const statusFilterOptions = computed(() => [{ value: '', label: t('keys.allStatus') },
  ...['active', 'inactive', 'quota_exhausted', 'expired'].map(value => ({ value, label: t('keys.status.' + value) }))])
const showBulkEditModal = ref(false)
const showColumnDropdown = ref(false)
const columnDropdownRef = ref<HTMLElement | null>(null)
const editor = ref<{ key: ApiKey | null } | null>(null)
const guideKey = ref<ApiKey | null>(null)
const confirmation = ref<{ kind: 'delete' | 'rate'; key: ApiKey } | null>(null)
const pending = ref(false)
function openCreate(): void { editor.value = { key: null }; void loadGroupPricing() }
function editKey(key: ApiKey): void { editor.value = { key }; void loadGroupPricing() }
function editorSaved(): void { editor.value = null; void loadApiKeys() }
function confirmDelete(key: ApiKey): void { confirmation.value = { kind: 'delete', key } }
function confirmResetRate(key: ApiKey): void { confirmation.value = { kind: 'rate', key } }
async function refreshPage(): Promise<void> { await Promise.all([loadApiKeys(), loadGroupPricing()]) }
async function updateKey(key: ApiKey, changes: UpdateApiKeyRequest, success: string, failure: string): Promise<void> {
  if (pending.value) return
  const ticket = request.begin()
  if (!ticket.isCurrent()) return
  const ownedConfirmation = confirmation.value
  pending.value = true
  try {
    await keysAPI.update(key.id, changes, ticket)
    if (!ticket.isCurrent()) return
    services.notifySuccess(t(success))
    if (confirmation.value === ownedConfirmation) confirmation.value = null
    void loadApiKeys()
  } catch (error) {
    if (ticket.isCurrent()) services.notifyError(keyErrorMessage(error, t(failure)))
  } finally { if (ticket.isCurrent()) pending.value = false }
}
function toggleKeyStatus(key: ApiKey): void {
  const status = key.status === 'active' ? 'inactive' : 'active'
  void updateKey(key, { status }, status === 'active' ? 'keys.keyEnabledSuccess' : 'keys.keyDisabledSuccess', 'keys.failedToUpdateStatus')
}
function changeGroup(key: ApiKey, groupId: number): void {
  void updateKey(key, { group_id: groupId }, 'keys.groupChangedSuccess', 'keys.failedToChangeGroup')
}
async function confirmAction(): Promise<void> {
  const action = confirmation.value
  if (!action || pending.value) return
  if (action.kind === 'rate') { await updateKey(action.key, { reset_rate_limit_usage: true }, 'keys.rateLimitResetSuccess', 'keys.failedToResetRateLimit'); return }
  const ticket = request.begin()
  if (!ticket.isCurrent()) return
  pending.value = true
  try {
    await keysAPI.delete(action.key.id, ticket)
    if (!ticket.isCurrent()) return
    services.notifySuccess(t('keys.keyDeletedSuccess'))
    if (confirmation.value === action) confirmation.value = null
    void loadApiKeys()
  } catch (error) { if (ticket.isCurrent()) services.notifyError(keyErrorMessage(error, t('keys.failedToDelete'))) }
  finally { if (ticket.isCurrent()) pending.value = false }
}
function closeColumns(event: MouseEvent): void {
  if (event.target instanceof Node && !columnDropdownRef.value?.contains(event.target)) showColumnDropdown.value = false
}
watch(services.scopeKey, () => { editor.value = null; guideKey.value = null; confirmation.value = null; showBulkEditModal.value = false; pending.value = false }, { flush: 'sync' })
onMounted(() => document.addEventListener('click', closeColumns))
onScopeDispose(() => document.removeEventListener('click', closeColumns))
</script>
<template>
  <section>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-col gap-3">
          <div class="flex flex-wrap items-center gap-3">
            <SearchInput
              v-model="filterSearch"
              :placeholder="t('keys.searchPlaceholder')"
              class="w-full sm:w-64"
              @search="onFilterChange"
            />
            <Select
              :model-value="filterGroupId"
              class="w-40"
              :options="groupFilterOptions"
              @update:model-value="onGroupFilterChange"
            />
            <Select
              :model-value="filterStatus"
              class="w-40"
              :options="statusFilterOptions"
              @update:model-value="onStatusFilterChange"
            />
          </div>
          <EndpointPopover
            v-if="publicSettings?.api_base_url || (publicSettings?.custom_endpoints?.length ?? 0) > 0"
            :api-base-url="publicSettings?.api_base_url || ''"
            :custom-endpoints="publicSettings?.custom_endpoints || []"
          />
          <div v-if="selectedIds.length" class="flex flex-wrap items-center gap-3 text-sm">
            <span class="text-gray-600 dark:text-gray-300">
              {{ t('keys.bulkEdit.selectedCount', { count: selectedIds.length }) }}
            </span>
            <button
              class="btn btn-primary btn-sm"
              :disabled="loading"
              data-test="bulk-edit-keys"
              @click="showBulkEditModal = true"
            >
              {{ t('keys.bulkEdit.title') }}
            </button>
            <button class="btn btn-secondary btn-sm" @click="selectedIds = []">
              {{ t('keys.bulkEdit.clearSelection') }}
            </button>
          </div>
        </div>
      </template>

      <template #actions>
        <div class="flex flex-wrap justify-end gap-3">
          <KeyDataTransfer
            :selected-ids="selectedIds"
            :filters="filters"
            :disabled="loading"
            @imported="loadApiKeys"
          />
          <button
            @click="refreshPage"
            :disabled="loading || groupPricingLoading"
            class="btn btn-secondary"
            :title="t('common.refresh')"
          >
            <Icon name="refresh" size="md" :class="loading || groupPricingLoading ? 'animate-spin' : ''" />
          </button>
          <div class="relative" ref="columnDropdownRef">
            <button
              @click="showColumnDropdown = !showColumnDropdown"
              class="btn btn-secondary px-2 md:px-3"
              :title="t('keys.columnSettings')"
            >
              <svg class="h-4 w-4 md:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 4.5v15m6-15v15m-10.875 0h15.75c.621 0 1.125-.504 1.125-1.125V5.625c0-.621-.504-1.125-1.125-1.125H4.125C3.504 4.5 3 5.004 3 5.625v12.75c0 .621.504 1.125 1.125 1.125z" />
              </svg>
              <span class="hidden md:inline">{{ t('keys.columnSettings') }}</span>
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
          <button @click="openCreate" class="btn btn-primary" data-tour="keys-create-btn">
            <Icon name="plus" size="md" class="mr-2" />
            {{ t('keys.createKey') }}
          </button>
        </div>
      </template>

      <template #table>
        <p v-if="state === 'error'" role="alert" class="p-4 text-red-600">{{ t('keys.failedToLoad') }}</p>
        <KeysTable v-if="state !== 'error' || apiKeys.length > 0" :columns="columns" :api-keys="apiKeys" :loading="loading" :selected-ids="selectedIds"
          :usage-stats="usageStats" :user-group-rates="userGroupRates" :group-options="groupOptions"
          @selection="handleSelectionChange" @sort="handleSort" @create="openCreate" @edit="editKey"
          @remove="confirmDelete" @toggle="toggleKeyStatus" @use="guideKey = $event" @reset-rate="confirmResetRate"
          @group="changeGroup" @refresh-groups="loadGroupPricing" />
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


    <KeyEditorDialog :show="editor !== null" :api-key="editor?.key ?? null" :group-options="groupOptions" @close="editor = null" @saved="editorSaved" @changed="applyUpdatedKey" />
    <BulkEditKeysModal :show="showBulkEditModal" :selected-keys="selectedApiKeys" :groups="groups"
      @close="showBulkEditModal = false" @updated="handleBulkUpdated" />
    <ConfirmDialog :show="confirmation !== null" :title="t(confirmation?.kind === 'delete' ? 'keys.deleteKey' : 'keys.resetRateLimitTitle')"
      :message="t(confirmation?.kind === 'delete' ? 'keys.deleteConfirmMessage' : 'keys.resetRateLimitConfirmMessage', { name: confirmation?.key.name })"
      :confirm-text="t(confirmation?.kind === 'delete' ? 'common.delete' : 'keys.reset')" :cancel-text="t('common.cancel')" danger
      @confirm="confirmAction" @cancel="confirmation = null" />
    <UseKeyModal :show="guideKey !== null" :api-key="guideKey?.key ?? ''" :base-url="publicSettings?.api_base_url ?? ''"
      :platform="guideKey?.group?.platform ?? null" :claude-code-only="guideKey?.group?.claude_code_only ?? false"
      :allow-messages-dispatch="guideKey?.group?.allow_messages_dispatch ?? false" @close="guideKey = null" />
  </section>
</template>
