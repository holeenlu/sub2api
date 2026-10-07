<template>
  <div :class="[baseClass, accent ? accentClass : '', branch ? branchClass : '']">
    <h4 class="flex items-center gap-2 text-[15px] font-semibold text-gray-900 dark:text-white">
      <span :class="plain ? BADGE_PLAIN_CLASS : BADGE_CLASS">{{ badge }}</span>
      {{ title }}
    </h4>
    <p v-if="english" class="ml-[30px] mt-0.5 text-[11.5px] text-gray-400 dark:text-dark-500">
      {{ english }}
    </p>
    <p v-if="sub" class="mt-2 text-[12.5px] leading-relaxed text-gray-500 dark:text-dark-400">
      {{ sub }}
    </p>
    <slot />
  </div>
</template>

<script setup lang="ts">
import { BADGE_CLASS, BADGE_PLAIN_CLASS } from './mechanisms'

withDefaults(
  defineProps<{
    badge: string
    title: string
    english?: string
    sub?: string
    /** 品牌色描边：属于七项机制的节点。 */
    accent?: boolean
    /** 灰色编号：不是机制，只是图上的位置（应用程式、上游帐号）。 */
    plain?: boolean
    /** 条件分支样式（④ 自动换线重试）。 */
    branch?: boolean
  }>(),
  { english: '', sub: '', accent: false, plain: false, branch: false }
)

const baseClass =
  'rounded-2xl border border-gray-200 bg-white p-3.5 shadow-sm dark:border-dark-700 dark:bg-dark-800'
const accentClass = 'border-primary-200 ring-[3px] ring-primary-500/[0.06] dark:border-primary-800'
const branchClass =
  'border-amber-200 border-l-[3px] border-l-amber-400 bg-amber-50/70 dark:border-amber-800/60 dark:bg-amber-950/20'
</script>
