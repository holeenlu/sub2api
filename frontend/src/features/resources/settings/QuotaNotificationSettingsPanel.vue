<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import Toggle from '@/shared/ui/Toggle.vue'

import type { DeepReadonly } from 'vue'
import type { QuotaNotificationDraft } from './quotaNotificationModel'
const props = defineProps<{ modelValue: DeepReadonly<QuotaNotificationDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<QuotaNotificationDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as QuotaNotificationDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof QuotaNotificationDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<QuotaNotificationDraft> = {}
  for (const key of Object.keys(value) as (keyof QuotaNotificationDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })


const { t } = useI18n();

const addQuotaNotifyEmail = () => {
  if (!form.account_quota_notify_emails) {
    form.account_quota_notify_emails = [];
  }
  form.account_quota_notify_emails.push({
    email: "",
    disabled: false,
    verified: true,
  });
};
</script>

<template>
<div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h3 class="text-base font-medium text-gray-900 dark:text-white">
                {{ t("admin.settings.quotaNotify.title") }}
              </h3>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.quotaNotify.description") }}
              </p>
            </div>
            <div class="px-6 py-6 space-y-4">
              <div class="flex items-center justify-between">
                <label
                  class="mb-0 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >{{ t("admin.settings.quotaNotify.enabled") }}</label
                >
                <Toggle v-model="form.account_quota_notify_enabled" />
              </div>
              <div v-if="form.account_quota_notify_enabled">
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >{{ t("admin.settings.quotaNotify.emails") }}</label
                >
                <div class="space-y-2">
                  <div
                    v-for="(entry, index) in form.account_quota_notify_emails ||
                    []"
                    :key="index"
                    class="flex items-center gap-2"
                  >
                    <label
                      class="relative inline-flex items-center cursor-pointer shrink-0"
                    >
                      <input
                        type="checkbox"
                        :checked="!entry.disabled"
                        @change="entry.disabled = !entry.disabled"
                        class="sr-only peer"
                      />
                      <div
                        class="w-9 h-5 bg-gray-200 peer-focus:outline-none rounded-full peer dark:bg-gray-600 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-4 after:w-4 after:transition-all dark:after:border-gray-500 peer-checked:bg-primary-600"
                      ></div>
                    </label>
                    <input
                      v-model="entry.email"
                      type="email"
                      class="input flex-1"
                      :placeholder="
                        t('admin.settings.quotaNotify.emailPlaceholder')
                      "
                    />
                    <button
                      @click="form.account_quota_notify_emails.splice(index, 1)"
                      class="btn btn-secondary px-2"
                      type="button"
                    >
                      <Icon name="x" size="xs" class="h-4 w-4" />
                    </button>
                  </div>
                  <button
                    @click="addQuotaNotifyEmail"
                    class="btn btn-secondary btn-sm"
                    type="button"
                  >
                    + {{ t("admin.settings.quotaNotify.addEmail") }}
                  </button>
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.quotaNotify.emailsHint") }}
                </p>
              </div>
            </div>
          </div>
</template>
