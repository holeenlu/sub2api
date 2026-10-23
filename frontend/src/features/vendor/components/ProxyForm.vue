<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ProxyImport from './ProxyImport.vue'
import ProxyFormFields from '@/components/common/ProxyFormFields.vue'
import Select from '@/components/common/Select.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { resourceAPI } from '../api'
import { toLocalDateTime } from '../helpers'
import type { ResourceScope, VendorProxy } from '../types'

const props = defineProps<{ scope: ResourceScope; proxy?: VendorProxy }>()
const emit = defineEmits<{ close: []; saved: [proxy: VendorProxy] }>()
const { t } = useI18n()
const quick = ref(false)
const batchCount = ref(0)
const saving = ref(false)
const importBusy = ref(false)
const busy = computed(() => saving.value || importBusy.value)
const completed = ref(false)
const error = ref('')
const form = ref({
  name: props.proxy?.name || '',
  protocol: props.proxy?.protocol || 'http',
  host: props.proxy?.host || '',
  port: props.proxy?.port || 8080,
  username: '',
  password: '',
  credential_action: 'preserve',
  vendor_notes: props.proxy?.vendor_notes || '',
  region_label_country_code: props.proxy?.region_label_country_code || '',
  region_label_city: props.proxy?.region_label_city || '',
  expires_at: props.proxy?.expires_at ? toLocalDateTime(props.proxy.expires_at) : '',
  status: props.proxy?.status || 'active',
  fallback_mode: props.proxy?.fallback_mode || 'none',
  backup_proxy_id: props.proxy?.backup_proxy_id ?? null,
  expiry_warn_days: props.proxy?.expiry_warn_days ?? 7
})
const backupOptions = ref<{ label: string; value: number }[]>([])
const backupLoading = ref(false)
const backupError = ref('')
let backupEpoch = 0
let backupRequest: AbortController | undefined
async function loadBackups() {
  const epoch = ++backupEpoch
  backupRequest?.abort()
  backupOptions.value = []
  backupError.value = ''
  backupLoading.value = false
  if (form.value.fallback_mode !== 'proxy' || props.scope.readOnly) return
  const controller = new AbortController()
  backupRequest = controller
  const scope = { ...props.scope }
  const owner = scope.vendorId || props.proxy?.vendor_id
  backupLoading.value = true
  try {
    if (scope.admin && !owner) throw new Error('A direct vendor scope is required')
    const api = resourceAPI(owner ? { ...scope, vendorId: owner } : scope)
    const seen = new Set<number>()
    const options: { label: string; value: number }[] = []
    let total = 0
    for (let page = 1; ; page++) {
      const result = await api.proxies({ page, page_size: 50, sort_by: 'created_at', sort_order: 'asc' }, controller.signal)
      if (epoch !== backupEpoch) return
      if (page === 1) total = result.total
      // The existing scoped endpoint is bounded; never accept a partial list.
      if (!Number.isInteger(total) || total < 0 || total > 10000 || result.total !== total ||
        result.items.length !== Math.min(50, total - (page - 1) * 50)) throw new Error('Incomplete proxy options')
      for (const proxy of result.items) {
        if (seen.has(proxy.id)) throw new Error('Incomplete proxy options')
        seen.add(proxy.id)
        if (proxy.id === props.proxy?.id || !proxy.vendor_id || (owner != null && proxy.vendor_id !== owner)) continue
        const host = proxy.host.includes(':') ? '[' + proxy.host + ']' : proxy.host
        options.push({ value: proxy.id, label: proxy.name + ' (' + host + ':' + proxy.port + ')' })
      }
      if (page * 50 >= total) break
    }
    backupOptions.value = options
  } catch {
    if (epoch === backupEpoch) backupError.value = t('admin.proxies.failedToLoad')
  } finally {
    if (epoch === backupEpoch) backupLoading.value = false
  }
}
watch(() => [form.value.fallback_mode, props.scope.admin, props.scope.vendorId, props.scope.readOnly], loadBackups, { immediate: true, flush: 'sync' })
let operationKey = crypto.randomUUID()
watch(form, () => { operationKey = crypto.randomUUID() }, { deep: true, flush: 'sync' })

async function imported(ids: number[]) {
  if (!ids.length) return
  saving.value = true
  try {
    const proxy = await resourceAPI(props.scope).proxy(ids[0])
    completed.value = true
    emit('saved', proxy)
  } catch {
    error.value = t('admin.proxies.failedToLoad')
  } finally {
    saving.value = false
  }
}

async function save() {
  if (busy.value || completed.value || props.scope.readOnly) return
  if (!form.value.name.trim()) {
    error.value = t('admin.proxies.nameRequired')
    return
  }
  if (!form.value.host.trim()) {
    error.value = t('admin.proxies.hostRequired')
    return
  }
  if (!Number.isInteger(form.value.port) || form.value.port < 1 || form.value.port > 65535) {
    error.value = t('admin.proxies.portInvalid')
    return
  }
  if (form.value.fallback_mode === 'direct') return
  if (form.value.fallback_mode === 'proxy' && (backupLoading.value || backupError.value || !backupOptions.value.some(option => option.value === form.value.backup_proxy_id))) {
    error.value = t('admin.proxies.backupProxy')
    return
  }
  saving.value = true
  error.value = ''
  try {
    const { username, password, credential_action, name, protocol, host, port, expires_at,
      vendor_notes, region_label_country_code, region_label_city, status, fallback_mode, backup_proxy_id, expiry_warn_days } = form.value
    const body = {
      name: name.trim(), protocol, host: host.trim(), port,
      fallback_mode, backup_proxy_id: fallback_mode === 'proxy' ? backup_proxy_id : null, expiry_warn_days,
      expires_at: expires_at ? new Date(expires_at).toISOString() : null,
      ...(props.proxy ? {
        config_version: props.proxy.config_version, credential_action,
        vendor_notes, region_label_country_code, region_label_city, status
      } : {}),
      ...(!props.proxy || credential_action === 'update' ? {
        ...(username ? { username } : {}),
        ...(password ? { password } : {})
      } : {})
    }
    const api = resourceAPI(props.scope)
    const saved = props.proxy
      ? await api.updateProxy(props.proxy.id, body, operationKey)
      : await api.createProxy(body, operationKey)
    completed.value = true
    form.value.username = ''
    form.value.password = ''
    emit('saved', saved)
  } catch {
    // API errors can include submitted URLs or credentials; never render them.
    error.value = t(props.proxy ? 'admin.proxies.failedToUpdate' : 'admin.proxies.failedToCreate')
  } finally {
    saving.value = false
  }
}
onUnmounted(() => { backupEpoch++; backupRequest?.abort(); form.value.username = ''; form.value.password = '' })
</script>
<template>
  <BaseDialog show :title="t(proxy ? 'admin.proxies.editProxy' : 'admin.proxies.createProxy')"
    :close-on-escape="!busy" :show-close-button="!busy" @close="!busy && emit('close')">
    <div v-if="!proxy" class="mb-6 flex items-center justify-between gap-3 border-b border-gray-200 dark:border-dark-600">
      <div class="flex min-w-0 shrink-0">
        <button v-for="mode in [false, true]" :key="String(mode)" type="button" :disabled="busy || completed || scope.readOnly"
          class="-mb-px border-b-2 px-4 py-2 text-sm font-medium transition-colors"
          :class="quick === mode
            ? 'border-primary-500 text-primary-600 dark:text-primary-400'
            : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300'"
          :aria-pressed="quick === mode" @click="quick = mode; error = ''">
          <Icon :name="mode ? 'menu' : 'plus'" size="sm" class="mr-1.5 inline" />
          {{ t(mode ? 'admin.proxies.batchAdd' : 'admin.proxies.standardAdd') }}
        </button>
      </div>
    </div>
    <p v-if="error" role="alert" class="mb-4 text-red-600">{{ error }}</p>
    <ProxyImport v-if="quick && !proxy" embedded :scope="scope" @saved="imported"
      @busy="importBusy = $event" @parsed="batchCount = $event" @close="!busy && emit('close')" />
    <form v-else id="vendor-proxy-form" class="space-y-5" @submit.prevent="save">
      <ProxyFormFields v-model="form" :disabled="busy || completed || scope.readOnly"
        :show-credentials="!proxy || form.credential_action === 'update'"
        :backup-options="backupOptions" :fallback-disabled="backupLoading" :allow-direct-fallback="false">
        <template v-if="proxy" #credentials>
          <div>
            <label class="input-label">{{ t('vendor.credentialAction') }}</label>
            <Select v-model="form.credential_action" :options="[
              { value: 'preserve', label: t('vendor.preserve') },
              { value: 'update', label: t('vendor.replace') },
              { value: 'clear', label: t('vendor.clearCredentials') }
            ]" :aria-label="t('vendor.credentialAction')" :disabled="busy || completed || scope.readOnly" />
          </div>
        </template>
        <template v-if="proxy" #expiry>
          <label class="block">{{ t('admin.proxies.expiresAt') }}
            <input v-model="form.expires_at" type="datetime-local" :disabled="busy || completed || scope.readOnly" class="input mt-1" />
          </label>
        </template>
      </ProxyFormFields>
      <p v-if="backupError" role="alert" class="text-red-600">
        {{ backupError }}
        <button type="button" class="btn btn-secondary btn-sm" :disabled="backupLoading || busy" @click="loadBackups">
          <Icon name="refresh" size="sm" class="mr-1" />{{ t('vendor.retry') }}
        </button>
      </p>
      <!-- Supplier metadata and lifecycle controls belong to editing only. -->
      <template v-if="proxy">
        <div class="grid grid-cols-2 gap-4">
          <label>{{ t('vendor.country') }}<input v-model="form.region_label_country_code" maxlength="2" :disabled="busy || completed || scope.readOnly" class="input mt-1" /></label>
          <label>{{ t('vendor.city') }}<input v-model="form.region_label_city" :disabled="busy || completed || scope.readOnly" class="input mt-1" /></label>
        </div>
        <div>
          <label class="input-label">{{ t('vendor.status') }}</label>
          <Select v-model="form.status" :options="['active', 'inactive'].map(value => ({ value, label: t('vendor.' + value) }))"
            :aria-label="t('vendor.status')" :disabled="busy || completed || scope.readOnly" />
        </div>
        <label class="block">{{ t('vendor.notes') }}<textarea v-model="form.vendor_notes" maxlength="2000" :disabled="busy || completed || scope.readOnly" class="input mt-1" /></label>
        <dl class="grid grid-cols-2 gap-2 rounded-lg border p-3 text-sm">
          <dt>{{ t('vendor.accounts') }}</dt><dd>{{ proxy.references?.accounts?.join(', ') || '—' }}</dd>
          <dt>{{ t('vendor.backups') }}</dt><dd>{{ proxy.references?.backups?.join(', ') || '—' }}</dd>
          <dt>{{ t('vendor.oauth') }}</dt><dd>{{ proxy.references?.oauth ?? '—' }}</dd>
          <dt>{{ t('vendor.inflight') }}</dt><dd>{{ proxy.references?.inflight ?? '—' }}</dd>
        </dl>
      </template>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button v-if="!quick || proxy" type="submit" form="vendor-proxy-form" class="btn btn-primary" :disabled="busy || completed || backupLoading || scope.readOnly">
          <Icon v-if="busy" name="refresh" size="sm" class="mr-2 animate-spin" />
          {{ t(busy ? (proxy ? 'common.saving' : 'admin.proxies.creating') : (proxy ? 'common.save' : 'common.create')) }}
        </button>
        <button v-else type="submit" form="vendor-proxy-import-form" class="btn btn-primary" :disabled="busy || completed || scope.readOnly || !batchCount">
          <Icon v-if="busy" name="refresh" size="sm" class="mr-2 animate-spin" />
          {{ busy ? t('admin.proxies.importing') : t('admin.proxies.importProxies', { count: batchCount }) }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>
