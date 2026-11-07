<script setup lang="ts">
import { RouterView, useRouter, useRoute } from 'vue-router'
import { computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toast from '@/app/components/Toast.vue'
import NavigationProgress from '@/app/components/NavigationProgress.vue'
import { loadAdminComplianceDialog } from '@/features/identity/panelLoaders'
import { applyRouteMetaDescription, resolveRouteDocumentTitle } from '@/app/router/title'
import { loadAnnouncementPopup } from '@/features/site/announcementPanels'
import { useAppStore } from '@/app/model/app'
import { useAdminSettingsStore } from '@/app/model/adminSettings'
import { useAuthStore, useAdminComplianceStore } from '@/features/identity'
import { useSubscriptionStore } from '@/features/subscriptions'
import { useAnnouncementStore } from '@/features/site/announcements'
import { getSetupStatus } from '@/features/system/api'
import { FeatureFlags, isFeatureFlagEnabled } from '@/app/navigation/featureFlags'
import { isVendorOrigin } from '@/features/supply'
import { resolveSiteBillingMode } from '@/app/navigation/siteBillingMode'
import { globalAdminStepUp } from '@/features/identity'
import { lazyPanel } from './compositions/panelLoadingServices'
import { observePeerSession } from './lifecycle/peerSession'

const AppLayout = lazyPanel(() => import('./layouts/AppLayout.vue'))
const AnnouncementPopup = lazyPanel(loadAnnouncementPopup)
const AdminComplianceDialog = lazyPanel(loadAdminComplianceDialog)

const router = useRouter()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const subscriptionStore = useSubscriptionStore()
const announcementStore = useAnnouncementStore()
const adminComplianceStore = useAdminComplianceStore()
const adminSettingsStore = useAdminSettingsStore()
const { locale } = useI18n()
const stopPeerSession = observePeerSession(() => authStore.checkAuth())
const supplierPortal = computed(() => isVendorOrigin(appStore.cachedPublicSettings) || import.meta.env.VITE_VENDOR_PORTAL === 'true' || route.path.startsWith('/vendor'))
const pageShell = computed(() => {
  if (route.name === 'ModelPlaza' && route.query.embedded === '1' && authStore.isAuthenticated) return 'user'
  if (route.name === 'StripePayment') return route.query.method ? undefined : 'user'
  return route.meta.shell
})

function updateDocumentTitle() {
  const customMenuItems = [
    ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
    ...(authStore.isAdmin ? adminSettingsStore.customMenuItems : []),
  ]
  document.title = resolveRouteDocumentTitle(route, appStore.siteName, customMenuItems, {
    billingMode: resolveSiteBillingMode(appStore.cachedPublicSettings),
  })
  applyRouteMetaDescription(route)
}

// Keep the page title in sync with routes, language and site settings.
watch(
  [
    () => route.fullPath,
    () => route.meta.title,
    () => route.meta.titleKey,
    () => route.meta.metaDescriptionKey,
    // 切换语言要立即换掉 title 与 description
    () => locale.value,
    () => appStore.siteName,
    () => appStore.cachedPublicSettings?.custom_menu_items,
    () => appStore.cachedPublicSettings?.subscription_enabled,
    () => appStore.cachedPublicSettings?.payment_balance_disabled,
    () => authStore.isAdmin,
    () => adminSettingsStore.customMenuItems,
  ],
  updateDocumentTitle,
  { deep: true }
)

// Watch for authentication state and manage subscription data + announcements
function onVisibilityChange() {
  if (!supplierPortal.value && document.visibilityState === 'visible' && authStore.isAuthenticated) {
    announcementStore.fetchAnnouncements()
  }
}

// 订阅功能开关（opt-out）。关闭后不再预加载/轮询订阅接口；开关在登录后才到达时补启动，反向则清空。
const subscriptionFeatureEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.subscription))

function startSubscriptionSync() {
  if (supplierPortal.value) return
  subscriptionStore.fetchActiveSubscriptions().catch((error) => {
    console.error('Failed to preload subscriptions:', error)
  })
  subscriptionStore.startPolling()
}

watch(subscriptionFeatureEnabled, (enabled) => {
  if (!authStore.isAuthenticated || supplierPortal.value) return
  if (enabled) {
    startSubscriptionSync()
  } else {
    subscriptionStore.clear()
  }
})

watch(
  [() => authStore.isAuthenticated, () => authStore.identityScopeKey, supplierPortal],
  async ([isAuthenticated, session, isSupplier], previous, onCleanup) => {
    subscriptionStore.setScope(isAuthenticated && !isSupplier ? session : null)
    announcementStore.setScope(isAuthenticated && !isSupplier ? session : null)
    adminSettingsStore.setScope(isAuthenticated ? session : null)
    let active = true
    let announcementTimer: ReturnType<typeof setTimeout> | undefined
    onCleanup(() => {
      active = false
      if (announcementTimer !== undefined) clearTimeout(announcementTimer)
      document.removeEventListener('visibilitychange', onVisibilityChange)
      subscriptionStore.stopPolling()
    })

    const changedIdentity = previous[0] === true && previous[1] !== session
    const changedPortal = previous[2] !== undefined && previous[2] !== isSupplier
    if (!isAuthenticated || changedIdentity || changedPortal) {
      globalAdminStepUp.onCancel()
      announcementStore.reset()
      adminComplianceStore.reset()
    }
    if (!isAuthenticated) return

    if (!appStore.publicSettingsLoaded) await appStore.fetchPublicSettings()
    if (!active || !authStore.isAuthenticated || session !== authStore.identityScopeKey) return
    if (authStore.isAdmin) {
      adminComplianceStore.fetchStatus().catch((error) => {
        console.error('Failed to fetch admin compliance status:', error)
      })
    }
    if (isSupplier) return

    if (subscriptionFeatureEnabled.value) startSubscriptionSync()
    if (previous[0] === false) {
      announcementTimer = setTimeout(() => {
        if (active && session === authStore.identityScopeKey) {
          announcementStore.fetchAnnouncements(true)
        }
      }, 3000)
    } else {
      announcementStore.fetchAnnouncements()
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
  },
  { immediate: true, flush: 'sync' }
)

// Route change trigger (throttled by store)
const removeAfterEach = router.afterEach(() => {
  if (!supplierPortal.value && authStore.isAuthenticated) {
    announcementStore.fetchAnnouncements()
  }
})

onBeforeUnmount(() => {
  stopPeerSession()
  adminSettingsStore.setScope(null)
  announcementStore.setScope(null)
  subscriptionStore.setScope(null)
  globalAdminStepUp.onCancel()
  removeAfterEach()
  subscriptionStore.stopPolling()
  document.removeEventListener('visibilitychange', onVisibilityChange)
})

onMounted(async () => {
  // Check if setup is needed
  try {
    const status = supplierPortal.value ? { needs_setup: false } : await getSetupStatus()
    if (!supplierPortal.value && status.needs_setup && route.path !== '/setup') {
      router.replace('/setup')
      return
    }
  } catch {
    // If setup endpoint fails, assume normal mode and continue
  }

  // Load public settings into appStore (will be cached for other components)
  await appStore.fetchPublicSettings()

  // Re-resolve document title now that site settings are available
  updateDocumentTitle()
})
</script>

<template>
  <NavigationProgress />
  <RouterView v-slot="{ Component }" :key="route.meta.requiresAuth === false ? undefined : authStore.identityScopeKey">
    <AppLayout v-if="pageShell === 'user' || pageShell === 'admin'">
      <component :is="Component" />
    </AppLayout>
    <component :is="Component" v-else />
  </RouterView>
  <Toast />
  <AnnouncementPopup v-if="!supplierPortal && authStore.isAuthenticated && announcementStore.currentPopup" />
  <AdminComplianceDialog v-if="authStore.isAuthenticated && authStore.isAdmin && adminComplianceStore.shouldShow && (!supplierPortal || authStore.isSuperAdmin)" />
</template>
