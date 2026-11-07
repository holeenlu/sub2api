<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'
import OpenAIFastPolicyUserSelector from './OpenAIFastPolicyUserSelector.vue'

import type { DeepReadonly } from 'vue'
import type { RiskControlDraft } from './riskControlModel'
const props = defineProps<{ modelValue: DeepReadonly<RiskControlDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<RiskControlDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as RiskControlDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof RiskControlDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<RiskControlDraft> = {}
  for (const key of Object.keys(value) as (keyof RiskControlDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })


const { t } = useI18n();

const riskControlAllowlistedUserIds = computed<number[]>({
  get: () => Array.from(new Set(
    form.cyber_policy_user_allowlist
      .split(/[,\s]+/)
      .map(Number)
      .filter((id) => Number.isSafeInteger(id) && id > 0),
  )),
  set: (ids) => {
    form.cyber_policy_user_allowlist = ids.join(",");
  },
});
</script>

<template>
<div class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('admin.settings.features.riskControl.title') }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.settings.features.riskControl.description') }}
            </p>
            <p class="mt-1.5 text-xs">
              <router-link
                to="/admin/risk-control"
                class="inline-flex items-center gap-1 text-primary-600 hover:underline dark:text-primary-400"
              >
                {{ t('admin.settings.features.riskControl.configureLink') }}
                <span aria-hidden="true">→</span>
              </router-link>
            </p>
          </div>
          <div class="space-y-5 p-6">
            <div class="flex items-center justify-between">
              <div>
                <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.settings.features.riskControl.enabled') }}
                </label>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.settings.features.riskControl.enabledHint') }}
                </p>
              </div>
              <Toggle v-model="form.risk_control_enabled" />
            </div>

            <div>
              <label class="input-label">
                {{ t('admin.settings.features.riskControl.riskControlUserAllowlist') }}
              </label>
              <OpenAIFastPolicyUserSelector
                v-model="riskControlAllowlistedUserIds"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.features.riskControl.riskControlUserAllowlistHint') }}
              </p>
            </div>

            <div class="flex items-center justify-between">
              <div>
                <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.settings.features.riskControl.cyberSessionBlock') }}
                </label>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.settings.features.riskControl.cyberSessionBlockHint') }}
                </p>
              </div>
              <Toggle v-model="form.cyber_session_block_enabled" />
            </div>

            <div v-if="form.cyber_session_block_enabled">
              <label class="input-label">
                {{ t('admin.settings.features.riskControl.cyberSessionBlockTTL') }}
                <span class="text-red-500">*</span>
              </label>
              <input
                v-model.number="form.cyber_session_block_ttl_seconds"
                type="number"
                min="1"
                class="input"
              />
            </div>
          </div>
        </div>
</template>
