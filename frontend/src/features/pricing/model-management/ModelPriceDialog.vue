<template>
  <BaseDialog :show="true" :title="entry.id" width="wide" :close-on-escape="!busy" :show-close-button="!busy" @close="close">
    <form class="space-y-5" :aria-busy="busy" @submit.prevent="save">
      <p class="text-xs text-gray-500">{{ t('unit') }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ t(`errors.${error}`) }}</p>
      <p v-if="stale" role="alert" class="text-sm text-amber-700">{{ t('stale') }}</p>
      <p v-else-if="!canWrite && !busy" role="status" class="text-sm text-amber-700">{{ t('reloadRequired') }}</p>
      <button v-if="!canWrite && !busy" type="button" data-reload class="btn btn-secondary" @click="emit('reload')">{{ t('refresh') }}</button>
      <p v-if="invalid" role="alert" class="text-sm text-red-600">{{ t('invalidDraft') }}</p>
      <fieldset :disabled="busy" class="space-y-5">
        <TokenPriceFields :model-value="draft" :disabled="busy" @update:model-value="Object.assign(draft, $event); removeMissingTokens(draft, $event)" />
        <section class="space-y-3 border-t pt-4 dark:border-dark-700">
          <div class="flex items-center justify-between"><h4 class="font-medium">{{ t('tiers') }}</h4><button type="button" data-add-tier class="btn btn-secondary" @click="addTier">{{ t('addTier') }}</button></div>
          <div v-for="(tier, index) in draft.tiers" :key="index" class="space-y-3 rounded-xl bg-gray-50 p-3 dark:bg-dark-800" :data-tier="index">
            <div class="flex flex-wrap items-center gap-3">
              <label class="text-xs">{{ t('above') }}<input v-model.number="tier.above" type="number" min="1" step="1" required class="input mt-1 w-full" /></label>
              <label class="text-sm"><input v-model="tier.unpriced" type="checkbox" /> {{ t('unpriced') }}</label>
              <button type="button" class="btn btn-secondary" @click="draft.tiers?.splice(index, 1)">{{ t('remove') }}</button>
            </div>
            <TokenPriceFields :model-value="tier" :disabled="busy || tier.unpriced" @update:model-value="updateTier(tier, $event)" />
          </div>
        </section>
        <section class="space-y-3 border-t pt-4 dark:border-dark-700">
          <h4 class="font-medium">{{ t('modes') }}</h4>
          <div v-for="mode in serviceModes" :key="mode" class="space-y-2" :data-mode="mode">
            <label class="flex items-center gap-3 text-sm">{{ mode }}
              <select :value="nestedState(draft.modes?.[mode])" class="input" @change="setMode(mode, ($event.target as HTMLSelectElement).value)">
                <option value="absent">{{ t('inherit') }}</option><option value="null">{{ t('unpriced') }}</option><option value="custom">{{ t('custom') }}</option>
              </select>
            </label>
            <TokenPriceFields v-if="draft.modes?.[mode] != null" :model-value="draft.modes[mode] ?? {}" :disabled="busy" @update:model-value="updateMode(mode, $event)" />
          </div>
        </section>
        <section v-if="image || draft.image !== undefined" class="space-y-3 border-t pt-4 dark:border-dark-700" data-image>
          <label class="flex items-center gap-3 font-medium">{{ t('image') }}
            <select :value="nestedState(draft.image)" class="input" @change="setImage(($event.target as HTMLSelectElement).value)">
              <option value="absent">{{ t('inherit') }}</option><option value="null">{{ t('noImage') }}</option><option value="custom">{{ t('custom') }}</option>
            </select>
          </label>
          <TokenPriceFields v-if="draft.image != null" v-model="draft.image" :disabled="busy" />
        </section>
      </fieldset>
      <div class="flex flex-wrap justify-between gap-2 border-t pt-4 dark:border-dark-700">
        <button type="button" data-reset class="btn btn-secondary" :disabled="!canWrite || stale || busy || entry.override === null" @click="emit('save', null)">{{ t('reset') }}</button>
        <div class="flex gap-2"><button type="button" class="btn btn-secondary" :disabled="busy" @click="close">{{ t('cancel') }}</button><button type="submit" data-save class="btn btn-primary" :disabled="!canWrite || stale || busy">{{ t('save') }}</button></div>
      </div>
    </form>
  </BaseDialog>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import TokenPriceFields from './TokenPriceFields.vue'
import { requirePriceWrite } from './api'
import { messages } from './locales'
import { clonePrice, serviceModes, tokenFields } from './types'
import type { ModelPrice, PriceEntry, PriceTier, ServiceMode, TokenPrice } from './types'
import type { PriceError } from './useManagedPrices'
const props = defineProps<{ entry: PriceEntry; busy: boolean; canWrite: boolean; stale: boolean; image: boolean; error?: PriceError }>()
const emit = defineEmits<{ close: []; reload: []; save: [price: ModelPrice | null]; 'dirty-change': [dirty: boolean] }>()
const { t } = useI18n({ useScope: 'local', messages })
// Panel mounts one dialog per captured entry/version; failures never replace this draft.
const draft = ref<ModelPrice>(clonePrice(props.entry.override ?? props.entry.effective ?? {}))
const invalid = ref(false)
function draftStamp() {
  return JSON.stringify(draft.value, (_key, value: unknown) => {
    if (typeof value === 'number' && !Number.isFinite(value)) return 'invalid-number'
    // Deleting and restoring a field changes property order, but not the price draft.
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      return Object.fromEntries(Object.entries(value).sort(([left], [right]) => left.localeCompare(right)))
    }
    return value
  })
}
const original = draftStamp()
watch(draftStamp, value => emit('dirty-change', value !== original), { immediate: true, flush: 'sync' })
function removeMissingTokens(target: TokenPrice, source: TokenPrice) {
  for (const field of tokenFields) if (!(field in source)) delete target[field]
}
function updateTier(tier: PriceTier, value: TokenPrice) {
  Object.assign(tier, value)
  removeMissingTokens(tier, value)
}
function nestedState(value: TokenPrice | null | undefined) { return value === undefined ? 'absent' : value === null ? 'null' : 'custom' }
function setMode(mode: ServiceMode, state: string) {
  if (!draft.value.modes && state === 'absent') return
  draft.value.modes ??= {}
  if (state === 'absent') delete draft.value.modes[mode]
  else draft.value.modes[mode] = state === 'null' ? null : {}
}
function updateMode(mode: ServiceMode, value: TokenPrice) {
  draft.value.modes ??= {}
  draft.value.modes[mode] = value
}
function setImage(state: string) {
  if (state === 'absent') delete draft.value.image
  else draft.value.image = state === 'null' ? null : {}
}
function addTier() {
  draft.value.tiers ??= []
  let above = 200000
  while (draft.value.tiers.some(tier => tier.above === above)) above += 200000
  draft.value.tiers.push({ above })
}
function close() { if (!props.busy) emit('close') }
function save() {
  if (props.busy || !props.canWrite || props.stale) return
  try {
    requirePriceWrite(draft.value)
    invalid.value = false
    emit('save', clonePrice(draft.value))
  } catch { invalid.value = true }
}
</script>
