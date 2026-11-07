<script setup lang="ts">
import { computed, ref } from 'vue'
import { useMutationObserver } from '@vueuse/core'
import { formatUsageCost, formatUsageNumber } from '../helpers'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Filler,
  type ChartOptions
} from 'chart.js'
import { Line } from 'vue-chartjs'
import type { UsageRow } from '../types'
ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Filler)
const props = defineProps<{
  rows: UsageRow[]
  metric: 'requests' | 'total_tokens' | 'cost_usd'
  label: string
}>()
const dark = ref(document.documentElement.classList.contains('dark'))
useMutationObserver(document.documentElement, () => {
  dark.value = document.documentElement.classList.contains('dark')
}, { attributes: true, attributeFilter: ['class'] })
const data = computed(() => ({
  labels: props.rows.map((row) => row.date || ''),
  datasets: [
    {
      label: props.label,
      data: props.rows.map((row) => {
        const raw = row[props.metric]
        return raw == null || !Number.isFinite(Number(raw)) ? null : Number(raw)
      }),
      borderColor: dark.value ? '#4db8c4' : '#0891a0',
      backgroundColor: dark.value ? 'rgba(77,184,196,0.12)' : 'rgba(8,145,160,0.08)',
      fill: true,
      borderWidth: 2,
      pointRadius: props.rows.length === 1 ? 4 : 0,
      pointHoverRadius: 4,
      pointHitRadius: 12,
      tension: 0,
      spanGaps: false
    }
  ]
}))
const options = computed<ChartOptions<'line'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { intersect: false, mode: 'index' },
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (context) => `${props.label}: ${props.metric === 'cost_usd'
          ? formatUsageCost(props.rows[context.dataIndex]?.cost_usd)
          : formatUsageNumber(context.parsed.y)}`
      }
    }
  },
  scales: {
    x: { grid: { display: false }, border: { display: false }, ticks: { color: dark.value ? '#9ca3af' : '#6b7280', maxRotation: 0, maxTicksLimit: 8 } },
    y: {
      beginAtZero: true, border: { display: false }, grid: { color: dark.value ? '#374151' : '#edf0f4' }, ticks: {
        color: dark.value ? '#9ca3af' : '#6b7280',
        precision: props.metric === 'cost_usd' ? undefined : 0,
        maxTicksLimit: 6,
        callback: value => (props.metric === 'cost_usd' ? '$' : '') + Intl.NumberFormat('en-US', { notation: 'compact' }).format(Number(value))
      }
    }
  }
}))
</script>
<template>
  <div class="h-56">
    <Line :data="data" :options="options" role="img" :aria-label="label" />
  </div>
</template>
