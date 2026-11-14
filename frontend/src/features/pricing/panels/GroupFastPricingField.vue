<script setup lang="ts">
import Toggle from '@/shared/ui/Toggle.vue'
import { useI18n } from 'vue-i18n'
import { useDraftSection } from '@/shared/lib/useDraftSection'
import type { GroupPricingDraft } from '../model/groupPricingDraft'
type Draft = Pick<GroupPricingDraft, 'free_openai_fast'>
const props = defineProps<{ modelValue: Readonly<Draft>; editable: (field: string) => boolean }>()
const emit = defineEmits<{ patch: [value: Partial<Draft>] }>()
const editForm = useDraftSection<Draft>(() => props.modelValue, patch => emit('patch', patch))
const { t } = useI18n()
const canEditGroupField = (field: string, _creating: boolean) => props.editable(field)
</script>

<template>
<div><div :inert="!canEditGroupField('free_openai_fast', false)" :aria-disabled="!canEditGroupField('free_openai_fast', false)" class="flex items-center justify-between gap-4 mt-4">
            <label class="text-sm text-gray-600 dark:text-gray-400">
              {{ t("admin.groups.openaiFast.free") }}
            </label>
            <Toggle
              data-testid="edit-free-openai-fast"
              :aria-label="t('admin.groups.openaiFast.free')"
              v-model="editForm.free_openai_fast"
            />
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">
            {{ t("admin.groups.openaiFast.freeHint") }}
          </p></div>
</template>
