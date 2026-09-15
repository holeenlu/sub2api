<template>
  <div class="space-y-14 pb-12">
    <section class="border-b border-gray-100 pb-12 dark:border-dark-800">
      <p class="text-xs font-semibold uppercase text-primary-700 dark:text-primary-400">{{ t('docs.home.eyebrow') }}</p>
      <h1 class="mt-4 max-w-3xl text-4xl font-bold text-gray-950 dark:text-white">{{ t('docs.home.title') }}</h1>
      <p class="mt-4 max-w-2xl text-base leading-7 text-gray-600 dark:text-dark-300">{{ t('docs.home.description') }}</p>
      <div class="mt-7 flex flex-wrap gap-3">
        <RouterLink to="/docs/quickstart" class="inline-flex h-10 items-center gap-2 rounded-md bg-primary-600 px-4 text-sm font-semibold text-white hover:bg-primary-700">
          {{ t('docs.startBuilding') }} <Icon name="arrowRight" size="sm" />
        </RouterLink>
        <RouterLink to="/docs/models" class="inline-flex h-10 items-center gap-2 rounded-md border border-gray-200 px-4 text-sm font-semibold text-gray-700 hover:border-gray-300 hover:bg-gray-50 dark:border-dark-700 dark:text-gray-200 dark:hover:bg-dark-800">
          {{ t('docs.allModels') }}
        </RouterLink>
      </div>
    </section>

    <section>
      <h2 class="text-xl font-semibold text-gray-950 dark:text-white">{{ t('docs.startBuilding') }}</h2>
      <div class="mt-5 grid gap-3 md:grid-cols-3">
        <RouterLink v-for="endpoint in endpoints" :key="endpoint.path" :to="endpoint.path" class="group rounded-lg border border-gray-200 bg-white p-5 transition hover:border-primary-300 hover:shadow-card dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-700">
          <div class="flex h-9 w-9 items-center justify-center rounded-md bg-gray-100 text-gray-700 group-hover:bg-primary-50 group-hover:text-primary-700 dark:bg-dark-900 dark:text-gray-200 dark:group-hover:bg-primary-950 dark:group-hover:text-primary-300">
            <Icon :name="endpoint.icon" size="sm" />
          </div>
          <h3 class="mt-4 text-sm font-semibold text-gray-950 dark:text-white">{{ t(endpoint.titleKey) }}</h3>
          <code class="mt-2 block text-xs text-gray-500 dark:text-dark-400">{{ endpoint.method }}</code>
        </RouterLink>
      </div>
    </section>

    <section class="grid gap-8 border-y border-gray-100 py-10 dark:border-dark-800 lg:grid-cols-[1fr_280px]">
      <div>
        <h2 class="text-xl font-semibold text-gray-950 dark:text-white">{{ t('docs.home.modelsTitle') }}</h2>
        <p class="mt-2 max-w-2xl text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('docs.home.modelsDescription') }}</p>
        <ol class="mt-6 grid gap-5 sm:grid-cols-3">
          <li v-for="(step, index) in steps" :key="index" class="flex gap-3">
            <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-gray-900 text-xs font-bold text-white dark:bg-white dark:text-dark-950">{{ index + 1 }}</span>
            <span class="pt-1 text-sm leading-5 text-gray-700 dark:text-dark-200">{{ t(step) }}</span>
          </li>
        </ol>
      </div>
      <DocsGroupSelect :groups="groups" :model-value="selectedGroupId" :loading="loading" @update:model-value="$emit('update:selectedGroupId', $event)" />
    </section>

    <section>
      <div class="flex items-end justify-between gap-4">
        <div>
          <h2 class="text-xl font-semibold text-gray-950 dark:text-white">{{ t('docs.home.priceTitle') }}</h2>
          <p class="mt-2 text-sm text-gray-600 dark:text-dark-300">{{ t('docs.home.priceDescription') }}</p>
        </div>
        <RouterLink to="/docs/pricing" class="shrink-0 text-sm font-medium text-primary-700 hover:text-primary-800 dark:text-primary-400">{{ t('docs.viewGuide') }}</RouterLink>
      </div>
      <div class="mt-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <RouterLink v-for="model in featuredModels" :key="model.id" :to="`/docs/models/${model.id}`" class="rounded-lg border border-gray-200 bg-white p-4 hover:border-primary-300 dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-700">
          <div class="flex items-center gap-3">
            <ModelIcon :model="model.id" size="24px" />
            <div class="min-w-0">
              <div class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ model.displayName }}</div>
              <div class="mt-0.5 font-mono text-[11px] text-gray-400">{{ model.id }}</div>
            </div>
          </div>
        </RouterLink>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import DocsGroupSelect from '@/components/docs/DocsGroupSelect.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import { docsModelCatalog } from '@/content/docs/modelCatalog'

defineProps<{ groups: ModelPlazaGroup[]; selectedGroupId: number | null; loading?: boolean }>()
defineEmits<{ 'update:selectedGroupId': [value: number] }>()
const { t } = useI18n()
const steps = ['docs.home.step1', 'docs.home.step2', 'docs.home.step3']
const featuredModels = docsModelCatalog.filter((item) => ['gpt-6-astra', 'gpt-5.6-sol', 'claude-opus-5', 'gpt-image-2.5-flare'].includes(item.id))
const endpoints = [
  { path: '/docs/api/chat/openai-responses', titleKey: 'docs.pages.responses.title', method: 'POST /v1/responses', icon: 'sparkles' as const },
  { path: '/docs/api/chat/anthropic-messages', titleKey: 'docs.pages.messages.title', method: 'POST /v1/messages', icon: 'chat' as const },
  { path: '/docs/api/image/openai-image', titleKey: 'docs.pages.image.title', method: 'POST /v1/images/generations', icon: 'sparkles' as const }
]
</script>
