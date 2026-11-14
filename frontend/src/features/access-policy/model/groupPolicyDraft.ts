import { cloneDraftValue } from '@/shared/lib/useDraftSection'
import type { AdminGroup, CodexModelsManifestConfig, ModelAllowlist } from '../api/groupTypes'
import { createGroupIdentityDraft } from './groupIdentityDraft'
import { createDefaultMessagesDispatchFormState, messagesDispatchConfigToFormState, messagesDispatchFormStateToConfig, type MessagesDispatchMappingRow, resetMessagesDispatchFormState, supportsMessagesDispatchPlatform } from './groupsMessagesDispatch'
import { normalizeGroupOpenAIFast } from './groupsOpenAIFast'
import { normalizeSupportedModelScopesForPlatform } from './groupsSupportedModelScopes'
import { normalizeReasoningEffortForPlatform, normalizeReasoningEffortOverLimit, reasoningEffortMappingsToAPI, reasoningEffortMappingsToRows, reasoningEffortOverLimitDowngrade, type ReasoningEffortMappingRow, supportsReasoningEffortPolicyPlatform } from './groupsReasoningEffort'
const messagesDefaults = createDefaultMessagesDispatchFormState()
export function createGroupPolicyDraft() {
 return {
  ...createGroupIdentityDraft(),
  is_exclusive: false,
  status: "active" as "active" | "inactive",
  force_openai_fast: false,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  claude_code_only: false,
  fallback_group_id: null as number | null,
  fallback_group_id_on_invalid_request: null as number | null,
  allow_messages_dispatch: false,
  allow_live: false,
  default_mapped_model: '',
  opus_mapped_model: messagesDefaults.opus_mapped_model,
  sonnet_mapped_model: messagesDefaults.sonnet_mapped_model,
  haiku_mapped_model: messagesDefaults.haiku_mapped_model,
  exact_model_mappings: [] as MessagesDispatchMappingRow[],
  require_oauth_only: false,
  require_privacy_set: false,
  model_routing_enabled: false,
  supported_model_scopes: ["claude", "gemini_text", "gemini_image"] as string[],
  mcp_xml_inject: true,
  copy_accounts_from_group_ids: [] as number[],
  rpm_limit: 0 as number,
  max_reasoning_effort: "",
  max_reasoning_effort_over_limit: reasoningEffortOverLimitDowngrade,
  reasoning_effort_mappings: [] as ReasoningEffortMappingRow[]
 }
}
export type GroupPolicyDraft = ReturnType<typeof createGroupPolicyDraft>
export function groupPolicyDraftFromAPI(group: Readonly<AdminGroup>): GroupPolicyDraft {
 const draft = createGroupPolicyDraft()
Object.assign(draft, createGroupIdentityDraft(group));
draft.is_exclusive = group.is_exclusive;
draft.status = group.status;
draft.force_openai_fast = group.force_openai_fast ?? false;
draft.allow_image_generation = group.allow_image_generation ?? false;
draft.allow_batch_image_generation =
    group.allow_batch_image_generation ?? false;
draft.claude_code_only = group.claude_code_only || false;
draft.fallback_group_id = group.fallback_group_id;
draft.fallback_group_id_on_invalid_request =
    group.fallback_group_id_on_invalid_request;
const messagesDispatchFormState = messagesDispatchConfigToFormState(
    group.messages_dispatch_model_config,
  );
draft.allow_messages_dispatch =
    group.allow_messages_dispatch ||
    messagesDispatchFormState.allow_messages_dispatch;
draft.allow_live = group.allow_live ?? false;
draft.opus_mapped_model = messagesDispatchFormState.opus_mapped_model;
draft.sonnet_mapped_model = messagesDispatchFormState.sonnet_mapped_model;
draft.haiku_mapped_model = messagesDispatchFormState.haiku_mapped_model;
draft.exact_model_mappings =
    messagesDispatchFormState.exact_model_mappings;
draft.require_oauth_only = group.require_oauth_only ?? false;
draft.require_privacy_set = group.require_privacy_set ?? false;
draft.model_routing_enabled = group.model_routing_enabled || false;
draft.supported_model_scopes = [...(group.supported_model_scopes || [
    "claude",
    "gemini_text",
    "gemini_image",
  ])];
draft.mcp_xml_inject = group.mcp_xml_inject ?? true;
draft.copy_accounts_from_group_ids = [];
draft.rpm_limit = group.rpm_limit ?? 0;
draft.max_reasoning_effort = normalizeReasoningEffortForPlatform(
    group.platform,
    group.max_reasoning_effort,
  );
draft.max_reasoning_effort_over_limit = normalizeReasoningEffortOverLimit(
    group.max_reasoning_effort_over_limit,
  );
draft.reasoning_effort_mappings = reasoningEffortMappingsToRows(
    group.reasoning_effort_mappings,
    group.platform,
  );
 return draft
}
export function projectGroupPolicyDraft(value: Readonly<GroupPolicyDraft>): GroupPolicyDraft {
 const keys = Object.keys(createGroupPolicyDraft()) as Array<keyof GroupPolicyDraft>
 return Object.fromEntries(keys.map(key => [key, cloneDraftValue(value[key])])) as GroupPolicyDraft
}

export interface GroupPolicyDetails {
 routing: Record<string, number[]> | null
 allowlist: ModelAllowlist
 manifest: CodexModelsManifestConfig
}
/** Existing backend default; no inference from unknown catalog capabilities. */
export const defaultAllowImageGenerationForPlatform = (platform: string): boolean => ['antigravity', 'gemini', 'grok'].includes(platform)
export const groupAllowsBatchImages = (value: Pick<GroupPolicyDraft, 'platform' | 'allow_image_generation' | 'allow_batch_image_generation'>): boolean =>
 value.platform === 'gemini' && value.allow_image_generation && value.allow_batch_image_generation
export function buildGroupPolicyPayload(value: Readonly<GroupPolicyDraft>, details: Readonly<GroupPolicyDetails>, updating: boolean) {
 const draft = projectGroupPolicyDraft(value)
 const { status, default_mapped_model, opus_mapped_model, sonnet_mapped_model, haiku_mapped_model, exact_model_mappings, ...fields } = draft
 return cloneDraftValue({
  ...fields,
  ...(updating ? { status, default_mapped_model } : {}),
  force_openai_fast: normalizeGroupOpenAIFast(draft.platform, draft.force_openai_fast),
  allow_batch_image_generation: groupAllowsBatchImages(draft),
  fallback_group_id: updating && draft.fallback_group_id === null ? 0 : draft.fallback_group_id,
  fallback_group_id_on_invalid_request: updating && draft.fallback_group_id_on_invalid_request === null ? 0 : draft.fallback_group_id_on_invalid_request,
  model_routing: details.routing,
  model_allowlist: details.allowlist,
  codex_models_manifest_config: updating && draft.platform === 'openai'
   ? details.manifest : { enabled: false, account_ids: [], fallback_to_scheduler: false },
  supported_model_scopes: normalizeSupportedModelScopesForPlatform(draft.platform, draft.supported_model_scopes),
  messages_dispatch_model_config: draft.platform === 'openai' ? messagesDispatchFormStateToConfig({
   allow_messages_dispatch: draft.allow_messages_dispatch, opus_mapped_model, sonnet_mapped_model, haiku_mapped_model, exact_model_mappings,
  }) : undefined,
  reasoning_effort_mappings: reasoningEffortMappingsToAPI(draft.reasoning_effort_mappings),
 })
}

/** Admission-only reset when the editor changes its platform; pricing is composed separately. */
export function groupPolicyAfterPlatformChange(value: Readonly<GroupPolicyDraft>): GroupPolicyDraft {
 const draft = projectGroupPolicyDraft(value)
 const platform = draft.platform
 if (!['anthropic', 'antigravity'].includes(platform)) draft.fallback_group_id_on_invalid_request = null
 if (!supportsMessagesDispatchPlatform(platform)) resetMessagesDispatchFormState(draft)
 if (platform !== 'openai' && platform !== 'composite') draft.allow_live = false
 draft.max_reasoning_effort = normalizeReasoningEffortForPlatform(platform, draft.max_reasoning_effort)
 draft.max_reasoning_effort_over_limit = supportsReasoningEffortPolicyPlatform(platform)
  ? normalizeReasoningEffortOverLimit(draft.max_reasoning_effort_over_limit) : reasoningEffortOverLimitDowngrade
 draft.reasoning_effort_mappings = reasoningEffortMappingsToRows(reasoningEffortMappingsToAPI(draft.reasoning_effort_mappings), platform)
 if (!['openai', 'antigravity', 'anthropic', 'gemini'].includes(platform)) {
  draft.require_oauth_only = false
  draft.require_privacy_set = false
 }
 draft.allow_image_generation = defaultAllowImageGenerationForPlatform(platform)
 return draft
}
