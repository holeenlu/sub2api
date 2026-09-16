<template>
  <div class="min-h-screen bg-white text-gray-900 dark:bg-dark-950 dark:text-gray-100">
    <header class="sticky top-0 z-40 border-b border-gray-200 bg-white/95 backdrop-blur dark:border-dark-800 dark:bg-dark-950/95">
      <div class="flex h-16 items-center gap-3 px-4 lg:px-6">
        <button type="button" class="flex h-9 w-9 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 lg:hidden dark:text-dark-300 dark:hover:bg-dark-800" :title="t('docs.menu')" @click="mobileOpen = true"><Icon name="menu" size="md" /></button>
        <RouterLink to="/home" class="flex min-w-0 items-center gap-3" :title="t('docs.backToSite')">
          <span class="flex h-9 w-9 shrink-0 items-center justify-center overflow-hidden rounded-md border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800"><img :src="siteLogo" alt="" class="h-full w-full object-contain" /></span>
          <span class="hidden truncate text-sm font-semibold text-gray-950 sm:block dark:text-white">{{ appStore.siteName }}</span>
        </RouterLink>

        <button type="button" class="min-w-0 flex-1 mx-0 flex h-10 max-w-md items-center gap-2 rounded-md border border-gray-200 bg-gray-50 px-3 text-left text-sm text-gray-400 hover:border-gray-300 dark:border-dark-700 dark:bg-dark-900 dark:hover:border-dark-600 sm:mx-auto" @click="searchOpen = true">
          <Icon name="search" size="sm" /><span class="min-w-0 flex-1 truncate">{{ searchLabel }}</span><kbd class="hidden rounded border border-gray-200 bg-white px-1.5 py-0.5 text-[10px] text-gray-400 sm:block dark:border-dark-700 dark:bg-dark-800">⌘ K</kbd>
        </button>

        <div class="flex shrink-0 items-center gap-1">
          <LocaleSwitcher />
          <RouterLink :to="authStore.isAuthenticated ? (authStore.isAdmin ? '/admin/dashboard' : '/dashboard') : '/login'" class="hidden h-9 items-center rounded-md bg-gray-900 px-3 text-sm font-semibold text-white hover:bg-gray-800 sm:inline-flex dark:bg-white dark:text-dark-950 dark:hover:bg-gray-200">{{ authStore.isAuthenticated ? t('docs.console') : t('docs.login') }}</RouterLink>
        </div>
      </div>
      <nav class="flex gap-6 border-t border-gray-100 px-5 dark:border-dark-800" :aria-label="t('docs.sections')">
        <RouterLink to="/docs" class="border-b-2 py-3 text-sm font-semibold" :class="!isApps ? 'border-primary-600 text-primary-700 dark:text-primary-400' : 'border-transparent text-gray-500'" :aria-current="!isApps ? 'page' : undefined">{{ t('home.apiDocs') }}</RouterLink>
        <RouterLink to="/apps" class="border-b-2 py-3 text-sm font-semibold" :class="isApps ? 'border-primary-600 text-primary-700 dark:text-primary-400' : 'border-transparent text-gray-500'" :aria-current="isApps ? 'page' : undefined">{{ t('home.aiApps') }}</RouterLink>
      </nav>
    </header>

    <div class="mx-auto flex max-w-[1500px]">
      <aside class="sticky top-28 hidden h-[calc(100vh-7rem)] w-64 shrink-0 overflow-y-auto border-r border-gray-100 px-4 py-6 lg:block dark:border-dark-800">
        <DocsSidebar :groups="groups" :selected-group-id="selectedGroupId" :loading="loading" @update:selected-group-id="selectedGroupId = $event" />
      </aside>
      <main ref="contentRoot" class="min-w-0 flex-1 px-5 py-9 sm:px-8 lg:px-10 xl:px-12">
        <div class="mx-auto max-w-4xl">
          <DocsHomeView v-if="currentKind === 'home'" :groups="groups" :selected-group-id="selectedGroupId" :loading="loading" @update:selected-group-id="selectedGroupId = $event" />
          <DocsModelsView v-else-if="currentKind === 'models'" :groups="groups" :selected-group="selectedGroup" :selected-group-id="selectedGroupId" :loading="loading" :load-failed="loadFailed" @update:selected-group-id="selectedGroupId = $event" />
          <DocsModelView v-else-if="currentKind === 'model'" :model-id="modelId" :selected-group="selectedGroup" :api-base-url="appStore.apiBaseUrl" />
          <DocsArticleView v-else-if="currentItem" :item="currentItem" :selected-group="selectedGroup" :api-base-url="appStore.apiBaseUrl" />
        </div>
      </main>
      <aside v-if="toc.length" class="sticky top-28 hidden h-[calc(100vh-7rem)] w-56 shrink-0 overflow-y-auto border-l border-gray-100 px-5 py-9 xl:block dark:border-dark-800">
        <h2 class="mb-3 text-[11px] font-semibold uppercase text-gray-400">{{ t('docs.onThisPage') }}</h2>
        <a v-for="entry in toc" :key="entry.id" :href="`#${entry.id}`" class="mb-2 block text-xs leading-5 text-gray-500 hover:text-primary-700 dark:text-dark-400 dark:hover:text-primary-400">{{ entry.label }}</a>
      </aside>
    </div>

    <transition name="fade">
      <div v-if="mobileOpen" class="fixed inset-0 z-50 lg:hidden">
        <button class="absolute inset-0 bg-gray-950/40" :aria-label="t('docs.close')" @click="mobileOpen = false"></button>
        <aside class="relative h-full w-[min(20rem,88vw)] overflow-y-auto bg-white p-5 shadow-xl dark:bg-dark-950">
          <div class="mb-5 flex items-center justify-between"><span class="font-semibold">{{ t(isApps ? 'home.aiApps' : 'home.apiDocs') }}</span><button type="button" class="flex h-9 w-9 items-center justify-center rounded-md hover:bg-gray-100 dark:hover:bg-dark-800" :title="t('docs.close')" @click="mobileOpen = false"><Icon name="x" size="md" /></button></div>
          <DocsSidebar :groups="groups" :selected-group-id="selectedGroupId" :loading="loading" @update:selected-group-id="selectedGroupId = $event" @navigate="mobileOpen = false" />
        </aside>
      </div>
    </transition>

    <div v-if="searchOpen" class="fixed inset-0 z-[60] flex items-start justify-center bg-gray-950/45 px-4 pt-[10vh]" @click.self="searchOpen = false">
      <div class="w-full max-w-2xl overflow-hidden rounded-lg border border-gray-200 bg-white shadow-2xl dark:border-dark-700 dark:bg-dark-900">
        <div class="flex h-14 items-center gap-3 border-b border-gray-100 px-4 dark:border-dark-800"><Icon name="search" size="sm" class="text-gray-400" /><input ref="searchInput" v-model="query" class="h-full min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-gray-400" :placeholder="searchLabel" /><button type="button" class="text-xs text-gray-400" @click="searchOpen = false">ESC</button></div>
        <div class="max-h-[60vh] overflow-y-auto p-2">
          <RouterLink v-for="result in searchResults" :key="result.path" :to="result.path" class="flex items-center gap-3 rounded-md px-3 py-3 hover:bg-gray-50 dark:hover:bg-dark-800" @click="searchOpen = false">
            <Icon :name="result.icon" size="sm" class="text-gray-400" /><div class="min-w-0"><div class="truncate text-sm font-medium">{{ result.title }}</div><div class="mt-0.5 truncate text-xs text-gray-400">{{ result.description }}</div></div>
          </RouterLink>
          <p v-if="searchResults.length === 0" class="px-3 py-10 text-center text-sm text-gray-400">{{ t('docs.noResults') }}</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import DocsSidebar from '@/components/docs/DocsSidebar.vue'
import DocsHomeView from './DocsHomeView.vue'
import DocsModelsView from './DocsModelsView.vue'
import DocsModelView from './DocsModelView.vue'
import DocsArticleView from './DocsArticleView.vue'
import { getModelPlaza, type ModelPlazaGroup } from '@/api/modelPlaza'
import { docsItemByPath, navGroupsForPath, isAppsPath } from '@/content/docs/nav'
import { docsModelCatalogById } from '@/content/docs/modelCatalog'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { sanitizeUrl } from '@/utils/url'

const { t, locale } = useI18n()
const route = useRoute()
const isApps = computed(() => isAppsPath(route.path))
const searchLabel = computed(() => t(isApps.value ? 'docs.searchApps' : 'docs.search'))
const appStore = useAppStore()
const authStore = useAuthStore()
const groups = ref<ModelPlazaGroup[]>([])
const selectedGroupId = ref<number | null>(null)
const loading = ref(true)
const loadFailed = ref(false)
const mobileOpen = ref(false)
const searchOpen = ref(false)
const searchInput = ref<HTMLInputElement | null>(null)
const query = ref('')
const contentRoot = ref<HTMLElement | null>(null)
const toc = ref<Array<{ id: string; label: string }>>([])
let contentObserver: MutationObserver | null = null
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '/logo.svg', { allowRelative: true, allowDataUrl: true }))
const selectedGroup = computed(() => groups.value.find((group) => group.id === selectedGroupId.value) ?? groups.value[0] ?? null)
const currentItem = computed(() => docsItemByPath.get(route.path))
const modelId = computed(() => typeof route.params.modelId === 'string' ? route.params.modelId : '')
const currentKind = computed(() => route.path === '/docs' ? 'home' : route.path === '/docs/models' ? 'models' : modelId.value ? 'model' : 'article')
const searchResults = computed(() => {
  const needle = query.value.trim().toLowerCase()
  const staticResults = navGroupsForPath(route.path).flatMap(group => group.items).map((item) => ({ path: item.path, title: t(item.titleKey), description: t(item.descriptionKey), icon: item.icon }))
  const seen = new Set<string>()
  const modelResults = (isApps.value ? [] : groups.value).flatMap((group) => group.models).filter((model) => !seen.has(model.name) && seen.add(model.name)).map((model) => {
    const catalog = docsModelCatalogById.get(model.name)
    return { path: `/docs/models/${model.name}`, title: catalog?.displayName ?? model.name, description: model.name, icon: 'cube' as const }
  })
  return [...staticResults, ...modelResults].filter((item) => !needle || `${item.title} ${item.description}`.toLowerCase().includes(needle)).slice(0, 20)
})

watch(searchOpen, async (open) => { if (open) { query.value = ''; await nextTick(); searchInput.value?.focus() } })
watch(selectedGroupId, (value) => { if (value != null) localStorage.setItem('docs_selected_group_id', String(value)) })
watch(() => route.path, () => { mobileOpen.value = false })
watch([modelId, groups], ([id]) => {
  if (!id || selectedGroup.value?.models.some((model) => model.name === id)) return
  const matchingGroup = groups.value.find((group) => group.models.some((model) => model.name === id))
  if (matchingGroup) selectedGroupId.value = matchingGroup.id
})
watch([modelId, () => appStore.siteName, locale], ([id]) => {
  if (!id) return
  const name = docsModelCatalogById.get(id)?.displayName ?? id
  document.title = `${name} - ${t('docs.brand')} - ${appStore.siteName}`
}, { immediate: true })

function collectToc() {
  const headings = Array.from(contentRoot.value?.querySelectorAll('h2') ?? [])
  toc.value = headings.map((heading, index) => {
    const id = `docs-heading-${index + 1}`
    heading.id = id
    return { id, label: heading.textContent?.trim() || id }
  })
}

function onKeydown(event: KeyboardEvent) {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); searchOpen.value = true }
  if (event.key === 'Escape') { searchOpen.value = false; mobileOpen.value = false }
}

onMounted(async () => {
  window.addEventListener('keydown', onKeydown)
  contentObserver = new MutationObserver(collectToc)
  if (contentRoot.value) contentObserver.observe(contentRoot.value, { childList: true, subtree: true })
  await nextTick()
  collectToc()
  void appStore.fetchPublicSettings()
  try {
    const response = await getModelPlaza()
    groups.value = response.groups
    const saved = Number(localStorage.getItem('docs_selected_group_id'))
    selectedGroupId.value = groups.value.some((group) => group.id === saved) ? saved : (groups.value[0]?.id ?? null)
  } catch { loadFailed.value = true } finally { loading.value = false }
})
onBeforeUnmount(() => { window.removeEventListener('keydown', onKeydown); contentObserver?.disconnect() })
</script>

<style scoped>
main :deep(h2) { scroll-margin-top: 8rem; }
.fade-enter-active, .fade-leave-active { transition: opacity 0.15s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
