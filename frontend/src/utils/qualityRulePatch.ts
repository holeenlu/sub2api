import type { QualityBPSPolicy } from '@/types'
import { DEFAULT_EXCEL_BPS_MODELS } from '@/constants/account'
import { DEFAULT_BPS_RECOVERY_INTERVAL_MINUTES, isValidBPSRecoveryInterval } from './excelBPSRecovery'

// 新建「降智开 BPS」时的默认勾选；target_group_id = -1 表示还没选 403 后的目标分组。
export function defaultQualityBPS(): QualityBPSPolicy {
  return { failure_threshold: 2, usage_percent: 0, require_all: false, all_models: false, models: [...DEFAULT_EXCEL_BPS_MODELS],
    omit_unsupported_tools: false, ignore_images: false, ignore_encrypted_content: true, auto_disable_on_403: true, auto_recover_on_403: false, recovery_interval_minutes: DEFAULT_BPS_RECOVERY_INTERVAL_MINUTES, auto_move_on_403: false,
    target_group_id: -1, session_proxy: false, proxy_source: '', cache_creation_as_input: true, pass_threshold: 2, hold_on_usage: true }
}

// 已保存的 BPS 设置盖在默认值上；「全部模型」时后端不存模型列表，切回按模型时给默认候选。
// 早先保存、没有满血关闭次数的规则后端按 1 次处理，表单也显示 1。
export function qualityBPSForm(saved?: Partial<QualityBPSPolicy> | null): QualityBPSPolicy {
  const value = saved || {}
  return { ...defaultQualityBPS(), ...value,
    models: value.models?.length ? [...value.models] : [...DEFAULT_EXCEL_BPS_MODELS],
    target_group_id: value.auto_move_on_403 ? value.target_group_id ?? -1 : -1,
    proxy_source: '',
    pass_threshold: saved ? saved.pass_threshold || 1 : defaultQualityBPS().pass_threshold }
}

const validPassCount = (count: number) => Number.isInteger(count) && count >= 1 && count <= 100

// 与后端 validateQualityBPSPolicy 对应的前置检查，返回 i18n 键；空串表示通过。
export function qualityBPSError(bps: QualityBPSPolicy): string {
  const count = bps.failure_threshold, usage = bps.usage_percent
  if (!Number.isInteger(count) || count < 0 || count > 100) return 'qualityOps.bpsCountInvalid'
  if (!Number.isFinite(usage) || usage < 0 || usage > 100) return 'qualityOps.bpsUsageInvalid'
  if (!count && !usage) return 'qualityOps.bpsTriggerRequired'
  if (!validPassCount(bps.pass_threshold)) return 'qualityOps.bpsPassCountInvalid'
  if (!bps.all_models && !bps.models.some(model => model.trim())) return 'qualityOps.bpsModelsRequired'
  if (bps.recovery_interval_minutes !== undefined && !isValidBPSRecoveryInterval(bps.recovery_interval_minutes)) return 'admin.accounts.openai.excelBPS403RecoveryIntervalInvalid'
  if (bps.auto_move_on_403 && !(bps.target_group_id >= 0)) return 'qualityOps.bpsTargetGroupRequired'
  return ''
}

// 表单里「未选目标分组」用 -1 占位；提交前按后端口径归一，避免未勾选的子项残留旧值。
export function qualityBPSPayload(bps: QualityBPSPolicy): QualityBPSPolicy {
  return {
    ...bps,
    recovery_interval_minutes: bps.recovery_interval_minutes ?? DEFAULT_BPS_RECOVERY_INTERVAL_MINUTES,
    models: bps.all_models ? [] : [...bps.models],
    target_group_id: bps.auto_move_on_403 ? bps.target_group_id : 0,
    session_proxy: false, proxy_source: '',
  }
}
