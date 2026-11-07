<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LoginAgreementDocument } from '@/features/site/api/publicSettingsTypes';
import Icon from '@/shared/ui/icons/Icon.vue'
import Toggle from '@/shared/ui/Toggle.vue'

import type { DeepReadonly } from 'vue'
import type { AgreementsDraft } from './agreementsModel'
const props = defineProps<{ modelValue: DeepReadonly<AgreementsDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<AgreementsDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as AgreementsDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof AgreementsDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<AgreementsDraft> = {}
  for (const key of Object.keys(value) as (keyof AgreementsDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })


const { t } = useI18n();

function normalizeLoginAgreementDocumentId(raw: string): string {
  return raw
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/[-_]{2,}/g, "-")
    .replace(/^[-_]+|[-_]+$/g, "");
}

function loginAgreementRoutePath(
  doc: LoginAgreementDocument,
  index: number,
): string {
  const id =
    normalizeLoginAgreementDocumentId(doc.id || doc.title) || `doc-${index + 1}`;
  return `/legal/${id}`;
}

function addLoginAgreementDocument() {
  form.login_agreement_documents.push({
    id: `custom-${Date.now().toString(36)}`,
    title: "",
    content_md: "",
  });
}

function removeLoginAgreementDocument(index: number) {
  form.login_agreement_documents.splice(index, 1);
}
</script>

<template>
<div class="card">
	            <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
	              <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
	                <div>
	                  <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
	                    {{ t('ui.loginAgreement') }}
	                  </h2>
	                  <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
	                    {{
	                      t('ui.controlWhetherTheLoginPageRequiresUsersToAccept')
	                    }}
	                  </p>
	                </div>
	                <div class="flex items-center gap-3">
	                  <span class="text-sm text-gray-600 dark:text-gray-300">
	                    {{ form.login_agreement_enabled ? t('ui.enabled') : t('ui.disabled') }}
	                  </span>
	                  <Toggle v-model="form.login_agreement_enabled" />
	                </div>
	              </div>
	            </div>

	            <div class="space-y-6 p-6">
	              <div class="grid grid-cols-1 gap-5 lg:grid-cols-[minmax(0,1fr)_220px]">
	                <div>
	                  <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
	                    {{ t('ui.displayMode') }}
	                  </label>
	                  <div class="grid grid-cols-2 gap-2 rounded-lg bg-gray-100 p-1 dark:bg-dark-700">
                    <button
                      type="button"
                      class="inline-flex items-center justify-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition"
                      :class="
                        form.login_agreement_mode === 'modal'
                          ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                          : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
                      "
                      @click="form.login_agreement_mode = 'modal'"
                    >
                      <Icon name="shield" size="sm" />
                      {{ t('ui.modal') }}
                    </button>
                    <button
                      type="button"
                      class="inline-flex items-center justify-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition"
                      :class="
                        form.login_agreement_mode === 'checkbox'
                          ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                          : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
                      "
                      @click="form.login_agreement_mode = 'checkbox'"
                    >
                      <Icon name="checkCircle" size="sm" />
                      {{ t('ui.checkbox') }}
                    </button>
                  </div>
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      form.login_agreement_mode === "checkbox"
                        ? t('ui.theCheckboxAppearsBelowTheLoginButtonAndGates')
                        : t('ui.theModalOpensOnTheLoginPageAndGates')
                    }}
                  </p>
                </div>

                <div>
                  <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                    {{ t('ui.updatedDate') }}
                  </label>
                  <input
                    v-model="form.login_agreement_updated_at"
                    type="date"
                    class="input"
                  />
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t('ui.changingTheDateOrContentRequiresFreshConsent') }}
                  </p>
                </div>
              </div>

              <div>
                <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                  <div>
                    <h3 class="text-sm font-medium text-gray-900 dark:text-white">
                      {{ t('ui.agreementDocuments') }}
                    </h3>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {{
                        t('ui.documentTitlesAreCustomizableAndContentIsSavedAs')
                      }}
                    </p>
                  </div>
                  <button
                    type="button"
                    class="btn btn-primary btn-sm inline-flex items-center gap-1.5"
                    @click="addLoginAgreementDocument"
                  >
                    <Icon name="plus" size="sm" />
                    {{ t('ui.addDocument') }}
                  </button>
                </div>

                <div class="mt-4 space-y-3">
                  <div
                    v-for="(doc, index) in form.login_agreement_documents"
                    :key="doc.id || index"
                    class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800/60"
                  >
                    <div class="mb-3 flex items-center justify-between gap-3">
                      <div class="flex min-w-0 items-center gap-3">
                        <span class="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-md bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-dark-200">
                          <Icon
                            :name="
                              index === 1
                                ? 'shield'
                                : index === 2
                                  ? 'globe'
                                  : index === 3
                                    ? 'cog'
                                    : 'document'
                            "
                            size="sm"
                          />
                        </span>
                        <div class="min-w-0">
                          <p class="truncate text-sm font-semibold text-gray-900 dark:text-white">
                            {{ doc.title || t('ui.untitledDocument') }}
                          </p>
                          <p class="truncate text-xs text-gray-500 dark:text-gray-400">
                            {{ loginAgreementRoutePath(doc, index) }}
                          </p>
                        </div>
                      </div>
                      <button
                        type="button"
                        class="rounded-md p-2 text-red-400 transition hover:bg-red-50 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:bg-red-900/20"
                        :disabled="
                          form.login_agreement_enabled &&
                          form.login_agreement_documents.length <= 1
                        "
                        @click="removeLoginAgreementDocument(index)"
                      >
                        <Icon name="trash" size="sm" />
                      </button>
                    </div>

                    <div class="grid grid-cols-1 gap-3 lg:grid-cols-2">
                      <div>
                        <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
                          {{ t('ui.documentTitle') }}
                        </label>
                        <input
                          v-model="doc.title"
                          type="text"
                          class="input text-sm"
                          :placeholder="t('ui.exampleTermsOfService')"
                        />
                      </div>
                      <div>
                        <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
                          {{ t('ui.routeSlug') }}
                        </label>
                        <div class="flex overflow-hidden rounded-lg border border-gray-300 bg-white focus-within:border-primary-500 focus-within:ring-1 focus-within:ring-primary-500 dark:border-dark-600 dark:bg-dark-900">
                          <span class="inline-flex flex-shrink-0 items-center border-r border-gray-200 bg-gray-50 px-3 text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-400">
                            /legal/
                          </span>
                          <input
                            v-model="doc.id"
                            type="text"
                            class="min-w-0 flex-1 border-0 bg-transparent px-3 py-2 text-sm text-gray-900 outline-none placeholder:text-gray-400 focus:ring-0 dark:text-white dark:placeholder:text-dark-500"
                            placeholder="usage-policy"
                          />
                        </div>
                      </div>
                    </div>
                    <div class="mt-3">
                      <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
                        {{ t('ui.markdownContent') }}
                      </label>
                        <textarea
                          v-model="doc.content_md"
                          rows="8"
                          class="input font-mono text-sm"
                          :placeholder="t('ui.writeTheFinalMarkdownContentHere')"
                        ></textarea>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
</template>
