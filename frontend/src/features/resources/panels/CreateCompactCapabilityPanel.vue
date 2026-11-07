<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OpenAICompactMode } from '@/features/resources/api/accountCredentialTypes';
import Select from '@/shared/ui/Select.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { ModelMapping, CreateResourceForm } from '../model/createEditorTypes'


const props = defineProps<{
  form: Pick<CreateResourceForm, "platform">
  accountCategory: UnwrapRef<Ref<"apikey" | "bedrock" | "service_account" | "oauth-based", "apikey" | "bedrock" | "service_account" | "oauth-based">>
  openAICompactMode: UnwrapRef<Ref<OpenAICompactMode, OpenAICompactMode>>
  openAICompactModeOptions: UnwrapRef<ComputedRef<{ value: string; label: string }[]>>
  openAICompactModelMappings: UnwrapRef<Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>>
  getOpenAICompactModelMappingKey: UnwrapRef<(item: ModelMapping) => string>
  removeOpenAICompactModelMapping: UnwrapRef<(index: number) => void>
  addOpenAICompactModelMapping: UnwrapRef<() => void>
}>()

const emit = defineEmits<{
  'update:openAICompactMode': [value: UnwrapRef<Ref<OpenAICompactMode, OpenAICompactMode>>]
}>()

const { t } = useI18n()

const form = computed(() => props.form)

const accountCategory = computed(() => props.accountCategory)

const openAICompactMode = usePanelField(() => props.openAICompactMode, value => emit('update:openAICompactMode', value))

const openAICompactModeOptions = computed(() => props.openAICompactModeOptions)

const openAICompactModelMappings = computed(() => props.openAICompactModelMappings)

const getOpenAICompactModelMappingKey = computed(() => props.getOpenAICompactModelMappingKey)

const removeOpenAICompactModelMapping = computed(() => props.removeOpenAICompactModelMapping)

const addOpenAICompactModelMapping = computed(() => props.addOpenAICompactModelMapping)
</script>

<template>
<div
        v-if="form.platform === 'openai' && (accountCategory === 'oauth-based' || accountCategory === 'apikey')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.compactMode') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.compactModeDesc') }}
            </p>
          </div>
          <div class="w-44">
            <Select v-model="openAICompactMode" :options="openAICompactModeOptions" />
          </div>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.openai.compactModelMapping') }}</label>
          <p class="input-hint">{{ t('admin.accounts.openai.compactModelMappingDesc') }}</p>
          <div v-if="openAICompactModelMappings.length > 0" class="mb-3 space-y-2">
            <div
              v-for="(mapping, index) in openAICompactModelMappings"
              :key="getOpenAICompactModelMappingKey(mapping)"
              class="flex items-center gap-2"
            >
              <input v-model="mapping.from" type="text" class="input flex-1" :placeholder="t('admin.accounts.fromModel')" />
              <span class="text-gray-400">→</span>
              <input v-model="mapping.to" type="text" class="input flex-1" :placeholder="t('admin.accounts.toModel')" />
              <button type="button" @click="removeOpenAICompactModelMapping(index)" class="text-red-500 hover:text-red-700">
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </div>
          <button type="button" @click="addOpenAICompactModelMapping" class="btn btn-secondary text-sm">
            + {{ t('admin.accounts.addMapping') }}
          </button>
        </div>
      </div>
</template>
