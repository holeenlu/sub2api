<script setup lang="ts">
import ResourceIdentityPanel from '../panels/ResourceIdentityPanel.vue'
import { createIdentityDraft } from '../model/identityDraft'

import { getAccountExpiryTimestamp } from '@/features/resources/model/accountExpiry'
import { VERTEX_LOCATION_OPTIONS } from '@/features/resources/model/accountConstants'
import CreateProviderIdentityPanel from '../panels/CreateProviderIdentityPanel.vue'
import CreateAnthropicCredentialKindPanel from '../panels/CreateAnthropicCredentialKindPanel.vue'
import CreateOpenAICredentialKindPanel from '../panels/CreateOpenAICredentialKindPanel.vue'
import CreateGrokCredentialKindPanel from '../panels/CreateGrokCredentialKindPanel.vue'
import CreateOpenCodeModePanel from '../panels/CreateOpenCodeModePanel.vue'
import CreateProviderAccountModePanel from '../panels/CreateProviderAccountModePanel.vue'
import CreateProviderAccountMode2Panel from '../panels/CreateProviderAccountMode2Panel.vue'
import CreateProviderTransportPanel from '../panels/CreateProviderTransportPanel.vue'
import CreateZhipuOrganizationPanel from '../panels/CreateZhipuOrganizationPanel.vue'
import CreateGeminiAuthorizationOptionsPanel from '../panels/CreateGeminiAuthorizationOptionsPanel.vue'
import CreateAntigravityCredentialKindPanel from '../panels/CreateAntigravityCredentialKindPanel.vue'
import CreateUpstreamCredentialsPanel from '../panels/CreateUpstreamCredentialsPanel.vue'
import CreateAntigravityModelMappingPanel from '../panels/CreateAntigravityModelMappingPanel.vue'
import CreateAnthropicAuthorizationMethodPanel from '../panels/CreateAnthropicAuthorizationMethodPanel.vue'
import CreateApiKeyCredentialsPanel from '../panels/CreateApiKeyCredentialsPanel.vue'
import CreateBedrockCredentialsPanel from '../panels/CreateBedrockCredentialsPanel.vue'
import CreateQuotaLimitsPanel from '../panels/CreateQuotaLimitsPanel.vue'
import CreateGrokUpstreamPanel from '../panels/CreateGrokUpstreamPanel.vue'
import CreateRequestHeadersPanel from '../panels/CreateRequestHeadersPanel.vue'
import CreateOAuthModelMappingPanel from '../panels/CreateOAuthModelMappingPanel.vue'
import CreateTemporaryRestrictionsPanel from '../panels/CreateTemporaryRestrictionsPanel.vue'
import CreateWarmupPolicyPanel from '../panels/CreateWarmupPolicyPanel.vue'
import CreateAnthropicRuntimeLimitsPanel from '../panels/CreateAnthropicRuntimeLimitsPanel.vue'
import CreatePassthroughPanel from '../panels/CreatePassthroughPanel.vue'
import CreateNamespaceToolsPanel from '../panels/CreateNamespaceToolsPanel.vue'
import CreateWebSocketTransportPanel from '../panels/CreateWebSocketTransportPanel.vue'
import CreatePassthrough2Panel from '../panels/CreatePassthrough2Panel.vue'
import CreateWebSearchPanel from '../panels/CreateWebSearchPanel.vue'
import CreateLongContextBillingPanel from '../panels/CreateLongContextBillingPanel.vue'
import CreateCodexFingerprintPanel from '../panels/CreateCodexFingerprintPanel.vue'
import CreateCompactCapabilityPanel from '../panels/CreateCompactCapabilityPanel.vue'
import CreateResponsesCapabilityPanel from '../panels/CreateResponsesCapabilityPanel.vue'
import CreateImageResponseFormatPanel from '../panels/CreateImageResponseFormatPanel.vue'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { useResourceEditorServices } from '@/features/resources/services'
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import accountsAPI from '@/features/resources/accounts'
import type { AccountPlatform, AccountType } from '@/features/resources/api/platformTypes';
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import UpstreamRequestIdHeaderField from '@/features/resources/components/UpstreamRequestIdHeaderField.vue'
import AccountTelemetryPanel from '@/features/resources/components/AccountTelemetryPanel.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import ProxySelector from '@/features/resources/components/ResourceProxySelector.vue'
import GroupSelector from '@/features/resources/components/ResourceGroupSelector.vue'
import { getBrowserTimeZone } from '@/shared/lib/format'
import { OPENAI_WS_MODE_OFF } from '@/features/resources/model/openaiWsMode'
import OAuthAuthorizationFlow from '@/features/resources/components/OAuthAuthorizationFlow.vue'
import { useCreateModelsDraft } from '../model/createModels'
import { useCreateAuthorizationDraft } from '../model/createAuthorization'
import { useCreateTelemetryDraft } from '../model/createTelemetry'
import { useCreateCredentialsDraft } from '../model/createCredentials'
import { useCreateSubmissionDraft } from '../model/createSubmission'
import { useCreateProviderDraft } from '../model/createProvider'
import { useCreateIdentityDraft } from '../model/createIdentity'
import { useCreateLimitsDraft } from '../model/createLimits'
import { useCreateRestrictionsDraft } from '../model/createRestrictions'
import { useCreateTransportDraft } from '../model/createTransport'
import { useCreateAssociationDraft } from '../model/createAssociation'
import { useCreateHydrationDraft } from '../model/createHydration'
import { useCreateOpenAIAuthorizationDraft } from '../model/createOpenAIAuthorization'
import { useCreateGrokAuthorizationDraft } from '../model/createGrokAuthorization'
import { useCreateGoogleAuthorizationDraft } from '../model/createGoogleAuthorization'
import { useCreateAnthropicAuthorizationDraft } from '../model/createAnthropicAuthorization'
import type { Props } from '../model/createEditorTypes'


const { cloneOpenCodeGoProtocolRules, cnSupportsNativeResponses, defaultCNAdaptiveBaseUrls, defaultCNBaseUrl, defaultOpenCodeProtocolRules, defaultProviderProtocolRules, isHeaderOverrideCapable, isMultiProtocolApiKeyPlatform, providerAccountModes, providerNativeProtocols, providerRoutesByModel, resolveProviderAccountMode } = createProviderCredentialOptions(useResourceEditorServices().catalog)


const resourceServices = useResourceEditorServices()


const getPresetMappingsByPlatform = useResourceEditorServices().modelPresets


const managedModelCatalog = resourceServices.managedModelCatalog


const listPlatforms = resourceServices.catalog.listPlatforms


const { t } = useI18n()


const browserTimeZone = getBrowserTimeZone()


const props = withDefaults(defineProps<Props>(), { allowRateEdit: true, allowSystemSettings: true })


const resourceTasks = useResourceTaskScope(() => props.show)


const emit = defineEmits<{
  close: []
  created: []
}>()


const appStore = useResourceEditorServices().notices


const authStore = useResourceEditorServices().access


const form = reactive({
  ...createIdentityDraft(),
  platform: 'anthropic' as AccountPlatform,
  type: 'oauth' as AccountType, // Will be 'oauth', 'setup-token', or 'apikey'
  credentials: {} as Record<string, unknown>,
  proxy_id: null as number | null,
  concurrency: 10,
  load_factor: null as number | null,
  priority: 1,
  rate_multiplier: 1,
  group_ids: [] as number[],
  expires_at: null as number | null
})


const { fetchAntigravityDefaultMappings, hideAccountLongContextBilling, routesByModel, modelMappings, openAICompactModelMappings, modelRestrictionMode, allowedModels, isLoadingBuiltinModels, openAILongContextBillingEnabled, openAILongContextBillingTouched, toggleOpenAILongContextBilling, antigravityModelRestrictionMode, antigravityWhitelistModels, antigravityModelMappings, antigravityPresetMappings, bedrockPresets, getModelMappingKey, getOpenAICompactModelMappingKey, getAntigravityModelMappingKey, buildOpenAICompactModelMapping, isOpenAIModelRestrictionDisabled, presetMappings, addModelMapping, addOpenAICompactModelMapping, removeOpenAICompactModelMapping, removeModelMapping, addPresetMapping, addAntigravityModelMapping, removeAntigravityModelMapping, addAntigravityPresetMapping } = useCreateModelsDraft({
  get form() { return form },
  get props() { return props },
  get providerRoutesByModel() { return providerRoutesByModel },
  get getPresetMappingsByPlatform() { return getPresetMappingsByPlatform },
  get openaiPassthroughEnabled() { return openaiPassthroughEnabled },
  get appStore() { return appStore },
  get t() { return t }
})


const { oauthStepTitle, oauth, openaiOAuth, geminiOAuth, antigravityOAuth, grokOAuth, currentAuthUrl, currentSessionId, currentOAuthLoading, currentOAuthError, oauthFlowRef, grokOAuthCustomBaseUrlEnabled, grokOAuthBaseUrl, openaiOAuthResponsesWebSocketV2Mode, geminiOAuthType, isOAuthFlow, isGrokSSOInputMethod, isManualInputMethod, canExchangeCode, handleSelectGeminiOAuthType, goBackToBasicInfo, handleGenerateUrl, handleValidateRefreshToken, handleValidateSessionToken, createAccountAndFinish, handleExchangeCode } = useCreateAuthorizationDraft({
  get resourceTasks() { return resourceTasks },
  get form() { return form },
  get t() { return t },
  get antigravityAccountType() { return antigravityAccountType },
  get accountCategory() { return accountCategory },
  get addMethod() { return addMethod },
  get geminiAIStudioOAuthEnabled() { return geminiAIStudioOAuthEnabled },
  get appStore() { return appStore },
  get step() { return step },
  get geminiSelectedTier() { return geminiSelectedTier },
  get handleOpenAIValidateRT() { return handleOpenAIValidateRT },
  get handleAntigravityValidateRT() { return handleAntigravityValidateRT },
  get handleGrokValidateRT() { return handleGrokValidateRT },
  get applyTempUnschedConfig() { return applyTempUnschedConfig },
  get withAccountExtra() { return withAccountExtra },
  get editQuotaLimit() { return editQuotaLimit },
  get editQuotaDailyLimit() { return editQuotaDailyLimit },
  get editQuotaWeeklyLimit() { return editQuotaWeeklyLimit },
  get editDailyResetMode() { return editDailyResetMode },
  get editDailyResetHour() { return editDailyResetHour },
  get editWeeklyResetMode() { return editWeeklyResetMode },
  get editWeeklyResetDay() { return editWeeklyResetDay },
  get editWeeklyResetHour() { return editWeeklyResetHour },
  get editResetTimezone() { return editResetTimezone },
  get writeQuotaNotifyToExtra() { return writeQuotaNotifyToExtra },
  get applyOpenAIEndpointCapabilities() { return applyOpenAIEndpointCapabilities },
  get buildOpenAICompactModelMapping() { return buildOpenAICompactModelMapping },
  get apiKeyBaseUrl() { return apiKeyBaseUrl },
  get modelRestrictionMode() { return modelRestrictionMode },
  get allowedModels() { return allowedModels },
  get modelMappings() { return modelMappings },
  get doCreateAccount() { return doCreateAccount },
  get props() { return props },
  get upstreamBillingAutoProbeEnabled() { return upstreamBillingAutoProbeEnabled },
  get autoPauseOnExpired() { return autoPauseOnExpired },
  get handleAnthropicSetupToken() { return handleAnthropicSetupToken },
  get handleOpenAIExchange() { return handleOpenAIExchange },
  get handleGeminiExchange() { return handleGeminiExchange },
  get handleAntigravityExchange() { return handleAntigravityExchange },
  get handleGrokExchange() { return handleGrokExchange },
  get handleAnthropicExchange() { return handleAnthropicExchange }
})


const { upstreamRequestIdHeader, withAccountExtra, upstreamBillingAutoProbeEnabled } = useCreateTelemetryDraft({

})


const { baseUrlHint, apiKeyHint, apiKeyBaseUrlPlaceholder, apiKeyValuePlaceholder, step, accountCategory, addMethod, apiKeyBaseUrl, apiKeyValue, antigravityAccountType, antigravityProjectId, upstreamBaseUrl, upstreamApiKey, bedrockAuthMode, bedrockAccessKeyId, bedrockSecretAccessKey, bedrockSessionToken, bedrockRegion, bedrockForceGlobal, bedrockApiKeyValue, vertexServiceAccountFileInput, vertexServiceAccountJson, vertexProjectId, vertexClientEmail, vertexLocation, vertexServiceAccountDragActive, vertexAuthMode, vertexApiKey, vertexUsesApiKey, geminiAIStudioOAuthEnabled, showAdvancedOAuth, showGeminiHelpDialog, geminiTierGoogleOne, geminiTierGcp, geminiTierAIStudio, geminiSelectedTier, parseVertexServiceAccountJson, handleVertexServiceAccountFile, handleVertexServiceAccountDrop } = useCreateCredentialsDraft({
  get resourceTasks() { return resourceTasks },
  get form() { return form },
  get t() { return t },
  get isMultiProtocolPlatform() { return isMultiProtocolPlatform },
  get openCodeAccountMode() { return openCodeAccountMode },
  get accountMode() { return accountMode },
  get defaultCNBaseUrl() { return defaultCNBaseUrl },
  get apiProtocol() { return apiProtocol },
  get geminiOAuthType() { return geminiOAuthType },
  get appStore() { return appStore }
})


const { submitting, handleClose, doCreateAccount, handleSubmit } = useCreateSubmissionDraft({
  get resourceTasks() { return resourceTasks },
  get withAntigravityConfirmFlag() { return withAntigravityConfirmFlag },
  get appStore() { return appStore },
  get t() { return t },
  get emit() { return emit },
  get needsMixedChannelCheck() { return needsMixedChannelCheck },
  get form() { return form },
  get openMixedChannelDialog() { return openMixedChannelDialog },
  get antigravityMixedChannelConfirmed() { return antigravityMixedChannelConfirmed },
  get clearMixedChannelDialog() { return clearMixedChannelDialog },
  get ensureAntigravityMixedChannelConfirmed() { return ensureAntigravityMixedChannelConfirmed },
  get isLoadingBuiltinModels() { return isLoadingBuiltinModels },
  get isOAuthFlow() { return isOAuthFlow },
  get isGrokSSOInputMethod() { return isGrokSSOInputMethod },
  get step() { return step },
  get accountCategory() { return accountCategory },
  get bedrockAuthMode() { return bedrockAuthMode },
  get bedrockRegion() { return bedrockRegion },
  get bedrockAccessKeyId() { return bedrockAccessKeyId },
  get bedrockSecretAccessKey() { return bedrockSecretAccessKey },
  get bedrockSessionToken() { return bedrockSessionToken },
  get bedrockApiKeyValue() { return bedrockApiKeyValue },
  get bedrockForceGlobal() { return bedrockForceGlobal },
  get modelRestrictionMode() { return modelRestrictionMode },
  get allowedModels() { return allowedModels },
  get modelMappings() { return modelMappings },
  get poolModeEnabled() { return poolModeEnabled },
  get normalizePoolModeRetryCount() { return normalizePoolModeRetryCount },
  get poolModeRetryCount() { return poolModeRetryCount },
  get parsePoolModeRetryStatusCodes() { return parsePoolModeRetryStatusCodes },
  get poolModeRetryStatusCodesInput() { return poolModeRetryStatusCodesInput },
  get interceptWarmupRequests() { return interceptWarmupRequests },
  get createAccountAndFinish() { return createAccountAndFinish },
  get antigravityAccountType() { return antigravityAccountType },
  get upstreamBaseUrl() { return upstreamBaseUrl },
  get upstreamApiKey() { return upstreamApiKey },
  get antigravityModelMappings() { return antigravityModelMappings },
  get buildAntigravityExtra() { return buildAntigravityExtra },
  get vertexUsesApiKey() { return vertexUsesApiKey },
  get vertexApiKey() { return vertexApiKey },
  get vertexProjectId() { return vertexProjectId },
  get vertexLocation() { return vertexLocation },
  get parseVertexServiceAccountJson() { return parseVertexServiceAccountJson },
  get vertexServiceAccountJson() { return vertexServiceAccountJson },
  get vertexClientEmail() { return vertexClientEmail },
  get apiKeyValue() { return apiKeyValue },
  get apiKeyBaseUrl() { return apiKeyBaseUrl },
  get geminiTierAIStudio() { return geminiTierAIStudio },
  get isMultiProtocolApiKeyPlatform() { return isMultiProtocolApiKeyPlatform },
  get openCodeAccountMode() { return openCodeAccountMode },
  get accountMode() { return accountMode },
  get apiProtocol() { return apiProtocol },
  get defaultCNAdaptiveBaseUrls() { return defaultCNAdaptiveBaseUrls },
  get cnAdaptiveProtocolOptions() { return cnAdaptiveProtocolOptions },
  get adaptiveBaseUrls() { return adaptiveBaseUrls },
  get defaultCNBaseUrl() { return defaultCNBaseUrl },
  get currentOpenCodeOrCNMode() { return currentOpenCodeOrCNMode },
  get zhipuOrganization() { return zhipuOrganization },
  get zhipuProject() { return zhipuProject },
  get providerRoutesByModel() { return providerRoutesByModel },
  get openCodeGoProtocolRules() { return openCodeGoProtocolRules },
  get isOpenAIModelRestrictionDisabled() { return isOpenAIModelRestrictionDisabled },
  get applyOpenAIEndpointCapabilities() { return applyOpenAIEndpointCapabilities },
  get buildOpenAICompactModelMapping() { return buildOpenAICompactModelMapping },
  get explicitCacheUpstreamEnabled() { return explicitCacheUpstreamEnabled },
  get explicitCacheUpstreamMaxTTL() { return explicitCacheUpstreamMaxTTL },
  get customErrorCodesEnabled() { return customErrorCodesEnabled },
  get selectedErrorCodes() { return selectedErrorCodes },
  get isHeaderOverrideCapable() { return isHeaderOverrideCapable },
  get headerOverrideEnabled() { return headerOverrideEnabled },
  get headerOverrideRows() { return headerOverrideRows },
  get applyTempUnschedConfig() { return applyTempUnschedConfig },
  get buildAnthropicExtra() { return buildAnthropicExtra },
  get buildOpenAIExtra() { return buildOpenAIExtra },
  get props() { return props },
  get withAccountExtra() { return withAccountExtra },
  get upstreamBillingAutoProbeEnabled() { return upstreamBillingAutoProbeEnabled },
  get autoPauseOnExpired() { return autoPauseOnExpired }
})


const { accountMode, openCodeAccountMode, apiProtocol, openCodeGoProtocolRules, zhipuOrganization, zhipuProject, adaptiveBaseUrls, isCNPlatform, isMultiProtocolPlatform, isGenericMultiProtocolPlatform, extraMultiProtocolPlatforms, genericAccountModes, currentOpenCodeOrCNMode, cnPresetPlatform, cnPresetMode, adaptivePresetPlatform, cnAdaptiveProtocolOptions, cnProtocolOptions, cnAccentActiveClass, cnAccentIconClass, selectCNPlatform, selectOpenCodeGoPlatform, selectTypeSafePlatform, selectGenericMultiProtocolPlatform } = useCreateProviderDraft({
  get cloneOpenCodeGoProtocolRules() { return cloneOpenCodeGoProtocolRules },
  get defaultOpenCodeProtocolRules() { return defaultOpenCodeProtocolRules },
  get form() { return form },
  get isMultiProtocolApiKeyPlatform() { return isMultiProtocolApiKeyPlatform },
  get isOpenCodeGoPlatform() { return isOpenCodeGoPlatform },
  get listPlatforms() { return listPlatforms },
  get providerAccountModes() { return providerAccountModes },
  get providerNativeProtocols() { return providerNativeProtocols },
  get defaultCNAdaptiveBaseUrls() { return defaultCNAdaptiveBaseUrls },
  get accountCategory() { return accountCategory },
  get resolveProviderAccountMode() { return resolveProviderAccountMode },
  get apiKeyBaseUrl() { return apiKeyBaseUrl },
  get defaultCNBaseUrl() { return defaultCNBaseUrl },
  get defaultProviderProtocolRules() { return defaultProviderProtocolRules }
})


const { isOpenCodeGoPlatform, onCnPresetSelect, buildAntigravityExtra, mixedChannelWarningDetails, mixedChannelWarningRawMessage, mixedChannelWarningAction, mixedChannelWarningMessageText, geminiHelpLinks, expiresAtInput } = useCreateIdentityDraft({
  get form() { return form },
  get accountMode() { return accountMode },
  get apiProtocol() { return apiProtocol },
  get apiKeyBaseUrl() { return apiKeyBaseUrl },
  get mixedScheduling() { return mixedScheduling },
  get allowOverages() { return allowOverages },
  get t() { return t }
})


const { editQuotaLimit, editQuotaDailyLimit, editQuotaWeeklyLimit, editDailyResetMode, editDailyResetHour, editWeeklyResetMode, editWeeklyResetDay, editWeeklyResetHour, editResetTimezone, autoPauseOnExpired, quotaNotifyGlobalEnabled, quotaNotifyState, loadQuotaNotifyGlobal, writeQuotaNotifyToExtra, windowCostEnabled, windowCostLimit, windowCostStickyReserve, sessionLimitEnabled, maxSessions, sessionIdleTimeout, rpmLimitEnabled, baseRpm, rpmStrategy, rpmStickyBuffer, userMsgQueueMode, umqModeOptions, geminiQuotaDocs } = useCreateLimitsDraft({
  get t() { return t }
})


const { DEFAULT_POOL_MODE_RETRY_COUNT, MAX_POOL_MODE_RETRY_COUNT, DEFAULT_POOL_MODE_RETRY_STATUS_CODES, poolModeEnabled, poolModeRetryCount, poolModeRetryStatusCodesInput, parsePoolModeRetryStatusCodes, customErrorCodesEnabled, selectedErrorCodes, customErrorCodeInput, tempUnschedEnabled, tempUnschedRules, getTempUnschedRuleKey, tempUnschedScopeOptions, tempUnschedPresets, toggleErrorCode, addCustomErrorCode, removeErrorCode, addTempUnschedRule, removeTempUnschedRule, moveTempUnschedRule, buildTempUnschedRules, applyTempUnschedConfig, normalizePoolModeRetryCount } = useCreateRestrictionsDraft({
  get t() { return t },
  get appStore() { return appStore }
})


const { explicitCacheUpstreamEnabled, explicitCacheUpstreamMaxTTL, headerOverrideEnabled, headerOverrideRows, validateGrokOAuthUpstreamConfig, applyGrokOAuthUpstreamConfig, interceptWarmupRequests, openaiPassthroughEnabled, openaiFlattenNamespacesEnabled, openAICompactMode, openAIResponsesMode, openAIImagesUrlToB64JsonEnabled, openAIEndpointCapabilities, openaiAPIKeyResponsesWebSocketV2Mode, codexCLIOnlyEnabled, codexCLIOnlyAppServerEnabled, codexFingerprintMode, codexFingerprintModeOptions, anthropicPassthroughEnabled, anthropicAPIKeyAuthScheme, webSearchEmulationMode, webSearchGlobalEnabled, mixedScheduling, allowOverages, openAICompactModeOptions, openAIResponsesModeOptions, openAIEndpointCapabilityOptions, openAITextGenerationCapabilityEnabled, toggleOpenAIEndpointCapability, applyOpenAIEndpointCapabilities, tlsFingerprintEnabled, tlsFingerprintProfileId, tlsFingerprintProfiles, sessionIdMaskingEnabled, cacheTTLOverrideEnabled, cacheTTLOverrideTarget, customBaseUrlEnabled, customBaseUrl, openAIWSModeOptions, openaiResponsesWebSocketV2Mode, openAIWSModeHintKey, buildOpenAIExtra, buildAnthropicExtra } = useCreateTransportDraft({
  get grokOAuthCustomBaseUrlEnabled() { return grokOAuthCustomBaseUrlEnabled },
  get grokOAuthBaseUrl() { return grokOAuthBaseUrl },
  get appStore() { return appStore },
  get t() { return t },
  get form() { return form },
  get accountCategory() { return accountCategory },
  get openaiOAuthResponsesWebSocketV2Mode() { return openaiOAuthResponsesWebSocketV2Mode },
  get props() { return props },
  get openAILongContextBillingEnabled() { return openAILongContextBillingEnabled }
})


const { showMixedChannelWarning, antigravityMixedChannelConfirmed, needsMixedChannelCheck, clearMixedChannelDialog, openMixedChannelDialog, withAntigravityConfirmFlag, ensureAntigravityMixedChannelConfirmed, handleMixedChannelConfirm, handleMixedChannelCancel } = useCreateAssociationDraft({
  get resourceTasks() { return resourceTasks },
  get mixedChannelWarningDetails() { return mixedChannelWarningDetails },
  get mixedChannelWarningRawMessage() { return mixedChannelWarningRawMessage },
  get mixedChannelWarningAction() { return mixedChannelWarningAction },
  get t() { return t },
  get form() { return form },
  get appStore() { return appStore },
  get submitting() { return submitting }
})


const { resetForm } = useCreateHydrationDraft({
  get step() { return step },
  get form() { return form },
  get accountCategory() { return accountCategory },
  get addMethod() { return addMethod },
  get accountMode() { return accountMode },
  get openCodeAccountMode() { return openCodeAccountMode },
  get apiProtocol() { return apiProtocol },
  get openCodeGoProtocolRules() { return openCodeGoProtocolRules },
  get cloneOpenCodeGoProtocolRules() { return cloneOpenCodeGoProtocolRules },
  get defaultOpenCodeProtocolRules() { return defaultOpenCodeProtocolRules },
  get adaptiveBaseUrls() { return adaptiveBaseUrls },
  get apiKeyBaseUrl() { return apiKeyBaseUrl },
  get apiKeyValue() { return apiKeyValue },
  get upstreamRequestIdHeader() { return upstreamRequestIdHeader },
  get upstreamBillingAutoProbeEnabled() { return upstreamBillingAutoProbeEnabled },
  get editQuotaLimit() { return editQuotaLimit },
  get editQuotaDailyLimit() { return editQuotaDailyLimit },
  get editQuotaWeeklyLimit() { return editQuotaWeeklyLimit },
  get editDailyResetMode() { return editDailyResetMode },
  get editDailyResetHour() { return editDailyResetHour },
  get editWeeklyResetMode() { return editWeeklyResetMode },
  get editWeeklyResetDay() { return editWeeklyResetDay },
  get editWeeklyResetHour() { return editWeeklyResetHour },
  get editResetTimezone() { return editResetTimezone },
  get modelMappings() { return modelMappings },
  get openAICompactModelMappings() { return openAICompactModelMappings },
  get modelRestrictionMode() { return modelRestrictionMode },
  get allowedModels() { return allowedModels },
  get antigravityModelRestrictionMode() { return antigravityModelRestrictionMode },
  get antigravityWhitelistModels() { return antigravityWhitelistModels },
  get antigravityModelMappings() { return antigravityModelMappings },
  get poolModeEnabled() { return poolModeEnabled },
  get poolModeRetryCount() { return poolModeRetryCount },
  get DEFAULT_POOL_MODE_RETRY_COUNT() { return DEFAULT_POOL_MODE_RETRY_COUNT },
  get poolModeRetryStatusCodesInput() { return poolModeRetryStatusCodesInput },
  get explicitCacheUpstreamEnabled() { return explicitCacheUpstreamEnabled },
  get explicitCacheUpstreamMaxTTL() { return explicitCacheUpstreamMaxTTL },
  get customErrorCodesEnabled() { return customErrorCodesEnabled },
  get selectedErrorCodes() { return selectedErrorCodes },
  get customErrorCodeInput() { return customErrorCodeInput },
  get headerOverrideEnabled() { return headerOverrideEnabled },
  get headerOverrideRows() { return headerOverrideRows },
  get openAIImagesUrlToB64JsonEnabled() { return openAIImagesUrlToB64JsonEnabled },
  get grokOAuthCustomBaseUrlEnabled() { return grokOAuthCustomBaseUrlEnabled },
  get grokOAuthBaseUrl() { return grokOAuthBaseUrl },
  get interceptWarmupRequests() { return interceptWarmupRequests },
  get autoPauseOnExpired() { return autoPauseOnExpired },
  get openaiPassthroughEnabled() { return openaiPassthroughEnabled },
  get openaiFlattenNamespacesEnabled() { return openaiFlattenNamespacesEnabled },
  get openAILongContextBillingEnabled() { return openAILongContextBillingEnabled },
  get openAILongContextBillingTouched() { return openAILongContextBillingTouched },
  get openAICompactMode() { return openAICompactMode },
  get openAIResponsesMode() { return openAIResponsesMode },
  get openAIEndpointCapabilities() { return openAIEndpointCapabilities },
  get openaiOAuthResponsesWebSocketV2Mode() { return openaiOAuthResponsesWebSocketV2Mode },
  get openaiAPIKeyResponsesWebSocketV2Mode() { return openaiAPIKeyResponsesWebSocketV2Mode },
  get codexCLIOnlyEnabled() { return codexCLIOnlyEnabled },
  get codexCLIOnlyAppServerEnabled() { return codexCLIOnlyAppServerEnabled },
  get codexFingerprintMode() { return codexFingerprintMode },
  get anthropicPassthroughEnabled() { return anthropicPassthroughEnabled },
  get anthropicAPIKeyAuthScheme() { return anthropicAPIKeyAuthScheme },
  get webSearchEmulationMode() { return webSearchEmulationMode },
  get windowCostEnabled() { return windowCostEnabled },
  get windowCostLimit() { return windowCostLimit },
  get windowCostStickyReserve() { return windowCostStickyReserve },
  get sessionLimitEnabled() { return sessionLimitEnabled },
  get maxSessions() { return maxSessions },
  get sessionIdleTimeout() { return sessionIdleTimeout },
  get rpmLimitEnabled() { return rpmLimitEnabled },
  get baseRpm() { return baseRpm },
  get rpmStrategy() { return rpmStrategy },
  get rpmStickyBuffer() { return rpmStickyBuffer },
  get userMsgQueueMode() { return userMsgQueueMode },
  get tlsFingerprintEnabled() { return tlsFingerprintEnabled },
  get tlsFingerprintProfileId() { return tlsFingerprintProfileId },
  get sessionIdMaskingEnabled() { return sessionIdMaskingEnabled },
  get cacheTTLOverrideEnabled() { return cacheTTLOverrideEnabled },
  get cacheTTLOverrideTarget() { return cacheTTLOverrideTarget },
  get customBaseUrlEnabled() { return customBaseUrlEnabled },
  get customBaseUrl() { return customBaseUrl },
  get allowOverages() { return allowOverages },
  get antigravityAccountType() { return antigravityAccountType },
  get antigravityProjectId() { return antigravityProjectId },
  get upstreamBaseUrl() { return upstreamBaseUrl },
  get upstreamApiKey() { return upstreamApiKey },
  get vertexServiceAccountJson() { return vertexServiceAccountJson },
  get vertexProjectId() { return vertexProjectId },
  get vertexClientEmail() { return vertexClientEmail },
  get vertexLocation() { return vertexLocation },
  get vertexAuthMode() { return vertexAuthMode },
  get vertexApiKey() { return vertexApiKey },
  get tempUnschedEnabled() { return tempUnschedEnabled },
  get tempUnschedRules() { return tempUnschedRules },
  get geminiOAuthType() { return geminiOAuthType },
  get geminiTierGoogleOne() { return geminiTierGoogleOne },
  get geminiTierGcp() { return geminiTierGcp },
  get geminiTierAIStudio() { return geminiTierAIStudio },
  get oauth() { return oauth },
  get openaiOAuth() { return openaiOAuth },
  get geminiOAuth() { return geminiOAuth },
  get antigravityOAuth() { return antigravityOAuth },
  get grokOAuth() { return grokOAuth },
  get oauthFlowRef() { return oauthFlowRef },
  get antigravityMixedChannelConfirmed() { return antigravityMixedChannelConfirmed },
  get clearMixedChannelDialog() { return clearMixedChannelDialog }
})


const { handleOpenAIExchange, handleOpenAIImportCodexSession, handleOpenAIImportCodexPAT, handleOpenAIValidateRT, handleOpenAIValidateMobileRT } = useCreateOpenAIAuthorizationDraft({
  get resourceTasks() { return resourceTasks },
  get buildOpenAIExtra() { return buildOpenAIExtra },
  get openAILongContextBillingTouched() { return openAILongContextBillingTouched },
  get openaiOAuth() { return openaiOAuth },
  get oauthFlowRef() { return oauthFlowRef },
  get t() { return t },
  get appStore() { return appStore },
  get form() { return form },
  get isOpenAIModelRestrictionDisabled() { return isOpenAIModelRestrictionDisabled },
  get modelRestrictionMode() { return modelRestrictionMode },
  get allowedModels() { return allowedModels },
  get modelMappings() { return modelMappings },
  get buildOpenAICompactModelMapping() { return buildOpenAICompactModelMapping },
  get applyTempUnschedConfig() { return applyTempUnschedConfig },
  get withAccountExtra() { return withAccountExtra },
  get props() { return props },
  get autoPauseOnExpired() { return autoPauseOnExpired },
  get emit() { return emit },
  get handleClose() { return handleClose }
})


const { handleGrokValidateRT, handleGrokImportSSO, handleGrokAuthorizePassword, handleGrokExchange } = useCreateGrokAuthorizationDraft({
  get resourceTasks() { return resourceTasks },
  get grokOAuth() { return grokOAuth },
  get t() { return t },
  get validateGrokOAuthUpstreamConfig() { return validateGrokOAuthUpstreamConfig },
  get form() { return form },
  get applyGrokOAuthUpstreamConfig() { return applyGrokOAuthUpstreamConfig },
  get modelRestrictionMode() { return modelRestrictionMode },
  get allowedModels() { return allowedModels },
  get modelMappings() { return modelMappings },
  get applyTempUnschedConfig() { return applyTempUnschedConfig },
  get withAccountExtra() { return withAccountExtra },
  get props() { return props },
  get autoPauseOnExpired() { return autoPauseOnExpired },
  get appStore() { return appStore },
  get emit() { return emit },
  get handleClose() { return handleClose },
  get resourceServices() { return resourceServices },
  get oauthFlowRef() { return oauthFlowRef },
  get createAccountAndFinish() { return createAccountAndFinish }
})


const { handleAntigravityValidateRT, handleGeminiExchange, handleAntigravityExchange } = useCreateGoogleAuthorizationDraft({
  get resourceTasks() { return resourceTasks },
  get antigravityOAuth() { return antigravityOAuth },
  get t() { return t },
  get form() { return form },
  get antigravityProjectId() { return antigravityProjectId },
  get withAntigravityConfirmFlag() { return withAntigravityConfirmFlag },
  get withAccountExtra() { return withAccountExtra },
  get props() { return props },
  get autoPauseOnExpired() { return autoPauseOnExpired },
  get appStore() { return appStore },
  get emit() { return emit },
  get handleClose() { return handleClose },
  get geminiOAuth() { return geminiOAuth },
  get oauthFlowRef() { return oauthFlowRef },
  get geminiOAuthType() { return geminiOAuthType },
  get geminiSelectedTier() { return geminiSelectedTier },
  get createAccountAndFinish() { return createAccountAndFinish },
  get interceptWarmupRequests() { return interceptWarmupRequests },
  get antigravityModelMappings() { return antigravityModelMappings },
  get buildAntigravityExtra() { return buildAntigravityExtra }
})


const { handleAnthropicSetupToken, handleAnthropicExchange, handleCookieAuth } = useCreateAnthropicAuthorizationDraft({
  get resourceTasks() { return resourceTasks },
  get windowCostEnabled() { return windowCostEnabled },
  get windowCostLimit() { return windowCostLimit },
  get windowCostStickyReserve() { return windowCostStickyReserve },
  get sessionLimitEnabled() { return sessionLimitEnabled },
  get maxSessions() { return maxSessions },
  get sessionIdleTimeout() { return sessionIdleTimeout },
  get rpmLimitEnabled() { return rpmLimitEnabled },
  get baseRpm() { return baseRpm },
  get rpmStrategy() { return rpmStrategy },
  get rpmStickyBuffer() { return rpmStickyBuffer },
  get userMsgQueueMode() { return userMsgQueueMode },
  get tlsFingerprintEnabled() { return tlsFingerprintEnabled },
  get tlsFingerprintProfileId() { return tlsFingerprintProfileId },
  get sessionIdMaskingEnabled() { return sessionIdMaskingEnabled },
  get cacheTTLOverrideEnabled() { return cacheTTLOverrideEnabled },
  get cacheTTLOverrideTarget() { return cacheTTLOverrideTarget },
  get customBaseUrlEnabled() { return customBaseUrlEnabled },
  get customBaseUrl() { return customBaseUrl },
  get oauth() { return oauth },
  get t() { return t },
  get createAccountAndFinish() { return createAccountAndFinish },
  get appStore() { return appStore },
  get form() { return form },
  get addMethod() { return addMethod },
  get interceptWarmupRequests() { return interceptWarmupRequests },
  get tempUnschedEnabled() { return tempUnschedEnabled },
  get buildTempUnschedRules() { return buildTempUnschedRules },
  get tempUnschedRules() { return tempUnschedRules },
  get withAccountExtra() { return withAccountExtra },
  get props() { return props },
  get autoPauseOnExpired() { return autoPauseOnExpired },
  get emit() { return emit },
  get handleClose() { return handleClose }
})


watch(() => form.platform, resourceTasks.invalidate, { flush: 'sync' })
watch(addMethod, () => {
  oauth.resetState()
  oauthFlowRef.value?.reset()
})


watch(openCodeAccountMode, (mode, previousMode) => {
  if (!isOpenCodeGoPlatform.value) return
  if (apiProtocol.value === 'adaptive') {
    const previousDefaults = defaultCNAdaptiveBaseUrls('opencode_go', previousMode)
    const nextDefaults = defaultCNAdaptiveBaseUrls('opencode_go', mode)
    for (const item of cnAdaptiveProtocolOptions.value) {
      if (!adaptiveBaseUrls.value[item.value] || adaptiveBaseUrls.value[item.value] === previousDefaults[item.value]) {
        adaptiveBaseUrls.value[item.value] = nextDefaults[item.value]
      }
    }
    apiKeyBaseUrl.value = adaptiveBaseUrls.value.chat_completions
  } else {
    apiKeyBaseUrl.value = defaultCNBaseUrl('opencode_go', mode, apiProtocol.value)
  }
  const previousRules = JSON.stringify(defaultOpenCodeProtocolRules(previousMode))
  if (JSON.stringify(openCodeGoProtocolRules.value) === previousRules) {
    openCodeGoProtocolRules.value = cloneOpenCodeGoProtocolRules(defaultOpenCodeProtocolRules(mode))
  }
})


watch(accountMode, (mode, previousMode) => {
  if (!isMultiProtocolPlatform.value || isOpenCodeGoPlatform.value) return
  if (routesByModel.value) {
    const previousRules = JSON.stringify(defaultProviderProtocolRules(form.platform, previousMode))
    if (JSON.stringify(openCodeGoProtocolRules.value) === previousRules) {
      openCodeGoProtocolRules.value = cloneOpenCodeGoProtocolRules(defaultProviderProtocolRules(form.platform, mode))
    }
  }
  if (apiProtocol.value === 'adaptive') {
    const previousDefaults = defaultCNAdaptiveBaseUrls(adaptivePresetPlatform.value, previousMode)
    const nextDefaults = defaultCNAdaptiveBaseUrls(adaptivePresetPlatform.value, mode)
    for (const item of cnAdaptiveProtocolOptions.value) {
      if (!adaptiveBaseUrls.value[item.value] || adaptiveBaseUrls.value[item.value] === previousDefaults[item.value]) {
        adaptiveBaseUrls.value[item.value] = nextDefaults[item.value]
      }
    }
    apiKeyBaseUrl.value = adaptiveBaseUrls.value.chat_completions
    return
  }
  apiKeyBaseUrl.value = defaultCNBaseUrl(form.platform, mode, apiProtocol.value)
})


watch(apiProtocol, (protocol) => {
  if (!isMultiProtocolPlatform.value) return
  if (protocol === 'adaptive') {
    const defaults = defaultCNAdaptiveBaseUrls(adaptivePresetPlatform.value, currentOpenCodeOrCNMode())
    for (const item of cnAdaptiveProtocolOptions.value) {
      if (!adaptiveBaseUrls.value[item.value]) adaptiveBaseUrls.value[item.value] = defaults[item.value]
    }
    apiKeyBaseUrl.value = adaptiveBaseUrls.value.chat_completions
    return
  }
  apiKeyBaseUrl.value = defaultCNBaseUrl(form.platform, currentOpenCodeOrCNMode(), protocol)
})


if (props.allowSystemSettings) resourceServices.webSearchConfig().then(resourceTasks.guard(cfg => {
  webSearchGlobalEnabled.value = cfg?.enabled === true && (cfg?.providers?.length ?? 0) > 0
})).catch(resourceTasks.guard(() => { webSearchGlobalEnabled.value = false }))


if (props.allowSystemSettings) loadQuotaNotifyGlobal()


watch(vertexAuthMode, () => {
  vertexServiceAccountJson.value = ''
  vertexProjectId.value = ''
  vertexClientEmail.value = ''
  vertexApiKey.value = ''
})


watch(
  () => props.show,
  (newVal) => {
    if (newVal) {
      if (typeof accountsAPI.creationDefaults === 'function') {
        accountsAPI.creationDefaults().then(resourceTasks.guard(defaults => {
          if (props.show) {
            form.concurrency = defaults.concurrency
            form.priority = defaults.priority
          }
        })).catch(resourceTasks.guard(() => appStore.showError(t('admin.accounts.failedToLoad'))))
      }
      // Load TLS fingerprint profiles
      if (props.allowSystemSettings) resourceServices.tlsProfiles()
        .then(resourceTasks.guard(profiles => { tlsFingerprintProfiles.value = profiles.map(p => ({ id: p.id, name: p.name })) }))
        .catch(resourceTasks.guard(() => { tlsFingerprintProfiles.value = [] }))
      // Antigravity: 默认使用映射模式并填充默认映射
      if (form.platform === 'antigravity') {
        antigravityModelRestrictionMode.value = 'mapping'
        const mappingAtStart = JSON.stringify(antigravityModelMappings.value)
        fetchAntigravityDefaultMappings().then(resourceTasks.guard(mappings => {
          if (JSON.stringify(antigravityModelMappings.value) === mappingAtStart) antigravityModelMappings.value = [...mappings]
        })).catch(resourceTasks.guard(() => appStore.showError(t('admin.accounts.failedToLoad'))))
        antigravityWhitelistModels.value = []
      } else {
        antigravityWhitelistModels.value = []
        antigravityModelMappings.value = []
        antigravityModelRestrictionMode.value = 'mapping'
      }
    } else {
      resetForm()
    }
  },
  { immediate: true }
)


watch(
  [accountCategory, addMethod, antigravityAccountType, () => form.platform],
  ([category, method, agType]) => {
    // Antigravity upstream 类型（实际创建为 apikey）
    if (form.platform === 'antigravity' && agType === 'upstream') {
      form.type = 'apikey'
      return
    }
    // Bedrock 类型
    if (form.platform === 'anthropic' && category === 'bedrock') {
      form.type = 'bedrock' as AccountType
      return
    }
    if ((form.platform === 'gemini' || form.platform === 'anthropic') && category === 'service_account') {
      form.type = 'service_account' as AccountType
    } else if (category === 'oauth-based') {
      form.type = form.platform === 'anthropic'
        ? (method === 'setup-token-manual' ? 'setup-token' : method as AccountType)
        : 'oauth'
    } else {
      form.type = 'apikey'
    }
  },
  { immediate: true }
)


watch(
  () => form.platform,
  (newPlatform) => {
    // Reset base URL based on platform
    if (isMultiProtocolApiKeyPlatform(newPlatform)) {
      const mode = newPlatform === 'opencode_go' ? openCodeAccountMode.value : accountMode.value
      apiKeyBaseUrl.value = defaultCNBaseUrl(newPlatform, mode, apiProtocol.value)
    } else {
      apiKeyBaseUrl.value =
        (newPlatform === 'openai')
          ? 'https://api.openai.com'
          : newPlatform === 'gemini'
            ? 'https://generativelanguage.googleapis.com'
            : newPlatform === 'grok'
              ? 'https://api.x.ai/v1'
              : newPlatform === 'typesafe'
                ? 'https://api.typesafe.ai'
              : 'https://api.anthropic.com'
    }
    // Clear model-related settings
    allowedModels.value = []
    modelMappings.value = []
    if (newPlatform === 'typesafe') {
      accountCategory.value = 'apikey'
      // Grok 等平台会把模式切到映射；TypeSafe 只用白名单写入 jev-latest。
      modelRestrictionMode.value = 'whitelist'
    }
    // Antigravity: 默认使用映射模式并填充默认映射
    if (newPlatform === 'antigravity') {
      antigravityModelRestrictionMode.value = 'mapping'
      const mappingAtStart = JSON.stringify(antigravityModelMappings.value)
      fetchAntigravityDefaultMappings().then(resourceTasks.guard(mappings => {
        if (JSON.stringify(antigravityModelMappings.value) === mappingAtStart) antigravityModelMappings.value = [...mappings]
      })).catch(resourceTasks.guard(() => appStore.showError(t('admin.accounts.failedToLoad'))))
      antigravityWhitelistModels.value = []
      accountCategory.value = 'oauth-based'
      antigravityAccountType.value = 'oauth'
    } else {
      allowOverages.value = false
      antigravityProjectId.value = ''
      antigravityWhitelistModels.value = []
      antigravityModelMappings.value = []
      antigravityModelRestrictionMode.value = 'mapping'
    }
    if (newPlatform === 'grok') {
      accountCategory.value = 'oauth-based'
      addMethod.value = 'oauth'
      modelRestrictionMode.value = 'mapping'
      form.concurrency = 1
      form.load_factor = null
    }
    if (newPlatform !== 'gemini' && newPlatform !== 'anthropic' && accountCategory.value === 'service_account') {
      accountCategory.value = 'oauth-based'
    }
    if (newPlatform !== 'anthropic' && accountCategory.value === 'bedrock') {
      accountCategory.value = 'oauth-based'
    }
    // Reset Bedrock fields when switching platforms
    bedrockAccessKeyId.value = ''
    bedrockSecretAccessKey.value = ''
    bedrockSessionToken.value = ''
    bedrockRegion.value = 'us-east-1'
    bedrockForceGlobal.value = false
    bedrockAuthMode.value = 'sigv4'
    bedrockApiKeyValue.value = ''
    vertexServiceAccountJson.value = ''
    vertexProjectId.value = ''
    vertexClientEmail.value = ''
    vertexLocation.value = 'global'
    vertexAuthMode.value = 'service_account'
    vertexApiKey.value = ''
    // Reset Anthropic/Antigravity-specific settings when switching to other platforms
    if (newPlatform !== 'anthropic' && newPlatform !== 'antigravity') {
      interceptWarmupRequests.value = false
    }
    if (newPlatform !== 'openai') {
      openaiPassthroughEnabled.value = false
      openaiFlattenNamespacesEnabled.value = false
      openAIEndpointCapabilities.value = ['chat_completions', 'embeddings']
      openaiOAuthResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
      openaiAPIKeyResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
      codexCLIOnlyEnabled.value = false
      codexCLIOnlyAppServerEnabled.value = false
    }
    if (newPlatform !== 'anthropic') {
      anthropicPassthroughEnabled.value = false
      anthropicAPIKeyAuthScheme.value = 'x_api_key'
      webSearchEmulationMode.value = 'default'
    }
    // 请求头覆写为平台相关配置（常用头集合不同），切换平台时清空，
    // 避免上一平台的配置行被提交到新平台账号
    headerOverrideEnabled.value = false
    headerOverrideRows.value = []
    openAIImagesUrlToB64JsonEnabled.value = false
    grokOAuthCustomBaseUrlEnabled.value = false
    grokOAuthBaseUrl.value = ''
    // Reset OAuth states
    oauth.resetState()
    openaiOAuth.resetState()

    geminiOAuth.resetState()
    antigravityOAuth.resetState()
    grokOAuth.resetState()
  }
)


watch(
  [accountCategory, () => form.platform],
  ([category, platform]) => {
    if (platform === 'openai' && category !== 'oauth-based') {
      codexCLIOnlyEnabled.value = false
      codexCLIOnlyAppServerEnabled.value = false
    }
    if (platform !== 'anthropic' || category !== 'apikey') {
      anthropicPassthroughEnabled.value = false
      anthropicAPIKeyAuthScheme.value = 'x_api_key'
      webSearchEmulationMode.value = 'default'
    }
  }
)


watch(
  [() => props.show, () => form.platform, accountCategory],
  async ([show, platform, category]) => {
const pending = resourceTasks.capture()
try {

    if (!show || platform !== 'gemini' || category !== 'oauth-based') {
      geminiAIStudioOAuthEnabled.value = false
      return
    }
    const caps = await pending.wait(async () => geminiOAuth.getCapabilities())
    geminiAIStudioOAuthEnabled.value = !!caps?.ai_studio_oauth_enabled
    if (!geminiAIStudioOAuthEnabled.value && geminiOAuthType.value === 'ai_studio') {
      geminiOAuthType.value = 'code_assist'
    }
  
} catch (failure: unknown) {
  if (!(failure instanceof StaleResourceTask)) throw failure
}
},
  { immediate: true }
)


watch(
  [() => props.show, () => form.platform, () => resourceServices.access.canManage, resourceServices.scopeKey],
  async ([show, platform], _previous, onCleanup) => {
const pending = resourceTasks.capture()
try {

    let cancelled = false
    const controller = new AbortController()
    onCleanup(() => { cancelled = true; controller.abort() })
    isLoadingBuiltinModels.value = false
    if (!show || platform === 'antigravity' || !resourceServices.access.canManage) return
    const draft = allowedModels.value
    isLoadingBuiltinModels.value = true
    try {
      const models = await pending.wait(async () => managedModelCatalog(platform, { signal: controller.signal }))
      if (cancelled || allowedModels.value !== draft) return
      allowedModels.value = [...new Set(models.filter(model => model.enabled).map(model => model.id.trim()).filter(Boolean))]
    } catch {
if (!pending.current()) throw new StaleResourceTask()

      if (!cancelled) appStore.showError(t('admin.accounts.modelReference.loadFailed'))
    } finally {
if (pending.current()) {

      if (!cancelled) isLoadingBuiltinModels.value = false
    
}
}
  
} catch (failure: unknown) {
  if (!(failure instanceof StaleResourceTask)) throw failure
}
},
  { immediate: true }
)


watch(resourceServices.scopeKey, () => {
  resourceTasks.invalidate()
  resetForm()
  if (props.show) emit('close')
}, { flush: 'sync' })
watch([() => props.show, resourceServices.scopeKey], () => { submitting.value = false }, { flush: 'sync' })
</script>

<template>


  <BaseDialog
    :show="show"
    :title="t('admin.accounts.createAccount')"
    width="wide"
    @close="handleClose"
  >
    <!-- Step Indicator for OAuth accounts -->
    <div v-if="isOAuthFlow" class="mb-6 flex items-center justify-center">
      <div class="flex items-center space-x-4">
        <div class="flex items-center">
          <div
            :class="[
              'flex h-8 w-8 items-center justify-center rounded-full text-sm font-semibold',
              step >= 1 ? 'bg-primary-500 text-white' : 'bg-gray-200 text-gray-500 dark:bg-dark-600'
            ]"
          >
            1
          </div>
          <span class="ml-2 text-sm font-medium text-gray-700 dark:text-gray-300">{{
            t('admin.accounts.oauth.authMethod')
          }}</span>
        </div>
        <div class="h-0.5 w-8 bg-gray-300 dark:bg-dark-600" />
        <div class="flex items-center">
          <div
            :class="[
              'flex h-8 w-8 items-center justify-center rounded-full text-sm font-semibold',
              step >= 2 ? 'bg-primary-500 text-white' : 'bg-gray-200 text-gray-500 dark:bg-dark-600'
            ]"
          >
            2
          </div>
          <span class="ml-2 text-sm font-medium text-gray-700 dark:text-gray-300">{{
            oauthStepTitle
          }}</span>
        </div>
      </div>
    </div>

    <!-- Step 1: Basic Info -->
    <form
      v-if="step === 1"
      id="create-account-form"
      @submit.prevent="handleSubmit"
      class="space-y-5"
    >
      <ResourceIdentityPanel :model-value="{ name: form.name, notes: form.notes }" :name-required="!isGrokSSOInputMethod" @update:model-value="Object.assign(form, $event)" />

      <!-- Platform Selection - Segmented Control Style -->
      <CreateProviderIdentityPanel
  :form="{ platform: form.platform }"
  :selectTypeSafePlatform="selectTypeSafePlatform"
  :selectCNPlatform="selectCNPlatform"
  :selectOpenCodeGoPlatform="selectOpenCodeGoPlatform"
  :extraMultiProtocolPlatforms="extraMultiProtocolPlatforms"
  :selectGenericMultiProtocolPlatform="selectGenericMultiProtocolPlatform"
  @update:form="Object.assign(form, $event)"
/>

      <!-- Account Type Selection (Anthropic) -->
      <CreateAnthropicCredentialKindPanel
  :form="{ platform: form.platform }"
  :accountCategory="accountCategory"
  @update:accountCategory="accountCategory = $event"
/>

      <!-- Account Type Selection (OpenAI) -->
      <CreateOpenAICredentialKindPanel
  :form="{ platform: form.platform }"
  :accountCategory="accountCategory"
  @update:accountCategory="accountCategory = $event"
/>

      <!-- Account Type Selection (Grok) -->
      <CreateGrokCredentialKindPanel
  :form="{ platform: form.platform }"
  :accountCategory="accountCategory"
  @update:accountCategory="accountCategory = $event"
/>

      <!-- OpenCode Zen vs Go -->
      <CreateOpenCodeModePanel
  :isOpenCodeGoPlatform="isOpenCodeGoPlatform"
  :openCodeAccountMode="openCodeAccountMode"
  :cnAccentActiveClass="cnAccentActiveClass"
  :cnAccentIconClass="cnAccentIconClass"
  @update:openCodeAccountMode="openCodeAccountMode = $event"
/>

      <!-- Account Mode Selection (providers using the generic form) -->
      <CreateProviderAccountModePanel
  :isGenericMultiProtocolPlatform="isGenericMultiProtocolPlatform"
  :genericAccountModes="genericAccountModes"
  :accountMode="accountMode"
  :cnAccentActiveClass="cnAccentActiveClass"
  :cnAccentIconClass="cnAccentIconClass"
  @update:accountMode="accountMode = $event"
/>

      <!-- Account Mode Selection (Kimi / Zhipu / DeepSeek) -->
      <CreateProviderAccountMode2Panel
  :isCNPlatform="isCNPlatform"
  :isOpenCodeGoPlatform="isOpenCodeGoPlatform"
  :accountMode="accountMode"
  :cnAccentActiveClass="cnAccentActiveClass"
  :cnAccentIconClass="cnAccentIconClass"
  :form="{ platform: form.platform }"
  @update:accountMode="accountMode = $event"
/>

      <!-- API Protocol Selection (Kimi / Zhipu / DeepSeek / OpenCode) -->
      <CreateProviderTransportPanel
  :isMultiProtocolPlatform="isMultiProtocolPlatform"
  :cnProtocolOptions="cnProtocolOptions"
  :apiProtocol="apiProtocol"
  :cnAccentActiveClass="cnAccentActiveClass"
  :cnAccentIconClass="cnAccentIconClass"
  @update:apiProtocol="apiProtocol = $event"
/>

      <!-- Zhipu 团队版 Coding Plan：组织/项目 ID（可选，填写后额度探测走团队版端点） -->
      <CreateZhipuOrganizationPanel
  :form="{ platform: form.platform }"
  :accountMode="accountMode"
  :zhipuOrganization="zhipuOrganization"
  :zhipuProject="zhipuProject"
  @update:zhipuOrganization="zhipuOrganization = $event"
  @update:zhipuProject="zhipuProject = $event"
/>

      <!-- Account Type Selection (Gemini) -->
      <CreateGeminiAuthorizationOptionsPanel
  :form="{ platform: form.platform }"
  :showGeminiHelpDialog="showGeminiHelpDialog"
  :accountCategory="accountCategory"
  :apiKeyBaseUrl="apiKeyBaseUrl"
  :geminiHelpLinks="geminiHelpLinks"
  :handleSelectGeminiOAuthType="handleSelectGeminiOAuthType"
  :geminiOAuthType="geminiOAuthType"
  :showAdvancedOAuth="showAdvancedOAuth"
  :geminiAIStudioOAuthEnabled="geminiAIStudioOAuthEnabled"
  :geminiTierGoogleOne="geminiTierGoogleOne"
  :geminiTierGcp="geminiTierGcp"
  :geminiTierAIStudio="geminiTierAIStudio"
  @update:showGeminiHelpDialog="showGeminiHelpDialog = $event"
  @update:accountCategory="accountCategory = $event"
  @update:showAdvancedOAuth="showAdvancedOAuth = $event"
  @update:geminiTierGoogleOne="geminiTierGoogleOne = $event"
  @update:geminiTierGcp="geminiTierGcp = $event"
  @update:geminiTierAIStudio="geminiTierAIStudio = $event"
/>

      <!-- Account Type Selection (Antigravity - OAuth or Upstream) -->
      <CreateAntigravityCredentialKindPanel
  :form="{ platform: form.platform }"
  :antigravityAccountType="antigravityAccountType"
  @update:antigravityAccountType="antigravityAccountType = $event"
/>

      <div v-if="form.platform === 'antigravity' && antigravityAccountType === 'oauth'">
        <label class="input-label">{{ t('admin.accounts.antigravityProjectIdLabel') }}</label>
        <input
          v-model="antigravityProjectId"
          data-testid="antigravity-project-id-input"
          type="text"
          class="input font-mono"
          :placeholder="t('admin.accounts.antigravityProjectIdPlaceholder')"
        />
        <p class="input-hint">{{ t('admin.accounts.antigravityProjectIdHint') }}</p>
      </div>

      <!-- Upstream config (only for Antigravity upstream type) -->
      <CreateUpstreamCredentialsPanel
  :form="{ platform: form.platform }"
  :antigravityAccountType="antigravityAccountType"
  :upstreamBaseUrl="upstreamBaseUrl"
  :upstreamApiKey="upstreamApiKey"
  :allowRateEdit="allowRateEdit"
  :upstreamBillingAutoProbeEnabled="upstreamBillingAutoProbeEnabled"
  @update:upstreamBaseUrl="upstreamBaseUrl = $event"
  @update:upstreamApiKey="upstreamApiKey = $event"
  @update:upstreamBillingAutoProbeEnabled="upstreamBillingAutoProbeEnabled = $event"
/>

      <!-- Vertex Service Account -->
      <div v-if="(form.platform === 'gemini' || form.platform === 'anthropic') && accountCategory === 'service_account'" class="space-y-4">
        <div v-if="form.platform === 'gemini'">
          <label class="input-label">{{ t('admin.accounts.vertexAuthMode') }}</label>
          <div class="mt-2 flex gap-4">
            <label class="flex cursor-pointer items-center">
              <input
                v-model="vertexAuthMode"
                type="radio"
                value="service_account"
                class="mr-2 text-primary-600 focus:ring-primary-500"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.vertexAuthModeServiceAccount') }}</span>
            </label>
            <label class="flex cursor-pointer items-center">
              <input
                v-model="vertexAuthMode"
                type="radio"
                value="apikey"
                data-testid="vertex-auth-mode-apikey"
                class="mr-2 text-primary-600 focus:ring-primary-500"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.vertexAuthModeApiKey') }}</span>
            </label>
          </div>
        </div>

        <template v-if="vertexUsesApiKey">
          <div>
            <label class="input-label">API Key</label>
            <input
              v-model="vertexApiKey"
              type="password"
              required
              class="input font-mono"
              data-testid="vertex-api-key-input"
            />
            <p class="input-hint">{{ t('admin.accounts.vertexApiKeyHint') }}</p>
          </div>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label class="input-label">Project ID</label>
              <input
                v-model="vertexProjectId"
                type="text"
                class="input font-mono"
                data-testid="vertex-api-key-project-id-input"
                :placeholder="t('admin.accounts.vertexApiKeyProjectIdPlaceholder')"
              />
              <p class="input-hint">{{ t('admin.accounts.vertexApiKeyProjectIdHint') }}</p>
            </div>
            <div v-if="vertexProjectId.trim()">
              <label class="input-label">Location</label>
              <select
                v-model="vertexLocation"
                required
                class="input font-mono"
              >
                <optgroup
                  v-for="group in VERTEX_LOCATION_OPTIONS"
                  :key="group.label"
                  :label="group.label"
                >
                  <option
                    v-for="option in group.options"
                    :key="option.value"
                    :value="option.value"
                  >
                    {{ option.label }}
                  </option>
                </optgroup>
              </select>
              <p class="input-hint">{{ t('admin.accounts.vertexLocationHint') }}</p>
            </div>
          </div>
        </template>

        <template v-else>
          <div>
            <label class="input-label">Service Account JSON</label>
            <input
              ref="vertexServiceAccountFileInput"
              type="file"
              accept="application/json,.json"
              class="hidden"
              @change="handleVertexServiceAccountFile"
            />
            <div
              :class="[
                'rounded-lg border-2 border-dashed px-4 py-5 transition-colors',
                vertexServiceAccountDragActive
                  ? 'border-sky-500 bg-sky-50 dark:border-sky-500 dark:bg-sky-900/20'
                  : 'border-gray-300 bg-gray-50 hover:border-sky-400 hover:bg-sky-50/60 dark:border-dark-500 dark:bg-dark-700/40 dark:hover:border-sky-600 dark:hover:bg-sky-900/10'
              ]"
              @dragenter.prevent="vertexServiceAccountDragActive = true"
              @dragover.prevent="vertexServiceAccountDragActive = true"
              @dragleave.prevent="vertexServiceAccountDragActive = false"
              @drop.prevent="handleVertexServiceAccountDrop"
            >
              <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div class="min-w-0">
                  <div class="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-white">
                    <Icon name="upload" size="sm" />
                    <span>{{ vertexClientEmail ? t('admin.accounts.vertexSaJsonLoaded') : t('admin.accounts.vertexSaJsonDrop') }}</span>
                  </div>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                    {{ vertexClientEmail ? t('admin.accounts.vertexSaJsonKeyHidden') : t('admin.accounts.vertexSaJsonDropHint') }}
                  </p>
                </div>
                <button
                  type="button"
                  class="btn btn-secondary shrink-0"
                  @click="vertexServiceAccountFileInput?.click()"
                >
                  <Icon name="upload" size="sm" />
                  {{ t('admin.accounts.vertexSaJsonSelectBtn') }}
                </button>
              </div>
              <div
                v-if="vertexClientEmail"
                class="mt-3 rounded-md border border-sky-200 bg-white px-3 py-2 text-xs text-sky-900 dark:border-sky-800/50 dark:bg-dark-800 dark:text-sky-200"
              >
                <div class="truncate">Project ID: <span class="font-mono">{{ vertexProjectId }}</span></div>
                <div class="truncate">Client Email: <span class="font-mono">{{ vertexClientEmail }}</span></div>
              </div>
            </div>
            <p class="input-hint">{{ t('admin.accounts.vertexSaJsonUploadHint') }}</p>
          </div>

          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label class="input-label">Project ID</label>
              <input
                v-model="vertexProjectId"
                type="text"
                class="input font-mono"
                readonly
                :placeholder="t('admin.accounts.vertexProjectIdPlaceholder')"
              />
            </div>
            <div>
              <label class="input-label">Location</label>
              <select
                v-model="vertexLocation"
                required
                class="input font-mono"
              >
                <optgroup
                  v-for="group in VERTEX_LOCATION_OPTIONS"
                  :key="group.label"
                  :label="group.label"
                >
                  <option
                    v-for="option in group.options"
                    :key="option.value"
                    :value="option.value"
                  >
                    {{ option.label }}
                  </option>
                </optgroup>
              </select>
              <p class="input-hint">{{ t('admin.accounts.vertexLocationHint') }}</p>
            </div>
          </div>
        </template>
      </div>

      <!-- Antigravity model restriction (applies to OAuth + Upstream) -->
      <!-- Antigravity 只支持模型映射模式，不支持白名单模式 -->
      <CreateAntigravityModelMappingPanel
  :form="{ platform: form.platform }"
  :antigravityModelMappings="antigravityModelMappings"
  :getAntigravityModelMappingKey="getAntigravityModelMappingKey"
  :removeAntigravityModelMapping="removeAntigravityModelMapping"
  :addAntigravityModelMapping="addAntigravityModelMapping"
  :antigravityPresetMappings="antigravityPresetMappings"
  :addAntigravityPresetMapping="addAntigravityPresetMapping"
/>

      <!-- Authorization method (only for Anthropic OAuth-based type) -->
      <CreateAnthropicAuthorizationMethodPanel
  :form="{ platform: form.platform }"
  :isOAuthFlow="isOAuthFlow"
  :addMethod="addMethod"
  :oauth="oauth"
  @update:addMethod="addMethod = $event"
/>

      <!-- API Key input (only for apikey type, excluding Antigravity which has its own fields) -->
      <CreateApiKeyCredentialsPanel
  :form="{ type: form.type, platform: form.platform }"
  :isMultiProtocolPlatform="isMultiProtocolPlatform"
  :apiProtocol="apiProtocol"
  :apiKeyBaseUrl="apiKeyBaseUrl"
  :apiKeyBaseUrlPlaceholder="apiKeyBaseUrlPlaceholder"
  :baseUrlHint="baseUrlHint"
  :isCNPlatform="isCNPlatform"
  :isOpenCodeGoPlatform="isOpenCodeGoPlatform"
  :cnPresetPlatform="cnPresetPlatform"
  :cnPresetMode="cnPresetMode"
  :onCnPresetSelect="onCnPresetSelect"
  :cnAdaptiveProtocolOptions="cnAdaptiveProtocolOptions"
  :adaptiveBaseUrls="adaptiveBaseUrls"
  :cnSupportsNativeResponses="cnSupportsNativeResponses"
  :currentOpenCodeOrCNMode="currentOpenCodeOrCNMode"
  :routesByModel="routesByModel"
  :openCodeGoProtocolRules="openCodeGoProtocolRules"
  :apiKeyValue="apiKeyValue"
  :apiKeyValuePlaceholder="apiKeyValuePlaceholder"
  :apiKeyHint="apiKeyHint"
  :allowRateEdit="allowRateEdit"
  :upstreamBillingAutoProbeEnabled="upstreamBillingAutoProbeEnabled"
  :geminiTierAIStudio="geminiTierAIStudio"
  :isOpenAIModelRestrictionDisabled="isOpenAIModelRestrictionDisabled"
  :modelRestrictionMode="modelRestrictionMode"
  :allowedModels="allowedModels"
  :modelMappings="modelMappings"
  :getModelMappingKey="getModelMappingKey"
  :removeModelMapping="removeModelMapping"
  :addModelMapping="addModelMapping"
  :presetMappings="presetMappings"
  :addPresetMapping="addPresetMapping"
  :poolModeEnabled="poolModeEnabled"
  :poolModeRetryCount="poolModeRetryCount"
  :MAX_POOL_MODE_RETRY_COUNT="MAX_POOL_MODE_RETRY_COUNT"
  :DEFAULT_POOL_MODE_RETRY_COUNT="DEFAULT_POOL_MODE_RETRY_COUNT"
  :poolModeRetryStatusCodesInput="poolModeRetryStatusCodesInput"
  :DEFAULT_POOL_MODE_RETRY_STATUS_CODES="DEFAULT_POOL_MODE_RETRY_STATUS_CODES"
  :explicitCacheUpstreamEnabled="explicitCacheUpstreamEnabled"
  :explicitCacheUpstreamMaxTTL="explicitCacheUpstreamMaxTTL"
  :customErrorCodesEnabled="customErrorCodesEnabled"
  :toggleErrorCode="toggleErrorCode"
  :selectedErrorCodes="selectedErrorCodes"
  :customErrorCodeInput="customErrorCodeInput"
  :addCustomErrorCode="addCustomErrorCode"
  :removeErrorCode="removeErrorCode"
  :isHeaderOverrideCapable="isHeaderOverrideCapable"
  :headerOverrideEnabled="headerOverrideEnabled"
  :headerOverrideRows="headerOverrideRows"
  @update:apiKeyBaseUrl="apiKeyBaseUrl = $event"
  @update:adaptiveBaseUrls="adaptiveBaseUrls = $event"
  @update:openCodeGoProtocolRules="openCodeGoProtocolRules = $event"
  @update:apiKeyValue="apiKeyValue = $event"
  @update:upstreamBillingAutoProbeEnabled="upstreamBillingAutoProbeEnabled = $event"
  @update:geminiTierAIStudio="geminiTierAIStudio = $event"
  @update:modelRestrictionMode="modelRestrictionMode = $event"
  @update:allowedModels="allowedModels = $event"
  @update:poolModeEnabled="poolModeEnabled = $event"
  @update:poolModeRetryCount="poolModeRetryCount = $event"
  @update:poolModeRetryStatusCodesInput="poolModeRetryStatusCodesInput = $event"
  @update:explicitCacheUpstreamEnabled="explicitCacheUpstreamEnabled = $event"
  @update:explicitCacheUpstreamMaxTTL="explicitCacheUpstreamMaxTTL = $event"
  @update:customErrorCodesEnabled="customErrorCodesEnabled = $event"
  @update:customErrorCodeInput="customErrorCodeInput = $event"
  @update:headerOverrideEnabled="headerOverrideEnabled = $event"
  @update:headerOverrideRows="headerOverrideRows = $event"
/>

      <!-- Bedrock credentials (only for Anthropic Bedrock type) -->
      <CreateBedrockCredentialsPanel
  :form="{ platform: form.platform }"
  :accountCategory="accountCategory"
  :bedrockAuthMode="bedrockAuthMode"
  :bedrockAccessKeyId="bedrockAccessKeyId"
  :bedrockSecretAccessKey="bedrockSecretAccessKey"
  :bedrockSessionToken="bedrockSessionToken"
  :bedrockApiKeyValue="bedrockApiKeyValue"
  :bedrockRegion="bedrockRegion"
  :bedrockForceGlobal="bedrockForceGlobal"
  :modelRestrictionMode="modelRestrictionMode"
  :allowedModels="allowedModels"
  :modelMappings="modelMappings"
  :bedrockPresets="bedrockPresets"
  :addPresetMapping="addPresetMapping"
  :poolModeEnabled="poolModeEnabled"
  :poolModeRetryCount="poolModeRetryCount"
  :MAX_POOL_MODE_RETRY_COUNT="MAX_POOL_MODE_RETRY_COUNT"
  :DEFAULT_POOL_MODE_RETRY_COUNT="DEFAULT_POOL_MODE_RETRY_COUNT"
  :poolModeRetryStatusCodesInput="poolModeRetryStatusCodesInput"
  :DEFAULT_POOL_MODE_RETRY_STATUS_CODES="DEFAULT_POOL_MODE_RETRY_STATUS_CODES"
  @update:bedrockAuthMode="bedrockAuthMode = $event"
  @update:bedrockAccessKeyId="bedrockAccessKeyId = $event"
  @update:bedrockSecretAccessKey="bedrockSecretAccessKey = $event"
  @update:bedrockSessionToken="bedrockSessionToken = $event"
  @update:bedrockApiKeyValue="bedrockApiKeyValue = $event"
  @update:bedrockRegion="bedrockRegion = $event"
  @update:bedrockForceGlobal="bedrockForceGlobal = $event"
  @update:modelRestrictionMode="modelRestrictionMode = $event"
  @update:allowedModels="allowedModels = $event"
  @update:poolModeEnabled="poolModeEnabled = $event"
  @update:poolModeRetryCount="poolModeRetryCount = $event"
  @update:poolModeRetryStatusCodesInput="poolModeRetryStatusCodesInput = $event"
/>

      <!-- 配额控制 (Anthropic apikey/bedrock: 配额限制 + 亲和) -->
      <CreateQuotaLimitsPanel
  :form="{ platform: form.platform, type: form.type }"
  :allowRateEdit="allowRateEdit"
  :editQuotaLimit="editQuotaLimit"
  :editQuotaDailyLimit="editQuotaDailyLimit"
  :editQuotaWeeklyLimit="editQuotaWeeklyLimit"
  :quotaNotifyGlobalEnabled="quotaNotifyGlobalEnabled"
  :quotaNotifyState="quotaNotifyState"
  :editDailyResetMode="editDailyResetMode"
  :editDailyResetHour="editDailyResetHour"
  :editWeeklyResetMode="editWeeklyResetMode"
  :editWeeklyResetDay="editWeeklyResetDay"
  :editWeeklyResetHour="editWeeklyResetHour"
  :editResetTimezone="editResetTimezone"
  @update:editQuotaLimit="editQuotaLimit = $event"
  @update:editQuotaDailyLimit="editQuotaDailyLimit = $event"
  @update:editQuotaWeeklyLimit="editQuotaWeeklyLimit = $event"
  @update:quotaNotifyState="Object.assign(quotaNotifyState, $event)"
  @update:editDailyResetMode="editDailyResetMode = $event"
  @update:editDailyResetHour="editDailyResetHour = $event"
  @update:editWeeklyResetMode="editWeeklyResetMode = $event"
  @update:editWeeklyResetDay="editWeeklyResetDay = $event"
  @update:editWeeklyResetHour="editWeeklyResetHour = $event"
  @update:editResetTimezone="editResetTimezone = $event"
/>

      <!-- Grok OAuth Custom Upstream URL (仅改写转发端点，OAuth 授权/刷新不受影响) -->
      <CreateGrokUpstreamPanel
  :form="{ platform: form.platform }"
  :isOAuthFlow="isOAuthFlow"
  :grokOAuthCustomBaseUrlEnabled="grokOAuthCustomBaseUrlEnabled"
  :grokOAuthBaseUrl="grokOAuthBaseUrl"
  @update:grokOAuthCustomBaseUrlEnabled="grokOAuthCustomBaseUrlEnabled = $event"
  @update:grokOAuthBaseUrl="grokOAuthBaseUrl = $event"
/>

      <!-- Grok OAuth Header Override (OAuth 类型没有 apikey 容器，需要独立区域) -->
      <CreateRequestHeadersPanel
  :form="{ platform: form.platform }"
  :isOAuthFlow="isOAuthFlow"
  :headerOverrideEnabled="headerOverrideEnabled"
  :headerOverrideRows="headerOverrideRows"
  @update:headerOverrideEnabled="headerOverrideEnabled = $event"
  @update:headerOverrideRows="headerOverrideRows = $event"
/>

      <!-- OpenAI OAuth Model Mapping (OAuth 类型没有 apikey 容器，需要独立的模型映射区域) -->
      <CreateOAuthModelMappingPanel
  :form="{ platform: form.platform }"
  :isOAuthFlow="isOAuthFlow"
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

      <!-- Temp Unschedulable Rules -->
      <CreateTemporaryRestrictionsPanel
  :tempUnschedEnabled="tempUnschedEnabled"
  :tempUnschedPresets="tempUnschedPresets"
  :addTempUnschedRule="addTempUnschedRule"
  :tempUnschedRules="tempUnschedRules"
  :getTempUnschedRuleKey="getTempUnschedRuleKey"
  :moveTempUnschedRule="moveTempUnschedRule"
  :removeTempUnschedRule="removeTempUnschedRule"
  :tempUnschedScopeOptions="tempUnschedScopeOptions"
  @update:tempUnschedEnabled="tempUnschedEnabled = $event"
/>

      <!-- Intercept Warmup Requests (Anthropic/Antigravity) -->
      <CreateWarmupPolicyPanel
  :form="{ platform: form.platform }"
  :interceptWarmupRequests="interceptWarmupRequests"
  @update:interceptWarmupRequests="interceptWarmupRequests = $event"
/>

      <!-- 配额控制 (Anthropic OAuth/SetupToken: 亲和 + 窗口费用 + 会话 + RPM 等) -->
      <CreateAnthropicRuntimeLimitsPanel
  :form="{ platform: form.platform }"
  :accountCategory="accountCategory"
  :allowRateEdit="allowRateEdit"
  :windowCostEnabled="windowCostEnabled"
  :windowCostLimit="windowCostLimit"
  :windowCostStickyReserve="windowCostStickyReserve"
  :sessionLimitEnabled="sessionLimitEnabled"
  :maxSessions="maxSessions"
  :sessionIdleTimeout="sessionIdleTimeout"
  :rpmLimitEnabled="rpmLimitEnabled"
  :baseRpm="baseRpm"
  :rpmStrategy="rpmStrategy"
  :rpmStickyBuffer="rpmStickyBuffer"
  :umqModeOptions="umqModeOptions"
  :userMsgQueueMode="userMsgQueueMode"
  :tlsFingerprintEnabled="tlsFingerprintEnabled"
  :tlsFingerprintProfileId="tlsFingerprintProfileId"
  :tlsFingerprintProfiles="tlsFingerprintProfiles"
  :sessionIdMaskingEnabled="sessionIdMaskingEnabled"
  :cacheTTLOverrideEnabled="cacheTTLOverrideEnabled"
  :cacheTTLOverrideTarget="cacheTTLOverrideTarget"
  :customBaseUrlEnabled="customBaseUrlEnabled"
  :customBaseUrl="customBaseUrl"
  @update:windowCostEnabled="windowCostEnabled = $event"
  @update:windowCostLimit="windowCostLimit = $event"
  @update:windowCostStickyReserve="windowCostStickyReserve = $event"
  @update:sessionLimitEnabled="sessionLimitEnabled = $event"
  @update:maxSessions="maxSessions = $event"
  @update:sessionIdleTimeout="sessionIdleTimeout = $event"
  @update:rpmLimitEnabled="rpmLimitEnabled = $event"
  @update:baseRpm="baseRpm = $event"
  @update:rpmStrategy="rpmStrategy = $event"
  @update:rpmStickyBuffer="rpmStickyBuffer = $event"
  @update:userMsgQueueMode="userMsgQueueMode = $event"
  @update:tlsFingerprintEnabled="tlsFingerprintEnabled = $event"
  @update:tlsFingerprintProfileId="tlsFingerprintProfileId = $event"
  @update:sessionIdMaskingEnabled="sessionIdMaskingEnabled = $event"
  @update:cacheTTLOverrideEnabled="cacheTTLOverrideEnabled = $event"
  @update:cacheTTLOverrideTarget="cacheTTLOverrideTarget = $event"
  @update:customBaseUrlEnabled="customBaseUrlEnabled = $event"
  @update:customBaseUrl="customBaseUrl = $event"
/>

      <div>
        <div class="mb-1 flex items-center gap-2">
          <label class="input-label mb-0">{{ t('admin.accounts.proxy') }}</label>

        </div>
        <ProxySelector v-model="form.proxy_id" :proxies="proxies" />
      </div>

      <AccountTelemetryPanel
        v-if="authStore.isSuperAdmin && allowSystemSettings && (form.platform === 'anthropic' && (form.type === 'oauth' || form.type === 'setup-token'))"
        :key="`${form.platform}:${show}`"
      />

      <UpstreamRequestIdHeaderField
        v-model="upstreamRequestIdHeader"
        :platform="form.platform"
        :type="form.type"
      />

      <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <div>
          <label class="input-label">{{ t('admin.accounts.concurrency') }}</label>
          <input v-model.number="form.concurrency" type="number" min="1" class="input"
            @input="form.concurrency = Math.max(1, form.concurrency || 1)" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.loadFactor') }}</label>
          <input v-model.number="form.load_factor" type="number" min="1"
            class="input" :placeholder="String(form.concurrency || 1)"
            @input="form.load_factor = (form.load_factor &amp;&amp; form.load_factor >= 1) ? form.load_factor : null" />
          <p class="input-hint">{{ t('admin.accounts.loadFactorHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.priority') }}</label>
          <input
            v-model.number="form.priority"
            type="number"
            min="1"
            class="input"
            data-tour="account-form-priority"
          />
          <p class="input-hint">{{ t('admin.accounts.priorityHint') }}</p>
        </div>
        <div v-if="allowRateEdit">
          <label class="input-label">{{ t('admin.accounts.billingRateMultiplier') }}</label>
          <input v-model.number="form.rate_multiplier" type="number" min="0" step="0.001" class="input" />
          <p class="input-hint">{{ t('admin.accounts.billingRateMultiplierHint') }}</p>
        </div>
      </div>
      <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <label class="input-label">{{ t('admin.accounts.expiresAt') }}</label>
        <input v-model="expiresAtInput" type="datetime-local" class="input" />
        <div class="mt-2 flex gap-2">
          <button type="button" class="btn btn-secondary btn-sm" @click="form.expires_at = getAccountExpiryTimestamp(1)">
            {{ t('payment.oneMonth') }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm" @click="form.expires_at = getAccountExpiryTimestamp(12)">
            {{ t('payment.oneYear') }}
          </button>
        </div>
        <p class="input-hint">
          {{ t('admin.accounts.expiresAtHint') }}
          {{ t('admin.accounts.expiresAtTimezoneHint', { timezone: browserTimeZone }) }}
        </p>
      </div>

      <!-- OpenAI 自动透传开关（OAuth/API Key） -->
      <CreatePassthroughPanel
  :form="{ platform: form.platform }"
  :openaiPassthroughEnabled="openaiPassthroughEnabled"
  @update:openaiPassthroughEnabled="openaiPassthroughEnabled = $event"
/>

      <!-- OpenAI Codex namespace 工具摊平（兼容开关，仅 OAuth） -->
      <CreateNamespaceToolsPanel
  :form="{ platform: form.platform, type: form.type }"
  :openaiFlattenNamespacesEnabled="openaiFlattenNamespacesEnabled"
  @update:openaiFlattenNamespacesEnabled="openaiFlattenNamespacesEnabled = $event"
/>

      <!-- OpenAI WS Mode 三态（off/ctx_pool/passthrough） -->
      <CreateWebSocketTransportPanel
  :form="{ platform: form.platform }"
  :accountCategory="accountCategory"
  :openAIWSModeHintKey="openAIWSModeHintKey"
  :openaiResponsesWebSocketV2Mode="openaiResponsesWebSocketV2Mode"
  :openAIWSModeOptions="openAIWSModeOptions"
  @update:openaiResponsesWebSocketV2Mode="openaiResponsesWebSocketV2Mode = $event"
/>

      <!-- Anthropic API Key 自动透传开关 -->
      <CreatePassthrough2Panel
  :form="{ platform: form.platform }"
  :accountCategory="accountCategory"
  :anthropicPassthroughEnabled="anthropicPassthroughEnabled"
  @update:anthropicPassthroughEnabled="anthropicPassthroughEnabled = $event"
/>

      <div
        v-if="form.platform === 'anthropic' && accountCategory === 'apikey'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between gap-4">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.anthropic.apiKeyAuthScheme') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.anthropic.apiKeyAuthSchemeDesc') }}
            </p>
          </div>
          <select v-model="anthropicAPIKeyAuthScheme" class="input w-52 text-sm">
            <option value="x_api_key">{{ t('admin.accounts.anthropic.apiKeyAuthSchemeXApiKey') }}</option>
            <option value="authorization_bearer">{{ t('admin.accounts.anthropic.apiKeyAuthSchemeBearer') }}</option>
          </select>
        </div>
      </div>

      <!-- Anthropic API Key: Web Search Emulation (hidden when global disabled) -->
      <CreateWebSearchPanel
  :form="{ platform: form.platform }"
  :accountCategory="accountCategory"
  :webSearchGlobalEnabled="webSearchGlobalEnabled"
  :webSearchEmulationMode="webSearchEmulationMode"
  @update:webSearchEmulationMode="webSearchEmulationMode = $event"
/>

      <!-- OpenAI API 长上下文计费开关 -->
      <CreateLongContextBillingPanel
  :allowRateEdit="allowRateEdit"
  :form="{ platform: form.platform }"
  :hideAccountLongContextBilling="hideAccountLongContextBilling"
  :accountCategory="accountCategory"
  :openAILongContextBillingEnabled="openAILongContextBillingEnabled"
  :toggleOpenAILongContextBilling="toggleOpenAILongContextBilling"
/>

      <div
        v-if="form.platform === 'openai' && accountCategory === 'oauth-based'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.codexCLIOnly') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.codexCLIOnlyDesc') }}
            </p>
          </div>
          <button
            type="button"
            @click="codexCLIOnlyEnabled = !codexCLIOnlyEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              codexCLIOnlyEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                codexCLIOnlyEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
        <div
          v-if="codexCLIOnlyEnabled"
          class="mt-4 flex items-center justify-between border-l-2 border-gray-200 pl-4 dark:border-dark-600"
        >
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.codexCLIOnlyAppServer') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.codexCLIOnlyAppServerDesc') }}
            </p>
          </div>
          <button
            type="button"
            @click="codexCLIOnlyAppServerEnabled = !codexCLIOnlyAppServerEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              codexCLIOnlyAppServerEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                codexCLIOnlyAppServerEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <!-- Codex 指纹收敛模式（仅 OpenAI OAuth） -->
      <CreateCodexFingerprintPanel
  :form="{ platform: form.platform }"
  :accountCategory="accountCategory"
  :codexFingerprintMode="codexFingerprintMode"
  :codexFingerprintModeOptions="codexFingerprintModeOptions"
  @update:codexFingerprintMode="codexFingerprintMode = $event"
/>

      <!-- OpenAI Compact 能力配置 -->
      <CreateCompactCapabilityPanel
  :form="{ platform: form.platform }"
  :accountCategory="accountCategory"
  :openAICompactMode="openAICompactMode"
  :openAICompactModeOptions="openAICompactModeOptions"
  :openAICompactModelMappings="openAICompactModelMappings"
  :getOpenAICompactModelMappingKey="getOpenAICompactModelMappingKey"
  :removeOpenAICompactModelMapping="removeOpenAICompactModelMapping"
  :addOpenAICompactModelMapping="addOpenAICompactModelMapping"
  @update:openAICompactMode="openAICompactMode = $event"
/>

      <!-- OpenAI APIKey Responses API support mode -->
      <CreateResponsesCapabilityPanel
  :form="{ platform: form.platform }"
  :accountCategory="accountCategory"
  :openAIResponsesMode="openAIResponsesMode"
  :openAIResponsesModeOptions="openAIResponsesModeOptions"
  :openAITextGenerationCapabilityEnabled="openAITextGenerationCapabilityEnabled"
  :openAIEndpointCapabilityOptions="openAIEndpointCapabilityOptions"
  :openAIEndpointCapabilities="openAIEndpointCapabilities"
  :toggleOpenAIEndpointCapability="toggleOpenAIEndpointCapability"
  @update:openAIResponsesMode="openAIResponsesMode = $event"
/>

      <!-- OpenAI APIKey images: backfill b64_json from url -->
      <CreateImageResponseFormatPanel
  :form="{ platform: form.platform }"
  :accountCategory="accountCategory"
  :openAIImagesUrlToB64JsonEnabled="openAIImagesUrlToB64JsonEnabled"
  @update:openAIImagesUrlToB64JsonEnabled="openAIImagesUrlToB64JsonEnabled = $event"
/>

      <div>
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{
              t('admin.accounts.autoPauseOnExpired')
            }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.autoPauseOnExpiredDesc') }}
            </p>
          </div>
          <button
            type="button"
            @click="autoPauseOnExpired = !autoPauseOnExpired"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              autoPauseOnExpired ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                autoPauseOnExpired ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <!-- Mixed Scheduling (only for antigravity accounts) -->
        <div v-if="form.platform === 'antigravity'" class="flex items-center gap-2">
          <label class="flex cursor-pointer items-center gap-2">
            <input
              type="checkbox"
              v-model="mixedScheduling"
              class="h-4 w-4 rounded border-gray-300 text-primary-500 focus:ring-primary-500 dark:border-dark-500"
            />
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.accounts.mixedScheduling') }}
            </span>
          </label>
          <div class="group relative">
            <span
              class="inline-flex h-4 w-4 cursor-help items-center justify-center rounded-full bg-gray-200 text-xs text-gray-500 hover:bg-gray-300 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500"
            >
              ?
            </span>
            <!-- Tooltip（向下显示避免被弹窗裁剪） -->
            <div
              class="pointer-events-none absolute left-0 top-full z-[100] mt-1.5 w-72 rounded bg-gray-900 px-3 py-2 text-xs text-white opacity-0 transition-opacity group-hover:opacity-100 dark:bg-gray-700"
            >
              {{ t('admin.accounts.mixedSchedulingTooltip') }}
              <div
                class="absolute bottom-full left-3 border-4 border-transparent border-b-gray-900 dark:border-b-gray-700"
              ></div>
            </div>
          </div>
        </div>
        <div v-if="allowRateEdit && form.platform === 'antigravity'" class="mt-3 flex items-center gap-2">
          <label class="flex cursor-pointer items-center gap-2">
            <input
              type="checkbox"
              v-model="allowOverages"
              class="h-4 w-4 rounded border-gray-300 text-primary-500 focus:ring-primary-500 dark:border-dark-500"
            />
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.accounts.allowOverages') }}
            </span>
          </label>
          <div class="group relative">
            <span
              class="inline-flex h-4 w-4 cursor-help items-center justify-center rounded-full bg-gray-200 text-xs text-gray-500 hover:bg-gray-300 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500"
            >
              ?
            </span>
            <div
              class="pointer-events-none absolute left-0 top-full z-[100] mt-1.5 w-72 rounded bg-gray-900 px-3 py-2 text-xs text-white opacity-0 transition-opacity group-hover:opacity-100 dark:bg-gray-700"
            >
              {{ t('admin.accounts.allowOveragesTooltip') }}
              <div
                class="absolute bottom-full left-3 border-4 border-transparent border-b-gray-900 dark:border-b-gray-700"
              ></div>
            </div>
          </div>
        </div>

        <!-- Group Selection - 仅标准模式显示 -->
        <GroupSelector
          v-model="form.group_ids"
          :groups="groups"
          :platform="form.platform"
          :mixed-scheduling="mixedScheduling"
          data-tour="account-form-groups"
        />
      </div>

    </form>

    <!-- Step 2: OAuth Authorization -->
    <div v-else class="space-y-5">
      <OAuthAuthorizationFlow
        ref="oauthFlowRef"
        :add-method="form.platform === 'anthropic' ? addMethod : 'oauth'"
        :auth-url="currentAuthUrl"
        :session-id="currentSessionId"
        :loading="currentOAuthLoading"
        :error="currentOAuthError"
        :show-help="form.platform === 'anthropic'"
        :show-proxy-warning="form.platform !== 'openai' && form.platform !== 'grok' && !!form.proxy_id"
        :allow-multiple="form.platform === 'anthropic'"
        :show-cookie-option="form.platform === 'anthropic' && addMethod !== 'setup-token-manual'"
        :show-refresh-token-option="form.platform === 'openai' || form.platform === 'antigravity' || form.platform === 'grok'"
        :show-mobile-refresh-token-option="form.platform === 'openai'"
        :show-session-token-option="false"
        :show-access-token-option="false"
        :show-codex-session-import-option="form.platform === 'openai'"
        :show-agent-identity-option="form.platform === 'openai'"
        :show-codex-pat-option="form.platform === 'openai'"
        :show-sso-option="form.platform === 'grok'"
        :show-email-password-option="false"
        :show-manual-option="addMethod !== 'setup-token-manual'"
        :show-setup-token-option="form.platform === 'anthropic' && addMethod === 'setup-token-manual'"
        :initial-input-method="form.platform === 'anthropic' && addMethod === 'setup-token-manual' ? 'setup_token' : 'manual'"
        :platform="form.platform"
        :show-project-id="geminiOAuthType === 'code_assist'"
        @generate-url="handleGenerateUrl"
        @cookie-auth="handleCookieAuth"
        @validate-refresh-token="handleValidateRefreshToken"
        @validate-mobile-refresh-token="handleOpenAIValidateMobileRT"
        @validate-session-token="handleValidateSessionToken"
        @import-codex-session="handleOpenAIImportCodexSession"
        @import-codex-pat="handleOpenAIImportCodexPAT"
        @import-sso="handleGrokImportSSO"
        @authorize-password="handleGrokAuthorizePassword"
      />

    </div>

    <template #footer>
      <div v-if="step === 1" class="flex justify-end gap-3">
        <button @click="handleClose" type="button" class="btn btn-secondary">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="create-account-form"
          :disabled="submitting || isLoadingBuiltinModels"
          class="btn btn-primary"
          data-tour="account-form-submit"
        >
          <svg
            v-if="submitting"
            class="-ml-1 mr-2 h-4 w-4 animate-spin"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            ></circle>
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
          {{
            isOAuthFlow
              ? t('common.next')
              : submitting
                ? t('admin.accounts.creating')
                : t('common.create')
          }}
        </button>
      </div>
      <div v-else class="flex justify-between gap-3">
        <button type="button" class="btn btn-secondary" @click="goBackToBasicInfo">
          {{ t('common.back') }}
        </button>
        <button
          v-if="isManualInputMethod"
          type="button"
          :disabled="!canExchangeCode"
          class="btn btn-primary"
          @click="handleExchangeCode"
        >
          <svg
            v-if="currentOAuthLoading"
            class="-ml-1 mr-2 h-4 w-4 animate-spin"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            ></circle>
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
          {{
            currentOAuthLoading
              ? t('admin.accounts.oauth.verifying')
              : t('admin.accounts.oauth.completeAuth')
          }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <!-- Gemini Help Dialog -->
  <BaseDialog
    :show="showGeminiHelpDialog"
    :title="t('admin.accounts.gemini.helpDialog.title')"
    width="wide"
    @close="showGeminiHelpDialog = false"
  >
    <div class="space-y-6">
      <!-- Setup Guide Section -->
      <div>
        <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('admin.accounts.gemini.setupGuide.title') }}
        </h3>
        <div class="space-y-4">
          <div>
            <p class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.accounts.gemini.setupGuide.checklistTitle') }}
            </p>
            <ul class="list-inside list-disc space-y-1 text-sm text-gray-600 dark:text-gray-400">
              <li>{{ t('admin.accounts.gemini.setupGuide.checklistItems.usIp') }}</li>
              <li>{{ t('admin.accounts.gemini.setupGuide.checklistItems.age') }}</li>
            </ul>
          </div>
          <div>
            <p class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.accounts.gemini.setupGuide.activationTitle') }}
            </p>
            <ul class="list-inside list-disc space-y-1 text-sm text-gray-600 dark:text-gray-400">
              <li>{{ t('admin.accounts.gemini.setupGuide.activationItems.geminiWeb') }}</li>
              <li>{{ t('admin.accounts.gemini.setupGuide.activationItems.gcpProject') }}</li>
            </ul>
            <div class="mt-2 flex flex-wrap gap-2">
              <a
                href="https://policies.google.com/terms"
                target="_blank"
                rel="noreferrer"
                class="text-sm text-blue-600 hover:underline dark:text-blue-400"
              >
                {{ t('admin.accounts.gemini.setupGuide.links.countryCheck') }}
              </a>
              <span class="text-gray-400">·</span>
              <a
                href="https://policies.google.com/country-association-form"
                target="_blank"
                rel="noreferrer"
                class="text-sm text-blue-600 hover:underline dark:text-blue-400"
              >
                {{ t('admin.accounts.gemini.setupGuide.links.countryChange') }}
              </a>
              <span class="text-gray-400">·</span>
              <a
                href="https://gemini.google.com/gems/create?hl=en-US&pli=1"
                target="_blank"
                rel="noreferrer"
                class="text-sm text-blue-600 hover:underline dark:text-blue-400"
              >
                {{ t('admin.accounts.gemini.setupGuide.links.geminiWebActivation') }}
              </a>
              <span class="text-gray-400">·</span>
              <a
                href="https://console.cloud.google.com"
                target="_blank"
                rel="noreferrer"
                class="text-sm text-blue-600 hover:underline dark:text-blue-400"
              >
                {{ t('admin.accounts.gemini.setupGuide.links.gcpProject') }}
              </a>
            </div>
          </div>
        </div>
      </div>

      <!-- Quota Policy Section -->
      <div class="border-t border-gray-200 pt-6 dark:border-dark-600">
        <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('admin.accounts.gemini.quotaPolicy.title') }}
        </h3>
        <p class="mb-4 text-xs text-amber-600 dark:text-amber-400">
          {{ t('admin.accounts.gemini.quotaPolicy.note') }}
        </p>
        <div class="overflow-x-auto">
          <table class="w-full text-xs">
            <thead class="bg-gray-50 dark:bg-dark-600">
              <tr>
                <th class="px-3 py-2 text-left font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.accounts.gemini.quotaPolicy.columns.channel') }}
                </th>
                <th class="px-3 py-2 text-left font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.accounts.gemini.quotaPolicy.columns.account') }}
                </th>
                <th class="px-3 py-2 text-left font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.accounts.gemini.quotaPolicy.columns.limits') }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-600">
              <tr>
                <td class="px-3 py-2 text-gray-900 dark:text-white">
                  {{ t('admin.accounts.gemini.quotaPolicy.rows.googleOne.channel') }}
                </td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">Free</td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">
                  {{ t('admin.accounts.gemini.quotaPolicy.rows.googleOne.limitsFree') }}
                </td>
              </tr>
              <tr>
                <td class="px-3 py-2 text-gray-900 dark:text-white"></td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">Pro</td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">
                  {{ t('admin.accounts.gemini.quotaPolicy.rows.googleOne.limitsPro') }}
                </td>
              </tr>
              <tr>
                <td class="px-3 py-2 text-gray-900 dark:text-white"></td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">Ultra</td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">
                  {{ t('admin.accounts.gemini.quotaPolicy.rows.googleOne.limitsUltra') }}
                </td>
              </tr>
              <tr>
                <td class="px-3 py-2 text-gray-900 dark:text-white">
                  {{ t('admin.accounts.gemini.quotaPolicy.rows.gcp.channel') }}
                </td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">Standard</td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">
                  {{ t('admin.accounts.gemini.quotaPolicy.rows.gcp.limitsStandard') }}
                </td>
              </tr>
              <tr>
                <td class="px-3 py-2 text-gray-900 dark:text-white"></td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">Enterprise</td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">
                  {{ t('admin.accounts.gemini.quotaPolicy.rows.gcp.limitsEnterprise') }}
                </td>
              </tr>
              <tr>
                <td class="px-3 py-2 text-gray-900 dark:text-white">
                  {{ t('admin.accounts.gemini.quotaPolicy.rows.aiStudio.channel') }}
                </td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">Free</td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">
                  {{ t('admin.accounts.gemini.quotaPolicy.rows.aiStudio.limitsFree') }}
                </td>
              </tr>
              <tr>
                <td class="px-3 py-2 text-gray-900 dark:text-white"></td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">Paid</td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-400">
                  {{ t('admin.accounts.gemini.quotaPolicy.rows.aiStudio.limitsPaid') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="mt-4 flex flex-wrap gap-3">
          <a
            :href="geminiQuotaDocs.codeAssist"
            target="_blank"
            rel="noreferrer"
            class="text-sm text-blue-600 hover:underline dark:text-blue-400"
          >
            {{ t('admin.accounts.gemini.quotaPolicy.docs.codeAssist') }}
          </a>
          <a
            :href="geminiQuotaDocs.aiStudio"
            target="_blank"
            rel="noreferrer"
            class="text-sm text-blue-600 hover:underline dark:text-blue-400"
          >
            {{ t('admin.accounts.gemini.quotaPolicy.docs.aiStudio') }}
          </a>
          <a
            :href="geminiQuotaDocs.vertex"
            target="_blank"
            rel="noreferrer"
            class="text-sm text-blue-600 hover:underline dark:text-blue-400"
          >
            {{ t('admin.accounts.gemini.quotaPolicy.docs.vertex') }}
          </a>
        </div>
      </div>

      <!-- API Key Links Section -->
      <div class="border-t border-gray-200 pt-6 dark:border-dark-600">
        <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('admin.accounts.gemini.helpDialog.apiKeySection') }}
        </h3>
        <div class="flex flex-wrap gap-3">
          <a
            :href="geminiHelpLinks.apiKey"
            target="_blank"
            rel="noreferrer"
            class="text-sm text-blue-600 hover:underline dark:text-blue-400"
          >
            {{ t('admin.accounts.gemini.accountType.apiKeyLink') }}
          </a>
          <a
            :href="geminiHelpLinks.aiStudioPricing"
            target="_blank"
            rel="noreferrer"
            class="text-sm text-blue-600 hover:underline dark:text-blue-400"
          >
            {{ t('admin.accounts.gemini.accountType.quotaLink') }}
          </a>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button @click="showGeminiHelpDialog = false" type="button" class="btn btn-primary">
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <!-- Mixed Channel Warning Dialog -->
  <ConfirmDialog
    :show="showMixedChannelWarning"
    :title="t('admin.accounts.mixedChannelWarningTitle')"
    :message="mixedChannelWarningMessageText"
    :confirm-text="t('common.confirm')"
    :cancel-text="t('common.cancel')"
    :danger="true"
    @confirm="handleMixedChannelConfirm"
    @cancel="handleMixedChannelCancel"
  />


</template>
