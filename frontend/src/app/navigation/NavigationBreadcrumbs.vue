<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import type { NavigationBreadcrumb } from './routeContributions'

defineProps<{ items: readonly NavigationBreadcrumb[] }>()
const { t } = useI18n()
</script>

<template>
  <nav v-if="items.length" :aria-label="t('ui.breadcrumb')" class="min-w-0 text-sm text-gray-500 dark:text-dark-400">
    <ol class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
      <li v-for="(item, index) in items" :key="index" class="flex min-w-0 max-w-full items-baseline gap-2">
        <span v-if="index" aria-hidden="true" class="shrink-0">/</span>
        <RouterLink
          v-if="item.to && index < items.length - 1"
          :to="item.to"
          class="min-w-0 rounded [overflow-wrap:anywhere] hover:text-primary-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:text-primary-400"
        >{{ item.label }}</RouterLink>
        <span v-else :aria-current="index === items.length - 1 ? 'page' : undefined" class="min-w-0 [overflow-wrap:anywhere]">{{ item.label }}</span>
      </li>
    </ol>
  </nav>
</template>
