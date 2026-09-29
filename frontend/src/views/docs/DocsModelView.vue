<template>
  <div v-if="model" class="pb-12">
    <header class="border-b border-gray-100 pb-8 dark:border-dark-800">
      <div class="flex items-start gap-4">
        <span class="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800"><ModelIcon :model="model.id" size="28px" /></span>
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h1 class="text-3xl font-bold text-gray-950 dark:text-white">{{ model.displayName }}</h1>
            <span class="rounded bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-800 dark:text-dark-300">{{ t(`docs.models.${model.kind}`) }}</span>
          </div>
          <div class="mt-2 flex items-center gap-2">
            <code class="break-all text-sm text-gray-500 dark:text-dark-400">{{ model.id }}</code>
            <button type="button" class="flex h-7 w-7 shrink-0 items-center justify-center rounded text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-800 dark:hover:text-gray-200" :title="t('docs.copyModel')" @click="copyModel"><Icon :name="copied ? 'check' : 'copy'" size="xs" /></button>
          </div>
        </div>
      </div>
      <p class="mt-5 max-w-2xl text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t(model.summaryKey) }}</p>
      <a v-if="model.sourceUrl" :href="model.sourceUrl" target="_blank" rel="noopener noreferrer" class="mt-3 inline-flex items-center gap-1.5 text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-400">
        {{ t('docs.officialSource') }} <Icon name="externalLink" size="xs" />
      </a>
    </header>

    <section class="grid gap-6 border-b border-gray-100 py-8 sm:grid-cols-2 lg:grid-cols-4 dark:border-dark-800">
      <div><div class="text-xs text-gray-400">{{ t('docs.platform') }}</div><div class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ t(`docs.models.${model.platform}`) }}</div></div>
      <div><div class="text-xs text-gray-400">{{ t('docs.context') }}</div><div class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ formatTokens(model.contextWindow) }}</div></div>
      <div><div class="text-xs text-gray-400">{{ t('docs.maxOutput') }}</div><div class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ formatTokens(model.maxOutputTokens) }}</div></div>
      <div><div class="text-xs text-gray-400">{{ t('docs.availability') }}</div><div class="mt-1 text-sm font-semibold" :class="available === true ? 'text-emerald-700 dark:text-emerald-400' : 'text-gray-500 dark:text-dark-400'">{{ available == null ? t('docs.unknownAvailability') : available ? t('docs.available') : t('docs.unavailable') }}</div></div>
    </section>

    <section v-if="model.features.length" class="border-b border-gray-100 py-8 dark:border-dark-800">
      <h2 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t('docs.features') }}</h2>
      <div class="mt-4 flex flex-wrap gap-2"><span v-for="feature in model.features" :key="feature" class="rounded-md border border-gray-200 bg-gray-50 px-2.5 py-1.5 text-xs text-gray-600 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300">{{ feature }}</span></div>
    </section>

    <section class="py-8">
      <h2 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t('docs.endpoints') }}</h2>
      <div class="mt-4 divide-y divide-gray-100 rounded-lg border border-gray-200 dark:divide-dark-700 dark:border-dark-700">
        <RouterLink v-for="endpoint in model.endpoints" :key="endpoint" :to="endpointDoc(endpoint)" class="flex items-center gap-3 px-4 py-3 hover:bg-gray-50 dark:hover:bg-dark-800">
          <span class="rounded bg-primary-50 px-2 py-1 text-[11px] font-bold text-primary-700 dark:bg-primary-950 dark:text-primary-300">{{ endpoint === '/v1/models' ? 'GET' : 'POST' }}</span>
          <code class="text-sm text-gray-700 dark:text-dark-200">{{ endpoint }}</code><Icon name="arrowRight" size="xs" class="ml-auto text-gray-400" />
        </RouterLink>
      </div>
    </section>

    <DocsPricingTable :kind="model.kind" :selected-group="selectedGroup" :model-ids="[model.id]" />

    <section class="mt-10">
      <h2 class="mb-3 text-lg font-semibold text-gray-950 dark:text-white">{{ t('docs.requestExample') }}</h2>
      <DocsCodeTabs :examples="examples" />
    </section>

    <p class="mt-8 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('docs.endpointScope') }}</p>
  </div>
  <div v-else class="py-16 text-center">
    <h1 class="text-2xl font-semibold text-gray-950 dark:text-white">{{ modelId }}</h1>
    <p class="mx-auto mt-3 max-w-lg text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('docs.modelNotFound') }}</p>
    <RouterLink to="/docs/models" class="mt-6 inline-flex h-10 items-center rounded-md bg-gray-900 px-4 text-sm font-semibold text-white dark:bg-white dark:text-dark-950">{{ t('docs.allModels') }}</RouterLink>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import DocsCodeTabs from '@/components/docs/DocsCodeTabs.vue'
import DocsPricingTable from '@/components/docs/DocsPricingTable.vue'
import { buildDocsExamples } from '@/content/docs/examples'
import { docsModelCatalogById } from '@/content/docs/modelCatalog'
import type { DocsModelCatalogEntry } from '@/content/docs/types'
import type { ModelPlazaGroup } from '@/api/modelPlaza'

const props = defineProps<{ modelId: string; selectedGroup: ModelPlazaGroup | null; apiBaseUrl: string }>()
const { t } = useI18n()
const copied = ref(false)
const model = computed<DocsModelCatalogEntry | undefined>(() => {
  const catalog = docsModelCatalogById.get(props.modelId)
  if (catalog) return catalog
  const live = props.selectedGroup?.models.find((item) => item.name === props.modelId)
  if (!live) return undefined
  const anthropic = live.platform.toLowerCase().includes('anthropic') || live.name.startsWith('claude-')
  return {
    id: live.name,
    displayName: live.name,
    platform: anthropic ? 'anthropic' : 'openai',
    kind: 'chat',
    summaryKey: 'docs.modelDetailsPending',
    endpoints: anthropic ? ['/v1/messages'] : ['/v1/responses', '/v1/chat/completions'],
    features: [],
    sourceUrl: ''
  }
})
const available = computed<boolean | null>(() => props.selectedGroup && props.selectedGroup.catalog_status !== 'unavailable' ? props.selectedGroup.models.some((item) => item.name === props.modelId) : null)
const exampleKind = computed<'image' | 'messages' | 'responses'>(() => model.value?.kind === 'image' ? 'image' : model.value?.platform === 'anthropic' ? 'messages' : 'responses')
const examples = computed(() => buildDocsExamples(exampleKind.value, props.apiBaseUrl || window.location.origin, props.modelId))
function formatTokens(value?: number) { return value ? `${(value / 1000).toLocaleString()}K` : '—' }
function endpointDoc(endpoint: string) {
  if (endpoint === '/v1/messages') return '/docs/api/chat/anthropic-messages'
  if (endpoint === '/v1/chat/completions') return '/docs/api/chat/openai-chat'
  if (endpoint.startsWith('/v1/images')) return '/docs/api/image/openai-image'
  return '/docs/api/chat/openai-responses'
}
async function copyModel() {
  await navigator.clipboard.writeText(props.modelId)
  copied.value = true
  window.setTimeout(() => { copied.value = false }, 1600)
}
</script>
