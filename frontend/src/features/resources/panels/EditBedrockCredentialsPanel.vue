<script setup lang="ts">
import EditBedrockRetryPoolPanel from './EditBedrockRetryPoolPanel.vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
import ModelWhitelistSelector from '@/features/resources/components/ModelWhitelistSelector.vue'
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { Props, ModelMapping } from '../model/editEditorTypes'



const props = defineProps<{
  account: NonNullable<Props['account']>
  isBedrockAPIKeyMode: UnwrapRef<ComputedRef<boolean>>
  allowAuthorize: NonNullable<Props['allowAuthorize']>
  editBedrockAccessKeyId: UnwrapRef<Ref<string, string>>
  editBedrockSecretAccessKey: UnwrapRef<Ref<string, string>>
  editBedrockSessionToken: UnwrapRef<Ref<string, string>>
  editBedrockApiKeyValue: UnwrapRef<Ref<string, string>>
  allowTransportEdit: NonNullable<Props['allowTransportEdit']>
  editBedrockRegion: UnwrapRef<Ref<string, string>>
  editBedrockForceGlobal: UnwrapRef<Ref<boolean, boolean>>
  modelRestrictionMode: UnwrapRef<Ref<"whitelist" | "mapping", "whitelist" | "mapping">>
  allowedModels: UnwrapRef<Ref<string[], string[]>>
  modelMappings: UnwrapRef<Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>>
  getModelMappingKey: UnwrapRef<(item: ModelMapping) => string>
  bedrockPresets: UnwrapRef<ComputedRef<{ label: string; from: string; to: string; color: string }[]>>
  poolModeEnabled: UnwrapRef<Ref<boolean, boolean>>
  poolModeRetryCount: UnwrapRef<Ref<number, number>>
  MAX_POOL_MODE_RETRY_COUNT: UnwrapRef<10>
  DEFAULT_POOL_MODE_RETRY_COUNT: UnwrapRef<3>
  poolModeRetryStatusCodesInput: UnwrapRef<Ref<string, string>>
  DEFAULT_POOL_MODE_RETRY_STATUS_CODES: UnwrapRef<number[]>
}>()


const emit = defineEmits<{
  'update:editBedrockAccessKeyId': [value: UnwrapRef<Ref<string, string>>]
  'update:editBedrockSecretAccessKey': [value: UnwrapRef<Ref<string, string>>]
  'update:editBedrockSessionToken': [value: UnwrapRef<Ref<string, string>>]
  'update:editBedrockApiKeyValue': [value: UnwrapRef<Ref<string, string>>]
  'update:editBedrockRegion': [value: UnwrapRef<Ref<string, string>>]
  'update:editBedrockForceGlobal': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:modelRestrictionMode': [value: UnwrapRef<Ref<"whitelist" | "mapping", "whitelist" | "mapping">>]
  'update:allowedModels': [value: UnwrapRef<Ref<string[], string[]>>]
  'update:poolModeEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:poolModeRetryCount': [value: UnwrapRef<Ref<number, number>>]
  'update:poolModeRetryStatusCodesInput': [value: UnwrapRef<Ref<string, string>>]
}>()


const { t } = useI18n()


const account = computed(() => props.account)


const isBedrockAPIKeyMode = computed(() => props.isBedrockAPIKeyMode)


const allowAuthorize = computed(() => props.allowAuthorize)


const editBedrockAccessKeyId = usePanelField(() => props.editBedrockAccessKeyId, value => emit('update:editBedrockAccessKeyId', value))


const editBedrockSecretAccessKey = usePanelField(() => props.editBedrockSecretAccessKey, value => emit('update:editBedrockSecretAccessKey', value))


const editBedrockSessionToken = usePanelField(() => props.editBedrockSessionToken, value => emit('update:editBedrockSessionToken', value))


const editBedrockApiKeyValue = usePanelField(() => props.editBedrockApiKeyValue, value => emit('update:editBedrockApiKeyValue', value))


const allowTransportEdit = computed(() => props.allowTransportEdit)


const editBedrockRegion = usePanelField(() => props.editBedrockRegion, value => emit('update:editBedrockRegion', value))


const editBedrockForceGlobal = usePanelField(() => props.editBedrockForceGlobal, value => emit('update:editBedrockForceGlobal', value))


const modelRestrictionMode = usePanelField(() => props.modelRestrictionMode, value => emit('update:modelRestrictionMode', value))


const allowedModels = usePanelField(() => props.allowedModels, value => emit('update:allowedModels', value))


const modelMappings = computed(() => props.modelMappings)


const getModelMappingKey = computed(() => props.getModelMappingKey)


const bedrockPresets = computed(() => props.bedrockPresets)


const poolModeEnabled = usePanelField(() => props.poolModeEnabled, value => emit('update:poolModeEnabled', value))


const poolModeRetryCount = usePanelField(() => props.poolModeRetryCount, value => emit('update:poolModeRetryCount', value))


const MAX_POOL_MODE_RETRY_COUNT = computed(() => props.MAX_POOL_MODE_RETRY_COUNT)


const DEFAULT_POOL_MODE_RETRY_COUNT = computed(() => props.DEFAULT_POOL_MODE_RETRY_COUNT)


const poolModeRetryStatusCodesInput = usePanelField(() => props.poolModeRetryStatusCodesInput, value => emit('update:poolModeRetryStatusCodesInput', value))


const DEFAULT_POOL_MODE_RETRY_STATUS_CODES = computed(() => props.DEFAULT_POOL_MODE_RETRY_STATUS_CODES)
</script>
<template>

<template v-if="account">
<div v-if="account.type === 'bedrock'" class="space-y-4">
        <!-- SigV4 fields -->
        <template v-if="!isBedrockAPIKeyMode">
          <div>
            <label class="input-label">{{ t('admin.accounts.bedrockAccessKeyId') }}</label>
            <input
 :disabled="!allowAuthorize"              v-model="editBedrockAccessKeyId"
              type="text"
              class="input font-mono"
              placeholder="AKIA..."
            />
          </div>
          <div>
            <label class="input-label">{{ t('admin.accounts.bedrockSecretAccessKey') }}</label>
            <input
 :disabled="!allowAuthorize"              v-model="editBedrockSecretAccessKey"
              type="password"
              class="input font-mono"
              :placeholder="t('admin.accounts.bedrockSecretKeyLeaveEmpty')"
            />
            <p class="input-hint">{{ t('admin.accounts.bedrockSecretKeyLeaveEmpty') }}</p>
          </div>
          <div>
            <label class="input-label">{{ t('admin.accounts.bedrockSessionToken') }}</label>
            <input
 :disabled="!allowAuthorize"              v-model="editBedrockSessionToken"
              type="password"
              class="input font-mono"
              :placeholder="t('admin.accounts.bedrockSecretKeyLeaveEmpty')"
            />
            <p class="input-hint">{{ t('admin.accounts.bedrockSessionTokenHint') }}</p>
          </div>
        </template>

        <!-- API Key field -->
        <div v-if="isBedrockAPIKeyMode">
          <label class="input-label">{{ t('admin.accounts.bedrockApiKeyInput') }}</label>
          <input
 :disabled="!allowAuthorize"            v-model="editBedrockApiKeyValue"
            type="password"
            class="input font-mono"
            :placeholder="t('admin.accounts.bedrockApiKeyLeaveEmpty')"
          />
          <p class="input-hint">{{ t('admin.accounts.bedrockApiKeyLeaveEmpty') }}</p>
        </div>

        <!-- Shared: Region -->
        <div>
          <label class="input-label">{{ t('admin.accounts.bedrockRegion') }}</label>
          <input
 :disabled="!allowTransportEdit"            v-model="editBedrockRegion"
            type="text"
            class="input"
            placeholder="us-east-1"
          />
          <p class="input-hint">{{ t('admin.accounts.bedrockRegionHint') }}</p>
        </div>

        <!-- Shared: Force Global -->
        <div>
          <label class="flex items-center gap-2 cursor-pointer">
            <input
              v-model="editBedrockForceGlobal"
              type="checkbox"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.bedrockForceGlobal') }}</span>
          </label>
          <p class="input-hint mt-1">{{ t('admin.accounts.bedrockForceGlobalHint') }}</p>
        </div>

        <!-- Model Restriction for Bedrock -->
        <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
          <label class="input-label">{{ t('admin.accounts.modelRestriction') }}</label>

          <!-- Mode Toggle -->
          <div class="mb-4 flex gap-2">
            <button
              type="button"
              @click="modelRestrictionMode = 'whitelist'"
              :class="[
                'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                modelRestrictionMode === 'whitelist'
                  ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
            >
              {{ t('admin.accounts.modelWhitelist') }}
            </button>
            <button
              type="button"
              @click="modelRestrictionMode = 'mapping'"
              :class="[
                'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                modelRestrictionMode === 'mapping'
                  ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
            >
              {{ t('admin.accounts.modelMapping') }}
            </button>
          </div>

          <!-- Whitelist Mode -->
          <div v-if="modelRestrictionMode === 'whitelist'">
            <ModelWhitelistSelector v-model="allowedModels" platform="anthropic" />
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.selectedModels', { count: allowedModels.length }) }}
              <span v-if="allowedModels.length === 0 && modelMappings.length === 0">{{ t('admin.accounts.supportsAllModels') }}</span>
            </p>
          </div>

          <!-- Mapping Mode -->
          <div v-else class="space-y-3">
            <div v-for="(mapping, index) in modelMappings" :key="getModelMappingKey(mapping)" class="flex items-center gap-2">
              <input v-model="mapping.from" type="text" class="input flex-1" :placeholder="t('admin.accounts.fromModel')" />
              <span class="text-gray-400">→</span>
              <input v-model="mapping.to" type="text" class="input flex-1" :placeholder="t('admin.accounts.toModel')" />
              <button type="button" @click="modelMappings.splice(index, 1)" class="text-red-500 hover:text-red-700">
                <Icon name="trash" size="sm" />
              </button>
            </div>
            <button type="button" @click="modelMappings.push({ from: '', to: '' })" class="btn btn-secondary text-sm">
              + {{ t('admin.accounts.addMapping') }}
            </button>
            <!-- Bedrock Preset Mappings -->
            <div class="flex flex-wrap gap-2">
              <button
                v-for="preset in bedrockPresets"
                :key="preset.from"
                type="button"
                @click="modelMappings.push({ from: preset.from, to: preset.to })"
                :class="['rounded-lg px-3 py-1 text-xs transition-colors', preset.color]"
              >
                + {{ preset.label }}
              </button>
            </div>
          </div>
        </div>

        <!-- Pool Mode Section for Bedrock -->
        <EditBedrockRetryPoolPanel
  :poolModeEnabled="poolModeEnabled"
  :poolModeRetryCount="poolModeRetryCount"
  :MAX_POOL_MODE_RETRY_COUNT="MAX_POOL_MODE_RETRY_COUNT"
  :DEFAULT_POOL_MODE_RETRY_COUNT="DEFAULT_POOL_MODE_RETRY_COUNT"
  :poolModeRetryStatusCodesInput="poolModeRetryStatusCodesInput"
  :DEFAULT_POOL_MODE_RETRY_STATUS_CODES="DEFAULT_POOL_MODE_RETRY_STATUS_CODES"
  @update:poolModeEnabled="poolModeEnabled = $event"
  @update:poolModeRetryCount="poolModeRetryCount = $event"
  @update:poolModeRetryStatusCodesInput="poolModeRetryStatusCodesInput = $event"
/>
      </div>
</template>

</template>
