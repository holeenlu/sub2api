import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useStepUp } from '../../composables/useStepUp'
import TotpStepUpDialog from '../TotpStepUpDialog.vue'

const { verifyStepUp, showError } = vi.hoisted(() => ({ verifyStepUp: vi.fn(), showError: vi.fn() }))
vi.mock('../../api/stepUp', () => ({ verifyStepUp }))
vi.mock('../../ports', () => ({ useIdentityNotifications: () => ({ showError }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

beforeEach(() => { localStorage.clear(); vi.clearAllMocks() })

describe('TOTP grant ownership', () => {
  it('never verifies a replacement prompt with a late grant from the previous session', async () => {
    let finish!: (grant: { verified: boolean }) => void
    verifyStepUp.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    localStorage.setItem('auth_token', 'old-session')
    const controller = useStepUp()
    const old = controller.prompt()
    const verified = vi.spyOn(controller, 'onVerified')
    const wrapper = mount(TotpStepUpDialog, { props: { controller } })
    await wrapper.get('input[autocomplete="one-time-code"]').setValue('123456')
    await flushPromises()
    expect(verifyStepUp).toHaveBeenCalledWith('123456')
    controller.onCancel()
    await expect(old).resolves.toBe(false)
    localStorage.setItem('auth_token', 'replacement-session')
    const current = controller.prompt()
    await flushPromises()
    finish({ verified: true })
    await flushPromises()
    expect(verified).not.toHaveBeenCalled()
    expect(controller.visible.value).toBe(true)
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
    await expect(current).resolves.toBe(false)
  })

  it('keeps the dialog open when the server explicitly denies the grant', async () => {
    verifyStepUp.mockResolvedValueOnce({ verified: false })
    const controller = useStepUp()
    const pending = controller.prompt()
    const wrapper = mount(TotpStepUpDialog, { props: { controller } })
    await wrapper.get('input[autocomplete="one-time-code"]').setValue('123456')
    await flushPromises()
    expect(controller.visible.value).toBe(true)
    expect(showError).toHaveBeenCalledWith('stepUp.verifyFailed')
    wrapper.unmount()
    await expect(pending).resolves.toBe(false)
  })

  it('silently discards a late error after the dialog is unmounted', async () => {
    let fail!: (error: Error) => void
    verifyStepUp.mockReturnValueOnce(new Promise((_resolve, reject) => { fail = reject }))
    const controller = useStepUp()
    const pending = controller.prompt()
    const wrapper = mount(TotpStepUpDialog, { props: { controller } })
    await wrapper.get('input[autocomplete="one-time-code"]').setValue('123456')
    await flushPromises()
    wrapper.unmount()
    fail(new Error('late failure'))
    await flushPromises()
    await expect(pending).resolves.toBe(false)
    expect(showError).not.toHaveBeenCalled()
  })
})
