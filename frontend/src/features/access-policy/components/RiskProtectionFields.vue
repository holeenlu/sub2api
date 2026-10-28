<template>
          <div v-if="activeSettingsTab === 'runtime'" class="grid grid-cols-1 gap-5 lg:grid-cols-2">
            <div>
              <label class="input-label">{{ t('admin.riskControl.workerCount') }}</label>
              <input v-model.number="configForm.worker_count" type="number" min="1" max="32" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.riskControl.queueSize') }}</label>
              <input v-model.number="configForm.queue_size" type="number" min="100" max="100000" class="input" />
            </div>
            <div class="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700 lg:col-span-2">
              <div>
                <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.recordNonHits') }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.recordNonHitsHint') }}</p>
              </div>
              <Toggle v-model="configForm.record_non_hits" />
            </div>
            <div class="space-y-4 rounded-lg border border-gray-100 p-4 dark:border-dark-700 lg:col-span-2">
              <div class="flex items-center justify-between gap-4">
                <div>
                  <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.preHashCheck') }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.preHashCheckHint') }}</p>
                </div>
                <Toggle v-model="configForm.pre_hash_check_enabled" />
              </div>
              <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-900/30">
                <div class="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
                  <div>
                    <p class="text-sm font-medium text-gray-900 dark:text-white">
                      {{ t('admin.riskControl.flaggedHashCount', { count: formatNumber(status?.flagged_hash_count ?? 0) }) }}
                    </p>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.flaggedHashHint') }}</p>
                  </div>
                  <button
                    type="button"
                    class="btn btn-secondary inline-flex items-center justify-center gap-2 text-red-600 hover:text-red-700 dark:text-red-300"
                    :disabled="hashActionLoading || ((status?.flagged_hash_count ?? 0) + (status?.refusal_guard_entries ?? 0) + (status?.device_guard_entries ?? 0)) === 0"
                    @click="clearFlaggedHashes"
                  >
                    <Icon name="trash" size="sm" :class="hashActionLoading ? 'animate-pulse' : ''" />
                    {{ t('admin.riskControl.guard.clear') }}
                  </button>
                </div>
                <div class="mt-3 flex flex-col gap-2 sm:flex-row">
                  <input
                    v-model.trim="flaggedHashInput"
                    type="text"
                    class="input font-mono text-sm"
                    :placeholder="t('admin.riskControl.flaggedHashPlaceholder')"
                  />
                  <button
                    type="button"
                    class="btn btn-secondary inline-flex items-center justify-center gap-2"
                    :disabled="hashActionLoading || !isFlaggedHashInputValid"
                    @click="deleteFlaggedHash"
                  >
                    <Icon name="trash" size="sm" />
                    {{ t('admin.riskControl.deleteFlaggedHash') }}
                  </button>
                </div>
              </div>
            </div>
          </div>

          <div v-if="activeSettingsTab === 'response'" class="space-y-5">
            <div class="grid grid-cols-1 gap-5 lg:grid-cols-2">
              <div>
                <label class="input-label">{{ t('admin.riskControl.blockStatus') }}</label>
                <input v-model.number="configForm.block_status" type="number" min="400" max="599" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.riskControl.blockMessage') }}</label>
                <input v-model.trim="configForm.block_message" type="text" class="input" />
              </div>
              <div class="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div>
                  <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.emailOnHit') }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.emailOnHitHint') }}</p>
                </div>
                <Toggle v-model="configForm.email_on_hit" />
              </div>
              <div class="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div>
                  <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.autoBan') }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.autoBanHint') }}</p>
                </div>
                <Toggle v-model="configForm.auto_ban_enabled" />
              </div>
              <div class="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700 lg:col-span-2">
                <div>
                  <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.cyberPolicyExcludeBan') }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberPolicyExcludeBanHint') }}</p>
                </div>
                <Toggle v-model="configForm.cyber_policy_exclude_from_ban_count" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.riskControl.banThreshold') }}</label>
                <input v-model.number="configForm.ban_threshold" type="number" min="1" max="1000" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.riskControl.violationWindowHours') }}</label>
                <input v-model.number="configForm.violation_window_hours" type="number" min="1" max="8760" class="input" />
              </div>
            </div>
          </div>

          <div v-if="activeSettingsTab === 'riskThresholds'" class="space-y-5">
            <p class="text-sm font-medium">{{ engineLabel(configForm.engine) }}</p>
            <p v-if="configForm.engine === 'typesafe'" class="text-sm text-amber-700 dark:text-amber-300">{{ t('admin.riskControl.typeSafeThresholds') }}</p>
            <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.riskThresholds') }}</h3>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.riskThresholdsHint') }}</p>
              </div>
              <button
                type="button"
                class="btn btn-secondary inline-flex items-center justify-center gap-2"
                @click="resetRiskThresholds"
              >
                <Icon name="refresh" size="sm" />
                {{ t('admin.riskControl.riskThresholdReset') }}
              </button>
            </div>

            <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
              <div
                v-for="row in riskThresholdRows"
                :key="row.category"
                class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/30"
              >
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0">
                    <label class="block truncate text-sm font-semibold text-gray-900 dark:text-white" :for="`risk-threshold-${row.category}`">
                      {{ row.category }}
                    </label>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {{ t('admin.riskControl.riskThresholdDefault', { value: formatThresholdPercent(row.defaultValue) }) }}
                    </p>
                  </div>
                  <span class="inline-flex shrink-0 rounded-md bg-white px-2 py-1 font-mono text-xs font-medium text-gray-600 shadow-sm dark:bg-dark-800 dark:text-gray-300">
                    {{ formatThresholdPercent(row.value) }}
                  </span>
                </div>
                <div class="mt-3">
                  <label class="sr-only" :for="`risk-threshold-${row.category}`">
                    {{ t('admin.riskControl.riskThresholdPercent') }}
                  </label>
                  <div class="relative">
                    <input
                      :id="`risk-threshold-${row.category}`"
                      v-model.number="configForm.thresholds[row.category]"
                      :data-test="`risk-threshold-${row.category}`"
                      type="number"
                      min="0"
                      max="100"
                      step="0.1"
                      class="input pr-8 font-mono"
                    />
                    <span class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-gray-400">%</span>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div v-if="activeSettingsTab === 'guards'" class="space-y-5" data-test="risk-guards">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.guard.hint') }}</p>
            <label class="block">
              <span class="input-label">{{ t('admin.riskControl.guard.failurePolicy') }}</span>
              <select v-model="configForm.failure_policy" class="input" data-test="failure-policy">
                <option value="fail_open">{{ t('admin.riskControl.guard.failOpen') }}</option>
                <option value="fail_closed">{{ t('admin.riskControl.guard.failClosed') }}</option>
              </select>
            </label>
            <fieldset class="space-y-4 rounded-lg border border-gray-100 p-4 dark:border-dark-700">
              <label class="flex items-center justify-between gap-4">
                <span class="font-medium">{{ t('admin.riskControl.guard.distillation') }}</span>
                <input v-model="configForm.distillation.enabled" type="checkbox" data-test="distillation-enabled" />
              </label>
              <label class="block">
                <span class="input-label">{{ t('admin.riskControl.guard.phrases') }}</span>
                <textarea v-model="configForm.distillation_phrases" class="input min-h-24 font-mono" data-test="distillation-phrases" />
              </label>
              <label class="block">
                <span class="input-label">{{ t('admin.riskControl.guard.patterns') }}</span>
                <textarea v-model="configForm.distillation_patterns" class="input min-h-24 font-mono" data-test="distillation-patterns" />
              </label>
              <label class="block">
                <span class="input-label">{{ t('admin.riskControl.guard.fingerprints') }}</span>
                <textarea v-model="configForm.distillation_fingerprints" class="input min-h-24 font-mono" data-test="distillation-fingerprints" />
              </label>
              <label class="block">
                <span class="input-label">{{ t('admin.riskControl.guard.structurePattern') }}</span>
                <input v-model="configForm.distillation.structure_pattern" class="input font-mono" data-test="distillation-structure" />
              </label>
              <div class="grid gap-4 sm:grid-cols-2">
                <label>
                  <span class="input-label">{{ t('admin.riskControl.guard.minMaxTokens') }}</span>
                  <input v-model.number="configForm.distillation.min_max_tokens" type="number" min="1" max="10000000" class="input" data-test="distillation-max-tokens" />
                </label>
                <label>
                  <span class="input-label">{{ t('admin.riskControl.guard.minThinkingTokens') }}</span>
                  <input v-model.number="configForm.distillation.min_thinking_tokens" type="number" min="1" max="10000000" class="input" data-test="distillation-thinking-tokens" />
                </label>
              </div>
            </fieldset>
            <label class="block">
              <span class="input-label">{{ t('admin.riskControl.guard.regexRules') }}</span>
              <textarea v-model="configForm.regex_rules" class="input min-h-24 font-mono" data-test="regex-rules" />
            </label>
            <fieldset v-for="kind in guardCacheTypes" :key="kind" class="space-y-4 rounded-lg border border-gray-100 p-4 dark:border-dark-700">
              <label class="flex items-center justify-between gap-4">
                <span class="font-medium">{{ kind === 'refusal_guard' ? t('admin.riskControl.guard.refusal') : t('admin.riskControl.guard.device') }}</span>
                <input v-model="configForm[kind].enabled" type="checkbox" :data-test="`${kind}-enabled`" />
              </label>
              <div class="grid gap-4 sm:grid-cols-2">
                <label>
                  <span class="input-label">{{ t('admin.riskControl.guard.ttlSeconds') }}</span>
                  <input v-model.number="configForm[kind].ttl_seconds" type="number" min="1" :max="kind === 'refusal_guard' ? 604800 : 86400" class="input" :data-test="`${kind}-ttl`" />
                </label>
                <label>
                  <span class="input-label">{{ t('admin.riskControl.guard.maxEntries') }}</span>
                  <input v-model.number="configForm[kind].max_entries" type="number" min="1" max="4096" class="input" :data-test="`${kind}-capacity`" />
                </label>
              </div>
            </fieldset>
            <label class="block">
              <span class="input-label">{{ t('admin.riskControl.guard.similarity') }}</span>
              <input v-model.number="configForm.refusal_guard.similarity_threshold" type="number" min="0.8" max="1" step="0.01" class="input" data-test="refusal-similarity" />
            </label>
            <p class="text-sm text-gray-500">{{ t('admin.riskControl.guard.cacheCounts', { refusals: status?.refusal_guard_entries ?? 0, devices: status?.device_guard_entries ?? 0 }) }}</p>
            <button type="button" class="btn btn-secondary" :disabled="hashActionLoading" data-test="clear-guard-memory" @click="clearFlaggedHashes">{{ t('admin.riskControl.guard.clear') }}</button>
          </div>
<div v-if="activeSettingsTab === 'retention'" class="grid grid-cols-1 gap-5 lg:grid-cols-2">
            <div>
              <label class="input-label">{{ t('admin.riskControl.hitRetentionDays') }}</label>
              <input v-model.number="configForm.hit_retention_days" type="number" min="1" max="3650" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.riskControl.nonHitRetentionDays') }}</label>
              <input v-model.number="configForm.non_hit_retention_days" type="number" min="1" max="3" class="input" />
            </div>
            <div class="rounded-lg border border-gray-100 p-4 text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400 lg:col-span-2">
              <div class="flex flex-wrap items-center gap-3">
                <Icon name="database" size="md" class="text-gray-400" />
                <span>{{ t('admin.riskControl.cleanupStats', { hit: status?.last_cleanup_deleted_hit ?? 0, nonHit: status?.last_cleanup_deleted_non_hit ?? 0 }) }}</span>
              </div>
            </div>
          </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import { useI18n } from 'vue-i18n';
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import type { ModerationEngine, ContentModerationRuntimeStatus } from '../api/types';
import type { RiskConfigDraft } from '../model/riskConfigDraft';
import { riskThresholdDefaults, riskThresholdCategories, formatThresholdPercent } from '../model/riskConfigDraft';
import { useDraftSection } from '@/shared/lib/useDraftSection';


import Toggle from '@/shared/ui/Toggle.vue';
import Icon from '@/shared/ui/icons/Icon.vue';

const props = defineProps<{ modelValue: Readonly<RiskConfigDraft>; tab: string; groups: AdminGroup[]; status: ContentModerationRuntimeStatus | null; hashActionLoading: boolean }>()
const emit = defineEmits<{ patch: [value: Partial<RiskConfigDraft>]; clear: []; deleteHash: [hash: string] }>()
const configForm = useDraftSection(() => props.modelValue, patch => emit('patch', patch))
const activeSettingsTab = computed(() => props.tab)

const status = computed(() => props.status)
const hashActionLoading = computed(() => props.hashActionLoading)
const { t } = useI18n()
const flaggedHashInput = ref('')
const isFlaggedHashInputValid = computed(() => /^[a-fA-F0-9]{64}$/.test(flaggedHashInput.value.trim()))
const deleteFlaggedHash = () => emit('deleteHash', flaggedHashInput.value)
function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value)
}
const engineLabel = (engine: ModerationEngine) => engine === 'typesafe' ? 'TypeSafe AI' : 'OpenAI'
const clearFlaggedHashes = () => emit('clear')
type RiskThresholdRow = {
  category: string
  value: number
  defaultValue: number
}
const riskThresholdRows = computed<RiskThresholdRow[]>(() => (
  riskThresholdCategories.map((category) => ({
    category,
    value: configForm.thresholds[category] ?? riskThresholdDefaults[category],
    defaultValue: riskThresholdDefaults[category],
  }))
))
function resetRiskThresholds() {
  configForm.thresholds = { ...riskThresholdDefaults }
}
const guardCacheTypes = ['refusal_guard', 'device_guard'] as const
</script>
