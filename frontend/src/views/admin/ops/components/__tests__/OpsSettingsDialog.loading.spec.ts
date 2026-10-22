import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

import OpsSettingsDialog from '../OpsSettingsDialog.vue'
import quotaCatalog from '../../../../../../../backend/internal/service/scheduling_quota_defaults.json'
const { opsAPI, showError } = vi.hoisted(() => ({
  opsAPI: {
    getAlertRuntimeSettings: vi.fn(), getEmailNotificationConfig: vi.fn(),
    getAdvancedSettings: vi.fn(), getMetricThresholds: vi.fn(),
    updateAlertRuntimeSettings: vi.fn(), updateEmailNotificationConfig: vi.fn(),
    updateAdvancedSettings: vi.fn(), updateMetricThresholds: vi.fn(),
  }, showError: vi.fn(),
}))
vi.mock('@/api/admin/ops', () => ({ opsAPI }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
async function openDialog() {
  const wrapper = shallowMount(OpsSettingsDialog, {
    props: { show: false },
    global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' } } },
  })
  await wrapper.setProps({ show: true })
  return wrapper
}
type View = { saveAllSettings: () => Promise<void> }
describe('ops settings load guard', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    opsAPI.getAlertRuntimeSettings.mockResolvedValue({ evaluation_interval_seconds: 30 })
    opsAPI.getEmailNotificationConfig.mockResolvedValue({ alert: { enabled: false, recipients: [] }, report: { enabled: false, recipients: [] } })
    opsAPI.getAdvancedSettings.mockResolvedValue({
      account_telemetry_sending_enabled: false,
      quota_protection: { ...quotaCatalog.protection },
      aggregation: { aggregation_enabled: false },
      data_retention: { cleanup_enabled: false, error_log_retention_days: 7, minute_metrics_retention_days: 7, hourly_metrics_retention_days: 7 },
      ignore_count_tokens_errors: true, ignore_context_canceled: true,
      ignore_no_available_accounts: false, ignore_insufficient_balance_errors: false,
      auto_refresh_enabled: false, display_alert_events: true, display_openai_token_stats: false,
    })
    opsAPI.getMetricThresholds.mockResolvedValue({ sla_percent_min: 98 })
  })
  afterEach(() => { vi.restoreAllMocks() })
  it.each([true, false])('saves the scheduled telemetry switch as %s', async (enabled) => {
    const wrapper = await openDialog()
    await flushPromises()
    const toggle = wrapper.findComponent('[data-testid="account-telemetry-enabled"]')
    expect(toggle.props('modelValue')).toBe(false)
    toggle.vm.$emit('update:modelValue', enabled)
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(opsAPI.updateAdvancedSettings).toHaveBeenCalledWith(expect.objectContaining({ account_telemetry_sending_enabled: enabled }))
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })
  it('applies the quota preset only on request and validates custom values before saving', async () => {
    const wrapper = await openDialog()
    await flushPromises()
    expect(opsAPI.updateAdvancedSettings).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="quota-protection-preset"]').trigger('click')
    await wrapper.get('[data-testid="quota-critical_band_percent"]').setValue('7.5')
    await wrapper.get('[data-testid="quota-large_cold_input_tokens"]').setValue('120000')
    await wrapper.get('[data-testid="quota-sample_max_age_seconds"]').setValue('0')
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(opsAPI.updateAdvancedSettings).not.toHaveBeenCalled()
    expect(wrapper.get('button.btn-primary').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="quota-sample_max_age_seconds"]').setValue('120')
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(opsAPI.updateAdvancedSettings).toHaveBeenCalledWith(expect.objectContaining({
      openai_account_quota_auto_pause: { default_threshold_5h: 0.85, default_threshold_7d: 0.8 },
      quota_protection: { ...quotaCatalog.protection, anthropic_tier_thresholds_enabled: true, critical_band_percent: 7.5, large_cold_input_tokens: 120000, sample_max_age_seconds: 120 },
    }))
  })
  it('blocks saving while any settings request is still loading', async () => {
    const pending = deferred<object>()
    opsAPI.getMetricThresholds.mockReturnValueOnce(pending.promise)
    const wrapper = await openDialog()
    expect(wrapper.get('button.btn-primary').attributes('disabled')).toBeDefined()
    await (wrapper.vm as unknown as View).saveAllSettings()
    expect(opsAPI.updateMetricThresholds).not.toHaveBeenCalled()
    pending.resolve({ sla_percent_min: 98 })
    await flushPromises()
    expect(wrapper.get('button.btn-primary').attributes('disabled')).toBeUndefined()
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(opsAPI.updateMetricThresholds).toHaveBeenCalledWith(expect.objectContaining({ sla_percent_min: 98, ttft_p99_ms_max: 10000 }))
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })
  it('does not save default thresholds after a failed initial load', async () => {
    opsAPI.getMetricThresholds.mockRejectedValueOnce(new Error('load failed'))
    const wrapper = await openDialog()
    await flushPromises()
    expect(wrapper.get('button.btn-primary').attributes('disabled')).toBeDefined()
    await (wrapper.vm as unknown as View).saveAllSettings()
    expect(opsAPI.updateMetricThresholds).not.toHaveBeenCalled()
    expect(wrapper.emitted('saved')).toBeUndefined()
  })

  it('blocks saving stale settings after a failed reopen and allows a later retry', async () => {
    const wrapper = await openDialog()
    await flushPromises()
    await wrapper.setProps({ show: false })
    opsAPI.getMetricThresholds.mockRejectedValueOnce(new Error('refresh failed'))
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.get('button.btn-primary').attributes('disabled')).toBeDefined()
    await (wrapper.vm as unknown as View).saveAllSettings()
    expect(opsAPI.updateAlertRuntimeSettings).not.toHaveBeenCalled()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.get('button.btn-primary').attributes('disabled')).toBeUndefined()
  })
})
