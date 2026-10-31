<script setup lang="ts">
import { ref, computed, h, watch, onScopeDispose, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { useKeysServices } from '../services'
import { useKeyGuideCatalog } from '../queries/useKeyGuideCatalog'
import { useKeyGuideFiles } from '../model/guide/useKeyGuideFiles'
import type { KeyGuideProps, FileConfig, CodexAuthMode } from '../model/guide/types'
const props = defineProps<KeyGuideProps>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const services = useKeysServices()
const clipboardCopy = services.copyText
const copiedIndex = ref<number | null>(null)
const activeTab = ref('unix')
const activeClientTab = ref('claude')
const codexAuthMode = ref<CodexAuthMode>('legacy')
let copyTimer: ReturnType<typeof setTimeout> | undefined
let alive = true
interface TabConfig { id: string; label: string; icon: Component }
// Reset tabs when platform changes
const defaultClientTab = computed(() => {
  if (props.claudeCodeOnly) return 'claude'
  switch (props.platform) {
    case 'openai':
      return 'codex'
    case 'grok':
      return 'grok'
    case 'gemini':
      return 'gemini'
    case 'antigravity':
      return 'claude'
    case 'typesafe':
      return 'systemone'
    default:
      return 'claude'
  }
})

watch(() => [props.platform, props.claudeCodeOnly], () => {
  activeTab.value = 'unix'
  activeClientTab.value = defaultClientTab.value
  codexAuthMode.value = 'legacy'
}, { immediate: true })

watch(() => props.show, (show) => {
  if (show) {
    codexAuthMode.value = 'legacy'
  }
})

// Reset shell tab when client changes
watch(activeClientTab, () => {
  activeTab.value = 'unix'
})

// Icon components
const AppleIcon = {
  render() {
    return h('svg', {
      fill: 'currentColor',
      viewBox: '0 0 24 24',
      class: 'w-4 h-4'
    }, [
      h('path', { d: 'M18.71 19.5c-.83 1.24-1.71 2.45-3.05 2.47-1.34.03-1.77-.79-3.29-.79-1.53 0-2 .77-3.27.82-1.31.05-2.3-1.32-3.14-2.53C4.25 17 2.94 12.45 4.7 9.39c.87-1.52 2.43-2.48 4.12-2.51 1.28-.02 2.5.87 3.29.87.78 0 2.26-1.07 3.81-.91.65.03 2.47.26 3.64 1.98-.09.06-2.17 1.28-2.15 3.81.03 3.02 2.65 4.03 2.68 4.04-.03.07-.42 1.44-1.38 2.83M13 3.5c.73-.83 1.94-1.46 2.94-1.5.13 1.17-.34 2.35-1.04 3.19-.69.85-1.83 1.51-2.95 1.42-.15-1.15.41-2.35 1.05-3.11z' })
    ])
  }
}

const WindowsIcon = {
  render() {
    return h('svg', {
      fill: 'currentColor',
      viewBox: '0 0 24 24',
      class: 'w-4 h-4'
    }, [
      h('path', { d: 'M3 12V6.75l6-1.32v6.48L3 12zm17-9v8.75l-10 .15V5.21L20 3zM3 13l6 .09v6.81l-6-1.15V13zm7 .25l10 .15V21l-10-1.91v-5.84z' })
    ])
  }
}

// Terminal icon for Claude Code
const TerminalIcon = {
  render() {
    return h('svg', {
      fill: 'none',
      stroke: 'currentColor',
      viewBox: '0 0 24 24',
      'stroke-width': '1.5',
      class: 'w-4 h-4'
    }, [
      h('path', {
        'stroke-linecap': 'round',
        'stroke-linejoin': 'round',
        d: 'm6.75 7.5 3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0 0 21 17.25V6.75A2.25 2.25 0 0 0 18.75 4.5H5.25A2.25 2.25 0 0 0 3 6.75v10.5A2.25 2.25 0 0 0 5.25 20.25Z'
      })
    ])
  }
}

// Sparkle icon for Gemini
const SparkleIcon = {
  render() {
    return h('svg', {
      fill: 'none',
      stroke: 'currentColor',
      viewBox: '0 0 24 24',
      'stroke-width': '1.5',
      class: 'w-4 h-4'
    }, [
      h('path', {
        'stroke-linecap': 'round',
        'stroke-linejoin': 'round',
        d: 'M9.813 15.904 9 18.75l-.813-2.846a4.5 4.5 0 0 0-3.09-3.09L2.25 12l2.846-.813a4.5 4.5 0 0 0 3.09-3.09L9 5.25l.813 2.846a4.5 4.5 0 0 0 3.09 3.09L15.75 12l-2.846.813a4.5 4.5 0 0 0-3.09 3.09ZM18.259 8.715 18 9.75l-.259-1.035a3.375 3.375 0 0 0-2.455-2.456L14.25 6l1.036-.259a3.375 3.375 0 0 0 2.455-2.456L18 2.25l.259 1.035a3.375 3.375 0 0 0 2.456 2.456L21.75 6l-1.035.259a3.375 3.375 0 0 0-2.456 2.456ZM16.894 20.567 16.5 21.75l-.394-1.183a2.25 2.25 0 0 0-1.423-1.423L13.5 18.75l1.183-.394a2.25 2.25 0 0 0 1.423-1.423l.394-1.183.394 1.183a2.25 2.25 0 0 0 1.423 1.423l1.183.394-1.183.394a2.25 2.25 0 0 0-1.423 1.423Z'
      })
    ])
  }
}

const clientTabs = computed((): TabConfig[] => {
  if (!props.platform) return []
  if (props.claudeCodeOnly) {
    return [{ id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: TerminalIcon }]
  }
  switch (props.platform) {
    case 'openai': {
      const tabs: TabConfig[] = [
        { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: TerminalIcon },
        { id: 'codex-ws', label: t('keys.useKeyModal.cliTabs.codexCliWs'), icon: TerminalIcon },
      ]
      if (props.allowMessagesDispatch) {
        tabs.push({ id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: TerminalIcon })
      }
      tabs.push({ id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: TerminalIcon })
      return tabs
    }
    case 'gemini':
      return [
        { id: 'gemini', label: t('keys.useKeyModal.cliTabs.geminiCli'), icon: SparkleIcon },
        { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: TerminalIcon },
        { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: TerminalIcon }
      ]
    case 'antigravity':
      return [
        { id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: TerminalIcon },
        { id: 'gemini', label: t('keys.useKeyModal.cliTabs.geminiCli'), icon: SparkleIcon },
        { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: TerminalIcon },
        { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: TerminalIcon }
      ]
    case 'grok':
      return [
        { id: 'grok', label: t('keys.useKeyModal.cliTabs.grokCli'), icon: TerminalIcon },
        { id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: TerminalIcon },
        { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: TerminalIcon },
        { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: TerminalIcon }
      ]
    case 'typesafe':
      return [
        { id: 'systemone', label: t('keys.useKeyModal.cliTabs.systemOne'), icon: TerminalIcon }
      ]
    case 'deepseek':
    case 'minimax':
    case 'composite':
      return [
        { id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: TerminalIcon },
        { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: TerminalIcon },
        { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: TerminalIcon }
      ]
    default:
      return [
        { id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: TerminalIcon },
        { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: TerminalIcon },
        { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: TerminalIcon }
      ]
  }
})

// Shell tabs (3 types for environment variable based configs)
const shellTabs: TabConfig[] = [
  { id: 'unix', label: 'macOS / Linux', icon: AppleIcon },
  { id: 'cmd', label: 'Windows CMD', icon: WindowsIcon },
  { id: 'powershell', label: 'PowerShell', icon: WindowsIcon }
]

// OpenAI tabs (2 OS types)
const openaiTabs: TabConfig[] = [
  { id: 'unix', label: 'macOS / Linux', icon: AppleIcon },
  { id: 'windows', label: 'Windows', icon: WindowsIcon }
]

const showShellTabs = computed(() => activeClientTab.value !== 'opencode')

const showCodexAuthMode = computed(() =>
  props.platform === 'openai' &&
  (activeClientTab.value === 'codex' || activeClientTab.value === 'codex-ws')
)

const currentTabs = computed(() => {
  if (!showShellTabs.value) return []
  if (activeClientTab.value === 'codex' || activeClientTab.value === 'codex-ws' || activeClientTab.value === 'grok') {
    return openaiTabs
  }
  return shellTabs
})

const platformDescription = computed(() => {
  if (activeClientTab.value === 'codex' &&
    props.platform !== 'openai' &&
    props.platform !== 'grok' &&
    props.platform !== 'deepseek' &&
    props.platform !== 'minimax' &&
    props.platform !== 'composite') {
    return t('keys.useKeyModal.routedCodex.description')
  }
  switch (props.platform) {
    case 'openai':
      if (activeClientTab.value === 'claude') {
        return t('keys.useKeyModal.description')
      }
      return t('keys.useKeyModal.openai.description')
    case 'gemini':
      return t('keys.useKeyModal.gemini.description')
    case 'antigravity':
      return t('keys.useKeyModal.antigravity.description')
    case 'grok':
      if (activeClientTab.value === 'claude') {
        return t('keys.useKeyModal.grok.claudeDescription')
      }
      if (activeClientTab.value === 'codex') {
        return t('keys.useKeyModal.grok.codexDescription')
      }
      return t('keys.useKeyModal.grok.description')
    case 'deepseek':
      return activeClientTab.value === 'codex'
        ? t('keys.useKeyModal.deepseek.codexDescription')
        : t('keys.useKeyModal.deepseek.description')
    case 'minimax':
      return activeClientTab.value === 'codex'
        ? t('keys.useKeyModal.minimax.codexDescription')
        : t('keys.useKeyModal.minimax.description')
    case 'composite':
      return activeClientTab.value === 'codex'
        ? t('keys.useKeyModal.composite.codexDescription')
        : t('keys.useKeyModal.composite.description')
    case 'typesafe':
      return t('keys.useKeyModal.typesafe.description')
    default:
      return t('keys.useKeyModal.description')
  }
})

const platformNote = computed(() => {
  if (activeClientTab.value === 'codex' &&
    props.platform !== 'openai' &&
    props.platform !== 'grok' &&
    props.platform !== 'deepseek' &&
    props.platform !== 'minimax' &&
    props.platform !== 'composite') {
    return t('keys.useKeyModal.routedCodex.note')
  }
  switch (props.platform) {
    case 'openai':
      if (activeClientTab.value === 'claude') {
        return t('keys.useKeyModal.note')
      }
      return activeTab.value === 'windows'
        ? t('keys.useKeyModal.openai.noteWindows')
        : t('keys.useKeyModal.openai.note')
    case 'gemini':
      return t('keys.useKeyModal.gemini.note')
    case 'antigravity':
      return activeClientTab.value === 'claude'
        ? t('keys.useKeyModal.antigravity.claudeNote')
        : t('keys.useKeyModal.antigravity.geminiNote')
    case 'grok':
      if (activeClientTab.value === 'claude') {
        return t('keys.useKeyModal.grok.claudeNote')
      }
      if (activeClientTab.value === 'codex') {
        return activeTab.value === 'windows'
          ? t('keys.useKeyModal.grok.codexNoteWindows')
          : t('keys.useKeyModal.grok.codexNote')
      }
      // Grok CLI: shell-specific path guidance (env + ~/.grok/config.toml).
      if (activeClientTab.value === 'grok' && (activeTab.value === 'cmd' || activeTab.value === 'powershell')) {
        return t('keys.useKeyModal.grok.noteWindows')
      }
      if (activeClientTab.value === 'grok' && activeTab.value === 'windows') {
        return t('keys.useKeyModal.grok.noteWindows')
      }
      return t('keys.useKeyModal.grok.note')
    case 'deepseek':
      return activeClientTab.value === 'codex'
        ? t('keys.useKeyModal.deepseek.codexNote')
        : t('keys.useKeyModal.note')
    case 'minimax':
      return activeClientTab.value === 'codex'
        ? t('keys.useKeyModal.minimax.codexNote')
        : t('keys.useKeyModal.note')
    case 'composite':
      return activeClientTab.value === 'codex'
        ? t('keys.useKeyModal.composite.codexNote')
        : t('keys.useKeyModal.note')
    case 'typesafe':
      return t('keys.useKeyModal.typesafe.note')
    default:
      return t('keys.useKeyModal.note')
  }
})

const showPlatformNote = computed(() => activeClientTab.value !== 'opencode')


const { codexModelManifestState, codexModelManifestContent,
  codexModelManifestModelCount, codexModelCatalogMode, codexModelCatalogOversized, codexModelCatalogUrl,
  codexLocalCatalogToml, showCodexModelCatalog, codexModelCatalogPath, loadCodexModelManifest } = useKeyGuideCatalog(props, activeClientTab, activeTab)
const { currentFiles } = useKeyGuideFiles(props, t, { activeTab, activeClientTab, codexAuthMode,
  codexModelManifestContent, codexLocalCatalogToml, codexModelCatalogMode,
  buildCodexModelCatalogUrl: services.codexModels.buildCatalogUrl })
function downloadCodexModelManifest() {
  if (!codexModelManifestContent.value) return
  saveAs(
    new Blob([codexModelManifestContent.value], { type: 'application/json;charset=utf-8' }),
    'codex-models.json'
  )
}

// Until the first catalog for this key arrives, catalog-derived files would
// show a placeholder model, so copying and downloading them waits.
function isAwaitingModelCatalog(file: FileConfig): boolean {
  return Boolean(file.usesModelCatalog) && codexModelManifestState.value === 'loading'
}

function downloadFile(file: FileConfig) {
  if (!file.downloadName) return
  const type = file.downloadName.endsWith('.json')
    ? 'application/json;charset=utf-8'
    : 'application/toml;charset=utf-8'
  saveAs(new Blob([file.content], { type }), file.downloadName)
}

const copyContent = async (content: string, index: number) => {
  const scope = services.scopeKey.value
  const success = await clipboardCopy(content, t('keys.copied'))
  if (success && alive && scope === services.scopeKey.value) {
    copiedIndex.value = index
    clearTimeout(copyTimer)
    copyTimer = setTimeout(() => {
      copiedIndex.value = null
    }, 2000)
  }
}

watch(services.scopeKey, () => { copiedIndex.value = null; clearTimeout(copyTimer) }, { flush: 'sync' })
onScopeDispose(() => { alive = false; clearTimeout(copyTimer) })
</script>
<template>
  <BaseDialog
    :show="show"
    :title="t('keys.useKeyModal.title')"
    width="wide"
    @close="emit('close')"
  >
    <div class="space-y-4">
      <!-- No Group Assigned Warning -->
      <div v-if="!platform" class="flex items-start gap-3 p-4 rounded-lg bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800">
        <svg class="w-5 h-5 text-yellow-500 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
        </svg>
        <div>
          <p class="text-sm font-medium text-yellow-800 dark:text-yellow-200">
            {{ t('keys.useKeyModal.noGroupTitle') }}
          </p>
          <p class="text-sm text-yellow-700 dark:text-yellow-300 mt-1">
            {{ t('keys.useKeyModal.noGroupDescription') }}
          </p>
        </div>
      </div>

      <!-- Platform-specific content -->
      <template v-else>
        <!-- Description -->
        <p class="text-sm text-gray-600 dark:text-gray-400">
          {{ platformDescription }}
        </p>

        <!-- Client Tabs -->
        <div v-if="clientTabs.length" class="overflow-x-auto border-b border-gray-200 dark:border-dark-700">
          <nav class="-mb-px flex min-w-max gap-4 sm:gap-6" aria-label="Client">
            <button
              v-for="tab in clientTabs"
              :key="tab.id"
              type="button"
              @click="activeClientTab = tab.id"
              :class="[
                'whitespace-nowrap py-2.5 px-1 border-b-2 font-medium text-sm transition-colors',
                activeClientTab === tab.id
                  ? 'border-primary-500 text-primary-600 dark:text-primary-400'
                  : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300 dark:text-gray-400 dark:hover:text-gray-300'
              ]"
            >
              <span class="flex items-center gap-2">
                <component :is="tab.icon" class="w-4 h-4" />
                {{ tab.label }}
              </span>
            </button>
          </nav>
        </div>

        <!-- Codex Authentication Mode -->
        <div
          v-if="showCodexAuthMode"
          class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"
        >
          <div class="mb-2">
            <p class="text-sm font-medium text-gray-900 dark:text-white">
              {{ t('keys.useKeyModal.openai.authModeTitle') }}
            </p>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('keys.useKeyModal.openai.authModeDescription') }}
            </p>
          </div>
          <div
            class="grid grid-cols-2 gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-700"
            role="radiogroup"
            :aria-label="t('keys.useKeyModal.openai.authModeTitle')"
          >
            <button
              type="button"
              role="radio"
              data-testid="codex-auth-mode-legacy"
              :aria-checked="codexAuthMode === 'legacy'"
              :class="[
                'rounded-md px-3 py-2 text-sm font-medium transition-colors',
                codexAuthMode === 'legacy'
                  ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                  : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
              ]"
              @click="codexAuthMode = 'legacy'"
            >
              {{ t('keys.useKeyModal.openai.authModeLegacy') }}
            </button>
            <button
              type="button"
              role="radio"
              data-testid="codex-auth-mode-api-key"
              :aria-checked="codexAuthMode === 'api-key'"
              :class="[
                'rounded-md px-3 py-2 text-sm font-medium transition-colors',
                codexAuthMode === 'api-key'
                  ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                  : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
              ]"
              @click="codexAuthMode = 'api-key'"
            >
              {{ t('keys.useKeyModal.openai.authModeApiKey') }}
            </button>
          </div>
          <div
            v-if="codexAuthMode === 'api-key'"
            data-testid="codex-api-key-restart-notice"
            class="mt-3 flex items-start gap-2 border-l-2 border-amber-400 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-500 dark:bg-amber-950/30 dark:text-amber-200"
          >
            <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
            <p>{{ t('keys.useKeyModal.openai.authModeApiKeyRestartNotice') }}</p>
          </div>
        </div>

        <!-- OS/Shell Tabs -->
        <div v-if="showShellTabs" class="overflow-x-auto border-b border-gray-200 dark:border-dark-700">
          <nav class="-mb-px flex min-w-max gap-4" aria-label="Tabs">
            <button
              v-for="tab in currentTabs"
              :key="tab.id"
              type="button"
              @click="activeTab = tab.id"
              :class="[
                'whitespace-nowrap py-2.5 px-1 border-b-2 font-medium text-sm transition-colors',
                activeTab === tab.id
                  ? 'border-primary-500 text-primary-600 dark:text-primary-400'
                  : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300 dark:text-gray-400 dark:hover:text-gray-300'
              ]"
            >
              <span class="flex items-center gap-2">
                <component :is="tab.icon" class="w-4 h-4" />
                {{ tab.label }}
              </span>
            </button>
          </nav>
        </div>

        <!-- Code Blocks (Stacked for multi-file platforms) -->
        <div class="space-y-4">
          <div
            v-for="(file, index) in currentFiles"
            :key="index"
            class="relative"
          >
            <!-- File Hint (if exists) -->
            <p v-if="file.hint" class="text-xs text-amber-600 dark:text-amber-400 mb-1.5 flex items-center gap-1">
              <Icon name="exclamationCircle" size="sm" class="flex-shrink-0" />
              {{ file.hint }}
            </p>
            <div class="bg-gray-900 dark:bg-dark-900 rounded-xl overflow-hidden">
              <!-- Code Header -->
              <div class="flex items-center justify-between gap-2 px-4 py-2 bg-gray-800 dark:bg-dark-800 border-b border-gray-700 dark:border-dark-700">
                <span class="min-w-0 truncate text-xs text-gray-400 font-mono">{{ file.path }}</span>
                <div class="flex flex-shrink-0 items-center gap-1.5">
                  <span
                    v-if="isAwaitingModelCatalog(file)"
                    data-testid="codex-config-resolving"
                    class="flex items-center gap-1 text-xs text-gray-400"
                  >
                    <Icon name="refresh" size="xs" class="animate-spin" />
                    <span class="hidden sm:inline">{{ t('keys.useKeyModal.codexModelCatalog.resolving') }}</span>
                  </span>
                  <button
                    v-if="file.downloadName"
                    type="button"
                    :data-testid="`use-key-download-${file.downloadName}`"
                    :disabled="isAwaitingModelCatalog(file)"
                    @click="downloadFile(file)"
                    class="flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded-lg transition-colors bg-gray-700 hover:bg-gray-600 text-gray-300 hover:text-white disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5M16.5 12L12 16.5m0 0L7.5 12m4.5 4.5V3" />
                    </svg>
                    {{ t('keys.useKeyModal.download') }}
                  </button>
                  <button
                    type="button"
                    :disabled="isAwaitingModelCatalog(file)"
                    @click="copyContent(file.content, index)"
                    class="flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded-lg transition-colors disabled:cursor-not-allowed disabled:opacity-50"
                    :class="copiedIndex === index
                      ? 'bg-green-500/20 text-green-400'
                      : 'bg-gray-700 hover:bg-gray-600 text-gray-300 hover:text-white'"
                  >
                    <svg v-if="copiedIndex === index" class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
                    </svg>
                    <svg v-else class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M15.666 3.888A2.25 2.25 0 0013.5 2.25h-3c-1.03 0-1.9.693-2.166 1.638m7.332 0c.055.194.084.4.084.612v0a.75.75 0 01-.75.75H9a.75.75 0 01-.75-.75v0c0-.212.03-.418.084-.612m7.332 0c.646.049 1.288.11 1.927.184 1.1.128 1.907 1.077 1.907 2.185V19.5a2.25 2.25 0 01-2.25 2.25H6.75A2.25 2.25 0 014.5 19.5V6.257c0-1.108.806-2.057 1.907-2.185a48.208 48.208 0 011.927-.184" />
                    </svg>
                    {{ copiedIndex === index ? t('keys.useKeyModal.copied') : t('keys.useKeyModal.copy') }}
                  </button>
                </div>
              </div>
              <!-- Code Content -->
              <pre
                class="p-4 text-sm font-mono text-gray-100 overflow-x-auto transition-opacity"
                :class="isAwaitingModelCatalog(file) ? 'opacity-50' : ''"
                :aria-busy="isAwaitingModelCatalog(file)"
              ><code v-if="file.highlighted" v-html="file.highlighted"></code><code v-else v-text="file.content"></code></pre>
            </div>
          </div>
        </div>

        <section
          v-if="showCodexModelCatalog"
          data-testid="codex-model-catalog"
          class="overflow-hidden rounded-lg border border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-800/50"
        >
          <div class="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
            <div class="min-w-0">
              <h3 class="text-sm font-medium text-gray-900 dark:text-white">
                {{ t('keys.useKeyModal.codexModelCatalog.title') }}
              </h3>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('keys.useKeyModal.codexModelCatalog.description') }}
              </p>
              <p class="mt-1 truncate font-mono text-xs text-gray-700 dark:text-gray-300">
                {{ codexModelCatalogMode === 'remote' ? codexModelCatalogUrl : codexModelCatalogPath }}
              </p>
              <select
                v-model="codexModelCatalogMode"
                data-testid="codex-model-catalog-mode"
                :aria-label="t('keys.useKeyModal.codexModelCatalog.mode')"
                class="input mt-2 text-sm"
              >
                <option value="remote" :disabled="codexModelCatalogOversized">{{ t('keys.useKeyModal.codexModelCatalog.remote') }}</option>
                <option value="file">{{ t('keys.useKeyModal.codexModelCatalog.local') }}</option>
              </select>
              <p v-if="codexModelCatalogOversized" class="mt-2 text-xs text-amber-700 dark:text-amber-300">
                {{ t('keys.useKeyModal.codexModelCatalog.oversized') }}
              </p>
            </div>
            <button
              v-if="codexModelManifestState === 'ready'"
              type="button"
              class="btn btn-primary min-h-9 flex-shrink-0 px-3 text-xs"
              @click="downloadCodexModelManifest"
            >
              <Icon name="download" size="sm" class="mr-1.5" />
              {{ t('keys.useKeyModal.codexModelCatalog.download') }}
            </button>
            <button
              v-else
              type="button"
              data-testid="codex-model-catalog-fetch"
              class="btn btn-primary min-h-9 flex-shrink-0 px-3 text-xs"
              :disabled="codexModelManifestState === 'loading' || !apiKey"
              @click="loadCodexModelManifest"
            >
              <Icon
                name="refresh"
                size="sm"
                class="mr-1.5"
                :class="codexModelManifestState === 'loading' ? 'animate-spin' : ''"
              />
              {{ codexModelManifestState === 'error'
                ? t('keys.useKeyModal.codexModelCatalog.retry')
                : t('keys.useKeyModal.codexModelCatalog.fetch') }}
            </button>
          </div>
          <p
            v-if="codexModelManifestState === 'ready' && codexModelManifestModelCount > 0"
            class="border-t border-gray-200 px-4 py-2 text-xs text-emerald-700 dark:border-dark-700 dark:text-emerald-300"
          >
            {{ t('keys.useKeyModal.codexModelCatalog.modelsCount', { count: codexModelManifestModelCount }) }}
          </p>
          <p
            v-else-if="codexModelManifestState === 'ready'"
            class="border-t border-amber-200 px-4 py-2 text-xs text-amber-700 dark:border-amber-900 dark:text-amber-300"
          >
            {{ t('keys.useKeyModal.codexModelCatalog.emptyDescription') }}
          </p>
          <p
            v-else-if="codexModelManifestState === 'error'"
            class="border-t border-red-200 px-4 py-2 text-xs text-red-700 dark:border-red-900 dark:text-red-300"
          >
            {{ t('keys.useKeyModal.codexModelCatalog.errorDescription') }}
          </p>
        </section>

        <!-- Usage Note -->
        <div v-if="showPlatformNote" class="flex items-start gap-3 p-3 rounded-lg bg-blue-50 dark:bg-blue-900/20 border border-blue-100 dark:border-blue-800">
          <Icon name="infoCircle" size="md" class="text-blue-500 flex-shrink-0 mt-0.5" />
          <p class="text-sm text-blue-700 dark:text-blue-300">
            {{ platformNote }}
          </p>
        </div>
      </template>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button
          @click="emit('close')"
          class="btn btn-secondary"
        >
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

