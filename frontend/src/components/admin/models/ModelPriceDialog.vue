<template>
  <BaseDialog :show="!!entry" :title="t('modelManagement.editPrice')" width="wide" content-class="!max-w-2xl" :close-on-escape="!busy" :show-close-button="!busy" @close="emit('close')">
    <form v-if="draft" id="model-price-form" @submit.prevent="save">
      <fieldset :disabled="busy" class="min-w-0 space-y-4">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <code class="min-w-0 break-all text-sm font-medium text-gray-800 dark:text-gray-100">{{ entry?.id }}</code>
          <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('modelManagement.priceUnit') }}</span>
        </div>
        <section class="space-y-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-900/60">
          <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('modelManagement.basePrices') }}</h4>
          <TokenPriceFields :model-value="draft" @update:model-value="Object.assign(draft!, $event)" />
        </section>
        <section v-if="image" class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
          <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('modelManagement.imagePrices') }}</h4>
          <TokenPriceFields v-model="imageDraft" image />
        </section>
        <details :open="!!draft.tiers?.length" class="group rounded-xl border border-gray-200 dark:border-dark-700">
          <summary class="flex cursor-pointer list-none items-center justify-between gap-2 p-4 text-sm font-semibold text-gray-800 dark:text-gray-100">
            <span>{{ t('modelManagement.contextTiers') }}<span v-if="draft.tiers?.length" class="ml-2 font-normal tabular-nums text-gray-400">{{ draft.tiers.length }}</span></span>
            <svg class="h-4 w-4 shrink-0 text-gray-400 transition-transform group-open:rotate-180" viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="m5 7.5 5 5 5-5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" /></svg>
          </summary>
          <div class="space-y-3 px-4 pb-4">
            <div v-for="(tier, index) in draft.tiers" :key="index" class="space-y-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-900/60">
              <div class="flex flex-wrap items-end gap-3">
                <label class="min-w-0 flex-1 text-xs font-medium text-gray-600 dark:text-gray-400">{{ t('modelManagement.above') }}<input v-model.number="tier.above" type="number" inputmode="numeric" min="1" step="1" class="input mt-1.5 w-full text-right tabular-nums" required /></label>
                <label class="flex items-center gap-2 py-2 text-sm"><input v-model="tier.unpriced" type="checkbox" />{{ t('modelManagement.unpriced') }}</label>
                <button type="button" class="btn btn-secondary btn-sm mb-1" @click="draft.tiers!.splice(index, 1)">{{ t('common.delete') }}</button>
              </div>
              <TokenPriceFields v-if="!tier.unpriced" :model-value="tier" @update:model-value="Object.assign(tier, $event)" />
            </div>
            <button type="button" class="btn btn-secondary btn-sm" @click="addTier">{{ t('modelManagement.addTier') }}</button>
          </div>
        </details>
        <section class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('modelManagement.serviceModes') }}</h4>
            <div class="flex flex-wrap gap-2">
              <label v-for="mode in modes" :key="mode" class="flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-1.5 text-xs font-medium" :class="draft.modes?.[mode] != null ? 'border-primary-200 bg-primary-50 text-primary-700 dark:border-primary-800 dark:bg-primary-900/20 dark:text-primary-300' : 'border-gray-200 text-gray-600 dark:border-dark-600 dark:text-gray-400'"><input type="checkbox" :checked="draft.modes?.[mode] != null" @change="toggleMode(mode, ($event.target as HTMLInputElement).checked)" />{{ mode }}</label>
            </div>
          </div>
          <template v-for="mode in modes" :key="mode">
            <div v-if="draft.modes?.[mode]" class="mt-4 space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700"><h5 class="text-xs font-medium text-gray-500">{{ mode }}</h5><TokenPriceFields :model-value="draft.modes[mode]!" @update:model-value="draft.modes![mode] = $event" /></div>
          </template>
        </section>
      </fieldset>
    </form>
    <template #footer>
      <div class="flex w-full flex-wrap items-center justify-between gap-3">
        <button type="button" class="text-xs font-medium text-gray-500 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-40 dark:text-gray-400" :disabled="busy || !entry?.override" @click="emit('save', null)">{{ t('modelManagement.resetOverride') }}</button>
        <div class="flex gap-2"><button type="button" class="btn btn-secondary" :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</button><button class="btn btn-primary" type="submit" form="model-price-form" :disabled="busy">{{ t('common.save') }}</button></div>
      </div>
    </template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import TokenPriceFields from './TokenPriceFields.vue'
import type { ModelPrice, PriceEntry, TokenPrice } from '@/api/admin/models'
const props = defineProps<{ entry: PriceEntry | null; platform: string; busy: boolean; image: boolean }>()
const emit = defineEmits<{ close: []; save: [value: ModelPrice | null] }>()
const { t } = useI18n()
const empty = (): TokenPrice => ({ input: null, output: null, cache_read: null })
const draft = ref<ModelPrice | null>(null)
const imageDraft = ref<TokenPrice>(empty())
const modes = computed<('fast' | 'flex' | 'priority')[]>(() => props.platform === 'anthropic' ? ['fast'] : ['flex', 'priority'])
watch(() => props.entry, entry => {
  draft.value = entry ? JSON.parse(JSON.stringify(entry.override || entry.effective || empty())) : null
  if (draft.value) { draft.value.tiers ||= []; draft.value.modes ||= {} }
  imageDraft.value = draft.value?.image ? { ...draft.value.image } : empty()
}, { immediate: true })
function addTier() { draft.value?.tiers?.push({ ...empty(), above: 200000 }) }
function toggleMode(mode: 'fast' | 'flex' | 'priority', enabled: boolean) {
  if (!draft.value) return
  draft.value.modes ||= {}
  if (enabled) draft.value.modes[mode] = empty()
  else delete draft.value.modes[mode]
}
function save() {
  if (!draft.value) return
  const value = JSON.parse(JSON.stringify(draft.value)) as ModelPrice
  if (props.image) value.image = Object.values(imageDraft.value).every(v => v == null) ? null : { ...imageDraft.value }
  emit('save', value)
}
</script>
