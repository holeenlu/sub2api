<template>
  <AppLayout>
    <div class="space-y-5">
      <SmartOpsNav />
      <header><h1 class="text-2xl font-bold">{{ t('autoBPSOps.title') }}</h1><p class="mt-2 text-sm text-gray-500">{{ t('autoBPSOps.hint') }}</p></header>
      <RouterLink to="/admin/accounts" class="text-primary-600">{{ t('autoBPSOps.accounts') }}</RouterLink>
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p><p v-if="notice" role="status" class="text-emerald-600">{{ notice }}</p>
      <section class="card p-5">
        <header class="flex justify-between"><h2 class="font-semibold">{{ t('autoBPSOps.rules') }}</h2><button class="btn btn-secondary" :disabled="busy" @click="refresh">{{ t('autoBPSOps.refresh') }}</button></header>
        <p class="my-3 text-sm text-gray-500">{{ t('autoBPSOps.notice') }}</p>
        <div class="overflow-x-auto"><table class="w-full text-left text-sm"><thead><tr><th v-for="key in ['account','model','cron','state','next']" :key="key" class="p-2">{{ t(`autoBPSOps.${key}`) }}</th><th /></tr></thead><tbody>
          <tr v-for="plan in plans" :key="plan.id" class="border-t dark:border-dark-700"><td class="p-2">{{ plan.account_name || plan.account_id }} #{{ plan.account_id }}</td><td class="p-2">{{ plan.model_id }}</td><td class="p-2 font-mono">{{ plan.cron_expression }}</td><td class="p-2">{{ t(plan.enabled ? 'autoBPSOps.enabled' : 'autoBPSOps.paused') }}</td><td class="p-2">{{ when(plan.next_run_at) }}</td><td class="flex gap-2 p-2"><button class="btn btn-secondary" :disabled="busy" @click="edit(plan)">{{ t('autoBPSOps.edit') }}</button><button class="btn btn-secondary" :disabled="busy" @click="toggle(plan)">{{ t(plan.enabled ? 'autoBPSOps.pause' : 'autoBPSOps.resume') }}</button><button class="btn btn-secondary" :disabled="busy || !plan.enabled" @click="run(plan)">{{ t('autoBPSOps.run') }}</button></td></tr>
        </tbody></table></div><p v-if="!plans.length" class="py-4 text-gray-500">{{ t('autoBPSOps.empty') }}</p>
      </section>
      <form v-if="editing" class="card space-y-4 p-5" @submit.prevent="save">
        <h2 class="font-semibold">{{ t('autoBPSOps.edit') }} #{{ editing.account_id }}</h2>
        <fieldset :disabled="busy" class="space-y-4"><div class="grid gap-4 sm:grid-cols-2"><label>{{ t('autoBPSOps.model') }}<input v-model="model" required class="input w-full" /></label><QualityProbeSchedule v-model="cron" /></div>
          <QualityBPSSettings v-model:bps="bps" v-model:auto-restore="autoRestore" :target-groups="groups" />
          <div class="flex gap-2"><button class="btn btn-primary" type="submit">{{ t('autoBPSOps.save') }}</button><button class="btn btn-secondary" type="button" @click="editing = null">{{ t('autoBPSOps.cancel') }}</button></div>
        </fieldset>
      </form>
      <section class="card p-5"><h2 class="mb-3 font-semibold">{{ t('autoBPSOps.history') }}</h2>
        <div v-for="item in history" :key="item.id" class="border-t py-3 text-sm dark:border-dark-700"><p>{{ item.account_name || item.account_id }} · {{ when(item.started_at) }} · {{ item.quality_judgment?.verdict || item.status }} · {{ item.quality_action || '—' }}</p><details @toggle="loadDetail($event, item)"><summary class="cursor-pointer text-primary-600">{{ t('autoBPSOps.details') }}</summary><pre class="mt-2 whitespace-pre-wrap break-words">{{ detailText[item.id] || item.error_message || item.quality_judgment?.reason }}</pre></details></div>
        <p v-if="!history.length" class="text-gray-500">{{ t('autoBPSOps.empty') }}</p><button v-if="cursor" class="btn btn-secondary mt-3" :disabled="busy" @click="older">{{ t('autoBPSOps.more') }}</button>
      </section>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import SmartOpsNav from '@/components/admin/operations/SmartOpsNav.vue'
import QualityBPSSettings from '@/components/admin/operations/QualityBPSSettings.vue'
import QualityProbeSchedule from '@/components/admin/operations/QualityProbeSchedule.vue'
import { apiClient } from '@/api/client'
import { adminAPI } from '@/api/admin'
import type { ScheduledTestPlan, ScheduledTestResult } from '@/types'
import { defaultQualityBPS, qualityBPSForm, qualityBPSError, qualityBPSPayload } from '@/utils/qualityRulePatch'
import { extractApiErrorMessage } from '@/utils/apiError'
const { t } = useI18n()
type History = ScheduledTestResult & { account_id: number; account_name: string }
const plans = ref<ScheduledTestPlan[]>([]), history = ref<History[]>([]), cursor = ref(0)
const detailText = ref<Record<number, string>>({})
const busy = ref(false), error = ref(''), notice = ref(''), editing = ref<ScheduledTestPlan | null>(null)
const bps = ref(defaultQualityBPS()), autoRestore = ref(true), model = ref(''), cron = ref('')
const groups = ref<{ id: number; name: string }[]>([])
const when = (value?: string | null) => value ? new Date(value).toLocaleString() : '—'
async function perform(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true; error.value = ''; notice.value = ''
  try { await action() } catch (e) { error.value = extractApiErrorMessage(e, t('autoBPSOps.error')) } finally { busy.value = false }
}
async function loadDetail(event: Event, item: History) {
  if (!(event.target as HTMLDetailsElement).open || detailText.value[item.id]) return
  try {
    const { data } = await apiClient.get<ScheduledTestResult>('/admin/scheduled-test-plans/' + item.plan_id + '/results/' + item.id)
    detailText.value[item.id] = data.response_text || data.error_message
  } catch (e) { error.value = extractApiErrorMessage(e, t('autoBPSOps.error')) }
}
async function loadPlans() { plans.value = (await apiClient.get<ScheduledTestPlan[]>('/admin/account-quality-plans')).data || [] }
async function loadHistory(append = false) {
  const { data } = await apiClient.get<{ items: History[]; next_cursor: number }>('/admin/account-quality-results', { params: { before_id: append ? cursor.value : 0 } })
  history.value = append ? [...history.value, ...(data.items || [])] : (data.items || []); cursor.value = data.next_cursor || 0
}
async function refresh() { await perform(async () => { await Promise.all([loadPlans(), loadHistory()]) }) }
async function older() { await perform(() => loadHistory(true)) }
function edit(plan: ScheduledTestPlan) {
  editing.value = plan; model.value = plan.model_id; cron.value = plan.cron_expression
  bps.value = qualityBPSForm(plan.pelican_config?.quality?.bps); autoRestore.value = !!plan.pelican_config?.quality?.auto_restore
}
async function toggle(plan: ScheduledTestPlan) { await perform(async () => { await adminAPI.scheduledTests.update(plan.id, { enabled: !plan.enabled }); await loadPlans() }) }
async function run(plan: ScheduledTestPlan) { await perform(async () => { await apiClient.post(`/admin/account-quality-plans/${plan.id}/run`); notice.value = t('autoBPSOps.queued'); await loadPlans() }) }
async function save() {
  if (!editing.value) return
  const validation = qualityBPSError(bps.value)
  if (validation) { error.value = t(validation); return }
  const plan = editing.value
  await perform(async () => {
    await adminAPI.scheduledTests.update(plan.id, { model_id: model.value.trim(), cron_expression: cron.value.trim(), auto_recover: false,
      pelican_config: { reasoning_effort: 'high', ...plan.pelican_config, question_kind: 'state_probe', prompt: '', parallel_count: 1,
        quality: { action: 'enable_bps', expected_answer: '', remove_group_ids: [], auto_restore: autoRestore.value, bps: qualityBPSPayload(bps.value) } } })
    editing.value = null; notice.value = t('autoBPSOps.saved'); await loadPlans()
  })
}
onMounted(async () => {
  await perform(async () => { await Promise.all([loadPlans(), loadHistory(), adminAPI.groups.getAll().then(data => { groups.value = data.filter(g => g.platform === 'openai') })]) })
})
</script>
