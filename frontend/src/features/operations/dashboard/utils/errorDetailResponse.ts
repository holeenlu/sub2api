import type { OpsErrorDetail } from '@/features/operations/api/ops'

const GENERIC_UPSTREAM_MESSAGES = new Set([
  'upstream request failed',
  'upstream request failed after retries',
  'upstream gateway error',
  'upstream service temporarily unavailable'
])

type ParsedGatewayError = {
  type: string
  message: string
}

function parseGatewayErrorBody(raw: string): ParsedGatewayError | null {
  const text = String(raw || '').trim()
  if (!text) return null

  try {
    const parsed: unknown = JSON.parse(text)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) || !('error' in parsed)) return null
    const err = parsed.error
    if (!err || typeof err !== 'object' || Array.isArray(err)) return null

    const type = 'type' in err && typeof err.type === 'string' ? err.type.trim() : ''
    const message = 'message' in err && typeof err.message === 'string' ? err.message.trim() : ''
    if (!type && !message) return null

    return { type, message }
  } catch {
    return null
  }
}

function isGenericGatewayUpstreamError(raw: string): boolean {
  const parsed = parseGatewayErrorBody(raw)
  if (!parsed) return false
  if (parsed.type !== 'upstream_error') return false
  return GENERIC_UPSTREAM_MESSAGES.has(parsed.message.toLowerCase())
}

function isSchedulingTrace(event: unknown): event is { kind: 'scheduling_trace'; detail?: unknown } {
  return typeof event === 'object' && event !== null && !Array.isArray(event) &&
    'kind' in event && event.kind === 'scheduling_trace'
}

// Local scheduling decisions share Ops storage, but are not provider responses.
export function splitSchedulingDiagnostics(raw: string | null | undefined): { upstream: string; scheduling: string } {
  const text = String(raw || '').trim()
  try {
    const parsed: unknown = JSON.parse(text)
    if (!Array.isArray(parsed)) return { upstream: text, scheduling: '' }
    const events: unknown[] = parsed
    const trace = events.find(isSchedulingTrace)
    if (!trace) return { upstream: text, scheduling: '' }
    const upstream = events.filter(event => !isSchedulingTrace(event))
    return {
      upstream: upstream.length ? JSON.stringify(upstream) : '',
      scheduling: typeof trace?.detail === 'string' ? trace.detail : ''
    }
  } catch {
    return { upstream: text, scheduling: '' }
  }
}

export function resolveUpstreamPayload(
  detail: Pick<OpsErrorDetail, 'upstream_error_detail' | 'upstream_errors' | 'upstream_error_message'> | null | undefined
): string {
  if (!detail) return ''

  const candidates = [
    detail.upstream_error_detail,
    splitSchedulingDiagnostics(detail.upstream_errors).upstream,
    detail.upstream_error_message
  ]

  for (const candidate of candidates) {
    const payload = String(candidate || '').trim()
    if (!payload) continue

    // Normalize common "empty but present" JSON placeholders.
    if (payload === '[]' || payload === '{}' || payload.toLowerCase() === 'null') {
      continue
    }

    return payload
  }

  return ''
}

export function resolvePrimaryResponseBody(
  detail: OpsErrorDetail | null,
  errorType?: 'request' | 'upstream'
): string {
  if (!detail) return ''

  const upstreamPayload = resolveUpstreamPayload(detail)
  const errorBody = String(detail.error_body || '').trim()

  if (errorType === 'upstream') {
    return upstreamPayload || errorBody
  }

  if (!errorBody) {
    return upstreamPayload
  }

  // For request detail modal, keep client-visible body by default.
  // But if that body is a generic gateway wrapper, show upstream payload first.
  if (upstreamPayload && isGenericGatewayUpstreamError(errorBody)) {
    return upstreamPayload
  }

  return errorBody
}
