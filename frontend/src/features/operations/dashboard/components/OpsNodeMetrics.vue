<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import HelpTooltip from '@/shared/ui/HelpTooltip.vue'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import type { OpsJobHeartbeat, OpsNodeMetricsSnapshot } from '@/features/operations/api/ops'
import { isOpsNodeStale } from '../../model/nodeMetrics'
import { getIntlLocale } from '@/i18n/localeUtils'

const props = withDefaults(defineProps<{ nodes: OpsNodeMetricsSnapshot[]; unavailable: boolean; jobHeartbeats?: OpsJobHeartbeat[]; fullscreen?: boolean; expanded?: boolean; snapshotReceivedAt?: number }>(), { jobHeartbeats: () => [], fullscreen: false, expanded: false })
const emit = defineEmits<{ jobsDetails: [] }>()
const { t, locale } = useI18n()
const showAllNodes = ref(false)
const hasMoreNodes = computed(() => !props.expanded && props.nodes.length > 3)
const now = ref(Date.now())
const receivedAt = ref(now.value)
watch(() => props.nodes, () => { receivedAt.value = Date.now(); now.value = receivedAt.value })
const timer = setInterval(() => { now.value = Date.now() }, 1000)
onScopeDispose(() => clearInterval(timer))
const number = (v: unknown): number | null => typeof v === 'number' && Number.isFinite(v) && v >= 0 ? v : null
const label = (v: unknown) => number(v) == null ? '—' : String(v)
const percent = (v: unknown, decimals = 1) => number(v) == null ? '—' : Number(v).toFixed(decimals) + '%'
const memory = (v: unknown) => number(v) == null ? '—' : Number(v) >= 1024 ? (Number(v) / 1024).toFixed(1) + ' GiB' : Number(v).toFixed(0) + ' MiB'
const poolPercent = (total: number | null, capacity: unknown) => total == null || number(capacity) == null || Number(capacity) <= 0 ? null : total / Number(capacity) * 100
const add = (a: unknown, b: unknown) => number(a) == null || number(b) == null ? null : Number(a) + Number(b)
const timeLabel = (value: string) => Number.isNaN(Date.parse(value)) ? '—' : new Date(value).toLocaleString(getIntlLocale(locale.value))
const stale = (node: OpsNodeMetricsSnapshot): boolean => isOpsNodeStale(node, props.snapshotReceivedAt ?? receivedAt.value, now.value)
// Prioritize current observations before truncating; retain the server order within each group.
const orderedNodes = computed(() => [...props.nodes].sort((left, right) => Number(stale(left)) - Number(stale(right))))
const visibleNodes = computed(() => props.expanded ? orderedNodes.value : orderedNodes.value.slice(0, 3))
function tone(value: number | null, warn = 80, critical = 95, failed = false) {
  if (failed || (value != null && value >= critical)) return 'text-rose-600 dark:text-rose-400'
  if (value == null) return 'text-gray-500'
  return value >= warn ? 'text-yellow-600 dark:text-yellow-400' : 'text-emerald-600 dark:text-emerald-400'
}
const jobsWarnCount = computed(() => props.jobHeartbeats.filter(job => job.health_status === 'failed' || job.health_status === 'stale').length)
const jobsStatus = computed(() => !props.jobHeartbeats.length ? 'unknown' : jobsWarnCount.value ? 'warning' : props.jobHeartbeats.some(job => job.health_status === 'healthy') ? 'ok' : 'unknown')
const jobsLabel = computed(() => t(jobsStatus.value === 'ok' ? 'admin.ops.ok' : jobsStatus.value === 'warning' ? 'common.warning' : 'admin.ops.noData'))
const jobsTone = computed(() => jobsStatus.value === 'ok' ? 'text-emerald-600 dark:text-emerald-400' : jobsStatus.value === 'warning' ? 'text-yellow-600 dark:text-yellow-400' : 'text-gray-500')
function cards(node: OpsNodeMetricsSnapshot) {
  const dbTotal = add(node.db_conn_active, node.db_conn_idle)
  const dbPct = poolPercent(dbTotal, node.db_max_open_conns)
  const redisPct = poolPercent(number(node.redis_conn_total), node.redis_pool_size)
  const redisActive = number(node.redis_conn_total) == null || number(node.redis_conn_idle) == null ? null : Math.max(0, Number(node.redis_conn_total) - Number(node.redis_conn_idle))
  const scope = (value: string) => t('admin.ops.nodes.' + (value === 'host' || value === 'container' ? value : 'unknown'))
  const goroutines = number(node.goroutine_count)
  const goroutineLabel = goroutines == null ? t('admin.ops.noData') : t(goroutines >= 15000 ? 'common.critical' : goroutines >= 8000 ? 'common.warning' : 'admin.ops.ok')
  const poolDetail = (total: number | null, capacity: unknown, active: unknown, idle: unknown) => t('admin.ops.conns') + ' ' + label(total) + ' / ' + label(capacity) + ' · ' + t('admin.ops.active') + ' ' + label(active) + ' · ' + t('admin.ops.idle') + ' ' + label(idle)
  return [
    { key: 'cpu', title: 'CPU', help: t('admin.ops.tooltips.cpu') + ' ' + scope(node.cpu_scope) + ' · ' + t('admin.ops.nodes.cpuCapacity', { count: label(node.cpu_capacity) }), value: percent(node.cpu_usage_percent), detail: t('common.warning') + ' 80% · ' + t('common.critical') + ' 95%', tone: tone(number(node.cpu_usage_percent)) },
    { key: 'memory', title: t('admin.ops.memory'), help: t('admin.ops.tooltips.memory') + ' ' + scope(node.memory_scope), value: percent(node.memory_usage_percent), detail: memory(node.memory_used_mb) + ' / ' + memory(node.memory_total_mb), tone: tone(number(node.memory_usage_percent)) },
    { key: 'database', title: t('admin.ops.db'), help: t('admin.ops.nodes.databasePool') + ' · ' + t('admin.ops.tooltips.db'), value: node.db_ok === false ? t('admin.ops.nodes.failed') : percent(dbPct, 0), detail: poolDetail(dbTotal, node.db_max_open_conns, node.db_conn_active, node.db_conn_idle), tone: tone(dbPct, 70, 90, node.db_ok === false) },
    { key: 'redis', title: 'Redis', help: t('admin.ops.nodes.redisPool') + ' · ' + t('admin.ops.tooltips.redis'), value: node.redis_ok === false ? t('admin.ops.nodes.failed') : percent(redisPct, 0), detail: poolDetail(number(node.redis_conn_total), node.redis_pool_size, redisActive, node.redis_conn_idle), tone: tone(redisPct, 70, 90, node.redis_ok === false) },
    { key: 'goroutines', title: t('admin.ops.goroutines'), help: t('admin.ops.tooltips.goroutines'), value: goroutineLabel, detail: t('admin.ops.current') + ' ' + label(goroutines) + ' · ' + t('common.warning') + ' 8000 · ' + t('common.critical') + ' 15000' + (number(node.concurrency_queue_depth) == null ? '' : ' · ' + t('admin.ops.queue') + ' ' + label(node.concurrency_queue_depth)), tone: tone(goroutines, 8000, 15000) }
  ]
}
</script>

<template>
  <section data-testid="ops-node-metrics" class="space-y-3">
    <p v-if="unavailable" role="status" class="text-sm text-amber-600" data-testid="node-metrics-unavailable">{{ t('admin.ops.nodes.unavailable') }}</p>
    <p v-if="!nodes.length" class="text-sm text-gray-500">{{ t('admin.ops.nodes.waiting') }}</p>
    <article v-for="(node, index) in visibleNodes" :key="node.node_id" :data-testid="'node-' + node.node_id" :class="index ? 'border-t border-gray-100 pt-3 dark:border-dark-700' : ''">
      <div class="mb-2 flex flex-wrap items-center justify-between gap-2 text-[10px] text-gray-500">
        <div class="flex min-w-0 items-center gap-2">
          <span class="font-semibold text-gray-700 dark:text-gray-300">{{ node.node_name || node.node_id }}</span>
          <span :class="stale(node) ? 'text-amber-600' : 'text-emerald-600'" data-testid="node-status">{{ t(stale(node) ? 'admin.ops.nodes.stale' : 'admin.ops.nodes.online') }}</span>
        </div>
        <span>{{ t('admin.ops.nodes.sampledAt') }} {{ timeLabel(node.created_at) }}</span>
      </div>
      <p v-if="stale(node)" class="mb-2 text-[10px] text-amber-600">{{ t('admin.ops.nodes.lastKnown') }}</p>
      <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-6" data-testid="node-metric-row">
        <div v-for="card in cards(node)" :key="card.key" :data-testid="'metric-' + card.key" class="min-w-0 rounded-xl bg-gray-50 p-3 dark:bg-dark-900" :class="{ 'opacity-60': stale(node) }">
          <div class="flex items-center gap-1">
            <div class="text-[10px] font-bold uppercase tracking-wider text-gray-400">{{ card.title }}</div>
            <HelpTooltip v-if="!fullscreen" :content="card.help" />
          </div>
          <div class="mt-1 text-lg font-black" :class="card.tone">{{ card.value }}</div>
          <div v-if="!fullscreen" class="mt-1 text-[10px] text-gray-500 dark:text-gray-400">{{ card.detail }}</div>
        </div>
        <div data-testid="metric-jobs" class="min-w-0 rounded-xl bg-gray-50 p-3 dark:bg-dark-900">
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-1">
              <div class="text-[10px] font-bold uppercase tracking-wider text-gray-400">{{ t('admin.ops.jobs') }}</div>
              <HelpTooltip v-if="!fullscreen" :content="t('admin.ops.nodes.clusterJobs')" />
            </div>
            <button v-if="!fullscreen" class="text-[10px] font-bold text-blue-500 hover:underline" type="button" @click="emit('jobsDetails')">{{ t('admin.ops.requestDetails.details') }}</button>
          </div>
          <div class="mt-1 text-lg font-black" :class="jobsTone">{{ jobsLabel }}</div>
          <div v-if="!fullscreen" class="mt-1 text-[10px] text-gray-500 dark:text-gray-400">{{ t('admin.ops.nodes.cluster') }} · {{ t('common.total') }} {{ jobHeartbeats.length }} · {{ t('common.warning') }} {{ jobsWarnCount }}</div>
        </div>
      </div>
    </article>
    <div v-if="hasMoreNodes" class="flex items-center justify-between border-t border-gray-100 pt-3 text-xs dark:border-dark-700" data-testid="node-overflow">
      <span class="text-gray-500">{{ t('admin.ops.nodes.showingFirst', { shown: visibleNodes.length, total: nodes.length }) }}</span>
      <button class="font-semibold text-primary-600 hover:text-primary-700 dark:text-primary-400" type="button" data-testid="view-all-nodes" @click="showAllNodes = true">{{ t('admin.ops.nodes.viewAll', { count: nodes.length }) }}</button>
    </div>
    <BaseDialog v-if="!expanded" :show="showAllNodes" :title="t('admin.ops.nodes.allServers', { count: nodes.length })" width="full" @close="showAllNodes = false">
      <OpsNodeMetrics v-if="showAllNodes" :nodes="nodes" :unavailable="unavailable" :job-heartbeats="jobHeartbeats" :fullscreen="fullscreen" :snapshot-received-at="snapshotReceivedAt ?? receivedAt" expanded @jobs-details="emit('jobsDetails')" />
    </BaseDialog>
  </section>
</template>
