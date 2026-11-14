<script setup lang="ts">
import { computed, onBeforeUnmount, provide, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/features/identity'
import { accountsAPI } from '@/features/resources/accounts'
import { readAccountModelReferences } from '@/features/resources/modelReferences'
import { loadModelWhitelistSelector } from '@/features/resources/panelLoaders'
import { resourceEditorServicesKey, useResourceEditorServices } from '@/features/resources/services'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { lazyPanel } from '../panelLoadingServices'

const ModelWhitelistSelector = lazyPanel(loadModelWhitelistSelector)
const props = defineProps<{ show: boolean; accountId: number; platform: string }>()
const emit = defineEmits<{ close: []; saved: [accountId: number] }>()
const { t } = useI18n()
const auth = useAuthStore()
const initialActor = auth.identityScopeKey
const services = useResourceEditorServices()
const dismissed = ref(false)
const allowed = computed(() => props.show && !dismissed.value && auth.isAuthenticated && auth.identityScopeKey === initialActor
  && auth.can('accounts.manage') && ['anthropic', 'openai'].includes(props.platform))
const scope = computed(() => JSON.stringify([
  auth.identityScopeKey, auth.isAuthenticated, auth.can('accounts.manage'),
  auth.can('accounts.authorize'), auth.isSuperAdmin, props.accountId, props.platform, allowed.value
]))
// Bind the CAS consumer to this dialog's real actor and lifetime, including a
// synchronous close before the parent has unmounted the lazy panel.
provide(resourceEditorServicesKey, {
  ...services,
  scopeKey: () => allowed.value ? scope.value : '',
  access: {
    get isSuperAdmin() { return auth.isSuperAdmin },
    get isSimpleMode() { return auth.isSimpleMode },
    get canAuthorize() { return auth.can('accounts.authorize') },
    get canManage() { return allowed.value },
    get canReadModelCatalog() { return allowed.value }
  }
})
const models = ref<string[]>([])
const ready = ref(false)
const error = ref('')
const busy = ref(false)
const failedDraft = ref(false)
let controller: AbortController | undefined
let generation = 0
function dismiss() {
  dismissed.value = true
  controller?.abort()
  emit('close')
}
function close() {
  if (busy.value || (failedDraft.value && !window.confirm(t('admin.accounts.modelReference.discardFailedDraft')))) return
  dismiss()
}
async function load() {
  controller?.abort()
  const version = ++generation
  ready.value = false
  models.value = []
  error.value = ''
  if (!allowed.value) return
  const request = new AbortController()
  controller = request
  const accountID = props.accountId, platform = props.platform
  const current = () => version === generation && !request.signal.aborted && allowed.value
  try {
    const account = await accountsAPI.getById(accountID, { signal: request.signal })
    if (!current()) return
    if (account.id !== accountID || account.platform !== platform) throw new Error(t('admin.accounts.modelReference.scopeMismatch'))
    models.value = readAccountModelReferences(platform, account.credentials?.model_mapping, account.credentials?.model_allowlist).allowedModels
    ready.value = true
  } catch (failure) {
    if (current()) error.value = extractApiErrorMessage(failure, t('admin.accounts.modelReference.loadFailed'))
  }
}
watch(scope, () => {
  void load()
  if (props.show && !allowed.value && !dismissed.value) dismiss()
}, { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { generation++; controller?.abort() })
function saved(value: string[]) {
  if (!allowed.value || !ready.value) return
  models.value = [...value]
  emit('saved', props.accountId)
}
</script>

<template>
  <BaseDialog :show="allowed" :title="t('admin.accounts.modelWhitelist')" :show-close-button="!busy" :close-on-escape="!busy" @close="close">
    <template #header="{ titleId }">
      <h3 :id="titleId" class="modal-title">{{ t('admin.accounts.modelWhitelist') }}</h3>
      <button type="button" :disabled="busy" :aria-label="t('ui.closeModal')" class="rounded-xl p-2 text-gray-400 disabled:opacity-50" @click="close"><Icon name="x" size="md" /></button>
    </template>
    <ModelWhitelistSelector v-if="ready && allowed" :key="scope" :model-value="models" :platform="platform" :account-id="accountId" save-mode="immediate" @update:model-value="saved" @busy-change="busy = $event" @draft-change="failedDraft = $event" />
    <div v-else-if="error" role="alert">
      <p>{{ error }}</p>
      <button type="button" class="btn btn-secondary" @click="load">{{ t('common.tryAgain') }}</button>
    </div>
    <p v-else role="status">{{ t('common.loading') }}</p>
  </BaseDialog>
</template>
