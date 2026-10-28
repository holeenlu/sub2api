import type { PendingOAuthAccountResponse } from '../api/pendingOAuth'
import { createPendingOAuthAccount, bindPendingOAuthAccount } from '../api/pendingOAuth'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../model/auth'
import { useIdentityNotifications } from '../ports'
import { exchangePendingOAuthCompletion, getOAuthCompletionKind, isOAuthLoginCompletion, login2FA, persistOAuthTokenContext, type PendingOAuthExchangeResponse } from '../api/auth'
import type { PendingOAuthCreateAccountPayload } from '../model/registration'
import { sanitizeRedirectPath, getRequestErrorMessage, isCreateAccountRecoveryError, serializeAdoptionDecision } from '../model/oauthCompletion'
import { useOAuthAdoption } from './useOAuthAdoption'


export function usePendingOAuthAccount(provider: 'linuxdo' | 'oidc' | 'dingtalk') {
  const router = useRouter()
  const { t } = useI18n()
  const authStore = useAuthStore()
  const notifications = useIdentityNotifications()
  const { adoptionRequired, suggestedDisplayName, suggestedAvatarUrl, adoptDisplayName, adoptAvatar, currentAdoptionDecision, applyAdoptionSuggestionState } = useOAuthAdoption()

  const isProcessing = ref(true)
  const errorMessage = ref('')
  const isSubmitting = ref(false)
  const redirectTo = ref('/dashboard')
  const needsAdoptionConfirmation = ref(false)
  const pendingAccountAction = ref<'none' | 'choose_account_action' | 'create_account' | 'bind_login'>('none')
  const pendingAccountEmail = ref('')
  const bindLoginEmail = ref('')
  const bindLoginPassword = ref('')
  const accountActionError = ref('')
  const canReturnToCreateAccount = ref(false)
  const bindSuccessMessage = t('profile.authBindings.bindSuccess')
  const needsTotpChallenge = ref(false)
  const totpTempToken = ref('')
  const totpCode = ref('')
  const totpError = ref('')
  const totpUserEmailMasked = ref('')

  const needsCreateAccount = computed(() => pendingAccountAction.value === 'create_account')
  const needsChooser = computed(() => pendingAccountAction.value === 'choose_account_action')
  const needsBindLogin = computed(() => pendingAccountAction.value === 'bind_login')

  watch(accountActionError, value => {
    if (value) {
      notifications.showError(value)
    }
  })

  watch(totpError, value => {
    if (value) {
      notifications.showError(value)
    }
  })

  watch(errorMessage, value => {
    if (value) {
      notifications.showError(value)
    }
  })


  function persistPendingAuthSession(redirect?: string) {
    authStore.setPendingAuthSession({
      token: '',
      token_field: 'pending_oauth_token',
      provider,
      redirect: sanitizeRedirectPath(redirect || redirectTo.value)
    })
  }

  function clearPendingAuthSession() {
    authStore.clearPendingAuthSession()
  }

  function normalizedPendingState(value: string | null | undefined): string {
    return value?.trim().toLowerCase() || ''
  }

  function extractPendingAccountEmail(completion: PendingOAuthAccountResponse): string {
    return (
      completion.pending_email ||
      completion.existing_account_email ||
      (provider === 'oidc' ? completion.compat_email || completion.resolved_email || completion.email : completion.email || completion.resolved_email) ||
      completion.suggested_email ||
      ''
    ).trim()
  }

  function resolvePendingAccountAction(
    completion: PendingOAuthAccountResponse
  ): 'none' | 'choose_account_action' | 'create_account' | 'bind_login' {
    const raw = normalizedPendingState(completion.step || completion.error || completion.intent)
    if (
      raw === 'choice' ||
      raw === 'choose_account_action_required' ||
      raw === 'choose_account_action' ||
      raw === 'choose_account' ||
      raw === 'choose'
    ) {
      return 'choose_account_action'
    }
    if (raw === 'email_required' || raw === 'create_account_required' || raw === 'create_account') {
      return 'create_account'
    }
    if (
      raw === 'bind_login_required' ||
      raw === 'bind_login' ||
      (provider !== 'oidc' && raw === 'existing_account') ||
      raw === 'existing_account_required' ||
      raw === 'existing_account_binding_required' ||
      raw === 'adopt_existing_user_by_email'
    ) {
      return 'bind_login'
    }
    return 'none'
  }

  function applyPendingAccountAction(completion: PendingOAuthAccountResponse) {
    const action = resolvePendingAccountAction(completion)
    pendingAccountAction.value = action
    accountActionError.value = ''
    needsTotpChallenge.value = false
    totpTempToken.value = ''
    totpCode.value = ''
    totpError.value = ''
    totpUserEmailMasked.value = ''

    const email = extractPendingAccountEmail(completion)
    if (action === 'choose_account_action') {
      pendingAccountEmail.value = email
      bindLoginEmail.value = email
      bindLoginPassword.value = ''
      canReturnToCreateAccount.value = false
      return
    }

    if (action === 'create_account') {
      pendingAccountEmail.value = email
      canReturnToCreateAccount.value = true
      return
    }

    if (action === 'bind_login') {
      bindLoginEmail.value = email
      bindLoginPassword.value = ''
      canReturnToCreateAccount.value = false
      return
    }

    canReturnToCreateAccount.value = false
  }

  function applyTotpChallenge(completion: PendingOAuthAccountResponse): boolean {
    if (completion.requires_2fa !== true || !completion.temp_token) {
      return false
    }

    pendingAccountAction.value = 'none'
    needsAdoptionConfirmation.value = false
    needsTotpChallenge.value = true
    totpTempToken.value = completion.temp_token
    totpCode.value = ''
    totpError.value = ''
    totpUserEmailMasked.value = completion.user_email_masked || ''
    isProcessing.value = false
    return true
  }

  function switchToBindLoginMode(nextEmail?: string) {
    pendingAccountAction.value = 'bind_login'
    bindLoginEmail.value = bindLoginEmail.value.trim() || nextEmail?.trim() || pendingAccountEmail.value.trim()
    bindLoginPassword.value = ''
    accountActionError.value = ''
    canReturnToCreateAccount.value = true
  }

  function switchToCreateAccountMode() {
    pendingAccountAction.value = 'create_account'
    pendingAccountEmail.value = pendingAccountEmail.value.trim() || bindLoginEmail.value.trim()
    accountActionError.value = ''
  }

  async function finalizeCompletion(completion: PendingOAuthExchangeResponse, redirect: string) {
    if (getOAuthCompletionKind(completion) === 'bind') {
      const bindRedirect = sanitizeRedirectPath(completion.redirect || '/profile')
      clearPendingAuthSession()
      notifications.showSuccess(bindSuccessMessage)
      await router.replace(bindRedirect)
      return
    }

    if (!isOAuthLoginCompletion(completion)) {
      throw new Error(t('auth.' + provider + '.callbackMissingToken'))
    }

    persistOAuthTokenContext(completion)
    await authStore.setToken(completion.access_token)
    notifications.showSuccess(t('auth.loginSuccess'))
    await router.replace(redirect)
  }

  async function finalizePendingAccountResponse(completion: PendingOAuthAccountResponse) {
    applyAdoptionSuggestionState(completion)
    const redirect = sanitizeRedirectPath(completion.redirect || redirectTo.value)

    if (provider === 'dingtalk' && (completion.step === 'email_completion' || completion.requires_email_completion === true)) {
      await router.replace('/auth/dingtalk/email-completion?redirect=' + encodeURIComponent(redirect))
      return
    }

    if (applyTotpChallenge(completion)) {
      persistPendingAuthSession(redirect)
      return
    }

    applyPendingAccountAction(completion)
    if (pendingAccountAction.value !== 'none') {
      needsAdoptionConfirmation.value = false
      isProcessing.value = false
      persistPendingAuthSession(redirect)
      return
    }

    if (completion.auth_result === 'pending_session') {
      needsAdoptionConfirmation.value = false
      isProcessing.value = false
      persistPendingAuthSession(redirect)
      return
    }

    await finalizeCompletion(completion, redirect)
  }

  async function handleContinueLogin() {
    isSubmitting.value = true
    try {
      const completion = await exchangePendingOAuthCompletion(currentAdoptionDecision()) as PendingOAuthAccountResponse
      await finalizePendingAccountResponse(completion)
    } catch (e: unknown) {
      errorMessage.value = getRequestErrorMessage(e, t('auth.loginFailed'))
      needsAdoptionConfirmation.value = false
    } finally {
      isSubmitting.value = false
    }
  }

  async function handleCreateAccount(payload: PendingOAuthCreateAccountPayload) {
    accountActionError.value = ''
    if (!payload.email || !payload.password) return

    isSubmitting.value = true
    try {
      const { data } = await createPendingOAuthAccount({
        email: payload.email,
        password: payload.password,
        verify_code: payload.verifyCode || undefined,
        ...(payload.turnstileToken ? { turnstile_token: payload.turnstileToken } : {}),
        ...(payload.tencentCaptchaTicket
          ? {
              tencent_captcha_ticket: payload.tencentCaptchaTicket,
              tencent_captcha_randstr: payload.tencentCaptchaRandstr
          }
          : {}),
        ...serializeAdoptionDecision(currentAdoptionDecision())
      })
      await finalizePendingAccountResponse(data)
    } catch (e: unknown) {
      if (isCreateAccountRecoveryError(e)) {
        switchToBindLoginMode(payload.email.trim())
        return
      }
      accountActionError.value = getRequestErrorMessage(e, t('auth.loginFailed'))
    } finally {
      isSubmitting.value = false
    }
  }

  async function handleBindLogin() {
    accountActionError.value = ''
    const email = bindLoginEmail.value.trim()
    const password = bindLoginPassword.value
    if (!email || !password) return

    isSubmitting.value = true
    try {
      const { data } = await bindPendingOAuthAccount({
        email,
        password,
        ...serializeAdoptionDecision(currentAdoptionDecision())
      })
      await finalizePendingAccountResponse(data)
    } catch (e: unknown) {
      accountActionError.value = getRequestErrorMessage(e, t('auth.loginFailed'))
    } finally {
      isSubmitting.value = false
    }
  }

  async function handleSubmitTotpChallenge() {
    totpError.value = ''
    const code = totpCode.value.trim()
    if (!totpTempToken.value || code.length !== 6) return

    isSubmitting.value = true
    try {
      const completion = await login2FA({
        temp_token: totpTempToken.value,
        totp_code: code
      })
      await authStore.setToken(completion.access_token)
      notifications.showSuccess(t('auth.loginSuccess'))
      await router.replace(redirectTo.value)
    } catch (e: unknown) {
      totpError.value = getRequestErrorMessage(e, t('auth.loginFailed'))
    } finally {
      isSubmitting.value = false
    }
  }

  return {
    adoptionRequired,
    suggestedDisplayName,
    suggestedAvatarUrl,
    adoptDisplayName,
    adoptAvatar,
    currentAdoptionDecision,
    applyAdoptionSuggestionState,
    isProcessing,
    errorMessage,
    isSubmitting,
    redirectTo,
    needsAdoptionConfirmation,
    pendingAccountAction,
    pendingAccountEmail,
    bindLoginEmail,
    bindLoginPassword,
    accountActionError,
    canReturnToCreateAccount,
    bindSuccessMessage,
    needsTotpChallenge,
    totpTempToken,
    totpCode,
    totpError,
    totpUserEmailMasked,
    needsCreateAccount,
    needsChooser,
    needsBindLogin,
    persistPendingAuthSession,
    clearPendingAuthSession,
    normalizedPendingState,
    extractPendingAccountEmail,
    resolvePendingAccountAction,
    applyPendingAccountAction,
    applyTotpChallenge,
    switchToBindLoginMode,
    switchToCreateAccountMode,
    finalizeCompletion,
    finalizePendingAccountResponse,
    handleContinueLogin,
    handleCreateAccount,
    handleBindLogin,
    handleSubmitTotpChallenge
  }
}
