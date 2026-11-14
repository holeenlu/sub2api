<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ResourceIdentityDraft } from '../model/identityDraft'

withDefaults(defineProps<{ modelValue: Readonly<ResourceIdentityDraft>; nameRequired?: boolean }>(), { nameRequired: true })
const emit = defineEmits<{ 'update:modelValue': [value: ResourceIdentityDraft] }>()
const { t } = useI18n()
</script>

<template>
  <div class="space-y-5">
    <label class="block">
      <span class="input-label">{{ t('admin.accounts.accountName') }}</span>
      <input
        :value="modelValue.name"
        type="text"
        :required="nameRequired"
        class="input"
        :placeholder="t('admin.accounts.enterAccountName')"
        data-tour="account-form-name"
        @input="emit('update:modelValue', { ...modelValue, name: ($event.target as HTMLInputElement).value })"
      />
    </label>
    <label class="block">
      <span class="input-label">{{ t('admin.accounts.notes') }}</span>
      <textarea
        :value="modelValue.notes"
        rows="3"
        class="input"
        :placeholder="t('admin.accounts.notesPlaceholder')"
        @input="emit('update:modelValue', { ...modelValue, notes: ($event.target as HTMLTextAreaElement).value })"
      />
      <span class="input-hint">{{ t('admin.accounts.notesHint') }}</span>
    </label>
  </div>
</template>
