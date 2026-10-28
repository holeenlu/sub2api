<template>

    <div class="space-y-6">
      <div v-if="loading" class="flex items-center justify-center py-12"><LoadingSpinner /></div>
      <template v-else-if="stats">
        <UserDashboardStats :stats="stats" :balance="user?.balance ?? null" :is-simple="isSimple" :platform-quotas="platformQuotas" />
        <UserDashboardCharts v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="loadingCharts" :trend="trendData" :models="modelStats" @dateRangeChange="loadCharts" @granularityChange="loadCharts" @refresh="refreshAll" />
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
          <div class="lg:col-span-2"><UserDashboardRecentUsage :data="recentUsage" :loading="loadingUsage" /></div>
          <div class="lg:col-span-1"><UserDashboardQuickActions :can-use-batch-image="canUseBatchImage" /></div>
        </div>
      </template>
    </div>

</template>

<script setup lang="ts">
import { computed, ref, watch, onMounted, onScopeDispose } from 'vue'
import { useUserDashboardData } from '@/features/usage'
import { UserDashboardCharts, UserDashboardRecentUsage } from '@/features/usage/panels'
import LoadingSpinner from '@/shared/ui/LoadingSpinner.vue'
import UserDashboardStats from '../components/UserDashboardStats.vue'
import UserDashboardQuickActions from '../components/UserDashboardQuickActions.vue'
import { useDashboardContext } from '../context'
const context = useDashboardContext(), { user, isSimple, canUseBatchImage } = context
const data = useUserDashboardData()
const { stats, loadingUsage, loadingCharts, trendData, modelStats, recentUsage, platformQuotas, startDate, endDate, granularity, loadCharts } = data
const profileLoading = ref(false), loading = computed(() => profileLoading.value || data.loading.value)
let refreshVersion = 0
async function refreshProfileAndStats(): Promise<void> {
  const version = ++refreshVersion, scope = context.scopeKey.value
  if (scope === null) return
  profileLoading.value = true
  try { await context.refreshUser(); if (version === refreshVersion && scope === context.scopeKey.value) await data.loadStats() }
  catch { /* Identity owns refresh recovery; no balance is inferred. */ }
  finally { if (version === refreshVersion && scope === context.scopeKey.value) profileLoading.value = false }
}
function refreshAll(): void { void refreshProfileAndStats(); void data.loadCharts(); void data.loadRecent(); void data.loadPlatformQuotas() }
watch(context.scopeKey, () => { refreshVersion++; profileLoading.value = false; if (context.scopeKey.value !== null) refreshAll() }, { flush: 'sync' })
onScopeDispose(() => { refreshVersion++ })
onMounted(() => { refreshAll(); void context.refreshBatchImageAccess() })
</script>
