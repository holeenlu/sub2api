<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { type UnwrapRef, type Ref } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { CreateResourceForm } from '../model/createEditorTypes'


const props = defineProps<{
  form: Pick<CreateResourceForm, "platform">
  openaiPassthroughEnabled: UnwrapRef<Ref<boolean, boolean>>
}>()

const emit = defineEmits<{
  'update:openaiPassthroughEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
}>()

const { t } = useI18n()

const form = computed(() => props.form)

const openaiPassthroughEnabled = usePanelField(() => props.openaiPassthroughEnabled, value => emit('update:openaiPassthroughEnabled', value))
</script>

<template>
<div
        v-if="form.platform === 'openai'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.oauthPassthrough') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.oauthPassthroughDesc') }}
            </p>
          </div>
          <button
            type="button"
            @click="openaiPassthroughEnabled = !openaiPassthroughEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              openaiPassthroughEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                openaiPassthroughEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>
</template>
