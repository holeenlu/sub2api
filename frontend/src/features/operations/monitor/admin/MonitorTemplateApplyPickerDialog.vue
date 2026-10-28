<template>
  <BaseDialog
    :show="show"
    :title="t('admin.channelMonitor.template.applyPickerTitle', { name: templateName })"
    @close="$emit('close')"
  >
    <p class="mb-3 text-sm text-gray-600 dark:text-gray-400">
      {{ t('admin.channelMonitor.template.applyPickerHint') }}
    </p>

    <div v-if="loading" class="py-6 text-center text-sm text-gray-400">
      {{ t('common.loading') }}
    </div>

    <div v-else-if="monitors.length === 0" class="py-6 text-center text-sm text-gray-400">
      {{ t('admin.channelMonitor.template.applyPickerEmpty') }}
    </div>

    <div v-else>
      <!-- 全选/全不选 -->
      <div class="mb-2 flex items-center gap-3 text-xs">
        <button
          type="button"
          class="text-primary-600 hover:underline dark:text-primary-400"
          @click="selectAll"
        >
          {{ t('common.selectAll') }}
        </button>
        <button
          type="button"
          class="text-gray-500 hover:underline dark:text-gray-400"
          @click="selectNone"
        >
          {{ t('admin.channelMonitor.template.selectNone') }}
        </button>
        <span class="ml-auto text-gray-500 dark:text-gray-400">
          {{ t('admin.channelMonitor.template.selectedCount', {
            n: selectedIds.length,
            total: monitors.length,
          }) }}
        </span>
      </div>

      <ul class="max-h-80 divide-y divide-gray-100 overflow-y-auto rounded-lg border border-gray-200 dark:divide-dark-700 dark:border-dark-700">
        <li
          v-for="m in monitors"
          :key="m.id"
          class="flex cursor-pointer items-center gap-3 px-3 py-2 hover:bg-gray-50 dark:hover:bg-dark-800"
          @click="toggle(m.id)"
        >
          <input
            type="checkbox"
            :checked="selectedSet.has(m.id)"
            class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            @click.stop="toggle(m.id)"
          />
          <span class="font-medium text-gray-900 dark:text-white">{{ m.name }}</span>
          <span class="text-xs text-gray-400">{{ m.provider }}</span>
          <span v-if="m.provider === 'openai'" class="text-xs text-gray-400">{{ m.api_mode }}</span>
          <span
            v-if="!m.enabled"
            class="ml-auto rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-500 dark:bg-dark-700 dark:text-gray-400"
          >
            {{ t('admin.channelMonitor.onlyDisabled').replace(/^仅|^Only /, '') }}
          </span>
        </li>
      </ul>
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <button class="btn btn-secondary" @click="$emit('close')">
          {{ t('common.cancel') }}
        </button>
        <button
          class="btn btn-primary"
          :disabled="submitting || selectedIds.length === 0"
          @click="handleApply"
        >
          {{ submitting
            ? t('common.submitting')
            : t('admin.channelMonitor.template.applyPickerConfirm', { n: selectedIds.length }) }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useOperationsRequest } from '@/features/operations/queries/useOperationsRequest'
import { useOperationsServices } from '@/features/operations/services'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { channelMonitorTemplateAPI } from '@/features/operations/api'
import type { AssociatedMonitorBrief } from '@/features/operations/api'
import BaseDialog from '@/shared/ui/BaseDialog.vue'

const props = defineProps<{
  show: boolean
  templateId: number | null
  templateName: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'applied', affected: number): void
}>()

const { t } = useI18n()
const services = useOperationsServices()
const operationsRequests = useOperationsRequest(services)

const loading = ref(false)
const submitting = ref(false)
const monitors = ref<AssociatedMonitorBrief[]>([])
const selectedIds = ref<number[]>([])

const selectedSet = computed(() => new Set(selectedIds.value))
let requestVersion = 0

onUnmounted(() => { requestVersion++ })

watch(
  () => [props.show, props.templateId] as const,
  ([show, id]) => {
    operationsRequests.cancel(); submitting.value = false; loading.value = false; monitors.value = []; selectedIds.value = []
    const version = ++requestVersion
    if (!show || id == null) return
    void fetchMonitors(id, version)
  },
  { immediate: true },
)

async function fetchMonitors(id: number, version: number) {
  if (services.scopeKey.value === null) return

  loading.value = true
  monitors.value = []
  selectedIds.value = []
  const operation = operationsRequests.begin('fetchMonitors')
  try {
    const { items } = await operation.wait(channelMonitorTemplateAPI.listAssociatedMonitors(id))
    if (version !== requestVersion) return
    monitors.value = items
    // 默认全选
    selectedIds.value = items.map((m) => m.id)
  } catch (err: unknown) {
    if (!operation.isCurrent()) return
    if (version !== requestVersion) return
    services.notifyError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    if (operation.isCurrent()) {
      if (version === requestVersion) loading.value = false
    }
  }
}

function toggle(id: number) {
  const idx = selectedIds.value.indexOf(id)
  if (idx >= 0) selectedIds.value.splice(idx, 1)
  else selectedIds.value.push(id)
}

function selectAll() {
  selectedIds.value = monitors.value.map((m) => m.id)
}

function selectNone() {
  selectedIds.value = []
}

async function handleApply() {
  if (services.scopeKey.value === null) return

  if (props.templateId == null || selectedIds.value.length === 0 || submitting.value) return
  submitting.value = true
  const operation = operationsRequests.begin('handleApply')
  try {
    const { affected } = await operation.wait(channelMonitorTemplateAPI.apply(
      props.templateId,
      [...selectedIds.value],
    ))
    services.notifySuccess(t('admin.channelMonitor.template.applySuccess', { n: affected }))
    emit('applied', affected)
    emit('close')
  } catch (err: unknown) {
    if (!operation.isCurrent()) return
    services.notifyError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    if (operation.isCurrent()) {
      submitting.value = false
    }
  }
}
operationsRequests.onScopeChange(() => { ++requestVersion; monitors.value = []; selectedIds.value = []; loading.value = false; submitting.value = false })
</script>
