<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useSystemRequests, isSystemRequestCancelled } from '../queries/useSystemRequests'
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { useSystemSettingsServices } from './ports'
const services = useSystemSettingsServices()
const requests = useSystemRequests(services.scope)


const settingsStepUp = services.createStepUp()
const { t } = useI18n();

const adminApiKeyLoading = ref(true);

const adminApiKeyExists = ref(false);

const adminApiKeyMasked = ref("");

const adminApiKeyOperating = ref(false);

const newAdminApiKey = ref("");

async function loadAdminApiKey() {
  const request = requests.begin('status')
  if (!request.isCurrent()) return
  adminApiKeyLoading.value = true
  try {
    const status = await request.wait(signal => services.control.getAdminApiKey({ signal }))
    adminApiKeyExists.value = status.exists
    adminApiKeyMasked.value = status.masked_key
  } catch { /* Non-critical status lookup; a stale request never changes the panel. */ }
  finally { if (request.isCurrent()) adminApiKeyLoading.value = false }
}

async function createAdminApiKey() {
  if (!adminApiKeyExists.value || adminApiKeyOperating.value) return
  const request = requests.begin('key-command')
  if (!request.isCurrent()) return
  adminApiKeyOperating.value = true
  try {
    const result = await settingsStepUp.run(() => request.wait(signal => services.control.regenerateAdminApiKey({ signal })))
    request.check()
    newAdminApiKey.value = result.key
    adminApiKeyExists.value = true
    adminApiKeyMasked.value = result.key.substring(0, 10) + '...' + result.key.slice(-4)
    services.notifications.showSuccess(t('admin.settings.adminApiKey.keyGenerated'))
  } catch (error: unknown) {
    if (!request.isCurrent() || isSystemRequestCancelled(error) || services.isStepUpCancelled(error)) return
    services.notifications.showError(extractApiErrorMessage(error, t('common.error')))
  } finally { if (request.isCurrent()) adminApiKeyOperating.value = false }
}

async function regenerateAdminApiKey() {
  if (adminApiKeyOperating.value || !confirm(t('admin.settings.adminApiKey.regenerateConfirm'))) return
  await createAdminApiKey()
}

async function deleteAdminApiKey() {
  if (adminApiKeyOperating.value || !confirm(t('admin.settings.adminApiKey.deleteConfirm'))) return
  const request = requests.begin('key-command')
  if (!request.isCurrent()) return
  adminApiKeyOperating.value = true
  try {
    await settingsStepUp.run(() => request.wait(signal => services.control.deleteAdminApiKey({ signal })))
    request.check()
    adminApiKeyExists.value = false
    adminApiKeyMasked.value = ''; newAdminApiKey.value = ''
    services.notifications.showSuccess(t('admin.settings.adminApiKey.keyDeleted'))
  } catch (error: unknown) {
    if (!request.isCurrent() || isSystemRequestCancelled(error) || services.isStepUpCancelled(error)) return
    services.notifications.showError(extractApiErrorMessage(error, t('common.error')))
  } finally { if (request.isCurrent()) adminApiKeyOperating.value = false }
}

async function copyNewKey() {
  const request = requests.begin('copy')
  if (!request.isCurrent() || !newAdminApiKey.value) return
  try {
    await request.wait(() => navigator.clipboard.writeText(newAdminApiKey.value))
    services.notifications.showSuccess(t('admin.settings.adminApiKey.keyCopied'))
  } catch (error: unknown) {
    if (request.isCurrent() && !isSystemRequestCancelled(error)) services.notifications.showError(t('common.copyFailed'))
  }
}
requests.onInvalidate(() => {
  settingsStepUp.onCancel()
  adminApiKeyLoading.value = false; adminApiKeyOperating.value = false
  adminApiKeyExists.value = false; adminApiKeyMasked.value = ''; newAdminApiKey.value = ''
})
watch(services.scope, scope => { if (scope !== null) void loadAdminApiKey() })
onMounted(loadAdminApiKey)
</script>

<template>
<details v-if="adminApiKeyExists" class="card">
            <summary class="cursor-pointer px-6 py-4 font-medium">{{ t("adminAccessKeys.legacy") }}</summary>
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.adminApiKey.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.adminApiKey.description") }}
              </p>
            </div>
            <div class="space-y-4 p-6">
              <!-- Security Warning -->
              <div
                class="rounded-lg border border-amber-200 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-900/20"
              >
                <div class="flex items-start">
                  <Icon
                    name="exclamationTriangle"
                    size="md"
                    class="mt-0.5 flex-shrink-0 text-amber-500"
                  />
                  <p class="ml-3 text-sm text-amber-700 dark:text-amber-300">
                    {{ t("admin.settings.adminApiKey.securityWarning") }}
                  </p>
                </div>
              </div>

              <!-- Loading State -->
              <div
                v-if="adminApiKeyLoading"
                class="flex items-center gap-2 text-gray-500"
              >
                <div
                  class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
                ></div>
                {{ t("common.loading") }}
              </div>

              <!-- Key Exists -->
              <div v-else class="space-y-4">
                <div class="flex items-center justify-between">
                  <div>
                    <label
                      class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.adminApiKey.currentKey") }}
                    </label>
                    <code
                      class="rounded bg-gray-100 px-2 py-1 font-mono text-sm text-gray-900 dark:bg-dark-700 dark:text-gray-100"
                    >
                      {{ adminApiKeyMasked }}
                    </code>
                  </div>
                  <div class="flex gap-2">
                    <button
                      type="button"
                      @click="regenerateAdminApiKey"
                      :disabled="adminApiKeyOperating"
                      class="btn btn-secondary btn-sm"
                    >
                      {{
                        adminApiKeyOperating
                          ? t("admin.settings.adminApiKey.regenerating")
                          : t("admin.settings.adminApiKey.regenerate")
                      }}
                    </button>
                    <button
                      type="button"
                      @click="deleteAdminApiKey"
                      :disabled="adminApiKeyOperating"
                      class="btn btn-secondary btn-sm text-red-600 hover:text-red-700 dark:text-red-400"
                    >
                      {{ t("admin.settings.adminApiKey.delete") }}
                    </button>
                  </div>
                </div>

                <!-- Newly Generated Key Display -->
                <div
                  v-if="newAdminApiKey"
                  class="space-y-3 rounded-lg border border-green-200 bg-green-50 p-4 dark:border-green-800 dark:bg-green-900/20"
                >
                  <p
                    class="text-sm font-medium text-green-700 dark:text-green-300"
                  >
                    {{ t("admin.settings.adminApiKey.keyWarning") }}
                  </p>
                  <div class="flex items-center gap-2">
                    <code
                      class="flex-1 select-all break-all rounded border border-green-300 bg-white px-3 py-2 font-mono text-sm dark:border-green-700 dark:bg-dark-800"
                    >
                      {{ newAdminApiKey }}
                    </code>
                    <button
                      type="button"
                      @click="copyNewKey"
                      class="btn btn-primary btn-sm flex-shrink-0"
                    >
                      {{ t("admin.settings.adminApiKey.copyKey") }}
                    </button>
                  </div>
                  <p class="text-xs text-green-600 dark:text-green-400">
                    {{ t("admin.settings.adminApiKey.usage") }}
                  </p>
                </div>
              </div>
            </div>
          </details>
<component :is="services.StepUpDialog" :controller="settingsStepUp" />
</template>
