<script setup lang="ts">
interface Props {
  fullscreen?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  fullscreen: false
})
</script>

<template>
  <div class="space-y-6">
    <!-- Header (matches OpsDashboardHeader + overview blocks) -->
    <div :class="['ops-skeleton-header rounded-3xl bg-white shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700', props.fullscreen ? 'p-8' : 'p-6']">
      <div class="flex flex-wrap items-center justify-between gap-4 border-b border-gray-100 pb-4 dark:border-dark-700">
        <div class="space-y-2">
          <div class="h-6 w-44 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
          <div class="h-3 w-80 max-w-full animate-pulse rounded bg-gray-100 dark:bg-dark-700/70"></div>
        </div>
        <div v-if="!props.fullscreen" class="flex flex-wrap items-center gap-3">
          <div class="h-9 w-[140px] animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700"></div>
          <div class="h-9 w-[160px] animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700"></div>
          <div class="h-9 w-[150px] animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700"></div>
          <div class="h-9 w-9 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700"></div>
          <div class="h-9 w-28 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700"></div>
          <div class="h-9 w-28 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700"></div>
          <div class="h-9 w-9 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700"></div>
        </div>
      </div>

      <div class="ops-skeleton-overview mt-4">
        <div class="ops-skeleton-realtime rounded-2xl bg-gray-50 p-4 dark:bg-dark-900/30">
          <div class="grid grid-cols-[76px_minmax(0,1fr)] items-center gap-3">
            <div class="h-20 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-700/70"></div>
            <div class="space-y-3">
              <div class="h-4 w-32 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
              <div class="grid grid-cols-2 gap-3">
                <div v-for="i in 2" :key="i" class="h-14 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-700/70"></div>
              </div>
            </div>
          </div>
        </div>

        <div v-for="i in 7" :key="i" class="h-40 animate-pulse rounded-2xl bg-gray-50 dark:bg-dark-900/30"></div>
      </div>
    </div>

    <!-- Row: Concurrency + Throughput (matches OpsDashboard.vue) -->
    <div class="grid grid-cols-1 gap-6 lg:grid-cols-4">
      <div :class="['min-h-[360px] rounded-3xl bg-white shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700 lg:col-span-1', props.fullscreen ? 'p-8' : 'p-6']">
        <div class="h-4 w-44 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        <div class="mt-6 h-72 animate-pulse rounded-2xl bg-gray-100 dark:bg-dark-700/70"></div>
      </div>
      <div :class="['min-h-[360px] rounded-3xl bg-white shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700 lg:col-span-1', props.fullscreen ? 'p-8' : 'p-6']">
        <div class="h-4 w-44 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        <div class="mt-6 h-72 animate-pulse rounded-2xl bg-gray-100 dark:bg-dark-700/70"></div>
      </div>
      <div :class="['min-h-[360px] rounded-3xl bg-white shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700 lg:col-span-2', props.fullscreen ? 'p-8' : 'p-6']">
        <div class="h-4 w-56 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        <div class="mt-6 h-72 animate-pulse rounded-2xl bg-gray-100 dark:bg-dark-700/70"></div>
      </div>
    </div>

    <!-- Row: Visual Analysis (baseline 3-up grid) -->
    <div class="grid grid-cols-1 gap-6 md:grid-cols-3">
      <div
        v-for="i in 3"
        :key="i"
        :class="['rounded-3xl bg-white shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700', props.fullscreen ? 'p-8' : 'p-6']"
      >
        <div class="h-4 w-44 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        <div class="mt-6 h-56 animate-pulse rounded-2xl bg-gray-100 dark:bg-dark-700/70"></div>
      </div>
    </div>

    <!-- Alert Events -->
    <div :class="['rounded-3xl bg-white shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700', props.fullscreen ? 'p-8' : 'p-6']">
      <div class="flex flex-wrap items-center justify-between gap-4">
        <div class="h-4 w-48 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        <div v-if="!props.fullscreen" class="flex flex-wrap items-center gap-2">
          <div class="h-9 w-[140px] animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700"></div>
          <div class="h-9 w-[120px] animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700"></div>
          <div class="h-9 w-[120px] animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700"></div>
        </div>
      </div>

      <div class="mt-6 space-y-3">
        <div v-for="i in 6" :key="i" class="flex items-center justify-between gap-4 rounded-2xl bg-gray-50 p-4 dark:bg-dark-900/30">
          <div class="flex-1 space-y-2">
            <div class="h-3 w-56 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
            <div class="h-3 w-80 max-w-full animate-pulse rounded bg-gray-100 dark:bg-dark-700/70"></div>
          </div>
          <div class="h-7 w-20 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700"></div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ops-skeleton-header { container-type: inline-size; }
.ops-skeleton-overview { display: grid; grid-template-columns: minmax(0, 1fr); gap: 1rem; }
@container (min-width: 640px) {
  .ops-skeleton-overview { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .ops-skeleton-realtime { grid-column: 1 / -1; }
}
@container (min-width: 960px) {
  .ops-skeleton-overview { grid-template-columns: repeat(5, minmax(0, 1fr)); }
  .ops-skeleton-realtime { grid-column: span 3; }
}
</style>
