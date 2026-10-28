<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAccountQuotaNotificationDraft as useQuotaNotifyState } from '@/features/resources/model/useAccountQuotaNotificationDraft'
import QuotaLimitCard from '@/features/resources/components/QuotaLimitCard.vue'
import { supportsAccountQuota } from '@/features/resources/model/accountQuota'
import { type UnwrapRef, type Ref } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { Props, CreateResourceForm } from '../model/createEditorTypes'


const props = defineProps<{
  form: Pick<CreateResourceForm, "platform" | "type">
  allowRateEdit: NonNullable<Props['allowRateEdit']>
  editQuotaLimit: UnwrapRef<Ref<number | null, number | null>>
  editQuotaDailyLimit: UnwrapRef<Ref<number | null, number | null>>
  editQuotaWeeklyLimit: UnwrapRef<Ref<number | null, number | null>>
  quotaNotifyGlobalEnabled: UnwrapRef<ReturnType<typeof useQuotaNotifyState>["globalEnabled"]>
  quotaNotifyState: UnwrapRef<ReturnType<typeof useQuotaNotifyState>["state"]>
  editDailyResetMode: UnwrapRef<Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>>
  editDailyResetHour: UnwrapRef<Ref<number | null, number | null>>
  editWeeklyResetMode: UnwrapRef<Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>>
  editWeeklyResetDay: UnwrapRef<Ref<number | null, number | null>>
  editWeeklyResetHour: UnwrapRef<Ref<number | null, number | null>>
  editResetTimezone: UnwrapRef<Ref<string | null, string | null>>
}>()

const emit = defineEmits<{
  'update:editQuotaLimit': [value: UnwrapRef<Ref<number | null, number | null>>]
  'update:editQuotaDailyLimit': [value: UnwrapRef<Ref<number | null, number | null>>]
  'update:editQuotaWeeklyLimit': [value: UnwrapRef<Ref<number | null, number | null>>]
  'update:quotaNotifyState': [value: UnwrapRef<ReturnType<typeof useQuotaNotifyState>["state"]>]
  'update:editDailyResetMode': [value: UnwrapRef<Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>>]
  'update:editDailyResetHour': [value: UnwrapRef<Ref<number | null, number | null>>]
  'update:editWeeklyResetMode': [value: UnwrapRef<Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>>]
  'update:editWeeklyResetDay': [value: UnwrapRef<Ref<number | null, number | null>>]
  'update:editWeeklyResetHour': [value: UnwrapRef<Ref<number | null, number | null>>]
  'update:editResetTimezone': [value: UnwrapRef<Ref<string | null, string | null>>]
}>()

const { t } = useI18n()

const form = computed(() => props.form)

const allowRateEdit = computed(() => props.allowRateEdit)

const editQuotaLimit = usePanelField(() => props.editQuotaLimit, value => emit('update:editQuotaLimit', value))

const editQuotaDailyLimit = usePanelField(() => props.editQuotaDailyLimit, value => emit('update:editQuotaDailyLimit', value))

const editQuotaWeeklyLimit = usePanelField(() => props.editQuotaWeeklyLimit, value => emit('update:editQuotaWeeklyLimit', value))

const quotaNotifyGlobalEnabled = computed(() => props.quotaNotifyGlobalEnabled)

const quotaNotifyState = usePanelField(() => props.quotaNotifyState, value => emit('update:quotaNotifyState', value))

const editDailyResetMode = usePanelField(() => props.editDailyResetMode, value => emit('update:editDailyResetMode', value))

const editDailyResetHour = usePanelField(() => props.editDailyResetHour, value => emit('update:editDailyResetHour', value))

const editWeeklyResetMode = usePanelField(() => props.editWeeklyResetMode, value => emit('update:editWeeklyResetMode', value))

const editWeeklyResetDay = usePanelField(() => props.editWeeklyResetDay, value => emit('update:editWeeklyResetDay', value))

const editWeeklyResetHour = usePanelField(() => props.editWeeklyResetHour, value => emit('update:editWeeklyResetHour', value))

const editResetTimezone = usePanelField(() => props.editResetTimezone, value => emit('update:editResetTimezone', value))
</script>

<template>
<div
        v-if="form.platform === 'anthropic' && (form.type === 'apikey' || form.type === 'bedrock')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
      >
        <div class="mb-3">
          <h3 class="input-label mb-0 text-base font-semibold">{{ t('admin.accounts.quotaControl.title') }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.quotaControl.hint') }}
          </p>
        </div>
        <QuotaLimitCard v-if="allowRateEdit"
          :totalLimit="editQuotaLimit"
          :dailyLimit="editQuotaDailyLimit"
          :weeklyLimit="editQuotaWeeklyLimit"
          :quotaNotifyGlobalEnabled="quotaNotifyGlobalEnabled"
          :quotaNotifyDailyEnabled="quotaNotifyState.daily.enabled"
          :quotaNotifyDailyThreshold="quotaNotifyState.daily.threshold"
          :quotaNotifyDailyThresholdType="quotaNotifyState.daily.thresholdType"
          :quotaNotifyWeeklyEnabled="quotaNotifyState.weekly.enabled"
          :quotaNotifyWeeklyThreshold="quotaNotifyState.weekly.threshold"
          :quotaNotifyWeeklyThresholdType="quotaNotifyState.weekly.thresholdType"
          :quotaNotifyTotalEnabled="quotaNotifyState.total.enabled"
          :quotaNotifyTotalThreshold="quotaNotifyState.total.threshold"
          :quotaNotifyTotalThresholdType="quotaNotifyState.total.thresholdType"
          :dailyResetMode="editDailyResetMode"
          :dailyResetHour="editDailyResetHour"
          :weeklyResetMode="editWeeklyResetMode"
          :weeklyResetDay="editWeeklyResetDay"
          :weeklyResetHour="editWeeklyResetHour"
          :resetTimezone="editResetTimezone"
          @update:totalLimit="editQuotaLimit = $event"
          @update:dailyLimit="editQuotaDailyLimit = $event"
          @update:weeklyLimit="editQuotaWeeklyLimit = $event"
          @update:quotaNotifyDailyEnabled="quotaNotifyState.daily.enabled = $event"
          @update:quotaNotifyDailyThreshold="quotaNotifyState.daily.threshold = $event"
          @update:quotaNotifyDailyThresholdType="quotaNotifyState.daily.thresholdType = $event"
          @update:quotaNotifyWeeklyEnabled="quotaNotifyState.weekly.enabled = $event"
          @update:quotaNotifyWeeklyThreshold="quotaNotifyState.weekly.threshold = $event"
          @update:quotaNotifyWeeklyThresholdType="quotaNotifyState.weekly.thresholdType = $event"
          @update:quotaNotifyTotalEnabled="quotaNotifyState.total.enabled = $event"
          @update:quotaNotifyTotalThreshold="quotaNotifyState.total.threshold = $event"
          @update:quotaNotifyTotalThresholdType="quotaNotifyState.total.thresholdType = $event"
          @update:dailyResetMode="editDailyResetMode = $event"
          @update:dailyResetHour="editDailyResetHour = $event"
          @update:weeklyResetMode="editWeeklyResetMode = $event"
          @update:weeklyResetDay="editWeeklyResetDay = $event"
          @update:weeklyResetHour="editWeeklyResetHour = $event"
          @update:resetTimezone="editResetTimezone = $event"
        />
      </div>

      <!-- 配额控制 (非 Anthropic apikey/bedrock，以及 Vertex service_account) -->
      <div
        v-else-if="supportsAccountQuota(form.type)"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
      >
        <div class="mb-3">
          <h3 class="input-label mb-0 text-base font-semibold">{{ t('admin.accounts.quotaControl.title') }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.quotaLimitHint') }}
          </p>
        </div>
        <QuotaLimitCard v-if="allowRateEdit"
          :totalLimit="editQuotaLimit"
          :dailyLimit="editQuotaDailyLimit"
          :weeklyLimit="editQuotaWeeklyLimit"
          :quotaNotifyGlobalEnabled="quotaNotifyGlobalEnabled"
          :quotaNotifyDailyEnabled="quotaNotifyState.daily.enabled"
          :quotaNotifyDailyThreshold="quotaNotifyState.daily.threshold"
          :quotaNotifyDailyThresholdType="quotaNotifyState.daily.thresholdType"
          :quotaNotifyWeeklyEnabled="quotaNotifyState.weekly.enabled"
          :quotaNotifyWeeklyThreshold="quotaNotifyState.weekly.threshold"
          :quotaNotifyWeeklyThresholdType="quotaNotifyState.weekly.thresholdType"
          :quotaNotifyTotalEnabled="quotaNotifyState.total.enabled"
          :quotaNotifyTotalThreshold="quotaNotifyState.total.threshold"
          :quotaNotifyTotalThresholdType="quotaNotifyState.total.thresholdType"
          :dailyResetMode="editDailyResetMode"
          :dailyResetHour="editDailyResetHour"
          :weeklyResetMode="editWeeklyResetMode"
          :weeklyResetDay="editWeeklyResetDay"
          :weeklyResetHour="editWeeklyResetHour"
          :resetTimezone="editResetTimezone"
          @update:totalLimit="editQuotaLimit = $event"
          @update:dailyLimit="editQuotaDailyLimit = $event"
          @update:weeklyLimit="editQuotaWeeklyLimit = $event"
          @update:quotaNotifyDailyEnabled="quotaNotifyState.daily.enabled = $event"
          @update:quotaNotifyDailyThreshold="quotaNotifyState.daily.threshold = $event"
          @update:quotaNotifyDailyThresholdType="quotaNotifyState.daily.thresholdType = $event"
          @update:quotaNotifyWeeklyEnabled="quotaNotifyState.weekly.enabled = $event"
          @update:quotaNotifyWeeklyThreshold="quotaNotifyState.weekly.threshold = $event"
          @update:quotaNotifyWeeklyThresholdType="quotaNotifyState.weekly.thresholdType = $event"
          @update:quotaNotifyTotalEnabled="quotaNotifyState.total.enabled = $event"
          @update:quotaNotifyTotalThreshold="quotaNotifyState.total.threshold = $event"
          @update:quotaNotifyTotalThresholdType="quotaNotifyState.total.thresholdType = $event"
          @update:dailyResetMode="editDailyResetMode = $event"
          @update:dailyResetHour="editDailyResetHour = $event"
          @update:weeklyResetMode="editWeeklyResetMode = $event"
          @update:weeklyResetDay="editWeeklyResetDay = $event"
          @update:weeklyResetHour="editWeeklyResetHour = $event"
          @update:resetTimezone="editResetTimezone = $event"
        />
      </div>
</template>
