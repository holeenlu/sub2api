<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';

import { useDraftSection } from '@/shared/lib/useDraftSection';
import type { GroupPricingDraft } from '../model/groupPricingDraft';

import { canConfigureImageCountBilling, getDefaultImagePreviewPrice, getDefaultVideoPreviewPrice, getImagePricePlaceholder, getVideoPricePlaceholder, imagePricingI18nKey, supportsImagePricingPlatform, supportsVideoPricingPlatform, videoPricingI18nKey } from '../model/groupsImagePricing';
import { grokVideoPriceResolutions, videoModelPriceFamilyRows } from '../model/groupsVideoModelPricing';
import type { GroupPlatform } from '@/features/access-policy/api/groupTypes';
type Draft = Pick<GroupPricingDraft, 'rate_multiplier' | 'image_count_billing' | 'batch_image_discount_multiplier' | 'batch_image_hold_multiplier' | 'image_price_1k' | 'image_price_2k' | 'image_price_4k' | 'image_rate_independent' | 'image_rate_multiplier' | 'video_model_prices' | 'video_price_1080p' | 'video_price_480p' | 'video_price_720p' | 'video_rate_independent' | 'video_rate_multiplier'>
const props = defineProps<{ modelValue: Readonly<Draft>; editable: (field: string) => boolean; platform: GroupPlatform; imageAllowed: boolean; batchImagesAllowed: boolean }>()
const emit = defineEmits<{ patch: [draft: Partial<Draft>] }>()
const editForm = useDraftSection<Draft>(() => props.modelValue, patch => emit('patch', patch))
const { t } = useI18n()
const canEditGroupField = (field: string, _creating: boolean) => props.editable(field)
type ImagePricingFormState = {
  platform: GroupPlatform;
  rate_multiplier: number;
  image_rate_independent: boolean;
  image_rate_multiplier: number;
  batch_image_discount_multiplier: number;
  batch_image_hold_multiplier: number;
  image_price_1k: number | string | null;
  image_price_2k: number | string | null;
  image_price_4k: number | string | null;
};
type VideoPricingFormState = {
  platform: GroupPlatform;
  rate_multiplier: number;
  video_rate_independent: boolean;
  video_rate_multiplier: number;
  video_price_480p: number | string | null;
  video_price_720p: number | string | null;
  video_price_1080p: number | string | null;
};
const imagePricingTiers = [
  { key: "image_price_1k", label: "1K" },
  { key: "image_price_2k", label: "2K" },
  { key: "image_price_4k", label: "4K" },
] as const;
const videoPricingTiers = [
  { key: "video_price_480p", label: "480p" },
  { key: "video_price_720p", label: "720p" },
  { key: "video_price_1080p", label: "1080p" },
] as const;
const normalizePreviewNumber = (value: number | string | null | undefined, fallback = 0) => {
  if (value === null || value === undefined || value === "") {
    return fallback;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
};
const parsePreviewPrice = (value: number | string | null | undefined) => {
  if (value === null || value === undefined || value === "") {
    return null;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
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
const formatVideoPricePreview = (value: number | string | null | undefined) => {
  if (value === null || value === undefined || value === "") {
    return t("admin.groups.videoPricing.notConfigured");
  }
  const price = Number(value);
  if (!Number.isFinite(price) || price < 0) {
    return t("admin.groups.videoPricing.notConfigured");
  }
  return `$${price.toFixed(6).replace(/0+$/, "").replace(/\.$/, "")}`;
};
const buildImageFinalPricePreview = (form: ImagePricingFormState) => {
  const imageMultiplier = form.image_rate_independent
    ? normalizePreviewNumber(form.image_rate_multiplier, 1)
    : normalizePreviewNumber(form.rate_multiplier, 1);
  const multiplier = imageMultiplier;
  return imagePricingTiers.map((tier) => {
    const basePrice =
      parsePreviewPrice(form[tier.key]) ??
      getDefaultImagePreviewPrice(form.platform, tier.key);
    return {
      label: tier.label,
      value: basePrice !== null
        ? formatImagePricePreview(basePrice * multiplier)
        : t("admin.groups.imagePricing.notConfigured"),
    };
  });
};
const buildVideoFinalPricePreview = (form: VideoPricingFormState) => {
  const multiplier = form.video_rate_independent
    ? normalizePreviewNumber(form.video_rate_multiplier, 1)
    : normalizePreviewNumber(form.rate_multiplier, 1);
  return videoPricingTiers.map((tier) => {
    const basePrice =
      parsePreviewPrice(form[tier.key]) ??
      getDefaultVideoPreviewPrice(form.platform, tier.key);
    return {
      label: tier.label,
      value: basePrice !== null
        ? formatVideoPricePreview(basePrice * multiplier)
        : t("admin.groups.videoPricing.notConfigured"),
    };
  });
};
const editImageFinalPricePreview = computed(() =>
  buildImageFinalPricePreview({ ...editForm, platform: props.platform }),
);
const editVideoFinalPricePreview = computed(() =>
  buildVideoFinalPricePreview({ ...editForm, platform: props.platform }),
);
</script>

<template>
<div class="space-y-5">
        <div
          v-if="supportsImagePricingPlatform(platform)"
          class="border-t pt-4"
        >
          <label
            class="block mb-2 font-medium text-gray-700 dark:text-gray-300"
          >
            {{ t(imagePricingI18nKey(platform, "title")) }}
          </label>
          <p class="text-xs text-gray-500 dark:text-gray-400 mb-3">
            {{ t(imagePricingI18nKey(platform, "description")) }}
          </p>
          <div class="mb-4 grid grid-cols-1 gap-3 md:grid-cols-2">
            <slot name="image-admission" />
            <label :inert="!canEditGroupField('image_rate_independent', false)" :aria-disabled="!canEditGroupField('image_rate_independent', false)" class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                v-model="editForm.image_rate_independent"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              {{ t(imagePricingI18nKey(platform, "independentMultiplier")) }}
            </label>
            <label
              v-if="canConfigureImageCountBilling(platform, imageAllowed)"
              :inert="!editable('image_count_billing')"
              :aria-disabled="!editable('image_count_billing')"
              class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300"
            >
              <input
                v-model="editForm.image_count_billing"
                type="checkbox"
                :disabled="!editable('image_count_billing')"
                data-testid="group-image-count-billing"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              {{ t('admin.groups.imagePricing.perImageBilling') }}
            </label>
          </div>
          <div :inert="!canEditGroupField('image_rate_multiplier', false)" :aria-disabled="!canEditGroupField('image_rate_multiplier', false)"
            v-if="editForm.image_rate_independent"
            class="mb-4"
          >
            <label class="input-label">{{
              t(imagePricingI18nKey(platform, "imageMultiplier"))
            }}</label>
            <input
              v-model.number="editForm.image_rate_multiplier"
              type="number"
              step="0.0001"
              min="0"
              class="input"
              placeholder="1"
            />
          </div>
          <div class="grid grid-cols-3 gap-3">
            <div :inert="!canEditGroupField('image_price_1k', false)" :aria-disabled="!canEditGroupField('image_price_1k', false)">
              <label class="input-label">1K ($)</label>
              <input
                v-model.number="editForm.image_price_1k"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getImagePricePlaceholder(platform, 'image_price_1k')"
              />
            </div>
            <div :inert="!canEditGroupField('image_price_2k', false)" :aria-disabled="!canEditGroupField('image_price_2k', false)">
              <label class="input-label">2K ($)</label>
              <input
                v-model.number="editForm.image_price_2k"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getImagePricePlaceholder(platform, 'image_price_2k')"
              />
            </div>
            <div :inert="!canEditGroupField('image_price_4k', false)" :aria-disabled="!canEditGroupField('image_price_4k', false)">
              <label class="input-label">4K ($)</label>
              <input
                v-model.number="editForm.image_price_4k"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getImagePricePlaceholder(platform, 'image_price_4k')"
              />
            </div>
          </div>
          <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t(imagePricingI18nKey(platform, "modeHint")) }}
          </p>
          <div class="mt-2 rounded-lg bg-gray-50 p-3 text-xs text-gray-700 dark:bg-gray-800 dark:text-gray-300">
            <div class="mb-1 font-medium">
              {{ t(imagePricingI18nKey(platform, "finalPricePreview")) }}
            </div>
            <div class="grid grid-cols-3 gap-2">
              <div
                v-for="item in editImageFinalPricePreview"
                :key="item.label"
              >
                {{ item.label }}: {{ item.value }}
              </div>
            </div>
          </div>
          <div v-if="platform === 'gemini' && imageAllowed" class="mt-4 border-t border-dashed border-gray-200 pt-4 dark:border-dark-700">
            <slot name="batch-admission" />
            <div
              v-if="batchImagesAllowed"
              class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2"
            >
              <div :inert="!canEditGroupField('batch_image_discount_multiplier', false)" :aria-disabled="!canEditGroupField('batch_image_discount_multiplier', false)">
                <label class="input-label">{{
                  t("admin.groups.imagePricing.batchDiscountMultiplier")
                }}</label>
                <input
                  v-model.number="editForm.batch_image_discount_multiplier"
                  type="number"
                  step="0.0001"
                  min="0"
                  class="input"
                  placeholder="0.5"
                />
              </div>
              <div :inert="!canEditGroupField('batch_image_hold_multiplier', false)" :aria-disabled="!canEditGroupField('batch_image_hold_multiplier', false)">
                <label class="input-label">{{
                  t("admin.groups.imagePricing.batchHoldMultiplier")
                }}</label>
                <input
                  v-model.number="editForm.batch_image_hold_multiplier"
                  type="number"
                  step="0.0001"
                  min="0"
                  class="input"
                  placeholder="0.6"
                />
              </div>
            </div>
          </div>
          <p
            v-else-if="platform !== 'gemini'"
            class="mt-4 border-t border-dashed border-gray-200 pt-4 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400"
          >
            {{ t("admin.groups.imagePricing.batchGeminiOnlyHint") }}
          </p>
        </div>

        <!-- 视频生成计费配置（仅 Grok 平台） -->
        <div
          v-if="supportsVideoPricingPlatform(platform)"
          class="border-t pt-4"
        >
          <label
            class="block mb-2 font-medium text-gray-700 dark:text-gray-300"
          >
            {{ t(videoPricingI18nKey("title")) }}
          </label>
          <p class="text-xs text-gray-500 dark:text-gray-400 mb-3">
            {{ t(videoPricingI18nKey("description")) }}
          </p>
          <div class="mb-4">
            <label :inert="!canEditGroupField('video_rate_independent', false)" :aria-disabled="!canEditGroupField('video_rate_independent', false)" class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                v-model="editForm.video_rate_independent"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              {{ t(videoPricingI18nKey("independentMultiplier")) }}
            </label>
          </div>
          <div :inert="!canEditGroupField('video_rate_multiplier', false)" :aria-disabled="!canEditGroupField('video_rate_multiplier', false)"
            v-if="editForm.video_rate_independent"
            class="mb-4"
          >
            <label class="input-label">{{
              t(videoPricingI18nKey("videoMultiplier"))
            }}</label>
            <input
              v-model.number="editForm.video_rate_multiplier"
              type="number"
              step="0.0001"
              min="0"
              class="input"
              placeholder="1"
            />
          </div>
          <div class="grid grid-cols-3 gap-3">
            <div :inert="!canEditGroupField('video_price_480p', false)" :aria-disabled="!canEditGroupField('video_price_480p', false)">
              <label class="input-label">480p ($/s)</label>
              <input
                v-model.number="editForm.video_price_480p"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getVideoPricePlaceholder(platform, 'video_price_480p')"
              />
            </div>
            <div :inert="!canEditGroupField('video_price_720p', false)" :aria-disabled="!canEditGroupField('video_price_720p', false)">
              <label class="input-label">720p ($/s)</label>
              <input
                v-model.number="editForm.video_price_720p"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getVideoPricePlaceholder(platform, 'video_price_720p')"
              />
            </div>
            <div :inert="!canEditGroupField('video_price_1080p', false)" :aria-disabled="!canEditGroupField('video_price_1080p', false)">
              <label class="input-label">1080p ($/s)</label>
              <input
                v-model.number="editForm.video_price_1080p"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getVideoPricePlaceholder(platform, 'video_price_1080p')"
              />
            </div>
          </div>
          <div
            class="mt-4 border-t border-dashed border-gray-200 pt-4 dark:border-dark-700"
            data-testid="edit-grok-video-model-prices"
          >
            <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.videoPricing.modelOverridesTitle") }}
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.groups.videoPricing.modelOverridesDescription") }}
            </p>
            <div class="mt-3 space-y-3">
              <div :inert="!canEditGroupField('video_model_prices', false)" :aria-disabled="!canEditGroupField('video_model_prices', false)"
                v-for="family in videoModelPriceFamilyRows(editForm.video_model_prices)"
                :key="family.key"
                class="grid gap-2 sm:grid-cols-[minmax(0,1fr)_repeat(3,minmax(0,7rem))] sm:items-end"
              >
                <div class="min-w-0 pb-1 font-mono text-xs text-gray-700 dark:text-gray-300">
                  {{ family.label }}
                </div>
                <label :inert="!canEditGroupField('video_model_prices', false)" :aria-disabled="!canEditGroupField('video_model_prices', false)"
                  v-for="resolution in grokVideoPriceResolutions"
                  :key="resolution.key"
                  class="block"
                >
                  <span class="mb-1 block text-xs text-gray-500 dark:text-gray-400">
                    {{ resolution.label }} ($/s)
                  </span>
                  <input
                    v-model.number="editForm.video_model_prices[family.key][resolution.key]"
                    type="number"
                    step="0.001"
                    min="0"
                    class="input"
                    :data-testid="`edit-grok-video-price-${family.key}-${resolution.key}`"
                  />
                </label>
              </div>
            </div>
          </div>
          <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t(videoPricingI18nKey("modeHint")) }}
          </p>
          <div class="mt-2 rounded-lg bg-gray-50 p-3 text-xs text-gray-700 dark:bg-gray-800 dark:text-gray-300">
            <div class="mb-1 font-medium">
              {{ t(videoPricingI18nKey("finalPricePreview")) }}
            </div>
            <div class="grid grid-cols-3 gap-2">
              <div
                v-for="item in editVideoFinalPricePreview"
                :key="item.label"
              >
                {{ item.label }}: {{ item.value }}
              </div>
            </div>
          </div>
        </div>
</div>
</template>
