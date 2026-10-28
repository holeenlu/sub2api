<script setup lang="ts">
import { toRaw } from 'vue'
import { ref, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { useSystemSettingsServices } from './ports'
import { useSystemRequests, isSystemRequestCancelled } from '../queries/useSystemRequests'
const services = useSystemSettingsServices()
const requests = useSystemRequests(services.scope)
let resetting = false

import type { DeepReadonly } from 'vue'
import type { SmtpDraft } from './smtpModel'
const props = defineProps<{ modelValue: DeepReadonly<SmtpDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<SmtpDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as SmtpDraft)
watch(() => props.modelValue, value => {
  resetting = true
  try {
    for (const key of Object.keys(value) as (keyof SmtpDraft)[]) {
      if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
    }
  } finally { resetting = false }
}, { deep: true })
watch(form, value => {
  if (resetting || services.scope.value === null) return
  const patch: Partial<SmtpDraft> = {}
  for (const key of Object.keys(value) as (keyof SmtpDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true, flush: 'sync' })


const { t } = useI18n();

const loadFailed = ref(false);

const testingSmtp = ref(false);

const sendingTestEmail = ref(false);

const smtpPasswordManuallyEdited = ref(false);

const testEmailAddress = ref("");

watch(form, () => {
  requests.cancel('test-smtp'); requests.cancel('send-email')
  testingSmtp.value = false; sendingTestEmail.value = false
}, { deep: true, flush: 'sync' })
watch(testEmailAddress, () => { requests.cancel('send-email'); sendingTestEmail.value = false }, { flush: 'sync' })

async function testSmtpConnection() {
  if (testingSmtp.value) return
  const request = requests.begin('test-smtp')
  if (!request.isCurrent()) return
  testingSmtp.value = true;
  try {
    const smtpPasswordForTest = smtpPasswordManuallyEdited.value
      ? form.smtp_password
      : "";
    const result = await request.wait(signal => services.control.testSmtpConnection({
      smtp_host: form.smtp_host,
      smtp_port: form.smtp_port,
      smtp_username: form.smtp_username,
      smtp_password: smtpPasswordForTest,
      smtp_use_tls: form.smtp_use_tls,
    }, { signal }));
    // API returns { message: "..." } on success, errors are thrown as exceptions
    services.notifications.showSuccess(
      result.message || t("admin.settings.smtpConnectionSuccess"),
    );
  } catch (error: unknown) {
    if (!request.isCurrent() || isSystemRequestCancelled(error)) return
    services.notifications.showError(
      extractApiErrorMessage(error, t("admin.settings.failedToTestSmtp")),
    );
  } finally {
    if (request.isCurrent()) testingSmtp.value = false;
  }
}

async function sendTestEmail() {
  if (sendingTestEmail.value) return
  const request = requests.begin('send-email')
  if (!request.isCurrent()) return
  if (!testEmailAddress.value) {
    services.notifications.showError(t("admin.settings.testEmail.enterRecipientHint"));
    return;
  }

  sendingTestEmail.value = true;
  try {
    const smtpPasswordForSend = smtpPasswordManuallyEdited.value
      ? form.smtp_password
      : "";
    const result = await request.wait(signal => services.control.sendTestEmail({
      email: testEmailAddress.value,
      smtp_host: form.smtp_host,
      smtp_port: form.smtp_port,
      smtp_username: form.smtp_username,
      smtp_password: smtpPasswordForSend,
      smtp_from_email: form.smtp_from_email,
      smtp_from_name: form.smtp_from_name,
      smtp_use_tls: form.smtp_use_tls,
    }, { signal }));
    // API returns { message: "..." } on success, errors are thrown as exceptions
    services.notifications.showSuccess(result.message || t("admin.settings.testEmailSent"));
  } catch (error: unknown) {
    if (!request.isCurrent() || isSystemRequestCancelled(error)) return
    services.notifications.showError(
      extractApiErrorMessage(error, t("admin.settings.failedToSendTestEmail")),
    );
  } finally {
    if (request.isCurrent()) sendingTestEmail.value = false;
  }
}
requests.onInvalidate(() => {
  testingSmtp.value = false; sendingTestEmail.value = false
  smtpPasswordManuallyEdited.value = false; testEmailAddress.value = ''
  resetting = true
  form.smtp_password = ''
  resetting = false
})
</script>

<template>
<div v-if="form.email_verify_enabled" class="card">
            <div
              class="flex items-center justify-between border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                  {{ t("admin.settings.smtp.title") }}
                </h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.smtp.description") }}
                </p>
              </div>
              <button
                type="button"
                @click="testSmtpConnection"
                :disabled="testingSmtp || loadFailed"
                class="btn btn-secondary btn-sm"
              >
                <svg
                  v-if="testingSmtp"
                  class="h-4 w-4 animate-spin"
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
                  testingSmtp
                    ? t("admin.settings.smtp.testing")
                    : t("admin.settings.smtp.testConnection")
                }}
              </button>
            </div>
            <div class="space-y-6 p-6">
              <div class="grid grid-cols-1 gap-6 md:grid-cols-2">
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.smtp.host") }}
                  </label>
                  <input
                    v-model="form.smtp_host"
                    type="text"
                    class="input"
                    :placeholder="t('admin.settings.smtp.hostPlaceholder')"
                  />
                </div>
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.smtp.port") }}
                  </label>
                  <input
                    v-model.number="form.smtp_port"
                    type="number"
                    min="1"
                    max="65535"
                    class="input"
                    :placeholder="t('admin.settings.smtp.portPlaceholder')"
                  />
                </div>
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.smtp.username") }}
                  </label>
                  <input
                    v-model="form.smtp_username"
                    type="text"
                    class="input"
                    :placeholder="t('admin.settings.smtp.usernamePlaceholder')"
                  />
                </div>
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.smtp.password") }}
                  </label>
                  <input
                    v-model="form.smtp_password"
                    type="password"
                    class="input"
                    autocomplete="new-password"
                    autocapitalize="off"
                    spellcheck="false"
                    @keydown="smtpPasswordManuallyEdited = true"
                    @paste="smtpPasswordManuallyEdited = true"
                    :placeholder="
                      form.smtp_password_configured
                        ? t('admin.settings.smtp.passwordConfiguredPlaceholder')
                        : t('admin.settings.smtp.passwordPlaceholder')
                    "
                  />
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      form.smtp_password_configured
                        ? t("admin.settings.smtp.passwordConfiguredHint")
                        : t("admin.settings.smtp.passwordHint")
                    }}
                  </p>
                </div>
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.smtp.fromEmail") }}
                  </label>
                  <input
                    v-model="form.smtp_from_email"
                    type="email"
                    class="input"
                    :placeholder="t('admin.settings.smtp.fromEmailPlaceholder')"
                  />
                </div>
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.smtp.fromName") }}
                  </label>
                  <input
                    v-model="form.smtp_from_name"
                    type="text"
                    class="input"
                    :placeholder="t('admin.settings.smtp.fromNamePlaceholder')"
                  />
                </div>
              </div>

              <!-- Use TLS Toggle -->
              <div
                class="flex items-center justify-between border-t border-gray-100 pt-4 dark:border-dark-700"
              >
                <div>
                  <label class="font-medium text-gray-900 dark:text-white">{{
                    t("admin.settings.smtp.useTls")
                  }}</label>
                  <p class="text-sm text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.smtp.useTlsHint") }}
                  </p>
                </div>
                <Toggle v-model="form.smtp_use_tls" />
              </div>
            </div>
          </div>
<div v-if="form.email_verify_enabled" class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.testEmail.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.testEmail.description") }}
              </p>
            </div>
            <div class="p-6">
              <div class="flex items-end gap-4">
                <div class="flex-1">
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.testEmail.recipientEmail") }}
                  </label>
                  <input
                    v-model="testEmailAddress"
                    type="email"
                    class="input"
                    :placeholder="
                      t('admin.settings.testEmail.recipientEmailPlaceholder')
                    "
                  />
                </div>
                <button
                  type="button"
                  @click="sendTestEmail"
                  :disabled="
                    sendingTestEmail || !testEmailAddress || loadFailed
                  "
                  class="btn btn-secondary"
                >
                  <svg
                    v-if="sendingTestEmail"
                    class="h-4 w-4 animate-spin"
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
                    sendingTestEmail
                      ? t("admin.settings.testEmail.sending")
                      : t("admin.settings.testEmail.sendTestEmail")
                  }}
                </button>
              </div>
            </div>
          </div>
</template>
