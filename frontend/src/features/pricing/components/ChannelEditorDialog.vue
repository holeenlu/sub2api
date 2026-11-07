<template>
    <!-- Create/Edit Dialog -->
    <BaseDialog
      :show="showDialog"
      :title="editingChannel ? t('admin.channels.editChannel', 'Edit Channel') : t('admin.channels.createChannel', 'Create Channel')"
      width="extra-wide"
      :show-close-button="!submitting"
      :close-on-escape="!submitting"
      @close="closeDialog"
    >
      <div class="channel-dialog-body">
        <!-- Tab Bar -->
        <div class="flex items-center border-b border-gray-200 dark:border-dark-700 flex-shrink-0 -mx-4 sm:-mx-6 px-4 sm:px-6 -mt-3 sm:-mt-4">
          <!-- Basic Settings Tab -->
          <button
            type="button"
            @click="activeTab = 'basic'"
            class="channel-tab"
            :class="activeTab === 'basic' ? 'channel-tab-active' : 'channel-tab-inactive'"
          >
            {{ t('admin.channels.form.basicSettings') }}
          </button>
          <!-- Platform Tabs (only enabled) -->
          <button
            v-for="section in form.platforms.filter(s => s.enabled)"
            :key="section.platform"
            type="button"
            @click="activeTab = section.platform"
            class="channel-tab group"
            :class="activeTab === section.platform ? 'channel-tab-active' : 'channel-tab-inactive'"
          >
            <PlatformIcon :platform="section.platform" size="xs" :class="platformTextClass(section.platform)" />
            <span :class="platformTextClass(section.platform)">{{ t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform)) }}</span>
          </button>
        </div>

        <!-- Tab Content -->
        <form id="channel-form" :inert="submitting || invalidated" @submit.prevent="handleSubmit" class="flex-1 overflow-y-auto pt-4">
          <!-- Basic Settings Tab -->
          <div v-show="activeTab === 'basic'" class="space-y-5">
            <!-- Name -->
            <div>
              <label class="input-label">{{ t('admin.channels.form.name', 'Name') }} <span class="text-red-500">*</span></label>
              <input
                v-model="form.name"
                type="text"
                required
                class="input"
                :placeholder="t('admin.channels.form.namePlaceholder', 'Enter channel name')"
              />
            </div>

            <!-- Description -->
            <div>
              <label class="input-label">{{ t('admin.channels.form.description', 'Description') }}</label>
              <textarea
                v-model="form.description"
                rows="2"
                class="input"
                :placeholder="t('admin.channels.form.descriptionPlaceholder', 'Optional description')"
              ></textarea>
            </div>

            <!-- Status (edit only) -->
            <div v-if="editingChannel">
              <label class="input-label">{{ t('admin.channels.form.status', 'Status') }}</label>
              <Select v-model="form.status" :options="statusEditOptions" />
            </div>

            <!-- Model Restriction -->
            <div>
              <label class="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  v-model="form.restrict_models"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                />
                <span class="input-label mb-0">{{ t('admin.channels.form.restrictModels', 'Restrict Models') }}</span>
              </label>
              <p class="mt-1 ml-6 text-xs text-gray-400">
                {{ t('admin.channels.form.restrictModelsHint', 'When enabled, only models in the pricing list are allowed. Others will be rejected.') }}
              </p>
            </div>

            <!-- Billing Basis -->
            <div :inert="!rolePermissions.canEditAdminField(editingChannel ? 'channel.update' : 'channel.create', 'billing_model_source')" :aria-disabled="!rolePermissions.canEditAdminField(editingChannel ? 'channel.update' : 'channel.create', 'billing_model_source')">
              <label class="input-label">{{ t('admin.channels.form.billingModelSource', 'Billing Basis') }}</label>
              <Select v-model="form.billing_model_source" :options="billingModelSourceOptions" />
              <p class="mt-1 text-xs text-gray-400">
                {{ t('admin.channels.form.billingModelSourceHint', 'Controls which model name is used for pricing lookup') }}
              </p>
            </div>

            <!-- Platform Management -->
            <div class="space-y-3">
              <label class="input-label mb-0">{{ t('admin.channels.form.platformConfig') }}</label>
              <div class="flex flex-wrap gap-2">
                <label
                  v-for="p in platformOrder"
                  :key="p"
                  class="inline-flex cursor-pointer items-center gap-1.5 rounded-md border px-3 py-1.5 text-sm transition-colors"
                  :class="activePlatforms.includes(p)
                    ? 'bg-primary-50 border-primary-300 dark:bg-primary-900/20 dark:border-primary-700'
                    : 'border-gray-200 hover:bg-gray-50 dark:border-dark-600 dark:hover:bg-dark-700'"
                >
                  <input
                    type="checkbox"
                    :checked="activePlatforms.includes(p)"
                    class="h-3.5 w-3.5 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                    @change="togglePlatform(p)"
                  />
                  <PlatformIcon :platform="p" size="xs" :class="platformTextClass(p)" />
                  <span :class="platformTextClass(p)">{{ t('admin.groups.platforms.' + p, catalogPlatformLabel(p)) }}</span>
                </label>
              </div>
            </div>

            <!-- Apply Pricing to Account Stats (toggle only in basic settings) -->
            <div class="border-t border-gray-200 pt-4 dark:border-dark-700">
              <div :inert="!rolePermissions.canEditAdminField(editingChannel ? 'channel.update' : 'channel.create', 'apply_pricing_to_account_stats')" :aria-disabled="!rolePermissions.canEditAdminField(editingChannel ? 'channel.update' : 'channel.create', 'apply_pricing_to_account_stats')" class="flex items-center justify-between">
                <div>
                  <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                    {{ t('admin.channels.form.applyPricingToAccountStats') }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t('admin.channels.form.applyPricingToAccountStatsDesc') }}
                  </p>
                </div>
                <Toggle
                  :modelValue="form.apply_pricing_to_account_stats"
                  @update:modelValue="form.apply_pricing_to_account_stats = $event"
                />
              </div>
            </div>
          </div>

          <!-- Platform Tab Content -->
          <div
            v-for="(section, sIdx) in form.platforms"
            :key="'tab-' + section.platform"
            v-show="section.enabled && activeTab === section.platform"
            class="space-y-4"
          >
            <!-- Groups -->
            <div>
              <label class="input-label text-xs">
                {{ t('admin.channels.form.groups', 'Associated Groups') }} <span class="text-red-500">*</span>
                <span v-if="section.group_ids.length > 0" class="ml-1 font-normal text-gray-400">
                  ({{ t('admin.channels.form.selectedCount', { count: section.group_ids.length }) }})
                </span>
              </label>
              <div class="max-h-40 overflow-auto rounded-lg border border-gray-200 bg-gray-50 p-2 dark:border-dark-600 dark:bg-dark-900">
                <div v-if="groupsLoading" class="py-2 text-center text-xs text-gray-500">
                  {{ t('common.loading', 'Loading...') }}
                </div>
                <div v-else-if="getGroupsForPlatform(section.platform).length === 0" class="py-2 text-center text-xs text-gray-500">
                  {{ t('admin.channels.form.noGroupsAvailable', 'No groups available') }}
                </div>
                <div v-else class="flex flex-wrap gap-1">
                  <label
                    v-for="group in getGroupsForPlatform(section.platform)"
                    :key="group.id"
                    class="inline-flex cursor-pointer items-center gap-1.5 rounded-md border border-gray-200 px-2 py-1 text-xs transition-colors hover:bg-gray-50 dark:border-dark-600 dark:hover:bg-dark-700"
                    :class="[
                      section.group_ids.includes(group.id) ? 'bg-primary-50 border-primary-300 dark:bg-primary-900/20 dark:border-primary-700' : '',
                      isGroupInOtherChannel(group.id, section.platform) ? 'opacity-40' : ''
                    ]"
                  >
                    <input
                      type="checkbox"
                      :checked="section.group_ids.includes(group.id)"
                      :disabled="isGroupInOtherChannel(group.id, section.platform)"
                      class="h-3 w-3 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                      @change="toggleGroupInSection(sIdx, group.id)"
                    />
                    <span :class="['font-medium', platformTextClass(group.platform)]">{{ group.name }}</span>
                    <span
                      :class="['rounded-full px-1 py-0 text-[10px]', platformBadgeLightClass(group.platform)]"
                    >{{ group.rate_multiplier }}x</span>
                    <span class="text-[10px] text-gray-400">{{ group.account_count || 0 }}</span>
                    <span
                      v-if="isGroupInOtherChannel(group.id, section.platform)"
                      class="text-[10px] text-gray-400"
                    >{{ getGroupInOtherChannelLabel(group.id) }}</span>
                  </label>
                </div>
              </div>
            </div>

            <!-- Web Search Emulation (hidden when global disabled) -->
            <div v-if="(section.platform === 'anthropic' || section.platform === 'openai') && webSearchGlobalEnabled" class="border-t border-gray-200 pt-3 dark:border-dark-600">
              <div class="flex items-center justify-between">
                <div>
                  <label class="text-xs font-medium text-gray-700 dark:text-gray-300">
                    {{ t('admin.channels.form.webSearchEmulation') }}
                  </label>
                  <p class="mt-0.5 text-[11px] text-red-500 dark:text-red-400">
                    {{ t('admin.channels.form.webSearchEmulationHint') }}
                  </p>
                </div>
                <Toggle :disabled="!rolePermissions.canEditAdminField(editingChannel ? 'channel.update' : 'channel.create', 'features_config')" v-model="section.web_search_emulation" />
              </div>
            </div>

            <!-- Codex Image Generation Bridge (OpenAI only) -->
            <div v-if="section.platform === 'openai'" class="border-t border-gray-200 pt-3 dark:border-dark-600">
              <div class="flex items-center justify-between gap-4">
                <div>
                  <label class="text-xs font-medium text-gray-700 dark:text-gray-300">
                    {{ t('admin.channels.form.codexImageGenerationBridge') }}
                  </label>
                  <p class="mt-0.5 text-[11px] text-amber-600 dark:text-amber-400">
                    {{ t('admin.channels.form.codexImageGenerationBridgeHint') }}
                  </p>
                </div>
                <Toggle :disabled="!rolePermissions.canEditAdminField(editingChannel ? 'channel.update' : 'channel.create', 'features_config')" v-model="section.codex_image_generation_bridge" />
              </div>
            </div>

            <!-- Bedrock CC Compatibility (Anthropic only) -->
            <div v-if="section.platform === 'anthropic'" class="border-t border-gray-200 pt-3 dark:border-dark-600">
              <div class="flex items-center justify-between gap-4">
                <div>
                  <label class="text-xs font-medium text-gray-700 dark:text-gray-300">
                    {{ t('admin.channels.form.bedrockCCCompat') }}
                  </label>
                  <p class="mt-0.5 text-[11px] text-amber-600 dark:text-amber-400">
                    {{ t('admin.channels.form.bedrockCCCompatHint') }}
                  </p>
                </div>
                <Toggle :disabled="!rolePermissions.canEditAdminField(editingChannel ? 'channel.update' : 'channel.create', 'features_config')" v-model="section.bedrock_cc_compat" />
              </div>
            </div>

            <!-- Model Mapping -->
            <div>
              <div class="mb-1 flex items-center justify-between">
                <label class="input-label text-xs mb-0">{{ t('admin.channels.form.modelMapping', 'Model Mapping') }}</label>
                <button type="button" @click="addMappingEntry(sIdx)" class="text-xs text-primary-600 hover:text-primary-700">
                  + {{ t('common.add', 'Add') }}
                </button>
              </div>
              <div
                v-if="Object.keys(section.model_mapping).length === 0"
                class="rounded border border-dashed border-gray-300 p-2 text-center text-xs text-gray-400 dark:border-dark-500"
              >
                {{ t('admin.channels.form.noMappingRules', 'No mapping rules. Click "Add" to create one.') }}
              </div>
              <div v-else class="space-y-1">
                <div
                  v-for="(_, srcModel) in section.model_mapping"
                  :key="srcModel"
                  class="flex items-center gap-2"
                >
                  <input
                    :value="srcModel"
                    type="text"
                    class="input flex-1 text-xs"
                    :class="platformTextClass(section.platform)"
                    :placeholder="t('admin.channels.form.mappingSource', 'Source model')"
                    @change="renameMappingKey(sIdx, srcModel, ($event.target as HTMLInputElement).value)"
                  />
                  <span class="text-gray-400 text-xs">→</span>
                  <input
                    :value="section.model_mapping[srcModel]"
                    type="text"
                    class="input flex-1 text-xs"
                    :class="platformTextClass(section.platform)"
                    :placeholder="t('admin.channels.form.mappingTarget', 'Target model')"
                    @input="section.model_mapping[srcModel] = ($event.target as HTMLInputElement).value"
                  />
                  <button
                    type="button"
                    @click="removeMappingEntry(sIdx, srcModel)"
                    class="rounded p-0.5 text-gray-400 hover:text-red-500"
                  >
                    <Icon name="trash" size="sm" />
                  </button>
                </div>
              </div>
            </div>

            <!-- Model Pricing -->
            <div :inert="!rolePermissions.canEditAdminField(editingChannel ? 'channel.update' : 'channel.create', 'model_pricing')">
              <div class="mb-1 flex items-center justify-between">
                <label class="input-label text-xs mb-0">{{ t('admin.channels.form.modelPricing', 'Model Pricing') }}</label>
                <div class="flex items-center gap-2">
                  <button
                    v-if="catalog.supportsPricingModelSync(section.platform)"
                    type="button"
                    @click="syncLatestModels(sIdx)"
                    :disabled="!!syncingPlatform"
                    class="text-xs text-gray-500 hover:text-primary-600 disabled:opacity-50"
                  >
                    {{ syncingPlatform === section.platform ? t('admin.channels.form.syncingModels') : t('admin.channels.form.syncLatestModels') }}
                  </button>
                  <button type="button" @click="addPricingEntry(sIdx)" class="text-xs text-primary-600 hover:text-primary-700">
                    + {{ t('common.add', 'Add') }}
                  </button>
                </div>
              </div>
              <div
                v-if="section.model_pricing.length === 0"
                class="rounded border border-dashed border-gray-300 p-2 text-center text-xs text-gray-400 dark:border-dark-500"
              >
                {{ t('admin.channels.form.noPricingRules', 'No pricing rules yet. Click "Add" to create one.') }}
              </div>
              <div v-else class="space-y-2">
                <PricingEntryCard
                  v-for="(entry, idx) in section.model_pricing"
                  :key="idx"
                  :entry="entry"
                  :platform="section.platform"
                  enable-time-pricing
                  enable-tier-multipliers
                  @update="updatePricingEntry(sIdx, idx, $event)"
                  @remove="removePricingEntry(sIdx, idx)"
                />
              </div>
            </div>

            <AccountStatsPricingRulesPanel v-if="rolePermissions.can('billing.cost.read')" :inert="!rolePermissions.canEditAdminField(editingChannel ? 'channel.update' : 'channel.create', 'account_stats_pricing_rules')" v-model="section.account_stats_pricing_rules" :platform="section.platform" :group-ids="section.group_ids" :groups="allGroups" />
          </div>
        </form>
      </div>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button @click="closeDialog" type="button" :disabled="submitting" class="btn btn-secondary">
            {{ t('common.cancel', 'Cancel') }}
          </button>
          <button
            type="submit"
            form="channel-form"
            :disabled="submitting"
            class="btn btn-primary"
          >
            {{ submitting
              ? t('common.submitting', 'Submitting...')
              : editingChannel
                ? t('common.update', 'Update')
                : t('common.create', 'Create')
            }}
          </button>
        </div>
      </template>
    </BaseDialog>

</template>
<script setup lang="ts">
import AccountStatsPricingRulesPanel from './AccountStatsPricingRulesPanel.vue'
import { channelDraftToAPI, channelToSections, accountStatsRulesToAPI, distributeRulesToPlatforms, type PlatformSection } from '../model/channelDraft'

import { requirePricingPort, pricingNotificationsKey, pricingPermissionsKey, pricingDirectoryKey, pricingCatalogKey, pricingWebSearchKey } from '../ports'
import { usePricingRequest } from '../queries/requestScope'
const rolePermissions = requirePricingPort(pricingPermissionsKey)
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import channelsAPI from '../api/channels'
import type { Channel, CreateChannelRequest, UpdateChannelRequest } from '../api/types'
import type { PricingFormEntry } from '@/features/pricing/model/pricingForm'
import { createDefaultTimePricingForm, findModelConflict, isValidPositiveMultiplier, validateIntervals, validateReasoningEffortMultipliers, validateTimePricing } from '@/features/pricing/model/pricingForm'
import type { AdminGroup, GroupPlatform } from '@/features/access-policy/api/groupTypes';
import { platformTextClass, platformBadgeLightClass } from '@/shared/styles/platformColors'
const catalog = requirePricingPort(pricingCatalogKey)
const catalogPlatformLabel = catalog.platformLabel
const directory = requirePricingPort(pricingDirectoryKey)
const readWebSearchEnabled = requirePricingPort(pricingWebSearchKey)
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import Select from '@/shared/ui/Select.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import PlatformIcon from '@/shared/ui/PlatformIcon.vue'
import Toggle from '@/shared/ui/Toggle.vue'
import PricingEntryCard from '@/features/pricing/components/PricingEntryCard.vue'

const props = defineProps<{ channel: Channel | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const notifications = requirePricingPort(pricingNotificationsKey)

// Web Search global enabled state (loaded once on mount)
const webSearchGlobalEnabled = ref(false)
async function loadWebSearchGlobalState() {
  if (!rolePermissions.isSuperAdmin) return
  const request = webSearchRequest.begin()
  try {
    const enabled = await readWebSearchEnabled(request.signal)
    if (request.isCurrent()) webSearchGlobalEnabled.value = enabled
  } catch (err: unknown) {
    if (!request.isCurrent()) return
    console.warn('Failed to load web search global state:', err)
    webSearchGlobalEnabled.value = false
  }
}

const statusEditOptions = computed(() => [
  { value: 'active', label: t('admin.channels.statusActive', 'Active') },
  { value: 'disabled', label: t('admin.channels.statusDisabled', 'Disabled') }
])

const billingModelSourceOptions = computed(() => [
  { value: 'channel_mapped', label: t('admin.channels.form.billingModelSourceChannelMapped', 'Bill by channel-mapped model') },
  { value: 'requested', label: t('admin.channels.form.billingModelSourceRequested', 'Bill by requested model') },
  { value: 'upstream', label: t('admin.channels.form.billingModelSourceUpstream', 'Bill by final upstream model') },
  { value: 'response_model', label: t('admin.channels.form.billingModelSourceResponse', 'Bill by upstream response model') }
])

// Dialog state
const showDialog = ref(false)
const editingChannel = ref<Channel | null>(null)
const submitting = ref(false)
const invalidated = ref(false)
const activeTab = ref<string>('basic')

// Groups
const allGroups = ref<AdminGroup[]>([])
const groupsLoading = ref(false)

// All channels for group-conflict detection (independent of current page)
const allChannelsForConflict = ref<Channel[]>([])

// Form data
const form = reactive({
  name: '',
  description: '',
  status: 'active',
  restrict_models: false,
  billing_model_source: 'channel_mapped' as string,
  platforms: [] as PlatformSection[],
  apply_pricing_to_account_stats: false,
})



// ── Platform config ──
// 平台清单中的全部具体平台（展示顺序）。
const platformOrder = computed<GroupPlatform[]>(() => catalog.listPlatformIds())
// Composite pricing/mapping may target every concrete schedulable provider.
const compositePlatforms = platformOrder

// ── Helpers ──
const activePlatforms = computed(() => form.platforms.filter(s => s.enabled).map(s => s.platform))

function addPlatformSection(platform: GroupPlatform) {
  form.platforms.push({
    platform,
    enabled: true,
    collapsed: false,
    group_ids: [],
    model_mapping: {},
    model_pricing: [],
    web_search_emulation: false,
    codex_image_generation_bridge: false,
    bedrock_cc_compat: false,
    account_stats_pricing_rules: [],
  })
}

function togglePlatform(platform: GroupPlatform) {
  const section = form.platforms.find(s => s.platform === platform)
  if (section) {
    section.enabled = !section.enabled
    if (!section.enabled && activeTab.value === platform) {
      activeTab.value = 'basic'
    }
  } else {
    addPlatformSection(platform)
  }
}

function getGroupsForPlatform(platform: GroupPlatform): AdminGroup[] {
  return allGroups.value.filter(
    g => g.platform === platform || (g.platform === 'composite' && compositePlatforms.value.includes(platform))
  )
}

// ── Group helpers ──
const groupToChannelMap = computed(() => {
  const map = new Map<number, Channel>()
  for (const ch of allChannelsForConflict.value) {
    if (editingChannel.value && ch.id === editingChannel.value.id) continue
    for (const gid of ch.group_ids || []) {
      map.set(gid, ch)
    }
  }
  return map
})

function isGroupInOtherChannel(groupId: number, _platform: string): boolean {
  return groupToChannelMap.value.has(groupId)
}

function getGroupChannelName(groupId: number): string {
  return groupToChannelMap.value.get(groupId)?.name || ''
}

function getGroupInOtherChannelLabel(groupId: number): string {
  const name = getGroupChannelName(groupId)
  return t('admin.channels.form.inOtherChannel', { name }, `In "${name}"`)
}

function toggleGroupInSection(sectionIdx: number, groupId: number) {
  const section = form.platforms[sectionIdx]
  const idx = section.group_ids.indexOf(groupId)
  if (idx >= 0) {
    section.group_ids.splice(idx, 1)
  } else {
    section.group_ids.push(groupId)
  }
}

// ── Pricing helpers ──
function addPricingEntry(sectionIdx: number) {
  form.platforms[sectionIdx].model_pricing.push({
    models: [],
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_write_1h_price: null,
    cache_read_price: null,
    fast_multiplier: null,
    flex_multiplier: null,
    reasoning_effort_multipliers: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    time_pricing: createDefaultTimePricingForm()
  })
}

const syncingPlatform = ref<string | null>(null)

async function syncLatestModels(sectionIdx: number) {
  const section = form.platforms[sectionIdx]
  if (!section || syncingPlatform.value || !catalog.supportsPricingModelSync(section.platform)) return
  const platform = section.platform
  const request = syncRequest.begin()
  syncingPlatform.value = platform
  try {
    const result = await channelsAPI.syncPricingModels(platform, request.signal)
    if (!request.isCurrent()) return
    // Collect all model names already present in this platform's pricing entries
    const existingModels = new Set<string>()
    for (const entry of section.model_pricing) {
      for (const m of entry.models) existingModels.add(m)
    }
    const newModels = result.models.filter(m => !existingModels.has(m))
    if (newModels.length === 0) {
      if (!request.isCurrent()) return
      notifications.showSuccess(t('admin.channels.form.syncModelsAlreadyUpToDate'))
      return
    }
    // Add new models as a single new pricing entry (user fills in prices)
    section.model_pricing.push({
      models: newModels,
      billing_mode: 'token',
      input_price: null,
      output_price: null,
      cache_write_price: null,
      cache_write_1h_price: null,
      cache_read_price: null,
      fast_multiplier: null,
      flex_multiplier: null,
      reasoning_effort_multipliers: null,
      image_input_price: null,
      image_output_price: null,
      per_request_price: null,
      intervals: [],
      time_pricing: createDefaultTimePricingForm()
    })
    notifications.showSuccess(t('admin.channels.form.syncModelsSuccess', { count: newModels.length }))
  } catch (error) {
    if (!request.isCurrent()) return
    notifications.showError(extractApiErrorMessage(error, t('admin.channels.form.syncModelsError')))
  } finally {
    if (request.isCurrent()) syncingPlatform.value = null
  }
}

function updatePricingEntry(sectionIdx: number, idx: number, updated: PricingFormEntry) {
  form.platforms[sectionIdx].model_pricing.splice(idx, 1, updated)
}

function removePricingEntry(sectionIdx: number, idx: number) {
  form.platforms[sectionIdx].model_pricing.splice(idx, 1)
}

// ── Model Mapping helpers ──
function addMappingEntry(sectionIdx: number) {
  const mapping = form.platforms[sectionIdx].model_mapping
  let key = ''
  let i = 1
  while (key === '' || key in mapping) {
    key = `model-${i}`
    i++
  }
  mapping[key] = ''
}

function removeMappingEntry(sectionIdx: number, key: string) {
  delete form.platforms[sectionIdx].model_mapping[key]
}

function renameMappingKey(sectionIdx: number, oldKey: string, newKey: string) {
  newKey = newKey.trim()
  if (!newKey || newKey === oldKey) return
  const mapping = form.platforms[sectionIdx].model_mapping
  if (newKey in mapping) return
  const value = mapping[oldKey]
  delete mapping[oldKey]
  mapping[newKey] = value
}

// ── Account Stats Pricing helpers ──
// ── Load data ──
async function loadGroups() {
  const request = groupsRequest.begin()
  groupsLoading.value = true
  try {
    const data = await directory.listGroups(request.signal)
    if (!request.isCurrent()) return false
    allGroups.value = data
    return true
  } catch (error) {
    if (!request.isCurrent()) return
    notifications.showError(extractApiErrorMessage(error, t('admin.channels.loadError')))
    return false
  } finally {
    if (request.isCurrent()) groupsLoading.value = false
  }
}

async function loadAllChannelsForConflict() {
  const request = conflictRequest.begin()
  try {
    const response = await channelsAPI.list(1, 1000, undefined, { signal: request.signal })
    if (!request.isCurrent()) return false
    allChannelsForConflict.value = response.items || []
    return true
  } catch (error) {
    if (!request.isCurrent()) return
    // Fallback to current page data
    notifications.showError(extractApiErrorMessage(error, t('admin.channels.loadError')))
    allChannelsForConflict.value = []
    return false
  }
}

function resetForm() {
  form.name = ''
  form.description = ''
  form.status = 'active'
  form.restrict_models = false
  form.billing_model_source = 'channel_mapped'
  form.platforms = []
  form.apply_pricing_to_account_stats = false
  activeTab.value = 'basic'


}

async function openCreateDialog() {
  const request = editorRequest.begin()
  editingChannel.value = null
  resetForm()
  const ready = await Promise.all([loadGroups(), loadAllChannelsForConflict()])
  if (!request.isCurrent()) return
  if (!ready.every(Boolean)) { closeDialog(); return }
  if (request.isCurrent()) showDialog.value = true
}

async function openEditDialog(channel: Channel) {
  const request = editorRequest.begin()
  editingChannel.value = channel
  form.name = channel.name
  form.description = channel.description || ''
  form.status = channel.status
  form.restrict_models = channel.restrict_models || false
  form.billing_model_source = channel.billing_model_source || 'channel_mapped'
  form.apply_pricing_to_account_stats = channel.apply_pricing_to_account_stats || false
  // Must load groups first so apiToForm can map groupID → platform
  const ready = await Promise.all([loadGroups(), loadAllChannelsForConflict()])
  if (!request.isCurrent()) return
  if (!ready.every(Boolean)) { closeDialog(); return }
  if (!request.isCurrent()) return
  form.platforms = channelToSections(channel, allGroups.value, platformOrder.value)

  // Distribute channel-level rules into per-platform sections
  distributeRulesToPlatforms(channel.account_stats_pricing_rules || [], allGroups.value, form.platforms)

  // Populate ruleAccountNameCache for existing rule accounts


  if (request.isCurrent()) showDialog.value = true
}

/** Distribute flat channel-level rules into the matching platform section based on group_ids */
function closeDialog() {
  if (submitting.value) return
  invalidateEditor()
}

function invalidateEditor() {
  invalidated.value = true
  saveRequest.cancel()
  syncRequest.cancel()
  groupsRequest.cancel()
  conflictRequest.cancel()
  editorRequest.cancel()
  showDialog.value = false
  editingChannel.value = null
  resetForm()
}

async function handleSubmit() {
  if (invalidated.value) return
  if (!rolePermissions.can('channels.manage')) return
  if (submitting.value) return
  if (!form.name.trim()) {
    notifications.showError(t('admin.channels.nameRequired', 'Please enter a channel name'))
    return
  }

  // Check for pricing entries with empty models (would be silently skipped)
  for (const section of form.platforms.filter(s => s.enabled)) {
    if (section.group_ids.length === 0) {
      const platformLabel = t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform))
      notifications.showError(t('admin.channels.noGroupsSelected', { platform: platformLabel }))
      activeTab.value = section.platform
      return
    }
    for (const entry of section.model_pricing) {
      if (entry.models.length === 0) {
        const platformLabel = t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform))
        notifications.showError(t('admin.channels.emptyModelsInPricing', { platform: platformLabel }))
        activeTab.value = section.platform
        return
      }
    }
  }

  // Check model pattern conflicts per platform (duplicate / wildcard overlap)
  for (const section of form.platforms.filter(s => s.enabled)) {
    // Collect all pricing models for this platform
    const allModels: string[] = []
    for (const entry of section.model_pricing) {
      allModels.push(...entry.models)
    }
    const pricingConflict = findModelConflict(allModels)
    if (pricingConflict) {
      notifications.showError(
        t('admin.channels.modelConflict',
          { model1: pricingConflict[0], model2: pricingConflict[1] })
      )
      activeTab.value = section.platform
      return
    }
    // Check model mapping source pattern conflicts
    const mappingKeys = Object.keys(section.model_mapping)
    if (mappingKeys.length > 0) {
      const mappingConflict = findModelConflict(mappingKeys)
      if (mappingConflict) {
        notifications.showError(
          t('admin.channels.mappingConflict',
            { model1: mappingConflict[0], model2: mappingConflict[1] })
        )
        activeTab.value = section.platform
        return
      }
    }
  }

  // 校验 per_request/image 模式必须有价格 (只校验启用的平台)
  for (const section of form.platforms.filter(s => s.enabled)) {
    for (const entry of section.model_pricing) {
      if (entry.models.length === 0) continue
      if ((entry.billing_mode === 'per_request' || entry.billing_mode === 'image') &&
          (entry.per_request_price == null || entry.per_request_price === '') &&
          (!entry.intervals || entry.intervals.length === 0)) {
        notifications.showError(t('admin.channels.form.perRequestPriceRequired'))
        return
      }
    }
  }

  // 思考等级倍率同时适用于渠道计价和独立的账号统计计价规则。
  for (const section of form.platforms.filter(s => s.enabled)) {
    const entries = [
      ...section.model_pricing,
      ...section.account_stats_pricing_rules.flatMap(rule => rule.pricing),
    ]
    for (const entry of entries) {
      const error = validateReasoningEffortMultipliers(entry.reasoning_effort_multipliers, t)
      if (!error) continue
      const platformLabel = t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform))
      const modelLabel = entry.models.join(', ') || t('admin.channels.form.unnamed')
      notifications.showError(`${platformLabel} - ${modelLabel}: ${error}`)
      activeTab.value = section.platform
      return
    }
  }

  // 校验区间合法性（范围、重叠等）
  for (const section of form.platforms.filter(s => s.enabled)) {
    for (const entry of section.model_pricing) {
      if (!isValidPositiveMultiplier(entry.fast_multiplier) ||
          !isValidPositiveMultiplier(entry.flex_multiplier)) {
        const platformLabel = t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform))
        const modelLabel = entry.models.join(', ') || t('admin.channels.form.unnamed')
        notifications.showError(`${platformLabel} - ${modelLabel}: ${t('admin.channels.form.multiplierPositive')}`)
        activeTab.value = section.platform
        return
      }
      if (!entry.intervals || entry.intervals.length === 0) continue
      const intervalErr = validateIntervals(entry.intervals, entry.billing_mode, t)
      if (intervalErr) {
        const platformLabel = t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform))
        const modelLabel = entry.models.join(', ') || t('admin.channels.form.unnamed')
        notifications.showError(`${platformLabel} - ${modelLabel}: ${intervalErr}`)
        activeTab.value = section.platform
        return
      }
    }
  }

  // 校验时间段定价，并切换到对应平台便于修正
  for (const section of form.platforms.filter(s => s.enabled)) {
    for (const entry of section.model_pricing) {
      const timePricingError = validateTimePricing(entry.time_pricing, t)
      if (timePricingError) {
        const platformLabel = t('admin.groups.platforms.' + section.platform, catalogPlatformLabel(section.platform))
        const modelLabel = entry.models.join(', ') || t('admin.channels.form.unnamed')
        notifications.showError(`${platformLabel} - ${modelLabel}: ${timePricingError}`)
        activeTab.value = section.platform
        return
      }
    }
  }

  const { group_ids, model_pricing, model_mapping, features_config } = channelDraftToAPI(form.platforms, editingChannel.value)

  const request = saveRequest.begin()
  const target = editingChannel.value
  const targetId = target?.id ?? null
  const current = () => request.isCurrent() && !invalidated.value
    && rolePermissions.can('channels.manage') && (props.channel?.id ?? null) === targetId
  submitting.value = true
  try {
    if (target) {
      const req: UpdateChannelRequest = {
        name: form.name.trim(),
        description: form.description.trim() || undefined,
        status: form.status,
        group_ids,
        model_pricing,
        model_mapping: Object.keys(model_mapping).length > 0 ? model_mapping : {},
        billing_model_source: form.billing_model_source,
        restrict_models: form.restrict_models,
        features_config,
        apply_pricing_to_account_stats: form.apply_pricing_to_account_stats,
        account_stats_pricing_rules: accountStatsRulesToAPI(form.platforms)
      }
      await channelsAPI.update(target.id, rolePermissions.editableAdminFields("channel.update", req))
      if (!current()) return
      notifications.showSuccess(t('admin.channels.updateSuccess', 'Channel updated'))
    } else {
      const req: CreateChannelRequest = {
        name: form.name.trim(),
        description: form.description.trim() || undefined,
        group_ids,
        model_pricing,
        model_mapping: Object.keys(model_mapping).length > 0 ? model_mapping : {},
        billing_model_source: form.billing_model_source,
        restrict_models: form.restrict_models,
        features_config,
        apply_pricing_to_account_stats: form.apply_pricing_to_account_stats,
        account_stats_pricing_rules: accountStatsRulesToAPI(form.platforms)
      }
      await channelsAPI.create(rolePermissions.editableAdminFields("channel.create", req))
      if (!current()) return
      notifications.showSuccess(t('admin.channels.createSuccess', 'Channel created'))
    }
    if (!current()) return
    submitting.value = false
    closeDialog()
    emit('saved')
  } catch (error: unknown) {
    if (!current()) return
    notifications.showError(extractApiErrorMessage(error, target
      ? t('admin.channels.updateError', 'Failed to update channel')
      : t('admin.channels.createError', 'Failed to create channel')))
  } finally {
    if (current()) submitting.value = false
  }
}

// ── Toggle status ──

const groupsRequest = usePricingRequest(() => { allGroups.value = []; groupsLoading.value = false })
const conflictRequest = usePricingRequest(() => { allChannelsForConflict.value = [] })
const webSearchRequest = usePricingRequest(() => { webSearchGlobalEnabled.value = false })
const syncRequest = usePricingRequest(() => { syncingPlatform.value = null })
const saveRequest = usePricingRequest(() => { submitting.value = false })
const editorRequest = usePricingRequest(() => { showDialog.value = false; emit('close') })
watch(editorRequest.scope, invalidateEditor, { flush: 'sync' })
watch([() => props.channel?.id ?? null, () => rolePermissions.can('channels.manage')], invalidateEditor, { flush: 'sync' })

onMounted(async () => {
  const startup = editorRequest.begin()
  await loadWebSearchGlobalState()
  if (!startup.isCurrent()) return
  if (props.channel) await openEditDialog(props.channel)
  else await openCreateDialog()
})

</script>
<style scoped>
.channel-dialog-body {
  display: flex;
  flex-direction: column;
  height: 70vh;
  min-height: 400px;
}

.channel-tab {
  @apply flex items-center gap-1.5 px-3 py-2.5 text-sm font-medium border-b-2 transition-colors whitespace-nowrap;
}

.channel-tab-active {
  @apply border-primary-600 text-primary-600 dark:border-primary-400 dark:text-primary-400;
}

.channel-tab-inactive {
  @apply border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300 dark:text-gray-400 dark:hover:text-gray-300;
}
</style>
