<template>
 <BaseDialog :show="true" :title="t(creating ? 'admin.groups.createGroup' : 'admin.groups.editGroup')" width="wide" :show-close-button="!submitting" :close-on-escape="!submitting" @close="close">
  <form :id="creating ? 'create-group-form' : 'edit-group-form'" class="space-y-5" :inert="submitting || invalidated" @submit.prevent="submit">
   <GroupIdentityPanel :model-value="form" :editable="editable" @update:model-value="Object.assign(form, $event)" />
   <div><label class="input-label">{{ t('admin.groups.form.platform') }}</label><Select v-model="form.platform" :options="platformOptions" data-tour="group-form-platform" :disabled="!creating || !editable('platform')" @change="form.copy_accounts_from_group_ids = []" /></div>
   <template v-if="!permissions.isSimpleMode">
    <GroupAdmissionFields :model-value="project(['name', 'description', 'i18n', 'platform', 'is_exclusive', 'status', 'rpm_limit', 'claude_code_only', 'fallback_group_id', 'fallback_group_id_on_invalid_request', 'require_oauth_only', 'require_privacy_set', 'supported_model_scopes', 'mcp_xml_inject', 'subscription_type', 'model_routing_enabled', 'allow_live', 'allow_messages_dispatch', 'copy_accounts_from_group_ids'])" :groups="groups" :group-id="group?.id ?? 0" :editable="editable" @patch="Object.assign(form, $event)" />
    <ReasoningEffortPolicyFields v-if="supportsReasoningEffortPolicyPlatform(form.platform)" ref="reasoning" :id-prefix="creating ? 'create-group-reasoning' : 'edit-group-reasoning'" :platform="form.platform" v-model:max-effort="form.max_reasoning_effort" v-model:over-limit="form.max_reasoning_effort_over_limit" v-model:mappings="form.reasoning_effort_mappings" />
    <div class="space-y-5">
     <GroupRateField :model-value="project(['rate_multiplier'])" :editable="editable" @patch="Object.assign(form, $event)" />
     <GroupExclusivityField :model-value="project(['is_exclusive'])" :subscription-type="form.subscription_type" :editable="editable" @patch="Object.assign(form, $event)" />
     <GroupSubscriptionPolicyPanel :model-value="project(['subscription_type', 'daily_limit_usd', 'weekly_limit_usd', 'monthly_limit_usd'])" :editable="editable" @patch="Object.assign(form, $event)" />
     <GroupPeakPricingPanel :model-value="project(['peak_rate_enabled', 'peak_start', 'peak_end', 'peak_rate_multiplier'])" :subscription-type="form.subscription_type" :editable="editable" @patch="Object.assign(form, $event)" />
    </div>
    <GroupModelAllowlistPanel v-model="allowlist" :group-id="group?.id ?? 0" :platform="form.platform" />
    <GroupMediaPricingPanel :model-value="project(['rate_multiplier', 'image_count_billing', 'batch_image_discount_multiplier', 'batch_image_hold_multiplier', 'image_price_1k', 'image_price_2k', 'image_price_4k', 'image_rate_independent', 'image_rate_multiplier', 'video_model_prices', 'video_price_1080p', 'video_price_480p', 'video_price_720p', 'video_rate_independent', 'video_rate_multiplier'])" :editable="editable" :platform="form.platform" :image-allowed="form.allow_image_generation" :batch-images-allowed="form.allow_batch_image_generation" @patch="Object.assign(form, $event)">
     <template #image-admission><GroupImageAdmissionField :model-value="project(['allow_image_generation'])" :editable="editable" @patch="Object.assign(form, $event)" /></template>
     <template #batch-admission><GroupBatchAdmissionField :model-value="project(['allow_batch_image_generation'])" :editable="editable" @patch="Object.assign(form, $event)" /></template>
    </GroupMediaPricingPanel>
    <GroupPricingPanel :model-value="project(['rate_multiplier', 'audio_realtime_price_per_min', 'audio_stt_price_per_hour', 'audio_tts_price_per_million_chars', 'long_context_pricing_enabled', 'model_pricing', 'profit_control_enabled', 'profit_min_margin_percent', 'profit_safety_buffer_percent', 'search_price_per_1k', 'web_search_price_per_call'])" :editable="editable" :platform="form.platform" @patch="Object.assign(form, $event)">
     <template #fast-policy><GroupFastPolicyFields :model-value="project(['platform', 'force_openai_fast'])" :editable="editable" @patch="Object.assign(form, $event)"><GroupFastPricingField :model-value="project(['free_openai_fast'])" :editable="editable" @patch="Object.assign(form, $event)" /></GroupFastPolicyFields></template>
    </GroupPricingPanel>
    <CodexManifestAccountsField v-if="!creating && form.platform === 'openai'" ref="manifestField" :group-id="group!.id" v-model="manifest" :account-names="manifestNames" />
    <GroupLiveToggle v-if="form.platform === 'openai' || form.platform === 'composite'" v-model="form.allow_live" />
    <GroupMessagesFields :model-value="project(['platform', 'allow_messages_dispatch', 'opus_mapped_model', 'sonnet_mapped_model', 'haiku_mapped_model', 'exact_model_mappings'])" @patch="Object.assign(form, $event)" />
    <GroupRoutingFields :model-value="{ platform: form.platform, model_routing_enabled: form.model_routing_enabled, rules }" @patch="patchRouting" />
   </template>
  </form>
  <template #footer><div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary" :disabled="submitting" @click="close">{{ t('common.cancel') }}</button><button type="submit" :form="creating ? 'create-group-form' : 'edit-group-form'" data-tour="group-form-submit" class="btn btn-primary" :disabled="submitting || invalidated || !permissions.can('groups.manage')">{{ t(submitting ? 'common.saving' : 'common.save') }}</button></div></template>
 </BaseDialog>
</template>
<script setup lang="ts">
import { inject, watch, computed, reactive, ref } from 'vue';
import { cloneDraftValue } from '@/shared/lib/useDraftSection';
import { groupPolicyAfterPlatformChange, groupAllowsBatchImages } from '@/features/access-policy/groupEditor';
import { canConfigureImageCountBilling, resetGroupBatchPricing, resetGroupPeakPricing, groupPricingAfterPlatformChange } from '@/features/pricing/groupEditor';
import { GroupExclusivityField, GroupImageAdmissionField, GroupBatchAdmissionField, GroupFastPolicyFields } from '@/features/access-policy/panels';
import { GroupRateField, GroupPeakPricingPanel, GroupFastPricingField, GroupPricingPanel, GroupMediaPricingPanel } from '@/features/pricing/panels';
import { GroupSubscriptionPolicyPanel } from '@/features/subscriptions/panels';
import { useI18n } from 'vue-i18n';
import BaseDialog from '@/shared/ui/BaseDialog.vue';
import Select from '@/shared/ui/Select.vue';
import { GROUP_PLATFORM_OPTIONS } from '@/features/catalog';
import type { AdminGroup, CodexModelsManifestConfig } from '@/features/access-policy/api/groupTypes';
import { accessPolicyNotificationsKey, accessPolicyPermissionsKey, accessPolicyAccountsKey, groupsAPI, hydrateModelAllowlistState, buildModelAllowlistConfig, supportsReasoningEffortPolicyPlatform } from '@/features/access-policy';
import { requireAccessPolicyService } from '@/features/access-policy';
import { useAccessPolicyRequest } from '@/features/access-policy';
import { GroupIdentityPanel, GroupAdmissionFields, GroupMessagesFields, GroupModelAllowlistPanel, GroupRoutingFields, ReasoningEffortPolicyFields, CodexManifestAccountsField } from '@/features/access-policy/panels';
import { GroupLiveToggle } from '@/features/access-policy/panels';
import type { GroupEditorDraft } from './model/groupDraft';
import { createGroupEditorDraft, groupEditorDraftFromAPI, buildCreateGroupRequest, buildUpdateGroupRequest } from './model/groupDraft';
import { validateProfitControlFormState } from '@/features/pricing/groupEditor';
import { validateReasoningEffortMultipliers } from '@/features/pricing';
import { extractApiErrorMessage } from '@/shared/lib/apiError';
// The app coordinates the admission switch with the pricing draft defaults.
function resetDisabledBatchImagePricing(form: Pick<GroupEditorDraft, 'platform' | 'allow_image_generation' | 'allow_batch_image_generation' | 'image_count_billing' | 'batch_image_discount_multiplier' | 'batch_image_hold_multiplier'>) {
 form.allow_batch_image_generation = groupAllowsBatchImages(form)
 if (!form.allow_batch_image_generation) Object.assign(form, resetGroupBatchPricing())
 if (!canConfigureImageCountBilling(form.platform, form.allow_image_generation)) form.image_count_billing = false
}
import { groupEditorCreatedKey } from './services';
const onCreated = inject(groupEditorCreatedKey, null)
const props = defineProps<{ group: AdminGroup | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const permissions = requireAccessPolicyService(accessPolicyPermissionsKey)
const notifications = requireAccessPolicyService(accessPolicyNotificationsKey)
const accounts = requireAccessPolicyService(accessPolicyAccountsKey)
const creating = computed(() => props.group === null)
const form = reactive(props.group ? groupEditorDraftFromAPI(props.group) : createGroupEditorDraft())
const groups = ref<AdminGroup[]>([])
const submitting = ref(false)
const invalidated = ref(false)
const allowlist = ref(hydrateModelAllowlistState(props.group?.model_allowlist, []))
const manifest = ref<CodexModelsManifestConfig>(cloneDraftValue(props.group?.codex_models_manifest_config ?? { enabled: false, account_ids: [], fallback_to_scheduler: false }))
const manifestNames = ref<Record<number, string>>({})
const rules = ref(Object.entries(props.group?.model_routing ?? {}).map(([pattern, ids]) => ({ pattern, accounts: ids.map(id => ({ id, name: '#' + id })) })))
const reasoning = ref<{ validate(): boolean; resetValidation?(): void } | null>(null)
const manifestField = ref<{ validate(): boolean } | null>(null)
function project<K extends keyof typeof form>(keys: K[]): Pick<GroupEditorDraft, K> { return Object.fromEntries(keys.map(key => [key, form[key]])) as Pick<GroupEditorDraft, K> }
const platformOptions = GROUP_PLATFORM_OPTIONS
const editable = (field: string) => permissions.isSuperAdmin || permissions.canEditAdminField(creating.value ? 'group.create' : 'group.update', field)
const initialization = useAccessPolicyRequest(() => { groups.value = []; manifestNames.value = {} })
const permissionFields = [...Object.keys(form), 'model_routing', 'model_allowlist', 'codex_models_manifest_config', 'messages_dispatch_model_config', 'profit_min_margin', 'profit_safety_buffer']
const fieldPermissions = computed(() => permissionFields.map(field => permissions.canEditAdminField(creating.value ? 'group.create' : 'group.update', field) ? '1' : '0').join(''))
const request = useAccessPolicyRequest(() => {
 invalidated.value = true
 submitting.value = false
 initialization.cancel()
 emit('close')
})
watch(
 [() => props.group?.id ?? null, () => permissions.can('groups.manage'), () => permissions.isSuperAdmin, () => permissions.isSimpleMode, fieldPermissions],
 () => request.cancel(),
 { flush: 'sync' },
)

function patchRouting(patch: { model_routing_enabled?: boolean; rules?: typeof rules.value }) { if (patch.model_routing_enabled !== undefined) form.model_routing_enabled = patch.model_routing_enabled; if (patch.rules) rules.value = patch.rules }
function close() { if (!submitting.value) emit('close') }
async function submit() {
 if (invalidated.value || submitting.value || !permissions.can('groups.manage')) return
 if (!form.name.trim()) { notifications.showError(t('admin.groups.nameRequired')); return }
 if (supportsReasoningEffortPolicyPlatform(form.platform) && reasoning.value && !reasoning.value.validate()) return
 const profitError = validateProfitControlFormState(form)
 if (profitError) { notifications.showError(t('admin.groups.profitControl.' + profitError)); return }
 for (const entry of form.model_pricing) {
  const error = validateReasoningEffortMultipliers(entry.reasoning_effort_multipliers, t)
  if (error) { notifications.showError(error); return }
 }
 if (!creating.value && form.platform === 'openai' && manifest.value.enabled && !manifest.value.account_ids.length) { manifestField.value?.validate(); return }
 const pending = request.begin()
 const creatingAtStart = creating.value
 const targetId = props.group?.id ?? null
 const current = () => pending.isCurrent() && !invalidated.value && permissions.can('groups.manage') && (props.group?.id ?? null) === targetId
 submitting.value = true
 try {
  const draft = cloneDraftValue(form)
  const routing: Record<string, number[]> = {}
  for (const rule of rules.value) {
   const ids = rule.accounts.map(account => account.id).filter(id => id > 0)
   if (rule.pattern.trim() && ids.length) routing[rule.pattern.trim()] = ids
  }
  const details = { routing: Object.keys(routing).length ? routing : null, allowlist: buildModelAllowlistConfig(allowlist.value), manifest: cloneDraftValue(manifest.value) }
  if (creatingAtStart) {
   const payload = buildCreateGroupRequest(draft, details, permissions.isSimpleMode)
   await groupsAPI.create(permissions.isSuperAdmin ? payload : permissions.editableAdminFields('group.create', payload))
  } else {
   const payload = buildUpdateGroupRequest(draft, details, permissions.isSimpleMode)
   await groupsAPI.update(targetId!, permissions.isSuperAdmin ? payload : permissions.editableAdminFields('group.update', payload))
  }
  if (!current()) return
  notifications.showSuccess(t(creatingAtStart ? 'admin.groups.groupCreated' : 'admin.groups.groupUpdated'))
  emit('saved')
  emit('close')
  if (creatingAtStart && onCreated) {
   try { onCreated() } catch (error) { console.error('Group creation tour callback failed:', error) }
  }
 } catch (error) {
  if (current()) notifications.showError(extractApiErrorMessage(error, t(creatingAtStart ? 'admin.groups.failedToCreate' : 'admin.groups.failedToUpdate')))
 } finally {
  if (current()) submitting.value = false
 }
}

async function initialize() {
 if (invalidated.value || !permissions.can('groups.manage')) return
 const pending = initialization.begin()
 try {
  const data = await groupsAPI.getAll(undefined, { signal: pending.signal })
  if (!pending.isCurrent() || invalidated.value) return
  groups.value = data
  const names = await Promise.allSettled(manifest.value.account_ids.map(id => accounts.getById(id, pending.signal)))
  if (!pending.isCurrent() || invalidated.value) return
  for (const result of names) if (result.status === 'fulfilled') manifestNames.value[result.value.id] = result.value.name
 } catch (error) {
  if (pending.isCurrent() && !invalidated.value) notifications.showError(extractApiErrorMessage(error, t('admin.groups.failedToLoad')))
 }
}

watch(
  () => form.subscription_type,
  (newVal) => {
    if (newVal === "subscription") {
      form.is_exclusive = true;
      form.fallback_group_id_on_invalid_request = null;
    } else {
      Object.assign(form, resetGroupPeakPricing());
    }
  },
);
watch(
 () => form.platform,
 platform => {
  Object.assign(form, groupPolicyAfterPlatformChange(form), groupPricingAfterPlatformChange(form, platform))
  reasoning.value?.resetValidation?.()
  resetDisabledBatchImagePricing(form)
  allowlist.value = hydrateModelAllowlistState(undefined, [])
 },
)
watch(
  () => form.allow_image_generation,
  () => {
    resetDisabledBatchImagePricing(form);
  },
);
watch(
  () => form.allow_batch_image_generation,
  () => {
    resetDisabledBatchImagePricing(form);
  },
);
void initialize()
</script>
