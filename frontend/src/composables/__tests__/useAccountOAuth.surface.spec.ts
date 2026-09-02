import { describe, expect, it, vi } from 'vitest'

import { useAccountOAuth } from '@/composables/useAccountOAuth'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() })
}))

// exchangeAuthCode / cookieAuth 在 frontend/src 无任何调用方（组件用的是 useOpenAIOAuth
// 的同名方法）。留着它们会让读者以为 setup-token 的拒绝路径真的经过这里，也会让后续
// 改动继续往死代码上叠逻辑。
describe('useAccountOAuth surface', () => {
  it('no longer exposes the unused Claude exchange helpers', () => {
    const oauth = useAccountOAuth()
    expect(oauth).not.toHaveProperty('exchangeAuthCode')
    expect(oauth).not.toHaveProperty('cookieAuth')
  })

  it('keeps the helpers the account modals actually call', () => {
    const oauth = useAccountOAuth()
    expect(typeof oauth.generateAuthUrl).toBe('function')
    expect(typeof oauth.parseSessionKeys).toBe('function')
    expect(typeof oauth.buildExtraInfo).toBe('function')
    expect(typeof oauth.resetState).toBe('function')
  })
})
