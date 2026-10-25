<script setup lang="ts">
import { RouterView, useRouter, useRoute } from 'vue-router'
import { computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toast from '@/components/common/Toast.vue'
import NavigationProgress from '@/components/common/NavigationProgress.vue'
import AdminComplianceDialog from '@/components/admin/AdminComplianceDialog.vue'
import { applyRouteMetaDescription, resolveRouteDocumentTitle } from '@/router/title'
import AnnouncementPopup from '@/components/common/AnnouncementPopup.vue'
import { useAppStore, useAuthStore, useSubscriptionStore, useAnnouncementStore, useAdminComplianceStore, useAdminSettingsStore } from '@/stores'
import { getSetupStatus } from '@/api/setup'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { isVendorOrigin } from '@/features/vendor/helpers'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import { authSessionStamp } from '@/shared/session'

const router = useRouter()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const subscriptionStore = useSubscriptionStore()
const announcementStore = useAnnouncementStore()
const adminComplianceStore = useAdminComplianceStore()
const adminSettingsStore = useAdminSettingsStore()
const { locale } = useI18n()
const supplierPortal = computed(() => isVendorOrigin(appStore.cachedPublicSettings) || import.meta.env.VITE_VENDOR_PORTAL === 'true' || route.path.startsWith('/vendor'))

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

function onAdminComplianceRequired(event: Event) {
  const detail = (event as CustomEvent<Record<string, string>>).detail || {}
  adminComplianceStore.requireAcknowledgement(detail)
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
  [() => authStore.isAuthenticated, () => authSessionStamp(authStore.token), supplierPortal],
  async ([isAuthenticated, session, isSupplier], previous, onCleanup) => {
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
      subscriptionStore.clear()
      announcementStore.reset()
      adminComplianceStore.reset()
    }
    if (!isAuthenticated) return

    if (!appStore.publicSettingsLoaded) await appStore.fetchPublicSettings()
    if (!active || !authStore.isAuthenticated || session !== authSessionStamp(authStore.token)) return
    if (authStore.isAdmin) {
      adminComplianceStore.fetchStatus().catch((error) => {
        console.error('Failed to fetch admin compliance status:', error)
      })
    }
    if (isSupplier) return

    if (subscriptionFeatureEnabled.value) startSubscriptionSync()
    if (previous[0] === false) {
      announcementTimer = setTimeout(() => {
        if (active && session === authSessionStamp(authStore.token)) {
          announcementStore.fetchAnnouncements(true)
        }
      }, 3000)
    } else {
      announcementStore.fetchAnnouncements()
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
  },
  { immediate: true }
)

// Route change trigger (throttled by store)
const removeAfterEach = router.afterEach(() => {
  if (!supplierPortal.value && authStore.isAuthenticated) {
    announcementStore.fetchAnnouncements()
  }
})

onBeforeUnmount(() => {
  removeAfterEach()
  subscriptionStore.stopPolling()
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('admin-compliance-required', onAdminComplianceRequired)
})

onMounted(async () => {
  window.addEventListener('admin-compliance-required', onAdminComplianceRequired)

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
  <RouterView :key="`${authStore.user?.id}:${authStore.user?.role}:${authStore.user?.policy_version}`" />
  <Toast />
  <AnnouncementPopup v-if="!supplierPortal" />
  <AdminComplianceDialog v-if="!supplierPortal || authStore.isSuperAdmin" />
</template>
