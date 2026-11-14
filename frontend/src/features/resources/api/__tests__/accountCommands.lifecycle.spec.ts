import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { AxiosError } from 'axios'
import type { AxiosResponse, InternalAxiosRequestConfig } from 'axios'

const { stepUp } = vi.hoisted(() => ({ stepUp: vi.fn() }))
vi.mock('@/features/identity', () => ({
  requestAdminStepUp: stepUp,
  StepUpCancelledError: class extends Error {}
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/i18n', () => ({
  getLocale: () => 'en',
  i18n: { global: { t: (key: string) => key } }
}))

type AccountAPI = typeof import('@/features/resources/accounts')
type PlanAPI = typeof import('@/features/resources/scheduledTests')
type Command = {
  name: string
  method: string
  path: string
  payload?: unknown
  params?: Record<string, string>
  call(accounts: AccountAPI, plans: PlanAPI, signal: AbortSignal): Promise<unknown>
}
const commands: Command[] = [
  { name: 'priority-update', method: 'put', path: '/admin/accounts/701', payload: { priority: 4 }, call: (a, _, signal) => a.update(701, { priority: 4 }, { signal }) },
  { name: 'delete', method: 'delete', path: '/admin/accounts/701', call: (a, _, signal) => a.deleteAccount(701, { signal }) },
  { name: 'duplicate', method: 'post', path: '/admin/accounts/701/duplicate', call: (a, _, signal) => a.duplicate(701, { signal }) },
  { name: 'refresh', method: 'post', path: '/admin/accounts/701/refresh', call: (a, _, signal) => a.refreshCredentials(701, { signal }) },
  { name: 'recover', method: 'post', path: '/admin/accounts/701/recover-state', call: (a, _, signal) => a.recoverState(701, { signal }) },
  { name: 'quota', method: 'post', path: '/admin/accounts/701/reset-quota', call: (a, _, signal) => a.resetAccountQuota(701, { signal }) },
  { name: 'privacy', method: 'post', path: '/admin/accounts/701/set-privacy', call: (a, _, signal) => a.setPrivacy(701, { signal }) },
  { name: 'fallback', method: 'post', path: '/admin/accounts/701/revert-proxy-fallback', call: (a, _, signal) => a.revertProxyFallback(701, { signal }) },
  { name: 'probe', method: 'post', path: '/admin/accounts/701/upstream-billing-probe', call: (a, _, signal) => a.probeUpstreamBilling(701, { signal }) },
  { name: 'models', method: 'get', path: '/admin/accounts/701/models', call: (a, _, signal) => a.getAvailableModels(701, { signal }) },
  { name: 'details', method: 'get', path: '/admin/accounts/701', call: (a, _, signal) => a.getById(701, { signal }) },
  { name: 'shadow', method: 'post', path: '/admin/accounts/701/shadow', payload: { name: 'original shadow', group_ids: [] }, call: (a, _, signal) => a.createSparkShadow(701, { name: 'original shadow', group_ids: [] }, { signal }) },
  { name: 'schedulable', method: 'post', path: '/admin/accounts/701/schedulable', payload: { schedulable: false }, call: (a, _, signal) => a.setSchedulable(701, false, { signal }) },
  { name: 'batch-delete', method: 'post', path: '/admin/accounts/batch-delete', payload: { account_ids: [701, 702] }, call: (a, _, signal) => a.batchDelete([701, 702], { signal }) },
  { name: 'batch-clear', method: 'post', path: '/admin/accounts/batch-clear-error', payload: { account_ids: [701, 702] }, call: (a, _, signal) => a.batchClearError([701, 702], { signal }) },
  { name: 'batch-refresh', method: 'post', path: '/admin/accounts/batch-refresh', payload: { account_ids: [701, 702] }, call: (a, _, signal) => a.batchRefresh([701, 702], { signal }) },
  { name: 'batch-probe', method: 'post', path: '/admin/accounts/upstream-billing-probe/batch', payload: { account_ids: [701, 702] }, call: (a, _, signal) => a.probeUpstreamBillingBatch([701, 702], { signal }) },
  { name: 'bulk-update', method: 'post', path: '/admin/accounts/bulk-update', payload: { account_ids: [701, 702], schedulable: false }, params: { confirm: 'true' }, call: (a, _, signal) => a.bulkUpdate([701, 702], { schedulable: false }, { confirm: 'true' }, { signal }) },
  { name: 'export', method: 'get', path: '/admin/accounts/data', params: { ids: '701,702', include_proxies: 'false' }, call: (a, _, signal) => a.exportData({ ids: [701, 702], includeProxies: false }, { signal }) },
  { name: 'plan-create', method: 'post', path: '/admin/scheduled-test-plans', payload: { account_id: 701, model_id: 'original-model', cron_expression: '0 * * * *', enabled: false }, call: (_, p, signal) => p.create({ account_id: 701, model_id: 'original-model', cron_expression: '0 * * * *', enabled: false }, { signal }) },
  { name: 'plan-update', method: 'put', path: '/admin/scheduled-test-plans/701', payload: { enabled: false }, call: (_, p, signal) => p.update(701, { enabled: false }, { signal }) },
  { name: 'plan-delete', method: 'delete', path: '/admin/scheduled-test-plans/701', call: (_, p, signal) => p.deletePlan(701, { signal }) }
]

function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Not initialized') }
  const promise = new Promise<T>(yes => { resolve = yes })
  return { promise, resolve }
}

// Only the adapter is synthetic. Shared interceptors and app step-up recovery
// run unchanged; no request reaches a real resource, upstream or payment API.
describe('account command HTTP ownership', () => {
  let accounts: AccountAPI
  let plans: PlanAPI
  const adapter = vi.fn<(config: InternalAxiosRequestConfig) => Promise<AxiosResponse>>()
  beforeEach(async () => {
    vi.resetModules()
    localStorage.clear()
    sessionStorage.clear()
    localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin', policy_version: 1 }))
    localStorage.setItem('auth_token', 'header.' + btoa(JSON.stringify({ user_id: 7, role: 'admin', sid: 'session-a', token_version: 1 })) + '.signature')
    stepUp.mockReset()
    adapter.mockReset().mockImplementation(async config => ({
      status: 200, statusText: 'OK', headers: {}, config,
      data: { code: 0, data: { id: 702, results: [] } }
    }))
    const { apiClient } = await import('@/shared/http')
    apiClient.defaults.adapter = adapter
    const { installApplicationHttp } = await import('@/app/http/install')
    installApplicationHttp()
    accounts = await import('@/features/resources/accounts')
    plans = await import('@/features/resources/scheduledTests')
    adapter.mockImplementationOnce(async config => {
      throw new AxiosError('Step-up required', 'ERR_BAD_REQUEST', config, undefined, {
        status: 403, statusText: 'Forbidden', config, headers: {}, data: { code: 'STEP_UP_REQUIRED' }
      })
    })
  })
  afterEach(() => { vi.restoreAllMocks() })

  it.each(commands)('$name does not dispatch a retry after its owner cancels', async command => {
    const verified = deferred<boolean>()
    stepUp.mockReturnValueOnce(verified.promise)
    const controller = new AbortController()
    const pending = command.call(accounts, plans, controller.signal)
    const rejected = expect(pending).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    await vi.waitFor(() => expect(stepUp).toHaveBeenCalledOnce())
    expect(adapter.mock.calls[0][0].signal).toBe(controller.signal)
    const key = adapter.mock.calls[0][0].headers.get('Idempotency-Key')
    controller.abort()
    verified.resolve(true)
    await rejected
    expect(adapter).toHaveBeenCalledOnce()
    if (command.name === 'duplicate') {
      expect(sessionStorage.getItem('sub2api:admin:account-duplicate:7:701')).toBe(key)
      await command.call(accounts, plans, new AbortController().signal)
      expect(adapter.mock.calls[1][0].headers.get('Idempotency-Key')).toBe(key)
      expect(sessionStorage.getItem('sub2api:admin:account-duplicate:7:701')).toBeNull()
    }
  })

  it.each(commands)('$name preserves its original method, target and payload on verified retry', async command => {
    stepUp.mockResolvedValueOnce(true)
    const controller = new AbortController()
    await command.call(accounts, plans, controller.signal)
    expect(adapter).toHaveBeenCalledTimes(2)
    const first = adapter.mock.calls[0][0]
    const retry = adapter.mock.calls[1][0]
    for (const request of [first, retry]) {
      expect(request.method).toBe(command.method)
      expect(request.url).toBe(command.path)
      expect(request.data === undefined ? undefined : JSON.parse(request.data)).toEqual(command.payload)
      expect(request.signal).toBe(controller.signal)
      if (command.params) expect(request.params).toMatchObject(command.params)
    }
    if (command.name === 'duplicate') expect(retry.headers.get('Idempotency-Key')).toBe(first.headers.get('Idempotency-Key'))
    if (command.name === 'batch-refresh') expect(retry.timeout).toBe(120000)
  })
  it.each(['actor', 'field', 'target', 'unmount'] as const)('real priority cell cancels a pending HTTP step-up on %s', async boundary => {
    const { createApp, reactive, h, nextTick } = await import('vue')
    const { default: PriorityCell } = await import('../../components/AccountPriorityCell.vue')
    const state = reactive({ scope: 'actor-a', allowed: true, account: { id: 701, priority: 3 } })
    const verified = deferred<boolean>()
    stepUp.mockReturnValueOnce(verified.promise)
    const updated = vi.fn()
    const failed = vi.fn()
    const host = document.createElement('div')
    document.body.append(host)
    const app = createApp({
      render: () => h(PriorityCell, {
        account: state.account, scopeKey: () => state.scope, canEdit: () => state.allowed,
        onUpdated: updated, onError: failed
      })
    })
    let mounted = false
    try {
      app.mount(host)
      mounted = true
      const button = host.querySelector('[data-testid="account-priority-increment"]')
      if (!(button instanceof HTMLButtonElement)) throw new Error('Missing priority action')
      button.click()
      await vi.waitFor(() => expect(stepUp).toHaveBeenCalledOnce(), { timeout: 1500 })
      const request = adapter.mock.calls[0][0]
      expect(request.url).toBe('/admin/accounts/701')
      expect(JSON.parse(request.data)).toEqual({ priority: 4 })
      if (boundary === 'actor') state.scope = 'actor-b'
      else if (boundary === 'field') state.allowed = false
      else if (boundary === 'target') { state.account = { id: 702, priority: 20 }; await nextTick() }
      else { app.unmount(); mounted = false }
      expect(request.signal?.aborted).toBe(true)
      verified.resolve(true)
      await flushPromises()
      expect(adapter).toHaveBeenCalledOnce()
      expect(updated).not.toHaveBeenCalled()
      expect(failed).not.toHaveBeenCalled()
    } finally {
      if (mounted) app.unmount()
      host.remove()
    }
  })

})
