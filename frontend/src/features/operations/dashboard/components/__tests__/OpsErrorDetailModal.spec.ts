import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import OpsErrorDetailModal from '../OpsErrorDetailModal.vue'

const mocks = vi.hoisted(() => ({
  getRequestErrorDetail: vi.fn(), getUpstreamErrorDetail: vi.fn(),
  listRequestErrorUpstreamErrors: vi.fn(),
  getErrorDiagnostics: vi.fn(), saveAs: vi.fn(), showError: vi.fn()
}))

vi.mock('@/features/operations/api/ops', () => ({
  opsAPI: {
    getRequestErrorDetail: mocks.getRequestErrorDetail,
    getErrorDiagnostics: mocks.getErrorDiagnostics,
    getUpstreamErrorDetail: mocks.getUpstreamErrorDetail,
    listRequestErrorUpstreamErrors: mocks.listRequestErrorUpstreamErrors
  }
}))

const roleState = vi.hoisted(() => ({ isSuperAdmin: true }))
const scopeState = vi.hoisted(() => ({ services: undefined as import('@/features/operations/services').OperationsServices | undefined }))

vi.mock('file-saver', () => ({ saveAs: mocks.saveAs }))


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
    mocks.getUpstreamErrorDetail.mockReset()
    mocks.getErrorDiagnostics.mockReset()
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

  it('downloads all modal diagnostics and collapsed upstream responses and handles loading failure', async () => {
    const upstreamEvent = { kind: 'http', message: 'BPS ownership rejected', detail: '{"ownership":"missing"}' }
    const scheduling = '{"events":[{"stage":"selection","reason":"selected"}],"dropped_events":0}'
    const detail = {
      id: 1, status_code: 502, error_body: 'authorized-body',
      upstream_error_message: 'BPS ownership rejected',
      upstream_error_detail: 'original-response',
      upstream_errors: JSON.stringify([upstreamEvent, { kind: 'scheduling_trace', detail: scheduling }])
    }
    const attempts = [
      { id: 2, status_code: 500, request_id: 'upstream-rid', message: 'BPS ownership rejected', upstream_error_detail: '{"body":"complete upstream response"}' },
      { id: 3, status_code: 503, error_body: 'fallback response body' }
    ]
    const exported = { ...detail, correlated_upstream_errors: attempts }
    mocks.getRequestErrorDetail.mockResolvedValue(detail)
    mocks.listRequestErrorUpstreamErrors.mockResolvedValue({ items: attempts })
    mocks.getErrorDiagnostics.mockResolvedValue(exported)
    const wrapper = shallowMount(OpsErrorDetailModal, {
      props: { show: true, errorId: 1, errorType: 'request' },
      global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Icon: true } }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('upstream-rid')
    expect(wrapper.text()).not.toContain('complete upstream response')
    await wrapper.get('[data-testid="error-detail-export"]').trigger('click')
    await flushPromises()
    expect(mocks.getErrorDiagnostics).toHaveBeenCalledWith(1, 'request', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(mocks.saveAs).toHaveBeenCalledOnce()
    const [blob, filename] = mocks.saveAs.mock.calls[0]
    expect(filename).toBe('request-diagnostics-1.json')
    const text = await new Promise<string>((resolve) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result)); reader.readAsText(blob) })
    expect(JSON.parse(text)).toEqual({
      ...exported,
      display: {
        message: 'BPS ownership rejected',
        root_cause: 'BPS ownership rejected',
        diagnostic_payloads: [
          { key: 'client', value: 'authorized-body' },
          { key: 'upstream_message', value: 'BPS ownership rejected' },
          { key: 'upstream_detail', value: 'original-response' },
          { key: 'upstream_events', value: JSON.stringify([upstreamEvent]) },
          { key: 'scheduling_trace', value: scheduling }
        ],
        upstream_errors: [
          { id: 2, response_preview: '{"body":"complete upstream response"}' },
          { id: 3, response_preview: 'fallback response body' }
        ]
      }
    })
    mocks.getErrorDiagnostics.mockRejectedValueOnce(new Error('detail unavailable'))
    await wrapper.get('[data-testid="error-detail-export"]').trigger('click')
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('admin.ops.errorDetail.exportFailed')
    expect(mocks.saveAs).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-testid="error-detail-export"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('exports upstream detail using the upstream error type', async () => {
    mocks.getUpstreamErrorDetail.mockResolvedValue({ id: 7, phase: 'upstream', status_code: 403 })
    mocks.getErrorDiagnostics.mockResolvedValue({ id: 7, phase: 'upstream', status_code: 403, correlated_upstream_errors: [] })
    const wrapper = shallowMount(OpsErrorDetailModal, {
      props: { show: true, errorId: 7, errorType: 'upstream' },
      global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Icon: true } }
    })
    await flushPromises()
    await wrapper.get('[data-testid="error-detail-export"]').trigger('click')
    await flushPromises()

    expect(mocks.getErrorDiagnostics).toHaveBeenCalledWith(7, 'upstream', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(mocks.saveAs).toHaveBeenCalledWith(expect.any(Blob), 'request-diagnostics-7.json')
    wrapper.unmount()
  })


})

it.each([false, true])('gates upstream account details for super-admin=%s', async (isSuperAdmin) => {
  roleState.isSuperAdmin = isSuperAdmin
  mocks.getRequestErrorDetail.mockResolvedValue({ id: 1, phase: 'upstream', error_owner: 'provider', account_id: 991, account_name: 'private-account' })
  const wrapper = shallowMount(OpsErrorDetailModal, {
    props: { show: true, errorId: 1, errorType: 'request' },
    global: { stubs: { BaseDialog: { template: '<div><slot /></div>' }, Icon: true } }
  })
  try {
    await flushPromises()
    expect(wrapper.text().includes('private-account')).toBe(isSuperAdmin)
    expect(wrapper.text().includes('admin.ops.errorDetail.account')).toBe(isSuperAdmin)
  } finally {
    wrapper.unmount()
    roleState.isSuperAdmin = true
  }
})

vi.mock('@/features/operations/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/operations/services')>('@/features/operations/services')
  const { createOperationsTestServices } = await import('@/features/operations/__tests__/services')
  const { ref } = await import('vue')
  const services = createOperationsTestServices({
    notifyError: mocks.showError,
    permissions: ref({ get isSuperAdmin() { return roleState.isSuperAdmin }, canManageOps: true, canReadPromptContent: true }),
  })
  scopeState.services = services
  return { ...actual, useOperationsServices: () => services }
})

it.each(['selection', 'close', 'scope', 'permission', 'unmount'] as const)('discards a diagnostic export after %s invalidates ownership', async change => {
  let finish!: (value: unknown) => void
  mocks.saveAs.mockClear(); mocks.showError.mockClear()
  mocks.getRequestErrorDetail.mockResolvedValue({ id: 1, phase: 'request', status_code: 502 })
  mocks.getUpstreamErrorDetail.mockResolvedValue({ id: 7, phase: 'upstream', status_code: 403 })
  mocks.getErrorDiagnostics.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
  const wrapper = shallowMount(OpsErrorDetailModal, {
    props: { show: true, errorId: 1, errorType: 'request' },
    global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Icon: true } }
  })
  await flushPromises()
  await wrapper.get('[data-testid="error-detail-export"]').trigger('click')
  const signal = mocks.getErrorDiagnostics.mock.calls.at(-1)![2].signal as AbortSignal
  if (change === 'selection') await wrapper.setProps({ errorId: 7, errorType: 'upstream' })
  if (change === 'close') await wrapper.setProps({ show: false })
  if (change === 'scope') (scopeState.services!.scopeKey as import('vue').Ref<string | null>).value = 'session-next'
  if (change === 'permission') (scopeState.services!.permissions as import('vue').Ref<import('@/features/operations/services').OperationsPermissions>).value.canManageOps = false
  if (change === 'unmount') wrapper.unmount()
  finish({ id: 1, phase: 'request', correlated_upstream_errors: [] })
  await flushPromises()
  expect(signal.aborted).toBe(true)
  expect(mocks.saveAs).not.toHaveBeenCalled()
  expect(mocks.showError).not.toHaveBeenCalled()
  if (change !== 'unmount') {
    const exportButton = wrapper.find('[data-testid="error-detail-export"]')
    if (exportButton.exists()) expect(exportButton.attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  }
  const scope = scopeState.services!.scopeKey as import('vue').Ref<string | null>
  const permissions = scopeState.services!.permissions as import('vue').Ref<import('@/features/operations/services').OperationsPermissions>
  scope.value = 'session-a'
  permissions.value.canManageOps = true
})
