<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { type UnwrapRef, type Ref } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { Props } from '../model/editEditorTypes'


const props = defineProps<{
  account: NonNullable<Props['account']>
  allowTransportEdit: NonNullable<Props['allowTransportEdit']>
  editBaseUrl: UnwrapRef<Ref<string, string>>
  allowAuthorize: NonNullable<Props['allowAuthorize']>
  editApiKey: UnwrapRef<Ref<string, string>>
}>()

const emit = defineEmits<{
  'update:editBaseUrl': [value: UnwrapRef<Ref<string, string>>]
  'update:editApiKey': [value: UnwrapRef<Ref<string, string>>]
}>()

const { t } = useI18n()

const account = computed(() => props.account)

const allowTransportEdit = computed(() => props.allowTransportEdit)

const editBaseUrl = usePanelField(() => props.editBaseUrl, value => emit('update:editBaseUrl', value))

const allowAuthorize = computed(() => props.allowAuthorize)

const editApiKey = usePanelField(() => props.editApiKey, value => emit('update:editApiKey', value))
</script>

<template>
<template v-if="account">
<div v-if="account.type === 'upstream'" class="space-y-4">
        <div>
          <label class="input-label">{{ t('admin.accounts.upstream.baseUrl') }}</label>
          <input
 :disabled="!allowTransportEdit"            v-model="editBaseUrl"
            type="text"
            class="input"
            placeholder="https://cloudcode-pa.googleapis.com"
          />
          <p class="input-hint">{{ t('admin.accounts.upstream.baseUrlHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.upstream.apiKey') }}</label>
          <input
 :disabled="!allowAuthorize"            v-model="editApiKey"
            type="password"
            class="input font-mono"
            placeholder="sk-..."
          />
          <p class="input-hint">{{ t('admin.accounts.leaveEmptyToKeep') }}</p>
        </div>
      </div>
</template>
</template>
