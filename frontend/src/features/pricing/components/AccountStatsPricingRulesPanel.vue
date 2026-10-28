<template>
<div class="space-y-3">
              <div class="flex items-center justify-between">
                <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.channels.form.accountStatsPricingRules') }}
                </h4>
                <button
                  type="button"
                  @click="addRule()"
                  class="rounded-lg border border-primary-300 px-3 py-1 text-xs font-medium text-primary-600 hover:bg-primary-50 dark:border-primary-600 dark:text-primary-400 dark:hover:bg-primary-900/20"
                >
                  + {{ t('admin.channels.form.addRule') }}
                </button>
              </div>

              <!-- Filter rules for this platform's groups -->
              <p
                v-if="rules.length === 0"
                class="text-xs italic text-gray-400 dark:text-gray-500"
              >
                {{ t('admin.channels.form.noRulesConfigured') }}
              </p>

              <div
                v-for="(rule, ruleIndex) in rules"
                :key="ruleIndex"
                class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600"
              >
                <div class="flex items-center justify-between">
                  <input
                    :value="rule.name" @input="updateRule(ruleIndex, { name: ($event.target as HTMLInputElement).value })"
                    :placeholder="t('admin.channels.form.ruleName')"
                    class="bg-transparent text-sm font-medium text-gray-700 placeholder-gray-400 outline-none dark:text-gray-300"
                  />
                  <button type="button" @click="removeRule(ruleIndex)" class="text-xs text-red-500 hover:text-red-700">
                    {{ t('common.delete') }}
                  </button>
                </div>

                <div>
                  <label class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.channels.form.ruleGroups') }}</label>
                  <div class="mt-1 flex flex-wrap gap-1">
                    <label
                      v-for="gid in groupIds"
                      :key="gid"
                      class="inline-flex cursor-pointer items-center gap-1 rounded-md border px-2 py-1 text-xs transition-colors"
                      :class="rule.group_ids.includes(gid)
                        ? 'border-primary-300 bg-primary-50 dark:border-primary-700 dark:bg-primary-900/20'
                        : 'border-gray-200 hover:bg-gray-50 dark:border-dark-600 dark:hover:bg-dark-700'"
                    >
                      <input type="checkbox" :checked="rule.group_ids.includes(gid)" class="h-3 w-3 rounded border-gray-300 text-primary-600 focus:ring-primary-500" @change="toggleGroup(ruleIndex, gid)" />
                      <span :class="['font-medium', platformTextClass(platform)]">{{ getGroupNameById(gid) }}</span>
                    </label>
                  </div>
                  <p v-if="groupIds.length === 0" class="mt-1 text-xs text-gray-400">
                    {{ t('admin.channels.form.noGroupsInChannel') }}
                  </p>
                </div>

                <div>
                  <label class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.channels.form.ruleAccounts') }}</label>
                  <!-- Selected account chips -->
                  <div class="mt-1 flex flex-wrap gap-1">
                    <span
                      v-for="accountId in rule.account_ids"
                      :key="accountId"
                      class="inline-flex items-center gap-1 rounded-md border border-primary-300 bg-primary-50 px-2 py-0.5 text-xs dark:border-primary-700 dark:bg-primary-900/20"
                    >
                      <span :class="['font-medium', platformTextClass(platform)]">{{ getRuleAccountLabel(accountId) }}</span>
                      <button type="button" @click="removeRuleAccount(ruleIndex, accountId)" class="text-gray-400 hover:text-red-500">
                        <Icon name="x" size="xs" />
                      </button>
                    </span>
                  </div>
                  <!-- Account search input -->
                  <div class="relative mt-1 rule-account-search-container">
                    <input
                      v-model="ruleAccountSearchKeyword[`${platform}-${ruleIndex}`]"
                      type="text"
                      class="input text-sm"
                      :placeholder="t('admin.channels.form.searchAccountPlaceholder')"
                      @input="onRuleAccountSearchInput(platform, ruleIndex)"
                      @focus="onRuleAccountSearchFocus(platform, ruleIndex)"
                    />
                    <!-- Search results dropdown -->
                    <div
                      v-if="showRuleAccountDropdown[`${platform}-${ruleIndex}`] && (ruleAccountSearchResults[`${platform}-${ruleIndex}`]?.length ?? 0) > 0"
                      class="absolute z-50 mt-1 max-h-48 w-full overflow-auto rounded-lg border bg-white shadow-lg dark:border-dark-600 dark:bg-dark-800"
                    >
                      <button
                        v-for="account in ruleAccountSearchResults[`${platform}-${ruleIndex}`]"
                        :key="account.id"
                        type="button"
                        @click="selectRuleAccount(ruleIndex, account)"
                        class="w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-dark-700"
                        :class="{ 'opacity-50': rule.account_ids.includes(account.id) }"
                        :disabled="rule.account_ids.includes(account.id)"
                      >
                        <span :class="platformTextClass(account.platform)">{{ account.name }}</span>
                        <span class="ml-2 text-xs text-gray-400">#{{ account.id }}</span>
                      </button>
                    </div>
                  </div>
                  <p class="mt-1 text-xs text-gray-400">
                    {{ t('admin.channels.form.ruleAccountsHint') }}
                  </p>
                </div>

                <div>
                  <div class="mb-1 flex items-center justify-between">
                    <label class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.channels.form.ruleModelPricing') }}</label>
                    <button type="button" @click="addRulePricingEntry(ruleIndex)" class="text-xs text-primary-600 hover:text-primary-700">
                      + {{ t('common.add') }}
                    </button>
                  </div>
                  <div v-if="rule.pricing.length === 0" class="rounded border border-dashed border-gray-300 p-2 text-center text-xs text-gray-400 dark:border-dark-500">
                    {{ t('admin.channels.form.noPricingRules') }}
                  </div>
                  <div v-else class="space-y-2">
                    <PricingEntryCard
                      v-for="(entry, pIdx) in rule.pricing"
                      :key="pIdx"
                      :entry="entry"
                      :platform="platform"
                      @update="updateRulePricing(ruleIndex, pIdx, $event)"
                      @remove="removeRulePricingEntry(ruleIndex, pIdx)"
                    />
                  </div>
                </div>
              </div>
            </div>
</template>
<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import type { FormPricingRule } from '../model/channelDraft'
import type { PricingFormEntry } from '@/features/pricing/model/pricingForm'
import { createDefaultTimePricingForm } from '@/features/pricing/model/pricingForm'
import { useKeyedDebouncedSearch } from '@/shared/lib/useKeyedDebouncedSearch'
import { platformTextClass } from '@/shared/styles/platformColors'
import Icon from '@/shared/ui/icons/Icon.vue'
import PricingEntryCard from '@/features/pricing/components/PricingEntryCard.vue'
import { pricingDirectoryKey, requirePricingPort, type PricingAccountOption } from '../ports'
import { usePricingRequest } from '../queries/requestScope'
const props = defineProps<{ modelValue: FormPricingRule[]; platform: string; groupIds: number[]; groups: AdminGroup[] }>()
const emit = defineEmits<{ 'update:modelValue': [rules: FormPricingRule[]] }>()
const { t } = useI18n()
const directory = requirePricingPort(pricingDirectoryKey)
const rules = computed(() => props.modelValue)
function updateRule(index: number, patch: Partial<FormPricingRule>) { emit('update:modelValue', props.modelValue.map((rule, i) => i === index ? { ...rule, ...patch } : rule)) }
function addRule() { emit('update:modelValue', [...props.modelValue, { name: '', group_ids: [], account_ids: [], pricing: [] }]) }
function removeRule(index: number) { runner.clearAll(); clearSearch(); emit('update:modelValue', props.modelValue.filter((_, i) => i !== index)) }
function toggleGroup(index: number, id: number) { const rule = props.modelValue[index]; updateRule(index, { group_ids: rule.group_ids.includes(id) ? rule.group_ids.filter(value => value !== id) : [...rule.group_ids, id] }) }
function removeRuleAccount(index: number, id: number) { updateRule(index, { account_ids: props.modelValue[index].account_ids.filter(value => value !== id) }) }
function selectRuleAccount(index: number, account: PricingAccountOption) {
 const rule = props.modelValue[index]
 if (!rule.account_ids.includes(account.id)) updateRule(index, { account_ids: [...rule.account_ids, account.id] })
 names.value[account.id] = account.name
 clearSearch()
}
function addRulePricingEntry(index: number) {
 const entry: PricingFormEntry = { models: [], billing_mode: 'token', input_price: null, output_price: null, cache_write_price: null, cache_write_1h_price: null, cache_read_price: null, image_input_price: null, image_output_price: null, per_request_price: null, intervals: [], time_pricing: createDefaultTimePricingForm() }
 updateRule(index, { pricing: [...props.modelValue[index].pricing, entry] })
}
function updateRulePricing(index: number, priceIndex: number, entry: PricingFormEntry) { updateRule(index, { pricing: props.modelValue[index].pricing.map((value, i) => i === priceIndex ? entry : value) }) }
function removeRulePricingEntry(index: number, priceIndex: number) { updateRule(index, { pricing: props.modelValue[index].pricing.filter((_, i) => i !== priceIndex) }) }
function getGroupNameById(id: number) { return props.groups.find(group => group.id === id)?.name || '#' + id }
const names = ref<Record<number, string>>({})
function getRuleAccountLabel(id: number) { return names.value[id] ? names.value[id] + ' #' + id : '#' + id }
const ruleAccountSearchKeyword = ref<Record<string, string>>({})
const ruleAccountSearchResults = ref<Record<string, PricingAccountOption[]>>({})
const showRuleAccountDropdown = ref<Record<string, boolean>>({})
const runner = useKeyedDebouncedSearch<PricingAccountOption[]>({
 delay: 300,
 search: (keyword, { signal }) => directory.searchAccounts(props.platform, keyword, signal),
 onSuccess: (key, result) => { ruleAccountSearchResults.value[key] = result },
 onError: (key) => { ruleAccountSearchResults.value[key] = [] }
})
function searchKey(platform: string, index: number) { return platform + '-' + index }
function onRuleAccountSearchInput(platform: string, index: number) { const key = searchKey(platform, index); showRuleAccountDropdown.value[key] = true; runner.trigger(key, ruleAccountSearchKeyword.value[key] || '') }
function onRuleAccountSearchFocus(platform: string, index: number) { onRuleAccountSearchInput(platform, index) }
function clearSearch() { ruleAccountSearchKeyword.value = {}; ruleAccountSearchResults.value = {}; showRuleAccountDropdown.value = {} }
const query = usePricingRequest(() => { names.value = {}; runner.clearAll(); clearSearch() })
watch(() => props.modelValue.flatMap(rule => rule.account_ids).join(','), async () => {
 const request = query.begin()
 const ids = [...new Set(props.modelValue.flatMap(rule => rule.account_ids))]
 const results = await Promise.allSettled(ids.map(id => directory.getAccount(id, request.signal)))
 if (!request.isCurrent()) return
 results.forEach((result, index) => { if (result.status === 'fulfilled') names.value[ids[index]] = result.value.name })
}, { immediate: true })
function outside(event: MouseEvent) { if (!(event.target as HTMLElement).closest('.rule-account-search-container')) showRuleAccountDropdown.value = {} }
onMounted(() => document.addEventListener('click', outside))
onUnmounted(() => { runner.clearAll(); document.removeEventListener('click', outside) })
</script>
