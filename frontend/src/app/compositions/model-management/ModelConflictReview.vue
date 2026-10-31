<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { modelConflictRows, type ManagedModelConflict } from '@/features/catalog/management'
const props = defineProps<{ conflict: ManagedModelConflict | null; comparing: boolean; failed: boolean }>()
const emit = defineEmits<{ compare: []; merge: [keys: string[]] }>()
const { t } = useI18n({ useScope: 'parent' })
const keep = ref<string[]>([])
const rows = computed(() => props.conflict ? modelConflictRows(props.conflict) : [])
watch(() => props.conflict, () => { keep.value = [] })
function label(key: string): string {
  try {
    const path: unknown = JSON.parse(key)
    if (Array.isArray(path) && path.every(part => typeof part === 'string')) return path.join(' › ')
  } catch { /* A plain key remains a usable label. */ }
  return key
}
</script>
<template>
  <section class="space-y-3 rounded-lg border border-amber-400 p-4" :aria-busy="comparing" data-testid="model-conflict-review">
    <p class="text-sm">{{ t('workspace.reviewHint') }}</p>
    <p v-if="failed" role="alert" class="text-sm text-red-700">{{ t('workspace.loadFailed') }}</p>
    <button type="button" class="btn btn-secondary" :disabled="comparing" @click="emit('compare')">{{ t('workspace.compare') }}</button>
    <div v-if="conflict" class="overflow-x-auto">
      <table class="w-full text-left text-sm">
        <thead><tr>
          <th class="p-2">{{ t('workspace.field') }}</th>
          <th class="p-2">{{ t('workspace.baseline') }}</th>
          <th class="p-2">{{ t('workspace.server') }}</th>
          <th class="p-2">{{ t('workspace.local') }}</th>
          <th class="p-2">{{ t('workspace.keep') }}</th>
        </tr></thead>
        <tbody><tr v-for="row in rows" :key="row.key" class="border-t dark:border-dark-700">
          <th class="p-2 font-normal">{{ label(row.key) }}</th>
          <td v-for="(value, index) in [row.baseline, row.server, row.local]" :key="index" class="p-2"><pre class="max-h-48 max-w-xs overflow-auto whitespace-pre-wrap break-words text-xs">{{ value }}</pre></td>
          <td class="p-2"><input v-if="row.dirty" v-model="keep" type="checkbox" :value="row.key" :disabled="comparing" :aria-label="t('workspace.keep') + ': ' + label(row.key)" /></td>
        </tr></tbody>
      </table>
      <button type="button" class="btn btn-primary mt-3" :disabled="comparing" @click="emit('merge', keep)">{{ t('workspace.merge') }}</button>
    </div>
  </section>
</template>
