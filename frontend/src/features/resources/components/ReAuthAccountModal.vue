<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.reAuthorizeAccount')"
    width="normal"
    @close="handleClose"
  >
    <div v-if="account" class="space-y-4">
      <!-- Account Info -->
      <div
        class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-700"
      >
        <div class="flex items-center gap-3">
          <div
            :class="[
              'flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br',
              isOpenAILike
                ? 'from-green-500 to-green-600'
                : isGemini
                  ? 'from-blue-500 to-blue-600'
                  : isAntigravity
                    ? 'from-purple-500 to-purple-600'
                    : isGrok
                      ? 'from-zinc-700 to-zinc-900'
                      : 'from-orange-500 to-orange-600'
            ]"
          >
            <Icon name="sparkles" size="md" class="text-white" />
          </div>
          <div>
            <span class="block font-semibold text-gray-900 dark:text-white">{{
              account.name
            }}</span>
            <span class="text-sm text-gray-500 dark:text-gray-400">
              {{
                isOpenAI
                  ? t('admin.accounts.openaiAccount')
                  : isGemini
                    ? t('admin.accounts.geminiAccount')
                    : isAntigravity
                      ? t('admin.accounts.antigravityAccount')
                      : isGrok
                        ? t('admin.accounts.grokAccount')
                        : t('admin.accounts.claudeCodeAccount')
              }}
            </span>
          </div>
        </div>
      </div>

      <!-- Add Method Selection (Claude only) -->
      <fieldset v-if="isAnthropic" class="border-0 p-0">
        <legend class="input-label">{{ t('admin.accounts.oauth.authMethod') }}</legend>
        <div class="mt-2 flex gap-4">
          <label class="flex cursor-pointer items-center">
            <input
              v-model="addMethod"
              type="radio"
              value="oauth"
              :disabled="claudeOAuth.loading.value"
              class="mr-2 text-primary-600 focus:ring-primary-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{
              t('admin.accounts.oauth.auth')
            }}</span>
          </label>
          <label class="flex cursor-pointer items-center">
            <input
              v-model="addMethod"
              type="radio"
              value="setup-token"
              :disabled="claudeOAuth.loading.value"
              class="mr-2 text-primary-600 focus:ring-primary-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{
              t('admin.accounts.oauth.oauthToken')
            }}</span>
          </label>
          <label class="flex cursor-pointer items-center">
            <input
              v-model="addMethod"
              type="radio"
              value="setup-token-manual"
              :disabled="claudeOAuth.loading.value"
              class="mr-2 text-primary-600 focus:ring-primary-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{
              t('admin.accounts.oauth.setupTokenLongLived')
            }}</span>
          </label>
        </div>
      </fieldset>

      <!-- Gemini OAuth Type Display (read-only) -->
      <div v-if="isGemini" class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-700">
        <div class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.accounts.oauth.gemini.oauthTypeLabel') }}
        </div>
        <div class="flex items-center gap-3">
          <div
            :class="[
              'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
              geminiOAuthType === 'google_one'
                ? 'bg-purple-500 text-white'
                : geminiOAuthType === 'code_assist'
                  ? 'bg-blue-500 text-white'
                  : 'bg-amber-500 text-white'
            ]"
          >
            <Icon v-if="geminiOAuthType === 'google_one'" name="user" size="sm" />
            <Icon v-else-if="geminiOAuthType === 'code_assist'" name="cloud" size="sm" />
            <Icon v-else name="sparkles" size="sm" />
          </div>
          <div>
            <span class="block text-sm font-medium text-gray-900 dark:text-white">
              {{
                geminiOAuthType === 'google_one'
                  ? 'Google One'
                  : geminiOAuthType === 'code_assist'
                    ? t('admin.accounts.gemini.oauthType.builtInTitle')
                    : t('admin.accounts.gemini.oauthType.customTitle')
              }}
            </span>
            <span class="text-xs text-gray-500 dark:text-gray-400">
              {{
                geminiOAuthType === 'google_one'
                  ? t('admin.accounts.gemini.oauthType.googleOneDesc')
                  : geminiOAuthType === 'code_assist'
                    ? t('admin.accounts.gemini.oauthType.builtInDesc')
                    : t('admin.accounts.gemini.oauthType.customDesc')
              }}
            </span>
          </div>
        </div>
      </div>

      <OAuthAuthorizationFlow
        ref="oauthFlowRef"
        :add-method="addMethod"
        :auth-url="currentAuthUrl"
        :session-id="currentSessionId"
        :loading="currentLoading"
        :error="currentError"
        :show-help="isAnthropic"
        :show-proxy-warning="isAnthropic"
        :show-cookie-option="isAnthropic && addMethod !== 'setup-token-manual'"
        :show-manual-option="addMethod !== 'setup-token-manual'"
        :show-setup-token-option="isAnthropic && addMethod === 'setup-token-manual'"
        :show-refresh-token-option="isOpenAI || isAntigravity || isGrok"
        :show-sso-option="isGrok"
        :show-email-password-option="false"
        :allow-multiple="false"
        :method-label="t('admin.accounts.inputMethod')"
        :platform="isOpenAI ? 'openai' : isGemini ? 'gemini' : isAntigravity ? 'antigravity' : isGrok ? 'grok' : 'anthropic'"
        :show-project-id="isGemini && geminiOAuthType === 'code_assist'"
        :initial-input-method="grokInitialInputMethod"
        @generate-url="handleGenerateUrl"
        @cookie-auth="handleCookieAuth"
        @validate-refresh-token="handleValidateRefreshToken"
        @import-sso="handleGrokImportSSO"
      />

    </div>

    <template #footer>
      <div v-if="account" class="flex justify-between gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          v-if="isManualInputMethod"
          type="button"
          :disabled="!canExchangeCode"
          class="btn btn-primary"
          @click="handleExchangeCode"
        >
          <svg
            v-if="currentLoading"
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
            ></circle>
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
          {{
            currentLoading
              ? t('admin.accounts.oauth.verifying')
              : t('admin.accounts.oauth.completeAuth')
          }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { readApiError } from '@/shared/lib/apiError'

import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { ref,computed,watch,onScopeDispose } from 'vue'
import { useResourceTaskScope,StaleResourceTask } from '../queries/resourceTaskScope'
import { useI18n } from 'vue-i18n'
import { useResourceEditorServices } from '../services'
import accountsAPI from '../accounts'
import {
  buildClaudeSetupTokenCredentials,
  useAccountOAuth,
  type AddMethod,
  type AuthInputMethod
} from '@/features/resources/queries/useAccountOAuth'
import { useOpenAIOAuth } from '@/features/resources/queries/useOpenAIOAuth'
import { useGeminiOAuth } from '@/features/resources/queries/useGeminiOAuth'
import { useAntigravityOAuth } from '@/features/resources/queries/useAntigravityOAuth'
import { useGrokOAuth } from '@/features/resources/queries/useGrokOAuth'
import type { GrokTokenInfo } from '@/features/resources/grok'
import type { Account } from '@/features/resources/api/accountTypes';
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import OAuthAuthorizationFlow from './OAuthAuthorizationFlow.vue'

// Type for exposed OAuthAuthorizationFlow component
// Note: defineExpose automatically unwraps refs, so we use the unwrapped types
interface OAuthFlowExposed {
  authCode: string
  oauthState: string
  projectId: string
  sessionKey: string
  setupToken: string
  inputMethod: AuthInputMethod
  reset: () => void
}

interface Props {
  allowExtraEdit?: boolean
  show: boolean
  account: Account|null
}

const props=withDefaults(defineProps<Props>(),{ allowExtraEdit: true })
const services=useResourceEditorServices()
const resourceTasks=useResourceTaskScope(() => [props.show,props.account?.id,props.account?.platform,props.account?.proxy_id])
const emit=defineEmits<{
  close: []
  reauthorized: [account: Account]
}>()

const appStore=services.notices
const { t }=useI18n()

// OAuth composables
const claudeOAuth=useAccountOAuth()
const openaiOAuth=useOpenAIOAuth()
const geminiOAuth=useGeminiOAuth()
const antigravityOAuth=useAntigravityOAuth()
const grokOAuth=useGrokOAuth()

// Refs
const oauthFlowRef=ref<OAuthFlowExposed|null>(null)
const applyingCredentials = ref(false)

// State
const addMethod=ref<AddMethod>('oauth')
watch(addMethod,() => {
  claudeOAuth.resetState()
  oauthFlowRef.value?.reset()
})
const geminiOAuthType=ref<'code_assist'|'google_one'|'ai_studio'>('code_assist')

// Computed - check platform
const isOpenAI=computed(() => props.account?.platform==='openai')
const isOpenAILike=computed(() => isOpenAI.value)
const isGemini=computed(() => props.account?.platform==='gemini')
const isAnthropic=computed(() => props.account?.platform==='anthropic')
const isAntigravity=computed(() => props.account?.platform==='antigravity')
const isGrok=computed(() => props.account?.platform==='grok')

/**
 * Grok reauth default tab (password auth is hidden):
 * - refresh_token when RT may still work
 * - SSO cookie otherwise
 */
const grokInitialInputMethod=computed<AuthInputMethod>(() => {
  if(isAnthropic.value&&addMethod.value==='setup-token-manual') return 'setup_token'
  if(!isGrok.value) return 'manual'
  const creds=props.account?.credentials||{}
  const hasRT=
    (typeof creds.refresh_token==='string'&&creds.refresh_token.trim()!=='')||
    (typeof creds.has_refresh_token==='boolean'&&creds.has_refresh_token)||
    props.account?.credentials_status?.has_refresh_token===true
  if(hasRT) return 'refresh_token'
  return 'sso_cookie'
})

// Computed - current OAuth state based on platform
const currentAuthUrl=computed(() => {
  if(isOpenAILike.value) return openaiOAuth.authUrl.value
  if(isGemini.value) return geminiOAuth.authUrl.value
  if(isAntigravity.value) return antigravityOAuth.authUrl.value
  if(isGrok.value) return grokOAuth.authUrl.value
  return claudeOAuth.authUrl.value
})
const currentSessionId=computed(() => {
  if(isOpenAILike.value) return openaiOAuth.sessionId.value
  if(isGemini.value) return geminiOAuth.sessionId.value
  if(isAntigravity.value) return antigravityOAuth.sessionId.value
  if(isGrok.value) return grokOAuth.sessionId.value
  return claudeOAuth.sessionId.value
})
const currentLoading=computed(() => {
  if (applyingCredentials.value) return true
  if(isOpenAILike.value) return openaiOAuth.loading.value
  if(isGemini.value) return geminiOAuth.loading.value
  if(isAntigravity.value) return antigravityOAuth.loading.value
  if(isGrok.value) return grokOAuth.loading.value
  return claudeOAuth.loading.value
})
const currentError=computed(() => {
  if(isOpenAILike.value) return openaiOAuth.error.value
  if(isGemini.value) return geminiOAuth.error.value
  if(isAntigravity.value) return antigravityOAuth.error.value
  if(isGrok.value) return grokOAuth.error.value
  return claudeOAuth.error.value
})

// Computed — footer "complete auth" only for code-exchange flows, not SSO/password/RT.
const isManualInputMethod=computed(() => {
  const method=oauthFlowRef.value?.inputMethod
  if(method==='sso_cookie'||method==='email_password'||method==='refresh_token') {
    return false
  }
  // OpenAI/Gemini/Antigravity/Grok use manual code paste by default (no cookie auth)
  return (
    isOpenAILike.value||
    isGemini.value||
    isAntigravity.value||
    isGrok.value||
    method==='manual'||
    method==='setup_token'
  )
})

const canExchangeCode=computed(() => {
  const authCode=oauthFlowRef.value?.authCode||''
  if(isAnthropic.value&&addMethod.value==='setup-token-manual') {
    return !!buildClaudeSetupTokenCredentials(oauthFlowRef.value?.setupToken||'')&&!claudeOAuth.loading.value
  }
  const sessionId=currentSessionId.value
  const loading=currentLoading.value
  return authCode.trim()&&sessionId&&!loading
})

// Watchers


// Methods
const resetState=() => {
  applyingCredentials.value = false
  addMethod.value='oauth'
  geminiOAuthType.value='code_assist'
  claudeOAuth.resetState()
  openaiOAuth.resetState()
  geminiOAuth.resetState()
  antigravityOAuth.resetState()
  grokOAuth.resetState()
  oauthFlowRef.value?.reset()
}

const handleClose=() => {
  resourceTasks.invalidate()
  resetState()
  emit('close')
}

const handleGenerateUrl=async () => {
  if (currentLoading.value) return
  const capturedAccount=props.account

  const pending=resourceTasks.capture()
  if(!props.show||!services.access.canAuthorize) return
  try {

    if(!capturedAccount) return

    if(isOpenAILike.value) {
      await pending.wait(async () => openaiOAuth.generateAuthUrl(capturedAccount.proxy_id))
    } else if(isGemini.value) {
      const creds=capturedAccount.credentials||{}
      const tierId=typeof creds.tier_id==='string'? creds.tier_id:undefined
      const projectId=geminiOAuthType.value==='code_assist'? oauthFlowRef.value?.projectId:undefined
      await pending.wait(async () => geminiOAuth.generateAuthUrl(capturedAccount.proxy_id,projectId,geminiOAuthType.value,tierId))
    } else if(isAntigravity.value) {
      await pending.wait(async () => antigravityOAuth.generateAuthUrl(capturedAccount.proxy_id))
    } else if(isGrok.value) {
      await pending.wait(async () => grokOAuth.generateAuthUrl(capturedAccount.proxy_id))
    } else {
      await pending.wait(async () => claudeOAuth.generateAuthUrl(addMethod.value,capturedAccount.proxy_id))
    }

  } catch(failure: unknown) {
    if(!(failure instanceof StaleResourceTask)) throw failure
  }
}

const handleAnthropicSetupToken=async (rawToken: string) => {
  const capturedAccount=props.account

  const pending=resourceTasks.capture()
  if(!props.show||!services.access.canAuthorize) return
  try {

    if(!capturedAccount) return
    const credentials=buildClaudeSetupTokenCredentials(rawToken)
    if(!credentials) {
      claudeOAuth.error.value=t('admin.accounts.oauth.setupTokenInvalid')
      return
    }

    claudeOAuth.loading.value=true
    claudeOAuth.error.value=''
    try {
      const updatedAccount=await pending.wait(async () => applyCredentials(capturedAccount.id,{
        type: 'setup-token',
        credentials
      }))
      appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
      emit('reauthorized',updatedAccount)
      handleClose()
    } catch(error: unknown) {
      if(!pending.current()||error instanceof StaleResourceTask) throw new StaleResourceTask()

      claudeOAuth.error.value=extractApiErrorMessage(error,t('admin.accounts.oauth.authFailed'))
      appStore.showError(claudeOAuth.error.value)
    } finally {
      if(pending.current()) {

        claudeOAuth.loading.value=false

      }
    }

  } catch(failure: unknown) {
    if(!(failure instanceof StaleResourceTask)) throw failure
  }
}

const handleExchangeCode=async () => {
  if (currentLoading.value) return
  const capturedAccount=props.account

  const pending=resourceTasks.capture()
  if(!props.show||!services.access.canAuthorize) return
  try {

    if(!capturedAccount) return

    if(isAnthropic.value&&addMethod.value==='setup-token-manual') {
      await pending.wait(async () => handleAnthropicSetupToken(oauthFlowRef.value?.setupToken||''))
      return
    }

    const authCode=oauthFlowRef.value?.authCode||''
    if(!authCode.trim()) return

    if(isOpenAILike.value) {
      // OpenAI OAuth flow
      const oauthClient=openaiOAuth
      const sessionId=oauthClient.sessionId.value
      if(!sessionId) return
      const stateToUse=(oauthFlowRef.value?.oauthState||oauthClient.oauthState.value||'').trim()
      if(!stateToUse) {
        oauthClient.error.value=t('admin.accounts.oauth.authFailed')
        appStore.showError(oauthClient.error.value)
        return
      }

      const tokenInfo=await pending.wait(async () => oauthClient.exchangeAuthCode(
        authCode.trim(),
        sessionId,
        stateToUse,
        capturedAccount.proxy_id
      ))
      if(!tokenInfo) return

      // Build credentials and extra info
      const credentials=oauthClient.buildCredentials(tokenInfo)
      const extra=oauthClient.buildExtraInfo(tokenInfo)

      try {
        const updatedAccount=await pending.wait(async () => applyCredentials(capturedAccount.id,{
          type: 'oauth',
          credentials,
          extra
        }))

        appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
        emit('reauthorized',updatedAccount)
        handleClose()
      } catch(error: unknown) {
        const errorDetails = readApiError(error)
        if(!pending.current()||error instanceof StaleResourceTask) throw new StaleResourceTask()

        oauthClient.error.value=errorDetails.response?.data?.detail||t('admin.accounts.oauth.authFailed')
        appStore.showError(oauthClient.error.value)
      }
    } else if(isGemini.value) {
      const sessionId=geminiOAuth.sessionId.value
      if(!sessionId) return

      const stateFromInput=oauthFlowRef.value?.oauthState||''
      const stateToUse=stateFromInput||geminiOAuth.state.value
      if(!stateToUse) return

      const tierId=capturedAccount.credentials?.tier_id
      const tokenInfo=await pending.wait(async () => geminiOAuth.exchangeAuthCode({
        code: authCode.trim(),
        sessionId,
        state: stateToUse,
        proxyId: capturedAccount.proxy_id,
        oauthType: geminiOAuthType.value,
        tierId: typeof tierId==='string'? tierId:undefined
      }))
      if(!tokenInfo) return

      const credentials=geminiOAuth.buildCredentials(tokenInfo)

      try {
        const updatedAccount=await pending.wait(async () => applyCredentials(capturedAccount.id,{
          type: 'oauth',
          credentials
        }))
        appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
        emit('reauthorized',updatedAccount)
        handleClose()
      } catch(error: unknown) {
        const errorDetails = readApiError(error)
        if(!pending.current()||error instanceof StaleResourceTask) throw new StaleResourceTask()

        geminiOAuth.error.value=errorDetails.response?.data?.detail||t('admin.accounts.oauth.authFailed')
        appStore.showError(geminiOAuth.error.value)
      }
    } else if(isAntigravity.value) {
      // Antigravity OAuth flow
      const sessionId=antigravityOAuth.sessionId.value
      if(!sessionId) return

      const stateFromInput=oauthFlowRef.value?.oauthState||''
      const stateToUse=stateFromInput||antigravityOAuth.state.value
      if(!stateToUse) return

      const tokenInfo=await pending.wait(async () => antigravityOAuth.exchangeAuthCode({
        code: authCode.trim(),
        sessionId,
        state: stateToUse,
        proxyId: capturedAccount.proxy_id
      }))
      if(!tokenInfo) return

      const credentials=antigravityOAuth.buildCredentials(tokenInfo)

      try {
        const updatedAccount=await pending.wait(async () => applyCredentials(capturedAccount.id,{
          type: 'oauth',
          credentials
        }))
        appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
        emit('reauthorized',updatedAccount)
        handleClose()
      } catch(error: unknown) {
        const errorDetails = readApiError(error)
        if(!pending.current()||error instanceof StaleResourceTask) throw new StaleResourceTask()

        antigravityOAuth.error.value=errorDetails.response?.data?.detail||t('admin.accounts.oauth.authFailed')
        appStore.showError(antigravityOAuth.error.value)
      }
    } else if(isGrok.value) {
      const sessionId=grokOAuth.sessionId.value
      if(!sessionId) return

      const stateFromInput=oauthFlowRef.value?.oauthState||''
      const stateToUse=stateFromInput||grokOAuth.state.value
      if(!stateToUse) return

      const tokenInfo=await pending.wait(async () => grokOAuth.exchangeAuthCode({
        code: authCode.trim(),
        sessionId,
        state: stateToUse,
        proxyId: capturedAccount.proxy_id
      }))
      if(!tokenInfo) return

      const credentials=grokOAuth.buildCredentials(tokenInfo)
      const extra=grokOAuth.buildExtraInfo(tokenInfo)

      try {
        const updatedAccount=await pending.wait(async () => applyCredentials(capturedAccount.id,{
          type: 'oauth',
          credentials,
          extra
        }))

        appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
        emit('reauthorized',updatedAccount)
        handleClose()
      } catch(error: unknown) {
        const errorDetails = readApiError(error)
        if(!pending.current()||error instanceof StaleResourceTask) throw new StaleResourceTask()

        grokOAuth.error.value=errorDetails.response?.data?.detail||t('admin.accounts.oauth.authFailed')
        appStore.showError(grokOAuth.error.value)
      }
    } else {
      // Claude OAuth flow
      const sessionId=claudeOAuth.sessionId.value
      if(!sessionId) return

      claudeOAuth.loading.value=true
      claudeOAuth.error.value=''

      try {
        const proxyConfig=capturedAccount.proxy_id? { proxy_id: capturedAccount.proxy_id }:{}
        const endpoint=
          addMethod.value==='oauth'
            ? '/admin/accounts/exchange-code'
            :'/admin/accounts/exchange-setup-token-code'

        const tokenInfo=await pending.wait(async () => accountsAPI.exchangeCode(endpoint,{
          session_id: sessionId,
          code: authCode.trim(),
          ...proxyConfig
        }))

        const extra=claudeOAuth.buildExtraInfo(tokenInfo)

        const updatedAccount=await pending.wait(async () => applyCredentials(capturedAccount.id,{
          type: addMethod.value as 'oauth'|'setup-token',
          credentials: tokenInfo,
          extra
        }))

        appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
        emit('reauthorized',updatedAccount)
        handleClose()
      } catch(error: unknown) {
        const errorDetails = readApiError(error)
        if(!pending.current()||error instanceof StaleResourceTask) throw new StaleResourceTask()

        claudeOAuth.error.value=errorDetails.response?.data?.detail||t('admin.accounts.oauth.authFailed')
        appStore.showError(claudeOAuth.error.value)
      } finally {
        if(pending.current()) {

          claudeOAuth.loading.value=false

        }
      }
    }

  } catch(failure: unknown) {
    if(!(failure instanceof StaleResourceTask)) throw failure
  }
}

const handleCookieAuth=async (sessionKey: string) => {
  if (currentLoading.value) return
  const capturedAccount=props.account

  const pending=resourceTasks.capture()
  if(!props.show||!services.access.canAuthorize) return
  try {

    if(!capturedAccount||isOpenAILike.value) return

    claudeOAuth.loading.value=true
    claudeOAuth.error.value=''

    try {
      const proxyConfig=capturedAccount.proxy_id? { proxy_id: capturedAccount.proxy_id }:{}
      const endpoint=
        addMethod.value==='oauth'
          ? '/admin/accounts/cookie-auth'
          :'/admin/accounts/setup-token-cookie-auth'

      const tokenInfo=await pending.wait(async () => accountsAPI.exchangeCode(endpoint,{
        session_id: '',
        code: sessionKey.trim(),
        ...proxyConfig
      }))

      const extra=claudeOAuth.buildExtraInfo(tokenInfo)

      const updatedAccount=await pending.wait(async () => applyCredentials(capturedAccount.id,{
        type: addMethod.value as 'oauth'|'setup-token',
        credentials: tokenInfo,
        extra
      }))

      appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
      emit('reauthorized',updatedAccount)
      handleClose()
    } catch(error: unknown) {
      const errorDetails = readApiError(error)
      if(!pending.current()||error instanceof StaleResourceTask) throw new StaleResourceTask()

      claudeOAuth.error.value=
        errorDetails.response?.data?.detail||t('admin.accounts.oauth.cookieAuthFailed')
    } finally {
      if(pending.current()) {

        claudeOAuth.loading.value=false

      }
    }

  } catch(failure: unknown) {
    if(!(failure instanceof StaleResourceTask)) throw failure
  }
}

/** Apply Grok Build OAuth tokens onto the existing account (never store password/SSO). */
const applyGrokReauthTokenInfo=async (tokenInfo: GrokTokenInfo): Promise<void> => {
  const capturedAccount=props.account

  const pending=resourceTasks.capture()
  if(!props.show||!services.access.canAuthorize) return
  try {

    if(!capturedAccount) return
    const credentials=grokOAuth.buildCredentials(tokenInfo)
    const extra=grokOAuth.buildExtraInfo(tokenInfo)
    const updatedAccount=await pending.wait(async () => applyCredentials(capturedAccount.id,{
      type: 'oauth',
      credentials,
      extra
    }))
    appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
    emit('reauthorized',updatedAccount)
    handleClose()

  } catch(failure: unknown) {
    if(!(failure instanceof StaleResourceTask)) throw failure
  }
}

/** Re-auth the existing account with one refresh token. */
const handleValidateRefreshToken=async (refreshTokenInput: string) => {
  if (currentLoading.value) return
  const capturedAccount=props.account

  const pending=resourceTasks.capture()
  if(!props.show||!services.access.canAuthorize) return
  try {

    if(!capturedAccount) return
    if(isGrok.value) {
      await pending.wait(async () => handleGrokValidateRefreshToken(refreshTokenInput))
      return
    }

    const refreshToken=refreshTokenInput
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)[0]
    if(!refreshToken) return

    if(isOpenAILike.value) {
      openaiOAuth.loading.value=true
      openaiOAuth.error.value=''
      try {
        const tokenInfo=await pending.wait(async () => openaiOAuth.validateRefreshToken(refreshToken,capturedAccount.proxy_id))
        if(!tokenInfo) return

        const updatedAccount=await pending.wait(async () => applyCredentials(capturedAccount.id,{
          type: 'oauth',
          credentials: openaiOAuth.buildCredentials(tokenInfo),
          extra: openaiOAuth.buildExtraInfo(tokenInfo)
        }))
        appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
        emit('reauthorized',updatedAccount)
        handleClose()
      } catch(error: unknown) {
        const errorDetails = readApiError(error)
        if(!pending.current()||error instanceof StaleResourceTask) throw new StaleResourceTask()

        openaiOAuth.error.value=
          errorDetails.response?.data?.detail||
          errorDetails.response?.data?.message||
          errorDetails.message||
          t('admin.accounts.oauth.authFailed')
        appStore.showError(openaiOAuth.error.value)
      } finally {
        if(pending.current()) {

          openaiOAuth.loading.value=false

        }
      }
      return
    }

    if(!isAntigravity.value) return
    antigravityOAuth.loading.value=true
    antigravityOAuth.error.value=''
    try {
      const tokenInfo=await pending.wait(async () => antigravityOAuth.validateRefreshToken(refreshToken,capturedAccount.proxy_id))
      if(!tokenInfo) return

      const updatedAccount=await pending.wait(async () => applyCredentials(capturedAccount.id,{
        type: 'oauth',
        credentials: antigravityOAuth.buildCredentials(tokenInfo,refreshToken)
      }))
      appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
      emit('reauthorized',updatedAccount)
      handleClose()
    } catch(error: unknown) {
      const errorDetails = readApiError(error)
      if(!pending.current()||error instanceof StaleResourceTask) throw new StaleResourceTask()

      antigravityOAuth.error.value=
        errorDetails.response?.data?.detail||
        errorDetails.response?.data?.message||
        errorDetails.message||
        t('admin.accounts.oauth.authFailed')
      appStore.showError(antigravityOAuth.error.value)
    } finally {
      if(pending.current()) {

        antigravityOAuth.loading.value=false

      }
    }

  } catch(failure: unknown) {
    if(!(failure instanceof StaleResourceTask)) throw failure
  }
}

/** Re-auth with a single SSO cookie → Build OAuth (not batch create). */
const handleGrokImportSSO=async (ssoInput: string) => {
  if (currentLoading.value) return
  const capturedAccount=props.account

  const pending=resourceTasks.capture()
  if(!props.show||!services.access.canAuthorize) return
  try {

    if(!capturedAccount||!isGrok.value) return
    const ssoToken=ssoInput
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)[0]
    if(!ssoToken) return

    grokOAuth.loading.value=true
    grokOAuth.error.value=''
    try {
      const tokenInfo=await pending.wait(async () => grokOAuth.validateSSOToken(ssoToken,capturedAccount.proxy_id))
      if(!tokenInfo) return
      await pending.wait(async () => applyGrokReauthTokenInfo(tokenInfo))
    } catch(error: unknown) {
      const errorDetails = readApiError(error)
      if(!pending.current()||error instanceof StaleResourceTask) throw new StaleResourceTask()

      grokOAuth.error.value=
        errorDetails.response?.data?.detail||
        errorDetails.message||
        t('admin.accounts.oauth.grok.failedToValidateSSO','Failed to validate Grok SSO')
      appStore.showError(grokOAuth.error.value)
    } finally {
      if(pending.current()) {

        grokOAuth.loading.value=false

      }
    }

  } catch(failure: unknown) {
    if(!(failure instanceof StaleResourceTask)) throw failure
  }
}

/** Re-auth with a single refresh token. */
const handleGrokValidateRefreshToken=async (refreshTokenInput: string) => {
  const capturedAccount=props.account

  const pending=resourceTasks.capture()
  if(!props.show||!services.access.canAuthorize) return
  try {

    if(!capturedAccount||!isGrok.value) return
    const refreshToken=refreshTokenInput
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)[0]
    if(!refreshToken) {
      grokOAuth.error.value=t('admin.accounts.oauth.grok.pleaseEnterRefreshToken')
      return
    }

    grokOAuth.loading.value=true
    grokOAuth.error.value=''
    try {
      const tokenInfo=await pending.wait(async () => grokOAuth.validateRefreshToken(refreshToken,capturedAccount.proxy_id))
      if(!tokenInfo) return
      await pending.wait(async () => applyGrokReauthTokenInfo(tokenInfo))
    } catch(error: unknown) {
      const errorDetails = readApiError(error)
      if(!pending.current()||error instanceof StaleResourceTask) throw new StaleResourceTask()

      grokOAuth.error.value=
        errorDetails.response?.data?.detail||
        errorDetails.message||
        t('admin.accounts.oauth.grok.failedToValidateRT')
      appStore.showError(grokOAuth.error.value)
    } finally {
      if(pending.current()) {

        grokOAuth.loading.value=false

      }
    }

  } catch(failure: unknown) {
    if(!(failure instanceof StaleResourceTask)) throw failure
  }
}

async function applyCredentials(id: number, payload: Parameters<typeof accountsAPI.applyOAuthCredentials>[1]): Promise<Account> {
  if (!props.show || props.account?.id !== id || !services.access.canAuthorize || applyingCredentials.value) throw new StaleResourceTask()
  const pending = resourceTasks.capture()
  const request = { ...payload }
  if (!props.allowExtraEdit) delete request.extra
  applyingCredentials.value = true
  try {
    return await pending.wait(() => accountsAPI.applyOAuthCredentials(id, request))
  } finally {
    if (pending.current()) applyingCredentials.value = false
  }
}

watch(() => [services.scopeKey(),props.show,props.account?.id,props.account?.platform,props.account?.proxy_id] as const,
  (next,previous) => {
    resourceTasks.invalidate()
    resetState()
    if(previous&&next[0]!==previous[0]) {
      if(props.show) emit('close')
      return
    }
    const account=props.account
    if(!props.show||!account) return
    if(account.platform==='anthropic'&&(account.type==='oauth'||account.type==='setup-token')) addMethod.value=account.type
    const oauthType=account.credentials?.oauth_type
    if(account.platform==='gemini'&&(oauthType==='google_one'||oauthType==='ai_studio'||oauthType==='code_assist')) geminiOAuthType.value=oauthType
  },{ immediate: true,flush: 'sync' })
onScopeDispose(resetState)
</script>
