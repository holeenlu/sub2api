<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, watch, type DeepReadonly } from 'vue'
import type { UserDefaultsDraft } from './userDefaultsModel'
const props = defineProps<{ modelValue: DeepReadonly<UserDefaultsDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<UserDefaultsDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as UserDefaultsDraft)
watch(() => props.modelValue, value => { for (const key of Object.keys(value) as (keyof UserDefaultsDraft)[]) { if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) }) } }, { deep: true })
watch(form, value => { const patch: Partial<UserDefaultsDraft> = {}; for (const key of Object.keys(value) as (keyof UserDefaultsDraft)[]) { if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) }) } if (Object.keys(patch).length) emit('patch', patch) }, { deep: true })
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>

<template>
<section class="card p-6"><div class="grid grid-cols-1 gap-6 md:grid-cols-2">
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.defaults.defaultBalance") }}
                  </label>
                  <input
                    v-model.number="form.default_balance"
                    type="number"
                    step="0.01"
                    min="0"
                    class="input"
                    placeholder="0.00"
                  />
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.defaults.defaultBalanceHint") }}
                  </p>
                </div>
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.defaults.defaultConcurrency") }}
                  </label>
                  <input
                    v-model.number="form.default_concurrency"
                    type="number"
                    min="1"
                    class="input"
                    placeholder="1"
                  />
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.defaults.defaultConcurrencyHint") }}
                  </p>
                </div>
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.defaults.defaultUserRpmLimit") }}
                  </label>
                  <input
                    v-model.number="form.default_user_rpm_limit"
                    type="number"
                    min="0"
                    step="1"
                    class="input"
                    placeholder="0"
                  />
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.defaults.defaultUserRpmLimitHint") }}
                  </p>
                </div>
              </div></section>
</template>
