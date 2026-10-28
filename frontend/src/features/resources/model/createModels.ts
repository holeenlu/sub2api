import type { CreateResourceForm } from './createEditorTypes'
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAntigravityDefaultModelMapping } from '@/features/resources/accounts'
import { buildModelMappingObject } from '../model/modelRestrictions'
import { allSelectedGroupsEnableLongContextPricing } from '@/features/resources/model/longContextBilling'
import { createStableObjectKeyResolver } from '@/shared/lib/stableObjectKey'
import type { Ref, ComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { Props, ModelMapping } from './createEditorTypes'




interface CreateModelsInput {
  form: Pick<CreateResourceForm, "group_ids" | "platform">
  props: Readonly<Pick<Props, "groups">>
  providerRoutesByModel: ReturnType<typeof createProviderCredentialOptions>["providerRoutesByModel"]
  getPresetMappingsByPlatform: (platform: string) => { label: string; from: string; to: string; color: string }[]
  openaiPassthroughEnabled: Ref<boolean, boolean>
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showInfo">>
  t: ReturnType<typeof useI18n>["t"]
}




export function useCreateModelsDraft(input: CreateModelsInput): {
  fetchAntigravityDefaultMappings: () => Promise<{ from: string; to: string }[]>
  hideAccountLongContextBilling: ComputedRef<boolean>
  routesByModel: ComputedRef<boolean>
  modelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  openAICompactModelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  modelRestrictionMode: Ref<"whitelist" | "mapping", "whitelist" | "mapping">
  allowedModels: Ref<string[], string[]>
  isLoadingBuiltinModels: Ref<boolean, boolean>
  openAILongContextBillingEnabled: Ref<boolean, boolean>
  openAILongContextBillingTouched: Ref<boolean, boolean>
  toggleOpenAILongContextBilling: () => void
  antigravityModelRestrictionMode: Ref<"whitelist" | "mapping", "whitelist" | "mapping">
  antigravityWhitelistModels: Ref<string[], string[]>
  antigravityModelMappings: Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>
  antigravityPresetMappings: ComputedRef<{ label: string; from: string; to: string; color: string }[]>
  bedrockPresets: ComputedRef<{ label: string; from: string; to: string; color: string }[]>
  getModelMappingKey: (item: ModelMapping) => string
  getOpenAICompactModelMappingKey: (item: ModelMapping) => string
  getAntigravityModelMappingKey: (item: ModelMapping) => string
  buildOpenAICompactModelMapping: () => Record<string, string> | null
  isOpenAIModelRestrictionDisabled: ComputedRef<boolean>
  presetMappings: ComputedRef<{ label: string; from: string; to: string; color: string }[]>
  addModelMapping: () => void
  addOpenAICompactModelMapping: () => void
  removeOpenAICompactModelMapping: (index: number) => void
  removeModelMapping: (index: number) => void
  addPresetMapping: (from: string, to: string) => void
  addAntigravityModelMapping: () => void
  removeAntigravityModelMapping: (index: number) => void
  addAntigravityPresetMapping: (from: string, to: string) => void
} {

  const fetchAntigravityDefaultMappings = async () => Object.entries(await getAntigravityDefaultModelMapping()).map(([from, to]) => ({ from, to }))


  const hideAccountLongContextBilling = computed(() => {
    return allSelectedGroupsEnableLongContextPricing(input.form.group_ids, input.props.groups)
  })

  // 按模型分流的供应商（OpenCode 等）：adaptive 账号携带 protocol_rules。
  const routesByModel = computed(() => input.providerRoutesByModel(input.form.platform))

  const modelMappings = ref<ModelMapping[]>([])

  const openAICompactModelMappings = ref<ModelMapping[]>([])

  const modelRestrictionMode = ref<'whitelist' | 'mapping'>('whitelist')

  const allowedModels = ref<string[]>([])

  const isLoadingBuiltinModels = ref(false)


  const openAILongContextBillingEnabled = ref(false)

  const openAILongContextBillingTouched = ref(false)


  const toggleOpenAILongContextBilling = () => {
    openAILongContextBillingEnabled.value = !openAILongContextBillingEnabled.value
    openAILongContextBillingTouched.value = true
  }
  // For upstream type: API key
  const antigravityModelRestrictionMode = ref<'whitelist' | 'mapping'>('whitelist')

  const antigravityWhitelistModels = ref<string[]>([])

  const antigravityModelMappings = ref<ModelMapping[]>([])

  const antigravityPresetMappings = computed(() => input.getPresetMappingsByPlatform('antigravity'))

  const bedrockPresets = computed(() => input.getPresetMappingsByPlatform('bedrock'))

  const getModelMappingKey = createStableObjectKeyResolver<ModelMapping>('create-model-mapping')

  const getOpenAICompactModelMappingKey = createStableObjectKeyResolver<ModelMapping>('create-openai-compact-model-mapping')

  const getAntigravityModelMappingKey = createStableObjectKeyResolver<ModelMapping>('create-antigravity-model-mapping')


  const buildOpenAICompactModelMapping = () =>
    buildModelMappingObject('mapping', [], openAICompactModelMappings.value)


  const isOpenAIModelRestrictionDisabled = computed(() =>
    input.form.platform === 'openai' && input.openaiPassthroughEnabled.value
  )


  // Computed: current preset mappings based on platform
  const presetMappings = computed(() => input.getPresetMappingsByPlatform(input.form.platform))


  // Model mapping helpers
  const addModelMapping = () => {
    modelMappings.value.push({ from: '', to: '' })
  }


  const addOpenAICompactModelMapping = () => {
    openAICompactModelMappings.value.push({ from: '', to: '' })
  }


  const removeOpenAICompactModelMapping = (index: number) => {
    openAICompactModelMappings.value.splice(index, 1)
  }


  const removeModelMapping = (index: number) => {
    modelMappings.value.splice(index, 1)
  }


  const addPresetMapping = (from: string, to: string) => {
    if (modelMappings.value.some((m) => m.from === from)) {
      input.appStore.showInfo(input.t('admin.accounts.mappingExists', { model: from }))
      return
    }
    modelMappings.value.push({ from, to })
  }


  const addAntigravityModelMapping = () => {
    antigravityModelMappings.value.push({ from: '', to: '' })
  }


  const removeAntigravityModelMapping = (index: number) => {
    antigravityModelMappings.value.splice(index, 1)
  }


  const addAntigravityPresetMapping = (from: string, to: string) => {
    if (antigravityModelMappings.value.some((m) => m.from === from)) {
      input.appStore.showInfo(input.t('admin.accounts.mappingExists', { model: from }))
      return
    }
    antigravityModelMappings.value.push({ from, to })
  }
  return { fetchAntigravityDefaultMappings, hideAccountLongContextBilling, routesByModel, modelMappings, openAICompactModelMappings, modelRestrictionMode, allowedModels, isLoadingBuiltinModels, openAILongContextBillingEnabled, openAILongContextBillingTouched, toggleOpenAILongContextBilling, antigravityModelRestrictionMode, antigravityWhitelistModels, antigravityModelMappings, antigravityPresetMappings, bedrockPresets, getModelMappingKey, getOpenAICompactModelMappingKey, getAntigravityModelMappingKey, buildOpenAICompactModelMapping, isOpenAIModelRestrictionDisabled, presetMappings, addModelMapping, addOpenAICompactModelMapping, removeOpenAICompactModelMapping, removeModelMapping, addPresetMapping, addAntigravityModelMapping, removeAntigravityModelMapping, addAntigravityPresetMapping }
}
