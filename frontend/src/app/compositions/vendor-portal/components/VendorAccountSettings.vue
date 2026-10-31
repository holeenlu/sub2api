<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/features/identity'
import { useAppStore } from '@/app/model/app'
import { useVendorPortalSecurityPanels } from '../panels'
const securityPanels = useVendorPortalSecurityPanels()
import { useSupplyStore } from '@/features/supply'

const { t, locale } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const vendor = useSupplyStore()
const activeTab = ref<'info' | 'password'>('info')

const displayName = computed(
  () =>
    auth.user?.username ||
    auth.user?.email?.split('@')[0] ||
    vendor.vendor?.name ||
    t('vendor.account'),
)
const initials = computed(() => displayName.value.slice(0, 2).toUpperCase() || 'V')
const role = computed(() => t(auth.user?.role === 'super_admin' ? 'admin.users.roles.super_admin' : auth.user?.role === 'vendor_level1' ? 'vendor.tier1' : 'vendor.tier2'))

function formatDate(value?: string | null) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale.value, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}
</script>

<template>
  <section class="mx-auto max-w-4xl space-y-5" data-testid="vendor-account-settings">
    <header>
      <p class="text-xs font-semibold uppercase tracking-[0.16em] text-primary-600 dark:text-primary-400">
        {{ t('vendor.accountCenter') }}
      </p>
      <h1 class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
        {{ t('vendor.accountSettings') }}
      </h1>
      <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
        {{ t('vendor.accountSettingsHelp') }}
      </p>
    </header>

    <div class="flex rounded-2xl border border-gray-200 bg-gray-100 p-1 dark:border-dark-700 dark:bg-dark-800" role="tablist" :aria-label="t('vendor.accountSettings')">
      <button
        type="button"
        role="tab"
        class="flex-1 rounded-xl px-4 py-2.5 text-sm font-medium transition-colors"
        :class="activeTab === 'info' ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white' : 'text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-gray-200'"
        :aria-selected="activeTab === 'info'"
        @click="activeTab = 'info'"
      >
        {{ t('vendor.accountInfo') }}
      </button>
      <button
        type="button"
        role="tab"
        class="flex-1 rounded-xl px-4 py-2.5 text-sm font-medium transition-colors"
        :class="activeTab === 'password' ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white' : 'text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-gray-200'"
        :aria-selected="activeTab === 'password'"
        @click="activeTab = 'password'"
      >
        {{ t('vendor.changePassword') }}
      </button>
    </div>

    <template v-if="activeTab === 'info'">
      <section class="card overflow-hidden">
        <div class="flex flex-wrap items-center gap-4 border-b border-gray-100 px-5 py-5 dark:border-dark-700 sm:px-6">
          <span class="flex h-14 w-14 shrink-0 items-center justify-center rounded-full bg-primary-50 text-lg font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
            <img v-if="auth.user?.avatar_url" :src="auth.user.avatar_url" :alt="displayName" class="h-full w-full rounded-full object-cover" />
            <span v-else>{{ initials }}</span>
          </span>
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <h2 class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ auth.user?.email || vendor.vendor?.email || '—' }}</h2>
              <span class="rounded-full border border-primary-200 px-2.5 py-0.5 text-xs font-medium text-primary-700 dark:border-primary-800 dark:text-primary-300">{{ role }}</span>
            </div>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ displayName }}</p>
          </div>
        </div>
        <dl class="divide-y divide-gray-100 dark:divide-dark-700">
          <div class="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5 sm:px-6">
            <dt class="text-sm text-gray-500 dark:text-dark-400">{{ t('vendor.email') }}</dt>
            <dd class="break-all text-right text-sm font-medium text-gray-900 dark:text-gray-100">{{ auth.user?.email || vendor.vendor?.email || '—' }}</dd>
          </div>
          <div class="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5 sm:px-6">
            <dt class="text-sm text-gray-500 dark:text-dark-400">{{ t('vendor.username') }}</dt>
            <dd class="text-right text-sm font-medium text-gray-900 dark:text-gray-100">{{ auth.user?.username || '—' }}</dd>
          </div>
          <div class="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5 sm:px-6">
            <dt class="text-sm text-gray-500 dark:text-dark-400">{{ t('vendor.role') }}</dt>
            <dd class="text-right text-sm font-medium text-gray-900 dark:text-gray-100">{{ role }}</dd>
          </div>
          <div class="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5 sm:px-6">
            <dt class="text-sm text-gray-500 dark:text-dark-400">{{ t('vendor.registeredAt') }}</dt>
            <dd class="text-right text-sm font-medium tabular-nums text-gray-900 dark:text-gray-100">{{ formatDate(auth.user?.created_at) }}</dd>
          </div>
          <div class="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5 sm:px-6">
            <dt class="text-sm text-gray-500 dark:text-dark-400">{{ t('vendor.loginPassword') }}</dt>
            <dd class="text-right text-sm font-medium text-gray-900 dark:text-gray-100">{{ t('vendor.passwordStatusUnknown') }}</dd>
          </div>
        </dl>
      </section>

      <section class="space-y-3">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('vendor.securityVerification') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('vendor.securityVerificationHelp') }}</p>
        </div>
        <component :is="securityPanels.totp" />
        <component :is="securityPanels.passkey" :enabled="app.cachedPublicSettings?.passkey_enabled === true" />
      </section>
    </template>

    <component :is="securityPanels.password" v-else embedded />
  </section>
</template>
