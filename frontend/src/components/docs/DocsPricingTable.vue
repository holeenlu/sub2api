<template>
  <section class="space-y-3">
    <div class="flex flex-wrap items-end justify-between gap-3">
      <div>
        <h2 class="text-lg font-semibold text-gray-950 dark:text-white">{{ kind === 'image' ? t('docs.models.image') : t('docs.models.chat') }}</h2>
        <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('docs.priceUnit') }} · {{ t('docs.updatedAt', { date: DOCS_PRICING_VERIFIED_AT }) }}</p>
      </div>
      <div v-if="kind === 'chat'" class="inline-flex h-9 rounded-md border border-gray-200 bg-gray-50 p-0.5 dark:border-dark-700 dark:bg-dark-900" role="group">
        <button v-for="tier in ['standard', 'long'] as const" :key="tier" type="button" class="rounded px-3 text-xs font-medium" :class="contextTier === tier ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white' : 'text-gray-500 dark:text-dark-400'" @click="contextTier = tier">
          {{ tier === 'standard' ? t('docs.standardContext') : t('docs.longContext') }}
        </button>
      </div>
    </div>

    <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
      <table class="min-w-[760px] w-full border-collapse text-left text-sm">
        <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-900 dark:text-dark-400">
          <tr>
            <th class="px-4 py-3 font-medium">{{ t('docs.modelId') }}</th>
            <th v-for="column in columns" :key="column.key" class="px-3 py-3 font-medium">{{ t(column.label) }}</th>
            <th class="px-4 py-3 font-medium">{{ t('docs.currentGroup') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-800">
          <tr v-for="model in rows" :key="model.id" class="align-top">
            <td class="px-4 py-3">
              <RouterLink :to="`/docs/models/${model.id}`" class="font-mono text-xs font-semibold text-gray-900 hover:text-primary-600 dark:text-gray-100 dark:hover:text-primary-400">{{ model.id }}</RouterLink>
              <div class="mt-1 text-[11px] text-gray-400">{{ t(`docs.models.${model.platform}`) }}</div>
            </td>
            <td v-for="column in columns" :key="column.key" class="px-3 py-3 tabular-nums">
              <div v-if="priceValue(model, column.key) != null" class="flex items-center gap-1 whitespace-nowrap">
                <span class="text-xs text-gray-400">{{ formatPerMillion(priceValue(model, column.key)) }}</span>
                <span class="text-gray-300 dark:text-dark-600">→</span>
                <span class="font-semibold text-gray-900 dark:text-gray-100">{{ formatPerMillion(halfOfficialPrice(priceValue(model, column.key))) }}</span>
              </div>
              <div v-if="priceValue(model, column.key) != null" class="mt-0.5 text-[11px] text-gray-400">{{ t('docs.official') }} → {{ t('docs.tapmodels') }}</div>
              <span v-else class="text-gray-400">—</span>
            </td>
            <td class="px-4 py-3 tabular-nums">
              <template v-if="livePrice(model).length">
                <div v-for="line in livePrice(model)" :key="line" class="whitespace-nowrap text-xs font-medium leading-5 text-gray-900 dark:text-gray-100">{{ line }}</div>
                <div class="mt-0.5 text-[11px] text-gray-400">{{ selectedGroup?.name }}</div>
              </template>
              <span v-else class="text-gray-400">—</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p class="flex gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-200">
      <Icon name="infoCircle" size="sm" class="mt-0.5 shrink-0" />{{ selectedGroup ? t('docs.priceMismatch') : t('docs.priceFallback') }}
    </p>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import { docsPricingModels, DOCS_PRICING_VERIFIED_AT, formatPerMillion, halfOfficialPrice } from '@/content/docs/pricingSnapshot'
import type { DocsModelCatalogEntry } from '@/content/docs/types'

const props = defineProps<{ kind: 'chat' | 'image'; selectedGroup?: ModelPlazaGroup | null; modelIds?: string[] }>()
const { t } = useI18n()
const contextTier = ref<'standard' | 'long'>('standard')
const columns = computed(() => props.kind === 'image'
  ? [
      { key: 'textInput', label: 'docs.textInput' }, { key: 'cachedTextInput', label: 'docs.cachedTextInput' },
      { key: 'imageInput', label: 'docs.imageInput' }, { key: 'cachedImageInput', label: 'docs.cachedImageInput' }, { key: 'imageOutput', label: 'docs.imageOutput' }
    ]
  : [
      { key: 'input', label: 'docs.input' }, { key: 'cachedInput', label: 'docs.cachedInput' },
      { key: 'cacheWrite', label: 'docs.cacheWrite' }, { key: 'cacheWrite1h', label: 'docs.cacheWrite1h' }, { key: 'output', label: 'docs.output' }
    ])
const rows = computed(() => docsPricingModels.filter((model) => model.kind === props.kind && (!props.modelIds || props.modelIds.includes(model.id))))

function priceValue(model: DocsModelCatalogEntry, key: string): number | undefined {
  if (model.imagePricing) return model.imagePricing[key as keyof typeof model.imagePricing]
  const pricing = contextTier.value === 'long' && model.tokenPricing?.longContext ? model.tokenPricing.longContext : model.tokenPricing
  return pricing?.[key as keyof typeof pricing] as number | undefined
}

function livePrice(model: DocsModelCatalogEntry): string[] {
  const live = props.selectedGroup?.models.find((item) => item.name === model.id)
  if (!live?.pricing) return []
  const rate = props.selectedGroup?.image_rate_independent && live.pricing.billing_mode === 'image'
    ? props.selectedGroup.image_rate_multiplier
    : (props.selectedGroup?.user_rate_multiplier ?? props.selectedGroup?.rate_multiplier ?? 1)
  if (live.pricing.billing_mode !== 'token') {
    if (live.pricing.per_request_price == null) return []
    return [`${formatPerMillion(live.pricing.per_request_price * rate)} / ${live.pricing.billing_mode === 'image' ? t('docs.unitImage') : t('docs.unitRequest')}`]
  }
  const scaled = (value: number | null | undefined) => value == null ? '—' : formatPerMillion(value * rate * 1_000_000)
  if (model.kind === 'image') {
    return [
      `${t('docs.textInput')} ${scaled(live.pricing.input_price)} · ${t('docs.imageInput')} ${scaled(live.pricing.image_input_price)}`,
      `${t('docs.imageOutput')} ${scaled(live.pricing.image_output_price)}`
    ]
  }
  return [
    `${t('docs.input')} ${scaled(live.pricing.input_price)} · ${t('docs.cachedInput')} ${scaled(live.pricing.cache_read_price)}`,
    `${t('docs.cacheWrite')} ${scaled(live.pricing.cache_write_price)} · ${t('docs.output')} ${scaled(live.pricing.output_price)}`
  ]
}
</script>
