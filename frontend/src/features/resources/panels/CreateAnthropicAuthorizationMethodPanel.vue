<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAccountOAuth, type AddMethod } from '@/features/resources/queries/useAccountOAuth'
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { CreateResourceForm } from '../model/createEditorTypes'


const props = defineProps<{
  form: Pick<CreateResourceForm, "platform">
  isOAuthFlow: UnwrapRef<ComputedRef<boolean>>
  addMethod: UnwrapRef<Ref<AddMethod, AddMethod>>
  oauth: ReturnType<typeof useAccountOAuth>
}>()

const emit = defineEmits<{
  'update:addMethod': [value: UnwrapRef<Ref<AddMethod, AddMethod>>]
}>()

const { t } = useI18n()

const form = computed(() => props.form)

const isOAuthFlow = computed(() => props.isOAuthFlow)

const addMethod = usePanelField(() => props.addMethod, value => emit('update:addMethod', value))

const oauth = computed(() => props.oauth)
</script>

<template>
<div v-if="form.platform === 'anthropic' && isOAuthFlow">
        <label class="input-label">{{ t('admin.accounts.oauth.authMethod') }}</label>
        <div class="mt-2 flex gap-4">
          <label class="flex cursor-pointer items-center">
            <input
              v-model="addMethod"
              type="radio"
              value="oauth"
              :disabled="oauth.loading.value"
              class="mr-2 text-primary-600 focus:ring-primary-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.oauth.auth') }}</span>
          </label>
          <label class="flex cursor-pointer items-center">
            <input
              v-model="addMethod"
              type="radio"
              value="setup-token"
              :disabled="oauth.loading.value"
              class="mr-2 text-primary-600 focus:ring-primary-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.oauth.oauthToken') }}</span>
          </label>
          <label class="flex cursor-pointer items-center">
            <input
              v-model="addMethod"
              type="radio"
              value="setup-token-manual"
              :disabled="oauth.loading.value"
              class="mr-2 text-primary-600 focus:ring-primary-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{
              t('admin.accounts.oauth.setupTokenLongLived')
            }}</span>
          </label>
        </div>
      </div>
</template>
