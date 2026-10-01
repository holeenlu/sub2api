<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold">{{ t('modelCatalog.title') }}</h1>
          <p class="mt-2 max-w-3xl text-sm text-gray-500 dark:text-dark-400">{{ t('modelCatalog.inventoryHint') }}</p>
        </div>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="busy" @click="syncNow">{{ t('modelCatalog.syncNow') }}</button>
          <button class="btn btn-primary" :disabled="busy" @click="editModel()">{{ t('modelCatalog.addModel') }}</button>
        </div>
      </div>
      <p v-if="error && !editor" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <p v-if="notice" role="status" class="text-sm text-primary-600">{{ notice }}</p>
      <details v-if="settings" class="card p-4">
        <summary class="cursor-pointer text-sm font-medium">{{ t('modelCatalog.settings') }}</summary>
        <form class="mt-4 space-y-3" @submit.prevent="saveSettings">
          <p class="text-sm text-gray-500">{{ t('modelCatalog.syncHint') }}</p>
          <div class="flex flex-wrap items-end gap-4">
            <label class="flex items-center gap-2 py-2"><input v-model="settings.enabled" type="checkbox" :disabled="busy" />{{ t('modelCatalog.enabled') }}</label>
            <label class="text-sm">{{ t('modelCatalog.interval') }}<input v-model.number="settings.interval_seconds" class="input mt-1 w-40" type="number" min="60" max="86400" required :disabled="busy || !settings.enabled" /></label>
            <button class="btn btn-secondary" :disabled="busy">{{ t('modelCatalog.save') }}</button>
          </div>
        </form>
      </details>
      <section class="card overflow-hidden">
        <div class="flex flex-wrap gap-3 p-4">
          <div class="w-full sm:w-48"><label class="sr-only" for="catalog-platform">{{ t('modelCatalog.platform') }}</label><Select id="catalog-platform" v-model="platform" :options="platformOptions" searchable /></div>
          <div class="w-full sm:w-40"><label class="sr-only" for="catalog-kind">{{ t('modelCatalog.kind') }}</label><Select id="catalog-kind" v-model="kind" :options="kindFilterOptions" /></div>
          <input v-model="search" class="input min-w-48 flex-1" type="search" :aria-label="t('modelCatalog.search')" :placeholder="t('modelCatalog.search')" />
          <label class="flex items-center gap-2 text-sm"><input v-model="showDisabled" type="checkbox" />{{ t('modelCatalog.showDisabled') }}</label>
        </div>
        <div class="overflow-x-auto">
          <table class="w-full text-left text-sm">
            <thead class="bg-gray-50 text-gray-500 dark:bg-dark-800"><tr><th class="px-4 py-3">{{ t('modelCatalog.model') }}</th><th class="px-4 py-3">{{ t('modelCatalog.platform') }}</th><th class="px-4 py-3">{{ t('modelCatalog.kind') }}</th><th class="px-4 py-3">{{ t('modelCatalog.state') }}</th><th class="px-4 py-3 text-right">{{ t('modelCatalog.actions') }}</th></tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="model in visibleModels" :key="model.platform + ':' + model.id">
                <td class="px-4 py-3"><div class="break-all font-mono">{{ model.id }}</div><div v-if="model.display_name && model.display_name !== model.id" class="mt-1 text-xs text-gray-500">{{ model.display_name }}</div></td>
                <td class="px-4 py-3">{{ platformLabel(model.platform) }}</td>
                <td class="px-4 py-3">{{ kindLabel(model.kind) }}</td>
                <td class="whitespace-nowrap px-4 py-3" :class="inactive(model) ? 'text-gray-400' : 'text-emerald-600'">{{ t(model.lifecycle === 'retired' ? 'modelCatalog.retired' : model.disabled ? 'modelCatalog.disabled' : 'modelCatalog.available') }}</td>
                <td class="whitespace-nowrap px-4 py-3 text-right"><button class="btn btn-ghost btn-sm" :disabled="busy" @click="editModel(model)">{{ t('modelCatalog.edit') }}</button></td>
              </tr>
              <tr v-if="!visibleModels.length"><td colspan="5" class="p-8 text-center text-gray-500">{{ t(busy ? 'modelCatalog.loading' : 'modelCatalog.empty') }}</td></tr>
            </tbody>
          </table>
        </div>
        <Pagination v-if="filteredModels.length > pageSize" :page="page" :page-size="pageSize" :total="filteredModels.length" :show-page-size-selector="false" @update:page="page = $event" />
      </section>
    </div>
    <BaseDialog :show="!!editor" :title="t(editingExisting ? 'modelCatalog.edit' : 'modelCatalog.addModel')" width="normal" @close="closeEditor">
      <form v-if="editor" class="space-y-4" @submit.prevent="saveModel">
        <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
        <div><label class="input-label" for="model-platform">{{ t('modelCatalog.platform') }}</label><Select id="model-platform" v-model="editor.platform" :disabled="editingExisting || busy" :options="concretePlatforms" searchable /></div>
        <label class="block text-sm" for="model-id">{{ t('modelCatalog.model') }}<input id="model-id" v-model.trim="editor.id" class="input mt-1 font-mono" maxlength="256" pattern="[^\s*]+" required :disabled="editingExisting || busy" /></label>
        <label class="block text-sm" for="model-display-name">{{ t('modelCatalog.displayName') }}<input id="model-display-name" v-model.trim="editor.display_name" class="input mt-1" maxlength="256" :disabled="busy" /></label>
        <div><label class="input-label" for="model-kind">{{ t('modelCatalog.kind') }}</label><Select id="model-kind" v-model="editor.kind" :options="kindOptions" :disabled="busy" /></div>
        <label class="flex items-center gap-2 text-sm"><input v-model="editor.disabled" type="checkbox" :disabled="busy" />{{ t('modelCatalog.disableModel') }}</label>
        <p class="text-xs text-gray-500">{{ t('modelCatalog.disableHint') }}</p>
        <div class="flex justify-end gap-2"><button type="button" class="btn btn-secondary" :disabled="busy" @click="closeEditor">{{ t('modelCatalog.cancel') }}</button><button class="btn btn-primary" :disabled="busy">{{ t('modelCatalog.save') }}</button></div>
      </form>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
import { extractApiErrorMessage } from '@/utils/apiError'
import { getModelCatalog, syncModelCatalog, getCatalogSettings, saveCatalogSettings, saveCatalogModel, type CatalogModel, type CatalogModelInput, type ModelCatalog, type CatalogSyncSettings } from '@/api/admin/modelCatalog'

const { t } = useI18n()
const catalog = ref<ModelCatalog | null>(null)
const settings = ref<CatalogSyncSettings | null>(null)
const platform = ref('')
const kind = ref('')
const search = ref('')
const showDisabled = ref(false)
const page = ref(1)
const pageSize = 50
const busy = ref(false)
const error = ref('')
const notice = ref('')
const editor = ref<CatalogModelInput | null>(null)
const editingExisting = ref(false)
const concretePlatforms = [...CONCRETE_PLATFORM_OPTIONS]
const platformOptions = computed(() => [{ value: '', label: t('modelCatalog.allPlatforms') }, ...concretePlatforms])
const kinds = ['chat', 'image', 'video', 'audio', 'embedding', 'other']
const kindLabel = (value: string) => t(`modelCatalog.kinds.${kinds.includes(value) ? value : 'other'}`)
const kindOptions = computed(() => kinds.map(value => ({ value, label: kindLabel(value) })))
const kindFilterOptions = computed(() => [{ value: '', label: t('modelCatalog.allKinds') }, ...kindOptions.value])
const platformLabel = (value: string) => concretePlatforms.find(option => option.value === value)?.label ?? value
const inactive = (model: CatalogModel) => model.disabled || model.lifecycle === 'retired'
const filteredModels = computed(() => {
  const term = search.value.trim().toLowerCase()
  return (catalog.value?.models ?? []).filter(model =>
    (!platform.value || model.platform === platform.value) &&
    (!kind.value || model.kind === kind.value) &&
    (showDisabled.value || !inactive(model)) &&
    (!term || `${model.id} ${model.display_name ?? ''}`.toLowerCase().includes(term))
  )
})
const visibleModels = computed(() => filteredModels.value.slice((page.value - 1) * pageSize, page.value * pageSize))
watch([platform, kind, search, showDisabled], () => { page.value = 1 })
const controller = new AbortController()
onBeforeUnmount(() => controller.abort())
async function action(operation: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try { await operation() } catch (cause) {
    if (!controller.signal.aborted) error.value = extractApiErrorMessage(cause, t('modelCatalog.error'))
  } finally { busy.value = false }
}
async function readCatalog() {
  catalog.value = await getModelCatalog({}, controller.signal)
  page.value = Math.min(page.value, Math.max(1, Math.ceil(filteredModels.value.length / pageSize)))
}
async function syncNow() {
  await action(async () => {
    notice.value = t('modelCatalog.syncRunning')
    const result = await syncModelCatalog(controller.signal)
    await readCatalog()
    notice.value = t('modelCatalog.syncSummary', { success: result.succeeded ?? 0, failed: result.failed ?? 0 })
    if (result.error) notice.value += ` ${t('modelCatalog.syncPartialFailure')}`
  })
}
async function saveSettings() {
  if (settings.value) await action(async () => {
    await saveCatalogSettings(settings.value!)
    notice.value = t('modelCatalog.saved')
  })
}
function editModel(model?: CatalogModel) {
  error.value = ''
  editingExisting.value = !!model
  editor.value = { id: model?.id ?? '', platform: model?.platform ?? (platform.value || 'openai'), display_name: model?.display_name ?? '', kind: kinds.includes(model?.kind ?? '') ? model!.kind : 'chat', disabled: !!model?.disabled }
}
function closeEditor() { if (!busy.value) editor.value = null }
async function saveModel() {
  if (!editor.value) return
  const input = { ...editor.value }
  if (!editingExisting.value && catalog.value?.models.some(model => model.platform === input.platform && model.id === input.id)) {
    error.value = t('modelCatalog.duplicateModel')
    return
  }
  await action(async () => {
    await saveCatalogModel(input)
    await readCatalog()
    editor.value = null
    notice.value = t('modelCatalog.saved')
  })
}
onMounted(() => action(async () => {
  const [inventory, config] = await Promise.all([getModelCatalog({}, controller.signal), getCatalogSettings()])
  catalog.value = inventory
  settings.value = config
}))
</script>
