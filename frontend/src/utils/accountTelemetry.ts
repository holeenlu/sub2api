export interface AccountTelemetryState {
 status: 'pending' | 'ready' | 'error'
 revision: string
 error_code?: string
 generated_at?: string
 retry_at?: string
 profile?: { source: 'official-cc-init'; subscription_type?: string; payload_json: string; headers: Record<string,string[]> }
}
export function readAccountTelemetry(extra: Record<string,unknown> | null | undefined): AccountTelemetryState | null {
 const value=extra?.account_telemetry as AccountTelemetryState | undefined
 if (!value || !['pending','ready','error'].includes(value.status) || typeof value.revision !== 'string') return null
 return value
}
