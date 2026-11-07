import type { AccessRule, AdminKey, AdminKeyInput } from '../api/adminAccessKeys'

// Null server slices become empty selections; no permissions are inferred.
export function normalizeAdminAccessKey(item: AdminKey): AdminKey {
  return { ...item, allowed_ips: item.allowed_ips ?? [], policy: {
    global_permissions: item.policy?.global_permissions ?? [],
    rules: (item.policy?.rules ?? []).map(rule => ({ ...rule,
      permissions: rule.permissions ?? [], group_ids: rule.group_ids ?? [],
      account_types: rule.account_types ?? [], platforms: rule.platforms ?? []
    }))
  } }
}

export function emptyAccessRule(): AccessRule {
  return { permissions: [], group_ids: [], all_groups: false, include_ungrouped: false, account_types: [], platforms: [] }
}

export function createAdminAccessKeyDraft(item?: AdminKey): AdminKeyInput {
  return item ? {
    name: item.name, description: item.description, enabled: item.enabled, expires_at: item.expires_at,
    allowed_ips: [...item.allowed_ips], policy: {
      global_permissions: [...item.policy.global_permissions],
      rules: item.policy.rules.map(rule => ({ ...rule, permissions: [...rule.permissions], group_ids: [...rule.group_ids], account_types: [...rule.account_types], platforms: [...rule.platforms] }))
    }
  } : { name: '', description: '', enabled: true, expires_at: null, allowed_ips: [], policy: { rules: [emptyAccessRule()], global_permissions: [] } }
}

export function localAccessKeyExpiration(value: string | null): string {
  if (!value) return ''
  const date = new Date(value)
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}

export function prepareAdminAccessKeyInput(draft: AdminKeyInput, expiration: string, initialExpiration: string, ips: string): AdminKeyInput {
  const input: AdminKeyInput = structuredClone(draft)
  input.name = input.name.trim()
  // Preserve server seconds, fractional precision and timezone when the field was not edited.
  if (expiration !== initialExpiration) input.expires_at = expiration ? new Date(expiration).toISOString() : null
  input.allowed_ips = ips.split(/[\n,]/).map(ip => ip.trim()).filter(Boolean)
  return input
}
