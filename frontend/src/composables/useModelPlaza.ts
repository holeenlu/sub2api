import { computed, onScopeDispose, ref, watch } from 'vue'
import { getModelPlaza, type ModelPlazaResponse } from '@/api/modelPlaza'
import { useAuthStore } from '@/stores/auth'

/** Each consumer owns its response; account changes invalidate it synchronously. */
export function useModelPlaza() {
  const auth = useAuthStore()
  const data = ref<ModelPlazaResponse | null>(null)
  const loading = ref(true)
  const errorStatus = ref<number | null>(null)
  const loadFailed = computed(() => errorStatus.value != null)
  let controller: AbortController | undefined
  let revision = 0

  async function load() {
    const current = ++revision
    controller?.abort()
    controller = new AbortController()
    data.value = null
    errorStatus.value = null
    loading.value = true
    try {
      const response = await getModelPlaza({ signal: controller.signal })
      if (current === revision) data.value = response
    } catch (error) {
      if (current === revision) {
        const failure = error as { status?: number; response?: { status?: number } }
        errorStatus.value = failure.status ?? failure.response?.status ?? 0
      }
    } finally {
      if (current === revision) loading.value = false
    }
  }

  watch(() => auth.isAuthenticated ? auth.user?.id : null, load, { immediate: true, flush: 'sync' })
  onScopeDispose(() => { revision++; controller?.abort(); data.value = null })
  return { data, loading, loadFailed, errorStatus, load }
}
