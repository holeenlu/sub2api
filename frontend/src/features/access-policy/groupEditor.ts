/** Admission-only Group drafts and payloads; app owns the single HTTP command. */
export { createGroupPolicyDraft, groupPolicyDraftFromAPI, projectGroupPolicyDraft, buildGroupPolicyPayload, defaultAllowImageGenerationForPlatform, groupAllowsBatchImages, groupPolicyAfterPlatformChange } from './model/groupPolicyDraft'
export type { GroupPolicyDraft, GroupPolicyDetails } from './model/groupPolicyDraft'
