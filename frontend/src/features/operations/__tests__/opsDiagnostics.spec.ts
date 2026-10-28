import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/shared/http', () => ({
  apiClient: { get },
  buildGatewayUrl: vi.fn()
}))

import { getErrorDiagnostics } from '@/features/operations/api/ops'

function errorDetail(id: number) {
  return {
    id,
    phase: 'request',
    created_at: '2025-08-19T00:00:00Z',
    status_code: 502,
    request_status_code: 200,
    upstream_status_code: 403,
    message: 'BPS upstream request failed',
    error_body: '{"error":{"code":"BPS_HTTP_403","message":"Provider rejected request"}}',
    upstream_error_message: 'Provider rejected request',
    upstream_error_detail: '{"status":403,"body":"original provider response"}',
    inbound_endpoint: '/v1/responses',
    upstream_endpoint: '/backend-api/codex/responses',
    model: 'client-model',
    requested_model: 'requested-model',
    upstream_model: 'provider-model',
    upstream_errors: JSON.stringify([
      { kind: 'http', stage: 'upstream', reason: 'bps_rejected', detail: '{"code":"BPS_HTTP_403","parameters":{"attempt":2,"retryable":false}}' },
      { kind: 'scheduling_trace', detail: '{"events":[{"stage":"queue","reason":"capacity","detail":"waited for account","parameters":{"limit":3}}],"dropped_events":0}' },
      { kind: 'scheduling_summary', stage: 'scheduling', reason: 'exhausted', detail: '{"candidates":3,"selected":0}' }
    ]),
    scheduling_summary: { candidates: 3, selected: 0 },
    historical_extra: { nested: ['preserved', 42] }
  }
}


describe('admin Ops current error full information export', () => {
  beforeEach(() => { get.mockReset() })

  it('exports the selected request and every page of its own upstream attempts with response details', async () => {
    const detail = errorDetail(62068)
    const attempts = [
      { ...errorDetail(62069), phase: 'upstream', upstream_error_detail: 'first full provider response' },
      { ...errorDetail(62070), phase: 'upstream', upstream_error_detail: 'second full provider response' }
    ]
    get.mockResolvedValueOnce({ data: detail })
      .mockResolvedValueOnce({ data: { items: [attempts[0]], total: 2, page: 1, pages: 2 } })
      .mockResolvedValueOnce({ data: { items: [attempts[1]], total: 2, page: 2, pages: 2 } })

    const exported = await getErrorDiagnostics(62068)

    expect(exported).toEqual({ ...detail, correlated_upstream_errors: attempts })
    expect(exported.upstream_errors).toBe(detail.upstream_errors)
    expect(get.mock.calls).toEqual([
      ['/admin/ops/request-errors/62068', { signal: undefined }],
      ['/admin/ops/request-errors/62068/upstream-errors', { signal: undefined, params: { page: 1, page_size: 100, view: 'all', include_detail: '1' } }],
      ['/admin/ops/request-errors/62068/upstream-errors', { signal: undefined, params: { page: 2, page_size: 100, view: 'all', include_detail: '1' } }]
    ])
  })

  it('exports the selected upstream error without querying other records', async () => {
    const detail = { ...errorDetail(62068), phase: 'upstream' }
    get.mockResolvedValueOnce({ data: detail })

    const exported = await getErrorDiagnostics(62068, 'upstream')

    expect(exported).toEqual({ ...detail, correlated_upstream_errors: [] })
    expect(exported.upstream_errors).toBe(detail.upstream_errors)
    expect(get).toHaveBeenCalledOnce()
    expect(get).toHaveBeenCalledWith('/admin/ops/upstream-errors/62068', { signal: undefined })
  })

  it('preserves fields omitted by server permissions without reconstructing them', async () => {
    const detail = { id: 1, phase: 'request', status_code: 502 }
    const attempt = { id: 2, phase: 'upstream', status_code: 403 }
    get.mockResolvedValueOnce({ data: detail })
      .mockResolvedValueOnce({ data: { items: [attempt], total: 1, page: 1, pages: 1 } })
    const exported = await getErrorDiagnostics(1)
    expect(exported).toEqual({ ...detail, correlated_upstream_errors: [attempt] })
    expect(exported).not.toHaveProperty('error_body')
    expect(exported).not.toHaveProperty('account_id')
    expect(exported.correlated_upstream_errors[0]).not.toHaveProperty('error_body')
    expect(exported.correlated_upstream_errors[0]).not.toHaveProperty('account_id')
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('rejects the export when the selected detail cannot be loaded', async () => {
    const failure = new Error('detail unavailable')
    get.mockRejectedValueOnce(failure)
    await expect(getErrorDiagnostics(1)).rejects.toBe(failure)
    expect(get).toHaveBeenCalledOnce()
  })

  it('rejects incomplete exports when a later upstream attempt page cannot be loaded', async () => {
    const failure = new Error('attempt page unavailable')
    get.mockResolvedValueOnce({ data: errorDetail(1) })
      .mockResolvedValueOnce({ data: { items: [errorDetail(2)], total: 2, page: 1, pages: 2 } })
      .mockRejectedValueOnce(failure)
    await expect(getErrorDiagnostics(1)).rejects.toBe(failure)
    expect(get).toHaveBeenCalledTimes(3)
  })

  it.each([
    {
      reason: 'a detail lookup was omitted',
      pages: [{ items: [errorDetail(2)], total: 2, page: 1, pages: 1 }],
      message: 'Incomplete upstream error details'
    },
    {
      reason: 'an attempt is repeated across pages',
      pages: [
        { items: [errorDetail(2)], total: 2, page: 1, pages: 2 },
        { items: [errorDetail(2)], total: 2, page: 2, pages: 2 }
      ],
      message: 'Duplicate upstream error during export'
    },
    {
      reason: 'the total changes even when the final item count matches',
      pages: [
        { items: [errorDetail(2)], total: 3, page: 1, pages: 2 },
        { items: [errorDetail(3)], total: 2, page: 2, pages: 2 }
      ],
      message: 'Upstream error count changed during export'
    }
  ])('rejects the export when $reason', async ({ pages, message }) => {
    get.mockResolvedValueOnce({ data: errorDetail(1) })
    for (const page of pages) get.mockResolvedValueOnce({ data: page })
    await expect(getErrorDiagnostics(1)).rejects.toThrow(message)
    expect(get).toHaveBeenCalledTimes(1 + pages.length)
  })
})

describe('diagnostic export cancellation', () => {
  beforeEach(() => get.mockReset())
  it('passes the same signal to the selected detail and every correlated page', async () => {
    const signal = new AbortController().signal
    get.mockResolvedValueOnce({ data: errorDetail(1) })
      .mockResolvedValueOnce({ data: { items: [errorDetail(2)], total: 2, page: 1, pages: 2 } })
      .mockResolvedValueOnce({ data: { items: [errorDetail(3)], total: 2, page: 2, pages: 2 } })
    await getErrorDiagnostics(1, 'request', { signal })
    expect(get.mock.calls.map(call => call[1].signal)).toEqual([signal, signal, signal])
  })
  it.each(['detail', 'attempts'])('does not start another page after abort during %s', async stage => {
    const controller = new AbortController()
    let finish!: (value: unknown) => void
    if (stage === 'attempts') get.mockResolvedValueOnce({ data: errorDetail(1) })
    get.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const pending = getErrorDiagnostics(1, 'request', { signal: controller.signal })
    await Promise.resolve(); await Promise.resolve()
    controller.abort()
    finish(stage === 'detail' ? { data: errorDetail(1) } : { data: { items: [errorDetail(2)], total: 2, page: 1, pages: 2 } })
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    expect(get).toHaveBeenCalledTimes(stage === 'detail' ? 1 : 2)
  })
})
