<template>
  <AppLayout>
    <div class="space-y-6">
      <h1 class="text-2xl font-semibold">{{ t('modelCatalog.title') }}</h1>
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      <p v-if="saved" role="status" class="text-emerald-600">{{ t('modelCatalog.saved') }}</p>
      <form v-if="settings" class="card space-y-4 p-5" @submit.prevent="saveSettings">
        <h2 class="font-semibold">{{ t('modelCatalog.settings') }}</h2>
        <label class="flex items-center gap-2"><input v-model="settings.enabled" type="checkbox" />{{ t('modelCatalog.enabled') }}</label>
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <label>{{ t('modelCatalog.interval') }}<input v-model.number="settings.interval_seconds" class="input mt-1" type="number" min="60" max="86400" required /></label>
          <label>{{ t('modelCatalog.deletionAlert') }}<input v-model.number="settings.deletion_alert_percent" class="input mt-1" type="number" min="1" max="100" required /></label>
          <label>{{ t('modelCatalog.priorityAccounts') }}<input :value="settings.priority_account_ids?.join(', ')" class="input mt-1" @change="settings.priority_account_ids=($event.target as HTMLInputElement).value.split(',').map(v=>Number(v.trim())).filter(v=>v>0)" /></label>
          <label>{{ t('modelCatalog.priorityInterval') }}<input v-model.number="settings.priority_interval_seconds" class="input mt-1" type="number" min="60" max="86400" required /></label>
          <label>{{ t('modelCatalog.priceInterval') }}<input v-model.number="settings.price_interval_seconds" class="input mt-1" type="number" min="60" max="86400" required /></label>
          <label>{{ t('modelCatalog.timeout') }}<input v-model.number="settings.timeout_seconds" class="input mt-1" type="number" min="5" max="120" required /></label>
          <label>{{ t('modelCatalog.concurrency') }}<input v-model.number="settings.concurrency" class="input mt-1" type="number" min="1" max="8" required /></label>
          <label>{{ t('modelCatalog.staleLimit') }}<input v-model.number="settings.stale_seconds" class="input mt-1" type="number" min="300" max="604800" required /></label>
        </div>
        <button class="btn btn-primary" :disabled="busy">{{ t('modelCatalog.save') }}</button>
      </form>
      <section class="card space-y-4 p-5">
        <div class="flex flex-wrap items-end gap-3">
          <label>{{ t('modelCatalog.platform') }}<input :disabled="busy" v-model="platform" class="input mt-1" placeholder="openai" /></label>
          <label>{{ t('modelCatalog.accountID') }}<input :disabled="busy" v-model="accountID" class="input mt-1" type="number" min="1" /></label>
          <button class="btn btn-secondary" :disabled="busy" @click="readCatalog">{{ t('modelCatalog.inspect') }}</button>
          <button v-if="accountID" class="btn btn-primary" :disabled="busy" @click="refreshCatalog">{{ t('modelCatalog.refresh') }}</button>
        </div>
        <p v-if="catalog" class="text-xs text-gray-500">{{ t('modelCatalog.updated', { time: catalog.checked_at ? new Date(catalog.checked_at).toLocaleString() : '—' }) }} · {{ catalog.status }} · {{ catalog.revision?.slice(0, 12) }}</p>
        <p v-if="catalog?.warnings?.length" role="status" class="text-sm text-amber-600">{{ t('modelCatalog.visibilityDrop') }}</p>
        <div class="overflow-auto">
          <table class="w-full text-left text-sm">
            <thead><tr><th class="p-2">{{ t('modelCatalog.model') }}</th><th class="p-2">{{ t('modelCatalog.kind') }}</th><th class="p-2">{{ t('modelCatalog.state') }}</th><th class="p-2">{{ t('modelCatalog.source') }}</th></tr></thead>
            <tbody><tr v-for="model in catalog?.models ?? []" :key="model.platform + ':' + model.id" class="border-t dark:border-dark-600"><td class="p-2 font-mono">{{ model.id }}</td><td class="p-2">{{ model.kind }}</td><td class="p-2">{{ model.lifecycle }} · {{ model.access }}<span v-if="model.missing?.length"> · {{ model.missing.join(', ') }}</span></td><td class="p-2">{{ model.source }}</td></tr></tbody>
          </table>
        </div>
      </section>
      <form v-if="catalog?.account_id && catalog.account_id === Number(accountID)" class="card space-y-3 p-5" @submit.prevent="savePolicy">
        <h2 class="font-semibold">{{ t('modelCatalog.accountPolicy') }}</h2>
        <p class="text-sm text-gray-500">{{ t('modelCatalog.policyHint') }}</p>
        <select v-model="policy.mode" class="input"><option value="legacy">{{ t('modelCatalog.legacy') }}</option><option value="follow">{{ t('modelCatalog.follow') }}</option><option value="fixed">{{ t('modelCatalog.fixed') }}</option></select>
        <label v-if="policy.mode === 'fixed'" class="block">{{ t('modelCatalog.allowed') }}<textarea v-model="allowedText" class="input h-32 font-mono text-xs" /></label>
        <label v-if="policy.mode !== 'legacy'" class="block">{{ t('modelCatalog.exclude') }}<textarea v-model="excludedText" class="input h-24 font-mono text-xs" /></label>
        <button class="btn btn-primary" :disabled="busy">{{ t('modelCatalog.save') }}</button>
        <h3 class="font-medium">{{ t('modelCatalog.history') }}</h3>
        <div v-for="release in history" :key="release.revision + release.created_at" class="flex items-center gap-3 text-xs">
          <time>{{ new Date(release.created_at).toLocaleString() }}</time><code>{{ release.revision.slice(0,12) }}</code><span>{{ release.operation }}</span>
          <button type="button" class="btn btn-secondary" :disabled="busy || release.revision===catalog.revision" @click="restore(release.revision)">{{ t('modelCatalog.restore') }}</button>
        </div>
      </form>
      <form class="card space-y-3 p-5" @submit.prevent="compare">
        <h2 class="font-semibold">{{ t('modelCatalog.compare') }}</h2>
        <p class="text-sm text-gray-500">{{ t('modelCatalog.compareHint') }}</p>
        <label>{{ t('modelCatalog.groupID') }}<input v-model="groupID" class="input" type="number" min="1" required /></label>
        <button class="btn btn-secondary" :disabled="busy">{{ t('modelCatalog.inspect') }}</button>
        <pre v-if="comparison" class="max-h-96 overflow-auto whitespace-pre-wrap text-xs">{{ comparison }}</pre>
      </form>
      <form class="card space-y-3 p-5" @submit.prevent="savePrices">
        <h2 class="font-semibold">{{ t('modelCatalog.priceData') }}</h2>
        <p class="text-sm text-gray-500">{{ t('modelCatalog.priceHint') }}</p>
        <p class="text-xs text-gray-500">{{ priceRevision.slice(0,12) }}</p>
        <textarea v-model="prices" class="input h-48 font-mono text-xs" spellcheck="false" :aria-label="t('modelCatalog.priceData')" />
        <button class="btn btn-primary" :disabled="busy">{{ t('modelCatalog.save') }}</button>
      </form>
      <form class="card space-y-3 p-5" @submit.prevent="saveRegistry">
        <h2 class="font-semibold">{{ t('modelCatalog.registry') }}</h2>
        <p class="text-sm text-gray-500">{{ t('modelCatalog.registryHint') }}</p>
        <textarea v-model="registry" class="input h-64 font-mono text-xs" spellcheck="false" :aria-label="t('modelCatalog.registry')" />
        <button class="btn btn-primary" :disabled="busy">{{ t('modelCatalog.save') }}</button>
      </form>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { getModelCatalog, refreshModelCatalog, getCatalogSettings, saveCatalogSettings, getCatalogRegistry, saveCatalogRegistry, saveAccountCatalogPolicy, getCatalogHistory, rollbackCatalog, getCatalogPrices, saveCatalogPrices, explainCatalog, type CatalogPolicy, type CatalogRelease, type ModelCatalog, type CatalogSyncSettings } from '@/api/admin/modelCatalog'
const { t } = useI18n()
const settings = ref<CatalogSyncSettings | null>(null)
const catalog = ref<ModelCatalog | null>(null)
const registry = ref('[]')
const platform = ref('openai')
const route=useRoute()
const accountID = ref(typeof route.query.account_id==='string'?route.query.account_id:'')
const policy=ref<CatalogPolicy>({mode:'legacy',models:[],excluded:[]})
const allowedText=ref('');const excludedText=ref('');const history=ref<CatalogRelease[]>([])
const prices=ref('{}');const priceRevision=ref('');const groupID=ref('');const comparison=ref('')
function lines(value:string){return [...new Set(value.split(/\r?\n/).map(v=>v.trim()).filter(Boolean))]}
async function loadAccountState(){if(!catalog.value?.account_id){history.value=[];return}
 policy.value=catalog.value.policy??{mode:'legacy',models:[],excluded:[]};allowedText.value=policy.value.models.join('\n');excludedText.value=policy.value.excluded.join('\n');history.value=await getCatalogHistory(catalog.value.account_id)
}
async function savePolicy(){await action(async()=>{await saveAccountCatalogPolicy(Number(accountID.value),{mode:policy.value.mode,models:lines(allowedText.value),excluded:lines(excludedText.value)});saved.value=true})}
async function restore(revision:string){await action(async()=>{await rollbackCatalog(Number(accountID.value),revision);catalog.value=await getModelCatalog({account_id:Number(accountID.value)});await loadAccountState();saved.value=true})}
async function savePrices(){await action(async()=>{const entries:unknown=JSON.parse(prices.value);if(!entries||typeof entries!=='object'||Array.isArray(entries))throw new Error('invalid prices');await saveCatalogPrices(entries as Record<string,unknown>);priceRevision.value=(await getCatalogPrices()).revision;saved.value=true})}
async function compare(){await action(async()=>{comparison.value=JSON.stringify(await explainCatalog(Number(groupID.value)),null,2)})}
const busy = ref(false)
const error = ref('')
const saved = ref(false)
async function action(operation: () => Promise<void>) {
  if (busy.value) return
  busy.value = true; error.value = ''; saved.value = false
  try { await operation() } catch { error.value = t('modelCatalog.error') } finally { busy.value = false }
}
async function readCatalog() { await action(async () => { catalog.value = await getModelCatalog({ account_id: accountID.value ? Number(accountID.value) : undefined, platform: platform.value });await loadAccountState() }) }
async function refreshCatalog() { await action(async () => { catalog.value = await refreshModelCatalog(Number(accountID.value));await loadAccountState() }) }
async function saveSettings() { if (settings.value) await action(async () => { await saveCatalogSettings(settings.value!); saved.value = true }) }
async function saveRegistry() {
  let models: unknown
  try { models = JSON.parse(registry.value) } catch { error.value = t('modelCatalog.registryInvalid'); return }
  if (!Array.isArray(models)) { error.value = t('modelCatalog.registryInvalid'); return }
  const entries = models
  await action(async () => { await saveCatalogRegistry(entries); saved.value = true })
}
onMounted(() => action(async () => {
  const [config, entries, priceData] = await Promise.all([getCatalogSettings(), getCatalogRegistry(),getCatalogPrices()])
  settings.value = config; registry.value = JSON.stringify(entries, null, 2)
  prices.value=JSON.stringify(priceData.supplemental??{},null,2);priceRevision.value=priceData.revision??''
  catalog.value = await getModelCatalog({ account_id:accountID.value?Number(accountID.value):undefined, platform: platform.value });await loadAccountState()
}))
</script>
