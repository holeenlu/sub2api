<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-[1600px] space-y-5 p-4 sm:p-6">
      <header class="flex flex-wrap items-center justify-between gap-3">
        <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">{{ t('modelManagement.title') }}</h1>
        <div class="inline-flex rounded-xl bg-gray-100 p-1 dark:bg-dark-800" role="tablist" :aria-label="t('modelManagement.title')">
          <button v-for="item in tabs" :key="item" type="button" role="tab" :aria-selected="tab === item" class="rounded-lg px-5 py-2 text-sm font-medium" :class="tab === item ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700' : 'text-gray-500'" @click="changeTab(item)">{{ t(`modelManagement.${item}`) }}</button>
        </div>
      </header>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex gap-2">
          <button v-for="provider in providers" :key="provider.id" class="rounded-lg border px-4 py-2 text-sm font-medium" :class="platform === provider.id ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'border-gray-200 dark:border-dark-600'" :disabled="busy" @click="switchPlatform(provider.id)">{{ provider.name }}</button>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <template v-if="tab === 'models'">
            <span v-if="dirty" class="mr-1 text-xs text-amber-600">{{ t('modelManagement.unsaved') }}</span>
            <button v-if="canManage && auth.can('accounts.read')" class="btn btn-secondary" :disabled="busy || !saved" @click="openSync">{{ t('modelManagement.syncCatalog') }}</button>
            <button v-if="canManage" class="btn btn-secondary" :disabled="busy || !saved" @click="confirmAction('resetConfirm', reset)">{{ t('modelManagement.reset') }}</button>
            <button v-if="canManage" class="btn btn-primary" :disabled="busy || !dirty" @click="savePolicy">{{ t('modelManagement.saveChanges') }}</button>
          </template>
          <button v-else-if="canPrice" class="btn btn-secondary" :disabled="busy || !prices" @click="syncPrices">{{ t('modelManagement.syncPrices') }}</button>
        </div>
      </div>
      <div v-if="loadError" class="flex items-center justify-between rounded-xl border border-red-200 p-4 text-sm text-red-600"><span>{{ loadError }}</span><button class="btn btn-secondary" @click="load">{{ t('modelManagement.retry') }}</button></div>
      <div v-if="loading" class="py-16 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <template v-else-if="draft && saved">
        <div class="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500">
          <span>{{ t('modelManagement.modelCount', { count: cards.length }) }}<span v-if="tab === 'pricing'"> · {{ t('modelManagement.priceUnit') }}</span><span v-if="saved.updated_at"> · {{ t('modelManagement.updated') }} {{ new Date(saved.updated_at).toLocaleString(locale) }}</span></span>
          <input v-model="search" class="input w-full sm:w-64" :placeholder="t('modelManagement.search')" :aria-label="t('modelManagement.search')" />
        </div>
        <template v-if="tab === 'models'">
          <section v-if="platform === 'openai' && mapping" class="rounded-2xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900 sm:p-5">
            <div class="mb-4 flex flex-wrap items-center justify-between gap-2"><h2 class="font-semibold">{{ t('modelManagement.mappingTitle') }}</h2><button v-if="canManage" class="btn btn-secondary btn-sm" :disabled="busy || !mappingDirty" @click="saveMappings">{{ t('modelManagement.saveMappings') }}</button></div>
            <div class="space-y-2">
              <div v-for="(row, index) in mappingRows" :key="index" class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem] items-center gap-2 sm:max-w-3xl">
                <input v-model="row.alias" class="input min-w-0 font-mono text-xs" :disabled="row.fixed || !canManage || busy" :aria-label="t('modelManagement.alias')" />
                <select v-model="row.target" class="input min-w-0 font-mono text-xs" :disabled="!canManage || busy" :aria-label="t('modelManagement.target')"><option v-if="!mappingTargets.includes(row.target)" :value="row.target">{{ row.target }} — {{ t('modelManagement.unavailable') }}</option><option v-for="id in mappingTargets" :key="id" :value="id">{{ id }}</option></select>
                <button v-if="canManage && !row.fixed" class="rounded p-2 text-gray-400 hover:text-red-500" :disabled="busy" :aria-label="t('common.delete')" @click="mappingRows.splice(index, 1)">×</button>
                <button v-else-if="canManage" class="rounded p-2 text-gray-400" :disabled="busy" :title="t('modelManagement.reset')" :aria-label="t('modelManagement.reset')" @click="row.target = 'gpt-6-luna'">↺</button>
              </div>
              <button v-if="canManage" class="mt-2 text-sm font-medium text-primary-600" :disabled="busy" @click="mappingRows.push({ alias: '', target: mappingTargets[0] || '', fixed: false })">+ {{ t('modelManagement.addMapping') }}</button>
            </div>
          </section>
          <div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            <article v-for="model in chatCards" :key="model.id" class="flex min-w-0 flex-col rounded-2xl border bg-white p-5 dark:bg-dark-900" :class="model.enabled ? 'border-gray-200 dark:border-dark-700' : 'border-gray-200 bg-gray-50/70 dark:border-dark-800'">
              <div class="mb-3 flex items-start justify-between gap-3">
                <div class="min-w-0"><h3 class="break-words font-semibold text-gray-900 dark:text-gray-100">{{ model.display_name || model.id }}</h3><button class="mt-1 break-all text-left font-mono text-xs text-gray-500 hover:text-primary-600" :aria-label="`${t('common.copy')} ${model.id}`" @click="copyToClipboard(model.id)">{{ model.id }}</button></div>
                <label class="flex shrink-0 items-center gap-2 text-xs"><input v-model="model.enabled" type="checkbox" class="h-4 w-4 accent-primary-600" :disabled="!canManage || busy" />{{ t('modelManagement.enabled') }}</label>
              </div>
              <div class="mb-4 flex flex-wrap gap-2 text-xs"><span class="rounded-md px-2 py-1" :class="saved.models[model.id]?.enabled ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300' : 'bg-gray-100 text-gray-500 dark:bg-dark-700'">{{ t(saved.models[model.id]?.enabled ? 'modelManagement.public' : 'modelManagement.hidden') }}</span><span v-if="contextOf(model)" class="rounded-md bg-gray-50 px-2 py-1 text-gray-500 dark:bg-dark-800">{{ t('modelManagement.context') }} {{ compact(contextOf(model)) }}</span><span v-if="model.params?.default_effort" class="rounded-md bg-gray-50 px-2 py-1 text-gray-500 dark:bg-dark-800">{{ model.params.default_effort }}</span></div>
              <div class="mt-auto flex items-end justify-between gap-2"><div class="min-w-0 text-xs text-gray-500"><template v-if="priceFor(model.id)?.effective">{{ t('modelManagement.inputOutput') }}<br /><span class="mt-1 inline-block text-sm tabular-nums text-gray-700 dark:text-gray-300">${{ priceFor(model.id)?.effective?.input }} / ${{ priceFor(model.id)?.effective?.output }}</span><span class="ml-1">/ 1M</span></template><span v-else>{{ t('modelManagement.unpriced') }}</span></div><button class="btn btn-secondary btn-sm shrink-0" @click="editingID = model.id">{{ t('modelManagement.configure') }}</button></div>
            </article>
          </div>
          <section v-if="imageCards.length" class="rounded-2xl border border-gray-200 p-4 dark:border-dark-700 sm:p-5"><h2 class="mb-3 font-semibold">{{ t('modelManagement.images') }}</h2><div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3"><label v-for="model in imageCards" :key="model.id" class="flex min-w-0 items-center gap-3 rounded-xl bg-gray-50 p-3 dark:bg-dark-800"><input v-model="model.enabled" type="checkbox" :disabled="!canManage || busy" class="h-4 w-4 shrink-0 accent-primary-600" /><span class="break-all font-mono text-xs">{{ model.id }}</span></label></div></section>
          <details v-if="platform === 'anthropic'" class="rounded-2xl border border-gray-200 p-4 dark:border-dark-700 sm:p-5">
            <summary class="cursor-pointer font-semibold">{{ t('modelManagement.advanced') }}</summary>
            <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <label class="text-sm">{{ t('modelManagement.catalogMode') }}<select v-model="draft.catalog_mode" class="input mt-1 w-full" :disabled="!canManage"><option value="policy_only">{{ t('modelManagement.policyCatalog') }}</option><option value="worker_intersect_policy">{{ t('modelManagement.policyCatalogLegacy') }}</option><option value="worker_only">{{ t('modelManagement.upstreamCatalog') }}</option></select></label>
              <label class="text-sm">{{ t('modelManagement.maxTokensDefault') }}<input v-model.number="draft.defaults.max_tokens" type="number" min="1" class="input mt-1 w-full" :disabled="!canManage" /></label>
              <label class="text-sm">{{ t('modelManagement.thinkingBudget') }}<input v-model.number="draft.defaults.thinking_fallback_budget" type="number" min="1024" class="input mt-1 w-full" :disabled="!canManage" /></label>
              <label class="flex items-center gap-2 text-sm"><input v-model="draft.defaults.enabled" type="checkbox" :disabled="!canManage" />{{ t('modelManagement.allowUnknown') }}</label>
              <label class="flex items-center gap-2 text-sm"><input v-model="draft.defaults.normalize_thinking" type="checkbox" :disabled="!canManage" />{{ t('modelManagement.normalizeThinking') }}</label>
              <label class="flex items-center gap-2 text-sm"><input v-model="draft.defaults.strip_context_1m" type="checkbox" :disabled="!canManage" />{{ t('modelManagement.strip1m') }}</label>
              <label class="text-sm sm:col-span-2 lg:col-span-3">{{ t('modelManagement.contextWhitelist') }}<input :value="draft.defaults.context_1m_whitelist.join(', ')" class="input mt-1 w-full font-mono text-xs" :disabled="!canManage" @change="draft.defaults.context_1m_whitelist = csv(($event.target as HTMLInputElement).value)" /></label>
            </div>
          </details>
        </template>
        <section v-else class="relative overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
          <div class="overflow-x-auto overscroll-x-contain">
            <table class="w-full min-w-[640px] text-left text-sm">
              <caption class="sr-only">{{ t('modelManagement.pricing') }}</caption>
              <thead class="bg-gray-50 text-xs font-medium text-gray-500 dark:bg-dark-800 dark:text-gray-400">
                <tr>
                  <th scope="col" class="px-4 py-3">{{ t('modelManagement.models') }}</th>
                  <th scope="col" class="whitespace-nowrap px-4 py-3 text-right">{{ t('modelManagement.input') }}</th>
                  <th scope="col" class="whitespace-nowrap px-4 py-3 text-right">{{ t('modelManagement.output') }}</th>
                  <th scope="col" class="whitespace-nowrap px-4 py-3 text-right">{{ t('modelManagement.cache_read') }}</th>
                  <th scope="col" class="px-4 py-3">{{ t('modelManagement.source') }}</th>
                  <th scope="col" class="px-4 py-3"><span class="sr-only">{{ t('modelManagement.editPrice') }}</span></th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                <tr v-for="row in filteredPrices" :key="row.id" class="transition-colors hover:bg-gray-50 dark:hover:bg-dark-800/60">
                  <th scope="row" class="px-4 py-2.5 text-left font-mono text-xs font-medium text-gray-800 dark:text-gray-200">{{ row.id }}</th>
                  <td class="whitespace-nowrap px-4 py-2.5 text-right font-medium tabular-nums text-gray-800 dark:text-gray-200">{{ money(row.effective?.input) }}</td>
                  <td class="whitespace-nowrap px-4 py-2.5 text-right font-medium tabular-nums text-gray-800 dark:text-gray-200">{{ money(row.effective?.output) }}</td>
                  <td class="whitespace-nowrap px-4 py-2.5 text-right font-medium tabular-nums text-gray-800 dark:text-gray-200">{{ money(row.effective?.cache_read) }}</td>
                  <td class="px-4 py-2.5">
                    <span class="inline-flex whitespace-nowrap rounded-md px-2 py-1 text-xs font-medium" :class="row.source === 'override' ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-400'">{{ row.source === 'models.dev' ? 'models.dev' : t(`modelManagement.${row.source}`) }}</span>
                  </td>
                  <td class="px-4 py-2.5 text-right">
                    <button v-if="canPrice" class="btn btn-secondary btn-sm whitespace-nowrap" :disabled="busy" :aria-label="`${t('modelManagement.editPrice')} ${row.id}`" @click="editingPrice = row">{{ t('modelManagement.editPrice') }}</button>
                  </td>
                </tr>
                <tr v-if="!filteredPrices.length"><td colspan="6" class="px-4 py-10 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accounts.noMatchingModels') }}</td></tr>
              </tbody>
            </table>
          </div>
        </section>
      </template>
    </div>
    <BaseDialog :show="!!editing" :title="editing?.display_name || editingID" width="wide" @close="editingID = ''">
      <div v-if="editing" class="space-y-5">
        <div class="flex flex-wrap items-center justify-between gap-2"><code class="break-all text-xs text-gray-500">{{ editing.id }}</code><label class="flex items-center gap-2 text-sm"><input v-model="editing.enabled" type="checkbox" :disabled="!canManage || busy" />{{ t('modelManagement.enabled') }}</label></div>
        <template v-if="platform === 'anthropic'">
          <div class="grid gap-4 sm:grid-cols-2">
            <label class="model-field">{{ t('modelManagement.displayName') }}<input v-model="editing.display_name" class="input" :disabled="!canManage" /></label>
            <label class="model-field">{{ t('modelManagement.context') }}<input v-if="editing.capabilities" v-model.number="editing.capabilities.context_window" class="input" type="number" min="1" :disabled="!canManage" /></label>
            <label class="model-field">{{ t('modelManagement.maxTokensDefault') }}<input v-model.number="editing.params.max_tokens_default" type="number" min="1" class="input" :disabled="!canManage" /></label>
            <label class="model-field">{{ t('modelManagement.maxTokensCap') }}<input v-model.number="editing.params.max_tokens_cap" type="number" min="1" class="input" :disabled="!canManage" /></label>
            <label class="model-field">{{ t('modelManagement.thinkingBudget') }}<input v-model.number="editing.params.thinking_fallback_budget" type="number" min="1024" class="input" :disabled="!canManage" /></label>
            <label class="model-field">{{ t('modelManagement.effort') }}<select v-model="editing.params.default_effort" class="input" :disabled="!canManage || !editing.capabilities?.supports_effort"><option value="">{{ t('modelManagement.inherit') }}</option><option v-for="effort in ['low', 'medium', 'high', 'max']" :key="effort">{{ effort }}</option></select></label>
            <label v-for="field in (['on_adaptive', 'on_enabled'] as const)" :key="field" class="model-field">{{ t(`modelManagement.${field}`) }}<select v-model="editing.params[field]" class="input" :disabled="!canManage"><option v-for="action in ['passthrough', 'strip', field === 'on_adaptive' ? 'convert_to_enabled' : 'convert_to_adaptive']" :key="action" :value="action">{{ t(`modelManagement.${action}`) }}</option></select></label>
            <label class="model-field sm:col-span-2">{{ t('modelManagement.aliases') }}<input :value="editing.aliases.join(', ')" class="input font-mono text-xs" :disabled="!canManage" @change="editing.aliases = csv(($event.target as HTMLInputElement).value)" /></label>
            <label class="model-field">{{ t('modelManagement.beta1m') }}<select :value="editing.betas.pass_context_1m == null ? 'inherit' : editing.betas.pass_context_1m ? 'pass' : 'strip'" class="input" :disabled="!canManage" @change="setBeta(($event.target as HTMLSelectElement).value)"><option value="inherit">{{ t('modelManagement.inherit') }}</option><option value="pass">{{ t('modelManagement.passthrough') }}</option><option value="strip">{{ t('modelManagement.strip') }}</option></select></label>
          </div>
        </template>
        <dl v-else-if="editing.catalog" class="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)] gap-x-4 gap-y-4 text-sm"><template v-for="fact in catalogFacts" :key="fact.key"><dt class="text-gray-500">{{ t(`modelManagement.${fact.key}`) }}</dt><dd class="break-words text-right font-mono text-xs">{{ fact.value || '—' }}</dd></template></dl>
        <p v-else class="rounded-xl bg-gray-50 p-4 text-sm text-gray-500 dark:bg-dark-800">{{ t('modelManagement.noFacts') }}</p>
      </div>
      <template #footer><div class="flex w-full flex-wrap items-center justify-between gap-2"><span class="text-xs text-gray-500">{{ t('modelManagement.draftHint') }}</span><button class="btn btn-primary" @click="editingID = ''">{{ t('modelManagement.done') }}</button></div></template>
    </BaseDialog>
    <BaseDialog :show="syncOpen" :title="t('modelManagement.syncCatalog')" @close="syncOpen = false"><p class="mb-4 text-sm text-gray-500">{{ t('modelManagement.selectSource') }}</p><div class="max-h-72 space-y-2 overflow-y-auto"><label v-for="source in sources" :key="source.id" class="flex items-center gap-3 rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-700"><input v-model="sourceIDs" :value="source.id" type="checkbox" :disabled="busy" /><span class="min-w-0 break-all">{{ source.name }}<span class="ml-2 text-xs text-gray-400">{{ source.type }}</span></span></label><p v-if="!sources.length" class="text-sm text-gray-500">{{ t('modelManagement.noSources') }}</p></div><template #footer><button class="btn btn-primary" :disabled="busy || !sourceIDs.length" @click="syncCatalog">{{ t('modelManagement.syncCatalog') }}</button></template></BaseDialog>
    <BaseDialog :show="!!pendingConfirm" :title="t('modelManagement.confirmTitle')" width="narrow" @close="pendingConfirm = null"><p class="text-sm text-gray-600 dark:text-gray-300">{{ t(`modelManagement.${pendingConfirm?.message || 'discardConfirm'}`) }}</p><template #footer><button class="btn btn-secondary" @click="pendingConfirm = null">{{ t('common.cancel') }}</button><button class="btn btn-primary" @click="runConfirmed">{{ t('common.confirm') }}</button></template></BaseDialog>
    <ModelPriceDialog :entry="editingPrice" :platform="platform" :busy="busy" :image="!!editingPrice && draft?.models[editingPrice.id]?.kind === 'image'" @close="editingPrice = null" @save="savePrice" />
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ModelPriceDialog from '@/components/admin/models/ModelPriceDialog.vue'
import { modelsAPI, type ManagedModel, type ModelMappings, type ModelPolicy, type ModelPrice, type ModelPrices, type ModelSource, type PriceEntry } from '@/api/admin/models'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'
const { t, locale } = useI18n()
const app = useAppStore(), auth = useAuthStore()
const { copyToClipboard } = useClipboard()
const providers = [{ id: 'anthropic', name: 'Anthropic' }, { id: 'openai', name: 'OpenAI' }]
const tabs = computed<('models' | 'pricing')[]>(() => auth.can('billing.cost.read') ? ['models', 'pricing'] : ['models'])
const tab = ref<'models' | 'pricing'>('models'), platform = ref('anthropic'), search = ref('')
const saved = ref<ModelPolicy | null>(null), draft = ref<ModelPolicy | null>(null), mapping = ref<ModelMappings | null>(null), prices = ref<ModelPrices | null>(null)
const mappingRows = ref<{ alias: string; target: string; fixed: boolean }[]>([])
const loading = ref(false), busy = ref(false), loadError = ref(''), editingID = ref(''), editingPrice = ref<PriceEntry | null>(null)
const syncOpen = ref(false), sources = ref<ModelSource[]>([]), sourceIDs = ref<number[]>([])
const pendingConfirm = ref<{ message: string; action: () => void | Promise<void> } | null>(null)
const canManage = computed(() => auth.can('models.manage')), canPrice = computed(() => canManage.value && auth.can('billing.rates.update'))
const dirty = computed(() => JSON.stringify(saved.value) !== JSON.stringify(draft.value))
const mappingValue = () => Object.fromEntries(mappingRows.value.map(row => [row.alias, row.target]))
const mappingDirty = computed(() => !!mapping.value && JSON.stringify(mapping.value.mappings) !== JSON.stringify(mappingValue()))
const cards = computed(() => Object.values(draft.value?.models || {}).filter(m => `${m.id} ${m.display_name}`.toLowerCase().includes(search.value.toLowerCase())).sort((a, b) => a.sort - b.sort || a.id.localeCompare(b.id)))
const chatCards = computed(() => cards.value.filter(m => m.kind !== 'image')), imageCards = computed(() => cards.value.filter(m => m.kind === 'image'))
const mappingTargets = computed(() => Object.values(saved.value?.models || {}).filter(m => m.enabled && m.kind === 'chat').map(m => m.id))
const editing = computed(() => draft.value?.models[editingID.value])
const filteredPrices = computed(() => prices.value?.entries.filter(row => row.id.toLowerCase().includes(search.value.toLowerCase())) || [])
const catalogFacts = computed(() => { const c = editing.value?.catalog; return [{ key: 'context', value: c?.context_window }, { key: 'maxContext', value: c?.max_context_window }, { key: 'maxOutput', value: c?.max_output_tokens }, { key: 'effort', value: c?.default_reasoning_level }, { key: 'reasoningLevels', value: c?.supported_reasoning_levels?.join(', ') }, { key: 'modalities', value: c?.input_modalities?.join(', ') }] })
const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value))
const csv = (value: string) => value.split(',').map(v => v.trim()).filter(Boolean)
const compact = (value: number) => new Intl.NumberFormat(locale.value, { notation: 'compact', maximumFractionDigits: 1 }).format(value)
const money = (value?: number | null) => value == null ? '—' : `$${new Intl.NumberFormat(locale.value, { maximumSignificantDigits: 15 }).format(value)}`
const contextOf = (m: ManagedModel) => Number(m.catalog?.context_window || m.capabilities?.context_window || 0)
const priceFor = (id: string) => prices.value?.entries.find(p => p.id === id)
function accept(value: ModelPolicy) { saved.value = clone(value); draft.value = clone(value) }
function acceptMappings(value: ModelMappings) { mapping.value = clone(value); mappingRows.value = Object.entries(value.mappings).map(([alias, target]) => ({ alias, target, fixed: alias === 'codex-auto-review' })).sort((a, b) => Number(b.fixed) - Number(a.fixed)); mapping.value.mappings = mappingValue() }
async function load() {
  loading.value = true; loadError.value = ''; editingID.value = ''; prices.value = null
  try {
    accept(await modelsAPI.policy(platform.value))
    if (platform.value === 'openai') acceptMappings(await modelsAPI.mappings())
    if (auth.can('billing.cost.read')) prices.value = await modelsAPI.prices(platform.value)
  } catch (error) { loadError.value = extractApiErrorMessage(error, t('modelManagement.loadFailed')) }
  finally { loading.value = false }
}
async function perform(action: () => Promise<void>) { busy.value = true; try { await action(); app.showSuccess(t('modelManagement.saved')) } catch (error) { app.showError(extractApiErrorMessage(error, t('modelManagement.saveFailed'))) } finally { busy.value = false } }
function confirmAction(message: string, action: () => void | Promise<void>) { pendingConfirm.value = { message, action } }
function runConfirmed() { const action = pendingConfirm.value?.action; pendingConfirm.value = null; void action?.() }
function switchPlatform(next: string) { if (next === platform.value) return; const action = async () => { platform.value = next; search.value = ''; mapping.value = null; mappingRows.value = []; await load() }; if (dirty.value || mappingDirty.value) confirmAction('discardConfirm', action); else void action() }
function changeTab(next: 'models' | 'pricing') { tab.value = next }
function savePolicy() { if (!draft.value) return; const value = clone(draft.value); void perform(async () => accept(await modelsAPI.save(value))) }
function reset() { if (!saved.value) return; void perform(async () => accept(await modelsAPI.reset(platform.value, saved.value!.version))) }
async function openSync() { try { sources.value = await modelsAPI.sources(platform.value); sourceIDs.value = []; syncOpen.value = true } catch (error) { app.showError(extractApiErrorMessage(error, t('modelManagement.loadFailed'))) } }
function syncCatalog() {
  if (!saved.value || !draft.value) return
  void perform(async () => {
    const previous = clone(saved.value!)
    const result = await modelsAPI.sync(platform.value, previous.version, sourceIDs.value)
    const changes = clone(draft.value!)
    accept(result)
    const updated = draft.value!
    for (const [id, model] of Object.entries(changes.models)) {
      if (!previous.models[id] || JSON.stringify(previous.models[id]) === JSON.stringify(model)) continue
      if (updated.models[id]) updated.models[id] = { ...model, catalog: updated.models[id].catalog, source_account_ids: updated.models[id].source_account_ids }
    }
    if (JSON.stringify(previous.defaults) !== JSON.stringify(changes.defaults)) updated.defaults = changes.defaults
    if (previous.catalog_mode !== changes.catalog_mode) updated.catalog_mode = changes.catalog_mode
    syncOpen.value = false
  })
}
function saveMappings() {
  if (!mapping.value) return
  const aliases = mappingRows.value.map(r => r.alias.toLowerCase())
  if (mappingRows.value.some(r => !r.alias || /\s/.test(r.alias)) || new Set(aliases).size !== aliases.length) { app.showError(t('modelManagement.invalidAlias')); return }
  void perform(async () => acceptMappings(await modelsAPI.saveMappings({ version: mapping.value!.version, mappings: mappingValue() })))
}
function savePrice(price: ModelPrice | null) { if (!editingPrice.value || !prices.value) return; const id = editingPrice.value.id; void perform(async () => { await modelsAPI.savePrice(platform.value, id, prices.value!.version, price); prices.value = await modelsAPI.prices(platform.value); editingPrice.value = null }) }
function syncPrices() { if (!prices.value) return; void perform(async () => { await modelsAPI.syncPrices(platform.value, prices.value!.version); prices.value = await modelsAPI.prices(platform.value) }) }
function setBeta(value: string) { if (!editing.value) return; if (value === 'inherit') delete editing.value.betas.pass_context_1m; else editing.value.betas.pass_context_1m = value === 'pass' }
onBeforeRouteLeave(() => !(dirty.value || mappingDirty.value) || window.confirm(t('modelManagement.discardConfirm')))
onMounted(load)
</script>
<style scoped>
.model-field { @apply flex min-w-0 flex-col gap-1 text-sm text-gray-600 dark:text-gray-400; }
</style>
