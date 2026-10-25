/**
 * Application State Store
 * Manages global UI state including sidebar, loading indicators, and toast notifications
 */

import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Toast, ToastType, PublicSettings } from '@/types'
import { i18n, setSiteName } from '@/i18n'
import { getVersion as getVersionAPI, type VersionInfo } from '@/api/admin/system'
import { getPublicSettings as fetchPublicSettingsAPI } from '@/api/auth'
import { updateFavicon } from '@/utils/branding'
import { BRAND_NAME } from '@/shared/config/project'
import { createPublicSettings } from '@/shared/config/publicSettings'

export const useAppStore = defineStore('app', () => {
  // ==================== State ====================

  const sidebarCollapsed = ref<boolean>(false)
  const mobileOpen = ref<boolean>(false)
  const sidebarScrollTop = ref<number>(0)
  const loading = ref<boolean>(false)
  const toasts = ref<Toast[]>([])

  const publicSettings = createPublicSettings<PublicSettings>({
    defaultSiteName: BRAND_NAME,
    load: fetchPublicSettingsAPI,
    readInjected: () => (typeof window === 'undefined' ? undefined : window.__APP_CONFIG__),
    onApplied: (settings, brand) => {
      if (typeof window !== 'undefined') window.__APP_CONFIG__ = { ...settings }
      setSiteName(brand.siteName)
      updateFavicon(brand.siteLogo)
    },
    onCleared: () => {
      if (typeof window !== 'undefined') delete window.__APP_CONFIG__
    },
    onError: (error) => console.error('Failed to fetch public settings:', error)
  })

  // Read-only information about the currently running build.
  const versionLoaded = ref<boolean>(false)
  const versionLoading = ref<boolean>(false)
  const currentVersion = ref<string>('')
  const upstreamVersion = ref<string>('')
  const buildCommit = ref<string>('')
  let versionRequest: Promise<VersionInfo | null> | null = null
  let versionGeneration = 0

  // Auto-incrementing ID for toasts
  let toastIdCounter = 0

  // ==================== Computed ====================

  const hasActiveToasts = computed(() => toasts.value.length > 0)

  const loadingCount = ref<number>(0)

  // ==================== Actions ====================

  /**
   * Toggle sidebar collapsed state
   */
  function toggleSidebar(): void {
    sidebarCollapsed.value = !sidebarCollapsed.value
  }

  /**
   * Set sidebar collapsed state explicitly
   * @param collapsed - Whether sidebar should be collapsed
   */
  function setSidebarCollapsed(collapsed: boolean): void {
    sidebarCollapsed.value = collapsed
  }

  /**
   * Toggle mobile sidebar open state
   */
  function toggleMobileSidebar(): void {
    mobileOpen.value = !mobileOpen.value
  }

  /**
   * Set mobile sidebar open state explicitly
   * @param open - Whether mobile sidebar should be open
   */
  function setMobileOpen(open: boolean): void {
    mobileOpen.value = open
  }

  /**
   * Set global loading state
   * @param isLoading - Whether app is in loading state
   */
  function setLoading(isLoading: boolean): void {
    if (isLoading) {
      loadingCount.value++
    } else {
      loadingCount.value = Math.max(0, loadingCount.value - 1)
    }
    loading.value = loadingCount.value > 0
  }

  /**
   * Show a toast notification
   * @param type - Type of toast (success, error, info, warning)
   * @param message - Toast message content
   * @param duration - Auto-dismiss duration in ms (undefined = no auto-dismiss)
   * @returns Toast ID for manual dismissal
   */
  function showToast(type: ToastType, message: string, duration?: number): string {
    const id = `toast-${++toastIdCounter}`
    const toast: Toast = {
      id,
      type,
      message,
      duration,
      startTime: duration !== undefined ? Date.now() : undefined
    }

    toasts.value.push(toast)

    // Auto-dismiss if duration is specified
    if (duration !== undefined) {
      setTimeout(() => {
        hideToast(id)
      }, duration)
    }

    return id
  }

  /**
   * Show a success toast
   * @param message - Success message
   * @param duration - Auto-dismiss duration in ms (default: 3000)
   */
  function showSuccess(message: string, duration: number = 3000): string {
    return showToast('success', message, duration)
  }

  /**
   * Show an error toast
   * @param message - Error message
   * @param duration - Auto-dismiss duration in ms (default: 5000)
   */
  function showError(message: string, duration: number = 5000): string {
    return showToast('error', message, duration)
  }

  /**
   * Show an info toast
   * @param message - Info message
   * @param duration - Auto-dismiss duration in ms (default: 3000)
   */
  function showInfo(message: string, duration: number = 3000): string {
    return showToast('info', message, duration)
  }

  /**
   * Show a warning toast
   * @param message - Warning message
   * @param duration - Auto-dismiss duration in ms (default: 4000)
   */
  function showWarning(message: string, duration: number = 4000): string {
    return showToast('warning', message, duration)
  }

  /**
   * Hide a specific toast by ID
   * @param id - Toast ID to hide
   */
  function hideToast(id: string): void {
    const index = toasts.value.findIndex((t) => t.id === id)
    if (index !== -1) {
      toasts.value.splice(index, 1)
    }
  }

  /**
   * Clear all toasts
   */
  function clearAllToasts(): void {
    toasts.value = []
  }

  /**
   * Execute an async operation with loading state
   * Automatically manages loading indicator
   * @param operation - Async operation to execute
   * @returns Promise resolving to operation result
   */
  async function withLoading<T>(operation: () => Promise<T>): Promise<T> {
    setLoading(true)
    try {
      return await operation()
    } finally {
      setLoading(false)
    }
  }

  /**
   * Execute an async operation with loading and error handling
   * Shows error toast on failure
   * @param operation - Async operation to execute
   * @param errorMessage - Custom error message (optional)
   * @returns Promise resolving to operation result or null on error
   */
  async function withLoadingAndError<T>(
    operation: () => Promise<T>,
    errorMessage?: string
  ): Promise<T | null> {
    setLoading(true)
    try {
      return await operation()
    } catch (error) {
      const message =
        errorMessage ||
        (error as { message?: string }).message ||
        i18n.global.t('common.unknownError')
      showError(message)
      return null
    } finally {
      setLoading(false)
    }
  }

  /**
   * Reset app state to defaults
   * Useful for cleanup or testing
   */
  function reset(): void {
    sidebarCollapsed.value = false
    loading.value = false
    loadingCount.value = 0
    toasts.value = []
  }

  // ==================== Version Management ====================

  /** Refresh current build information; force only bypasses a settled local cache. */
  function fetchVersion(force = false): Promise<VersionInfo | null> {
    // Every caller joins the current request, including force and cache readers.
    if (versionRequest) return versionRequest
    if (versionLoaded.value && !force) {
      return Promise.resolve({
        version: currentVersion.value,
        upstream_version: upstreamVersion.value || undefined,
        build_commit: buildCommit.value || undefined
      })
    }

    const generation = versionGeneration
    versionLoading.value = true
    let loading: Promise<VersionInfo>
    try {
      loading = getVersionAPI()
    } catch (error) {
      versionLoading.value = false
      console.error('Failed to fetch version:', error)
      return Promise.resolve(null)
    }

    const request = loading
      .then((data) => {
        if (generation !== versionGeneration) return null
        currentVersion.value = data.version
        upstreamVersion.value = data.upstream_version ?? ''
        buildCommit.value = data.build_commit ?? ''
        versionLoaded.value = true
        return data
      })
      .catch((error: unknown) => {
        if (generation === versionGeneration) console.error('Failed to fetch version:', error)
        return null
      })
      .finally(() => {
        if (versionRequest === request) {
          versionRequest = null
          versionLoading.value = false
        }
      })
    versionRequest = request
    return request
  }

  /** Invalidate the cached build and prevent earlier requests from restoring it. */
  function clearVersionCache(): void {
    versionGeneration += 1
    versionRequest = null
    versionLoading.value = false
    versionLoaded.value = false
    currentVersion.value = ''
    upstreamVersion.value = ''
    buildCommit.value = ''
  }

  // ==================== Return Store API ====================

  return {
    // State
    sidebarCollapsed,
    mobileOpen,
    sidebarScrollTop,
    loading,
    toasts,

    ...publicSettings,

    // Version state
    versionLoaded,
    versionLoading,
    currentVersion,
    upstreamVersion,
    buildCommit,

    // Computed
    hasActiveToasts,

    // Actions
    toggleSidebar,
    setSidebarCollapsed,
    toggleMobileSidebar,
    setMobileOpen,
    setLoading,
    showToast,
    showSuccess,
    showError,
    showInfo,
    showWarning,
    hideToast,
    clearAllToasts,
    withLoading,
    withLoadingAndError,
    reset,

    // Version actions
    fetchVersion,
    clearVersionCache
  }
})
