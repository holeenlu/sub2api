<template>
          <!-- Ring Cards Grid -->
          <div v-if="items.length > 0" :class="ringGridClass">
            <div
              v-for="(ring, i) in items"
              :key="i"
              class="fade-up rounded-2xl border border-gray-200 bg-white/90 p-8 backdrop-blur-sm transition-all duration-300 hover:shadow-lg dark:border-dark-700 dark:bg-dark-900/90"
              :class="`fade-up-delay-${Math.min(i + 1, 4)}`"
            >
              <div class="flex items-center justify-between mb-6">
                <h3 class="text-sm font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400">
                  {{ ring.title }}
                </h3>
                <!-- Clock icon -->
                <svg v-if="ring.iconType === 'clock'" class="w-5 h-5 text-gray-400 dark:text-dark-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>
                </svg>
                <!-- Calendar icon -->
                <svg v-else-if="ring.iconType === 'calendar'" class="w-5 h-5 text-gray-400 dark:text-dark-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <rect x="3" y="4" width="18" height="18" rx="2" ry="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/>
                </svg>
                <!-- Dollar icon -->
                <svg v-else class="w-5 h-5 text-gray-400 dark:text-dark-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <line x1="12" y1="1" x2="12" y2="23"/><path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/>
                </svg>
              </div>
              <div class="flex justify-center">
                <div class="relative">
                  <svg class="w-44 h-44" viewBox="0 0 160 160">
                    <circle cx="80" cy="80" r="68" fill="none" :stroke="ringTrackColor" stroke-width="10"/>
                    <circle
                      class="progress-ring"
                      cx="80" cy="80" r="68" fill="none"
                      :stroke="`url(#ring-grad-${i})`"
                      stroke-width="10" stroke-linecap="round"
                      :stroke-dasharray="CIRCUMFERENCE.toFixed(2)"
                      :stroke-dashoffset="getRingOffset(ring)"
                    />
                    <defs>
                      <linearGradient :id="`ring-grad-${i}`" x1="0%" y1="0%" x2="100%" y2="100%">
                        <stop offset="0%" :stop-color="RING_GRADIENTS[i % 4].from"/>
                        <stop offset="100%" :stop-color="RING_GRADIENTS[i % 4].to"/>
                      </linearGradient>
                    </defs>
                  </svg>
                  <div class="absolute inset-0 flex flex-col items-center justify-center">
                    <template v-if="ring.isBalance">
                      <span class="text-2xl font-bold tabular-nums" :style="{ color: RING_GRADIENTS[i % 4].from }">
                        {{ ring.amount }}
                      </span>
                    </template>
                    <template v-else>
                      <span class="text-3xl font-bold tabular-nums text-gray-900 dark:text-white">
                        {{ displayPcts[i] ?? 0 }}%
                      </span>
                      <span class="text-xs text-gray-500 dark:text-dark-400 mt-0.5">{{ t('keyUsage.used') }}</span>
                      <span
                        class="text-sm font-semibold mt-1 tabular-nums"
                        :style="{ color: RING_GRADIENTS[i % 4].from }"
                      >{{ ring.amount }}</span>
                      <p v-if="ring.resetAt && formatResetTime(ring.resetAt)" class="text-xs text-gray-400 dark:text-gray-500 mt-0.5 tabular-nums">
                        ⟳ {{ formatResetTime(ring.resetAt) }}
                      </p>
                    </template>
                  </div>
                </div>
              </div>
            </div>
          </div>


</template>
<script setup lang="ts">
import { computed, ref, watch, onScopeDispose, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { type PublicUsageRing } from '../model/publicKeyUsageFormat'
const props = defineProps<{ items: PublicUsageRing[]; isDark: boolean; formatResetTime: (value: string | null | undefined) => string }>()
const { t } = useI18n()
const CIRCUMFERENCE = 2 * Math.PI * 68
const RING_GRADIENTS = [{ from: '#14b8a6', to: '#5eead4' }, { from: '#6366F1', to: '#A5B4FC' }, { from: '#10B981', to: '#6EE7B7' }, { from: '#F59E0B', to: '#FCD34D' }]
const ringAnimated = ref(false), displayPcts = ref<number[]>([])
const ringTrackColor = computed(() => props.isDark ? '#222222' : '#F0F0EE')
const ringGridClass = computed(() => props.items.length === 1 ? 'grid grid-cols-1 max-w-md mx-auto gap-6' : props.items.length === 2 ? 'grid grid-cols-1 md:grid-cols-2 gap-6' : 'grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6')
function getRingOffset(ring: PublicUsageRing): number { return !ringAnimated.value ? CIRCUMFERENCE : ring.isBalance ? 0 : CIRCUMFERENCE - Math.min(ring.pct, 100) / 100 * CIRCUMFERENCE }
let version = 0, disposed = false, frame: number | undefined, timer: ReturnType<typeof setTimeout> | undefined
function cancel(): void { version++; if (frame !== undefined) cancelAnimationFrame(frame); clearTimeout(timer) }
watch(() => props.items, async items => {
  cancel(); const id = version
  ringAnimated.value = false; displayPcts.value = items.map(() => 0)
  await nextTick()
  if (disposed || id !== version) return
  frame = requestAnimationFrame(() => {
    if (disposed || id !== version) return
    timer = setTimeout(() => {
      if (disposed || id !== version) return
      ringAnimated.value = true
      const start = performance.now(), targets = items.map(item => item.isBalance ? 0 : item.pct)
      function tick() {
        if (disposed || id !== version) return
        const progress = Math.min((performance.now() - start) / 1000, 1), ease = 1 - Math.pow(1 - progress, 3)
        displayPcts.value = targets.map(target => Math.round(ease * target))
        if (progress < 1) frame = requestAnimationFrame(tick)
      }
      frame = requestAnimationFrame(tick)
    }, 50)
  })
}, { immediate: true })
onScopeDispose(() => { disposed = true; cancel() })
</script>
