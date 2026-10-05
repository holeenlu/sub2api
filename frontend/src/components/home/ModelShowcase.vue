<template>
  <div data-testid="model-showcase" class="space-y-5">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200/70 pb-4 dark:border-dark-700">
      <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('home.models.priceUnit') }}</p>
      <div class="flex flex-wrap gap-4">
        <a v-for="group in OFFICIAL_MODEL_GROUPS" :key="group.provider" :href="group.pricingSource" target="_blank" rel="noopener noreferrer" class="text-xs text-gray-500 transition-colors hover:text-primary-600 dark:text-dark-400">
          {{ group.provider }} · {{ t('home.models.officialPricing') }} ↗
        </a>
      </div>
    </div>
    <p v-if="loading" class="py-6 text-sm text-gray-500" role="status">{{ t('home.models.loading') }}</p>
    <div v-else-if="loadError" class="py-6 text-sm text-gray-500" role="alert">
      {{ t('home.models.error') }}
      <button type="button" class="ml-2 text-primary-600 hover:underline" @click="load">{{ t('home.models.retry') }}</button>
    </div>
    <p v-else-if="!cards.length" class="py-6 text-sm text-gray-500">{{ t('home.models.empty') }}</p>
    <div v-else class="grid grid-cols-1 items-start gap-3 md:grid-cols-2 lg:grid-cols-3 lg:gap-4">
        <article v-for="card in cards" :key="card.key" :class="cardClass" data-testid="channel-model-card">
          <div class="flex items-start gap-3">
            <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-gray-50 dark:bg-dark-700" :class="card.platform === 'anthropic' ? 'text-orange-600' : 'text-gray-900 dark:text-white'">
              <PlatformIcon :platform="card.platform" size="md" />
            </div>
            <div class="min-w-0 flex-1">
              <h4 class="break-all font-mono text-[15px] font-semibold text-gray-900 dark:text-white">{{ card.model }}</h4>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ card.channelName || t('home.models.channelUnavailable') }}</p>
            </div>
            <button type="button" :aria-label="t('home.models.copyModel', { model: card.model })" class="shrink-0 rounded p-1 text-gray-500 hover:bg-gray-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:hover:bg-dark-700" @click="copyToClipboard(card.model)">
              <svg aria-hidden="true" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8">
                <rect x="8" y="8" width="12" height="12" rx="2" />
                <path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3" />
              </svg>
            </button>
          </div>
          <p class="my-3 text-[13px] leading-relaxed text-gray-600 dark:text-dark-300">{{ t(`home.models.introductions.${card.descriptionKey}`) }}</p>
          <div>
            <dl class="grid grid-cols-3 gap-2 rounded-xl bg-gray-50/90 px-3 py-2.5 dark:bg-dark-900/50">
              <div v-for="field in priceFields" :key="field">
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t(`home.models.${field}`) }}</dt>
                <dd class="mt-1 font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ usd(card[field]) }}</dd>
              </div>
            </dl>
            <a v-if="card.source" :href="card.source" target="_blank" rel="noopener noreferrer" class="mt-2 inline-block text-[11px] text-gray-500 hover:text-primary-600 hover:underline dark:text-dark-400">{{ t('home.models.modelDetails') }} ↗</a>
          </div>
        </article>
    </div>
    <details class="text-xs text-gray-500 dark:text-dark-400">
      <summary class="cursor-pointer select-none transition-colors hover:text-primary-600">{{ t('home.models.pricingDetails') }} · {{ t('home.models.verifiedAt', { date: PRICING_VERIFIED_AT }) }}</summary>
      <div class="mt-3 space-y-2 rounded-xl bg-gray-50/80 p-4 leading-6 dark:bg-dark-800/60">
        <p v-for="group in OFFICIAL_MODEL_GROUPS" :key="group.provider"><span class="font-medium">{{ group.provider }} — </span>{{ t(`home.models.${group.noteKey}`) }}</p>
      </div>
    </details>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useModelPlaza } from '@/composables/useModelPlaza'
import type { GroupPlatform } from '@/types'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { formatScaled } from '@/utils/pricing'
import { OFFICIAL_MODEL_GROUPS, PRICING_VERIFIED_AT } from './officialModels'

const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const { data: plaza, loading, loadFailed: loadError, load } = useModelPlaza()
const descriptions = OFFICIAL_MODEL_GROUPS.flatMap(group => group.models)
const cards = computed(() => (plaza.value?.groups ?? []).flatMap(group =>
  group.models.map(model => {
    const fixed = descriptions.find(item => item.model === model.name)
    const provider = OFFICIAL_MODEL_GROUPS.find(item => item.platform === model.platform)
    const rate = group.user_rate_multiplier ?? group.rate_multiplier
    const pricing = model.pricing
    const tokenPricing = !pricing?.billing_mode || pricing.billing_mode === 'token'
    const paid = (price: number | null | undefined) => tokenPricing && price != null ? price * rate : null
    return {
      key: `${group.id}:${model.platform}:${model.name}`,
      model: model.name,
      platform: model.platform as GroupPlatform,
      channelName: group.name,
      descriptionKey: fixed?.descriptionKey ?? 'generic',
      source: fixed?.source ?? provider?.pricingSource,
      input: paid(pricing?.input_price),
      output: paid(pricing?.output_price),
      cacheRead: paid(pricing?.cache_read_price)
    }
  })
).slice(0, 6))

const priceFields = ['input', 'output', 'cacheRead'] as const
const usd = (value: number | null) => value == null ? '—' : `US${formatScaled(value, 1_000_000, 2)}`
const cardClass = 'flex min-w-0 flex-col rounded-2xl border border-gray-200/70 bg-white/80 p-4 shadow-sm backdrop-blur-sm transition duration-200 hover:border-primary-200 hover:shadow-md motion-reduce:transition-none dark:border-dark-700/70 dark:bg-dark-800/70'
</script>
