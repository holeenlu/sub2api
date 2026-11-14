<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.bulkEdit.title')"
    width="wide"
    @close="handleClose"
  >
    <form id="bulk-edit-account-form" class="space-y-5" @submit.prevent="() => handleSubmit()">
      <div v-if="!isMixedPlatform && targetSelectedTypes.length === 1" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
        <p class="mb-3 text-sm text-gray-500">{{ t('admin.accounts.bulkEdit.fullEditorHelp') }}</p>
        <button type="button" class="btn btn-secondary" data-testid="bulk-full-editor" :disabled="submitting" @click="openFullEditor">{{ t('admin.accounts.bulkEdit.fullEditor') }}</button>
      </div>
      <!-- Info -->
      <div class="rounded-lg bg-blue-50 p-4 dark:bg-blue-900/20">
        <p class="text-sm text-blue-700 dark:text-blue-400">
          <svg class="mr-1.5 inline h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
            />
          </svg>
          {{ t('admin.accounts.bulkEdit.selectionInfo', { count: targetAccountIds.length }) }}
        </p>
      </div>

      <!-- Mixed platform warning -->
      <div v-if="isMixedPlatform" class="rounded-lg bg-amber-50 p-4 dark:bg-amber-900/20">
        <p class="text-sm text-amber-700 dark:text-amber-400">
          <svg class="mr-1.5 inline h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
          {{ t('admin.accounts.bulkEdit.mixedPlatformWarning', { platforms: targetSelectedPlatforms.join(', ') }) }}
        </p>
      </div>

      <div v-if="allowTransportEdit && !supplierScope && allOpenAIOAuthOnly" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between gap-4">
          <div class="min-w-0">
            <label for="bulk-edit-cloud-enabled" class="input-label mb-0">{{ t('admin.accounts.openai.cloud.enable') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.cloud.nativeHint') }}</p>
          </div>
          <input id="bulk-edit-cloud-enabled" v-model="enableCloud" type="checkbox"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
        </div>
        <Toggle id="bulk-edit-cloud-toggle" v-model="cloudEnabled" :disabled="!enableCloud || cloudEligibilityLoading || !!cloudEligibilityError"
          :class="!enableCloud && 'pointer-events-none opacity-50'" :aria-label="t('admin.accounts.openai.cloud.enable')" />
        <p v-if="enableCloud && cloudEligibilityLoading" class="mt-2 text-xs text-gray-500">{{ t('common.loading') }}</p>
        <p v-else-if="enableCloud && cloudEligibilityError" class="mt-2 text-xs text-amber-700 dark:text-amber-400" role="status">{{ cloudEligibilityError }}</p>
      </div>

      <!-- Excel / BPS protocol (ChatGPT OAuth only) -->
      <div v-if="allowTransportEdit && allOpenAIOAuthOnly" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <div class="flex-1 pr-4">
            <label id="bulk-edit-excel-bps-label" class="input-label mb-0" for="bulk-edit-excel-bps-enabled">
              {{ t('admin.accounts.openai.excelBPS') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.excelBPSDesc') }}
            </p>
          </div>
          <input v-model="enableExcelBPS" id="bulk-edit-excel-bps-enabled" type="checkbox"
            aria-controls="bulk-edit-excel-bps-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
        </div>
        <fieldset id="bulk-edit-excel-bps-body" :disabled="!enableExcelBPS"
          :class="!enableExcelBPS && 'pointer-events-none opacity-50'"
          aria-labelledby="bulk-edit-excel-bps-label">
          <Toggle v-model="excelBPSEnabled" data-testid="bulk-excel-bps-toggle" :aria-label="t('admin.accounts.openai.excelBPS')" />
          <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSAccountSettingsHint') }}</p>
          <div v-if="excelBPSEnabled" class="mt-3 space-y-3">
            <p class="text-xs text-amber-700 dark:text-amber-300" data-testid="bulk-bps-conditional-hint">{{ t('admin.accounts.openai.excelBPSBulkStateConflict') }}</p>
            <div data-testid="bulk-excel-bps-model-selection">
              <label class="input-label">{{ t('admin.accounts.openai.excelBPSModels') }}</label>
              <ModelWhitelistSelector v-model="excelBPSModels" platform="openai" empty-mode="explicit" />
              <p class="input-hint">{{ t('admin.accounts.openai.excelBPSModelsHint') }}</p>
            </div>
            <p class="text-xs text-amber-600 dark:text-amber-400">{{ t('admin.accounts.openai.excelBPSNotice') }}</p>
            <div class="mt-3">
              <label class="flex items-center gap-2">
                <input v-model="excelBPSOmitUnsupportedTools" type="checkbox"
                  data-testid="bulk-excel-bps-omit-unsupported-tools"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500" />
                <span class="text-sm">{{ t('admin.accounts.openai.excelBPSOmitUnsupportedTools') }}</span>
              </label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSOmitUnsupportedToolsDesc') }}</p>
            </div>
            <div class="mt-3">
              <label class="flex items-center gap-2">
                <input v-model="excelBPSIgnoreEncryptedContent" type="checkbox"
                  data-testid="bulk-excel-bps-ignore-encrypted-content"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500" />
                <span class="text-sm">{{ t('admin.accounts.openai.excelBPSIgnoreEncryptedContent') }}</span>
              </label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSIgnoreEncryptedContentDesc') }}</p>
            </div>
            <div class="mt-3">
              <label class="flex items-center gap-2">
                <input v-model="excelBPSAutoDisableOn403" type="checkbox"
                  data-testid="bulk-excel-bps-auto-disable-on-403"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500" />
                <span class="text-sm">{{ t('admin.accounts.openai.excelBPSAutoDisableOn403') }}</span>
              </label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSAutoDisableOn403Desc') }}</p>
            </div>
            <div class="mt-3">
              <label class="flex items-center gap-2">
                <input v-model="excelBPSAutoRecoverOn403" type="checkbox" :disabled="!excelBPSAutoDisableOn403"
                  data-testid="bulk-excel-bps-auto-recover-on-403"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 disabled:opacity-50" />
                <span class="text-sm">{{ t('admin.accounts.openai.excelBPSAutoRecoverOn403') }}</span>
              </label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSAutoRecoverOn403Desc') }}</p>
              <div v-if="excelBPSAutoRecoverOn403 && excelBPSAutoDisableOn403" class="mt-2">
                <label class="block space-y-1">
                  <span class="text-sm">{{ t('admin.accounts.openai.excelBPS403RecoveryInterval') }}</span>
                  <input v-model.number="excelBPSRecoveryIntervalMinutes" type="number" min="1" :max="MAX_BPS_RECOVERY_INTERVAL_MINUTES" step="1" required
                    data-testid="bulk-excel-bps-recovery-interval" class="input w-40" />
                </label>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPS403RecoveryIntervalHint') }}</p>
              </div>
            </div>
            <div class="mt-3">
              <label class="flex items-center gap-2">
                <input v-model="excelBPSAutoMoveOn403" type="checkbox"
                  data-testid="bulk-excel-bps-auto-move-on-403"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500" />
                <span class="text-sm">{{ t('admin.accounts.openai.excelBPSAutoMoveOn403') }}</span>
              </label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSAutoMoveOn403Desc') }}</p>
              <div v-if="excelBPSAutoMoveOn403" class="mt-2">
                <label class="input-label">{{ t('admin.accounts.openai.excelBPS403TargetGroup') }}</label>
                <Select v-model="excelBPS403TargetGroupID" :options="excelBPS403GroupOptions"
                  :aria-label="t('admin.accounts.openai.excelBPS403TargetGroup')"
                  data-testid="bulk-excel-bps-403-target-group" />
              </div>
            </div>
            <div>
              <label class="flex items-center gap-2">
                <input v-model="excelBPSCacheCreationAsInput" type="checkbox"
                  data-testid="bulk-excel-bps-cache-creation-as-input"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500" />
                <span class="text-sm">{{ t('admin.accounts.openai.excelBPSCacheCreationAsInput') }}</span>
              </label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSCacheCreationAsInputDesc') }}</p>
            </div>
          </div>
        </fieldset>
      </div>

      <!-- OpenAI passthrough -->
      <div
        v-if="allowTransportEdit && (allOpenAIPassthroughCapable)"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="mb-3 flex items-center justify-between">
          <div class="flex-1 pr-4">
            <label
              id="bulk-edit-openai-passthrough-label"
              class="input-label mb-0"
              for="bulk-edit-openai-passthrough-enabled"
            >
              {{ t('admin.accounts.openai.oauthPassthrough') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.oauthPassthroughDesc') }}
            </p>
          </div>
          <input
            v-model="enableOpenAIPassthrough"
            id="bulk-edit-openai-passthrough-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-passthrough-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-passthrough-body"
          :class="!enableOpenAIPassthrough && 'pointer-events-none opacity-50'"
          role="group"
          aria-labelledby="bulk-edit-openai-passthrough-label"
        >
          <button
            id="bulk-edit-openai-passthrough-toggle"
            type="button"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              openaiPassthroughEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
            @click="openaiPassthroughEnabled = !openaiPassthroughEnabled"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                openaiPassthroughEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <!-- OpenAI Codex namespace 工具摊平（兼容开关，仅 OAuth） -->
      <div
        v-if="allowTransportEdit && (allOpenAIOAuthOnly)"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="mb-3 flex items-center justify-between">
          <div class="flex-1 pr-4">
            <label
              id="bulk-edit-openai-flatten-namespaces-label"
              class="input-label mb-0"
              for="bulk-edit-openai-flatten-namespaces-enabled"
            >
              {{ t('admin.accounts.openai.flattenNamespaces') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.flattenNamespacesDesc') }}
            </p>
          </div>
          <input
            v-model="enableOpenAIFlattenNamespaces"
            id="bulk-edit-openai-flatten-namespaces-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-flatten-namespaces-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-flatten-namespaces-body"
          :class="!enableOpenAIFlattenNamespaces && 'pointer-events-none opacity-50'"
          role="group"
          aria-labelledby="bulk-edit-openai-flatten-namespaces-label"
        >
          <button
            id="bulk-edit-openai-flatten-namespaces-toggle"
            type="button"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              openaiFlattenNamespacesEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
            @click="openaiFlattenNamespacesEnabled = !openaiFlattenNamespacesEnabled"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                openaiFlattenNamespacesEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <!-- OpenAI API long-context billing -->
      <div
        v-if="allowTransportEdit && (allOpenAIPassthroughCapable)"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="mb-3 flex items-center justify-between gap-4">
          <div class="flex-1">
            <label
              id="bulk-edit-openai-long-context-billing-label"
              class="input-label mb-0"
              for="bulk-edit-openai-long-context-billing-enabled"
            >
              {{ t('admin.accounts.openai.longContextBilling') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.longContextBillingDesc') }}
            </p>
          </div>
          <input
            v-model="enableOpenAILongContextBilling"
            id="bulk-edit-openai-long-context-billing-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-long-context-billing-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-long-context-billing-body"
          :class="!enableOpenAILongContextBilling && 'pointer-events-none opacity-50'"
          role="group"
          aria-labelledby="bulk-edit-openai-long-context-billing-label"
        >
          <button
            type="button"
            data-testid="bulk-edit-openai-long-context-billing-toggle"
            role="switch"
            :disabled="!enableOpenAILongContextBilling"
            :aria-checked="openAILongContextBillingEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              openAILongContextBillingEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
            @click="openAILongContextBillingEnabled = !openAILongContextBillingEnabled"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                openAILongContextBillingEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
        <p
          class="mt-3 rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
          data-testid="bulk-edit-openai-long-context-shadow-hint"
        >
          {{ t('admin.accounts.bulkEdit.longContextShadowHint') }}
        </p>
      </div>

      <!-- Base URL (API Key only) -->
      <div v-if="allowTransportEdit" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-base-url-label"
            class="input-label mb-0"
            for="bulk-edit-base-url-enabled"
          >
            {{ t('admin.accounts.baseUrl') }}
          </label>
          <input
            v-model="enableBaseUrl"
            id="bulk-edit-base-url-enabled"
            type="checkbox"
            aria-controls="bulk-edit-base-url"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <input
          v-model="baseUrl"
          id="bulk-edit-base-url"
          type="text"
          :disabled="!enableBaseUrl"
          class="input"
          :class="!enableBaseUrl && 'cursor-not-allowed opacity-50'"
          :placeholder="t('admin.accounts.bulkEdit.baseUrlPlaceholder')"
          aria-labelledby="bulk-edit-base-url-label"
        />
        <GrokBaseUrlPresets
          v-if="allTargetsGrok"
          class="mt-2"
          @select="baseUrl = $event; enableBaseUrl = true"
        />
        <p class="input-hint">
          {{ t('admin.accounts.bulkEdit.baseUrlNotice') }}
        </p>
      </div>

      <BulkModelRestrictionPanel
        v-if="canEditModelCredentials"
        v-model:selected="enableModelRestriction"
        v-model:mode="modelRestrictionMode"
        v-model:models="allowedModels"
        v-model:mappings="modelMappings"
        :platforms="targetSelectedPlatforms"
        :presets="filteredPresets"
        :disabled-by-passthrough="isOpenAIModelRestrictionDisabled"
        :mixed-references="bulkEligibility.mixedModelReferences"
        @info="appStore.showInfo"
      />

      <BulkErrorCodesPanel
        v-if="allowAuthorize"
        v-model:selected="enableCustomErrorCodes"
        v-model:codes="selectedErrorCodes"
        v-model:input="customErrorCodeInput"
        @info="appStore.showInfo"
        @error="appStore.showError"
      />

      <!-- Intercept warmup requests (Anthropic only) -->
      <div v-if="allowAuthorize" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="flex items-center justify-between">
          <div class="flex-1 pr-4">
            <label
              id="bulk-edit-intercept-warmup-label"
              class="input-label mb-0"
              for="bulk-edit-intercept-warmup-enabled"
            >
              {{ t('admin.accounts.interceptWarmupRequests') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.interceptWarmupRequestsDesc') }}
            </p>
          </div>
          <input
            v-model="enableInterceptWarmup"
            id="bulk-edit-intercept-warmup-enabled"
            type="checkbox"
            aria-controls="bulk-edit-intercept-warmup-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div v-if="enableInterceptWarmup" id="bulk-edit-intercept-warmup-body" class="mt-3">
          <button
            type="button"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              interceptWarmupRequests ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
            @click="interceptWarmupRequests = !interceptWarmupRequests"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                interceptWarmupRequests ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <!-- Header Override (eligible API-key platforms + grok OAuth) -->
      <div v-if="allowAuthorize && (allHeaderOverrideCapable)" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="flex items-center justify-between">
          <div class="flex-1 pr-4">
            <label
              id="bulk-edit-header-override-label"
              class="input-label mb-0"
              for="bulk-edit-header-override-enabled"
            >
              {{ t('admin.accounts.headerOverride.title') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.headerOverride.hint') }}
            </p>
          </div>
          <input
            v-model="enableHeaderOverride"
            id="bulk-edit-header-override-enabled"
            type="checkbox"
            aria-controls="bulk-edit-header-override-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div v-if="enableHeaderOverride" id="bulk-edit-header-override-body" class="mt-3 space-y-3">
          <button
            type="button"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              headerOverrideEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
            @click="headerOverrideEnabled = !headerOverrideEnabled"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                headerOverrideEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>

          <div v-if="headerOverrideEnabled" class="space-y-3">
            <div class="rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
              <p class="text-xs text-blue-700 dark:text-blue-400">
                <Icon name="exclamationCircle" size="sm" class="mr-1 inline" :stroke-width="2" />
                {{ t('admin.accounts.headerOverride.info') }}
              </p>
            </div>

            <p class="text-xs text-amber-600 dark:text-amber-400">
              {{ t('admin.accounts.headerOverride.bulkReplaceHint') }}
            </p>

            <HeaderOverrideEditor
              :rows="headerOverrideRows"
              @update:rows="headerOverrideRows = $event"
            />
          </div>
          <p v-else class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.headerOverride.bulkDisableHint') }}
          </p>
        </div>
      </div>

      <!-- Proxy -->
      <div v-if="allowTransportEdit" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-proxy-label"
            class="input-label mb-0"
            for="bulk-edit-proxy-enabled"
          >
            {{ t('admin.accounts.proxy') }}
          </label>
          <input
            v-model="enableProxy"
            id="bulk-edit-proxy-enabled"
            type="checkbox"
            aria-controls="bulk-edit-proxy-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div id="bulk-edit-proxy-body" :class="!enableProxy && 'pointer-events-none opacity-50'">
          <ProxySelector
            v-model="proxyId"
            :proxies="proxies"
            aria-labelledby="bulk-edit-proxy-label"
          />
        </div>
      </div>

      <!-- Concurrency & Priority -->
      <div class="grid grid-cols-2 gap-4 border-t border-gray-200 pt-4 dark:border-dark-600 lg:grid-cols-4">
        <div>
          <div class="mb-3 flex items-center justify-between">
            <label
              id="bulk-edit-concurrency-label"
              class="input-label mb-0"
              for="bulk-edit-concurrency-enabled"
            >
              {{ t('admin.accounts.concurrency') }}
            </label>
            <input
              v-model="enableConcurrency"
              id="bulk-edit-concurrency-enabled"
              type="checkbox"
              aria-controls="bulk-edit-concurrency"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
          </div>
          <input
            v-model.number="concurrency"
            id="bulk-edit-concurrency"
            type="number"
            min="1"
            :disabled="!enableConcurrency"
            class="input"
            :class="!enableConcurrency && 'cursor-not-allowed opacity-50'"
            aria-labelledby="bulk-edit-concurrency-label"
            @input="concurrency = Math.max(1, concurrency || 1)"
          />
        </div>
        <div>
          <div class="mb-3 flex items-center justify-between">
            <label
              id="bulk-edit-load-factor-label"
              class="input-label mb-0"
              for="bulk-edit-load-factor-enabled"
            >
              {{ t('admin.accounts.loadFactor') }}
            </label>
            <input
              v-model="enableLoadFactor"
              id="bulk-edit-load-factor-enabled"
              type="checkbox"
              aria-controls="bulk-edit-load-factor"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
          </div>
          <input
            v-model.number="loadFactor"
            id="bulk-edit-load-factor"
            type="number"
            min="1"
            :disabled="!enableLoadFactor"
            class="input"
            :class="!enableLoadFactor && 'cursor-not-allowed opacity-50'"
            aria-labelledby="bulk-edit-load-factor-label"
            @input="loadFactor = (loadFactor &amp;&amp; loadFactor >= 1) ? loadFactor : null"
          />
          <p class="input-hint">{{ t('admin.accounts.loadFactorHint') }}</p>
        </div>
        <div>
          <div class="mb-3 flex items-center justify-between">
            <label
              id="bulk-edit-priority-label"
              class="input-label mb-0"
              for="bulk-edit-priority-enabled"
            >
              {{ t('admin.accounts.priority') }}
            </label>
            <input
              v-model="enablePriority"
              id="bulk-edit-priority-enabled"
              type="checkbox"
              aria-controls="bulk-edit-priority"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
          </div>
          <input
            v-model.number="priority"
            id="bulk-edit-priority"
            type="number"
            min="1"
            :disabled="!enablePriority"
            class="input"
            :class="!enablePriority && 'cursor-not-allowed opacity-50'"
            aria-labelledby="bulk-edit-priority-label"
          />
        </div>
        <div v-if="allowRateEdit">
          <div class="mb-3 flex items-center justify-between">
            <label
              id="bulk-edit-rate-multiplier-label"
              class="input-label mb-0"
              for="bulk-edit-rate-multiplier-enabled"
            >
              {{ t('admin.accounts.billingRateMultiplier') }}
            </label>
            <input
              v-model="enableRateMultiplier"
              id="bulk-edit-rate-multiplier-enabled"
              type="checkbox"
              aria-controls="bulk-edit-rate-multiplier"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
          </div>
          <input
            v-model.number="rateMultiplier"
            id="bulk-edit-rate-multiplier"
            type="number"
            min="0"
            step="0.01"
            :disabled="!enableRateMultiplier"
            class="input"
            :class="!enableRateMultiplier && 'cursor-not-allowed opacity-50'"
            aria-labelledby="bulk-edit-rate-multiplier-label"
          />
          <p class="input-hint">{{ t('admin.accounts.billingRateMultiplierHint') }}</p>
          <p
            v-if="enableRateMultiplier"
            class="mt-2 flex items-start gap-1 text-xs text-amber-700 dark:text-amber-300"
            data-testid="bulk-rate-sync-warning"
          >
            <Icon name="exclamationTriangle" size="xs" class="mt-0.5 flex-shrink-0" />
            <span>{{ t('admin.accounts.bulkEdit.rateSyncWarning') }}</span>
          </p>
        </div>
      </div>

      <!-- Status -->
      <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-status-label"
            class="input-label mb-0"
            for="bulk-edit-status-enabled"
          >
            {{ t('common.status') }}
          </label>
          <input
            v-model="enableStatus"
            id="bulk-edit-status-enabled"
            type="checkbox"
            aria-controls="bulk-edit-status"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div id="bulk-edit-status" :class="!enableStatus && 'pointer-events-none opacity-50'">
          <Select
            v-model="status"
            :options="statusOptions"
            aria-labelledby="bulk-edit-status-label"
          />
        </div>
      </div>

      <BulkOpenAIWebSocketPanel
        v-if="allowTransportEdit && allOpenAIOAuth"
        v-model:selected="enableOpenAIWSMode"
        v-model:mode="openaiOAuthResponsesWebSocketV2Mode"
        prefix="bulk-edit-openai-ws-mode"
      />

      <!-- OpenAI OAuth Codex CLI only -->
      <div v-if="allowTransportEdit && (allOpenAIOAuth)" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-openai-codex-cli-only-label"
            class="input-label mb-0"
            for="bulk-edit-openai-codex-cli-only-enabled"
          >
            {{ t('admin.accounts.openai.codexCLIOnly') }}
          </label>
          <input
            v-model="enableCodexCLIOnly"
            id="bulk-edit-openai-codex-cli-only-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-codex-cli-only"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-codex-cli-only"
          :class="!enableCodexCLIOnly && 'pointer-events-none opacity-50'"
        >
          <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.openai.codexCLIOnlyDesc') }}
          </p>
          <button
            id="bulk-edit-openai-codex-cli-only-toggle"
            type="button"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              codexCLIOnlyEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
            @click="codexCLIOnlyEnabled = !codexCLIOnlyEnabled"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                codexCLIOnlyEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <!-- OpenAI OAuth: Codex app-server -->
      <div v-if="allowTransportEdit && (allOpenAIOAuth)" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-openai-codex-app-server-label"
            class="input-label mb-0"
            for="bulk-edit-openai-codex-app-server-enabled"
          >
            {{ t('admin.accounts.openai.codexCLIOnlyAppServer') }}
          </label>
          <input
            v-model="enableCodexCLIOnlyAppServer"
            id="bulk-edit-openai-codex-app-server-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-codex-app-server"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-codex-app-server"
          :class="!enableCodexCLIOnlyAppServer && 'pointer-events-none opacity-50'"
        >
          <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.openai.codexCLIOnlyAppServerDesc') }}
          </p>
          <button
            id="bulk-edit-openai-codex-app-server-toggle"
            type="button"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              codexCLIOnlyAppServerEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
            @click="codexCLIOnlyAppServerEnabled = !codexCLIOnlyAppServerEnabled"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                codexCLIOnlyAppServerEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <!-- Codex 指纹收敛模式（仅 OpenAI OAuth） -->
      <div v-if="allowTransportEdit && (allOpenAIOAuth)" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label class="input-label mb-0">{{ t('admin.accounts.openai.codexFingerprintMode') }}</label>
          <input
            id="bulk-edit-openai-codex-fingerprint-mode-enabled"
            v-model="enableCodexFingerprintMode"
            type="checkbox"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div :class="!enableCodexFingerprintMode && 'pointer-events-none opacity-50'">
          <p class="mb-2 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.openai.codexFingerprintModeDesc') }}
          </p>
          <Select v-model="codexFingerprintMode" data-testid="bulk-codex-fingerprint-mode-select" :options="codexFingerprintModeOptions" />
        </div>
      </div>

      <!-- Upstream billing auto probe (any API-key platform) -->
      <div v-if="allowRateEdit && (allBillingProbeCapable)" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <div class="flex-1 pr-4">
            <label
              id="bulk-edit-upstream-billing-auto-probe-label"
              class="input-label mb-0"
              for="bulk-edit-upstream-billing-auto-probe-enabled"
            >
              {{ t('admin.accounts.upstreamBilling.autoProbe') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.upstreamBilling.autoProbeHint') }}
            </p>
          </div>
          <input
            v-model="enableUpstreamBillingAutoProbe"
            id="bulk-edit-upstream-billing-auto-probe-enabled"
            type="checkbox"
            aria-controls="bulk-edit-upstream-billing-auto-probe"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-upstream-billing-auto-probe"
          :class="!enableUpstreamBillingAutoProbe && 'pointer-events-none opacity-50'"
          role="group"
          aria-labelledby="bulk-edit-upstream-billing-auto-probe-label"
        >
          <Select
            v-model="upstreamBillingAutoProbeMode"
            :disabled="!enableUpstreamBillingAutoProbe"
            data-testid="bulk-edit-upstream-billing-auto-probe-select"
            :options="upstreamBillingAutoProbeOptions"
            aria-labelledby="bulk-edit-upstream-billing-auto-probe-label"
          />
        </div>
      </div>

      <!-- OpenAI API Key endpoint capabilities -->
      <div v-if="allowAuthorize && (allOpenAIAPIKey)" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between gap-4">
          <div class="flex-1">
            <label
              id="bulk-edit-openai-endpoint-capabilities-label"
              class="input-label mb-0"
              for="bulk-edit-openai-endpoint-capabilities-enabled"
            >
              {{ t('admin.accounts.openai.endpointCapabilities') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.endpointCapabilitiesDesc') }}
            </p>
          </div>
          <input
            v-model="enableOpenAIEndpointCapabilities"
            id="bulk-edit-openai-endpoint-capabilities-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-endpoint-capabilities-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-endpoint-capabilities-body"
          :class="!enableOpenAIEndpointCapabilities && 'pointer-events-none opacity-50'"
          role="group"
          aria-labelledby="bulk-edit-openai-endpoint-capabilities-label"
        >
          <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
            <label
              v-for="option in openAIEndpointCapabilityOptions"
              :key="option.value"
              class="flex cursor-pointer items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm dark:border-dark-600"
            >
              <input
                type="checkbox"
                :disabled="!enableOpenAIEndpointCapabilities"
                class="rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
                :data-testid="`bulk-edit-openai-endpoint-capability-${option.value}`"
                :checked="openAIEndpointCapabilities.includes(option.value)"
                @change="toggleOpenAIEndpointCapability(option.value, $event)"
              />
              <span class="text-gray-700 dark:text-gray-200">{{ option.label }}</span>
            </label>
          </div>
        </div>
      </div>

      <!-- OpenAI API Key Responses route -->
      <div v-if="allowTransportEdit && (allOpenAIAPIKey)" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between gap-4">
          <div class="flex-1">
            <label
              id="bulk-edit-openai-responses-mode-label"
              class="input-label mb-0"
              for="bulk-edit-openai-responses-mode-enabled"
            >
              {{ t('admin.accounts.openai.responsesMode') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.responsesModeDesc') }}
            </p>
          </div>
          <input
            v-model="enableOpenAIResponsesMode"
            id="bulk-edit-openai-responses-mode-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-responses-mode-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-responses-mode-body"
          :class="!enableOpenAIResponsesMode && 'pointer-events-none opacity-50'"
          role="group"
          aria-labelledby="bulk-edit-openai-responses-mode-label"
        >
          <Select
            v-model="openAIResponsesMode"
            :disabled="!enableOpenAIResponsesMode || !openAIResponsesModeApplicable"
            data-testid="bulk-edit-openai-responses-mode-select"
            :options="openAIResponsesModeOptions"
            aria-labelledby="bulk-edit-openai-responses-mode-label"
          />
          <p
            v-if="enableOpenAIEndpointCapabilities && !openAITextGenerationCapabilityEnabled"
            class="mt-2 rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
            data-testid="bulk-edit-openai-responses-mode-not-applicable"
          >
            {{ t('admin.accounts.openai.responsesModeTextDisabledHint') }}
          </p>
        </div>
      </div>

      <BulkOpenAIWebSocketPanel
        v-if="allowTransportEdit && allOpenAIAPIKey"
        v-model:selected="enableOpenAIAPIKeyWSMode"
        v-model:mode="openaiAPIKeyResponsesWebSocketV2Mode"
        prefix="bulk-edit-openai-apikey-ws-mode"
      />

      <!-- OpenAI Compact mode -->
      <div v-if="allowTransportEdit && (allOpenAIPassthroughCapable)" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <div class="flex-1 pr-4">
            <label
              id="bulk-edit-openai-compact-mode-label"
              class="input-label mb-0"
              for="bulk-edit-openai-compact-mode-enabled"
            >
              {{ t('admin.accounts.openai.compactMode') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.compactModeDesc') }}
            </p>
          </div>
          <input
            v-model="enableOpenAICompactMode"
            id="bulk-edit-openai-compact-mode-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-compact-mode"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-compact-mode"
          :class="!enableOpenAICompactMode && 'pointer-events-none opacity-50'"
        >
          <Select
            v-model="openAICompactMode"
            data-testid="bulk-edit-openai-compact-mode-select"
            :options="openAICompactModeOptions"
            aria-labelledby="bulk-edit-openai-compact-mode-label"
          />
        </div>
      </div>

      <BulkOpenAICompactMappingPanel
        v-if="allowAuthorize && allOpenAIPassthroughCapable"
        v-model:selected="enableOpenAICompactModelMapping"
        v-model:mappings="openAICompactModelMappings"
      />

      <!-- RPM Limit (仅全部为 Anthropic OAuth/SetupToken 时显示) -->
      <div v-if="allowTransportEdit && (allAnthropicOAuthOrSetupToken)" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-rpm-limit-label"
            class="input-label mb-0"
            for="bulk-edit-rpm-limit-enabled"
          >
            {{ t('admin.accounts.quotaControl.rpmLimit.label') }}
          </label>
          <input
            v-model="enableRpmLimit"
            id="bulk-edit-rpm-limit-enabled"
            type="checkbox"
            aria-controls="bulk-edit-rpm-limit-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>

        <div
          id="bulk-edit-rpm-limit-body"
          :class="!enableRpmLimit && 'pointer-events-none opacity-50'"
          role="group"
          aria-labelledby="bulk-edit-rpm-limit-label"
        >
          <div class="mb-3 flex items-center justify-between">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.quotaControl.rpmLimit.hint') }}</span>
            <button
              type="button"
              @click="rpmLimitEnabled = !rpmLimitEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                rpmLimitEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  rpmLimitEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="rpmLimitEnabled" class="space-y-3">
            <div>
              <label class="input-label text-xs">{{ t('admin.accounts.quotaControl.rpmLimit.baseRpm') }}</label>
              <input
                v-model.number="bulkBaseRpm"
                type="number"
                min="1"
                max="1000"
                step="1"
                class="input"
                :placeholder="t('admin.accounts.quotaControl.rpmLimit.baseRpmPlaceholder')"
              />
              <p class="input-hint">{{ t('admin.accounts.quotaControl.rpmLimit.baseRpmHint') }}</p>
            </div>

            <div>
              <label class="input-label text-xs">{{ t('admin.accounts.quotaControl.rpmLimit.strategy') }}</label>
              <div class="flex gap-2">
                <button
                  type="button"
                  @click="bulkRpmStrategy = 'tiered'"
                  :class="[
                    'flex-1 rounded-lg px-3 py-2 text-sm font-medium transition-all',
                    bulkRpmStrategy === 'tiered'
                      ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                      : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
                  ]"
                >
                  {{ t('admin.accounts.quotaControl.rpmLimit.strategyTiered') }}
                </button>
                <button
                  type="button"
                  @click="bulkRpmStrategy = 'sticky_exempt'"
                  :class="[
                    'flex-1 rounded-lg px-3 py-2 text-sm font-medium transition-all',
                    bulkRpmStrategy === 'sticky_exempt'
                      ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                      : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
                  ]"
                >
                  {{ t('admin.accounts.quotaControl.rpmLimit.strategyStickyExempt') }}
                </button>
              </div>
            </div>

            <div v-if="bulkRpmStrategy === 'tiered'">
              <label class="input-label text-xs">{{ t('admin.accounts.quotaControl.rpmLimit.stickyBuffer') }}</label>
              <input
                v-model.number="bulkRpmStickyBuffer"
                type="number"
                min="1"
                step="1"
                class="input"
                :placeholder="t('admin.accounts.quotaControl.rpmLimit.stickyBufferPlaceholder')"
              />
              <p class="input-hint">{{ t('admin.accounts.quotaControl.rpmLimit.stickyBufferHint') }}</p>
            </div>

            </div>
          </div>

        <!-- 用户消息限速模式（独立于 RPM 开关，始终可见） -->
        <div class="mt-4">
          <label class="input-label">{{ t('admin.accounts.quotaControl.rpmLimit.userMsgQueue') }}</label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 mb-2">
            {{ t('admin.accounts.quotaControl.rpmLimit.userMsgQueueHint') }}
          </p>
          <div class="flex space-x-2">
            <button type="button" v-for="opt in umqModeOptions" :key="opt.value"
              @click="userMsgQueueMode = userMsgQueueMode === opt.value ? null : opt.value"
              :class="[
                'px-3 py-1.5 text-sm rounded-md border transition-colors',
                userMsgQueueMode === opt.value
                  ? 'bg-primary-600 text-white border-primary-600'
                  : 'bg-white dark:bg-dark-700 text-gray-700 dark:text-gray-300 border-gray-300 dark:border-dark-500 hover:bg-gray-50 dark:hover:bg-dark-600'
              ]">
              {{ opt.label }}
            </button>
          </div>
        </div>
      </div>

      <!-- Groups -->
      <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-groups-label"
            class="input-label mb-0"
            for="bulk-edit-groups-enabled"
          >
            {{ t('nav.groups') }}
          </label>
          <input
            v-model="enableGroups"
            id="bulk-edit-groups-enabled"
            type="checkbox"
            aria-controls="bulk-edit-groups"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div id="bulk-edit-groups" :class="!enableGroups && 'pointer-events-none opacity-50'">
          <GroupSelector
            v-model="groupIds"
            :groups="groups"
            aria-labelledby="bulk-edit-groups-label"
          />
        </div>
      </div>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="bulk-edit-account-form"
          :disabled="submitting"
          class="btn btn-primary"
        >
          <svg
            v-if="submitting"
            class="-ml-1 mr-2 h-4 w-4 animate-spin"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            />
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            />
          </svg>
          {{
            submitting ? t('admin.accounts.bulkEdit.updating') : t('admin.accounts.bulkEdit.submit')
          }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <ConfirmDialog
    :show="showMixedChannelWarning"
    :title="t('admin.accounts.mixedChannelWarningTitle')"
    :message="mixedChannelWarningMessage"
    :confirm-text="t('common.confirm')"
    :cancel-text="t('common.cancel')"
    :danger="true"
    @confirm="handleMixedChannelConfirm"
    @cancel="handleMixedChannelCancel"
  />
  <EditAccountModal v-if="referenceAccount" :show="true" :account="referenceAccount"
    :proxies="proxies" :groups="groups" :allow-authorize="false" :allow-rate-edit="allowRateEdit"
    :allow-transport-edit="allowTransportEdit" :supplier-scope="supplierScope" :save-account="saveFullEditor" capture-baseline @baseline="initialParameters = $event"
    @close="referenceAccount = null" @updated="referenceAccount = null; emit('updated'); handleClose()" />
</template>

<script setup lang="ts">
import { readApiError } from '@/shared/lib/apiError'

import type { ResourceProxyOption } from '../model/proxyOption'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'

import { buildBulkAccountPatch } from '../model/bulkAccountPatch'
import type { BulkAccountDraft, BulkAccountPatch } from '../model/bulkAccountPatch'
import { bulkTargetEligibility, bulkModelRestrictionDisabled } from '../model/bulkTargetEligibility'
import { bulkResponsesModeApplicable } from '../model/bulkOpenAIPatch'
import { createProviderCredentialOptions } from '@/features/resources/model/providerCredentialOptions'
const { isHeaderOverrideCapable } = createProviderCredentialOptions(useResourceEditorServices().catalog)

import { useResourceEditorServices } from '@/features/resources/services'

import EditAccountModal from './EditAccountModal.vue'
import BulkModelRestrictionPanel from './BulkModelRestrictionPanel.vue'
import BulkErrorCodesPanel from './BulkErrorCodesPanel.vue'
import BulkOpenAICompactMappingPanel from './BulkOpenAICompactMappingPanel.vue'
import BulkOpenAIWebSocketPanel from './BulkOpenAIWebSocketPanel.vue'
import type { ModelMappingEntry } from '../model/modelRestrictions'
import { canEnableOpenAICloud } from '@/features/resources/model/openaiCloud'
import { accountParameterPatch } from '@/features/resources/model/accountParameterPatch'
import type { Account } from '@/features/resources/api/accountTypes';
import { ref, shallowRef, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'
import { DEFAULT_BPS_RECOVERY_INTERVAL_MINUTES, MAX_BPS_RECOVERY_INTERVAL_MINUTES, isValidBPSRecoveryInterval } from '@/features/resources/model/excelBPSRecovery'


import accountsAPI from '@/features/resources/accounts'
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import type { AccountPlatform } from '@/features/resources/api/platformTypes';
import type { OpenAICompactMode, OpenAIEndpointCapability, OpenAIResponsesMode } from '@/features/resources/api/accountCredentialTypes';
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import Select from '@/shared/ui/Select.vue'
import ProxySelector from '@/features/resources/components/ResourceProxySelector.vue'
import GroupSelector from '@/features/resources/components/ResourceGroupSelector.vue'
import ModelWhitelistSelector from '@/features/resources/components/ModelWhitelistSelector.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
const getPresetMappingsByPlatform = useResourceEditorServices().modelPresets

import HeaderOverrideEditor from '@/features/resources/components/HeaderOverrideEditor.vue'
import { validateHeaderOverrideRows, type HeaderOverrideRow } from "@/features/resources/model/credentialsBuilder"
import GrokBaseUrlPresets from '@/features/resources/components/GrokBaseUrlPresets.vue'
import { OPENAI_WS_MODE_OFF } from '@/features/resources/model/openaiWsMode'
import type { OpenAIWSMode } from '@/features/resources/model/openaiWsMode'
interface Props {
  supplierScope?: boolean
  loadReferenceAccount?: () => Promise<Account>
  saveBatch?: (ids: number[], updates: Record<string, unknown>, advanced?: boolean) => ReturnType<typeof accountsAPI.bulkUpdate>

  allowRateEdit?: boolean
  allowAuthorize?: boolean
  allowTransportEdit?: boolean
  show: boolean
  accountIds: number[]
  selectedPlatforms: AccountPlatform[]
  selectedTypes: readonly string[]
  target?: {
    mode: 'selected'
    accountIds?: number[]
    selectedPlatforms?: AccountPlatform[]
    selectedTypes?: readonly string[]
  }
  proxies: readonly ResourceProxyOption[]
  groups: AdminGroup[]
}

const props = withDefaults(defineProps<Props>(), { allowRateEdit: true, allowAuthorize: true, allowTransportEdit: true })
const readBulkTarget = () => [
  props.show, props.target?.accountIds ?? props.accountIds,
  props.target?.selectedPlatforms ?? props.selectedPlatforms,
  props.target?.selectedTypes ?? props.selectedTypes,
  props.allowAuthorize, props.allowTransportEdit, props.allowRateEdit, props.supplierScope
]
const resourceTasks = useResourceTaskScope(readBulkTarget)

const emit = defineEmits<{
  close: []
  updated: []
}>()

const referenceAccount = ref<Account | null>(null)
const initialParameters = ref<Record<string, unknown>>()
type BulkTarget = ReturnType<typeof captureBulkTarget>
const advancedTarget = shallowRef<BulkTarget | null>(null)

async function openFullEditor() {
  if (!props.show || submitting.value) return
  const target = captureBulkTarget()
  referenceAccount.value = null
  advancedTarget.value = null
  initialParameters.value = undefined
  submitting.value = true
  try {
    if (!target.ids.length) throw new Error(t('admin.accounts.bulkEdit.noSelection'))
    // Supplier reads and writes must stay on the injected scoped endpoints.
    if (props.supplierScope && (!target.saveBatch || !target.loadReferenceAccount)) {
      throw new Error(t('admin.accounts.failedToLoad'))
    }
    const account = await target.pending.wait(() => target.loadReferenceAccount
      ? target.loadReferenceAccount()
      : accountsAPI.getById(target.ids[0]))
    if (!target.current()) return
    if (!target.ids.includes(account.id)) throw new Error(t('admin.accounts.failedToLoad'))
    advancedTarget.value = target
    referenceAccount.value = account
  } catch (error) {
    if (target.current() && !(error instanceof StaleResourceTask)) {
      appStore.showError(t('admin.accounts.failedToLoad'))
    }
  } finally {
    if (target.current()) submitting.value = false
  }
}

// The child may retain this callback while its own confirmation is pending.
const saveFullEditor = computed(() => {
  const target = advancedTarget.value
  const reference = referenceAccount.value
  const baseline = initialParameters.value
  return async (id: number, payload: Record<string, unknown>): Promise<Account> => {
    if (!target || !reference || id !== reference.id || !target.current()
      || advancedTarget.value !== target || referenceAccount.value !== reference
      || initialParameters.value !== baseline) throw new StaleResourceTask()
    if (!baseline || submitting.value) throw new Error(t('common.loading'))
    const patch = accountParameterPatch(reference, payload, baseline)
    if (!Object.keys(patch).length) throw new Error(t('admin.accounts.bulkEdit.noChanges'))
    submitting.value = true
    try {
      const result = await target.pending.wait(() => target.saveBatch
        ? target.saveBatch([...target.ids], patch, true)
        : target.directWriter([...target.ids], patch, { view: 'management' }))
      if (!target.current() || advancedTarget.value !== target) throw new StaleResourceTask()
      if (result.failed) throw new Error(t('admin.accounts.bulkEdit.partialSuccess', { success: result.success, failed: result.failed }))
      appStore.showSuccess(t('admin.accounts.bulkEdit.success', { count: result.success }))
      return reference
    } finally {
      if (target.current()) submitting.value = false
    }
  }
})

const { t } = useI18n()
const resourceServices = useResourceEditorServices()
const appStore = resourceServices.notices
const authStore = resourceServices.access

// Platform awareness
const targetAccountIds = computed(() => props.target?.accountIds ?? props.accountIds)
const targetSelectedPlatforms = computed(() => props.target?.selectedPlatforms ?? props.selectedPlatforms)
const targetSelectedTypes = computed(() => props.target?.selectedTypes ?? props.selectedTypes)
// Grok 快捷端点仅在所选账号全部为 grok 平台时展示（其他平台不显示）
const allTargetsGrok = computed(
  () =>
    targetSelectedPlatforms.value.length > 0 &&
    targetSelectedPlatforms.value.every((p) => p === 'grok')
)
const isMixedPlatform = computed(() => targetSelectedPlatforms.value.length > 1)

const bulkEligibility = computed(() => bulkTargetEligibility(
  targetSelectedPlatforms.value, targetSelectedTypes.value, isHeaderOverrideCapable
))
const allOpenAIPassthroughCapable = computed(() => bulkEligibility.value.openAIPassthrough)
const allOpenAIOAuth = computed(() => bulkEligibility.value.openAIOAuth)
const allOpenAIOAuthOnly = computed(() => bulkEligibility.value.openAIOAuthOnly)
const allOpenAIAPIKey = computed(() => bulkEligibility.value.openAIAPIKey)
const allBillingProbeCapable = computed(() => bulkEligibility.value.billingProbe)
const allHeaderOverrideCapable = computed(() => bulkEligibility.value.headerOverride)
const allAnthropicOAuthOrSetupToken = computed(() => bulkEligibility.value.anthropicOAuth)

const filteredPresets = computed(() => {
  if (targetSelectedPlatforms.value.length === 0) return []

  const dedupedPresets = new Map<string, ReturnType<typeof getPresetMappingsByPlatform>[number]>()
  for (const platform of targetSelectedPlatforms.value) {
    for (const preset of getPresetMappingsByPlatform(platform)) {
      const key = `${preset.from}=>${preset.to}`
      if (!dedupedPresets.has(key)) {
        dedupedPresets.set(key, preset)
      }
    }
  }

  return Array.from(dedupedPresets.values())
})

// State - field enable flags
const enableBaseUrl = ref(false)
const enableModelRestriction = ref(false)
const enableCustomErrorCodes = ref(false)
const enableInterceptWarmup = ref(false)
const enableHeaderOverride = ref(false)
const enableProxy = ref(false)
const enableConcurrency = ref(false)
const enableLoadFactor = ref(false)
const enablePriority = ref(false)
const enableRateMultiplier = ref(false)
const enableStatus = ref(false)
const enableGroups = ref(false)
const enableCloud = ref(false)
const cloudEnabled = ref(false)
const cloudEligibilityLoading = ref(false)
const cloudEligibilityError = ref('')
watch(() => [JSON.stringify(readBulkTarget()), enableCloud.value, props.saveBatch, props.loadReferenceAccount, authStore.canAuthorize, authStore.isSuperAdmin], async (_, __, onCleanup) => {
const capturedShow = props.show
const capturedSupplierScope = props.supplierScope
const capturedAllowTransportEdit = props.allowTransportEdit

const pending = resourceTasks.capture()
try {

  let current = true
  onCleanup(() => { current = false })
  cloudEligibilityError.value = ''
  cloudEligibilityLoading.value = false
  if (!capturedShow || !enableCloud.value || capturedSupplierScope || !capturedAllowTransportEdit || !allOpenAIOAuthOnly.value) return
  cloudEligibilityLoading.value = true
  const ids = [...targetAccountIds.value]
  const invalid: string[] = []
  let cursor = 0, failed = false
  await pending.wait(async () => Promise.all(Array.from({ length: Math.min(4, ids.length) }, async () => {
const pending = resourceTasks.capture()
try {

    while (current && cursor < ids.length) {
      const id = ids[cursor++]
      try {
        const account = await pending.wait(async () => accountsAPI.getById(id))
        if (!canEnableOpenAICloud(account)) invalid.push(`${account?.name || ''} #${id}`.trim())
      } catch {
if (!pending.current()) throw new StaleResourceTask()
 failed = true }
    }
  
} catch (failure: unknown) {
  if (failure instanceof StaleResourceTask) { return }
  throw failure
}
})))
  if (!current) return
  cloudEligibilityLoading.value = false
  cloudEligibilityError.value = failed ? t('admin.accounts.failedToLoad')
    : invalid.length ? t('admin.accounts.openai.cloud.ineligibleAccounts', { accounts: invalid.join(', ') }) : ''
  if (cloudEligibilityError.value) cloudEnabled.value = false

} catch (failure: unknown) {
  if (failure instanceof StaleResourceTask) { return }
  throw failure
}
})

const enableExcelBPS = ref(false)
const enableOpenAIPassthrough = ref(false)
const enableOpenAIFlattenNamespaces = ref(false)
const enableOpenAILongContextBilling = ref(false)
const enableOpenAIEndpointCapabilities = ref(false)
const enableOpenAIResponsesMode = ref(false)
const enableOpenAIWSMode = ref(false)
const enableOpenAIAPIKeyWSMode = ref(false)
const enableUpstreamBillingAutoProbe = ref(false)
const enableCodexCLIOnly = ref(false)
const enableCodexCLIOnlyAppServer = ref(false)
const enableOpenAICompactMode = ref(false)
const enableOpenAICompactModelMapping = ref(false)
const enableRpmLimit = ref(false)

// State - field values
const submitting = ref(false)
const showMixedChannelWarning = ref(false)
const mixedChannelWarningMessage = ref('')
type BulkSubmission = ReturnType<typeof captureBulkSubmission>
const pendingSubmission = shallowRef<BulkSubmission | null>(null)
const baseUrl = ref('')
const canEditModelCredentials = computed(() => props.allowAuthorize && authStore.canManage && authStore.canAuthorize
  && (authStore.canEditAdminField?.('account.bulk', 'credentials') ?? authStore.isSuperAdmin))
const modelRestrictionMode = ref<'whitelist' | 'mapping'>('whitelist')
const allowedModels = ref<string[]>([])
const modelMappings = ref<ModelMappingEntry[]>([])
const selectedErrorCodes = ref<number[]>([])
const customErrorCodeInput = ref<number | string | null>(null)
const interceptWarmupRequests = ref(false)
const headerOverrideEnabled = ref(false)
const headerOverrideRows = ref<HeaderOverrideRow[]>([])
const proxyId = ref<number | null>(null)
const concurrency = ref(1)
const loadFactor = ref<number | null>(null)
const priority = ref(1)
const rateMultiplier = ref(1)
const status = ref<'active' | 'inactive'>('active')
const groupIds = ref<number[]>([])
const excelBPSEnabled = ref(false)
const excelBPSModels = ref<string[]>([])
const excelBPSCacheCreationAsInput = ref(false)
const excelBPSAutoDisableOn403 = ref(false)
const excelBPSAutoRecoverOn403 = ref(false)
const excelBPSRecoveryIntervalMinutes = ref<number | string>(DEFAULT_BPS_RECOVERY_INTERVAL_MINUTES)
const excelBPSOmitUnsupportedTools = ref(false)
const excelBPSIgnoreEncryptedContent = ref(false)
const excelBPSAutoMoveOn403 = ref(false)
const excelBPS403TargetGroupID = ref<number | string>('')
const excelBPS403GroupOptions = computed(() => [
  { value: '', label: t('admin.accounts.openai.excelBPS403SelectTarget') },
  { value: 0, label: t('admin.accounts.openai.excelBPS403LeaveAllGroups') },
  ...props.groups
    .filter(group => group.platform === 'openai' || (!authStore.isSimpleMode && group.platform === 'composite'))
    .map(group => ({ value: group.id, label: group.name }))
])
const openaiPassthroughEnabled = ref(false)
// Codex namespace 工具摊平兼容开关（仅 OAuth），缺省关闭即原样保留
const openaiFlattenNamespacesEnabled = ref(false)
const openAILongContextBillingEnabled = ref(false)
const openAIEndpointCapabilities = ref<OpenAIEndpointCapability[]>([
  'chat_completions',
  'embeddings'
])
const openAIResponsesMode = ref<OpenAIResponsesMode>('auto')
const openaiOAuthResponsesWebSocketV2Mode = ref<OpenAIWSMode>(OPENAI_WS_MODE_OFF)
const openaiAPIKeyResponsesWebSocketV2Mode = ref<OpenAIWSMode>(OPENAI_WS_MODE_OFF)
const upstreamBillingAutoProbeMode = ref<'enabled' | 'disabled'>('enabled')
const codexCLIOnlyEnabled = ref(false)
const codexCLIOnlyAppServerEnabled = ref(false)
type CodexFingerprintMode = 'off' | 'device' | 'session' | 'full'
const enableCodexFingerprintMode = ref(false)
const codexFingerprintMode = ref<CodexFingerprintMode>('off')
const codexFingerprintModeOptions = computed(() => [
  { value: 'off' as CodexFingerprintMode, label: t('admin.accounts.openai.codexFingerprintOff') },
  { value: 'device' as CodexFingerprintMode, label: t('admin.accounts.openai.codexFingerprintDevice') },
  { value: 'session' as CodexFingerprintMode, label: t('admin.accounts.openai.codexFingerprintSession') },
  { value: 'full' as CodexFingerprintMode, label: t('admin.accounts.openai.codexFingerprintFull') },
])
const openAICompactMode = ref<OpenAICompactMode>('auto')
const openAICompactModelMappings = ref<ModelMappingEntry[]>([])
const rpmLimitEnabled = ref(false)
const bulkBaseRpm = ref<number | null>(null)
const bulkRpmStrategy = ref<'tiered' | 'sticky_exempt'>('tiered')
const bulkRpmStickyBuffer = ref<number | null>(null)
const userMsgQueueMode = ref<string | null>(null)
const umqModeOptions = computed(() => [
  { value: '', label: t('admin.accounts.quotaControl.rpmLimit.umqModeOff') },
  { value: 'throttle', label: t('admin.accounts.quotaControl.rpmLimit.umqModeThrottle') },
  { value: 'serialize', label: t('admin.accounts.quotaControl.rpmLimit.umqModeSerialize') },
])

const statusOptions = computed(() => [
  { value: 'active', label: t('common.active') },
  { value: 'inactive', label: t('common.inactive') }
])
const upstreamBillingAutoProbeOptions = computed(() => [
  { value: 'enabled', label: t('common.enabled') },
  { value: 'disabled', label: t('common.disabled') }
])
const isOpenAIModelRestrictionDisabled = computed(() => bulkModelRestrictionDisabled(
  bulkEligibility.value, props.allowTransportEdit && enableOpenAIPassthrough.value
    ? openaiPassthroughEnabled.value : undefined
))

const openAICompactModeOptions = computed(() => [
  { value: 'auto', label: t('admin.accounts.openai.compactModeAuto') },
  { value: 'force_on', label: t('admin.accounts.openai.compactModeForceOn') },
  { value: 'force_off', label: t('admin.accounts.openai.compactModeForceOff') }
])
const openAIResponsesModeOptions = computed(() => [
  { value: 'auto', label: t('admin.accounts.openai.responsesModeAuto') },
  { value: 'force_responses', label: t('admin.accounts.openai.responsesModeForceResponses') },
  {
    value: 'force_chat_completions',
    label: t('admin.accounts.openai.responsesModeForceChatCompletions')
  }
])
const openAITextEndpointCapabilityLabel = computed(() => {
  if (openAIResponsesMode.value === 'force_responses') {
    return t('admin.accounts.openai.capabilityResponses')
  }
  if (openAIResponsesMode.value === 'force_chat_completions') {
    return t('admin.accounts.openai.capabilityChatCompletions')
  }
  return t('admin.accounts.openai.capabilityTextAuto')
})
const openAIEndpointCapabilityOptions = computed<
  Array<{ value: OpenAIEndpointCapability; label: string }>
>(() => [
  { value: 'chat_completions', label: openAITextEndpointCapabilityLabel.value },
  { value: 'embeddings', label: t('admin.accounts.openai.capabilityEmbeddings') },
  { value: 'seedance', label: 'Seedance (Ark)' }
])
const openAITextGenerationCapabilityEnabled = computed(() =>
  openAIEndpointCapabilities.value.includes('chat_completions')
)
const openAIResponsesModeApplicable = computed(() => bulkResponsesModeApplicable(
  props.allowAuthorize && enableOpenAIEndpointCapabilities.value ? openAIEndpointCapabilities.value : undefined
))

const normalizeOpenAIEndpointCapabilities = (values: OpenAIEndpointCapability[]) => {
  const allowed: OpenAIEndpointCapability[] = ['chat_completions', 'embeddings', 'seedance']
  const selected = allowed.filter((value) => values.includes(value))
  return selected.length > 0 ? selected : ['chat_completions', 'embeddings'] as OpenAIEndpointCapability[]
}

const toggleOpenAIEndpointCapability = (
  capability: OpenAIEndpointCapability,
  event?: Event
) => {
  if (openAIEndpointCapabilities.value.includes(capability)) {
    if (openAIEndpointCapabilities.value.length <= 1) {
      const input = event?.target as HTMLInputElement | null
      if (input) input.checked = true
      return
    }
    openAIEndpointCapabilities.value = openAIEndpointCapabilities.value.filter(
      (value) => value !== capability
    )
    if (!openAITextGenerationCapabilityEnabled.value) {
      openAIResponsesMode.value = 'auto'
    }
    return
  }
  openAIEndpointCapabilities.value = normalizeOpenAIEndpointCapabilities([
    ...openAIEndpointCapabilities.value,
    capability
  ])
}
// Capture selected values only. Permission, target eligibility and JSONB merge
// projection belong to the pure bulk models; reactive state and validation stay here.
function readBulkDraft(): BulkAccountDraft {
  return {
    fields: {
      proxyId: enableProxy.value ? proxyId.value : undefined,
      concurrency: enableConcurrency.value ? concurrency.value : undefined,
      loadFactor: enableLoadFactor.value ? loadFactor.value : undefined,
      priority: enablePriority.value ? priority.value : undefined,
      rateMultiplier: enableRateMultiplier.value ? rateMultiplier.value : undefined,
      status: enableStatus.value ? status.value : undefined,
      groupIds: enableGroups.value ? groupIds.value : undefined,
      upstreamBillingAutoProbe: enableUpstreamBillingAutoProbe.value ? upstreamBillingAutoProbeMode.value : undefined
    },
    credentials: {
      baseUrl: enableBaseUrl.value ? baseUrl.value : undefined,
      modelRestriction: enableModelRestriction.value && canEditModelCredentials.value ? {
        mode: modelRestrictionMode.value, allowedModels: allowedModels.value, mappings: modelMappings.value
      } : undefined,
      customErrorCodes: enableCustomErrorCodes.value ? selectedErrorCodes.value : undefined,
      interceptWarmup: enableInterceptWarmup.value ? interceptWarmupRequests.value : undefined,
      headerOverride: enableHeaderOverride.value ? {
        enabled: headerOverrideEnabled.value, rows: headerOverrideRows.value
      } : undefined,
      compactModelMappings: enableOpenAICompactModelMapping.value ? openAICompactModelMappings.value : undefined
    },
    openai: {
      cloud: enableCloud.value ? cloudEnabled.value : undefined,
      bps: enableExcelBPS.value ? {
        enabled: excelBPSEnabled.value,
        models: excelBPSModels.value,
        cacheCreationAsInput: excelBPSCacheCreationAsInput.value,
        autoDisableOn403: excelBPSAutoDisableOn403.value, autoRecoverOn403: excelBPSAutoRecoverOn403.value,
        recoveryIntervalMinutes: excelBPSRecoveryIntervalMinutes.value,
        omitUnsupportedTools: excelBPSOmitUnsupportedTools.value,
        ignoreEncryptedContent: excelBPSIgnoreEncryptedContent.value,
        autoMoveOn403: excelBPSAutoMoveOn403.value, targetGroupID: excelBPS403TargetGroupID.value
      } : undefined,
      passthrough: enableOpenAIPassthrough.value ? openaiPassthroughEnabled.value : undefined,
      flattenNamespaces: enableOpenAIFlattenNamespaces.value ? openaiFlattenNamespacesEnabled.value : undefined,
      longContextBilling: enableOpenAILongContextBilling.value ? openAILongContextBillingEnabled.value : undefined,
      endpointCapabilities: enableOpenAIEndpointCapabilities.value ? openAIEndpointCapabilities.value : undefined,
      responsesMode: enableOpenAIResponsesMode.value ? openAIResponsesMode.value : undefined,
      oauthWSMode: enableOpenAIWSMode.value ? openaiOAuthResponsesWebSocketV2Mode.value : undefined,
      apiKeyWSMode: enableOpenAIAPIKeyWSMode.value ? openaiAPIKeyResponsesWebSocketV2Mode.value : undefined,
      codexCLIOnly: enableCodexCLIOnly.value ? codexCLIOnlyEnabled.value : undefined,
      codexCLIOnlyAppServer: enableCodexCLIOnlyAppServer.value ? codexCLIOnlyAppServerEnabled.value : undefined,
      fingerprintMode: enableCodexFingerprintMode.value ? codexFingerprintMode.value : undefined,
      compactMode: enableOpenAICompactMode.value ? openAICompactMode.value : undefined
    },
    anthropic: {
      rpm: enableRpmLimit.value ? {
        enabled: rpmLimitEnabled.value, baseRpm: bulkBaseRpm.value,
        strategy: bulkRpmStrategy.value, stickyBuffer: bulkRpmStickyBuffer.value
      } : undefined,
      userMsgQueueMode: userMsgQueueMode.value
    }
  }
}

function captureBulkTarget() {
  const pending = resourceTasks.capture()
  const saveBatch = props.saveBatch
  const directWriter = accountsAPI.bulkUpdate
  const loadReferenceAccount = props.loadReferenceAccount
  return {
    pending, saveBatch, directWriter, loadReferenceAccount,
    ids: [...targetAccountIds.value],
    current: () => pending.current() && props.show && props.saveBatch === saveBatch
      && props.loadReferenceAccount === loadReferenceAccount
      && (saveBatch !== undefined || accountsAPI.bulkUpdate === directWriter)
  }
}

// A confirmation retains the exact target, writer and payload that produced the warning.
function captureBulkSubmission(updates: BulkAccountPatch) {
  const target = captureBulkTarget()
  const platform = targetSelectedPlatforms.value[0]
  const groups = [...groupIds.value]
  const requiresPreCheck = !props.supplierScope && enableGroups.value && groups.length > 0
    && targetSelectedPlatforms.value.length === 1
    && (platform === 'antigravity' || platform === 'anthropic')
  return { ...target, platform, groups, requiresPreCheck, updates }
}

const clearPendingSubmission = () => {
  pendingSubmission.value = null
  showMixedChannelWarning.value = false
  mixedChannelWarningMessage.value = ''
}

const handleClose = () => {
  resourceTasks.invalidate()
  submitting.value = false
  referenceAccount.value = null
  initialParameters.value = undefined
  advancedTarget.value = null
  clearPendingSubmission()
  emit('close')
}

const preCheckMixedChannelRisk = async (command: BulkSubmission): Promise<boolean> => {
  if (!command.current()) return false
  if (!command.requiresPreCheck) return true
  try {
    const result = await command.pending.wait(() => accountsAPI.checkMixedChannelRisk({
      platform: command.platform, group_ids: [...command.groups]
    }))
    if (!command.current()) return false
    if (!result.has_risk) return true
    pendingSubmission.value = command
    mixedChannelWarningMessage.value = result.message || t('admin.accounts.bulkEdit.failed')
    showMixedChannelWarning.value = true
    return false
  } catch (error: unknown) {
    const errorDetails = readApiError(error)
    if (!command.current() || error instanceof StaleResourceTask) return false
    appStore.showError(errorDetails.message || t('admin.accounts.bulkEdit.failed'))
    return false
  }
}

const handleSubmit = async () => {
  if (!props.show || submitting.value) return
  clearPendingSubmission()
  if (props.supplierScope && !props.saveBatch) {
    appStore.showError(t('admin.accounts.bulkEdit.failed'))
    return
  }
  const applyCloud = props.allowTransportEdit && !props.supplierScope && enableCloud.value && allOpenAIOAuthOnly.value
  const applyExcelBPS = props.allowTransportEdit && enableExcelBPS.value && allOpenAIOAuthOnly.value
  if (applyCloud && cloudEligibilityLoading.value) return
  if (applyCloud && cloudEnabled.value && cloudEligibilityError.value) {
    appStore.showError(cloudEligibilityError.value)
    return
  }
  if (targetAccountIds.value.length === 0) {
    appStore.showError(t('admin.accounts.bulkEdit.noSelection'))
    return
  }
  if (applyExcelBPS && excelBPSEnabled.value && excelBPSAutoDisableOn403.value && excelBPSAutoRecoverOn403.value && !isValidBPSRecoveryInterval(excelBPSRecoveryIntervalMinutes.value)) {
    appStore.showError(t('admin.accounts.openai.excelBPS403RecoveryIntervalInvalid'))
    return
  }
  if (applyExcelBPS && excelBPSEnabled.value && excelBPSAutoMoveOn403.value) {
    const target = Number(excelBPS403TargetGroupID.value)
    if (excelBPS403TargetGroupID.value === '' || !Number.isSafeInteger(target) || target < 0 ||
      !excelBPS403GroupOptions.value.some(option => option.value === target)) {
      appStore.showError(t('admin.accounts.openai.excelBPS403SelectTarget'))
      return
    }
  }
  if (props.allowTransportEdit && enableBaseUrl.value) {
    const value = baseUrl.value.trim()
    if (value && !/^https?:\/\//i.test(value)) {
      appStore.showError(t('admin.accounts.grokCustomBaseUrl.invalid'))
      return
    }
  }
  if (props.allowAuthorize && allHeaderOverrideCapable.value && enableHeaderOverride.value && headerOverrideEnabled.value) {
    if (!headerOverrideRows.value.some(row => row.name.trim())) {
      appStore.showError(t('admin.accounts.headerOverride.bulkEmptyRows'))
      return
    }
    const headerError = validateHeaderOverrideRows(headerOverrideRows.value)
    if (headerError) {
      appStore.showError(t('admin.accounts.headerOverride.' + headerError))
      return
    }
  }
  const built = buildBulkAccountPatch(readBulkDraft(), {
    allowAuthorize: props.allowAuthorize, allowTransportEdit: props.allowTransportEdit,
    allowRateEdit: props.allowRateEdit, supplierScope: props.supplierScope
  }, bulkEligibility.value)
  if (!built) {
    appStore.showError(t('admin.accounts.bulkEdit.noFieldsSelected'))
    return
  }
  // The builder creates detached nested objects and copies selected arrays.
  const command = captureBulkSubmission(built)
  submitting.value = true
  try {
    if (!await preCheckMixedChannelRisk(command)) return
    await submitBulkUpdate(command)
  } finally {
    if (command.current()) submitting.value = false
  }
}

const submitBulkUpdate = async (command: BulkSubmission, confirmed = false) => {
  if (!command.current()) return
  const updates = confirmed
    ? { ...command.updates, confirm_mixed_channel_risk: true }
    : { ...command.updates }
  submitting.value = true
  try {
    const result = await command.pending.wait(() => command.saveBatch
      ? command.saveBatch([...command.ids], updates)
      : command.directWriter([...command.ids], updates))
    if (!command.current()) return
    const success = result.success || 0
    const failed = result.failed || 0
    const inherited = result.long_context_inherited_count || 0
    if (success > 0 && failed === 0) {
      appStore.showSuccess(inherited > 0
        ? t('admin.accounts.bulkEdit.successWithInherited', { count: success, inherited })
        : t('admin.accounts.bulkEdit.success', { count: success }))
    } else if (success > 0) {
      appStore.showError(t(inherited > 0
        ? 'admin.accounts.bulkEdit.partialSuccessWithInherited'
        : 'admin.accounts.bulkEdit.partialSuccess', { success, failed, inherited }))
    } else {
      appStore.showError(t('admin.accounts.bulkEdit.failed'))
    }
    if (success > 0) {
      clearPendingSubmission()
      emit('updated')
      handleClose()
    }
  } catch (error: unknown) {
    const errorDetails = readApiError(error)
    if (!command.current() || error instanceof StaleResourceTask) return
    if (errorDetails.status === 409 && errorDetails.error === 'mixed_channel_warning') {
      pendingSubmission.value = command
      mixedChannelWarningMessage.value = errorDetails.message || t('admin.accounts.bulkEdit.failed')
      showMixedChannelWarning.value = true
    } else if (errorDetails.status === 409 && errorDetails.reason === 'OPENAI_EXCEL_BPS_STATE_CONFLICT') {
      appStore.showError(t('admin.accounts.openai.excelBPSBulkStateConflict'))
    } else if (errorDetails.reason === 'UPSTREAM_BILLING_RATE_SYNC_BULK_CONFLICT') {
      appStore.showError(t('admin.accounts.bulkEdit.rateSyncConflict', { count: errorDetails.metadata?.count ?? 1 }))
    } else if (errorDetails.reason === 'OPENAI_LONG_CONTEXT_PARENT_REQUIRED') {
      appStore.showError(t('admin.accounts.bulkEdit.longContextParentRequired'))
    } else {
      appStore.showError(errorDetails.message || t('admin.accounts.bulkEdit.failed'))
      console.error('Error bulk updating accounts:')
    }
  } finally {
    if (command.current()) submitting.value = false
  }
}

const handleMixedChannelConfirm = async () => {
  const command = pendingSubmission.value
  if (submitting.value) return
  clearPendingSubmission()
  if (command?.current()) await submitBulkUpdate(command, true)
}

const handleMixedChannelCancel = () => { clearPendingSubmission() }

// Reset form when modal closes
watch(
  () => props.show,
  (newShow) => {
    if (!newShow) {
      // Reset all enable flags
      enableBaseUrl.value = false
      enableModelRestriction.value = false
      enableCustomErrorCodes.value = false
      enableInterceptWarmup.value = false
      enableHeaderOverride.value = false
      enableProxy.value = false
      enableConcurrency.value = false
      enableLoadFactor.value = false
      enablePriority.value = false
      enableRateMultiplier.value = false
      enableStatus.value = false
      enableGroups.value = false
      enableCloud.value = false
      cloudEnabled.value = false
      enableExcelBPS.value = false
      enableOpenAIPassthrough.value = false
      enableOpenAIFlattenNamespaces.value = false
      enableOpenAILongContextBilling.value = false
      enableOpenAIEndpointCapabilities.value = false
      enableOpenAIResponsesMode.value = false
      enableOpenAIWSMode.value = false
      enableOpenAIAPIKeyWSMode.value = false
      enableUpstreamBillingAutoProbe.value = false
      enableCodexCLIOnly.value = false
      enableCodexCLIOnlyAppServer.value = false
      enableCodexFingerprintMode.value = false
      codexFingerprintMode.value = 'off'
      enableOpenAICompactMode.value = false
      enableOpenAICompactModelMapping.value = false
      enableRpmLimit.value = false

      // Reset all values
      baseUrl.value = ''
      excelBPSEnabled.value = false
      excelBPSModels.value = []
      excelBPSCacheCreationAsInput.value = false
      excelBPSAutoDisableOn403.value = false
      excelBPSAutoRecoverOn403.value = false
      excelBPSRecoveryIntervalMinutes.value = DEFAULT_BPS_RECOVERY_INTERVAL_MINUTES
      excelBPSOmitUnsupportedTools.value = false
      excelBPSIgnoreEncryptedContent.value = false
      excelBPSAutoMoveOn403.value = false
      excelBPS403TargetGroupID.value = ''
      openaiPassthroughEnabled.value = false
      openaiFlattenNamespacesEnabled.value = false
      openAILongContextBillingEnabled.value = false
      openAIEndpointCapabilities.value = ['chat_completions', 'embeddings']
      openAIResponsesMode.value = 'auto'
      modelRestrictionMode.value = 'whitelist'
      allowedModels.value = []
      modelMappings.value = []
      selectedErrorCodes.value = []
      customErrorCodeInput.value = null
      interceptWarmupRequests.value = false
      headerOverrideEnabled.value = false
      headerOverrideRows.value = []
      proxyId.value = null
      concurrency.value = 1
      loadFactor.value = null
      priority.value = 1
      rateMultiplier.value = 1
      status.value = 'active'
      groupIds.value = []
      openaiOAuthResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
      openaiAPIKeyResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
      upstreamBillingAutoProbeMode.value = 'enabled'
      codexCLIOnlyEnabled.value = false
      codexCLIOnlyAppServerEnabled.value = false
      openAICompactMode.value = 'auto'
      openAICompactModelMappings.value = []
      rpmLimitEnabled.value = false
      bulkBaseRpm.value = null
      bulkRpmStrategy.value = 'tiered'
      bulkRpmStickyBuffer.value = null
      userMsgQueueMode.value = null

      // Reset mixed channel warning state
      showMixedChannelWarning.value = false
      mixedChannelWarningMessage.value = ''
      clearPendingSubmission()
    }
  }
)
// Revoke approval and asynchronous editors while retaining allowed field drafts.
watch([
  () => JSON.stringify(readBulkTarget()), () => props.saveBatch, () => props.loadReferenceAccount,
  () => authStore.canAuthorize, () => authStore.canManage, () => canEditModelCredentials.value, () => authStore.isSuperAdmin
], () => {
  resourceTasks.invalidate()
  clearPendingSubmission()
  submitting.value = false
  referenceAccount.value = null
  initialParameters.value = undefined
  advancedTarget.value = null
  cloudEligibilityLoading.value = false
  cloudEligibilityError.value = ''
}, { flush: 'sync' })
const readResourceScope = useResourceEditorServices().scopeKey
watch(readResourceScope, handleClose, { flush: 'sync' })
</script>
