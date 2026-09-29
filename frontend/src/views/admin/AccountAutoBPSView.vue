<template>
  <AppLayout>
    <div class="space-y-5">
      <SmartOpsNav />
      <header><h1 class="text-2xl font-bold">{{ t('autoBPSOps.title') }}</h1><p class="mt-2 text-sm text-gray-500">{{ t('autoBPSOps.hint') }}</p></header>
      <RouterLink to="/admin/accounts" class="text-primary-600">{{ t('autoBPSOps.accounts') }}</RouterLink>
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p><p v-if="notice" role="status" class="text-emerald-600">{{ notice }}</p>
      <BPSDefaultsPanel v-if="auth.user?.role === 'admin'" :key="auth.user.id" :groups="templateGroups" />
      <section class="card p-5">
        <header class="flex justify-between"><h2 class="font-semibold">{{ t('autoBPSOps.rules') }}</h2><button class="btn btn-secondary" :disabled="busy" @click="refresh">{{ t('autoBPSOps.refresh') }}</button></header>
        <p class="my-3 text-sm text-gray-500">{{ t('autoBPSOps.notice') }}</p>
        <div class="my-3 flex flex-wrap items-center gap-3 text-sm">
          <label class="flex items-center gap-2"><input type="checkbox" data-testid="bps-select-all" :checked="allSelected" :indeterminate="selectedRuleIds.length > 0 && !allSelected" :disabled="busy || !!deleteTargets.length || !plans.length" @change="toggleSelection" />{{ t('autoBPSOps.selectAll') }}</label>
          <span aria-live="polite">{{ t('autoBPSOps.selected', { count: selectedRuleIds.length }) }}</span>
          <button class="btn btn-secondary text-red-600" data-testid="bps-bulk-delete" :disabled="busy || !!deleteTargets.length || !selectedRuleIds.length" @click="requestDelete(selectedRuleIds)">{{ t('autoBPSOps.bulkDelete') }}</button>
        </div>
        <div class="overflow-x-auto"><table class="w-full text-left text-sm"><thead><tr><th><span class="sr-only">{{ t('autoBPSOps.selectAll') }}</span></th><th v-for="key in ['account','model','cron','state','next']" :key="key" class="p-2">{{ t(`autoBPSOps.${key}`) }}</th><th /></tr></thead><tbody>
          <tr v-for="plan in plans" :key="plan.id" :data-plan-id="plan.id" class="border-t dark:border-dark-700"><td class="p-2"><input v-model="selectedRuleIds" type="checkbox" :value="plan.id" :disabled="busy || !!deleteTargets.length" :aria-label="t('autoBPSOps.selectRule', { id: plan.id })" /></td><td class="p-2">{{ plan.account_name || plan.account_id }} #{{ plan.account_id }}</td><td class="p-2">{{ plan.model_id }}</td><td class="p-2 font-mono">{{ plan.cron_expression }}</td><td class="p-2">{{ t(plan.enabled ? 'autoBPSOps.enabled' : 'autoBPSOps.paused') }}</td><td class="p-2">{{ when(plan.next_run_at) }}</td><td class="flex gap-2 p-2"><button class="btn btn-secondary" :disabled="busy" @click="edit(plan)">{{ t('autoBPSOps.edit') }}</button><button class="btn btn-secondary" :disabled="busy" @click="toggle(plan)">{{ t(plan.enabled ? 'autoBPSOps.pause' : 'autoBPSOps.resume') }}</button><button class="btn btn-secondary" :disabled="busy || !plan.enabled" @click="run(plan)">{{ t('autoBPSOps.run') }}</button></td></tr>
        </tbody></table></div><p v-if="!plans.length" class="py-4 text-gray-500">{{ t('autoBPSOps.empty') }}</p>
      </section>
      <form v-if="editing" class="card space-y-4 p-5" @submit.prevent="save">
        <h2 class="font-semibold">{{ t('autoBPSOps.edit') }} #{{ editing.account_id }}</h2>
        <fieldset :disabled="busy" class="space-y-4"><div class="grid gap-4 sm:grid-cols-2"><label>{{ t('autoBPSOps.model') }}<input v-model="model" required class="input w-full" /></label><QualityProbeSchedule v-model="cron" /></div>
          <QualityBPSSettings v-model:bps="bps" v-model:auto-restore="autoRestore" :target-groups="groups" />
          <div class="flex gap-2"><button class="btn btn-primary" type="submit">{{ t('autoBPSOps.save') }}</button><button class="btn btn-secondary" type="button" @click="editing = null">{{ t('autoBPSOps.cancel') }}</button><button class="btn btn-secondary text-red-600" type="button" data-testid="bps-editor-delete" @click="requestDelete([editing.id])">{{ t('autoBPSOps.delete') }}</button></div>
        </fieldset>
      </form>
      <section class="card p-5"><h2 class="mb-3 font-semibold">{{ t('autoBPSOps.history') }}</h2>
        <div v-for="item in history" :key="item.id" class="border-t py-3 text-sm dark:border-dark-700"><p>{{ item.account_name || item.account_id }} · {{ when(item.started_at) }} · {{ item.quality_judgment?.verdict || item.status }} · {{ item.quality_action || '—' }}</p><details @toggle="loadDetail($event, item)"><summary class="cursor-pointer text-primary-600">{{ t('autoBPSOps.details') }}</summary><pre class="mt-2 whitespace-pre-wrap break-words">{{ detailText[item.id] || item.error_message || item.quality_judgment?.reason }}</pre></details></div>
        <p v-if="!history.length" class="text-gray-500">{{ t('autoBPSOps.empty') }}</p><button v-if="cursor" class="btn btn-secondary mt-3" :disabled="busy" @click="older">{{ t('autoBPSOps.more') }}</button>
      </section>
    </div>
    <BaseDialog :show="!!deleteTargets.length" :title="t('autoBPSOps.deleteTitle', { count: deleteTargets.length })" width="narrow" :close-on-escape="!deleting" :show-close-button="!deleting" @close="cancelDelete">
      <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('autoBPSOps.deleteConfirm', { count: deleteTargets.length }) }}</p>
      <ul class="mt-3 max-h-48 space-y-1 overflow-auto break-words text-sm" data-testid="bps-delete-targets"><li v-for="plan in deleteTargets" :key="plan.id">{{ plan.account_name || plan.account_id }} · #{{ plan.id }}</li></ul>
      <p v-if="deleteProgress" class="mt-3 text-sm" role="status">{{ deleteProgress }}</p>
      <p v-if="deleteError" class="mt-3 text-sm text-red-600" role="alert" data-testid="bps-delete-error">{{ deleteError }}</p>
      <template #footer><div class="flex justify-end gap-2"><button class="btn btn-secondary" :disabled="deleting" data-testid="bps-cancel-delete" @click="cancelDelete">{{ t('autoBPSOps.cancel') }}</button><button class="btn bg-red-600 text-white hover:bg-red-700" :disabled="deleting" data-testid="bps-confirm-delete" @click="confirmDelete">{{ t(deleting ? 'autoBPSOps.deleting' : 'autoBPSOps.delete') }}</button></div></template>
    </BaseDialog>
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAuthStore } from '@/stores/auth'
import SmartOpsNav from '@/components/admin/operations/SmartOpsNav.vue'
import QualityBPSSettings from '@/components/admin/operations/QualityBPSSettings.vue'
import BPSDefaultsPanel from '@/components/admin/operations/BPSDefaultsPanel.vue'
import QualityProbeSchedule from '@/components/admin/operations/QualityProbeSchedule.vue'
import { apiClient } from '@/api/client'
import { adminAPI } from '@/api/admin'
import type { ScheduledTestPlan, ScheduledTestResult } from '@/types'
import { defaultQualityBPS, qualityBPSForm, qualityBPSError, qualityBPSPayload } from '@/utils/qualityRulePatch'
import { extractApiErrorMessage } from '@/utils/apiError'
const { t } = useI18n()
const auth = useAuthStore()
type History = ScheduledTestResult & { account_id: number; account_name: string }
const plans = ref<ScheduledTestPlan[]>([]), history = ref<History[]>([]), cursor = ref(0)
const detailText = ref<Record<number, string>>({})
const busy = ref(false), error = ref(''), notice = ref(''), editing = ref<ScheduledTestPlan | null>(null)
const bps = ref(defaultQualityBPS()), autoRestore = ref(true), model = ref(''), cron = ref('')
const groups = ref<{ id: number; name: string }[]>([])
const templateGroups = ref<{ id: number; name: string; platform: string }[]>([])
const selectedRuleIds = ref<number[]>([]), deleteTargets = ref<ScheduledTestPlan[]>([])
const deleting = ref(false), deleteError = ref(''), deleteProgress = ref('')
const deletedPlanIds = new Set<number>()
const allSelected = computed(() => plans.value.length > 0 && plans.value.every(plan => selectedRuleIds.value.includes(plan.id)))
let alive = true, generation = 0
const active = (version: number) => alive && generation === version
const when = (value?: string | null) => value ? new Date(value).toLocaleString() : '—'
async function perform(action: () => Promise<void>) {
  if (busy.value || deleteTargets.value.length) return
  const version = generation
  busy.value = true; error.value = ''; notice.value = ''
  try { await action() } catch (e) { if (active(version)) error.value = extractApiErrorMessage(e, t('autoBPSOps.error')) } finally { if (active(version)) busy.value = false }
}
async function loadDetail(event: Event, item: History) {
  if (!(event.target as HTMLDetailsElement).open || detailText.value[item.id]) return
  const version = generation
  try {
    const { data } = await apiClient.get<ScheduledTestResult>('/admin/scheduled-test-plans/' + item.plan_id + '/results/' + item.id)
    if (active(version) && !deletedPlanIds.has(item.plan_id)) detailText.value[item.id] = data.response_text || data.error_message
  } catch (e) { if (active(version) && !deletedPlanIds.has(item.plan_id)) error.value = extractApiErrorMessage(e, t('autoBPSOps.error')) }
}
async function loadPlans() {
  const version = generation
  const { data } = await apiClient.get<ScheduledTestPlan[]>('/admin/account-quality-plans')
  if (!active(version)) return
  plans.value = (data || []).filter(plan => !deletedPlanIds.has(plan.id))
  selectedRuleIds.value = selectedRuleIds.value.filter(id => plans.value.some(plan => plan.id === id))
}
async function loadHistory(append = false) {
  const version = generation
  const { data } = await apiClient.get<{ items: History[]; next_cursor: number }>('/admin/account-quality-results', { params: { before_id: append ? cursor.value : 0 } })
  if (!active(version)) return
  const items = (data.items || []).filter(item => !deletedPlanIds.has(item.plan_id))
  history.value = append ? [...history.value, ...items] : items; cursor.value = data.next_cursor || 0
}
async function refresh() { await perform(async () => { await Promise.all([loadPlans(), loadHistory()]) }) }
async function older() { await perform(() => loadHistory(true)) }
function edit(plan: ScheduledTestPlan) {
  if (busy.value || deleteTargets.value.length) return
  editing.value = plan; model.value = plan.model_id; cron.value = plan.cron_expression
  bps.value = qualityBPSForm(plan.pelican_config?.quality?.bps); autoRestore.value = !!plan.pelican_config?.quality?.auto_restore
}
async function toggle(plan: ScheduledTestPlan) { const version = generation; await perform(async () => { await adminAPI.scheduledTests.update(plan.id, { enabled: !plan.enabled }); if (active(version)) await loadPlans() }) }
async function run(plan: ScheduledTestPlan) { const version = generation; await perform(async () => { await apiClient.post(`/admin/account-quality-plans/${plan.id}/run`); if (!active(version)) return; notice.value = t('autoBPSOps.queued'); await loadPlans() }) }
async function save() {
  if (!editing.value) return
  const validation = qualityBPSError(bps.value)
  if (validation) { error.value = t(validation); return }
  const plan = editing.value, version = generation
  await perform(async () => {
    await adminAPI.scheduledTests.update(plan.id, { model_id: model.value.trim(), cron_expression: cron.value.trim(), auto_recover: false,
      pelican_config: { reasoning_effort: 'high', ...plan.pelican_config, question_kind: 'state_probe', prompt: '', parallel_count: 1,
        quality: { action: 'enable_bps', expected_answer: '', remove_group_ids: [], auto_restore: autoRestore.value, bps: qualityBPSPayload(bps.value) } } })
    if (!active(version)) return
    editing.value = null; notice.value = t('autoBPSOps.saved'); await loadPlans()
  })
}
function toggleSelection() {
  if (busy.value || deleteTargets.value.length) return
  selectedRuleIds.value = allSelected.value ? [] : plans.value.map(plan => plan.id)
}
function requestDelete(ids: number[]) {
  if (busy.value || deleteTargets.value.length) return
  const selected = new Set(ids)
  deleteTargets.value = plans.value.filter(plan => selected.has(plan.id)).map(plan => ({ ...plan }))
  deleteError.value = deleteProgress.value = ''
}
function cancelDelete() { if (!deleting.value) deleteTargets.value = [] }
async function confirmDelete() {
  if (!deleteTargets.value.length || deleting.value || busy.value) return
  const targets = [...deleteTargets.value], version = generation
  deleting.value = busy.value = true
  deleteError.value = deleteProgress.value = error.value = notice.value = ''
  const failed: ScheduledTestPlan[] = [], failures: string[] = []
  let completed = 0
  try {
    for (const plan of targets) {
      if (!active(version)) return
      try {
        await adminAPI.scheduledTests.delete(plan.id)
        if (!active(version)) return
        deletedPlanIds.add(plan.id)
        selectedRuleIds.value = selectedRuleIds.value.filter(id => id !== plan.id)
        plans.value = plans.value.filter(item => item.id !== plan.id)
        for (const item of history.value.filter(item => item.plan_id === plan.id)) delete detailText.value[item.id]
        history.value = history.value.filter(item => item.plan_id !== plan.id)
        if (editing.value?.id === plan.id) editing.value = null
      } catch (e) {
        if (!active(version)) return
        failed.push(plan)
        failures.push(`#${plan.id}: ${extractApiErrorMessage(e, t('autoBPSOps.error'))}`)
      }
      completed++
      deleteProgress.value = t('autoBPSOps.deleteProgress', { completed, total: targets.length })
    }
    deleteTargets.value = failed
    if (failed.length) deleteError.value = `${t('autoBPSOps.deletePartial', { deleted: targets.length - failed.length, failed: failed.length })} ${failures.join(' / ')}`
    else notice.value = t('autoBPSOps.deleted', { count: targets.length })
    await Promise.all([loadPlans(), loadHistory()])
  } catch (e) {
    if (active(version)) error.value = extractApiErrorMessage(e, t('autoBPSOps.error'))
  } finally {
    if (active(version)) deleting.value = busy.value = false
  }
}
watch(() => [auth.user?.id, auth.user?.role], () => {
  generation++
  plans.value = []; history.value = []; cursor.value = 0; groups.value = []; templateGroups.value = []; detailText.value = {}
  selectedRuleIds.value = []; deleteTargets.value = []; editing.value = null; deletedPlanIds.clear()
  error.value = notice.value = deleteError.value = deleteProgress.value = ''; busy.value = deleting.value = false
}, { flush: 'sync' })
onBeforeUnmount(() => { alive = false; generation++ })
onMounted(async () => {
  const version = generation
  await perform(async () => { await Promise.all([loadPlans(), loadHistory(), adminAPI.groups.getAll().then(data => {
    if (!active(version)) return
    groups.value = data.filter(g => g.platform === 'openai')
    templateGroups.value = data.filter(g => g.platform === 'openai' || g.platform === 'composite')
  })]) })
})
</script>
