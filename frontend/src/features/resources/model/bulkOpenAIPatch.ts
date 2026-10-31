import type { OpenAICompactMode, OpenAIEndpointCapability, OpenAIResponsesMode } from '../api/accountCredentialTypes'
import { readExcelBPSModels } from './excelBPSModels'
import { bpsRecoveryIntervalOrDefault } from './excelBPSRecovery'
import { isOpenAIWSModeEnabled } from './openaiWsMode'
import type { OpenAIWSMode } from './openaiWsMode'
import type { BulkEditPermissions, BulkTargetEligibility } from './bulkTargetEligibility'

export interface BulkBPSDraft {
  readonly enabled: boolean
  readonly models: readonly string[]
  readonly cacheCreationAsInput: boolean
  readonly autoDisableOn403: boolean
  readonly autoRecoverOn403: boolean
  readonly recoveryIntervalMinutes: number | string
  readonly omitUnsupportedTools: boolean
  readonly ignoreEncryptedContent: boolean
  readonly autoMoveOn403: boolean
  readonly targetGroupID: number | string
}

export interface BulkOpenAIDraft {
  readonly cloud?: boolean
  readonly bps?: BulkBPSDraft
  readonly passthrough?: boolean
  readonly flattenNamespaces?: boolean
  readonly longContextBilling?: boolean
  readonly endpointCapabilities?: readonly OpenAIEndpointCapability[]
  readonly responsesMode?: OpenAIResponsesMode
  readonly oauthWSMode?: OpenAIWSMode
  readonly apiKeyWSMode?: OpenAIWSMode
  readonly codexCLIOnly?: boolean
  readonly codexCLIOnlyAppServer?: boolean
  readonly fingerprintMode?: 'off' | 'device' | 'session' | 'full'
  readonly compactMode?: OpenAICompactMode
}

type BulkBPSPatch = {
  openai_excel_bps: boolean
  openai_excel_bps_models: string[] | null
  openai_excel_bps_cache_creation_as_input: boolean
  openai_excel_bps_auto_disable_on_403: boolean
  openai_excel_bps_auto_recover_on_403: boolean
  openai_excel_bps_403_recovery_interval_minutes: number
  openai_excel_bps_omit_unsupported_tools: boolean
  openai_excel_bps_ignore_encrypted_content: boolean
  openai_excel_bps_auto_move_on_403: boolean
  openai_excel_bps_403_target_group_id: number | null
}

export type BulkOpenAIExtraPatch = Partial<BulkBPSPatch> & {
  openai_cloud?: boolean
  openai_passthrough?: boolean
  openai_oauth_passthrough?: boolean
  openai_responses_flatten_namespaces?: boolean
  openai_long_context_billing_enabled?: boolean
  openai_responses_mode?: OpenAIResponsesMode | null
  openai_oauth_responses_websockets_v2_mode?: OpenAIWSMode
  openai_oauth_responses_websockets_v2_enabled?: boolean
  openai_apikey_responses_websockets_v2_mode?: OpenAIWSMode
  openai_apikey_responses_websockets_v2_enabled?: boolean
  codex_cli_only?: boolean
  codex_cli_only_allow_app_server?: boolean
  codex_fingerprint_mode?: BulkOpenAIDraft['fingerprintMode']
  openai_compact_mode?: OpenAICompactMode
}

export type BulkOpenAICredentialPatch = { openai_capabilities?: OpenAIEndpointCapability[] | null }

/** Unselected capabilities do not restrict an independently selected Responses mode. */
export function bulkResponsesModeApplicable(capabilities: readonly OpenAIEndpointCapability[] | undefined): boolean {
  return capabilities === undefined || capabilities.includes('chat_completions')
}

function projectBPS(draft: BulkBPSDraft): BulkBPSPatch {
  return {
    openai_excel_bps: draft.enabled,
    // null clears the stored setting when disabling; [] selects no BPS models.
    openai_excel_bps_models: draft.enabled ? readExcelBPSModels(draft.models) : null,
    openai_excel_bps_cache_creation_as_input: draft.enabled && draft.cacheCreationAsInput,
    openai_excel_bps_auto_disable_on_403: draft.enabled && draft.autoDisableOn403,
    openai_excel_bps_auto_recover_on_403: draft.enabled && draft.autoDisableOn403 && draft.autoRecoverOn403,
    openai_excel_bps_403_recovery_interval_minutes: bpsRecoveryIntervalOrDefault(draft.recoveryIntervalMinutes),
    openai_excel_bps_omit_unsupported_tools: draft.enabled && draft.omitUnsupportedTools,
    openai_excel_bps_ignore_encrypted_content: draft.enabled && draft.ignoreEncryptedContent,
    openai_excel_bps_auto_move_on_403: draft.enabled && draft.autoMoveOn403,
    openai_excel_bps_403_target_group_id: draft.enabled && draft.autoMoveOn403 ? Number(draft.targetGroupID) : null
  }
}

/** Batch-only JSONB projection. Validation and live Cloud eligibility remain with the editor. */
export function projectBulkOpenAI(
  draft: BulkOpenAIDraft,
  permissions: BulkEditPermissions,
  target: BulkTargetEligibility
): { extra: BulkOpenAIExtraPatch; credentials: BulkOpenAICredentialPatch } {
  const extra: BulkOpenAIExtraPatch = {}
  const credentials: BulkOpenAICredentialPatch = {}
  if (permissions.allowTransportEdit) {
    if (target.openAIOAuthOnly) {
      if (!permissions.supplierScope && draft.cloud !== undefined) extra.openai_cloud = draft.cloud
      if (draft.bps) Object.assign(extra, projectBPS(draft.bps))
      if (draft.flattenNamespaces !== undefined) extra.openai_responses_flatten_namespaces = draft.flattenNamespaces
    }
    if (target.openAIPassthrough) {
      if (draft.passthrough !== undefined) {
        extra.openai_passthrough = draft.passthrough
        if (!draft.passthrough) extra.openai_oauth_passthrough = false
      }
      if (draft.longContextBilling !== undefined) extra.openai_long_context_billing_enabled = draft.longContextBilling
      if (draft.compactMode !== undefined) extra.openai_compact_mode = draft.compactMode
    }
    if (target.openAIOAuth) {
      if (draft.oauthWSMode !== undefined) {
        extra.openai_oauth_responses_websockets_v2_mode = draft.oauthWSMode
        extra.openai_oauth_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(draft.oauthWSMode)
      }
      if (draft.codexCLIOnly !== undefined) extra.codex_cli_only = draft.codexCLIOnly
      // A batch child edit is valid only when this same patch explicitly enables its parent.
      if (draft.codexCLIOnly === true && draft.codexCLIOnlyAppServer !== undefined) {
        extra.codex_cli_only_allow_app_server = draft.codexCLIOnlyAppServer
      }
      // Unlike full-object Edit/Create replacement, omission cannot clear an existing mode.
      // Explicit off also prevents the empty-extra/no-updates failure (#6327).
      if (draft.fingerprintMode !== undefined) extra.codex_fingerprint_mode = draft.fingerprintMode
    }
    if (target.openAIAPIKey && draft.apiKeyWSMode !== undefined) {
      extra.openai_apikey_responses_websockets_v2_mode = draft.apiKeyWSMode
      extra.openai_apikey_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(draft.apiKeyWSMode)
    }
  }

  if (target.openAIAPIKey) {
    const capabilities = draft.endpointCapabilities
    const applyCapabilities = permissions.allowAuthorize && capabilities !== undefined
    const responsesApplicable = bulkResponsesModeApplicable(capabilities)
    if (applyCapabilities) {
      credentials.openai_capabilities = capabilities.length === 2 && !capabilities.includes('seedance')
        ? null : [...capabilities]
    }
    // Removing text capability must clear the old routing override even without a
    // separate transport edit. This is the existing authorize-field dependency.
    if ((permissions.allowTransportEdit && draft.responsesMode !== undefined) ||
      (applyCapabilities && !responsesApplicable)) {
      extra.openai_responses_mode = !responsesApplicable || draft.responsesMode === 'auto'
        ? null : draft.responsesMode
    }
  }
  return { extra, credentials }
}
