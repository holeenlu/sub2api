<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import type { DeepReadonly } from 'vue'
import type { ErrorVisibilityDraft } from './errorVisibilityModel'
const props = defineProps<{ modelValue: DeepReadonly<ErrorVisibilityDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<ErrorVisibilityDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as ErrorVisibilityDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof ErrorVisibilityDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<ErrorVisibilityDraft> = {}
  for (const key of Object.keys(value) as (keyof ErrorVisibilityDraft)[]) {
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
              {{ t('admin.settings.usageRecords.title') }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.settings.usageRecords.description') }}
            </p>
          </div>
          <div class="space-y-4 p-6">
            <!-- User error requests visibility -->
            <div class="flex items-center justify-between">
              <div>
                <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.settings.user_error_view.label') }}
                </label>
                <p class="text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.settings.user_error_view.description') }}
                </p>
              </div>
              <label class="toggle">
                <input v-model="form.allow_user_view_error_requests" type="checkbox" />
                <span class="toggle-slider"></span>
              </label>
            </div>
          </div>
        </div>
</template>
