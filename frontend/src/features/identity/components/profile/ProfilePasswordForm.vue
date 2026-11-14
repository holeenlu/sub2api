<template>
  <div :class="props.embedded ? 'space-y-4' : 'card'">
    <div
      v-if="!props.embedded"
      class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
    >
      <h2 class="text-lg font-medium text-gray-900 dark:text-white">
        {{ t('profile.changePassword') }}
      </h2>
    </div>
    <div :class="props.embedded ? '' : 'px-6 py-6'">
      <form @submit.prevent="handleChangePassword" class="space-y-4">
        <div v-if="props.embedded">
          <p class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('profile.changePassword') }}
          </p>
        </div>
        <div>
          <label for="old_password" class="input-label">
            {{ t('profile.currentPassword') }}
          </label>
          <input
            id="old_password"
            v-model="form.old_password"
            type="password"
            required
            autocomplete="current-password"
            class="input"
          />
        </div>

        <div>
          <label for="new_password" class="input-label">
            {{ t('profile.newPassword') }}
          </label>
          <input
            id="new_password"
            v-model="form.new_password"
            type="password"
            required
            autocomplete="new-password"
            class="input"
          />
          <p class="input-hint">
            {{ t('profile.passwordHint') }}
          </p>
        </div>

        <div>
          <label for="confirm_password" class="input-label">
            {{ t('profile.confirmNewPassword') }}
          </label>
          <input
            id="confirm_password"
            v-model="form.confirm_password"
            type="password"
            required
            autocomplete="new-password"
            class="input"
          />
        </div>

        <div class="flex justify-end pt-4">
          <button type="submit" :disabled="loading" class="btn btn-primary">
            {{ loading ? t('profile.changingPassword') : t('profile.changePasswordButton') }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onScopeDispose } from 'vue'
import { isCancel } from 'axios'
import { useI18n } from 'vue-i18n'
import { useIdentityNotifications } from '../../ports'
import { profileAPI } from '../../api/profile'
import { useAuthStore } from '../../model/auth'
import { useIdentityRequest } from '../../queries/useIdentityRequest'
import { extractApiErrorMessage } from '@/shared/lib/apiError'

const { t } = useI18n()
const notifications = useIdentityNotifications()
const auth = useAuthStore()
const context = () => [auth.identityScopeKey, auth.isAuthenticated]
const writes = useIdentityRequest(context)
const props = withDefaults(defineProps<{
  embedded?: boolean
}>(), {
  embedded: false,
})

const loading = ref(false)
const form = ref({
  old_password: '',
  new_password: '',
  confirm_password: ''
})

const handleChangePassword = async () => {
  if (!auth.isAuthenticated || loading.value) return
  if (form.value.new_password !== form.value.confirm_password) {
    notifications.showError(t('profile.passwordsNotMatch'))
    return
  }

  if (form.value.new_password.length < 8) {
    notifications.showError(t('profile.passwordTooShort'))
    return
  }

  const ticket = writes.begin()
  if (!ticket.isCurrent()) return
  const { old_password, new_password } = form.value
  loading.value = true
  try {
    await profileAPI.changePassword(old_password, new_password, { signal: ticket.signal })
    if (!ticket.isCurrent()) return
    form.value = { old_password: '', new_password: '', confirm_password: '' }
    notifications.showSuccess(t('profile.passwordChangeSuccess'))
  } catch (error: unknown) {
    if (!ticket.isCurrent() || isCancel(error)) return
    notifications.showError(extractApiErrorMessage(error, t('profile.passwordChangeFailed')))
  } finally {
    if (ticket.isCurrent()) loading.value = false
  }
}

function clearScope(): void {
  writes.cancel()
  form.value = { old_password: '', new_password: '', confirm_password: '' }
  loading.value = false
}
watch(context, clearScope, { flush: 'sync' })
onScopeDispose(clearScope)
</script>
