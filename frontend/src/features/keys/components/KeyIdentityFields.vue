<script setup lang="ts">
import { computed, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import { keyDraftField, type KeyDraft } from '../model/keyDraft'
import { watch, ref } from 'vue'
import Select from '@/shared/ui/Select.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { useKeysServices } from '../services'
import PlatformIcon from '@/shared/ui/PlatformIcon.vue'
import { platformBadgeLightClass } from '@/shared/styles/platformColors'
import { KEY_GROUP_PROVIDERS, KEY_GROUP_PROVIDER_ICONS, getKeyGroupProvider, type KeyGroupProvider } from '@/features/keys/model/keyGroupProviders'
import { customKeyErrorKey } from '../model/keyDraft'
import type { KeyGroupOption } from '../queries/useKeyGroups'
const props = defineProps<{ editing: boolean; groupOptions: KeyGroupOption[] }>()
const { t } = useI18n()
const { GroupBadge, GroupOptionItem } = useKeysServices().components
const draft = defineModel<KeyDraft>({ required: true })
const formData = reactive({
  custom_key: keyDraftField(draft, 'custom_key'),
  group_id: keyDraftField(draft, 'group_id'),
  name: keyDraftField(draft, 'name'),
  status: keyDraftField(draft, 'status'),
  use_custom_key: keyDraftField(draft, 'use_custom_key'),
})
const showEditModal = computed(() => props.editing)
const createProvider = ref<KeyGroupProvider>('anthropic')
const createProviderOptions = computed(() => KEY_GROUP_PROVIDERS.map(value => ({
  value, label: t('keys.providers.' + value), count: props.groupOptions.filter(group => getKeyGroupProvider(group.platform) === value).length
})))
const formGroupOptions = computed(() => props.editing ? props.groupOptions : props.groupOptions.filter(group => getKeyGroupProvider(group.platform) === createProvider.value))
function matchingGroupOptions(value: unknown): KeyGroupOption[] { return props.groupOptions.filter(group => group.value === value) }
function selectCreateProvider(provider: KeyGroupProvider): void {
  if (createProvider.value === provider) return
  createProvider.value = provider; formData.group_id = null
}
watch(createProviderOptions, providers => {
  if (props.editing) return
  if (!providers.some(provider => provider.value === createProvider.value && provider.count > 0)) selectCreateProvider(providers.find(provider => provider.count > 0)?.value ?? 'anthropic')
  if (!formGroupOptions.value.some(group => group.value === formData.group_id)) formData.group_id = null
}, { immediate: true })
const customKeyError = computed(() => { const key = customKeyErrorKey(draft.value); return key ? t(key) : '' })
const statusOptions = computed(() => [{ value: 'active', label: t('common.active') }, { value: 'inactive', label: t('common.inactive') }])
</script>
<template>
<div>
          <label class="input-label">{{ t('keys.nameLabel') }}</label>
          <input
            v-model="formData.name"
            type="text"
            required
            class="input"
            :placeholder="t('keys.namePlaceholder')"
            data-tour="key-form-name"
          />
        </div>

        <fieldset v-if="!showEditModal" data-tour="key-form-provider">
          <legend class="input-label">{{ t('keys.providerLabel') }}</legend>
          <div class="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <label
              v-for="provider in createProviderOptions"
              :key="provider.value"
              class="relative min-w-0"
              :class="provider.count === 0 ? 'cursor-not-allowed' : 'cursor-pointer'"
            >
              <input
                type="radio"
                name="key-provider"
                :value="provider.value"
                :checked="createProvider === provider.value"
                :disabled="provider.count === 0"
                class="peer sr-only"
                @change="selectCreateProvider(provider.value)"
              />
              <span
                class="flex h-full flex-col items-center gap-2 rounded-xl border border-gray-200 bg-white px-2 py-3 text-center transition-colors peer-checked:border-primary-500 peer-checked:bg-primary-50/60 peer-checked:ring-1 peer-checked:ring-primary-500 peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-primary-500 peer-disabled:opacity-40 dark:border-dark-600 dark:bg-dark-800 dark:peer-checked:border-primary-500 dark:peer-checked:bg-primary-500/10"
                :class="provider.count > 0 && 'hover:border-primary-300 dark:hover:border-primary-700'"
              >
                <span class="flex h-8 items-center justify-center gap-1.5" aria-hidden="true">
                  <span
                    v-for="platform in KEY_GROUP_PROVIDER_ICONS[provider.value]"
                    :key="platform"
                    class="flex h-8 w-8 items-center justify-center rounded-lg"
                    :class="platformBadgeLightClass(platform)"
                  >
                    <PlatformIcon :platform="platform" size="lg" />
                  </span>
                </span>
                <span class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ provider.label }}</span>
              </span>
              <span
                v-if="createProvider === provider.value"
                class="absolute right-1.5 top-1.5 flex h-4 w-4 items-center justify-center rounded-full bg-primary-500 text-white"
                aria-hidden="true"
              >
                <Icon name="check" size="xs" :stroke-width="3" />
              </span>
            </label>
          </div>
          <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400" aria-live="polite">
            {{ groupOptions.length === 0 ? t('common.noGroupsAvailable') : t(`keys.providerHints.${createProvider}`) }}
          </p>
        </fieldset>

        <div>
          <label class="input-label" for="key-form-group">{{ t('keys.groupLabel') }}</label>
          <Select
            :key="showEditModal ? 'edit' : createProvider"
            id="key-form-group"
            :aria-label="t('keys.groupLabel')"
            v-model="formData.group_id"
            :options="formGroupOptions"
            :placeholder="t('keys.selectGroup')"
            :empty-text="t('common.noGroupsAvailable')"
            :searchable="true"
            :search-placeholder="t('keys.searchGroup')"
            data-tour="key-form-group"
          >
            <template #selected="{ option }">
              <template v-if="option"><GroupBadge v-for="group in matchingGroupOptions(option.value)" :key="group.value" :name="group.label" :platform="group.platform" :subscription-type="group.subscriptionType" :rate-multiplier="group.rate" :user-rate-multiplier="group.userRate" :peak-rate-enabled="group.peakRateEnabled" :peak-start="group.peakStart" :peak-end="group.peakEnd" :peak-rate-multiplier="group.peakRateMultiplier" /></template>
              <span v-else class="text-gray-400">{{ t('keys.selectGroup') }}</span>
            </template>
            <template #option="{ option, selected }"><GroupOptionItem v-for="group in matchingGroupOptions(option.value)" :key="group.value" :name="group.label" :platform="group.platform" :subscription-type="group.subscriptionType" :rate-multiplier="group.rate" :user-rate-multiplier="group.userRate" :peak-rate-enabled="group.peakRateEnabled" :peak-start="group.peakStart" :peak-end="group.peakEnd" :peak-rate-multiplier="group.peakRateMultiplier" :description="group.description" :selected="selected" /></template>
          </Select>
        </div>

        <!-- Custom Key Section (only for create) -->
        <div v-if="!showEditModal" class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.customKeyLabel') }}</label>
            <button
              type="button"
              @click="formData.use_custom_key = !formData.use_custom_key"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.use_custom_key ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  formData.use_custom_key ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <div v-if="formData.use_custom_key">
            <input
              v-model="formData.custom_key"
              type="text"
              class="input font-mono"
              :placeholder="t('keys.customKeyPlaceholder')"
              :class="{ 'border-red-500 dark:border-red-500': customKeyError }"
            />
            <p v-if="customKeyError" class="mt-1 text-sm text-red-500">{{ customKeyError }}</p>
            <p v-else class="input-hint">{{ t('keys.customKeyHint') }}</p>
          </div>
        </div>

        <div v-if="showEditModal">
          <label class="input-label">{{ t('keys.statusLabel') }}</label>
          <Select
            v-model="formData.status"
            :options="statusOptions"
            :placeholder="t('keys.selectStatus')"
          />
        </div>


</template>
