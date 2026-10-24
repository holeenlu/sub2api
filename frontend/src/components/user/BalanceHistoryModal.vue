<template>
  <BaseDialog :show="show" :title="t('balanceHistory.title')" width="extra-wide" close-on-click-outside @close="$emit('close')">
    <div class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-4 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="min-w-0">
          <p class="break-all font-medium text-gray-900 dark:text-white">{{ account?.email }}</p>
          <p v-if="account?.username" class="text-sm text-gray-500">{{ account.username }}</p>
        </div>
        <div class="flex flex-wrap gap-6 text-right">
          <div>
            <p class="text-xs text-gray-500">{{ t('balanceHistory.currentBalance') }}</p>
            <p class="text-lg font-semibold">{{ account ? money(account.balance) : '—' }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-500">{{ t('balanceHistory.totalRecharged') }}</p>
            <p class="text-lg font-semibold text-emerald-600 dark:text-emerald-400">{{ totalRecharged === null ? '—' : money(totalRecharged) }}</p>
          </div>
        </div>
      </div>
      <p class="text-xs text-gray-500">{{ t('balanceHistory.summaryHint') }}</p>

      <div v-if="loading" class="py-10 text-center text-gray-500" role="status">{{ t('common.loading') }}</div>
      <div v-else-if="failed" class="space-y-3 py-8 text-center" role="alert">
        <p class="text-red-600">{{ t('balanceHistory.loadFailed') }}</p>
        <button class="btn btn-secondary" @click="loadHistory(page)">{{ t('balanceHistory.retry') }}</button>
      </div>
      <div v-else-if="items.length === 0" class="py-10 text-center text-gray-500">{{ t('balanceHistory.empty') }}</div>
      <div v-else class="max-h-[28rem] overflow-auto rounded-xl border border-gray-200 dark:border-dark-600">
        <table class="w-full min-w-[650px] text-left text-sm">
          <thead class="sticky top-0 bg-gray-50 text-gray-500 dark:bg-dark-700">
            <tr>
              <th class="px-4 py-3">{{ t('balanceHistory.time') }}</th>
              <th class="px-4 py-3">{{ t('balanceHistory.type') }}</th>
              <th class="px-4 py-3 text-right">{{ t('balanceHistory.amount') }}</th>
              <th class="px-4 py-3">{{ t('balanceHistory.source') }}</th>
              <th class="px-4 py-3">{{ t('balanceHistory.notes') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-600">
            <tr v-for="item in items" :key="item.id">
              <td class="whitespace-nowrap px-4 py-3 text-gray-500">{{ formatDateTime(item.occurred_at) }}</td>
              <td class="whitespace-nowrap px-4 py-3">{{ itemTitle(item) }}</td>
              <td :class="['whitespace-nowrap px-4 py-3 text-right font-semibold tabular-nums', item.value < 0 ? 'text-red-600 dark:text-red-400' : 'text-emerald-600 dark:text-emerald-400']">{{ formatValue(item) }}</td>
              <td class="whitespace-nowrap px-4 py-3">{{ t(`balanceHistory.sources.${item.source}`) }}</td>
              <td class="max-w-xs break-words px-4 py-3 text-gray-500">{{ item.notes }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!failed" class="flex flex-wrap items-center justify-between gap-3 text-sm text-gray-500">
        <span>{{ t('balanceHistory.pagination', { total, page, pages }) }}</span>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="loading || page <= 1" @click="loadHistory(page - 1)">{{ t('pagination.previous') }}</button>
          <button class="btn btn-secondary" :disabled="loading || page >= pages" @click="loadHistory(page + 1)">{{ t('pagination.next') }}</button>
        </div>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { getBalanceHistory, getProfile, type BalanceHistoryItem } from '@/api/user'
import type { User } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ show: boolean }>()
defineEmits<{ close: [] }>()
const { t } = useI18n()
const authStore = useAuthStore()
const items = ref<BalanceHistoryItem[]>([])
const account = ref<User | null>(null)
const totalRecharged = ref<number | null>(null)
const total = ref(0)
const page = ref(1)
const pageSize = 15
const pages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const loading = ref(false)
const failed = ref(false)
let requestVersion = 0

watch([() => props.show, () => authStore.user?.id], ([show, userID], previous) => {
  requestVersion++
  const opening = show && (!previous?.[0] || userID !== previous[1])
  if (!show || !userID || opening) {
    items.value = []
    account.value = null
    totalRecharged.value = null
    total.value = 0
    page.value = 1
    failed.value = false
  }
  if (show && userID) void loadHistory(1)
}, { immediate: true })
onUnmounted(() => { requestVersion++ })

async function loadHistory(nextPage: number) {
  const version = ++requestVersion
  const userID = authStore.user?.id
  loading.value = true
  failed.value = false
  items.value = []
  page.value = nextPage
  try {
    const [result, profile] = await Promise.all([
      getBalanceHistory({ category: 'balance', direction: '', source: '', page: nextPage, page_size: pageSize }),
      account.value ? Promise.resolve(account.value) : getProfile()
    ])
    if (version !== requestVersion || userID !== authStore.user?.id) return
    if (profile.id !== userID) throw new Error('Account changed')
    account.value = profile
    items.value = result.items
    total.value = result.total
    totalRecharged.value = result.total_recharged
  } catch {
    if (version === requestVersion && userID === authStore.user?.id) failed.value = true
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

function money(value: number) {
  return `$${value.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}
function itemTitle(item: BalanceHistoryItem) {
  return t(`balanceHistory.balance${item.value < 0 ? 'Decrease' : 'Increase'}`)
}
function formatValue(item: BalanceHistoryItem) {
  const sign = item.value < 0 ? '−' : '+'
  const value = Math.abs(item.value)
  return `${sign}${money(value)}`
}
</script>
