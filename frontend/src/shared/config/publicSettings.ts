import { computed, ref, shallowRef } from 'vue'
import type { ComputedRef, Ref, ShallowRef } from 'vue'

/** Only fields projected by the shared settings lifecycle; callers retain their full DTO. */
export interface PublicSettingsFields {
  site_name: string
  site_logo: string
  version: string
  contact_info: string
  api_base_url: string
  doc_url: string
  backend_mode_enabled: boolean
}

export interface PublicSettingsBrand {
  siteName: string
  siteLogo: string
}

export interface PublicSettingsOptions<T extends PublicSettingsFields> {
  defaultSiteName: string
  load: () => Promise<T>
  readInjected: () => T | undefined
  onApplied: (settings: T, brand: PublicSettingsBrand) => void
  onCleared: () => void
  onError: (error: unknown) => void
}

export interface PublicSettingsState<T extends PublicSettingsFields> {
  publicSettingsLoaded: ComputedRef<boolean>
  publicSettingsLoading: Ref<boolean>
  cachedPublicSettings: ShallowRef<T | null>
  siteName: Ref<string>
  siteLogo: Ref<string>
  siteVersion: Ref<string>
  contactInfo: Ref<string>
  apiBaseUrl: Ref<string>
  docUrl: Ref<string>
  backendModeEnabled: ComputedRef<boolean>
  fetchPublicSettings: (force?: boolean) => Promise<T | null>
  clearPublicSettingsCache: () => void
  initFromInjectedConfig: () => boolean
}

/** One settings owner per application instance, with browser effects supplied by its host. */
export function createPublicSettings<T extends PublicSettingsFields>(
  options: PublicSettingsOptions<T>
): PublicSettingsState<T> {
  const cachedPublicSettings: ShallowRef<T | null> = shallowRef(null)
  const publicSettingsLoaded = computed(() => cachedPublicSettings.value !== null)
  const publicSettingsLoading = ref(false)
  // Keep the last applied display values when invalidating the cache to avoid a brand flash.
  const siteName = ref(options.defaultSiteName)
  const siteLogo = ref('')
  const siteVersion = ref('')
  const contactInfo = ref('')
  const apiBaseUrl = ref('')
  const docUrl = ref('')
  const backendModeEnabled = computed(
    () => cachedPublicSettings.value?.backend_mode_enabled ?? false
  )
  let generation = 0
  let request: Promise<T | null> | null = null

  function applySettings(settings: T): void {
    siteName.value = settings.site_name || options.defaultSiteName
    siteLogo.value = settings.site_logo || ''
    siteVersion.value = settings.version || ''
    contactInfo.value = settings.contact_info || ''
    apiBaseUrl.value = settings.api_base_url || ''
    docUrl.value = settings.doc_url || ''
    options.onApplied(settings, { siteName: siteName.value, siteLogo: siteLogo.value })
    cachedPublicSettings.value = settings
  }

  function initFromInjectedConfig(): boolean {
    // Injection bootstraps an empty owner; it cannot replace a loaded or refreshing snapshot.
    if (request || publicSettingsLoaded.value) return false
    const injected = options.readInjected()
    if (!injected) return false
    applySettings(injected)
    return true
  }

  function fetchPublicSettings(force = false): Promise<T | null> {
    // Force bypasses settled cache, never an active request in the same generation.
    if (request) return request
    if (!force) {
      if (cachedPublicSettings.value) {
        return Promise.resolve({ ...cachedPublicSettings.value })
      }
      if (initFromInjectedConfig()) return Promise.resolve(cachedPublicSettings.value)
    }

    const requestGeneration = generation
    publicSettingsLoading.value = true
    let loading: Promise<T>
    try {
      loading = options.load()
    } catch (error) {
      publicSettingsLoading.value = false
      options.onError(error)
      return Promise.resolve(null)
    }

    const pending = loading
      .then((settings) => {
        if (requestGeneration !== generation) return null
        applySettings(settings)
        return settings
      })
      .catch((error: unknown) => {
        if (requestGeneration === generation) options.onError(error)
        return null
      })
      .finally(() => {
        // An invalidated request must not release another generation's loading state.
        if (request === pending) {
          request = null
          publicSettingsLoading.value = false
        }
      })
    request = pending
    return pending
  }

  function clearPublicSettingsCache(): void {
    generation += 1
    request = null
    publicSettingsLoading.value = false
    cachedPublicSettings.value = null
    // The host also drops its injection mirror so the next read cannot resurrect stale data.
    options.onCleared()
  }

  return {
    publicSettingsLoaded,
    publicSettingsLoading,
    cachedPublicSettings,
    siteName,
    siteLogo,
    siteVersion,
    contactInfo,
    apiBaseUrl,
    docUrl,
    backendModeEnabled,
    fetchPublicSettings,
    clearPublicSettingsCache,
    initFromInjectedConfig
  }
}
