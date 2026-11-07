<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useDraftSection } from '@/shared/lib/useDraftSection'
import type { GroupPolicyDraft } from '../model/groupPolicyDraft'
type Draft = Pick<GroupPolicyDraft, 'allow_batch_image_generation'>
const props = defineProps<{ modelValue: Readonly<Draft>; editable: (field: string) => boolean }>()
const emit = defineEmits<{ patch: [value: Partial<Draft>] }>()
const editForm = useDraftSection<Draft>(() => props.modelValue, patch => emit('patch', patch))
const { t } = useI18n()
const canEditGroupField = (field: string, _creating: boolean) => props.editable(field)
</script>

<template>
<div><label
              class="flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              <input
                v-model="editForm.allow_batch_image_generation"
                type="checkbox"
 :disabled="!canEditGroupField('allow_batch_image_generation', false)"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              {{ t("admin.groups.imagePricing.allowBatchImageGeneration") }}
            </label>
            <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.groups.imagePricing.batchSectionHint") }}
            </p></div>
</template>
