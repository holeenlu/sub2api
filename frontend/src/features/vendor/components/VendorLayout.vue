<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useVendorStore } from '../store'
import Icon from '@/components/icons/Icon.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import DisplayModeSwitcher from './DisplayModeSwitcher.vue'
import VersionBadge from '@/components/common/VersionBadge.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { globalAdminStepUp } from '@/composables/useStepUp'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()
const vendor = useVendorStore()
const mobileOpen = ref(false)
const desktop = useMediaQuery('(min-width: 1024px)')
const sidebarCollapsed = computed(() => desktop.value && app.sidebarCollapsed)
const navigation = ref<HTMLElement>()
const navigationTrigger = ref<HTMLButtonElement>()
const profileMenu = ref<HTMLElement>()
const profileTrigger = ref<HTMLButtonElement>()
const profileOpen = ref(false)
const displayName = computed(
  () =>
    auth.user?.username ||
    auth.user?.email?.split('@')[0] ||
    vendor.vendor?.name ||
    t('vendor.account'),
)
const profileEmail = computed(
  () => auth.user?.email || vendor.vendor?.email || '—',
)
const profileInitials = computed(() => {
  const value = displayName.value.trim()
  return value ? value.slice(0, 2).toUpperCase() : 'V'
})
watch(mobileOpen, async (open) => {
  await nextTick()
  if (desktop.value) return
  if (open) navigation.value?.querySelector<HTMLAnchorElement>('a')?.focus()
  else navigationTrigger.value?.focus()
})
function navigationKeydown(event: KeyboardEvent) {
  if (desktop.value || !mobileOpen.value || event.key !== 'Tab') return
  const links = navigation.value?.querySelectorAll<HTMLElement>('a, button:not(:disabled)')
  if (!links?.length) return
  const target = event.shiftKey ? links[links.length - 1] : links[0]
  if (document.activeElement === (event.shiftKey ? links[0] : links[links.length - 1])) {
    event.preventDefault()
    target.focus()
  }
}
const roleLabel = computed(() => t(auth.user?.role === 'super_admin' ? 'admin.users.roles.super_admin' : auth.user?.role === 'vendor_level1' ? 'vendor.tier1' : 'vendor.tier2'))
const home = computed(() => auth.isSuperAdmin ? '/vendor/platform' : '/vendor/accounts')
type NavigationSection = { label: string; items: { path: string; query?: { tab: string }; label: string; icon: 'users' | 'grid' | 'globe' | 'chart' | 'document' | 'cube' | 'clock' }[] }
const sections = computed<NavigationSection[]>(() => auth.isSuperAdmin ? [{ label: 'supplierManagement', items: [
  { path: 'platform', query: { tab: 'suppliers' }, label: 'suppliers', icon: 'users' as const },
  { path: 'platform', query: { tab: 'accounts' }, label: 'accounts', icon: 'grid' as const },
  { path: 'platform', query: { tab: 'proxies' }, label: 'proxies', icon: 'globe' as const },
  { path: 'platform', query: { tab: 'usage' }, label: 'usage', icon: 'chart' as const },
  { path: 'platform', query: { tab: 'settlements' }, label: 'settlements', icon: 'document' as const },
  { path: 'platform', query: { tab: 'supply' }, label: 'supply', icon: 'cube' as const },
  { path: 'platform', query: { tab: 'audit' }, label: 'audit', icon: 'clock' as const }
]}] : [
  { label: 'resources', items: [
    { path: 'accounts', label: 'accounts', icon: 'grid' as const },
    { path: 'proxies', label: 'proxies', icon: 'globe' as const }
  ]},
  ...(auth.user?.role === 'vendor_level1' ? [{ label: 'downstream', items: [
    { path: 'children', label: 'childVendors', icon: 'users' as const },
    { path: 'child-accounts', label: 'childAccounts', icon: 'grid' as const },
    { path: 'child-proxies', label: 'childProxies', icon: 'globe' as const },
    { path: 'payables', label: 'downstreamPayables', icon: 'document' as const }
  ]}] : []),
  { label: 'reports', items: [
    { path: 'usage', label: 'usage', icon: 'chart' as const },
    { path: 'settlements', label: 'upstreamReceivables', icon: 'document' as const }
  ]}
])

const pageTitle = computed(() => {
  if (route.path === '/vendor/profile') return t('vendor.accountSettings')
  const item = sections.value.flatMap(section => section.items).find(item =>
    route.path === '/vendor/' + item.path && (!item.query || String(route.query.tab || 'suppliers') === item.query.tab))
  return t('vendor.' + (item?.label || 'portal'))
})

watch(
  () => route.fullPath,
  () => {
    mobileOpen.value = false
    profileOpen.value = false
  }
)
function closeProfileMenu() {
  profileOpen.value = false
}
function toggleProfileMenu() {
  profileOpen.value = !profileOpen.value
}
function handleProfilePointerDown(event: PointerEvent) {
  if (profileMenu.value && !profileMenu.value.contains(event.target as Node)) {
    closeProfileMenu()
  }
}
function handleProfileKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && profileOpen.value) {
    closeProfileMenu()
    profileTrigger.value?.focus()
  }
}
onMounted(() => {
  document.addEventListener('pointerdown', handleProfilePointerDown)
  document.addEventListener('keydown', handleProfileKeydown)
})
onUnmounted(() => {
  document.removeEventListener('pointerdown', handleProfilePointerDown)
  document.removeEventListener('keydown', handleProfileKeydown)
  globalAdminStepUp.onCancel()
})
async function logout() {
  vendor.clear()
  await auth.logout()
  await router.replace('/login')
}
</script>

<template>
  <div
    class="vendor-shell min-h-screen overflow-x-clip bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-gray-100"
  >
    <button
      v-if="mobileOpen"
      class="fixed inset-0 z-40 bg-black/40 lg:hidden"
      :aria-label="t('vendor.closeNavigation')"
      @click="mobileOpen = false"
    />
    <aside
      ref="navigation"
      id="vendor-navigation"
      class="sidebar"
      :class="[sidebarCollapsed ? 'w-[72px]' : 'w-64', { '-translate-x-full lg:translate-x-0': !mobileOpen }]"
      :inert="!desktop && !mobileOpen"
      :role="!desktop && mobileOpen ? 'dialog' : undefined"
      :aria-modal="!desktop && mobileOpen ? true : undefined"
      :aria-label="t('vendor.portal')"
      @keydown="navigationKeydown"
      @keydown.esc="mobileOpen = false"
    >
      <div class="sidebar-header" :class="{ 'sidebar-header-collapsed': sidebarCollapsed }">
        <RouterLink :to="home" class="sidebar-logo flex h-9 w-9 items-center justify-center overflow-hidden rounded-xl shadow-glow transition-opacity hover:opacity-80">
          <img :src="app.siteLogo || '/logo.svg'" :alt="app.siteName" class="h-full w-full object-contain" />
        </RouterLink>
        <div class="sidebar-brand" :class="{ 'sidebar-brand-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed">
          <RouterLink :to="home" class="sidebar-brand-title text-lg font-bold text-gray-900 transition-colors hover:text-primary-600 dark:text-white dark:hover:text-primary-400">{{ app.siteName }}</RouterLink>
          <VersionBadge v-if="auth.isSuperAdmin" portal />
        </div>
      </div>
      <nav class="sidebar-nav scrollbar-hide" :aria-label="t('vendor.portal')">
        <div v-for="section in sections" :key="section.label" class="sidebar-section">
          <div v-if="!auth.isSuperAdmin" class="sidebar-section-title" :class="{ 'sidebar-section-title-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed">
            <span class="sidebar-section-title-text" :class="{ 'sidebar-section-title-text-collapsed': sidebarCollapsed }">{{ t('vendor.' + section.label) }}</span>
          </div>
          <RouterLink
            v-for="item in section.items"
            :key="item.label"
            :to="{ path: '/vendor/' + item.path, query: item.query }"
            class="sidebar-link mb-1"
            :title="sidebarCollapsed ? t('vendor.' + item.label) : undefined"
            :aria-label="t('vendor.' + item.label)"
            active-class=""
            :class="{ 'sidebar-link-active': route.path === '/vendor/' + item.path && (!item.query || String(route.query.tab || 'suppliers') === item.query.tab), 'sidebar-link-collapsed': sidebarCollapsed }"
            ><Icon :name="item.icon" size="md" class="shrink-0" /><span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed">{{
              t('vendor.' + item.label)
            }}</span></RouterLink
          >
        </div>
      </nav>
      <div class="mt-auto hidden border-t border-gray-100 p-3 dark:border-dark-800 lg:block">
        <button type="button" class="sidebar-link w-full" :class="{ 'sidebar-link-collapsed': sidebarCollapsed }"
          :title="t(sidebarCollapsed ? 'nav.expand' : 'nav.collapse')"
          :aria-label="t(sidebarCollapsed ? 'nav.expand' : 'nav.collapse')"
          @click="app.toggleSidebar()">
          <Icon :name="sidebarCollapsed ? 'chevronRight' : 'chevronLeft'" size="md" class="shrink-0" />
          <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed">{{ t('nav.collapse') }}</span>
        </button>
      </div>
    </aside>
    <div class="relative min-h-screen min-w-0 transition-all duration-300" :class="sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64'" :inert="mobileOpen && !desktop">
      <header
        class="glass sticky top-0 z-30 flex h-16 items-center justify-between gap-2 border-b border-gray-200/50 px-2 dark:border-dark-700/50 sm:px-4 md:px-6"
        :inert="mobileOpen && !desktop"
      >
        <div class="flex min-w-0 items-center gap-3">
          <button
            ref="navigationTrigger"
            class="btn-ghost btn-icon lg:hidden"
            :aria-label="t('vendor.openNavigation')"
            :aria-expanded="mobileOpen"
            aria-controls="vendor-navigation"
            @click="mobileOpen = !mobileOpen"
          >
            <Icon name="menu" size="md" />
          </button>
          <h1 class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ pageTitle }}</h1>
        </div>
        <div class="flex shrink-0 items-center gap-2">
          <DisplayModeSwitcher />
          <LocaleSwitcher />
          <div ref="profileMenu" class="relative">
            <button
              ref="profileTrigger"
              type="button"
              data-testid="vendor-profile-trigger"
              class="flex items-center gap-2 rounded-xl p-1.5 text-left transition-colors hover:bg-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 dark:hover:bg-dark-800"
              :aria-label="t('vendor.accountCenter')"
              :aria-expanded="profileOpen"
              aria-controls="vendor-profile-menu"
              @click="toggleProfileMenu"
            >
              <span
                class="flex h-8 w-8 shrink-0 items-center justify-center overflow-hidden rounded-full bg-primary-50 text-xs font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
              >
                <img
                  v-if="auth.user?.avatar_url"
                  :src="auth.user.avatar_url"
                  :alt="displayName"
                  class="h-full w-full object-cover"
                />
                <span v-else>{{ profileInitials }}</span>
              </span>
              <span class="hidden max-w-36 sm:block">
                <span class="block truncate text-xs font-semibold text-gray-800 dark:text-gray-100">{{ displayName }}</span>
                <span class="mt-0.5 block truncate text-[11px] text-gray-500 dark:text-dark-400">{{ roleLabel }}</span>
              </span>
              <Icon name="chevronDown" size="xs" class="hidden text-gray-400 sm:block" :class="{ 'rotate-180': profileOpen }" />
            </button>
            <div
              v-if="profileOpen"
              id="vendor-profile-menu"
              role="menu"
              data-testid="vendor-profile-menu"
              class="absolute right-0 top-11 z-50 w-72 overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-xl shadow-gray-900/10 dark:border-dark-700 dark:bg-dark-800 dark:shadow-black/30"
            >
              <div class="border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                <div class="flex items-center gap-3">
                  <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-primary-50 text-sm font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">{{ profileInitials }}</span>
                  <div class="min-w-0">
                    <p class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ displayName }}</p>
                    <p class="truncate text-xs text-gray-500 dark:text-dark-400">{{ profileEmail }}</p>
                    <span class="mt-1 inline-flex rounded-full border border-primary-200 px-2 py-0.5 text-[11px] font-medium text-primary-700 dark:border-primary-800 dark:text-primary-300">{{ roleLabel }}</span>
                  </div>
                </div>
              </div>
              <div class="p-2">
                <RouterLink
                  to="/vendor/profile"
                  role="menuitem"
                  class="flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm text-gray-700 transition-colors hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-dark-700"
                  @click="closeProfileMenu"
                >
                  <Icon name="user" size="sm" class="text-gray-500" />
                  <span>{{ t('vendor.accountSettings') }}</span>
                </RouterLink>
                <button
                  type="button"
                  role="menuitem"
                  class="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-left text-sm text-red-600 transition-colors hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
                  @click="closeProfileMenu(); logout()"
                >
                  <Icon name="login" size="sm" />
                  <span>{{ t('vendor.logout') }}</span>
                </button>
              </div>
            </div>
          </div>
        </div>
      </header>
      <main class="min-w-0 p-4 md:p-6 lg:p-8">
        <slot />
      </main>
    </div>
    <TotpStepUpDialog :controller="globalAdminStepUp" />
  </div>
</template>
