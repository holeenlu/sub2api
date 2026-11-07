<template>
    <!-- Adjust Subscription Modal -->
    <BaseDialog
      :show="showExtendModal"
      :title="t('admin.subscriptions.adjustSubscription')"
      width="narrow"
      @close="closeExtendModal"
    >
      <form
        v-if="(extendingSubscription) &amp;&amp; (canManage)"
        id="extend-subscription-form"
        @submit.prevent="handleExtendSubscription"
        class="space-y-5"
      >
        <div class="rounded-lg bg-gray-50 p-4 dark:bg-dark-700">
          <p class="text-sm text-gray-600 dark:text-gray-400">
            {{ t('admin.subscriptions.adjustingFor') }}
            <span class="font-medium text-gray-900 dark:text-white">{{
              extendingSubscription.user?.email
            }}</span>
          </p>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
            {{ t('admin.subscriptions.currentExpiration') }}:
            <span class="font-medium text-gray-900 dark:text-white">
              {{
                extendingSubscription.expires_at
                  ? formatDateTimeToMinute(extendingSubscription.expires_at)
                  : t('admin.subscriptions.noExpiration')
              }}
            </span>
          </p>
          <p v-if="extendingSubscription.expires_at" class="mt-1 text-sm text-gray-600 dark:text-gray-400">
            {{ t('admin.subscriptions.remainingDays') }}:
            <span class="font-medium text-gray-900 dark:text-white">
              {{ getDaysRemaining(extendingSubscription.expires_at) ?? 0 }}
            </span>
          </p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.subscriptions.form.adjustDays') }}</label>
          <div class="flex items-center gap-2">
            <input
              v-model.number="extendForm.days"
              type="number"
              required
              class="input text-center"
              :placeholder="t('admin.subscriptions.adjustDaysPlaceholder')"
            />
          </div>
          <p class="input-hint">{{ t('admin.subscriptions.adjustHint') }}</p>
        </div>
      </form>
      <template #footer>
        <div v-if="extendingSubscription" class="flex justify-end gap-3">
          <button @click="closeExtendModal" type="button" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="extend-subscription-form"
            :disabled="submitting"
            class="btn btn-primary"
          >
            {{ submitting ? t('admin.subscriptions.adjusting') : t('admin.subscriptions.adjust') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Revoke Confirmation Dialog -->
    <ConfirmDialog
      :show="showRevokeDialog"
      :title="t('admin.subscriptions.revokeSubscription')"
      :message="t('admin.subscriptions.revokeConfirm', { user: revokingSubscription?.user?.email })"
      :confirm-text="t('admin.subscriptions.revoke')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmRevoke"
      @cancel="showRevokeDialog = false"
    />

    <!-- Restore Confirmation Dialog -->
    <ConfirmDialog
      :show="showRestoreDialog"
      :title="t('admin.subscriptions.restoreSubscription')"
      :message="t('admin.subscriptions.restoreConfirm', { user: restoringSubscription?.user?.email })"
      :confirm-text="t('admin.subscriptions.restore')"
      :cancel-text="t('common.cancel')"
      @confirm="confirmRestore"
      @cancel="showRestoreDialog = false"
    />

    <!-- Reset Quota Confirmation Dialog -->
    <ConfirmDialog
      :show="showResetQuotaConfirm"
      :title="t('admin.subscriptions.resetQuotaTitle')"
      :message="t('admin.subscriptions.resetQuotaConfirm', { user: resettingSubscription?.user?.email })"
      :confirm-text="t('admin.subscriptions.resetQuota')"
      :cancel-text="t('common.cancel')"
      @confirm="confirmResetQuota"
      @cancel="showResetQuotaConfirm = false"
    />
</template>
<script setup lang="ts">
import { ref, reactive, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserSubscription } from '../api/types'
import { subscriptionsAPI } from '../api/admin'
import { requireSubscriptionPort, subscriptionNotificationsKey, subscriptionCanManageKey } from '../ports'
import { useSubscriptionRequest } from '../queries/requestScope'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { formatDateTimeToMinute } from '@/shared/lib/format'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
const emit = defineEmits<{ completed: [] }>()
const { t } = useI18n()
const notifications = requireSubscriptionPort(subscriptionNotificationsKey)
const canManage = requireSubscriptionPort(subscriptionCanManageKey)
const showExtendModal = ref(false)
const showRevokeDialog = ref(false)
const showRestoreDialog = ref(false)
const showResetQuotaConfirm = ref(false)
const submitting = ref(false)
const resettingSubscription = ref<UserSubscription | null>(null)
const resettingQuota = ref(false)
const extendingSubscription = ref<UserSubscription | null>(null)
const revokingSubscription = ref<UserSubscription | null>(null)
const restoringSubscription = ref<UserSubscription | null>(null)

const extendForm = reactive({
  days: 30
})

const handleExtend = (subscription: UserSubscription) => {
  extendingSubscription.value = subscription
  extendForm.days = 30
  showExtendModal.value = true
}

const closeExtendModal = () => {
  showExtendModal.value = false
  extendingSubscription.value = null
}

const handleExtendSubscription = async () => {
  if (!extendingSubscription.value || submitting.value || !canManage.value) return

  // 前端验证：调整后的过期时间必须在未来
  if (extendingSubscription.value.expires_at) {
    const expiresAt = new Date(extendingSubscription.value.expires_at)
    const newExpiresAt = new Date(expiresAt.getTime() + extendForm.days * 24 * 60 * 60 * 1000)
    if (newExpiresAt <= new Date()) {
      notifications.showError(t('admin.subscriptions.adjustWouldExpire'))
      return
    }
  }

  submitting.value = true
  const command = commandRequest.begin()
  try {
    await subscriptionsAPI.extend(extendingSubscription.value.id, {
      days: extendForm.days
    })
    if (!command.isCurrent()) return
    notifications.showSuccess(t('admin.subscriptions.subscriptionAdjusted'))
    closeExtendModal()
    emit('completed')
  } catch (error: unknown) {
    if (!command.isCurrent()) return
    notifications.showError(extractApiErrorMessage(error, t('admin.subscriptions.failedToAdjust')))
    console.error('Error adjusting subscription:', error)
  } finally {
    if (command.isCurrent()) submitting.value = false
  }
}

const handleRevoke = (subscription: UserSubscription) => {
  revokingSubscription.value = subscription
  showRevokeDialog.value = true
}

const confirmRevoke = async () => {
  if (!revokingSubscription.value || submitting.value || !canManage.value) return

  submitting.value = true
  const command = commandRequest.begin()
  try {
    await subscriptionsAPI.revoke(revokingSubscription.value.id)
    if (!command.isCurrent()) return
    notifications.showSuccess(t('admin.subscriptions.subscriptionRevoked'))
    showRevokeDialog.value = false
    revokingSubscription.value = null
    emit('completed')
  } catch (error: unknown) {
    if (!command.isCurrent()) return
    notifications.showError(extractApiErrorMessage(error, t('admin.subscriptions.failedToRevoke')))
    console.error('Error revoking subscription:', error)
  } finally {
    if (command.isCurrent()) submitting.value = false
  }
}

const handleRestore = (subscription: UserSubscription) => {
  restoringSubscription.value = subscription
  showRestoreDialog.value = true
}

const confirmRestore = async () => {
  if (!restoringSubscription.value || submitting.value || !canManage.value) return

  submitting.value = true
  const command = commandRequest.begin()
  try {
    await subscriptionsAPI.restore(restoringSubscription.value.id)
    if (!command.isCurrent()) return
    notifications.showSuccess(t('admin.subscriptions.subscriptionRestored'))
    showRestoreDialog.value = false
    restoringSubscription.value = null
    emit('completed')
  } catch (error: unknown) {
    if (!command.isCurrent()) return
    notifications.showError(extractApiErrorMessage(error, t('admin.subscriptions.failedToRestore')))
    console.error('Error restoring subscription:', error)
  } finally {
    if (command.isCurrent()) submitting.value = false
  }
}

const handleResetQuota = (subscription: UserSubscription) => {
  resettingSubscription.value = subscription
  showResetQuotaConfirm.value = true
}

const confirmResetQuota = async () => {
  if (!resettingSubscription.value || !canManage.value) return
  if (resettingQuota.value) return
  resettingQuota.value = true
  const command = commandRequest.begin()
  try {
    await subscriptionsAPI.resetQuota(resettingSubscription.value.id, { daily: true, weekly: true, monthly: true })
    if (!command.isCurrent()) return
    notifications.showSuccess(t('admin.subscriptions.quotaResetSuccess'))
    showResetQuotaConfirm.value = false
    resettingSubscription.value = null
    emit('completed')
  } catch (error: unknown) {
    if (!command.isCurrent()) return
    notifications.showError(extractApiErrorMessage(error, t('admin.subscriptions.failedToResetQuota')))
    console.error('Error resetting quota:', error)
  } finally {
    if (command.isCurrent()) resettingQuota.value = false
  }
}

const getDaysRemaining = (expiresAt: string): number | null => {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  if (diff < 0) return null
  return Math.ceil(diff / (1000 * 60 * 60 * 24))
}


const commandRequest = useSubscriptionRequest(() => { submitting.value = false; resettingQuota.value = false; showExtendModal.value = false; showRevokeDialog.value = false; showRestoreDialog.value = false; showResetQuotaConfirm.value = false; extendingSubscription.value = null; revokingSubscription.value = null; restoringSubscription.value = null; resettingSubscription.value = null })
function open(action: 'extend' | 'revoke' | 'restore' | 'reset_quota', subscription: UserSubscription) {
 if (!canManage.value || submitting.value || resettingQuota.value) return
 if (action === 'extend') handleExtend(subscription)
 else if (action === 'revoke') handleRevoke(subscription)
 else if (action === 'restore') handleRestore(subscription)
 else handleResetQuota(subscription)
}
defineExpose({ open, busy: computed(() => submitting.value || resettingQuota.value) })
</script>
