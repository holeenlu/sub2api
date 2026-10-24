import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CloudAuthorization from '../CloudAuthorization.vue'

const api = vi.hoisted(() => ({ getCloudAuthorization: vi.fn(), authorizeCloud: vi.fn(), completeCloudAuthorization: vi.fn(), disconnectCloud: vi.fn() }))
vi.mock('@/api/admin/accounts', () => api)
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSuperAdmin: false }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const unauthorized = { authorized: false, enabled: false, status: 'unauthorized' }
const authorized = { ...unauthorized, authorized: true, status: 'authorized' }
const authURL = 'https://auth.openai.com/oauth/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback'
const props = { accountId: 1, enabled: false, canAuthorize: true, canEdit: true }
beforeEach(() => { vi.resetAllMocks(); api.getCloudAuthorization.mockResolvedValue(unauthorized) })

describe('Cloud account authorization', () => {
  it('shows authorization without verification controls or a second model allowlist', async () => {
    api.getCloudAuthorization.mockResolvedValue(authorized)
    const wrapper = mount(CloudAuthorization, { props })
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('status.authorized')
    expect(wrapper.find('[data-testid="cloud-test"]').exists()).toBe(false)
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="cloud-enabled"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="cloud-enabled"]').setValue(true)
    expect(wrapper.emitted('update:enabled')).toEqual([[true]])
    wrapper.unmount()
  })

  it('checks authorization and unresolved usage without a model test gate', async () => {
    api.getCloudAuthorization.mockResolvedValue(authorized)
    const wrapper = mount(CloudAuthorization, { props })
    await flushPromises()
    expect(wrapper.get('[data-testid="cloud-enabled"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).toContain('cloud.priorityHint')
    api.getCloudAuthorization.mockResolvedValue({ ...authorized, runtime_blocked: true })
    await wrapper.setProps({ accountId: 2 }); await flushPromises()
    expect(wrapper.get('[data-testid="cloud-enabled"]').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ enabled: true })
    expect(wrapper.get('[data-testid="cloud-enabled"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('ignores old account requests and clears callback material on account change', async () => {
    api.authorizeCloud.mockResolvedValue({ auth_url: authURL, session_id: 'session-1' })
    const wrapper = mount(CloudAuthorization, { props })
    await flushPromises()
    await wrapper.get('[data-testid="cloud-authorize"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-testid="cloud-callback"]').setValue('http://localhost:1455/auth/callback?code=secret&state=state')
    let resolveOld!: (value: unknown) => void
    api.completeCloudAuthorization.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    await wrapper.get('[data-testid="cloud-save-authorization"]').trigger('click')
    await wrapper.setProps({ accountId: 2 }); await flushPromises()
    resolveOld(authorized); await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('unauthorized')
    expect(wrapper.find('[data-testid="cloud-callback"]').exists()).toBe(false)
    expect(api.getCloudAuthorization).toHaveBeenLastCalledWith(2)
    expect(wrapper.emitted('update:enabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('synchronizes the saved gateway selection after reauthorization', async () => {
    api.getCloudAuthorization.mockResolvedValueOnce({ ...authorized, enabled: true }).mockResolvedValue(authorized)
    api.authorizeCloud.mockResolvedValue({ auth_url: authURL, session_id: 'session-1' })
    api.completeCloudAuthorization.mockResolvedValue(authorized)
    const wrapper = mount(CloudAuthorization, { props: { ...props, enabled: true } })
    await flushPromises()
    await wrapper.get('[data-testid="cloud-authorize"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-testid="cloud-callback"]').setValue('http://localhost:1455/auth/callback?code=secret&state=state')
    await wrapper.get('[data-testid="cloud-save-authorization"]').trigger('click'); await flushPromises()
    expect(wrapper.emitted('update:enabled')).toEqual([[false]])
    expect(wrapper.get('[role="status"]').text()).toContain('authorized')
    expect(wrapper.find('[data-testid="cloud-callback"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('rejects a callback from another redirect origin before exchanging', async () => {
    api.authorizeCloud.mockResolvedValue({ auth_url: authURL, session_id: 'session-1' })
    const wrapper = mount(CloudAuthorization, { props })
    await flushPromises()
    await wrapper.get('[data-testid="cloud-authorize"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-testid="cloud-callback"]').setValue('https://other.example/auth/callback?code=secret&state=state')
    await wrapper.get('[data-testid="cloud-save-authorization"]').trigger('click'); await flushPromises()
    expect(api.completeCloudAuthorization).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('invalidCallback')
    wrapper.unmount()
  })

  it('disconnects only this Cloud binding and turns off pending gateway settings', async () => {
    api.getCloudAuthorization.mockResolvedValueOnce(authorized).mockResolvedValue(unauthorized)
    api.disconnectCloud.mockResolvedValue({ authorized: false, remote_revoked: false })
    const wrapper = mount(CloudAuthorization, { props })
    await flushPromises()
    await wrapper.get('[data-testid="cloud-disconnect"]').trigger('click'); await flushPromises()
    expect(api.disconnectCloud).toHaveBeenCalledWith(1)
    expect(wrapper.emitted('update:enabled')).toEqual([[false]])
    expect(wrapper.get('[role="status"]').text()).toContain('unauthorized')
    wrapper.unmount()
  })
})
