import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get },
  buildGatewayUrl: vi.fn()
}))

import { getErrorDiagnostics } from '@/api/admin/ops'

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

  it.each(['request', 'upstream'] as const)('exports exactly the selected %s record without related queries', async (type) => {
    const detail = { ...errorDetail(62068), phase: type }
    get.mockResolvedValueOnce({ data: detail })

    const exported = await getErrorDiagnostics(62068, type)

    expect(exported).toBe(detail)
    expect(exported).toEqual(detail)
    expect(exported.upstream_errors).toBe(detail.upstream_errors)
    expect(get).toHaveBeenCalledOnce()
    expect(get).toHaveBeenCalledWith('/admin/ops/' + (type === 'upstream' ? 'upstream-errors' : 'request-errors') + '/62068')
  })

  it('preserves fields omitted by server permissions without reconstructing them', async () => {
    const detail = { id: 1, phase: 'request', status_code: 502 }
    get.mockResolvedValueOnce({ data: detail })
    const exported = await getErrorDiagnostics(1)
    expect(exported).toEqual(detail)
    expect(exported).not.toHaveProperty('error_body')
    expect(exported).not.toHaveProperty('account_id')
    expect(get).toHaveBeenCalledOnce()
  })

  it('rejects the export when the selected detail cannot be loaded', async () => {
    const failure = new Error('detail unavailable')
    get.mockRejectedValueOnce(failure)
    await expect(getErrorDiagnostics(1)).rejects.toBe(failure)
    expect(get).toHaveBeenCalledOnce()
  })
})
