# Identity integration

Identity owns authentication, pending OAuth completion, profile/security, administrator user-directory actions, permission configuration and compliance state. Resource-credential OAuth belongs to resources; API Key management belongs to keys; payment callbacks belong to payments.

## Public owners and app assembly

- `@/features/identity` explicitly publishes identity APIs, state, ports and supported panels; `@/features/identity/routes` contributes the authentication, profile and user-directory pages. The removed root API/store/type barrels are not import targets.
- Auth DTOs are in `api/types.ts`, user/profile DTOs in `api/profileTypes.ts` and security DTOs in `api/securityTypes.ts`. `api/profile.ts` owns the current-profile read/update operations; `getProfile({signal})` forwards cancellation. `api/totp.ts` owns setup/enable/disable; `api/stepUp.ts` owns step-up verification.
- `app/compositions/featureServices.ts` installs `identityNotificationsKey`, `identitySettingsKey`, `identityAuthShellKey`, `identityClipboardKey` and `identityPortalKey`. Settings are reactive read-only projections with a real loader; missing WeChat capabilities remain unknown. AuthLayout remains app-owned and provides its default/footer slots.
- `app/compositions/identityUserServices.ts` installs user-directory projections and composition panels. Cross-domain balances, Key queries, quota configuration and group replacement are assembled there or in the user-directory composition, using the payment/keys/usage/access-policy owners. They are not identity CRUD.
- `app/compositions/settings/installServices.ts` supplies the identity settings contract. Settings saves retain the existing revision/sent-field contract.
- `@/features/identity/panelLoaders` provides `loadProfilePasswordForm`, `loadProfileTotpCard` and `loadProfilePasskeyCard` for app-bound security cards. These load the real Vue components. The global Step-up dialog is still used by the application shell.

## Session and command contracts

The auth store's `identityScopeKey` combines logical-session epoch and user id/role/policy_version, without exposing token material. Same-session access-token rotation does not constitute a new login; a new same-user session does. App lifecycle code invalidates private domain state, cancels global step-up and observes completed peer-tab session changes. Identity does not import subscription or app stores to perform those cross-domain effects.

Login/registration share agreement-revision state. Pending OAuth create/bind and email completion have their API/DTO owner in `api/pendingOAuth.ts` and preserve the provider-specific OIDC, DingTalk and WeChat flows. Late credential/step-up results must not be accepted into a replaced session. Transient refresh failure is distinct from a definitive expired session; disposal stops owned timers.

## Retired paths

Paths below are relative to `frontend/src`. The legacy locations are historical mapping labels, not forwarding modules. Profile, user-directory and security actions use the owners above.

| Removed path | Canonical implementation or disposition |
| --- | --- |
| api/__tests__/auth-captcha-oauth-start.spec.ts | features/identity/api/__tests__/auth-captcha-oauth-start.spec.ts |
| api/__tests__/auth-oauth-adoption.spec.ts | features/identity/api/__tests__/auth-oauth-adoption.spec.ts |
| api/__tests__/passkey.spec.ts | features/identity/api/__tests__/passkey.spec.ts |
| api/auth.ts | features/identity/api/auth.ts |
| api/passkey.ts | features/identity/api/passkey.ts |
| components/auth/DingTalkOAuthSection.vue | features/identity/components/DingTalkOAuthSection.vue |
| components/auth/EmailOAuthButtons.vue | features/identity/components/EmailOAuthButtons.vue |
| components/auth/GitHubMark.vue | features/identity/components/GitHubMark.vue |
| components/auth/GoogleMark.vue | features/identity/components/GoogleMark.vue |
| components/auth/LinuxDoOAuthSection.vue | features/identity/components/LinuxDoOAuthSection.vue |
| components/auth/LoginAgreementPrompt.vue | features/identity/components/LoginAgreementPrompt.vue |
| components/auth/OidcOAuthSection.vue | features/identity/components/OidcOAuthSection.vue |
| components/auth/PendingOAuthCreateAccountForm.vue | features/identity/components/PendingOAuthCreateAccountForm.vue |
| components/auth/TotpLoginModal.vue | features/identity/components/TotpLoginModal.vue |
| components/auth/TotpStepUpDialog.vue | features/identity/components/TotpStepUpDialog.vue |
| components/auth/WechatOAuthSection.vue | features/identity/components/WechatOAuthSection.vue |
| components/auth/__tests__/EmailOAuthButtons.spec.ts | features/identity/components/__tests__/EmailOAuthButtons.spec.ts |
| components/auth/__tests__/OAuthLoginSections.spec.ts | features/identity/components/__tests__/OAuthLoginSections.spec.ts |
| components/auth/__tests__/PendingOAuthCreateAccountForm.spec.ts | features/identity/components/__tests__/PendingOAuthCreateAccountForm.spec.ts |
| components/auth/__tests__/TotpLoginModal.spec.ts | features/identity/components/__tests__/TotpLoginModal.spec.ts |
| components/auth/__tests__/WechatOAuthSection.spec.ts | features/identity/components/__tests__/WechatOAuthSection.spec.ts |
| composables/__tests__/useStepUp.spec.ts | features/identity/composables/__tests__/useStepUp.spec.ts |
| composables/useStepUp.ts | features/identity/composables/useStepUp.ts |
| stores/__tests__/auth.permissions.spec.ts | features/identity/model/__tests__/auth.permissions.spec.ts |
| stores/__tests__/auth.spec.ts | features/identity/model/__tests__/auth.spec.ts |
| stores/auth.ts | features/identity/model/auth.ts |
| views/auth/DingTalkCallbackView.vue | features/identity/pages/DingTalkCallbackView.vue |
| views/auth/DingTalkEmailCompletionView.vue | features/identity/pages/DingTalkEmailCompletionView.vue |
| views/auth/EmailVerifyView.vue | features/identity/pages/EmailVerifyView.vue |
| views/auth/ForgotPasswordView.vue | features/identity/pages/ForgotPasswordView.vue |
| views/auth/LinuxDoCallbackView.vue | features/identity/pages/LinuxDoCallbackView.vue |
| views/auth/LoginView.vue | features/identity/pages/LoginView.vue |
| views/auth/OAuthCallbackView.vue | features/identity/pages/OAuthCallbackView.vue |
| views/auth/OidcCallbackView.vue | features/identity/pages/OidcCallbackView.vue |
| views/auth/RegisterView.vue | features/identity/pages/RegisterView.vue |
| views/auth/ResetPasswordView.vue | features/identity/pages/ResetPasswordView.vue |
| views/auth/WechatCallbackView.vue | features/identity/pages/WechatCallbackView.vue |
| views/auth/__tests__/EmailVerifyView.spec.ts | features/identity/pages/__tests__/EmailVerifyView.spec.ts |
| views/auth/__tests__/LinuxDoCallbackView.spec.ts | features/identity/pages/__tests__/LinuxDoCallbackView.spec.ts |
| views/auth/__tests__/LoginView.spec.ts | features/identity/pages/__tests__/LoginView.spec.ts |
| views/auth/__tests__/OAuthCallbackView.spec.ts | features/identity/pages/__tests__/OAuthCallbackView.spec.ts |
| views/auth/__tests__/OidcCallbackView.spec.ts | features/identity/pages/__tests__/OidcCallbackView.spec.ts |
| views/auth/__tests__/RegisterView.spec.ts | features/identity/pages/__tests__/RegisterView.spec.ts |
| views/auth/__tests__/TencentCaptchaActionGate.spec.ts | features/identity/pages/__tests__/TencentCaptchaActionGate.spec.ts |
| views/auth/__tests__/TencentCaptchaForgotPassword.spec.ts | features/identity/pages/__tests__/TencentCaptchaForgotPassword.spec.ts |
| views/auth/__tests__/WechatCallbackView.spec.ts | features/identity/pages/__tests__/WechatCallbackView.spec.ts |
| views/auth/index.ts | Deleted eager page barrel; lazy routes in features/identity/routes.ts |
## Standards and acceptance

[FRONTEND_ARCHITECTURE](../../../../docs/architecture-rebuild/FRONTEND_ARCHITECTURE.md), [CODING_STANDARDS](../../../../docs/architecture-rebuild/CODING_STANDARDS.md) and [CT-11](../../../../docs/architecture-rebuild/CONTRACTS.md#ct-11-控制面与前端) remain the rule sources. Use the existing Vitest, TypeScript and ESLint configuration for code changes; this document does not establish a separate validation platform. This documentation pass runs no business tests or builds. Docker and page-by-page acceptance are `not_run`; full frontend acceptance remains incomplete. Source migration alone does not establish runtime acceptance.
