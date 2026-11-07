import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useLoginAgreement } from '../useLoginAgreement'

beforeEach(() => localStorage.clear())

const settings = {
  login_agreement_enabled: true,
  login_agreement_mode: 'checkbox',
  login_agreement_updated_at: '2026-10-25T08:30:00Z',
  login_agreement_revision: 'terms-2',
  login_agreement_documents: [{ id: 'terms', title: 'Terms', content: 'Terms body', content_type: 'markdown' }]
}

describe('login agreement revision state', () => {
  it('keeps the server update timestamp and requires acceptance of a changed revision', () => {
    const agreement = useLoginAgreement(vi.fn())
    agreement.applyLoginAgreementSettings(settings)
    expect(agreement.loginAgreementUpdatedAt.value).toBe(settings.login_agreement_updated_at)
    expect(agreement.agreementGateActive.value).toBe(true)
    agreement.acceptLoginAgreement()
    expect(agreement.agreementGateActive.value).toBe(false)
    const registration = useLoginAgreement(vi.fn())
    registration.applyLoginAgreementSettings(settings)
    expect(registration.agreementGateActive.value).toBe(false)
    registration.applyLoginAgreementSettings({ ...settings, login_agreement_revision: 'terms-3' })
    expect(registration.agreementGateActive.value).toBe(true)
  })

  it('rejecting consent clears persistence and reports the page-specific warning', () => {
    const onRejected = vi.fn()
    const agreement = useLoginAgreement(onRejected)
    agreement.applyLoginAgreementSettings(settings)
    agreement.acceptLoginAgreement()
    agreement.rejectLoginAgreement()
    expect(agreement.agreementGateActive.value).toBe(true)
    expect(onRejected).toHaveBeenCalledOnce()
    expect(localStorage.getItem('sub2api_login_agreement_consent')).toBeNull()
  })
})
