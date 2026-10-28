<script setup lang="ts">
import { computed } from 'vue'
import Select from '@/shared/ui/Select.vue'
import { useI18n } from 'vue-i18n'
import { useDraftSection } from '@/shared/lib/useDraftSection'
import type { GroupSubscriptionDraft } from '../model/groupSubscriptionDraft'
type Draft = GroupSubscriptionDraft
const props = defineProps<{ modelValue: Readonly<Draft>; editable: (field: string) => boolean }>()
const emit = defineEmits<{ patch: [value: Partial<Draft>] }>()
const editForm = useDraftSection<Draft>(() => props.modelValue, patch => emit('patch', patch))
const { t } = useI18n()
const canEditGroupField = (field: string, _creating: boolean) => props.editable(field)
const subscriptionTypeOptions = computed(() => [{ value: 'standard', label: t('admin.groups.subscription.standard') }, { value: 'subscription', label: t('admin.groups.subscription.subscription') }])
</script>

<template>
<div class="mt-4 border-t pt-4">
          <div :inert="!canEditGroupField('subscription_type', false)" :aria-disabled="!canEditGroupField('subscription_type', false)">
            <label class="input-label">{{
              t("admin.groups.subscription.type")
            }}</label>
            <Select
              v-model="editForm.subscription_type"
              :options="subscriptionTypeOptions"
              :disabled="true"
            />
            <p class="input-hint">
              {{ t("admin.groups.subscription.typeNotEditable") }}
            </p>
          </div>

          <!-- Subscription limits (only show when subscription type is selected) -->
          <div :inert="!canEditGroupField('subscription_type', false)" :aria-disabled="!canEditGroupField('subscription_type', false)"
            v-if="editForm.subscription_type === 'subscription'"
            class="space-y-4 border-l-2 border-primary-200 pl-4 dark:border-primary-800"
          >
            <div :inert="!canEditGroupField('daily_limit_usd', false)" :aria-disabled="!canEditGroupField('daily_limit_usd', false)">
              <label class="input-label">{{
                t("admin.groups.subscription.dailyLimit")
              }}</label>
              <input
                v-model.number="editForm.daily_limit_usd"
                type="number"
                step="0.01"
                min="0"
                class="input"
                :placeholder="t('admin.groups.subscription.noLimit')"
              />
            </div>
            <div :inert="!canEditGroupField('weekly_limit_usd', false)" :aria-disabled="!canEditGroupField('weekly_limit_usd', false)">
              <label class="input-label">{{
                t("admin.groups.subscription.weeklyLimit")
              }}</label>
              <input
                v-model.number="editForm.weekly_limit_usd"
                type="number"
                step="0.01"
                min="0"
                class="input"
                :placeholder="t('admin.groups.subscription.noLimit')"
              />
            </div>
            <div :inert="!canEditGroupField('monthly_limit_usd', false)" :aria-disabled="!canEditGroupField('monthly_limit_usd', false)">
              <label class="input-label">{{
                t("admin.groups.subscription.monthlyLimit")
              }}</label>
              <input
                v-model.number="editForm.monthly_limit_usd"
                type="number"
                step="0.01"
                min="0"
                class="input"
                :placeholder="t('admin.groups.subscription.noLimit')"
              />
            </div>
          </div>
        </div>
</template>
