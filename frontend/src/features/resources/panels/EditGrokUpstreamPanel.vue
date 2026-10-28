<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import GrokBaseUrlPresets from '@/features/resources/components/GrokBaseUrlPresets.vue'
import { type UnwrapRef, type Ref } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { Props } from '../model/editEditorTypes'


const props = defineProps<{
  account: NonNullable<Props['account']>
  grokOAuthCustomBaseUrlEnabled: UnwrapRef<Ref<boolean, boolean>>
  allowTransportEdit: NonNullable<Props['allowTransportEdit']>
  grokOAuthBaseUrl: UnwrapRef<Ref<string, string>>
}>()

const emit = defineEmits<{
  'update:grokOAuthCustomBaseUrlEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:grokOAuthBaseUrl': [value: UnwrapRef<Ref<string, string>>]
}>()

const { t } = useI18n()

const account = computed(() => props.account)

const grokOAuthCustomBaseUrlEnabled = usePanelField(() => props.grokOAuthCustomBaseUrlEnabled, value => emit('update:grokOAuthCustomBaseUrlEnabled', value))

const allowTransportEdit = computed(() => props.allowTransportEdit)

const grokOAuthBaseUrl = usePanelField(() => props.grokOAuthBaseUrl, value => emit('update:grokOAuthBaseUrl', value))
</script>

<template>
<template v-if="account">
<div
        v-if="account.platform === 'grok' && account.type === 'oauth'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="mb-3 flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.grokCustomBaseUrl.title') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.grokCustomBaseUrl.hint') }}
            </p>
          </div>
          <button
            type="button"
            data-testid="grok-custom-base-url-toggle"
            @click="grokOAuthCustomBaseUrlEnabled = !grokOAuthCustomBaseUrlEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              grokOAuthCustomBaseUrlEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                grokOAuthCustomBaseUrlEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
        <div v-if="grokOAuthCustomBaseUrlEnabled" class="space-y-2">
          <input
 :disabled="!allowTransportEdit"            v-model="grokOAuthBaseUrl"
            type="text"
            class="input"
            data-testid="grok-custom-base-url-input"
            :placeholder="t('admin.accounts.grokCustomBaseUrl.placeholder')"
          />
          <GrokBaseUrlPresets @select="grokOAuthBaseUrl = $event" />
        </div>
      </div>
</template>
</template>
