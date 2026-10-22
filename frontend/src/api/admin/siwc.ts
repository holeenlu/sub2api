import { apiClient } from '../client'

// Only non-secret, server-verified authorization metadata crosses this boundary.
// A saved direct-use grant does not prove model entitlement or upstream health.
export interface SIWCStatus {
  bound: boolean
  enabled: boolean
  registered?: boolean
  client_id?: string
  identity_verified?: boolean
  direct_access?: boolean
  email?: string
  subject?: string
  expires_at?: number
  refresh_required?: boolean
}

export interface SIWCAuthorization {
  auth_url: string
  session_id: string
}

const path = (id: number) => `/admin/accounts/${id}/siwc`

export async function getSIWCStatus(id: number): Promise<SIWCStatus> {
  return (await apiClient.get<SIWCStatus>(path(id))).data
}

export async function authorizeSIWC(id: number): Promise<SIWCAuthorization> {
  return (await apiClient.post<SIWCAuthorization>(`${path(id)}/authorize`)).data
}

export async function completeSIWC(id: number, sessionId: string, callbackURL: string): Promise<SIWCStatus> {
  return (await apiClient.post<SIWCStatus>(`${path(id)}/callback`, {
    session_id: sessionId, callback_url: callbackURL
  })).data
}

export interface SIWCDisconnectResult {
  bound: boolean
  remote_revocation_confirmed: boolean
  message: string
}

export async function disconnectSIWC(id: number): Promise<SIWCDisconnectResult> {
  return (await apiClient.delete<SIWCDisconnectResult>(path(id))).data
}
