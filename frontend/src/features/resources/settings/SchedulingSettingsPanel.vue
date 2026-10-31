<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, computed, watch } from 'vue'
import ClaudePromptEditor from '@/features/resources/components/ClaudePromptEditor.vue'
import { useI18n } from 'vue-i18n'
import { SCHEDULING_THRESHOLD_SCOPES } from './schedulingThresholds'
import Toggle from '@/shared/ui/Toggle.vue'

import type { DeepReadonly } from 'vue'
import type { SchedulingDraft } from './schedulingModel'
const props = defineProps<{ modelValue: DeepReadonly<SchedulingDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<SchedulingDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as SchedulingDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof SchedulingDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<SchedulingDraft> = {}
  for (const key of Object.keys(value) as (keyof SchedulingDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })


const { t } = useI18n();

const schedulingThresholdPlatforms = SCHEDULING_THRESHOLD_SCOPES;

const schedulingThresholdScopeLabel = (scope: string) => scope === "anthropic_fable" ? t("admin.settings.scheduling.accountSchedulingThresholdsFableLabel") : scope;

const codexSyncedVersionLabel = computed(() => {
  const synced = form.openai_codex_client_version_synced?.trim();
  if (!synced) return "";
  return t("admin.settings.gatewayForwarding.openaiCodexVersionSyncedValue", {
    version: synced,
  });
});

const claudeSyncedVersionLabel = computed(() => {
  const synced = form.claude_code_client_version_synced?.trim();
  if (!synced) return "";
  return t("admin.settings.gatewayForwarding.claudeCodeVersionSyncedValue", {
    version: synced,
  });
});
</script>

<template>
<div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.scheduling.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.scheduling.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.scheduling.allowUngroupedKey") }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.scheduling.allowUngroupedKeyHint") }}
                  </p>
                </div>
                <Toggle v-model="form.allow_ungrouped_key_scheduling" />
              </div>

              <div class="border-t border-gray-100 pt-4 dark:border-dark-700">
                <div class="mb-3">
                  <label class="font-medium text-gray-900 dark:text-white">
                    {{
                      t(
                        "admin.settings.scheduling.accountSchedulingThresholdsTitle",
                      )
                    }}
                  </label>
                  <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.scheduling.accountSchedulingThresholdsDescription",
                      )
                    }}
                  </p>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.scheduling.accountSchedulingThresholdsGlobalHint",
                      )
                    }}
                  </p>
                  <p class="mt-0.5 text-xs text-amber-600 dark:text-amber-400">
                    {{
                      t(
                        "admin.settings.scheduling.accountSchedulingThresholdsDisabledHint",
                      )
                    }}
                  </p>
                </div>
                <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
                  <div
                    v-for="scope in schedulingThresholdPlatforms"
                    :key="scope"
                    class="rounded-lg border border-gray-200 p-4 dark:border-dark-700"
                  >
                    <div class="flex items-start justify-between gap-3">
                      <div>
                        <label
                          class="font-mono text-sm font-medium text-gray-900 dark:text-white"
                        >
                          {{ schedulingThresholdScopeLabel(scope) }}
                        </label>
                        <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                          {{
                            scope === "anthropic_fable" ? t("admin.settings.scheduling.accountSchedulingThresholdsFableHint") : t("admin.settings.scheduling.accountSchedulingThresholdsRangeHint")
                          }}
                        </p>
                      </div>
                      <span
                        class="rounded bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300"
                      >
                        %
                      </span>
                    </div>
                    <input
                      v-model.number="form.account_scheduling_thresholds[scope]"
                      type="number"
                      min="1"
                      max="100"
                      step="1"
                      class="input mt-3"
                      :data-testid="`account-scheduling-threshold-${scope}`"
                      placeholder="100"
                    />
                  </div>
                </div>
              </div>

              <div class="border-t border-gray-100 pt-5 dark:border-dark-700" data-testid="openai-unified-scheduler">
                <h4 class="text-sm font-medium">{{ t("admin.settings.openaiScheduler.title") }}</h4>
                <p class="mt-1 text-xs text-gray-500">{{ t("admin.settings.openaiScheduler.description") }}</p>
              </div>
              <div class="flex items-center justify-between gap-5 border-t border-gray-100 pt-5 dark:border-dark-700">
                <div>
                  <label class="text-sm font-medium">{{ t("admin.settings.openaiScheduler.subscriptionPriorityTitle") }}</label>
                  <p class="mt-1 text-xs text-gray-500">{{ t("admin.settings.openaiScheduler.subscriptionPriorityDescription") }}</p>
                </div>
                <Toggle v-model="form.openai_advanced_scheduler_subscription_priority_enabled" data-testid="openai-subscription-priority-toggle" />
              </div>
              <div class="flex items-center justify-between gap-5 border-t border-gray-100 pt-5 dark:border-dark-700">
                <div>
                  <label class="text-sm font-medium">{{ t("admin.settings.openaiScheduler.lowRatePriorityTitle") }}</label>
                  <p class="mt-1 text-xs text-gray-500">{{ t("admin.settings.openaiScheduler.lowRatePriorityDescription") }}</p>
                </div>
                <Toggle v-model="form.openai_low_upstream_rate_priority_enabled" data-testid="openai-low-rate-priority-toggle" />
              </div>
              <div v-if="form.openai_low_upstream_rate_priority_enabled" class="border-t border-gray-100 pt-5 dark:border-dark-700">
                <label for="openai-oauth-scheduling-rate-multiplier" class="text-sm font-medium">{{ t("admin.settings.openaiScheduler.oauthRateTitle") }}</label>
                <p class="mt-1 text-xs text-gray-500">{{ t("admin.settings.openaiScheduler.oauthRateDescription") }}</p>
                <input id="openai-oauth-scheduling-rate-multiplier" v-model.number="form.openai_oauth_scheduling_rate_multiplier" class="input mt-2 w-32" data-testid="openai-oauth-scheduling-rate-multiplier" min="0" step="0.01" type="number" />
              </div>

              <div class="grid gap-5 border-b border-gray-100 pb-5 dark:border-dark-700 md:grid-cols-[minmax(0,1fr)_auto] md:items-end">
                <div>
                  <label
                    for="grok-default-text-model"
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.gatewayForwarding.grokDefaultTextModel") }}
                  </label>
                  <input
                    id="grok-default-text-model"
                    v-model.trim="form.grok_default_text_model"
                    type="text"
                    class="input mt-2 w-full"
                    list="grok-default-text-model-options"
                    data-testid="grok-default-text-model"
                    placeholder="grok-4.5"
                  />
                  <datalist id="grok-default-text-model-options">
                    <option value="grok-4.5" />
                    <option value="grok-4.1-fast" />
                    <option value="grok-4" />
                  </datalist>
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.gatewayForwarding.grokDefaultTextModelHint") }}
                  </p>
                </div>
                <div class="flex items-center justify-between gap-5 md:min-w-72">
                  <div>
                    <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                      {{ t("admin.settings.gatewayForwarding.grokCrossClientMap") }}
                    </label>
                    <p class="mt-0.5 max-w-sm text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.gatewayForwarding.grokCrossClientMapHint") }}
                    </p>
                  </div>
                  <Toggle
                    v-model="form.grok_cross_client_model_map_enabled"
                    data-testid="grok-cross-client-model-map-toggle"
                  />
                </div>
                </div>
                <div class="md:col-span-2">
                  <label
                    for="grok-default-base-url-mode"
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.gatewayForwarding.grokDefaultBaseURLMode") }}
                  </label>
                  <select
                    id="grok-default-base-url-mode"
                    v-model="form.grok_default_base_url_mode"
                    class="input mt-2 w-full"
                    data-testid="grok-default-base-url-mode"
                  >
                    <option value="cli">{{ t("admin.settings.gatewayForwarding.grokBaseURLModeCLI") }}</option>
                    <option value="api">{{ t("admin.settings.gatewayForwarding.grokBaseURLModeAPI") }}</option>
                    <option value="us-east-1">{{ t("admin.settings.gatewayForwarding.grokBaseURLModeUSEast1") }}</option>
                    <option value="us-west-2">{{ t("admin.settings.gatewayForwarding.grokBaseURLModeUSWest2") }}</option>
                    <option value="eu-west-1">{{ t("admin.settings.gatewayForwarding.grokBaseURLModeEUWest1") }}</option>
                  </select>
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.gatewayForwarding.grokDefaultBaseURLModeHint") }}
                  </p>
                </div>

              <!-- OpenAI Responses 首 token 统计 -->
              <div class="border-b border-gray-100 pb-5 dark:border-dark-700 md:col-span-2">
                <label
                  for="openai-ttft-mode"
                  class="text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.gatewayForwarding.openaiTTFTMode") }}
                </label>
                <select
                  id="openai-ttft-mode"
                  v-model="form.openai_ttft_mode"
                  class="input mt-2 w-full"
                  data-testid="openai-ttft-mode"
                >
                  <option value="semantic">
                    {{ t("admin.settings.gatewayForwarding.openaiTTFTModeSemantic") }}
                  </option>
                  <option value="visible">
                    {{ t("admin.settings.gatewayForwarding.openaiTTFTModeVisible") }}
                  </option>
                </select>
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.gatewayForwarding.openaiTTFTModeHint") }}
                </p>
              </div>

              <!-- Fingerprint Unification -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.fingerprintUnification",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.fingerprintUnificationHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle v-model="form.enable_fingerprint_unification" />
              </div>

              <!-- Metadata Passthrough -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t("admin.settings.gatewayForwarding.metadataPassthrough")
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.metadataPassthroughHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle v-model="form.enable_metadata_passthrough" />
              </div>

              <!-- CCH Signing -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.gatewayForwarding.cchSigning") }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.gatewayForwarding.cchSigningHint") }}
                  </p>
                </div>
                <Toggle v-model="form.enable_cch_signing" />
              </div>

              <!-- Claude OAuth System Prompt Injection -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.claudeOAuthSystemPromptInjection",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.claudeOAuthSystemPromptInjectionHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle
                  v-model="form.enable_claude_oauth_system_prompt_injection"
                />
              </div>

              <ClaudePromptEditor
                v-model="form.claude_oauth_system_prompt_blocks"
                v-model:enabled="form.enable_claude_oauth_system_prompt_injection"
              />

              <!-- Anthropic Cache TTL 1h Injection -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.anthropicCacheTTL1hInjection",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.anthropicCacheTTL1hInjectionHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle
                  v-model="form.enable_anthropic_cache_ttl_1h_injection"
                />
              </div>

              <!-- messages cache_control 改写 -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.rewriteMessageCacheControl",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.rewriteMessageCacheControlHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle v-model="form.rewrite_message_cache_control" />
              </div>

              <!-- 客户端 dateline 归一化（仅 Anthropic OAuth/SetupToken） -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.clientDatelineNormalization",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.clientDatelineNormalizationHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle
                  v-model="form.enable_client_dateline_normalization"
                />
              </div>

              <!-- Antigravity UA 版本 -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{
                    t(
                      "admin.settings.gatewayForwarding.antigravityUserAgentVersion",
                    )
                  }}
                </label>
                <input
                  v-model="form.antigravity_user_agent_version"
                  type="text"
                  class="input max-w-xs font-mono text-sm"
                  :placeholder="
                    t(
                      'admin.settings.gatewayForwarding.antigravityUserAgentVersionPlaceholder',
                    )
                  "
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{
                    t(
                      "admin.settings.gatewayForwarding.antigravityUserAgentVersionHint",
                    )
                  }}
                </p>
              </div>



              <!-- OpenAI Codex UA -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{
                    t(
                      "admin.settings.gatewayForwarding.openaiCodexUserAgent",
                    )
                  }}
                </label>
                <input
                  v-model="form.openai_codex_user_agent"
                  type="text"
                  class="input w-full font-mono text-sm"
                  :placeholder="
                    t(
                      'admin.settings.gatewayForwarding.openaiCodexUserAgentPlaceholder',
                    )
                  "
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{
                    t(
                      "admin.settings.gatewayForwarding.openaiCodexUserAgentHint",
                    )
                  }}
                </p>
              </div>

              <!-- Codex 客户端版本号 -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{
                    t(
                      "admin.settings.gatewayForwarding.openaiCodexClientVersion",
                    )
                  }}
                </label>
                <input
                  v-model="form.openai_codex_client_version"
                  type="text"
                  class="input w-full font-mono text-sm"
                  :placeholder="
                    t(
                      'admin.settings.gatewayForwarding.openaiCodexClientVersionPlaceholder',
                    )
                  "
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{
                    t(
                      "admin.settings.gatewayForwarding.openaiCodexClientVersionHint",
                    )
                  }}
                </p>
              </div>

              <!-- Codex 版本号自动同步 -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.openaiCodexVersionAutoSync",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.openaiCodexVersionAutoSyncHint",
                      )
                    }}
                  </p>
                  <p
                    v-if="codexSyncedVersionLabel"
                    class="mt-0.5 text-xs text-gray-500 dark:text-gray-400"
                  >
                    {{ codexSyncedVersionLabel }}
                  </p>
                </div>
                <Toggle v-model="form.openai_codex_version_auto_sync_enabled" />
              </div>

              <!-- Claude Code 客户端版本号 -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{
                    t(
                      "admin.settings.gatewayForwarding.claudeCodeClientVersion",
                    )
                  }}
                </label>
                <input
                  v-model="form.claude_code_client_version"
                  type="text"
                  class="input w-full font-mono text-sm"
                  placeholder="2.1.280"
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{
                    t(
                      "admin.settings.gatewayForwarding.claudeCodeClientVersionHint",
                    )
                  }}
                </p>
              </div>

              <!-- Claude Code 版本号自动同步 -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.claudeCodeVersionAutoSync",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.claudeCodeVersionAutoSyncHint",
                      )
                    }}
                  </p>
                  <p
                    v-if="claudeSyncedVersionLabel"
                    class="mt-0.5 text-xs text-gray-500 dark:text-gray-400"
                  >
                    {{ claudeSyncedVersionLabel }}
                  </p>
                </div>
                <Toggle v-model="form.claude_code_version_auto_sync_enabled" />
              </div>

            </div>
          </div>
</template>
