import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type ClientIpDraft = Pick<SettingsDraft, 'api_key_acl_trust_forwarded_ip' | 'forwarded_client_ip_headers'>
export const clientIpFields = ['api_key_acl_trust_forwarded_ip', 'forwarded_client_ip_headers'] as const
export function projectClientIp(settings: SettingsDraft): ClientIpDraft {
  return {
    api_key_acl_trust_forwarded_ip: structuredClone(toRaw(settings.api_key_acl_trust_forwarded_ip ?? (true))),
    forwarded_client_ip_headers: structuredClone(toRaw(settings.forwarded_client_ip_headers ?? ([]))),
  }
}
