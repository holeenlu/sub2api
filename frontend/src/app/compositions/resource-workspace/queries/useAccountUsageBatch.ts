import { ref, watch, onScopeDispose, type Ref } from 'vue'
import type { Account } from '@/features/resources/api/accountTypes';
import type { AccountUsageInfo } from '@/features/resources/api/accountUsageTypes';
import type { accountsAPI } from '@/features/resources'

type UsageAccount = Pick<Account, 'id' | 'platform' | 'type'>

/** Batch only the active desktop view; requests and cache belong to its identity scope. */
export function useAccountUsageBatch({ isDesktopViewport, readScope, fetchUsage }: {
  isDesktopViewport: Readonly<Ref<boolean>>
  readScope: () => string
  fetchUsage: typeof accountsAPI.getBatchUsage
}) {
  let active = true
  let generation = 0
  const usageBatchByAccountId = ref<Record<string, AccountUsageInfo | null>>({})
  const usageBatchErrorByAccountId = ref<Record<string, string | null>>({})
  const usageBatchLoadingByAccountId = ref<Record<string, boolean>>({})
  const usageBatchRequestTokenByAccountId = ref<Record<string, number>>({})
  const usageBatchCache = new Map<number, { data: AccountUsageInfo; ts: number }>()
  const USAGE_BATCH_CACHE_TTL = 5 * 60 * 1000
  const pendingUsageBatchIds = new Set<number>()
  let usageBatchFlushTimer: ReturnType<typeof setTimeout> | null = null
  let queuedUsageBatchForce = false
  let usageBatchRequestToken = 0

  const accountSupportsBatchUsage = (account: UsageAccount) => {
    if (account.platform === 'anthropic') {
      return account.type === 'oauth' || account.type === 'setup-token'
    }
    if (account.platform === 'gemini') return true
    if (account.platform === 'antigravity') return account.type === 'oauth'
    if (account.platform === 'openai') return account.type === 'oauth'
    if (account.platform === 'grok') return account.type === 'oauth'
    return false
  }

  const setUsageBatchLoading = (accountID: number, loadingState: boolean) => {
    usageBatchLoadingByAccountId.value = {
      ...usageBatchLoadingByAccountId.value,
      [String(accountID)]: loadingState
    }
  }

  const setUsageBatchState = (accountID: number, usage: AccountUsageInfo | null, error: string | null) => {
    const key = String(accountID)
    usageBatchByAccountId.value = {
      ...usageBatchByAccountId.value,
      [key]: usage
    }
    usageBatchErrorByAccountId.value = {
      ...usageBatchErrorByAccountId.value,
      [key]: error
    }
  }

  const handleAccountUsageLoaded = (accountID: number, usage: AccountUsageInfo) => {
    if (!active || usageBatchByAccountId.value[String(accountID)] === usage) return
    // A direct query/reset result supersedes any older desktop batch and cache.
    usageBatchRequestTokenByAccountId.value = {
      ...usageBatchRequestTokenByAccountId.value,
      [String(accountID)]: ++usageBatchRequestToken
    }
    pendingUsageBatchIds.delete(accountID)
    usageBatchCache.set(accountID, { data: usage, ts: Date.now() })
    setUsageBatchLoading(accountID, false)
    setUsageBatchState(accountID, usage, null)
  }

  const flushQueuedUsageBatch = async () => {
    usageBatchFlushTimer = null
    if (!active) return
    const version = generation
    const scope = readScope()
    const current = () => active && version === generation && scope === readScope()
    const accountIDs = Array.from(pendingUsageBatchIds)
    const force = queuedUsageBatchForce
    pendingUsageBatchIds.clear()
    queuedUsageBatchForce = false

    if (accountIDs.length === 0) return

    const requestTokensByAccount = accountIDs.reduce<Record<string, number>>((acc, accountID) => {
      acc[String(accountID)] = usageBatchRequestTokenByAccountId.value[String(accountID)] ?? 0
      return acc
    }, {})

    try {
      const result = await fetchUsage(accountIDs, force)

      if (!current()) return
      const usageMap = result.usage ?? {}
      const errorMap = result.errors ?? {}
      const now = Date.now()
      const nextUsage = { ...usageBatchByAccountId.value }
      const nextErrors = { ...usageBatchErrorByAccountId.value }
      const nextLoading = { ...usageBatchLoadingByAccountId.value }

      for (const accountID of accountIDs) {
        const key = String(accountID)
        if ((usageBatchRequestTokenByAccountId.value[key] ?? 0) !== requestTokensByAccount[key]) {
          continue
        }
        const usage = usageMap[key] ?? null
        nextUsage[key] = usage
        nextErrors[key] = errorMap[key] ?? null
        nextLoading[key] = false
        if (usage) {
          usageBatchCache.set(accountID, { data: usage, ts: now })
        } else {
          usageBatchCache.delete(accountID)
        }
      }

      usageBatchByAccountId.value = nextUsage
      usageBatchErrorByAccountId.value = nextErrors
      usageBatchLoadingByAccountId.value = nextLoading
    } catch (error) {
      if (!current()) return
      const nextErrors = { ...usageBatchErrorByAccountId.value }
      const nextLoading = { ...usageBatchLoadingByAccountId.value }
      for (const accountID of accountIDs) {
        const key = String(accountID)
        if ((usageBatchRequestTokenByAccountId.value[key] ?? 0) !== requestTokensByAccount[key]) {
          continue
        }
        nextErrors[key] = 'Failed'
        nextLoading[key] = false
      }
      usageBatchErrorByAccountId.value = nextErrors
      usageBatchLoadingByAccountId.value = nextLoading
      console.error('Failed to load account usage batch:', error)
    }
  }

  const queueBatchedUsage = (account: UsageAccount, options?: { force?: boolean }) => {
    if (!active || !isDesktopViewport.value) return
    if (!accountSupportsBatchUsage(account)) return

    const force = options?.force === true
    const cacheKey = account.id
    const key = String(cacheKey)

    if (force) {
      usageBatchCache.delete(cacheKey)
    } else {
      const cached = usageBatchCache.get(cacheKey)
      if (cached && Date.now() - cached.ts < USAGE_BATCH_CACHE_TTL) {
        setUsageBatchState(cacheKey, cached.data, null)
        setUsageBatchLoading(cacheKey, false)
        return
      }
    }

    usageBatchErrorByAccountId.value = {
      ...usageBatchErrorByAccountId.value,
      [key]: null
    }
    usageBatchRequestTokenByAccountId.value = {
      ...usageBatchRequestTokenByAccountId.value,
      [key]: ++usageBatchRequestToken
    }
    setUsageBatchLoading(cacheKey, true)
    pendingUsageBatchIds.add(cacheKey)
    queuedUsageBatchForce = queuedUsageBatchForce || force

    if (usageBatchFlushTimer !== null) return
    usageBatchFlushTimer = setTimeout(() => {
      void flushQueuedUsageBatch()
    }, 0)
  }


  function reset() {
    generation++
    if (usageBatchFlushTimer !== null) clearTimeout(usageBatchFlushTimer)
    usageBatchFlushTimer = null
    pendingUsageBatchIds.clear()
    usageBatchCache.clear()
    queuedUsageBatchForce = false
    usageBatchByAccountId.value = {}
    usageBatchErrorByAccountId.value = {}
    usageBatchLoadingByAccountId.value = {}
    usageBatchRequestTokenByAccountId.value = {}
  }
  watch(readScope, reset, { flush: 'sync' })
  onScopeDispose(() => { active = false; reset() })
  function retainVisibleUsage(ids: number[]) {
    const visibleIDs = new Set(ids.map(String))
    usageBatchByAccountId.value = Object.fromEntries(
      Object.entries(usageBatchByAccountId.value).filter(([key]) => visibleIDs.has(key))
    )
    usageBatchErrorByAccountId.value = Object.fromEntries(
      Object.entries(usageBatchErrorByAccountId.value).filter(([key]) => visibleIDs.has(key))
    )
    usageBatchLoadingByAccountId.value = Object.fromEntries(
      Object.entries(usageBatchLoadingByAccountId.value).filter(([key]) => visibleIDs.has(key))
    )
    usageBatchRequestTokenByAccountId.value = Object.fromEntries(
      Object.entries(usageBatchRequestTokenByAccountId.value).filter(([key]) => visibleIDs.has(key))
    )
  }
  return { usageBatchByAccountId, usageBatchErrorByAccountId, usageBatchLoadingByAccountId,
    queueBatchedUsage, handleAccountUsageLoaded, retainVisibleUsage }
}
