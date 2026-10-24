<template>
  <details v-if="auth.isSuperAdmin" class="space-y-2 rounded border p-3">
    <summary>{{ t('admin.accounts.openai.cloud.reconcileTitle') }}</summary>
    <p class="text-sm">{{ t('admin.accounts.openai.cloud.reconcileHint') }}</p>
    <button type="button" class="btn btn-secondary" :disabled="busy" @click="load">{{ t('common.refresh') }}</button>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="accepted" role="status">{{ t('admin.accounts.openai.cloud.reconcileAccepted') }}</p>
    <select v-model="selected" class="input" :disabled="busy" :aria-label="t('admin.accounts.openai.cloud.unsettledRequest')">
      <option value="" disabled>{{ t('admin.accounts.openai.cloud.unsettledRequest') }}</option>
      <option v-for="item in items" :key="item.root_id" :value="item.root_id">{{ item.model }} · {{ item.root_id }}</option>
    </select>
    <template v-if="current">
      <p class="break-all text-xs">{{ current.thread_id }} / {{ current.turn_id }}</p>
      <pre class="overflow-auto text-xs">{{ JSON.stringify(current.observed_usage, null, 2) }}</pre>
      <label class="block">{{ t('admin.accounts.openai.cloud.finalStatus') }}
        <select v-model="terminal" class="input"><option value="completed">completed</option><option value="failed">failed</option><option value="interrupted">interrupted</option></select>
      </label>
      <label class="block">{{ t('admin.accounts.openai.cloud.finalUsage') }}
        <textarea v-model="usage" class="input min-h-32 font-mono text-xs" spellcheck="false" :placeholder="usageExample" />
      </label>
      <label class="block">{{ t('admin.accounts.openai.cloud.evidence') }}<input v-model="evidence" class="input" maxlength="2000" /></label>
      <label class="block">{{ t('admin.accounts.openai.cloud.reconcileReason') }}<input v-model="reason" class="input" maxlength="2000" /></label>
      <label class="flex gap-2"><input v-model="confirmed" type="checkbox" />{{ t('admin.accounts.openai.cloud.confirmUsage') }}</label>
      <button type="button" class="btn btn-primary" :disabled="busy || !confirmed || !usage.trim() || evidence.trim().length < 5 || reason.trim().length < 5" @click="submit">{{ t('admin.accounts.openai.cloud.reconcileSubmit') }}</button>
    </template>
  </details>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { getCloudUnsettled, reconcileCloud, type CloudUnsettled } from '@/api/admin/accounts'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ accountId: number }>()
const emit = defineEmits<{ refreshed: [] }>()
const { t } = useI18n(), auth = useAuthStore()
const items = ref<CloudUnsettled[]>([]), selected = ref(''), usage = ref(''), evidence = ref(''), reason = ref(''), terminal = ref('completed')
const confirmed = ref(false), busy = ref(false), accepted = ref(false), error = ref('')
const current = computed(() => items.value.find(item => item.root_id === selected.value))
const usageExample = '{"input_tokens": 0, "output_tokens": 0, "cached_input_tokens": 0, "cache_write_input_tokens": 0, "total_tokens": 0, "web_search_calls": 0, "image_count": 0, "image_sizes": {}, "image_output_sizes": []}'
let generation = 0
watch(() => props.accountId, () => { generation++; items.value = []; selected.value = ''; error.value = ''; accepted.value = false; busy.value = false })
watch(selected, () => { usage.value = ''; evidence.value = ''; reason.value = ''; confirmed.value = false })
async function load() {
  if (busy.value) return
  const version = generation
  busy.value = true; error.value = ''
  try { const values = await getCloudUnsettled(props.accountId); if (version === generation) { items.value = values; selected.value = ''; emit('refreshed') } }
  catch (cause) { if (version === generation) error.value = extractApiErrorMessage(cause, t('admin.accounts.openai.cloud.failed')) }
  finally { if (version === generation) busy.value = false }
}
async function submit() {
  if (!current.value || !confirmed.value || busy.value) return
  const version = generation, id = props.accountId, item = current.value
  busy.value = true; error.value = ''; accepted.value = false
  try {
    const values = JSON.parse(usage.value)
    if (!values || typeof values !== 'object' || Array.isArray(values)) throw new Error(t('admin.accounts.openai.cloud.finalUsage'))
    await reconcileCloud(id, { root_id: item.root_id, sequence: item.sequence, status: terminal.value, usage: values, evidence: evidence.value, reason: reason.value, confirmed: true })
    if (version === generation) { accepted.value = true; items.value = items.value.filter(value => value.root_id !== item.root_id); selected.value = ''; emit('refreshed') }
  } catch (cause) { if (version === generation) error.value = extractApiErrorMessage(cause, t('admin.accounts.openai.cloud.failed')) }
  finally { if (version === generation) busy.value = false }
}
</script>
