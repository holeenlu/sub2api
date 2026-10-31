import type { DefaultSubscriptionSetting } from '@/features/system/api'

export function normalizeDefaultSubscriptionSettings(
  subscriptions: DefaultSubscriptionSetting[] | null | undefined,
): DefaultSubscriptionSetting[] {
  if (!Array.isArray(subscriptions)) return [];

  return subscriptions
    .filter((item) => item.group_id > 0 && item.validity_days > 0)
    .map((item) => ({
      group_id: Math.floor(item.group_id),
      validity_days: Math.min(
        36500,
        Math.max(1, Math.floor(item.validity_days)),
      ),
    }));
}

/** Validate only the collection the user actually changed. */
export function prepareDefaultSubscriptionSettings(
  input: DefaultSubscriptionSetting[] | null | undefined,
  field: string,
): DefaultSubscriptionSetting[] {
  const subscriptions = normalizeDefaultSubscriptionSettings(input)
  const ids = new Set<number>()
  for (const subscription of subscriptions) {
    if (ids.has(subscription.group_id)) throw { field, messageKey: 'admin.settings.defaults.defaultSubscriptionsDuplicate', params: { groupId: subscription.group_id } }
    ids.add(subscription.group_id)
  }
  return subscriptions
}
