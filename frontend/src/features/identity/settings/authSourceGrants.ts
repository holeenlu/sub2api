import type { AuthSourceType, AuthSourceDefaultsValue } from '@/features/system/api'

export const AUTH_SOURCE_TYPES: AuthSourceType[] = [
  "email",
  "linuxdo",
  "oidc",
  "wechat",
  "github",
  "google",
  "dingtalk",
];
const AUTH_SOURCE_DEFAULT_BALANCE = 0;
const AUTH_SOURCE_DEFAULT_CONCURRENCY = 5;
type AuthSourceGrants = Pick<AuthSourceDefaultsValue, 'balance' | 'concurrency' | 'grant_on_signup' | 'grant_on_first_bind'>

export function projectAuthSourceGrants(value: Partial<AuthSourceGrants>): AuthSourceGrants {
  return {
    balance: Number(value.balance ?? AUTH_SOURCE_DEFAULT_BALANCE),
    concurrency: Math.max(1, Number(value.concurrency ?? AUTH_SOURCE_DEFAULT_CONCURRENCY)),
    grant_on_signup: value.grant_on_signup === true,
    grant_on_first_bind: value.grant_on_first_bind === true,
  }
}

export function prepareAuthSourceGrants(value: AuthSourceGrants): AuthSourceGrants {
  return {
    balance: Number(value.balance) || 0,
    concurrency: Math.max(1, Math.floor(Number(value.concurrency) || AUTH_SOURCE_DEFAULT_CONCURRENCY)),
    grant_on_signup: value.grant_on_signup,
    grant_on_first_bind: value.grant_on_first_bind,
  }
}
