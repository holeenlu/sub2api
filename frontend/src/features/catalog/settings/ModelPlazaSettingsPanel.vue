<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'

import type { DeepReadonly } from 'vue'
import type { ModelPlazaDraft } from './modelPlazaModel'
const props = defineProps<{ modelValue: DeepReadonly<ModelPlazaDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<ModelPlazaDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as ModelPlazaDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof ModelPlazaDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<ModelPlazaDraft> = {}
  for (const key of Object.keys(value) as (keyof ModelPlazaDraft)[]) {
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
              {{ t('admin.settings.features.modelPlaza.title') }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.settings.features.modelPlaza.description') }}
            </p>
          </div>
          <div class="space-y-5 p-6">
            <div class="flex items-center justify-between">
              <div>
                <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.settings.features.modelPlaza.enabled') }}
                </label>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.settings.features.modelPlaza.enabledHint') }}
                </p>
              </div>
              <Toggle v-model="form.model_plaza_enabled" />
            </div>

            <div v-if="form.model_plaza_enabled" class="flex items-center justify-between">
              <div>
                <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.settings.features.modelPlaza.requireAuth') }}
                </label>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.settings.features.modelPlaza.requireAuthHint') }}
                </p>
              </div>
              <Toggle v-model="form.model_plaza_require_auth" />
            </div>

            <div v-if="form.model_plaza_enabled">
              <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.settings.features.modelPlaza.priceDescription') }}
              </label>
              <p class="mb-2 mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.features.modelPlaza.priceDescriptionHint') }}
              </p>
              <textarea
                v-model="form.model_plaza_description"
                rows="6"
                class="input font-mono text-sm"
              ></textarea>
            </div>
          </div>
        </div>
</template>
