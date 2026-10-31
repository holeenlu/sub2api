import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, inject } from 'vue'
import { createPinia } from 'pinia'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { apiClient } from '@/shared/http'
import { identityUserDirectoryDataKey, identityUserPanelsKey } from '@/features/identity'
import { installIdentityUserServices } from '../../identityUserServices'

const mocks = vi.hoisted(() => ({
  getAll: vi.fn(), getAllIncludingInactive: vi.fn(), usage: vi.fn(), quotas: vi.fn(),
}))
vi.mock('@/features/access-policy', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/access-policy')>()
  return { ...actual, groupsAPI: { ...actual.groupsAPI, getAll: mocks.getAll, getAllIncludingInactive: mocks.getAllIncludingInactive } }
})
vi.mock('@/features/usage/adminDashboard', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/usage/adminDashboard')>()),
  getBatchUsersUsage: mocks.usage,
}))
vi.mock('@/features/access-policy/userAccess', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/access-policy/userAccess')>()),
  getPlatformQuotas: mocks.quotas,
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  for (const method of ['get', 'post', 'put', 'delete'] as const) {
    vi.spyOn(apiClient, method).mockRejectedValue(new Error('Unexpected HTTP request'))
  }
})
afterEach(() => vi.restoreAllMocks())

function install() {
  const app = createApp({ render: () => null })
  installIdentityUserServices(app)
  return app
}

describe('user directory app composition', () => {
  it('preserves inactive-group selection and server usage/quota projections', async () => {
    const app = install()
    const data = app.runWithContext(() => inject(identityUserDirectoryDataKey))!
    const active = [{ id: 3, name: 'Active', status: 'active', is_exclusive: false, subscription_type: 'standard' }]
    const all = [...active, { ...active[0], id: 7, name: 'Historical', status: 'disabled' }]
    const usage = { stats: { '42': { total_cost: 1.25 } } }
    const quotas = { platform_quotas: [{ platform: 'openai', daily_limit_usd: 0, weekly_limit_usd: null, daily_usage_usd: 2.5 }] }
    mocks.getAll.mockResolvedValue(active)
    mocks.getAllIncludingInactive.mockResolvedValue(all)
    mocks.usage.mockResolvedValue(usage)
    mocks.quotas.mockResolvedValue(quotas)

    await expect(data.listGroups()).resolves.toBe(active)
    const includingInactive = await data.listGroups(true)
    expect(includingInactive).toBe(all)
    expect(data.buildApiKeyGroupOptions(includingInactive, { all: 'All', exclusive: 'Exclusive', public: 'Public', subscription: 'Subscription', disabled: 'Disabled' })).toContainEqual({ value: 7, label: 'Historical' })
    await expect(data.loadUsage([42])).resolves.toBe(usage)
    await expect(data.loadPlatformQuotas(42)).resolves.toBe(quotas)
    expect(mocks.usage).toHaveBeenCalledWith([42])
    expect(mocks.quotas).toHaveBeenCalledWith(42)
    expect(apiClient.get).not.toHaveBeenCalled()
  })

  it.each(['UserApiKeysModal', 'UserAllowedGroupsModal', 'GroupReplaceModal', 'UserBalanceModal', 'UserBalanceHistoryModal', 'UserPlatformQuotaModal'] as const)('loads the real %s implementation through the installed panel port', async name => {
    const app = install()
    const panels = app.runWithContext(() => inject(identityUserPanelsKey))!
    const panelProps = {
      show: false, user: null,
      ...(name === 'UserBalanceModal' ? { operation: 'add' } : {}),
      ...(name === 'GroupReplaceModal' ? { oldGroup: null, allGroups: [] } : {}),
    }
    const wrapper = mount(defineComponent({
      render: () => h(panels[name], panelProps),
    }), { global: { plugins: [createPinia()], stubs: { BaseDialog: true } } })
    await vi.waitFor(() => expect(wrapper.findComponent({ name }).exists()).toBe(true))
    expect(apiClient.get).not.toHaveBeenCalled()
    expect(apiClient.post).not.toHaveBeenCalled()
    expect(apiClient.put).not.toHaveBeenCalled()
  })
})
