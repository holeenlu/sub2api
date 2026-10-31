<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import { formatCostFixed } from '@/shared/lib/format'
import { isKnownKeyMetric } from '../model/keyUsageDisplay'
import { keysAPI } from '../api/keys'
import type { ApiKey } from '../api/types'
import { useKeysServices } from '../services'
import { keyErrorMessage, useKeysRequest } from '../queries/useKeysRequest'
import type { KeyGroupOption } from '../queries/useKeyGroups'
import { emptyKeyDraft, editKeyDraft, validateKeyDraft, createKeyPayload, updateKeyPayload } from '../model/keyDraft'
import KeyIdentityFields from './KeyIdentityFields.vue'
import KeyRestrictionFields from './KeyRestrictionFields.vue'
import KeyExpirationFields from './KeyExpirationFields.vue'

const props = defineProps<{ show: boolean; apiKey: ApiKey | null; groupOptions: KeyGroupOption[] }>()
const emit = defineEmits<{ close: []; saved: []; changed: [key: ApiKey] }>()
const { t } = useI18n()
const services = useKeysServices()
const request = useKeysRequest(services)
const baseline = ref<ApiKey | null>(null)
const draft = ref(emptyKeyDraft())
const submitting = ref(false)
const error = ref('')
const reset = ref<'quota' | 'rate' | null>(null)
const editing = computed(() => baseline.value !== null)
const resetQuotaAmount = computed(() => {
  const used = baseline.value?.quota_used
  return isKnownKeyMetric(used) ? formatCostFixed(used) : '—'
})
watch(() => [props.show, props.apiKey?.id] as const, () => {
  request.cancel(); submitting.value = false; error.value = ''; reset.value = null
  baseline.value = props.show ? props.apiKey : null
  draft.value = baseline.value ? editKeyDraft(baseline.value) : emptyKeyDraft()
}, { immediate: true, flush: 'sync' })
function close(): void { if (!submitting.value) emit('close') }
async function submit(): Promise<void> {
  if (submitting.value) return
  const validation = validateKeyDraft(draft.value, editing.value)
  if (validation) { error.value = t(validation); services.notifyError(error.value); return }
  const ticket = request.begin()
  if (!ticket.isCurrent()) return
  const original = baseline.value
  const snapshot = { ...draft.value }
  submitting.value = true; error.value = ''
  try {
    if (original) await keysAPI.update(original.id, updateKeyPayload(snapshot, original), ticket)
    else {
      const payload = createKeyPayload(snapshot)
      await keysAPI.create(payload.name, payload.group_id, payload.custom_key, payload.ip_whitelist,
        payload.ip_blacklist, payload.quota, payload.expires_in_days, payload, ticket)
    }
    if (!ticket.isCurrent()) return
    services.notifySuccess(t(original ? 'keys.keyUpdatedSuccess' : 'keys.keyCreatedSuccess'))
    if (!original) services.onKeyCreated()
    emit('saved')
  } catch (failure) {
    if (!ticket.isCurrent()) return
    error.value = keyErrorMessage(failure, t('keys.failedToSave'))
    services.notifyError(error.value)
  } finally { if (ticket.isCurrent()) submitting.value = false }
}
async function resetUsage(): Promise<void> {
  const key = baseline.value
  const kind = reset.value
  if (!key || !kind || submitting.value) return
  const ticket = request.begin()
  if (!ticket.isCurrent()) return
  reset.value = null; submitting.value = true
  try {
    const updated = await keysAPI.update(key.id, kind === 'quota' ? { reset_quota: true } : { reset_rate_limit_usage: true }, ticket)
    if (!ticket.isCurrent()) return
    baseline.value = updated
    if (kind === 'quota' && key.status !== updated.status) draft.value.status = updated.status === 'active' ? 'active' : 'inactive'
    services.notifySuccess(t(kind === 'quota' ? 'keys.quotaResetSuccess' : 'keys.rateLimitResetSuccess'))
    emit('changed', updated)
  } catch (failure) {
    if (ticket.isCurrent()) services.notifyError(keyErrorMessage(failure, t(kind === 'quota' ? 'keys.failedToResetQuota' : 'keys.failedToResetRateLimit')))
  } finally { if (ticket.isCurrent()) submitting.value = false }
}
watch(services.scopeKey, () => {
  baseline.value = null; draft.value = emptyKeyDraft(); reset.value = null; submitting.value = false; error.value = ''; emit('close')
}, { flush: 'sync' })
</script>
<template>
  <BaseDialog :show="show" :title="t(editing ? 'keys.editKey' : 'keys.createKey')" width="normal"
    :close-on-escape="!submitting" :show-close-button="!submitting" @close="close">
    <form id="key-form" class="space-y-5" @submit.prevent="submit">
      <fieldset :disabled="submitting" class="space-y-5">
        <KeyIdentityFields v-if="show" v-model="draft" :editing="editing" :group-options="groupOptions" />
        <KeyRestrictionFields v-model="draft" :editing="editing" :selected-key="baseline" @reset-quota="reset = 'quota'" @reset-rate-limit="reset = 'rate'" />
        <KeyExpirationFields v-model="draft" :editing="editing" :selected-key="baseline" />
      </fieldset>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="close">{{ t('common.cancel') }}</button>
        <button form="key-form" type="submit" class="btn btn-primary" data-tour="key-form-submit" :disabled="submitting">
          {{ t(submitting ? 'keys.saving' : editing ? 'common.update' : 'common.create') }}
        </button>
      </div>
    </template>
  </BaseDialog>
  <ConfirmDialog :show="reset !== null" :title="t(reset === 'quota' ? 'keys.resetQuotaTitle' : 'keys.resetRateLimitTitle')"
    :message="reset === 'quota' ? t('keys.resetQuotaConfirmMessage', { name: baseline?.name, used: resetQuotaAmount }) : t('keys.resetRateLimitConfirmMessage', { name: baseline?.name })"
    :confirm-text="t('keys.reset')" :cancel-text="t('common.cancel')" danger @confirm="resetUsage" @cancel="reset = null" />
</template>
