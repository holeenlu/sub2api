<template>
  <BaseDialog
    :show="show"
    :title="t('admin.users.createUser')"
    width="normal"
    @close="$emit('close')"
  >
    <form id="create-user-form" @submit.prevent="submit" class="space-y-5">
      <div>
        <label class="input-label">{{ t('admin.users.email') }}</label>
        <input v-model="form.email" type="email" required class="input" :placeholder="t('admin.users.enterEmail')" />
      </div>
      <div>
        <label class="input-label">{{ t('admin.users.password') }}</label>
        <div class="flex gap-2">
          <div class="relative flex-1">
            <input v-model="form.password" type="text" required minlength="6" maxlength="72" autocomplete="new-password" class="input pr-10" :placeholder="t('admin.users.enterPassword')" />
          </div>
          <button type="button" @click="form.password = generateRandomPassword()" :title="t('admin.users.generatePassword')" :aria-label="t('admin.users.generatePassword')" class="btn btn-secondary px-3">
            <Icon name="refresh" size="md" />
          </button>
        </div>
      </div>
      <div>
        <label class="input-label">{{ t('admin.users.username') }}</label>
        <input v-model="form.username" type="text" class="input" :placeholder="t('admin.users.enterUsername')" />
      </div>
      <div>
        <label class="input-label">{{ t('admin.users.form.roleLabel') }}</label>
        <select v-model="form.role" class="input">
          <option v-for="role in roles" :key="role" :value="role">{{ t(`admin.users.roles.${role}`) }}</option>
        </select>
      </div>
      <p v-if="form.role === 'admin'" class="text-sm text-gray-500">{{ t('admin.rolePermissions.sharedPolicy') }}</p>
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div v-if="auth.can('billing.balance.adjust') && form.role !== 'super_admin'">
          <label class="input-label">{{ t('admin.users.columns.balance') }}</label>
          <input v-model="form.balance" type="number" step="any" min="0" :max="auth.isSuperAdmin ? undefined : auth.user?.balance ?? 0" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.users.columns.concurrency') }}</label>
          <input v-model.number="form.concurrency" type="number" class="input" />
        </div>
      </div>
      <div>
        <label class="input-label">{{ t('admin.users.form.rpmLimit') }}</label>
        <input
          v-model.number="form.rpm_limit"
          type="number"
          min="0"
          step="1"
          class="input"
          :placeholder="t('admin.users.form.rpmLimitPlaceholder')"
        />
        <p class="input-hint">{{ t('admin.users.form.rpmLimitHint') }}</p>
      </div>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" type="button" class="btn btn-secondary">{{ t('common.cancel') }}</button>
        <button type="submit" form="create-user-form" :disabled="loading" class="btn btn-primary">
          {{ loading ? t('admin.users.creating') : t('common.create') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <!-- 创建管理员账号时后端要求 step-up 2FA，弹出 TOTP 验证后自动重试 -->
  <TotpStepUpDialog :controller="stepUp" />
</template>

<script setup lang="ts">
import { readApiError } from '@/shared/lib/apiError'

import { computed, reactive, ref, watch } from 'vue'
import { newUserDraft, createUserPayload } from '../../model/userDrafts'
import { useI18n } from 'vue-i18n'; import { usersAPI } from '../../api/users'
import { useIdentityNotifications } from '../../ports'
import { useAuthStore } from '../../model/auth'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { generateRandomPassword } from '@/shared/lib/password'
import { USER_ROLES, isVendorRole } from '../../model/userRoles'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '../../composables/useStepUp'
import TotpStepUpDialog from '../TotpStepUpDialog.vue'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{
  close: []
  success: []
}>(); const { t } = useI18n()
const appStore = useIdentityNotifications()
const auth = useAuthStore()
const roles = computed(() => USER_ROLES.filter(role => !isVendorRole(role) && (
  role === 'user' ? auth.can('users.create') : auth.isSuperAdmin
)))

const form = reactive(newUserDraft())

const stepUp = useStepUp()
const loading = ref(false)

const submit = async () => {
  if (loading.value) return
  loading.value = true
  try {
    const payload = createUserPayload(form)
    // 创建管理员属敏感操作：后端返回 STEP_UP_REQUIRED 时弹 TOTP 验证并重试
    await stepUp.run(() => usersAPI.create(payload))
    await auth.refreshUser().catch(() => {})
    appStore.showSuccess(t('admin.users.userCreated'))
    emit('success'); emit('close')
  } catch (e: unknown) {
    const errorDetails = readApiError(e)
    if (isStepUpCancelled(e)) {
      // 用户主动取消二次验证：静默返回，表单保持打开。
    } else if (isStepUpBlocked(e)) {
      appStore.showError(
        stepUpBlockReason(e) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
          ? t('stepUp.adminApiKeyForbidden')
          : t('stepUp.notEnabled')
      )
    } else {
      appStore.showError(errorDetails?.message || t('admin.users.failedToCreate'))
    }
  } finally { loading.value = false }
}

watch(() => props.show, (v) => { if(v) Object.assign(form, newUserDraft()) })

</script>
