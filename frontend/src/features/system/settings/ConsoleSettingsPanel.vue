<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, watch, type DeepReadonly } from 'vue'
import type { ConsoleDraft } from './consoleModel'
const props = defineProps<{ modelValue: DeepReadonly<ConsoleDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<ConsoleDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as ConsoleDraft)
watch(() => props.modelValue, value => { for (const key of Object.keys(value) as (keyof ConsoleDraft)[]) { if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) }) } }, { deep: true })
watch(form, value => { const patch: Partial<ConsoleDraft> = {}; for (const key of Object.keys(value) as (keyof ConsoleDraft)[]) { if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) }) } if (Object.keys(patch).length) emit('patch', patch) }, { deep: true })
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'
const { t } = useI18n()
const tablePageSizeOptionsInput = ref("10, 20, 50, 100");
const tablePageSizeMin = 5;
const tablePageSizeMax = 1000;
function formatTablePageSizeOptions(options: number[]): string {
  return options.join(", ");
}
function parseTablePageSizeOptionsInput(raw: string): number[] | null {
  const tokens = raw
    .split(",")
    .map((token) => token.trim())
    .filter((token) => token.length > 0);

  if (tokens.length === 0) {
    return null;
  }

  const parsed = tokens.map((token) => Number(token));
  if (parsed.some((value) => !Number.isInteger(value))) {
    return null;
  }

  const deduped = Array.from(new Set(parsed)).sort((a, b) => a - b);
  if (
    deduped.some(
      (value) => value < tablePageSizeMin || value > tablePageSizeMax,
    )
  ) {
    return null;
  }

  return deduped;
}
watch(() => form.table_page_size_options, value => { tablePageSizeOptionsInput.value = formatTablePageSizeOptions(value || [10, 20, 50, 100]) }, { immediate: true })
watch(tablePageSizeOptionsInput, value => { const parsed = parseTablePageSizeOptionsInput(value); if (parsed) form.table_page_size_options = parsed; emit('validity', parsed !== null) })
</script>

<template>
<section class="card space-y-6 p-6">
<section class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
                <div class="flex items-center justify-between gap-4">
                  <div><h3 class="font-medium">{{ t('vendor.portalEnabled') }}</h3><p class="mt-1 text-xs text-gray-500">{{ t('vendor.portalHelp') }}</p></div>
                  <Toggle v-model="form.vendor_console_enabled" />
                </div>
                <label class="block text-sm">{{ t('vendor.portalOrigin') }}<input v-model="form.vendor_portal_origin" type="url" class="input mt-2" placeholder="https://supplier.example.invalid" /></label>
              </section>
<div
                class="flex items-center justify-between rounded-lg border border-amber-200 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-900/20"
              >
                <div>
                  <h3 class="text-sm font-medium text-gray-900 dark:text-white">
                    {{ t("admin.settings.site.backendMode") }}
                  </h3>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.site.backendModeDescription") }}
                  </p>
	                </div>
	                <Toggle v-model="form.backend_mode_enabled" />
	              </div>
<div class="border-t border-gray-100 pt-4 dark:border-dark-700">
                <h3 class="text-sm font-medium text-gray-900 dark:text-white">
                  {{ t("admin.settings.site.tablePreferencesTitle") }}
                </h3>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.site.tablePreferencesDescription") }}
                </p>
                <div class="mt-4 grid grid-cols-1 gap-6 md:grid-cols-2">
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.site.tableDefaultPageSize") }}
                    </label>
                    <input
                      v-model.number="form.table_default_page_size"
                      type="number"
                      min="5"
                      max="1000"
                      step="1"
                      class="input w-40"
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.site.tableDefaultPageSizeHint") }}
                    </p>
                  </div>
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.site.tablePageSizeOptions") }}
                    </label>
                    <input
                      v-model="tablePageSizeOptionsInput"
                      type="text"
                      class="input font-mono text-sm"
                      :placeholder="
                        t('admin.settings.site.tablePageSizeOptionsPlaceholder')
                      "
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.site.tablePageSizeOptionsHint") }}
                    </p>
                  </div>
                </div>
              </div>
</section>
</template>
