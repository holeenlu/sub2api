import type { ResourceProxyOption } from './proxyOption'
import type { Account } from '@/features/resources/api/accountTypes';
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import type { TempUnschedulableScope } from '@/features/resources/api/accountObservationTypes';


export interface Props {
  saveAccount?: (id: number, updates: Record<string, unknown>) => Promise<Account>
  supplierScope?: boolean
  captureBaseline?: boolean
  show: boolean
  account: Account | null
  proxies: readonly ResourceProxyOption[]
  groups: AdminGroup[]
  allowRateEdit?: boolean
  allowAuthorize?: boolean
  allowTransportEdit?: boolean
  showCost?: boolean
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


export type CodexImageToolMode = 'inherit' | 'enabled' | 'disabled' | 'block'


export type AnthropicAPIKeyAuthScheme = 'x_api_key' | 'authorization_bearer'


export type EditResourceForm = { name: string; notes: string; proxy_id: number | null; concurrency: number; load_factor: number | null; priority: number; rate_multiplier: number; status: Account["status"]; group_ids: number[]; expires_at: number | null }
