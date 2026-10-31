<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { isSecretSetting, type SettingsConflict } from './transaction'

const props = defineProps<{
  conflict: SettingsConflict | null
  baseline: object | null
  draft: object | null
  dirtyFields: readonly string[]
  saving: boolean
}>()
const emit = defineEmits<{ compare: []; merge: [fields: string[]] }>()
const { t } = useI18n()
const keepFields = ref<string[]>([])
watch(() => props.conflict?.server, () => { keepFields.value = [] })

function display(key: string, value: object | null): string {
  if (isSecretSetting(key)) return '••••••'
  if (!value) return '—'
  const field = (value as Record<string, unknown>)[key]
  return field === undefined ? '—' : String(typeof field === 'object' ? JSON.stringify(field, null, 2) : field)
}
</script>

<template>
  <section class="space-y-3 rounded-lg border border-amber-400 p-4" role="alert" data-testid="settings-conflict">
    <h2 class="font-semibold">{{ t('admin.settings.workspace.conflict') }}</h2>
    <p>{{ t('admin.settings.workspace.conflictHint') }}</p>
    <button v-if="!conflict" type="button" class="btn btn-secondary" @click="emit('compare')">{{ t('common.tryAgain') }}</button>
    <div v-else class="overflow-x-auto">
      <table class="w-full text-left text-sm">
        <thead><tr><th>{{ t('admin.settings.workspace.field') }}</th><th>{{ t('admin.settings.workspace.baseline') }}</th><th>{{ t('admin.settings.workspace.server') }}</th><th>{{ t('admin.settings.workspace.draft') }}</th><th>{{ t('admin.settings.workspace.keep') }}</th></tr></thead>
        <tbody><tr v-for="field in conflict.fields" :key="field" :data-field="field">
          <th class="p-2">{{ field }}</th>
          <td v-for="(value, index) in [baseline, conflict.server, draft]" :key="index" class="max-w-xs p-2"><div class="max-h-48 overflow-auto whitespace-pre-wrap break-words">{{ display(field, value) }}</div></td>
          <td><input v-if="dirtyFields.includes(field)" v-model="keepFields" type="checkbox" :value="field" :aria-label="t('admin.settings.workspace.keep') + ': ' + field" /></td>
        </tr></tbody>
      </table>
      <button type="button" class="btn btn-primary" data-testid="merge-settings" :disabled="saving" @click="emit('merge', keepFields)">{{ t('admin.settings.workspace.merge') }}</button>
    </div>
  </section>
</template>
