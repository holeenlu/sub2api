<template>
  <section class="w-full rounded-xl border border-indigo-100 bg-indigo-50/60 p-4 dark:border-indigo-900 dark:bg-indigo-950/30 sm:p-5">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div><h3 class="font-semibold text-slate-900 dark:text-white">{{ t('admin.accounts.codexTicketPool.title') }}</h3><p class="mt-1 text-xs text-slate-600 dark:text-slate-400">{{ t('admin.accounts.codexTicketPool.description') }}</p></div>
      <button type="button" class="btn btn-primary !py-1.5 text-xs" :disabled="saving || loading || (mode === 'custom' && !selected.length)" @click="save">{{ saving ? '…' : t('admin.accounts.codexTicketPool.save') }}</button>
    </div>
    <div v-if="error" class="mt-3 text-xs text-rose-600">{{ error }}</div>
    <div v-else-if="saved" role="status" class="mt-3 text-xs text-emerald-600">{{ t('admin.accounts.codexTicketPool.saved') }}</div>
    <div class="mt-4 flex flex-wrap gap-4 text-sm"><label class="flex cursor-pointer items-center gap-2"><input v-model="mode" type="radio" value="all" />{{ t('admin.accounts.codexTicketPool.all') }}</label><label class="flex cursor-pointer items-center gap-2"><input v-model="mode" type="radio" value="custom" />{{ t('admin.accounts.codexTicketPool.custom') }}</label></div>
    <div v-if="mode === 'custom'" class="mt-3 grid max-h-40 gap-2 overflow-y-auto sm:grid-cols-2 lg:grid-cols-3"><label v-for="proxy in proxies" :key="proxy.id" class="flex min-w-0 cursor-pointer items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs dark:border-slate-700 dark:bg-slate-900"><input v-model="selected" type="checkbox" :value="proxy.id" /><span class="truncate">{{ proxy.name }}</span></label><span v-if="!proxies.length" class="text-xs text-amber-700 dark:text-amber-400">{{ t('admin.accounts.codexTicketPool.empty') }}</span></div>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { apiClient } from '@/api/client'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { Proxy } from '@/types'
const { t } = useI18n()
const mode = ref('all')
const selected = ref<number[]>([])
const proxies = ref<Proxy[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const saved = ref(false)
onMounted(async () => { loading.value = true; try { const [pool, available] = await Promise.all([apiClient.get<{ mode: string; proxy_ids: number[] }>('/admin/proxies/codex-ticket-pool'), adminAPI.proxies.getAll()]); mode.value = pool.data.mode; selected.value = pool.data.proxy_ids ?? []; proxies.value = available } catch (cause) { error.value = extractApiErrorMessage(cause, t('admin.accounts.codexTicketPool.loadFailed')) } finally { loading.value = false } })
async function save() { saving.value = true; error.value = ''; saved.value = false; try { await apiClient.put('/admin/proxies/codex-ticket-pool', { mode: mode.value, proxy_ids: mode.value === 'all' ? [] : selected.value }); saved.value = true } catch (cause) { error.value = extractApiErrorMessage(cause, t('admin.accounts.codexTicketPool.saveFailed')) } finally { saving.value = false } }
</script>
