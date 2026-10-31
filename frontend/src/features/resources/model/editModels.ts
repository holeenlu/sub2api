import { readAccountModelReferences, usesManagedModelReferences } from './accountModelReferences'
import type { EditResourceForm } from './editEditorTypes'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import accountsAPI from '@/features/resources/accounts'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { createStableObjectKeyResolver } from '@/shared/lib/stableObjectKey'
import { allSelectedGroupsEnableLongContextPricing } from '@/features/resources/model/longContextBilling'
import { buildModelMappingObject } from '../model/modelRestrictions'
import type { Ref, ComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { Props, ModelMapping } from './editEditorTypes'




interface EditModelsInput {
  resourceTasks: ReturnType<typeof useResourceTaskScope>
  form: Pick<EditResourceForm, "group_ids">
  props: Readonly<Pick<Props, "groups" | "account">>
  getPresetMappingsByPlatform: (platform: string) => { label: string; from: string; to: string; color: string }[]
  openaiPassthroughEnabled: Ref<boolean, boolean>
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showInfo" | "showWarning" | "showSuccess" | "showError">>
  t: ReturnType<typeof useI18n>["t"]
  isSyncingAntigravityUpstream: Ref<boolean, boolean>
}




export function useEditModelsDraft(input: EditModelsInput): {
  hideAccountLongContextBilling: ComputedRef<boolean>
  antigravityPresetMappings: ComputedRef<{ label: string; from: string; to: string; color: string }[]>
  bedrockPresets: ComputedRef<{ label: string; from: string; to: string; color: string }[]>
  modelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  openAICompactModelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  modelRestrictionMode: Ref<"whitelist" | "mapping", "whitelist" | "mapping">
  hasOAuthModelRestriction: ComputedRef<boolean>
  allowedModels: Ref<string[], string[]>
  antigravityModelRestrictionMode: Ref<"whitelist" | "mapping", "whitelist" | "mapping">
  antigravityWhitelistModels: Ref<string[], string[]>
  antigravityModelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  getModelMappingKey: (item: ModelMapping) => string
  getOpenAICompactModelMappingKey: (item: ModelMapping) => string
  getAntigravityModelMappingKey: (item: ModelMapping) => string
  excelBPSModels: Ref<string[], string[]>
  openAILongContextBillingEnabled: Ref<boolean, boolean>
  isOpenAIModelRestrictionDisabled: ComputedRef<boolean>
  presetMappings: ComputedRef<{ label: string; from: string; to: string; color: string }[]>
  loadModelRestrictionFromMapping: (rawMapping?: Record<string, unknown>, allowlist?: unknown) => void
  buildModelRestrictionMapping: () => Record<string, string> | null
  applyOpenAIModelMappingCredentials: (credentials: Record<string, unknown>) => void
  addModelMapping: () => void
  removeModelMapping: (index: number) => void
  addPresetMapping: (from: string, to: string) => void
  addAntigravityModelMapping: () => void
  addOpenAICompactModelMapping: () => void
  removeOpenAICompactModelMapping: (index: number) => void
  removeAntigravityModelMapping: (index: number) => void
  addAntigravityPresetMapping: (from: string, to: string) => void
  syncAntigravityUpstreamModels: () => Promise<void>
} {


  const hideAccountLongContextBilling = computed(() => {
    return allSelectedGroupsEnableLongContextPricing(input.form.group_ids, input.props.groups)
  })


  const antigravityPresetMappings = computed(() => input.getPresetMappingsByPlatform('antigravity'))

  const bedrockPresets = computed(() => input.getPresetMappingsByPlatform('bedrock'))

  const modelMappings = ref<ModelMapping[]>([])

  const openAICompactModelMappings = ref<ModelMapping[]>([])

  const modelRestrictionMode = ref<'whitelist' | 'mapping'>('whitelist')

  const hasOAuthModelRestriction = computed(() => {
    const account = input.props.account
    return account != null && (
      (account.platform === 'anthropic' && (account.type === 'oauth' || account.type === 'setup-token')) ||
      ((account.platform === 'openai' || account.platform === 'grok') && account.type === 'oauth')
    )
  })

  const allowedModels = ref<string[]>([])

  const antigravityModelRestrictionMode = ref<'whitelist' | 'mapping'>('whitelist')

  const antigravityWhitelistModels = ref<string[]>([])

  const antigravityModelMappings = ref<ModelMapping[]>([])

  const getModelMappingKey = createStableObjectKeyResolver<ModelMapping>('edit-model-mapping')

  const getOpenAICompactModelMappingKey = createStableObjectKeyResolver<ModelMapping>('edit-openai-compact-model-mapping')

  const getAntigravityModelMappingKey = createStableObjectKeyResolver<ModelMapping>('edit-antigravity-model-mapping')


  const excelBPSModels = ref<string[]>([])

  const openAILongContextBillingEnabled = ref(false)

  const isOpenAIModelRestrictionDisabled = computed(() =>
    input.props.account?.platform === 'openai' && input.openaiPassthroughEnabled.value
  )


  // Computed: current preset mappings based on platform
  const presetMappings = computed(() => input.getPresetMappingsByPlatform(input.props.account?.platform || 'anthropic'))


  const loadModelRestrictionFromMapping = (rawMapping?: Record<string, unknown>, allowlist?: unknown) => {
    const parsed = readAccountModelReferences(input.props.account?.platform || '', rawMapping, allowlist)
    allowedModels.value = parsed.allowedModels
    modelMappings.value = parsed.modelMappings
    modelRestrictionMode.value =
      parsed.modelMappings.length > 0 && parsed.allowedModels.length === 0
        ? 'mapping'
        : 'whitelist'
  }


  const buildModelRestrictionMapping = () =>
    buildModelMappingObject(usesManagedModelReferences(input.props.account?.platform || '') ? 'mapping' : 'combined', allowedModels.value, modelMappings.value)


  const applyOpenAIModelMappingCredentials = (credentials: Record<string, unknown>) => {
    const shouldApplyModelMapping = !input.openaiPassthroughEnabled.value

    if (shouldApplyModelMapping) {
      const modelMapping = buildModelRestrictionMapping()
      if (modelMapping) {
        credentials.model_mapping = modelMapping
      } else {
        delete credentials.model_mapping
      }
    } else if (!credentials.model_mapping) {
      delete credentials.model_mapping
    }

    const compactModelMapping = buildModelMappingObject('mapping', [], openAICompactModelMappings.value)
    if (compactModelMapping) {
      credentials.compact_model_mapping = compactModelMapping
    } else {
      delete credentials.compact_model_mapping
    }
  }


  // Model mapping helpers
  const addModelMapping = () => {
    modelMappings.value.push({ from: '', to: '' })
  }


  const removeModelMapping = (index: number) => {
    modelMappings.value.splice(index, 1)
  }


  const addPresetMapping = (from: string, to: string) => {
    const exists = modelMappings.value.some((m) => m.from === from)
    if (exists) {
      input.appStore.showInfo(input.t('admin.accounts.mappingExists', { model: from }))
      return
    }
    modelMappings.value.push({ from, to })
  }


  const addAntigravityModelMapping = () => {
    antigravityModelMappings.value.push({ from: '', to: '' })
  }


  const addOpenAICompactModelMapping = () => {
    openAICompactModelMappings.value.push({ from: '', to: '' })
  }


  const removeOpenAICompactModelMapping = (index: number) => {
    openAICompactModelMappings.value.splice(index, 1)
  }


  const removeAntigravityModelMapping = (index: number) => {
    antigravityModelMappings.value.splice(index, 1)
  }


  const addAntigravityPresetMapping = (from: string, to: string) => {
    const exists = antigravityModelMappings.value.some((m) => m.from === from)
    if (exists) {
      input.appStore.showInfo(input.t('admin.accounts.mappingExists', { model: from }))
      return
    }
    antigravityModelMappings.value.push({ from, to })
  }


  const syncAntigravityUpstreamModels = async () => {
    const pending = input.resourceTasks.capture()
    try {

      if (!input.props.account?.id || input.isSyncingAntigravityUpstream.value) return

      input.isSyncingAntigravityUpstream.value = true
      try {
        const accountID = input.props.account.id
        const result = await pending.wait(async () => accountsAPI.syncUpstreamModels(accountID))
        const upstreamModels = result.models.map((model) => model.trim()).filter(Boolean)
        if (upstreamModels.length === 0) {
          input.appStore.showInfo(input.t('admin.accounts.syncUpstreamModelsEmpty'))
          return
        }

        let addedCount = 0
        for (const model of upstreamModels) {
          const exists = antigravityModelMappings.value.some((mapping) => mapping.from === model)
          if (!exists) {
            antigravityModelMappings.value.push({ from: model, to: model })
            addedCount += 1
          }
        }

        const warnings = result.warnings ?? []
        const hasPartialMetadata = warnings.some(
          (warning) => warning.code === 'upstream_model_metadata_partial'
        )
        const hasIncompleteMetadata = warnings.some(
          (warning) => warning.code === 'upstream_model_metadata_incomplete'
        )
        if (hasIncompleteMetadata) {
          input.appStore.showWarning(input.t('admin.accounts.syncUpstreamModelsMetadataIncomplete'))
          return
        }
        if (addedCount > 0) {
          input.appStore.showSuccess(input.t('admin.accounts.syncUpstreamModelsSuccess', { count: addedCount, total: upstreamModels.length }))
        } else {
          input.appStore.showInfo(input.t('admin.accounts.syncUpstreamModelsNoChanges', { count: upstreamModels.length }))
        }
        if (hasPartialMetadata) {
          input.appStore.showWarning(input.t('admin.accounts.syncUpstreamModelsMetadataPartial'))
        }
      } catch (error) {
        if (!pending.current()) throw new StaleResourceTask()

        const message = extractApiErrorMessage(error, input.t('admin.accounts.syncUpstreamModelsFailed'))
        input.appStore.showError(input.t('admin.accounts.syncUpstreamModelsError', { message }))
      } finally {
        if (pending.current()) {

          input.isSyncingAntigravityUpstream.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }
  return { hideAccountLongContextBilling, antigravityPresetMappings, bedrockPresets, modelMappings, openAICompactModelMappings, modelRestrictionMode, hasOAuthModelRestriction, allowedModels, antigravityModelRestrictionMode, antigravityWhitelistModels, antigravityModelMappings, getModelMappingKey, getOpenAICompactModelMappingKey, getAntigravityModelMappingKey, excelBPSModels, openAILongContextBillingEnabled, isOpenAIModelRestrictionDisabled, presetMappings, loadModelRestrictionFromMapping, buildModelRestrictionMapping, applyOpenAIModelMappingCredentials, addModelMapping, removeModelMapping, addPresetMapping, addAntigravityModelMapping, addOpenAICompactModelMapping, removeOpenAICompactModelMapping, removeAntigravityModelMapping, addAntigravityPresetMapping, syncAntigravityUpstreamModels }
}
