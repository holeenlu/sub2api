import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/features/resources/api/accountTypes';
import type { TempUnschedulableScope } from '@/features/resources/api/accountObservationTypes';
import { createStableObjectKeyResolver } from '@/shared/lib/stableObjectKey'
import type { Ref, ComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { Props, TempUnschedRuleForm } from './editEditorTypes'




interface EditRestrictionsInput {
  ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY: "account_scheduling_threshold"
  ANTHROPIC_FABLE_SCHEDULING_THRESHOLD_CREDENTIAL_KEY: "anthropic_fable_scheduling_threshold"
  props: Readonly<Pick<Props, "account">>
  t: ReturnType<typeof useI18n>["t"]
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showError" | "showInfo">>
  toPositiveNumber: (value: unknown) => number | null
}




export function useEditRestrictionsDraft(input: EditRestrictionsInput): {
  DEFAULT_POOL_MODE_RETRY_COUNT: 3
  MAX_POOL_MODE_RETRY_COUNT: 10
  DEFAULT_POOL_MODE_RETRY_STATUS_CODES: number[]
  poolModeEnabled: Ref<boolean, boolean>
  poolModeRetryCount: Ref<number, number>
  poolModeRetryStatusCodesInput: Ref<string, string>
  parsePoolModeRetryStatusCodes: (input: string) => number[]
  formatPoolModeRetryStatusCodes: (value: unknown) => string
  customErrorCodesEnabled: Ref<boolean, boolean>
  selectedErrorCodes: Ref<number[], number[]>
  customErrorCodeInput: Ref<number | null, number | null>
  tempUnschedEnabled: Ref<boolean, boolean>
  accountSchedulingThresholdOverride: { enabled: Ref<boolean, boolean>; value: Ref<number, number>; load: (platform: Account["platform"] | undefined, type: Account["type"] | undefined, credentials: Record<string, unknown> | undefined) => void; applyPatch: (credentials: Record<string, unknown>, currentCredentials: Record<string, unknown>, platform?: Account["platform"] | undefined, type?: Account["type"] | undefined) => void }
  anthropicFableSchedulingThresholdOverride: { enabled: Ref<boolean, boolean>; value: Ref<number, number>; load: (platform: Account["platform"] | undefined, type: Account["type"] | undefined, credentials: Record<string, unknown> | undefined) => void; applyPatch: (credentials: Record<string, unknown>, currentCredentials: Record<string, unknown>, platform?: Account["platform"] | undefined, type?: Account["type"] | undefined) => void }
  accountSchedulingThresholdOverrideEnabled: Ref<boolean, boolean>
  accountSchedulingThresholdOverrideValue: Ref<number, number>
  supportsAccountSchedulingThresholdOverride: ComputedRef<boolean>
  anthropicFableSchedulingThresholdOverrideEnabled: Ref<boolean, boolean>
  anthropicFableSchedulingThresholdOverrideValue: Ref<number, number>
  supportsAnthropicFableSchedulingThresholdOverride: ComputedRef<boolean>
  tempUnschedRules: Ref<{ error_code: number | null; keywords: string; duration_minutes: number | null; description: string; scope: TempUnschedulableScope }[], TempUnschedRuleForm[] | { error_code: number | null; keywords: string; duration_minutes: number | null; description: string; scope: TempUnschedulableScope }[]>
  getTempUnschedRuleKey: (item: TempUnschedRuleForm) => string
  tempUnschedScopeOptions: ComputedRef<{ value: TempUnschedulableScope; label: string; description: string }[]>
  tempUnschedPresets: ComputedRef<{ label: string; rule: { error_code: number; keywords: string; duration_minutes: number; description: string; scope: "model" } }[]>
  normalizePoolModeRetryCount: (value: number) => number
  toggleErrorCode: (code: number) => void
  addCustomErrorCode: () => void
  removeErrorCode: (code: number) => void
  addTempUnschedRule: (preset?: TempUnschedRuleForm) => void
  removeTempUnschedRule: (index: number) => void
  moveTempUnschedRule: (index: number, direction: number) => void
  buildTempUnschedRules: (rules: TempUnschedRuleForm[]) => { error_code: number; keywords: string[]; duration_minutes: number; description: string; scope: TempUnschedulableScope }[]
  applyTempUnschedConfig: (credentials: Record<string, unknown>) => boolean
  supportsAccountSchedulingThresholdOverridePlatform: (platform: Account["platform"] | undefined) => boolean
  supportsAnthropicFableSchedulingThresholdAccount: (platform: Account["platform"] | undefined, type: Account["type"] | undefined) => boolean
  normalizeAccountSchedulingThresholdOverride: (value: unknown) => number | null
  clampAccountSchedulingThresholdOverride: (value: unknown) => number
  createSchedulingThresholdOverrideBinding: (credentialKey: string, supportsAccount: (platform: Account["platform"] | undefined, type: Account["type"] | undefined) => boolean) => { enabled: Ref<boolean, boolean>; value: Ref<number, number>; load: (platform: Account["platform"] | undefined, type: Account["type"] | undefined, credentials: Record<string, unknown> | undefined) => void; applyPatch: (credentials: Record<string, unknown>, currentCredentials: Record<string, unknown>, platform?: Account["platform"] | undefined, type?: Account["type"] | undefined) => void }
  loadAccountSchedulingThresholdOverride: (platform: Account["platform"] | undefined, type: Account["type"] | undefined, credentials: Record<string, unknown> | undefined) => void
  applyAccountSchedulingThresholdOverridePatch: (credentials: Record<string, unknown>, currentCredentials: Record<string, unknown>, platform?: Account["platform"] | undefined, type?: Account["type"] | undefined) => void
  loadTempUnschedRules: (credentials?: Record<string, unknown>) => void
  formatTempUnschedKeywords: (value: unknown) => string
  resolveTempUnschedScope: (value: unknown, errorCode: number | null) => TempUnschedulableScope
  splitTempUnschedKeywords: (value: string) => string[]
} {

  const DEFAULT_POOL_MODE_RETRY_COUNT = 3

  const MAX_POOL_MODE_RETRY_COUNT = 10

  const DEFAULT_POOL_MODE_RETRY_STATUS_CODES = [401, 403, 429]

  const poolModeEnabled = ref(false)

  const poolModeRetryCount = ref(DEFAULT_POOL_MODE_RETRY_COUNT)

  const poolModeRetryStatusCodesInput = ref('')


  function parsePoolModeRetryStatusCodes(input: string): number[] {
    if (!input || !input.trim()) return []
    const seen = new Set<number>()
    const out: number[] = []
    for (const token of input.split(/[,\s]+/)) {
      const trimmed = token.trim()
      if (!trimmed) continue
      const n = Number(trimmed)
      if (!Number.isFinite(n) || !Number.isInteger(n)) continue
      if (n < 100 || n > 599) continue
      if (seen.has(n)) continue
      seen.add(n)
      out.push(n)
    }
    return out.sort((a, b) => a - b)
  }


  function formatPoolModeRetryStatusCodes(value: unknown): string {
    if (!Array.isArray(value)) return ''
    const out: number[] = []
    const seen = new Set<number>()
    for (const v of value) {
      const n = typeof v === 'string' ? Number(v.trim()) : Number(v)
      if (!Number.isFinite(n) || !Number.isInteger(n)) continue
      if (n < 100 || n > 599) continue
      if (seen.has(n)) continue
      seen.add(n)
      out.push(n)
    }
    return out.sort((a, b) => a - b).join(', ')
  }

  const customErrorCodesEnabled = ref(false)

  const selectedErrorCodes = ref<number[]>([])

  const customErrorCodeInput = ref<number | null>(null)

  const tempUnschedEnabled = ref(false)


  const accountSchedulingThresholdOverride = createSchedulingThresholdOverrideBinding(
    input.ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY,
    supportsAccountSchedulingThresholdOverridePlatform
  )

  // Fable 是 Anthropic 独有的模型家族，其他平台没有这个概念。
  const anthropicFableSchedulingThresholdOverride = createSchedulingThresholdOverrideBinding(
    input.ANTHROPIC_FABLE_SCHEDULING_THRESHOLD_CREDENTIAL_KEY,
    supportsAnthropicFableSchedulingThresholdAccount
  )


  const accountSchedulingThresholdOverrideEnabled = accountSchedulingThresholdOverride.enabled

  const accountSchedulingThresholdOverrideValue = accountSchedulingThresholdOverride.value

  const supportsAccountSchedulingThresholdOverride = computed(() =>
    supportsAccountSchedulingThresholdOverridePlatform(input.props.account?.platform)
  )

  const anthropicFableSchedulingThresholdOverrideEnabled = anthropicFableSchedulingThresholdOverride.enabled

  const anthropicFableSchedulingThresholdOverrideValue = anthropicFableSchedulingThresholdOverride.value

  const supportsAnthropicFableSchedulingThresholdOverride = computed(() =>
    supportsAnthropicFableSchedulingThresholdAccount(input.props.account?.platform, input.props.account?.type)
  )

  const tempUnschedRules = ref<TempUnschedRuleForm[]>([])

  const getTempUnschedRuleKey = createStableObjectKeyResolver<TempUnschedRuleForm>('edit-temp-unsched-rule')

  const tempUnschedScopeOptions = computed<Array<{ value: TempUnschedulableScope; label: string; description: string }>>(() => [
    {
      value: 'model',
      label: input.t('admin.accounts.tempUnschedulable.scopeModel'),
      description: input.t('admin.accounts.tempUnschedulable.scopeModelDesc')
    },
    {
      value: 'account',
      label: input.t('admin.accounts.tempUnschedulable.scopeAccount'),
      description: input.t('admin.accounts.tempUnschedulable.scopeAccountDesc')
    }
  ])


  const tempUnschedPresets = computed(() => [
    {
      label: input.t('admin.accounts.tempUnschedulable.presets.overloadLabel'),
      rule: {
        error_code: 529,
        keywords: 'overloaded, too many',
        duration_minutes: 60,
        description: input.t('admin.accounts.tempUnschedulable.presets.overloadDesc'),
        scope: 'model' as const
      }
    },
    {
      label: input.t('admin.accounts.tempUnschedulable.presets.rateLimitLabel'),
      rule: {
        error_code: 429,
        keywords: 'rate limit, too many requests',
        duration_minutes: 10,
        description: input.t('admin.accounts.tempUnschedulable.presets.rateLimitDesc'),
        scope: 'model' as const
      }
    },
    {
      label: input.t('admin.accounts.tempUnschedulable.presets.unavailableLabel'),
      rule: {
        error_code: 503,
        keywords: 'unavailable, maintenance',
        duration_minutes: 30,
        description: input.t('admin.accounts.tempUnschedulable.presets.unavailableDesc'),
        scope: 'model' as const
      }
    }
  ])


  // Watchers
  const normalizePoolModeRetryCount = (value: number) => {
    if (!Number.isFinite(value)) {
      return DEFAULT_POOL_MODE_RETRY_COUNT
    }
    const normalized = Math.trunc(value)
    if (normalized < 0) {
      return 0
    }
    if (normalized > MAX_POOL_MODE_RETRY_COUNT) {
      return MAX_POOL_MODE_RETRY_COUNT
    }
    return normalized
  }


  // Error code toggle helper
  const toggleErrorCode = (code: number) => {
    const index = selectedErrorCodes.value.indexOf(code)
    if (index === -1) {
      // Adding code - check for 429/529 warning
      if (code === 429) {
        if (!confirm(input.t('admin.accounts.customErrorCodes429Warning'))) {
          return
        }
      } else if (code === 529) {
        if (!confirm(input.t('admin.accounts.customErrorCodes529Warning'))) {
          return
        }
      }
      selectedErrorCodes.value.push(code)
    } else {
      selectedErrorCodes.value.splice(index, 1)
    }
  }


  // Add custom error code from input
  const addCustomErrorCode = () => {
    const code = customErrorCodeInput.value
    if (code === null || code < 100 || code > 599) {
      input.appStore.showError(input.t('admin.accounts.invalidErrorCode'))
      return
    }
    if (selectedErrorCodes.value.includes(code)) {
      input.appStore.showInfo(input.t('admin.accounts.errorCodeExists'))
      return
    }
    // Check for 429/529 warning
    if (code === 429) {
      if (!confirm(input.t('admin.accounts.customErrorCodes429Warning'))) {
        return
      }
    } else if (code === 529) {
      if (!confirm(input.t('admin.accounts.customErrorCodes529Warning'))) {
        return
      }
    }
    selectedErrorCodes.value.push(code)
    customErrorCodeInput.value = null
  }


  // Remove error code
  const removeErrorCode = (code: number) => {
    const index = selectedErrorCodes.value.indexOf(code)
    if (index !== -1) {
      selectedErrorCodes.value.splice(index, 1)
    }
  }


  const addTempUnschedRule = (preset?: TempUnschedRuleForm) => {
    if (preset) {
      tempUnschedRules.value.push({ ...preset })
      return
    }
    tempUnschedRules.value.push({
      error_code: null,
      keywords: '',
      duration_minutes: 30,
      description: '',
      scope: 'model'
    })
  }


  const removeTempUnschedRule = (index: number) => {
    tempUnschedRules.value.splice(index, 1)
  }


  const moveTempUnschedRule = (index: number, direction: number) => {
    const target = index + direction
    if (target < 0 || target >= tempUnschedRules.value.length) return
    const rules = tempUnschedRules.value
    const current = rules[index]
    rules[index] = rules[target]
    rules[target] = current
  }


  const buildTempUnschedRules = (rules: TempUnschedRuleForm[]) => {
    const out: Array<{
      error_code: number
      keywords: string[]
      duration_minutes: number
      description: string
      scope: TempUnschedulableScope
    }> = []

    for (const rule of rules) {
      const errorCode = Number(rule.error_code)
      const duration = Number(rule.duration_minutes)
      const keywords = splitTempUnschedKeywords(rule.keywords)
      if (!Number.isFinite(errorCode) || errorCode < 100 || errorCode > 599) {
        continue
      }
      if (!Number.isFinite(duration) || duration <= 0) {
        continue
      }
      if (keywords.length === 0) {
        continue
      }
      out.push({
        error_code: Math.trunc(errorCode),
        keywords,
        duration_minutes: Math.trunc(duration),
        description: rule.description.trim(),
        scope: rule.scope
      })
    }

    return out
  }


  const applyTempUnschedConfig = (credentials: Record<string, unknown>) => {
    if (!tempUnschedEnabled.value) {
      delete credentials.temp_unschedulable_enabled
      delete credentials.temp_unschedulable_rules
      return true
    }

    const rules = buildTempUnschedRules(tempUnschedRules.value)
    if (rules.length === 0) {
      input.appStore.showError(input.t('admin.accounts.tempUnschedulable.rulesInvalid'))
      return false
    }

    credentials.temp_unschedulable_enabled = true
    credentials.temp_unschedulable_rules = rules
    return true
  }



  function supportsAccountSchedulingThresholdOverridePlatform(platform: Account['platform'] | undefined) {
    return platform === 'openai' || platform === 'anthropic' || platform === 'grok'
  }


  // 7d / 7d_oi 窗口只来自 Anthropic OAuth 类账号的被动采样（后端
  // supportsAnthropicPassiveUsage 同样只认 oauth / setup-token），apikey 与 bedrock
  // 永远没有窗口样本，覆盖写进去也不会生效，入口就不该出现。
  function supportsAnthropicFableSchedulingThresholdAccount(
    platform: Account['platform'] | undefined,
    type: Account['type'] | undefined
  ) {
    return platform === 'anthropic' && (type === 'oauth' || type === 'setup-token')
  }


  function normalizeAccountSchedulingThresholdOverride(value: unknown): number | null {
    if (value === null || value === undefined || value === '') {
      return null
    }
    const numeric = Number(value)
    if (!Number.isFinite(numeric)) {
      return null
    }
    const integer = Math.trunc(numeric)
    if (integer < 1 || integer > 100) {
      return null
    }
    return integer
  }


  function clampAccountSchedulingThresholdOverride(value: unknown): number {
    return Math.min(100, Math.max(1, Math.trunc(Number(value) || 100)))
  }


  // 通用阈值覆盖与 Fable 阈值覆盖只差一个 credentials 键和一个适用性判定，其余（回填、
  // 关闭时发 null 删键、无变化不进 patch）逐字相同，因此按键参数化成一个工厂。
  function createSchedulingThresholdOverrideBinding(
    credentialKey: string,
    supportsAccount: (
      platform: Account['platform'] | undefined,
      type: Account['type'] | undefined
    ) => boolean
  ) {
    const enabled = ref(false)
    const value = ref(100)

    const load = (
      platform: Account['platform'] | undefined,
      type: Account['type'] | undefined,
      credentials: Record<string, unknown> | undefined
    ) => {
      if (!supportsAccount(platform, type)) {
        enabled.value = false
        value.value = 100
        return
      }
      const stored = normalizeAccountSchedulingThresholdOverride(credentials?.[credentialKey])
      enabled.value = stored !== null
      value.value = stored ?? 100
    }

    const applyPatch = (
      credentials: Record<string, unknown>,
      currentCredentials: Record<string, unknown>,
      platform: Account['platform'] | undefined = input.props.account?.platform,
      type: Account['type'] | undefined = input.props.account?.type
    ) => {
      if (!supportsAccount(platform, type)) {
        return
      }
      const current = normalizeAccountSchedulingThresholdOverride(currentCredentials[credentialKey])
      if (!enabled.value) {
        // 显式发 null 才能让后端删掉这个键；本来就没有则不进 patch。
        if (current !== null) {
          credentials[credentialKey] = null
        }
        return
      }
      const next = clampAccountSchedulingThresholdOverride(value.value)
      if (current !== next) {
        credentials[credentialKey] = next
      }
    }

    return { enabled, value, load, applyPatch }
  }


  function loadAccountSchedulingThresholdOverride(
    platform: Account['platform'] | undefined,
    type: Account['type'] | undefined,
    credentials: Record<string, unknown> | undefined
  ) {
    accountSchedulingThresholdOverride.load(platform, type, credentials)
    anthropicFableSchedulingThresholdOverride.load(platform, type, credentials)
  }


  const applyAccountSchedulingThresholdOverridePatch = (
    credentials: Record<string, unknown>,
    currentCredentials: Record<string, unknown>,
    platform: Account['platform'] | undefined = input.props.account?.platform,
    type: Account['type'] | undefined = input.props.account?.type
  ) => {
    accountSchedulingThresholdOverride.applyPatch(credentials, currentCredentials, platform, type)
    anthropicFableSchedulingThresholdOverride.applyPatch(credentials, currentCredentials, platform, type)
  }


  function loadTempUnschedRules(credentials?: Record<string, unknown>) {
    tempUnschedEnabled.value = credentials?.temp_unschedulable_enabled === true
    const rawRules = credentials?.temp_unschedulable_rules
    if (!Array.isArray(rawRules)) {
      tempUnschedRules.value = []
      return
    }

    tempUnschedRules.value = rawRules.map((rule) => {
      const entry = rule as Record<string, unknown>
      const errorCode = input.toPositiveNumber(entry.error_code)
      return {
        error_code: errorCode,
        keywords: formatTempUnschedKeywords(entry.keywords),
        duration_minutes: input.toPositiveNumber(entry.duration_minutes),
        description: typeof entry.description === 'string' ? entry.description : '',
        scope: resolveTempUnschedScope(entry.scope, errorCode)
      }
    })
  }


  function formatTempUnschedKeywords(value: unknown) {
    if (Array.isArray(value)) {
      return value
        .filter((item): item is string => typeof item === 'string')
        .map((item) => item.trim())
        .filter((item) => item.length > 0)
        .join(', ')
    }
    if (typeof value === 'string') {
      return value
    }
    return ''
  }


  // 未设置 scope 的规则由后端按「401 账号级、其余模型级」处理，这里按同一规则回显，
  // 保证打开再保存不改变规则行为。
  function resolveTempUnschedScope(value: unknown, errorCode: number | null): TempUnschedulableScope {
    const scope = typeof value === 'string' ? value.trim().toLowerCase() : ''
    if (scope === 'account' || scope === 'model') {
      return scope
    }
    return errorCode === 401 ? 'account' : 'model'
  }


  const splitTempUnschedKeywords = (value: string) => {
    return value
      .split(/[,;]/)
      .map((item) => item.trim())
      .filter((item) => item.length > 0)
  }
  return { DEFAULT_POOL_MODE_RETRY_COUNT, MAX_POOL_MODE_RETRY_COUNT, DEFAULT_POOL_MODE_RETRY_STATUS_CODES, poolModeEnabled, poolModeRetryCount, poolModeRetryStatusCodesInput, parsePoolModeRetryStatusCodes, formatPoolModeRetryStatusCodes, customErrorCodesEnabled, selectedErrorCodes, customErrorCodeInput, tempUnschedEnabled, accountSchedulingThresholdOverride, anthropicFableSchedulingThresholdOverride, accountSchedulingThresholdOverrideEnabled, accountSchedulingThresholdOverrideValue, supportsAccountSchedulingThresholdOverride, anthropicFableSchedulingThresholdOverrideEnabled, anthropicFableSchedulingThresholdOverrideValue, supportsAnthropicFableSchedulingThresholdOverride, tempUnschedRules, getTempUnschedRuleKey, tempUnschedScopeOptions, tempUnschedPresets, normalizePoolModeRetryCount, toggleErrorCode, addCustomErrorCode, removeErrorCode, addTempUnschedRule, removeTempUnschedRule, moveTempUnschedRule, buildTempUnschedRules, applyTempUnschedConfig, supportsAccountSchedulingThresholdOverridePlatform, supportsAnthropicFableSchedulingThresholdAccount, normalizeAccountSchedulingThresholdOverride, clampAccountSchedulingThresholdOverride, createSchedulingThresholdOverrideBinding, loadAccountSchedulingThresholdOverride, applyAccountSchedulingThresholdOverridePatch, loadTempUnschedRules, formatTempUnschedKeywords, resolveTempUnschedScope, splitTempUnschedKeywords }
}
