<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { formatDateTime } from '@/utils/format'
import { accountOpenAIModes, openAIAuthorizationModes, vendorOpenAIModes } from '../helpers'
import type { AccountAuthorizationPatch, OpenAIAuthorizationMode, Vendor, VendorAccount } from '../types'

const props = defineProps<{ account: VendorAccount; vendor?: Vendor; readOnly: boolean; busy: boolean }>()
const emit = defineEmits<{
  authorize: [mode: OpenAIAuthorizationMode]
  update: [patch: AccountAuthorizationPatch]
  revoke: [mode: OpenAIAuthorizationMode]
  refresh: [mode: OpenAIAuthorizationMode]
}>()
const { t, te } = useI18n()
const modes = computed(() => accountOpenAIModes(props.account))
const permittedModes = computed(() => vendorOpenAIModes(props.vendor, props.account))
const canManage = (mode: OpenAIAuthorizationMode) =>
  !props.readOnly && !props.account.archived && permittedModes.value.includes(mode)
const grant = (mode: OpenAIAuthorizationMode) => props.account.authorizations?.find(value => value.mode === mode)
function statusLabel(mode: OpenAIAuthorizationMode) {
  const status = grant(mode)?.status || (props.account.authorizations ? 'unconfigured' : '')
  return status && te('vendor.authorization.status.' + status) ? t('vendor.authorization.status.' + status) : t('vendor.unknown')
}
const allModels = ref(true), modelText = ref('')
watch(() => props.account.bps_models, models => {
  allModels.value = models === null
  modelText.value = models?.join('\n') || ''
}, { immediate: true })
const modelNames = computed(() => [...new Set(modelText.value.split(/[\s,]+/).filter(Boolean))])
const modelsChanged = computed(() => props.account.bps_models !== undefined &&
  (allModels.value ? props.account.bps_models !== null :
    props.account.bps_models === null || JSON.stringify(modelNames.value) !== JSON.stringify(props.account.bps_models)))
function saveModels() {
  if (props.busy || !canManage('bps') || !modelsChanged.value) return
  emit('update', allModels.value ? { bps_all_models: true } : { bps_all_models: false, bps_models: modelNames.value })
}
</script>

<template>
  <section class="space-y-3" data-detail-section="authorizations">
    <h3 class="font-semibold">{{ t('vendor.authorization.management') }}</h3>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.authorization.routingHelp') }}</p>
    <div class="grid gap-3 sm:grid-cols-2">
    <div v-for="mode in openAIAuthorizationModes" :key="mode" :data-authorization="mode" class="space-y-3 rounded-xl border border-gray-200 p-3 dark:border-dark-600">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h4 class="text-sm font-semibold">{{ t('vendor.authorization.' + mode) }}</h4>
        <span class="badge" :class="grant(mode)?.status === 'active' ? 'badge-success' : 'badge-gray'" data-authorization-status>{{ statusLabel(mode) }}</span>
      </div>
      <dl v-if="grant(mode)" class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-2 gap-y-3 text-xs">
        <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.authorization.dispatch') }}</dt>
        <dd class="break-words text-right">{{ t(grant(mode)?.enabled ? 'vendor.authorization.enabled' : 'vendor.disabled') }}</dd>
        <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.authorizationExpiresAt') }}</dt>
        <dd class="break-words text-right">{{ formatDateTime(grant(mode)?.expires_at) || '—' }}</dd>
        <dt class="text-gray-500 dark:text-gray-400">{{ t('vendor.refreshAvailable') }}</dt>
        <dd class="break-words text-right">{{ t(grant(mode)?.can_refresh ? 'vendor.yes' : 'vendor.no') }}</dd>
      </dl>
      <div v-if="canManage(mode)" class="flex flex-wrap items-center gap-2">
        <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" :data-testid="'authorize-' + mode" @click="emit('authorize', mode)">
          {{ t(modes.includes(mode) ? 'vendor.reauthorize' : 'vendor.authorization.attach') }}
        </button>
        <template v-if="modes.includes(mode) && grant(mode)">
          <button v-if="grant(mode)?.can_refresh" type="button" class="btn btn-secondary btn-sm" :disabled="busy" :data-testid="'refresh-' + mode" @click="emit('refresh', mode)">{{ t('vendor.refreshToken') }}</button>
          <Toggle :model-value="grant(mode)?.enabled === true" :aria-label="t('vendor.authorization.' + mode) + ' · ' + t('vendor.authorization.dispatch')" :disabled="busy"
            :data-testid="'enable-' + mode" @update:model-value="emit('update', mode === 'native' ? { native_enabled: $event } : { bps_enabled: $event })" />
          <button type="button" class="btn btn-ghost btn-sm text-red-600 dark:text-red-400" :disabled="busy" :data-testid="'revoke-' + mode" @click="emit('revoke', mode)">{{ t('vendor.authorization.revoke') }}</button>
        </template>
      </div>
    </div>
    </div>
    <details v-if="modes.includes('bps')" class="rounded-xl border border-gray-200 p-3 dark:border-dark-600" data-testid="bps-model-settings">
      <summary class="cursor-pointer text-sm font-medium">{{ t('vendor.authorization.models') }}</summary>
      <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.authorization.modelsHelp') }}</p>
      <form v-if="canManage('bps') && account.bps_models !== undefined" class="mt-3 space-y-3" @submit.prevent="saveModels">
        <fieldset :disabled="busy" class="space-y-3">
          <label class="flex items-center gap-2 text-sm"><input v-model="allModels" type="checkbox" data-testid="bps-all-models" />{{ t('vendor.authorization.allTextModels') }}</label>
          <label v-if="!allModels" class="block text-sm">{{ t('vendor.authorization.modelNames') }}
            <textarea v-model="modelText" rows="3" class="input mt-1 font-mono" spellcheck="false" :aria-label="t('vendor.authorization.modelNames')" />
          </label>
          <p v-if="!allModels && !modelNames.length" class="text-xs text-amber-700 dark:text-amber-400">{{ t('vendor.authorization.noModelsHelp') }}</p>
          <button type="submit" class="btn btn-secondary btn-sm" :disabled="!modelsChanged">{{ t('vendor.save') }}</button>
        </fieldset>
      </form>
      <p v-else class="mt-2 break-words text-sm">{{ account.bps_models === undefined ? t('vendor.unknown') : account.bps_models === null ? t('vendor.authorization.allTextModels') : account.bps_models.length ? account.bps_models.join(', ') : t('vendor.authorization.noModels') }}</p>
    </details>
  </section>
</template>
