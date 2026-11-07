export interface AccountTelemetryDeviceInstallation {
  format: string
  version: number
  exported_at: string
  device_id: string
  machine_id: string
  cli_version: string
  platform: string
  arch: string
  locale: string
  environment: Record<string, unknown>
}

export interface AccountTelemetryState {
  status: 'waiting_import' | 'ready'
  revision: string
  owner: string
  imported_at?: string
  session_id?: string
  device_installation?: AccountTelemetryDeviceInstallation
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

export function readAccountTelemetry(extra: Record<string, unknown> | null | undefined): AccountTelemetryState | null {
  const value = extra?.account_telemetry
  if (!isRecord(value)) return null

  const state: AccountTelemetryState = {
    status: 'waiting_import',
    revision: typeof value.revision === 'string' ? value.revision : '',
    owner: typeof value.owner === 'string' ? value.owner : ''
  }
  const device = value.device_installation
  // Only device bundles survive the removal of server-generated profiles.
  // Import validation belongs to the backend; this guards the saved JSON shape.
  if (!isRecord(device) || device.format !== 'sub2api-account-telemetry' || device.version !== 2 ||
    typeof device.exported_at !== 'string' || typeof device.device_id !== 'string' ||
    typeof device.machine_id !== 'string' || typeof device.cli_version !== 'string' ||
    typeof device.platform !== 'string' || typeof device.arch !== 'string' ||
    typeof device.locale !== 'string' || !isRecord(device.environment)) return state

  state.device_installation = {
    format: device.format,
    version: device.version,
    exported_at: device.exported_at,
    device_id: device.device_id,
    machine_id: device.machine_id,
    cli_version: device.cli_version,
    platform: device.platform,
    arch: device.arch,
    locale: device.locale,
    environment: device.environment
  }
  state.status = 'ready'
  if (typeof value.imported_at === 'string') state.imported_at = value.imported_at
  if (typeof value.session_id === 'string') state.session_id = value.session_id
  return state
}
