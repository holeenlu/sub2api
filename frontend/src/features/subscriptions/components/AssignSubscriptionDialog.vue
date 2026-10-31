<template>
    <!-- Assign Subscription Modal -->
    <BaseDialog
      :show="true"
      :title="t('admin.subscriptions.assignSubscription')"
      width="normal"
      :show-close-button="!submitting"
      :close-on-escape="!submitting"
      @close="closeAssignModal"
    >
      <form v-if="canManage"
        id="assign-subscription-form"
        @submit.prevent="handleAssignSubscription"
        class="space-y-5"
      >
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
          <input v-model="batchAssignEnabled" type="checkbox" :disabled="submitting" @change="resetAssignUsers" />
          {{ t('admin.subscriptions.batchAssign.enable') }}
        </label>
        <p v-if="batchAssignEnabled" class="input-hint">{{ t('admin.subscriptions.batchAssign.hint') }}</p>
        <div>
          <label class="input-label">{{ t('admin.subscriptions.form.user') }}</label>
          <div class="relative" data-assign-user-search>
            <input
              v-model="userSearchKeyword"
              type="text"
              :disabled="submitting || (batchAssignEnabled && assignUsers.length >= 100)"
              class="input pr-8"
              :placeholder="t('admin.usage.searchUserPlaceholder')"
              @input="debounceSearchUsers"
              @focus="showUserDropdown = true"
            />
            <button
              v-if="selectedUser"
              @click="clearUserSelection"
              type="button"
              class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
            >
              <Icon name="x" size="sm" :stroke-width="2" />
            </button>
            <!-- User Dropdown -->
            <div
              v-if="showUserDropdown && (userSearchResults.length > 0 || userSearchKeyword)"
              class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800"
            >
              <div
                v-if="userSearchLoading"
                class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400"
              >
                {{ t('common.loading') }}
              </div>
              <div
                v-else-if="userSearchResults.length === 0 && userSearchKeyword"
                class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400"
              >
                {{ t('common.noOptionsFound') }}
              </div>
              <button
                v-for="user in userSearchResults"
                :key="user.id"
                type="button"
                :disabled="submitting || (batchAssignEnabled && assignUsers.some((selected) => selected.id === user.id))"
                @click="selectUser(user)"
                class="w-full px-4 py-2 text-left text-sm hover:bg-gray-100 disabled:opacity-50 dark:hover:bg-dark-700"
              >
                <span class="font-medium text-gray-900 dark:text-white">{{ user.email }}</span>
                <span class="ml-2 text-gray-500 dark:text-gray-400">#{{ user.id }}</span>
              </button>
            </div>
          </div>
          <div v-if="batchAssignEnabled && assignUsers.length > 0" class="mt-2 space-y-2" data-test="assign-users">
            <p class="text-sm text-gray-600 dark:text-gray-400">
              {{ t('admin.subscriptions.batchAssign.selected', { count: assignUsers.length }) }}
            </p>
            <ul class="max-h-40 space-y-1 overflow-y-auto">
              <li v-for="user in assignUsers" :key="user.id" class="flex items-center justify-between gap-2 rounded-lg bg-gray-50 px-3 py-1 text-sm dark:bg-dark-700">
                <span class="truncate">{{ user.email }} <span class="text-gray-500">#{{ user.id }}</span></span>
                <button
                  type="button"
                  :disabled="submitting"
                  :aria-label="t('admin.subscriptions.batchAssign.removeUser', { email: user.email })"
                  @click="assignUsers = assignUsers.filter((selected) => selected.id !== user.id)"
                >
                  <Icon name="x" size="sm" />
                </button>
              </li>
            </ul>
          </div>
        </div>
        <div>
          <label class="input-label">{{ t('admin.subscriptions.form.group') }}</label>
          <Select
            v-model="assignForm.group_id"
            :disabled="submitting"
            :options="subscriptionGroupOptions"
            :placeholder="t('admin.subscriptions.selectGroup')"
          >
            <template #selected="{ option }">
              <GroupBadge
                v-if="option"
                :name="option.label"
                :platform="option.platform"
                :subscription-type="option.subscriptionType"
                :rate-multiplier="option.rate"
              />
              <span v-else class="text-gray-400">{{ t('admin.subscriptions.selectGroup') }}</span>
            </template>
            <template #option="{ option, selected }">
              <GroupOptionItem
                v-if="!('_creatable' in option)"
                :name="option.label"
                :platform="option.platform"
                :subscription-type="option.subscriptionType"
                :rate-multiplier="option.rate"
                :description="option.description"
                :selected="selected"
              />
            </template>
          </Select>
          <p class="input-hint">{{ t('admin.subscriptions.groupHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.subscriptions.form.validityDays') }}</label>
          <input v-model.number="assignForm.validity_days" type="number" min="1" max="36500" step="1" :disabled="submitting" class="input" />
          <p class="input-hint">{{ t('admin.subscriptions.validityHint') }}</p>
        </div>
        <div v-if="batchAssignResult" class="space-y-2 text-sm" role="status" data-test="batch-assign-result">
          <p>{{ t('admin.subscriptions.batchAssign.result', { success: batchAssignResult.success_count, failed: batchAssignResult.failed_count }) }}</p>
          <ul v-if="batchAssignResult.errors.length" class="max-h-40 space-y-1 overflow-y-auto text-red-600 dark:text-red-400">
            <li v-for="(error, index) in batchAssignResult.errors" :key="index">{{ error }}</li>
          </ul>
          <p v-if="batchAssignResult.failed_count > 0" class="input-hint">{{ t('admin.subscriptions.batchAssign.retryHint') }}</p>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button @click="closeAssignModal" type="button" :disabled="submitting" class="btn btn-secondary">
            {{ batchAssignResult ? t('common.close') : t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="assign-subscription-form"
            :disabled="submitting || (batchAssignEnabled && assignUsers.length === 0)"
            class="btn btn-primary"
          >
            <svg
              v-if="submitting"
              class="-ml-1 mr-2 h-4 w-4 animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            {{ submitting ? t('admin.subscriptions.assigning') : t('admin.subscriptions.assign') }}
          </button>
        </div>
      </template>
    </BaseDialog>

</template>
<script setup lang="ts">
import { ref, reactive, computed, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Group, GroupPlatform } from '@/features/access-policy/api/groupTypes';
import type { SubscriptionType } from '@/features/subscriptions/api/subscriptionPolicyTypes';
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { subscriptionsAPI } from '../api/admin'
import type { BulkAssignSubscriptionResult } from '../api/admin'
import { requireSubscriptionPort, subscriptionDirectoryKey, subscriptionNotificationsKey, subscriptionCanManageKey, type SubscriptionUserOption } from '../ports'
import { useSubscriptionRequest } from '../queries/requestScope'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import Select from '@/shared/ui/Select.vue'
import { subscriptionPresentationKey } from '../ports'
const { GroupBadge, GroupOptionItem } = requireSubscriptionPort(subscriptionPresentationKey).components
import Icon from '@/shared/ui/icons/Icon.vue'
const props = defineProps<{ groups: Group[] }>()
const emit = defineEmits<{ close: []; completed: [] }>()
const { t } = useI18n()
const directory = requireSubscriptionPort(subscriptionDirectoryKey)
const notifications = requireSubscriptionPort(subscriptionNotificationsKey)
const canManage = requireSubscriptionPort(subscriptionCanManageKey)
const submitting = ref(false)
interface GroupOption { value: number; label: string; description: string | null; platform: GroupPlatform; subscriptionType: SubscriptionType; rate: number }
// User search state
const userSearchKeyword = ref('')
const userSearchResults = ref<SubscriptionUserOption[]>([])
const userSearchLoading = ref(false)
const showUserDropdown = ref(false)
const selectedUser = ref<SubscriptionUserOption | null>(null)
const batchAssignEnabled = ref(false)
const assignUsers = ref<SubscriptionUserOption[]>([])
const batchAssignResult = ref<BulkAssignSubscriptionResult | null>(null)
let userSearchTimeout: ReturnType<typeof setTimeout> | null = null

const assignForm = reactive({
  user_id: null as number | null,
  group_id: null as number | null,
  validity_days: 30
})

// Group options for assign (only subscription type groups)
const subscriptionGroupOptions = computed<GroupOption[]>(() =>
  props.groups
    .filter((g) => g.subscription_type === 'subscription' && g.status === 'active')
    .map((g) => ({
      value: g.id,
      label: g.name,
      description: g.description,
      platform: g.platform,
      subscriptionType: g.subscription_type,
      rate: g.rate_multiplier
    }))
)

// User search with debounce
const debounceSearchUsers = () => {
  searchRequest.cancel()
  // Invalidate the assignment target before the debounced search runs.
  if (selectedUser.value && userSearchKeyword.value.trim() !== selectedUser.value.email) {
    selectedUser.value = null
    assignForm.user_id = null
  }
  if (userSearchTimeout) {
    clearTimeout(userSearchTimeout)
  }
  userSearchTimeout = setTimeout(searchUsers, 300)
}

const searchUsers = async () => {
  const keyword = userSearchKeyword.value.trim()

  if (!keyword) {
    userSearchResults.value = []
    return
  }

  const request = searchRequest.begin()
  userSearchLoading.value = true
  try {
    const result = await directory.searchAssignableUsers(keyword, request.signal)
    if (!request.isCurrent()) return
    userSearchResults.value = result
  } catch (error) {
    if (!request.isCurrent()) return
    console.error('Failed to search users:', error)
    userSearchResults.value = []
  } finally {
    if (request.isCurrent()) userSearchLoading.value = false
  }
}

const selectUser = (user: SubscriptionUserOption) => {
  if (submitting.value) return
  if (batchAssignEnabled.value) {
    if (assignUsers.value.length < 100 && !assignUsers.value.some((selected) => selected.id === user.id)) {
      assignUsers.value = [...assignUsers.value, user]
    }
    userSearchKeyword.value = ''
    userSearchResults.value = []
    showUserDropdown.value = false
    return
  }
  selectedUser.value = user
  userSearchKeyword.value = user.email
  showUserDropdown.value = false
  assignForm.user_id = user.id
}

const clearUserSelection = () => {
  selectedUser.value = null
  userSearchKeyword.value = ''
  userSearchResults.value = []
  assignForm.user_id = null
}

const resetAssignUsers = () => {
  clearUserSelection()
  assignUsers.value = []
  batchAssignResult.value = null
}

const closeAssignModal = () => {
  if (submitting.value) return
  emit('close')
  batchAssignEnabled.value = false
  assignUsers.value = []
  batchAssignResult.value = null
  assignForm.user_id = null
  assignForm.group_id = null
  assignForm.validity_days = 30
  // Clear user search state
  selectedUser.value = null
  userSearchKeyword.value = ''
  userSearchResults.value = []
  showUserDropdown.value = false
}

const handleAssignSubscription = async () => {
  if (!canManage.value) return
  if (submitting.value) return
  if (batchAssignEnabled.value ? assignUsers.value.length === 0 : !assignForm.user_id) {
    notifications.showError(t('admin.subscriptions.pleaseSelectUser'))
    return
  }
  if (!assignForm.group_id) {
    notifications.showError(t('admin.subscriptions.pleaseSelectGroup'))
    return
  }
  if (!Number.isInteger(assignForm.validity_days) || assignForm.validity_days < 1 || assignForm.validity_days > 36500) {
    notifications.showError(t('admin.subscriptions.validityDaysRequired'))
    return
  }

  const command = commandRequest.begin()
  submitting.value = true
  try {
    if (batchAssignEnabled.value) {
      const result = await subscriptionsAPI.bulkAssign({
        user_ids: assignUsers.value.map((user) => user.id),
        group_id: assignForm.group_id,
        validity_days: assignForm.validity_days
      })
      if (!command.isCurrent()) return
      batchAssignResult.value = result
      const successIds = new Set(result.subscriptions.map((subscription) => subscription.user_id))
      assignUsers.value = assignUsers.value.filter((user) => !successIds.has(user.id))
      if (result.success_count > 0) {
        notifications.showSuccess(t('admin.subscriptions.batchAssign.result', { success: result.success_count, failed: result.failed_count }))
        emit('completed')
      }
      return
    }
    await subscriptionsAPI.assign({
      user_id: assignForm.user_id!,
      group_id: assignForm.group_id,
      validity_days: assignForm.validity_days
    })
    if (!command.isCurrent()) return
    notifications.showSuccess(t('admin.subscriptions.subscriptionAssigned'))
    submitting.value = false
    closeAssignModal()
    emit('completed')
  } catch (error: unknown) {
    if (!command.isCurrent()) return
    notifications.showError(extractApiErrorMessage(error, t('admin.subscriptions.failedToAssign')))
    console.error('Error assigning subscription:', error)
  } finally {
    if (command.isCurrent()) submitting.value = false
  }
}


const searchRequest = useSubscriptionRequest(() => { userSearchLoading.value = false; userSearchResults.value = [] })
const commandRequest = useSubscriptionRequest(() => { submitting.value = false; emit('close') })
onUnmounted(() => { if (userSearchTimeout) clearTimeout(userSearchTimeout) })
</script>
