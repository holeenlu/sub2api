<template>
          <!-- User Usage Trend (Full Width) -->
          <div class="card p-4">
            <div class="mb-4 flex items-center justify-between gap-3">
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.dashboard.recentUsage') }} (Top 12)</h3>
              <div class="flex gap-1" role="group" :aria-label="t('admin.dashboard.recentUsage')">
                <button v-for="metric in (['tokens', 'actual_cost'] as const)" :key="metric" type="button"
                  class="rounded px-2 py-1 text-xs" :class="userTrendMetric === metric ? 'bg-blue-600 text-white' : 'text-gray-600 dark:text-gray-300'"
                  :aria-pressed="userTrendMetric === metric" @click="emit('metric', metric)">
                  {{ t(metric === 'tokens' ? 'admin.dashboard.tokens' : 'admin.dashboard.actualSpending') }}
                </button>
              </div>
            </div>
            <div class="h-64">
              <div v-if="userTrendLoading" class="flex h-full items-center justify-center">
                <LoadingSpinner size="md" />
              </div>
              <Line v-else-if="userTrendChartData" :data="userTrendChartData" :options="lineOptions" />
              <div
                v-else
                class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400"
              >
                {{ t('admin.dashboard.noDataAvailable') }}
              </div>
            </div>
          </div>

</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler, type TooltipItem } from 'chart.js'
import { Line } from 'vue-chartjs'
import LoadingSpinner from '@/shared/ui/LoadingSpinner.vue'
import type { UserUsageTrendPoint } from '../api/types'
import { formatDashboardTokens as formatTokens, formatDashboardCost as formatCost } from '../model/dashboardFormat'
const props = defineProps<{ trend: UserUsageTrendPoint[]; metric: 'tokens' | 'actual_cost'; loading: boolean }>()
const emit = defineEmits<{ metric: [value: 'tokens' | 'actual_cost'] }>(), { t } = useI18n()
const userTrend = computed(() => props.trend), userTrendMetric = computed(() => props.metric), userTrendLoading = computed(() => props.loading)
ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)
const formatUserTrendValue = (value: number) => userTrendMetric.value === 'tokens' ? formatTokens(value) : '$' + formatCost(value)
// Dark mode detection
const isDarkMode = computed(() => {
  return document.documentElement.classList.contains('dark')
})

// Chart colors
const chartColors = computed(() => ({
  text: isDarkMode.value ? '#e5e7eb' : '#374151',
  grid: isDarkMode.value ? '#374151' : '#e5e7eb'
}))

// Line chart options (for user trend chart)
const lineOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    intersect: false,
    mode: 'index' as const
  },
  plugins: {
    legend: {
      position: 'top' as const,
      labels: {
        color: chartColors.value.text,
        usePointStyle: true,
        pointStyle: 'circle',
        padding: 15,
        font: {
          size: 11
        }
      }
    },
    tooltip: {
      itemSort: (a: TooltipItem<'line'>, b: TooltipItem<'line'>) => {
        const aValue = typeof a?.raw === 'number' ? a.raw : Number(a?.parsed?.y ?? 0)
        const bValue = typeof b?.raw === 'number' ? b.raw : Number(b?.parsed?.y ?? 0)
        return bValue - aValue
      },
      callbacks: {
        label: (context: TooltipItem<'line'>) => {
          return `${context.dataset.label}: ${formatUserTrendValue(Number(context.raw))}`
        }
      }
    }
  },
  scales: {
    x: {
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        }
      }
    },
    y: {
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        },
        callback: (value: string | number) => formatUserTrendValue(Number(value))
      }
    }
  }
}))

// User trend chart data
const userTrendChartData = computed(() => {
  if (!userTrend.value?.length) return null

  const getDisplayName = (point: UserUsageTrendPoint): string => {
    const username = point.username?.trim()
    if (username) {
      return username
    }

    const email = point.email?.trim()
    if (email) {
      return email
    }

    return t('admin.users.userPrefix', { id: point.user_id })
  }

  // Group by user_id to avoid merging different users with the same display name
  const userGroups = new Map<number, { name: string; data: Map<string, number | null> }>()
  const allDates = new Set<string>()

  userTrend.value.forEach((point) => {
    allDates.add(point.date)
    const key = point.user_id
    if (!userGroups.has(key)) {
      userGroups.set(key, { name: getDisplayName(point), data: new Map() })
    }
    const value = userTrendMetric.value === 'tokens' ? point.tokens : point.actual_cost
    userGroups.get(key)!.data.set(point.date, Number.isFinite(value) ? value : null)
  })

  const sortedDates = Array.from(allDates).sort()
  const colors = [
    '#3b82f6',
    '#10b981',
    '#f59e0b',
    '#ef4444',
    '#8b5cf6',
    '#ec4899',
    '#14b8a6',
    '#f97316',
    '#6366f1',
    '#84cc16',
    '#06b6d4',
    '#a855f7'
  ]

  const datasets = Array.from(userGroups.values()).map((group, idx) => ({
    label: group.name,
    data: sortedDates.map((date) => group.data.has(date) ? group.data.get(date) ?? null : 0),
    borderColor: colors[idx % colors.length],
    backgroundColor: `${colors[idx % colors.length]}20`,
    fill: false,
    tension: 0.3
  }))

  return {
    labels: sortedDates,
    datasets
  }
})


</script>
