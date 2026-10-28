<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'

import type { DeepReadonly } from 'vue'
import type { ExpiryNotificationDraft } from './expiryNotificationModel'
const props = defineProps<{ modelValue: DeepReadonly<ExpiryNotificationDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<ExpiryNotificationDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as ExpiryNotificationDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof ExpiryNotificationDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<ExpiryNotificationDraft> = {}
  for (const key of Object.keys(value) as (keyof ExpiryNotificationDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })


const { t } = useI18n();
</script>

<template>
<div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h3 class="text-base font-medium text-gray-900 dark:text-white">
                {{ t("admin.settings.subscriptionExpiryNotify.title") }}
              </h3>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.subscriptionExpiryNotify.description") }}
              </p>
            </div>
            <div class="px-6 py-6">
              <div class="flex items-center justify-between gap-4">
                <div>
                  <label
                    class="mb-0 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.subscriptionExpiryNotify.enabled") }}
                  </label>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.subscriptionExpiryNotify.enabledHint") }}
                  </p>
                </div>
                <Toggle v-model="form.subscription_expiry_notify_enabled" />
              </div>
            </div>
          </div>
</template>
