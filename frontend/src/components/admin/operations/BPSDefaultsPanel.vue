<template>
  <section id="bps-defaults" class="space-y-3" data-testid="bps-defaults-panel">
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="saved" role="status" class="text-sm text-emerald-600">{{ t('autoBPSOps.defaults.saved') }}</p>
    <fieldset :disabled="!ready || loading || saving">
      <BPSDefaultsCard v-model="draft" :groups="groups" />
    </fieldset>
    <div class="flex gap-3">
      <button type="button" class="btn btn-primary" data-testid="bps-save-defaults" :disabled="!ready || loading || saving" @click="save">{{ t('autoBPSOps.defaults.save') }}</button>
      <button type="button" class="btn btn-secondary" :disabled="loading || saving" @click="reload">{{ t('autoBPSOps.refresh') }}</button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BPSDefaultsCard from './BPSDefaultsCard.vue'
import { getExcelBPSDefaults, saveExcelBPSDefaults } from '@/api/admin/excelBPSDefaults'
import { defaultExcelBPSDefaults, excelBPSDefaultsError, excelBPSDefaultsPayload } from '@/utils/excelBPSDefaults'
import { extractApiErrorMessage } from '@/utils/apiError'

defineProps<{ groups: { id: number; name: string; platform?: string }[] }>()
const { t } = useI18n()
const draft = ref(defaultExcelBPSDefaults())
const ready = ref(false), loading = ref(false), saving = ref(false), saved = ref(false), error = ref('')
let active = true
onBeforeUnmount(() => { active = false })
async function reload() {
  if (loading.value || saving.value) return
  loading.value = true; ready.value = false; error.value = ''; saved.value = false
  try {
    const value = await getExcelBPSDefaults()
    if (active) { draft.value = value; ready.value = true }
  } catch (e) { if (active) error.value = extractApiErrorMessage(e, t('autoBPSOps.defaults.loadFailed')) }
  finally { if (active) loading.value = false }
}
async function save() {
  if (!active || !ready.value || loading.value || saving.value) return
  saved.value = false; error.value = ''
  const invalid = excelBPSDefaultsError(draft.value)
  if (invalid) { error.value = t(invalid); return }
  saving.value = true
  try {
    const value = await saveExcelBPSDefaults(excelBPSDefaultsPayload(draft.value))
    if (active) { draft.value = value; saved.value = true }
  } catch (e) { if (active) error.value = extractApiErrorMessage(e, t('autoBPSOps.defaults.saveFailed')) }
  finally { if (active) saving.value = false }
}
onMounted(reload)
</script>
