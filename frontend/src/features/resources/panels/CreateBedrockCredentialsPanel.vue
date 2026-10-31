<script setup lang="ts">
import CreateBedrockModelsPanel from './CreateBedrockModelsPanel.vue'
import CreateBedrockRetryPoolPanel from './CreateBedrockRetryPoolPanel.vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { ModelMapping, CreateResourceForm } from '../model/createEditorTypes'



const props = defineProps<{
  form: Pick<CreateResourceForm, "platform">
  accountCategory: UnwrapRef<Ref<"apikey" | "bedrock" | "service_account" | "oauth-based", "apikey" | "bedrock" | "service_account" | "oauth-based">>
  bedrockAuthMode: UnwrapRef<Ref<"apikey" | "sigv4", "apikey" | "sigv4">>
  bedrockAccessKeyId: UnwrapRef<Ref<string, string>>
  bedrockSecretAccessKey: UnwrapRef<Ref<string, string>>
  bedrockSessionToken: UnwrapRef<Ref<string, string>>
  bedrockApiKeyValue: UnwrapRef<Ref<string, string>>
  bedrockRegion: UnwrapRef<Ref<string, string>>
  bedrockForceGlobal: UnwrapRef<Ref<boolean, boolean>>
  modelRestrictionMode: UnwrapRef<Ref<"whitelist" | "mapping", "whitelist" | "mapping">>
  allowedModels: UnwrapRef<Ref<string[], string[]>>
  modelMappings: UnwrapRef<Ref<{ from: string; to: string }[], ModelMapping[] | { from: string; to: string }[]>>
  bedrockPresets: UnwrapRef<ComputedRef<{ label: string; from: string; to: string; color: string }[]>>
  addPresetMapping: UnwrapRef<(from: string, to: string) => void>
  poolModeEnabled: UnwrapRef<Ref<boolean, boolean>>
  poolModeRetryCount: UnwrapRef<Ref<number, number>>
  MAX_POOL_MODE_RETRY_COUNT: UnwrapRef<10>
  DEFAULT_POOL_MODE_RETRY_COUNT: UnwrapRef<3>
  poolModeRetryStatusCodesInput: UnwrapRef<Ref<string, string>>
  DEFAULT_POOL_MODE_RETRY_STATUS_CODES: UnwrapRef<number[]>
}>()


const emit = defineEmits<{
  'update:bedrockAuthMode': [value: UnwrapRef<Ref<"apikey" | "sigv4", "apikey" | "sigv4">>]
  'update:bedrockAccessKeyId': [value: UnwrapRef<Ref<string, string>>]
  'update:bedrockSecretAccessKey': [value: UnwrapRef<Ref<string, string>>]
  'update:bedrockSessionToken': [value: UnwrapRef<Ref<string, string>>]
  'update:bedrockApiKeyValue': [value: UnwrapRef<Ref<string, string>>]
  'update:bedrockRegion': [value: UnwrapRef<Ref<string, string>>]
  'update:bedrockForceGlobal': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:modelRestrictionMode': [value: UnwrapRef<Ref<"whitelist" | "mapping", "whitelist" | "mapping">>]
  'update:allowedModels': [value: UnwrapRef<Ref<string[], string[]>>]
  'update:poolModeEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:poolModeRetryCount': [value: UnwrapRef<Ref<number, number>>]
  'update:poolModeRetryStatusCodesInput': [value: UnwrapRef<Ref<string, string>>]
}>()


const { t } = useI18n()


const form = computed(() => props.form)


const accountCategory = computed(() => props.accountCategory)


const bedrockAuthMode = usePanelField(() => props.bedrockAuthMode, value => emit('update:bedrockAuthMode', value))


const bedrockAccessKeyId = usePanelField(() => props.bedrockAccessKeyId, value => emit('update:bedrockAccessKeyId', value))


const bedrockSecretAccessKey = usePanelField(() => props.bedrockSecretAccessKey, value => emit('update:bedrockSecretAccessKey', value))


const bedrockSessionToken = usePanelField(() => props.bedrockSessionToken, value => emit('update:bedrockSessionToken', value))


const bedrockApiKeyValue = usePanelField(() => props.bedrockApiKeyValue, value => emit('update:bedrockApiKeyValue', value))


const bedrockRegion = usePanelField(() => props.bedrockRegion, value => emit('update:bedrockRegion', value))


const bedrockForceGlobal = usePanelField(() => props.bedrockForceGlobal, value => emit('update:bedrockForceGlobal', value))


const modelRestrictionMode = usePanelField(() => props.modelRestrictionMode, value => emit('update:modelRestrictionMode', value))


const allowedModels = usePanelField(() => props.allowedModels, value => emit('update:allowedModels', value))


const modelMappings = computed(() => props.modelMappings)






const bedrockPresets = computed(() => props.bedrockPresets)


const addPresetMapping = computed(() => props.addPresetMapping)


const poolModeEnabled = usePanelField(() => props.poolModeEnabled, value => emit('update:poolModeEnabled', value))


const poolModeRetryCount = usePanelField(() => props.poolModeRetryCount, value => emit('update:poolModeRetryCount', value))


const MAX_POOL_MODE_RETRY_COUNT = computed(() => props.MAX_POOL_MODE_RETRY_COUNT)


const DEFAULT_POOL_MODE_RETRY_COUNT = computed(() => props.DEFAULT_POOL_MODE_RETRY_COUNT)


const poolModeRetryStatusCodesInput = usePanelField(() => props.poolModeRetryStatusCodesInput, value => emit('update:poolModeRetryStatusCodesInput', value))


const DEFAULT_POOL_MODE_RETRY_STATUS_CODES = computed(() => props.DEFAULT_POOL_MODE_RETRY_STATUS_CODES)
</script>
<template>

<div v-if="form.platform === 'anthropic' && accountCategory === 'bedrock'" class="space-y-4">
        <!-- Auth Mode Radio -->
        <div>
          <label class="input-label">{{ t('admin.accounts.bedrockAuthMode') }}</label>
          <div class="mt-2 flex gap-4">
            <label class="flex cursor-pointer items-center">
              <input
                v-model="bedrockAuthMode"
                type="radio"
                value="sigv4"
                class="mr-2 text-primary-600 focus:ring-primary-500"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.bedrockAuthModeSigv4') }}</span>
            </label>
            <label class="flex cursor-pointer items-center">
              <input
                v-model="bedrockAuthMode"
                type="radio"
                value="apikey"
                class="mr-2 text-primary-600 focus:ring-primary-500"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.bedrockAuthModeApikey') }}</span>
            </label>
          </div>
        </div>

        <!-- SigV4 fields -->
        <template v-if="bedrockAuthMode === 'sigv4'">
          <div>
            <label class="input-label">{{ t('admin.accounts.bedrockAccessKeyId') }}</label>
            <input
              v-model="bedrockAccessKeyId"
              type="text"
              required
              class="input font-mono"
              placeholder="AKIA..."
            />
          </div>
          <div>
            <label class="input-label">{{ t('admin.accounts.bedrockSecretAccessKey') }}</label>
            <input
              v-model="bedrockSecretAccessKey"
              type="password"
              required
              class="input font-mono"
            />
          </div>
          <div>
            <label class="input-label">{{ t('admin.accounts.bedrockSessionToken') }}</label>
            <input
              v-model="bedrockSessionToken"
              type="password"
              class="input font-mono"
            />
            <p class="input-hint">{{ t('admin.accounts.bedrockSessionTokenHint') }}</p>
          </div>
        </template>

        <!-- API Key field -->
        <div v-if="bedrockAuthMode === 'apikey'">
          <label class="input-label">{{ t('admin.accounts.bedrockApiKeyInput') }}</label>
          <input
            v-model="bedrockApiKeyValue"
            type="password"
            required
            class="input font-mono"
          />
        </div>

        <!-- Shared: Region -->
        <div>
          <label class="input-label">{{ t('admin.accounts.bedrockRegion') }}</label>
          <select v-model="bedrockRegion" class="input">
            <optgroup label="US">
              <option value="us-east-1">us-east-1 (N. Virginia)</option>
              <option value="us-east-2">us-east-2 (Ohio)</option>
              <option value="us-west-1">us-west-1 (N. California)</option>
              <option value="us-west-2">us-west-2 (Oregon)</option>
              <option value="us-gov-east-1">us-gov-east-1 (GovCloud US-East)</option>
              <option value="us-gov-west-1">us-gov-west-1 (GovCloud US-West)</option>
            </optgroup>
            <optgroup label="Europe">
              <option value="eu-west-1">eu-west-1 (Ireland)</option>
              <option value="eu-west-2">eu-west-2 (London)</option>
              <option value="eu-west-3">eu-west-3 (Paris)</option>
              <option value="eu-central-1">eu-central-1 (Frankfurt)</option>
              <option value="eu-central-2">eu-central-2 (Zurich)</option>
              <option value="eu-south-1">eu-south-1 (Milan)</option>
              <option value="eu-south-2">eu-south-2 (Spain)</option>
              <option value="eu-north-1">eu-north-1 (Stockholm)</option>
            </optgroup>
            <optgroup label="Asia Pacific">
              <option value="ap-northeast-1">ap-northeast-1 (Tokyo)</option>
              <option value="ap-northeast-2">ap-northeast-2 (Seoul)</option>
              <option value="ap-northeast-3">ap-northeast-3 (Osaka)</option>
              <option value="ap-south-1">ap-south-1 (Mumbai)</option>
              <option value="ap-south-2">ap-south-2 (Hyderabad)</option>
              <option value="ap-southeast-1">ap-southeast-1 (Singapore)</option>
              <option value="ap-southeast-2">ap-southeast-2 (Sydney)</option>
            </optgroup>
            <optgroup label="Canada">
              <option value="ca-central-1">ca-central-1 (Canada)</option>
            </optgroup>
            <optgroup label="South America">
              <option value="sa-east-1">sa-east-1 (São Paulo)</option>
            </optgroup>
          </select>
          <p class="input-hint">{{ t('admin.accounts.bedrockRegionHint') }}</p>
        </div>

        <!-- Shared: Force Global -->
        <div>
          <label class="flex items-center gap-2 cursor-pointer">
            <input
              v-model="bedrockForceGlobal"
              type="checkbox"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.bedrockForceGlobal') }}</span>
          </label>
          <p class="input-hint mt-1">{{ t('admin.accounts.bedrockForceGlobalHint') }}</p>
        </div>

        <!-- Model Restriction Section for Bedrock -->
        <CreateBedrockModelsPanel
  :modelRestrictionMode="modelRestrictionMode"
  :allowedModels="allowedModels"
  :modelMappings="modelMappings"
  :bedrockPresets="bedrockPresets"
  :addPresetMapping="addPresetMapping"
  @update:modelRestrictionMode="modelRestrictionMode = $event"
  @update:allowedModels="allowedModels = $event"
/>

        <!-- Pool Mode Section for Bedrock -->
        <CreateBedrockRetryPoolPanel
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
