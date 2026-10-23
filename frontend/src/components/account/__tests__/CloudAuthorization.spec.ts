import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CloudAuthorization from '../CloudAuthorization.vue'

const api = vi.hoisted(() => ({ getCloudAuthorization: vi.fn(), authorizeCloud: vi.fn(), completeCloudAuthorization: vi.fn(), disconnectCloud: vi.fn() }))
vi.mock('@/api/admin/accounts', () => api)
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const unauthorized = { authorized: false, enabled: false, status: 'unauthorized', verified_models: [], models: [] }
const verified = { ...unauthorized, authorized: true, status: 'verified', verified_models: ['verified-model'], models: ['verified-model', 'discovered-model'] }
const authURL = 'https://auth.openai.com/oauth/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback'
const props = { accountId: 1, enabled: false, models: [], bpsEnabled: false, canAuthorize: true, canEdit: true, canTest: true }
beforeEach(() => { vi.resetAllMocks(); api.getCloudAuthorization.mockResolvedValue(unauthorized) })

describe('Cloud account authorization', () => {
  it('requires a fully verified selected model and rejects BPS coexistence', async () => {
    api.getCloudAuthorization.mockResolvedValue(verified)
    const wrapper = mount(CloudAuthorization, { props })
    await flushPromises()
    expect(wrapper.get('[data-testid="cloud-enabled"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('input[value="discovered-model"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="cloud-test-model"]').text()).toContain('discovered-model')
    await wrapper.setProps({ models: ['verified-model'] })
    expect(wrapper.get('[data-testid="cloud-enabled"]').attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ bpsEnabled: true })
    expect(wrapper.get('[data-testid="cloud-enabled"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('does not allow discovery or an authorized-only state to enable Cloud', async () => {
    api.getCloudAuthorization.mockResolvedValue({ ...verified, status: 'authorized', verified_models: [] })
    const wrapper = mount(CloudAuthorization, { props: { ...props, models: ['discovered-model'] } })
    await flushPromises()
    expect(wrapper.get('[data-testid="cloud-enabled"]').attributes('disabled')).toBeDefined()
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
    resolveOld(verified); await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('unauthorized')
    expect(wrapper.find('[data-testid="cloud-callback"]').exists()).toBe(false)
    expect(api.getCloudAuthorization).toHaveBeenLastCalledWith(2)
    expect(wrapper.emitted('update:enabled')).toBeUndefined()
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
    api.getCloudAuthorization.mockResolvedValueOnce(verified).mockResolvedValue(unauthorized)
    api.disconnectCloud.mockResolvedValue({ authorized: false, remote_revoked: false })
    const wrapper = mount(CloudAuthorization, { props: { ...props, enabled: true, models: ['verified-model'] } })
    await flushPromises()
    await wrapper.get('[data-testid="cloud-disconnect"]').trigger('click'); await flushPromises()
    expect(api.disconnectCloud).toHaveBeenCalledWith(1)
    expect(wrapper.emitted('update:enabled')).toEqual([[false]])
    expect(wrapper.emitted('update:models')).toEqual([[[]]])
    expect(wrapper.get('[role="status"]').text()).toContain('unauthorized')
    wrapper.unmount()
  })
})
