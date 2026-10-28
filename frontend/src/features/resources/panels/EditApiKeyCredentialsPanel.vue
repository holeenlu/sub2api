<script setup lang="ts">
import EditApiKeyModelsPanel from './EditApiKeyModelsPanel.vue'
import EditApiKeyRetryPoolPanel from './EditApiKeyRetryPoolPanel.vue'
import EditApiKeyExplicitCachePanel from './EditApiKeyExplicitCachePanel.vue'
import EditApiKeyErrorPolicyPanel from './EditApiKeyErrorPolicyPanel.vue'
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import HelpTooltip from '@/shared/ui/HelpTooltip.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import GrokBaseUrlPresets from '@/features/resources/components/GrokBaseUrlPresets.vue'
import CnBaseUrlPresets from '@/features/resources/components/CnBaseUrlPresets.vue'
import OpenCodeGoProtocolRulesEditor from '@/features/resources/components/OpenCodeGoProtocolRulesEditor.vue'
import { providerModeLabel, isCNProviderPlatform, type CnAccountMode, type CnApiProtocol, type CnNativeApiProtocol, type CnProviderPlatform, type OpenCodeAccountMode, type OpenCodeGoProtocolRule } from "@/features/resources/model/credentialsBuilder"
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { Props, ModelMapping } from '../model/editEditorTypes'



const props = defineProps<{
  account: NonNullable<Props['account']>
  isCNApiKeyAccount: UnwrapRef<ComputedRef<boolean>>
  editApiProtocol: UnwrapRef<Ref<CnApiProtocol, CnApiProtocol>>
  allowTransportEdit: NonNullable<Props['allowTransportEdit']>
  editBaseUrl: UnwrapRef<Ref<string, string>>
  baseUrlHint: UnwrapRef<ComputedRef<string>>
  cnPresetPlatform: UnwrapRef<ComputedRef<CnProviderPlatform>>
  cnPresetMode: UnwrapRef<ComputedRef<CnAccountMode>>
  onCnPresetSelect: UnwrapRef<(preset: { mode: CnAccountMode; protocol: CnApiProtocol; url: string }) => void>
  editAdaptiveProtocolOptions: UnwrapRef<ComputedRef<{ value: CnNativeApiProtocol; labelKey: string }[]>>
  editAdaptiveBaseUrls: UnwrapRef<Ref<{ chat_completions: string; anthropic: string; responses: string }, Record<CnNativeApiProtocol, string> | { chat_completions: string; anthropic: string; responses: string }>>
  cnSupportsNativeResponses: UnwrapRef<ReturnType<typeof createProviderCredentialOptions>["cnSupportsNativeResponses"]>
  currentOpenCodeOrCNMode: UnwrapRef<() => string>
  editOpenCodeAccountMode: UnwrapRef<Ref<OpenCodeAccountMode, OpenCodeAccountMode>>
  isGenericMultiProtocolAccount: UnwrapRef<ComputedRef<boolean>>
  genericAccountModes: UnwrapRef<ComputedRef<string[]>>
  editAccountMode: UnwrapRef<Ref<string, string>>
  cnAccountModeOptions: UnwrapRef<ComputedRef<{ value: CnAccountMode; labelKey: "payg" | "coding" }[]>>
  cnProtocolOptions: UnwrapRef<ComputedRef<{ value: CnApiProtocol; labelKey: string }[]>>
  cnProtocolDescKey: UnwrapRef<ComputedRef<string>>
  providerRoutesByModel: UnwrapRef<ReturnType<typeof createProviderCredentialOptions>["providerRoutesByModel"]>
  editOpenCodeGoProtocolRules: UnwrapRef<Ref<{ pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[], OpenCodeGoProtocolRule[] | { pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[]>>
  editZhipuOrganization: UnwrapRef<Ref<string, string>>
  editZhipuProject: UnwrapRef<Ref<string, string>>
  allowAuthorize: NonNullable<Props['allowAuthorize']>
  editApiKey: UnwrapRef<Ref<string, string>>
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
}>()


const emit = defineEmits<{
  'update:editBaseUrl': [value: UnwrapRef<Ref<string, string>>]
  'update:editAdaptiveBaseUrls': [value: UnwrapRef<Ref<{ chat_completions: string; anthropic: string; responses: string }, Record<CnNativeApiProtocol, string> | { chat_completions: string; anthropic: string; responses: string }>>]
  'update:editOpenCodeAccountMode': [value: UnwrapRef<Ref<OpenCodeAccountMode, OpenCodeAccountMode>>]
  'update:editAccountMode': [value: UnwrapRef<Ref<string, string>>]
  'update:editApiProtocol': [value: UnwrapRef<Ref<CnApiProtocol, CnApiProtocol>>]
  'update:editOpenCodeGoProtocolRules': [value: UnwrapRef<Ref<{ pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[], OpenCodeGoProtocolRule[] | { pattern: string; protocol: CnNativeApiProtocol; extraProtocols?: CnNativeApiProtocol[] | undefined }[]>>]
  'update:editZhipuOrganization': [value: UnwrapRef<Ref<string, string>>]
  'update:editZhipuProject': [value: UnwrapRef<Ref<string, string>>]
  'update:editApiKey': [value: UnwrapRef<Ref<string, string>>]
  'update:modelRestrictionMode': [value: UnwrapRef<Ref<"whitelist" | "mapping", "whitelist" | "mapping">>]
  'update:allowedModels': [value: UnwrapRef<Ref<string[], string[]>>]
  'update:poolModeEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:poolModeRetryCount': [value: UnwrapRef<Ref<number, number>>]
  'update:poolModeRetryStatusCodesInput': [value: UnwrapRef<Ref<string, string>>]
  'update:explicitCacheUpstreamEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:explicitCacheUpstreamMaxTTL': [value: UnwrapRef<Ref<string | number, string | number>>]
  'update:customErrorCodesEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:customErrorCodeInput': [value: UnwrapRef<Ref<number | null, number | null>>]
}>()


const { t } = useI18n()


const account = computed(() => props.account)


const isCNApiKeyAccount = computed(() => props.isCNApiKeyAccount)


const editApiProtocol = usePanelField(() => props.editApiProtocol, value => emit('update:editApiProtocol', value))


const allowTransportEdit = computed(() => props.allowTransportEdit)


const editBaseUrl = usePanelField(() => props.editBaseUrl, value => emit('update:editBaseUrl', value))


const baseUrlHint = computed(() => props.baseUrlHint)


const cnPresetPlatform = computed(() => props.cnPresetPlatform)


const cnPresetMode = computed(() => props.cnPresetMode)


const onCnPresetSelect = computed(() => props.onCnPresetSelect)


const editAdaptiveProtocolOptions = computed(() => props.editAdaptiveProtocolOptions)


const editAdaptiveBaseUrls = usePanelField(() => props.editAdaptiveBaseUrls, value => emit('update:editAdaptiveBaseUrls', value))


const cnSupportsNativeResponses = computed(() => props.cnSupportsNativeResponses)


const currentOpenCodeOrCNMode = computed(() => props.currentOpenCodeOrCNMode)


const editOpenCodeAccountMode = usePanelField(() => props.editOpenCodeAccountMode, value => emit('update:editOpenCodeAccountMode', value))


const isGenericMultiProtocolAccount = computed(() => props.isGenericMultiProtocolAccount)


const genericAccountModes = computed(() => props.genericAccountModes)


const editAccountMode = usePanelField(() => props.editAccountMode, value => emit('update:editAccountMode', value))


const cnAccountModeOptions = computed(() => props.cnAccountModeOptions)


const cnProtocolOptions = computed(() => props.cnProtocolOptions)


const cnProtocolDescKey = computed(() => props.cnProtocolDescKey)


const providerRoutesByModel = computed(() => props.providerRoutesByModel)


const editOpenCodeGoProtocolRules = usePanelField(() => props.editOpenCodeGoProtocolRules, value => emit('update:editOpenCodeGoProtocolRules', value))


const editZhipuOrganization = usePanelField(() => props.editZhipuOrganization, value => emit('update:editZhipuOrganization', value))


const editZhipuProject = usePanelField(() => props.editZhipuProject, value => emit('update:editZhipuProject', value))


const allowAuthorize = computed(() => props.allowAuthorize)


const editApiKey = usePanelField(() => props.editApiKey, value => emit('update:editApiKey', value))


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
</script>
<template>

<template v-if="account">
<div v-if="account.type === 'apikey'" class="space-y-4">
        <div v-if="!isCNApiKeyAccount || editApiProtocol !== 'adaptive'">
          <label class="input-label">{{ t('admin.accounts.baseUrl') }}</label>
          <input
 :disabled="!allowTransportEdit"            v-model="editBaseUrl"
            type="text"
            class="input"
            :placeholder="
              account.platform === 'openai'
                ? 'https://api.openai.com'
                : account.platform === 'gemini'
                  ? 'https://generativelanguage.googleapis.com'
                  : account.platform === 'antigravity'
                    ? 'https://cloudcode-pa.googleapis.com'
                    : account.platform === 'grok'
                      ? 'https://api.x.ai/v1'
                      : 'https://api.anthropic.com'
            "
          />
          <p v-if="baseUrlHint" class="input-hint">{{ baseUrlHint }}</p>
          <GrokBaseUrlPresets
            v-if="account.platform === 'grok'"
            class="mt-2"
            @select="editBaseUrl = $event"
          />
          <CnBaseUrlPresets
            v-if="isCNApiKeyAccount && isCNProviderPlatform(account.platform)"
            class="mt-2"
            :platform="cnPresetPlatform"
            :mode="cnPresetMode"
            :protocol="editApiProtocol"
            :current-url="editBaseUrl"
            @select="onCnPresetSelect"
          />
        </div>
        <div v-else>
          <label class="input-label">{{ t('admin.accounts.cnProviders.apiProtocol.endpoints') }}</label>
          <div class="mt-2 space-y-3">
            <div v-for="item in editAdaptiveProtocolOptions" :key="item.value">
              <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
                {{ t(`admin.accounts.cnProviders.apiProtocol.${item.labelKey}`) }}
              </label>
              <input :disabled="!allowTransportEdit" v-model="editAdaptiveBaseUrls[item.value]" type="text" class="input" />
            </div>
          </div>
          <p v-if="!cnSupportsNativeResponses(account.platform, currentOpenCodeOrCNMode())" class="input-hint">
            {{ t('admin.accounts.cnProviders.apiProtocol.responsesFallbackDesc') }}
          </p>
        </div>
        <!-- OpenCode Zen vs GO -->
        <div v-if="isCNApiKeyAccount && account.platform === 'opencode_go'">
          <label class="input-label">{{ t('admin.accounts.cnProviders.accountMode.title') }}</label>
          <div class="mt-2 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <button
              type="button"
              @click="editOpenCodeAccountMode = 'zen'"
              :class="[
                'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
                editOpenCodeAccountMode === 'zen'
                  ? 'border-amber-500 bg-amber-50 dark:bg-amber-900/20'
                  : 'border-gray-200 hover:border-gray-400 dark:border-dark-600 dark:hover:border-gray-600'
              ]"
            >
              <div
                :class="[
                  'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                  editOpenCodeAccountMode === 'zen' ? 'bg-amber-500 text-white' : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
                ]"
              >
                <Icon name="creditCard" size="sm" />
              </div>
              <div>
                <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.opencodeGo.accountMode.zen') }}</span>
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.accountMode.zenDesc') }}</span>
              </div>
            </button>
            <button
              type="button"
              @click="editOpenCodeAccountMode = 'go'"
              :class="[
                'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
                editOpenCodeAccountMode === 'go'
                  ? 'border-amber-500 bg-amber-50 dark:bg-amber-900/20'
                  : 'border-gray-200 hover:border-gray-400 dark:border-dark-600 dark:hover:border-gray-600'
              ]"
            >
              <div
                :class="[
                  'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                  editOpenCodeAccountMode === 'go' ? 'bg-amber-500 text-white' : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
                ]"
              >
                <Icon name="bolt" size="sm" />
              </div>
              <div>
                <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.opencodeGo.accountMode.go') }}</span>
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.accountMode.goDesc') }}</span>
              </div>
            </button>
          </div>
        </div>
        <!-- Account Mode Selection (providers using the generic form) -->
        <div v-if="isGenericMultiProtocolAccount && genericAccountModes.length > 1" data-testid="edit-generic-account-mode">
          <label class="input-label">{{ t('admin.accounts.cnProviders.accountMode.title') }}</label>
          <div class="mt-2 flex flex-wrap gap-2">
            <button
              v-for="mode in genericAccountModes"
              :key="mode"
              type="button"
              :class="[
                'rounded-lg border-2 px-3 py-1.5 text-xs transition-all',
                editAccountMode === mode
                  ? 'border-primary-500 bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
                  : 'border-gray-200 text-gray-700 hover:border-gray-400 dark:border-dark-600 dark:text-gray-300 dark:hover:border-gray-600'
              ]"
              @click="editAccountMode = mode"
            >
              {{ providerModeLabel(mode, t) }}
            </button>
          </div>
        </div>
        <!-- Account Mode Selection (CN providers) -->
        <div v-if="isCNApiKeyAccount && isCNProviderPlatform(account.platform)">
          <label class="input-label">{{ t('admin.accounts.cnProviders.accountMode.title') }}</label>
          <div class="mt-2 flex flex-wrap gap-2">
            <button
              v-for="opt in cnAccountModeOptions"
              :key="opt.value"
              type="button"
              :class="[
                'rounded-lg border-2 px-3 py-1.5 text-xs transition-all',
                editAccountMode === opt.value
                  ? 'border-primary-500 bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
                  : 'border-gray-200 text-gray-700 hover:border-gray-400 dark:border-dark-600 dark:text-gray-300 dark:hover:border-gray-600'
              ]"
              @click="editAccountMode = opt.value"
            >
              {{ t(`admin.accounts.cnProviders.accountMode.${opt.labelKey}`) }}
            </button>
          </div>
          <p class="input-hint">{{ t(`admin.accounts.cnProviders.accountMode.${editAccountMode}Desc`) }}</p>
        </div>
        <!-- API Protocol Selection (CN providers / OpenCode) -->
        <div v-if="isCNApiKeyAccount">
          <label class="input-label">{{ t('admin.accounts.cnProviders.apiProtocol.title') }}</label>
          <div class="mt-2 flex flex-wrap gap-2">
            <button
              v-for="opt in cnProtocolOptions"
              :key="opt.value"
              type="button"
              :class="[
                'rounded-lg border-2 px-3 py-1.5 text-xs transition-all',
                editApiProtocol === opt.value
                  ? 'border-primary-500 bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
                  : 'border-gray-200 text-gray-700 hover:border-gray-400 dark:border-dark-600 dark:text-gray-300 dark:hover:border-gray-600'
              ]"
              @click="editApiProtocol = opt.value"
            >
              {{ t(`admin.accounts.cnProviders.apiProtocol.${opt.labelKey}`) }}
            </button>
          </div>
          <p class="input-hint">{{ t(`admin.accounts.cnProviders.apiProtocol.${cnProtocolDescKey}Desc`) }}</p>
        </div>
        <OpenCodeGoProtocolRulesEditor
          v-if="isCNApiKeyAccount && providerRoutesByModel(account.platform) && editApiProtocol === 'adaptive'"
          v-model:rows="editOpenCodeGoProtocolRules"
          :platform="account.platform"
          :plan="currentOpenCodeOrCNMode()"
        />
        <!-- Zhipu 团队版 Coding Plan：组织/项目 ID（可选，填写后用量查询走团队版端点） -->
        <div v-if="account.platform === 'zhipu' && editAccountMode === 'coding'">
          <div class="flex items-center">
            <label class="input-label">{{ t('admin.accounts.cnProviders.zhipuTeam.title') }}</label>
            <HelpTooltip trigger="click" width-class="w-80">
              <p class="mb-1 font-medium">{{ t('admin.accounts.cnProviders.zhipuTeam.help.title') }}</p>
              <ol class="list-decimal space-y-1 pl-4">
                <li>{{ t('admin.accounts.cnProviders.zhipuTeam.help.step1') }}</li>
                <li>{{ t('admin.accounts.cnProviders.zhipuTeam.help.step2') }}</li>
                <li>{{ t('admin.accounts.cnProviders.zhipuTeam.help.step3') }}</li>
                <li>{{ t('admin.accounts.cnProviders.zhipuTeam.help.step4') }}</li>
              </ol>
              <p class="mt-2 break-all rounded bg-black/20 p-1.5 font-mono text-[11px] leading-relaxed">
                {{ t('admin.accounts.cnProviders.zhipuTeam.help.example') }}
              </p>
            </HelpTooltip>
          </div>
          <div class="mt-2 grid gap-4 sm:grid-cols-2">
            <div>
              <label class="input-label">{{ t('admin.accounts.cnProviders.zhipuTeam.organization') }}</label>
              <input v-model="editZhipuOrganization" type="text" class="input" :placeholder="t('admin.accounts.cnProviders.zhipuTeam.organizationPlaceholder')" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.accounts.cnProviders.zhipuTeam.project') }}</label>
              <input v-model="editZhipuProject" type="text" class="input" :placeholder="t('admin.accounts.cnProviders.zhipuTeam.projectPlaceholder')" />
            </div>
          </div>
          <p class="input-hint mt-2">{{ t('admin.accounts.cnProviders.zhipuTeam.hint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.apiKey') }}</label>
          <input
 :disabled="!allowAuthorize"            v-model="editApiKey"
            type="password"
            class="input font-mono"
            autocomplete="new-password"
            data-1p-ignore
            data-lpignore="true"
            data-bwignore="true"
            :placeholder="
              account.platform === 'openai'
                ? 'sk-proj-...'
                : account.platform === 'gemini'
                  ? 'AQ.Ab...'
                  : account.platform === 'antigravity'
                    ? 'sk-...'
                    : account.platform === 'grok'
                      ? 'xai-...'
                      : 'sk-ant-...'
            "
          />
          <p class="input-hint">{{ t('admin.accounts.leaveEmptyToKeep') }}</p>
        </div>

        <!-- Model Restriction Section (不适用于 Antigravity) -->
        <EditApiKeyModelsPanel
  :account="account"
  :isOpenAIModelRestrictionDisabled="isOpenAIModelRestrictionDisabled"
  :modelRestrictionMode="modelRestrictionMode"
  :allowedModels="allowedModels"
  :modelMappings="modelMappings"
  :getModelMappingKey="getModelMappingKey"
  :removeModelMapping="removeModelMapping"
  :addModelMapping="addModelMapping"
  :presetMappings="presetMappings"
  :addPresetMapping="addPresetMapping"
  @update:modelRestrictionMode="modelRestrictionMode = $event"
  @update:allowedModels="allowedModels = $event"
/>

        <!-- Pool Mode Section -->
        <EditApiKeyRetryPoolPanel
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
        <EditApiKeyExplicitCachePanel
  :account="account"
  :editBaseUrl="editBaseUrl"
  :explicitCacheUpstreamEnabled="explicitCacheUpstreamEnabled"
  :explicitCacheUpstreamMaxTTL="explicitCacheUpstreamMaxTTL"
  @update:explicitCacheUpstreamEnabled="explicitCacheUpstreamEnabled = $event"
  @update:explicitCacheUpstreamMaxTTL="explicitCacheUpstreamMaxTTL = $event"
/>

        <!-- Custom Error Codes Section -->
        <EditApiKeyErrorPolicyPanel
  :customErrorCodesEnabled="customErrorCodesEnabled"
  :toggleErrorCode="toggleErrorCode"
  :selectedErrorCodes="selectedErrorCodes"
  :customErrorCodeInput="customErrorCodeInput"
  :addCustomErrorCode="addCustomErrorCode"
  :removeErrorCode="removeErrorCode"
  @update:customErrorCodesEnabled="customErrorCodesEnabled = $event"
  @update:customErrorCodeInput="customErrorCodeInput = $event"
/>

      </div>
</template>

</template>
