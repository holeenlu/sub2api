import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountInfoCell from '../AccountInfoCell.vue'

vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key }),
}))
afterEach(() => { document.body.innerHTML = '' })
const props = { name: 'openai-701ad4a5-e635-4a17-b78e-f62ffab41c97', id: 3, platform: 'openai' as const, type: 'oauth' as const, planType: 'go', email: 'owner@example.invalid', authorizationModes: ['native'], subscriptionExpiresAt: '2026-11-19', privacyMode: 'training_set_failed', compact: { mode: 'auto' } }

describe('compact account identity and copyable details', () => {
  it('copies immediately from the name without pinning details and offers a separate details control', async () => {
    const w=mount(AccountInfoCell,{attachTo:document.body,props})
    await w.get('[data-account-copy]').trigger('click')
    expect(w.emitted('copy')).toHaveLength(1)
    expect(w.get('[data-account-details]').attributes('aria-expanded')).toBe('false')
    await w.get('[data-account-details]').trigger('click')
    expect(w.get('[data-account-details]').attributes('aria-expanded')).toBe('true')
    expect(w.emitted('copy')).toHaveLength(1)
    const button=document.querySelector('[data-account-info] button') as HTMLButtonElement
    button.click()
    expect(w.emitted('copy')).toHaveLength(2)
    w.unmount()
  })
  it('keeps the list compact without proxy repetition and presents complete structured metadata', async () => {
    const w=mount(AccountInfoCell,{attachTo:document.body,props})
    expect(w.text()).not.toContain('103.120.23.112:9905')
    expect(w.text()).not.toContain('training_set_failed')
    const panel=document.querySelector('[data-account-info]')!
    expect(panel.querySelectorAll('dl')).toHaveLength(3)
    expect(panel.textContent).not.toContain('admin.accounts.columns.proxy')
    for (const text of [props.name, props.email, 'Go', 'OAuth', '2026-11-19', 'vendor.authorization.native', 'admin.accounts.privacyFailed', 'admin.accounts.openai.compactAuto']) expect(panel.textContent).toContain(text)
    expect(panel.querySelector('svg')).toBeNull()
    w.unmount()
  })
})
