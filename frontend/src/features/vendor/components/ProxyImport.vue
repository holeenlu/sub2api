<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import ProxyBatchInput, { type ProxyBatchParseResult } from '@/components/common/ProxyBatchInput.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { resourceAPI } from '../api'
import { readImport } from '../helpers'
import type { ImportResult, ResourceScope } from '../types'
const props = defineProps<{ scope: ResourceScope; embedded?: boolean }>(),
  emit = defineEmits<{ close: []; saved: [proxyIds: number[]]; busy: [value: boolean]; parsed: [count: number] }>()
const { t, te } = useI18n(),
  content = ref(''),
  format = ref('quick'),
  busy = ref(false),
  error = ref(''),
  result = ref<ImportResult>(),
  digest = ref('')
const completed = ref(false)
function parsed(result: ProxyBatchParseResult) { emit('parsed', result.valid) }
// Only known machine codes may be displayed; response messages can contain secrets.
const safeCodes = new Set(['READY', 'PROXY_DUPLICATE', 'PROXY_URL_INVALID', 'PROXY_INPUT_INVALID',
  'PROXY_HOST_INVALID', 'PROXY_PROTOCOL_INVALID', 'PROXY_COUNTRY_INVALID', 'PROXY_EXPIRY_INVALID',
  'PROXY_STATUS_INVALID', 'PROXY_FALLBACK_INVALID', 'PROXY_BACKUP_REQUIRED', 'PROXY_BACKUP_SELF', 'PROXY_WARN_DAYS_INVALID', 'PROXY_NOT_FOUND', 'PROXY_LIMIT', 'PROXY_IMPORT_CANCELLED', 'PROXY_IMPORT_FAILED'])
let generation = 0
let operationKey = crypto.randomUUID()
watch(busy, (value) => emit('busy', value), { flush: 'sync' })
function rowStatus(status: string) {
  const key =
    (
      {
        valid: 'validRows',
        ready: 'validRows',
        created: 'success',
        updated: 'success',
        duplicate: 'duplicateRows',
        invalid: 'invalidRows',
        unresolved: 'unknown'
      } as Record<string, string>
    )[status] || status
  return te('vendor.' + key) ? t('vendor.' + key) : t('vendor.unknown')
}
const rows = computed(() => result.value?.rows || result.value?.items || [])
watch(
  [content, format, () => props.scope.admin, () => props.scope.vendorId, () => props.scope.readOnly],
  () => {
    generation++
    completed.value = false
    operationKey = crypto.randomUUID()
    error.value = ''
    digest.value = ''
    result.value = undefined
  },
  { flush: 'sync' }
)
async function fileChanged(event: Event) {
  const current = ++generation
  try {
    const file = (event.target as HTMLInputElement).files?.[0]
    if (file) {
      const text = await readImport(file, t('vendor.importSize'))
      if (current === generation) {
        format.value = 'json'
        content.value = text
      }
    }
  } catch (e) {
    error.value = e instanceof SyntaxError ? t('vendor.invalidJSON') : t('admin.proxies.failedToImport')
  }
}
async function submit(preview: boolean, quickCreate = false) {
  if (busy.value || completed.value || props.scope.readOnly || !content.value.trim()) return
  if (!preview && (!digest.value || !result.value?.valid_count)) return
  const current = generation
  const input = { format: format.value, content: content.value }
  const key = operationKey
  busy.value = true
  error.value = ''
  try {
    const api = resourceAPI(props.scope)
    const confirming = !preview || (quickCreate && !!digest.value && !!result.value?.valid_count)
    let response = await api.proxyImport(
      { ...input, ...(confirming ? { input_digest: digest.value } : {}) },
      !confirming, confirming ? key : undefined
    )
    if (current !== generation) return
    result.value = response
    digest.value = !confirming ? response.input_digest || '' : ''
    if (quickCreate && !confirming) {
      // Keep the server parser and digest contract; preserve the original input.
      if (!digest.value || !response.valid_count) return
      response = await api.proxyImport({ ...input, input_digest: digest.value }, false, key)
      if (current !== generation) return
    }
    if (!preview || quickCreate) {
      content.value = ''
      completed.value = true
      result.value = response
      digest.value = ''
      emit('saved', (response.rows || response.items || []).flatMap(row => row.proxy_id ? [row.proxy_id] : []))
    }
  } catch (e) {
    if (current !== generation) return
    error.value = e instanceof SyntaxError ? t('vendor.invalidJSON') : t('admin.proxies.failedToImport')
  } finally {
    busy.value = false
  }
}
onUnmounted(() => {
  generation++
  content.value = ''
  digest.value = ''
})
</script>
<template>
  <component
    :is="embedded ? 'form' : BaseDialog"
    :id="embedded ? 'vendor-proxy-import-form' : undefined"
    @submit.prevent="embedded && submit(true, true)"
    show
    :title="t('vendor.import')"
    width="wide"
    :close-on-escape="!busy"
    :show-close-button="!busy"
    @close="emit('close')"
    ><div class="space-y-4">
      <p v-if="!embedded" class="text-sm text-gray-500">{{ t('vendor.importPrivacy') }}</p>
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      <Select
        v-if="!embedded"
        v-model="format"
        :options="[
          { value: 'quick', label: t('vendor.quick') },
          { value: 'json', label: 'JSON v1' }
        ]"
        :aria-label="t('vendor.import')"
        class="mt-1 min-w-0"
        :disabled="busy || scope.readOnly"
      />
      <p v-if="!embedded && format === 'quick'" class="text-xs text-gray-500">
        {{ t('vendor.quickProxyHelp') }}
      </p>
      <ProxyBatchInput v-if="embedded" v-model="content" :disabled="busy || completed || scope.readOnly" @parsed="parsed" />
      <label v-if="!embedded && format === 'json'"
        >{{ t('vendor.file')
        }}<input
          type="file"
          accept=".json,application/json"
          :disabled="busy || scope.readOnly"
          class="input my-2"
          @change="fileChanged" /></label
      ><textarea
        v-if="!embedded"
        v-model="content"
        :disabled="busy || scope.readOnly"
        class="input h-48 font-mono text-sm"
        :placeholder="
          format === 'quick'
            ? 'socks5://user:pass@proxy.example.com:1080\nhttp://proxy.example.com:8080\nhttps://user:pass@proxy.example.com:443'
            : undefined
        "
        spellcheck="false"
        autocomplete="off"
        :aria-label="t('vendor.import')"
      />
      <div v-if="!embedded" class="flex gap-2">
        <button class="btn btn-secondary" :disabled="busy || scope.readOnly || !content.trim()" @click="submit(true)">
          {{ t('vendor.preview') }}</button
        ><button
          class="btn btn-primary"
          :disabled="busy || scope.readOnly || !digest || !result?.valid_count"
          @click="submit(false)"
        >
          {{ t('vendor.confirm') }}
        </button>
      </div>
      <p v-if="result" class="text-sm">
        {{ t('vendor.validRows') }}: {{ result.valid_count ?? '—' }} ·
        {{ t('vendor.duplicateRows') }}: {{ result.duplicate_count ?? '—' }} ·
        {{ t('vendor.invalidRows') }}: {{ result.invalid_count ?? '—' }}
      </p>
      <button v-if="result && !digest" class="btn btn-secondary" @click="emit('close')">
        {{ t('vendor.done') }}
      </button>
      <div v-if="result" class="max-h-72 overflow-auto">
        <table class="table">
          <thead>
            <tr>
              <th>{{ t('vendor.line') }}</th>
              <th>{{ t('vendor.status') }}</th>
              <th>{{ t('vendor.code') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in rows" :key="i">
              <td class="p-2">{{ row.line ?? row.index ?? i + 1 }}</td>
              <td>
                <StatusBadge
                  :status="
                    ['failed', 'invalid'].includes(row.status)
                      ? 'error'
                      : ['created', 'updated', 'valid', 'ready'].includes(row.status)
                        ? 'success'
                        : 'warning'
                  "
                  :label="rowStatus(row.status)"
                />
              </td>
              <td>{{ row.code && safeCodes.has(row.code) ? row.code : '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div></component
  >
</template>
