<template>
  <div
    class="group/priority inline-flex h-7 items-center rounded-lg border transition-colors duration-150"
    :class="[
      editing || dirty
        ? 'border-primary-300 bg-primary-50/70 dark:border-primary-700 dark:bg-primary-900/20'
        : 'border-transparent hover:border-gray-200 hover:bg-gray-50 dark:hover:border-dark-600 dark:hover:bg-dark-700/60'
    ]"
    data-testid="account-priority-cell"
  >
    <button
      type="button"
      class="flex h-full w-6 items-center justify-center rounded-l-lg text-gray-500 opacity-0 transition hover:bg-gray-200/70 hover:text-primary-600 focus:opacity-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:text-gray-300 disabled:hover:bg-transparent disabled:hover:text-gray-300 dark:disabled:text-dark-600 dark:disabled:hover:text-dark-600 group-hover/priority:opacity-100 group-focus-within/priority:opacity-100 dark:text-gray-400 dark:hover:bg-dark-600 dark:hover:text-primary-400 [@media(hover:none)]:opacity-100"
      :class="{ '!opacity-100': editing || dirty }"
      :disabled="!writable || saving || draft <= MIN_PRIORITY"
      :title="t('admin.accounts.priorityQuick.raise')"
      :aria-label="t('admin.accounts.priorityQuick.raise')"
      data-testid="account-priority-decrement"
      @click="step(-1)"
    >
      <svg class="h-3 w-3" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round">
        <path d="M2.5 6h7" />
      </svg>
    </button>

    <input
      v-if="editing"
      ref="inputRef"
      v-model="inputValue"
      type="text"
      inputmode="numeric"
      class="h-full w-10 border-0 bg-transparent p-0 text-center font-mono text-sm tabular-nums text-gray-900 focus:outline-none focus:ring-0 dark:text-white"
      :aria-label="t('admin.accounts.columns.priority')"
      data-testid="account-priority-input"
      @keydown.enter.prevent="commitInput"
      @keydown.esc.prevent="cancelInput"
      @keydown.up.prevent="nudgeInput(1)"
      @keydown.down.prevent="nudgeInput(-1)"
      @blur="commitInput"
    />
    <button
      v-else
      type="button"
      class="relative flex h-full min-w-[2rem] items-center justify-center px-1 font-mono text-sm tabular-nums transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
      :class="dirty ? 'font-semibold text-primary-600 dark:text-primary-400' : 'text-gray-700 hover:text-gray-900 dark:text-gray-300 dark:hover:text-white'"
      :disabled="!writable || saving"
      :title="writable ? t('admin.accounts.priorityQuick.editHint') : undefined"
      data-testid="account-priority-value"
      @click="startEditing"
    >
      {{ draft }}
      <span
        v-if="saving"
        class="absolute -right-0.5 top-1 h-1.5 w-1.5 animate-pulse rounded-full bg-primary-500"
        data-testid="account-priority-saving"
      />
    </button>

    <button
      type="button"
      class="flex h-full w-6 items-center justify-center rounded-r-lg text-gray-500 opacity-0 transition hover:bg-gray-200/70 hover:text-primary-600 focus:opacity-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:text-gray-300 disabled:hover:bg-transparent disabled:hover:text-gray-300 dark:disabled:text-dark-600 dark:disabled:hover:text-dark-600 group-hover/priority:opacity-100 group-focus-within/priority:opacity-100 dark:text-gray-400 dark:hover:bg-dark-600 dark:hover:text-primary-400 [@media(hover:none)]:opacity-100"
      :class="{ '!opacity-100': editing || dirty }"
      :disabled="!writable || saving || draft >= MAX_PRIORITY"
      :title="t('admin.accounts.priorityQuick.lower')"
      :aria-label="t('admin.accounts.priorityQuick.lower')"
      data-testid="account-priority-increment"
      @click="step(1)"
    >
      <svg class="h-3 w-3" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round">
        <path d="M2.5 6h7M6 2.5v7" />
      </svg>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { update as updateAccount } from '@/features/resources/accounts'
import { extractApiErrorMessage, readApiError } from '@/shared/lib/apiError'
import type { Account } from '@/features/resources/api/accountTypes';

const MIN_PRIORITY = 1
const MAX_PRIORITY = 9999
// 连续点击时合并为一次保存，避免每次 +/- 都打一次接口
const SAVE_DEBOUNCE_MS = 450

// The application supplies live authorization and actor scope readers. Reading
// them here lets revocation cancel before Vue rerenders the parent table.
const props = defineProps<{
  account: Pick<Account, 'id' | 'priority'>
  scopeKey: () => string
  canEdit: () => boolean
}>()
const emit = defineEmits<{
  (e: 'updated', account: Account): void
  (e: 'error', message: string): void
}>()

const { t } = useI18n()
const draft = ref(props.account.priority)
const saving = ref(false)
const editing = ref(false)
const inputValue = ref('')
const inputRef = ref<HTMLInputElement | null>(null)
let active = true
let editGeneration = 0
let saveTimer: ReturnType<typeof setTimeout> | null = null
type PriorityIntent = {
  accountId: number
  priority: number
  scope: string
  controller: AbortController
}
let intent: PriorityIntent | null = null
const canWrite = () => active && Boolean(props.scopeKey()) && props.canEdit()
const writable = computed(canWrite)
const dirty = computed(() => draft.value !== props.account.priority)
const clamp = (value: number) => Math.min(MAX_PRIORITY, Math.max(MIN_PRIORITY, Math.round(value)))

function clearTimer(): void {
  if (saveTimer !== null) clearTimeout(saveTimer)
  saveTimer = null
}
function cancelPending(): void {
  clearTimer()
  intent?.controller.abort()
  intent = null
}
function invalidate(): void {
  editGeneration++
  cancelPending()
  editing.value = false
  saving.value = false
  draft.value = props.account.priority
  inputValue.value = ''
}
watch([() => props.scopeKey(), () => props.account.id, () => props.canEdit()], invalidate, { flush: 'sync' })
watch(() => props.account.priority, next => {
  if (!saving.value && !saveTimer && !editing.value) draft.value = next
})

function isCurrent(pending: PriorityIntent): boolean {
  return intent === pending && !pending.controller.signal.aborted && canWrite()
    && pending.scope === props.scopeKey() && pending.accountId === props.account.id
}
function capture(): PriorityIntent | null {
  cancelPending()
  if (!canWrite()) return null
  const pending: PriorityIntent = {
    accountId: props.account.id, priority: draft.value,
    scope: props.scopeKey(), controller: new AbortController()
  }
  intent = pending
  return pending
}
async function save(pending: PriorityIntent): Promise<void> {
  clearTimer()
  if (!isCurrent(pending)) return
  if (pending.priority === props.account.priority) { intent = null; return }
  saving.value = true
  try {
    const updated = await updateAccount(pending.accountId, { priority: pending.priority }, {
      signal: pending.controller.signal
    })
    if (!isCurrent(pending)) return
    draft.value = updated.priority
    emit('updated', updated)
  } catch (error: unknown) {
    if (!isCurrent(pending)) return
    draft.value = props.account.priority
    const { code } = readApiError(error)
    if (code === 'STEP_UP_CANCELLED' || code === 'ERR_CANCELED') return
    emit('error', extractApiErrorMessage(error, t('admin.accounts.priorityQuick.failed')))
  } finally {
    if (intent === pending) { intent = null; saving.value = false }
  }
}
function scheduleSave(): void {
  const pending = capture()
  if (pending) saveTimer = setTimeout(() => void save(pending), SAVE_DEBOUNCE_MS)
}
function step(delta: number): void {
  if (!canWrite() || saving.value) return
  const next = clamp(draft.value + delta)
  if (next === draft.value) return
  draft.value = next
  scheduleSave()
}
async function startEditing(): Promise<void> {
  if (!canWrite() || saving.value) return
  cancelPending()
  const generation = ++editGeneration
  inputValue.value = String(draft.value)
  editing.value = true
  await nextTick()
  if (generation !== editGeneration || !canWrite() || !editing.value) return
  inputRef.value?.focus()
  inputRef.value?.select()
}
function nudgeInput(delta: number): void {
  if (!canWrite() || !editing.value) return
  const current = Number.parseInt(inputValue.value, 10)
  inputValue.value = String(clamp((Number.isFinite(current) ? current : draft.value) + delta))
}
function commitInput(): void {
  if (!editing.value || !canWrite() || saving.value) return
  editing.value = false
  const parsed = Number.parseInt(inputValue.value.trim(), 10)
  if (Number.isFinite(parsed)) draft.value = clamp(parsed)
  const pending = capture()
  if (pending) void save(pending)
}
function cancelInput(): void {
  editGeneration++
  editing.value = false
  cancelPending()
  if (!saving.value) draft.value = props.account.priority
}
onBeforeUnmount(() => {
  // Disposal cancels this UI intent; it cannot confirm a server-side rollback.
  active = false
  invalidate()
})

</script>
