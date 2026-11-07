import type { User } from './profileTypes'

export interface LoginRequest {
  email: string
  password: string
  turnstile_token?: string
  tencent_captcha_ticket?: string
  tencent_captcha_randstr?: string
}

export interface TencentCaptchaRequestProof {
  tencent_captcha_ticket: string
  tencent_captcha_randstr: string
}

export interface ActionCaptchaRequestProof extends Partial<TencentCaptchaRequestProof> {
  turnstile_token?: string
}

export interface RegisterRequest {
  email: string
  password: string
  verify_code?: string
  turnstile_token?: string
  tencent_captcha_ticket?: string
  tencent_captcha_randstr?: string
}

export interface SendVerifyCodeRequest {
  email: string
  turnstile_token?: string
  tencent_captcha_ticket?: string
  tencent_captcha_randstr?: string
  pending_auth_token?: string
  pending_oauth_token?: string
}

export interface SendVerifyCodeResponse {
  message: string
  countdown: number
}

export interface AuthResponse {
  access_token: string
  refresh_token?: string  // New: Refresh Token for token renewal
  expires_in?: number     // New: Access Token expiry time in seconds
  token_type: string
  user: User & { run_mode?: 'standard' | 'simple' }
}

export interface CurrentUserResponse extends User {
  run_mode?: 'standard' | 'simple'
}

export interface TotpLoginResponse {
  requires_2fa: boolean
  temp_token?: string
  user_email_masked?: string
}

export interface TotpLogin2FARequest {
  temp_token: string
  totp_code: string
}
