<script setup lang="ts">
import { usePricingSettingsServices, type SiteBillingMode } from './ports'
const services = usePricingSettingsServices()
import { toRaw } from 'vue'
import { computed, reactive, watch, type DeepReadonly } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/shared/ui/Select.vue'
import type { SelectOption } from '@/shared/ui/Select.vue'



import type { BillingModeDraft } from './billingModeModel'
const props = defineProps<{ modelValue: DeepReadonly<BillingModeDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<BillingModeDraft>] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as BillingModeDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof BillingModeDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<BillingModeDraft> = {}
  for (const key of Object.keys(value) as (keyof BillingModeDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })


const { t } = useI18n();

const siteBillingModeOptions = computed<SelectOption[]>(() => services.modes.map(mode => ({ value: mode.value, label: t(mode.labelKey) })))
const siteBillingMode = computed<SiteBillingMode>({ get: () => services.resolve(form), set: value => Object.assign(form, services.patch(value)) })
const siteBillingModeHint = computed(() => t(services.modes.find(mode => mode.value === siteBillingMode.value)?.hintKey || 'admin.settings.features.siteBillingMode.description'))
</script>

<template>
<div class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('admin.settings.features.siteBillingMode.title') }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.settings.features.siteBillingMode.description') }}
            </p>
          </div>
          <div class="space-y-5 p-6">
            <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div class="min-w-0">
                <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.settings.features.siteBillingMode.label') }}
                </label>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ siteBillingModeHint }}
                </p>
              </div>
              <div class="w-full shrink-0 sm:w-56">
                <Select
                  :modelValue="siteBillingMode"
                  :options="siteBillingModeOptions"
                  @update:modelValue="siteBillingMode = $event as SiteBillingMode"
                />
              </div>
            </div>
          </div>
        </div>
</template>
