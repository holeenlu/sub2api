<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useNow } from '@vueuse/core'
import Select from '@/components/common/Select.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import { platformDisplayName } from '@/constants/platformCatalog'
import { platformBadgeLightClass } from '@/utils/platformColors'
import { vendorAPI, resourceAPI } from '../api'
import { errorText, proxyAvailable, readImport } from '../helpers'
import type {
  Vendor,
  VendorProxy,
  ImportResult,
  ImportRow,
  ResourceScope,
} from '../types'
const props = withDefaults(
  defineProps<{ vendor: Vendor; scope?: ResourceScope }>(),
  { scope: () => ({ admin: false }) },
)
const emit = defineEmits<{ close: []; saved: [] }>()
const { t, te } = useI18n()
const content = ref(''),
  mode = ref('skip'),
  groups = ref<number[]>([]),
  proxy = ref('')
const proxies = ref<VendorProxy[]>([]),
  proxyLoading = ref(false)
const busy = ref(false),
  error = ref(''),
  result = ref<ImportResult>(),
  previewed = ref(false)
const now = useNow({ interval: 1000 })
interface RowMapping {
  index: number
  name: string
  platform: string
  custom: boolean
  group_ids: number[]
  proxy: string
  mode: string
  bundledProxy: boolean
}
const mappings = ref<RowMapping[]>([])
let generation = 0,
  proxyGeneration = 0
const input = computed(() => {
  if (!content.value.trim()) return { rows: [], problem: '' }
  try {
    const data = JSON.parse(content.value)
    if (
      !data ||
      (data.type !== undefined &&
        data.type !== '' &&
        !['sub2api-data', 'sub2api-bundle'].includes(data.type)) ||
      (data.version !== undefined && data.version !== 0 && data.version !== 1) ||
      !Array.isArray(data.accounts)
    )
      return { rows: [], problem: 'importFormatInvalid' }
    // Match the existing account import endpoint's bounded batch size.
    if (!data.accounts.length || data.accounts.length > 25)
      return { rows: [], problem: 'importRowsLimit' }
    return {
      rows: data.accounts.map((row: unknown, index: number) => {
        const account =
          row && typeof row === 'object' ? (row as Record<string, unknown>) : {}
        // Only these safe labels enter the mapping UI; credentials stay in the input.
        return {
          index,
          name: typeof account.name === 'string' ? account.name : '',
          platform:
            typeof account.platform === 'string' ? account.platform : '',
          bundledProxy:
            typeof account.proxy_key === 'string' && !!account.proxy_key,
        }
      }) as Pick<RowMapping, 'index' | 'name' | 'platform' | 'bundledProxy'>[],
      problem: '',
    }
  } catch {
    return { rows: [], problem: 'invalidJSON' }
  }
})
watch(
  content,
  () => {
    mappings.value = input.value.rows.map((row) => ({
      ...row,
      custom: false,
      group_ids: [],
      proxy: '',
      mode: 'skip',
    }))
    if (!proxy.value || proxy.value === 'import')
      proxy.value = input.value.rows.some((row) => row.bundledProxy) ? 'import' : ''
  },
  { flush: 'sync' },
)
const rows = computed(() => result.value?.items || result.value?.rows || [])
const modeOptions = computed(() => [
  { value: 'skip', label: t('vendor.skip') },
  { value: 'update', label: t('vendor.update') },
])
function proxyLabel(id: number) {
  const owned = proxies.value.find((p) => p.id === id)
  return owned
    ? owned.name +
        ' · #' +
        owned.id +
        ' · ' +
        owned.protocol.toUpperCase() +
        ' · ' +
        (owned.endpoint ||
          owned.protocol + '://' + owned.host + ':' + owned.port)
    : '#' + id
}
const proxyOptions = computed(() => [
  { value: '', label: t('vendor.select'), disabled: true },
  { value: 'import', label: t('vendor.importBundledProxy') },
  ...(props.vendor.allow_direct
    ? [{ value: 'direct', label: t('vendor.direct') }]
    : []),
  ...proxies.value
    .filter((p) => proxyAvailable(p, now.value.getTime()))
    .map((p) => ({ value: String(p.id), label: proxyLabel(p.id) })),
])
function permittedGroups(platform: string) {
  return (props.vendor.groups || []).filter(
    (group) => !group.platform || group.platform === platform,
  )
}
function effectiveMapping(row: RowMapping) {
  const permitted = new Set(
    permittedGroups(row.platform).map((group) => group.id),
  )
  return {
    group_ids: (row.custom ? row.group_ids : groups.value).filter((id) =>
      permitted.has(id),
    ),
    proxy:
      (row.custom ? row.proxy : proxy.value) || (row.bundledProxy ? 'import' : ''),
    mode: row.custom ? row.mode : mode.value,
  }
}
function customize(row: RowMapping, checked: boolean) {
  if (checked) {
    const defaults = effectiveMapping(row)
    row.group_ids = [...defaults.group_ids]
    row.proxy = defaults.proxy
    row.mode = defaults.mode
  }
  row.custom = checked
}
function mappingValid(row: RowMapping) {
  // Unsupported rows can still receive the server's safe per-row failure alongside valid rows.
  if (!['anthropic', 'openai'].includes(row.platform)) return true
  const mapping = effectiveMapping(row)
  if (!mapping.group_ids.length) return false
  if (mapping.proxy === 'import') return row.bundledProxy
  if (mapping.proxy === 'direct') return props.vendor.allow_direct
  return proxies.value.some(
    (p) =>
      p.id === Number(mapping.proxy) && proxyAvailable(p, now.value.getTime()),
  )
}
const mappingReady = computed(() => mappings.value.every(mappingValid))
const canConfirm = computed(
  () =>
    !!result.value?.preview_id &&
    previewed.value &&
    (!result.value.expires_at ||
      Date.parse(result.value.expires_at) > now.value.getTime()) &&
    rows.value.some((row) =>
      ['create', 'update', 'ready', 'valid'].includes(row.status),
    ),
)
function groupLabels(ids?: number[]) {
  return ids?.length
    ? ids
        .map(
          (id) =>
            (props.vendor.groups.find((group) => group.id === id)?.name ||
              t('vendor.unknown')) +
            ' · #' +
            id,
        )
        .join(', ')
    : t('vendor.noGroups')
}
function egressLabel(value: string) {
  if (value === 'import') return t('vendor.importBundledProxy')
  return value === 'direct'
    ? t('vendor.direct')
    : value
      ? proxyLabel(Number(value))
      : t('vendor.importMappingRequired')
}
function finalEgress(row: ImportRow) {
  if (row.proxy) return [row.proxy.name, row.proxy.endpoint].filter(Boolean).join(' · ')
  return row.direct === true
    ? t('vendor.direct')
    : row.proxy_id
      ? proxyLabel(row.proxy_id)
      : t('vendor.unknown')
}
function rowStatus(status: string) {
  const key =
    (
      {
        valid: 'validRows',
        ready: 'validRows',
        create: 'create',
        update: 'update',
        created: 'success',
        updated: 'success',
        skipped: 'skip',
        duplicate: 'duplicateRows',
        invalid: 'invalidRows',
        unresolved: 'unknown',
      } as Record<string, string>
    )[status] || status
  return te('vendor.' + key) ? t('vendor.' + key) : t('vendor.unknown')
}
watch(
  [content, mode, groups, proxy, mappings],
  () => {
    generation++
    error.value = ''
    result.value = undefined
    previewed.value = false
  },
  { deep: true, flush: 'sync' },
)
async function fileChanged(event: Event) {
  const current = ++generation
  try {
    const file = (event.target as HTMLInputElement).files?.[0]
    if (file) {
      const text = await readImport(file, t('vendor.importSize'))
      if (current === generation) content.value = text
    }
  } catch (e) {
    if (current === generation)
      error.value =
        e instanceof SyntaxError ? t('vendor.invalidJSON') : errorText(e)
  }
}
async function submit(preview: boolean) {
  if (busy.value) return
  if (preview && input.value.problem) {
    error.value = t('vendor.' + input.value.problem)
    return
  }
  if (
    preview ? !mappings.value.length || !mappingReady.value : !canConfirm.value
  )
    return
  busy.value = true
  error.value = ''
  const current = generation
  try {
    const body = preview
      ? {
          data: JSON.parse(content.value),
          mode: mode.value,
          group_ids: [...groups.value],
          proxy_id:
            proxy.value && !['direct', 'import'].includes(proxy.value)
              ? Number(proxy.value)
              : null,
          direct: proxy.value === 'direct',
          mappings: mappings.value.map((row) => {
            const mapping = effectiveMapping(row)
            return {
              index: row.index,
              group_ids: mapping.group_ids,
              proxy_id:
                mapping.proxy && !['direct', 'import'].includes(mapping.proxy)
                  ? Number(mapping.proxy)
                  : null,
              direct: mapping.proxy === 'direct',
              mode: mapping.mode,
            }
          }),
        }
      : { preview_id: result.value?.preview_id }
    const response = await vendorAPI.importAccounts(body, preview, props.scope)
    if (current !== generation) return
    if (!preview) content.value = ''
    result.value = response
    previewed.value = preview
    if (!preview) emit('saved')
  } catch (e) {
    if (current === generation)
      error.value =
        e instanceof SyntaxError ? t('vendor.invalidJSON') : errorText(e)
  } finally {
    busy.value = false
  }
}
async function loadProxies() {
  const current = ++proxyGeneration,
    api = resourceAPI(props.scope)
  proxyLoading.value = true
  try {
    const available: VendorProxy[] = []
    let page = 1,
      total = 0
    do {
      const list = await api.proxies({ page, page_size: 50 })
      if (current !== proxyGeneration) return
      available.push(...list.items)
      total = list.total
      page++
      if (!list.items.length) break
    } while (available.length < total && page <= 200)
    proxies.value = available
  } catch (e) {
    if (current === proxyGeneration) error.value = errorText(e)
  } finally {
    if (current === proxyGeneration) proxyLoading.value = false
  }
}
watch(
  () => [props.scope.admin, props.scope.vendorId, props.vendor.id],
  () => {
    generation++
    content.value = ''
    groups.value = []
    proxy.value = ''
    mode.value = 'skip'
    proxies.value = []
    result.value = undefined
    previewed.value = false
    void loadProxies()
  },
  { immediate: true },
)
onUnmounted(() => {
  proxyGeneration++
  generation++
  content.value = ''
})
</script>
<template>
  <BaseDialog
    show
    :title="t('vendor.import')"
    width="wide"
    :close-on-escape="!busy"
    :show-close-button="!busy"
    @close="emit('close')"
  >
    <div class="space-y-4">
      <p class="text-sm text-gray-500">{{ t('vendor.importHelp') }}</p>
      <p class="text-xs text-gray-500">{{ t('vendor.importPrivacy') }}</p>
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      <label class="block">
        {{ t('vendor.file') }}
        <input
          type="file"
          accept=".json,application/json"
          :disabled="busy"
          class="input my-2"
          @change="fileChanged"
        />
      </label>
      <textarea
        v-model="content"
        :disabled="busy"
        :aria-label="t('vendor.import')"
        spellcheck="false"
        autocomplete="off"
        class="input h-28 font-mono"
      />
      <fieldset
        :disabled="busy"
        class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700"
        data-import-defaults
      >
        <legend class="px-1 text-sm font-semibold">
          {{ t('vendor.importDefaults') }}
        </legend>
        <p class="text-xs text-gray-500">
          {{ t('vendor.importDefaultsHelp') }}
        </p>
        <GroupSelector v-model="groups" :groups="vendor.groups" />
        <label class="block">
          {{ t('vendor.proxy') }}
          <Select
            v-model="proxy"
            :options="proxyOptions"
            :aria-label="t('vendor.proxy')"
            :disabled="busy || proxyLoading"
            :loading="proxyLoading"
            class="mt-1"
          />
        </label>
        <label class="block">
          {{ t('vendor.importDuplicateMode') }}
          <Select
            v-model="mode"
            :options="modeOptions"
            :aria-label="t('vendor.importDuplicateMode')"
            :disabled="busy"
            class="mt-1"
          />
        </label>
      </fieldset>
      <section v-if="mappings.length" class="space-y-3">
        <h3 class="font-semibold">{{ t('vendor.importRowMappings') }}</h3>
        <div
          v-for="row in mappings"
          :key="row.index"
          :data-import-row="row.index"
          class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <p class="font-medium">
              {{ t('vendor.line') }} {{ row.index + 1 }} ·
              {{ row.name || t('vendor.unknown') }}
            </p>
            <span
              class="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium"
              :class="platformBadgeLightClass(row.platform)"
            >
              <PlatformIcon :platform="row.platform" size="xs" />{{
                platformDisplayName(row.platform) || t('vendor.unknown')
              }}
            </span>
          </div>
          <label class="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              :checked="row.custom"
              :disabled="busy"
              :aria-label="
                t('vendor.importCustomizeRow', { line: row.index + 1 })
              "
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              @change="
                customize(row, ($event.target as HTMLInputElement).checked)
              "
            />
            {{ t('vendor.importCustomizeRow', { line: row.index + 1 }) }}
          </label>
          <fieldset v-if="row.custom" :disabled="busy" class="space-y-3">
            <GroupSelector
              v-model="row.group_ids"
              :groups="permittedGroups(row.platform)"
            />
            <Select
              v-model="row.proxy"
              :options="proxyOptions"
              :aria-label="t('vendor.importRowProxy', { line: row.index + 1 })"
              :disabled="busy || proxyLoading"
            />
            <Select
              v-model="row.mode"
              :options="modeOptions"
              :aria-label="t('vendor.importRowMode', { line: row.index + 1 })"
              :disabled="busy"
            />
          </fieldset>
          <p v-else class="text-xs text-gray-500">
            {{ t('vendor.importUseDefaults') }}
          </p>
          <dl class="grid gap-1 text-sm" data-import-effective>
            <div>
              <dt class="inline text-gray-500">{{ t('vendor.groups') }}:</dt>
              <dd class="inline">
                {{ groupLabels(effectiveMapping(row).group_ids) }}
              </dd>
            </div>
            <div>
              <dt class="inline text-gray-500">{{ t('vendor.proxy') }}:</dt>
              <dd class="inline break-all">
                {{ egressLabel(effectiveMapping(row).proxy) }}
              </dd>
            </div>
            <div>
              <dt class="inline text-gray-500">
                {{ t('vendor.importDuplicateMode') }}:
              </dt>
              <dd class="inline">
                {{
                  t(
                    effectiveMapping(row).mode === 'update'
                      ? 'vendor.update'
                      : 'vendor.skip',
                  )
                }}
              </dd>
            </div>
          </dl>
          <p v-if="!mappingValid(row)" class="text-sm text-amber-700">
            {{ t('vendor.importMappingRequired') }}
          </p>
        </div>
      </section>
      <div class="flex gap-2">
        <button
          class="btn btn-secondary"
          :disabled="
            busy ||
            proxyLoading ||
            !content.trim() ||
            (!input.problem && !mappingReady)
          "
          @click="submit(true)"
        >
          {{ t('vendor.preview') }}
        </button>
        <button
          class="btn btn-primary"
          :disabled="busy || !canConfirm"
          @click="submit(false)"
        >
          {{ t('vendor.confirm') }}
        </button>
      </div>
      <button
        v-if="result && !previewed"
        class="btn btn-secondary"
        @click="emit('close')"
      >
        {{ t('vendor.done') }}
      </button>
      <section v-if="rows.length" class="space-y-2" data-import-result>
        <h3 class="font-semibold">{{ t('vendor.importFinalMappings') }}</h3>
        <div class="table-container">
          <table class="table">
            <thead>
              <tr>
                <th>{{ t('vendor.line') }}</th>
                <th>{{ t('vendor.account') }}</th>
                <th>{{ t('vendor.groups') }}</th>
                <th>{{ t('vendor.proxy') }}</th>
                <th>{{ t('vendor.status') }}</th>
                <th>{{ t('vendor.results') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in rows" :key="row.index ?? i">
                <td>
                  {{ row.index != null ? row.index + 1 : (row.line ?? i + 1) }}
                </td>
                <td>
                  <p>
                    {{ row.name || t('vendor.unknown')
                    }}<span
                      v-if="row.account_id"
                      class="ml-1 text-xs text-gray-500"
                      >#{{ row.account_id }}</span
                    >
                  </p>
                  <p class="text-xs text-gray-500">
                    {{
                      platformDisplayName(row.platform) || t('vendor.unknown')
                    }}
                  </p>
                </td>
                <td>
                  {{
                    row.group_ids
                      ? groupLabels(row.group_ids)
                      : t('vendor.unknown')
                  }}
                </td>
                <td class="max-w-xs break-all">{{ finalEgress(row) }}</td>
                <td>
                  <StatusBadge
                    :status="
                      ['failed', 'invalid'].includes(row.status)
                        ? 'error'
                        : [
                              'created',
                              'updated',
                              'valid',
                              'ready',
                              'create',
                            ].includes(row.status)
                          ? 'success'
                          : 'warning'
                    "
                    :label="rowStatus(row.status)"
                  />
                </td>
                <td>{{ row.code || row.message || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </BaseDialog>
</template>
