<template>
  <div class="relative flex min-h-screen flex-col bg-gray-50 dark:bg-dark-950">
    <!-- Header (same pattern as HomeView) -->
    <header class="relative z-20 px-6 py-4">
      <nav class="mx-auto flex max-w-6xl items-center justify-between">
        <router-link to="/home" class="flex items-center gap-3">
          <div class="h-10 w-10 overflow-hidden rounded-xl shadow-md">
            <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <span class="text-lg font-semibold tracking-tight text-gray-900 dark:text-white">{{ siteName }}</span>
        </router-link>
        <div class="flex items-center gap-3">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <button
            @click="toggleTheme"
            class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
        </div>
      </nav>
    </header>

    <!-- Main Content -->
    <main class="flex-1 w-full max-w-5xl mx-auto px-6 py-12">
      <!-- Hero -->
      <div class="text-center mb-12">
        <h1 class="text-3xl sm:text-4xl font-bold tracking-tight mb-3 text-gray-900 dark:text-white">
          {{ t('keyUsage.title') }}
        </h1>
        <p class="text-gray-500 dark:text-dark-400 text-base max-w-md mx-auto">
          {{ t('keyUsage.subtitle') }}
        </p>
      </div>

      <!-- Input Section -->
      <div class="max-w-xl mx-auto mb-14">
        <div class="flex gap-3">
          <div class="flex-1 relative">
            <div class="absolute left-4 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500">
              <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>
              </svg>
            </div>
            <input
              v-model="apiKey"
              :type="keyVisible ? 'text' : 'password'"
              :placeholder="t('keyUsage.placeholder')"
              class="input-ring w-full h-12 pl-12 pr-12 rounded-xl border border-gray-200 bg-white text-sm text-gray-900 placeholder:text-gray-400 transition-all dark:border-dark-700 dark:bg-dark-900 dark:text-white dark:placeholder:text-dark-500"
              @keydown.enter="queryKey"
            />
            <button
              @click="keyVisible = !keyVisible"
              class="absolute right-4 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-700 dark:text-dark-500 dark:hover:text-white transition-colors"
            >
              <svg v-if="!keyVisible" class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/>
                <line x1="1" y1="1" x2="23" y2="23"/>
              </svg>
              <svg v-else class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/>
              </svg>
            </button>
          </div>
          <button
            @click="queryKey"
            :disabled="isQuerying"
            class="h-12 px-7 rounded-xl bg-primary-500 hover:bg-primary-600 text-white font-medium text-sm transition-all active:scale-[0.97] flex items-center gap-2 whitespace-nowrap disabled:opacity-60"
          >
            <svg v-if="isQuerying" class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none">
              <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3" opacity="0.25"/>
              <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" stroke-width="3" stroke-linecap="round"/>
            </svg>
            <svg v-else class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
            </svg>
            {{ isQuerying ? t('keyUsage.querying') : t('keyUsage.query') }}
          </button>
        </div>
        <p class="text-xs text-gray-400 dark:text-dark-500 mt-3 text-center">
          {{ t('keyUsage.privacyNote') }}
        </p>

        <!-- Date Range Picker -->
        <div v-if="showDatePicker" class="mt-4">
          <div class="flex flex-wrap items-center gap-2 justify-center">
            <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('keyUsage.dateRange') }}</span>
            <button
              v-for="range in dateRanges"
              :key="range.key"
              @click="setDateRange(range.key)"
              class="text-xs px-3 py-1.5 rounded-lg border transition-all"
              :class="currentRange === range.key
                ? 'bg-primary-500 text-white border-primary-500'
                : 'border-gray-200 bg-white text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200 hover:border-primary-300 dark:hover:border-dark-600'"
            >{{ range.label }}</button>
            <div v-if="currentRange === 'custom'" class="flex items-center gap-2 ml-1">
              <input
                v-model="customStartDate"
                type="date"
                class="input-ring text-xs px-2 py-1.5 rounded-lg border border-gray-200 bg-white text-gray-900 dark:border-dark-700 dark:bg-dark-900 dark:text-white"
              />
              <span class="text-xs text-gray-400">-</span>
              <input
                v-model="customEndDate"
                type="date"
                class="input-ring text-xs px-2 py-1.5 rounded-lg border border-gray-200 bg-white text-gray-900 dark:border-dark-700 dark:bg-dark-900 dark:text-white"
              />
              <button
                @click="queryKey"
                class="text-xs px-3 py-1.5 rounded-lg bg-primary-500 text-white hover:bg-primary-600"
              >{{ t('keyUsage.apply') }}</button>
            </div>
          </div>
        </div>
      </div>

      <!-- Results Container -->
      <div v-if="showResults">
        <!-- Loading Skeleton -->
        <div v-if="showLoading" class="space-y-6">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div class="rounded-2xl border border-gray-200 bg-white p-8 dark:border-dark-700 dark:bg-dark-900">
              <div class="skeleton h-5 w-24 mb-6"></div>
              <div class="flex justify-center"><div class="skeleton w-44 h-44 rounded-full"></div></div>
            </div>
            <div class="rounded-2xl border border-gray-200 bg-white p-8 dark:border-dark-700 dark:bg-dark-900">
              <div class="skeleton h-5 w-24 mb-6"></div>
              <div class="flex justify-center"><div class="skeleton w-44 h-44 rounded-full"></div></div>
            </div>
          </div>
          <div class="rounded-2xl border border-gray-200 bg-white p-8 dark:border-dark-700 dark:bg-dark-900">
            <div class="skeleton h-5 w-32 mb-6"></div>
            <div class="space-y-4">
              <div class="skeleton h-4 w-full"></div>
              <div class="skeleton h-4 w-3/4"></div>
              <div class="skeleton h-4 w-5/6"></div>
              <div class="skeleton h-4 w-2/3"></div>
            </div>
          </div>
        </div>

        <!-- Result Content -->
        <div v-else-if="resultData" class="space-y-6">
          <!-- Status Badge -->
          <div v-if="statusInfo" class="fade-up flex items-center justify-center mb-2">
            <div class="inline-flex items-center gap-2 px-5 py-2.5 rounded-full border border-gray-200 bg-white/90 shadow-sm backdrop-blur-sm dark:border-dark-700 dark:bg-dark-900/90">
              <span
                class="w-2.5 h-2.5 rounded-full pulse-dot"
                :class="statusInfo.isActive ? 'bg-emerald-500' : 'bg-rose-500'"
              ></span>
              <span class="text-sm font-medium text-gray-900 dark:text-white">{{ statusInfo.label }}</span>
              <span class="text-xs text-gray-400 dark:text-dark-500">|</span>
              <span class="text-xs text-gray-500 dark:text-dark-400">{{ statusInfo.statusText }}</span>
            </div>
          </div>

          <PublicKeyUsageRings :items="ringItems" :is-dark="isDark" :format-reset-time="formatResetTime" />
          <PublicKeyUsageDetails :detail-rows="detailRows" />
          <PublicKeyUsageTables :usage-stat-cells="usageStatCells" :daily-usage-rows="dailyUsageRows" :model-stats="modelStats" :show-daily-usage="showDailyUsage" :daily-usage-days="dailyUsageDays" @days="setDailyUsageDays" />
        </div>
      </div>
    </main>

    <!-- Footer (same pattern as HomeView) -->
    <footer class="relative z-10 border-t border-gray-200/50 px-6 py-8 dark:border-dark-800/50">
      <div class="mx-auto flex max-w-6xl flex-col items-center justify-center gap-4 text-center sm:flex-row sm:text-left">
        <p class="text-sm text-gray-500 dark:text-dark-400">
          &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
        </p>
        <div class="flex items-center gap-4">
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="text-sm text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-white"
          >{{ t('home.docs') }}</a>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from '@/shared/ui/LocaleSwitcher.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { sanitizeUrl } from '@/shared/lib/url'
import { BRAND_NAME } from '@/shared/config/project'
import { usePublicKeyUsageServices } from '../publicKeyServices'
import { usePublicKeyUsage } from '../queries/usePublicKeyUsage'
import { usePublicKeyUsagePresentation } from '../model/usePublicKeyUsagePresentation'
import PublicKeyUsageRings from '../components/PublicKeyUsageRings.vue'
import PublicKeyUsageDetails from '../components/PublicKeyUsageDetails.vue'
import PublicKeyUsageTables from '../components/PublicKeyUsageTables.vue'
const { t } = useI18n(), services = usePublicKeyUsageServices()
const siteName = computed(() => services.appearance.value.siteName || BRAND_NAME)
const siteLogo = computed(() => sanitizeUrl(services.appearance.value.siteLogo, { allowRelative: true, allowDataUrl: true }))
const docUrl = computed(() => sanitizeUrl(services.appearance.value.docUrl))
const subscriptionFeatureEnabled = computed(() => services.appearance.value.subscriptionEnabled)
const isDark = ref(document.documentElement.classList.contains('dark')), keyVisible = ref(false)
function toggleTheme(): void { isDark.value = !isDark.value; document.documentElement.classList.toggle('dark', isDark.value); localStorage.setItem('theme', isDark.value ? 'dark' : 'light') }
const currentYear = new Date().getFullYear()
const { apiKey, isQuerying, showLoading, showResults, showDatePicker, resultData, currentRange, customStartDate, customEndDate, dailyUsageDays, queryKey, setDateRange, setDailyUsageDays } = usePublicKeyUsage()
const { statusInfo, ringItems, detailRows, usageStatCells, modelStats, dailyUsageRows, showDailyUsage, formatResetTime } = usePublicKeyUsagePresentation(resultData, subscriptionFeatureEnabled)
const dateRanges = computed(() => [
  { key: 'today' as const, label: t('keyUsage.dateRangeToday') }, { key: '7d' as const, label: t('keyUsage.dateRange7d') },
  { key: '30d' as const, label: t('keyUsage.dateRange30d') }, { key: 'custom' as const, label: t('keyUsage.dateRangeCustom') },
])
onMounted(() => {
  const saved = localStorage.getItem('theme')
  if (saved === 'dark' || (!saved && window.matchMedia('(prefers-color-scheme: dark)').matches)) { isDark.value = true; document.documentElement.classList.add('dark') }
  void services.ensureSettings().catch(() => {})
})
</script>
<style scoped>
/* Input focus ring */
.input-ring {
  transition: box-shadow 0.2s ease, border-color 0.2s ease;
}
.input-ring:focus {
  box-shadow: 0 0 0 3px rgba(20, 184, 166, 0.2);
  border-color: #14b8a6;
  outline: none;
}

/* Ring animation */
:deep(.progress-ring) {
  transition: stroke-dashoffset 1.2s cubic-bezier(0.4, 0, 0.2, 1);
  transform: rotate(-90deg);
  transform-origin: 50% 50%;
}

/* Skeleton loading */
@keyframes shimmer-kv {
  0%   { background-position: -200% 0; }
  100% { background-position: 200% 0; }
}
:deep(.skeleton) {
  background: linear-gradient(90deg, #e5e7eb 25%, #f3f4f6 50%, #e5e7eb 75%);
  background-size: 200% 100%;
  animation: shimmer-kv 1.8s ease-in-out infinite;
  border-radius: 8px;
}
:global(.dark) :deep(.skeleton) {
  background: linear-gradient(90deg, #334155 25%, #1e293b 50%, #334155 75%);
  background-size: 200% 100%;
}

/* Fade up animation */
@keyframes fade-up-kv {
  from { opacity: 0; transform: translateY(16px); }
  to { opacity: 1; transform: translateY(0); }
}
:deep(.fade-up) {
  animation: fade-up-kv 0.5s cubic-bezier(0.4, 0, 0.2, 1) forwards;
}
:deep(.fade-up-delay-1) { animation-delay: 0.1s; opacity: 0; }
:deep(.fade-up-delay-2) { animation-delay: 0.2s; opacity: 0; }
:deep(.fade-up-delay-3) { animation-delay: 0.3s; opacity: 0; }
:deep(.fade-up-delay-4) { animation-delay: 0.4s; opacity: 0; }

/* Pulse dot */
@keyframes pulse-dot-kv {
  0%, 100% { opacity: 1; box-shadow: 0 0 0 0 currentColor; }
  50% { opacity: 0.6; box-shadow: 0 0 8px 2px currentColor; }
}
:deep(.pulse-dot) {
  animation: pulse-dot-kv 2s ease-in-out infinite;
}

/* Tabular nums */
:deep(.tabular-nums) {
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.02em;
}
</style>
