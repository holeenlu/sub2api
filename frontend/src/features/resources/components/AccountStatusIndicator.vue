<template>
    <span
      v-if="isExcelBPSEnabled"
      data-testid="bps-status-badge"
      class="inline-flex items-center rounded bg-[#217346] px-1.5 py-0.5 text-[10px] font-semibold leading-3 text-white"
      :title="t('admin.accounts.openai.excelBPS')"
    >bps</span>
  <div class="flex items-center gap-2">
    <span
      v-for="item in bpsCooldowns"
      :key="item.scope"
      data-testid="bps-cooldown-badge"
      class="badge text-xs badge-warning"
      :title="t('admin.accounts.status.bpsCooldownHint') + ' ' + t('admin.accounts.status.bpsCooldownUntil', { time: formatDateTime(item.resetAt) })"
    >{{ bpsScopeLabel(item.presentation) }}</span>
    <!-- Rate Limit Display (429) - Two-line layout -->
    <div v-if="isRateLimited" class="flex flex-col items-center gap-1">
      <span class="badge text-xs badge-warning">{{ t('admin.accounts.status.rateLimited') }}</span>
      <span v-if="rateLimitResetAt" class="text-[11px] text-gray-400 dark:text-gray-500">{{ rateLimitResumeText }}</span>
    </div>

    <!-- Overload Display (529) - Two-line layout -->
    <div v-else-if="isOverloaded" class="flex flex-col items-center gap-1">
      <span class="badge text-xs badge-danger">{{ t('admin.accounts.status.overloaded') }}</span>
      <span class="text-[11px] text-gray-400 dark:text-gray-500">{{ overloadCountdown }}</span>
    </div>

    <!-- Main Status Badge (shown when not rate limited/overloaded) -->
    <template v-else>
      <div v-if="isTempUnschedulable" class="flex flex-col items-center gap-1">
        <button
          type="button"
          :class="['badge text-xs', statusClass, 'cursor-pointer']"
          :title="hasError && errorMessage ? errorMessage : t('admin.accounts.status.viewTempUnschedDetails')"
          @click="handleTempUnschedClick"
        >
          {{ statusText }}
        </button>
        <span class="max-w-[180px] text-center text-[11px] leading-4 text-gray-500 dark:text-gray-400">
          {{ tempUnschedRecoveryText }}
        </span>
      </div>
      <span v-else :class="['badge text-xs', statusClass]" :title="hasError ? errorMessage : undefined">
        {{ statusText }}
      </span>
    </template>

    <!-- Error Info Indicator -->
    <HelpTooltip v-if="hasError && errorMessage" class="!ml-0" width-class="w-72 max-w-[calc(100vw-16px)]" tone="brand">
      <template #trigger>
        <svg
          class="h-4 w-4 cursor-help text-red-500 transition-colors hover:text-red-600 dark:text-red-400 dark:hover:text-red-300"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9 5.25h.008v.008H12v-.008z"
          />
        </svg>
      </template>
      <div class="whitespace-pre-wrap break-words leading-relaxed">{{ errorMessage }}</div>
    </HelpTooltip>

    <!-- Rate Limit Indicator (429) -->
    <div v-if="isRateLimited" class="group relative">
      <span
        class="inline-flex items-center gap-1 rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400"
      >
        <Icon name="exclamationTriangle" size="xs" :stroke-width="2" />
        429
      </span>
      <!-- Tooltip -->
      <div
        v-if="rateLimitResetAt"
        class="pointer-events-none absolute bottom-full left-1/2 z-50 mb-2 w-56 -translate-x-1/2 whitespace-normal rounded bg-gray-900 px-3 py-2 text-center text-xs leading-relaxed text-white opacity-0 transition-opacity group-hover:opacity-100 dark:bg-gray-700"
      >
        {{ t('admin.accounts.status.rateLimitedUntil', { time: formatDateTime(rateLimitResetAt) }) }}
        <div
          class="absolute left-1/2 top-full -translate-x-1/2 border-4 border-transparent border-t-gray-900 dark:border-t-gray-700"
        ></div>
      </div>
    </div>

    <!-- Model Status Indicators (普通限流 / 超量请求中) -->
    <div
      v-if="activeModelStatuses.length > 0"
      :class="[
        activeModelStatuses.length <= 4
          ? 'flex flex-col gap-1'
          : activeModelStatuses.length <= 8
            ? 'columns-2 gap-x-2'
            : 'columns-3 gap-x-2'
      ]"
    >
      <div v-for="item in activeModelStatuses" :key="`${item.kind}-${item.model}`" class="group relative mb-1 break-inside-avoid">
        <!-- 积分已用尽 -->
        <span
          v-if="item.kind === 'credits_exhausted'"
          class="inline-flex items-center gap-1 rounded bg-red-100 px-1.5 py-0.5 text-xs font-medium text-red-700 dark:bg-red-900/30 dark:text-red-400"
        >
          <Icon name="exclamationTriangle" size="xs" :stroke-width="2" />
          {{ t('admin.accounts.status.creditsExhausted') }}
          <span v-if="item.reset_at" class="text-[10px] opacity-70">{{ formatCountdown(item.reset_at) }}</span>
        </span>
        <!-- 正在走积分（模型限流但积分可用）-->
        <span
          v-else-if="item.kind === 'credits_active'"
          class="inline-flex items-center gap-1 rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400"
        >
          <span>⚡</span>
          {{ formatScopeName(item.model) }}
          <span v-if="item.reset_at" class="text-[10px] opacity-70">{{ formatCountdown(item.reset_at) }}</span>
        </span>
        <!-- 普通模型限流 -->
        <span
          v-else
          class="inline-flex items-center gap-1 rounded bg-purple-100 px-1.5 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/30 dark:text-purple-400"
        >
          <Icon name="exclamationTriangle" size="xs" :stroke-width="2" />
          {{ formatScopeName(item.model) }}
          <span v-if="item.reset_at" class="text-[10px] opacity-70">{{ formatCountdown(item.reset_at) }}</span>
        </span>
        <!-- Tooltip -->
        <div
          v-if="item.reset_at"
          class="pointer-events-none absolute bottom-full left-1/2 z-50 mb-2 w-max max-w-[320px] -translate-x-1/2 whitespace-nowrap rounded bg-gray-900 px-3 py-2 text-center text-xs leading-relaxed text-white opacity-0 transition-opacity group-hover:opacity-100 dark:bg-gray-700"
        >
          {{
            item.kind === 'credits_exhausted'
              ? t('admin.accounts.status.creditsExhaustedUntil', { time: formatDateTimeToMinute(item.reset_at) })
              : item.kind === 'credits_active'
                ? t('admin.accounts.status.modelCreditOveragesUntil', { model: formatScopeName(item.model), time: formatDateTimeToMinute(item.reset_at) })
                : t('admin.accounts.status.modelRateLimitedUntil', { model: formatScopeName(item.model), time: formatDateTimeToMinute(item.reset_at) })
          }}
          <div
            class="absolute left-1/2 top-full -translate-x-1/2 border-4 border-transparent border-t-gray-900 dark:border-t-gray-700"
          ></div>
        </div>
      </div>
    </div>

    <!-- Overload Indicator (529) -->
    <div v-if="isOverloaded" class="group relative">
      <span
        class="inline-flex items-center gap-1 rounded bg-red-100 px-1.5 py-0.5 text-xs font-medium text-red-700 dark:bg-red-900/30 dark:text-red-400"
      >
        <Icon name="exclamationTriangle" size="xs" :stroke-width="2" />
        529
      </span>
      <!-- Tooltip -->
      <div
        class="pointer-events-none absolute bottom-full left-1/2 z-50 mb-2 w-56 -translate-x-1/2 whitespace-normal rounded bg-gray-900 px-3 py-2 text-center text-xs leading-relaxed text-white opacity-0 transition-opacity group-hover:opacity-100 dark:bg-gray-700"
      >
        {{ t('admin.accounts.status.overloadedUntil', { time: formatTime(account.overload_until) }) }}
        <div
          class="absolute left-1/2 top-full -translate-x-1/2 border-4 border-transparent border-t-gray-900 dark:border-t-gray-700"
        ></div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts" generic="T extends { platform: string; type: string; status: string; schedulable: boolean } & Partial<Pick<Account, 'parent_account_id' | 'extra' | 'credentials' | 'anthropic_status' | 'rate_limit_reset_at' | 'overload_until' | 'temp_unschedulable_until' | 'error_message' | 'quota_used' | 'quota_limit' | 'quota_daily_used' | 'quota_daily_limit' | 'quota_weekly_used' | 'quota_weekly_limit'>>">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import HelpTooltip from '@/shared/ui/HelpTooltip.vue'
import type { Account } from '@/features/resources/api/accountTypes';
import { isBPSCooldownScope, readActiveAccountRateLimits, readBPSCooldownScope } from '../model/accountRateLimitScopes'
import type { BPSCooldownScope } from '../model/accountRateLimitScopes'
import { formatCountdown, formatDateTime, formatDateTimeToMinute, formatCountdownWithSuffix, formatTime } from '@/shared/lib/format'

const { t } = useI18n()

const props = defineProps<{
  account: T
}>()

const emit = defineEmits<{
  (e: 'show-temp-unsched', account: T): void
}>()

// Keep the account-list badge aligned with the backend BPS eligibility check.
const isExcelBPSEnabled = computed(() => {
  const account = props.account
  if (
    account.platform !== 'openai' ||
    account.type !== 'oauth' ||
    account.parent_account_id != null ||
    account.extra?.openai_excel_bps !== true
  ) return false
  const credential = (key: string) => {
    const value = account.credentials?.[key]
    return typeof value === 'string' ? value.trim().toLowerCase() : ''
  }
  const isPAT = (mode: string) => mode === 'personalaccesstoken' || mode === 'personal_access_token'
  return credential('plan_type') !== 'free' && credential('auth_mode') !== 'agentidentity' &&
    !isPAT(credential('auth_mode')) && !isPAT(credential('openai_auth_mode'))
})

// Computed: is rate limited (429)
const upstreamStatus = computed(() => props.account.platform === 'anthropic' && props.account.status === 'active'
  ? props.account.anthropic_status : null)
const rateLimitResetAt = computed(() => {
  const native = props.account.rate_limit_reset_at
  return native && new Date(native) > new Date() ? native : upstreamStatus.value?.rate_limit_reset_at
})
const isRateLimited = computed(() => {
  if (upstreamStatus.value?.error_code) return false
  const reset = rateLimitResetAt.value
  if (reset) return new Date(reset) > new Date()
  return upstreamStatus.value?.rate_limited === true
})

type AccountModelStatusItem = {
  kind: 'rate_limit' | 'credits_exhausted' | 'credits_active'
  model: string
  reset_at: string
}

const modelCooldowns = computed(() => readActiveAccountRateLimits(props.account.extra?.model_rate_limits))
const bpsCooldowns = computed(() => {
  if (props.account.platform !== 'openai' || props.account.type !== 'oauth') return []
  return modelCooldowns.value.flatMap(item => {
    const presentation = readBPSCooldownScope(item.scope)
    return presentation ? [{ ...item, presentation }] : []
  })
})
function bpsScopeLabel(scope: BPSCooldownScope): string {
  switch (scope.kind) {
    case 'account': return t('admin.accounts.status.bpsScopeAccount')
    case 'model': return t('admin.accounts.status.bpsScopeModel', { model: scope.model })
    case 'endpoint': return t('admin.accounts.status.bpsScopeEndpoint', { endpoint: scope.endpoint })
    case 'model_endpoint': return t('admin.accounts.status.bpsScopeModelEndpoint', { model: scope.model, endpoint: scope.endpoint })
    case 'model_permission': return t('admin.accounts.status.bpsModelPermission', { model: scope.model })
    case 'unknown': return t('admin.accounts.status.bpsScopeUnknown')
  }
}

// Computed: active model statuses (普通模型限流 + 积分耗尽 + 走积分中)
const activeModelStatuses = computed<AccountModelStatusItem[]>(() => {
  const extra = props.account.extra as Record<string, unknown> | undefined
  const now = new Date()
  const items: AccountModelStatusItem[] = []

  const limits = modelCooldowns.value.filter(item => !isBPSCooldownScope(item.scope))

  // 检查 AICredits key 是否生效（积分是否耗尽）
  const hasActiveAICredits = limits.some(item => item.scope === 'AICredits')
  const allowOverages = !!(extra?.allow_overages)

  for (const { scope: model, resetAt } of limits) {
    if (model === 'AICredits') {
      // AICredits key → 积分已用尽
      items.push({ kind: 'credits_exhausted', model, reset_at: resetAt })
    } else if (allowOverages && !hasActiveAICredits) {
      // 普通模型限流 + overages 启用 + 积分可用 → 正在走积分
      items.push({ kind: 'credits_active', model, reset_at: resetAt })
    } else {
      // 普通模型限流
      items.push({ kind: 'rate_limit', model, reset_at: resetAt })
    }
  }

  for (const [model, reset] of Object.entries(upstreamStatus.value?.model_rate_limits ?? {})) {
    if (isBPSCooldownScope(model)) continue
    if ((reset && new Date(reset) <= now) || items.some(item => item.model === model)) continue
    items.push({ kind: 'rate_limit', model, reset_at: reset ?? '' })
  }
  return items
})

const formatScopeName = (scope: string): string => {
  const aliases: Record<string, string> = {
    // Claude 系列
    'claude-fable-5-1': 'CFable51',
    'claude-fable-5': 'CFable5',
    'claude-opus-4-6': 'COpus46',
    'claude-opus-4-6-thinking': 'COpus46T',
    'claude-opus-4-7': 'COpus47',
    'claude-opus-4-8': 'COpus48',
    'claude-opus-5-5': 'COpus55',
    'claude-opus-5': 'COpus5',
    'claude-sonnet-4-6': 'CSon46',
    'claude-sonnet-4-5': 'CSon45',
    'claude-sonnet-4-5-thinking': 'CSon45T',
    'claude-sonnet-5-5': 'CSon55',
    'claude-sonnet-5': 'CSon5',
    // Gemini 2.5 系列
    'gemini-2.5-flash': 'G25F',
    'gemini-2.5-flash-lite': 'G25FL',
    'gemini-2.5-flash-thinking': 'G25FT',
    'gemini-2.5-pro': 'G25P',
    'gemini-2.5-flash-image': 'G25I',
    // Gemini 3.5 系列
    'gemini-3.5-flash': 'G35F',
    // Gemini 3 系列
    'gemini-3-flash': 'G3F',
    'gemini-3.1-pro-high': 'G3PH',
    'gemini-3.1-pro-low': 'G3PL',
    'gemini-3-pro-image': 'G3PI',
    'gemini-3.1-flash-image': 'G31FI',
    // 其他
    'gpt-oss-120b-medium': 'GPT120',
    'tab_flash_lite_preview': 'TabFL',
    // 旧版 scope 别名（兼容）
    claude: 'Claude',
    claude_sonnet: 'CSon',
    claude_opus: 'COpus',
    claude_haiku: 'CHaiku',
    gemini_text: 'Gemini',
    gemini_image: 'GImg',
    gemini_flash: 'GFlash',
    gemini_pro: 'GPro',
  }
  return aliases[scope] || scope
}

// Computed: is overloaded (529)
const isOverloaded = computed(() => {
  if (!props.account.overload_until) return false
  return new Date(props.account.overload_until) > new Date()
})

// Computed: is temp unschedulable
const isTempUnschedulable = computed(() => {
  if (!props.account.temp_unschedulable_until) return false
  return new Date(props.account.temp_unschedulable_until) > new Date()
})

const errorMessage = computed(() => props.account.error_message || (upstreamStatus.value?.error_code
  ? t('admin.accounts.status.anthropicErrors.' + upstreamStatus.value.error_code) : ''))

// Computed: has error status
const hasError = computed(() => {
  return props.account.status === 'error' || !!upstreamStatus.value?.error_code
})

const isQuotaExceeded = computed(() => {
  const exceeded = (used?: number | null, limit?: number | null) =>
    typeof limit === 'number' && limit > 0 && typeof used === 'number' && used >= limit
  return (
    exceeded(props.account.quota_used, props.account.quota_limit) ||
    exceeded(props.account.quota_daily_used, props.account.quota_daily_limit) ||
    exceeded(props.account.quota_weekly_used, props.account.quota_weekly_limit)
  )
})

// Computed: countdown text for rate limit (429)
const rateLimitCountdown = computed(() => {
  return formatCountdown(rateLimitResetAt.value)
})

const rateLimitResumeText = computed(() => {
  if (!rateLimitCountdown.value) return ''
  return t('admin.accounts.status.rateLimitedAutoResume', { time: rateLimitCountdown.value })
})

// Computed: countdown text for overload (529)
const overloadCountdown = computed(() => {
  return formatCountdownWithSuffix(props.account.overload_until)
})

const tempUnschedRecoveryText = computed(() => {
  if (!isTempUnschedulable.value || !props.account.temp_unschedulable_until) return ''
  return t('admin.accounts.status.tempUnschedulableUntil', {
    time: formatDateTime(props.account.temp_unschedulable_until)
  })
})

// Computed: status badge class
const statusClass = computed(() => {
  if (hasError.value) {
    return 'badge-danger'
  }
  if (isTempUnschedulable.value) {
    return 'badge-warning'
  }
  if (props.account.status !== 'active') {
    return props.account.status === 'error' ? 'badge-danger' : 'badge-gray'
  }
  if (isQuotaExceeded.value) {
    return 'badge-warning'
  }
  if (!props.account.schedulable) {
    return 'badge-gray'
  }
  return 'badge-success'
})

// Computed: status text
const statusText = computed(() => {
  if (hasError.value) {
    return t('admin.accounts.status.error')
  }
  if (isTempUnschedulable.value) {
    return t('admin.accounts.status.tempUnschedulable')
  }
  if (props.account.status !== 'active') {
    return t(`admin.accounts.status.${props.account.status}`)
  }
  if (isQuotaExceeded.value) {
    return t('admin.accounts.status.quotaExceeded')
  }
  if (!props.account.schedulable) {
    return t('admin.accounts.status.paused')
  }
  return t(`admin.accounts.status.${props.account.status}`)
})

const handleTempUnschedClick = () => {
  if (!isTempUnschedulable.value) return
  emit('show-temp-unsched', props.account)
}
</script>
