<script setup lang="ts">
import { buildApiCallbackUrl } from './callbackUrl'
import { toRaw } from 'vue'
import { reactive, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'
import { useIdentitySettingsServices } from './ports'
const services = useIdentitySettingsServices()

import type { DeepReadonly } from 'vue'
import type { EmailOAuthDraft } from './emailOAuthModel'
const props = defineProps<{ modelValue: DeepReadonly<EmailOAuthDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<EmailOAuthDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as EmailOAuthDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof EmailOAuthDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<EmailOAuthDraft> = {}
  for (const key of Object.keys(value) as (keyof EmailOAuthDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })


const { t } = useI18n();


type EmailOAuthProvider = "github" | "google";

const githubOAuthRedirectUrlSuggestion = computed(() => {
  return buildApiCallbackUrl("/auth/oauth/github/callback");
});

const googleOAuthRedirectUrlSuggestion = computed(() => {
  return buildApiCallbackUrl("/auth/oauth/google/callback");
});

async function setAndCopyEmailOAuthRedirectUrl(provider: EmailOAuthProvider) {
  const url =
    provider === "github"
      ? githubOAuthRedirectUrlSuggestion.value
      : googleOAuthRedirectUrlSuggestion.value;
  if (!url) return;

  if (provider === "github") {
    form.github_oauth_redirect_url = url;
  } else {
    form.google_oauth_redirect_url = url;
  }
  await services.copy(
    url,
    t('ui.callbackUrlSetAndCopied'),
  );
}
</script>

<template>
<div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('ui.emailOauthSignIn') }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{
                  t('ui.afterGithubOrGoogleEmailOauthIsEnabledThe')
                }}
              </p>
            </div>
            <div class="space-y-6 p-6">
              <div class="grid grid-cols-1 gap-6 xl:grid-cols-2">
                <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
                  <div class="flex items-start justify-between gap-4">
                    <div>
                      <h3 class="font-medium text-gray-900 dark:text-white">
                        GitHub
                      </h3>
                      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                        {{
                          t('ui.githubOauthAppNeedsReadUserUserEmailScopes')
                        }}
                      </p>
                    </div>
                    <Toggle v-model="form.github_oauth_enabled" />
                  </div>

                  <div v-if="form.github_oauth_enabled" class="mt-4 space-y-4">
                    <div class="rounded-lg bg-gray-50 px-3 py-2 text-xs text-gray-600 dark:bg-dark-800 dark:text-gray-300">
                        {{ t('ui.githubSetupStart') }}
                        <a
                          data-testid="github-oauth-apps-guide-link"
                          href="https://github.com/settings/developers"
                          target="_blank"
                          rel="noopener noreferrer"
                          class="font-medium text-primary-600 hover:underline dark:text-primary-400"
                        >OAuth Apps</a>
                        {{ t('ui.githubSetupEnd') }}
                    </div>

                    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
                      <div>
                        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">Client ID</label>
                        <input
                          v-model="form.github_oauth_client_id"
                          type="text"
                          class="input font-mono text-sm"
                          placeholder="GitHub OAuth Client ID"
                        />
                      </div>
                      <div>
                        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">Client Secret</label>
                        <input
                          v-model="form.github_oauth_client_secret"
                          type="password"
                          class="input font-mono text-sm"
                          :placeholder="
                            form.github_oauth_client_secret_configured
                              ? t('ui.secretConfiguredLeaveEmptyToKeepTheCurrentValue')
                              : 'GitHub OAuth Client Secret'
                          "
                        />
                      </div>
                    </div>

                    <div>
                      <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                        {{ t('ui.backendCallbackUrl') }}
                      </label>
                      <input
                        v-model="form.github_oauth_redirect_url"
                        type="url"
                        class="input font-mono text-sm"
                        placeholder="https://your-domain.com/api/v1/auth/oauth/github/callback"
                      />
                      <div class="mt-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
                        <button
                          type="button"
                          class="btn btn-secondary btn-sm w-fit"
                          @click="setAndCopyEmailOAuthRedirectUrl('github')"
                        >
                          {{ t('ui.generateAndCopy') }}
                        </button>
                        <code
                          v-if="githubOAuthRedirectUrlSuggestion"
                          class="select-all break-all rounded bg-gray-50 px-2 py-1 font-mono text-xs text-gray-600 dark:bg-dark-800 dark:text-gray-300"
                        >
                          {{ githubOAuthRedirectUrlSuggestion }}
                        </code>
                      </div>
                    </div>

                    <div>
                      <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                        {{ t('ui.frontendCallbackUrl') }}
                      </label>
                      <input
                        v-model="form.github_oauth_frontend_redirect_url"
                        type="text"
                        class="input font-mono text-sm"
                        placeholder="/auth/oauth/callback"
                      />
                    </div>
                  </div>
                </div>

                <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
                  <div class="flex items-start justify-between gap-4">
                    <div>
                      <h3 class="font-medium text-gray-900 dark:text-white">
                        Google
                      </h3>
                      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                        {{
                          t('ui.googleOauthClientNeedsOpenidEmailProfileScopesAnd')
                        }}
                      </p>
                    </div>
                    <Toggle v-model="form.google_oauth_enabled" />
                  </div>

                  <div v-if="form.google_oauth_enabled" class="mt-4 space-y-4">
                    <div class="rounded-lg bg-gray-50 px-3 py-2 text-xs text-gray-600 dark:bg-dark-800 dark:text-gray-300">
                      {{
                        t('ui.setupGuideGoogleCloudConsoleApisServicesOauthConsent')
                      }}
                    </div>

                    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
                      <div>
                        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">Client ID</label>
                        <input
                          v-model="form.google_oauth_client_id"
                          type="text"
                          class="input font-mono text-sm"
                          placeholder="Google OAuth Client ID"
                        />
                      </div>
                      <div>
                        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">Client Secret</label>
                        <input
                          v-model="form.google_oauth_client_secret"
                          type="password"
                          class="input font-mono text-sm"
                          :placeholder="
                            form.google_oauth_client_secret_configured
                              ? t('ui.secretConfiguredLeaveEmptyToKeepTheCurrentValue')
                              : 'Google OAuth Client Secret'
                          "
                        />
                      </div>
                    </div>

                    <div>
                      <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                        {{ t('ui.backendCallbackUrl') }}
                      </label>
                      <input
                        v-model="form.google_oauth_redirect_url"
                        type="url"
                        class="input font-mono text-sm"
                        placeholder="https://your-domain.com/api/v1/auth/oauth/google/callback"
                      />
                      <div class="mt-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
                        <button
                          type="button"
                          class="btn btn-secondary btn-sm w-fit"
                          @click="setAndCopyEmailOAuthRedirectUrl('google')"
                        >
                          {{ t('ui.generateAndCopy') }}
                        </button>
                        <code
                          v-if="googleOAuthRedirectUrlSuggestion"
                          class="select-all break-all rounded bg-gray-50 px-2 py-1 font-mono text-xs text-gray-600 dark:bg-dark-800 dark:text-gray-300"
                        >
                          {{ googleOAuthRedirectUrlSuggestion }}
                        </code>
                      </div>
                    </div>

                    <div>
                      <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                        {{ t('ui.frontendCallbackUrl') }}
                      </label>
                      <input
                        v-model="form.google_oauth_frontend_redirect_url"
                        type="text"
                        class="input font-mono text-sm"
                        placeholder="/auth/oauth/callback"
                      />
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
</template>
