<script setup lang="ts">
import { platformAccountTabClass } from '@/shared/styles/platformColors'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/shared/ui/PlatformIcon.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { type CnProviderPlatform } from "@/features/resources/model/credentialsBuilder"
import { type UnwrapRef, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { CreateResourceForm } from '../model/createEditorTypes'
import type { PlatformSpec } from '@/features/catalog'


const props = defineProps<{
  form: Pick<CreateResourceForm, "platform">
  selectTypeSafePlatform: UnwrapRef<() => void>
  selectCNPlatform: UnwrapRef<(platform: CnProviderPlatform) => void>
  selectOpenCodeGoPlatform: UnwrapRef<() => void>
  extraMultiProtocolPlatforms: UnwrapRef<ComputedRef<PlatformSpec[]>>
  selectGenericMultiProtocolPlatform: UnwrapRef<(platform: string) => void>
}>()

const emit = defineEmits<{
  'update:form': [value: Pick<CreateResourceForm, "platform">]
}>()

const { t } = useI18n()

const form = usePanelField(() => props.form, value => emit('update:form', value))

const selectTypeSafePlatform = computed(() => props.selectTypeSafePlatform)

const selectCNPlatform = computed(() => props.selectCNPlatform)

const selectOpenCodeGoPlatform = computed(() => props.selectOpenCodeGoPlatform)

const extraMultiProtocolPlatforms = computed(() => props.extraMultiProtocolPlatforms)

const selectGenericMultiProtocolPlatform = computed(() => props.selectGenericMultiProtocolPlatform)
</script>

<template>
<div>
        <label class="input-label">{{ t('admin.accounts.platform') }}</label>
        <div class="mt-2 flex flex-wrap rounded-lg bg-gray-100 p-1 dark:bg-dark-700" data-tour="account-form-platform">
          <button
            type="button"
            @click="form.platform = 'anthropic'"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-4 py-2.5 text-sm font-medium transition-all',
              form.platform === 'anthropic'
                ? platformAccountTabClass('anthropic')
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <Icon name="sparkles" size="sm" />
            Anthropic
          </button>
          <button
            type="button"
            @click="form.platform = 'openai'"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-4 py-2.5 text-sm font-medium transition-all',
              form.platform === 'openai'
                ? platformAccountTabClass('openai')
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <svg
              class="h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="1.5"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M3.75 13.5l10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75z"
              />
            </svg>
            OpenAI
          </button>
          <button
            type="button"
            @click="form.platform = 'gemini'"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-4 py-2.5 text-sm font-medium transition-all',
              form.platform === 'gemini'
                ? 'bg-white text-blue-600 shadow-sm dark:bg-dark-600 dark:text-blue-400'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <svg
              class="h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="1.5"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M12 2l1.5 6.5L20 10l-6.5 1.5L12 18l-1.5-6.5L4 10l6.5-1.5L12 2z"
              />
            </svg>
            Gemini
          </button>
          <button
            type="button"
            @click="form.platform = 'antigravity'"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-4 py-2.5 text-sm font-medium transition-all',
              form.platform === 'antigravity'
                ? 'bg-white text-purple-600 shadow-sm dark:bg-dark-600 dark:text-purple-400'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <Icon name="cloud" size="sm" />
            Antigravity
          </button>
          <button
            type="button"
            @click="form.platform = 'grok'"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-4 py-2.5 text-sm font-medium transition-all',
              form.platform === 'grok'
                ? 'bg-white text-zinc-900 shadow-sm dark:bg-dark-600 dark:text-zinc-100'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <PlatformIcon platform="grok" size="sm" />
            Grok
          </button>
          <button
            type="button"
            @click="selectTypeSafePlatform()"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-4 py-2.5 text-sm font-medium transition-all',
              form.platform === 'typesafe'
                ? 'bg-white text-sky-700 shadow-sm dark:bg-dark-600 dark:text-sky-300'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <PlatformIcon platform="typesafe" size="sm" />
            TypeSafe / Jev
          </button>
        </div>
        <!-- 国产厂商：Kimi / Zhipu GLM / DeepSeek / MiniMax -->
        <div class="mt-2 flex flex-wrap rounded-lg bg-gray-100 p-1 dark:bg-dark-700" data-testid="platform-row-cn">
          <button
            type="button"
            @click="selectCNPlatform('kimi')"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-3 py-2.5 text-sm font-medium transition-all',
              form.platform === 'kimi'
                ? 'bg-white text-pink-600 shadow-sm dark:bg-dark-600 dark:text-pink-400'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <PlatformIcon platform="kimi" size="sm" />
            Kimi
          </button>
          <button
            type="button"
            @click="selectCNPlatform('zhipu')"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-3 py-2.5 text-sm font-medium transition-all',
              form.platform === 'zhipu'
                ? 'bg-white text-indigo-600 shadow-sm dark:bg-dark-600 dark:text-indigo-400'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <PlatformIcon platform="zhipu" size="sm" />
            Zhipu GLM
          </button>
          <button
            type="button"
            @click="selectCNPlatform('deepseek')"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-3 py-2.5 text-sm font-medium transition-all',
              form.platform === 'deepseek'
                ? 'bg-white text-teal-600 shadow-sm dark:bg-dark-600 dark:text-teal-400'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <PlatformIcon platform="deepseek" size="sm" />
            DeepSeek
          </button>
          <button
            type="button"
            @click="selectCNPlatform('minimax')"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-3 py-2.5 text-sm font-medium transition-all',
              form.platform === 'minimax'
                ? 'bg-white text-rose-600 shadow-sm dark:bg-dark-600 dark:text-rose-400'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <PlatformIcon platform="minimax" size="sm" />
            MiniMax
          </button>
        </div>

        <!-- 多模型聚合平台：OpenCode 与平台清单中登记的其他多协议供应商 -->
        <div class="mt-2 flex flex-wrap rounded-lg bg-gray-100 p-1 dark:bg-dark-700" data-testid="platform-row-aggregators">
          <button
            type="button"
            data-testid="platform-button-opencode_go"
            @click="selectOpenCodeGoPlatform()"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-3 py-2.5 text-sm font-medium transition-all',
              form.platform === 'opencode_go'
                ? 'bg-white text-amber-700 shadow-sm dark:bg-dark-600 dark:text-amber-300'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <PlatformIcon platform="opencode_go" size="sm" />
            OpenCode
          </button>
          <!-- 没有专属界面的多协议供应商：通用表单（模式 / 协议 / 端点来自 profile） -->
          <button
            v-for="spec in extraMultiProtocolPlatforms"
            :key="spec.id"
            type="button"
            :data-testid="`platform-button-${spec.id}`"
            @click="selectGenericMultiProtocolPlatform(spec.id)"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-3 py-2.5 text-sm font-medium transition-all',
              form.platform === spec.id
                ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-600 dark:text-primary-400'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <PlatformIcon :platform="spec.id" size="sm" />
            {{ spec.display_name }}
          </button>
        </div>
      </div>
</template>
