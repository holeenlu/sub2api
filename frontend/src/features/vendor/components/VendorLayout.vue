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
import PortalUpdate from './PortalUpdate.vue'
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
    <header
      class="fixed inset-x-0 top-0 z-40 flex h-14 items-center justify-between border-b border-gray-200 bg-white px-4 dark:border-dark-700 dark:bg-dark-900"
      :inert="mobileOpen && !desktop"
    >
      <div class="flex min-w-0 items-center gap-3">
        <button
          ref="navigationTrigger"
          class="btn btn-secondary btn-sm lg:hidden"
          :aria-label="t('vendor.openNavigation')"
          :aria-expanded="mobileOpen"
          aria-controls="vendor-navigation"
          @click="mobileOpen = !mobileOpen"
        >
          <Icon name="menu" size="sm" />
        </button>
        <RouterLink :to="home" class="flex items-center gap-2.5 text-sm font-semibold">
          <img
            :src="app.siteLogo || '/logo.svg'"
            :alt="app.siteName"
            class="h-8 w-8 rounded-lg object-contain"
          />
          <span>{{ t(auth.isSuperAdmin ? 'vendor.title' : 'vendor.portal') }}</span>
        </RouterLink>
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <PortalUpdate v-if="auth.isSuperAdmin" />
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
    <button
      v-if="mobileOpen"
      class="fixed inset-0 z-40 bg-black/40 lg:hidden"
      :aria-label="t('vendor.closeNavigation')"
      @click="mobileOpen = false"
    />
    <aside
      ref="navigation"
      id="vendor-navigation"
      class="sidebar !top-14 !w-56 !bg-gray-50 dark:!bg-dark-900"
      :class="{ '-translate-x-full lg:translate-x-0': !mobileOpen }"
      :inert="!desktop && !mobileOpen"
      :role="!desktop && mobileOpen ? 'dialog' : undefined"
      :aria-modal="!desktop && mobileOpen ? true : undefined"
      :aria-label="t('vendor.portal')"
      @keydown="navigationKeydown"
      @keydown.esc="mobileOpen = false"
    >
      <nav class="sidebar-nav space-y-6 !px-4 !py-6" :aria-label="t('vendor.portal')">
        <div v-for="section in sections" :key="section.label">
          <p class="mb-2 px-2 text-xs text-gray-400">{{ t('vendor.' + section.label) }}</p>
          <RouterLink
            v-for="item in section.items"
            :key="item.label"
            :to="{ path: '/vendor/' + item.path, query: item.query }"
            class="sidebar-link mb-1 !gap-2 !rounded-lg !px-2 !py-2 !text-[13px]"
            active-class=""
            :class="route.path === '/vendor/' + item.path && (!item.query || String(route.query.tab || 'suppliers') === item.query.tab) ? 'sidebar-link-active' : ''"
            ><Icon :name="item.icon" size="sm" /><span>{{
              t('vendor.' + item.label)
            }}</span></RouterLink
          >
        </div>
      </nav>
      <div class="flex items-center gap-2 border-t border-gray-200 px-4 py-4 dark:border-dark-700">
        <div class="min-w-0 flex-1 text-xs">
          <p class="truncate font-medium" :title="displayName">{{ displayName }}</p>
          <p class="mt-1 text-gray-500">{{ roleLabel }}</p>
        </div>
        <button
          class="rounded-md p-1.5 text-gray-500 hover:bg-gray-100 dark:hover:bg-dark-800"
          :aria-label="t('vendor.logout')"
          :title="t('vendor.logout')"
          @click="logout"
        >
          <Icon name="login" size="sm" />
        </button>
      </div>
    </aside>
    <div class="min-w-0 pt-14 lg:ml-56" :inert="mobileOpen && !desktop">
      <main class="min-w-0 p-4 lg:p-6">

        <slot />
      </main>
    </div>
    <TotpStepUpDialog :controller="globalAdminStepUp" />
  </div>
</template>

<style scoped>
.vendor-shell :deep(.card) {
  @apply rounded-[14px] border-gray-200 shadow-sm dark:border-dark-700;
}
.vendor-shell :deep(.btn-sm) {
  @apply min-h-8 shadow-sm;
}
.vendor-shell :deep(.vendor-filter-bar .input),
.vendor-shell :deep(.vendor-filter-bar .select-trigger) {
  @apply h-8 min-h-8 rounded-lg px-2.5 py-1.5 text-xs;
}
</style>
