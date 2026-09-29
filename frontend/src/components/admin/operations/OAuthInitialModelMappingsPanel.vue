<template>
  <section class="card space-y-4 p-5" data-testid="oauth-initial-model-mappings-panel">
    <div>
      <h2 class="font-semibold">{{ t('autoBPSOps.oauthMappings.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500">{{ t('autoBPSOps.oauthMappings.subtitle') }}</p>
    </div>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="saved" role="status" class="text-sm text-emerald-600">{{ t('autoBPSOps.oauthMappings.saved') }}</p>
    <label class="flex items-center gap-2 text-sm">
      <input v-model="draft.enabled" type="checkbox" :disabled="loading || saving" data-testid="oauth-mappings-enabled" />
      {{ t('autoBPSOps.oauthMappings.enabled') }}
    </label>
    <p class="text-xs text-gray-500">{{ t('autoBPSOps.oauthMappings.scope') }}</p>
    <div class="space-y-2">
      <div v-for="(rule, index) in draft.rules" :key="index" class="flex flex-wrap items-center gap-2">
        <input v-model="rule.from" class="input min-w-[12rem] flex-1" :placeholder="t('autoBPSOps.oauthMappings.from')" :disabled="loading || saving" />
        <span class="text-gray-400">→</span>
        <input v-model="rule.to" class="input min-w-[12rem] flex-1" :placeholder="t('autoBPSOps.oauthMappings.to')" :disabled="loading || saving" />
        <button type="button" class="btn btn-secondary" :disabled="loading || saving" :aria-label="t('autoBPSOps.oauthMappings.remove')" @click="remove(index)">×</button>
      </div>
      <button type="button" class="btn btn-secondary" :disabled="loading || saving || draft.rules.length >= 100" @click="add">{{ t('autoBPSOps.oauthMappings.add') }}</button>
    </div>
    <div class="flex gap-3">
      <button type="button" class="btn btn-primary" data-testid="oauth-mappings-save" :disabled="loading || saving" @click="save">{{ t('autoBPSOps.oauthMappings.save') }}</button>
      <button type="button" class="btn btn-secondary" :disabled="loading || saving" @click="reload">{{ t('autoBPSOps.refresh') }}</button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { extractApiErrorMessage } from '@/utils/apiError'
import { defaultOAuthInitialModelMappings, oauthModelMappingsError, oauthModelMappingsPayload } from '@/utils/oauthModelMappings'
import { getOAuthInitialModelMappings, saveOAuthInitialModelMappings, type OAuthInitialModelMappings } from '@/api/admin/oauthInitialModelMappings'

const { t } = useI18n()
const draft = ref<OAuthInitialModelMappings>(defaultOAuthInitialModelMappings())
const loading = ref(false), saving = ref(false), saved = ref(false), error = ref('')
let active = true
onBeforeUnmount(() => { active = false })
async function reload() {
  if (loading.value || saving.value) return
  loading.value = true; error.value = ''; saved.value = false
  try {
    const value = await getOAuthInitialModelMappings()
    if (active) draft.value = { ...value, rules: value.rules || [] }
  } catch (e) { if (active) error.value = extractApiErrorMessage(e, t('autoBPSOps.oauthMappings.loadFailed')) }
  finally { if (active) loading.value = false }
}
function add() { draft.value.rules.push({ from: '', to: '' }) }
function remove(index: number) { draft.value.rules.splice(index, 1) }
async function save() {
  if (loading.value || saving.value) return
  error.value = ''; saved.value = false
  const invalid = oauthModelMappingsError(draft.value)
  if (invalid) { error.value = t(invalid); return }
  saving.value = true
  try {
    const value = await saveOAuthInitialModelMappings(oauthModelMappingsPayload(draft.value))
    if (active) { draft.value = { ...value, rules: value.rules || [] }; saved.value = true }
  } catch (e) { if (active) error.value = extractApiErrorMessage(e, t('autoBPSOps.oauthMappings.saveFailed')) }
  finally { if (active) saving.value = false }
}
onMounted(reload)
</script>
