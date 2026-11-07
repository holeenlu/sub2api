import { installFeatureServices } from './compositions/featureServices'
import { installApplicationHttp } from './http/install'
import { createApp } from 'vue'
import { createPinia, disposePinia } from 'pinia'
import App from './App.vue'
import router from '@/app/router'
import { installRoutePrefetch } from './router/useRoutePrefetch'
import i18n, { configureLocaleMessageLoader, initI18n } from '@/i18n'
import { loadApplicationLocale } from './i18n'
import { useAppStore } from '@/app/model/app'
import { useAdminComplianceStore } from '@/features/identity'
import { BRAND_NAME, BRAND_TITLE_SUFFIX_EN } from '@/shared/config/project'
import { isIOSDevice } from '@/shared/lib/device'

function initIOSViewportZoomFix() {
  // iOS Safari 在输入框字号小于 16px 时聚焦会自动放大页面，且失焦后不会恢复。
  // 限制 maximum-scale 可阻止该行为；iOS 10+ 用户仍可双指手动缩放，不影响可访问性。
  // 仅在 iOS 设备上注入，避免影响 Android Chrome 的手动缩放能力。
  if (!isIOSDevice()) return

  const viewport = document.querySelector('meta[name="viewport"]')
  if (!viewport) return

  const content = viewport.getAttribute('content') || ''
  if (/maximum-scale/i.test(content)) return
  viewport.setAttribute('content', `${content}, maximum-scale=1.0`)
}

export async function bootstrap(): Promise<void> {
  configureLocaleMessageLoader(loadApplicationLocale)
  initIOSViewportZoomFix()

  const app = createApp(App)
  const pinia = createPinia()
  let stateDisposed = false
  const disposeState = (): void => {
    if (stateDisposed) return
    stateDisposed = true
    disposePinia(pinia)
  }
  app.use(pinia)
  let disposeHttp: (() => void) | undefined
  let disposeFeatures: (() => void) | undefined
  let disposePrefetch: (() => void) | undefined

  try {
    disposeHttp = installApplicationHttp({
      requireAdminCompliance: status => useAdminComplianceStore(pinia).requireAcknowledgement(status)
    })
    app.onUnmount(disposeHttp)
    disposeFeatures = installFeatureServices(app)
    // Initialize settings from injected config BEFORE mounting (prevents flash)
    // This must happen after pinia is installed but before router and i18n
    const appStore = useAppStore()
    appStore.initFromInjectedConfig()

    // Set document title immediately after config is loaded
    document.title = `${appStore.siteName || BRAND_NAME} - ${BRAND_TITLE_SUFFIX_EN}`

    await initI18n()

    disposePrefetch = installRoutePrefetch(router)
    app.onUnmount(disposePrefetch)
    app.use(router)
    app.use(i18n)
    app.onUnmount(disposeState)

    // 等待路由器完成初始导航后再挂载，避免竞态条件导致的空白渲染
    await router.isReady()
    app.mount('#app')
  } catch (error) {
    // Vue Router wraps unmount to release its installation even before mount.
    // Vue's own unmount callbacks require a mounted app, so release our owners
    // explicitly as well. Preserve the initialization error if cleanup fails.
    for (const release of [() => app.unmount(), disposePrefetch, disposeFeatures, disposeHttp, disposeState]) {
      try { release?.() } catch (cleanupError) {
        console.error('Failed to release application initialization resources:', cleanupError)
      }
    }
    throw error
  }
}
