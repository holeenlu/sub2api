import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { useKeyList } from '../queries/useKeyList'
import { useKeyGroups } from '../queries/useKeyGroups'
import { keysServicesKey, type KeysServices, type KeysGroup } from '../services'
import { createKeysTestServices } from './services'
import { keyFixture } from './keyFixture'
const { list } = vi.hoisted(() => ({ list: vi.fn() }))
vi.mock('../api/keys', () => ({ keysAPI: { list } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('not initialized') }
  let reject: (value: unknown) => void = () => { throw new Error('not initialized') }
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function host<T>(create: () => T, services: KeysServices) {
  const results: T[] = []
  const wrapper = mount(defineComponent({ setup() { results.push(create()); return () => null } }), {
    global: { provide: { [keysServicesKey as symbol]: services } }
  })
  return { wrapper, state: results[0] }
}
const response = (id: number) => ({ items: [keyFixture(id)], total: 1, page: 1, page_size: 10, pages: 1 })
beforeEach(() => { list.mockReset() })
describe('Key queries capture session, scope and request generation', () => {
  it('rejects a late response even if the transport ignored abort', async () => {
    const older = deferred<ReturnType<typeof response>>(), newer = deferred<ReturnType<typeof response>>()
    list.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)
    const { wrapper, state } = host(useKeyList, createKeysTestServices())
    state.filterSearch.value = 'new'; state.onFilterChange()
    expect(list.mock.calls[0][3].signal.aborted).toBe(true)
    newer.resolve(response(2)); await flushPromises()
    older.resolve(response(1)); await flushPromises()
    expect(state.apiKeys.value.map(key => key.id)).toEqual([2]); expect(state.loading.value).toBe(false)
    wrapper.unmount()
  })
  it('clears private rows immediately on a new session for the same user and rejects old usage', async () => {
    const scopeKey = ref<string | null>('same-user:session-one')
    const usage = deferred<{ stats: Record<string, { api_key_id: number; today_actual_cost: number; total_actual_cost: number }> }>()
    const services = createKeysTestServices({ scopeKey, getKeyUsage: vi.fn().mockReturnValueOnce(usage.promise).mockResolvedValueOnce({ stats: {} }) })
    list.mockResolvedValueOnce(response(1)).mockResolvedValueOnce(response(2))
    const { wrapper, state } = host(useKeyList, services)
    await flushPromises(); state.handleSelectionChange([1])
    expect(state.loading.value).toBe(false); expect(state.usageState.value).toBe('loading')
    scopeKey.value = 'same-user:session-two'
    expect(state.apiKeys.value).toEqual([]); expect(state.selectedIds.value).toEqual([])
    usage.resolve({ stats: { 1: { api_key_id: 1, today_actual_cost: 99, total_actual_cost: 999 } } })
    await flushPromises(); expect(state.apiKeys.value[0].id).toBe(2); expect(state.usageStats.value).toEqual({})
    wrapper.unmount()
  })
  it('aborts on unmount and does not notify or change loading from a late failure', async () => {
    const pending = deferred<ReturnType<typeof response>>(); list.mockReturnValue(pending.promise)
    const services = createKeysTestServices(); const { wrapper, state } = host(useKeyList, services)
    wrapper.unmount(); expect(list.mock.calls[0][3].signal.aborted).toBe(true)
    pending.reject(new Error('late')); await flushPromises()
    expect(services.notifyError).not.toHaveBeenCalled(); expect(state.state.value).toBe('loading')
  })
  it('does not let an old finally hide the next loading state and preserves group zero', async () => {
    const older = deferred<ReturnType<typeof response>>(), newer = deferred<ReturnType<typeof response>>()
    list.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)
    const services = createKeysTestServices(); const { wrapper, state } = host(useKeyList, services)
    state.onGroupFilterChange(0); older.reject(new Error('old failure')); await flushPromises()
    expect(state.loading.value).toBe(true); expect(services.notifyError).not.toHaveBeenCalled()
    expect(list.mock.calls[1][2].group_id).toBe(0)
    newer.resolve(response(2)); await flushPromises(); wrapper.unmount()
  })
  it('does not fetch with an unauthenticated scope', () => {
    const { wrapper } = host(useKeyList, createKeysTestServices({ scopeKey: ref(null) }))
    expect(list).not.toHaveBeenCalled(); wrapper.unmount()
  })
  it('keeps group defaults and rates from the same captured refresh', async () => {
    const oldGroups = deferred<KeysGroup[]>(), oldRates = deferred<Record<number, number>>()
    const newGroups = deferred<KeysGroup[]>(), newRates = deferred<Record<number, number>>()
    const services = createKeysTestServices({
      getAvailableGroups: vi.fn().mockReturnValueOnce(oldGroups.promise).mockReturnValueOnce(newGroups.promise),
      getUserGroupRates: vi.fn().mockReturnValueOnce(oldRates.promise).mockReturnValueOnce(newRates.promise)
    })
    const { wrapper, state } = host(useKeyGroups, services)
    const refresh = state.loadGroupPricing()
    newGroups.resolve([]); await flushPromises(); expect(state.groupPricingLoading.value).toBe(true)
    newRates.resolve({ 2: 0 }); await refresh
    oldGroups.resolve([]); oldRates.resolve({ 1: 9 }); await flushPromises()
    expect(state.userGroupRates.value).toEqual({ 2: 0 }); expect(state.groupPricingLoading.value).toBe(false)
    wrapper.unmount()
  })
  it('clears available groups and cancels both requests on scope revocation', async () => {
    const scopeKey = ref<string | null>('user:1')
    const groups = deferred<KeysGroup[]>(), rates = deferred<Record<number, number>>()
    const getAvailableGroups = vi.fn().mockReturnValue(groups.promise), getUserGroupRates = vi.fn().mockReturnValue(rates.promise)
    const services = createKeysTestServices({ scopeKey, getAvailableGroups, getUserGroupRates })
    const { wrapper, state } = host(useKeyGroups, services)
    scopeKey.value = null
    expect(getAvailableGroups.mock.calls[0][0].signal.aborted).toBe(true)
    expect(getUserGroupRates.mock.calls[0][0].signal.aborted).toBe(true)
    groups.resolve([]); rates.resolve({ 1: 10 }); await flushPromises()
    expect(state.groups.value).toEqual([]); expect(state.userGroupRates.value).toEqual({}); expect(state.groupPricingLoading.value).toBe(false)
    wrapper.unmount()
  })
})
