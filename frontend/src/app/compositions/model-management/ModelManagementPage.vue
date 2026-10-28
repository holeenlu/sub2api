<template>
  <section class="mx-auto max-w-7xl space-y-5 p-4 sm:p-6" :aria-label="t('workspace.title')" :aria-busy="loading || busy || priceBusy">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('workspace.title') }}</h2>
        <p v-if="hasDirty" class="mt-1 text-sm text-amber-700 dark:text-amber-300" role="status">{{ t('workspace.unsaved') }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-3">
        <label class="flex items-center gap-2 text-sm">
          {{ t('workspace.platform') }}
          <select :value="platform" class="input" :disabled="busy || priceBusy" @change="changePlatform">
            <option value="anthropic">Anthropic</option>
            <option value="openai">OpenAI</option>
          </select>
        </label>
        <button type="button" class="btn btn-secondary" :disabled="loading || busy || priceBusy || !canRead" @click="requestReload">{{ t('workspace.reload') }}</button>
      </div>
    </header>
    <p v-if="!canRead" role="alert" class="rounded-lg border border-amber-300 p-4 text-sm">{{ t('workspace.denied') }}</p>
    <template v-else>
      <nav :aria-label="t('workspace.title')" class="flex gap-2 border-b border-gray-200 pb-2 dark:border-dark-700">
        <button type="button" class="btn" :class="tab === 'models' ? 'btn-primary' : 'btn-secondary'" :aria-pressed="tab === 'models'" @click="tab = 'models'">{{ t('workspace.models') }}</button>
        <button v-if="canReadPrices" type="button" class="btn" :class="tab === 'pricing' ? 'btn-primary' : 'btn-secondary'" :aria-pressed="tab === 'pricing'" @click="openPrices">{{ t('workspace.pricing') }}</button>
      </nav>
      <p v-if="completedVersion !== null && !hasDirty" role="status" class="rounded-lg border border-emerald-300 p-3 text-sm">{{ t('workspace.saved', { version: completedVersion }) }}</p>
      <p v-if="conflict" role="alert" class="rounded-lg border border-amber-300 p-4 text-sm">{{ t('workspace.conflict') }}</p>
      <p v-else-if="commandError" role="alert" class="rounded-lg border border-red-300 p-4 text-sm">{{ t('workspace.writeFailed') }}</p>
      <ModelConflictReview v-if="needsRead" :conflict="review" :comparing="comparing" :failed="!!reviewError" @compare="compareLatest" @merge="mergeReviewed" />
      <section v-show="tab === 'models'" class="space-y-4">
        <p v-if="loading" role="status" class="p-6 text-sm text-gray-500">{{ t('workspace.loading') }}</p>
        <div v-if="error" role="alert" class="space-y-3 rounded-lg border border-red-300 p-4 text-sm">
          <p>{{ t('workspace.loadFailed') }}</p>
          <button type="button" class="btn btn-secondary" :disabled="loading || busy" @click="requestReload">{{ t('workspace.retry') }}</button>
        </div>
        <div v-if="mappingError" role="alert" class="space-y-3 rounded-lg border border-amber-300 p-4 text-sm">
          <p>{{ t('workspace.mappingFailed') }}</p>
          <button type="button" class="btn btn-secondary" :disabled="loading || busy" @click="requestReload">{{ t('workspace.retry') }}</button>
        </div>
        <ManagedCatalogPanel
          v-if="draft" :policy="draft" :mappings="mappings" :can-manage="canManage" :can-sync="canSync" :mapping-targets="mappingTargets" :busy="busy || loading || needsRead || comparing"
          @update:policy="updatePolicy" @update:mappings="updateMappings" @mapping-draft-state="mappingLocalDirty = $event.dirty"
          @save-policy="savePolicy" @save-mappings="saveMappings" @reset-policy="requestReset" @open-sync="openSync"
        />
      </section>
      <ManagedPricesPanel
        v-if="priceVisited && canReadPrices" v-show="tab === 'pricing'" :key="priceEpoch"
        :platform="platform" :scope="scope" :can-read="canReadPrices" :can-manage="canManagePrices" :image-model-ids="imageModelIds"
        @dirty-change="priceDirty = $event" @busy-change="priceBusy = $event"
      />
    </template>
    <BaseDialog :show="syncOpen" :title="t('workspace.syncTitle')" :close-on-escape="!busy" :show-close-button="!busy" @close="closeSync">
      <p v-if="loadingSources" role="status">{{ t('workspace.sourcesLoading') }}</p>
      <div v-else-if="sourceError" role="alert" class="space-y-3">
        <p>{{ t('workspace.sourcesFailed') }}</p>
        <button type="button" class="btn btn-secondary" @click="loadSources">{{ t('workspace.retry') }}</button>
      </div>
      <p v-else-if="!sources.length">{{ t('workspace.noSources') }}</p>
      <div v-else class="max-h-80 space-y-2 overflow-y-auto">
        <label v-for="source in sources" :key="source.id" class="flex gap-3 rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-700">
          <input v-model="sourceIDs" type="checkbox" :value="source.id" :disabled="busy" />
          <span class="min-w-0 break-all">{{ source.name }} <span class="text-gray-500">{{ source.type }}</span></span>
        </label>
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="closeSync">{{ t('workspace.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="busy || loadingSources || !sourceIDs.length || !canSync" @click="requestSync">{{ t('workspace.sync') }}</button>
      </template>
    </BaseDialog>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/features/identity'
import { useManagedModels, type ManagedModelPlatform } from '@/features/catalog/management'
import { ManagedCatalogPanel } from '@/features/catalog/management'
import { ManagedPricesPanel } from '@/features/pricing/management'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import messages from './locales/messages'
import ModelConflictReview from './ModelConflictReview.vue'

const auth = useAuthStore()
const { t } = useI18n({ useScope: 'local', messages })
const platform = ref<ManagedModelPlatform>('anthropic')
const scope = computed(() => auth.isAuthenticated ? auth.identityScopeKey : '')
const canRead = computed(() => auth.can('models.read'))
const canManage = computed(() => canRead.value && auth.can('models.manage'))
const canSync = computed(() => canManage.value && auth.can('accounts.read'))
const canReadPrices = computed(() => canRead.value && auth.can('billing.cost.read'))
const canManagePrices = computed(() => canManage.value && canReadPrices.value && auth.can('billing.rates.update'))
const editor = useManagedModels({ scope, platform, canRead, canManage, canSync })
const { draft, mappings, sources, loading, loadingSources, busy, error, mappingError, sourceError, commandError,
  dirty, mappingsDirty, conflict, completedVersion, mappingTargets, needsRead, comparing, review, reviewError, compareLatest, mergeReviewed, updatePolicy, updateMappings, reload, savePolicy, saveMappings, resetPolicy, loadSources, syncPolicy } = editor
const tab = ref<'models' | 'pricing'>('models')
const priceVisited = ref(false)
const priceDirty = ref(false)
const mappingLocalDirty = ref(false)
const priceBusy = ref(false)
const priceEpoch = ref(0)
const syncOpen = ref(false)
const sourceIDs = ref<number[]>([])
const hasDirty = computed(() => dirty.value || mappingsDirty.value || mappingLocalDirty.value || priceDirty.value)
const imageModelIds = computed(() => Object.values(draft.value?.models ?? {}).filter(model => model.kind === 'image').map(model => model.id))
function confirmDiscard(): boolean { return !hasDirty.value || window.confirm(t('workspace.discard')) }
function changePlatform(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLSelectElement)) return
  const value = target.value
  if (value !== 'openai' && value !== 'anthropic') return
  if (busy.value || priceBusy.value || !confirmDiscard()) { target.value = platform.value; return }
  syncOpen.value = false
  sourceIDs.value = []
  platform.value = value
  priceDirty.value = false
  mappingLocalDirty.value = false
}
async function requestReload(): Promise<void> {
  if (busy.value || priceBusy.value || loading.value) return
  if (!confirmDiscard()) return
  if (await reload(true)) { priceEpoch.value++; priceDirty.value = false }
}
async function requestReset(): Promise<void> {
  if (!canManage.value || busy.value || !window.confirm(t('workspace.resetConfirm'))) return
  await resetPolicy(true)
}
function openPrices(): void { tab.value = 'pricing'; priceVisited.value = true }
async function openSync(): Promise<void> {
  if (!canSync.value || busy.value) return
  sourceIDs.value = []
  syncOpen.value = true
  await loadSources()
}
function closeSync(): void { if (!busy.value) syncOpen.value = false }
async function requestSync(): Promise<void> {
  if (dirty.value && !window.confirm(t('workspace.syncConfirm'))) return
  if (await syncPolicy(sourceIDs.value, true)) syncOpen.value = false
}
function beforeUnload(event: BeforeUnloadEvent): void {
  if (!hasDirty.value && !busy.value && !priceBusy.value) return
  event.preventDefault()
  event.returnValue = ''
}
watch([scope, canManage, canRead], () => {
  syncOpen.value = false
  sourceIDs.value = []
  priceDirty.value = false
  priceBusy.value = false
  mappingLocalDirty.value = false
}, { flush: 'sync' })
watch(canSync, value => { if (!value) { syncOpen.value = false; sourceIDs.value = [] } }, { flush: 'sync' })
watch(canReadPrices, value => { if (!value) tab.value = 'models' }, { flush: 'sync' })
onBeforeRouteLeave(() => {
  if (busy.value || priceBusy.value) return false
  return confirmDiscard()
})
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
</script>
