import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

const { updateAccountMock, showErrorMock } = vi.hoisted(() => ({
  updateAccountMock: vi.fn(),
  showErrorMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: showErrorMock, showSuccess: vi.fn(), showInfo: vi.fn() })
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: true }) }))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: { update: updateAccountMock, checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false }) },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
  }
}))
vi.mock('@/api/admin/accounts', () => ({ getAntigravityDefaultModelMapping: vi.fn() }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import EditAccountModal from '../EditAccountModal.vue'

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const account = (extra: Record<string, unknown> = {}, mapping: Record<string, string> = { 'gpt-5': 'gpt-5' }) => ({
  id: 21, name: 'OpenAI key', notes: '', platform: 'openai', type: 'apikey',
  credentials: { base_url: 'https://api.openai.com', model_mapping: mapping },
  credentials_status: { has_api_key: true },
  extra, proxy_id: null, concurrency: 1, priority: 1, rate_multiplier: 1, status: 'active',
  group_ids: [], expires_at: null, auto_pause_on_expired: false
})

function mountModal(value = account()) {
  return mount(EditAccountModal, {
    props: { show: true, account: value, proxies: [], groups: [] },
    global: { stubs: {
      BaseDialog: BaseDialogStub, Select: true, Icon: true, ProxySelector: true,
      GroupSelector: true, ModelWhitelistSelector: true
    } }
  })
}

const submit = (wrapper: ReturnType<typeof mountModal>) => wrapper.get('form#edit-account-form').trigger('submit.prevent')

describe('EditAccountModal model access policy', () => {
  beforeEach(() => {
    updateAccountMock.mockReset().mockImplementation(async (_id: number, payload: Record<string, unknown>) => ({ ...account(), ...payload }))
    showErrorMock.mockReset()
  })

  it('does not rewrite unchanged legacy policy', async () => {
    await submit(mountModal())
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalled())
    expect(updateAccountMock.mock.calls[0][1]).not.toHaveProperty('model_catalog_policy')
  })

  it('saves mode and exclusions on the account page without a second model list', async () => {
    const wrapper = mountModal()
    const field = wrapper.get('[data-testid="account-catalog-policy"]')
    await field.get('select').setValue('fixed')
    await field.get('textarea').setValue('gpt-5-mini\n gpt-5-mini \n')
    await submit(wrapper)
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalled())
    expect(updateAccountMock.mock.calls[0][1].model_catalog_policy).toEqual({ mode: 'fixed', models: ['gpt-5'], excluded: ['gpt-5-mini'] })
    const credentials = updateAccountMock.mock.calls[0][1].credentials as Record<string, unknown>
    expect(credentials.model_mapping).toEqual({})
  })

  it('uses the saved fixed list without adding stale whitelist entries', async () => {
    const wrapper = mountModal(account({ model_catalog_policy: { mode: 'fixed', models: ['fixed-only'], excluded: [] } }, { 'stale': 'stale', 'alias': 'fixed-only' }))
    const selector = wrapper.findComponent({ name: 'ModelWhitelistSelector' })
    expect(selector.props('modelValue')).toEqual(['fixed-only'])
    await submit(wrapper)
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalled())
    expect(updateAccountMock.mock.calls[0][1].credentials.model_mapping).toEqual({ alias: 'fixed-only' })
    expect(updateAccountMock.mock.calls[0][1]).not.toHaveProperty('model_catalog_policy')
  })

  it('saves whitelist edits and aliases together', async () => {
    const wrapper = mountModal(account({ model_catalog_policy: { mode: 'fixed', models: ['fixed-only'], excluded: [] } }, { stale: 'stale', alias: 'fixed-only' }))
    wrapper.findComponent({ name: 'ModelWhitelistSelector' }).vm.$emit('update:modelValue', ['new-model'])
    await submit(wrapper)
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalled())
    const payload = updateAccountMock.mock.calls[0][1]
    expect(payload.model_catalog_policy).toEqual({ mode: 'fixed', models: ['new-model'], excluded: [] })
    expect(payload.credentials.model_mapping).toEqual({ alias: 'fixed-only' })
  })

  it('leaves the dialog open without reporting success when validation fails', async () => {
    updateAccountMock.mockRejectedValueOnce({ message: 'refresh first' })
    const wrapper = mountModal()
    await wrapper.get('[data-testid="account-catalog-policy"] select').setValue('follow')
    await submit(wrapper)
    await vi.waitFor(() => expect(showErrorMock).toHaveBeenCalled())
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('migrates explicitly allowed alias names only when the user switches legacy to fixed', async () => {
    const wrapper = mountModal(account({}, { allowed: 'allowed', alias: 'target' }))
    await wrapper.get('[data-testid="account-catalog-policy"] select').setValue('fixed')
    await submit(wrapper)
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalled())
    expect(updateAccountMock.mock.calls[0][1].model_catalog_policy.models).toEqual(['allowed', 'alias'])
    expect(updateAccountMock.mock.calls[0][1].credentials.model_mapping).toEqual({ alias: 'target' })
  })

  it('does not add stale alias names while loading an existing fixed policy', async () => {
    const wrapper = mountModal(account({ model_catalog_policy: { mode: 'fixed', models: ['allowed'], excluded: [] } }, { stale: 'blocked' }))
    await submit(wrapper)
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalled())
    expect(wrapper.findComponent({ name: 'ModelWhitelistSelector' }).props('modelValue')).toEqual(['allowed'])
    expect(updateAccountMock.mock.calls[0][1]).not.toHaveProperty('model_catalog_policy')
  })

  it.each(['apikey', 'oauth'])('edits fixed access on %s passthrough accounts without touching dormant mappings', async type => {
    const wrapper = mountModal({ ...account({ openai_passthrough: true, model_catalog_policy: { mode: 'fixed', models: ['allowed'], excluded: [] } }, { dormant: 'target' }), type })
    expect(wrapper.findAllComponents({ name: 'ModelWhitelistSelector' })).toHaveLength(1)
    const selector = wrapper.findComponent({ name: 'ModelWhitelistSelector' })
    expect(selector.props('modelValue')).toEqual(['allowed'])
    expect(wrapper.text()).toContain('modelCatalog.passthroughPolicyHint')
    selector.vm.$emit('update:modelValue', ['new'])
    await submit(wrapper)
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalled())
    const payload = updateAccountMock.mock.calls[0][1]
    expect(payload.model_catalog_policy.models).toEqual(['new'])
    expect(payload.credentials.model_mapping).toEqual({ dormant: 'target' })
  })

  it('edits Antigravity policy independently of its explicit identity routing', async () => {
    const value = { ...account({ model_catalog_policy: { mode: 'fixed', models: ['allowed'], excluded: [] } }, { route: 'route', alias: 'allowed' }), platform: 'antigravity' }
    const wrapper = mountModal(value)
    const selector = wrapper.findComponent({ name: 'ModelWhitelistSelector' })
    expect(selector.props('modelValue')).toEqual(['allowed'])
    selector.vm.$emit('update:modelValue', [])
    await submit(wrapper)
    await vi.waitFor(() => expect(updateAccountMock).toHaveBeenCalled())
    const payload = updateAccountMock.mock.calls[0][1]
    expect(payload.model_catalog_policy).toEqual({ mode: 'fixed', models: [], excluded: [] })
    expect(payload.credentials.model_mapping).toEqual({ route: 'route', alias: 'allowed' })
  })
})
