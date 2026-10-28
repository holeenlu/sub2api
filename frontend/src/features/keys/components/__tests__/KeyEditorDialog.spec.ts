import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import KeyEditorDialog from '../KeyEditorDialog.vue'
import { keysServicesKey } from '../../services'
import { createKeysTestServices } from '../../__tests__/services'
import { keyFixture } from '../../__tests__/keyFixture'
const { update, create } = vi.hoisted(() => ({ update: vi.fn(), create: vi.fn() }))
vi.mock('../../api/keys', () => ({ keysAPI: { update, create } }))
vi.mock('@/i18n', () => ({ i18n: { global: { t: (key: string) => key } }, getLocale: () => 'en-US' }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
function render() {
  const scopeKey = ref<string | null>('user:1|session:1')
  const services = createKeysTestServices({ scopeKey })
  const row = keyFixture()
  const wrapper = mount(KeyEditorDialog, {
    props: { show: true, apiKey: row, groupOptions: [] },
    global: { provide: { [keysServicesKey as symbol]: services }, stubs: {
      BaseDialog: { template: '<div><slot/><slot name="footer"/></div>' }, ConfirmDialog: true,
      KeyIdentityFields: { props: ['modelValue'], emits: ['update:modelValue'], template: '<button data-test="rename" @click="$emit(\'update:modelValue\', { ...modelValue, name: \'Edited\' })">Edit</button>' },
      KeyRestrictionFields: true, KeyExpirationFields: true
    } }
  })
  return { wrapper, scopeKey, services, row }
}
beforeEach(() => { update.mockReset(); create.mockReset() })
describe('Key editor owns a detached draft and captured command', () => {
  it('keeps the original row intact and retains the draft after server rejection', async () => {
    update.mockRejectedValue({ response: { data: { detail: 'Group not available' } } })
    const { wrapper, row } = render()
    await wrapper.get('[data-test="rename"]').trigger('click')
    expect(row.name).toBe('Client')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(update.mock.calls[0][1].name).toBe('Edited')
    expect(wrapper.get('[role="alert"]').text()).toBe('Group not available')
    expect(wrapper.emitted('saved')).toBeUndefined()
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(update.mock.calls[1][1].name).toBe('Edited')
    wrapper.unmount()
  })
  it('aborts a pending mutation and suppresses success after session change', async () => {
    let finish: (value: unknown) => void = () => undefined
    update.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const { wrapper, scopeKey, services } = render()
    await wrapper.get('form').trigger('submit')
    const signal = update.mock.calls[0][2].signal
    scopeKey.value = 'user:1|session:2'
    expect(signal.aborted).toBe(true)
    finish(keyFixture()); await flushPromises()
    expect(wrapper.emitted('saved')).toBeUndefined(); expect(services.notifySuccess).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('rejects the previous editor response after selecting another key', async () => {
    let finish: (value: unknown) => void = () => undefined
    update.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const { wrapper } = render()
    await wrapper.get('form').trigger('submit')
    await wrapper.setProps({ apiKey: keyFixture(2) })
    finish(keyFixture()); await flushPromises()
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(update.mock.calls[0][2].signal.aborted).toBe(true)
    wrapper.unmount()
  })
})
