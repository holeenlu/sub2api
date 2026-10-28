import type { ResourceProxyOption } from './proxyOption'
import { type AuthInputMethod } from '@/features/resources/queries/useAccountOAuth'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import type { AccountPlatform, AccountType } from '@/features/resources/api/platformTypes';
import type { TempUnschedulableScope } from '@/features/resources/api/accountObservationTypes';


export interface OAuthFlowExposed {
  authCode: string
  oauthState: string
  projectId: string
  sessionKey: string
  setupToken: string
  refreshToken: string
  sessionToken: string
  codexSession: string
  codexPAT: string
  ssoCookie: string
  inputMethod: AuthInputMethod
  reset: () => void
}


export interface Props {
  show: boolean
  proxies: readonly ResourceProxyOption[]
  groups: AdminGroup[]
  allowRateEdit?: boolean
  allowSystemSettings?: boolean
}


export interface ModelMapping {
  from: string
  to: string
}


export interface TempUnschedRuleForm {
  error_code: number | null
  keywords: string
  duration_minutes: number | null
  description: string
  // 作用范围：account 暂停整个账号，model 只冷却本次请求的模型。
  scope: TempUnschedulableScope
}


export type CodexFingerprintMode = 'off' | 'device' | 'session' | 'full'


export type AnthropicAPIKeyAuthScheme = 'x_api_key' | 'authorization_bearer'


export type CreateResourceForm = { name: string; notes: string; platform: AccountPlatform; type: AccountType; credentials: Record<string, unknown>; proxy_id: number | null; concurrency: number; load_factor: number | null; priority: number; rate_multiplier: number; group_ids: number[]; expires_at: number | null }
