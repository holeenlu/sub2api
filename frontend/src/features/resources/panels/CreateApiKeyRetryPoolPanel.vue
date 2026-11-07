<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import { type UnwrapRef, type Ref } from 'vue'
import { usePanelField } from '../model/usePanelField'

const props = defineProps<{
  poolModeEnabled: UnwrapRef<Ref<boolean, boolean>>
  poolModeRetryCount: UnwrapRef<Ref<number, number>>
  MAX_POOL_MODE_RETRY_COUNT: UnwrapRef<10>
  DEFAULT_POOL_MODE_RETRY_COUNT: UnwrapRef<3>
  poolModeRetryStatusCodesInput: UnwrapRef<Ref<string, string>>
  DEFAULT_POOL_MODE_RETRY_STATUS_CODES: UnwrapRef<number[]>
}>()

const emit = defineEmits<{
  'update:poolModeEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:poolModeRetryCount': [value: UnwrapRef<Ref<number, number>>]
  'update:poolModeRetryStatusCodesInput': [value: UnwrapRef<Ref<string, string>>]
}>()

const { t } = useI18n()

const poolModeEnabled = usePanelField(() => props.poolModeEnabled, value => emit('update:poolModeEnabled', value))

const poolModeRetryCount = usePanelField(() => props.poolModeRetryCount, value => emit('update:poolModeRetryCount', value))

const MAX_POOL_MODE_RETRY_COUNT = computed(() => props.MAX_POOL_MODE_RETRY_COUNT)

const DEFAULT_POOL_MODE_RETRY_COUNT = computed(() => props.DEFAULT_POOL_MODE_RETRY_COUNT)

const poolModeRetryStatusCodesInput = usePanelField(() => props.poolModeRetryStatusCodesInput, value => emit('update:poolModeRetryStatusCodesInput', value))

const DEFAULT_POOL_MODE_RETRY_STATUS_CODES = computed(() => props.DEFAULT_POOL_MODE_RETRY_STATUS_CODES)
</script>
<template>
<div class="border-t border-gray-200 pt-4 dark:border-dark-600">
          <div class="mb-3 flex items-center justify-between">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.poolMode') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.poolModeHint') }}
              </p>
            </div>
            <button
              type="button"
              @click="poolModeEnabled = !poolModeEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                poolModeEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  poolModeEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <div v-if="poolModeEnabled" class="rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
            <p class="text-xs text-blue-700 dark:text-blue-400">
              <Icon name="exclamationCircle" size="sm" class="mr-1 inline" :stroke-width="2" />
              {{ t('admin.accounts.poolModeInfo') }}
            </p>
          </div>
          <div v-if="poolModeEnabled" class="mt-3">
            <label class="input-label">{{ t('admin.accounts.poolModeRetryCount') }}</label>
            <input
              v-model.number="poolModeRetryCount"
              type="number"
              min="0"
              :max="MAX_POOL_MODE_RETRY_COUNT"
              step="1"
              class="input"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{
                t('admin.accounts.poolModeRetryCountHint', {
                  default: DEFAULT_POOL_MODE_RETRY_COUNT,
                  max: MAX_POOL_MODE_RETRY_COUNT
                })
              }}
            </p>
          </div>
          <div v-if="poolModeEnabled" class="mt-3">
            <label class="input-label">{{ t('admin.accounts.poolModeRetryStatusCodes') }}</label>
            <input
              v-model="poolModeRetryStatusCodesInput"
              type="text"
              class="input"
              :placeholder="DEFAULT_POOL_MODE_RETRY_STATUS_CODES.join(', ')"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.poolModeRetryStatusCodesHint', { default: DEFAULT_POOL_MODE_RETRY_STATUS_CODES.join(', ') }) }}
            </p>
          </div>
        </div>
</template>
