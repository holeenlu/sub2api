<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/features/identity'
const { t } = useI18n(),
  auth = useAuthStore()
async function logout() {
  await auth.logout()
  window.location.assign('/login')
}
</script>
<template>
  <main class="mx-auto max-w-xl space-y-4 p-8">
    <h1 class="text-2xl font-semibold">{{ t('vendor.portal') }}</h1>
    <p role="alert">{{ t('vendor.unavailable') }}</p>
    <button class="btn btn-primary" @click="logout">
      {{ t('vendor.logout') }}</button
    ><button
      class="btn btn-secondary ml-2"
      @click="$router.replace('/vendor/accounts')"
    >
      {{ t('vendor.retry') }}
    </button>
  </main>
</template>
