<template>
  <div class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <!-- Background Decoration -->
    <div class="pointer-events-none fixed inset-0 bg-mesh-gradient"></div>

    <TotpStepUpDialog :controller="globalAdminStepUp" />

    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64']"
    >
      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main class="p-4 md:p-6 lg:p-8">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onMounted, onUnmounted } from 'vue'
import { useAppStore } from "@/app/model/app"
import { useAuthStore } from '@/features/identity'
import { useOnboardingTour } from '@/app/onboarding/useOnboardingTour'
import { useOnboardingStore } from '@/app/onboarding/store'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import { TotpStepUpDialog as TotpStepUpDialog } from '@/features/identity'
import { globalAdminStepUp } from '@/features/identity'

const appStore = useAppStore()
const authStore = useAuthStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const isAdmin = computed(() => authStore.isAdmin)

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

onUnmounted(() => globalAdminStepUp.onCancel())

defineExpose({ replayTour })
</script>
