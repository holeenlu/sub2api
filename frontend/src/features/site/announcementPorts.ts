import { inject, type Component, type InjectionKey, type Ref } from 'vue'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';

export type AnnouncementAudienceGroup = Pick<AdminGroup, 'id' | 'name' | 'description' | 'platform' | 'subscription_type' | 'rate_multiplier' | 'account_count'>

/** App owns actor/session and cross-domain audience lookup; site owns announcement state. */
export interface AnnouncementServices {
  scopeKey: Readonly<Ref<string | null>>
  canManage: Readonly<Ref<boolean>>
  notifySuccess(message: string): void
  notifyError(message: string): void
  listSubscriptionGroups(options?: { signal?: AbortSignal }): Promise<AnnouncementAudienceGroup[]>
  GroupSelector: Component
}
export const announcementServicesKey: InjectionKey<AnnouncementServices> = Symbol('site.announcements')
export function useAnnouncementServices(): AnnouncementServices {
  const services = inject(announcementServicesKey)
  if (!services) throw new Error('AnnouncementServices must be provided by app composition')
  return services
}
