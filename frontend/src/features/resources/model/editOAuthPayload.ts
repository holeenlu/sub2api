import type { Account } from '@/features/resources/api/accountTypes';
import { applyAntigravityProjectID, applyHeaderOverride, applyPlanType, validateHeaderOverrideRows } from "@/features/resources/model/credentialsBuilder"
import { buildModelMappingObject } from '../model/modelRestrictions'
import type { EditSubmissionInput } from './editSubmission'



/** Applies only oauth fields to the local outgoing payload. */
export function applyOAuthUpdate(input: Pick<EditSubmissionInput, "hasOAuthModelRestriction" | "isSparkShadow" | "applyOpenAIModelMappingCredentials" | "buildModelRestrictionMapping" | "grokOAuthCustomBaseUrlEnabled" | "grokOAuthBaseUrl" | "appStore" | "t" | "headerOverrideEnabled" | "headerOverrideRows" | "GROK_CLIENT_TOOL_CACHE_EXTRA_KEY" | "grokClientToolCacheEnabled" | "editPlanType" | "antigravityProjectId" | "antigravityModelMappings" | "mixedScheduling" | "allowOverages">, account: Account, updatePayload: Record<string, unknown>): boolean {
  if (input.hasOAuthModelRestriction.value) {
    const currentCredentials = input.isSparkShadow.value
      ? {}
      : (updatePayload.credentials as Record<string, unknown>) ||
      ((account.credentials as Record<string, unknown>) || {})
    const newCredentials: Record<string, unknown> = { ...currentCredentials }
    if (account.platform === 'openai') {
      input.applyOpenAIModelMappingCredentials(newCredentials)
    } else {
      const modelMapping = input.buildModelRestrictionMapping()
      if (modelMapping) {
        newCredentials.model_mapping = modelMapping
      } else {
        delete newCredentials.model_mapping
      }
    }

    updatePayload.credentials = newCredentials
  }

  // Grok OAuth: 自定义上游地址 + 请求头覆写。base_url 仅改写转发端点，
  // OAuth 授权与令牌刷新链路不读取该值；关闭开关即恢复默认官方网关。
  if (account.platform === 'grok' && account.type === 'oauth') {
    const currentCredentials =
      (updatePayload.credentials as Record<string, unknown>) ||
      ((account.credentials as Record<string, unknown>) || {})
    const newCredentials: Record<string, unknown> = { ...currentCredentials }

    if (input.grokOAuthCustomBaseUrlEnabled.value) {
      const trimmedBaseUrl = input.grokOAuthBaseUrl.value.trim()
      if (!trimmedBaseUrl) {
        input.appStore.showError(input.t('admin.accounts.grokCustomBaseUrl.required'))
        return false
      }
      if (!/^https?:\/\//i.test(trimmedBaseUrl)) {
        input.appStore.showError(input.t('admin.accounts.grokCustomBaseUrl.invalid'))
        return false
      }
      newCredentials.base_url = trimmedBaseUrl
    } else {
      delete newCredentials.base_url
    }

    if (input.headerOverrideEnabled.value) {
      const headerError = validateHeaderOverrideRows(input.headerOverrideRows.value)
      if (headerError) {
        input.appStore.showError(input.t(`admin.accounts.headerOverride.${headerError}`))
        return false
      }
    }
    applyHeaderOverride(newCredentials, input.headerOverrideEnabled.value, input.headerOverrideRows.value, 'edit')

    updatePayload.credentials = newCredentials

    const newExtra: Record<string, unknown> = {
      ...((account.extra as Record<string, unknown>) || {})
    }
    // Persist both states so a disabled account remains opted out when the
    // backend applies the default-enabled policy to missing values.
    newExtra[input.GROK_CLIENT_TOOL_CACHE_EXTRA_KEY] = input.grokClientToolCacheEnabled.value
    updatePayload.extra = newExtra
  }

  // OpenAI: 手动覆盖订阅档位 plan_type（Plus / Pro 20x / Pro 5x / Business Standard / Business Premium / Free）。
  // 仅 OAuth 非影子账号：
  // 影子账号凭据由母账号管理(且后端会 sanitize),setup-token 无订阅调度语义。
  if (account.platform === 'openai' && account.type === 'oauth' && !input.isSparkShadow.value) {
    const currentCredentials = (updatePayload.credentials as Record<string, unknown>) ||
      ((account.credentials as Record<string, unknown>) || {})
    updatePayload.credentials = applyPlanType({ ...currentCredentials }, input.editPlanType.value)
  }

  // Antigravity: persist model mapping to credentials (applies to all antigravity types)
  // Antigravity 只支持映射模式
  if (account.platform === 'antigravity') {
    const currentCredentials = (updatePayload.credentials as Record<string, unknown>) ||
      ((account.credentials as Record<string, unknown>) || {})
    const newCredentials: Record<string, unknown> = { ...currentCredentials }
    if (account.type === 'oauth') {
      applyAntigravityProjectID(newCredentials, input.antigravityProjectId.value, 'edit')
    }

    // 移除旧字段
    delete newCredentials.model_whitelist
    delete newCredentials.model_mapping

    // 只使用映射模式
    const antigravityModelMapping = buildModelMappingObject(
      'mapping',
      [],
      input.antigravityModelMappings.value
    )
    if (antigravityModelMapping) {
      newCredentials.model_mapping = antigravityModelMapping
    }

    updatePayload.credentials = newCredentials
  }

  // For antigravity accounts, handle mixed_scheduling and allow_overages in extra
  if (account.platform === 'antigravity') {
    const currentExtra = (account.extra as Record<string, unknown>) || {}
    const newExtra: Record<string, unknown> = { ...currentExtra }
    if (input.mixedScheduling.value) {
      newExtra.mixed_scheduling = true
    } else {
      delete newExtra.mixed_scheduling
    }
    if (input.allowOverages.value) {
      newExtra.allow_overages = true
    } else {
      delete newExtra.allow_overages
    }
    updatePayload.extra = newExtra
  }
  return true
}
