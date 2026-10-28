<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ProxyQualityCheckResult } from '@/features/resources/api/proxyTypes';
import { formatDateTime } from '@/shared/lib/format'
import { proxyQualityStatusClass, proxyQualityStatusKey, proxyQualityTargetLabel } from '@/features/resources/model/proxyQuality'

defineProps<{ report: ProxyQualityCheckResult; name?: string }>()
const { t } = useI18n()
const messages: Record<string, string> = {
  'Target reachable': 'qualityReachable',
  'Target rate limited': 'qualityRateLimited',
  'Target requires verification': 'qualityVerification',
  'Target connection failed': 'qualityConnectionFailed'
}
const message = (value?: string) => value && messages[value] ? t('admin.proxies.' + messages[value]) : value || '—'
const latency = (value?: number) => value == null ? '—' : value + ' ms'
</script>

<template>
  <section class="space-y-4" data-proxy-quality-report>
    <div class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-700">
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <p class="break-words text-sm font-medium text-gray-700 dark:text-gray-200">{{ name || '—' }}</p>
          <p class="mt-2 text-xs leading-5 text-gray-600 dark:text-gray-300">
            {{ t('admin.proxies.qualitySummary', { passed: report.passed_count ?? '—', warn: report.warn_count ?? '—', failed: report.failed_count ?? '—', challenge: report.challenge_count ?? '—' }) }}
          </p>
        </div>
        <div class="shrink-0 text-right">
          <p class="text-3xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ report.score }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.proxies.qualityGrade', { grade: report.grade }) }}</p>
        </div>
      </div>
      <dl class="mt-4 grid gap-x-4 gap-y-2 text-xs text-gray-600 dark:text-gray-300 sm:grid-cols-2">
        <div><dt class="inline">{{ t('admin.proxies.qualityExitIP') }}: </dt><dd class="inline break-all">{{ report.exit_ip || '—' }}</dd></div>
        <div><dt class="inline">{{ t('admin.proxies.qualityCountry') }}: </dt><dd class="inline">{{ report.country || report.country_code || '—' }}</dd></div>
        <div><dt class="inline">{{ t('admin.proxies.qualityBaseLatency') }}: </dt><dd class="inline tabular-nums">{{ latency(report.base_latency_ms) }}</dd></div>
        <div><dt class="inline">{{ t('admin.proxies.qualityCheckedAt') }}: </dt><dd class="inline">{{ report.checked_at ? formatDateTime(new Date(report.checked_at * 1000)) : '—' }}</dd></div>
      </dl>
    </div>
    <div class="overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-600">
      <table class="w-full text-left text-sm" :aria-label="t('admin.proxies.qualityReportTitle')">
        <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400">
          <tr>
            <th class="whitespace-nowrap px-3 py-2 font-medium">{{ t('admin.proxies.qualityTableTarget') }}</th>
            <th class="whitespace-nowrap px-3 py-2 font-medium">{{ t('admin.proxies.qualityTableStatus') }}</th>
            <th class="px-3 py-2 font-medium">HTTP</th>
            <th class="whitespace-nowrap px-3 py-2 font-medium">{{ t('admin.proxies.qualityTableLatency') }}</th>
            <th class="min-w-40 px-3 py-2 font-medium">{{ t('admin.proxies.qualityTableMessage') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900">
          <tr v-for="item in report.items" :key="item.target">
            <td class="whitespace-nowrap px-3 py-3 text-gray-900 dark:text-gray-100">{{ proxyQualityTargetLabel(item.target, t) }}</td>
            <td class="px-3 py-3"><span class="badge whitespace-nowrap" :class="proxyQualityStatusClass(item.status)">{{ t(proxyQualityStatusKey(item.status)) }}</span></td>
            <td class="px-3 py-3 tabular-nums text-gray-600 dark:text-gray-300">{{ item.http_status ?? '—' }}</td>
            <td class="whitespace-nowrap px-3 py-3 tabular-nums text-gray-600 dark:text-gray-300">{{ latency(item.latency_ms) }}</td>
            <td class="break-words px-3 py-3 text-gray-600 dark:text-gray-300">
              {{ message(item.message) }}
              <span v-if="item.cf_ray" class="ml-1 text-xs text-gray-400">(cf-ray: {{ item.cf_ray }})</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
