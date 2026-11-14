<template>
 <div class="space-y-4">
  <GroupI18nField :model-value="modelValue.name" :i18n="modelValue.i18n" field="name" data-tour="group-form-name" :placeholder="t('admin.groups.enterGroupName')" :label="t('admin.groups.form.name')" required :disabled="!editable('name')" @update:model-value="patch({ name: $event })" @update:i18n="patch({ i18n: $event })" />
  <GroupI18nField :model-value="modelValue.description" :i18n="modelValue.i18n" field="description" rows="3" :placeholder="t('admin.groups.optionalDescription')" :label="t('admin.groups.form.description')" multiline :disabled="!editable('description')" @update:model-value="patch({ description: $event })" @update:i18n="patch({ i18n: $event })" />
 </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import GroupI18nField from '../components/GroupI18nField.vue';
import type { GroupIdentityDraft } from '../model/groupIdentityDraft';
const props = defineProps<{ modelValue: GroupIdentityDraft; editable: (field: string) => boolean }>()
const emit = defineEmits<{ 'update:modelValue': [draft: GroupIdentityDraft] }>()
const { t } = useI18n()
function patch(value: Partial<GroupIdentityDraft>) { emit('update:modelValue', { name: props.modelValue.name, description: props.modelValue.description, platform: props.modelValue.platform, i18n: props.modelValue.i18n, ...value }) }
</script>
