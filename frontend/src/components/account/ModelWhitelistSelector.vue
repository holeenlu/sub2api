<template>
  <div class="relative mb-3">
    <div @click="toggleDropdown" class="cursor-pointer rounded-lg border border-gray-300 bg-white px-3 py-2 dark:border-dark-500 dark:bg-dark-700">
      <div class="grid grid-cols-2 gap-1.5">
        <span v-for="model in modelValue" :key="model" class="inline-flex min-w-0 items-center justify-between gap-1 rounded bg-gray-100 px-2 py-1 text-xs text-gray-700 dark:bg-dark-600 dark:text-gray-300"><span class="flex min-w-0 items-center gap-1"><ModelIcon :model="model" size="14px" /><span class="truncate">{{ model }}</span></span><button type="button" class="shrink-0 rounded-full hover:bg-gray-200" @click.stop="toggleModel(model)"><Icon name="x" size="xs" /></button></span>
      </div>
      <div class="mt-2 flex items-center justify-between border-t border-gray-200 pt-2 dark:border-dark-600"><span class="text-xs text-gray-400">{{ t('admin.accounts.modelCount', { count: modelValue.length }) }}</span><svg class="h-5 w-5 text-gray-400" fill="none" viewBox="0 0 24 24"><path d="M19 9l-7 7-7-7" stroke="currentColor" stroke-width="2" /></svg></div>
    </div>
    <div v-if="showDropdown" class="absolute left-0 right-0 top-full z-50 mt-1 rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-600 dark:bg-dark-700">
      <div class="sticky top-0 border-b border-gray-200 bg-white p-2 dark:border-dark-600 dark:bg-dark-700"><input v-model="searchQuery" type="text" class="input w-full text-sm" :placeholder="t('admin.accounts.searchModels')" @click.stop /></div>
      <div class="max-h-52 overflow-auto">
        <div v-for="model in filteredModels" :key="model.id" class="group flex items-center hover:bg-gray-100 dark:hover:bg-dark-600">
          <button type="button" data-testid="select-model" class="flex min-w-0 flex-1 items-center gap-2 px-3 py-2 text-left text-sm" @click="toggleModel(model.id)"><span :class="['flex h-4 w-4 shrink-0 items-center justify-center rounded border', modelValue.includes(model.id) ? 'border-primary-500 bg-primary-500 text-white' : 'border-gray-300 dark:border-dark-500']"><svg v-if="modelValue.includes(model.id)" class="h-3 w-3" fill="none" viewBox="0 0 24 24"><path d="M5 13l4 4L19 7" stroke="currentColor" stroke-width="3" /></svg></span><ModelIcon :model="model.id" size="18px" /><span class="truncate text-gray-900 dark:text-white">{{ model.id }}</span></button>
          <button type="button" class="mr-2 rounded p-1.5 text-gray-400 hover:text-primary-600" :title="`${t('common.copy')} ${model.id}`" @click="copyToClipboard(model.id)"><Icon name="copy" size="sm" /></button>
        </div>
        <div v-if="!filteredModels.length" class="px-3 py-4 text-center text-sm text-gray-500">{{ t('admin.accounts.noMatchingModels') }}</div>
      </div>
    </div>
    <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('modelManagement.accountReferenceHint') }}</p>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { modelsAPI, type ManagedModel } from '@/api/admin/models'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
const { t } = useI18n(); const app = useAppStore(); const { copyToClipboard } = useClipboard()
const props = defineProps<{ modelValue: string[]; modelMappings?: { from: string; to: string }[]; platform?: string; platforms?: string[]; accountId?: number; syncAccountIds?: () => Promise<number[]>; syncCredentials?: Record<string, unknown> }>()
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const showDropdown = ref(false), searchQuery = ref(''), catalog = ref<ManagedModel[]>([]), expected = ref<string[]>([...props.modelValue])
const platforms = computed(() => Array.from(new Set((props.platforms?.length ? props.platforms : [props.platform || 'anthropic']).filter(Boolean))))
const options = computed(() => catalog.value.filter(model => model.enabled || props.modelValue.includes(model.id)))
const filteredModels = computed(() => { const query = searchQuery.value.trim().toLowerCase(); return query ? options.value.filter(m => `${m.id} ${m.display_name}`.toLowerCase().includes(query)) : options.value })
async function loadCatalog() { try { const rows = await Promise.all(platforms.value.map(platform => modelsAPI.catalog(platform))); catalog.value = rows.flat().filter((row,index,all) => all.findIndex(item => item.id === row.id) === index) } catch (error) { app.showError(String(error)) } }
function toggleDropdown() { showDropdown.value = !showDropdown.value; if (!showDropdown.value) searchQuery.value = '' }
async function toggleModel(id: string) { const next = props.modelValue.includes(id) ? props.modelValue.filter(item => item !== id) : [...props.modelValue, id]; emit('update:modelValue', next); if (props.accountId) { try { const result = await modelsAPI.saveAccountModels(props.accountId, expected.value, next); expected.value = result.models || []; } catch (error) { emit('update:modelValue', expected.value); app.showError(String(error)) } } }
watch(() => props.modelValue, value => { if (!props.accountId) expected.value = [...value] })
watch(platforms, loadCatalog, { immediate: true }); onMounted(loadCatalog)
</script>
