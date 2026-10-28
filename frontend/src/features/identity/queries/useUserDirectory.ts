import { computed, onScopeDispose, reactive, ref, watch } from 'vue'
import { isCancel } from 'axios'
import { useI18n } from 'vue-i18n'
import { usersAPI } from '../api/users'
import { userAttributesAPI } from '../api/attributes'
import { useAuthStore } from '../model/auth'
import { useIdentityNotifications } from '../ports'
import { useIdentityUserDirectoryData } from '../userPorts'
import type { AdminUser } from '../api/profileTypes'
import type { UserAttributeDefinition } from '../api/attributeTypes'
import type { BatchUserUsageStats } from '@/features/usage/adminDashboard'
import type { PlatformQuotaItem } from '@/features/access-policy/api/platformQuotaTypes';

export interface UserDirectoryQueryOptions {
  pageSize: number
  filters(): NonNullable<Parameters<typeof usersAPI.list>[2]>
  needsUsage(): boolean
  needsAttributes(): boolean
  needsPlatformQuotas(): boolean
}

/** Owns list/enrichment requests and their session lifetime, independently of table preferences. */
export function useUserDirectory(options: UserDirectoryQueryOptions) {
  const auth = useAuthStore()
  const data = useIdentityUserDirectoryData()
  const notifications = useIdentityNotifications()
  const { t } = useI18n()
  const users = ref<AdminUser[]>([])
  const loading = ref(false)
  const pagination = reactive({ page: 1, page_size: options.pageSize, total: 0, pages: 0 })
  const attributeDefinitions = ref<UserAttributeDefinition[]>([])
  const userAttributeValues = ref<Record<number, Record<number, string>>>({})
  const usageStats = ref<Record<string, BatchUserUsageStats>>({})
  const platformQuotaStats = ref<Record<number, PlatformQuotaItem[]>>({})
  const scope = computed(() => auth.identityScopeKey)
  let active = true
  let requestSequence = 0
  let secondarySequence = 0
  let controller: AbortController | null = null
  let secondaryTimer: ReturnType<typeof setTimeout> | null = null

  function stopRequests(): void {
    requestSequence += 1
    secondarySequence += 1
    controller?.abort()
    if (secondaryTimer !== null) clearTimeout(secondaryTimer)
    secondaryTimer = null
    loading.value = false
  }

  function clearRows(): void {
    users.value = []
    usageStats.value = {}
    userAttributeValues.value = {}
    platformQuotaStats.value = {}
    pagination.total = 0
    pagination.pages = 0
  }

  async function loadAttributeDefinitions(): Promise<void> {
    const expectedScope = scope.value
    try {
      const definitions = await userAttributesAPI.listEnabledDefinitions()
      if (active && expectedScope === scope.value) attributeDefinitions.value = definitions
    } catch (error) {
      if (active && expectedScope === scope.value && !isCancel(error)) console.error('Failed to load attribute definitions:', error)
    }
  }

  async function loadSecondary(userIds: number[], request: number, signal?: AbortSignal): Promise<void> {
    const expectedScope = scope.value
    const secondary = ++secondarySequence
    const current = () => active && !signal?.aborted && expectedScope === scope.value &&
      request === requestSequence && secondary === secondarySequence
    const tasks: Promise<void>[] = []
    if (options.needsUsage() && auth.can('usage.read')) {
      tasks.push((async () => {
        try {
          const response = await data.loadUsage(userIds)
          if (current() && auth.can('usage.read')) usageStats.value = response.stats
        } catch (error) {
          if (current() && !isCancel(error)) console.error('Failed to load usage stats:', error)
        }
      })())
    }
    if (attributeDefinitions.value.length && options.needsAttributes()) {
      tasks.push((async () => {
        try {
          const response = await userAttributesAPI.getBatchUserAttributes(userIds)
          if (current()) userAttributeValues.value = response.attributes
        } catch (error) {
          if (current() && !isCancel(error)) console.error('Failed to load user attribute values:', error)
        }
      })())
    }
    if (options.needsPlatformQuotas()) {
      tasks.push((async () => {
        for (let offset = 0; offset < userIds.length; offset += 6) {
          if (!current()) return
          const chunk = userIds.slice(offset, offset + 6)
          const results = await Promise.allSettled(chunk.map(id => data.loadPlatformQuotas(id)))
          if (!current()) return
          const merged = { ...platformQuotaStats.value }
          results.forEach((result, index) => {
            if (result.status === 'fulfilled') merged[chunk[index]] = result.value.platform_quotas || []
          })
          platformQuotaStats.value = merged
        }
      })())
    }
    await Promise.allSettled(tasks)
  }

  async function loadUsers(): Promise<void> {
    if (!active) return
    stopRequests()
    const request = requestSequence
    const expectedScope = scope.value
    const requestController = new AbortController()
    controller = requestController
    const current = () => active && !requestController.signal.aborted &&
      request === requestSequence && expectedScope === scope.value
    loading.value = true
    try {
      // The subscription summary is also used by the external quota dialog when its column is hidden.
      const response = await usersAPI.list(pagination.page, pagination.page_size,
        { ...options.filters(), include_subscriptions: true }, { signal: requestController.signal })
      if (!current()) return
      users.value = response.items
      pagination.total = response.total
      pagination.pages = response.pages
      usageStats.value = {}
      userAttributeValues.value = {}
      platformQuotaStats.value = {}
      if (response.items.length) {
        secondaryTimer = setTimeout(() => {
          secondaryTimer = null
          if (current()) loadSecondary(response.items.map(user => user.id), request, requestController.signal).catch(error => {
            if (current() && !isCancel(error)) console.error('Failed to load user enrichment:', error)
          })
        }, 50)
      }
    } catch (error: unknown) {
      if (!current() || isCancel(error) || (error as { name?: string })?.name === 'AbortError') return
      const failure = error as { message?: string; response?: { data?: { detail?: string } } }
      notifications.showError(failure.response?.data?.detail || failure.message || t('admin.users.failedToLoad'))
    } finally {
      if (current()) loading.value = false
    }
  }

  async function refreshCurrentPageSecondaryData(): Promise<void> {
    const ids = users.value.map(user => user.id)
    if (active && ids.length) await loadSecondary(ids, requestSequence, controller?.signal)
  }

  watch(scope, async () => {
    const expectedScope = scope.value
    stopRequests()
    clearRows()
    attributeDefinitions.value = []
    if (auth.isAuthenticated) {
      await loadAttributeDefinitions()
      if (active && expectedScope === scope.value && auth.isAuthenticated) await loadUsers()
    }
  }, { flush: 'sync' })
  onScopeDispose(() => { active = false; stopRequests(); clearRows() })

  return { users, loading, pagination, attributeDefinitions, userAttributeValues, usageStats,
    platformQuotaStats, loadUsers, loadAttributeDefinitions, refreshCurrentPageSecondaryData }
}
