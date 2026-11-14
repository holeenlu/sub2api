import { readExcelBPSModels } from './excelBPSModels'
import { bpsRecoveryIntervalOrDefault } from '@/features/resources/model/excelBPSRecovery'
import type { Account } from '@/features/resources/api/accountTypes';
import { isOpenAIWSModeEnabled } from '@/features/resources/model/openaiWsMode'
import type { EditSubmissionInput } from './editSubmission'



/** Applies only openaitransport fields to the local outgoing payload. */
export function applyOpenAITransportUpdate(input: Pick<EditSubmissionInput, "cloudEligible" | "cloudEnabled" | "isSparkShadow" | "excelBPSEnabled" | "excelBPSModels" | "excelBPSStopRequested" | "excelBPSSwitchTouched" | "excelBPSExpectedDisabledAt" | "excelBPSCacheCreationAsInput" | "excelBPSOmitUnsupportedTools" | "excelBPSIgnoreEncryptedContent" | "excelBPSAutoDisableOn403" | "excelBPSAutoRecoverOn403" | "excelBPSRecoveryIntervalMinutes" | "excelBPSAutoMoveOn403" | "excelBPS403TargetGroupID" | "openaiOAuthResponsesWebSocketV2Mode" | "openaiAPIKeyResponsesWebSocketV2Mode" | "openaiPassthroughEnabled" | "openaiFlattenNamespacesEnabled" | "openAILongContextBillingEnabled" | "openAICompactMode" | "openAITextGenerationCapabilityEnabled" | "openAIResponsesMode" | "openAIImagesUrlToB64JsonEnabled" | "webSearchEmulationMode" | "autoPause5hThreshold" | "autoPause7dThreshold" | "autoPause5hDisabled" | "autoPause7dDisabled" | "autoResetCreditEnabled" | "autoResetCredit5hThreshold" | "autoResetCredit7dThreshold" | "codexImageToolMode" | "codexCLIOnlyEnabled" | "codexCLIOnlyAppServerEnabled" | "codexFingerprintMode">, account: Account, updatePayload: Record<string, unknown>): boolean {
  if (account.platform === 'openai' && (account.type === 'oauth' || account.type === 'setup-token' || account.type === 'apikey')) {
    const currentExtra = (account.extra as Record<string, unknown>) || {}
    const newExtra: Record<string, unknown> = { ...currentExtra }
    delete newExtra.openai_excel_bps_config_mode
    delete newExtra.openai_excel_bps_expected_disabled_at
    // The server preserves these persisted runtime records across edits. Do not
    // submit an editor snapshot as writable configuration, including opaque failure data.
    for (const key of [
      'openai_excel_bps_failure',
      'openai_excel_bps_403_disabled_at',
      'openai_excel_bps_403_last_probe_at',
      'openai_excel_bps_403_last_probe_result',
      'openai_excel_bps_403_moved_at',
      'openai_excel_bps_403_moved_group_id'
    ]) delete newExtra[key]
    const hadCodexCLIOnlyEnabled = currentExtra.codex_cli_only === true
    if (input.cloudEligible.value) newExtra.openai_cloud = input.cloudEnabled.value

    if (account.type === 'oauth' && !input.isSparkShadow.value && input.excelBPSEnabled.value) {
      newExtra.openai_excel_bps = true
      newExtra.openai_excel_bps_models = readExcelBPSModels(input.excelBPSModels.value)
    } else {
      delete newExtra.openai_excel_bps
      delete newExtra.openai_excel_bps_models
    }
    // Hidden fields on an already-off account are not edits. Preserve their
    // stored values even when status is unavailable; no recovery policy is inferred.
    const preserveDisabledBPS = account.type === 'oauth' && !input.isSparkShadow.value &&
      !input.excelBPSEnabled.value && currentExtra.openai_excel_bps !== true
    if ((newExtra.openai_excel_bps === true || preserveDisabledBPS) && input.excelBPSCacheCreationAsInput.value) {
      newExtra.openai_excel_bps_cache_creation_as_input = true
    } else {
      delete newExtra.openai_excel_bps_cache_creation_as_input
    }
    if ((newExtra.openai_excel_bps === true || preserveDisabledBPS) && input.excelBPSOmitUnsupportedTools.value) {
      newExtra.openai_excel_bps_omit_unsupported_tools = true
    } else {
      delete newExtra.openai_excel_bps_omit_unsupported_tools
    }
    if ((newExtra.openai_excel_bps === true || preserveDisabledBPS) && input.excelBPSIgnoreEncryptedContent.value) {
      newExtra.openai_excel_bps_ignore_encrypted_content = true
    } else {
      delete newExtra.openai_excel_bps_ignore_encrypted_content
    }
    // Preserve hidden saved values after
    // projecting the draft so an unrelated edit cannot change recovery settings.
    if (preserveDisabledBPS) {
      for (const key of ['openai_excel_bps_models', 'openai_excel_bps_cache_creation_as_input',
        'openai_excel_bps_omit_unsupported_tools',
        'openai_excel_bps_ignore_encrypted_content']) {
        if (Object.prototype.hasOwnProperty.call(currentExtra, key)) newExtra[key] = currentExtra[key]
        else delete newExtra[key]
      }
    }
    if ((newExtra.openai_excel_bps === true || preserveDisabledBPS) && input.excelBPSAutoDisableOn403.value) {
      newExtra.openai_excel_bps_auto_disable_on_403 = true
    } else {
      delete newExtra.openai_excel_bps_auto_disable_on_403
    }
    if ((newExtra.openai_excel_bps === true || preserveDisabledBPS) && input.excelBPSAutoDisableOn403.value && input.excelBPSAutoRecoverOn403.value) {
      newExtra.openai_excel_bps_auto_recover_on_403 = true
    } else {
      delete newExtra.openai_excel_bps_auto_recover_on_403
    }
    if (newExtra.openai_excel_bps === true || preserveDisabledBPS) {
      newExtra.openai_excel_bps_403_recovery_interval_minutes = bpsRecoveryIntervalOrDefault(input.excelBPSRecoveryIntervalMinutes.value)
    }
    if ((newExtra.openai_excel_bps === true || preserveDisabledBPS) && input.excelBPSAutoMoveOn403.value) {
      newExtra.openai_excel_bps_auto_move_on_403 = true
      newExtra.openai_excel_bps_403_target_group_id = Number(input.excelBPS403TargetGroupID.value)
    } else {
      delete newExtra.openai_excel_bps_auto_move_on_403
      delete newExtra.openai_excel_bps_403_target_group_id
    }
    if (account.type === 'oauth' || account.type === 'setup-token') {
      newExtra.openai_oauth_responses_websockets_v2_mode = input.openaiOAuthResponsesWebSocketV2Mode.value
      newExtra.openai_oauth_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(input.openaiOAuthResponsesWebSocketV2Mode.value)
    } else if (account.type === 'apikey') {
      newExtra.openai_apikey_responses_websockets_v2_mode = input.openaiAPIKeyResponsesWebSocketV2Mode.value
      newExtra.openai_apikey_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(input.openaiAPIKeyResponsesWebSocketV2Mode.value)
    }
    delete newExtra.responses_websockets_v2_enabled
    delete newExtra.openai_ws_enabled
    if (input.openaiPassthroughEnabled.value) {
      newExtra.openai_passthrough = true
    } else {
      delete newExtra.openai_passthrough
      delete newExtra.openai_oauth_passthrough
    }
    // 缺省即保留 namespace，不写空值，避免 extra 里堆积默认项
    if (account.type === 'oauth' && input.openaiFlattenNamespacesEnabled.value) {
      newExtra.openai_responses_flatten_namespaces = true
    } else {
      delete newExtra.openai_responses_flatten_namespaces
    }
    if (input.isSparkShadow.value) {
      delete newExtra.openai_long_context_billing_enabled
    } else {
      newExtra.openai_long_context_billing_enabled = input.openAILongContextBillingEnabled.value
    }
    if (input.openAICompactMode.value === 'auto') {
      delete newExtra.openai_compact_mode
    } else {
      newExtra.openai_compact_mode = input.openAICompactMode.value
    }
    if (account.type === 'apikey') {
      if (!input.openAITextGenerationCapabilityEnabled.value || input.openAIResponsesMode.value === 'auto') {
        delete newExtra.openai_responses_mode
      } else {
        newExtra.openai_responses_mode = input.openAIResponsesMode.value
      }
      if (input.openAIImagesUrlToB64JsonEnabled.value) {
        newExtra.images_url_to_b64_json = true
      } else {
        delete newExtra.images_url_to_b64_json
      }
      if (input.webSearchEmulationMode.value === 'default') {
        delete newExtra.web_search_emulation
      } else {
        newExtra.web_search_emulation = input.webSearchEmulationMode.value
      }
    }
    if (input.autoPause5hThreshold.value != null && input.autoPause5hThreshold.value > 0) {
      newExtra.auto_pause_5h_threshold = input.autoPause5hThreshold.value / 100
    } else {
      delete newExtra.auto_pause_5h_threshold
    }
    if (input.autoPause7dThreshold.value != null && input.autoPause7dThreshold.value > 0) {
      newExtra.auto_pause_7d_threshold = input.autoPause7dThreshold.value / 100
    } else {
      delete newExtra.auto_pause_7d_threshold
    }
    if (input.autoPause5hDisabled.value) {
      newExtra.auto_pause_5h_disabled = true
    } else {
      delete newExtra.auto_pause_5h_disabled
    }
    if (input.autoPause7dDisabled.value) {
      newExtra.auto_pause_7d_disabled = true
    } else {
      delete newExtra.auto_pause_7d_disabled
    }
    if (account.type === 'oauth' && !input.isSparkShadow.value) {
      newExtra.auto_reset_credit_enabled = input.autoResetCreditEnabled.value
      newExtra.auto_reset_credit_5h_threshold = input.autoResetCredit5hThreshold.value / 100
      newExtra.auto_reset_credit_7d_threshold = input.autoResetCredit7dThreshold.value / 100
    }
    // 运行态只允许后端服务更新，账号编辑不得回写旧状态。
    delete newExtra.codex_auto_reset_credit_state

    delete newExtra.codex_image_generation_bridge_enabled
    switch (input.codexImageToolMode.value) {
      case 'enabled':
      case 'disabled':
        newExtra.codex_image_generation_bridge = input.codexImageToolMode.value === 'enabled'
        delete newExtra.codex_image_generation_explicit_tool_policy
        break
      case 'block':
        newExtra.codex_image_generation_explicit_tool_policy = 'strip'
        delete newExtra.codex_image_generation_bridge
        break
      default:
        delete newExtra.codex_image_generation_bridge
        delete newExtra.codex_image_generation_explicit_tool_policy
    }

    if (account.type === 'oauth' || account.type === 'setup-token') {
      if (input.codexCLIOnlyEnabled.value) {
        newExtra.codex_cli_only = true
      } else if (hadCodexCLIOnlyEnabled) {
        // 关闭时显式写 false，避免 extra 为空被后端忽略导致旧值无法清除
        newExtra.codex_cli_only = false
      } else {
        delete newExtra.codex_cli_only
      }
      // Claude Code 插件放行已迁移到全局 codex_cli_only_whitelist，编辑时清理废弃账号级快捷字段。
      delete newExtra.codex_cli_only_allowed_clients
      if (input.codexCLIOnlyEnabled.value && input.codexCLIOnlyAppServerEnabled.value) {
        newExtra.codex_cli_only_allow_app_server = true
      } else {
        delete newExtra.codex_cli_only_allow_app_server
      }
    }

    // 指纹收敛模式：默认 off（不写入）；device/session/full 是显式 opt-in，
    // 必须落键，否则管理员的选择会被后端当作"未设置"而回落到 off（#5610）。
    if (account.type === 'oauth') {
      if (input.codexFingerprintMode.value !== 'off') {
        newExtra.codex_fingerprint_mode = input.codexFingerprintMode.value
      } else {
        delete newExtra.codex_fingerprint_mode
      }
    }

    if (account.type === 'oauth' && !input.isSparkShadow.value &&
      input.excelBPSStopRequested.value && !input.excelBPSEnabled.value) {
      // The existing PUT distinguishes an explicit false from omission/null.
      // Only an intentional stop withdraws the prior server recovery claim.
      newExtra.openai_excel_bps = false
    }
    // Omission preserves the server's current switch under its row lock. Only
    // an explicit user switch command may enable or withdraw recovery authority.
    if (!input.excelBPSSwitchTouched.value) delete newExtra.openai_excel_bps
    else if (input.excelBPSEnabled.value && input.excelBPSExpectedDisabledAt.value !== undefined) {
      newExtra.openai_excel_bps_expected_disabled_at = input.excelBPSExpectedDisabledAt.value
    }
    updatePayload.extra = newExtra
  }
  return true
}
