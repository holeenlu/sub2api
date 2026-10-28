<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import type { Props, CreateResourceForm } from '../model/createEditorTypes'


const props = defineProps<{
  allowRateEdit: NonNullable<Props['allowRateEdit']>
  form: Pick<CreateResourceForm, "platform">
  hideAccountLongContextBilling: UnwrapRef<ComputedRef<boolean>>
  accountCategory: UnwrapRef<Ref<"apikey" | "bedrock" | "service_account" | "oauth-based", "apikey" | "bedrock" | "service_account" | "oauth-based">>
  openAILongContextBillingEnabled: UnwrapRef<Ref<boolean, boolean>>
  toggleOpenAILongContextBilling: UnwrapRef<() => void>
}>()

const { t } = useI18n()

const allowRateEdit = computed(() => props.allowRateEdit)

const form = computed(() => props.form)

const hideAccountLongContextBilling = computed(() => props.hideAccountLongContextBilling)

const accountCategory = computed(() => props.accountCategory)

const openAILongContextBillingEnabled = computed(() => props.openAILongContextBillingEnabled)

const toggleOpenAILongContextBilling = computed(() => props.toggleOpenAILongContextBilling)
</script>

<template>
<div
        v-if="allowRateEdit && form.platform === 'openai' && !hideAccountLongContextBilling && (accountCategory === 'oauth-based' || accountCategory === 'apikey')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between gap-4">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.longContextBilling') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.longContextBillingDesc') }}
            </p>
          </div>
          <button
            type="button"
            data-testid="openai-long-context-billing-toggle"
            role="switch"
            :aria-checked="openAILongContextBillingEnabled"
            @click="toggleOpenAILongContextBilling"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              openAILongContextBillingEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                openAILongContextBillingEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>
</template>
