import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import AccountModelReferencesDialog from '../AccountModelReferencesDialog.vue'
import BaseDialog from '@/shared/ui/BaseDialog.vue'
import ModelWhitelistSelector from '@/features/resources/components/ModelWhitelistSelector.vue'
import { resourceEditorServicesKey } from '@/features/resources/services'
import { editorTestServices } from '@/features/resources/__tests__/editorTestServices'
import { apiClient } from '@/shared/http'
import { uiFeedbackKey } from '@/shared/ui/feedback'

enableAutoUnmount(afterEach)
const { getById } = vi.hoisted(() => ({ getById: vi.fn() }))
const actor = reactive({ identityScopeKey: 'actor-a', isAuthenticated: true, isSuperAdmin: false, isSimpleMode: false, permissions: ['accounts.manage'], can: (permission: string) => actor.permissions.includes(permission) })
vi.mock('@/features/identity', () => ({ useAuthStore: () => actor }))
vi.mock('@/features/resources/accounts', () => ({ accountsAPI: { getById } }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const account = { id: 42, platform: 'openai', credentials: { model_allowlist: ['gpt-old'], model_mapping: { 'rewrite-source': 'rewrite-target' } } }
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(yes => { resolve = yes })
  return { promise, resolve }
}
async function settle() { await vi.dynamicImportSettled(); await flushPromises() }
function setup(props = {}) {
  const services = editorTestServices()
  services.managedModelCatalog = vi.fn().mockResolvedValue([
    { id: 'gpt-old', display_name: 'Old model', enabled: true },
    { id: 'gpt-new', display_name: 'New model', enabled: true }
  ])
  const wrapper = mount(AccountModelReferencesDialog, {
    attachTo: document.body,
    props: { show: true, accountId: 42, platform: 'openai', ...props },
    global: { provide: { [resourceEditorServicesKey as symbol]: services, [uiFeedbackKey as symbol]: { success: vi.fn(), error: vi.fn() } } }
  })
  return { wrapper, services }
}
async function selectNew(wrapper: ReturnType<typeof setup>['wrapper']) {
  const selector = wrapper.getComponent(ModelWhitelistSelector)
  await selector.get('[role="button"]').trigger('click')
  expect(selector.findAll('[data-testid="select-model"]')[1].attributes('disabled')).toBeUndefined()
  await selector.findAll('[data-testid="select-model"]')[1].trigger('click')
  await flushPromises()
}
beforeEach(() => {
  vi.clearAllMocks()
  Object.assign(actor, { identityScopeKey: 'actor-a', isAuthenticated: true, permissions: ['accounts.manage'] })
  getById.mockResolvedValue(account)
  vi.spyOn(apiClient, 'put').mockResolvedValue({ data: { models: ['gpt-old', 'gpt-new'] } })
})
afterEach(() => vi.restoreAllMocks())

describe('independent account model reference dialog', () => {
  it('reads the full account before mounting the immediate CAS consumer and sends the dedicated PUT', async () => {
    const read = deferred<typeof account>()
    getById.mockReturnValueOnce(read.promise)
    const { wrapper, services } = setup()
    expect(wrapper.findComponent(ModelWhitelistSelector).exists()).toBe(false)
    expect(services.managedModelCatalog).not.toHaveBeenCalled()
    expect(apiClient.put).not.toHaveBeenCalled()
    read.resolve(account)
    await settle()
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    expect(selector.props()).toMatchObject({ accountId: 42, saveMode: 'immediate', modelValue: ['gpt-old'], platform: 'openai' })
    await selectNew(wrapper)
    expect(apiClient.put).toHaveBeenCalledTimes(1)
    expect(apiClient.put).toHaveBeenCalledWith('/admin/models/accounts/42/allowlist', { expected: ['gpt-old'], models: ['gpt-old', 'gpt-new'] }, { signal: expect.any(AbortSignal) })
    expect(wrapper.emitted('saved')).toEqual([[42]])
  })

  it('ignores rewrite mappings when the explicit allowlist is inherit', async () => {
    getById.mockResolvedValue({ ...account, credentials: { model_allowlist: null, model_mapping: { from: 'to' } } })
    const { wrapper } = setup()
    await settle()
    expect(wrapper.getComponent(ModelWhitelistSelector).props('modelValue')).toEqual([])
    await wrapper.getComponent(ModelWhitelistSelector).get('[data-testid="inherit-models"]').trigger('click')
    await flushPromises()
    expect(apiClient.put).toHaveBeenCalledWith(expect.any(String), { expected: [], models: [] }, expect.any(Object))
  })

  it('does not publish success or blindly retry a conflicting CAS', async () => {
    vi.mocked(apiClient.put).mockRejectedValue({ response: { data: { message: 'references changed' } } })
    const { wrapper } = setup()
    await settle()
    await selectNew(wrapper)
    expect(wrapper.getComponent(ModelWhitelistSelector).get('[role="alert"]').text()).toContain('references changed')
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(apiClient.put).toHaveBeenCalledTimes(1)
    expect(getById).toHaveBeenCalledTimes(1)
  })

  it.each(['risk.manage', 'models.manage', 'accounts.read'])('does not read or write with only %s', async permission => {
    actor.permissions = [permission]
    const { wrapper, services } = setup()
    await settle()
    expect(wrapper.emitted('close')).toBeDefined()
    expect(getById).not.toHaveBeenCalled()
    expect(services.managedModelCatalog).not.toHaveBeenCalled()
    expect(apiClient.put).not.toHaveBeenCalled()
  })

  it('cancels stale hydration when the target changes and rejects mismatched account details', async () => {
    const read = deferred<typeof account>()
    getById.mockReturnValueOnce(read.promise).mockResolvedValueOnce({ ...account, id: 43 })
    const { wrapper } = setup()
    const signal = getById.mock.calls[0][1].signal as AbortSignal
    await wrapper.setProps({ accountId: 43 })
    expect(signal.aborted).toBe(true)
    await settle()
    read.resolve({ ...account, credentials: { model_allowlist: ['wrong-account'], model_mapping: {} } })
    await settle()
    expect(wrapper.getComponent(ModelWhitelistSelector).props('accountId')).toBe(43)
    expect(wrapper.getComponent(ModelWhitelistSelector).props('modelValue')).toEqual(['gpt-old'])
    await wrapper.setProps({ accountId: 44 })
    await settle()
    expect(wrapper.findComponent(ModelWhitelistSelector).exists()).toBe(false)
    expect(new DOMWrapper(document.body).get('[role="alert"]').text()).toContain('scopeMismatch')
  })

  it('closes and cancels a pending account read when the actor identity changes', async () => {
    const read = deferred<typeof account>()
    getById.mockReturnValueOnce(read.promise)
    const { wrapper } = setup()
    const signal = getById.mock.calls[0][1].signal as AbortSignal
    actor.isAuthenticated = false
    await settle()
    expect(signal.aborted).toBe(true)
    expect(wrapper.emitted('close')).toBeDefined()
    read.resolve(account)
    await settle()
    expect(wrapper.findComponent(ModelWhitelistSelector).exists()).toBe(false)
    expect(apiClient.put).not.toHaveBeenCalled()
  })

  it('blocks the close button, Escape and close handler during a real pending PUT', async () => {
    const write = deferred<{ data: { models: string[] } }>()
    vi.mocked(apiClient.put).mockReturnValueOnce(write.promise)
    const { wrapper } = setup()
    await settle()
    await selectNew(wrapper)
    const dialog = wrapper.getComponent(BaseDialog)
    expect(dialog.props()).toMatchObject({ closeOnEscape: false, showCloseButton: false })
    const overlay = wrapper.getComponent(ModelWhitelistSelector).element.closest('[role="dialog"]')
    expect(overlay?.querySelector<HTMLButtonElement>('button[aria-label="ui.closeModal"]')?.disabled).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    dialog.vm.$emit('close')
    await settle()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(vi.mocked(apiClient.put).mock.calls[0][2]?.signal?.aborted).toBe(false)
    write.resolve({ data: { models: ['gpt-old', 'gpt-new'] } })
    await settle()
    expect(wrapper.emitted('saved')).toEqual([[42]])
    expect(dialog.props()).toMatchObject({ closeOnEscape: true, showCloseButton: true })
    const confirm = vi.spyOn(window, 'confirm')
    dialog.vm.$emit('close')
    await settle()
    expect(confirm).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toEqual([[]])
  })

  it('requires explicit discard to close a failed CAS draft and never retries the command', async () => {
    vi.mocked(apiClient.put).mockRejectedValue({ response: { data: { message: 'CAS conflict' } } })
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { wrapper } = setup()
    await settle()
    await selectNew(wrapper)
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    await settle()
    expect(confirm).toHaveBeenCalledWith('admin.accounts.modelReference.discardFailedDraft')
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.getComponent(ModelWhitelistSelector).text()).toContain('gpt-new')
    confirm.mockReturnValue(true)
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    await settle()
    expect(wrapper.emitted('close')).toEqual([[]])
    expect(apiClient.put).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('saved')).toBeUndefined()
  })

  it.each(['revoke', 'identity', 'unmount'] as const)('cancels a pending PUT on %s and ignores late success', async mode => {
    const write = deferred<{ data: { models: string[] } }>()
    vi.mocked(apiClient.put).mockReturnValueOnce(write.promise)
    const { wrapper } = setup()
    await settle()
    await selectNew(wrapper)
    const signal = vi.mocked(apiClient.put).mock.calls[0][2]?.signal
    if (mode === 'identity') actor.identityScopeKey = 'actor-b'
    else if (mode === 'revoke') actor.permissions = ['risk.read', 'risk.manage']
    else wrapper.unmount()
    await settle()
    expect(signal?.aborted).toBe(true)
    write.resolve({ data: { models: ['gpt-old', 'gpt-new'] } })
    await settle()
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(apiClient.put).toHaveBeenCalledTimes(1)
    if (mode === 'revoke') expect(wrapper.emitted('close')).toBeDefined()
  })
})
