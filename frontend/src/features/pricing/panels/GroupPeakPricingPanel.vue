<script setup lang="ts">
import type { SubscriptionType } from '@/features/subscriptions/api/subscriptionPolicyTypes'
import { useI18n } from 'vue-i18n'
import { useDraftSection } from '@/shared/lib/useDraftSection'
import type { GroupPricingDraft } from '../model/groupPricingDraft'
type Draft = Pick<GroupPricingDraft, 'peak_rate_enabled' | 'peak_start' | 'peak_end' | 'peak_rate_multiplier'>
const props = defineProps<{ modelValue: Readonly<Draft>; editable: (field: string) => boolean; subscriptionType: SubscriptionType }>()
const emit = defineEmits<{ patch: [value: Partial<Draft>] }>()
const editForm = useDraftSection<Draft>(() => props.modelValue, patch => emit('patch', patch))
const { t } = useI18n()
const canEditGroupField = (field: string, _creating: boolean) => props.editable(field)
</script>

<template>
<div :inert="!canEditGroupField('subscription_type', false)" :aria-disabled="!canEditGroupField('subscription_type', false)" v-if="subscriptionType === 'subscription'" class="border-t pt-4">
          <div class="mb-4 grid grid-cols-1 gap-3 md:grid-cols-2">
            <label :inert="!canEditGroupField('peak_rate_enabled', false)" :aria-disabled="!canEditGroupField('peak_rate_enabled', false)" class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                v-model="editForm.peak_rate_enabled"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              <span>{{ t("admin.groups.peakRate.enable") }}</span>
            </label>
          </div>
          <div :inert="!canEditGroupField('peak_rate_enabled', false)" :aria-disabled="!canEditGroupField('peak_rate_enabled', false)"
            v-if="editForm.peak_rate_enabled"
            class="mb-4 grid grid-cols-1 gap-3 sm:grid-cols-3"
          >
            <div :inert="!canEditGroupField('peak_start', false)" :aria-disabled="!canEditGroupField('peak_start', false)">
              <label class="input-label">{{ t("admin.groups.peakRate.peakStart") }}</label>
              <input
                v-model="editForm.peak_start"
                type="time"
                class="input"
              />
            </div>
            <div :inert="!canEditGroupField('peak_end', false)" :aria-disabled="!canEditGroupField('peak_end', false)">
              <label class="input-label">{{ t("admin.groups.peakRate.peakEnd") }}</label>
              <input
                v-model="editForm.peak_end"
                type="time"
                class="input"
              />
            </div>
            <div :inert="!canEditGroupField('peak_rate_multiplier', false)" :aria-disabled="!canEditGroupField('peak_rate_multiplier', false)">
              <label class="input-label">{{ t("admin.groups.peakRate.peakMultiplier") }}</label>
              <input
                v-model.number="editForm.peak_rate_multiplier"
                type="number"
                step="0.001"
                min="0"
                class="input"
                placeholder="1"
                :title="t('admin.groups.peakRate.multiplierHint')"
              />
            </div>
          </div>
        </div>
</template>
