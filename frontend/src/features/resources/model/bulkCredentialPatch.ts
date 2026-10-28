import { applyAccountModelReferences } from './accountModelReferences'
import {
  buildHeaderOverridesObject,
  HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY,
  HEADER_OVERRIDES_CREDENTIAL_KEY
} from './credentialsBuilder'
import type { HeaderOverrideRow } from './credentialsBuilder'
import { buildModelMappingObject } from './modelRestrictions'
import type { ModelMappingEntry } from './modelRestrictions'
import type { BulkEditPermissions, BulkTargetEligibility } from './bulkTargetEligibility'

/** Undefined means the batch field was not selected; empty values are explicit edits. */
export interface BulkCredentialDraft {
  readonly baseUrl?: string
  readonly modelRestriction?: {
    readonly mode: 'whitelist' | 'mapping'
    readonly allowedModels: readonly string[]
    readonly mappings: readonly ModelMappingEntry[]
  }
  readonly customErrorCodes?: readonly number[]
  readonly interceptWarmup?: boolean
  readonly headerOverride?: { readonly enabled: boolean; readonly rows: readonly HeaderOverrideRow[] }
  readonly compactModelMappings?: readonly ModelMappingEntry[]
}

export type BulkCredentialPatch = {
  base_url?: string
  model_allowlist?: string[] | null
  model_mapping?: Record<string, string>
  custom_error_codes_enabled?: boolean
  custom_error_codes?: number[]
  intercept_warmup_requests?: boolean
  header_override_enabled?: boolean
  header_overrides?: Record<string, string>
  compact_model_mapping?: Record<string, string>
}

/** Produces JSONB merge keys, never an Edit/Create credential replacement. */
export function projectBulkCredentials(
  draft: BulkCredentialDraft,
  permissions: BulkEditPermissions,
  target: BulkTargetEligibility,
  modelRestrictionDisabled: boolean
): BulkCredentialPatch {
  const patch: BulkCredentialPatch = {}
  if (permissions.allowTransportEdit && draft.baseUrl !== undefined) {
    const baseUrl = draft.baseUrl.trim()
    if (baseUrl) patch.base_url = baseUrl
  }
  if (!permissions.allowAuthorize) return patch

  if (draft.modelRestriction && !modelRestrictionDisabled) {
    const { mode, allowedModels, mappings } = draft.modelRestriction
    if (target.mixedModelReferences) throw new Error('Select managed and legacy platform accounts separately to edit model restrictions')
    if (target.managedModelReferences) {
      if (mode === 'whitelist') patch.model_allowlist = allowedModels.length ? [...allowedModels] : null
      else {
        const models: Record<string, unknown> = {}
        applyAccountModelReferences(models, 'openai', 'mapping', [], mappings)
        patch.model_mapping = models.model_mapping as Record<string, string> | undefined ?? {}
      }
    } else if (mode === 'whitelist') {
      // Preserve the bulk selector's exact names. The shared mapping builder trims and
      // filters whitelist entries, which is not this existing batch wire contract.
      const mapping: Record<string, string> = {}
      for (const model of allowedModels) mapping[model] = model
      patch.model_mapping = mapping
    } else {
      patch.model_mapping = buildModelMappingObject('mapping', [], [...mappings]) ?? {}
    }
  }
  if (draft.customErrorCodes !== undefined) {
    patch.custom_error_codes_enabled = true
    patch.custom_error_codes = [...draft.customErrorCodes]
  }
  if (draft.interceptWarmup !== undefined) patch.intercept_warmup_requests = draft.interceptWarmup
  if (target.headerOverride && draft.headerOverride) {
    patch[HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY] = draft.headerOverride.enabled
    patch[HEADER_OVERRIDES_CREDENTIAL_KEY] = draft.headerOverride.enabled
      ? buildHeaderOverridesObject([...draft.headerOverride.rows]) : {}
  }
  if (target.openAIPassthrough && draft.compactModelMappings !== undefined) {
    patch.compact_model_mapping = buildModelMappingObject('mapping', [], [...draft.compactModelMappings]) ?? {}
  }
  return patch
}
