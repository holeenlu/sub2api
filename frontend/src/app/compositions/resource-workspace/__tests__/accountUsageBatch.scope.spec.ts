import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, ref, type EffectScope } from 'vue'
import type { AccountUsageInfo } from '@/features/resources/api/accountUsageTypes';
import type { accountsAPI } from '@/features/resources'
import { useAccountUsageBatch } from '../queries/useAccountUsageBatch'

const account = { id: 7, platform: 'openai', type: 'oauth' } as const
const usage = (tier: string): AccountUsageInfo => ({ updated_at: null, five_hour: null, seven_day: null, seven_day_sonnet: null, subscription_tier: tier })
let scope: EffectScope
beforeEach(() => { vi.useFakeTimers(); scope = effectScope() })
afterEach(() => { scope.stop(); vi.useRealTimers() })
function harness() {
  const identity = ref('session-a')
  const fetchUsage = vi.fn<typeof accountsAPI.getBatchUsage>()
  const query = scope.run(() => useAccountUsageBatch({ isDesktopViewport: ref(true), readScope: () => identity.value, fetchUsage }))
  if (!query) throw new Error('Query scope did not start')
  return { identity, fetchUsage, query }
}
describe('account usage query scope', () => {
  it('discards late batches after the authenticated identity changes', async () => {
    const { identity, fetchUsage, query } = harness()
    let finish = (_value: Awaited<ReturnType<typeof accountsAPI.getBatchUsage>>): void => { throw new Error('Deferred request is not initialized') }
    fetchUsage.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    query.queueBatchedUsage(account)
    await vi.advanceTimersByTimeAsync(0)
    identity.value = 'session-b'
    finish({ usage: { 7: usage('prior-account') }, errors: {} })
    await Promise.resolve(); await Promise.resolve()
    expect(query.usageBatchByAccountId.value).toEqual({})
    expect(query.usageBatchLoadingByAccountId.value).toEqual({})
    fetchUsage.mockResolvedValueOnce({ usage: { 7: usage('current-account') }, errors: {} })
    query.queueBatchedUsage(account)
    await vi.advanceTimersByTimeAsync(0)
    expect(fetchUsage).toHaveBeenCalledTimes(2)
    expect(query.usageBatchByAccountId.value['7']?.subscription_tier).toBe('current-account')
  })
  it('preserves a newer direct observation when an older batch completes', async () => {
    const { fetchUsage, query } = harness()
    let finish = (_value: Awaited<ReturnType<typeof accountsAPI.getBatchUsage>>): void => { throw new Error('Deferred request is not initialized') }
    fetchUsage.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    query.queueBatchedUsage(account)
    await vi.advanceTimersByTimeAsync(0)
    query.handleAccountUsageLoaded(account.id, usage('direct'))
    finish({ usage: { 7: usage('batch') }, errors: {} })
    await Promise.resolve(); await Promise.resolve()
    expect(query.usageBatchByAccountId.value['7']?.subscription_tier).toBe('direct')
  })
  it('cancels queued work when the page unmounts', async () => {
    const { fetchUsage, query } = harness()
    query.queueBatchedUsage(account)
    scope.stop()
    await vi.runAllTimersAsync()
    expect(fetchUsage).not.toHaveBeenCalled()
    expect(query.usageBatchByAccountId.value).toEqual({})
  })
})
