<template>
  <article class="pb-12">
    <header class="border-b border-gray-100 pb-8 dark:border-dark-800">
      <p class="text-xs font-semibold uppercase text-primary-700 dark:text-primary-400">{{ sectionTitle }}</p>
      <h1 class="mt-3 text-3xl font-bold text-gray-950 dark:text-white">{{ t(item.titleKey) }}</h1>
      <p class="mt-3 max-w-2xl text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t(item.descriptionKey) }}</p>
    </header>

    <div v-if="item.path === '/apps'" class="mt-8 grid gap-4 sm:grid-cols-2">
      <RouterLink v-for="guide in appGuides" :key="guide.path" :to="guide.path" class="group rounded-xl border border-gray-200 p-5 transition hover:border-primary-400 hover:bg-primary-50/40 dark:border-dark-700 dark:hover:bg-dark-800">
        <p class="font-semibold text-gray-950 dark:text-white">{{ t(guide.titleKey) }} <span aria-hidden="true" class="float-right text-primary-600">↗</span></p>
        <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-dark-300">{{ t(guide.descriptionKey) }}</p>
      </RouterLink>
    </div>

    <section v-if="item.exampleKind" class="mt-8">
      <h2 class="mb-3 text-lg font-semibold text-gray-950 dark:text-white">{{ t('docs.requestExample') }}</h2>
      <DocsCodeTabs :examples="examples" />
    </section>

    <div v-if="contentLoading" class="mt-8 space-y-3">
      <div class="h-5 w-2/3 animate-pulse rounded bg-gray-100 dark:bg-dark-800"></div>
      <div class="h-4 w-full animate-pulse rounded bg-gray-100 dark:bg-dark-800"></div>
      <div class="h-4 w-5/6 animate-pulse rounded bg-gray-100 dark:bg-dark-800"></div>
    </div>
    <div v-else class="docs-markdown mt-9" @click="onContentClick" v-html="contentHtml"></div>

    <div v-if="item.path === '/docs/pricing'" class="mt-10 space-y-12">
      <DocsPricingTable kind="chat" :selected-group="selectedGroup" />
      <DocsPricingTable kind="image" :selected-group="selectedGroup" />
    </div>
    <DocsPricingTable v-else-if="item.pricingKind" class="mt-10" :kind="item.pricingKind" :selected-group="selectedGroup" :model-ids="availableModelIds" />

    <nav class="mt-14 flex border-t border-gray-100 pt-6 dark:border-dark-800">
      <RouterLink v-if="previous" :to="previous.path" class="mr-auto text-sm text-gray-600 hover:text-primary-700 dark:text-dark-300 dark:hover:text-primary-400">
        <span class="block text-xs text-gray-400">{{ t('docs.previous') }}</span>{{ t(previous.titleKey) }}
      </RouterLink>
      <RouterLink v-if="next" :to="next.path" class="ml-auto text-right text-sm text-gray-600 hover:text-primary-700 dark:text-dark-300 dark:hover:text-primary-400">
        <span class="block text-xs text-gray-400">{{ t('docs.next') }}</span>{{ t(next.titleKey) }}
      </RouterLink>
    </nav>
  </article>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import DocsCodeTabs from '@/components/docs/DocsCodeTabs.vue'
import DocsPricingTable from '@/components/docs/DocsPricingTable.vue'
import { buildDocsExamples } from '@/content/docs/examples'
import { docsModelCatalog } from '@/content/docs/modelCatalog'
import { docsNavGroups, appsNavGroups, navGroupsForPath } from '@/content/docs/nav'
import type { DocsNavItem } from '@/content/docs/types'
import type { ModelPlazaGroup } from '@/api/modelPlaza'

const props = defineProps<{ item: DocsNavItem; selectedGroup: ModelPlazaGroup | null; apiBaseUrl: string }>()
const { t, locale } = useI18n()
const router = useRouter()
const markdownModules = import.meta.glob('../../content/docs/**/*.md', { query: '?raw', import: 'default' })
const contentHtml = ref('')
const contentLoading = ref(false)
let loadVersion = 0
const appGuides = appsNavGroups.flatMap(group => group.items).filter(entry => entry.path !== '/apps')
const localizedAsset = (name: string) => {
  const suffix = locale.value === 'en' ? '-en' : locale.value === 'zh-TW' ? '-zh-TW' : '-zh'
  return `/docs-assets/${name}${suffix}.png`
}
const sectionItems = computed(() => navGroupsForPath(props.item.path).flatMap(group => group.items))

async function onContentClick(event: MouseEvent) {
  const target = event.target as HTMLElement
  const button = target.closest<HTMLButtonElement>('button[data-docs-copy]')
  if (button) {
    try {
      await navigator.clipboard.writeText(button.closest('pre')?.querySelector('code')?.textContent ?? '')
      button.textContent = t('docs.copied')
    } catch { button.textContent = t('docs.copy') }
    return
  }
  const link = target.closest<HTMLAnchorElement>('a')
  if (link && link.target !== '_blank' && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && event.button === 0 && link.origin === location.origin && /^\/(docs|apps)(\/|$)/.test(link.pathname)) {
    event.preventDefault()
    await router.push(link.pathname + link.search + link.hash)
  }
}

const groupIndex = computed(() => docsNavGroups.findIndex((group) => group.items.some((entry) => entry.path === props.item.path)))
const sectionTitle = computed(() => t(docsNavGroups[groupIndex.value]?.titleKey ?? 'docs.brand'))
const itemIndex = computed(() => sectionItems.value.findIndex((entry) => entry.path === props.item.path))
const previous = computed(() => itemIndex.value > 0 ? sectionItems.value[itemIndex.value - 1] : null)
const next = computed(() => itemIndex.value >= 0 && itemIndex.value < sectionItems.value.length - 1 ? sectionItems.value[itemIndex.value + 1] : null)
const availableModelIds = computed(() => props.selectedGroup?.models.map((model) => model.name))
const exampleModel = computed(() => {
  const kind = props.item.exampleKind === 'image' ? 'image' : 'chat'
  const platform = props.item.exampleKind === 'messages' ? 'anthropic' : 'openai'
  const live = props.selectedGroup?.models.find((model) => {
    const catalog = docsModelCatalog.find((entry) => entry.id === model.name)
    return catalog?.kind === kind && catalog.platform === platform
  })
  return live?.name ?? docsModelCatalog.find((entry) => entry.kind === kind && entry.platform === platform)?.id ?? 'gpt-5.6-sol'
})
const examples = computed(() => buildDocsExamples(props.item.exampleKind!, props.apiBaseUrl || window.location.origin, exampleModel.value))

async function loadArticle() {
  const version = ++loadVersion
  if (!props.item.articleId) { contentHtml.value = ''; return }
  contentLoading.value = true
  try {
    const language = locale.value === 'en' ? 'en' : locale.value === 'zh-TW' ? 'zh-TW' : 'zh'
    const suffix = `/content/docs/${language}/${props.item.articleId}.md`
    const fallbackSuffix = `/content/docs/zh/${props.item.articleId}.md`
    const key = Object.keys(markdownModules).find((path) => path.endsWith(suffix))
      ?? Object.keys(markdownModules).find((path) => path.endsWith(fallbackSuffix))
    const source = key ? String(await markdownModules[key]()) : ''
    if (version !== loadVersion) return
    // Insert the configured URL as text after parsing, so configuration cannot inject Markdown/HTML.
    const container = document.createElement('div')
    container.innerHTML = DOMPurify.sanitize(marked.parse(source) as string)
    if (props.item.path === '/apps') {
      container.querySelectorAll<HTMLImageElement>('img[src="/docs-assets/create-api-key.png"]').forEach((image) => {
        image.src = localizedAsset('create-api-key')
      })
    }
    let root = 'https://api.example.com'
    try {
      const url = new URL(props.apiBaseUrl || location.origin)
      if (['http:', 'https:'].includes(url.protocol) && !url.username && !url.password) root = (url.origin + url.pathname).replace(/\/+$/, '').replace(/\/v1$/, '')
    } catch { /* Keep the documented placeholder for invalid public configuration. */ }
    const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT)
    while (walker.nextNode()) walker.currentNode.textContent = walker.currentNode.textContent?.split('{{API_ROOT}}').join(root) ?? ''
    for (const img of container.querySelectorAll<HTMLImageElement>('img')) {
      if (img.getAttribute('src')?.startsWith('/docs-assets/') && !img.closest('a')) {
        const link = document.createElement('a')
        link.href = img.src
        link.target = '_blank'
        link.rel = 'noopener noreferrer'
        link.title = t('docs.openImage')
        img.replaceWith(link)
        link.append(img)
      }
    }
    for (const table of container.querySelectorAll('table')) {
      const wrapper = document.createElement('div')
      wrapper.className = 'docs-table-scroll'
      wrapper.tabIndex = 0
      table.replaceWith(wrapper)
      wrapper.append(table)
    }
    for (const pre of container.querySelectorAll('pre')) {
      pre.classList.add('docs-code-block')
      const code = pre.querySelector('code')
      const language = code?.className.match(/language-([\w-]+)/i)?.[1]?.toUpperCase() ?? 'TEXT'
      const header = document.createElement('div')
      header.className = 'docs-code-header'
      const label = document.createElement('span')
      label.textContent = language
      header.append(label)
      const button = document.createElement('button')
      button.type = 'button'
      button.dataset.docsCopy = 'true'
      button.textContent = t('docs.copy')
      header.append(button)
      pre.prepend(header)
    }
    for (const link of container.querySelectorAll<HTMLAnchorElement>('a[href]')) {
      if (link.origin !== location.origin) { link.target = '_blank'; link.rel = 'noopener noreferrer' }
      if (link.pathname.startsWith('/downloads/')) link.download = link.pathname.split('/').pop() ?? ''
    }
    contentHtml.value = container.innerHTML
  } finally {
    if (version === loadVersion) contentLoading.value = false
  }
}

watch(() => [props.item.articleId, locale.value, props.apiBaseUrl], loadArticle, { immediate: true })
</script>

<style scoped>
.docs-markdown { @apply text-[15px] leading-7 text-gray-700 dark:text-dark-200; }
.docs-markdown :deep(h2) { @apply mb-3 mt-9 text-xl font-semibold text-gray-950 first:mt-0 dark:text-white; }
.docs-markdown :deep(h3) { @apply mb-2 mt-7 text-base font-semibold text-gray-900 dark:text-gray-100; }
.docs-markdown :deep(p) { @apply my-3; }
.docs-markdown :deep(ul) { @apply my-3 list-disc space-y-1 pl-6; }
.docs-markdown :deep(ol) { @apply my-3 list-decimal space-y-1 pl-6; }
.docs-markdown :deep(a) { @apply font-medium text-primary-700 underline underline-offset-4 dark:text-primary-400; }
.docs-markdown :deep(code) { @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-[13px] text-gray-800 dark:bg-dark-800 dark:text-gray-100; }
.docs-markdown :deep(pre) { @apply my-5 overflow-x-auto rounded-lg bg-gray-950 text-[13px] leading-6 text-gray-200; }
.docs-markdown :deep(.docs-code-header) { @apply flex items-center border-b border-white/10 bg-gray-900 px-4 py-2 text-[11px] font-semibold uppercase tracking-wider text-gray-400; }
.docs-markdown :deep(.docs-code-header button) { @apply ml-auto mb-0 rounded border border-white/20 px-2 py-1 text-xs font-normal normal-case tracking-normal text-gray-300 hover:bg-white/10; }
.docs-markdown :deep(.docs-code-block > code) { @apply block p-4; }
.docs-markdown :deep(pre code) { @apply bg-transparent p-0 text-inherit; }
.docs-markdown :deep(.docs-table-scroll) { @apply my-5 max-w-full overflow-x-auto; }
.docs-markdown :deep(img) { @apply my-5 max-h-[520px] max-w-full rounded-lg border border-gray-200 object-contain dark:border-dark-700; }
.docs-markdown :deep(table) { @apply my-5 w-full min-w-[620px] border-collapse text-sm; }
.docs-markdown :deep(table) { display: table; }
.docs-markdown :deep(thead) { @apply bg-gray-50 dark:bg-dark-900; }
.docs-markdown :deep(th), .docs-markdown :deep(td) { @apply border border-gray-200 px-3 py-2 text-left align-top dark:border-dark-700; }
.docs-markdown :deep(blockquote) { @apply my-5 border-l-4 border-primary-300 bg-primary-50 px-4 py-3 text-gray-700 dark:border-primary-700 dark:bg-primary-950/30 dark:text-dark-200; }
</style>
