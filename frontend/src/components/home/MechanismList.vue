<template>
  <div data-testid="mechanism-list" class="mt-10 grid grid-cols-1 gap-4 md:grid-cols-2">
    <div
      v-for="item in items"
      :key="item.name"
      class="rounded-2xl border border-gray-200/70 bg-white/80 p-5 shadow-sm backdrop-blur-sm dark:border-dark-700/70 dark:bg-dark-800/70"
    >
      <h3 class="flex items-center gap-2.5 text-[15.5px] font-semibold text-gray-900 dark:text-white">
        <span :class="BADGE_CLASS">{{ item.badge }}</span>
        {{ t(`home.mechanisms.${item.name}.title`) }}
      </h3>
      <p v-if="showEnglish" class="ml-[31px] mt-1 text-[11.5px] text-gray-400 dark:text-dark-500">
        {{ englishTitle(item.name) }}
      </p>
      <p class="ml-[31px] mt-2.5 text-[13.5px] leading-relaxed text-gray-600 dark:text-dark-300">
        {{ t(`home.mechanisms.${item.name}.description`) }}
      </p>
      <!-- ③ 的范围注记固定显示，不藏进 tooltip -->
      <p
        v-if="item.name === 'cost'"
        class="ml-[31px] mt-2 text-[12px] leading-relaxed text-gray-500 dark:text-dark-400"
      >
        {{ t('home.mechanisms.cost.note') }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { BADGES, BADGE_CLASS, MECHANISM_NAMES, useMechanismEnglishTitle } from './mechanisms'

const { t } = useI18n()
const { showEnglish, englishTitle } = useMechanismEnglishTitle()

const items = MECHANISM_NAMES.map((name, index) => ({ name, badge: BADGES[index] }))
</script>
