<template>
  <div
    v-if="uniqueIps.length > 0"
    class="flex flex-shrink-0 items-center justify-end gap-2 border-b border-gray-200 px-4 py-2 dark:border-dark-700"
  >
    <span v-if="pendingCount > 0" class="text-xs text-gray-500 dark:text-gray-400">
      {{ t('usage.ipGeo.pending', { count: pendingCount }) }}
    </span>
    <button
      type="button"
      class="inline-flex items-center gap-1 rounded px-2 py-1 text-xs font-medium text-primary-600 transition-colors hover:bg-primary-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-400 dark:hover:bg-primary-900/30"
      :disabled="loading || pendingCount === 0"
      @click="run"
    >
      {{ loading ? t('usage.ipGeo.batchFetching') : t('usage.ipGeo.batchFetch') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useIpGeoLookup } from '@/shared/lib/ipGeoLookup'

// 当前页 IP 批量地理查询工具条:传入原始 IP 列表(可含空值),内部去重;
// 无 IP 时自身不渲染。批量失败 emit failed,由使用方弹提示。
const props = defineProps<{
  ips: Array<string | null | undefined>
}>()

const emit = defineEmits<{
  (e: 'failed'): void
}>()

const { t } = useI18n()
const lookup = useIpGeoLookup()

const uniqueIps = computed(() =>
  Array.from(new Set(props.ips.filter((ip): ip is string => Boolean(ip))))
)

const pendingCount = computed(() =>
  uniqueIps.value.filter((ip) => {
    const status = lookup.getEntry(ip).status
    return status === 'idle' || status === 'error'
  }).length
)

const loading = ref(false)
let controller: AbortController | undefined
let sequence = 0
let alive = true
function cancel() {
  sequence++
  controller?.abort()
  controller = undefined
  loading.value = false
}
watch([lookup.scope, () => uniqueIps.value.join('\n')], cancel, { flush: 'sync' })
onScopeDispose(() => { alive = false; cancel() })

const run = async () => {
  if (loading.value || pendingCount.value === 0 || lookup.scope.value === null) return
  const scope = lookup.scope.value
  const request = ++sequence
  controller = new AbortController()
  const signal = controller.signal
  const current = () => alive && request === sequence && scope === lookup.scope.value && !signal.aborted
  loading.value = true
  try {
    const ok = await lookup.fetchBatch(uniqueIps.value, { signal })
    if (current() && !ok) emit('failed')
  } catch {
    if (current()) emit('failed')
  } finally {
    if (current()) { loading.value = false; controller = undefined }
  }
}
</script>
