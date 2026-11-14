<template>
  <BaseDialog :show="show" :title="operation === 'add' ? t('admin.users.deposit') : t('admin.users.withdraw')" width="narrow" @close="closeDialog">
    <form v-if="user" id="balance-form" @submit.prevent="handleBalanceSubmit" class="space-y-5">
      <div class="flex items-center gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100"><span class="text-lg font-medium text-primary-700">{{ user.email.charAt(0).toUpperCase() }}</span></div>
        <div class="flex-1"><p class="font-medium text-gray-900 dark:text-gray-100">{{ user.email }}</p><p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.users.currentBalance') }}: ${{ formatBalance(user.balance) }}</p></div>
      </div>
      <div>
        <label class="input-label">{{ operation === 'add' ? t('admin.users.depositAmount') : t('admin.users.withdrawAmount') }}</label>
        <div class="relative flex gap-2">
          <div class="relative flex-1"><div class="absolute left-3 top-1/2 -translate-y-1/2 font-medium text-gray-500">$</div><input v-model.number="form.amount" type="number" step="any" min="0" :max="availableTransfer" required class="input pl-8" /></div>
          <button v-if="operation === 'subtract'" type="button" @click="fillAllBalance" class="btn btn-secondary whitespace-nowrap">{{ t('admin.users.withdrawAll') }}</button>
        </div>
      </div>
      <p v-if="!auth.isSuperAdmin" class="text-sm text-gray-500">{{ t('admin.users.transferFundingHint', { balance: formatBalance(auth.user?.balance ?? 0) }) }}</p>
      <div><label class="input-label">{{ t('admin.users.notes') }}</label><textarea v-model="form.notes" rows="3" class="input"></textarea></div>
      <p v-if="outcomeUnknown" role="alert" class="text-sm text-amber-700 dark:text-amber-300">{{ t('common.unknown') }}: {{ submission?.message }}</p>
      <div v-if="form.amount > 0" class="rounded-xl border border-blue-200 bg-blue-50 p-4 dark:border-blue-800 dark:bg-blue-950"><div class="flex items-center justify-between text-sm"><span class="text-gray-700 dark:text-gray-300">{{ t('admin.users.newBalance') }}:</span><span class="font-bold text-gray-900 dark:text-gray-100">${{ formatBalance(calculateNewBalance()) }}</span></div></div>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="closeDialog" class="btn btn-secondary">{{ t('common.cancel') }}</button>
        <button type="submit" form="balance-form" :disabled="submitting || outcomeUnknown || !form.amount" class="btn" :class="operation === 'add' ? 'bg-emerald-600 text-white' : 'btn-danger'">{{ submitting ? t('common.saving') : outcomeUnknown ? t('common.unknown') : t('common.confirm') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onScopeDispose, reactive, watch } from 'vue'
import { useAuthStore } from '@/features/identity'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/app/model/app'
import { updateBalance } from '@/features/payments/userBalances'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import type { AdminUser } from '@/features/identity/api/profileTypes';
import BaseDialog from '@/shared/ui/BaseDialog.vue'

const props = defineProps<{ show: boolean, user: AdminUser | null, operation: 'add' | 'subtract' }>()
const emit = defineEmits<{ close: [], success: [] }>(); const { t } = useI18n(); const appStore = useAppStore()

const auth = useAuthStore()
const availableTransfer = computed(() => props.operation === 'subtract' ? props.user?.balance : auth.isSuperAdmin ? undefined : auth.user?.balance ?? 0)
const form = reactive({ amount: 0, notes: '' })
// Keep unresolved commands for their original actor/target even after closing or switching rows.
const submissions = reactive(new Map<string, { status: 'pending' | 'unknown', message?: string }>())
const targetScope = computed(() => JSON.stringify([auth.identityScopeKey, props.user?.id]))
const submission = computed(() => submissions.get(targetScope.value))
const submitting = computed(() => submission.value?.status === 'pending')
const outcomeUnknown = computed(() => submission.value?.status === 'unknown')
let viewVersion = 0
let disposed = false
watch([() => props.show, () => props.user?.id, () => props.operation, () => auth.identityScopeKey],
  ([show], [wasShown]) => {
    viewVersion += 1
    form.amount = 0
    form.notes = ''
    if (show && !wasShown) void auth.refreshUser().catch(() => {})
  }, { flush: 'sync' })
onScopeDispose(() => { disposed = true; viewVersion += 1 })

function closeDialog(): void {
  viewVersion += 1
  emit('close')
}

function isDefiniteRejection(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false
  const response = 'response' in error ? error.response : undefined
  const status = 'status' in error ? error.status
    : typeof response === 'object' && response !== null && 'status' in response ? response.status : undefined
  // Timeout, in-progress/conflict and client cancellation do not prove the write was rejected.
  return typeof status === 'number' && status >= 400 && status < 500 && ![408, 409, 499].includes(status)
}

// 格式化余额：显示完整精度，去除尾部多余的0
const formatBalance = (value: number) => {
  if (value === 0) return '0.00'
  // 最多保留8位小数，去除尾部的0
  const formatted = value.toFixed(8).replace(/\.?0+$/, '')
  // 确保至少有2位小数
  const parts = formatted.split('.')
  if (parts.length === 1) return formatted + '.00'
  if (parts[1].length === 1) return formatted + '0'
  return formatted
}

// 填入全部余额
const fillAllBalance = () => {
  if (props.user) {
    form.amount = props.user.balance
  }
}

const calculateNewBalance = () => {
  if (!props.user) return 0
  const result = props.operation === 'add' ? props.user.balance + form.amount : props.user.balance - form.amount
  // 避免浮点数精度问题导致的 -0.00 显示
  return Math.abs(result) < 1e-10 ? 0 : result
}
const handleBalanceSubmit = async () => {
  if (disposed || !props.show || !props.user || submission.value) return
  if (!Number.isFinite(form.amount) || form.amount <= 0) {
    appStore.showError(t('admin.users.amountRequired'))
    return
  }
  // 退款时验证金额不超过实际余额
  if (availableTransfer.value !== undefined && form.amount > availableTransfer.value) {
    appStore.showError(t('admin.users.insufficientBalance'))
    return
  }
  const scope = targetScope.value
  const version = viewVersion
  const targetId = props.user.id
  const amount = form.amount
  const operation = props.operation
  const notes = form.notes
  const isCurrent = () => !disposed && props.show && version === viewVersion && scope === targetScope.value
  submissions.set(scope, { status: 'pending' })
  try {
    await updateBalance(targetId, amount, operation, notes)
    if (!isCurrent()) return
    await auth.refreshUser().catch(() => {})
    if (!isCurrent()) return
    // Both full and restricted id-only acknowledgements use the parent's authoritative list refresh.
    appStore.showSuccess(t('common.success'))
    emit('success')
    if (isCurrent()) emit('close')
  } catch (e: unknown) {
    const message = extractApiErrorMessage(e, t('common.error'))
    if (!isDefiniteRejection(e)) submissions.set(scope, { status: 'unknown', message })
    if (!isCurrent()) return
    console.error('Failed to update balance:', e)
    appStore.showError(message)
  } finally {
    if (submissions.get(scope)?.status === 'pending') submissions.delete(scope)
  }
}
</script>
