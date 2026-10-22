<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import api from '@/api/client'
import BaseDialog from '@/components/common/BaseDialog.vue'

type UpdateStatus = {
  status: string
  portal_version: string
  backend_version: string
  mode: 'portal' | 'paired'
  message?: string
}
const { t } = useI18n()
const open = ref(false)
const portalVersion = ref('')
const loadedVersion = ref('')
const backendVersion = ref('')
const status = ref<UpdateStatus | null>(null)
const loading = ref(false)
const error = ref('')
const completed = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined
let polls = 0
let disposed = false
const busy = computed(() => loading.value || status.value?.status === 'running')
const mismatch = computed(() => !!portalVersion.value && !!backendVersion.value && portalVersion.value !== backendVersion.value)
const canUpdate = computed(() => !!status.value && mismatch.value && !busy.value && /^\d+\.\d+\.\d+(\.\d+)?$/.test(backendVersion.value))

async function versions() {
  const results = await Promise.allSettled([
    api.get<{ version: string }>('/portal-build.json', { baseURL: '/' }),
    api.get<{ version: string }>('/admin/system/version'),
  ])
  if (disposed) return
  if (results[0].status === 'fulfilled') {
    loadedVersion.value = results[0].value.data.version || ''
    portalVersion.value = loadedVersion.value
  }
  if (results[1].status === 'fulfilled') backendVersion.value = results[1].value.data.version || ''
}
function schedule() {
  clearTimeout(timer)
  if (disposed || !open.value) return
  if (++polls > 160) {
    error.value = t('vendor.portalUpdate.stillRunning')
    return
  }
  timer = setTimeout(() => refresh(true), 3000)
}
async function refresh(poll = false) {
  if (!poll) loading.value = true
  error.value = ''
  try {
    const { data } = await api.get<UpdateStatus>('/portal-updater/status')
    if (disposed) return
    if (!data || !['portal', 'paired'].includes(data.mode)) throw new Error('Unsupported updater')
    status.value = data
    portalVersion.value = data.portal_version || portalVersion.value
    backendVersion.value = data.backend_version
    completed.value = data.status === 'succeeded' && !mismatch.value && portalVersion.value !== loadedVersion.value
    if (data.status === 'running') schedule()
    if (data.status === 'failed') error.value = data.message || t('vendor.portalUpdate.failed')
  } catch {
    if (disposed) return
    if (poll) schedule()
    else {
      status.value = null
      error.value = t('vendor.portalUpdate.setupRequired')
    }
  } finally {
    loading.value = false
  }
}
function show() {
  open.value = true
  polls = 0
  void refresh()
}
function close() {
  open.value = false
  clearTimeout(timer)
}
async function update() {
  if (!canUpdate.value) return
  loading.value = true
  error.value = ''
  try {
    await api.post('/portal-updater/update', { version: backendVersion.value })
    if (disposed) return
    status.value = { ...status.value!, status: 'running' }
    polls = 0
    schedule()
  } catch {
    error.value = t('vendor.portalUpdate.failed')
  } finally {
    loading.value = false
  }
}
function reload() { window.location.reload() }
onMounted(versions)
onUnmounted(() => { disposed = true; clearTimeout(timer) })
</script>

<template>
  <button type="button" class="btn btn-secondary btn-sm" :class="{ 'text-amber-600': mismatch }" @click="show">
    {{ t('vendor.portalUpdate.title') }}<span v-if="portalVersion" class="hidden sm:inline"> · {{ portalVersion }}</span>
  </button>
  <BaseDialog :show="open" :title="t('vendor.portalUpdate.title')" @close="close">
    <div class="space-y-4">
      <dl class="grid grid-cols-2 gap-2 text-sm">
        <dt>{{ t('vendor.portalUpdate.portalVersion') }}</dt><dd>{{ portalVersion || '—' }}</dd>
        <dt>{{ t('vendor.portalUpdate.backendVersion') }}</dt><dd>{{ backendVersion || '—' }}</dd>
      </dl>
      <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('vendor.portalUpdate.explanation') }}</p>
      <p v-if="status?.mode === 'paired'" class="text-sm">{{ t('vendor.portalUpdate.paired') }}</p>
      <p v-if="mismatch" class="text-sm text-amber-600">{{ t('vendor.portalUpdate.mismatch') }}</p>
      <p v-else-if="status" class="text-sm text-green-600">{{ t('vendor.portalUpdate.matched') }}</p>
      <p v-if="status?.status === 'running'" role="status" class="text-sm">{{ t('vendor.portalUpdate.running') }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="refresh()">{{ t('vendor.portalUpdate.check') }}</button>
        <button v-if="completed" type="button" class="btn btn-primary" @click="reload">{{ t('vendor.portalUpdate.reload') }}</button>
        <button v-else type="button" class="btn btn-primary" :disabled="!canUpdate" @click="update">{{ t('vendor.portalUpdate.sync', { version: backendVersion || '—' }) }}</button>
      </div>
    </div>
  </BaseDialog>
</template>
