<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { type UnwrapRef, type Ref } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { Props } from '../model/editEditorTypes'


const props = defineProps<{
  account: NonNullable<Props['account']>
  webSearchGlobalEnabled: UnwrapRef<Ref<boolean, boolean>>
  webSearchEmulationMode: UnwrapRef<Ref<string, string>>
}>()

const emit = defineEmits<{
  'update:webSearchEmulationMode': [value: UnwrapRef<Ref<string, string>>]
}>()

const { t } = useI18n()

const account = computed(() => props.account)

const webSearchGlobalEnabled = computed(() => props.webSearchGlobalEnabled)

const webSearchEmulationMode = usePanelField(() => props.webSearchEmulationMode, value => emit('update:webSearchEmulationMode', value))
</script>

<template>
<template v-if="account">
<div
        v-if="(account?.platform === 'anthropic' || account?.platform === 'openai') && account?.type === 'apikey' && webSearchGlobalEnabled"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.anthropic.webSearchEmulation') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.anthropic.webSearchEmulationDesc') }}
            </p>
          </div>
          <select v-model="webSearchEmulationMode" class="input w-24 text-sm">
            <option value="default">{{ t('admin.accounts.anthropic.webSearchDefault') }}</option>
            <option value="enabled">{{ t('admin.accounts.anthropic.webSearchEnabled') }}</option>
            <option value="disabled">{{ t('admin.accounts.anthropic.webSearchDisabled') }}</option>
          </select>
        </div>
      </div>
</template>
</template>
