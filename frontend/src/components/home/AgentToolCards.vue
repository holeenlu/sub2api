<template>
  <div data-testid="agent-tool-cards">
    <div class="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
      <div
        v-for="tool in TOOLS"
        :key="tool"
        class="flex flex-col gap-2.5 rounded-2xl border border-gray-200/70 bg-white/80 p-5 shadow-sm backdrop-blur-sm dark:border-dark-700/70 dark:bg-dark-800/70"
      >
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ tool }}</h3>
        <div class="flex flex-wrap gap-1.5">
          <template v-if="tool === 'Codex'">
            <span :class="chipClass">{{ t('home.agents.codexContinuation') }}</span>
            <span :class="chipClass">{{ t('home.agents.codexWebSocket') }}</span>
          </template>
          <span v-else :class="chipClass">{{ t('home.agents.dedicatedSetup') }}</span>
        </div>
        <router-link
          :to="tool === 'Codex' ? '/apps/codex' : tool === 'Claude Code' ? '/apps/claude-code' : '/apps/console'"
          class="mt-auto pt-1.5 text-[13px] font-semibold text-primary-700 hover:text-primary-600 dark:text-primary-400 dark:hover:text-primary-300"
        >
          {{ t('home.agents.setupGuide') }} →
        </router-link>
      </div>
    </div>

    <p
      class="mt-5 rounded-2xl border border-dashed border-gray-200 bg-white/70 px-5 py-4 text-[13px] leading-relaxed text-gray-600 dark:border-dark-700 dark:bg-dark-800/50 dark:text-dark-300"
    >
      {{ t('home.agents.details') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

/** 后端实际提供设定范本的代理工具（不含 Gemini CLI）。 */
const TOOLS = ['Claude Code', 'Codex', 'OpenCode', 'Roo Code', 'Grok Build CLI'] as const

const chipClass =
  'inline-flex items-center rounded-full border border-primary-100 bg-primary-50 px-2.5 py-0.5 text-[11.5px] text-primary-700 dark:border-primary-900 dark:bg-primary-950/40 dark:text-primary-300'
</script>
