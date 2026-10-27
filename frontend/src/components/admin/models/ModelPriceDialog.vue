<template>
  <BaseDialog :show="!!entry" :title="entry?.id || ''" width="wide" @close="emit('close')">
    <form v-if="draft" id="model-price-form" class="space-y-5" @submit.prevent="save">
      <p class="text-xs text-gray-500">{{ t('modelManagement.priceUnit') }}</p>
      <TokenPriceFields :model-value="draft" @update:model-value="Object.assign(draft!, $event)" />
      <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
        <div class="flex items-center justify-between gap-2"><h4 class="font-medium">{{ t('modelManagement.contextTiers') }}</h4><button type="button" class="btn btn-secondary btn-sm" @click="addTier">{{ t('modelManagement.addTier') }}</button></div>
        <div v-for="(tier, index) in draft.tiers" :key="index" class="space-y-3 rounded-xl bg-gray-50 p-3 dark:bg-dark-800">
          <div class="flex flex-wrap items-end gap-3">
            <label class="flex-1 text-xs">{{ t('modelManagement.above') }}<input v-model.number="tier.above" type="number" min="1" step="1" class="input mt-1 w-full" required /></label>
            <label class="flex items-center gap-2 py-2 text-sm"><input v-model="tier.unpriced" type="checkbox" />{{ t('modelManagement.unpriced') }}</label>
            <button type="button" class="btn btn-secondary" @click="draft.tiers!.splice(index, 1)">{{ t('common.delete') }}</button>
          </div>
          <TokenPriceFields v-if="!tier.unpriced" :model-value="tier" @update:model-value="Object.assign(tier, $event)" />
        </div>
      </section>
      <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
        <h4 class="font-medium">{{ t('modelManagement.serviceModes') }}</h4>
        <div v-for="mode in modes" :key="mode" class="space-y-3">
          <label class="flex items-center gap-2 text-sm"><input type="checkbox" :checked="draft.modes?.[mode] != null" @change="toggleMode(mode, ($event.target as HTMLInputElement).checked)" />{{ mode }}</label>
          <TokenPriceFields v-if="draft.modes?.[mode]" :model-value="draft.modes[mode]!" @update:model-value="draft.modes![mode] = $event" />
        </div>
      </section>
      <section v-if="image" class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
        <h4 class="font-medium">{{ t('modelManagement.imagePrices') }}</h4>
        <TokenPriceFields v-model="imageDraft" image />
      </section>
    </form>
    <template #footer>
      <div class="flex w-full flex-wrap justify-between gap-2">
        <button type="button" class="btn btn-secondary" :disabled="busy || !entry?.override" @click="emit('save', null)">{{ t('modelManagement.resetOverride') }}</button>
        <div class="flex gap-2"><button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button><button class="btn btn-primary" type="submit" form="model-price-form" :disabled="busy">{{ t('common.save') }}</button></div>
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
