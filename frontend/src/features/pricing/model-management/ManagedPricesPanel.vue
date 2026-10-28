<template>
  <section class="space-y-4" :aria-label="t('title')" :aria-busy="busy">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <h2 class="text-lg font-semibold">{{ t('title') }}</h2>
      <div v-if="readable" class="flex gap-2">
        <button type="button" data-refresh class="btn btn-secondary" :disabled="busy" @click="load">{{ t('refresh') }}</button>
        <button v-if="canManage" type="button" data-sync class="btn btn-secondary" :disabled="!writable || !!editing" @click="sync">{{ t('sync') }}</button>
      </div>
    </header>
    <p v-if="!readable" role="status">{{ t('readDenied') }}</p>
    <template v-else>
      <p class="text-xs text-gray-500">{{ t('unit') }}</p>
      <p v-if="query.status === 'error'" role="alert" class="text-sm text-red-600">{{ t(`errors.${query.error}`) }}</p>
      <p v-if="command.status === 'error'" role="alert" class="text-sm text-red-600">{{ t(`errors.${command.error}`) }}</p>
      <p v-else-if="command.status === 'pending'" role="status">{{ t('working') }}</p>
      <p v-else-if="command.status === 'success'" role="status">{{ t('saved') }}</p>
      <p v-if="needsRead" role="status" class="text-sm text-amber-700">{{ t('reloadRequired') }}</p>
      <p v-if="query.status === 'loading'" role="status">{{ t('loading') }}</p>
      <template v-if="snapshot">
        <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500">
          <div><span>{{ t('version', { version: snapshot.version }) }}</span><span v-if="snapshot.synced_at" class="ml-3">{{ t('synced', { time: snapshot.synced_at }) }}</span></div>
          <input v-model="search" class="input w-full sm:w-64" :placeholder="t('search')" :aria-label="t('search')" />
        </div>
        <p v-if="!filtered.length" role="status">{{ t('empty') }}</p>
        <div v-else class="overflow-x-auto rounded-xl border dark:border-dark-700">
          <table class="w-full min-w-[700px] text-left text-sm">
            <thead class="bg-gray-50 dark:bg-dark-800"><tr><th class="p-3">{{ t('model') }}</th><th v-for="field in primaryFields" :key="field" class="p-3">{{ t(field) }}</th><th class="p-3">{{ t('source') }}</th><th class="p-3"><span class="sr-only">{{ t('edit') }}</span></th></tr></thead>
            <tbody>
              <template v-for="entry in filtered" :key="entry.id">
                <tr :data-model="entry.id" class="border-t dark:border-dark-700">
                  <th class="p-3 text-xs font-normal"><code>{{ entry.id }}</code></th>
                  <td v-for="field in primaryFields" :key="field" class="p-3 tabular-nums">{{ entry.effective === null ? t('unpriced') : rate(entry.effective[field]) }}</td>
                  <td class="p-3 text-xs">{{ entry.source }}</td>
                  <td class="p-3"><button v-if="canManage" type="button" data-edit class="btn btn-secondary whitespace-nowrap" :disabled="!writable || !!editing" @click="open(entry)">{{ t('edit') }}</button></td>
                </tr>
                <tr><td colspan="6" class="px-3 pb-3">
                  <details class="text-xs"><summary class="cursor-pointer">{{ t('details') }}</summary>
                    <div class="mt-3 space-y-3">
                      <TokenPriceSummary :value="entry.effective" />
                      <section v-for="tier in entry.effective?.tiers" :key="tier.above" class="space-y-2"><h4>{{ t('tiers') }} · {{ t('above') }} {{ tier.above }}</h4><TokenPriceSummary :value="tier.unpriced ? null : tier" /></section>
                      <section v-for="mode in serviceModes" :key="mode" class="space-y-2"><h4>{{ t('modes') }} · {{ mode }}</h4><TokenPriceSummary :value="entry.effective?.modes?.[mode]" /></section>
                      <section v-if="imageModelIds?.includes(entry.id) || entry.effective?.image !== undefined" class="space-y-2"><h4>{{ t('image') }}</h4><p v-if="entry.effective?.image === null">{{ t('noImage') }}</p><TokenPriceSummary v-else :value="entry.effective?.image" /></section>
                    </div>
                  </details>
                </td></tr>
              </template>
            </tbody>
          </table>
        </div>
      </template>
      <ModelPriceDialog v-if="editing" :key="editing.entry.id" :entry="editing.entry" :busy="busy" :can-write="writable" :stale="draftStale" :image="imageModelIds?.includes(editing.entry.id) ?? false" :error="command.status === 'error' ? command.error : undefined" @close="close" @reload="load" @save="save" @dirty-change="dirty = $event" />
    </template>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatManagedRate } from './formatRate'
import ModelPriceDialog from './ModelPriceDialog.vue'
import TokenPriceSummary from './TokenPriceSummary.vue'
import { messages } from './locales'
import { serviceModes } from './types'
import type { ManagedPricePlatform } from './types'
import { useManagedPrices } from './useManagedPrices'
const props = defineProps<{ platform: ManagedPricePlatform; scope: string; canRead: boolean; canManage: boolean; imageModelIds?: readonly string[] }>()
const emit = defineEmits<{ 'dirty-change': [dirty: boolean]; 'busy-change': [busy: boolean] }>()
const { t, locale } = useI18n({ useScope: 'local', messages })
const search = ref('')
const readable = computed(() => props.canRead && !!props.scope.trim())
const dirty = ref(false)
const primaryFields = ['input', 'output', 'cache_read'] as const
const { snapshot, query, command, editing, busy, writable, draftStale, needsRead, load, open, close, save, sync } = useManagedPrices(() => props)
watch(dirty, value => emit('dirty-change', value), { immediate: true, flush: 'sync' })
watch(busy, value => emit('busy-change', value), { immediate: true, flush: 'sync' })
watch(editing, value => { if (!value) dirty.value = false }, { flush: 'sync' })
onBeforeUnmount(() => {
  if (dirty.value) emit('dirty-change', false)
  if (busy.value) emit('busy-change', false)
})
const filtered = computed(() => snapshot.value?.entries.filter(entry => entry.id.toLowerCase().includes(search.value.trim().toLowerCase())) ?? [])
watch(() => [props.scope, props.platform, props.canRead, props.canManage], () => { search.value = '' }, { flush: 'sync' })
function rate(value: number | null | undefined) { return value === undefined ? t('unknown') : value === null ? t('unpriced') : formatManagedRate(value, locale.value) }
</script>
