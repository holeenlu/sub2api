import type { Account } from '@/features/resources/api/accountTypes';
import { applyExplicitCacheUpstreamCredentials, isOfficialGeminiBaseUrl } from '@/features/resources/model/geminiExplicitCache'
import { applyHeaderOverride, applyInterceptWarmup, applyOpenCodeGoProtocolRules, validateHeaderOverrideRows } from "@/features/resources/model/credentialsBuilder"
import { buildModelMappingObject } from '../model/modelRestrictions'
import type { EditSubmissionInput } from './editSubmission'



/** Applies only credential fields to the local outgoing payload. */
export function applyCredentialUpdate(input: Pick<EditSubmissionInput, "props" | "editBaseUrl" | "defaultBaseUrl" | "openaiPassthroughEnabled" | "isCNApiKeyAccount" | "currentOpenCodeOrCNMode" | "editApiProtocol" | "defaultCNAdaptiveBaseUrls" | "adaptivePresetPlatform" | "editAdaptiveProtocolOptions" | "editAdaptiveBaseUrls" | "providerRoutesByModel" | "editOpenCodeGoProtocolRules" | "editZhipuOrganization" | "editZhipuProject" | "editApiKey" | "appStore" | "t" | "buildModelRestrictionMapping" | "applyOpenAIEndpointCapabilities" | "openAICompactModelMappings" | "poolModeEnabled" | "normalizePoolModeRetryCount" | "poolModeRetryCount" | "parsePoolModeRetryStatusCodes" | "poolModeRetryStatusCodesInput" | "explicitCacheUpstreamEnabled" | "explicitCacheUpstreamMaxTTL" | "customErrorCodesEnabled" | "selectedErrorCodes" | "isHeaderOverrideCapable" | "headerOverrideEnabled" | "headerOverrideRows" | "interceptWarmupRequests" | "applyAccountSchedulingThresholdOverridePatch" | "applyTempUnschedConfig" | "isVertexAPIKeyMode" | "editVertexApiKey" | "editVertexProjectId" | "editVertexLocation" | "editVertexClientEmail" | "editBedrockRegion" | "editBedrockForceGlobal" | "isBedrockAPIKeyMode" | "editBedrockApiKeyValue" | "editBedrockAccessKeyId" | "editBedrockSecretAccessKey" | "editBedrockSessionToken">, account: Account, updatePayload: Record<string, unknown>): boolean {
  // Disabled authorization inputs can still contain drafts from an earlier permission scope.
  const canAuthorize = input.props.allowAuthorize !== false
  if (account.type === 'apikey') {
    const currentCredentials = (account.credentials as Record<string, unknown>) || {}
    const newBaseUrl = input.editBaseUrl.value.trim() || input.defaultBaseUrl.value
    const shouldApplyModelMapping = !(account.platform === 'openai' && input.openaiPassthroughEnabled.value)

    // Always update credentials for apikey type to handle model mapping changes
    const newCredentials: Record<string, unknown> = {
      ...currentCredentials,
      base_url: newBaseUrl
    }

    // 国产供应商：模式与协议写入凭据（决定额度/余额探测与转发端点/格式）。
    if (input.isCNApiKeyAccount.value) {
      newCredentials.account_mode = input.currentOpenCodeOrCNMode()
      newCredentials.api_protocol = input.editApiProtocol.value
      if (input.editApiProtocol.value === 'adaptive') {
        const defaults = input.defaultCNAdaptiveBaseUrls(input.adaptivePresetPlatform.value, input.currentOpenCodeOrCNMode())
        const protocolBaseUrls: Record<string, string> = {}
        for (const item of input.editAdaptiveProtocolOptions.value) {
          protocolBaseUrls[item.value] = (input.editAdaptiveBaseUrls.value[item.value] || defaults[item.value]).trim()
        }
        newCredentials.api_base_urls = protocolBaseUrls
        newCredentials.base_url = protocolBaseUrls.chat_completions
      } else {
        delete newCredentials.api_base_urls
      }
      if (input.providerRoutesByModel(account.platform)) {
        applyOpenCodeGoProtocolRules(newCredentials, input.editOpenCodeGoProtocolRules.value, 'edit')
      }
      // 智谱团队版 Coding Plan：组织/项目 ID 写入凭据（非空才写，清空即移除回落个人版路径）
      if (account.platform === 'zhipu') {
        const org = input.editZhipuOrganization.value.trim()
        const project = input.editZhipuProject.value.trim()
        if (org) {
          newCredentials.zhipu_organization = org
          if (project) newCredentials.zhipu_project = project
          else delete newCredentials.zhipu_project
        } else {
          delete newCredentials.zhipu_organization
          delete newCredentials.zhipu_project
        }
      }
    }

    // Handle API key
    // 后端响应已脱敏：currentCredentials 不会再包含 api_key 原文。
    // 用户填入新值则覆盖；留空时优先看 credentials_status.has_api_key；
    // 若后端尚未升级（无 credentials_status），回退读旧结构 currentCredentials.api_key。
    // 两者都无才报错。
    const hasExistingApiKey =
      account.credentials_status?.has_api_key ?? Boolean(currentCredentials.api_key)
    if (canAuthorize && input.editApiKey.value.trim()) {
      newCredentials.api_key = input.editApiKey.value.trim()
    } else if (!hasExistingApiKey) {
      input.appStore.showError(input.t('admin.accounts.apiKeyIsRequired'))
      return false
    }

    // Add model mapping if configured（OpenAI 开启自动透传时保留现有映射，不再编辑）
    if (shouldApplyModelMapping) {
      const modelMapping = input.buildModelRestrictionMapping()
      if (modelMapping) {
        newCredentials.model_mapping = modelMapping
      } else {
        delete newCredentials.model_mapping
      }
    } else if (currentCredentials.model_mapping) {
      newCredentials.model_mapping = currentCredentials.model_mapping
    }
    if (account.platform === 'openai') {
      input.applyOpenAIEndpointCapabilities(newCredentials)
      const compactModelMapping = buildModelMappingObject('mapping', [], input.openAICompactModelMappings.value)
      if (compactModelMapping) {
        newCredentials.compact_model_mapping = compactModelMapping
      } else {
        delete newCredentials.compact_model_mapping
      }
    }

    // Add pool mode if enabled
    if (input.poolModeEnabled.value) {
      newCredentials.pool_mode = true
      newCredentials.pool_mode_retry_count = input.normalizePoolModeRetryCount(input.poolModeRetryCount.value)
      const parsedRetryStatusCodes = input.parsePoolModeRetryStatusCodes(input.poolModeRetryStatusCodesInput.value)
      if (parsedRetryStatusCodes.length > 0) {
        newCredentials.pool_mode_retry_status_codes = parsedRetryStatusCodes
      } else {
        delete newCredentials.pool_mode_retry_status_codes
      }
    } else {
      delete newCredentials.pool_mode
      delete newCredentials.pool_mode_retry_count
      delete newCredentials.pool_mode_retry_status_codes
    }

    applyExplicitCacheUpstreamCredentials(
      newCredentials,
      account.platform === 'gemini' && !isOfficialGeminiBaseUrl(input.editBaseUrl.value) && input.explicitCacheUpstreamEnabled.value,
      input.explicitCacheUpstreamMaxTTL.value
    )

    // Add custom error codes if enabled
    if (input.customErrorCodesEnabled.value) {
      newCredentials.custom_error_codes_enabled = true
      newCredentials.custom_error_codes = [...input.selectedErrorCodes.value]
    } else {
      delete newCredentials.custom_error_codes_enabled
      delete newCredentials.custom_error_codes
    }

    // Add header override if enabled for this API-key platform
    if (input.isHeaderOverrideCapable(account.platform, 'apikey')) {
      if (input.headerOverrideEnabled.value) {
        const headerError = validateHeaderOverrideRows(input.headerOverrideRows.value)
        if (headerError) {
          input.appStore.showError(input.t(`admin.accounts.headerOverride.${headerError}`))
          return false
        }
      }
      applyHeaderOverride(newCredentials, input.headerOverrideEnabled.value, input.headerOverrideRows.value, 'edit')
    }

    // Add intercept warmup requests setting
    applyInterceptWarmup(newCredentials, input.interceptWarmupRequests.value, 'edit')
    input.applyAccountSchedulingThresholdOverridePatch(newCredentials, currentCredentials)
    if (!input.applyTempUnschedConfig(newCredentials)) {
      return false
    }

    updatePayload.credentials = newCredentials
  } else if (account.type === 'upstream') {
    const currentCredentials = (account.credentials as Record<string, unknown>) || {}
    const newCredentials: Record<string, unknown> = { ...currentCredentials }

    newCredentials.base_url = input.editBaseUrl.value.trim()

    if (canAuthorize && input.editApiKey.value.trim()) {
      newCredentials.api_key = input.editApiKey.value.trim()
    }

    // Add intercept warmup requests setting
    applyInterceptWarmup(newCredentials, input.interceptWarmupRequests.value, 'edit')

    input.applyAccountSchedulingThresholdOverridePatch(newCredentials, currentCredentials)
    if (!input.applyTempUnschedConfig(newCredentials)) {
      return false
    }

    updatePayload.credentials = newCredentials
  } else if ((account.platform === 'gemini' || account.platform === 'anthropic') && account.type === 'service_account') {
    const currentCredentials = (account.credentials as Record<string, unknown>) || {}
    const newCredentials: Record<string, unknown> = { ...currentCredentials }
    const credentialsStatus = account.credentials_status

    if (input.isVertexAPIKeyMode.value) {
      // API Key 已脱敏不再随 credentials 返回，存在性读 credentials_status；留空表示保留原 Key。
      const newApiKey = canAuthorize ? input.editVertexApiKey.value.trim() : ''
      const hasExistingApiKey = credentialsStatus
        ? Boolean(credentialsStatus.has_api_key)
        : Boolean(currentCredentials.api_key)
      if (!newApiKey && !hasExistingApiKey) {
        input.appStore.showError(input.t('admin.accounts.vertexApiKeyRequired'))
        return false
      }
      const projectId = input.editVertexProjectId.value.trim()
      if (projectId && !input.editVertexLocation.value.trim()) {
        input.appStore.showError(input.t('admin.accounts.vertexLocationRequired'))
        return false
      }
      if (newApiKey) {
        newCredentials.api_key = newApiKey
      }
      if (projectId) {
        newCredentials.project_id = projectId
      } else {
        delete newCredentials.project_id
      }
      newCredentials.location = input.editVertexLocation.value.trim() || 'global'
    } else {
      if (!input.editVertexProjectId.value.trim()) {
        input.appStore.showError(input.t('admin.accounts.vertexSaJsonMissingProjectId'))
        return false
      }
      if (!input.editVertexClientEmail.value.trim()) {
        input.appStore.showError(input.t('admin.accounts.vertexSaJsonMissingClientEmail'))
        return false
      }
      if (!input.editVertexLocation.value.trim()) {
        input.appStore.showError(input.t('admin.accounts.vertexLocationRequired'))
        return false
      }

      // SA JSON 已脱敏不再随 credentials 返回，存在性优先读 credentials_status。
      // 若后端尚未升级（无 credentials_status），回退读旧结构 service_account_json / service_account。
      const hasExistingServiceAccountJson = credentialsStatus
        ? Boolean(
          credentialsStatus.has_service_account_json || credentialsStatus.has_service_account
        )
        : Boolean(currentCredentials.service_account_json || currentCredentials.service_account)
      if (!hasExistingServiceAccountJson) {
        input.appStore.showError(input.t('admin.accounts.vertexSaJsonRequired'))
        return false
      }
      newCredentials.project_id = input.editVertexProjectId.value.trim()
      newCredentials.client_email = input.editVertexClientEmail.value.trim()
      newCredentials.location = input.editVertexLocation.value.trim()
    }
    newCredentials.tier_id = 'vertex'

    // Add model mapping if configured
    const modelMapping = input.buildModelRestrictionMapping()
    if (modelMapping) {
      newCredentials.model_mapping = modelMapping
    } else {
      delete newCredentials.model_mapping
    }

    applyInterceptWarmup(newCredentials, input.interceptWarmupRequests.value, 'edit')
    input.applyAccountSchedulingThresholdOverridePatch(newCredentials, currentCredentials)
    if (!input.applyTempUnschedConfig(newCredentials)) {
      return false
    }

    updatePayload.credentials = newCredentials
  } else if (account.type === 'bedrock') {
    const currentCredentials = (account.credentials as Record<string, unknown>) || {}
    const newCredentials: Record<string, unknown> = { ...currentCredentials }

    newCredentials.aws_region = input.editBedrockRegion.value.trim()
    if (input.editBedrockForceGlobal.value) {
      newCredentials.aws_force_global = 'true'
    } else {
      delete newCredentials.aws_force_global
    }

    if (input.isBedrockAPIKeyMode.value) {
      // API Key mode: only update api_key if user provided new value
      if (canAuthorize && input.editBedrockApiKeyValue.value.trim()) {
        newCredentials.api_key = input.editBedrockApiKeyValue.value.trim()
      }
    } else if (canAuthorize) {
      // SigV4 identity and secrets remain unchanged when authorization is delegated away.
      newCredentials.aws_access_key_id = input.editBedrockAccessKeyId.value.trim()
      if (input.editBedrockSecretAccessKey.value.trim()) {
        newCredentials.aws_secret_access_key = input.editBedrockSecretAccessKey.value.trim()
      }
      if (input.editBedrockSessionToken.value.trim()) {
        newCredentials.aws_session_token = input.editBedrockSessionToken.value.trim()
      }
    }

    // Pool mode
    if (input.poolModeEnabled.value) {
      newCredentials.pool_mode = true
      newCredentials.pool_mode_retry_count = input.normalizePoolModeRetryCount(input.poolModeRetryCount.value)
      const parsedRetryStatusCodes = input.parsePoolModeRetryStatusCodes(input.poolModeRetryStatusCodesInput.value)
      if (parsedRetryStatusCodes.length > 0) {
        newCredentials.pool_mode_retry_status_codes = parsedRetryStatusCodes
      } else {
        delete newCredentials.pool_mode_retry_status_codes
      }
    } else {
      delete newCredentials.pool_mode
      delete newCredentials.pool_mode_retry_count
      delete newCredentials.pool_mode_retry_status_codes
    }

    // Model mapping
    const modelMapping = input.buildModelRestrictionMapping()
    if (modelMapping) {
      newCredentials.model_mapping = modelMapping
    } else {
      delete newCredentials.model_mapping
    }

    applyInterceptWarmup(newCredentials, input.interceptWarmupRequests.value, 'edit')
    input.applyAccountSchedulingThresholdOverridePatch(newCredentials, currentCredentials)
    if (!input.applyTempUnschedConfig(newCredentials)) {
      return false
    }

    updatePayload.credentials = newCredentials
  } else {
    // For oauth/setup-token types, only update intercept_warmup_requests if changed
    const currentCredentials = (account.credentials as Record<string, unknown>) || {}
    const newCredentials: Record<string, unknown> = { ...currentCredentials }

    applyInterceptWarmup(newCredentials, input.interceptWarmupRequests.value, 'edit')
    input.applyAccountSchedulingThresholdOverridePatch(newCredentials, currentCredentials)
    if (!input.applyTempUnschedConfig(newCredentials)) {
      return false
    }

    updatePayload.credentials = newCredentials
  }
  return true
}
