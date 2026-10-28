export { useAuthStore } from './model/auth'
export type { PendingAuthSessionSummary, PendingAuthTokenField } from './model/pendingSession'
export { authAPI, login, login2FA, register, getCurrentUser, logout, getAuthToken, getRefreshToken,
  getTokenExpiresAt, setAuthToken, setRefreshToken, setTokenExpiresAt, isAuthenticated,
  isTotp2FARequired, refreshToken, revokeAllSessions, sendVerifyCode,
  sendPendingOAuthVerifyCode, forgotPassword, resetPassword, buildOAuthLoginStartURL,
  startOAuthLogin, isWeChatWebOAuthEnabled, hasExplicitWeChatOAuthCapabilities,
  resolveWeChatOAuthStart, resolveWeChatOAuthStartStrict, getOAuthCompletionKind,
  getPendingOAuthBindLoginKind, hasPendingOAuthSuggestedProfile, isOAuthLoginCompletion,
  persistOAuthTokenContext, prepareOAuthBindAccessTokenCookie, completePendingOAuthBindLogin,
  exchangePendingOAuthCompletion } from './api/auth'
export type { LoginResponse, OAuthLoginStart, OAuthLoginProvider, OAuthTokenResponse,
  OAuthAdoptionDecision, PendingOAuthExchangeResponse, PendingOAuthBindLoginResponse,
  PendingOAuthCreateAccountResponse, PendingOAuthSendVerifyCodeResponse,
  WeChatOAuthPublicSettings, RefreshTokenResponse, ForgotPasswordRequest,
  ForgotPasswordResponse, ResetPasswordRequest, ResetPasswordResponse } from './api/auth'
export type { LoginRequest, RegisterRequest, AuthResponse, CurrentUserResponse,
  ActionCaptchaRequestProof, TencentCaptchaRequestProof, SendVerifyCodeRequest,
  SendVerifyCodeResponse, TotpLoginResponse, TotpLogin2FARequest } from './api/types'
export { passkeyAPI } from './api/passkey'
export type { PasskeyCredentialSummary } from './api/passkey'
export { verifyStepUp } from './api/stepUp'
export type { StepUpGrant } from './api/stepUp'
export { useStepUp, StepUpCancelledError, isStepUpCancelled, isStepUpRequired,
  isStepUpBlocked, stepUpBlockReason, globalAdminStepUp, requestAdminStepUp } from './composables/useStepUp'
export type { StepUpController } from './composables/useStepUp'
export { useIdentityRequest } from './queries/useIdentityRequest'
export type { IdentityRequestOwner, IdentityRequestTicket } from './queries/useIdentityRequest'
export { identityNotificationsKey, identityClipboardKey, identitySettingsKey, identityPortalKey, identityAuthShellKey } from './ports'
export type { IdentityNotifications, IdentityClipboard, IdentityPublicSettings, IdentitySettings, IdentityPortal } from './ports'
export { default as TotpStepUpDialog } from './components/TotpStepUpDialog.vue'

export { profileAPI, getProfile, updateProfile, changePassword, sendNotifyEmailCode, verifyNotifyEmail, removeNotifyEmail, toggleNotifyEmail, buildOAuthBindingStartURL, startOAuthBinding, bindEmailIdentity, sendEmailBindingCode, unbindAuthIdentity } from './api/profile'
export type { BindableOAuthProvider } from './api/profile'
export { totpAPI } from './api/totp'
export type { NotifyEmailEntry, UserAuthBindingStatus, UserProfileSourceContext, User, AdminUser, UpdateUserRequest, ChangePasswordRequest, UserAuthProvider } from './api/profileTypes'
export type { TotpStatus, TotpSetupRequest, TotpSetupResponse, TotpEnableRequest, TotpEnableResponse, TotpDisableRequest, TotpVerificationMethod } from './api/securityTypes'

export { usersAPI } from './api/users'
export type { AdminBindAuthIdentityChannelRequest, AdminBindAuthIdentityRequest, AdminBoundAuthIdentityChannel, AdminBoundAuthIdentity, BatchUpdateUserLimitsRequest, BatchUpdateUserLimitsResponse } from './api/users'

export { userAttributesAPI } from './api/attributes'
export type { UserAttributeType, UserAttributeOption, UserAttributeValidation, UserAttributeDefinition, UserAttributeValue, CreateUserAttributeRequest, UpdateUserAttributeRequest, UserAttributeValuesMap } from './api/attributeTypes'
export { getAdminRolePermissions, updateAdminRolePermissions } from './api/roles'
export type { AdminRolePolicy, AdminPermission, AdminRolePermissionsResponse } from './api/roles'
export { USER_ROLES, isVendorRole } from './model/userRoles'
export { default as AdminRolePermissions } from './panels/AdminRolePermissions.vue'
export { default as UserAttributeForm } from './components/users/UserAttributeForm.vue'
export { default as UserAttributesConfigModal } from './components/users/UserAttributesConfigModal.vue'
export { default as UserConcurrencyCell } from './components/users/UserConcurrencyCell.vue'

export { identityUserDirectoryDataKey, identityUserPanelsKey } from './userPorts'
export type { IdentityUserDirectoryData, IdentityUserPanels } from './userPorts'
export { default as UserCreateModal } from './components/users/UserCreateModal.vue'
export { default as UserEditModal } from './components/users/UserEditModal.vue'
export { default as BulkEditUserModal } from './components/users/BulkEditUserModal.vue'
export { default as ProfilePasswordForm } from './components/profile/ProfilePasswordForm.vue'
export { default as ProfileTotpCard } from './components/profile/ProfileTotpCard.vue'
export { default as ProfilePasskeyCard } from './components/profile/ProfilePasskeyCard.vue'

export type { AccessRule, AccessPolicy, Permission as AdminKeyPermission, AdminKeyInput, AdminKey } from './api/adminAccessKeys'
export { adminComplianceAPI } from './api/compliance'
export type { AdminComplianceAcknowledgement, AdminComplianceStatus, AcceptAdminComplianceRequest } from './api/compliance'
export { useAdminComplianceStore } from './model/adminCompliance'
