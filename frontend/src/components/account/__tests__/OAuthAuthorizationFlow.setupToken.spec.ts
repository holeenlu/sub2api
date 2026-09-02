import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn() })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    grok: {
      getCapabilities: vi.fn().mockResolvedValue({ password_auth_enabled: false })
    }
  }
}))

import OAuthAuthorizationFlow from '../OAuthAuthorizationFlow.vue'

const TOKEN = 'sk-ant-oat01-' + 'a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8s9T0'

function mountFlow(props: Record<string, unknown> = {}) {
  return mount(OAuthAuthorizationFlow, {
    props: {
      addMethod: 'setup-token',
      platform: 'anthropic',
      showCookieOption: true,
      showManualOption: true,
      allowMultiple: false,
      ...props
    },
    global: {
      stubs: { Icon: true }
    }
  })
}

describe('OAuthAuthorizationFlow direct Claude setup-token mode', () => {
  it('replaces the method picker, manual and cookie flows with a paste panel', async () => {
    const wrapper = mountFlow()

    expect(wrapper.find('[data-testid="setup-token-import-panel"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('admin.accounts.oauth.setupTokenDirectDesc')
    expect(wrapper.text()).not.toContain('admin.accounts.oauth.cookieAutoAuthDesc')
    expect(wrapper.text()).not.toContain('admin.accounts.oauth.followSteps')
    expect(wrapper.find('input[type="radio"]').exists()).toBe(false)
    // Parents hide their "complete authorization" footer when inputMethod !== 'manual'.
    expect((wrapper.vm as unknown as { inputMethod: string }).inputMethod).toBe('setup_token')
  })

  it('emits import-setup-token with the pasted block and never cookie-auth', async () => {
    const wrapper = mountFlow()

    const submit = wrapper.get('[data-testid="setup-token-import-submit"]')
    expect(submit.attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="setup-token-input"]').setValue(`${TOKEN}\n`)
    expect(submit.attributes('disabled')).toBeUndefined()
    await submit.trigger('click')

    expect(wrapper.emitted('import-setup-token')).toEqual([[`${TOKEN}\n`]])
    expect(wrapper.emitted('cookie-auth')).toBeUndefined()
    expect(wrapper.emitted('exchange-code')).toBeUndefined()
  })

  it('shows the batch hint only when multiple tokens are allowed', async () => {
    const single = mountFlow({ allowMultiple: false })
    await single.get('[data-testid="setup-token-input"]').setValue(`${TOKEN}\n${TOKEN}x`)
    expect(single.text()).not.toContain('admin.accounts.oauth.batchCreateAccounts')

    const batch = mountFlow({ allowMultiple: true })
    await batch.get('[data-testid="setup-token-input"]').setValue(`${TOKEN}\n${TOKEN}x`)
    expect(batch.text()).toContain('admin.accounts.oauth.batchCreateAccounts')
  })

  it('restores the regular flow when the admin switches back to OAuth', async () => {
    const wrapper = mountFlow()
    expect(wrapper.find('[data-testid="setup-token-import-panel"]').exists()).toBe(true)

    await wrapper.setProps({ addMethod: 'oauth' })

    expect(wrapper.find('[data-testid="setup-token-import-panel"]').exists()).toBe(false)
    expect((wrapper.vm as unknown as { inputMethod: string }).inputMethod).toBe('manual')
    expect(wrapper.text()).toContain('admin.accounts.oauth.followSteps')
    expect(wrapper.find('input[type="radio"][value="cookie"]').exists()).toBe(true)
  })

  it('keeps the OAuth flow untouched for a non-Anthropic setup-token', () => {
    const wrapper = mountFlow({ platform: 'openai', showCookieOption: false })

    expect(wrapper.find('[data-testid="setup-token-import-panel"]').exists()).toBe(false)
    expect((wrapper.vm as unknown as { inputMethod: string }).inputMethod).toBe('manual')
  })
})
