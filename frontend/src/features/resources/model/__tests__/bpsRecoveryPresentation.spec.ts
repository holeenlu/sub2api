import { describe, expect, it } from 'vitest'
import { isBPSSystemDisabled, readBPSRecoveryPresentation, readBPSRecoveryDisabledAt } from '../bpsRecoveryPresentation'

const next = '2099-01-01T00:00:00Z'
describe('public BPS recovery projection', () => {
  it.each([null, undefined])('does not invent absent status %j', value => {
    expect(readBPSRecoveryPresentation(value)).toBeNull()
    expect(isBPSSystemDisabled(readBPSRecoveryPresentation(value))).toBe(false)
  })
  it.each(['reauthorization_required', 'paused', 'pending', 'failed', 'probing'])('uses known public state %s for an off account', state => {
    const view = readBPSRecoveryPresentation({ state, interval_minutes: 30 })
    expect(view?.state).toBe(state)
    expect(isBPSSystemDisabled(view)).toBe(true)
  })
  it.each([{}, [], false, 'pending', 42, { state: 'future' }])('preserves unknown input %j without inferring a disabled account', raw => {
    const view = readBPSRecoveryPresentation(raw)
    expect(view?.state).toBe('unknown')
    expect(view?.nextProbeAt).toBeNull()
    expect(isBPSSystemDisabled(view)).toBe(false)
  })
  it('keeps history distinct from active disablement', () => {
    expect(isBPSSystemDisabled(readBPSRecoveryPresentation({ state: 'history' }))).toBe(false)
  })
  it.each(['reauthorization_required', 'paused', 'history', 'unknown', 'future'])('does not invent a probe for state %s even if malformed input includes a date', state => {
    expect(readBPSRecoveryPresentation({ state, next_probe_at: next })?.nextProbeAt).toBeNull()
  })
  it.each(['pending', 'failed', 'probing'])('keeps the server date for %s without deciding recovery', state => {
    expect(readBPSRecoveryPresentation({ state, next_probe_at: next })).toMatchObject({ state, nextProbeAt: new Date(next) })
    expect(readBPSRecoveryPresentation({ state, next_probe_at: '2000-01-01T00:00:00Z' })?.state).toBe(state)
  })
  it.each([null, '', 0, '0', 'invalid', '2026-10-27', '2026-02-29T00:00:00Z', '2026-02-30T00:00:00Z', '2026-10-27T24:00:00Z'])('does not coerce an invalid deadline %j', next_probe_at => {
    expect(readBPSRecoveryPresentation({ state: 'pending', next_probe_at })?.nextProbeAt).toBeNull()
  })
  it('does not read a private failure record or expose identities/policy', () => {
    const input = Object.freeze({
      state: 'reauthorization_required', reason: 'grant_missing', interval_minutes: 30,
      grant_fingerprint: 'private-fingerprint', policy: { secret: 'private-policy' },
      openai_excel_bps_failure: { state: 'pending', next_probe_at: next },
      last_result: { status: 'failed', reason: 'timeout', completed_at: '2026-10-27T00:00:00Z', raw_body: 'private-response' }
    })
    const view = readBPSRecoveryPresentation(input)
    expect(view?.state).toBe('reauthorization_required')
    expect(view?.nextProbeAt).toBeNull()
    expect(view?.lastResult).toMatchObject({ status: 'failed', reason: 'timeout', completedAt: new Date('2026-10-27T00:00:00Z') })
    expect(JSON.stringify(view)).not.toContain('private')
  })
  it('does not infer reauthorization from a reason or a legacy private boolean', () => {
    expect(readBPSRecoveryPresentation({ state: 'pending', reason: 'invalid_grant', reauth_required: true, next_probe_at: next }))
      .toMatchObject({ state: 'pending', nextProbeAt: new Date(next) })
  })
})


describe('BPS conditional enable timestamp', () => {
  it.each(['2026-10-27T01:02:03.000004Z', '2026-10-27T09:02:03.000004+08:00'])(
    'preserves the exact server token %s without formatting', disabled_at => {
      expect(readBPSRecoveryDisabledAt({ state: 'pending', disabled_at })).toBe(disabled_at)
    })
  it.each([null, undefined, {}, [], { disabled_at: 0 }, { disabled_at: '' },
    { disabled_at: '2026-02-30T00:00:00Z' }, { disabled_at: '2026-10-27T24:00:00Z' },
    { extra: { openai_excel_bps_403_disabled_at: '2026-10-27T00:00:00Z' } }
  ])('never derives an enable condition from invalid or private input %j', value => {
    expect(readBPSRecoveryDisabledAt(value)).toBeUndefined()
  })
})


describe('BPS probe cycle diagnostics', () => {
  it.each([true, false, undefined, null, 'true', 1])('uses only an explicit boolean for cycle flag %j', flag => {
    const view = readBPSRecoveryPresentation({ state: 'pending', last_result_current_cycle: flag,
      disabled_at: '2026-10-28T00:00:00Z', last_result: { status: 'failed', disabled_at: '2026-10-28T00:00:00Z' } })
    expect(view?.lastResultCurrentCycle).toBe(flag === true)
    expect(view?.state).toBe('pending')
  })

  it('retains safe diagnostics without changing recovery state or reading private fields', () => {
    const view = readBPSRecoveryPresentation({ state: 'pending', last_result_current_cycle: false,
      last_result: { status: 'failed', stage: 'request', reason: 'http_rejected', http_status: 403, duration_ms: 0,
        attempted_at: '2026-10-27T08:00:00.000004+08:00', completed_at: '2026-10-27T00:00:01Z',
        upstream_request_id: 'req-safe_123.4:5', error_code: 'account_deactivated', error_type: 'permission_error',
        raw_body: 'private-response', credential: 'private-token' } })
    expect(view).toMatchObject({ state: 'pending', lastResultCurrentCycle: false, lastResult: {
      attemptedAt: new Date('2026-10-27T00:00:00Z'), completedAt: new Date('2026-10-27T00:00:01Z'),
      upstreamRequestId: 'req-safe_123.4:5', errorCode: 'account_deactivated', errorType: 'permission_error',
      stage: 'request', httpStatus: 403, durationMs: 0,
    } })
    expect(JSON.stringify(view)).not.toContain('private')
  })

  it.each([null, {}, 42, '', '<script>secret</script>', 'line\nbreak', 'x'.repeat(257)])('rejects malformed diagnostic identifiers %j', value => {
    expect(readBPSRecoveryPresentation({ state: 'failed', last_result: {
      upstream_request_id: value, error_code: value, error_type: value,
      attempted_at: 'not-a-time', completed_at: 'not-a-time', stage: {}, http_status: '403', duration_ms: -1,
    } })?.lastResult).toMatchObject({ upstreamRequestId: null, errorCode: null, errorType: null,
      attemptedAt: null, completedAt: null, stage: null, httpStatus: null, durationMs: null })
  })
})
