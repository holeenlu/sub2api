import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Account } from '@/types'

const { applyOAuthCredentialsMock, exchangeCodeMock, updateAccountMock, showErrorMock } = vi.hoisted(
  () => ({
    applyOAuthCredentialsMock: vi.fn(),
    exchangeCodeMock: vi.fn(),
    updateAccountMock: vi.fn(),
    showErrorMock: vi.fn()
  })
)

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: showErrorMock, showSuccess: vi.fn() })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      applyOAuthCredentials: applyOAuthCredentialsMock,
      exchangeCode: exchangeCodeMock,
      update: updateAccountMock,
      clearError: vi.fn()
    },
    grok: {
      getCapabilities: vi.fn().mockResolvedValue({ password_auth_enabled: false })
    }
  }
}))

import ReAuthAccountModal from '../ReAuthAccountModal.vue'

const SETUP_TOKEN = 'sk-ant-oat01-' + 'a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8s9T0'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  props: { addMethod: String, platform: String, error: String },
  data: () => ({ inputMethod: 'manual' }),
  emits: ['import-setup-token', 'cookie-auth'],
  template: '<div />'
})

function makeAccount(overrides: Partial<Account> = {}): Account {
  return {
    id: 42,
    name: 'claude setup token',
    platform: 'anthropic',
    type: 'setup-token',
    proxy_id: null,
    concurrency: 1,
    priority: 50,
    status: 'active',
    credentials: { model_mapping: { 'claude-sonnet-5': 'claude-sonnet-5' } },
    ...overrides
  } as Account
}

// addMethod is initialised by the `show` watcher, so open the dialog after mount.
async function mountModal(account: Account) {
  const wrapper = mount(ReAuthAccountModal, {
    props: { show: false, account },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub,
        Icon: true
      }
    }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('ReAuthAccountModal direct Claude setup-token re-authorization', () => {
  beforeEach(() => {
    applyOAuthCredentialsMock.mockReset().mockResolvedValue({ id: 42, type: 'setup-token' })
    exchangeCodeMock.mockReset()
    updateAccountMock.mockReset()
    showErrorMock.mockReset()
  })

  it('defaults to the account type and passes it to the authorization flow', async () => {
    const wrapper = await mountModal(makeAccount())

    expect(wrapper.getComponent(OAuthAuthorizationFlowStub).props('addMethod')).toBe('setup-token')
    expect(wrapper.getComponent(OAuthAuthorizationFlowStub).props('platform')).toBe('anthropic')
  })

  it('applies the pasted token through apply-oauth-credentials without token residue', async () => {
    const wrapper = await mountModal(makeAccount())

    wrapper.getComponent(OAuthAuthorizationFlowStub).vm.$emit('import-setup-token', `${SETUP_TOKEN}\n`)
    await flushPromises()

    expect(exchangeCodeMock).not.toHaveBeenCalled()
    expect(updateAccountMock).not.toHaveBeenCalled()
    expect(applyOAuthCredentialsMock).toHaveBeenCalledTimes(1)
    expect(applyOAuthCredentialsMock.mock.calls[0]).toEqual([
      42,
      {
        type: 'setup-token',
        credentials: {
          access_token: SETUP_TOKEN,
          token_type: 'Bearer',
          scope: 'user:inference'
        }
      }
    ])
    const payload = applyOAuthCredentialsMock.mock.calls[0]?.[1]
    expect(payload.credentials).not.toHaveProperty('refresh_token')
    expect(payload.credentials).not.toHaveProperty('expires_at')
    expect(wrapper.emitted('reauthorized')).toHaveLength(1)
  })

  it('rejects an invalid or multi-line paste with an i18n message and no request', async () => {
    const wrapper = await mountModal(makeAccount())
    const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)

    flow.vm.$emit('import-setup-token', 'sk-ant-sid01-session-key')
    await flushPromises()
    expect(applyOAuthCredentialsMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.oauth.invalidSetupToken')

    flow.vm.$emit('import-setup-token', `${SETUP_TOKEN}\n${SETUP_TOKEN}x`)
    await flushPromises()
    expect(applyOAuthCredentialsMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.oauth.pleaseEnterSetupToken')
    expect(wrapper.emitted('reauthorized')).toBeUndefined()
  })

  it('ignores the setup-token import while OAuth re-authorization is selected', async () => {
    const wrapper = await mountModal(makeAccount({ type: 'oauth' }))

    wrapper.getComponent(OAuthAuthorizationFlowStub).vm.$emit('import-setup-token', SETUP_TOKEN)
    await flushPromises()

    expect(applyOAuthCredentialsMock).not.toHaveBeenCalled()
  })
})
