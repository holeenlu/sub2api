<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TempUnschedulableScope } from '@/features/resources/api/accountObservationTypes';
import Icon from '@/shared/ui/icons/Icon.vue'
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { TempUnschedRuleForm } from '../model/createEditorTypes'


const props = defineProps<{
  tempUnschedEnabled: UnwrapRef<Ref<boolean, boolean>>
  tempUnschedPresets: UnwrapRef<ComputedRef<{ label: string; rule: { error_code: number; keywords: string; duration_minutes: number; description: string; scope: "model" } }[]>>
  addTempUnschedRule: UnwrapRef<(preset?: TempUnschedRuleForm) => void>
  tempUnschedRules: UnwrapRef<Ref<{ error_code: number | null; keywords: string; duration_minutes: number | null; description: string; scope: TempUnschedulableScope }[], TempUnschedRuleForm[] | { error_code: number | null; keywords: string; duration_minutes: number | null; description: string; scope: TempUnschedulableScope }[]>>
  getTempUnschedRuleKey: UnwrapRef<(item: TempUnschedRuleForm) => string>
  moveTempUnschedRule: UnwrapRef<(index: number, direction: number) => void>
  removeTempUnschedRule: UnwrapRef<(index: number) => void>
  tempUnschedScopeOptions: UnwrapRef<ComputedRef<{ value: TempUnschedulableScope; label: string; description: string }[]>>
}>()

const emit = defineEmits<{
  'update:tempUnschedEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
}>()

const { t } = useI18n()

const tempUnschedEnabled = usePanelField(() => props.tempUnschedEnabled, value => emit('update:tempUnschedEnabled', value))

const tempUnschedPresets = computed(() => props.tempUnschedPresets)

const addTempUnschedRule = computed(() => props.addTempUnschedRule)

const tempUnschedRules = computed(() => props.tempUnschedRules)

const getTempUnschedRuleKey = computed(() => props.getTempUnschedRuleKey)

const moveTempUnschedRule = computed(() => props.moveTempUnschedRule)

const removeTempUnschedRule = computed(() => props.removeTempUnschedRule)

const tempUnschedScopeOptions = computed(() => props.tempUnschedScopeOptions)
</script>

<template>
<div class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4">
        <div class="mb-3 flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.tempUnschedulable.title') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.tempUnschedulable.hint') }}
            </p>
          </div>
          <button
            type="button"
            @click="tempUnschedEnabled = !tempUnschedEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              tempUnschedEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                tempUnschedEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>

        <div v-if="tempUnschedEnabled" class="space-y-3">
          <div class="rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
              <p class="text-xs text-blue-700 dark:text-blue-400">
                <Icon name="exclamationTriangle" size="sm" class="mr-1 inline" :stroke-width="2" />
                {{ t('admin.accounts.tempUnschedulable.notice') }}
              </p>
            </div>

          <div class="flex flex-wrap gap-2">
            <button
              v-for="preset in tempUnschedPresets"
              :key="preset.label"
              type="button"
              @click="addTempUnschedRule(preset.rule)"
              class="rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-300 dark:hover:bg-dark-500"
            >
              + {{ preset.label }}
            </button>
          </div>

          <div v-if="tempUnschedRules.length > 0" class="space-y-3">
            <div
              v-for="(rule, index) in tempUnschedRules"
              :key="getTempUnschedRuleKey(rule)"
              class="rounded-lg border border-gray-200 p-3 dark:border-dark-600"
            >
              <div class="mb-2 flex items-center justify-between">
                <span class="text-xs font-medium text-gray-500 dark:text-gray-400">
                  {{ t('admin.accounts.tempUnschedulable.ruleIndex', { index: index + 1 }) }}
                </span>
                <div class="flex items-center gap-2">
                  <button
                    type="button"
                    :disabled="index === 0"
                    @click="moveTempUnschedRule(index, -1)"
                    class="rounded p-1 text-gray-400 transition-colors hover:text-gray-600 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:text-gray-200"
                  >
                    <Icon name="chevronUp" size="sm" :stroke-width="2" />
                  </button>
                  <button
                    type="button"
                    :disabled="index === tempUnschedRules.length - 1"
                    @click="moveTempUnschedRule(index, 1)"
                    class="rounded p-1 text-gray-400 transition-colors hover:text-gray-600 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:text-gray-200"
                  >
                    <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
                    </svg>
                  </button>
                  <button
                    type="button"
                    @click="removeTempUnschedRule(index)"
                    class="rounded p-1 text-red-500 transition-colors hover:text-red-600"
                  >
                    <Icon name="x" size="sm" :stroke-width="2" />
                  </button>
                </div>
              </div>

              <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div>
                  <label class="input-label">{{ t('admin.accounts.tempUnschedulable.errorCode') }}</label>
                  <input
                    v-model.number="rule.error_code"
                    type="number"
                    min="100"
                    max="599"
                    class="input"
                    :placeholder="t('admin.accounts.tempUnschedulable.errorCodePlaceholder')"
                  />
                </div>
                <div>
                  <label class="input-label">{{ t('admin.accounts.tempUnschedulable.durationMinutes') }}</label>
                  <input
                    v-model.number="rule.duration_minutes"
                    type="number"
                    min="1"
                    class="input"
                    :placeholder="t('admin.accounts.tempUnschedulable.durationPlaceholder')"
                  />
                </div>
                <div class="sm:col-span-2">
                  <label class="input-label">{{ t('admin.accounts.tempUnschedulable.scope') }}</label>
                  <div
                    class="grid grid-cols-1 gap-2 sm:grid-cols-2"
                    role="radiogroup"
                    :aria-label="t('admin.accounts.tempUnschedulable.scope')"
                  >
                    <button
                      v-for="option in tempUnschedScopeOptions"
                      :key="option.value"
                      type="button"
                      role="radio"
                      :aria-checked="rule.scope === option.value"
                      :data-testid="`temp-unsched-scope-${option.value}`"
                      @click="rule.scope = option.value"
                      :class="[
                        'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
                        rule.scope === option.value
                          ? 'border-primary-500 bg-primary-50 dark:bg-primary-900/20'
                          : 'border-gray-200 hover:border-gray-400 dark:border-dark-600 dark:hover:border-gray-600'
                      ]"
                    >
                      <span
                        :class="[
                          'flex h-4 w-4 shrink-0 items-center justify-center rounded-full border-2',
                          rule.scope === option.value ? 'border-primary-500' : 'border-gray-300 dark:border-dark-500'
                        ]"
                      >
                        <span v-if="rule.scope === option.value" class="h-1.5 w-1.5 rounded-full bg-primary-500" />
                      </span>
                      <span>
                        <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ option.label }}</span>
                        <span class="text-xs text-gray-500 dark:text-gray-400">{{ option.description }}</span>
                      </span>
                    </button>
                  </div>
                  <p class="input-hint">{{ t('admin.accounts.tempUnschedulable.scopeHint') }}</p>
                </div>
                <div class="sm:col-span-2">
                  <label class="input-label">{{ t('admin.accounts.tempUnschedulable.keywords') }}</label>
                  <input
                    v-model="rule.keywords"
                    type="text"
                    class="input"
                    :placeholder="t('admin.accounts.tempUnschedulable.keywordsPlaceholder')"
                  />
                  <p class="input-hint">{{ t('admin.accounts.tempUnschedulable.keywordsHint') }}</p>
                </div>
                <div class="sm:col-span-2">
                  <label class="input-label">{{ t('admin.accounts.tempUnschedulable.description') }}</label>
                  <input
                    v-model="rule.description"
                    type="text"
                    class="input"
                    :placeholder="t('admin.accounts.tempUnschedulable.descriptionPlaceholder')"
                  />
                </div>
              </div>
            </div>
          </div>

          <button
            type="button"
            @click="addTempUnschedRule()"
            class="w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-sm text-gray-600 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400 dark:hover:text-gray-300"
          >
            <svg
              class="mr-1 inline h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
            </svg>
            {{ t('admin.accounts.tempUnschedulable.addRule') }}
          </button>
        </div>
      </div>
</template>
