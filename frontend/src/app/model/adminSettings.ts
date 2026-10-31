import { defineStore } from 'pinia'
import { onScopeDispose, ref } from 'vue'
import { isCancel } from 'axios'
import { useAuthStore } from '@/features/identity'
import { useAppStore } from '@/app/model/app'
import { settingsAPI } from '@/features/system/api'
import { adminPaymentAPI } from '@/features/payments'
import type { CustomMenuItem } from '@/features/site/api/publicSettingsTypes';

interface FetchOptions {
  signal?: AbortSignal
  /** Save flows report a failed projection separately from their confirmed write. */
  throwOnError?: boolean
}

async function observeFetch(task: Promise<void>, options?: FetchOptions): Promise<void> {
  try {
    await task
  } catch (error) {
    // Background navigation loads retain their existing non-throwing contract.
    if (options?.throwOnError) throw error
  }
}

export const useAdminSettingsStore = defineStore('adminSettings', () => {
  const loaded = ref(false)
  const loading = ref(false)

  const readCachedBool = (key: string, defaultValue: boolean): boolean => {
    try {
      const raw = localStorage.getItem(key)
      if (raw === 'true') return true
      if (raw === 'false') return false
    } catch {
      // ignore localStorage failures
    }
    return defaultValue
  }

  const writeCachedBool = (key: string, value: boolean) => {
    try {
      localStorage.setItem(key, value ? 'true' : 'false')
    } catch {
      // ignore localStorage failures
    }
  }

  const readCachedString = (key: string, defaultValue: string): string => {
    try {
      const raw = localStorage.getItem(key)
      if (typeof raw === 'string' && raw.length > 0) return raw
    } catch {
      // ignore localStorage failures
    }
    return defaultValue
  }

  const writeCachedString = (key: string, value: string) => {
    try {
      localStorage.setItem(key, value)
    } catch {
      // ignore localStorage failures
    }
  }

  // Default open, but honor cached value to reduce UI flicker on first paint.
  const opsMonitoringEnabled = ref(readCachedBool('ops_monitoring_enabled_cached', true))
  const opsRealtimeMonitoringEnabled = ref(readCachedBool('ops_realtime_monitoring_enabled_cached', true))
  const opsQueryModeDefault = ref(readCachedString('ops_query_mode_default_cached', 'auto'))
  const paymentEnabled = ref(readCachedBool('payment_enabled_cached', false))
  const customMenuItems = ref<CustomMenuItem[]>([])

  const auth = useAuthStore()
  let scope: string | null | undefined
  let generation = 0
  let controller: AbortController | undefined
  let pending: Promise<void> | undefined

  function setScope(next: string | null): void {
    if (next === scope) return
    const initialized = scope !== undefined
    scope = next
    generation += 1
    controller?.abort()
    controller = undefined
    pending = undefined
    loaded.value = false
    loading.value = false
    customMenuItems.value = []
    if (initialized || next === null) {
      // A prior administrator's settings must not survive an identity/role change.
      opsMonitoringEnabled.value = false
      opsRealtimeMonitoringEnabled.value = false
      opsQueryModeDefault.value = 'auto'
      paymentEnabled.value = false
    }
  }

  async function fetch(force = false, options?: FetchOptions): Promise<void> {
    const expectedScope = auth.isAuthenticated ? auth.identityScopeKey : null
    setScope(expectedScope)
    if (expectedScope === null || options?.signal?.aborted) return
    if (!auth.isSuperAdmin) {
      const features = auth.user?.admin_features || {}
      opsMonitoringEnabled.value = features.ops_monitoring_enabled === true
      opsRealtimeMonitoringEnabled.value = features.ops_realtime_monitoring_enabled === true
      paymentEnabled.value = useAppStore().cachedPublicSettings?.payment_enabled === true
      customMenuItems.value = []
      loaded.value = true
      return
    }
    if (loaded.value && !force) return
    if (pending && !force) return observeFetch(pending, options)

    controller?.abort()
    const request = new AbortController()
    controller = request
    const version = ++generation
    const cancel = () => request.abort()
    options?.signal?.addEventListener('abort', cancel, { once: true })
    const isCurrent = () => !request.signal.aborted && version === generation
      && scope === expectedScope && auth.isAuthenticated && auth.identityScopeKey === expectedScope
    loading.value = true
    const task = (async () => {
      try {
        const [settings, paymentConfigResp] = await Promise.all([
          settingsAPI.getSettings({ signal: request.signal }),
          adminPaymentAPI.getConfig({ signal: request.signal })
        ])
        if (!isCurrent()) return
        opsMonitoringEnabled.value = settings.ops_monitoring_enabled ?? true
        opsRealtimeMonitoringEnabled.value = settings.ops_realtime_monitoring_enabled ?? true
        opsQueryModeDefault.value = settings.ops_query_mode_default || 'auto'
        customMenuItems.value = Array.isArray(settings.custom_menu_items) ? settings.custom_menu_items : []
        paymentEnabled.value = paymentConfigResp.data?.enabled ?? false
        writeCachedBool('ops_monitoring_enabled_cached', opsMonitoringEnabled.value)
        writeCachedBool('ops_realtime_monitoring_enabled_cached', opsRealtimeMonitoringEnabled.value)
        writeCachedString('ops_query_mode_default_cached', opsQueryModeDefault.value)
        writeCachedBool('payment_enabled_cached', paymentEnabled.value)
        loaded.value = true
      } catch (error) {
        if (isCurrent() && !isCancel(error)) {
          console.error('[adminSettings] Failed to fetch settings:', error)
          throw error
        }
      } finally {
        options?.signal?.removeEventListener('abort', cancel)
        if (version === generation) {
          loading.value = false
          controller = undefined
          pending = undefined
        }
      }
    })()
    pending = task
    return observeFetch(task, options)
  }

  onScopeDispose(() => setScope(null))

  function setOpsMonitoringEnabledLocal(value: boolean) {
    opsMonitoringEnabled.value = value
    writeCachedBool('ops_monitoring_enabled_cached', value)
    loaded.value = true
  }

  function setOpsRealtimeMonitoringEnabledLocal(value: boolean) {
    opsRealtimeMonitoringEnabled.value = value
    writeCachedBool('ops_realtime_monitoring_enabled_cached', value)
    loaded.value = true
  }

  function setPaymentEnabledLocal(value: boolean) {
    paymentEnabled.value = value
    writeCachedBool('payment_enabled_cached', value)
    loaded.value = true
  }

  function setOpsQueryModeDefaultLocal(value: string) {
    opsQueryModeDefault.value = value || 'auto'
    writeCachedString('ops_query_mode_default_cached', opsQueryModeDefault.value)
    loaded.value = true
  }

  return {
    loaded,
    loading,
    opsMonitoringEnabled,
    opsRealtimeMonitoringEnabled,
    opsQueryModeDefault,
    paymentEnabled,
    customMenuItems,
    fetch,
    setScope,
    setOpsMonitoringEnabledLocal,
    setOpsRealtimeMonitoringEnabledLocal,
    setPaymentEnabledLocal,
    setOpsQueryModeDefaultLocal
  }
})
