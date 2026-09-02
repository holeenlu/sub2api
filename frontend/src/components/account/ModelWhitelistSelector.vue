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

    <!-- Quick Actions -->
    <div class="mb-4 flex flex-wrap gap-2">
      <button
        type="button"
        @click="fillRelated"
        class="rounded-lg border border-blue-200 px-3 py-1.5 text-sm text-blue-600 hover:bg-blue-50 dark:border-blue-800 dark:text-blue-400 dark:hover:bg-blue-900/30"
      >
        {{ t('admin.accounts.fillRelatedModels') }}
      </button>
      <button
        v-if="canSyncUpstream"
        type="button"
        @click="syncUpstreamModels"
        :disabled="isSyncingUpstream"
        class="rounded-lg border border-emerald-200 px-3 py-1.5 text-sm text-emerald-600 hover:bg-emerald-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-emerald-800 dark:text-emerald-400 dark:hover:bg-emerald-900/30"
      >
        {{ isSyncingUpstream ? t('admin.accounts.syncUpstreamModelsLoading') : t('admin.accounts.syncUpstreamModels') }}
      </button>
      <button
        v-if="canSyncAnthropicLive"
        type="button"
        data-testid="sync-live-anthropic-models"
        @click="syncLiveAnthropicModels"
        :disabled="isSyncingLiveAnthropic"
        class="rounded-lg border border-emerald-200 px-3 py-1.5 text-sm text-emerald-600 hover:bg-emerald-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-emerald-800 dark:text-emerald-400 dark:hover:bg-emerald-900/30"
      >
        {{ isSyncingLiveAnthropic ? t('admin.accounts.syncLiveAnthropicModelsLoading') : t('admin.accounts.syncLiveAnthropicModels') }}
      </button>
      <button
        v-if="modelsOutsideLiveIntersection.length > 0"
        type="button"
        data-testid="replace-with-live-anthropic-models"
        @click="replaceWithLiveAnthropicModels"
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

    <!-- Accounts that did not answer the live model sync -->
    <div
      v-if="liveAnthropicFailures.length > 0"
      data-testid="live-anthropic-sync-failures"
      class="mb-4 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
    >
      <p class="font-medium">
        {{ t('admin.accounts.syncLiveAnthropicModelsFailures', { count: liveAnthropicFailures.length }) }}
      </p>
      <ul class="mt-1 space-y-0.5">
        <li v-for="failure in liveAnthropicFailures" :key="failure.account_id">
          {{ failure.name || `#${failure.account_id}` }} — {{ failure.error }}
        </li>
      </ul>
    </div>

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
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { accountsAPI } from '@/api/admin/accounts'
import type {
  AnthropicModelSyncFailure,
  SyncAnthropicModelsBulkFilters,
  SyncUpstreamPreviewParams
} from '@/api/admin/accounts'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { allModels, getModelsByPlatform } from '@/composables/useModelWhitelist'

const { t } = useI18n()

const props = defineProps<{
  modelValue: string[]
  platform?: string
  platforms?: string[]
  accountId?: number
  /** Batch targets for the live Anthropic /v1/models sync (explicit selection). */
  accountIds?: number[]
  /** Batch targets for the live Anthropic /v1/models sync (filter selection). */
  syncFilters?: SyncAnthropicModelsBulkFilters
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
  'deepseek'
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

// 批量编辑 Anthropic 账号时，同一份白名单要写给每个选中的账号，所以只有实时
// /v1/models 的交集才是安全的。单账号编辑仍走 canSyncUpstream 那条路。
const canSyncAnthropicLive = computed(() => {
  return (
    normalizedPlatforms.value.length === 1 &&
    normalizedPlatforms.value[0].toLowerCase() === 'anthropic' &&
    ((props.accountIds?.length ?? 0) > 0 || Boolean(props.syncFilters))
  )
})

const isSyncingLiveAnthropic = ref(false)
const liveAnthropicModels = ref<string[]>([])
const liveAnthropicFailures = ref<AnthropicModelSyncFailure[]>([])

// 已勾选但不在实时交集里的条目。它们未必非法（映射别名、上游刚下架的旧模型都
// 会落在这里），所以只提示、不自动删除。
const modelsOutsideLiveIntersection = computed(() => {
  if (liveAnthropicModels.value.length === 0) return []
  return props.modelValue.filter(model => !liveAnthropicModels.value.includes(model))
})

watch(
  () => [normalizedPlatforms.value.join(','), props.accountIds?.join(',') ?? '', props.syncFilters],
  () => {
    liveAnthropicModels.value = []
    liveAnthropicFailures.value = []
  }
)

const availableOptions = computed(() => {
  if (normalizedPlatforms.value.length === 0) {
    return allModels
  }

  const allowedModels = new Set<string>()
  for (const platform of normalizedPlatforms.value) {
    for (const model of getModelsByPlatform(platform)) {
      allowedModels.add(model)
    }
  }

  return allModels.filter(model => allowedModels.has(model.value))
})

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
  emit('update:modelValue', [...props.modelValue, model])
  customModel.value = ''
}

const handleEnter = () => {
  if (!isComposing.value) addCustom()
}

const fillRelated = () => {
  const newModels = [...props.modelValue]
  for (const platform of normalizedPlatforms.value) {
    for (const model of getModelsByPlatform(platform)) {
      if (!newModels.includes(model)) {
        newModels.push(model)
      }
    }
  }
  emit('update:modelValue', newModels)
}

const syncUpstreamModels = async () => {
  if (isSyncingUpstream.value) return
  if (!props.accountId && !props.syncCredentials) return

  isSyncingUpstream.value = true
  try {
    let result
    if (props.accountId) {
      result = await accountsAPI.syncUpstreamModels(props.accountId)
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

    const newModels = [...props.modelValue]
    let addedCount = 0
    for (const model of upstreamModels) {
      if (!newModels.includes(model)) {
        newModels.push(model)
        addedCount += 1
      }
    }

    emit('update:modelValue', newModels)
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

const syncLiveAnthropicModels = async () => {
  if (isSyncingLiveAnthropic.value || !canSyncAnthropicLive.value) return

  isSyncingLiveAnthropic.value = true
  try {
    const useIDs = (props.accountIds?.length ?? 0) > 0
    const result = await accountsAPI.syncAnthropicModelsBulk({
      account_ids: useIDs ? props.accountIds : undefined,
      filters: useIDs ? undefined : props.syncFilters,
      aggregation: 'intersection',
      // The resulting whitelist is written to every selected account. A
      // partial intersection only describes the accounts that answered and
      // is therefore unsafe to apply to the failures.
      require_all: true
    })

    const models = Array.from(new Set(result.models.map(model => model.trim()).filter(Boolean)))
    liveAnthropicModels.value = models
    liveAnthropicFailures.value = result.failures ?? []
    // 整批失败也走 200，好让逐账号明细能随响应一起回来（错误响应带不了 data）。
    if (result.error || liveAnthropicFailures.value.length > 0) {
      const message = result.error || t('admin.accounts.syncLiveAnthropicModelsFailures', {
        count: liveAnthropicFailures.value.length
      })
      appStore.showError(t('admin.accounts.syncUpstreamModelsError', { message }))
      return
    }
    if (models.length === 0) {
      appStore.showInfo(t('admin.accounts.syncUpstreamModelsEmpty'))
      return
    }

    // 默认合并：实时列表是「上游现在确实支持什么」，不是「这个白名单应该是
    // 什么」。整体替换要管理员在看到差异之后再确认一次。
    const merged = [...props.modelValue]
    let addedCount = 0
    for (const model of models) {
      if (!merged.includes(model)) {
        merged.push(model)
        addedCount += 1
      }
    }
    emit('update:modelValue', merged)

    if (liveAnthropicFailures.value.length > 0) {
      appStore.showWarning(t('admin.accounts.syncLiveAnthropicModelsPartial', {
        count: addedCount,
        total: models.length,
        failed: liveAnthropicFailures.value.length
      }))
    } else if (addedCount > 0) {
      appStore.showSuccess(t('admin.accounts.syncLiveAnthropicModelsSuccess', {
        count: addedCount,
        total: models.length
      }))
    } else {
      appStore.showInfo(t('admin.accounts.syncUpstreamModelsNoChanges', { count: models.length }))
    }
  } catch (error) {
    appStore.showError(t('admin.accounts.syncUpstreamModelsError', { message: extractApiErrorMessage(error, t('admin.accounts.syncUpstreamModelsFailed')) }))
  } finally {
    isSyncingLiveAnthropic.value = false
  }
}

const replaceWithLiveAnthropicModels = () => {
  const dropped = modelsOutsideLiveIntersection.value
  if (dropped.length === 0) return
  if (!confirm(t('admin.accounts.syncLiveAnthropicModelsReplaceConfirm', {
    count: dropped.length,
    models: dropped.join(', ')
  }))) {
    return
  }
  emit('update:modelValue', [...liveAnthropicModels.value])
}

const clearAll = () => {
  emit('update:modelValue', [])
}

</script>
