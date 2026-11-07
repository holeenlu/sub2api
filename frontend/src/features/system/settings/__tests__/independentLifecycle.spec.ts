import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, readonly, ref } from 'vue'
import { useStepUp, isStepUpCancelled } from '@/features/identity/composables/useStepUp'
import EmailTemplateEditor from '../EmailTemplateEditor.vue'
import AdminKeysSettingsPanel from '../AdminKeysSettingsPanel.vue'
import SmtpSettingsPanel from '../SmtpSettingsPanel.vue'
import { systemSettingsServicesKey, type SystemSettingsServices } from '../ports'
import type { SmtpDraft } from '../smtpModel'

vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.restoreAllMocks(); vi.spyOn(window, 'confirm').mockReturnValue(true) })
function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (error: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
const template = (event = 'auth.verify_code', language = 'en') => ({ event, locale: language, subject: event + ':' + language, html: '<p>' + event + ':' + language + '</p>', is_custom: true, placeholders: [] })
function fixture() {
  const scope = ref<string | null>('actor-a')
  const stepUp = useStepUp()
  const control = {
    getEmailTemplates: vi.fn().mockResolvedValue({ events: ['auth.verify_code', 'auth.password_reset'], locales: ['en', 'zh'], placeholders: [] }),
    getEmailTemplate: vi.fn(async (event: string, language: string, _options?: { signal?: AbortSignal }) => template(event, language)),
    updateEmailTemplate: vi.fn().mockResolvedValue(template()), restoreOfficialEmailTemplate: vi.fn().mockResolvedValue(template()),
    previewEmailTemplate: vi.fn(async (value: { subject: string; html: string }) => ({ subject: value.subject, html: value.html })),
    getAdminApiKey: vi.fn().mockResolvedValue({ exists: true, masked_key: 'masked-current' }),
    regenerateAdminApiKey: vi.fn().mockResolvedValue({ key: 'fixture-transient-secret' }), deleteAdminApiKey: vi.fn().mockResolvedValue({ message: 'deleted' }),
    testSmtpConnection: vi.fn().mockResolvedValue({ message: 'tested' }), sendTestEmail: vi.fn().mockResolvedValue({ message: 'sent' }),
  }
  const notifications = { showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }
  const services = { scope: readonly(scope), control, notifications, createStepUp: () => stepUp, isStepUpCancelled, StepUpDialog: defineComponent({ template: '<div />' }) } satisfies SystemSettingsServices
  return { scope, control, notifications, stepUp, global: { provide: { [systemSettingsServicesKey as symbol]: services }, stubs: { Icon: true } } }
}
function button(wrapper: VueWrapper, key: string) {
  const result = wrapper.findAll('button').find(item => item.text() === key)
  if (!result) throw new Error('Missing action: ' + key)
  return result
}

describe('independent email template ownership', () => {
  it('does not apply an old save or preview after changing the selected event', async () => {
    const f = fixture(), write = deferred<ReturnType<typeof template>>()
    f.control.updateEmailTemplate.mockReturnValueOnce(write.promise)
    const wrapper = mount(EmailTemplateEditor, { global: f.global })
    await flushPromises()
    await button(wrapper, 'admin.settings.emailTemplates.save').trigger('click')
    const signal = f.control.updateEmailTemplate.mock.calls[0][3].signal as AbortSignal
    await wrapper.get('#email-template-event').setValue('auth.password_reset')
    await flushPromises()
    write.resolve(template())
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect((wrapper.get('#email-template-subject').element as HTMLInputElement).value).toBe('auth.password_reset:en')
    expect(f.notifications.showSuccess).not.toHaveBeenCalled()
    expect(f.control.updateEmailTemplate).toHaveBeenCalledTimes(1)
  })

  it('cancels a save and restores usable read-only loading state when editing permission changes', async () => {
    const f = fixture(), write = deferred<ReturnType<typeof template>>()
    f.control.updateEmailTemplate.mockReturnValueOnce(write.promise)
    const wrapper = mount(EmailTemplateEditor, { global: f.global })
    await flushPromises()
    await button(wrapper, 'admin.settings.emailTemplates.save').trigger('click')
    await wrapper.setProps({ readOnly: true })
    await flushPromises()
    write.resolve({ ...template(), subject: 'Old writable result' })
    await flushPromises()
    expect(wrapper.get('#email-template-event').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('#email-template-subject').attributes('disabled')).toBeDefined()
    expect((wrapper.get('#email-template-subject').element as HTMLInputElement).value).not.toBe('Old writable result')
    expect(f.notifications.showSuccess).not.toHaveBeenCalled()
    expect(f.control.updateEmailTemplate).toHaveBeenCalledTimes(1)
  })

  it('preserves a newer unsaved draft when a save of the earlier draft finishes', async () => {
    const f = fixture(), write = deferred<ReturnType<typeof template>>()
    f.control.updateEmailTemplate.mockReturnValueOnce(write.promise)
    const wrapper = mount(EmailTemplateEditor, { global: f.global })
    await flushPromises()
    await button(wrapper, 'admin.settings.emailTemplates.save').trigger('click')
    await wrapper.get('#email-template-subject').setValue('New unsaved subject')
    write.resolve(template())
    await flushPromises()
    expect((wrapper.get('#email-template-subject').element as HTMLInputElement).value).toBe('New unsaved subject')
    expect(f.control.updateEmailTemplate.mock.calls[0][2].subject).toBe('auth.verify_code:en')
    expect(f.notifications.showSuccess).toHaveBeenCalledOnce()
  })

  it('drops a late preview after editing and never replays the save on failure', async () => {
    const f = fixture(), preview = deferred<{ subject: string; html: string }>()
    const wrapper = mount(EmailTemplateEditor, { global: f.global })
    await flushPromises()
    f.control.previewEmailTemplate.mockReturnValueOnce(preview.promise)
    await button(wrapper, 'admin.settings.emailTemplates.preview').trigger('click')
    await wrapper.get('#email-template-subject').setValue('Current draft')
    preview.resolve({ subject: 'Stale preview', html: '<p>Stale preview</p>' })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Stale preview')
    f.control.updateEmailTemplate.mockRejectedValueOnce({ status: 0, code: 'ERR_NETWORK', message: 'Uncertain result' })
    await button(wrapper, 'admin.settings.emailTemplates.save').trigger('click')
    await flushPromises()
    expect(f.control.updateEmailTemplate).toHaveBeenCalledTimes(1)
    expect((wrapper.get('#email-template-subject').element as HTMLInputElement).value).toBe('Current draft')
    expect(f.notifications.showError).toHaveBeenCalledWith('Uncertain result')
    expect(button(wrapper, 'admin.settings.emailTemplates.save').attributes('disabled')).toBeUndefined()
  })

  it.each(['actor', 'unmount'] as const)('rejects late template reads after %s changes', async transition => {
    const f = fixture(), read = deferred<ReturnType<typeof template>>()
    f.control.getEmailTemplate.mockReturnValueOnce(read.promise)
    const wrapper = mount(EmailTemplateEditor, { global: f.global })
    await flushPromises()
    const signal = f.control.getEmailTemplate.mock.calls[0][2]?.signal as AbortSignal
    if (transition === 'actor') f.scope.value = 'actor-b'
    else wrapper.unmount()
    await flushPromises()
    read.resolve({ ...template(), subject: 'Old actor private data' })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(f.notifications.showError).not.toHaveBeenCalled()
    if (transition === 'actor') expect((wrapper.get('#email-template-subject').element as HTMLInputElement).value).not.toBe('Old actor private data')
  })

  it('silently cancels a denied step-up continuation and keeps the editable draft', async () => {
    const f = fixture()
    f.control.updateEmailTemplate.mockRejectedValueOnce({ code: 'STEP_UP_CANCELLED' })
    const wrapper = mount(EmailTemplateEditor, { global: f.global })
    await flushPromises()
    await wrapper.get('#email-template-subject').setValue('Retain this draft')
    await button(wrapper, 'admin.settings.emailTemplates.save').trigger('click')
    await flushPromises()
    expect(f.notifications.showError).not.toHaveBeenCalled()
    expect(f.control.updateEmailTemplate).toHaveBeenCalledTimes(1)
    expect((wrapper.get('#email-template-subject').element as HTMLInputElement).value).toBe('Retain this draft')
  })
})

describe('independent secret and SMTP commands', () => {
  it('invalidates step-up before retrying a key rotation after an actor change', async () => {
    const f = fixture()
    f.control.regenerateAdminApiKey.mockRejectedValueOnce({ status: 403, reason: 'STEP_UP_REQUIRED' })
    const wrapper = mount(AdminKeysSettingsPanel, { global: f.global })
    await flushPromises()
    const action = wrapper.findAll('button').find(item => item.text().includes('regenerate'))
    if (!action) throw new Error('Key rotation action is missing')
    await action.trigger('click')
    await flushPromises()
    expect(f.stepUp.visible.value).toBe(true)
    f.scope.value = 'actor-b'
    await flushPromises()
    f.stepUp.onVerified()
    await flushPromises()
    expect(f.control.regenerateAdminApiKey).toHaveBeenCalledTimes(1)
    expect(f.stepUp.visible.value).toBe(false)
    expect(f.notifications.showSuccess).not.toHaveBeenCalled()
    expect(f.notifications.showError).not.toHaveBeenCalled()
  })

  it('never displays a regenerated secret after unmount', async () => {
    const f = fixture(), write = deferred<{ key: string }>()
    f.control.regenerateAdminApiKey.mockReturnValueOnce(write.promise)
    const wrapper = mount(AdminKeysSettingsPanel, { global: f.global })
    await flushPromises()
    const action = wrapper.findAll('button').find(item => item.text().includes('regenerate'))
    if (!action) throw new Error('Key rotation action is missing')
    await action.trigger('click')
    const signal = f.control.regenerateAdminApiKey.mock.calls[0][0].signal as AbortSignal
    wrapper.unmount()
    write.resolve({ key: 'old-private-secret' })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(f.notifications.showSuccess).not.toHaveBeenCalled()
  })

  it('accepts an incoming SMTP baseline without emitting old fields as dirty patches', async () => {
    const f = fixture()
    const draft: SmtpDraft = { email_verify_enabled: true, smtp_host: 'old.example.test', smtp_port: 587, smtp_username: 'old', smtp_password: '', smtp_password_configured: true, smtp_from_email: 'old@example.test', smtp_from_name: 'Old', smtp_use_tls: true }
    const wrapper = mount(SmtpSettingsPanel, { props: { modelValue: readonly(draft) }, global: f.global })
    await wrapper.setProps({ modelValue: readonly({ ...draft, smtp_host: 'new.example.test', smtp_username: 'new', smtp_from_name: 'New' }) })
    expect(wrapper.emitted('patch')).toBeUndefined()
    expect(draft.smtp_host).toBe('old.example.test')
    expect(wrapper.findAll('input').some(input => (input.element as HTMLInputElement).value === 'new.example.test')).toBe(true)
  })

  it('aborts SMTP test feedback after unmount without changing the readonly draft', async () => {
    const f = fixture(), request = deferred<{ message: string }>()
    f.control.testSmtpConnection.mockReturnValueOnce(request.promise)
    const draft: SmtpDraft = { email_verify_enabled: true, smtp_host: 'smtp.example.test', smtp_port: 587, smtp_username: 'fixture', smtp_password: '', smtp_password_configured: true, smtp_from_email: 'sender@example.test', smtp_from_name: 'Fixture', smtp_use_tls: true }
    const wrapper = mount(SmtpSettingsPanel, { props: { modelValue: readonly(draft) }, global: f.global })
    const action = wrapper.findAll('button')[0]
    await action.trigger('click')
    const signal = f.control.testSmtpConnection.mock.calls[0][1].signal as AbortSignal
    wrapper.unmount()
    request.resolve({ message: 'Old actor success' })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(f.notifications.showSuccess).not.toHaveBeenCalled()
    expect(wrapper.emitted('patch')).toBeUndefined()
    expect(draft.smtp_password).toBe('')
  })
})
