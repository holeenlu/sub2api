import type { Account } from '@/features/resources/api/accountTypes';
import type { EditSubmissionInput } from './editSubmission'



/** Applies only anthropictransport fields to the local outgoing payload. */
export function applyAnthropicTransportUpdate(input: Pick<EditSubmissionInput, "windowCostEnabled" | "windowCostLimit" | "windowCostStickyReserve" | "sessionLimitEnabled" | "maxSessions" | "sessionIdleTimeout" | "rpmLimitEnabled" | "baseRpm" | "rpmStrategy" | "rpmStickyBuffer" | "userMsgQueueMode" | "tlsFingerprintEnabled" | "tlsFingerprintProfileId" | "sessionIdMaskingEnabled" | "cacheTTLOverrideEnabled" | "cacheTTLOverrideTarget" | "customBaseUrlEnabled" | "customBaseUrl" | "anthropicPassthroughEnabled" | "anthropicAPIKeyAuthScheme" | "webSearchEmulationMode">, account: Account, updatePayload: Record<string, unknown>): boolean {
  if (account.platform === 'anthropic' && (account.type === 'oauth' || account.type === 'setup-token')) {
    const currentExtra = (updatePayload.extra as Record<string, unknown>) || (account.extra as Record<string, unknown>) || {}
    const newExtra: Record<string, unknown> = { ...currentExtra }

    // Window cost limit settings
    if (input.windowCostEnabled.value && input.windowCostLimit.value != null && input.windowCostLimit.value > 0) {
      newExtra.window_cost_limit = input.windowCostLimit.value
      newExtra.window_cost_sticky_reserve = input.windowCostStickyReserve.value ?? 10
    } else {
      delete newExtra.window_cost_limit
      delete newExtra.window_cost_sticky_reserve
    }

    // Session limit settings
    if (input.sessionLimitEnabled.value && input.maxSessions.value != null && input.maxSessions.value > 0) {
      newExtra.max_sessions = input.maxSessions.value
      newExtra.session_idle_timeout_minutes = input.sessionIdleTimeout.value ?? 5
    } else {
      delete newExtra.max_sessions
      delete newExtra.session_idle_timeout_minutes
    }

    // RPM limit settings
    if (input.rpmLimitEnabled.value) {
      const DEFAULT_BASE_RPM = 15
      newExtra.base_rpm = (input.baseRpm.value != null && input.baseRpm.value > 0)
        ? input.baseRpm.value
        : DEFAULT_BASE_RPM
      newExtra.rpm_strategy = input.rpmStrategy.value
      if (input.rpmStickyBuffer.value != null && input.rpmStickyBuffer.value > 0) {
        newExtra.rpm_sticky_buffer = input.rpmStickyBuffer.value
      } else {
        delete newExtra.rpm_sticky_buffer
      }
    } else {
      delete newExtra.base_rpm
      delete newExtra.rpm_strategy
      delete newExtra.rpm_sticky_buffer
    }

    // UMQ mode（独立于 RPM 保存）
    if (input.userMsgQueueMode.value) {
      newExtra.user_msg_queue_mode = input.userMsgQueueMode.value
    } else {
      delete newExtra.user_msg_queue_mode
    }
    delete newExtra.user_msg_queue_enabled  // 清理旧字段

    // TLS fingerprint setting
    if (input.tlsFingerprintEnabled.value) {
      newExtra.enable_tls_fingerprint = true
      if (input.tlsFingerprintProfileId.value) {
        newExtra.tls_fingerprint_profile_id = input.tlsFingerprintProfileId.value
      } else {
        delete newExtra.tls_fingerprint_profile_id
      }
    } else {
      delete newExtra.enable_tls_fingerprint
      delete newExtra.tls_fingerprint_profile_id
    }

    // Session ID masking setting
    if (input.sessionIdMaskingEnabled.value) {
      newExtra.session_id_masking_enabled = true
    } else {
      delete newExtra.session_id_masking_enabled
    }

    // Cache TTL override setting
    if (input.cacheTTLOverrideEnabled.value) {
      newExtra.cache_ttl_override_enabled = true
      newExtra.cache_ttl_override_target = input.cacheTTLOverrideTarget.value
    } else {
      delete newExtra.cache_ttl_override_enabled
      delete newExtra.cache_ttl_override_target
    }

    // Custom base URL relay setting
    if (input.customBaseUrlEnabled.value && input.customBaseUrl.value.trim()) {
      newExtra.custom_base_url_enabled = true
      newExtra.custom_base_url = input.customBaseUrl.value.trim()
    } else {
      delete newExtra.custom_base_url_enabled
      delete newExtra.custom_base_url
    }

    updatePayload.extra = newExtra
  }

  // For Anthropic API Key accounts, handle passthrough mode + web search emulation in extra
  if (account.platform === 'anthropic' && account.type === 'apikey') {
    const currentExtra = (updatePayload.extra as Record<string, unknown>) || (account.extra as Record<string, unknown>) || {}
    const newExtra: Record<string, unknown> = { ...currentExtra }
    if (input.anthropicPassthroughEnabled.value) {
      newExtra.anthropic_passthrough = true
    } else {
      delete newExtra.anthropic_passthrough
    }
    if (input.anthropicAPIKeyAuthScheme.value === 'authorization_bearer') {
      newExtra.anthropic_apikey_auth_scheme = 'authorization_bearer'
    } else {
      delete newExtra.anthropic_apikey_auth_scheme
    }
    if (input.webSearchEmulationMode.value === 'default') {
      delete newExtra.web_search_emulation
    } else {
      newExtra.web_search_emulation = input.webSearchEmulationMode.value
    }
    updatePayload.extra = newExtra
  }
  return true
}
