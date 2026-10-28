<template>
  <div>
    <MonitorHero
      :overall-status="overallStatus"
      :interval-seconds="DEFAULT_INTERVAL_SECONDS"
      :window="currentWindow"
      :loading="loading"
      :auto-refresh="autoRefresh"
      @update:window="handleWindowChange"
      @refresh="refresh()"
    />

    <MonitorCardGrid
      :items="items"
      :window="currentWindow"
      :countdown-seconds="countdown"
      :loading="loading"
      :detail-cache="detailCache"
      @card-click="openDetail"
    />

    <MonitorDetailDialog
      :show="showDetail"
      :monitor-id="detailTarget?.id ?? null"
      :title="detailTitle"
      @close="closeDetail"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useOperationsRequest } from '@/features/operations/queries/useOperationsRequest'
import { useOperationsServices } from '@/features/operations/services'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import {
  channelMonitorUserAPI,
  type UserMonitorView,
  type UserMonitorDetail,
} from '@/features/operations/api'

import MonitorHero, {
  type MonitorWindow,
  type OverallStatus,
} from '@/features/operations/monitor/user/MonitorHero.vue'
import MonitorCardGrid from '@/features/operations/monitor/user/MonitorCardGrid.vue'
import MonitorDetailDialog from '@/features/operations/monitor/components/MonitorDetailDialog.vue'
import { DEFAULT_INTERVAL_SECONDS, STATUS_OPERATIONAL } from '@/features/operations/monitor/constants'
import { useAutoRefresh } from '@/shared/lib/useAutoRefresh'

const { t } = useI18n()
const services = useOperationsServices()
const requests = useOperationsRequest(services)

// ── State ──
const items = ref<UserMonitorView[]>([])
const loading = ref(false)
const currentWindow = ref<MonitorWindow>('7d')
const detailCache = reactive<Record<number, UserMonitorDetail>>({})
const showDetail = ref(false)
const detailTarget = ref<UserMonitorView | null>(null)

let mounted = false

const autoRefresh = useAutoRefresh({
  storageKey: 'channel-status-auto-refresh',
  intervals: [30, 60, 120] as const,
  defaultInterval: DEFAULT_INTERVAL_SECONDS,
  defaultEnabled: true,
  onRefresh: () => refresh(true),
  shouldPause: () => document.hidden || loading.value || services.scopeKey.value === null || services.publicSettings.value?.channel_monitor_enabled === false,
})
const countdown = autoRefresh.countdown

// ── Computed ──
const overallStatus = computed<OverallStatus>(() => {
  if (items.value.length === 0) return 'operational'
  for (const it of items.value) {
    if (it.primary_status === 'failed' || it.primary_status === 'error') return 'degraded'
    if (it.primary_status !== STATUS_OPERATIONAL) return 'degraded'
  }
  return 'operational'
})

const detailTitle = computed(() => {
  return detailTarget.value?.name || t('channelStatus.detailTitle')
})

// ── Loaders ──
function canRead() { return services.scopeKey.value !== null && services.publicSettings.value?.channel_monitor_enabled !== false }
async function reload(silent = false): Promise<boolean> {
  if (!canRead()) return false
  const request = requests.begin('list')
  if (!silent) loading.value = true
  try {
    const result = await request.wait(channelMonitorUserAPI.list({ signal: request.signal }))
    items.value = result.items || []
    return true
  } catch (error: unknown) {
    if (request.isCurrent()) services.notifyError(extractApiErrorMessage(error, t('channelStatus.loadError')))
    return false
  } finally {
    if (request.isCurrent()) { loading.value = false; autoRefresh.resetCountdown() }
  }
}
async function refresh(silent = false) {
  const scope = services.scopeKey.value
  if (!await reload(silent) || scope !== services.scopeKey.value) return
  if (currentWindow.value !== '7d') await Promise.all(items.value.map(item => loadDetail(item.id, true)))
}
async function loadDetail(id: number, force = false) {
  if (!canRead() || (!force && detailCache[id])) return
  const request = requests.begin('detail:' + id)
  try { detailCache[id] = await request.wait(channelMonitorUserAPI.status(id, { signal: request.signal })) }
  catch (error: unknown) { if (request.isCurrent()) services.notifyError(extractApiErrorMessage(error, t('channelStatus.detailLoadError'))) }
}
async function ensureDetailsForWindow() {
  if (currentWindow.value !== '7d') await Promise.all(items.value.map(item => loadDetail(item.id)))
}
function resetObservations() {
  requests.cancel(); items.value = []; loading.value = false
  for (const key of Object.keys(detailCache)) delete detailCache[Number(key)]
  closeDetail(); autoRefresh.stop()
}
requests.onScopeChange(() => {
  resetObservations()
  if (mounted && canRead()) { void reload(); if (autoRefresh.enabled.value) autoRefresh.start() }
})

// ── Handlers ──
async function handleWindowChange(value: MonitorWindow) {
  currentWindow.value = value
  await ensureDetailsForWindow()
}

function openDetail(row: UserMonitorView) {
  detailTarget.value = row
  showDetail.value = true
}

function closeDetail() {
  showDetail.value = false
  detailTarget.value = null
}

watch(items, () => {
  void ensureDetailsForWindow()
})

watch(() => services.publicSettings.value?.channel_monitor_enabled, () => {
  if (!canRead()) resetObservations()
  else if (mounted) { void reload(); if (autoRefresh.enabled.value) autoRefresh.start() }
})
onMounted(() => {
  mounted = true
  if (canRead()) { void reload(); autoRefresh.setEnabled(autoRefresh.enabled.value) }
})
onBeforeUnmount(() => { mounted = false; requests.cancel(); autoRefresh.stop() })
</script>
