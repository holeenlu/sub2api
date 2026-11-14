import { projectBulkAnthropic } from './bulkAnthropicPatch'
import type { BulkAnthropicDraft, BulkAnthropicPatch } from './bulkAnthropicPatch'
import { projectBulkCredentials } from './bulkCredentialPatch'
import type { BulkCredentialDraft, BulkCredentialPatch } from './bulkCredentialPatch'
import { projectBulkOpenAI } from './bulkOpenAIPatch'
import type { BulkOpenAIDraft, BulkOpenAIExtraPatch, BulkOpenAICredentialPatch } from './bulkOpenAIPatch'
import { bulkModelRestrictionDisabled } from './bulkTargetEligibility'
import type { BulkEditPermissions, BulkTargetEligibility } from './bulkTargetEligibility'

export interface BulkFieldDraft {
  readonly proxyId?: number | null
  readonly concurrency?: number
  readonly loadFactor?: number | null
  readonly priority?: number
  readonly rateMultiplier?: number
  readonly status?: 'active' | 'inactive'
  readonly groupIds?: readonly number[]
  readonly upstreamBillingAutoProbe?: 'enabled' | 'disabled'
}

export interface BulkAccountDraft {
  readonly fields: BulkFieldDraft
  readonly credentials: BulkCredentialDraft
  readonly openai: BulkOpenAIDraft
  readonly anthropic: BulkAnthropicDraft
}

type BulkFieldPatch = {
  proxy_id?: number
  concurrency?: number
  load_factor?: number
  priority?: number
  rate_multiplier?: number
  status?: 'active' | 'inactive'
  group_ids?: number[]
  upstream_billing_probe_enabled?: boolean
}

export type BulkAccountPatch = BulkFieldPatch & {
  credentials?: BulkCredentialPatch & BulkOpenAICredentialPatch
  extra?: BulkOpenAIExtraPatch & BulkAnthropicPatch
}

function projectBulkFields(
  draft: BulkFieldDraft,
  permissions: BulkEditPermissions,
  target: BulkTargetEligibility
): BulkFieldPatch {
  const patch: BulkFieldPatch = {}
  if (permissions.allowTransportEdit && draft.proxyId !== undefined) patch.proxy_id = draft.proxyId ?? 0
  if (draft.concurrency !== undefined) patch.concurrency = draft.concurrency
  if (draft.loadFactor !== undefined) {
    const value = draft.loadFactor
    patch.load_factor = value != null && Number.isFinite(value) && value > 0 ? value : 0
  }
  if (draft.priority !== undefined) patch.priority = draft.priority
  if (permissions.allowRateEdit && draft.rateMultiplier !== undefined) patch.rate_multiplier = draft.rateMultiplier
  if (draft.status !== undefined) patch.status = draft.status
  if (draft.groupIds !== undefined) patch.group_ids = [...draft.groupIds]
  if (permissions.allowRateEdit && target.billingProbe && draft.upstreamBillingAutoProbe !== undefined) {
    patch.upstream_billing_probe_enabled = draft.upstreamBillingAutoProbe === 'enabled'
  }
  return patch
}

/** Builds one detached incremental patch. Undefined drafts mean "leave unchanged";
 * selected false/0/null/empty values must survive JSONB merge at the bulk endpoint.
 * No account baseline, scope, asynchronous validation or submission belongs here.
 */
export function buildBulkAccountPatch(
  draft: BulkAccountDraft,
  permissions: BulkEditPermissions,
  target: BulkTargetEligibility
): BulkAccountPatch | null {
  const updates: BulkAccountPatch = projectBulkFields(draft.fields, permissions, target)
  // Cross-field dependencies must see only edits this actor may send. Hidden
  // transport/capability drafts cannot suppress or clear other permitted edits.
  const openAIDraft: BulkOpenAIDraft = {
    ...draft.openai,
    passthrough: permissions.allowTransportEdit ? draft.openai.passthrough : undefined,
    endpointCapabilities: permissions.allowAuthorize ? draft.openai.endpointCapabilities : undefined
  }
  const openai = projectBulkOpenAI(openAIDraft, permissions, target)
  const credentials = {
    ...projectBulkCredentials(draft.credentials, permissions, target,
      bulkModelRestrictionDisabled(target, openAIDraft.passthrough)),
    ...openai.credentials
  }
  const extra = { ...openai.extra, ...projectBulkAnthropic(draft.anthropic, permissions, target) }
  if (Object.keys(credentials).length > 0) updates.credentials = credentials
  if (Object.keys(extra).length > 0) updates.extra = extra
  return Object.keys(updates).length > 0 ? updates : null
}
