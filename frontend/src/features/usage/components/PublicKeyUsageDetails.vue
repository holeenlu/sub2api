<template>
          <!-- Detail Card -->
          <div
            v-if="detailRows.length > 0"
            class="fade-up fade-up-delay-3 rounded-2xl border border-gray-200 bg-white/90 backdrop-blur-sm overflow-hidden dark:border-dark-700 dark:bg-dark-900/90"
          >
            <div class="px-8 py-5 border-b border-gray-200 dark:border-dark-700">
              <h3 class="text-sm font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('keyUsage.detailInfo') }}</h3>
            </div>
            <div class="divide-y divide-gray-100 dark:divide-dark-800">
              <div
                v-for="(row, i) in detailRows"
                :key="i"
                class="px-8 py-4 flex items-center justify-between"
              >
                <div class="flex items-center gap-3">
                  <div class="w-8 h-8 rounded-lg flex items-center justify-center" :class="row.iconBg">
                    <svg
                      class="w-4 h-4"
                      :class="row.iconColor"
                      viewBox="0 0 24 24" fill="none" stroke="currentColor"
                      stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
                      v-html="row.iconSvg"
                    ></svg>
                  </div>
                  <span class="text-sm text-gray-700 dark:text-dark-200">{{ row.label }}</span>
                </div>
                <span class="text-sm font-semibold tabular-nums" :class="row.valueClass || 'text-gray-900 dark:text-white'">
                  {{ row.value }}
                </span>
              </div>
            </div>
          </div>


</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ detailRows: Array<{ iconBg: string; iconColor: string; iconSvg: string; label: string; value: string; valueClass: string }> }>()
const { t } = useI18n()
</script>
