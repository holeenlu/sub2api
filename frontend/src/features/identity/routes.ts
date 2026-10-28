import type { RouteRecordRaw } from 'vue-router'

/** Existing URLs and names remain stable; app owns guards and layout composition. */
export const identityRoutes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/features/identity/pages/LoginView.vue'),
    meta: { domain: 'identity',  requiresAuth: false, title: 'Login', titleKey: 'home.login' }
  },
  {
    path: '/register',
    name: 'Register',
    component: () => import('@/features/identity/pages/RegisterView.vue'),
    meta: { domain: 'identity',  requiresAuth: false, title: 'Register', titleKey: 'auth.createAccount' }
  },
  {
    path: '/email-verify',
    name: 'EmailVerify',
    component: () => import('@/features/identity/pages/EmailVerifyView.vue'),
    meta: { domain: 'identity',  requiresAuth: false, title: 'Verify Email', titleKey: 'auth.verifyYourEmail' }
  },
  {
    path: '/auth/callback',
    name: 'OAuthCallback',
    alias: '/auth/oauth/callback',
    component: () => import('@/features/identity/pages/OAuthCallbackView.vue'),
    meta: { domain: 'identity',  requiresAuth: false, title: 'OAuth Callback', titleKey: 'auth.oauthCallbackPageTitle' }
  },
  {
    path: '/auth/linuxdo/callback',
    name: 'LinuxDoOAuthCallback',
    component: () => import('@/features/identity/pages/LinuxDoCallbackView.vue'),
    meta: { domain: 'identity',  requiresAuth: false, title: 'LinuxDo OAuth Callback', titleKey: 'auth.linuxdoCallbackPageTitle' }
  },
  {
    path: '/auth/wechat/callback',
    name: 'WeChatOAuthCallback',
    component: () => import('@/features/identity/pages/WechatCallbackView.vue'),
    meta: { domain: 'identity',  requiresAuth: false, title: 'WeChat OAuth Callback', titleKey: 'auth.wechatCallbackPageTitle' }
  },
  {
    path: '/auth/dingtalk/callback',
    name: 'DingTalkOAuthCallback',
    component: () => import('@/features/identity/pages/DingTalkCallbackView.vue'),
    meta: { domain: 'identity',  requiresAuth: false, title: 'DingTalk OAuth Callback', titleKey: 'auth.dingtalkCallbackPageTitle' }
  },
  {
    path: '/auth/dingtalk/email-completion',
    name: 'dingtalk-email-completion',
    component: () => import('@/features/identity/pages/DingTalkEmailCompletionView.vue'),
    meta: { domain: 'identity',  requiresAuth: false, title: 'DingTalk Email Completion', titleKey: 'auth.dingtalk.createAccountTitle' }
  },
  {
    path: '/auth/oidc/callback',
    name: 'OIDCOAuthCallback',
    component: () => import('@/features/identity/pages/OidcCallbackView.vue'),
    meta: { domain: 'identity',  requiresAuth: false, title: 'OIDC OAuth Callback', titleKey: 'auth.oidcCallbackPageTitle' }
  },
  {
    path: '/forgot-password',
    name: 'ForgotPassword',
    component: () => import('@/features/identity/pages/ForgotPasswordView.vue'),
    meta: { domain: 'identity',  requiresAuth: false, title: 'Forgot Password', titleKey: 'auth.forgotPasswordTitle' }
  },
  {
    path: '/reset-password',
    name: 'ResetPassword',
    component: () => import('@/features/identity/pages/ResetPasswordView.vue'),
    meta: { domain: 'identity',  requiresAuth: false, title: 'Reset Password', titleKey: 'auth.resetPassword' }
  },
  {
    path: '/profile',
    name: 'Profile',
    component: () => import('./pages/ProfilePage.vue'),
    meta: { domain: 'identity', navigation: {order: 100, icon: 'user', labelKey: 'nav.profile'},  requiresAuth: true, requiresAdmin: false, title: 'Profile', titleKey: 'profile.title', descriptionKey: 'profile.description', shell: 'user' }
  },
  {
    path: '/admin/users',
    name: 'AdminUsers',
    component: () => import('./pages/UsersPage.vue'),
    meta: { domain: 'identity', navigation: {order: 30, icon: 'users', labelKey: 'nav.users', hideInSimpleMode: true},  requiresAuth: true, requiresAdmin: true, title: 'User Management', titleKey: 'admin.users.title', descriptionKey: 'admin.users.description', shell: 'admin' }
  }
]
