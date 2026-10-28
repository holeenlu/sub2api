<template>
  <div v-if="isSuperAdmin" ref="container" class="relative" @keydown.esc="detailsOpen = false">
    <button
      type="button"
      class="inline-flex items-center gap-1 rounded-lg text-xs text-gray-600 hover:bg-gray-100 dark:text-dark-300 dark:hover:bg-dark-800"
      :class="compact || collapsed ? 'p-1' : 'px-2 py-1'"
      :title="t('version.details')"
      :aria-label="t('version.details')"
      :aria-expanded="detailsOpen"
      @click="detailsOpen = !detailsOpen"
    >
      <Icon v-if="collapsed" name="infoCircle" size="sm" />
      <span :class="{ 'sr-only': collapsed }">{{ displayedVersion }}</span>
    </button>
    <div
      v-if="detailsOpen"
      class="absolute left-0 z-50 mt-2 w-72 space-y-3 rounded-xl border border-gray-200 bg-white p-4 text-xs shadow-lg dark:border-dark-700 dark:bg-dark-800"
      :aria-busy="source.versionLoading"
    >
      <dl class="space-y-2">
        <div>
          <dt class="text-gray-500 dark:text-dark-400">{{ currentLabel }}</dt>
          <dd data-testid="current-version" class="break-all font-mono">{{ displayedVersion }}</dd>
        </div>
        <div v-if="portal">
          <dt class="text-gray-500 dark:text-dark-400">{{ t('vendor.portalVersion.backendVersion') }}</dt>
          <dd data-testid="backend-version" class="break-all font-mono">{{ formatVersion(backendVersion) }}</dd>
        </div>
        <div v-if="upstreamVersion">
          <dt class="text-gray-500 dark:text-dark-400">{{ t('version.upstreamVersion') }}</dt>
          <dd data-testid="upstream-version" class="break-all font-mono">{{ formatVersion(upstreamVersion) }}</dd>
        </div>
        <div v-if="buildCommit">
          <dt class="text-gray-500 dark:text-dark-400">{{ t('version.buildCommit') }}</dt>
          <dd data-testid="build-commit" :title="buildCommit" class="break-all font-mono">{{ buildCommit.slice(0, 9) }}</dd>
        </div>
        <div v-if="backendBuildCommit">
          <dt class="text-gray-500 dark:text-dark-400">{{ t('vendor.portalVersion.backendCommit') }}</dt>
          <dd data-testid="backend-build-commit" :title="backendBuildCommit" class="break-all font-mono">{{ backendBuildCommit.slice(0, 9) }}</dd>
        </div>
      </dl>
      <p v-if="readFailed" role="status" class="text-amber-700 dark:text-amber-300">{{ t('version.unavailable') }}</p>
      <div class="flex items-center gap-3">
        <button type="button" class="text-primary-600 disabled:opacity-50 dark:text-primary-300" :disabled="source.versionLoading" @click="refreshVersion(true)">
          {{ source.versionLoading ? t('common.loading') : t('version.refresh') }}
        </button>
        <button type="button" class="text-primary-600 disabled:opacity-50 dark:text-primary-300" :disabled="!copyText" @click="copyVersion">
          {{ copied ? t('common.copied') : t('version.copyDetails') }}
        </button>
      </div>
    </div>
  </div>
  <span v-else-if="publicVersion" class="text-xs text-gray-500 dark:text-dark-400" :class="{ 'sr-only': collapsed }">
    {{ formatVersion(publicVersion) }}
  </span>
</template>

<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { onClickOutside } from '@vueuse/core'
import Icon from '@/shared/ui/icons/Icon.vue'
import { useAuthStore } from "@/features/identity"
import { useAppStore } from "@/app/model/app"
import { useClipboard } from '@/shared/lib/useClipboard'
import { usePortalVersion } from '@/app/compositions/vendor-portal'

const props = defineProps<{ version?: string; portal?: boolean; compact?: boolean; collapsed?: boolean }>()
const { t } = useI18n()
const auth = useAuthStore()
const portalState = props.portal ? usePortalVersion() : null
const source = portalState ?? useAppStore()
const { copied, copyToClipboard } = useClipboard()
const container = ref<HTMLElement | null>(null)
const detailsOpen = ref(false)
const readFailed = ref(false)
const isSuperAdmin = computed(() => auth.isSuperAdmin)
const publicVersion = computed(() => knownVersion(props.version))
const currentVersion = computed(() => knownVersion(source.currentVersion) || (props.portal ? '' : publicVersion.value))
const displayedVersion = computed(() => formatVersion(currentVersion.value))
const backendVersion = computed(() => knownVersion(portalState?.backendVersion))
const upstreamVersion = computed(() => knownVersion(source.upstreamVersion))
const buildCommit = computed(() => knownCommit(source.buildCommit))
const backendBuildCommit = computed(() => knownCommit(portalState?.backendBuildCommit))
const currentLabel = computed(() => t(props.portal ? 'vendor.portalVersion.portalVersion' : 'version.currentVersion'))
const copyText = computed(() => [
  currentVersion.value && currentLabel.value + ': ' + formatVersion(currentVersion.value),
  backendVersion.value && t('vendor.portalVersion.backendVersion') + ': ' + formatVersion(backendVersion.value),
  upstreamVersion.value && t('version.upstreamVersion') + ': ' + formatVersion(upstreamVersion.value),
  buildCommit.value && t('version.buildCommit') + ': ' + buildCommit.value,
  backendBuildCommit.value && t('vendor.portalVersion.backendCommit') + ': ' + backendBuildCommit.value
].filter(Boolean).join('\n'))
let requestVersion = 0
let disposed = false

function knownVersion(value: string | undefined): string {
  const normalized = value?.trim() ?? ''
  return normalized.toLowerCase() === 'unknown' ? '' : normalized
}

function knownCommit(value: string | undefined): string {
  const normalized = value?.trim() ?? ''
  return /^[a-f0-9]{7,40}$/i.test(normalized) ? normalized.toLowerCase() : ''
}

function formatVersion(value: string): string {
  return value ? (/^\d/.test(value) ? 'v' + value : value) : '—'
}

async function refreshVersion(force = false): Promise<void> {
  if (!isSuperAdmin.value || source.versionLoading) return
  const version = ++requestVersion
  try {
    const result = await source.fetchVersion(force)
    if (!disposed && version === requestVersion && isSuperAdmin.value) readFailed.value = !result
  } catch {
    if (!disposed && version === requestVersion && isSuperAdmin.value) readFailed.value = true
  }
}

async function copyVersion(): Promise<void> {
  if (isSuperAdmin.value && copyText.value) await copyToClipboard(copyText.value)
}

onClickOutside(container, () => { detailsOpen.value = false })
watch(isSuperAdmin, (allowed) => {
  if (allowed) void refreshVersion()
  else {
    requestVersion++
    detailsOpen.value = false
    readFailed.value = false
    portalState?.clearVersionCache()
  }
}, { immediate: true })
onScopeDispose(() => { disposed = true })
</script>
