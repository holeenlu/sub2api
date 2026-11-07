import type { AxiosResponse } from 'axios'
import { apiClient } from '@/shared/http'
import type { PendingOAuthExchangeResponse, OAuthTokenResponse } from './auth'
import type { RegisterRequest } from './types'

export interface PendingOAuthAccountResponse extends PendingOAuthExchangeResponse {
  step?: string
  status?: string
  state?: string
  intent?: string
  provider?: string
  email?: string
  resolved_email?: string
  pending_email?: string
  existing_account_email?: string
  suggested_email?: string
  compat_email?: string
  requires_email_completion?: boolean
  existing_account_bindable?: boolean
}

export interface PendingOAuthCreateAccountRequest extends RegisterRequest {
  pending_auth_token?: string
  pending_oauth_token?: string
  adopt_display_name?: boolean
  adopt_avatar?: boolean
}

export interface PendingOAuthBindLoginRequest {
  email: string
  password: string
  adopt_display_name?: boolean
  adopt_avatar?: boolean
}

export function createPendingOAuthAccount(
  request: PendingOAuthCreateAccountRequest
): Promise<AxiosResponse<PendingOAuthAccountResponse>> {
  return apiClient.post<PendingOAuthAccountResponse>('/auth/oauth/pending/create-account', request)
}

export function bindPendingOAuthAccount(
  request: PendingOAuthBindLoginRequest
): Promise<AxiosResponse<PendingOAuthAccountResponse>> {
  return apiClient.post<PendingOAuthAccountResponse>('/auth/oauth/pending/bind-login', request)
}

export function completeEmailOAuthRegistration(
  provider: 'github' | 'google', request: { password: string }
): Promise<AxiosResponse<OAuthTokenResponse>> {
  return apiClient.post<OAuthTokenResponse>('/auth/oauth/' + provider + '/complete-registration', request)
}
