<script setup lang="ts">
import ResourceIdentityPanel from '../panels/ResourceIdentityPanel.vue'
import { createIdentityDraft } from '../model/identityDraft'

import { MAX_BPS_RECOVERY_INTERVAL_MINUTES } from '@/features/resources/model/excelBPSRecovery'
import { readAccountTelemetry } from '@/features/resources/model/accountTelemetry'
import { getAccountExpiryTimestamp } from '@/features/resources/model/accountExpiry'
import { VERTEX_LOCATION_OPTIONS } from '@/features/resources/model/accountConstants'
import EditApiKeyCredentialsPanel from '../panels/EditApiKeyCredentialsPanel.vue'
import EditGrokPromptCachePanel from '../panels/EditGrokPromptCachePanel.vue'
import EditGrokMediaEligibilityPanel from '../panels/EditGrokMediaEligibilityPanel.vue'
import EditGrokUpstreamPanel from '../panels/EditGrokUpstreamPanel.vue'
import EditRequestHeadersPanel from '../panels/EditRequestHeadersPanel.vue'
import EditOAuthModelMappingPanel from '../panels/EditOAuthModelMappingPanel.vue'
import EditUpstreamCredentialsPanel from '../panels/EditUpstreamCredentialsPanel.vue'
import EditBedrockCredentialsPanel from '../panels/EditBedrockCredentialsPanel.vue'
import EditAntigravityModelMappingPanel from '../panels/EditAntigravityModelMappingPanel.vue'
import EditTemporaryRestrictionsPanel from '../panels/EditTemporaryRestrictionsPanel.vue'
import EditWarmupPolicyPanel from '../panels/EditWarmupPolicyPanel.vue'
import EditPassthroughPanel from '../panels/EditPassthroughPanel.vue'
import EditNamespaceToolsPanel from '../panels/EditNamespaceToolsPanel.vue'
import EditImageToolPolicyPanel from '../panels/EditImageToolPolicyPanel.vue'
import EditWebSocketTransportPanel from '../panels/EditWebSocketTransportPanel.vue'
import EditResponsesCapabilityPanel from '../panels/EditResponsesCapabilityPanel.vue'
import EditImageResponseFormatPanel from '../panels/EditImageResponseFormatPanel.vue'
import EditPassthrough2Panel from '../panels/EditPassthrough2Panel.vue'
import EditWebSearchPanel from '../panels/EditWebSearchPanel.vue'
import EditQuotaLimitsPanel from '../panels/EditQuotaLimitsPanel.vue'
import EditLongContextBillingPanel from '../panels/EditLongContextBillingPanel.vue'
import EditCodexFingerprintPanel from '../panels/EditCodexFingerprintPanel.vue'
import EditSubscriptionDisplayPanel from '../panels/EditSubscriptionDisplayPanel.vue'
import EditAnthropicRuntimeLimitsPanel from '../panels/EditAnthropicRuntimeLimitsPanel.vue'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { useResourceEditorServices } from '@/features/resources/services'
import { reactive, watch, nextTick, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import ExcelBPSAuthorization from '@/features/resources/components/ExcelBPSAuthorization.vue'
import CloudGatewaySettings from '@/features/resources/components/CloudGatewaySettings.vue'
import type { Account } from '@/features/resources/api/accountTypes';
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import Select from '@/shared/ui/Select.vue'
import UpstreamRequestIdHeaderField from '@/features/resources/components/UpstreamRequestIdHeaderField.vue'
import AccountTelemetryPanel from '@/features/resources/components/AccountTelemetryPanel.vue'
import Toggle from '@/shared/ui/Toggle.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import ProxySelector from '@/features/resources/components/ResourceProxySelector.vue'
import GroupSelector from '@/features/resources/components/ResourceGroupSelector.vue'
import ModelWhitelistSelector from '@/features/resources/components/ModelWhitelistSelector.vue'
import OllamaCloudUsageSettings from '@/features/resources/components/OllamaCloudUsageSettings.vue'
import { formatDateTime, getBrowserTimeZone } from '@/shared/lib/format'
import { useEditIdentityDraft } from '../model/editIdentity'
import { useEditModelsDraft } from '../model/editModels'
import { useEditTelemetryDraft } from '../model/editTelemetry'
import { useEditCredentialsDraft } from '../model/editCredentials'
import { useEditSubmissionDraft } from '../model/editSubmission'
import { useEditProviderDraft } from '../model/editProvider'
import { useEditHydrationDraft } from '../model/editHydration'
import { useEditRestrictionsDraft } from '../model/editRestrictions'
import { useEditTransportDraft } from '../model/editTransport'
import { useEditAuthorizationDraft } from '../model/editAuthorization'
import { useEditLimitsDraft } from '../model/editLimits'
import { useEditAssociationDraft } from '../model/editAssociation'
import type { Props } from '../model/editEditorTypes'


const { cloneOpenCodeGoProtocolRules, defaultOpenCodeProtocolRules, defaultProviderProtocolRules, isMultiProtocolApiKeyPlatform, parseOpenCodeGoProtocolRules, providerAccountModes, providerNativeProtocols, providerRoutesByModel, resolveProviderAccountMode, isHeaderOverrideCapable, cnSupportsNativeResponses, defaultCNAdaptiveBaseUrls, defaultCNBaseUrl } = createProviderCredentialOptions(useResourceEditorServices().catalog)


const resourceServices = useResourceEditorServices()


const getPresetMappingsByPlatform = useResourceEditorServices().modelPresets


let capturedBaselineID: number | undefined
const props = withDefaults(defineProps<Props>(), { allowRateEdit: true, allowAuthorize: true, allowTransportEdit: true, showCost: true })


const resourceTasks = useResourceTaskScope(() => [props.show, props.account?.id])


const emit = defineEmits<{
  close: []
  updated: [account: Account]
  baseline: [payload: Record<string, unknown>]
}>()


const { t } = useI18n()


const appStore = useResourceEditorServices().notices


const authStore = useResourceEditorServices().access


const browserTimeZone = getBrowserTimeZone()


const form = reactive({
  ...createIdentityDraft(),
  proxy_id: null as number | null,
  concurrency: 1,
  load_factor: null as number | null,
  priority: 1,
  rate_multiplier: 1,
  status: 'active' as Account['status'],
  group_ids: [] as number[],
  expires_at: null as number | null
})


const { selectableGroups, isSparkShadow, setOpenCodeGoAutoRefresh, onCnPresetSelect, GROK_CLIENT_TOOL_CACHE_EXTRA_KEY, isGrokOAuthAccount, isSyncingAntigravityUpstream, ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY, ANTHROPIC_FABLE_SCHEDULING_THRESHOLD_CREDENTIAL_KEY, mixedChannelWarningDetails, mixedChannelWarningRawMessage, mixedChannelWarningAction, editPlanType, planTypeOptions, mixedChannelWarningMessageText, statusOptions, expiresAtInput, toPositiveNumber } = useEditIdentityDraft({
  get resourceTasks() { return resourceTasks },
  get props() { return props },
  get opencodeGoSaving() { return opencodeGoSaving },
  get applyOpenCodeGoState() { return applyOpenCodeGoState },
  get appStore() { return appStore },
  get t() { return t },
  get editAccountMode() { return editAccountMode },
  get editApiProtocol() { return editApiProtocol },
  get editBaseUrl() { return editBaseUrl },
  get form() { return form }
})


const { hideAccountLongContextBilling, antigravityPresetMappings, bedrockPresets, modelMappings, openAICompactModelMappings, modelRestrictionMode, hasOAuthModelRestriction, allowedModels, antigravityModelRestrictionMode, antigravityWhitelistModels, antigravityModelMappings, getModelMappingKey, getOpenAICompactModelMappingKey, getAntigravityModelMappingKey, excelBPSModels, openAILongContextBillingEnabled, isOpenAIModelRestrictionDisabled, presetMappings, loadModelRestrictionFromMapping, buildModelRestrictionMapping, applyOpenAIModelMappingCredentials, addModelMapping, removeModelMapping, addPresetMapping, addAntigravityModelMapping, addOpenAICompactModelMapping, removeOpenAICompactModelMapping, removeAntigravityModelMapping, addAntigravityPresetMapping, syncAntigravityUpstreamModels } = useEditModelsDraft({
  get resourceTasks() { return resourceTasks },
  get form() { return form },
  get props() { return props },
  get getPresetMappingsByPlatform() { return getPresetMappingsByPlatform },
  get openaiPassthroughEnabled() { return openaiPassthroughEnabled },
  get appStore() { return appStore },
  get t() { return t },
  get isSyncingAntigravityUpstream() { return isSyncingAntigravityUpstream }
})


const { handleOllamaCloudUsageUpdated, opencodeGoState, opencodeGoLoading, opencodeGoSaving, opencodeGoRefreshing, opencodeGoSnapshot, opencodeGoStatusOk, opencodeGoStatusLabel, opencodeGoFormatDate, opencodeGoWindowSummary, applyOpenCodeGoState, loadOpenCodeGoUsage, refreshOpenCodeGoUsage, upstreamBillingAutoProbeEnabled, upstreamBillingRateSyncEnabled, upstreamRequestIdHeader, readUpstreamRequestIdHeader, handleUpstreamBillingRateSyncChange, handleUpstreamBillingAutoProbeChange } = useEditTelemetryDraft({
  get resourceTasks() { return resourceTasks },
  get props() { return props },
  get emit() { return emit },
  get t() { return t },
  get appStore() { return appStore }
})


const { baseUrlHint, editBaseUrl, editApiKey, editBedrockAccessKeyId, editBedrockSecretAccessKey, editBedrockSessionToken, editBedrockRegion, editBedrockForceGlobal, editBedrockApiKeyValue, editVertexProjectId, editVertexClientEmail, editVertexLocation, editVertexApiKey, isVertexAPIKeyMode, isBedrockAPIKeyMode, antigravityProjectId, defaultBaseUrl } = useEditCredentialsDraft({
  get props() { return props },
  get t() { return t },
  get isMultiProtocolApiKeyPlatform() { return isMultiProtocolApiKeyPlatform },
  get defaultCNBaseUrl() { return defaultCNBaseUrl },
  get currentOpenCodeOrCNMode() { return currentOpenCodeOrCNMode },
  get editApiProtocol() { return editApiProtocol }
})


const { bpsStateConflict, submitting, handleClose, buildAccountUpdatePayload, handleSubmit } = useEditSubmissionDraft({
  get canManageModels() { return resourceServices.access.canManage && resourceServices.access.canAuthorize && (resourceServices.access.canEditAdminField?.('account.update', 'credentials') ?? resourceServices.access.isSuperAdmin) },
  get resourceTasks() { return resourceTasks },
  get antigravityMixedChannelConfirmed() { return antigravityMixedChannelConfirmed },
  get clearMixedChannelDialog() { return clearMixedChannelDialog },
  get emit() { return emit },
  get props() { return props },
  get withAntigravityConfirmFlag() { return withAntigravityConfirmFlag },
  get persistGrokMediaEligibility() { return persistGrokMediaEligibility },
  get appStore() { return appStore },
  get t() { return t },
  get needsMixedChannelCheck() { return needsMixedChannelCheck },
  get openMixedChannelDialog() { return openMixedChannelDialog },
  get form() { return form },
  get editApiKey() { return editApiKey },
  get isSparkShadow() { return isSparkShadow },
  get excelBPSEnabled() { return excelBPSEnabled },
  get excelBPSSystemDisabled() { return excelBPSSystemDisabled },
  get excelBPSStopRequested() { return excelBPSStopRequested },
  get excelBPSSwitchTouched() { return excelBPSSwitchTouched },
  get excelBPSExpectedDisabledAt() { return excelBPSExpectedDisabledAt },
  get excelBPSAutoDisableOn403() { return excelBPSAutoDisableOn403 },
  get excelBPSAutoRecoverOn403() { return excelBPSAutoRecoverOn403 },
  get excelBPSRecoveryIntervalMinutes() { return excelBPSRecoveryIntervalMinutes },
  get excelBPSAutoMoveOn403() { return excelBPSAutoMoveOn403 },
  get excelBPS403TargetGroupID() { return excelBPS403TargetGroupID },
  get excelBPS403GroupOptions() { return excelBPS403GroupOptions },
  get autoResetCreditEnabled() { return autoResetCreditEnabled },
  get autoResetCredit5hThreshold() { return autoResetCredit5hThreshold },
  get autoResetCredit7dThreshold() { return autoResetCredit7dThreshold },
  get autoPauseOnExpired() { return autoPauseOnExpired },
  get upstreamBillingAutoProbeEnabled() { return upstreamBillingAutoProbeEnabled },
  get upstreamBillingRateSyncEnabled() { return upstreamBillingRateSyncEnabled },
  get editBaseUrl() { return editBaseUrl },
  get defaultBaseUrl() { return defaultBaseUrl },
  get openaiPassthroughEnabled() { return openaiPassthroughEnabled },
  get isCNApiKeyAccount() { return isCNApiKeyAccount },
  get currentOpenCodeOrCNMode() { return currentOpenCodeOrCNMode },
  get editApiProtocol() { return editApiProtocol },
  get defaultCNAdaptiveBaseUrls() { return defaultCNAdaptiveBaseUrls },
  get adaptivePresetPlatform() { return adaptivePresetPlatform },
  get editAdaptiveProtocolOptions() { return editAdaptiveProtocolOptions },
  get editAdaptiveBaseUrls() { return editAdaptiveBaseUrls },
  get providerRoutesByModel() { return providerRoutesByModel },
  get editOpenCodeGoProtocolRules() { return editOpenCodeGoProtocolRules },
  get editZhipuOrganization() { return editZhipuOrganization },
  get editZhipuProject() { return editZhipuProject },
  get allowedModels() { return allowedModels },
  get modelMappings() { return modelMappings },
  get buildModelRestrictionMapping() { return buildModelRestrictionMapping },
  get applyOpenAIEndpointCapabilities() { return applyOpenAIEndpointCapabilities },
  get openAICompactModelMappings() { return openAICompactModelMappings },
  get poolModeEnabled() { return poolModeEnabled },
  get normalizePoolModeRetryCount() { return normalizePoolModeRetryCount },
  get poolModeRetryCount() { return poolModeRetryCount },
  get parsePoolModeRetryStatusCodes() { return parsePoolModeRetryStatusCodes },
  get poolModeRetryStatusCodesInput() { return poolModeRetryStatusCodesInput },
  get explicitCacheUpstreamEnabled() { return explicitCacheUpstreamEnabled },
  get explicitCacheUpstreamMaxTTL() { return explicitCacheUpstreamMaxTTL },
  get customErrorCodesEnabled() { return customErrorCodesEnabled },
  get selectedErrorCodes() { return selectedErrorCodes },
  get isHeaderOverrideCapable() { return isHeaderOverrideCapable },
  get headerOverrideEnabled() { return headerOverrideEnabled },
  get headerOverrideRows() { return headerOverrideRows },
  get interceptWarmupRequests() { return interceptWarmupRequests },
  get applyAccountSchedulingThresholdOverridePatch() { return applyAccountSchedulingThresholdOverridePatch },
  get applyTempUnschedConfig() { return applyTempUnschedConfig },
  get isVertexAPIKeyMode() { return isVertexAPIKeyMode },
  get editVertexApiKey() { return editVertexApiKey },
  get editVertexProjectId() { return editVertexProjectId },
  get editVertexLocation() { return editVertexLocation },
  get editVertexClientEmail() { return editVertexClientEmail },
  get editBedrockRegion() { return editBedrockRegion },
  get editBedrockForceGlobal() { return editBedrockForceGlobal },
  get isBedrockAPIKeyMode() { return isBedrockAPIKeyMode },
  get editBedrockApiKeyValue() { return editBedrockApiKeyValue },
  get editBedrockAccessKeyId() { return editBedrockAccessKeyId },
  get editBedrockSecretAccessKey() { return editBedrockSecretAccessKey },
  get editBedrockSessionToken() { return editBedrockSessionToken },
  get hasOAuthModelRestriction() { return hasOAuthModelRestriction },
  get applyOpenAIModelMappingCredentials() { return applyOpenAIModelMappingCredentials },
  get grokOAuthCustomBaseUrlEnabled() { return grokOAuthCustomBaseUrlEnabled },
  get grokOAuthBaseUrl() { return grokOAuthBaseUrl },
  get GROK_CLIENT_TOOL_CACHE_EXTRA_KEY() { return GROK_CLIENT_TOOL_CACHE_EXTRA_KEY },
  get grokClientToolCacheEnabled() { return grokClientToolCacheEnabled },
  get editPlanType() { return editPlanType },
  get antigravityProjectId() { return antigravityProjectId },
  get antigravityModelMappings() { return antigravityModelMappings },
  get mixedScheduling() { return mixedScheduling },
  get allowOverages() { return allowOverages },
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
  get anthropicPassthroughEnabled() { return anthropicPassthroughEnabled },
  get anthropicAPIKeyAuthScheme() { return anthropicAPIKeyAuthScheme },
  get webSearchEmulationMode() { return webSearchEmulationMode },
  get cloudEligible() { return cloudEligible },
  get cloudEnabled() { return cloudEnabled },
  get excelBPSModels() { return excelBPSModels },
  get excelBPSCacheCreationAsInput() { return excelBPSCacheCreationAsInput },
  get excelBPSOmitUnsupportedTools() { return excelBPSOmitUnsupportedTools },
  get excelBPSIgnoreEncryptedContent() { return excelBPSIgnoreEncryptedContent },
  get openaiOAuthResponsesWebSocketV2Mode() { return openaiOAuthResponsesWebSocketV2Mode },
  get openaiAPIKeyResponsesWebSocketV2Mode() { return openaiAPIKeyResponsesWebSocketV2Mode },
  get openaiFlattenNamespacesEnabled() { return openaiFlattenNamespacesEnabled },
  get openAILongContextBillingEnabled() { return openAILongContextBillingEnabled },
  get openAICompactMode() { return openAICompactMode },
  get openAITextGenerationCapabilityEnabled() { return openAITextGenerationCapabilityEnabled },
  get openAIResponsesMode() { return openAIResponsesMode },
  get openAIImagesUrlToB64JsonEnabled() { return openAIImagesUrlToB64JsonEnabled },
  get autoPause5hThreshold() { return autoPause5hThreshold },
  get autoPause7dThreshold() { return autoPause7dThreshold },
  get autoPause5hDisabled() { return autoPause5hDisabled },
  get autoPause7dDisabled() { return autoPause7dDisabled },
  get codexImageToolMode() { return codexImageToolMode },
  get codexCLIOnlyEnabled() { return codexCLIOnlyEnabled },
  get codexCLIOnlyAppServerEnabled() { return codexCLIOnlyAppServerEnabled },
  get codexFingerprintMode() { return codexFingerprintMode },
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
  get upstreamRequestIdHeader() { return upstreamRequestIdHeader },
  get readUpstreamRequestIdHeader() { return readUpstreamRequestIdHeader },
  get ensureAntigravityMixedChannelConfirmed() { return ensureAntigravityMixedChannelConfirmed }
})


const { isCNApiKeyAccount, isGenericMultiProtocolAccount, genericAccountModes, cnPresetPlatform, adaptivePresetPlatform, editApiProtocol, editOpenCodeGoProtocolRules, editAccountMode, editOpenCodeAccountMode, cnPresetMode, currentOpenCodeOrCNMode, editZhipuOrganization, editZhipuProject, editAdaptiveBaseUrls, cnAccountModeOptions, editAdaptiveProtocolOptions, cnProtocolOptions, cnProtocolDescKey } = useEditProviderDraft({
  get props() { return props },
  get isMultiProtocolApiKeyPlatform() { return isMultiProtocolApiKeyPlatform },
  get providerAccountModes() { return providerAccountModes },
  get cloneOpenCodeGoProtocolRules() { return cloneOpenCodeGoProtocolRules },
  get providerNativeProtocols() { return providerNativeProtocols }
})


const { syncingForm, syncFormFromAccount } = useEditHydrationDraft({
  get antigravityMixedChannelConfirmed() { return antigravityMixedChannelConfirmed },
  get showMixedChannelWarning() { return showMixedChannelWarning },
  get mixedChannelWarningDetails() { return mixedChannelWarningDetails },
  get mixedChannelWarningRawMessage() { return mixedChannelWarningRawMessage },
  get mixedChannelWarningAction() { return mixedChannelWarningAction },
  get form() { return form },
  get interceptWarmupRequests() { return interceptWarmupRequests },
  get autoPauseOnExpired() { return autoPauseOnExpired },
  get editVertexProjectId() { return editVertexProjectId },
  get editVertexClientEmail() { return editVertexClientEmail },
  get editVertexLocation() { return editVertexLocation },
  get editVertexApiKey() { return editVertexApiKey },
  get antigravityProjectId() { return antigravityProjectId },
  get mixedScheduling() { return mixedScheduling },
  get allowOverages() { return allowOverages },
  get upstreamRequestIdHeader() { return upstreamRequestIdHeader },
  get readUpstreamRequestIdHeader() { return readUpstreamRequestIdHeader },
  get openAIImagesUrlToB64JsonEnabled() { return openAIImagesUrlToB64JsonEnabled },
  get autoPause5hThreshold() { return autoPause5hThreshold },
  get autoPause7dThreshold() { return autoPause7dThreshold },
  get autoPause5hDisabled() { return autoPause5hDisabled },
  get autoPause7dDisabled() { return autoPause7dDisabled },
  get autoResetCreditEnabled() { return autoResetCreditEnabled },
  get autoResetCredit5hThreshold() { return autoResetCredit5hThreshold },
  get autoResetCredit7dThreshold() { return autoResetCredit7dThreshold },
  get upstreamBillingAutoProbeEnabled() { return upstreamBillingAutoProbeEnabled },
  get upstreamBillingRateSyncEnabled() { return upstreamBillingRateSyncEnabled },
  get cloudEnabled() { return cloudEnabled },
  get cloudEligible() { return cloudEligible },
  get excelBPSEnabled() { return excelBPSEnabled },
  get excelBPSModels() { return excelBPSModels },
  get excelBPSCacheCreationAsInput() { return excelBPSCacheCreationAsInput },
  get excelBPSAutoDisableOn403() { return excelBPSAutoDisableOn403 },
  get excelBPSAutoRecoverOn403() { return excelBPSAutoRecoverOn403 },
  get excelBPSRecoveryIntervalMinutes() { return excelBPSRecoveryIntervalMinutes },
  get excelBPSOmitUnsupportedTools() { return excelBPSOmitUnsupportedTools },
  get excelBPSIgnoreEncryptedContent() { return excelBPSIgnoreEncryptedContent },
  get excelBPSAutoMoveOn403() { return excelBPSAutoMoveOn403 },
  get excelBPS403TargetGroupID() { return excelBPS403TargetGroupID },
  get openaiPassthroughEnabled() { return openaiPassthroughEnabled },
  get openaiFlattenNamespacesEnabled() { return openaiFlattenNamespacesEnabled },
  get openAILongContextBillingEnabled() { return openAILongContextBillingEnabled },
  get editPlanType() { return editPlanType },
  get openAICompactMode() { return openAICompactMode },
  get openAIResponsesMode() { return openAIResponsesMode },
  get openAIEndpointCapabilities() { return openAIEndpointCapabilities },
  get openAICompactModelMappings() { return openAICompactModelMappings },
  get openaiOAuthResponsesWebSocketV2Mode() { return openaiOAuthResponsesWebSocketV2Mode },
  get openaiAPIKeyResponsesWebSocketV2Mode() { return openaiAPIKeyResponsesWebSocketV2Mode },
  get codexCLIOnlyEnabled() { return codexCLIOnlyEnabled },
  get codexCLIOnlyAppServerEnabled() { return codexCLIOnlyAppServerEnabled },
  get codexFingerprintMode() { return codexFingerprintMode },
  get codexImageToolMode() { return codexImageToolMode },
  get anthropicPassthroughEnabled() { return anthropicPassthroughEnabled },
  get anthropicAPIKeyAuthScheme() { return anthropicAPIKeyAuthScheme },
  get webSearchEmulationMode() { return webSearchEmulationMode },
  get normalizeOpenAIResponsesMode() { return normalizeOpenAIResponsesMode },
  get readOpenAIEndpointCapabilities() { return readOpenAIEndpointCapabilities },
  get openAITextGenerationCapabilityEnabled() { return openAITextGenerationCapabilityEnabled },
  get editQuotaLimit() { return editQuotaLimit },
  get editQuotaDailyLimit() { return editQuotaDailyLimit },
  get editQuotaWeeklyLimit() { return editQuotaWeeklyLimit },
  get editDailyResetMode() { return editDailyResetMode },
  get editDailyResetHour() { return editDailyResetHour },
  get editWeeklyResetMode() { return editWeeklyResetMode },
  get editWeeklyResetDay() { return editWeeklyResetDay },
  get editWeeklyResetHour() { return editWeeklyResetHour },
  get editResetTimezone() { return editResetTimezone },
  get loadQuotaNotifyFromExtra() { return loadQuotaNotifyFromExtra },
  get resetQuotaNotify() { return resetQuotaNotify },
  get antigravityModelRestrictionMode() { return antigravityModelRestrictionMode },
  get antigravityWhitelistModels() { return antigravityWhitelistModels },
  get antigravityModelMappings() { return antigravityModelMappings },
  get loadQuotaControlSettings() { return loadQuotaControlSettings },
  get loadTempUnschedRules() { return loadTempUnschedRules },
  get loadAccountSchedulingThresholdOverride() { return loadAccountSchedulingThresholdOverride },
  get headerOverrideEnabled() { return headerOverrideEnabled },
  get headerOverrideRows() { return headerOverrideRows },
  get isHeaderOverrideCapable() { return isHeaderOverrideCapable },
  get grokOAuthCustomBaseUrlEnabled() { return grokOAuthCustomBaseUrlEnabled },
  get grokOAuthBaseUrl() { return grokOAuthBaseUrl },
  get GROK_CLIENT_TOOL_CACHE_EXTRA_KEY() { return GROK_CLIENT_TOOL_CACHE_EXTRA_KEY },
  get grokClientToolCacheEnabled() { return grokClientToolCacheEnabled },
  get grokMediaEligibilityMode() { return grokMediaEligibilityMode },
  get modeFromGrokMediaExtra() { return modeFromGrokMediaExtra },
  get grokMediaEligibilityInitialMode() { return grokMediaEligibilityInitialMode },
  get grokMediaEligibilityState() { return grokMediaEligibilityState },
  get grokMediaEligibilityError() { return grokMediaEligibilityError },
  get loadGrokMediaEligibility() { return loadGrokMediaEligibility },
  get grokMediaEligibilityRequestVersion() { return grokMediaEligibilityRequestVersion },
  get isMultiProtocolApiKeyPlatform() { return isMultiProtocolApiKeyPlatform },
  get editOpenCodeAccountMode() { return editOpenCodeAccountMode },
  get editAccountMode() { return editAccountMode },
  get resolveProviderAccountMode() { return resolveProviderAccountMode },
  get editApiProtocol() { return editApiProtocol },
  get cnSupportsNativeResponses() { return cnSupportsNativeResponses },
  get currentOpenCodeOrCNMode() { return currentOpenCodeOrCNMode },
  get defaultCNAdaptiveBaseUrls() { return defaultCNAdaptiveBaseUrls },
  get editAdaptiveBaseUrls() { return editAdaptiveBaseUrls },
  get editZhipuOrganization() { return editZhipuOrganization },
  get editZhipuProject() { return editZhipuProject },
  get providerRoutesByModel() { return providerRoutesByModel },
  get editOpenCodeGoProtocolRules() { return editOpenCodeGoProtocolRules },
  get parseOpenCodeGoProtocolRules() { return parseOpenCodeGoProtocolRules },
  get cloneOpenCodeGoProtocolRules() { return cloneOpenCodeGoProtocolRules },
  get defaultProviderProtocolRules() { return defaultProviderProtocolRules },
  get defaultCNBaseUrl() { return defaultCNBaseUrl },
  get editBaseUrl() { return editBaseUrl },
  get isCNApiKeyAccount() { return isCNApiKeyAccount },
  get loadModelRestrictionFromMapping() { return loadModelRestrictionFromMapping },
  get poolModeEnabled() { return poolModeEnabled },
  get poolModeRetryCount() { return poolModeRetryCount },
  get normalizePoolModeRetryCount() { return normalizePoolModeRetryCount },
  get DEFAULT_POOL_MODE_RETRY_COUNT() { return DEFAULT_POOL_MODE_RETRY_COUNT },
  get poolModeRetryStatusCodesInput() { return poolModeRetryStatusCodesInput },
  get formatPoolModeRetryStatusCodes() { return formatPoolModeRetryStatusCodes },
  get explicitCacheUpstreamEnabled() { return explicitCacheUpstreamEnabled },
  get explicitCacheUpstreamMaxTTL() { return explicitCacheUpstreamMaxTTL },
  get customErrorCodesEnabled() { return customErrorCodesEnabled },
  get selectedErrorCodes() { return selectedErrorCodes },
  get editBedrockRegion() { return editBedrockRegion },
  get editBedrockForceGlobal() { return editBedrockForceGlobal },
  get editBedrockApiKeyValue() { return editBedrockApiKeyValue },
  get editBedrockAccessKeyId() { return editBedrockAccessKeyId },
  get editBedrockSecretAccessKey() { return editBedrockSecretAccessKey },
  get editBedrockSessionToken() { return editBedrockSessionToken },
  get hasOAuthModelRestriction() { return hasOAuthModelRestriction },
  get modelRestrictionMode() { return modelRestrictionMode },
  get modelMappings() { return modelMappings },
  get allowedModels() { return allowedModels },
  get editApiKey() { return editApiKey }
})


const { DEFAULT_POOL_MODE_RETRY_COUNT, MAX_POOL_MODE_RETRY_COUNT, DEFAULT_POOL_MODE_RETRY_STATUS_CODES, poolModeEnabled, poolModeRetryCount, poolModeRetryStatusCodesInput, parsePoolModeRetryStatusCodes, formatPoolModeRetryStatusCodes, customErrorCodesEnabled, selectedErrorCodes, customErrorCodeInput, tempUnschedEnabled, accountSchedulingThresholdOverrideEnabled, accountSchedulingThresholdOverrideValue, supportsAccountSchedulingThresholdOverride, anthropicFableSchedulingThresholdOverrideEnabled, anthropicFableSchedulingThresholdOverrideValue, supportsAnthropicFableSchedulingThresholdOverride, tempUnschedRules, getTempUnschedRuleKey, tempUnschedScopeOptions, tempUnschedPresets, normalizePoolModeRetryCount, toggleErrorCode, addCustomErrorCode, removeErrorCode, addTempUnschedRule, removeTempUnschedRule, moveTempUnschedRule, applyTempUnschedConfig, loadAccountSchedulingThresholdOverride, applyAccountSchedulingThresholdOverridePatch, loadTempUnschedRules } = useEditRestrictionsDraft({
  get ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY() { return ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY },
  get ANTHROPIC_FABLE_SCHEDULING_THRESHOLD_CREDENTIAL_KEY() { return ANTHROPIC_FABLE_SCHEDULING_THRESHOLD_CREDENTIAL_KEY },
  get props() { return props },
  get t() { return t },
  get appStore() { return appStore },
  get toPositiveNumber() { return toPositiveNumber }
})


const { explicitCacheUpstreamEnabled, explicitCacheUpstreamMaxTTL, headerOverrideEnabled, headerOverrideRows, headerOverrideCapable, grokClientToolCacheEnabled, grokMediaEligibilityMode, grokMediaEligibilityInitialMode, grokMediaEligibilityState, grokMediaEligibilityLoading, grokMediaEligibilityError, grokMediaEligibilityRequestVersion, modeFromGrokMediaExtra, loadGrokMediaEligibility, interceptWarmupRequests, mixedScheduling, allowOverages, tlsFingerprintEnabled, tlsFingerprintProfileId, tlsFingerprintProfiles, sessionIdMaskingEnabled, cacheTTLOverrideEnabled, cacheTTLOverrideTarget, customBaseUrlEnabled, customBaseUrl, cloudEligible, cloudEnabled, excelBPSEnabled, excelBPSCacheCreationAsInput, excelBPSAutoDisableOn403, excelBPSAutoRecoverOn403, excelBPSRecoveryIntervalMinutes, excelBPSSystemDisabled, excelBPSStopRequested, excelBPSSwitchTouched, excelBPSExpectedDisabledAt, canStopBPSRecovery, requestBPSStop, setBPSEnabled, excelBPSOmitUnsupportedTools, excelBPSIgnoreEncryptedContent, excelBPSAutoMoveOn403, excelBPS403TargetGroupID, excelBPS403GroupOptions, openaiPassthroughEnabled, openaiFlattenNamespacesEnabled, openAICompactMode, openAIResponsesMode, openAIImagesUrlToB64JsonEnabled, openAIEndpointCapabilities, openaiAPIKeyResponsesWebSocketV2Mode, codexCLIOnlyEnabled, codexCLIOnlyAppServerEnabled, codexFingerprintMode, codexImageToolMode, anthropicPassthroughEnabled, anthropicAPIKeyAuthScheme, webSearchEmulationMode, webSearchGlobalEnabled, codexFingerprintModeOptions, openAIWSModeOptions, openaiResponsesWebSocketV2Mode, openAIWSModeHintKey, codexImageToolOptions, codexImageToolBadgeLabel, codexImageToolBadgeClass, openAICompactModeOptions, openAIResponsesModeOptions, openAIEndpointCapabilityOptions, openAITextGenerationCapabilityEnabled, readOpenAIEndpointCapabilities, toggleOpenAIEndpointCapability, applyOpenAIEndpointCapabilities, normalizeOpenAIResponsesMode, openAIResponsesStatusKey, openAICompactStatusKey, loadTLSProfiles, persistGrokMediaEligibility } = useEditTransportDraft({
  get resourceTasks() { return resourceTasks },
  get props() { return props },
  get isHeaderOverrideCapable() { return isHeaderOverrideCapable },
  get isGrokOAuthAccount() { return isGrokOAuthAccount },
  get t() { return t },
  get authStore() { return authStore },
  get openaiOAuthResponsesWebSocketV2Mode() { return openaiOAuthResponsesWebSocketV2Mode },
  get resourceServices() { return resourceServices },
  get appStore() { return appStore }
})


const { grokOAuthCustomBaseUrlEnabled, grokOAuthBaseUrl, openaiOAuthResponsesWebSocketV2Mode } = useEditAuthorizationDraft({

})


const { autoPauseOnExpired, autoPause5hThreshold, autoPause7dThreshold, autoPause5hDisabled, autoPause7dDisabled, autoResetCreditEnabled, autoResetCredit5hThreshold, autoResetCredit7dThreshold, windowCostEnabled, windowCostLimit, windowCostStickyReserve, sessionLimitEnabled, maxSessions, sessionIdleTimeout, rpmLimitEnabled, baseRpm, rpmStrategy, rpmStickyBuffer, userMsgQueueMode, umqModeOptions, quotaNotifyGlobalEnabled, quotaNotifyState, loadQuotaNotifyGlobal, loadQuotaNotifyFromExtra, writeQuotaNotifyToExtra, resetQuotaNotify, editQuotaLimit, editQuotaDailyLimit, editQuotaWeeklyLimit, editDailyResetMode, editDailyResetHour, editWeeklyResetMode, editWeeklyResetDay, editWeeklyResetHour, editResetTimezone, loadQuotaControlSettings } = useEditLimitsDraft({
  get t() { return t },
  get tlsFingerprintEnabled() { return tlsFingerprintEnabled },
  get tlsFingerprintProfileId() { return tlsFingerprintProfileId },
  get sessionIdMaskingEnabled() { return sessionIdMaskingEnabled },
  get cacheTTLOverrideEnabled() { return cacheTTLOverrideEnabled },
  get cacheTTLOverrideTarget() { return cacheTTLOverrideTarget },
  get customBaseUrlEnabled() { return customBaseUrlEnabled },
  get customBaseUrl() { return customBaseUrl }
})


const { showMixedChannelWarning, antigravityMixedChannelConfirmed, needsMixedChannelCheck, clearMixedChannelDialog, openMixedChannelDialog, withAntigravityConfirmFlag, ensureAntigravityMixedChannelConfirmed, handleMixedChannelConfirm, handleMixedChannelCancel } = useEditAssociationDraft({
  get resourceTasks() { return resourceTasks },
  get props() { return props },
  get mixedChannelWarningDetails() { return mixedChannelWarningDetails },
  get mixedChannelWarningRawMessage() { return mixedChannelWarningRawMessage },
  get mixedChannelWarningAction() { return mixedChannelWarningAction },
  get t() { return t },
  get form() { return form },
  get appStore() { return appStore },
  get submitting() { return submitting }
})


watch(() => props.account?.id, () => {
  opencodeGoState.value = props.account?.opencode_go_usage ?? null
  if (opencodeGoState.value && !opencodeGoState.value.snapshot) void loadOpenCodeGoUsage()
})


onMounted(() => {
  if (opencodeGoState.value && !opencodeGoState.value.snapshot) void loadOpenCodeGoUsage()
})


watch(editApiProtocol, (protocol, previousProtocol) => {
  if (!isCNApiKeyAccount.value || syncingForm.value) return
  if (protocol === 'adaptive') {
    const defaults = defaultCNAdaptiveBaseUrls(adaptivePresetPlatform.value, currentOpenCodeOrCNMode())
    for (const item of editAdaptiveProtocolOptions.value) {
      if (!editAdaptiveBaseUrls.value[item.value]) editAdaptiveBaseUrls.value[item.value] = defaults[item.value]
    }
    if (previousProtocol !== 'adaptive' && editBaseUrl.value.trim()) {
      editAdaptiveBaseUrls.value[previousProtocol] = editBaseUrl.value.trim()
    }
    editBaseUrl.value = editAdaptiveBaseUrls.value.chat_completions
    return
  }
  if (previousProtocol === 'adaptive') {
    editBaseUrl.value = editAdaptiveBaseUrls.value[protocol] ||
      defaultCNBaseUrl(props.account!.platform, currentOpenCodeOrCNMode(), protocol)
    return
  }
  editBaseUrl.value = defaultCNBaseUrl(props.account!.platform, currentOpenCodeOrCNMode(), protocol)
})


watch(editAccountMode, (mode, previousMode) => {
  if (!isCNApiKeyAccount.value || syncingForm.value) return
  if (props.account?.platform === 'opencode_go') return
  // 供应商没有的接入模式（如 deepseek 无 coding 套餐）防御性回退默认模式（UI 已隐藏该选项）。
  const effectiveMode = resolveProviderAccountMode(props.account!.platform, mode)
  if (effectiveMode !== mode) {
    editAccountMode.value = effectiveMode
    return
  }
  if (providerRoutesByModel(props.account!.platform)) {
    const previousRules = JSON.stringify(defaultProviderProtocolRules(props.account!.platform, previousMode))
    if (JSON.stringify(editOpenCodeGoProtocolRules.value) === previousRules) {
      editOpenCodeGoProtocolRules.value = cloneOpenCodeGoProtocolRules(
        defaultProviderProtocolRules(props.account!.platform, mode)
      )
    }
  }
  if (editApiProtocol.value === 'adaptive') {
    const previousDefaults = defaultCNAdaptiveBaseUrls(adaptivePresetPlatform.value, previousMode)
    const nextDefaults = defaultCNAdaptiveBaseUrls(adaptivePresetPlatform.value, mode)
    for (const item of editAdaptiveProtocolOptions.value) {
      if (!editAdaptiveBaseUrls.value[item.value] || editAdaptiveBaseUrls.value[item.value] === previousDefaults[item.value]) {
        editAdaptiveBaseUrls.value[item.value] = nextDefaults[item.value]
      }
    }
    editBaseUrl.value = editAdaptiveBaseUrls.value.chat_completions
    return
  }
  editBaseUrl.value = defaultCNBaseUrl(props.account!.platform, mode, editApiProtocol.value)
})


watch(editOpenCodeAccountMode, (mode, previousMode) => {
  if (!isCNApiKeyAccount.value || props.account?.platform !== 'opencode_go' || syncingForm.value) return
  if (editApiProtocol.value === 'adaptive') {
    const previousDefaults = defaultCNAdaptiveBaseUrls('opencode_go', previousMode)
    const nextDefaults = defaultCNAdaptiveBaseUrls('opencode_go', mode)
    for (const item of editAdaptiveProtocolOptions.value) {
      if (!editAdaptiveBaseUrls.value[item.value] || editAdaptiveBaseUrls.value[item.value] === previousDefaults[item.value]) {
        editAdaptiveBaseUrls.value[item.value] = nextDefaults[item.value]
      }
    }
    editBaseUrl.value = editAdaptiveBaseUrls.value.chat_completions
  } else {
    editBaseUrl.value = defaultCNBaseUrl('opencode_go', mode, editApiProtocol.value)
  }
  const previousRules = JSON.stringify(defaultOpenCodeProtocolRules(previousMode))
  if (JSON.stringify(editOpenCodeGoProtocolRules.value) === previousRules) {
    editOpenCodeGoProtocolRules.value = cloneOpenCodeGoProtocolRules(defaultOpenCodeProtocolRules(mode))
  }
})


if (props.allowTransportEdit) resourceServices.webSearchConfig().then(resourceTasks.guard(cfg => {
  webSearchGlobalEnabled.value = cfg?.enabled === true && (cfg?.providers?.length ?? 0) > 0
})).catch(resourceTasks.guard(() => { webSearchGlobalEnabled.value = false }))


if (props.allowTransportEdit) loadQuotaNotifyGlobal()


watch(
  [() => props.show, () => props.account],
  ([show, newAccount], [wasShow, previousAccount]) => {
    if (!show || !newAccount) {
      return
    }
    if (!wasShow || newAccount !== previousAccount) {
      syncFormFromAccount(newAccount)
      loadTLSProfiles()
    }
  },
  { immediate: true }
)


watch([() => props.account?.id, () => props.show], async () => {
const pending = resourceTasks.capture()
try {

  if (!props.show) { capturedBaselineID = undefined; return }
  if (!props.captureBaseline || capturedBaselineID === props.account?.id) return
  await pending.wait(async () => nextTick())
  const baseline = buildAccountUpdatePayload()
  if (baseline) {
    capturedBaselineID = props.account?.id
    emit('baseline', JSON.parse(JSON.stringify(baseline)))
  }

} catch (failure: unknown) {
  if (!(failure instanceof StaleResourceTask)) throw failure
}
}, { immediate: true, flush: 'post' })


watch(resourceServices.scopeKey, () => {
  resourceTasks.invalidate()
  editApiKey.value = ''
  editBedrockAccessKeyId.value = ''
  editBedrockSecretAccessKey.value = ''
  editBedrockSessionToken.value = ''
  editBedrockApiKeyValue.value = ''
  editVertexApiKey.value = ''
  headerOverrideRows.value = []
  if (props.show) emit('close')
}, { flush: 'sync' })
watch([() => props.show, () => props.account?.id, resourceServices.scopeKey], () => { submitting.value = false }, { flush: 'sync' })
watch(() => props.show, show => {
  if (show) return
  editApiKey.value = ''
  editBedrockAccessKeyId.value = ''
  editBedrockSecretAccessKey.value = ''
  editBedrockSessionToken.value = ''
  editBedrockApiKeyValue.value = ''
  editVertexApiKey.value = ''
  headerOverrideRows.value = []
}, { flush: 'sync' })
</script>

<template>


  <BaseDialog
    :show="show"
    :title="t('admin.accounts.editAccount')"
    width="wide"
    @close="handleClose"
  >
    <form
      v-if="account"
      id="edit-account-form"
      @submit.prevent="handleSubmit"
      class="space-y-5"
    >
      <p v-if="bpsStateConflict" role="alert" data-testid="bps-state-conflict" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
        {{ t('admin.accounts.openai.excelBPSStateConflict') }}
      </p>
      <ResourceIdentityPanel :model-value="{ name: form.name, notes: form.notes }" @update:model-value="Object.assign(form, $event)" />

      <div v-if="!allowTransportEdit" class="space-y-4" data-testid="delegated-account-editor">
        <div class="grid grid-cols-3 gap-4">
          <label class="input-label">{{ t('admin.accounts.concurrency') }}<input v-model.number="form.concurrency" type="number" min="1" class="input" /></label>
          <label class="input-label">{{ t('admin.accounts.priority') }}<input v-model.number="form.priority" type="number" min="1" class="input" /></label>
          <label class="input-label">{{ t('admin.accounts.loadFactor') }}<input v-model.number="form.load_factor" type="number" min="1" class="input" /></label>
        </div>
        <label v-if="showCost && allowRateEdit" class="input-label">{{ t('admin.accounts.billingRateMultiplier') }}<input v-model.number="form.rate_multiplier" type="number" min="0" step="0.001" class="input" /></label>
        <label v-if="allowAuthorize && account.type === 'apikey'" class="input-label">{{ t('admin.accounts.apiKey') }}<input v-model="editApiKey" type="password" autocomplete="new-password" class="input" /></label>
      </div>
      <template v-if="allowTransportEdit">
      <!-- API Key fields (only for apikey type) -->
      <EditApiKeyCredentialsPanel
  :account="account"
  :isCNApiKeyAccount="isCNApiKeyAccount"
  :editApiProtocol="editApiProtocol"
  :allowTransportEdit="allowTransportEdit"
  :editBaseUrl="editBaseUrl"
  :baseUrlHint="baseUrlHint"
  :cnPresetPlatform="cnPresetPlatform"
  :cnPresetMode="cnPresetMode"
  :onCnPresetSelect="onCnPresetSelect"
  :editAdaptiveProtocolOptions="editAdaptiveProtocolOptions"
  :editAdaptiveBaseUrls="editAdaptiveBaseUrls"
  :cnSupportsNativeResponses="cnSupportsNativeResponses"
  :currentOpenCodeOrCNMode="currentOpenCodeOrCNMode"
  :editOpenCodeAccountMode="editOpenCodeAccountMode"
  :isGenericMultiProtocolAccount="isGenericMultiProtocolAccount"
  :genericAccountModes="genericAccountModes"
  :editAccountMode="editAccountMode"
  :cnAccountModeOptions="cnAccountModeOptions"
  :cnProtocolOptions="cnProtocolOptions"
  :cnProtocolDescKey="cnProtocolDescKey"
  :providerRoutesByModel="providerRoutesByModel"
  :editOpenCodeGoProtocolRules="editOpenCodeGoProtocolRules"
  :editZhipuOrganization="editZhipuOrganization"
  :editZhipuProject="editZhipuProject"
  :allowAuthorize="allowAuthorize"
  :editApiKey="editApiKey"
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
  @update:editBaseUrl="editBaseUrl = $event"
  @update:editAdaptiveBaseUrls="editAdaptiveBaseUrls = $event"
  @update:editOpenCodeAccountMode="editOpenCodeAccountMode = $event"
  @update:editAccountMode="editAccountMode = $event"
  @update:editApiProtocol="editApiProtocol = $event"
  @update:editOpenCodeGoProtocolRules="editOpenCodeGoProtocolRules = $event"
  @update:editZhipuOrganization="editZhipuOrganization = $event"
  @update:editZhipuProject="editZhipuProject = $event"
  @update:editApiKey="editApiKey = $event"
  @update:modelRestrictionMode="modelRestrictionMode = $event"
  @update:allowedModels="allowedModels = $event"
  @update:poolModeEnabled="poolModeEnabled = $event"
  @update:poolModeRetryCount="poolModeRetryCount = $event"
  @update:poolModeRetryStatusCodesInput="poolModeRetryStatusCodesInput = $event"
  @update:explicitCacheUpstreamEnabled="explicitCacheUpstreamEnabled = $event"
  @update:explicitCacheUpstreamMaxTTL="explicitCacheUpstreamMaxTTL = $event"
  @update:customErrorCodesEnabled="customErrorCodesEnabled = $event"
  @update:customErrorCodeInput="customErrorCodeInput = $event"
/>

      <!-- Grok OAuth client-tool prompt cache opt-in -->
      <EditGrokPromptCachePanel
  :account="account"
  :grokClientToolCacheEnabled="grokClientToolCacheEnabled"
  @update:grokClientToolCacheEnabled="grokClientToolCacheEnabled = $event"
/>

      <!-- Grok OAuth media generation eligibility override -->
      <EditGrokMediaEligibilityPanel
  :isGrokOAuthAccount="isGrokOAuthAccount"
  :grokMediaEligibilityMode="grokMediaEligibilityMode"
  :grokMediaEligibilityLoading="grokMediaEligibilityLoading"
  :grokMediaEligibilityError="grokMediaEligibilityError"
  :grokMediaEligibilityState="grokMediaEligibilityState"
  @update:grokMediaEligibilityMode="grokMediaEligibilityMode = $event"
/>

      <!-- Grok OAuth Custom Upstream URL (仅改写转发端点，OAuth 授权/刷新不受影响) -->
      <EditGrokUpstreamPanel
  :account="account"
  :grokOAuthCustomBaseUrlEnabled="grokOAuthCustomBaseUrlEnabled"
  :allowTransportEdit="allowTransportEdit"
  :grokOAuthBaseUrl="grokOAuthBaseUrl"
  @update:grokOAuthCustomBaseUrlEnabled="grokOAuthCustomBaseUrlEnabled = $event"
  @update:grokOAuthBaseUrl="grokOAuthBaseUrl = $event"
/>

      <!-- Header Override Section (eligible API-key platforms + grok OAuth) -->
      <EditRequestHeadersPanel
  :headerOverrideCapable="headerOverrideCapable"
  :headerOverrideEnabled="headerOverrideEnabled"
  :headerOverrideRows="headerOverrideRows"
  @update:headerOverrideEnabled="headerOverrideEnabled = $event"
  @update:headerOverrideRows="headerOverrideRows = $event"
/>

      <!-- OAuth / setup-token accounts have no API key model restriction section. -->
      <EditOAuthModelMappingPanel
  :hasOAuthModelRestriction="hasOAuthModelRestriction"
  :isOpenAIModelRestrictionDisabled="isOpenAIModelRestrictionDisabled"
  :modelRestrictionMode="modelRestrictionMode"
  :allowedModels="allowedModels"
  :modelMappings="modelMappings"
  :account="account"
  :getModelMappingKey="getModelMappingKey"
  :removeModelMapping="removeModelMapping"
  :addModelMapping="addModelMapping"
  :presetMappings="presetMappings"
  :addPresetMapping="addPresetMapping"
  @update:modelRestrictionMode="modelRestrictionMode = $event"
  @update:allowedModels="allowedModels = $event"
/>

      <!-- Upstream fields (only for upstream type) -->
      <EditUpstreamCredentialsPanel
  :account="account"
  :allowTransportEdit="allowTransportEdit"
  :editBaseUrl="editBaseUrl"
  :allowAuthorize="allowAuthorize"
  :editApiKey="editApiKey"
  @update:editBaseUrl="editBaseUrl = $event"
  @update:editApiKey="editApiKey = $event"
/>

      <!-- Vertex Service Account -->
      <div v-if="(account.platform === 'gemini' || account.platform === 'anthropic') && account.type === 'service_account'" class="space-y-4">
        <div v-if="isVertexAPIKeyMode">
          <label class="input-label">API Key</label>
          <input
            v-model="editVertexApiKey"
            type="password"
            class="input font-mono"
            data-testid="edit-vertex-api-key-input"
            :placeholder="t('admin.accounts.leaveEmptyToKeep')"
          />
          <p class="input-hint">{{ t('admin.accounts.vertexApiKeyHint') }}</p>
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div v-if="isVertexAPIKeyMode">
            <label class="input-label">Project ID</label>
            <input
              v-model="editVertexProjectId"
              type="text"
              class="input font-mono"
              data-testid="edit-vertex-api-key-project-id-input"
              :placeholder="t('admin.accounts.vertexApiKeyProjectIdPlaceholder')"
            />
            <p class="input-hint">{{ t('admin.accounts.vertexApiKeyProjectIdHint') }}</p>
          </div>
          <div v-else>
            <label class="input-label">Project ID</label>
            <input
              v-model="editVertexProjectId"
              type="text"
              class="input font-mono"
              readonly
              :placeholder="t('admin.accounts.vertexProjectIdPlaceholder')"
            />
            <p class="input-hint">{{ t('admin.accounts.vertexSaJsonEditHint') }}</p>
          </div>
          <div v-if="!isVertexAPIKeyMode || editVertexProjectId.trim()">
            <label class="input-label">Location</label>
            <select
 :disabled="!allowTransportEdit"              v-model="editVertexLocation"
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

        <!-- Model Restriction Section for Service Account -->
        <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
          <label class="input-label">{{ t('admin.accounts.modelRestriction') }}</label>

          <!-- Mode Toggle -->
          <div class="mb-4 flex gap-2">
            <button
              type="button"
              @click="modelRestrictionMode = 'whitelist'"
              :class="[
                'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                modelRestrictionMode === 'whitelist'
                  ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
            >
              <svg
                class="mr-1.5 inline h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
                />
              </svg>
              {{ t('admin.accounts.modelWhitelist') }}
            </button>
            <button
              type="button"
              @click="modelRestrictionMode = 'mapping'"
              :class="[
                'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                modelRestrictionMode === 'mapping'
                  ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
            >
              <svg
                class="mr-1.5 inline h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4"
                />
              </svg>
              {{ t('admin.accounts.modelMapping') }}
            </button>
          </div>

          <!-- Whitelist Mode -->
          <div v-if="modelRestrictionMode === 'whitelist'">
            <ModelWhitelistSelector save-mode="draft" v-model="allowedModels" :platform="account?.platform || 'anthropic'" :account-id="account?.id" />
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.selectedModels', { count: allowedModels.length }) }}
              <span v-if="allowedModels.length === 0 && modelMappings.length === 0">{{
                t('admin.accounts.supportsAllModels')
              }}</span>
            </p>
          </div>

          <!-- Mapping Mode -->
          <div v-else>
            <div class="mb-3 rounded-lg bg-purple-50 p-3 dark:bg-purple-900/20">
              <p class="text-xs text-purple-700 dark:text-purple-400">
                <svg
                  class="mr-1 inline h-4 w-4"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                  />
                </svg>
                {{ t('admin.accounts.mapRequestModels') }}
              </p>
            </div>

            <!-- Model Mapping List -->
            <div v-if="modelMappings.length > 0" class="mb-3 space-y-2">
              <div
                v-for="(mapping, index) in modelMappings"
                :key="getModelMappingKey(mapping)"
                class="flex items-center gap-2"
              >
                <input
                  v-model="mapping.from"
                  type="text"
                  class="input flex-1"
                  :placeholder="t('admin.accounts.requestModel')"
                />
                <svg
                  class="h-4 w-4 flex-shrink-0 text-gray-400"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M14 5l7 7m0 0l-7 7m7-7H3"
                  />
                </svg>
                <input
                  v-model="mapping.to"
                  type="text"
                  class="input flex-1"
                  :placeholder="t('admin.accounts.actualModel')"
                />
                <button
                  type="button"
                  @click="removeModelMapping(index)"
                  class="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                >
                  <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      stroke-width="2"
                      d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                    />
                  </svg>
                </button>
              </div>
            </div>

            <button
              type="button"
              @click="addModelMapping"
              class="mb-3 w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-gray-600 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400 dark:hover:text-gray-300"
            >
              <svg
                class="mr-1 inline h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M12 4v16m8-8H4"
                />
              </svg>
              {{ t('admin.accounts.addMapping') }}
            </button>

            <!-- Quick Add Buttons -->
            <div class="flex flex-wrap gap-2">
              <button
                v-for="preset in presetMappings"
                :key="preset.label"
                type="button"
                @click="addPresetMapping(preset.from, preset.to)"
                :class="['rounded-lg px-3 py-1 text-xs transition-colors', preset.color]"
              >
                + {{ preset.label }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- Bedrock fields (for bedrock type, both SigV4 and API Key modes) -->
      <EditBedrockCredentialsPanel
  :account="account"
  :isBedrockAPIKeyMode="isBedrockAPIKeyMode"
  :allowAuthorize="allowAuthorize"
  :editBedrockAccessKeyId="editBedrockAccessKeyId"
  :editBedrockSecretAccessKey="editBedrockSecretAccessKey"
  :editBedrockSessionToken="editBedrockSessionToken"
  :editBedrockApiKeyValue="editBedrockApiKeyValue"
  :allowTransportEdit="allowTransportEdit"
  :editBedrockRegion="editBedrockRegion"
  :editBedrockForceGlobal="editBedrockForceGlobal"
  :modelRestrictionMode="modelRestrictionMode"
  :allowedModels="allowedModels"
  :modelMappings="modelMappings"
  :getModelMappingKey="getModelMappingKey"
  :bedrockPresets="bedrockPresets"
  :poolModeEnabled="poolModeEnabled"
  :poolModeRetryCount="poolModeRetryCount"
  :MAX_POOL_MODE_RETRY_COUNT="MAX_POOL_MODE_RETRY_COUNT"
  :DEFAULT_POOL_MODE_RETRY_COUNT="DEFAULT_POOL_MODE_RETRY_COUNT"
  :poolModeRetryStatusCodesInput="poolModeRetryStatusCodesInput"
  :DEFAULT_POOL_MODE_RETRY_STATUS_CODES="DEFAULT_POOL_MODE_RETRY_STATUS_CODES"
  @update:editBedrockAccessKeyId="editBedrockAccessKeyId = $event"
  @update:editBedrockSecretAccessKey="editBedrockSecretAccessKey = $event"
  @update:editBedrockSessionToken="editBedrockSessionToken = $event"
  @update:editBedrockApiKeyValue="editBedrockApiKeyValue = $event"
  @update:editBedrockRegion="editBedrockRegion = $event"
  @update:editBedrockForceGlobal="editBedrockForceGlobal = $event"
  @update:modelRestrictionMode="modelRestrictionMode = $event"
  @update:allowedModels="allowedModels = $event"
  @update:poolModeEnabled="poolModeEnabled = $event"
  @update:poolModeRetryCount="poolModeRetryCount = $event"
  @update:poolModeRetryStatusCodesInput="poolModeRetryStatusCodesInput = $event"
/>

      <div
        v-if="account.platform === 'antigravity' && account.type === 'oauth'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
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

      <!-- Antigravity model restriction (applies to all antigravity types) -->
      <!-- Antigravity 只支持模型映射模式，不支持白名单模式 -->
      <EditAntigravityModelMappingPanel
  :account="account"
  :syncAntigravityUpstreamModels="syncAntigravityUpstreamModels"
  :isSyncingAntigravityUpstream="isSyncingAntigravityUpstream"
  :antigravityModelMappings="antigravityModelMappings"
  :getAntigravityModelMappingKey="getAntigravityModelMappingKey"
  :removeAntigravityModelMapping="removeAntigravityModelMapping"
  :addAntigravityModelMapping="addAntigravityModelMapping"
  :antigravityPresetMappings="antigravityPresetMappings"
  :addAntigravityPresetMapping="addAntigravityPresetMapping"
/>

      <!-- Temp Unschedulable Rules -->
      <EditTemporaryRestrictionsPanel
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


      <div
        v-if="supportsAccountSchedulingThresholdOverride"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
        data-testid="account-scheduling-threshold-section"
      >
        <div class="mb-3 flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.accountSchedulingThresholdOverride') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.accountSchedulingThresholdOverrideHint') }}
            </p>
          </div>
          <input
            v-model="accountSchedulingThresholdOverrideEnabled"
            data-testid="account-scheduling-threshold-override-enabled"
            type="checkbox"
            class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div v-if="accountSchedulingThresholdOverrideEnabled">
          <label class="input-label">{{ t('admin.accounts.accountSchedulingThresholdOverrideValue') }}</label>
          <input
            v-model.number="accountSchedulingThresholdOverrideValue"
            data-testid="account-scheduling-threshold-override-value"
            type="number"
            min="1"
            max="100"
            class="input"
          />
          <p class="input-hint">{{ t('admin.accounts.accountSchedulingThresholdOverrideDisabledHint') }}</p>
        </div>
      </div>

      <!-- Anthropic 7d Fable 阈值覆盖：只拦 Fable 模型，不停整个账号 -->
      <div
        v-if="supportsAnthropicFableSchedulingThresholdOverride"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
        data-testid="anthropic-fable-scheduling-threshold-section"
      >
        <div class="mb-3 flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.anthropicFableSchedulingThresholdOverride') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.anthropicFableSchedulingThresholdOverrideHint') }}
            </p>
          </div>
          <input
            v-model="anthropicFableSchedulingThresholdOverrideEnabled"
            data-testid="anthropic-fable-scheduling-threshold-override-enabled"
            type="checkbox"
            class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div v-if="anthropicFableSchedulingThresholdOverrideEnabled">
          <label class="input-label">{{ t('admin.accounts.anthropicFableSchedulingThresholdOverrideValue') }}</label>
          <input
            v-model.number="anthropicFableSchedulingThresholdOverrideValue"
            data-testid="anthropic-fable-scheduling-threshold-override-value"
            type="number"
            min="1"
            max="100"
            class="input"
          />
          <p class="input-hint">{{ t('admin.accounts.anthropicFableSchedulingThresholdOverrideDisabledHint') }}</p>
        </div>
      </div>

      <!-- Intercept Warmup Requests (Anthropic/Antigravity) -->
      <EditWarmupPolicyPanel
  :account="account"
  :interceptWarmupRequests="interceptWarmupRequests"
  @update:interceptWarmupRequests="interceptWarmupRequests = $event"
/>

      <div v-if="!isSparkShadow">
        <div class="mb-1 flex items-center gap-2">
          <label class="input-label mb-0">{{ t('admin.accounts.proxy') }}</label>

        </div>
        <ProxySelector :disabled="!allowTransportEdit" v-model="form.proxy_id" :proxies="proxies" />
      </div>

      <AccountTelemetryPanel
        v-if="authStore.isSuperAdmin && allowTransportEdit && !supplierScope && !captureBaseline && !isSparkShadow && (account.platform === 'anthropic' && (account.type === 'oauth' || account.type === 'setup-token'))"
        :key="`${account.id}:${show}`"
        :account-id="account.id"
        :value="readAccountTelemetry(account.extra)"
      />

      <UpstreamRequestIdHeaderField
        v-model="upstreamRequestIdHeader"
        :platform="account.platform"
        :type="account.type"
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
        <div v-if="showCost">
          <label class="input-label">{{ t('admin.accounts.billingRateMultiplier') }}</label>
          <input
            v-model.number="form.rate_multiplier"
            type="number"
            min="0"
            step="0.001"
            class="input disabled:cursor-not-allowed disabled:opacity-60"
            data-testid="account-rate-multiplier"
            :disabled="upstreamBillingRateSyncEnabled || !allowRateEdit"
          />
          <p class="input-hint">
            {{
              t(
                upstreamBillingRateSyncEnabled
                  ? 'admin.accounts.upstreamBilling.syncRateManagedHint'
                  : 'admin.accounts.billingRateMultiplierHint'
              )
            }}
          </p>
          <div
            v-if="account?.type === 'apikey'"
            class="mt-3 flex items-center justify-between gap-3"
          >
            <div class="min-w-0">
              <p class="text-xs font-medium text-gray-700 dark:text-gray-200">
                {{ t('admin.accounts.upstreamBilling.syncRate') }}
              </p>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.upstreamBilling.syncRateHint') }}
              </p>
            </div>
            <Toggle
              :model-value="upstreamBillingRateSyncEnabled"
              data-testid="upstream-billing-rate-sync"
              :aria-label="t('admin.accounts.upstreamBilling.syncRate')"
              @update:model-value="handleUpstreamBillingRateSyncChange"
            />
          </div>
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

      <CloudGatewaySettings
        v-if="show && cloudEligible && account"
        v-model:enabled="cloudEnabled"
        :account-id="account.id"
        :can-edit="allowTransportEdit"
      />

      <div v-if="account?.platform === 'openai' && account?.type === 'oauth' && !isSparkShadow"
        class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="flex items-center justify-between gap-4">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.excelBPS') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSDesc') }}</p>
          </div>
        </div>
        <ExcelBPSAuthorization v-if="account && !account.vendor_id && allowAuthorize && authStore.canAuthorize" :account-id="account.id" :proxy-id="account.proxy_id" class="mb-4" />
        <Toggle :model-value="excelBPSEnabled" :disabled="submitting" data-testid="excel-bps-toggle"
          :aria-label="t('admin.accounts.openai.excelBPS')" @update:model-value="setBPSEnabled" />
        <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSAccountSettingsHint') }}</p>
        <div v-if="canStopBPSRecovery" class="mt-3 space-y-2">
          <button type="button" class="btn btn-secondary" data-testid="excel-bps-stop-recovery"
            :disabled="submitting || excelBPSStopRequested" @click="requestBPSStop">
            {{ t('admin.accounts.openai.excelBPSStopRecovery') }}
          </button>
          <p v-if="excelBPSStopRequested" role="status" class="text-sm text-amber-700 dark:text-amber-300">
            {{ t('admin.accounts.openai.excelBPSStopRecoveryPending') }}
          </p>
        </div>
        <div v-if="excelBPSEnabled" class="mt-3 space-y-3">
          <div data-testid="excel-bps-model-selection">
            <label class="input-label">{{ t('admin.accounts.openai.excelBPSModels') }}</label>
            <ModelWhitelistSelector v-model="excelBPSModels" platform="openai" empty-mode="explicit" />
            <p class="input-hint">{{ t('admin.accounts.openai.excelBPSModelsHint') }}</p>
          </div>
        </div>
        <p v-if="excelBPSEnabled" class="mt-2 text-xs text-amber-600 dark:text-amber-400">{{ t('admin.accounts.openai.excelBPSNotice') }}</p>
        <div v-if="excelBPSEnabled" class="mt-3">
          <label class="flex items-center gap-2">
            <input v-model="excelBPSOmitUnsupportedTools" type="checkbox"
              data-testid="excel-bps-omit-unsupported-tools"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500" />
            <span class="text-sm">{{ t('admin.accounts.openai.excelBPSOmitUnsupportedTools') }}</span>
          </label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSOmitUnsupportedToolsDesc') }}</p>
        </div>
        <div v-if="excelBPSEnabled" class="mt-3">
          <label class="flex items-center gap-2">
            <input v-model="excelBPSIgnoreEncryptedContent" type="checkbox"
              data-testid="excel-bps-ignore-encrypted-content"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500" />
            <span class="text-sm">{{ t('admin.accounts.openai.excelBPSIgnoreEncryptedContent') }}</span>
          </label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSIgnoreEncryptedContentDesc') }}</p>
        </div>
        <div v-if="excelBPSEnabled || excelBPSSystemDisabled" class="mt-3">
          <label class="flex items-center gap-2">
            <input v-model="excelBPSAutoDisableOn403" type="checkbox"
              data-testid="excel-bps-auto-disable-on-403"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500" />
            <span class="text-sm">{{ t('admin.accounts.openai.excelBPSAutoDisableOn403') }}</span>
          </label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSAutoDisableOn403Desc') }}</p>
        </div>
        <div v-if="excelBPSEnabled || excelBPSSystemDisabled" class="mt-3">
          <label class="flex items-center gap-2">
            <input v-model="excelBPSAutoRecoverOn403" type="checkbox" :disabled="!excelBPSAutoDisableOn403"
              data-testid="excel-bps-auto-recover-on-403"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 disabled:opacity-50" />
            <span class="text-sm">{{ t('admin.accounts.openai.excelBPSAutoRecoverOn403') }}</span>
          </label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSAutoRecoverOn403Desc') }}</p>
          <div v-if="excelBPSAutoRecoverOn403 && excelBPSAutoDisableOn403" class="mt-2">
            <label class="block space-y-1">
              <span class="text-sm">{{ t('admin.accounts.openai.excelBPS403RecoveryInterval') }}</span>
              <input v-model.number="excelBPSRecoveryIntervalMinutes" type="number" min="1" :max="MAX_BPS_RECOVERY_INTERVAL_MINUTES" step="1" required
                data-testid="excel-bps-recovery-interval" class="input w-40" />
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPS403RecoveryIntervalHint') }}</p>
          </div>
        </div>
        <div v-if="excelBPSEnabled" class="mt-3">
          <label class="flex items-center gap-2">
            <input v-model="excelBPSAutoMoveOn403" type="checkbox"
              data-testid="excel-bps-auto-move-on-403"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500" />
            <span class="text-sm">{{ t('admin.accounts.openai.excelBPSAutoMoveOn403') }}</span>
          </label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSAutoMoveOn403Desc') }}</p>
          <div v-if="excelBPSAutoMoveOn403" class="mt-2">
            <label class="input-label">{{ t('admin.accounts.openai.excelBPS403TargetGroup') }}</label>
            <Select v-model="excelBPS403TargetGroupID" :options="excelBPS403GroupOptions"
              :aria-label="t('admin.accounts.openai.excelBPS403TargetGroup')"
              data-testid="excel-bps-403-target-group" />
          </div>
        </div>
        <div v-if="excelBPSEnabled" class="mt-3">
          <label class="flex items-center gap-2">
            <input v-model="excelBPSCacheCreationAsInput" type="checkbox"
              data-testid="excel-bps-cache-creation-as-input"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500" />
            <span class="text-sm">{{ t('admin.accounts.openai.excelBPSCacheCreationAsInput') }}</span>
          </label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSCacheCreationAsInputDesc') }}</p>
        </div>
      </div>

      <!-- OpenAI 自动透传开关（OAuth/API Key） -->
      <EditPassthroughPanel
  :account="account"
  :openaiPassthroughEnabled="openaiPassthroughEnabled"
  @update:openaiPassthroughEnabled="openaiPassthroughEnabled = $event"
/>

      <!-- OpenAI Codex namespace 工具摊平（兼容开关，仅 OAuth） -->
      <EditNamespaceToolsPanel
  :account="account"
  :openaiFlattenNamespacesEnabled="openaiFlattenNamespacesEnabled"
  @update:openaiFlattenNamespacesEnabled="openaiFlattenNamespacesEnabled = $event"
/>

      <!-- OpenAI Codex hosted image_generation bridge policy -->
      <EditImageToolPolicyPanel
  :account="account"
  :codexImageToolBadgeClass="codexImageToolBadgeClass"
  :codexImageToolBadgeLabel="codexImageToolBadgeLabel"
  :codexImageToolOptions="codexImageToolOptions"
  :codexImageToolMode="codexImageToolMode"
  @update:codexImageToolMode="codexImageToolMode = $event"
/>

      <!-- OpenAI WS Mode 三态（off/ctx_pool/passthrough） -->
      <EditWebSocketTransportPanel
  :account="account"
  :openAIWSModeHintKey="openAIWSModeHintKey"
  :openaiResponsesWebSocketV2Mode="openaiResponsesWebSocketV2Mode"
  :openAIWSModeOptions="openAIWSModeOptions"
  @update:openaiResponsesWebSocketV2Mode="openaiResponsesWebSocketV2Mode = $event"
/>

      <!-- OpenAI APIKey Responses API support mode -->
      <EditResponsesCapabilityPanel
  :account="account"
  :openAIResponsesMode="openAIResponsesMode"
  :openAIResponsesModeOptions="openAIResponsesModeOptions"
  :openAITextGenerationCapabilityEnabled="openAITextGenerationCapabilityEnabled"
  :openAIResponsesStatusKey="openAIResponsesStatusKey"
  :openAIEndpointCapabilityOptions="openAIEndpointCapabilityOptions"
  :openAIEndpointCapabilities="openAIEndpointCapabilities"
  :toggleOpenAIEndpointCapability="toggleOpenAIEndpointCapability"
  @update:openAIResponsesMode="openAIResponsesMode = $event"
/>

      <!-- OpenAI APIKey images: backfill b64_json from url -->
      <EditImageResponseFormatPanel
  :account="account"
  :openAIImagesUrlToB64JsonEnabled="openAIImagesUrlToB64JsonEnabled"
  @update:openAIImagesUrlToB64JsonEnabled="openAIImagesUrlToB64JsonEnabled = $event"
/>

      <div
        v-if="account?.type === 'apikey'"
        class="flex items-center justify-between gap-4 border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div>
          <label class="input-label mb-0">{{ t('admin.accounts.upstreamBilling.autoProbe') }}</label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.upstreamBilling.autoProbeHint') }}
          </p>
        </div>
        <Toggle
          :model-value="upstreamBillingAutoProbeEnabled"
          data-testid="upstream-billing-auto-probe"
          :aria-label="t('admin.accounts.upstreamBilling.autoProbe')"
          @update:model-value="handleUpstreamBillingAutoProbeChange"
        />
      </div>

      <OllamaCloudUsageSettings
        v-if="account?.ollama_cloud_usage?.eligible"
        :account="account"
        @updated="handleOllamaCloudUsageUpdated"
      />

      <section
        v-if="account?.opencode_go_usage?.eligible"
        class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-600"
        data-testid="opencode-go-usage-settings"
      >
        <div class="flex items-start justify-between gap-4">
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.accounts.opencodeGo.title') }}
            </h3>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.opencodeGo.panelHint') }}
            </p>
          </div>
          <span
            class="whitespace-nowrap rounded px-2 py-1 text-xs font-medium"
            :class="opencodeGoStatusOk
              ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
              : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'"
          >
            {{ opencodeGoStatusLabel }}
          </span>
        </div>

        <div v-if="opencodeGoLoading" class="flex h-20 items-center justify-center text-gray-400">
          <Icon name="refresh" size="sm" class="animate-spin" />
        </div>
        <template v-else>
          <div
            v-if="opencodeGoSnapshot"
            class="border-y border-gray-100 py-3 dark:border-dark-700"
            data-testid="opencode-go-usage-details"
          >
            <div class="grid grid-cols-[minmax(4rem,auto)_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-xs">
              <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.rolling') }}</span>
              <span class="break-words text-gray-900 dark:text-white">{{ opencodeGoWindowSummary(opencodeGoSnapshot.data?.rolling) }}</span>
              <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.weekly') }}</span>
              <span class="break-words text-gray-900 dark:text-white">{{ opencodeGoWindowSummary(opencodeGoSnapshot.data?.weekly) }}</span>
              <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.monthly') }}</span>
              <span class="break-words text-gray-900 dark:text-white">{{ opencodeGoWindowSummary(opencodeGoSnapshot.data?.monthly) }}</span>
              <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.status') }}</span>
              <span class="break-words font-medium text-gray-900 dark:text-white">{{ opencodeGoStatusLabel }}</span>
              <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.updatedAt') }}</span>
              <span class="break-words text-gray-900 dark:text-white">{{ opencodeGoFormatDate(opencodeGoSnapshot.fetched_at || opencodeGoSnapshot.last_attempt_at) }}</span>
            </div>
            <p
              v-if="opencodeGoSnapshot.last_error"
              class="mt-2 break-words border-t border-gray-100 pt-2 text-xs text-amber-700 dark:border-dark-700 dark:text-amber-300"
            >
              {{ t(`admin.accounts.opencodeGo.errors.${opencodeGoSnapshot.last_error}`, opencodeGoSnapshot.last_error) }}
            </p>
          </div>

          <div class="flex flex-wrap items-center gap-2">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="opencodeGoRefreshing"
              data-testid="opencode-go-refresh"
              @click="refreshOpenCodeGoUsage"
            >
              <Icon name="refresh" size="xs" class="mr-1.5" :class="{ 'animate-spin': opencodeGoRefreshing }" />
              {{ t('admin.accounts.opencodeGo.refreshNow') }}
            </button>
          </div>

          <div class="flex items-center justify-between gap-4 border-t border-gray-100 pt-4 dark:border-dark-700">
            <div>
              <label class="text-sm font-medium text-gray-900 dark:text-white">
                {{ t('admin.accounts.opencodeGo.autoRefresh') }}
              </label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.opencodeGo.autoRefreshHint') }}
              </p>
            </div>
            <Toggle
              :model-value="opencodeGoState?.auto_refresh_enabled ?? false"
              :disabled="opencodeGoSaving"
              data-testid="opencode-go-auto-refresh"
              @update:model-value="setOpenCodeGoAutoRefresh"
            />
          </div>
        </template>
      </section>

      <!-- Anthropic API Key 自动透传开关 -->
      <EditPassthrough2Panel
  :account="account"
  :anthropicPassthroughEnabled="anthropicPassthroughEnabled"
  @update:anthropicPassthroughEnabled="anthropicPassthroughEnabled = $event"
/>

      <div
        v-if="account?.platform === 'anthropic' && account?.type === 'apikey'"
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

      <!-- Anthropic / OpenAI API Key: Web Search Emulation (hidden when global disabled) -->
      <EditWebSearchPanel
  :account="account"
  :webSearchGlobalEnabled="webSearchGlobalEnabled"
  :webSearchEmulationMode="webSearchEmulationMode"
  @update:webSearchEmulationMode="webSearchEmulationMode = $event"
/>

      <!-- 配额控制 (Anthropic apikey/bedrock: 配额限制 + 亲和) -->
      <EditQuotaLimitsPanel
  :account="account"
  :editQuotaLimit="editQuotaLimit"
  :editQuotaDailyLimit="editQuotaDailyLimit"
  :editQuotaWeeklyLimit="editQuotaWeeklyLimit"
  :editDailyResetMode="editDailyResetMode"
  :editDailyResetHour="editDailyResetHour"
  :editWeeklyResetMode="editWeeklyResetMode"
  :editWeeklyResetDay="editWeeklyResetDay"
  :editWeeklyResetHour="editWeeklyResetHour"
  :editResetTimezone="editResetTimezone"
  :quotaNotifyGlobalEnabled="quotaNotifyGlobalEnabled"
  :quotaNotifyState="quotaNotifyState"
  @update:editQuotaLimit="editQuotaLimit = $event"
  @update:editQuotaDailyLimit="editQuotaDailyLimit = $event"
  @update:editQuotaWeeklyLimit="editQuotaWeeklyLimit = $event"
  @update:editDailyResetMode="editDailyResetMode = $event"
  @update:editDailyResetHour="editDailyResetHour = $event"
  @update:editWeeklyResetMode="editWeeklyResetMode = $event"
  @update:editWeeklyResetDay="editWeeklyResetDay = $event"
  @update:editWeeklyResetHour="editWeeklyResetHour = $event"
  @update:editResetTimezone="editResetTimezone = $event"
  @update:quotaNotifyState="Object.assign(quotaNotifyState, $event)"
/>

      <!-- OpenAI API 长上下文计费开关 -->
      <EditLongContextBillingPanel
  :account="account"
  :isSparkShadow="isSparkShadow"
  :hideAccountLongContextBilling="hideAccountLongContextBilling"
  :openAILongContextBillingEnabled="openAILongContextBillingEnabled"
  @update:openAILongContextBillingEnabled="openAILongContextBillingEnabled = $event"
/>

      <div
        v-if="account?.platform === 'openai' && (account?.type === 'oauth' || account?.type === 'setup-token')"
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
      <EditCodexFingerprintPanel
  :account="account"
  :codexFingerprintMode="codexFingerprintMode"
  :codexFingerprintModeOptions="codexFingerprintModeOptions"
  @update:codexFingerprintMode="codexFingerprintMode = $event"
/>

      <!-- OpenAI 订阅档位手动覆盖（Plus/Pro/Free），仅 OAuth 非影子账号 -->
      <EditSubscriptionDisplayPanel
  :account="account"
  :isSparkShadow="isSparkShadow"
  :editPlanType="editPlanType"
  :planTypeOptions="planTypeOptions"
  @update:editPlanType="editPlanType = $event"
/>

      <div
        v-if="account?.platform === 'openai' && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.compactMode') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.compactModeDesc') }}
            </p>
          </div>
          <div class="w-44">
            <Select v-model="openAICompactMode" :options="openAICompactModeOptions" />
          </div>
        </div>
        <div class="rounded-lg bg-gray-50 px-3 py-2 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">
          <span class="font-medium">{{ t(openAICompactStatusKey) }}</span>
          <span
            v-if="account?.extra?.openai_compact_checked_at"
            class="ml-2 text-gray-500 dark:text-gray-400"
          >
            {{ t('admin.accounts.openai.compactLastChecked') }}:
            {{ formatDateTime(new Date(String(account.extra.openai_compact_checked_at))) }}
          </span>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.openai.compactModelMapping') }}</label>
          <p class="input-hint">{{ t('admin.accounts.openai.compactModelMappingDesc') }}</p>
          <div v-if="openAICompactModelMappings.length > 0" class="mb-3 space-y-2">
            <div
              v-for="(mapping, index) in openAICompactModelMappings"
              :key="getOpenAICompactModelMappingKey(mapping)"
              class="flex items-center gap-2"
            >
              <input
                v-model="mapping.from"
                type="text"
                class="input flex-1"
                :placeholder="t('admin.accounts.fromModel')"
              />
              <span class="text-gray-400">→</span>
              <input
                v-model="mapping.to"
                type="text"
                class="input flex-1"
                :placeholder="t('admin.accounts.toModel')"
              />
              <button type="button" @click="removeOpenAICompactModelMapping(index)" class="text-red-500 hover:text-red-700">
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </div>
          <button type="button" @click="addOpenAICompactModelMapping" class="btn btn-secondary text-sm">
            + {{ t('admin.accounts.addMapping') }}
          </button>
        </div>
      </div>

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

      <div
        v-if="account?.platform === 'openai'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
      >
        <div class="space-y-2">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('admin.accounts.autoPause5hDisabled') }}</label>
            <button
              type="button"
              @click="autoPause5hDisabled = !autoPause5hDisabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                autoPause5hDisabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
              data-testid="auto-pause-5h-disabled"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  autoPause5hDisabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <p class="input-hint">{{ t('admin.accounts.autoPauseDisabledHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.autoPause5hThreshold') }}</label>
          <input
            v-model.number="autoPause5hThreshold"
            type="number"
            min="0"
            max="100"
            step="0.1"
            class="input"
            :disabled="autoPause5hDisabled"
            data-testid="auto-pause-5h-threshold"
          />
          <p class="input-hint">{{ t('admin.accounts.autoPauseThresholdHint') }}</p>
        </div>
        <div class="space-y-2">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('admin.accounts.autoPause7dDisabled') }}</label>
            <button
              type="button"
              @click="autoPause7dDisabled = !autoPause7dDisabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                autoPause7dDisabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
              data-testid="auto-pause-7d-disabled"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  autoPause7dDisabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <p class="input-hint">{{ t('admin.accounts.autoPauseDisabledHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.autoPause7dThreshold') }}</label>
          <input
            v-model.number="autoPause7dThreshold"
            type="number"
            min="0"
            max="100"
            step="0.1"
            class="input"
            :disabled="autoPause7dDisabled"
            data-testid="auto-pause-7d-threshold"
          />
          <p class="input-hint">{{ t('admin.accounts.autoPauseThresholdHint') }}</p>
        </div>
      </div>

      <div
        v-if="account?.platform === 'openai' && account?.type === 'oauth' && !isSparkShadow"
        class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-600"
        data-testid="auto-reset-credit-settings"
      >
        <div class="flex items-center justify-between gap-4">
          <div class="min-w-0">
            <label class="input-label mb-0">{{ t('admin.accounts.autoResetCredit.title') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.autoResetCredit.hint') }}
            </p>
          </div>
          <button
            type="button"
            data-testid="auto-reset-credit-enabled"
            @click="autoResetCreditEnabled = !autoResetCreditEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              autoResetCreditEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                autoResetCreditEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.accounts.autoResetCredit.threshold5h') }}</label>
            <input
              v-model.number="autoResetCredit5hThreshold"
              type="number"
              min="0.1"
              max="100"
              step="0.1"
              class="input"
              :disabled="!autoResetCreditEnabled"
              data-testid="auto-reset-credit-5h-threshold"
            />
          </div>
          <div>
            <label class="input-label">{{ t('admin.accounts.autoResetCredit.threshold7d') }}</label>
            <input
              v-model.number="autoResetCredit7dThreshold"
              type="number"
              min="0.1"
              max="100"
              step="0.1"
              class="input"
              :disabled="!autoResetCreditEnabled"
              data-testid="auto-reset-credit-7d-threshold"
            />
          </div>
        </div>
        <p class="input-hint">{{ t('admin.accounts.autoResetCredit.thresholdHint') }}</p>
      </div>

      <!-- 配额控制 (Anthropic OAuth/SetupToken: 亲和 + 窗口费用 + 会话 + RPM 等) -->
      <EditAnthropicRuntimeLimitsPanel
  :account="account"
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
  :allowTransportEdit="allowTransportEdit"
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

      <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div>
          <label class="input-label">{{ t('common.status') }}</label>
          <Select v-model="form.status" :options="statusOptions" />
        </div>

        <!-- Mixed Scheduling (only for antigravity accounts, read-only in edit mode) -->
        <div v-if="account?.platform === 'antigravity'" class="flex items-center gap-2">
          <label class="flex cursor-not-allowed items-center gap-2 opacity-60">
            <input
              type="checkbox"
              v-model="mixedScheduling"
              disabled
              class="h-4 w-4 cursor-not-allowed rounded border-gray-300 text-primary-500 focus:ring-primary-500 dark:border-dark-500"
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
        <div v-if="account?.platform === 'antigravity'" class="mt-3 flex items-center gap-2">
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
      </div>

      </template>
      <!-- Group Selection - 仅标准模式显示 -->
      <GroupSelector
        v-model="form.group_ids"
        :groups="selectableGroups"
        :platform="account?.platform"
        :mixed-scheduling="mixedScheduling"
        data-tour="account-form-groups"
      />

    </form>

    <template #footer>
      <div v-if="account" class="flex justify-end gap-3">
        <button @click="handleClose" type="button" class="btn btn-secondary">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="edit-account-form"
          :disabled="submitting || bpsStateConflict"
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
          {{ submitting ? t('admin.accounts.updating') : t('common.update') }}
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
