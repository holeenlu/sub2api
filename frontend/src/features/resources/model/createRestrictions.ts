import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TempUnschedulableScope } from '@/features/resources/api/accountObservationTypes';
import { createStableObjectKeyResolver } from '@/shared/lib/stableObjectKey'
import type { Ref, ComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { TempUnschedRuleForm } from './createEditorTypes'




interface CreateRestrictionsInput {
  t: ReturnType<typeof useI18n>["t"]
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showError" | "showInfo">>
}




export function useCreateRestrictionsDraft(input: CreateRestrictionsInput): {
  DEFAULT_POOL_MODE_RETRY_COUNT: 3
  MAX_POOL_MODE_RETRY_COUNT: 10
  DEFAULT_POOL_MODE_RETRY_STATUS_CODES: number[]
  poolModeEnabled: Ref<boolean, boolean>
  poolModeRetryCount: Ref<number, number>
  poolModeRetryStatusCodesInput: Ref<string, string>
  parsePoolModeRetryStatusCodes: (input: string) => number[]
  customErrorCodesEnabled: Ref<boolean, boolean>
  selectedErrorCodes: Ref<number[], number[]>
  customErrorCodeInput: Ref<number | null, number | null>
  tempUnschedEnabled: Ref<boolean, boolean>
  tempUnschedRules: Ref<{ error_code: number | null; keywords: string; duration_minutes: number | null; description: string; scope: TempUnschedulableScope }[], TempUnschedRuleForm[] | { error_code: number | null; keywords: string; duration_minutes: number | null; description: string; scope: TempUnschedulableScope }[]>
  getTempUnschedRuleKey: (item: TempUnschedRuleForm) => string
  tempUnschedScopeOptions: ComputedRef<{ value: TempUnschedulableScope; label: string; description: string }[]>
  tempUnschedPresets: ComputedRef<{ label: string; rule: { error_code: number; keywords: string; duration_minutes: number; description: string; scope: "model" } }[]>
  toggleErrorCode: (code: number) => void
  addCustomErrorCode: () => void
  removeErrorCode: (code: number) => void
  addTempUnschedRule: (preset?: TempUnschedRuleForm) => void
  removeTempUnschedRule: (index: number) => void
  moveTempUnschedRule: (index: number, direction: number) => void
  buildTempUnschedRules: (rules: TempUnschedRuleForm[]) => { error_code: number; keywords: string[]; duration_minutes: number; description: string; scope: TempUnschedulableScope }[]
  applyTempUnschedConfig: (credentials: Record<string, unknown>) => boolean
  splitTempUnschedKeywords: (value: string) => string[]
  normalizePoolModeRetryCount: (value: number) => number
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

  const customErrorCodesEnabled = ref(false)

  const selectedErrorCodes = ref<number[]>([])

  const customErrorCodeInput = ref<number | null>(null)

  const tempUnschedEnabled = ref(false)

  const tempUnschedRules = ref<TempUnschedRuleForm[]>([])

  const getTempUnschedRuleKey = createStableObjectKeyResolver<TempUnschedRuleForm>('create-temp-unsched-rule')

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


  const splitTempUnschedKeywords = (value: string) => {
    return value
      .split(/[,;]/)
      .map((item) => item.trim())
      .filter((item) => item.length > 0)
  }


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
  return { DEFAULT_POOL_MODE_RETRY_COUNT, MAX_POOL_MODE_RETRY_COUNT, DEFAULT_POOL_MODE_RETRY_STATUS_CODES, poolModeEnabled, poolModeRetryCount, poolModeRetryStatusCodesInput, parsePoolModeRetryStatusCodes, customErrorCodesEnabled, selectedErrorCodes, customErrorCodeInput, tempUnschedEnabled, tempUnschedRules, getTempUnschedRuleKey, tempUnschedScopeOptions, tempUnschedPresets, toggleErrorCode, addCustomErrorCode, removeErrorCode, addTempUnschedRule, removeTempUnschedRule, moveTempUnschedRule, buildTempUnschedRules, applyTempUnschedConfig, splitTempUnschedKeywords, normalizePoolModeRetryCount }
}
