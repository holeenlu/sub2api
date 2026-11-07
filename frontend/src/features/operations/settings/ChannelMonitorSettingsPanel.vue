<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'

import type { DeepReadonly } from 'vue'
import type { ChannelMonitorDraft } from './channelMonitorModel'
const props = defineProps<{ modelValue: DeepReadonly<ChannelMonitorDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<ChannelMonitorDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as ChannelMonitorDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof ChannelMonitorDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<ChannelMonitorDraft> = {}
  for (const key of Object.keys(value) as (keyof ChannelMonitorDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })


const { t } = useI18n();
</script>

<template>
<div class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('admin.settings.features.channelMonitor.title') }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.settings.features.channelMonitor.description') }}
            </p>
            <p class="mt-1.5 text-xs">
              <router-link
                to="/admin/channels/monitor"
                class="inline-flex items-center gap-1 text-primary-600 hover:underline dark:text-primary-400"
              >
                {{ t('admin.settings.features.channelMonitor.configureLink') }}
                <span aria-hidden="true">→</span>
              </router-link>
            </p>
          </div>
          <div class="space-y-5 p-6">
            <div class="flex items-center justify-between">
              <div>
                <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.settings.features.channelMonitor.enabled') }}
                </label>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.settings.features.channelMonitor.enabledHint') }}
                </p>
              </div>
              <Toggle v-model="form.channel_monitor_enabled" />
            </div>

            <div v-if="form.channel_monitor_enabled" class="space-y-5">
              <div>
                <label class="input-label">
                  {{ t('admin.settings.features.channelMonitor.mode') }}
                </label>
                <div class="mt-1.5 inline-flex w-full max-w-md rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-dark-600 dark:bg-dark-900/40">
                  <button
                    type="button"
                    class="inline-flex flex-1 items-center justify-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition"
                    :class="
                      form.channel_monitor_mode === 'v2'
                        ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                        : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
                    "
                    @click="form.channel_monitor_mode = 'v2'"
                  >
                    {{ t('admin.settings.features.channelMonitor.modeV2') }}
                  </button>
                  <button
                    type="button"
                    class="inline-flex flex-1 items-center justify-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition"
                    :class="
                      form.channel_monitor_mode === 'v1'
                        ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                        : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
                    "
                    @click="form.channel_monitor_mode = 'v1'"
                  >
                    {{ t('admin.settings.features.channelMonitor.modeV1') }}
                  </button>
                </div>
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{
                    form.channel_monitor_mode === 'v1'
                      ? t('admin.settings.features.channelMonitor.modeV1Hint')
                      : t('admin.settings.features.channelMonitor.modeV2Hint')
                  }}
                </p>
                <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
                  {{ t('admin.settings.features.channelMonitor.modeHint') }}
                </p>
              </div>

              <div v-if="form.channel_monitor_mode === 'v1'">
                <label class="input-label">
                  {{ t('admin.settings.features.channelMonitor.defaultInterval') }}
                  <span class="text-red-500">*</span>
                </label>
                <input
                  v-model.number="form.channel_monitor_default_interval_seconds"
                  type="number"
                  min="15"
                  max="3600"
                  class="input"
                />
                <p class="mt-1 text-xs text-gray-400">
                  {{ t('admin.settings.features.channelMonitor.defaultIntervalHint') }}
                </p>
              </div>

              <div v-if="form.channel_monitor_mode === 'v2'" class="space-y-4">
                <div class="flex items-start justify-between gap-4">
                  <div class="min-w-0">
                    <p class="text-sm font-medium text-gray-900 dark:text-white">
                      {{ t('admin.settings.features.channelMonitor.hideThroughput') }}
                    </p>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {{ t('admin.settings.features.channelMonitor.hideThroughputHint') }}
                    </p>
                  </div>
                  <Toggle v-model="form.channel_monitor_hide_throughput" />
                </div>
                <div class="flex items-start justify-between gap-4">
                  <div class="min-w-0">
                    <p class="text-sm font-medium text-gray-900 dark:text-white">
                      {{ t('admin.settings.features.channelMonitor.hideUserRanking') }}
                    </p>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {{ t('admin.settings.features.channelMonitor.hideUserRankingHint') }}
                    </p>
                  </div>
                  <Toggle v-model="form.channel_monitor_hide_user_ranking" />
                </div>
              </div>

              <div v-if="form.channel_monitor_mode === 'v1'" class="flex items-start justify-between gap-4">
                <div class="min-w-0">
                  <p class="text-sm font-medium text-gray-900 dark:text-white">
                    {{ t('admin.settings.features.channelMonitor.showQuota') }}
                  </p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                    {{ t('admin.settings.features.channelMonitor.showQuotaHint') }}
                  </p>
                </div>
                <Toggle v-model="form.channel_monitor_show_quota" />
              </div>
            </div>
          </div>
        </div>
</template>
