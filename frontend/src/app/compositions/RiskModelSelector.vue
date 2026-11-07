<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { managedModelsAPI, type ManagedModel } from '@/features/catalog/managedModels'
import { accessPolicyPermissionsKey, accessPolicyScopeKey, requireAccessPolicyService } from '@/features/access-policy/services'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import SearchInput from '@/shared/ui/SearchInput.vue'

defineOptions({ inheritAttrs: false })
const props = defineProps<{ modelValue: string[]; disabled?: boolean; readOnly?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [models: string[]] }>()
const { t } = useI18n()
const scope = requireAccessPolicyService(accessPolicyScopeKey)
const permissions = requireAccessPolicyService(accessPolicyPermissionsKey)
const canRead = computed(() => !!scope.value && permissions.can('risk.read'))
const canWrite = computed(() => canRead.value && permissions.can('risk.manage') && !props.disabled && !props.readOnly)
const catalog = ref<ManagedModel[]>([])
const search = ref('')
const ready = ref(false)
const error = ref('')
let controller: AbortController | undefined
let generation = 0
const actor = () => JSON.stringify([scope.value, canRead.value, canWrite.value])
const options = computed(() => {
  if (!canRead.value) return []
  const enabled = catalog.value.filter(model => model.enabled)
  const known = new Set(enabled.map(model => model.id))
  return [...enabled, ...props.modelValue.filter(id => !known.has(id)).map(id => ({ id, display_name: id, enabled: false }))]
    .filter(model => `${model.id} ${model.display_name}`.toLowerCase().includes(search.value.trim().toLowerCase()))
})
async function load() {
  controller?.abort()
  const version = ++generation
  catalog.value = []
  ready.value = false
  error.value = ''
  if (!canRead.value) return
  const request = new AbortController()
  controller = request
  try {
    const models = await managedModelsAPI.catalog('composite', { signal: request.signal })
    if (version !== generation || request.signal.aborted || !canRead.value) return
    catalog.value = models
    ready.value = true
  } catch (failure) {
    if (version === generation && !request.signal.aborted && canRead.value) error.value = extractApiErrorMessage(failure, t('common.error'))
  }
}
watch(actor, () => { search.value = ''; void load() }, { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { generation++; controller?.abort() })
function toggle(id: string) {
  if (!canWrite.value || !ready.value) return
  if (!props.modelValue.includes(id) && !catalog.value.some(model => model.id === id && model.enabled)) return
  // Include/exclude lists are explicit, including []. The risk form/backend
  // owns validation; an empty list must never expand to the entire catalog.
  emit('update:modelValue', props.modelValue.includes(id) ? props.modelValue.filter(model => model !== id) : [...props.modelValue, id])
}
</script>

<template>
  <div v-if="canRead" class="space-y-2">
    <label class="block">
      <span class="sr-only">{{ t('admin.accounts.searchModels') }}</span>
      <SearchInput v-model="search" :placeholder="t('admin.accounts.searchModels')" />
    </label>
    <div class="max-h-52 space-y-1 overflow-auto">
      <label v-for="model in options" :key="model.id" class="flex items-center gap-2 text-sm">
        <input type="checkbox" class="checkbox" :value="model.id" :checked="modelValue.includes(model.id)" :disabled="!canWrite || !ready" @change="toggle(model.id)" />
        <span>{{ model.display_name || model.id }}<span v-if="model.display_name !== model.id" class="ml-1 text-xs text-gray-500">{{ model.id }}</span></span>
        <span v-if="!model.enabled" class="text-xs text-gray-500">{{ t('admin.accounts.modelReference.unavailable') }}</span>
      </label>
    </div>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-else-if="!ready" role="status">{{ t('common.loading') }}</p>
    <p v-else-if="!options.length" class="text-sm text-gray-500">{{ t('admin.accounts.noMatchingModels') }}</p>
    <button type="button" class="btn btn-secondary" @click="load">{{ t('common.refresh') }}</button>
  </div>
</template>
