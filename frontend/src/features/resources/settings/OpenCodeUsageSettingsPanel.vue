<script setup lang="ts">

import { ref, reactive, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { useResourcesSettingsServices } from './ports'
const sectionReady = ref(false), sectionSaving = ref(false), sectionSaved = ref(false), sectionError = ref('')
const services = useResourcesSettingsServices()



const { t } = useI18n();

const opencodeGoUsageLoading = ref(true);

const opencodeGoUsageSaving = ref(false);

const opencodeGoUsageForm = reactive({
  enabled: false,
  interval_minutes: 15,
  debounce_minutes: 1,
});

async function loadOpenCodeGoUsageSettings() {
  sectionReady.value = false; sectionError.value = ''; sectionSaved.value = false

  opencodeGoUsageLoading.value = true;
  try {
    Object.assign(
      opencodeGoUsageForm,
      await services.accounts.getOpenCodeGoUsageSettings(),
    );
  
    sectionReady.value = true
} catch (_error: unknown) {
    sectionError.value = t('admin.settings.failedToLoad')

    // Keep the fail-safe disabled defaults when this optional setting cannot be loaded.
  } finally {
    opencodeGoUsageLoading.value = false;
  }
}

async function saveOpenCodeGoUsageSettings() {
  if (!sectionReady.value || sectionSaving.value) return
  sectionSaving.value = true; sectionSaved.value = false; sectionError.value = ''

  opencodeGoUsageSaving.value = true;
  try {
    const updated = await services.accounts.updateOpenCodeGoUsageSettings({
      ...opencodeGoUsageForm,
    });
    Object.assign(opencodeGoUsageForm, updated);
    services.notifications.showSuccess(t("admin.settings.opencodeGoUsage.saved"));
  
    sectionSaved.value = true
} catch (error: unknown) {
    sectionError.value = t('admin.settings.failedToSave')

    services.notifications.showError(
      extractApiErrorMessage(error, t("admin.settings.opencodeGoUsage.saveFailed")),
    );
  } finally {
    sectionSaving.value = false

    opencodeGoUsageSaving.value = false;
  }
}
onMounted(loadOpenCodeGoUsageSettings)
</script>

<template><section class="space-y-3">
<p v-if="sectionError" role="alert" class="text-sm text-red-600">{{ sectionError }}</p>
<p v-if="sectionSaved" role="status" class="text-sm text-green-600">{{ t('admin.settings.settingsSaved') }}</p>
<button v-if="!sectionReady" type="button" class="btn btn-secondary" @click="loadOpenCodeGoUsageSettings">{{ t('common.tryAgain') }}</button>
<fieldset :disabled="!sectionReady || sectionSaving">

<div class="card" data-testid="opencode-go-usage-global-settings">
            <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.opencodeGoUsage.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.opencodeGoUsage.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <div v-if="opencodeGoUsageLoading" class="flex items-center gap-2 text-gray-500">
                <div class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"></div>
                {{ t("common.loading") }}
              </div>
              <template v-else>
                <div class="flex items-center justify-between gap-4">
                  <div>
                    <label class="font-medium text-gray-900 dark:text-white">
                      {{ t("admin.settings.opencodeGoUsage.enabled") }}
                    </label>
                    <p class="text-sm text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.opencodeGoUsage.enabledHint") }}
                    </p>
                  </div>
                  <Toggle
                    v-model="opencodeGoUsageForm.enabled"
                    :aria-label="t('admin.settings.opencodeGoUsage.enabled')"
                    data-testid="opencode-go-usage-global-enabled"
                  />
                </div>
                <div v-if="opencodeGoUsageForm.enabled" class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700">
                  <div>
                    <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300" for="opencode-go-usage-debounce">
                      {{ t("admin.settings.opencodeGoUsage.debounceMinutes") }}
                    </label>
                    <input
                      id="opencode-go-usage-debounce"
                      v-model.number="opencodeGoUsageForm.debounce_minutes"
                      type="number"
                      min="1"
                      max="60"
                      class="input w-32"
                      data-testid="opencode-go-usage-global-debounce"
                      @keydown.enter.prevent="saveOpenCodeGoUsageSettings"
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.opencodeGoUsage.debounceHint") }}
                    </p>
                  </div>
                  <div>
                    <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300" for="opencode-go-usage-interval">
                      {{ t("admin.settings.opencodeGoUsage.intervalMinutes") }}
                    </label>
                    <input
                      id="opencode-go-usage-interval"
                      v-model.number="opencodeGoUsageForm.interval_minutes"
                      type="number"
                      min="5"
                      max="1440"
                      class="input w-32"
                      data-testid="opencode-go-usage-global-interval"
                      @keydown.enter.prevent="saveOpenCodeGoUsageSettings"
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.opencodeGoUsage.intervalHint") }}
                    </p>
                  </div>
                </div>
                <div class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700">
                  <button
                    type="button"
                    class="btn btn-primary btn-sm"
                    :disabled="opencodeGoUsageSaving"
                    data-testid="opencode-go-usage-global-save"
                    @click="saveOpenCodeGoUsageSettings"
                  >
                    {{ opencodeGoUsageSaving ? t("common.saving") : t("common.save") }}
                  </button>
                </div>
              </template>
            </div>
          </div>

</fieldset>
</section></template>
