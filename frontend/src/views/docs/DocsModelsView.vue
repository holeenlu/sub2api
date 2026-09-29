<template>
  <div class="pb-12">
    <header class="border-b border-gray-100 pb-8 dark:border-dark-800">
      <p class="text-xs font-semibold uppercase text-primary-700 dark:text-primary-400">{{ t('docs.nav.models') }}</p>
      <h1 class="mt-3 text-3xl font-bold text-gray-950 dark:text-white">{{ t('docs.pages.models.title') }}</h1>
      <p class="mt-3 max-w-2xl text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('docs.pages.models.description') }}</p>
      <div class="mt-6 max-w-sm"><DocsGroupSelect :groups="groups" :model-value="selectedGroupId" :loading="loading" @update:model-value="$emit('update:selectedGroupId', $event)" /></div>
    </header>

    <p v-if="loadFailed" class="mt-6 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-200">{{ t('docs.catalogUnavailable') }}</p>
    <div v-if="loading" class="grid gap-3 pt-8 sm:grid-cols-2">
      <div v-for="i in 6" :key="i" class="h-36 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800"></div>
    </div>
    <div v-else class="grid gap-3 pt-8 sm:grid-cols-2">
      <RouterLink v-for="model in models" :key="model.id" :to="`/docs/models/${encodeURIComponent(model.id)}`" class="rounded-lg border border-gray-200 bg-white p-5 transition hover:border-primary-300 hover:shadow-card dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-700">
        <div class="flex items-start gap-3">
          <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-gray-50 dark:bg-dark-900"><ModelIcon :model="model.id" size="24px" /></span>
          <div class="min-w-0 flex-1">
            <h2 class="truncate text-sm font-semibold text-gray-950 dark:text-white">{{ model.displayName }}</h2>
            <code class="mt-1 block truncate text-xs text-gray-500 dark:text-dark-400">{{ model.id }}</code>
          </div>
          <span class="rounded bg-gray-100 px-2 py-1 text-[11px] font-medium text-gray-600 dark:bg-dark-900 dark:text-dark-300">{{ t(`docs.models.${model.kind}`) }}</span>
        </div>
        <p class="mt-4 line-clamp-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t(model.summaryKey) }}</p>
        <div class="mt-4 flex flex-wrap gap-1.5">
          <code v-for="endpoint in model.endpoints.slice(0, 2)" :key="endpoint" class="rounded bg-gray-50 px-2 py-1 text-[11px] text-gray-500 dark:bg-dark-900 dark:text-dark-400">{{ endpoint }}</code>
        </div>
      </RouterLink>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelIcon from '@/components/common/ModelIcon.vue'
import DocsGroupSelect from '@/components/docs/DocsGroupSelect.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import { docsModelCatalogById } from '@/content/docs/modelCatalog'

const props = defineProps<{ groups: ModelPlazaGroup[]; selectedGroup: ModelPlazaGroup | null; selectedGroupId: number | null; loading: boolean; loadFailed: boolean }>()
defineEmits<{ 'update:selectedGroupId': [value: number] }>()
const { t } = useI18n()
const models = computed(() => {
  if (!props.selectedGroup) return []
  const seen = new Set<string>()
  return props.selectedGroup.models.filter(model => !seen.has(model.name) && seen.add(model.name)).map((live) => docsModelCatalogById.get(live.name) ?? {
    id: live.name, displayName: live.name, platform: live.platform === 'anthropic' ? 'anthropic' : 'openai', kind: 'chat',
    summaryKey: 'docs.modelDetailsPending', endpoints: live.platform === 'anthropic' ? ['/v1/messages'] : ['/v1/responses', '/v1/chat/completions'], features: [], sourceUrl: ''
  })
})
</script>
