import { cloneDraftValue } from '@/shared/lib/useDraftSection'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes'
import type { SubscriptionType } from '../api/subscriptionPolicyTypes'
export function createGroupSubscriptionDraft() {
 return {
  subscription_type: "standard" as SubscriptionType,
  daily_limit_usd: null as number | null,
  weekly_limit_usd: null as number | null,
  monthly_limit_usd: null as number | null
 }
}
export type GroupSubscriptionDraft = ReturnType<typeof createGroupSubscriptionDraft>
export function groupSubscriptionDraftFromAPI(group: Readonly<AdminGroup>): GroupSubscriptionDraft {
 const draft = createGroupSubscriptionDraft()
draft.subscription_type = group.subscription_type || "standard";
draft.daily_limit_usd = group.daily_limit_usd;
draft.weekly_limit_usd = group.weekly_limit_usd;
draft.monthly_limit_usd = group.monthly_limit_usd;
 return draft
}
export function projectGroupSubscriptionDraft(value: Readonly<GroupSubscriptionDraft>): GroupSubscriptionDraft {
 const keys = Object.keys(createGroupSubscriptionDraft()) as Array<keyof GroupSubscriptionDraft>
 return Object.fromEntries(keys.map(key => [key, cloneDraftValue(value[key])])) as GroupSubscriptionDraft
}
const normalizeOptionalLimit = (
  value: number | string | null | undefined,
): number | null => {
  if (value === null || value === undefined) {
    return null;
  }

  if (typeof value === "string") {
    const trimmed = value.trim();
    if (!trimmed) {
      return null;
    }
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : null;
  }

  return Number.isFinite(value) && value > 0 ? value : null;
};
export function buildGroupSubscriptionPayload(value: Readonly<GroupSubscriptionDraft>) {
 return {
  subscription_type: value.subscription_type,
  daily_limit_usd: normalizeOptionalLimit(value.daily_limit_usd),
  weekly_limit_usd: normalizeOptionalLimit(value.weekly_limit_usd),
  monthly_limit_usd: normalizeOptionalLimit(value.monthly_limit_usd),
 }
}
