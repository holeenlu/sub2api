<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { Props, CodexImageToolMode } from '../model/editEditorTypes'


const props = defineProps<{
  account: NonNullable<Props['account']>
  codexImageToolBadgeClass: UnwrapRef<ComputedRef<"bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300" | "bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300" | "bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300" | "bg-slate-100 text-slate-600 dark:bg-dark-600 dark:text-slate-300">>
  codexImageToolBadgeLabel: UnwrapRef<ComputedRef<string>>
  codexImageToolOptions: UnwrapRef<ComputedRef<{ value: CodexImageToolMode; label: string; description: string; selectedCardClass: string; selectedDotClass: string }[]>>
  codexImageToolMode: UnwrapRef<Ref<CodexImageToolMode, CodexImageToolMode>>
}>()

const emit = defineEmits<{
  'update:codexImageToolMode': [value: UnwrapRef<Ref<CodexImageToolMode, CodexImageToolMode>>]
}>()

const { t } = useI18n()

const account = computed(() => props.account)

const codexImageToolBadgeClass = computed(() => props.codexImageToolBadgeClass)

const codexImageToolBadgeLabel = computed(() => props.codexImageToolBadgeLabel)

const codexImageToolOptions = computed(() => props.codexImageToolOptions)

const codexImageToolMode = usePanelField(() => props.codexImageToolMode, value => emit('update:codexImageToolMode', value))
</script>

<template>
<template v-if="account">
<div
        v-if="account?.platform === 'openai' && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="overflow-hidden rounded-lg border border-sky-100 bg-sky-50/60 shadow-sm dark:border-sky-900/50 dark:bg-sky-950/20">
          <div class="flex items-start gap-3 px-4 py-3">
            <div class="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-white text-sky-600 shadow-sm ring-1 ring-sky-100 dark:bg-dark-800 dark:text-sky-300 dark:ring-sky-900/60">
              <Icon name="sparkles" size="sm" />
            </div>
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <label class="input-label mb-0">{{ t('admin.accounts.openai.codexImageTool') }}</label>
                <span
                  class="rounded-full px-2 py-0.5 text-[11px] font-medium"
                  :class="codexImageToolBadgeClass"
                >
                  {{ codexImageToolBadgeLabel }}
                </span>
              </div>
              <p class="mt-1 text-xs leading-5 text-slate-600 dark:text-slate-300">
                {{ t('admin.accounts.openai.codexImageToolDesc') }}
              </p>
            </div>
          </div>
          <div class="border-t border-sky-100 bg-white/70 p-2 dark:border-sky-900/50 dark:bg-dark-800/70">
            <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
              <button
                v-for="option in codexImageToolOptions"
                :key="option.value"
                type="button"
                :data-testid="`codex-image-tool-${option.value}`"
                @click="codexImageToolMode = option.value"
                :class="[
                  'group flex min-h-[62px] items-start gap-2 rounded-md border px-3 py-2 text-left transition-all',
                  codexImageToolMode === option.value
                    ? option.selectedCardClass
                    : 'border-transparent bg-transparent text-slate-600 hover:border-gray-200 hover:bg-gray-50 dark:text-slate-300 dark:hover:border-dark-500 dark:hover:bg-dark-700'
                ]"
              >
                <span
                  :class="[
                    'mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full border transition-colors',
                    codexImageToolMode === option.value
                      ? option.selectedDotClass
                      : 'border-gray-300 text-transparent group-hover:border-gray-400 dark:border-dark-500'
                  ]"
                >
                  <Icon name="check" size="xs" :stroke-width="2" />
                </span>
                <span class="min-w-0">
                  <span class="block text-sm font-medium">{{ option.label }}</span>
                  <span class="mt-0.5 block text-xs leading-4 text-slate-500 dark:text-slate-400">{{ option.description }}</span>
                </span>
              </button>
            </div>
          </div>
        </div>
      </div>
</template>
</template>
