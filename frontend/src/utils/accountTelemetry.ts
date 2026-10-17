import type { ClientTelemetryLog } from '@/api/admin/ops'

export const ACCOUNT_TELEMETRY_KEY = 'client_telemetry'

export interface AccountTelemetry {
  client: 'claude' | 'codex'
  payload_json: string
  headers: Record<string, string[]>
}

export function copyAccountTelemetry(value: unknown): AccountTelemetry | null {
  if (!value || typeof value !== 'object') return null
  const source = value as Partial<AccountTelemetry>
  if ((source.client !== 'claude' && source.client !== 'codex') || typeof source.payload_json !== 'string') return null
  const headers: Record<string, string[]> = {}
  if (source.headers && typeof source.headers === 'object') {
    for (const [name, values] of Object.entries(source.headers)) {
      if (!Array.isArray(values) || !values.every(value => typeof value === 'string')) return null
      headers[name] = [...values]
    }
  }
  return { client: source.client, payload_json: source.payload_json, headers }
}

export function telemetryFromSample(sample: ClientTelemetryLog): AccountTelemetry | null {
  // Only independent data is copied; management IDs and attribution stay in the log.
  return copyAccountTelemetry(sample)
}

export function readAccountTelemetry(extra: Record<string, unknown> | null | undefined): AccountTelemetry | null {
  return copyAccountTelemetry(extra?.[ACCOUNT_TELEMETRY_KEY])
}

export function withAccountTelemetry(extra: Record<string, unknown> | undefined, value: AccountTelemetry | null): Record<string, unknown> | undefined {
  const result = { ...extra }
  if (value) result[ACCOUNT_TELEMETRY_KEY] = copyAccountTelemetry(value)
  else delete result[ACCOUNT_TELEMETRY_KEY]
  return Object.keys(result).length ? result : undefined
}
