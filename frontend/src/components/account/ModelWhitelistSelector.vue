<template>
  <div>
    <!-- Multi-select Dropdown -->
    <div class="relative mb-3">
      <div
        @click="toggleDropdown"
        class="cursor-pointer rounded-lg border border-gray-300 bg-white px-3 py-2 dark:border-dark-500 dark:bg-dark-700"
      >
        <div class="grid grid-cols-2 gap-1.5">
          <span
            v-for="model in modelValue"
            :key="model"
            class="inline-flex items-center justify-between gap-1 rounded bg-gray-100 px-2 py-1 text-xs text-gray-700 dark:bg-dark-600 dark:text-gray-300"
          >
            <span class="flex items-center gap-1 truncate">
              <ModelIcon :model="model" size="14px" />
              <span class="truncate">{{ model }}</span>
            </span>
            <button
              type="button"
              @click.stop="removeModel(model)"
              class="shrink-0 rounded-full hover:bg-gray-200 dark:hover:bg-dark-500"
            >
              <Icon name="x" size="xs" class="h-3.5 w-3.5" :stroke-width="2" />
            </button>
          </span>
        </div>
        <div class="mt-2 flex items-center justify-between border-t border-gray-200 pt-2 dark:border-dark-600">
          <span class="text-xs text-gray-400">{{ t('admin.accounts.modelCount', { count: modelValue.length }) }}</span>
          <svg class="h-5 w-5 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
          </svg>
        </div>
      </div>
      <!-- Dropdown List -->
      <div
        v-if="showDropdown"
        class="absolute left-0 right-0 top-full z-50 mt-1 rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-600 dark:bg-dark-700"
      >
        <div class="sticky top-0 border-b border-gray-200 bg-white p-2 dark:border-dark-600 dark:bg-dark-700">
          <input
            v-model="searchQuery"
            type="text"
            class="input w-full text-sm"
            :placeholder="t('admin.accounts.searchModels')"
            @click.stop
          />
        </div>
        <div class="max-h-52 overflow-auto">
          <div
            v-for="model in filteredModels"
            :key="model.value"
            data-testid="model-option"
            class="group flex items-center hover:bg-gray-100 dark:hover:bg-dark-600"
          >
            <button
              type="button"
              data-testid="select-model"
              class="flex min-w-0 flex-1 items-center gap-2 px-3 py-2 text-left text-sm"
              @click="toggleModel(model.value)"
            >
              <span
                :class="[
                  'flex h-4 w-4 shrink-0 items-center justify-center rounded border',
                  modelValue.includes(model.value)
                    ? 'border-primary-500 bg-primary-500 text-white'
                    : 'border-gray-300 dark:border-dark-500'
                ]"
              >
                <svg v-if="modelValue.includes(model.value)" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" />
                </svg>
              </span>
              <ModelIcon :model="model.value" size="18px" />
              <span class="truncate text-gray-900 dark:text-white">{{ model.value }}</span>
            </button>
            <button
              type="button"
              data-testid="copy-model-id"
              class="mr-2 rounded p-1.5 text-gray-400 opacity-70 transition-colors hover:bg-gray-200 hover:text-primary-600 focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 group-hover:opacity-100 dark:text-gray-500 dark:hover:bg-dark-500 dark:hover:text-primary-400"
              :title="`${t('common.copy')} ${model.value}`"
              :aria-label="`${t('common.copy')} ${model.value}`"
              @click="copyModelId(model.value)"
            >
              <Icon name="copy" size="sm" />
            </button>
          </div>
          <div v-if="filteredModels.length === 0" class="px-3 py-4 text-center text-sm text-gray-500">
            {{ t('admin.accounts.noMatchingModels') }}
          </div>
        </div>
      </div>
    </div>

    <p v-if="catalogLoading" class="mb-2 text-xs text-gray-500" role="status">{{ t('modelCatalog.loading') }}</p>
    <p v-else-if="catalogError" class="mb-2 text-xs text-amber-600" role="alert">{{ catalogError }}</p>
    <p v-else-if="!catalogModels.length" class="mb-2 text-xs text-gray-500">{{ t('modelCatalog.notSynced') }}</p>
    <p v-if="selectedRetiredModels.length" class="mb-2 text-xs text-amber-600" data-testid="retired-models">
      {{ t('modelCatalog.retiredSelected', { models: selectedRetiredModels.join(', ') }) }}
      <button type="button" class="ml-2 underline" @click="removeRetiredModels">{{ t('modelCatalog.removeRetired') }}</button>
    </p>
    <a href="/admin/model-catalog" target="_blank" rel="noopener" class="mb-3 inline-block text-xs text-primary-600 underline">{{ t('modelCatalog.manage') }}</a>
    <!-- Quick Actions -->
    <div class="mb-4 flex flex-wrap gap-2">
      <button
        type="button"
        @click="fillRelated"
        :disabled="catalogLoading || !availableOptions.length"
        class="rounded-lg border border-blue-200 px-3 py-1.5 text-sm text-blue-600 hover:bg-blue-50 dark:border-blue-800 dark:text-blue-400 dark:hover:bg-blue-900/30"
      >
        {{ t('modelCatalog.selectAvailable') }}
      </button>
      <button
        v-if="canSyncUpstream"
        type="button"
        data-testid="sync-upstream-models"
        @click="syncUpstreamModels"
        :disabled="isSyncingUpstream"
        class="rounded-lg border border-emerald-200 px-3 py-1.5 text-sm text-emerald-600 hover:bg-emerald-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-emerald-800 dark:text-emerald-400 dark:hover:bg-emerald-900/30"
      >
        {{ isSyncingUpstream || catalogLoading ? t('admin.accounts.syncUpstreamModelsLoading') : t('modelCatalog.refresh') }}
      </button>
      <button
        v-if="canSyncBatch"
        type="button"
        data-testid="sync-upstream-models-bulk"
        @click="syncBatchModels"
        :disabled="isSyncingBatch"
        class="rounded-lg border border-emerald-200 px-3 py-1.5 text-sm text-emerald-600 hover:bg-emerald-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-emerald-800 dark:text-emerald-400 dark:hover:bg-emerald-900/30"
      >
        {{ isSyncingBatch ? t('admin.accounts.syncUpstreamModelsLoading') : t('admin.accounts.syncUpstreamModels') }}
      </button>
      <button
        v-if="modelsOutsideLiveIntersection.length > 0"
        type="button"
        data-testid="replace-with-live-models"
        @click="replaceWithLiveModels"
        class="rounded-lg border border-amber-200 px-3 py-1.5 text-sm text-amber-600 hover:bg-amber-50 dark:border-amber-800 dark:text-amber-400 dark:hover:bg-amber-900/30"
      >
        {{ t('admin.accounts.syncLiveAnthropicModelsReplace', { count: modelsOutsideLiveIntersection.length }) }}
      </button>
      <button
        type="button"
        @click="clearAll"
        class="rounded-lg border border-red-200 px-3 py-1.5 text-sm text-red-600 hover:bg-red-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-900/30"
      >
        {{ t('admin.accounts.clearAllModels') }}
      </button>
    </div>

    <p v-if="canSyncBatch" class="mb-3 text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.syncBulkUpstreamModelsHint') }}
    </p>

    <!-- Accounts that did not answer the live model sync -->
    <div
      v-if="liveFailures.length > 0"
      data-testid="bulk-upstream-sync-failures"
      class="mb-4 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
    >
      <p class="font-medium">
        {{ t('admin.accounts.syncLiveAnthropicModelsFailures', { count: liveFailures.length }) }}
      </p>
      <ul class="mt-1 space-y-0.5">
        <li v-for="failure in liveFailures" :key="failure.account_id">
          {{ failure.name || `#${failure.account_id}` }} — {{ failure.error }}
        </li>
      </ul>
    </div>

    <button v-if="!canSyncUpstream && !canSyncBatch" type="button" class="mb-3 text-xs text-primary-600 underline" :disabled="catalogLoading" @click="loadCatalog()">{{ t('modelCatalog.refresh') }}</button>
    <!-- Custom Model Input -->
    <div class="mb-3">
      <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.customModelName') }}</label>
      <div class="flex gap-2">
        <input
          v-model="customModel"
          type="text"
          class="input flex-1"
          :placeholder="t('admin.accounts.enterCustomModelName')"
          @keydown.enter.prevent="handleEnter"
          @compositionstart="isComposing = true"
          @compositionend="isComposing = false"
        />
        <button
          type="button"
          @click="addCustom"
          class="rounded-lg bg-primary-50 px-4 py-2 text-sm font-medium text-primary-600 hover:bg-primary-100 dark:bg-primary-900/30 dark:text-primary-400 dark:hover:bg-primary-900/50"
        >
          {{ t('admin.accounts.addModel') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { accountsAPI } from '@/api/admin/accounts'
import type {
  AnthropicModelSyncFailure,
  SyncUpstreamModelsBulkFilters,
  SyncUpstreamPreviewParams
} from '@/api/admin/accounts'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { getModelCatalog, refreshModelCatalog, type CatalogModel } from '@/api/admin/modelCatalog'

const { t } = useI18n()

const props = defineProps<{
  modelValue: string[]
  modelMappings?: { from: string; to: string }[]
  platform?: string
  platforms?: string[]
  accountId?: number
  /** Batch targets for the live upstream model sync (explicit selection). */
  accountIds?: number[]
  /** Batch targets for the live upstream model sync (filter selection). */
  syncFilters?: SyncUpstreamModelsBulkFilters
  syncCredentials?: {
    platform: string
    type: string
    base_url?: string
    api_key: string
  }
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
  'upstream-synced': []
}>()

const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const showDropdown = ref(false)
const searchQuery = ref('')
const customModel = ref('')
const isComposing = ref(false)
const isSyncingUpstream = ref(false)
const normalizedPlatforms = computed(() => {
  const rawPlatforms =
    props.platforms && props.platforms.length > 0
      ? props.platforms
      : props.platform
        ? [props.platform]
        : []

  return Array.from(
    new Set(
      rawPlatforms
        .map(platform => platform?.trim())
        .filter((platform): platform is string => Boolean(platform))
    )
  )
})

const upstreamSyncPlatforms = new Set([
  'anthropic',
  'openai',
  'gemini',
  'antigravity',
  'grok',
  'kimi',
  'zhipu',
  'deepseek',
  'minimax',
  'opencode_go'
])
const canSyncUpstream = computed(() => {
  if (props.accountId) {
    if (normalizedPlatforms.value.length === 0) return true
    return normalizedPlatforms.value.some(platform => upstreamSyncPlatforms.has(platform.toLowerCase()))
  }
  if (props.syncCredentials) {
    return upstreamSyncPlatforms.has(props.syncCredentials.platform.toLowerCase())
  }
  return false
})

// A shared whitelist must only add models verified for every batch target.
const canSyncBatch = computed(() =>
  !props.accountId && !props.syncCredentials &&
  ((props.accountIds?.length ?? 0) > 0 || Boolean(props.syncFilters)) &&
  normalizedPlatforms.value.every(platform => upstreamSyncPlatforms.has(platform.toLowerCase()))
)

const isSyncingBatch = ref(false)
const liveModels = ref<string[]>([])
const liveFailures = ref<AnthropicModelSyncFailure[]>([])

// 已勾选但不在实时交集里的条目。它们未必非法（映射别名、上游刚下架的旧模型都
// 会落在这里），所以只提示、不自动删除。
const modelsOutsideLiveIntersection = computed(() => {
  if (liveModels.value.length === 0) return []
  return props.modelValue.filter(model => !liveModels.value.includes(model) && !isMediaChoice(model))
})

let batchRequestVersion = 0
onBeforeUnmount(() => { batchRequestVersion += 1 })

watch(
  () => [normalizedPlatforms.value.join(','), props.accountIds?.join(',') ?? '', props.syncFilters],
  () => {
    batchRequestVersion += 1
    isSyncingBatch.value = false
    liveModels.value = []
    liveFailures.value = []
  },
  { deep: true, flush: 'sync' }
)

const catalogModels = ref<CatalogModel[]>([])
const catalogLoading = ref(false)
const catalogError = ref('')
let catalogRequest = 0
let catalogController: AbortController | undefined
const retiredModels = computed(() => new Set(catalogModels.value.filter(m => m.lifecycle === 'retired').map(m => m.id)))
const selectedRetiredModels = computed(() => props.modelValue.filter(id => retiredModels.value.has(id)))
// These choices configure account supply; discovery evidence is shown in the
// catalog admin page. Loading candidates never changes the saved policy.
const availableOptions = computed(() => catalogModels.value
  .map(m => ({ value: m.id, label: m.display_name || m.id })))

async function loadCatalog(refresh = false) {
  const serial = ++catalogRequest
  catalogController?.abort()
  catalogController = new AbortController()
  const signal = catalogController.signal
  catalogLoading.value = true
  catalogError.value = ''
  try {
    const results = props.accountId
      ? [await (refresh ? refreshModelCatalog(props.accountId, signal, 'selection') : getModelCatalog({ account_id: props.accountId, view: 'selection' }, signal))]
      : await Promise.all((normalizedPlatforms.value.length ? normalizedPlatforms.value : ['']).map(platform => getModelCatalog({ platform, view: 'selection' }, signal)))
    if (serial !== catalogRequest) return
    const entries = new Map<string, CatalogModel>()
    for (const result of results) for (const entry of result.models) if (!entries.has(entry.id)) entries.set(entry.id, entry)
    catalogModels.value = [...entries.values()]
  } catch (error) {
    if (serial === catalogRequest && !(error instanceof DOMException && error.name === 'AbortError')) {
      const reason = extractApiErrorMessage(error, '')
      const knownReasons = ['authentication_unavailable', 'upstream_rate_limited', 'discovery_not_supported', 'upstream_timeout', 'catalog_busy', 'scope_changed', 'upstream_unavailable', 'catalog_unavailable']
      catalogError.value = t('modelCatalog.loadFailed')
      if (reason) catalogError.value += ` ${knownReasons.includes(reason) ? t(`modelCatalog.syncErrors.${reason}`) : reason}`
    }
  } finally { if (serial === catalogRequest) catalogLoading.value = false }
}
watch(() => [props.accountId, normalizedPlatforms.value.join(',')], () => {
  catalogModels.value = []
  void loadCatalog()
}, { immediate: true })
onBeforeUnmount(() => { ++catalogRequest; catalogController?.abort() })

const removeRetiredModels = () => emit('update:modelValue', props.modelValue.filter(id => !retiredModels.value.has(id)))

const filteredModels = computed(() => {
  const query = searchQuery.value.toLowerCase().trim()
  if (!query) return availableOptions.value
  return availableOptions.value.filter(
    m => m.value.toLowerCase().includes(query) || m.label.toLowerCase().includes(query)
  )
})

const toggleDropdown = () => {
  showDropdown.value = !showDropdown.value
  if (!showDropdown.value) searchQuery.value = ''
}

const removeModel = (model: string) => {
  emit('update:modelValue', props.modelValue.filter(m => m !== model))
}

const toggleModel = (model: string) => {
  if (props.modelValue.includes(model)) {
    removeModel(model)
  } else {
    emit('update:modelValue', [...props.modelValue, model])
  }
}

const copyModelId = async (model: string) => {
  await copyToClipboard(model)
}

const addCustom = () => {
  const model = customModel.value.trim()
  if (!model) return
  if (props.modelValue.includes(model)) {
    appStore.showInfo(t('admin.accounts.modelExists'))
    return
  }
  const conflict = props.modelMappings?.find(mapping => mapping.from.trim() === model && mapping.to.trim() && mapping.to.trim() !== model)
  if (conflict) {
    appStore.showInfo(t('admin.accounts.modelMappingConflict', { from: model, to: conflict.to.trim() }))
    return
  }
  emit('update:modelValue', [...props.modelValue, model])
  customModel.value = ''
}

const handleEnter = () => {
  if (!isComposing.value) addCustom()
}

const fillRelated = () => {
  const values = new Set(props.modelValue)
  for (const model of availableOptions.value) {
    const conflict = props.modelMappings?.some(mapping => mapping.from.trim() === model.value && mapping.to.trim() !== model.value)
    if (!conflict) values.add(model.value)
  }
  emit('update:modelValue', [...values])
}

function isMediaChoice(id: string): boolean {
  return catalogModels.value.some(model => model.id === id && ['image', 'video'].includes(model.kind))
}

function applyDiscoveredChoices(models: CatalogModel[]) {
  // Chat discovery may omit media endpoints. Keep media inventory visible;
  // refreshing is not evidence that image/video supply has been withdrawn.
  const existing = new Map(catalogModels.value.map(model => [model.id, model]))
  const incoming = new Set(models.map(model => model.id))
  catalogModels.value = [
    ...catalogModels.value.filter(model => ['image', 'video'].includes(model.kind) && !incoming.has(model.id)),
    ...models.map(model => {
      const previous = existing.get(model.id)
      return previous ? {
        ...model,
        kind: previous.kind === 'unknown' ? model.kind : previous.kind,
        lifecycle: previous.lifecycle === 'retired' ? previous.lifecycle : model.lifecycle,
        metadata: { ...previous.metadata, ...model.metadata }
      } : model
    })
  ]
}

const syncUpstreamModels = async () => {
  if (isSyncingUpstream.value) return
  if (!props.accountId && !props.syncCredentials) return

  isSyncingUpstream.value = true
  try {
    let result
    if (props.accountId) {
      await loadCatalog(true)
      return
    } else if (props.syncCredentials) {
      result = await accountsAPI.syncUpstreamModelsPreview(props.syncCredentials as SyncUpstreamPreviewParams)
    } else {
      return
    }

    const upstreamModels = result.models.map(model => model.trim()).filter(Boolean)
    if (upstreamModels.length === 0) {
      appStore.showInfo(t('admin.accounts.syncUpstreamModelsEmpty'))
      return
    }

    if (!props.accountId) {
      emit('upstream-synced')
    }

    applyDiscoveredChoices(upstreamModels.map(id => ({ id, display_name: id, platform: props.platform || '', kind: result.metadata?.[id]?.model_kind ?? 'unknown', lifecycle: 'active', access: 'listed', source: 'upstream_preview', metadata: result.metadata?.[id] ?? { id }, missing: [], endpoints: [] })))
    const addedCount = upstreamModels.filter(id => !props.modelValue.includes(id)).length
    const warnings = result.warnings ?? []
    const hasPartialMetadata = warnings.some(
      warning => warning.code === 'upstream_model_metadata_partial'
    )
    const hasIncompleteMetadata = warnings.some(
      warning => warning.code === 'upstream_model_metadata_incomplete'
    )
    if (hasIncompleteMetadata) {
      appStore.showWarning(t('admin.accounts.syncUpstreamModelsMetadataIncomplete'))
      return
    }
    if (addedCount > 0) {
      appStore.showSuccess(t('admin.accounts.syncUpstreamModelsSuccess', { count: addedCount, total: upstreamModels.length }))
    } else {
      appStore.showInfo(t('admin.accounts.syncUpstreamModelsNoChanges', { count: upstreamModels.length }))
    }
    if (hasPartialMetadata) {
      appStore.showWarning(t('admin.accounts.syncUpstreamModelsMetadataPartial'))
    }
  } catch (error) {
    appStore.showError(t('admin.accounts.syncUpstreamModelsError', { message: extractApiErrorMessage(error, t('admin.accounts.syncUpstreamModelsFailed')) }))
  } finally {
    isSyncingUpstream.value = false
  }
}

const syncBatchModels = async () => {
  if (isSyncingBatch.value || !canSyncBatch.value) return

  const requestVersion = ++batchRequestVersion
  ++catalogRequest;catalogController?.abort();catalogLoading.value=false
  isSyncingBatch.value = true
  liveModels.value = []
  liveFailures.value = []
  try {
    const useIDs = (props.accountIds?.length ?? 0) > 0
    const result = await accountsAPI.syncUpstreamModelsBulk({
      account_ids: useIDs ? props.accountIds : undefined,
      filters: useIDs ? undefined : props.syncFilters
    })
    if (requestVersion !== batchRequestVersion) return

    const models = Array.from(new Set(result.models.map(model => model.trim()).filter(Boolean)))
    liveFailures.value = result.failures ?? []
    // 整批失败也走 200，好让逐账号明细能随响应一起回来（错误响应带不了 data）。
    if (result.error || liveFailures.value.length > 0) {
      const message = result.error || t('admin.accounts.syncLiveAnthropicModelsFailures', {
        count: liveFailures.value.length
      })
      appStore.showError(t('admin.accounts.syncUpstreamModelsError', { message }))
      return
    }
    if (models.length === 0) {
      appStore.showInfo(t('admin.accounts.syncUpstreamModelsEmpty'))
      return
    }

    liveModels.value = models

    // A successful bulk intersection updates candidates only. Applying it to
    // account restrictions remains an explicit form action.
    await loadCatalog()
    if (requestVersion !== batchRequestVersion) return
    applyDiscoveredChoices(models.map(id => ({ id, display_name: id, platform: props.platform ?? '', kind: 'unknown', lifecycle: 'unknown', access: 'listed', source: 'upstream_bulk', metadata: {id}, missing: [], endpoints: [] })))
    appStore.showInfo(t('admin.accounts.syncUpstreamModelsNoChanges', { count: models.length }))
  } catch (error) {
    if (requestVersion !== batchRequestVersion) return
    appStore.showError(t('admin.accounts.syncUpstreamModelsError', { message: extractApiErrorMessage(error, t('admin.accounts.syncUpstreamModelsFailed')) }))
  } finally {
    if (requestVersion === batchRequestVersion) isSyncingBatch.value = false
  }
}

const replaceWithLiveModels = () => {
  const dropped = modelsOutsideLiveIntersection.value
  if (dropped.length === 0) return
  if (!confirm(t('admin.accounts.syncLiveAnthropicModelsReplaceConfirm', {
    count: dropped.length,
    models: dropped.join(', ')
  }))) {
    return
  }
  emit('update:modelValue', [...new Set([...liveModels.value, ...props.modelValue.filter(isMediaChoice)])])
}

const clearAll = () => {
  emit('update:modelValue', [])
}

</script>
