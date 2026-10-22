<template>
  <div class="space-y-6">
    <form v-if="auth.can('settings.general.read')" class="card space-y-4 p-6" @submit.prevent="save">
      <h2 class="text-lg font-semibold">{{ t('admin.rolePermissions.generalSettings') }}</h2>
      <div v-for="field in fields" :key="field.key">
        <label class="input-label" :for="`delegated-${field.key}`">{{ t(field.label) }}</label>
        <input :id="`delegated-${field.key}`" v-model="values[field.key]" class="input" :disabled="!auth.can('settings.general.manage') || loading" />
      </div>
      <button v-if="auth.can('settings.general.manage')" type="submit" class="btn btn-primary" :disabled="loading || saving">{{ t('common.save') }}</button>
    </form>
    <div v-if="auth.can('settings.content.read')" class="card space-y-4 p-6">
      <h2 class="text-lg font-semibold">{{ t('admin.rolePermissions.contentSettings') }}</h2>
      <label v-for="document in documents" :key="document.id" class="block space-y-2">
        <span class="input-label">{{ document.title }}</span>
        <textarea v-model="document.content_md" class="input min-h-40" :disabled="!auth.can('settings.content.manage') || loading" />
      </label>
      <button v-if="auth.can('settings.content.manage')" type="button" class="btn btn-primary" :disabled="loading || saving" @click="saveDocuments">{{ t('common.save') }}</button>
      <EmailTemplateEditor :read-only="!auth.can('settings.content.manage')" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api'
import type { LoginAgreementDocument } from '@/types'
import EmailTemplateEditor from '@/views/admin/settings/EmailTemplateEditor.vue'

const { t } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const loading = ref(false)
const saving = ref(false)
const fields = [
  { key: 'site_name', label: 'admin.settings.site.siteName' },
  { key: 'site_subtitle', label: 'admin.settings.site.siteSubtitle' },
  { key: 'site_logo', label: 'admin.settings.site.siteLogo' },
  { key: 'contact_info', label: 'admin.settings.site.contactInfo' },
  { key: 'doc_url', label: 'admin.settings.site.docUrl' },
]
const values = reactive<Record<string, string>>({})
const previous = ref<Record<string, string>>({})
const documents = ref<LoginAgreementDocument[]>([])
async function load() {
  loading.value = true
  try {
    const response = await adminAPI.settings.getSettings()
    for (const field of fields) values[field.key] = String((response as unknown as Record<string, unknown>)[field.key] || '')
    previous.value = { ...values }
    documents.value = response.login_agreement_documents || []
  } catch { app.showError(t('admin.rolePermissions.loadFailed')) }
  finally { loading.value = false }
}
async function persist(payload: Record<string, unknown>) {
  saving.value = true
  try {
    await adminAPI.settings.updateSettings(payload)
    app.showSuccess(t('common.saved'))
    await load()
  } catch (error: any) { app.showError(error?.message || t('admin.rolePermissions.saveFailed')) }
  finally { saving.value = false }
}
function save() {
  if (!auth.can('settings.general.manage')) return
  return persist(Object.fromEntries(fields.filter(field => values[field.key] !== previous.value[field.key]).map(field => [field.key, values[field.key]])))
}
function saveDocuments() {
  if (!auth.can('settings.content.manage')) return
  return persist({ login_agreement_documents: documents.value })
}
onMounted(load)
</script>
