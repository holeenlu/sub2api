<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="hasHomeContent" class="min-h-screen">
    <!-- iframe mode -->
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <!-- HTML mode - SECURITY: homeContent is admin-only setting, XSS risk is acceptable -->
    <div v-else v-html="homeContent"></div>
  </div>

  <!-- Compact Home Page -->
  <div
    v-else-if="compactHomeEnabled"
    data-testid="compact-home"
    class="flex min-h-screen flex-col bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white"
  >
    <header class="border-b border-gray-200 px-4 py-4 sm:px-6 dark:border-dark-800">
      <nav class="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3 sm:gap-4">
        <div class="flex min-w-0 flex-1 items-center gap-3">
          <img
            :src="siteLogo || '/logo.svg'"
            alt="Logo"
            class="h-9 w-9 shrink-0 rounded-lg object-contain"
          />
          <span class="min-w-0 truncate text-base font-semibold">{{ siteName }}</span>
        </div>
        <div class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-1 sm:gap-2">
          <LocaleSwitcher align="left" />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <router-link
            to="/docs"
            data-testid="compact-home-api-docs"
            class="inline-flex min-h-10 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
          >
            <Icon name="book" size="md" />
            <span>{{ t('home.apiDocs') }}</span>
          </router-link>
          <router-link
            to="/apps"
            data-testid="compact-home-ai-apps"
            class="inline-flex min-h-10 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
          >
            <Icon name="sparkles" size="md" />
            <span>{{ t('home.aiApps') }}</span>
          </router-link>
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="flex h-10 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="t('nav.modelPlaza')"
          >
            <Icon name="grid" size="md" />
            <span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span>
          </router-link>
          <button
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link
            data-testid="compact-home-primary"
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex min-h-10 shrink-0 items-center justify-center rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
          >
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main class="flex min-w-0 flex-1 items-center justify-center px-4 py-16 sm:px-6">
      <div class="min-w-0 max-w-2xl text-center">
        <img
          :src="siteLogo || '/logo.svg'"
          alt="Logo"
          class="mx-auto mb-6 h-20 w-20 rounded-2xl object-contain"
        />
        <h1 class="[overflow-wrap:anywhere] text-3xl font-bold md:text-4xl">{{ siteName }}</h1>
        <p class="mt-4 whitespace-pre-wrap [overflow-wrap:anywhere] text-base text-gray-600 dark:text-dark-300">{{ siteSubtitle }}</p>
        <router-link
          data-testid="compact-home-hero-primary"
          :to="isAuthenticated ? dashboardPath : '/login'"
          class="mt-8 inline-flex min-h-10 items-center justify-center rounded-lg bg-primary-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-primary-700"
        >
          {{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}
        </router-link>
      </div>
    </main>

    <footer class="min-w-0 border-t border-gray-200 px-4 py-5 text-center text-sm text-gray-500 [overflow-wrap:anywhere] sm:px-6 dark:border-dark-800 dark:text-dark-400">
      &copy; {{ currentYear }} {{ siteName }}
    </footer>
  </div>

  <!-- Default Home Page -->
  <div
    v-else
    class="relative flex min-h-screen flex-col overflow-hidden bg-gradient-to-br from-gray-50 via-primary-50/30 to-gray-100 dark:from-dark-950 dark:via-dark-900 dark:to-dark-950"
  >
    <!-- Background Decorations -->
    <div class="pointer-events-none absolute inset-0 overflow-hidden">
      <div
        class="absolute -right-40 -top-40 h-96 w-96 rounded-full bg-primary-400/20 blur-3xl"
      ></div>
      <div
        class="absolute -bottom-40 -left-40 h-96 w-96 rounded-full bg-primary-500/15 blur-3xl"
      ></div>
      <div
        class="absolute left-1/3 top-1/4 h-72 w-72 rounded-full bg-primary-300/10 blur-3xl"
      ></div>
      <div
        class="absolute bottom-1/4 right-1/4 h-64 w-64 rounded-full bg-primary-400/10 blur-3xl"
      ></div>
      <div
        class="absolute inset-0 bg-[linear-gradient(rgba(20,184,166,0.03)_1px,transparent_1px),linear-gradient(90deg,rgba(20,184,166,0.03)_1px,transparent_1px)] bg-[size:64px_64px]"
      ></div>
    </div>

    <!-- Header -->
    <header class="relative z-20 px-6 py-4">
      <nav class="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-3">
        <!-- Wordmark -->
        <router-link to="/" class="flex min-w-0 items-center gap-2.5">
          <img
            :src="siteLogo || '/logo.svg'"
            :alt="siteName"
            data-testid="home-brand-logo"
            class="h-[26px] w-[26px] shrink-0 object-contain"
          />
          <span class="truncate text-[19px] font-bold tracking-tight text-gray-900 dark:text-white">
            {{ siteName }}
          </span>
        </router-link>

        <!-- Nav Actions -->
        <div class="flex max-w-full flex-wrap items-center justify-end gap-1 sm:gap-2">
          <!-- Language Switcher -->
          <LocaleSwitcher />

          <!-- External Docs -->
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
          <router-link
            to="/docs"
            data-testid="home-api-docs"
            class="inline-flex min-h-10 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
          >
            <Icon name="book" size="md" />
            <span>{{ t('home.apiDocs') }}</span>
          </router-link>
          <router-link
            to="/apps"
            data-testid="home-ai-apps"
            class="inline-flex min-h-10 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
          >
            <Icon name="sparkles" size="md" />
            <span>{{ t('home.aiApps') }}</span>
          </router-link>

          <!-- Theme Toggle -->
          <button
            @click="toggleTheme"
            class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>

          <!-- Login / Dashboard Button -->
          <router-link
            v-if="isAuthenticated"
            :to="dashboardPath"
            class="inline-flex items-center gap-1.5 rounded-full bg-gray-900 py-1 pl-1 pr-2.5 transition-colors hover:bg-gray-800 dark:bg-gray-800 dark:hover:bg-gray-700"
          >
            <span
              class="flex h-5 w-5 items-center justify-center rounded-full bg-gradient-to-br from-primary-400 to-primary-600 text-[10px] font-semibold text-white"
            >
              {{ userInitial }}
            </span>
            <span class="text-xs font-medium text-white">{{ t('home.dashboard') }}</span>
            <svg
              class="h-3 w-3 text-gray-400"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M4.5 19.5l15-15m0 0H8.25m11.25 0v11.25"
              />
            </svg>
          </router-link>
          <router-link
            v-else
            to="/login"
            class="inline-flex items-center rounded-full bg-gray-900 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-gray-800 dark:bg-gray-800 dark:hover:bg-gray-700"
          >
            {{ t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <!-- Main Content -->
    <main class="relative z-10 flex-1 px-6">
      <div class="mx-auto max-w-6xl">
        <!-- ============ 1. 主视觉 ============ -->
        <section id="hero" class="py-16">
          <div class="flex flex-col items-center justify-between gap-12 lg:flex-row lg:gap-14">
            <!-- Left: Text Content -->
            <div class="w-full min-w-0 flex-1 text-center lg:text-left">
              <span
                class="mb-5 inline-flex items-center rounded-full border border-primary-100 bg-primary-50 px-3.5 py-1 text-[13px] font-semibold tracking-wide text-primary-700 dark:border-primary-900 dark:bg-primary-950/40 dark:text-primary-300"
              >
                {{ t('home.heroEyebrow') }}
              </span>
              <h1
                class="mb-5 break-words text-3xl font-extrabold tracking-tight text-gray-900 sm:text-4xl md:text-5xl lg:text-[52px] dark:text-white"
              >
                {{ t('home.heroTitle') }}
              </h1>
              <p
                class="mx-auto mb-8 max-w-full whitespace-pre-line break-words text-base text-gray-600 md:text-[17px] lg:mx-0 lg:max-w-[34em] dark:text-dark-300"
              >
                {{ t('home.heroDescription') }}
              </p>

              <!-- CTA Buttons -->
              <div class="flex flex-wrap items-center justify-center gap-3.5 lg:justify-start">
                <router-link
                  :to="isAuthenticated ? dashboardPath : '/login'"
                  class="btn btn-primary px-8 py-3 text-base shadow-lg shadow-primary-500/30"
                >
                  {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
                  <Icon name="arrowRight" size="md" class="ml-2" :stroke-width="2" />
                </router-link>
                <button
                  type="button"
                  class="btn btn-secondary px-8 py-3 text-base"
                  @click="scrollToModels"
                >
                  {{ t('home.exploreModels') }}
                </button>
                <a v-if="contactMailto" :href="contactMailto" :class="textLinkClass">
                  {{ t('home.contactIntegration') }}
                </a>
              </div>
            </div>

            <!-- Right: Terminal Animation -->
            <div class="flex w-full min-w-0 flex-1 flex-col items-center lg:items-end">
              <div class="terminal-container">
                <div class="terminal-window">
                  <!-- Window header -->
                  <div class="terminal-header">
                    <div class="terminal-buttons">
                      <span class="btn-close"></span>
                      <span class="btn-minimize"></span>
                      <span class="btn-maximize"></span>
                    </div>
                    <span class="terminal-title">terminal</span>
                  </div>
                  <!-- Terminal content -->
                  <div class="terminal-body">
                    <div class="code-line line-1">
                      <span class="code-prompt">$</span>
                      <span class="code-cmd">curl</span>
                      <span class="code-flag">-X POST</span>
                      <span class="code-url">/v1/messages</span>
                    </div>
                    <div class="code-line line-2">
                      <span class="code-comment"># {{ t('home.terminal.routing') }}</span>
                    </div>
                    <div class="code-line line-3">
                      <span class="code-success">200 OK</span>
                      <span class="code-response">{ "content": "Hello!" }</span>
                    </div>
                    <div class="code-line line-4">
                      <span class="code-prompt">$</span>
                      <span class="cursor"></span>
                    </div>
                  </div>
                </div>
              </div>
              <p class="mt-3 text-center text-xs text-gray-500 dark:text-dark-400">
                {{ t('home.terminal.caption') }}
              </p>
            </div>
          </div>
        </section>

        <section class="mx-auto max-w-6xl px-6 pb-4 sm:px-8 lg:px-10">
          <div class="rounded-2xl border border-primary-200/70 bg-white/80 p-5 shadow-sm backdrop-blur dark:border-primary-900/60 dark:bg-dark-900/70 sm:flex sm:items-center sm:justify-between sm:gap-8">
            <div class="min-w-0"><p class="text-xs font-semibold uppercase tracking-[0.16em] text-primary-700 dark:text-primary-400">{{ t('home.quickInstall.eyebrow') }}</p><h2 class="mt-2 text-xl font-bold text-gray-900 dark:text-white">{{ t('home.quickInstall.title') }}</h2><p class="mt-1 text-sm text-gray-600 dark:text-dark-300">{{ t('home.quickInstall.description') }}</p></div>
            <div class="mt-4 flex shrink-0 flex-wrap gap-2 sm:mt-0"><router-link to="/apps/codex" class="btn btn-secondary text-sm">Codex CLI</router-link><router-link to="/apps/claude-code" class="btn btn-secondary text-sm">Claude Code</router-link></div>
          </div>
        </section>

        <!-- ============ 2. 模型与渠道费率（整区常驻；广场非公开时只隐藏深连结） ============ -->
        <section id="models" class="scroll-mt-20 py-16">
          <div :class="sectionHeadClass">
            <div :class="kickerClass">{{ sectionNumber('models') }}</div>
            <h2 :class="sectionTitleClass">{{ t('home.models.title') }}</h2>
            <p :class="sectionDescClass">{{ t('home.models.description') }}</p>
          </div>
          <ModelShowcase />
          <div class="mt-6 flex flex-wrap items-center justify-between gap-4">
            <p class="text-[13px] text-gray-500 dark:text-dark-400">
              {{ t('home.models.rateNote') }}
            </p>
            <router-link v-if="showModelPlazaEntry" to="/model-plaza" :class="textLinkClass">
              {{ t('home.exploreModels') }} →
            </router-link>
          </div>
        </section>

        <!-- ============ 3. 平台如何运作 ============ -->
        <section id="how" class="py-16">
          <div :class="sectionHeadClass">
            <div :class="kickerClass">{{ sectionNumber('how') }}</div>
            <h2 :class="sectionTitleClass">{{ t('home.architecture.title') }}</h2>
            <p :class="sectionDescClass">{{ t('home.architecture.description') }}</p>
          </div>
          <ArchitectureDiagram />
          <MechanismList />
        </section>

        <!-- ============ 4. 企业级 AI 闸道 ============ -->
        <section id="gateway" class="py-16">
          <div :class="sectionHeadClass">
            <div :class="kickerClass">
              {{ sectionNumber('gateway') }} · {{ t('home.management.subtitle') }}
            </div>
            <h2 :class="sectionTitleClass">{{ t('home.mechanisms.gateway.title') }}</h2>
            <p :class="sectionDescClass">{{ t('home.management.description') }}</p>
          </div>
          <GatewayShowcase />
          <p class="mt-5 text-[13px] text-gray-500 dark:text-dark-400">
            {{ t('home.management.auditNote') }}
          </p>
        </section>

        <!-- ============ 5. Agent 工具接入 ============ -->
        <section id="agents" class="py-16">
          <div :class="sectionHeadClass">
            <div :class="kickerClass">{{ sectionNumber('agents') }}</div>
            <h2 :class="sectionTitleClass">{{ t('home.agents.title') }}</h2>
            <p :class="sectionDescClass">{{ t('home.mechanisms.agents.description') }}</p>
          </div>
          <AgentToolCards />
        </section>

        <!-- ============ 6. 开始使用 ============ -->
        <section id="cta" class="py-16">
          <div
            class="rounded-2xl border border-primary-100 bg-gradient-to-b from-white to-primary-50/60 px-8 py-12 text-center shadow-sm dark:border-primary-900/60 dark:from-dark-800/80 dark:to-dark-900/80"
          >
            <h2 class="text-2xl font-bold text-gray-900 md:text-3xl dark:text-white">
              {{ t('home.cta.title') }}
            </h2>
            <p class="mt-3.5 text-gray-600 dark:text-dark-300">{{ t('home.cta.description') }}</p>
            <div class="mt-8 flex flex-wrap items-center justify-center gap-3.5">
              <router-link
                :to="isAuthenticated ? dashboardPath : '/login'"
                class="btn btn-primary px-8 py-3 text-base shadow-lg shadow-primary-500/30"
              >
                {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
                <Icon name="arrowRight" size="md" class="ml-2" :stroke-width="2" />
              </router-link>
              <button
                type="button"
                class="btn btn-secondary px-8 py-3 text-base"
                @click="scrollToModels"
              >
                {{ t('home.exploreModels') }}
              </button>
              <a v-if="contactMailto" :href="contactMailto" :class="textLinkClass">
                {{ t('home.contactIntegration') }}
              </a>
            </div>
          </div>
        </section>
      </div>
    </main>

    <!-- Footer -->
    <footer class="relative z-10 border-t border-gray-200/50 px-6 py-8 dark:border-dark-800/50">
      <div
        class="mx-auto flex max-w-6xl flex-col items-center justify-center gap-4 text-center sm:flex-row sm:text-left"
      >
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
          >
            {{ t('home.docs') }}
          </a>
          <router-link to="/docs" class="text-sm text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-white">{{ t('home.apiDocs') }}</router-link>
          <router-link to="/apps" class="text-sm text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-white">{{ t('home.aiApps') }}</router-link>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import ArchitectureDiagram from '@/components/home/ArchitectureDiagram.vue'
import MechanismList from '@/components/home/MechanismList.vue'
import ModelShowcase from '@/components/home/ModelShowcase.vue'
import GatewayShowcase from '@/components/home/GatewayShowcase.vue'
import AgentToolCards from '@/components/home/AgentToolCards.vue'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { BRAND_NAME } from '@/config/brand'

const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

// Site settings - directly from appStore (already initialized from injected config)
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || BRAND_NAME)
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || t('home.heroSubtitle'))
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))

// Check if homeContent is a URL (for iframe display)
const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

// Theme
const isDark = ref(document.documentElement.classList.contains('dark'))


// Auth state
const isAuthenticated = computed(() => authStore.isAuthenticated)
const modelPlazaRequiresAuth = computed(
  () => appStore.cachedPublicSettings?.model_plaza_require_auth === true,
)
const showModelPlazaEntry = computed(
  () => modelPlazaEnabled.value && (isAuthenticated.value || !modelPlazaRequiresAuth.value),
)
const dashboardPath = computed(() => authStore.adminLandingPath || '/dashboard')
const userInitial = computed(() => {
  const user = authStore.user
  if (!user || !user.email) return ''
  return user.email.charAt(0).toUpperCase()
})

// Current year for footer
const currentYear = computed(() => new Date().getFullYear())

/**
 * 「洽询导入」次要连结：contact_info 是自由文字栏位，只有看起来像信箱时才生成 mailto，
 * 缺值或填了其他联络方式（群组、网址）就整个隐藏，不产生坏连结。
 */
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
const contactMailto = computed(() => {
  const raw = String(
    appStore.contactInfo || appStore.cachedPublicSettings?.contact_info || ''
  ).trim()
  return EMAIL_PATTERN.test(raw) ? `mailto:${raw}` : ''
})

/**
 * 区块编号取自「实际可见」的区块顺序：主视觉固定是 01，之后依序递补，不留空号。
 */
const visibleSectionIds = computed(() => ['hero', 'models', 'how', 'gateway', 'agents'])
function sectionNumber(id: string): string {
  return String(visibleSectionIds.value.indexOf(id) + 1).padStart(2, '0')
}

/** 「探索模型」滚到 02 模型与费率区块（该区常驻，显示模型广场的前六项）。 */
function scrollToModels() {
  document.getElementById('models')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

// 预设首页区块的共用排版类别
const sectionHeadClass = 'mb-10 max-w-3xl'
const kickerClass = 'mb-2.5 text-[13px] font-semibold tracking-[0.06em] text-primary-600 dark:text-primary-400'
const sectionTitleClass = 'text-3xl font-bold text-gray-900 lg:text-[36px] dark:text-white'
const sectionDescClass = 'mt-3.5 text-base text-gray-600 dark:text-dark-300'
const textLinkClass =
  'border-b border-primary-200 pb-px text-sm font-medium text-primary-700 hover:text-primary-600 dark:border-primary-800 dark:text-primary-400 dark:hover:text-primary-300'

// Toggle theme
function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

// Initialize theme
function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  ) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

onMounted(() => {
  initTheme()

  // Check auth state
  authStore.checkAuth()

  // Ensure public settings are loaded (will use cache if already loaded from injected config)
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>

<style scoped>
/* Terminal Container */
.terminal-container {
  position: relative;
  display: block;
  width: 100%;
  max-width: 420px;
}

/* Terminal Window */
.terminal-window {
  width: 100%;
  max-width: 100%;
  background: linear-gradient(145deg, #1e293b 0%, #0f172a 100%);
  border-radius: 14px;
  box-shadow:
    0 25px 50px -12px rgba(0, 0, 0, 0.4),
    0 0 0 1px rgba(255, 255, 255, 0.1),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
  overflow: hidden;
  transform: perspective(1000px) rotateX(2deg) rotateY(-2deg);
  transition: transform 0.3s ease;
}

.terminal-window:hover {
  transform: perspective(1000px) rotateX(0deg) rotateY(0deg) translateY(-4px);
}

/* Terminal Header */
.terminal-header {
  display: flex;
  align-items: center;
  padding: 12px 16px;
  background: rgba(30, 41, 59, 0.8);
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}

.terminal-buttons {
  display: flex;
  gap: 8px;
}

.terminal-buttons span {
  width: 12px;
  height: 12px;
  border-radius: 50%;
}

.btn-close {
  background: #ef4444;
}
.btn-minimize {
  background: #eab308;
}
.btn-maximize {
  background: #22c55e;
}

.terminal-title {
  flex: 1;
  text-align: center;
  font-size: 12px;
  font-family: ui-monospace, monospace;
  color: #64748b;
  margin-right: 52px;
}

/* Terminal Body */
.terminal-body {
  padding: 20px 24px;
  font-family: ui-monospace, 'Fira Code', monospace;
  font-size: 14px;
  line-height: 2;
}

.code-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  opacity: 0;
  animation: line-appear 0.5s ease forwards;
}

.line-1 {
  animation-delay: 0.3s;
}
.line-2 {
  animation-delay: 1s;
}
.line-3 {
  animation-delay: 1.8s;
}
.line-4 {
  animation-delay: 2.5s;
}

@keyframes line-appear {
  from {
    opacity: 0;
    transform: translateY(5px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.code-prompt {
  color: #22c55e;
  font-weight: bold;
}
.code-cmd {
  color: #38bdf8;
}
.code-flag {
  color: #a78bfa;
}
.code-url {
  color: #14b8a6;
}
.code-comment {
  color: #64748b;
  font-style: italic;
}
.code-success {
  color: #22c55e;
  background: rgba(34, 197, 94, 0.15);
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 600;
}
.code-response {
  color: #fbbf24;
}

/* Blinking Cursor */
.cursor {
  display: inline-block;
  width: 8px;
  height: 16px;
  background: #22c55e;
  animation: blink 1s step-end infinite;
}

@keyframes blink {
  0%,
  50% {
    opacity: 1;
  }
  51%,
  100% {
    opacity: 0;
  }
}

/* Dark mode adjustments */
:deep(.dark) .terminal-window {
  box-shadow:
    0 25px 50px -12px rgba(0, 0, 0, 0.6),
    0 0 0 1px rgba(20, 184, 166, 0.2),
    0 0 40px rgba(20, 184, 166, 0.1),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
}
</style>
