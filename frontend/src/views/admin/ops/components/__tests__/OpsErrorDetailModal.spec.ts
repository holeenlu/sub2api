import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import OpsErrorDetailModal from '../OpsErrorDetailModal.vue'

const mocks = vi.hoisted(() => ({
  getRequestErrorDetail: vi.fn(),
  listRequestErrorUpstreamErrors: vi.fn(),
  getErrorDiagnostics: vi.fn(), saveAs: vi.fn(), showError: vi.fn()
}))

vi.mock('@/api/admin/ops', () => ({
  opsAPI: {
    getRequestErrorDetail: mocks.getRequestErrorDetail,
    getErrorDiagnostics: mocks.getErrorDiagnostics,
    getUpstreamErrorDetail: vi.fn(),
    listRequestErrorUpstreamErrors: mocks.listRequestErrorUpstreamErrors
  }
}))

vi.mock('file-saver', () => ({ saveAs: mocks.saveAs }))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: mocks.showError })
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

describe('OpsErrorDetailModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getRequestErrorDetail.mockReset()
    mocks.listRequestErrorUpstreamErrors.mockReset()
    mocks.listRequestErrorUpstreamErrors.mockResolvedValue({ items: [] })
  })

  it('prioritizes upstream root cause and deduplicates diagnostic payloads', async () => {
    mocks.getRequestErrorDetail.mockResolvedValue({
      id: 1,
      created_at: '2026-08-19T00:00:00Z',
      phase: 'request',
      type: 'upstream_error',
      error_owner: 'provider',
      error_source: 'gateway',
      severity: 'P1',
      status_code: 502,
      upstream_status_code: 429,
      platform: 'openai',
      model: 'gpt-5.6',
      resolved: false,
      request_id: 'rid-1',
      message: 'All available accounts exhausted',
      error_body: '{"error":"same"}',
      upstream_error_message: 'provider rate limit exhausted',
      upstream_error_detail: '{"error":"same"}',
      upstream_errors: '[]',
      account_name: 'account',
      group_name: 'group',
      is_business_limited: false
    })

    const wrapper = shallowMount(OpsErrorDetailModal, {
      props: { show: true, errorId: 1, errorType: 'request' },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /></div>' },
          Icon: true
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('provider rate limit exhausted')
    expect(wrapper.text()).not.toContain('All available accounts exhausted')
    expect(wrapper.text()).toContain('admin.ops.errorDetail.upstreamStatus')
    expect(wrapper.text()).toContain('429')
    expect(wrapper.findAll('pre')).toHaveLength(2)
    expect(wrapper.text()).not.toContain('admin.ops.errorDetail.payloads.upstream_detail')
  })
  it('shows the final request status separately from a recovered upstream failure', async () => {
    mocks.getRequestErrorDetail.mockResolvedValue({
      id: 4712, status_code: 503, request_status_code: 200, upstream_status_code: 503,
      message: 'Recovered upstream error 503: overloaded', upstream_error_message: 'overloaded',
      platform: 'openai', model: 'gpt-6.1-sol', request_type: 2, error_body: '', upstream_errors: '[]'
    })
    const wrapper = shallowMount(OpsErrorDetailModal, {
      props: { show: true, errorId: 4712, errorType: 'request' },
      global: { stubs: { BaseDialog: { template: '<div><slot /></div>' }, Icon: true } }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.ops.errorDetail.requestStatus')
    expect(wrapper.text()).toContain('admin.ops.errorLog.recovered')
    expect(wrapper.text()).toContain('200')
    expect(wrapper.text()).toContain('503')
    expect(wrapper.text()).toContain('Recovered upstream error 503: overloaded')
  })

  it('exports only the server diagnostic projection and handles failure', async () => {
    mocks.getRequestErrorDetail.mockResolvedValue({ id: 1, status_code: 502, error_body: 'private-body', upstream_errors: '[]' })
    mocks.getErrorDiagnostics.mockResolvedValue({ version: 1, coverage: 'recorded_events_only', gaps: ['missing_events'] })
    const wrapper = shallowMount(OpsErrorDetailModal, {
      props: { show: true, errorId: 1, errorType: 'request' },
      global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Icon: true } }
    })
    await flushPromises()
    await wrapper.get('[data-testid="error-detail-export"]').trigger('click')
    await flushPromises()
    expect(mocks.getErrorDiagnostics).toHaveBeenCalledWith(1)
    expect(mocks.saveAs).toHaveBeenCalledOnce()
    const [blob, filename] = mocks.saveAs.mock.calls[0]
    expect(filename).toBe('request-diagnostics-1.json')
    const text = await new Promise<string>((resolve) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result)); reader.readAsText(blob) })
    expect(text).toContain('recorded_events_only')
    expect(text).not.toContain('private-body')
    mocks.getErrorDiagnostics.mockRejectedValueOnce(new Error('unavailable'))
    await wrapper.get('[data-testid="error-detail-export"]').trigger('click')
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('admin.ops.errorDetail.exportFailed')
    expect(mocks.saveAs).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

})
