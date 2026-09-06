<template>
  <div data-testid="model-showcase">
    <p v-if="loading" class="py-8 text-center text-sm text-gray-500 dark:text-dark-400">
      {{ t('home.models.loading') }}
    </p>

    <template v-else-if="cards.length">
      <div class="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
        <div v-for="card in cards" :key="card.key" :class="cardClass">
          <p class="break-all font-mono text-[14.5px] font-semibold text-gray-900 dark:text-white">
            {{ card.model }}
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ card.group }}</p>
          <div :class="priceRowClass">
            <div class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('modelPlaza.table.input') }}
              <b :class="priceValueClass">{{ card.input }}</b>
            </div>
            <div class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('modelPlaza.table.output') }}
              <b :class="priceValueClass">{{ card.output }}</b>
            </div>
          </div>
        </div>
      </div>
    </template>

    <!-- 没有公开模型（或载入失败）时，用示意卡片撑起版面 -->
    <template v-else>
      <div class="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
        <div
          v-for="card in sampleCards"
          :key="card.model"
          :class="cardClass"
          data-testid="sample-model-card"
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <p
                class="break-all font-mono text-[14.5px] font-semibold text-gray-900 dark:text-white"
              >
                {{ card.model }}
              </p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ card.group }}</p>
            </div>
            <span :class="demoPillClass">{{ t('home.management.demoLabel') }}</span>
          </div>
          <div :class="priceRowClass">
            <div class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('home.models.inputPer1M') }}
              <b :class="priceValueClass">{{ card.input }}</b>
            </div>
            <div class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('home.models.outputPer1M') }}
              <b :class="priceValueClass">{{ card.output }}</b>
            </div>
          </div>
        </div>
      </div>
      <p class="mt-4 text-[13px] text-gray-500 dark:text-dark-400" data-testid="sample-model-note">
        {{ t('home.models.sampleNote') }}
      </p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getModelPlaza, type ModelPlazaResponse } from '@/api/modelPlaza'
import { formatScaled } from '@/utils/pricing'
import { SAMPLE_MODELS } from './sampleModels'

const { t } = useI18n()

/** 首页只放前 6 个「模型＋分组」，完整清单交给模型广场。 */
const MAX_CARDS = 6
const PER_MILLION = 1_000_000
const MIN_DECIMALS = 2

const loading = ref(true)
const plaza = ref<ModelPlazaResponse | null>(null)

/**
 * 与模型广场同一套实付口径：单价 × 生效倍率（专属倍率优先），按 $ / 1M token 展示。
 * 未知价格显示 '-'，不补 0。
 */
const cards = computed(() => {
  const rows: { key: string; model: string; group: string; input: string; output: string }[] = []
  for (const group of plaza.value?.groups ?? []) {
    const rate = group.user_rate_multiplier ?? group.rate_multiplier
    for (const model of group.models ?? []) {
      if (rows.length >= MAX_CARDS) return rows
      rows.push({
        key: `${group.id}:${model.platform}:${model.name}`,
        model: model.name,
        group: group.name,
        input: paid(model.pricing?.input_price, rate),
        output: paid(model.pricing?.output_price, rate)
      })
    }
  }
  return rows
})

/** 示意卡片：价格已是 USD / 1M tokens，故不再乘以倍率或缩放。 */
const sampleCards = computed(() =>
  SAMPLE_MODELS.map((item) => ({
    model: item.model,
    group: t('home.models.sampleGroup', { provider: item.provider }),
    input: sampleUsd(item.input),
    output: sampleUsd(item.output)
  }))
)

function paid(value: number | null | undefined, rate: number): string {
  if (value == null) return '-'
  return formatScaled(value * rate, PER_MILLION, MIN_DECIMALS)
}

/** 与真实卡片共用同一个格式化器，另加 US 前缀标明币别（US$3.00）。 */
function sampleUsd(value: number): string {
  return `US${formatScaled(value, 1, MIN_DECIMALS)}`
}

async function load() {
  loading.value = true
  try {
    plaza.value = await getModelPlaza()
  } catch {
    plaza.value = null
  } finally {
    loading.value = false
  }
}

onMounted(load)

const cardClass =
  'rounded-2xl border border-gray-200/70 bg-white/80 p-5 shadow-sm backdrop-blur-sm dark:border-dark-700/70 dark:bg-dark-800/70'
const priceRowClass =
  'mt-4 flex gap-6 border-t border-dashed border-gray-200 pt-3.5 dark:border-dark-700'
const priceValueClass =
  'mt-0.5 block font-mono text-[15px] font-semibold text-gray-900 dark:text-white'
const demoPillClass =
  'inline-flex shrink-0 items-center rounded-full border border-gray-200 bg-gray-100 px-2.5 py-0.5 text-[11px] font-semibold text-gray-500 dark:border-dark-600 dark:bg-dark-700 dark:text-dark-300'
</script>
