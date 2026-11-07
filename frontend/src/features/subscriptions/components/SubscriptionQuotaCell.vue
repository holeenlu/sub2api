<template>

            <div class="min-w-[280px] space-y-2">
              <!-- Daily Usage -->
              <div v-if="subscription.group?.daily_limit_usd" class="usage-row">
                <div class="flex items-center gap-2">
                  <span class="usage-label">{{ t('admin.subscriptions.daily') }}</span>
                  <div class="h-1.5 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="h-1.5 rounded-full transition-all"
                      :class="getProgressClass(subscription.daily_usage_usd, subscription.group?.daily_limit_usd)"
                      :style="{
                        width: getProgressWidth(subscription.daily_usage_usd, subscription.group?.daily_limit_usd)
                      }"
                    ></div>
                  </div>
                  <span class="usage-amount">
                    {{ formatUsage(subscription.daily_usage_usd) }}
                    <span class="text-gray-400">/</span>
                    ${{ subscription.group?.daily_limit_usd?.toFixed(2) }}
                  </span>
                </div>
                <div class="reset-info" v-if="subscription.daily_window_start">
                  <svg
                    class="h-3 w-3"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                  <span>{{ formatDailyUsageWindow(subscription) }}</span>
                </div>
              </div>

              <!-- Weekly Usage -->
              <div v-if="subscription.group?.weekly_limit_usd" class="usage-row">
                <div class="flex items-center gap-2">
                  <span class="usage-label">{{ t('admin.subscriptions.weekly') }}</span>
                  <div class="h-1.5 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="h-1.5 rounded-full transition-all"
                      :class="getProgressClass(subscription.weekly_usage_usd, subscription.group?.weekly_limit_usd)"
                      :style="{
                        width: getProgressWidth(subscription.weekly_usage_usd, subscription.group?.weekly_limit_usd)
                      }"
                    ></div>
                  </div>
                  <span class="usage-amount">
                    {{ formatUsage(subscription.weekly_usage_usd) }}
                    <span class="text-gray-400">/</span>
                    ${{ subscription.group?.weekly_limit_usd?.toFixed(2) }}
                  </span>
                </div>
                <div class="reset-info" v-if="subscription.weekly_window_start">
                  <svg
                    class="h-3 w-3"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                  <span>{{ formatResetTime(subscription.weekly_window_start, 'weekly') }}</span>
                </div>
              </div>

              <!-- Monthly Usage -->
              <div v-if="subscription.group?.monthly_limit_usd" class="usage-row">
                <div class="flex items-center gap-2">
                  <span class="usage-label">{{ t('admin.subscriptions.monthly') }}</span>
                  <div class="h-1.5 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="h-1.5 rounded-full transition-all"
                      :class="getProgressClass(subscription.monthly_usage_usd, subscription.group?.monthly_limit_usd)"
                      :style="{
                        width: getProgressWidth(subscription.monthly_usage_usd, subscription.group?.monthly_limit_usd)
                      }"
                    ></div>
                  </div>
                  <span class="usage-amount">
                    {{ formatUsage(subscription.monthly_usage_usd) }}
                    <span class="text-gray-400">/</span>
                    ${{ subscription.group?.monthly_limit_usd?.toFixed(2) }}
                  </span>
                </div>
                <div class="reset-info" v-if="subscription.monthly_window_start">
                  <svg
                    class="h-3 w-3"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                  <span>{{ formatResetTime(subscription.monthly_window_start, 'monthly') }}</span>
                </div>
              </div>

              <!-- No Limits - Unlimited badge -->
              <div
                v-if="
                  !subscription.group?.daily_limit_usd &&
                  !subscription.group?.weekly_limit_usd &&
                  !subscription.group?.monthly_limit_usd
                "
                class="flex items-center gap-2 rounded-lg bg-gradient-to-r from-emerald-50 to-teal-50 px-3 py-2 dark:from-emerald-900/20 dark:to-teal-900/20"
              >
                <span class="text-lg text-emerald-600 dark:text-emerald-400">∞</span>
                <span class="text-xs font-medium text-emerald-700 dark:text-emerald-300">
                  {{ t('admin.subscriptions.unlimited') }}
                </span>
              </div>
            </div>
          </template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { UserSubscription } from '../api/types'
import { getRemainingDurationParts, isOneTimeDailyQuota, type RemainingDurationParts } from '@/features/subscriptions/model/subscriptionQuota'
defineProps<{ subscription: UserSubscription }>()
const { t } = useI18n()
function formatUsage(value: number | null | undefined) { return typeof value === 'number' && Number.isFinite(value) ? '$' + value.toFixed(2) : t('common.unknown') }
const getProgressWidth = (used: number | null | undefined, limit: number | null): string => {
  if (!limit || limit === 0) return '0%'
  if (used == null || !Number.isFinite(used)) return '0%'
  const percentage = Math.min((used / limit) * 100, 100)
  return `${percentage}%`
}

const getProgressClass = (used: number | null | undefined, limit: number | null): string => {
  if (!limit || limit === 0) return 'bg-gray-400'
  if (used == null || !Number.isFinite(used)) return 'bg-gray-400'
  const percentage = (used / limit) * 100
  if (percentage >= 90) return 'bg-red-500'
  if (percentage >= 70) return 'bg-orange-500'
  return 'bg-green-500'
}

const formatResetDuration = (parts: RemainingDurationParts): string => {
  if (parts.days > 0) {
    return t('admin.subscriptions.resetInDaysHours', { days: parts.days, hours: parts.hours })
  }

  if (parts.hours > 0) {
    return t('admin.subscriptions.resetInHoursMinutes', { hours: parts.hours, minutes: parts.minutes })
  }

  return t('admin.subscriptions.resetInMinutes', { minutes: parts.minutes })
}

const formatQuotaEndDuration = (parts: RemainingDurationParts): string => {
  if (parts.days > 0) {
    return t('admin.subscriptions.quotaEndsInDaysHours', { days: parts.days, hours: parts.hours })
  }

  if (parts.hours > 0) {
    return t('admin.subscriptions.quotaEndsInHoursMinutes', { hours: parts.hours, minutes: parts.minutes })
  }

  return t('admin.subscriptions.quotaEndsInMinutes', { minutes: parts.minutes })
}

const formatDailyUsageWindow = (subscription: UserSubscription): string => {
  if (isOneTimeDailyQuota(subscription) && subscription.expires_at) {
    const parts = getRemainingDurationParts(subscription.expires_at)
    return parts ? formatQuotaEndDuration(parts) : t('admin.subscriptions.windowNotActive')
  }

  return formatResetTime(subscription.daily_window_start, 'daily')
}

// Format reset time based on window start and period type
const formatResetTime = (windowStart: string | null, period: 'daily' | 'weekly' | 'monthly'): string => {
  if (!windowStart) return t('admin.subscriptions.windowNotActive')

  const start = new Date(windowStart)
  const now = new Date()

  // Calculate reset time based on period
  let resetTime: Date
  switch (period) {
    case 'daily':
      resetTime = new Date(start.getTime() + 24 * 60 * 60 * 1000)
      break
    case 'weekly':
      resetTime = new Date(start.getTime() + 7 * 24 * 60 * 60 * 1000)
      break
    case 'monthly':
      resetTime = new Date(start.getTime() + 30 * 24 * 60 * 60 * 1000)
      break
  }

  const parts = getRemainingDurationParts(resetTime, now)

  return parts ? formatResetDuration(parts) : t('admin.subscriptions.windowNotActive')
}

// Handle click outside to close dropdowns
</script>
