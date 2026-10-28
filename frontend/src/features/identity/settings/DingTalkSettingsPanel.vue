<script setup lang="ts">
import { toRaw } from 'vue'
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'

import type { DeepReadonly } from 'vue'
import type { DingTalkDraft } from './dingTalkModel'
const props = defineProps<{ modelValue: DeepReadonly<DingTalkDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<DingTalkDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as DingTalkDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof DingTalkDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<DingTalkDraft> = {}
  for (const key of Object.keys(value) as (keyof DingTalkDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })


const { t } = useI18n();
watch(() => form.dingtalk_connect_corp_restriction_policy, policy => { if (policy !== 'internal_only') { form.dingtalk_connect_bypass_registration = false; form.dingtalk_connect_sync_corp_email = false; form.dingtalk_connect_sync_display_name = false; form.dingtalk_connect_sync_dept = false } })
</script>

<template>
<div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.dingtalk.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.dingtalk.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <div class="flex items-center justify-between">
                <div>
                  <label class="font-medium text-gray-900 dark:text-white">{{
                    t("admin.settings.dingtalk.enable")
                  }}</label>
                  <p class="text-sm text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.dingtalk.enableHint") }}
                  </p>
                </div>
                <Toggle v-model="form.dingtalk_connect_enabled" />
              </div>

              <div
                v-if="form.dingtalk_connect_enabled"
                class="border-t border-gray-100 pt-4 dark:border-dark-700"
              >
                <div class="grid grid-cols-1 gap-6">
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.dingtalk.clientId") }}
                    </label>
                    <input
                      v-model="form.dingtalk_connect_client_id"
                      type="text"
                      class="input font-mono text-sm"
                      :placeholder="
                        t('admin.settings.dingtalk.clientIdPlaceholder')
                      "
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.dingtalk.clientIdHint") }}
                    </p>
                  </div>

                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.dingtalk.clientSecret") }}
                    </label>
                    <input
                      v-model="form.dingtalk_connect_client_secret"
                      type="password"
                      class="input font-mono text-sm"
                      :placeholder="
                        form.dingtalk_connect_client_secret_configured
                          ? t(
                              'admin.settings.dingtalk.clientSecretConfiguredPlaceholder',
                            )
                          : t('admin.settings.dingtalk.clientSecretPlaceholder')
                      "
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{
                        form.dingtalk_connect_client_secret_configured
                          ? t(
                              "admin.settings.dingtalk.clientSecretConfiguredHint",
                            )
                          : t("admin.settings.dingtalk.clientSecretHint")
                      }}
                    </p>
                  </div>

                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.dingtalk.redirectUrl") }}
                    </label>
                    <input
                      v-model="form.dingtalk_connect_redirect_url"
                      type="url"
                      class="input font-mono text-sm"
                      :placeholder="
                        t('admin.settings.dingtalk.redirectUrlPlaceholder')
                      "
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.dingtalk.redirectUrlHint") }}
                    </p>
                  </div>

                  <!-- Corp Restriction Policy -->
                  <div class="border-t border-gray-100 pt-4 dark:border-dark-700">
                    <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                      {{ t("admin.settings.dingtalk.corpPolicy.label") }}
                    </label>
                    <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.dingtalk.corpPolicy.hint") }}
                    </p>
                    <div class="space-y-2">
                      <label class="flex cursor-pointer items-center gap-3">
                        <input
                          v-model="form.dingtalk_connect_corp_restriction_policy"
                          type="radio"
                          value="none"
                          class="h-4 w-4 text-primary-600"
                        />
                        <span class="text-sm text-gray-700 dark:text-gray-300">
                          {{ t("admin.settings.dingtalk.corpPolicy.none") }}
                        </span>
                      </label>
                      <label class="flex cursor-pointer items-center gap-3">
                        <input
                          v-model="form.dingtalk_connect_corp_restriction_policy"
                          type="radio"
                          value="internal_only"
                          class="h-4 w-4 text-primary-600"
                        />
                        <span class="text-sm text-gray-700 dark:text-gray-300">
                          {{ t("admin.settings.dingtalk.corpPolicy.internalOnly") }}
                        </span>
                      </label>
                    </div>
                  </div>

                  <!-- bypass_registration toggle（仅 internal_only 模式下可见可用） -->
                  <div
                    v-if="form.dingtalk_connect_corp_restriction_policy === 'internal_only'"
                    class="flex items-center justify-between pt-4 border-t border-gray-100 dark:border-dark-700"
                  >
                    <div>
                      <label class="font-medium text-gray-900 dark:text-white">{{
                        t("admin.settings.dingtalk.bypassRegistration")
                      }}</label>
                      <p class="text-sm text-gray-500 dark:text-gray-400">
                        {{ t("admin.settings.dingtalk.bypassRegistrationHint") }}
                      </p>
                    </div>
                    <Toggle v-model="form.dingtalk_connect_bypass_registration" />
                  </div>

                  <!-- 身份同步开关（仅 internal_only 模式下可见） -->
                  <div
                    v-if="form.dingtalk_connect_corp_restriction_policy === 'internal_only'"
                    class="pt-4 border-t border-gray-100 dark:border-dark-700 space-y-2"
                  >
                    <div class="flex items-center justify-between">
                      <div>
                        <label class="font-medium text-gray-900 dark:text-white">{{
                          t("admin.settings.dingtalk.syncDisplayName")
                        }}</label>
                        <p class="text-sm text-gray-500 dark:text-gray-400">
                          {{ t("admin.settings.dingtalk.syncDisplayNameHint") }}
                        </p>
                      </div>
                      <Toggle v-model="form.dingtalk_connect_sync_display_name" />
                    </div>
                    <div v-if="form.dingtalk_connect_sync_display_name" class="space-y-2">
                      <div class="flex items-center gap-2">
                        <label class="text-sm text-gray-600 dark:text-gray-400 whitespace-nowrap min-w-[5rem]">
                          {{ t("admin.settings.dingtalk.syncDisplayNameTarget") }}
                        </label>
                        <input
                          v-model="form.dingtalk_connect_sync_display_name_attr_key"
                          type="text"
                          placeholder="dingtalk_name"
                          class="input text-sm flex-1 max-w-xs"
                        />
                      </div>
                      <div class="flex items-center gap-2">
                        <label class="text-sm text-gray-600 dark:text-gray-400 whitespace-nowrap min-w-[5rem]">
                          {{ t("admin.settings.dingtalk.syncAttrDisplayName") }}
                        </label>
                        <input
                          v-model="form.dingtalk_connect_sync_display_name_attr_name"
                          type="text"
                          :placeholder="t('ui.dingtalkName')"
                          class="input text-sm flex-1 max-w-xs"
                        />
                      </div>
                    </div>
                    <p v-if="form.dingtalk_connect_sync_display_name" class="text-xs text-gray-400 dark:text-gray-500">
                      {{ t("admin.settings.dingtalk.syncDisplayNameTargetHint") }}
                    </p>
                  </div>
                  <div
                    v-if="form.dingtalk_connect_corp_restriction_policy === 'internal_only'"
                    class="pt-4 border-t border-gray-100 dark:border-dark-700 space-y-2"
                  >
                    <div class="flex items-center justify-between">
                      <div>
                        <label class="font-medium text-gray-900 dark:text-white">{{
                          t("admin.settings.dingtalk.syncCorpEmail")
                        }}</label>
                        <p class="text-sm text-gray-500 dark:text-gray-400">
                          {{ t("admin.settings.dingtalk.syncCorpEmailHint") }}
                        </p>
                        <p class="text-xs text-amber-600 dark:text-amber-400 mt-1">
                          {{ t("admin.settings.dingtalk.syncCorpEmailPermissionHint") }}
                        </p>
                      </div>
                      <Toggle v-model="form.dingtalk_connect_sync_corp_email" />
                    </div>
                    <div v-if="form.dingtalk_connect_sync_corp_email" class="space-y-2">
                      <div class="flex items-center gap-2">
                        <label class="text-sm text-gray-600 dark:text-gray-400 whitespace-nowrap min-w-[5rem]">
                          {{ t("admin.settings.dingtalk.syncCorpEmailTarget") }}
                        </label>
                        <input
                          v-model="form.dingtalk_connect_sync_corp_email_attr_key"
                          type="text"
                          placeholder="dingtalk_email"
                          class="input text-sm flex-1 max-w-xs"
                        />
                      </div>
                      <div class="flex items-center gap-2">
                        <label class="text-sm text-gray-600 dark:text-gray-400 whitespace-nowrap min-w-[5rem]">
                          {{ t("admin.settings.dingtalk.syncAttrDisplayName") }}
                        </label>
                        <input
                          v-model="form.dingtalk_connect_sync_corp_email_attr_name"
                          type="text"
                          :placeholder="t('ui.dingtalkCorporateEmail')"
                          class="input text-sm flex-1 max-w-xs"
                        />
                      </div>
                    </div>
                    <p v-if="form.dingtalk_connect_sync_corp_email" class="text-xs text-gray-400 dark:text-gray-500">
                      {{ t("admin.settings.dingtalk.syncCorpEmailTargetHint") }}
                    </p>
                  </div>
                  <div
                    v-if="form.dingtalk_connect_corp_restriction_policy === 'internal_only'"
                    class="pt-4 border-t border-gray-100 dark:border-dark-700 space-y-2"
                  >
                    <div class="flex items-center justify-between">
                      <div>
                        <label class="font-medium text-gray-900 dark:text-white">{{
                          t("admin.settings.dingtalk.syncDept")
                        }}</label>
                        <p class="text-sm text-gray-500 dark:text-gray-400">
                          {{ t("admin.settings.dingtalk.syncDeptHint") }}
                        </p>
                        <p class="text-xs text-amber-600 dark:text-amber-400 mt-1">
                          {{ t("admin.settings.dingtalk.syncDeptPermissionHint") }}
                        </p>
                      </div>
                      <Toggle v-model="form.dingtalk_connect_sync_dept" />
                    </div>
                    <div v-if="form.dingtalk_connect_sync_dept" class="space-y-2">
                      <div class="flex items-center gap-2">
                        <label class="text-sm text-gray-600 dark:text-gray-400 whitespace-nowrap min-w-[5rem]">
                          {{ t("admin.settings.dingtalk.syncDeptTarget") }}
                        </label>
                        <input
                          v-model="form.dingtalk_connect_sync_dept_attr_key"
                          type="text"
                          placeholder="dingtalk_department"
                          class="input text-sm flex-1 max-w-xs"
                        />
                      </div>
                      <div class="flex items-center gap-2">
                        <label class="text-sm text-gray-600 dark:text-gray-400 whitespace-nowrap min-w-[5rem]">
                          {{ t("admin.settings.dingtalk.syncAttrDisplayName") }}
                        </label>
                        <input
                          v-model="form.dingtalk_connect_sync_dept_attr_name"
                          type="text"
                          :placeholder="t('ui.dingtalkDepartment')"
                          class="input text-sm flex-1 max-w-xs"
                        />
                      </div>
                    </div>
                    <p v-if="form.dingtalk_connect_sync_dept" class="text-xs text-gray-400 dark:text-gray-500">
                      {{ t("admin.settings.dingtalk.syncDeptTargetHint") }}
                    </p>
                  </div>
                </div>
              </div>
            </div>
          </div>
</template>
