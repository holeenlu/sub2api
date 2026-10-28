<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelWhitelistSelector from '@/features/resources/components/ModelWhitelistSelector.vue'

import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { ModelMapping, CreateResourceForm } from '../model/createEditorTypes'


const props = defineProps<{
  form: Pick<CreateResourceForm, "platform">
  isOAuthFlow: UnwrapRef<ComputedRef<boolean>>
  isOpenAIModelRestrictionDisabled: UnwrapRef<ComputedRef<boolean>>
  modelRestrictionMode: UnwrapRef<Ref<"whitelist" | "mapping", "whitelist" | "mapping">>
  allowedModels: UnwrapRef<Ref<string[], string[]>>
  modelMappings: UnwrapRef<Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>>
  getModelMappingKey: UnwrapRef<(item: ModelMapping) => string>
  removeModelMapping: UnwrapRef<(index: number) => void>
  addModelMapping: UnwrapRef<() => void>
  presetMappings: UnwrapRef<ComputedRef<{ label: string; from: string; to: string; color: string }[]>>
  addPresetMapping: UnwrapRef<(from: string, to: string) => void>
}>()

const emit = defineEmits<{
  'update:modelRestrictionMode': [value: UnwrapRef<Ref<"whitelist" | "mapping", "whitelist" | "mapping">>]
  'update:allowedModels': [value: UnwrapRef<Ref<string[], string[]>>]
}>()

const { t } = useI18n()

const form = computed(() => props.form)

const isOAuthFlow = computed(() => props.isOAuthFlow)

const isOpenAIModelRestrictionDisabled = computed(() => props.isOpenAIModelRestrictionDisabled)

const modelRestrictionMode = usePanelField(() => props.modelRestrictionMode, value => emit('update:modelRestrictionMode', value))

const allowedModels = usePanelField(() => props.allowedModels, value => emit('update:allowedModels', value))

const modelMappings = computed(() => props.modelMappings)



const getModelMappingKey = computed(() => props.getModelMappingKey)

const removeModelMapping = computed(() => props.removeModelMapping)

const addModelMapping = computed(() => props.addModelMapping)

const presetMappings = computed(() => props.presetMappings)

const addPresetMapping = computed(() => props.addPresetMapping)
</script>

<template>
<div
        v-if="(form.platform === 'openai' || form.platform === 'grok') && isOAuthFlow"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <label class="input-label">{{ t('admin.accounts.modelRestriction') }}</label>

        <div
          v-if="isOpenAIModelRestrictionDisabled"
          class="mb-3 rounded-lg bg-amber-50 p-3 dark:bg-amber-900/20"
        >
          <p class="text-xs text-amber-700 dark:text-amber-400">
            {{ t('admin.accounts.openai.modelRestrictionDisabledByPassthrough') }}
          </p>
        </div>

        <template v-else>
          <!-- Mode Toggle -->
          <div class="mb-4 flex gap-2">
            <button
              type="button"
              @click="modelRestrictionMode = 'whitelist'"
              :class="[
                'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                modelRestrictionMode === 'whitelist'
                  ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
            >
              {{ t('admin.accounts.modelWhitelist') }}
            </button>
            <button
              type="button"
              @click="modelRestrictionMode = 'mapping'"
              :class="[
                'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                modelRestrictionMode === 'mapping'
                  ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
            >
              {{ t('admin.accounts.modelMapping') }}
            </button>
          </div>

          <!-- Whitelist Mode -->
          <div v-if="modelRestrictionMode === 'whitelist'">
            <ModelWhitelistSelector
              v-model="allowedModels"
              :platform="form.platform"
            />
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.selectedModels', { count: allowedModels.length }) }}
              <span v-if="allowedModels.length === 0">{{
                t('admin.accounts.supportsAllModels')
              }}</span>
            </p>
          </div>

          <!-- Mapping Mode -->
          <div v-else>
            <div class="mb-3 rounded-lg bg-purple-50 p-3 dark:bg-purple-900/20">
              <p class="text-xs text-purple-700 dark:text-purple-400">
                {{ t('admin.accounts.mapRequestModels') }}
              </p>
            </div>

            <div v-if="modelMappings.length > 0" class="mb-3 space-y-2">
              <div
                v-for="(mapping, index) in modelMappings"
                :key="'oauth-' + getModelMappingKey(mapping)"
                class="flex items-center gap-2"
              >
                <input
                  v-model="mapping.from"
                  type="text"
                  class="input flex-1"
                  :placeholder="t('admin.accounts.requestModel')"
                />
                <svg
                  class="h-4 w-4 flex-shrink-0 text-gray-400"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M14 5l7 7m0 0l-7 7m7-7H3"
                  />
                </svg>
                <input
                  v-model="mapping.to"
                  type="text"
                  class="input flex-1"
                  :placeholder="t('admin.accounts.actualModel')"
                />
                <button
                  type="button"
                  @click="removeModelMapping(index)"
                  class="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                >
                  <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      stroke-width="2"
                      d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                    />
                  </svg>
                </button>
              </div>
            </div>

            <button
              type="button"
              @click="addModelMapping"
              class="mb-3 w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-gray-600 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400 dark:hover:text-gray-300"
            >
              + {{ t('admin.accounts.addMapping') }}
            </button>

            <!-- Quick Add Buttons -->
            <div class="flex flex-wrap gap-2">
              <button
                v-for="preset in presetMappings"
                :key="'oauth-' + preset.label"
                type="button"
                @click="addPresetMapping(preset.from, preset.to)"
                :class="['rounded-lg px-3 py-1 text-xs transition-colors', preset.color]"
              >
                + {{ preset.label }}
              </button>
            </div>
          </div>
        </template>
      </div>
</template>
