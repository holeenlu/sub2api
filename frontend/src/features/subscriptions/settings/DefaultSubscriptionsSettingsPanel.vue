<script setup lang="ts">
import { toRaw } from 'vue'
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { isCancel } from 'axios'
import { readApiError } from '@/shared/lib/apiError'
import { useSubscriptionRequest } from '../queries/requestScope'

import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import Select from '@/shared/ui/Select.vue'
import { useSubscriptionsSettingsServices } from './ports'
const services = useSubscriptionsSettingsServices()
const { GroupBadge, GroupOptionItem } = services.components

import type { DeepReadonly } from 'vue'
import type { DefaultSubscriptionsDraft } from './defaultSubscriptionsModel'
const props = defineProps<{ modelValue: DeepReadonly<DefaultSubscriptionsDraft> }>()
const emit = defineEmits<{ patch: [patch: Partial<DefaultSubscriptionsDraft>]; validity: [valid: boolean] }>()
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const form = reactive(clone(props.modelValue) as DefaultSubscriptionsDraft)
watch(() => props.modelValue, value => {
  for (const key of Object.keys(value) as (keyof DefaultSubscriptionsDraft)[]) {
    if (JSON.stringify(form[key]) !== JSON.stringify(value[key])) Object.assign(form, { [key]: clone(value[key]) })
  }
}, { deep: true })
watch(form, value => {
  const patch: Partial<DefaultSubscriptionsDraft> = {}
  for (const key of Object.keys(value) as (keyof DefaultSubscriptionsDraft)[]) {
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
const groupRequest = useSubscriptionRequest(resetGroupQuery)
// The existing settings port has no signal argument. Fence results with the
// domain request owner and exact reader instead of accepting late responses.
watch([() => services.groups, () => services.groups.getAll], () => {
  groupRequest.cancel()
  resetGroupQuery()
  void loadSubscriptionGroups()
}, { flush: 'sync' })
watch(groupRequest.scope, () => {
  if (groupRequest.scope.value) void loadSubscriptionGroups()
}, { flush: 'sync' })


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
  if (!groupRequest.scope.value) return
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

function addDefaultSubscription() {
  if (groupLoadState.value !== 'loaded' || subscriptionGroups.value.length === 0) return;
  const candidate = findNextAvailableSubscriptionGroup(
    form.default_subscriptions.map((item) => item.group_id),
  );
  if (!candidate) return;
  form.default_subscriptions.push({
    group_id: candidate.id,
    validity_days: 30,
  });
}

function removeDefaultSubscription(index: number) {
  form.default_subscriptions.splice(index, 1);
}
onMounted(loadSubscriptionGroups)
</script>

<template>
<div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.defaults.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.defaults.description") }}
              </p>
            </div>
            <div class="space-y-6 p-6">
              <p v-if="groupLoadState === 'loading'" role="status" class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</p>
              <div v-else-if="groupLoadState === 'error' || groupLoadState === 'idle'" :role="groupLoadState === 'error' ? 'alert' : undefined" class="space-y-2">
                <p v-if="groupLoadState === 'error'" class="text-sm text-red-600 dark:text-red-400">{{ t('admin.users.failedToLoadGroups') }}</p>
                <button type="button" class="btn btn-secondary btn-sm" @click="loadSubscriptionGroups">{{ t('common.tryAgain') }}</button>
              </div>
              <p v-else-if="subscriptionGroups.length === 0" role="status" class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.noGroupsAvailable') }}</p>

              

              <div class="border-t border-gray-100 pt-4 dark:border-dark-700">
                <div class="mb-3 flex items-center justify-between">
                  <div>
                    <label class="font-medium text-gray-900 dark:text-white">
                      {{ t("admin.settings.defaults.defaultSubscriptions") }}
                    </label>
                    <p class="text-sm text-gray-500 dark:text-gray-400">
                      {{
                        t("admin.settings.defaults.defaultSubscriptionsHint")
                      }}
                    </p>
                  </div>
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    @click="addDefaultSubscription"
                    :disabled="groupLoadState !== 'loaded' || subscriptionGroups.length === 0"
                  >
                    {{ t("admin.settings.defaults.addDefaultSubscription") }}
                  </button>
                </div>

                <div
                  v-if="groupLoadState === 'loaded' && form.default_subscriptions.length === 0"
                  class="rounded border border-dashed border-gray-300 px-4 py-3 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400"
                >
                  {{ t("admin.settings.defaults.defaultSubscriptionsEmpty") }}
                </div>

                <div v-else-if="form.default_subscriptions.length > 0" class="space-y-3">
                  <div
                    v-for="(item, index) in form.default_subscriptions"
                    :key="`default-sub-${index}`"
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
                            {{ t("admin.settings.defaults.subscriptionGroup") }}
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
                          t("admin.settings.defaults.subscriptionValidityDays")
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
                        class="btn btn-secondary default-sub-delete-btn w-full text-red-600 hover:text-red-700 dark:text-red-400"
                        @click="removeDefaultSubscription(index)"
                      >
                        {{ t("common.delete") }}
                      </button>
                    </div>
                  </div>
                </div>
              </div>

              <!-- ★ 新增：系统全局默认平台限额矩阵 -->
              
              <!-- /全局平台限额矩阵 -->
            </div>
          </div>
</template>
