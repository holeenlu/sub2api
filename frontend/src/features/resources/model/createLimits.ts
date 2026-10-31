import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAccountQuotaNotificationDraft as useQuotaNotifyState } from '@/features/resources/model/useAccountQuotaNotificationDraft'
import type { Ref, ComputedRef } from 'vue'




interface CreateLimitsInput {
  t: ReturnType<typeof useI18n>["t"]
}




export function useCreateLimitsDraft(input: CreateLimitsInput): {
  editQuotaLimit: Ref<number | null, number | null>
  editQuotaDailyLimit: Ref<number | null, number | null>
  editQuotaWeeklyLimit: Ref<number | null, number | null>
  editDailyResetMode: Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>
  editDailyResetHour: Ref<number | null, number | null>
  editWeeklyResetMode: Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>
  editWeeklyResetDay: Ref<number | null, number | null>
  editWeeklyResetHour: Ref<number | null, number | null>
  editResetTimezone: Ref<string | null, string | null>
  autoPauseOnExpired: Ref<boolean, boolean>
  quotaNotifyGlobalEnabled: ReturnType<typeof useQuotaNotifyState>["globalEnabled"]
  quotaNotifyState: ReturnType<typeof useQuotaNotifyState>["state"]
  loadQuotaNotifyGlobal: ReturnType<typeof useQuotaNotifyState>["loadGlobalState"]
  writeQuotaNotifyToExtra: ReturnType<typeof useQuotaNotifyState>["writeToExtra"]
  windowCostEnabled: Ref<boolean, boolean>
  windowCostLimit: Ref<number | null, number | null>
  windowCostStickyReserve: Ref<number | null, number | null>
  sessionLimitEnabled: Ref<boolean, boolean>
  maxSessions: Ref<number | null, number | null>
  sessionIdleTimeout: Ref<number | null, number | null>
  rpmLimitEnabled: Ref<boolean, boolean>
  baseRpm: Ref<number | null, number | null>
  rpmStrategy: Ref<"tiered" | "sticky_exempt", "tiered" | "sticky_exempt">
  rpmStickyBuffer: Ref<number | null, number | null>
  userMsgQueueMode: Ref<string, string>
  umqModeOptions: ComputedRef<{ value: string; label: string }[]>
  geminiQuotaDocs: { codeAssist: string; aiStudio: string; vertex: string }
} {


  const editQuotaLimit = ref<number | null>(null)

  const editQuotaDailyLimit = ref<number | null>(null)

  const editQuotaWeeklyLimit = ref<number | null>(null)

  const editDailyResetMode = ref<'rolling' | 'fixed' | null>(null)

  const editDailyResetHour = ref<number | null>(null)

  const editWeeklyResetMode = ref<'rolling' | 'fixed' | null>(null)

  const editWeeklyResetDay = ref<number | null>(null)

  const editWeeklyResetHour = ref<number | null>(null)

  const editResetTimezone = ref<string | null>(null)

  const autoPauseOnExpired = ref(true)

  const {
    globalEnabled: quotaNotifyGlobalEnabled,
    state: quotaNotifyState,
    loadGlobalState: loadQuotaNotifyGlobal,
    writeToExtra: writeQuotaNotifyToExtra,
  } = useQuotaNotifyState()


  // Quota control state (Anthropic OAuth/SetupToken only)
  const windowCostEnabled = ref(false)

  const windowCostLimit = ref<number | null>(null)

  const windowCostStickyReserve = ref<number | null>(null)

  const sessionLimitEnabled = ref(false)

  const maxSessions = ref<number | null>(null)

  const sessionIdleTimeout = ref<number | null>(null)

  const rpmLimitEnabled = ref(false)

  const baseRpm = ref<number | null>(null)

  const rpmStrategy = ref<'tiered' | 'sticky_exempt'>('tiered')

  const rpmStickyBuffer = ref<number | null>(null)

  const userMsgQueueMode = ref('')

  const umqModeOptions = computed(() => [
    { value: '', label: input.t('admin.accounts.quotaControl.rpmLimit.umqModeOff') },
    { value: 'throttle', label: input.t('admin.accounts.quotaControl.rpmLimit.umqModeThrottle') },
    { value: 'serialize', label: input.t('admin.accounts.quotaControl.rpmLimit.umqModeSerialize') },
  ])


  const geminiQuotaDocs = {
    codeAssist: 'https://developers.google.com/gemini-code-assist/resources/quotas',
    aiStudio: 'https://ai.google.dev/pricing',
    vertex: 'https://cloud.google.com/vertex-ai/generative-ai/docs/quotas'
  }
  return { editQuotaLimit, editQuotaDailyLimit, editQuotaWeeklyLimit, editDailyResetMode, editDailyResetHour, editWeeklyResetMode, editWeeklyResetDay, editWeeklyResetHour, editResetTimezone, autoPauseOnExpired, quotaNotifyGlobalEnabled, quotaNotifyState, loadQuotaNotifyGlobal, writeQuotaNotifyToExtra, windowCostEnabled, windowCostLimit, windowCostStickyReserve, sessionLimitEnabled, maxSessions, sessionIdleTimeout, rpmLimitEnabled, baseRpm, rpmStrategy, rpmStickyBuffer, userMsgQueueMode, umqModeOptions, geminiQuotaDocs }
}
