<script setup lang="ts">
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import HeaderOverrideEditor from '@/features/resources/components/HeaderOverrideEditor.vue'
import { type HeaderOverrideRow } from "@/features/resources/model/credentialsBuilder"
import { type UnwrapRef, type Ref } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { CreateResourceForm } from '../model/createEditorTypes'

const props = defineProps<{
  isHeaderOverrideCapable: UnwrapRef<ReturnType<typeof createProviderCredentialOptions>["isHeaderOverrideCapable"]>
  form: Pick<CreateResourceForm, "type" | "platform">
  headerOverrideEnabled: UnwrapRef<Ref<boolean, boolean>>
  headerOverrideRows: UnwrapRef<Ref<{ name: string; value: string }[], HeaderOverrideRow[] | { name: string; value: string }[]>>
}>()

const emit = defineEmits<{
  'update:headerOverrideEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:headerOverrideRows': [value: UnwrapRef<Ref<{ name: string; value: string }[], HeaderOverrideRow[] | { name: string; value: string }[]>>]
}>()

const { t } = useI18n()

const isHeaderOverrideCapable = computed(() => props.isHeaderOverrideCapable)

const form = computed(() => props.form)

const headerOverrideEnabled = usePanelField(() => props.headerOverrideEnabled, value => emit('update:headerOverrideEnabled', value))

const headerOverrideRows = usePanelField(() => props.headerOverrideRows, value => emit('update:headerOverrideRows', value))
</script>
<template>
<div
          v-if="isHeaderOverrideCapable(form.platform, 'apikey')"
          class="border-t border-gray-200 pt-4 dark:border-dark-600"
        >
          <div class="mb-3 flex items-center justify-between">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.headerOverride.title') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.headerOverride.hint') }}
              </p>
            </div>
            <button
              type="button"
              @click="headerOverrideEnabled = !headerOverrideEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                headerOverrideEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  headerOverrideEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="headerOverrideEnabled" class="space-y-3">
            <div class="rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
              <p class="text-xs text-blue-700 dark:text-blue-400">
                <Icon name="exclamationCircle" size="sm" class="mr-1 inline" :stroke-width="2" />
                {{ t('admin.accounts.headerOverride.info') }}
              </p>
            </div>

            <HeaderOverrideEditor
              :rows="headerOverrideRows"
              @update:rows="headerOverrideRows = $event"
            />
          </div>
        </div>
</template>
