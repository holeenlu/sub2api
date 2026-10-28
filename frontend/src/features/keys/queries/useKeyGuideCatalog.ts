import { computed, ref, shallowReactive, watch, onScopeDispose, type Ref } from 'vue'
import { useKeysServices, type KeyModelsManifest } from '../services'
import { useKeysRequest } from './useKeysRequest'
import { joinConfigPath, type KeyGuideProps } from '../model/guide/types'

export function useKeyGuideCatalog(props: KeyGuideProps, activeClientTab: Ref<string>, activeTab: Ref<string>) {
  const services = useKeysServices()
  const request = useKeysRequest(services)
  type CodexModelManifestState = 'idle' | 'loading' | 'ready' | 'error'
  interface CachedCodexModelsManifest extends KeyModelsManifest {
    fetchedAt: number
  }
  // Catalogs load automatically and stay cached per key while /keys is open, so
  // the catalog-derived model survives reopening the dialog and switching tabs.
  // Older entries refresh in the background while the cached catalog stays shown.
  const CODEX_MODEL_MANIFEST_REFRESH_MS = 60_000
  const codexModelManifests = shallowReactive(new Map<string, CachedCodexModelsManifest>())
  const codexModelManifestLoading = ref(false)
  const codexModelManifestFailed = ref(false)
  const codexModelManifest = computed(() => codexModelManifests.get(codexManifestContext.value))
  const codexModelManifestState = computed<CodexModelManifestState>(() => {
    if (codexModelManifest.value) return 'ready'
    if (codexModelManifestLoading.value) return 'loading'
    return codexModelManifestFailed.value ? 'error' : 'idle'
  })
  const codexModelManifestContent = computed(() => codexModelManifest.value?.content ?? '')
  const codexModelManifestModelCount = computed(() => codexModelManifest.value?.modelCount ?? 0)
  const codexModelCatalogMode = ref<'remote' | 'file'>('remote')
  const codexModelCatalogOversized = computed(() => (codexModelManifest.value?.responseBytes ?? 0) > 1024 * 1024)
  const codexModelCatalogUrl = computed(() => services.codexModels.buildCatalogUrl(props.baseUrl))
  const codexLocalCatalogToml = computed(() => codexModelCatalogMode.value === 'file'
    ? `model_catalog_json = "${CODEX_MODEL_CATALOG_CONFIG_PATH}"\n`
    : '')

  const showCodexModelCatalog = computed(() =>
    props.show &&
    (activeClientTab.value === 'codex' ||
      (props.platform === 'openai' && activeClientTab.value === 'codex-ws'))
  )

  const codexModelCatalogPath = computed(() => {
    const isWindows = activeTab.value === 'windows'
    const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'
    return joinConfigPath(configDir, 'codex-models.json', isWindows)
  })

  // Codex expands a leading ~/ on every platform but not %userprofile%, which it
  // resolves relative to the config directory, so config.toml always uses ~/.
  const CODEX_MODEL_CATALOG_CONFIG_PATH = '~/.codex/codex-models.json'

  const codexManifestContext = computed(() => {
    if (!showCodexModelCatalog.value || services.scopeKey.value === null) return ''
    return `${services.scopeKey.value}|${props.platform}|${props.baseUrl}|${props.apiKey}`
  })


  function cancelCodexModelManifestRequest(): void {
    request.cancel(); codexModelManifestLoading.value = false; codexModelManifestFailed.value = false
  }
  async function loadCodexModelManifest(): Promise<void> {
    const context = codexManifestContext.value
    if (!context || !props.apiKey) return
    const ticket = request.begin()
    if (!ticket.isCurrent()) return
    codexModelManifestLoading.value = true; codexModelManifestFailed.value = false
    try {
      const result = await services.codexModels.fetchManifest(props.baseUrl, props.apiKey, ticket.signal)
      if (!ticket.isCurrent() || context !== codexManifestContext.value) return
      codexModelManifests.set(context, { ...result, fetchedAt: Date.now() })
    } catch {
      if (ticket.isCurrent() && context === codexManifestContext.value) codexModelManifestFailed.value = true
    } finally { if (ticket.isCurrent() && context === codexManifestContext.value) codexModelManifestLoading.value = false }
  }
  watch(services.scopeKey, () => { cancelCodexModelManifestRequest(); codexModelManifests.clear() }, { flush: 'sync' })
  watch(codexManifestContext, (context) => {
    cancelCodexModelManifestRequest()
    if (!context) return
    const cached = codexModelManifests.get(context)
    if (!cached || Date.now() - cached.fetchedAt > CODEX_MODEL_MANIFEST_REFRESH_MS) {
      void loadCodexModelManifest()
    }
  }, { immediate: true })

  // Remote mode cannot load a catalog above the 1 MiB limit, including cached ones.
  watch(codexModelCatalogOversized, (oversized) => {
    if (oversized) codexModelCatalogMode.value = 'file'
  })




  onScopeDispose(() => codexModelManifests.clear())
  return { codexModelManifestLoading, codexModelManifestState, codexModelManifestContent,
    codexModelManifestModelCount, codexModelCatalogMode, codexModelCatalogOversized, codexModelCatalogUrl,
    codexLocalCatalogToml, showCodexModelCatalog, codexModelCatalogPath, loadCodexModelManifest }
}
