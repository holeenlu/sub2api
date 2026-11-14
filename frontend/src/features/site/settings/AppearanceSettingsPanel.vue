<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'
import ImageUpload from '@/shared/ui/ImageUpload.vue'

import type { DeepReadonly } from 'vue'
import type { AppearanceDraft } from './appearanceModel'
const props = defineProps<{ modelValue: DeepReadonly<AppearanceDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<AppearanceDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as AppearanceDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof AppearanceDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<AppearanceDraft> = {}
  for (const key of Object.keys(value) as (keyof AppearanceDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })


const { t } = useI18n();







function addEndpoint() {
  form.custom_endpoints.push({ name: "", endpoint: "", description: "" });
}

function removeEndpoint(index: number) {
  form.custom_endpoints.splice(index, 1);
}






</script>

<template>
<div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.site.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.site.description") }}
              </p>
            </div>
            <div class="space-y-6 p-6">
              
              <!-- Backend Mode -->
              

	              <div class="grid grid-cols-1 gap-6 md:grid-cols-2">
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.site.siteName") }}
                  </label>
                  <input
                    v-model="form.site_name"
                    type="text"
                    class="input"
                    :placeholder="t('admin.settings.site.siteNamePlaceholder')"
                  />
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.site.siteNameHint") }}
                  </p>
                </div>
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.site.siteSubtitle") }}
                  </label>
                  <input
                    v-model="form.site_subtitle"
                    type="text"
                    class="input"
                    :placeholder="
                      t('admin.settings.site.siteSubtitlePlaceholder')
                    "
                  />
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.site.siteSubtitleHint") }}
                  </p>
                </div>
              </div>

              <!-- API Base URL -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.site.apiBaseUrl") }}
                </label>
                <input
                  v-model="form.api_base_url"
                  type="text"
                  class="input font-mono text-sm"
                  :placeholder="t('admin.settings.site.apiBaseUrlPlaceholder')"
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.site.apiBaseUrlHint") }}
                </p>
              </div>

              <!-- Global Table Preferences -->
              

              <!-- Custom Endpoints -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.site.customEndpoints.title") }}
                </label>
                <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.site.customEndpoints.description") }}
                </p>

                <div class="space-y-3">
                  <div
                    v-for="(ep, index) in form.custom_endpoints"
                    :key="index"
                    class="rounded-lg border border-gray-200 p-4 dark:border-dark-600"
                  >
                    <div class="mb-3 flex items-center justify-between">
                      <span
                        class="text-sm font-medium text-gray-700 dark:text-gray-300"
                      >
                        {{
                          t("admin.settings.site.customEndpoints.itemLabel", {
                            n: index + 1,
                          })
                        }}
                      </span>
                      <button
                        type="button"
                        class="rounded p-1 text-red-400 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                        @click="removeEndpoint(index)"
                      >
                        <svg
                          class="h-4 w-4"
                          fill="none"
                          viewBox="0 0 24 24"
                          stroke="currentColor"
                          stroke-width="2"
                        >
                          <path
                            stroke-linecap="round"
                            stroke-linejoin="round"
                            d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                          />
                        </svg>
                      </button>
                    </div>
                    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
                      <div>
                        <label
                          class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                        >
                          {{ t("admin.settings.site.customEndpoints.name") }}
                        </label>
                        <input
                          v-model="ep.name"
                          type="text"
                          class="input text-sm"
                          :placeholder="
                            t(
                              'admin.settings.site.customEndpoints.namePlaceholder',
                            )
                          "
                        />
                      </div>
                      <div>
                        <label
                          class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                        >
                          {{
                            t("admin.settings.site.customEndpoints.endpointUrl")
                          }}
                        </label>
                        <input
                          v-model="ep.endpoint"
                          type="url"
                          class="input font-mono text-sm"
                          :placeholder="
                            t(
                              'admin.settings.site.customEndpoints.endpointUrlPlaceholder',
                            )
                          "
                        />
                      </div>
                      <div class="sm:col-span-2">
                        <label
                          class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                        >
                          {{
                            t(
                              "admin.settings.site.customEndpoints.descriptionLabel",
                            )
                          }}
                        </label>
                        <input
                          v-model="ep.description"
                          type="text"
                          class="input text-sm"
                          :placeholder="
                            t(
                              'admin.settings.site.customEndpoints.descriptionPlaceholder',
                            )
                          "
                        />
                      </div>
                    </div>
                  </div>
                </div>

                <button
                  type="button"
                  class="mt-3 flex w-full items-center justify-center gap-2 rounded-lg border-2 border-dashed border-gray-300 px-4 py-2.5 text-sm text-gray-500 transition-colors hover:border-primary-400 hover:text-primary-600 dark:border-dark-600 dark:text-gray-400 dark:hover:border-primary-500 dark:hover:text-primary-400"
                  @click="addEndpoint"
                >
                  <svg
                    class="h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M12 4v16m8-8H4"
                    />
                  </svg>
                  {{ t("admin.settings.site.customEndpoints.add") }}
                </button>
              </div>

              <!-- Contact Info -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.site.contactInfo") }}
                </label>
                <input
                  v-model="form.contact_info"
                  type="text"
                  class="input"
                  :placeholder="t('admin.settings.site.contactInfoPlaceholder')"
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.site.contactInfoHint") }}
                </p>
              </div>

              <!-- Doc URL -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.site.docUrl") }}
                </label>
                <input
                  v-model="form.doc_url"
                  type="url"
                  class="input font-mono text-sm"
                  :placeholder="t('admin.settings.site.docUrlPlaceholder')"
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.site.docUrlHint") }}
                </p>
              </div>

              <!-- Site Logo Upload -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.site.siteLogo") }}
                </label>
                <ImageUpload
                  v-model="form.site_logo"
                  mode="image"
                  :upload-label="t('admin.settings.site.uploadImage')"
                  :remove-label="t('admin.settings.site.remove')"
                  :hint="t('admin.settings.site.logoHint')"
                  :max-size="300 * 1024"
                />
              </div>

              <!-- Home Content -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.site.homeContent") }}
                </label>
                <textarea
                  v-model="form.home_content"
                  rows="6"
                  class="input font-mono text-sm"
                  :placeholder="t('admin.settings.site.homeContentPlaceholder')"
                ></textarea>
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.site.homeContentHint") }}
                </p>
                <!-- iframe CSP Warning -->
                <p class="mt-2 text-xs text-amber-600 dark:text-amber-400">
                  {{ t("admin.settings.site.homeContentIframeWarning") }}
                </p>
              </div>

              <!-- Compact Home Page -->
              <div class="flex items-center justify-between border-t border-gray-100 pt-4 dark:border-dark-700">
                <div>
                  <label class="font-medium text-gray-900 dark:text-white">{{
                    t("admin.settings.site.compactHome")
                  }}</label>
                  <p class="text-sm text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.site.compactHomeHint") }}
                  </p>
                </div>
                <Toggle v-model="form.compact_home_enabled" data-testid="compact-home-toggle" />
              </div>
            </div>
          </div>
</template>
