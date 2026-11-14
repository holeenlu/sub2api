<script setup lang="ts">
import Toggle from '@/shared/ui/Toggle.vue'
import { supportsGroupOpenAIFast } from '../model/groupsOpenAIFast'
import { useI18n } from 'vue-i18n'
import { useDraftSection } from '@/shared/lib/useDraftSection'
import type { GroupPolicyDraft } from '../model/groupPolicyDraft'
type Draft = Pick<GroupPolicyDraft, 'platform' | 'force_openai_fast'>
const props = defineProps<{ modelValue: Readonly<Draft>; editable: (field: string) => boolean }>()
const emit = defineEmits<{ patch: [value: Partial<Draft>] }>()
const editForm = useDraftSection<Draft>(() => props.modelValue, patch => emit('patch', patch))
const { t } = useI18n()
const canEditGroupField = (field: string, _creating: boolean) => props.editable(field)
</script>

<template>
<div
          v-if="supportsGroupOpenAIFast(editForm.platform)"
          class="border-t border-gray-200 dark:border-dark-400 pt-4 mt-4"
        >
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
            {{ t("admin.groups.openaiFast.title") }}
          </h4>
          <div :inert="!canEditGroupField('force_openai_fast', false)" :aria-disabled="!canEditGroupField('force_openai_fast', false)" class="flex items-center justify-between gap-4">
            <label class="text-sm text-gray-600 dark:text-gray-400">
              {{ t("admin.groups.openaiFast.force") }}
            </label>
            <Toggle
              data-testid="edit-force-openai-fast"
              :aria-label="t('admin.groups.openaiFast.force')"
              v-model="editForm.force_openai_fast"
            />
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">
            {{ t("admin.groups.openaiFast.hint") }}
          </p>
<slot />
</div>
</template>
