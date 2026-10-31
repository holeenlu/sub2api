<script setup lang="ts">
import { toRaw } from 'vue'
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { isCancel } from 'axios'
import { readApiError } from '@/shared/lib/apiError'
import { currentAuthorizationScope } from '@/shared/http/client'
import { useIdentityRequest } from '../queries/useIdentityRequest'
import type { AuthSourceDefaultsState } from '@/features/system/api'
import type { AuthSourceType } from '@/features/system/api'
import type { DefaultPlatformQuotasMap } from '@/features/system/api'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import Select from '@/shared/ui/Select.vue'
import Toggle from '@/shared/ui/Toggle.vue'
import { useIdentitySettingsServices } from './ports'
const services = useIdentitySettingsServices()
const { GroupBadge, GroupOptionItem } = services.components

import type { DeepReadonly } from 'vue'
import type { AuthDefaultsDraft } from './authDefaultsModel'
const props = defineProps<{ modelValue: DeepReadonly<AuthDefaultsDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<AuthDefaultsDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as AuthDefaultsDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof AuthDefaultsDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<AuthDefaultsDraft> = {}
  for (const key of Object.keys(value) as (keyof AuthDefaultsDraft)[]) {
    if (JSON.stringify(value[key]) !== JSON.stringify(props.modelValue[key])) Object.assign(patch, { [key]: clone(value[key]) })
  }
  if (Object.keys(patch).length) emit('patch', patch)
}, { deep: true })

const subscriptionGroups = ref<AdminGroup[]>([])

const groupLoadState = ref<'idle' | 'loading' | 'loaded' | 'error'>('idle')
function resetGroupQuery(): void {
  subscriptionGroups.value = []
  groupLoadState.value = 'idle'
}
const groupRequest = useIdentityRequest(currentAuthorizationScope)
// The existing settings port has no signal argument. Fence results with the
// domain request owner and exact reader instead of accepting late responses.
watch([() => services.groups, () => services.groups.getAll], () => {
  groupRequest.cancel()
  resetGroupQuery()
  void loadSubscriptionGroups()
}, { flush: 'sync' })

const listPlatformIds = services.platformIds
const { t } = useI18n();

interface DefaultSubscriptionGroupOption {
  value: number;
  label: string;
  description: string | null;
  platform: AdminGroup["platform"];
  subscriptionType: AdminGroup["subscription_type"];
  rate: number;
  [key: string]: unknown;
}

function platformQuotaRows(map: DefaultPlatformQuotasMap | undefined): string[] {
  return listPlatformIds().filter((platform) => !!map?.[platform]);
}

const authSourceDefaults = reactive<AuthSourceDefaultsState>(
  services.authDefaults.build({}),
);

const authSourceDefaultsMeta = computed(() => [
  {
    source: "email" as AuthSourceType,
    title: t("admin.settings.authSourceDefaults.sources.email.title"),
    description: t("admin.settings.authSourceDefaults.sources.email.description"),
  },
  {
    source: "linuxdo" as AuthSourceType,
    title: t("admin.settings.authSourceDefaults.sources.linuxdo.title"),
    description: t("admin.settings.authSourceDefaults.sources.linuxdo.description"),
  },
  {
    source: "oidc" as AuthSourceType,
    title: t("admin.settings.authSourceDefaults.sources.oidc.title"),
    description: t("admin.settings.authSourceDefaults.sources.oidc.description"),
  },
  {
    source: "wechat" as AuthSourceType,
    title: t("admin.settings.authSourceDefaults.sources.wechat.title"),
    description: t("admin.settings.authSourceDefaults.sources.wechat.description"),
  },
  {
    source: "github" as AuthSourceType,
    title: "GitHub",
    description: t('ui.appliedOnFirstSignupOrFirstBindThroughA'),
  },
  {
    source: "google" as AuthSourceType,
    title: "Google",
    description: t('ui.appliedOnFirstSignupOrFirstBindThroughADetails'),
  },
  {
    source: "dingtalk" as AuthSourceType,
    title: t("auth.dingtalkProviderName"),
    description: t('ui.appliedOnFirstSignupOrFirstBindThroughDingtalk'),
  },
]);

const defaultSubscriptionGroupOptions = computed<
  DefaultSubscriptionGroupOption[]
>(() =>
  subscriptionGroups.value.map((group) => ({
    value: group.id,
    label: group.name,
    description: group.description,
    platform: group.platform,
    subscriptionType: group.subscription_type,
    rate: group.rate_multiplier,
  })),
);

async function loadSubscriptionGroups(): Promise<void> {
  if (groupLoadState.value === 'loading') return
  const request = groupRequest.begin()
  const source = services.groups
  const reader = source.getAll
  const isCurrent = () => request.isCurrent() && source === services.groups && reader === services.groups.getAll
  const discard = () => {
    // A non-reactive reader/session can change while awaiting the transport.
    // Never reset a newer request that already aborted this ticket.
    if (!request.signal.aborted) {
      groupRequest.cancel()
      resetGroupQuery()
    }
  }
  groupLoadState.value = 'loading'
  try {
    const groups = await reader.call(source)
    if (!isCurrent()) { discard(); return }
    subscriptionGroups.value = groups.filter(
      group => group.subscription_type === 'subscription' && group.status === 'active'
    )
    groupLoadState.value = 'loaded'
  } catch (error: unknown) {
    if (!isCurrent()) { discard(); return }
    const details = readApiError(error)
    groupLoadState.value = isCancel(error) || details.name === 'AbortError'
      || details.name === 'CanceledError' || details.code === 'ERR_CANCELED' ? 'idle' : 'error'
  }
}

function findNextAvailableSubscriptionGroup(
  existingGroupIDs: number[],
): AdminGroup | undefined {
  const existing = new Set(existingGroupIDs);
  return subscriptionGroups.value.find((group) => !existing.has(group.id));
}


function addAuthSourceDefaultSubscription(source: AuthSourceType) {
  if (groupLoadState.value !== 'loaded' || subscriptionGroups.value.length === 0) return;
  const candidate = findNextAvailableSubscriptionGroup(
    authSourceDefaults[source].subscriptions.map((item) => item.group_id),
  );
  if (!candidate) return;
  authSourceDefaults[source].subscriptions.push({
    group_id: candidate.id,
    validity_days: 30,
  });
}

function removeAuthSourceDefaultSubscription(
  source: AuthSourceType,
  index: number,
) {
  authSourceDefaults[source].subscriptions.splice(index, 1);
}
watch(() => props.modelValue, value => { const next = services.authDefaults.build(clone(value) as Partial<AuthDefaultsDraft>); if (JSON.stringify(next) !== JSON.stringify(authSourceDefaults)) Object.assign(authSourceDefaults, next) }, { immediate: true })
watch(authSourceDefaults, value => { const patch: Partial<AuthDefaultsDraft> = {}; services.authDefaults.append(patch, value); const initial: Partial<AuthDefaultsDraft> = {}; services.authDefaults.append(initial, services.authDefaults.build(clone(props.modelValue) as Partial<AuthDefaultsDraft>)); const changed = Object.fromEntries(Object.entries(patch).filter(([key, next]) => JSON.stringify(next) !== JSON.stringify(initial[key as keyof AuthDefaultsDraft]))); if (Object.keys(changed).length) emit('patch', changed) }, { deep: true })
onMounted(loadSubscriptionGroups)
</script>

<template>
<div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.authSourceDefaults.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.authSourceDefaults.description") }}
              </p>
            </div>
            <div class="space-y-6 p-6">
              <p v-if="groupLoadState === 'loading'" role="status" class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</p>
              <div v-else-if="groupLoadState === 'error' || groupLoadState === 'idle'" :role="groupLoadState === 'error' ? 'alert' : undefined" class="space-y-2">
                <p v-if="groupLoadState === 'error'" class="text-sm text-red-600 dark:text-red-400">{{ t('admin.users.failedToLoadGroups') }}</p>
                <button type="button" class="btn btn-secondary btn-sm" @click="loadSubscriptionGroups">{{ t('common.tryAgain') }}</button>
              </div>
              <p v-else-if="subscriptionGroups.length === 0" role="status" class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.noGroupsAvailable') }}</p>

              <div
                class="flex items-center justify-between rounded border border-gray-200 px-4 py-3 dark:border-dark-700"
              >
                <div>
                  <label class="font-medium text-gray-900 dark:text-white">
                    {{ t("admin.settings.authSourceDefaults.requireEmailLabel") }}
                  </label>
                  <p class="text-sm text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.authSourceDefaults.requireEmailHint") }}
                  </p>
                </div>
                <Toggle v-model="form.force_email_on_third_party_signup" />
              </div>

              <div class="space-y-4">
                <div
                  v-for="authSource in authSourceDefaultsMeta"
                  :key="authSource.source"
                  class="rounded-xl border border-gray-200 p-4 dark:border-dark-700"
                >
                  <div class="flex items-center justify-between gap-4">
                    <div>
                      <div class="font-medium text-gray-900 dark:text-white">
                        {{ authSource.title }}
                      </div>
                      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                        {{ authSource.description }}
                      </p>
                    </div>
                    <Toggle
                      v-model="
                        authSourceDefaults[authSource.source].grant_on_signup
                      "
                      :data-testid="`auth-source-${authSource.source}-enabled`"
                    />
                  </div>

                  <div
                    v-if="authSourceDefaults[authSource.source].grant_on_signup"
                    :data-testid="`auth-source-${authSource.source}-panel`"
                    class="mt-4 space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700"
                  >
                    <p class="text-sm text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.authSourceDefaults.enabledHint") }}
                    </p>

                    <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
                      <div>
                        <label
                          class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                        >
                          {{ t("admin.settings.defaults.defaultBalance") }}
                        </label>
                        <input
                          v-model.number="
                            authSourceDefaults[authSource.source].balance
                          "
                          type="number"
                          step="0.01"
                          min="0"
                          class="input"
                          placeholder="0.00"
                        />
                      </div>
                      <div>
                        <label
                          class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                        >
                          {{ t("admin.settings.defaults.defaultConcurrency") }}
                        </label>
                        <input
                          v-model.number="
                            authSourceDefaults[authSource.source].concurrency
                          "
                          type="number"
                          min="1"
                          class="input"
                          placeholder="5"
                        />
                      </div>
                    </div>

                    <div
                      class="flex items-center justify-between rounded border border-gray-200 px-4 py-3 dark:border-dark-700"
                    >
                      <div>
                        <label
                          class="font-medium text-gray-900 dark:text-white"
                        >
                          {{ t("admin.settings.authSourceDefaults.grantOnFirstBindLabel") }}
                        </label>
                        <p
                          class="mt-0.5 text-xs text-gray-500 dark:text-gray-400"
                        >
                          {{ t("admin.settings.authSourceDefaults.grantOnFirstBindHint") }}
                        </p>
                      </div>
                      <Toggle
                        v-model="
                          authSourceDefaults[authSource.source]
                            .grant_on_first_bind
                        "
                      />
                    </div>

                    <div class="mb-3 flex items-center justify-between">
                      <div>
                        <label
                          class="font-medium text-gray-900 dark:text-white"
                        >
                          {{ t("admin.settings.authSourceDefaults.defaultSubscriptionsLabel") }}
                        </label>
                        <p class="text-sm text-gray-500 dark:text-gray-400">
                          {{ t("admin.settings.authSourceDefaults.defaultSubscriptionsHint") }}
                        </p>
                      </div>
                      <button
                        type="button"
                        class="btn btn-secondary btn-sm"
                        @click="
                          addAuthSourceDefaultSubscription(authSource.source)
                        "
                        :disabled="groupLoadState !== 'loaded' || subscriptionGroups.length === 0"
                      >
                        {{
                          t("admin.settings.defaults.addDefaultSubscription")
                        }}
                      </button>
                    </div>

                    <div
                      v-if="
                        groupLoadState === 'loaded' && authSourceDefaults[authSource.source].subscriptions
                          .length === 0
                      "
                      class="rounded border border-dashed border-gray-300 px-4 py-3 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400"
                    >
                      {{ t("admin.settings.authSourceDefaults.noSourceSubscriptions") }}
                    </div>

                    <div v-else-if="authSourceDefaults[authSource.source].subscriptions.length > 0" class="space-y-3">
                      <div
                        v-for="(item, index) in authSourceDefaults[
                          authSource.source
                        ].subscriptions"
                        :key="`${authSource.source}-sub-${index}`"
                        class="grid grid-cols-1 gap-3 rounded border border-gray-200 p-3 md:grid-cols-[1fr_160px_auto] dark:border-dark-600"
                      >
                        <div>
                          <label
                            class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                          >
                            {{ t("admin.settings.defaults.subscriptionGroup") }}
                          </label>
                          <Select
                            v-model="item.group_id"
                            class="default-sub-group-select"
                            :options="defaultSubscriptionGroupOptions"
                            :loading="groupLoadState === 'loading'"
                            :error="groupLoadState === 'error'"
                            :disabled="groupLoadState !== 'loaded'"
                            :placeholder="
                              t('admin.settings.defaults.subscriptionGroup')
                            "
                          >
                            <template #selected="{ option }">
                              <GroupBadge
                                v-if="option"
                                :name="
                                  (
                                    option as unknown as DefaultSubscriptionGroupOption
                                  ).label
                                "
                                :platform="
                                  (
                                    option as unknown as DefaultSubscriptionGroupOption
                                  ).platform
                                "
                                :subscription-type="
                                  (
                                    option as unknown as DefaultSubscriptionGroupOption
                                  ).subscriptionType
                                "
                                :rate-multiplier="
                                  (
                                    option as unknown as DefaultSubscriptionGroupOption
                                  ).rate
                                "
                              />
                              <span v-else class="text-gray-400">
                                {{
                                  t("admin.settings.defaults.subscriptionGroup")
                                }}
                              </span>
                            </template>
                            <template #option="{ option, selected }">
                              <GroupOptionItem
                                :name="
                                  (
                                    option as unknown as DefaultSubscriptionGroupOption
                                  ).label
                                "
                                :platform="
                                  (
                                    option as unknown as DefaultSubscriptionGroupOption
                                  ).platform
                                "
                                :subscription-type="
                                  (
                                    option as unknown as DefaultSubscriptionGroupOption
                                  ).subscriptionType
                                "
                                :rate-multiplier="
                                  (
                                    option as unknown as DefaultSubscriptionGroupOption
                                  ).rate
                                "
                                :description="
                                  (
                                    option as unknown as DefaultSubscriptionGroupOption
                                  ).description
                                "
                                :selected="selected"
                              />
                            </template>
                          </Select>
                        </div>
                        <div>
                          <label
                            class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                          >
                            {{
                              t(
                                "admin.settings.defaults.subscriptionValidityDays",
                              )
                            }}
                          </label>
                          <input
                            v-model.number="item.validity_days"
                            type="number"
                            min="1"
                            max="36500"
                            class="input h-[42px]"
                          />
                        </div>
                        <div class="flex items-end">
                          <button
                            type="button"
                            class="btn btn-secondary w-full text-red-600 hover:text-red-700 dark:text-red-400"
                            @click="
                              removeAuthSourceDefaultSubscription(
                                authSource.source,
                                index,
                              )
                            "
                          >
                            {{ t("common.delete") }}
                          </button>
                        </div>
                      </div>
                    </div>

                    <!-- ★ 新增：auth source 平台限额覆盖区块 -->
                    <div class="border-t border-gray-100 pt-4 dark:border-dark-700">
                      <div class="mb-3">
                        <label class="font-medium text-gray-900 dark:text-white">
                          {{ t("admin.settings.authSourceDefaults.platformQuotasOverride") }}
                        </label>
                        <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                          {{ t("admin.settings.authSourceDefaults.platformQuotasOverrideHint") }}
                        </p>
                      </div>
                      <div class="overflow-x-auto">
                        <table class="min-w-full text-sm">
                          <thead>
                            <tr class="text-left text-xs text-gray-500 dark:text-gray-400">
                              <th class="pb-2 pr-4 font-medium">{{ t("admin.settings.platformQuota.platform") }}</th>
                              <th class="pb-2 pr-4 font-medium">{{ t("admin.settings.platformQuota.daily") }}</th>
                              <th class="pb-2 pr-4 font-medium">{{ t("admin.settings.platformQuota.weekly") }}</th>
                              <th class="pb-2 font-medium">{{ t("admin.settings.platformQuota.monthly") }}</th>
                            </tr>
                          </thead>
                          <tbody>
                            <tr v-for="p in platformQuotaRows(authSourceDefaults[authSource.source].platform_quotas)" :key="`${authSource.source}-pq-${p}`" class="align-top">
                              <td class="pr-4 py-1">
                                <span class="font-mono text-xs text-gray-700 dark:text-gray-300">{{ p }}</span>
                              </td>
                              <td class="pr-4 py-1">
                                <input
                                  v-model.number="authSourceDefaults[authSource.source].platform_quotas[p]!.daily"
                                  type="number"
                                  step="0.01"
                                  min="0"
                                  class="input h-8 w-28 text-sm"
                                  :placeholder="t('admin.settings.platformQuota.placeholder')"
                                />
                              </td>
                              <td class="pr-4 py-1">
                                <input
                                  v-model.number="authSourceDefaults[authSource.source].platform_quotas[p]!.weekly"
                                  type="number"
                                  step="0.01"
                                  min="0"
                                  class="input h-8 w-28 text-sm"
                                  :placeholder="t('admin.settings.platformQuota.placeholder')"
                                />
                              </td>
                              <td class="py-1">
                                <input
                                  v-model.number="authSourceDefaults[authSource.source].platform_quotas[p]!.monthly"
                                  type="number"
                                  step="0.01"
                                  min="0"
                                  class="input h-8 w-28 text-sm"
                                  :placeholder="t('admin.settings.platformQuota.placeholder')"
                                />
                              </td>
                            </tr>
                          </tbody>
                        </table>
                      </div>
                    </div>
                    <!-- /auth source 平台限额覆盖区块 -->
                  </div>
                </div>
              </div>
            </div>
          </div>
</template>
