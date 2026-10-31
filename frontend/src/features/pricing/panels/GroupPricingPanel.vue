<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';

import Icon from '@/shared/ui/icons/Icon.vue';
import { useDraftSection } from '@/shared/lib/useDraftSection';
import type { GroupPricingDraft } from '../model/groupPricingDraft';
import type { GroupPlatform } from '@/features/access-policy/api/groupTypes';
import PricingEntryCard from '../components/PricingEntryCard.vue';
import { createDefaultTimePricingForm } from '../model/pricingForm';
import type { PricingFormEntry } from '../model/pricingForm';
import { isProfitControlPlatform } from '../model/groupsProfitControl';

type Draft = Pick<GroupPricingDraft, 'rate_multiplier' |  'audio_realtime_price_per_min' | 'audio_stt_price_per_hour' | 'audio_tts_price_per_million_chars' | 'long_context_pricing_enabled' | 'model_pricing' | 'profit_control_enabled' | 'profit_min_margin_percent' | 'profit_safety_buffer_percent' | 'search_price_per_1k' | 'web_search_price_per_call'>
const props = defineProps<{ modelValue: Readonly<Draft>; editable: (field: string) => boolean; platform: GroupPlatform }>()
const emit = defineEmits<{ patch: [draft: Partial<Draft>] }>()
const editForm = useDraftSection<Draft>(() => props.modelValue, patch => emit('patch', patch))
const { t } = useI18n()
const canEditGroupField = (field: string, _creating: boolean) => props.editable(field)
const emptyGroupPricing = (): PricingFormEntry => ({
  models: [],
  billing_mode: "token",
  input_price: null,
  output_price: null,
  cache_write_price: null,
  cache_write_1h_price: null,
  cache_read_price: null,
  reasoning_effort_multipliers: null,
  image_input_price: null,
  image_output_price: null,
  per_request_price: null,
  intervals: [],
  time_pricing: createDefaultTimePricingForm(),
});
const addGroupPricing = (entries: PricingFormEntry[]) =>
  entries.push(emptyGroupPricing());
const parsePreviewPrice = (value: number | string | null | undefined) => {
  if (value === null || value === undefined || value === "") {
    return null;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
};
const normalizePreviewNumber = (value: number | string | null | undefined, fallback = 0) => {
  if (value === null || value === undefined || value === "") {
    return fallback;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
};
const formatImagePricePreview = (value: number | string | null | undefined) => {
  if (value === null || value === undefined || value === "") {
    return t("admin.groups.imagePricing.notConfigured");
  }
  const price = Number(value);
  if (!Number.isFinite(price) || price < 0) {
    return t("admin.groups.imagePricing.notConfigured");
  }
  return `$${price.toFixed(6).replace(/0+$/, "").replace(/\.$/, "")}`;
};
const DEFAULT_WEB_SEARCH_PRICE_PER_CALL = 0.01;
const buildWebSearchFinalPricePreview = (form: {
  web_search_price_per_call: number | string | null;
  rate_multiplier: number | string | null;
}) => {
  const basePrice =
    parsePreviewPrice(form.web_search_price_per_call) ??
    DEFAULT_WEB_SEARCH_PRICE_PER_CALL;
  const multiplier = normalizePreviewNumber(form.rate_multiplier, 1);
  return formatImagePricePreview(basePrice * multiplier);
};
const editWebSearchFinalPricePreview = computed(() =>
  buildWebSearchFinalPricePreview(editForm),
);
</script>

<template>
<div class="space-y-5">
        <div v-if="isProfitControlPlatform(platform)" class="border-t pt-4">
          <label :inert="!canEditGroupField('profit_control_enabled', false)" :aria-disabled="!canEditGroupField('profit_control_enabled', false)" class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input
              v-model="editForm.profit_control_enabled"
              type="checkbox"
              class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
            />
            <span>{{ t("admin.groups.profitControl.enable") }}</span>
          </label>
          <p class="mb-3 mt-1.5 text-xs text-gray-500 dark:text-gray-400">
            {{
              editForm.profit_control_enabled
                ? t("admin.groups.profitControl.enabledHint")
                : t("admin.groups.profitControl.disabledHint")
            }}
          </p>
          <div :inert="!canEditGroupField('profit_control_enabled', false)" :aria-disabled="!canEditGroupField('profit_control_enabled', false)"
            v-if="editForm.profit_control_enabled"
            class="mb-3 grid grid-cols-1 gap-3 sm:grid-cols-2"
          >
            <div :inert="!canEditGroupField('profit_min_margin', false)" :aria-disabled="!canEditGroupField('profit_min_margin', false)">
              <label class="input-label">{{ t("admin.groups.profitControl.minMargin") }}</label>
              <input
                v-model.number="editForm.profit_min_margin_percent"
                type="number"
                step="0.1"
                min="0"
                max="99.99"
                class="input"
                placeholder="0"
                :title="t('admin.groups.profitControl.minMarginHint')"
              />
            </div>
            <div :inert="!canEditGroupField('profit_safety_buffer', false)" :aria-disabled="!canEditGroupField('profit_safety_buffer', false)">
              <label class="input-label">{{ t("admin.groups.profitControl.safetyBuffer") }}</label>
              <input
                v-model.number="editForm.profit_safety_buffer_percent"
                type="number"
                step="0.1"
                min="0"
                max="99.99"
                class="input"
                placeholder="0"
                :title="t('admin.groups.profitControl.safetyBufferHint')"
              />
            </div>
          </div>
        </div>
        <div
          v-if="platform === 'openai'"
          class="border-t border-gray-200 dark:border-dark-400 pt-4 mt-4"
        >
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
            {{ t("admin.groups.webSearchPricing.title") }}
          </h4>
          <div :inert="!canEditGroupField('web_search_price_per_call', false)" :aria-disabled="!canEditGroupField('web_search_price_per_call', false)">
            <label class="input-label">{{
              t("admin.groups.webSearchPricing.pricePerCall")
            }}</label>
            <input
              v-model.number="editForm.web_search_price_per_call"
              type="number"
              step="0.001"
              min="0"
              placeholder="0.01"
              class="input"
            />
            <p class="input-hint">
              {{ t("admin.groups.webSearchPricing.pricePerCallHint") }}
            </p>
            <div
              class="mt-2 rounded-lg bg-gray-50 p-3 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300"
            >
              {{
                t("admin.groups.webSearchPricing.finalPricePreview", {
                  price: editWebSearchFinalPricePreview,
                })
              }}
            </div>
          </div>
        </div>
        <div class="border-t border-gray-200 pt-4 mt-4 dark:border-dark-400">
          <div :inert="!canEditGroupField('model_pricing', false)" :aria-disabled="!canEditGroupField('model_pricing', false)" class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t("admin.groups.modelPricing.title") }}</h4>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t("admin.groups.modelPricing.description") }}</p>
            </div>
            <button type="button" class="btn btn-secondary shrink-0 whitespace-nowrap" @click="addGroupPricing(editForm.model_pricing)">
              <Icon name="plus" size="sm" class="mr-1" />{{ t("admin.groups.modelPricing.add") }}
            </button>
          </div>
          <label :inert="!canEditGroupField('long_context_pricing_enabled', false)" :aria-disabled="!canEditGroupField('long_context_pricing_enabled', false)" class="mt-3 flex items-start gap-2">
            <input v-model="editForm.long_context_pricing_enabled" type="checkbox" class="mt-0.5" />
            <span><span class="block text-sm text-gray-700 dark:text-gray-300">{{ t("admin.groups.modelPricing.longContext") }}</span><span class="block text-xs text-gray-500">{{ t("admin.groups.modelPricing.longContextHint") }}</span></span>
          </label>
          <div :inert="!canEditGroupField('model_pricing', false)" :aria-disabled="!canEditGroupField('model_pricing', false)" class="mt-3 space-y-2">
            <PricingEntryCard v-for="(entry, index) in editForm.model_pricing" :key="index" :entry="entry" :platform="platform" hide-token-intervals @update="editForm.model_pricing[index] = $event" @remove="editForm.model_pricing.splice(index, 1)" />
          </div>
        </div>
        <div
          v-if="platform === 'grok'"
          class="border-t border-gray-200 dark:border-dark-400 pt-4 mt-4"
        >
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
            {{ t("admin.groups.explicitPricing.title") }}
          </h4>
          <p class="text-xs text-gray-500 dark:text-gray-400 mb-3">
            {{ t("admin.groups.explicitPricing.description") }}
          </p>
          <div class="grid grid-cols-1 gap-3 md:grid-cols-3">
            <div :inert="!canEditGroupField('search_price_per_1k', false)" :aria-disabled="!canEditGroupField('search_price_per_1k', false)">
              <label class="input-label">{{ t("admin.groups.explicitPricing.searchPricePer1k") }}</label>
              <input
                v-model.number="editForm.search_price_per_1k"
                type="number"
                step="0.000001"
                min="0"
                class="input"
                :placeholder="t('admin.groups.explicitPricing.pricePlaceholder')"
                data-testid="edit-search-price"
              />
            </div>
            <div :inert="!canEditGroupField('audio_realtime_price_per_min', false)" :aria-disabled="!canEditGroupField('audio_realtime_price_per_min', false)">
              <label class="input-label">{{ t("admin.groups.voicePricing.audioRealtimePerMin") }}</label>
              <input
                v-model.number="editForm.audio_realtime_price_per_min"
                type="number"
                step="0.000001"
                min="0"
                class="input"
                :placeholder="t('admin.groups.voicePricing.pricePlaceholder')"
                data-testid="edit-audio-realtime-price"
              />
            </div>
            <div :inert="!canEditGroupField('audio_tts_price_per_million_chars', false)" :aria-disabled="!canEditGroupField('audio_tts_price_per_million_chars', false)">
              <label class="input-label">{{ t("admin.groups.voicePricing.audioTtsPerMillionChars") }}</label>
              <input
                v-model.number="editForm.audio_tts_price_per_million_chars"
                type="number"
                step="0.000001"
                min="0"
                class="input"
                :placeholder="t('admin.groups.voicePricing.pricePlaceholder')"
                data-testid="edit-audio-tts-price"
              />
            </div>
            <div :inert="!canEditGroupField('audio_stt_price_per_hour', false)" :aria-disabled="!canEditGroupField('audio_stt_price_per_hour', false)">
              <label class="input-label">{{ t("admin.groups.voicePricing.audioSttPerHour") }}</label>
              <input
                v-model.number="editForm.audio_stt_price_per_hour"
                type="number"
                step="0.000001"
                min="0"
                class="input"
                :placeholder="t('admin.groups.voicePricing.pricePlaceholder')"
                data-testid="edit-audio-stt-price"
              />
            </div>
          </div>
        </div>
        <!-- OpenAI Fast 开关（OpenAI 与 Composite 平台） -->
        <slot name="fast-policy" />
</div>
</template>
