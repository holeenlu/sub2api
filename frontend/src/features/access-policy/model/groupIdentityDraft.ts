import type { GroupI18n, GroupPlatform, AdminGroup } from '@/features/access-policy/api/groupTypes';
/** Identity is independent of access policy, pricing and subscription drafts. */
export interface GroupIdentityDraft {
 name: string
 description: string
 i18n: GroupI18n
 platform: GroupPlatform
}
export function createGroupIdentityDraft(group?: Readonly<AdminGroup>): GroupIdentityDraft {
 return {
  name: group?.name ?? '', description: group?.description ?? '', platform: group?.platform ?? 'anthropic',
  i18n: Object.fromEntries(Object.entries(group?.i18n ?? {}).map(([locale, value]) => [locale, { ...value }]))
 }
}
