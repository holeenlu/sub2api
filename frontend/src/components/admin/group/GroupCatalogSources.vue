<template>
  <details class="card space-y-3 p-5" :open="!!initialGroupId">
    <summary class="cursor-pointer font-semibold">{{ t('admin.groups.codexModelsManifest.advancedTitle') }}</summary>
    <p class="text-sm text-gray-500">{{ t('admin.groups.codexModelsManifest.advancedHint') }}</p>
    <form class="flex items-end gap-3" @submit.prevent="loadGroup">
      <label>{{ t('modelCatalog.groupID') }}<input v-model="groupID" :disabled="busy" class="input mt-1" type="number" min="1" required /></label>
      <button class="btn btn-secondary" :disabled="busy">{{ t('modelCatalog.inspect') }}</button>
    </form>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="saved" role="status" class="text-sm text-emerald-600">{{ t('modelCatalog.saved') }}</p>
    <form v-if="group?.platform === 'openai'" class="space-y-3" @submit.prevent="save">
      <p class="text-sm font-medium">{{ group.name }} #{{ group.id }}</p>
      <CodexManifestAccountsField ref="field" v-model="config" :group-id="group.id" :account-names="names" />
      <button class="btn btn-primary" :disabled="busy">{{ t('modelCatalog.save') }}</button>
    </form>
  </details>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminGroup, CodexModelsManifestConfig } from '@/types'
import CodexManifestAccountsField from './CodexManifestAccountsField.vue'

const props = defineProps<{ initialGroupId?: string }>()
const { t } = useI18n()
const groupID = ref(props.initialGroupId ?? '')
const group = ref<AdminGroup | null>(null)
const config = ref<CodexModelsManifestConfig>({ enabled: false, account_ids: [], fallback_to_scheduler: false })
const names = ref<Record<number, string>>({})
const field = ref<{ validate: () => boolean } | null>(null)
const busy = ref(false)
const saved = ref(false)
const error = ref('')
watch(groupID, () => { group.value = null; saved.value = false; error.value = '' })

async function loadGroup() {
  const id = Number(groupID.value)
  if (busy.value || !Number.isSafeInteger(id) || id < 1) return
  busy.value = true; error.value = ''; saved.value = false; group.value = null
  try {
    const loaded = await adminAPI.groups.getById(id)
    config.value = { enabled: false, account_ids: [], fallback_to_scheduler: false, ...loaded.codex_models_manifest_config }
    const results = await Promise.allSettled(config.value.account_ids.map(id => adminAPI.accounts.getById(id)))
    names.value = Object.fromEntries(results.flatMap(result => result.status === 'fulfilled' ? [[result.value.id, result.value.name]] : []))
    group.value = loaded
  } catch { error.value = t('modelCatalog.error') } finally { busy.value = false }
}

async function save() {
  if (busy.value || !group.value || !field.value?.validate()) return
  busy.value = true; error.value = ''; saved.value = false
  try {
    await adminAPI.groups.update(group.value.id, { codex_models_manifest_config: { ...config.value, account_ids: [...config.value.account_ids] } })
    saved.value = true
  } catch { error.value = t('modelCatalog.error') } finally { busy.value = false }
}

onMounted(() => { if (props.initialGroupId) void loadGroup() })
</script>
