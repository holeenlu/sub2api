<script setup lang="ts">
import { computed, onMounted, onScopeDispose } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { isStepUpCancelled, useAuthStore } from '@/features/identity'
import { settingsAPI } from '@/features/system/api'
import { EmailTemplateEditor } from '@/features/system/panels'
import { readApiError } from '@/shared/lib/apiError'
import { useSettingsWorkspaceServices } from './ports'
import { useSettingsTransaction, type SettingsPatch } from './transaction'
import { useSettingsSave } from './useSettingsSave'
import SettingsConflictReview from './SettingsConflictReview.vue'

const { t } = useI18n()
const auth = useAuthStore()
const services = useSettingsWorkspaceServices()
const fields = [
  { key: 'site_name', label: 'admin.settings.site.siteName' },
  { key: 'site_subtitle', label: 'admin.settings.site.siteSubtitle' },
  { key: 'site_logo', label: 'admin.settings.site.siteLogo' },
  { key: 'contact_info', label: 'admin.settings.site.contactInfo' },
  { key: 'doc_url', label: 'admin.settings.site.docUrl' },
] as const
const generalKeys = fields.map(field => field.key)
const contentKeys = ['login_agreement_documents'] as const
// These are the delegated fields of CT-11, not the super-admin write schema.
const readableFields = computed(() => [
  ...(auth.can('settings.general.read') ? generalKeys : []),
  ...(auth.can('settings.content.read') ? [...contentKeys, 'login_agreement_updated_at'] : [])
])
const editableFields = computed(() => [
  ...(auth.can('settings.general.read') && auth.can('settings.general.manage') ? generalKeys : []),
  ...(auth.can('settings.content.read') && auth.can('settings.content.manage') ? contentKeys : [])
])
const scope = computed(() => services.scope.value && readableFields.value.length
  ? JSON.stringify([services.scope.value, readableFields.value, editableFields.value]) : null)
const transaction = useSettingsTransaction({
  get: settingsAPI.getSettings, put: settingsAPI.updateSettings, isCancelled: isStepUpCancelled
}, scope)
const saveSettings = useSettingsSave(transaction, scope, 'common.saved')
const { baseline, draft, dirtyFields, error, loading, saving, conflicted, revisionMissing, uncertain, load, compare } = transaction
const conflict = computed(() => transaction.conflict.value && {
  ...transaction.conflict.value,
  fields: transaction.conflict.value.fields.filter(field => readableFields.value.includes(field))
})
const editableDirtyFields = computed(() => dirtyFields.value.filter(field => editableFields.value.some(key => key === field)))
const saveBlocked = computed(() => loading.value || saving.value || conflicted.value || revisionMissing.value || uncertain.value)
const documents = computed(() => draft.value?.login_agreement_documents || [])
const errorMessage = computed(() => readApiError(error.value).message || t('admin.rolePermissions.saveFailed'))

function patch(value: SettingsPatch): void {
  transaction.patch(Object.fromEntries(Object.entries(value).filter(([key]) => editableFields.value.some(field => field === key))))
}
function patchDocument(index: number, content: string): void {
  patch({ login_agreement_documents: documents.value.map((document, position) => position === index ? { ...document, content_md: content } : document) })
}
function hasEdits(keys: readonly string[]): boolean {
  return editableDirtyFields.value.some(field => keys.includes(field))
}
async function save(keys: readonly string[]): Promise<void> {
  await saveSettings(dirty => Object.fromEntries(Object.entries(dirty).filter(([key]) =>
    keys.includes(key) && editableFields.value.some(field => field === key)
  )))
}
function mergeReviewed(keys: string[]): void {
  transaction.mergeReviewed(keys.filter(key => editableDirtyFields.value.includes(key)))
}
function confirmLeave(): boolean { return !dirtyFields.value.length || window.confirm(t('admin.settings.workspace.leave')) }
function beforeUnload(event: BeforeUnloadEvent): void {
  if (dirtyFields.value.length) { event.preventDefault(); event.returnValue = '' }
}
onBeforeRouteLeave(confirmLeave)
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onScopeDispose(() => window.removeEventListener('beforeunload', beforeUnload))
</script>

<template>
  <div class="space-y-6">
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <div v-if="error" role="alert" class="rounded-lg border border-red-300 p-4 text-red-700">
      {{ errorMessage }}
      <button v-if="!draft" type="button" class="btn btn-secondary" @click="load">{{ t('common.tryAgain') }}</button>
    </div>
    <p v-if="revisionMissing" role="alert">{{ t('admin.settings.workspace.revisionMissing') }} <button type="button" class="btn btn-secondary" @click="compare">{{ t('common.refresh') }}</button></p>
    <p v-if="uncertain" role="alert">{{ t('admin.settings.workspace.outcomeUnknown') }} <button type="button" class="btn btn-secondary" @click="compare">{{ t('common.refresh') }}</button></p>
    <SettingsConflictReview v-if="conflicted" :conflict="conflict" :baseline="baseline" :draft="draft" :dirty-fields="editableDirtyFields" :saving="saving" @compare="compare" @merge="mergeReviewed" />
    <template v-if="draft">
      <form v-if="auth.can('settings.general.read')" class="card space-y-4 p-6" @submit.prevent="save(generalKeys)">
        <h2 class="text-lg font-semibold">{{ t('admin.rolePermissions.generalSettings') }}</h2>
        <div v-for="field in fields" :key="field.key">
          <label class="input-label" :for="`delegated-${field.key}`">{{ t(field.label) }}</label>
          <input :id="`delegated-${field.key}`" :value="draft[field.key] ?? ''" class="input" :disabled="!auth.can('settings.general.manage') || loading" @input="patch({ [field.key]: ($event.target as HTMLInputElement).value })" />
        </div>
        <button v-if="auth.can('settings.general.manage')" type="submit" class="btn btn-primary" data-testid="save-general" :disabled="saveBlocked || !hasEdits(generalKeys)">{{ t('common.save') }}</button>
      </form>
      <div v-if="auth.can('settings.content.read')" class="card space-y-4 p-6">
        <h2 class="text-lg font-semibold">{{ t('admin.rolePermissions.contentSettings') }}</h2>
        <label v-for="(document, index) in documents" :key="document.id" class="block space-y-2">
          <span class="input-label">{{ document.title }}</span>
          <textarea :value="document.content_md" class="input min-h-40" :disabled="!auth.can('settings.content.manage') || loading" @input="patchDocument(index, ($event.target as HTMLTextAreaElement).value)" />
        </label>
        <button v-if="auth.can('settings.content.manage')" type="button" class="btn btn-primary" data-testid="save-content" :disabled="saveBlocked || !hasEdits(contentKeys)" @click="save(contentKeys)">{{ t('common.save') }}</button>
        <EmailTemplateEditor :read-only="!auth.can('settings.content.manage')" />
      </div>
    </template>
  </div>
</template>
