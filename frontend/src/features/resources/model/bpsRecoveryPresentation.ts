import { parseRfc3339Timestamp } from '@/shared/lib/parseTimestamp'

export type BPSRecoveryState = 'history' | 'reauthorization_required' | 'paused' | 'pending' | 'failed' | 'probing' | 'unknown'

export interface BPSRecoveryPresentation {
  state: BPSRecoveryState
  reason: string | null
  nextProbeAt: Date | null
  lastResultCurrentCycle: boolean
  lastResult: {
    status: 'restored' | 'failed' | 'unknown'
    reason: string | null
    completedAt: Date | null
    attemptedAt: Date | null
    stage: string | null
    httpStatus: number | null
    durationMs: number | null
    upstreamRequestId: string | null
    errorCode: string | null
    errorType: string | null
  } | null
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}
function classification(value: unknown): string | null {
  return typeof value === 'string' && /^[a-z][a-z0-9_]{0,79}$/.test(value) ? value : null
}
function diagnosticIdentifier(value: unknown): string | null {
  return typeof value === 'string' && /^[a-zA-Z0-9_.:-]{1,256}$/.test(value) ? value : null
}
function nonnegativeNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
}
function recoveryState(value: unknown): BPSRecoveryState {
  switch (value) {
    case 'history': case 'reauthorization_required': case 'paused': case 'pending': case 'failed': case 'probing': return value
    default: return 'unknown'
  }
}

/** Consume the safe API projection, never account.extra's private recovery/CAS record. */
export function readBPSRecoveryPresentation(value: unknown): BPSRecoveryPresentation | null {
  if (value === null || value === undefined) return null
  const raw = record(value) ? value : {}
  const state = recoveryState(raw.state)
  const canHaveProbe = state === 'pending' || state === 'failed' || state === 'probing'
  const result = record(raw.last_result) ? raw.last_result : null
  return {
    state,
    reason: classification(raw.reason),
    nextProbeAt: canHaveProbe ? parseRfc3339Timestamp(raw.next_probe_at) : null,
    lastResultCurrentCycle: raw.last_result_current_cycle === true,
    lastResult: result ? {
      status: result.status === 'restored' || result.status === 'failed' ? result.status : 'unknown',
      reason: classification(result.reason),
      completedAt: parseRfc3339Timestamp(result.completed_at),
      attemptedAt: parseRfc3339Timestamp(result.attempted_at),
      stage: classification(result.stage),
      httpStatus: typeof result.http_status === 'number' && Number.isInteger(result.http_status) && result.http_status >= 100 && result.http_status <= 599 ? result.http_status : null,
      durationMs: nonnegativeNumber(result.duration_ms),
      upstreamRequestId: diagnosticIdentifier(result.upstream_request_id),
      errorCode: diagnosticIdentifier(result.error_code),
      errorType: diagnosticIdentifier(result.error_type)
    } : null
  }
}

export function isBPSSystemDisabled(status: BPSRecoveryPresentation | null): boolean {
  return status !== null && status.state !== 'history' && status.state !== 'unknown'
}

/** Preserve the server's exact conditional-write token, including fractional seconds and offset. */
export function readBPSRecoveryDisabledAt(value: unknown): string | undefined {
  if (!record(value) || typeof value.disabled_at !== 'string') return undefined
  return parseRfc3339Timestamp(value.disabled_at) ? value.disabled_at : undefined
}
