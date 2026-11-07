/** Existing account test wire payload, including all active Grok media inputs. */
export interface ResourceAccountTestRequest {
  model_id: string
  prompt: string
  mode?: string
  image_data_url?: string
  audio_data_url?: string
}


import { apiClient, postEventStream } from '@/shared/http'

export interface ExcelAuthorizationStatus { authorized: boolean; refresh_required?: boolean; source?: string }
export const excelAuthorizationAPI = {
  async generateUrl(proxyID?: number | null): Promise<{ auth_url: string; session_id: string }> {
    const { data } = await apiClient.post<{ auth_url: string; session_id: string }>('/admin/openai/generate-auth-url', { oauth_profile: 'excel', proxy_id: proxyID })
    return data
  },
  async get(accountID: number): Promise<ExcelAuthorizationStatus> {
    const { data } = await apiClient.get<ExcelAuthorizationStatus>('/admin/openai/accounts/' + accountID + '/excel-authorization')
    return data
  },
  async save(accountID: number, credentials: Record<string, unknown>): Promise<unknown> {
    const { data } = await apiClient.post<unknown>('/admin/openai/accounts/' + accountID + '/excel-authorization', credentials)
    return data
  },
  async revoke(accountID: number): Promise<unknown> {
    const { data } = await apiClient.delete<unknown>('/admin/openai/accounts/' + accountID + '/excel-authorization')
    return data
  }
}

export function openAccountTest(accountID: number, request: ResourceAccountTestRequest, signal: AbortSignal): Promise<Response> {
  return postEventStream('/admin/accounts/' + accountID + '/test', request, signal)
}
