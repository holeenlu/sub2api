<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Icon from '@/shared/ui/icons/Icon.vue'

const props = defineProps<{ start: string; end: string; label: string }>()
const emit = defineEmits<{ change: [range: { start: string; end: string }] }>()
const open = ref(false)
const localStart = ref(props.start)
const localEnd = ref(props.end)
const localStartTime = ref('00:00')
const localEndTime = ref('23:59')
function splitValue(value: string, fallbackTime: string) {
  if (!value) return { date: '', time: fallbackTime }
  const match = /^(\d{4}-\d{2}-\d{2})(?:T(\d{2}:\d{2}))?/.exec(value)
  return { date: match?.[1] || '', time: match?.[2] || fallbackTime }
}
function syncFromProps() {
  const start = splitValue(props.start, '00:00')
  const end = splitValue(props.end, '23:59')
  localStart.value = start.date
  localEnd.value = end.date
  localStartTime.value = start.time
  localEndTime.value = end.time
}
watch(
  () => [props.start, props.end],
  syncFromProps,
  { immediate: true },
)
const presets = computed(() => [
  { key: 'today', label: 'vendor.dateToday', days: 0 },
  { key: 'yesterday', label: 'vendor.dateYesterday', days: 1 },
  { key: '7days', label: 'vendor.dateLast7Days', days: 6 },
  { key: '30days', label: 'vendor.dateLast30Days', days: 29 },
  { key: 'month', label: 'vendor.dateThisMonth', days: -1 },
  { key: 'lastMonth', label: 'vendor.dateLastMonth', days: -2 },
])
const display = computed(() => {
  if (!props.start && !props.end) return props.label
  const start = splitValue(props.start, '00:00')
  const end = splitValue(props.end, '23:59')
  return `${start.date || '—'} ${start.time} – ${end.date || '—'} ${end.time}`
})
function dateValue(value: Date) {
  return `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, '0')}-${String(value.getDate()).padStart(2, '0')}`
}
function choosePreset(key: string, days: number) {
  const now = new Date()
  if (key === 'month') {
    localStart.value = dateValue(new Date(now.getFullYear(), now.getMonth(), 1))
    localEnd.value = dateValue(now)
  } else if (key === 'lastMonth') {
    localStart.value = dateValue(new Date(now.getFullYear(), now.getMonth() - 1, 1))
    localEnd.value = dateValue(new Date(now.getFullYear(), now.getMonth(), 0))
  } else {
    const end = new Date(now)
    end.setDate(now.getDate() - (key === 'yesterday' ? 1 : 0))
    const start = new Date(end)
    start.setDate(end.getDate() - (key === 'yesterday' ? 0 : days))
    localStart.value = dateValue(start)
    localEnd.value = dateValue(end)
  }
  apply()
}
function apply() {
  emit('change', {
    start: localStart.value ? `${localStart.value}T${localStartTime.value}:00+08:00` : '',
    end: localEnd.value ? `${localEnd.value}T${localEndTime.value}:00+08:00` : '',
  })
  open.value = false
}
function clear() {
  localStart.value = ''
  localEnd.value = ''
  emit('change', { start: '', end: '' })
  open.value = false
}
</script>

<template>
  <div class="relative">
    <button
      type="button"
      class="btn btn-secondary btn-sm max-w-[260px] truncate"
      :aria-label="display"
      :aria-expanded="open"
      @click="open = !open"
    >
      <Icon name="calendar" size="sm" />{{ display }}
    </button>
    <div
      v-if="open"
      class="absolute right-0 z-50 mt-2 flex w-[min(600px,calc(100vw-2rem))] overflow-hidden rounded-xl border border-gray-200 bg-white shadow-xl dark:border-dark-600 dark:bg-dark-800"
    >
      <div class="w-32 shrink-0 border-r border-gray-100 p-2 dark:border-dark-700">
        <button
          v-for="preset in presets"
          :key="preset.key"
          type="button"
          class="block w-full rounded-lg px-3 py-2 text-left text-sm hover:bg-gray-50 dark:hover:bg-dark-700"
          @click="choosePreset(preset.key, preset.days)"
        >
          {{ $t(preset.label) }}
        </button>
      </div>
      <div class="min-w-0 flex-1 p-3">
        <div class="grid grid-cols-2 gap-3">
          <label class="text-xs text-gray-500">{{ $t('vendor.createdFrom') }}<input v-model="localStart" type="date" class="input mt-1 w-full" /></label>
          <label class="text-xs text-gray-500">{{ $t('vendor.createdTo') }}<input v-model="localEnd" type="date" class="input mt-1 w-full" /></label>
        </div>
        <div class="mt-3 grid grid-cols-2 gap-3">
          <label class="text-xs text-gray-500">{{ $t('vendor.startTime') }}<input v-model="localStartTime" type="time" class="input mt-1 w-full" /></label>
          <label class="text-xs text-gray-500">{{ $t('vendor.endTime') }}<input v-model="localEndTime" type="time" class="input mt-1 w-full" /></label>
        </div>
        <div class="mt-4 flex justify-end gap-2">
          <button type="button" class="btn btn-ghost btn-sm" @click="clear">{{ $t('vendor.clear') }}</button>
          <button
            type="button"
            class="btn btn-ghost btn-sm"
            @click="syncFromProps(); open = false"
          >{{ $t('vendor.cancel') }}</button>
          <button type="button" class="btn btn-primary btn-sm" @click="apply">{{ $t('vendor.confirm') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>
