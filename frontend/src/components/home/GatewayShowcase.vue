<template>
  <div data-testid="gateway-showcase" class="grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3">
    <!-- 逐笔用量与费用 -->
    <section :class="cardClass">
      <header :class="headerClass">
        <h3 :class="titleClass">{{ t('home.management.usage.title') }}</h3>
        <span :class="demoPillClass">{{ t('home.management.demoLabel') }}</span>
      </header>
      <p :class="descClass">{{ t('home.management.usage.description') }}</p>
      <div :class="panelClass">
        <table class="w-full border-collapse text-[12.5px]">
          <thead>
            <tr>
              <th :class="thClass">{{ t('home.management.fields.model') }}</th>
              <th :class="[thClass, 'text-right']">{{ t('home.management.fields.tokens') }}</th>
              <th :class="[thClass, 'text-right']">{{ t('home.management.fields.cost') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in usageRows" :key="row.label" class="[&:last-child>td]:border-b-0">
              <td :class="[tdClass, 'text-gray-900 dark:text-white']">{{ row.label }}</td>
              <td :class="[tdClass, 'text-right']">{{ integer.format(row.tokens) }}</td>
              <td :class="[tdClass, 'text-right']">{{ usd4.format(row.cost) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- 使用者、分组与金钥 -->
    <section :class="cardClass">
      <header :class="headerClass">
        <h3 :class="titleClass">{{ t('home.management.keys.title') }}</h3>
        <span :class="demoPillClass">{{ t('home.management.demoLabel') }}</span>
      </header>
      <p :class="descClass">{{ t('home.management.keys.description') }}</p>
      <div :class="panelClass">
        <div
          v-for="row in keyRows"
          :key="row.masked"
          class="flex items-center justify-between gap-2.5 border-b border-gray-100 py-2.5 last:border-b-0 dark:border-dark-700/70"
        >
          <div class="min-w-0">
            <p class="truncate text-[13px] font-medium text-gray-900 dark:text-white">{{ row.name }}</p>
            <p class="mt-0.5 truncate text-[11px] text-gray-500 dark:text-dark-400">{{ row.group }}</p>
          </div>
          <code
            class="shrink-0 rounded-md bg-gray-100 px-2 py-0.5 font-mono text-[11.5px] text-gray-500 dark:bg-dark-700 dark:text-dark-300"
          >
            {{ row.masked }}
          </code>
        </div>
      </div>
    </section>

    <!-- 配额与请求限制 -->
    <section :class="cardClass">
      <header :class="headerClass">
        <h3 :class="titleClass">{{ t('home.management.quotas.title') }}</h3>
        <span :class="demoPillClass">{{ t('home.management.demoLabel') }}</span>
      </header>
      <p :class="descClass">{{ t('home.management.quotas.description') }}</p>
      <div :class="panelClass">
        <div class="flex justify-between text-xs text-gray-500 dark:text-dark-400">
          <span>{{ t('home.management.quotas.monthlyQuota') }}</span>
          <span>{{ usd2.format(QUOTA_USED) }} / {{ usd2.format(QUOTA_TOTAL) }}</span>
        </div>
        <div class="mt-2 h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
          <div
            class="h-full rounded-full bg-gradient-to-r from-primary-500 to-primary-600"
            :style="{ width: quotaPercent }"
          ></div>
        </div>
        <div
          class="mt-4 flex items-baseline justify-between border-t border-dashed border-gray-200 pt-3.5 dark:border-dark-700"
        >
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('home.management.fields.rpm') }}
          </span>
          <span class="font-mono text-xl font-semibold text-gray-900 dark:text-white">
            {{ integer.format(RPM_LIMIT) }}
          </span>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIntlLocale } from '@/i18n/localeUtils'

const { t, locale } = useI18n()

/**
 * 示意数据：只用已有栏位的结构化常值，不放假按钮、假趋势或未实作的控制项。
 * 金钥一律遮罩，不呈现可还原的字串。
 */
const USAGE_TOKENS = [18420, 9640, 42180, 5310]
const USAGE_COSTS = [0.1836, 0.0721, 0.2604, 0.0058]
const MASKED_KEYS = ['sk-…a1b2', 'sk-…7f3e', 'sk-…c9d0']
const SUFFIXES = ['A', 'B', 'C', 'D']
const QUOTA_USED = 124
const QUOTA_TOTAL = 200
const RPM_LIMIT = 120

const intlLocale = computed(() => getIntlLocale(String(locale.value)))
const integer = computed(() => new Intl.NumberFormat(intlLocale.value))
const usd2 = computed(
  () =>
    new Intl.NumberFormat(intlLocale.value, {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 2,
      maximumFractionDigits: 2
    })
)
const usd4 = computed(
  () =>
    new Intl.NumberFormat(intlLocale.value, {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 4,
      maximumFractionDigits: 4
    })
)

const usageRows = computed(() =>
  USAGE_TOKENS.map((tokens, index) => ({
    label: `${t('home.management.sample.model')} ${SUFFIXES[index]}`,
    tokens,
    cost: USAGE_COSTS[index]
  }))
)

const keyRows = computed(() =>
  MASKED_KEYS.map((masked, index) => ({
    masked,
    name: `${t('home.management.sample.key')} ${SUFFIXES[index]}`,
    group: `${t('home.management.sample.group')} ${SUFFIXES[index]}`
  }))
)

const quotaPercent = computed(() => `${Math.round((QUOTA_USED / QUOTA_TOTAL) * 100)}%`)

const cardClass =
  'flex flex-col rounded-2xl border border-gray-200/70 bg-white/80 p-5 shadow-sm backdrop-blur-sm dark:border-dark-700/70 dark:bg-dark-800/70'
const headerClass = 'flex items-start justify-between gap-2.5'
const titleClass = 'text-base font-semibold text-gray-900 dark:text-white'
const descClass = 'mt-2 text-[13px] text-gray-500 dark:text-dark-400'
const demoPillClass =
  'inline-flex shrink-0 items-center rounded-full border border-gray-200 bg-gray-100 px-2.5 py-0.5 text-[11px] font-semibold text-gray-500 dark:border-dark-600 dark:bg-dark-700 dark:text-dark-300'
const panelClass =
  'mt-4 flex-1 rounded-xl border border-gray-200 bg-gray-50/60 p-3 dark:border-dark-700 dark:bg-dark-900/40'
const thClass =
  'border-b border-gray-200 px-1.5 pb-2 text-left text-[11px] font-semibold tracking-wide text-gray-500 dark:border-dark-700 dark:text-dark-400'
const tdClass =
  'border-b border-gray-100 px-1.5 py-2 font-mono text-[12px] text-gray-600 dark:border-dark-700/70 dark:text-dark-300'
</script>
