<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useDraftSection } from '@/shared/lib/useDraftSection'
import type { GroupPricingDraft } from '../model/groupPricingDraft'
type Draft = Pick<GroupPricingDraft, 'rate_multiplier'>
const props = defineProps<{ modelValue: Readonly<Draft>; editable: (field: string) => boolean }>()
const emit = defineEmits<{ patch: [value: Partial<Draft>] }>()
const editForm = useDraftSection<Draft>(() => props.modelValue, patch => emit('patch', patch))
const { t } = useI18n()
const canEditGroupField = (field: string, _creating: boolean) => props.editable(field)
</script>

<template>
<div :inert="!canEditGroupField('rate_multiplier', false)" :aria-disabled="!canEditGroupField('rate_multiplier', false)">
          <label class="input-label">{{
            t("admin.groups.form.rateMultiplier")
          }}</label>
          <input
            v-model.number="editForm.rate_multiplier"
            type="number"
            step="0.001"
            min="0.001"
            required
            class="input"
            data-tour="group-form-multiplier"
          />
        </div>
</template>
