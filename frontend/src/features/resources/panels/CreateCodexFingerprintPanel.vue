<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/shared/ui/Select.vue'
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { CodexFingerprintMode, CreateResourceForm } from '../model/createEditorTypes'


const props = defineProps<{
  form: Pick<CreateResourceForm, "platform">
  accountCategory: UnwrapRef<Ref<"apikey" | "bedrock" | "service_account" | "oauth-based", "apikey" | "bedrock" | "service_account" | "oauth-based">>
  codexFingerprintMode: UnwrapRef<Ref<CodexFingerprintMode, CodexFingerprintMode>>
  codexFingerprintModeOptions: UnwrapRef<ComputedRef<{ value: CodexFingerprintMode; label: string }[]>>
}>()

const emit = defineEmits<{
  'update:codexFingerprintMode': [value: UnwrapRef<Ref<CodexFingerprintMode, CodexFingerprintMode>>]
}>()

const { t } = useI18n()

const form = computed(() => props.form)

const accountCategory = computed(() => props.accountCategory)

const codexFingerprintMode = usePanelField(() => props.codexFingerprintMode, value => emit('update:codexFingerprintMode', value))

const codexFingerprintModeOptions = computed(() => props.codexFingerprintModeOptions)
</script>

<template>
<div
        v-if="form.platform === 'openai' && accountCategory === 'oauth-based'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between gap-4">
          <div class="min-w-0">
            <label class="input-label mb-0">{{ t('admin.accounts.openai.codexFingerprintMode') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.codexFingerprintModeDesc') }}
            </p>
          </div>
          <div class="w-52 flex-shrink-0">
            <Select v-model="codexFingerprintMode" data-testid="create-codex-fingerprint-mode-select" :options="codexFingerprintModeOptions" />
          </div>
        </div>
      </div>
</template>
