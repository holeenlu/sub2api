<script setup lang="ts">
import CreateApiKeyModelsPanel from './CreateApiKeyModelsPanel.vue'
import CreateApiKeyRetryPoolPanel from './CreateApiKeyRetryPoolPanel.vue'
import CreateApiKeyExplicitCachePanel from './CreateApiKeyExplicitCachePanel.vue'
import CreateApiKeyErrorPolicyPanel from './CreateApiKeyErrorPolicyPanel.vue'
import CreateApiKeyHeadersPanel from './CreateApiKeyHeadersPanel.vue'
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'
import GrokBaseUrlPresets from '@/features/resources/components/GrokBaseUrlPresets.vue'
import CnBaseUrlPresets from '@/features/resources/components/CnBaseUrlPresets.vue'
import { isOfficialGeminiBaseUrl } from '@/features/resources/model/geminiExplicitCache'
import OpenCodeGoProtocolRulesEditor from '@/features/resources/components/OpenCodeGoProtocolRulesEditor.vue'
import { type CnAccountMode, type CnApiProtocol, type CnNativeApiProtocol, type CnProviderPlatform, type HeaderOverrideRow, type OpenCodeGoProtocolRule } from "@/features/resources/model/credentialsBuilder"
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { Props, ModelMapping, CreateResourceForm } from '../model/createEditorTypes'



const props = defineProps<{
  form: Pick<CreateResourceForm, "type" | "platform">
  isMultiProtocolPlatform: UnwrapRef<ComputedRef<boolean>>
  apiProtocol: UnwrapRef<Ref<CnApiProtocol, CnApiProtocol>>
  apiKeyBaseUrl: UnwrapRef<Ref<string, string>>
  apiKeyBaseUrlPlaceholder: UnwrapRef<ComputedRef<string>>
  baseUrlHint: UnwrapRef<ComputedRef<string>>
  isCNPlatform: UnwrapRef<ComputedRef<boolean>>
  isOpenCodeGoPlatform: UnwrapRef<ComputedRef<boolean>>
  cnPresetPlatform: UnwrapRef<ComputedRef<CnProviderPlatform>>
  cnPresetMode: UnwrapRef<ComputedRef<CnAccountMode>>
  onCnPresetSelect: UnwrapRef<(preset: { mode: CnAccountMode; protocol: CnApiProtocol; url: string }) => void>
  cnAdaptiveProtocolOptions: UnwrapRef<ComputedRef<{ value: CnNativeApiProtocol; labelKey: string }[]>>
  adaptiveBaseUrls: UnwrapRef<Ref<{ anthropic: string; chat_completions: string; responses: string }, Record<CnNativeApiProtocol, string> | { anthropic: string; chat_completions: string; responses: string }>>
  cnSupportsNativeResponses: UnwrapRef<ReturnType<typeof createProviderCredentialOptions>["cnSupportsNativeResponses"]>
  currentOpenCodeOrCNMode: UnwrapRef<() => string>
  routesByModel: UnwrapRef<ComputedRef<boolean>>
  openCodeGoProtocolRules: UnwrapRef<Ref<{ pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[], OpenCodeGoProtocolRule[] | { pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[]>>
  apiKeyValue: UnwrapRef<Ref<string, string>>
  apiKeyValuePlaceholder: UnwrapRef<ComputedRef<"sk-proj-..." | "AQ.Ab..." | "xai-..." | "sk-..." | "<api-key>.<secret>" | "ts-..." | "sk-ant-...">>
  apiKeyHint: UnwrapRef<ComputedRef<string>>
  allowRateEdit: NonNullable<Props['allowRateEdit']>
  upstreamBillingAutoProbeEnabled: UnwrapRef<Ref<boolean, boolean>>
  geminiTierAIStudio: UnwrapRef<Ref<"aistudio_free" | "aistudio_paid", "aistudio_free" | "aistudio_paid">>
  isOpenAIModelRestrictionDisabled: UnwrapRef<ComputedRef<boolean>>
  modelRestrictionMode: UnwrapRef<Ref<"whitelist" | "mapping", "whitelist" | "mapping">>
  allowedModels: UnwrapRef<Ref<string[], string[]>>
  modelMappings: UnwrapRef<Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>>
  getModelMappingKey: UnwrapRef<(item: ModelMapping) => string>
  removeModelMapping: UnwrapRef<(index: number) => void>
  addModelMapping: UnwrapRef<() => void>
  presetMappings: UnwrapRef<ComputedRef<{ label: string; from: string; to: string; color: string }[]>>
  addPresetMapping: UnwrapRef<(from: string, to: string) => void>
  poolModeEnabled: UnwrapRef<Ref<boolean, boolean>>
  poolModeRetryCount: UnwrapRef<Ref<number, number>>
  MAX_POOL_MODE_RETRY_COUNT: UnwrapRef<10>
  DEFAULT_POOL_MODE_RETRY_COUNT: UnwrapRef<3>
  poolModeRetryStatusCodesInput: UnwrapRef<Ref<string, string>>
  DEFAULT_POOL_MODE_RETRY_STATUS_CODES: UnwrapRef<number[]>
  explicitCacheUpstreamEnabled: UnwrapRef<Ref<boolean, boolean>>
  explicitCacheUpstreamMaxTTL: UnwrapRef<Ref<string | number, string | number>>
  customErrorCodesEnabled: UnwrapRef<Ref<boolean, boolean>>
  toggleErrorCode: UnwrapRef<(code: number) => void>
  selectedErrorCodes: UnwrapRef<Ref<number[], number[]>>
  customErrorCodeInput: UnwrapRef<Ref<number | null, number | null>>
  addCustomErrorCode: UnwrapRef<() => void>
  removeErrorCode: UnwrapRef<(code: number) => void>
  isHeaderOverrideCapable: UnwrapRef<ReturnType<typeof createProviderCredentialOptions>["isHeaderOverrideCapable"]>
  headerOverrideEnabled: UnwrapRef<Ref<boolean, boolean>>
  headerOverrideRows: UnwrapRef<Ref<{ name: string; value: string }[], HeaderOverrideRow[] | { name: string; value: string }[]>>
}>()


const emit = defineEmits<{
  'update:apiKeyBaseUrl': [value: UnwrapRef<Ref<string, string>>]
  'update:adaptiveBaseUrls': [value: UnwrapRef<Ref<{ anthropic: string; chat_completions: string; responses: string }, Record<CnNativeApiProtocol, string> | { anthropic: string; chat_completions: string; responses: string }>>]
  'update:openCodeGoProtocolRules': [value: UnwrapRef<Ref<{ pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[], OpenCodeGoProtocolRule[] | { pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[]>>]
  'update:apiKeyValue': [value: UnwrapRef<Ref<string, string>>]
  'update:upstreamBillingAutoProbeEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:geminiTierAIStudio': [value: UnwrapRef<Ref<"aistudio_free" | "aistudio_paid", "aistudio_free" | "aistudio_paid">>]
  'update:modelRestrictionMode': [value: UnwrapRef<Ref<"whitelist" | "mapping", "whitelist" | "mapping">>]
  'update:allowedModels': [value: UnwrapRef<Ref<string[], string[]>>]
  'update:poolModeEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:poolModeRetryCount': [value: UnwrapRef<Ref<number, number>>]
  'update:poolModeRetryStatusCodesInput': [value: UnwrapRef<Ref<string, string>>]
  'update:explicitCacheUpstreamEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:explicitCacheUpstreamMaxTTL': [value: UnwrapRef<Ref<string | number, string | number>>]
  'update:customErrorCodesEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:customErrorCodeInput': [value: UnwrapRef<Ref<number | null, number | null>>]
  'update:headerOverrideEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:headerOverrideRows': [value: UnwrapRef<Ref<{ name: string; value: string }[], HeaderOverrideRow[] | { name: string; value: string }[]>>]
}>()


const { t } = useI18n()


const form = computed(() => props.form)


const isMultiProtocolPlatform = computed(() => props.isMultiProtocolPlatform)


const apiProtocol = computed(() => props.apiProtocol)


const apiKeyBaseUrl = usePanelField(() => props.apiKeyBaseUrl, value => emit('update:apiKeyBaseUrl', value))


const apiKeyBaseUrlPlaceholder = computed(() => props.apiKeyBaseUrlPlaceholder)


const baseUrlHint = computed(() => props.baseUrlHint)


const isCNPlatform = computed(() => props.isCNPlatform)


const isOpenCodeGoPlatform = computed(() => props.isOpenCodeGoPlatform)


const cnPresetPlatform = computed(() => props.cnPresetPlatform)


const cnPresetMode = computed(() => props.cnPresetMode)


const onCnPresetSelect = computed(() => props.onCnPresetSelect)


const cnAdaptiveProtocolOptions = computed(() => props.cnAdaptiveProtocolOptions)


const adaptiveBaseUrls = usePanelField(() => props.adaptiveBaseUrls, value => emit('update:adaptiveBaseUrls', value))


const cnSupportsNativeResponses = computed(() => props.cnSupportsNativeResponses)


const currentOpenCodeOrCNMode = computed(() => props.currentOpenCodeOrCNMode)


const routesByModel = computed(() => props.routesByModel)


const openCodeGoProtocolRules = usePanelField(() => props.openCodeGoProtocolRules, value => emit('update:openCodeGoProtocolRules', value))


const apiKeyValue = usePanelField(() => props.apiKeyValue, value => emit('update:apiKeyValue', value))


const apiKeyValuePlaceholder = computed(() => props.apiKeyValuePlaceholder)


const apiKeyHint = computed(() => props.apiKeyHint)


const allowRateEdit = computed(() => props.allowRateEdit)


const upstreamBillingAutoProbeEnabled = usePanelField(() => props.upstreamBillingAutoProbeEnabled, value => emit('update:upstreamBillingAutoProbeEnabled', value))


const geminiTierAIStudio = usePanelField(() => props.geminiTierAIStudio, value => emit('update:geminiTierAIStudio', value))


const isOpenAIModelRestrictionDisabled = computed(() => props.isOpenAIModelRestrictionDisabled)


const modelRestrictionMode = usePanelField(() => props.modelRestrictionMode, value => emit('update:modelRestrictionMode', value))


const allowedModels = usePanelField(() => props.allowedModels, value => emit('update:allowedModels', value))


const modelMappings = computed(() => props.modelMappings)






const getModelMappingKey = computed(() => props.getModelMappingKey)


const removeModelMapping = computed(() => props.removeModelMapping)


const addModelMapping = computed(() => props.addModelMapping)


const presetMappings = computed(() => props.presetMappings)


const addPresetMapping = computed(() => props.addPresetMapping)


const poolModeEnabled = usePanelField(() => props.poolModeEnabled, value => emit('update:poolModeEnabled', value))


const poolModeRetryCount = usePanelField(() => props.poolModeRetryCount, value => emit('update:poolModeRetryCount', value))


const MAX_POOL_MODE_RETRY_COUNT = computed(() => props.MAX_POOL_MODE_RETRY_COUNT)


const DEFAULT_POOL_MODE_RETRY_COUNT = computed(() => props.DEFAULT_POOL_MODE_RETRY_COUNT)


const poolModeRetryStatusCodesInput = usePanelField(() => props.poolModeRetryStatusCodesInput, value => emit('update:poolModeRetryStatusCodesInput', value))


const DEFAULT_POOL_MODE_RETRY_STATUS_CODES = computed(() => props.DEFAULT_POOL_MODE_RETRY_STATUS_CODES)


const explicitCacheUpstreamEnabled = usePanelField(() => props.explicitCacheUpstreamEnabled, value => emit('update:explicitCacheUpstreamEnabled', value))


const explicitCacheUpstreamMaxTTL = usePanelField(() => props.explicitCacheUpstreamMaxTTL, value => emit('update:explicitCacheUpstreamMaxTTL', value))


const customErrorCodesEnabled = usePanelField(() => props.customErrorCodesEnabled, value => emit('update:customErrorCodesEnabled', value))


const toggleErrorCode = computed(() => props.toggleErrorCode)


const selectedErrorCodes = computed(() => props.selectedErrorCodes)


const customErrorCodeInput = usePanelField(() => props.customErrorCodeInput, value => emit('update:customErrorCodeInput', value))


const addCustomErrorCode = computed(() => props.addCustomErrorCode)


const removeErrorCode = computed(() => props.removeErrorCode)


const isHeaderOverrideCapable = computed(() => props.isHeaderOverrideCapable)


const headerOverrideEnabled = usePanelField(() => props.headerOverrideEnabled, value => emit('update:headerOverrideEnabled', value))


const headerOverrideRows = usePanelField(() => props.headerOverrideRows, value => emit('update:headerOverrideRows', value))
</script>
<template>

<div v-if="form.type === 'apikey' && form.platform !== 'antigravity'" class="space-y-4">
        <div v-if="!isMultiProtocolPlatform || apiProtocol !== 'adaptive'">
          <label class="input-label">{{ t('admin.accounts.baseUrl') }}</label>
          <input
            v-model="apiKeyBaseUrl"
            type="text"
            class="input"
            :placeholder="apiKeyBaseUrlPlaceholder"
          />
          <p v-if="baseUrlHint" class="input-hint">{{ baseUrlHint }}</p>
          <GrokBaseUrlPresets
            v-if="form.platform === 'grok'"
            class="mt-2"
            @select="apiKeyBaseUrl = $event"
          />
          <CnBaseUrlPresets
            v-if="isCNPlatform && !isOpenCodeGoPlatform"
            class="mt-2"
            :platform="cnPresetPlatform"
            :mode="cnPresetMode"
            :protocol="apiProtocol"
            :current-url="apiKeyBaseUrl"
            @select="onCnPresetSelect"
          />
        </div>
        <div v-else>
          <label class="input-label">{{ t('admin.accounts.cnProviders.apiProtocol.endpoints') }}</label>
          <div class="mt-2 space-y-3">
            <div v-for="item in cnAdaptiveProtocolOptions" :key="item.value">
              <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
                {{ t(`admin.accounts.cnProviders.apiProtocol.${item.labelKey}`) }}
              </label>
              <input
                v-model="adaptiveBaseUrls[item.value]"
                type="text"
                class="input"
                :data-testid="`cn-adaptive-base-url-${item.value}`"
              />
            </div>
          </div>
          <p v-if="!cnSupportsNativeResponses(form.platform, currentOpenCodeOrCNMode())" class="input-hint">
            {{ t('admin.accounts.cnProviders.apiProtocol.responsesFallbackDesc') }}
          </p>
        </div>
        <OpenCodeGoProtocolRulesEditor
          v-if="routesByModel && apiProtocol === 'adaptive'"
          v-model:rows="openCodeGoProtocolRules"
          :platform="form.platform"
          :plan="currentOpenCodeOrCNMode()"
        />
        <div>
          <label class="input-label">{{ t('admin.accounts.apiKeyRequired') }}</label>
          <input
            v-model="apiKeyValue"
            type="password"
            required
            class="input font-mono"
            :placeholder="apiKeyValuePlaceholder"
          />
          <p v-if="apiKeyHint" class="input-hint">{{ apiKeyHint }}</p>
        </div>

        <!-- 上游倍率自动探测：全部 API-key 平台可用（所在区块已限定 apikey 类型） -->
        <div v-if="allowRateEdit"
          class="flex items-center justify-between gap-4 border-t border-gray-200 pt-4 dark:border-dark-600"
        >
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.upstreamBilling.autoProbe') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.upstreamBilling.autoProbeHint') }}
            </p>
          </div>
          <Toggle
            v-model="upstreamBillingAutoProbeEnabled"
            data-testid="upstream-billing-auto-probe"
            :aria-label="t('admin.accounts.upstreamBilling.autoProbe')"
          />
        </div>

        <!-- Gemini API Key tier selection -->
        <div v-if="form.platform === 'gemini' && isOfficialGeminiBaseUrl(apiKeyBaseUrl)">
          <label class="input-label">{{ t('admin.accounts.gemini.tier.label') }}</label>
          <select v-model="geminiTierAIStudio" class="input" data-testid="gemini-apikey-tier">
            <option value="aistudio_free">{{ t('admin.accounts.gemini.tier.aiStudio.free') }}</option>
            <option value="aistudio_paid">{{ t('admin.accounts.gemini.tier.aiStudio.paid') }}</option>
          </select>
          <p class="input-hint">{{ t('admin.accounts.gemini.tier.aiStudioHint') }}</p>
        </div>

        <!-- Model Restriction Section (Antigravity 已在上层条件排除) -->
        <CreateApiKeyModelsPanel
  :isOpenAIModelRestrictionDisabled="isOpenAIModelRestrictionDisabled"
  :modelRestrictionMode="modelRestrictionMode"
  :allowedModels="allowedModels"
  :modelMappings="modelMappings"
  :form="form"
  :getModelMappingKey="getModelMappingKey"
  :removeModelMapping="removeModelMapping"
  :addModelMapping="addModelMapping"
  :presetMappings="presetMappings"
  :addPresetMapping="addPresetMapping"
  @update:modelRestrictionMode="modelRestrictionMode = $event"
  @update:allowedModels="allowedModels = $event"
/>

        <!-- Pool Mode Section -->
        <CreateApiKeyRetryPoolPanel
  :poolModeEnabled="poolModeEnabled"
  :poolModeRetryCount="poolModeRetryCount"
  :MAX_POOL_MODE_RETRY_COUNT="MAX_POOL_MODE_RETRY_COUNT"
  :DEFAULT_POOL_MODE_RETRY_COUNT="DEFAULT_POOL_MODE_RETRY_COUNT"
  :poolModeRetryStatusCodesInput="poolModeRetryStatusCodesInput"
  :DEFAULT_POOL_MODE_RETRY_STATUS_CODES="DEFAULT_POOL_MODE_RETRY_STATUS_CODES"
  @update:poolModeEnabled="poolModeEnabled = $event"
  @update:poolModeRetryCount="poolModeRetryCount = $event"
  @update:poolModeRetryStatusCodesInput="poolModeRetryStatusCodesInput = $event"
/>

        <!-- Gemini 显式缓存上游开关：仅自定义地址的 Gemini API Key 账号 -->
        <CreateApiKeyExplicitCachePanel
  :form="form"
  :apiKeyBaseUrl="apiKeyBaseUrl"
  :explicitCacheUpstreamEnabled="explicitCacheUpstreamEnabled"
  :explicitCacheUpstreamMaxTTL="explicitCacheUpstreamMaxTTL"
  @update:explicitCacheUpstreamEnabled="explicitCacheUpstreamEnabled = $event"
  @update:explicitCacheUpstreamMaxTTL="explicitCacheUpstreamMaxTTL = $event"
/>

        <!-- Custom Error Codes Section -->
        <CreateApiKeyErrorPolicyPanel
  :customErrorCodesEnabled="customErrorCodesEnabled"
  :toggleErrorCode="toggleErrorCode"
  :selectedErrorCodes="selectedErrorCodes"
  :customErrorCodeInput="customErrorCodeInput"
  :addCustomErrorCode="addCustomErrorCode"
  :removeErrorCode="removeErrorCode"
  @update:customErrorCodesEnabled="customErrorCodesEnabled = $event"
  @update:customErrorCodeInput="customErrorCodeInput = $event"
/>

        <!-- Header Override Section (eligible API-key platforms) -->
        <CreateApiKeyHeadersPanel
  :isHeaderOverrideCapable="isHeaderOverrideCapable"
  :form="form"
  :headerOverrideEnabled="headerOverrideEnabled"
  :headerOverrideRows="headerOverrideRows"
  @update:headerOverrideEnabled="headerOverrideEnabled = $event"
  @update:headerOverrideRows="headerOverrideRows = $event"
/>

      </div>

</template>
