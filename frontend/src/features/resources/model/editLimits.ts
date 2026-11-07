import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAccountQuotaNotificationDraft as useQuotaNotifyState } from '@/features/resources/model/useAccountQuotaNotificationDraft'
import type { Account } from '@/features/resources/api/accountTypes';
import type { Ref, ComputedRef } from 'vue'




interface EditLimitsInput {
  t: ReturnType<typeof useI18n>["t"]
  tlsFingerprintEnabled: Ref<boolean, boolean>
  tlsFingerprintProfileId: Ref<number | null, number | null>
  sessionIdMaskingEnabled: Ref<boolean, boolean>
  cacheTTLOverrideEnabled: Ref<boolean, boolean>
  cacheTTLOverrideTarget: Ref<string, string>
  customBaseUrlEnabled: Ref<boolean, boolean>
  customBaseUrl: Ref<string, string>
}




export function useEditLimitsDraft(input: EditLimitsInput): {
  autoPauseOnExpired: Ref<boolean, boolean>
  autoPause5hThreshold: Ref<number | null, number | null>
  autoPause7dThreshold: Ref<number | null, number | null>
  autoPause5hDisabled: Ref<boolean, boolean>
  autoPause7dDisabled: Ref<boolean, boolean>
  autoResetCreditEnabled: Ref<boolean, boolean>
  autoResetCredit5hThreshold: Ref<number, number>
  autoResetCredit7dThreshold: Ref<number, number>
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
  quotaNotifyGlobalEnabled: ReturnType<typeof useQuotaNotifyState>["globalEnabled"]
  quotaNotifyState: ReturnType<typeof useQuotaNotifyState>["state"]
  loadQuotaNotifyGlobal: ReturnType<typeof useQuotaNotifyState>["loadGlobalState"]
  loadQuotaNotifyFromExtra: ReturnType<typeof useQuotaNotifyState>["loadFromExtra"]
  writeQuotaNotifyToExtra: ReturnType<typeof useQuotaNotifyState>["writeToExtra"]
  resetQuotaNotify: ReturnType<typeof useQuotaNotifyState>["reset"]
  editQuotaLimit: Ref<number | null, number | null>
  editQuotaDailyLimit: Ref<number | null, number | null>
  editQuotaWeeklyLimit: Ref<number | null, number | null>
  editDailyResetMode: Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>
  editDailyResetHour: Ref<number | null, number | null>
  editWeeklyResetMode: Ref<"rolling" | "fixed" | null, "rolling" | "fixed" | null>
  editWeeklyResetDay: Ref<number | null, number | null>
  editWeeklyResetHour: Ref<number | null, number | null>
  editResetTimezone: Ref<string | null, string | null>
  loadQuotaControlSettings: (account: Account) => void
} {

  const autoPauseOnExpired = ref(false)

  const autoPause5hThreshold = ref<number | null>(null)

  const autoPause7dThreshold = ref<number | null>(null)

  const autoPause5hDisabled = ref(false)

  const autoPause7dDisabled = ref(false)

  const autoResetCreditEnabled = ref(false)

  const autoResetCredit5hThreshold = ref(100)

  const autoResetCredit7dThreshold = ref(100)


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

  const {
    globalEnabled: quotaNotifyGlobalEnabled,
    state: quotaNotifyState,
    loadGlobalState: loadQuotaNotifyGlobal,
    loadFromExtra: loadQuotaNotifyFromExtra,
    writeToExtra: writeQuotaNotifyToExtra,
    reset: resetQuotaNotify,
  } = useQuotaNotifyState()

  const editQuotaLimit = ref<number | null>(null)

  const editQuotaDailyLimit = ref<number | null>(null)

  const editQuotaWeeklyLimit = ref<number | null>(null)

  const editDailyResetMode = ref<'rolling' | 'fixed' | null>(null)

  const editDailyResetHour = ref<number | null>(null)

  const editWeeklyResetMode = ref<'rolling' | 'fixed' | null>(null)

  const editWeeklyResetDay = ref<number | null>(null)

  const editWeeklyResetHour = ref<number | null>(null)

  const editResetTimezone = ref<string | null>(null)


  // Load quota control settings from account (Anthropic OAuth/SetupToken only)
  function loadQuotaControlSettings(account: Account) {
    // Reset all quota control state first
    windowCostEnabled.value = false
    windowCostLimit.value = null
    windowCostStickyReserve.value = null
    sessionLimitEnabled.value = false
    maxSessions.value = null
    sessionIdleTimeout.value = null
    rpmLimitEnabled.value = false
    baseRpm.value = null
    rpmStrategy.value = 'tiered'
    rpmStickyBuffer.value = null
    userMsgQueueMode.value = ''
    input.tlsFingerprintEnabled.value = false
    input.tlsFingerprintProfileId.value = null
    input.sessionIdMaskingEnabled.value = false
    input.cacheTTLOverrideEnabled.value = false
    input.cacheTTLOverrideTarget.value = '5m'
    input.customBaseUrlEnabled.value = false
    input.customBaseUrl.value = ''

    // Remaining quota control settings only apply to Anthropic accounts
    if (account.platform !== 'anthropic') {
      return
    }

    // Window cost / session limit only apply to Anthropic OAuth/SetupToken accounts
    if (account.type !== 'oauth' && account.type !== 'setup-token') {
      return
    }

    // Load from extra field (via backend DTO fields)
    if (account.window_cost_limit != null && account.window_cost_limit > 0) {
      windowCostEnabled.value = true
      windowCostLimit.value = account.window_cost_limit
      windowCostStickyReserve.value = account.window_cost_sticky_reserve ?? 10
    }

    if (account.max_sessions != null && account.max_sessions > 0) {
      sessionLimitEnabled.value = true
      maxSessions.value = account.max_sessions
      sessionIdleTimeout.value = account.session_idle_timeout_minutes ?? 5
    }

    // RPM limit
    if (account.base_rpm != null && account.base_rpm > 0) {
      rpmLimitEnabled.value = true
      baseRpm.value = account.base_rpm
      rpmStrategy.value = (account.rpm_strategy as 'tiered' | 'sticky_exempt') || 'tiered'
      rpmStickyBuffer.value = account.rpm_sticky_buffer ?? null
    }

    // UMQ mode（独立于 RPM 加载，防止编辑无 RPM 账号时丢失已有配置）
    userMsgQueueMode.value = account.user_msg_queue_mode ?? ''

    // Load TLS fingerprint setting
    if (account.enable_tls_fingerprint === true) {
      input.tlsFingerprintEnabled.value = true
    }
    input.tlsFingerprintProfileId.value = account.tls_fingerprint_profile_id ?? null

    // Load session ID masking setting
    if (account.session_id_masking_enabled === true) {
      input.sessionIdMaskingEnabled.value = true
    }

    // Load cache TTL override setting
    if (account.cache_ttl_override_enabled === true) {
      input.cacheTTLOverrideEnabled.value = true
      input.cacheTTLOverrideTarget.value = account.cache_ttl_override_target || '5m'
    }

    // Load custom base URL setting
    if (account.custom_base_url_enabled === true) {
      input.customBaseUrlEnabled.value = true
      input.customBaseUrl.value = account.custom_base_url || ''
    }
  }
  return { autoPauseOnExpired, autoPause5hThreshold, autoPause7dThreshold, autoPause5hDisabled, autoPause7dDisabled, autoResetCreditEnabled, autoResetCredit5hThreshold, autoResetCredit7dThreshold, windowCostEnabled, windowCostLimit, windowCostStickyReserve, sessionLimitEnabled, maxSessions, sessionIdleTimeout, rpmLimitEnabled, baseRpm, rpmStrategy, rpmStickyBuffer, userMsgQueueMode, umqModeOptions, quotaNotifyGlobalEnabled, quotaNotifyState, loadQuotaNotifyGlobal, loadQuotaNotifyFromExtra, writeQuotaNotifyToExtra, resetQuotaNotify, editQuotaLimit, editQuotaDailyLimit, editQuotaWeeklyLimit, editDailyResetMode, editDailyResetHour, editWeeklyResetMode, editWeeklyResetDay, editWeeklyResetHour, editResetTimezone, loadQuotaControlSettings }
}
