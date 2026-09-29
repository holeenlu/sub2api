<template>
  <BaseDialog :show="groupId != null" :title="t('modelPlaza.preview.title')" width="wide" @close="$emit('close')">
    <p class="mb-4 text-sm text-gray-500 dark:text-dark-400">{{ t('modelPlaza.preview.hint') }}</p>
    <p v-if="loading" role="status">{{ t('modelPlaza.loading') }}</p>
    <p v-else-if="failed" role="alert" class="text-sm text-red-600">{{ t('modelPlaza.loadFailed') }}</p>
    <template v-else-if="preview">
      <PlazaGroupSection :group="preview.group" />
      <details v-if="preview.issues.length" class="mt-4 text-sm">
        <summary class="cursor-pointer font-medium">{{ t('modelPlaza.preview.diagnostics') }} ({{ preview.issues.length }})</summary>
        <ul class="mt-2 space-y-2 text-gray-600 dark:text-dark-300">
          <li v-for="issue in preview.issues" :key="`${issue.model}:${issue.reason}`">
            <code v-if="issue.model" class="mr-2 break-all">{{ issue.model }}</code>{{ te(`modelPlaza.preview.reasons.${issue.reason}`) ? t(`modelPlaza.preview.reasons.${issue.reason}`) : issue.reason }}
          </li>
        </ul>
      </details>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlazaGroupSection from '@/components/modelPlaza/PlazaGroupSection.vue'
import { getModelPlazaPreview, type ModelPlazaPreview } from '@/api/modelPlaza'

const props = defineProps<{ groupId: number | null }>()
defineEmits<{ close: [] }>()
const { t, te } = useI18n()
const preview = ref<ModelPlazaPreview | null>(null)
const loading = ref(false)
const failed = ref(false)
watch(() => props.groupId, async (id, _, onCleanup) => {
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  preview.value = null
  failed.value = false
  if (id == null) return
  loading.value = true
  try {
    const result = await getModelPlazaPreview(id, controller.signal)
    if (!controller.signal.aborted) preview.value = result
  } catch {
    if (!controller.signal.aborted) failed.value = true
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}, { immediate: true })
</script>
