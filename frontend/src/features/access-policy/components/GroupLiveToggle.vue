<template>
 <div class="space-y-2 border-t pt-4"><div class="flex items-center justify-between"><label class="input-label">{{ t('admin.groups.openaiLive.title') }}</label><Toggle :model-value="modelValue" :disabled="loading" @update:model-value="toggle" /></div><p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p></div>
 <ConfirmDialog :show="confirmUnsupported" :title="t('admin.groups.openaiLive.title')" :message="reason" :confirm-text="t('common.confirm')" :cancel-text="t('common.cancel')" @confirm="confirm" @cancel="confirmUnsupported = false" />
</template>
<script setup lang="ts">
import { ref } from 'vue';
import { useI18n } from 'vue-i18n';
import Toggle from '@/shared/ui/Toggle.vue';
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue';
import { groupsAPI } from '../api/groups';
import { useAccessPolicyRequest } from '../queries/requestScope';
const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
const { t } = useI18n()
const loading = ref(false); const confirmUnsupported = ref(false); const reason = ref(''); const error = ref('')
const owner = useAccessPolicyRequest(() => { loading.value = false; confirmUnsupported.value = false; error.value = '' })
async function toggle() {
 if (props.modelValue) { emit('update:modelValue', false); return }
 const request = owner.begin(); loading.value = true; error.value = ''
 try { const result = await groupsAPI.getLiveCapability(); if (!request.isCurrent()) return; if (result.supported === true) emit('update:modelValue', true); else if (result.supported === false) { reason.value = result.reason || t('admin.groups.openaiLive.unsupportedMessage'); confirmUnsupported.value = true } else { error.value = t('admin.groups.failedToLoad') } }
 catch { if (request.isCurrent()) error.value = t('admin.groups.failedToLoad') }
 finally { if (request.isCurrent()) loading.value = false }
}
function confirm() { confirmUnsupported.value = false; emit('update:modelValue', true) }
</script>
