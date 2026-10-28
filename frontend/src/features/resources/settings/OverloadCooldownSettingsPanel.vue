<script setup lang="ts">

import { ref, reactive, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { useResourcesSettingsServices } from './ports'
const sectionReady = ref(false), sectionSaving = ref(false), sectionSaved = ref(false), sectionError = ref('')
const services = useResourcesSettingsServices()



const { t } = useI18n();

const overloadCooldownLoading = ref(true);

const overloadCooldownSaving = ref(false);

const overloadCooldownForm = reactive({
  enabled: true,
  cooldown_minutes: 10,
});

async function loadOverloadCooldownSettings() {
  sectionReady.value = false; sectionError.value = ''; sectionSaved.value = false

  overloadCooldownLoading.value = true;
  try {
    const settings = await services.control.getOverloadCooldownSettings();
    Object.assign(overloadCooldownForm, settings);
  
    sectionReady.value = true
} catch (_error: unknown) {
    sectionError.value = t('admin.settings.failedToLoad')

    // Keep this section unavailable until a successful read.
  } finally {
    overloadCooldownLoading.value = false;
  }
}

async function saveOverloadCooldownSettings() {
  if (!sectionReady.value || sectionSaving.value) return
  sectionSaving.value = true; sectionSaved.value = false; sectionError.value = ''

  overloadCooldownSaving.value = true;
  try {
    const updated = await services.control.updateOverloadCooldownSettings({
      enabled: overloadCooldownForm.enabled,
      cooldown_minutes: overloadCooldownForm.cooldown_minutes,
    });
    Object.assign(overloadCooldownForm, updated);
    services.notifications.showSuccess(t("admin.settings.overloadCooldown.saved"));
  
    sectionSaved.value = true
} catch (error: unknown) {
    sectionError.value = t('admin.settings.failedToSave')

    services.notifications.showError(
      extractApiErrorMessage(
        error,
        t("admin.settings.overloadCooldown.saveFailed"),
      ),
    );
  } finally {
    sectionSaving.value = false

    overloadCooldownSaving.value = false;
  }
}
onMounted(loadOverloadCooldownSettings)
</script>

<template><section class="space-y-3">
<p v-if="sectionError" role="alert" class="text-sm text-red-600">{{ sectionError }}</p>
<p v-if="sectionSaved" role="status" class="text-sm text-green-600">{{ t('admin.settings.settingsSaved') }}</p>
<button v-if="!sectionReady" type="button" class="btn btn-secondary" @click="loadOverloadCooldownSettings">{{ t('common.tryAgain') }}</button>
<fieldset :disabled="!sectionReady || sectionSaving">

<div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.overloadCooldown.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.overloadCooldown.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <div
                v-if="overloadCooldownLoading"
                class="flex items-center gap-2 text-gray-500"
              >
                <div
                  class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
                ></div>
                {{ t("common.loading") }}
              </div>

              <template v-else>
                <div class="flex items-center justify-between">
                  <div>
                    <label class="font-medium text-gray-900 dark:text-white">{{
                      t("admin.settings.overloadCooldown.enabled")
                    }}</label>
                    <p class="text-sm text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.overloadCooldown.enabledHint") }}
                    </p>
                  </div>
                  <Toggle v-model="overloadCooldownForm.enabled" />
                </div>

                <div
                  v-if="overloadCooldownForm.enabled"
                  class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.overloadCooldown.cooldownMinutes") }}
                    </label>
                    <input
                      v-model.number="overloadCooldownForm.cooldown_minutes"
                      type="number"
                      min="1"
                      max="120"
                      class="input w-32"
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{
                        t("admin.settings.overloadCooldown.cooldownMinutesHint")
                      }}
                    </p>
                  </div>
                </div>

                <div
                  class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <button
                    type="button"
                    @click="saveOverloadCooldownSettings"
                    :disabled="overloadCooldownSaving"
                    class="btn btn-primary btn-sm"
                  >
                    <svg
                      v-if="overloadCooldownSaving"
                      class="mr-1 h-4 w-4 animate-spin"
                      fill="none"
                      viewBox="0 0 24 24"
                    >
                      <circle
                        class="opacity-25"
                        cx="12"
                        cy="12"
                        r="10"
                        stroke="currentColor"
                        stroke-width="4"
                      ></circle>
                      <path
                        class="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                      ></path>
                    </svg>
                    {{
                      overloadCooldownSaving
                        ? t("common.saving")
                        : t("common.save")
                    }}
                  </button>
                </div>
              </template>
            </div>
          </div>

</fieldset>
</section></template>
