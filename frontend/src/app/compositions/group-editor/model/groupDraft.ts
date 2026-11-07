import { cloneDraftValue } from '@/shared/lib/useDraftSection'
import type { AdminGroup, CreateGroupRequest, UpdateGroupRequest } from '@/features/access-policy/api/groupTypes'
import { supportsGroupOpenAIFast } from '@/features/access-policy'
import { createGroupPolicyDraft, groupPolicyDraftFromAPI, buildGroupPolicyPayload, type GroupPolicyDraft, type GroupPolicyDetails } from '@/features/access-policy/groupEditor'
import { createGroupPricingDraft, groupPricingDraftFromAPI, buildGroupPricingPayload, type GroupPricingDraft } from '@/features/pricing/groupEditor'
import { createGroupSubscriptionDraft, groupSubscriptionDraftFromAPI, buildGroupSubscriptionPayload, type GroupSubscriptionDraft } from '@/features/subscriptions/groupEditor'

export type GroupEditorDraft = GroupPolicyDraft & GroupPricingDraft & GroupSubscriptionDraft
export type { GroupPolicyDetails } from '@/features/access-policy/groupEditor'
export function createGroupEditorDraft(): GroupEditorDraft {
 return { ...createGroupPolicyDraft(), ...createGroupPricingDraft(), ...createGroupSubscriptionDraft() }
}
export function groupEditorDraftFromAPI(group: Readonly<AdminGroup>): GroupEditorDraft {
 return { ...groupPolicyDraftFromAPI(group), ...groupPricingDraftFromAPI(group), ...groupSubscriptionDraftFromAPI(group) }
}
function composePayload(draft: GroupEditorDraft, details: GroupPolicyDetails, updating: boolean) {
 const policy = buildGroupPolicyPayload(draft, details, updating)
 return cloneDraftValue({
  ...policy,
  ...buildGroupPricingPayload(draft, {
   platform: draft.platform,
   imageGenerationAllowed: policy.allow_image_generation,
   openAIFastSupported: supportsGroupOpenAIFast(draft.platform),
   batchImageAllowed: policy.allow_batch_image_generation,
  }, updating),
  ...buildGroupSubscriptionPayload(draft),
 })
}
/** The API command and server field whitelist stay in the app composition. */
export function buildCreateGroupRequest(draft: GroupEditorDraft, details: GroupPolicyDetails, simpleMode: boolean): CreateGroupRequest {
 return simpleMode ? cloneDraftValue({ name: draft.name, description: draft.description, i18n: draft.i18n, platform: draft.platform }) : composePayload(draft, details, false)
}
export function buildUpdateGroupRequest(draft: GroupEditorDraft, details: GroupPolicyDetails, simpleMode: boolean): UpdateGroupRequest {
 return simpleMode ? cloneDraftValue({ name: draft.name, description: draft.description, i18n: draft.i18n }) : composePayload(draft, details, true)
}
