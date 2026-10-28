import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
import { resourceEditorServicesKey, type ResourceManagedModel } from '../../services'
import { editorTestServices } from '../../__tests__/editorTestServices'
import { accountFixture } from '../../__tests__/accountFixture'
import { saveAccountModels } from '../../api/accountModels'
import { accountsAPI } from '../../accounts'

vi.mock('../../api/accountModels', () => ({ saveAccountModels: vi.fn() }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('../../accounts', () => ({ accountsAPI: { getById: vi.fn() } }))
const copy = vi.hoisted(() => vi.fn())
vi.mock('@/shared/lib/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: copy }) }))
const rows = (): ResourceManagedModel[] => [
  { id: 'seed', display_name: 'Seed model', enabled: true },
  { id: 'new', display_name: 'New model', enabled: true },
  { id: 'disabled', display_name: 'Disabled model', enabled: false },
]
const wrappers: VueWrapper[] = []
function deferred<T>() { let resolve: (value: T) => void = () => { throw new Error('Deferred promise not initialized') }; const promise = new Promise<T>(accept => { resolve = accept }); return { promise, resolve } }
function required<T>(value: T | null | undefined): T { if (value == null) throw new Error('Expected test value'); return value }
function setup(props: Partial<InstanceType<typeof ModelWhitelistSelector>['$props']> = {}, canManage = true, attrs: Record<string, unknown> = {}, canReadModelCatalog?: boolean) {
  const access = reactive({ isSuperAdmin: true, isSimpleMode: false, canAuthorize: true, canManage, canReadModelCatalog })
  const scope = reactive({ value: 'console:admin-a' })
  const services = editorTestServices()
  services.access = access; services.scopeKey = () => scope.value
  services.managedModelCatalog = vi.fn().mockResolvedValue(rows())
  const wrapper = mount(ModelWhitelistSelector, {
    attrs,
    props: { modelValue: [], platform: 'openai', ...props },
    global: { provide: { [resourceEditorServicesKey as symbol]: services }, stubs: { ModelIcon: true } }
  })
  wrappers.push(wrapper)
  return { wrapper, services, access, scope }
}
function option(wrapper: VueWrapper, id: string) {
  return required(wrapper.findAll('[data-testid="model-option"]').find(row => row.text().includes(id))).get('[data-testid="select-model"]')
}
async function open(wrapper: VueWrapper) { await flushPromises(); await wrapper.get('[role="button"]').trigger('click') }
beforeEach(() => vi.clearAllMocks())
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

describe('managed account model references', () => {
  it('shows seed inheritance using only enabled catalog entries; never fills the parent', async () => {
    const { wrapper } = setup()
    await open(wrapper)
    expect(option(wrapper, 'seed').attributes('aria-pressed')).toBe('true')
    expect(option(wrapper, 'disabled').attributes('aria-pressed')).toBe('false')
    expect(option(wrapper, 'disabled').attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.find('input[placeholder="admin.accounts.enterCustomModelName"]').exists()).toBe(false)
  })
  it('immediately reports idle state and keeps busy/draft events truthful through a confirmed save', async () => {
    const request = deferred<{ models: string[] }>()
    vi.mocked(saveAccountModels).mockReturnValueOnce(request.promise)
    const { wrapper } = setup({ modelValue: ['seed'], accountId: 7 })
    expect(wrapper.emitted('busy-change')).toEqual([[false]])
    expect(wrapper.emitted('draft-change')).toEqual([[false]])
    await open(wrapper); await option(wrapper, 'new').trigger('click')
    expect(wrapper.emitted('busy-change')).toEqual([[false], [true]])
    expect(wrapper.emitted('draft-change')).toEqual([[false], [true]])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    request.resolve({ models: ['seed', 'new'] }); await flushPromises()
    expect(wrapper.emitted('busy-change')).toEqual([[false], [true], [false]])
    expect(wrapper.emitted('draft-change')).toEqual([[false], [true], [false]])
  })
  it('reports a retained failed draft after busy ends, and clears it only after explicit recovery', async () => {
    vi.mocked(saveAccountModels).mockRejectedValueOnce({ status: 409, message: 'Version conflict' })
    vi.mocked(accountsAPI.getById).mockResolvedValueOnce(accountFixture({ id: 7, credentials: { model_allowlist: ['remote'] } }))
    const { wrapper } = setup({ modelValue: ['seed'], accountId: 7 })
    await open(wrapper); await option(wrapper, 'new').trigger('click'); await flushPromises()
    expect(wrapper.emitted('busy-change')?.at(-1)).toEqual([false])
    expect(wrapper.emitted('draft-change')).toEqual([[false], [true]])
    await wrapper.get('[data-testid="reload-current-models"]').trigger('click'); await flushPromises()
    expect(wrapper.emitted('draft-change')?.at(-1)).toEqual([true])
    await wrapper.get('[data-testid="adopt-current-models"]').trigger('click')
    expect(wrapper.emitted('draft-change')?.at(-1)).toEqual([false])
  })
  it('resets both public lifecycle signals on unmount and ignores late completion', async () => {
    const request = deferred<{ models: string[] }>()
    vi.mocked(saveAccountModels).mockReturnValueOnce(request.promise)
    const { wrapper } = setup({ modelValue: ['seed'], accountId: 7 })
    await open(wrapper); await option(wrapper, 'new').trigger('click')
    wrapper.unmount()
    expect(wrapper.emitted('busy-change')?.at(-1)).toEqual([false])
    expect(wrapper.emitted('draft-change')?.at(-1)).toEqual([false])
    const busyEvents = [...(wrapper.emitted('busy-change') ?? [])]
    const draftEvents = [...(wrapper.emitted('draft-change') ?? [])]
    request.resolve({ models: ['stale'] }); await flushPromises()
    expect(wrapper.emitted('busy-change')).toEqual(busyEvents)
    expect(wrapper.emitted('draft-change')).toEqual(draftEvents)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
  it('never forwards retired credential or mapping attributes to the DOM', async () => {
    const { wrapper } = setup({}, true, {
      syncCredentials: { api_key: 'fixture-secret-key', base_url: 'https://private.example' },
      'sync-credentials': { api_key: 'fixture-secret-key' },
      modelMappings: [{ from: 'private-reference', to: 'private-target' }],
      syncAccountIds: () => Promise.resolve([42]), onUpstreamSynced: () => undefined
    })
    await flushPromises()
    for (const name of ['synccredentials', 'sync-credentials', 'modelmappings', 'syncaccountids', 'onupstreamsynced']) {
      expect(wrapper.attributes(name)).toBeUndefined()
    }
    expect(wrapper.html()).not.toContain('fixture-secret-key')
    expect(wrapper.html()).not.toContain('private-reference')
  })
  it('BPS explicit empty lists select nothing, add only the clicked ID, and never save native allowlists', async () => {
    const native = setup({ modelValue: ['seed'] })
    const bps = setup({ modelValue: [], emptyMode: 'explicit', accountId: 7 })
    await open(native.wrapper); await open(bps.wrapper)
    expect(option(native.wrapper, 'seed').attributes('aria-pressed')).toBe('true')
    expect(option(bps.wrapper, 'seed').attributes('aria-pressed')).toBe('false')
    expect(option(bps.wrapper, 'new').attributes('aria-pressed')).toBe('false')
    expect(bps.wrapper.find('[data-testid="inherit-models"]').exists()).toBe(false)
    expect(bps.wrapper.text()).toContain('admin.accounts.modelReference.explicitHint')
    await option(bps.wrapper, 'new').trigger('click')
    expect(bps.wrapper.emitted('update:modelValue')).toEqual([[['new']]])
    await bps.wrapper.setProps({ modelValue: ['new'] })
    await option(bps.wrapper, 'new').trigger('click')
    expect(bps.wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
    await bps.wrapper.setProps({ modelValue: [] })
    await bps.wrapper.get('[data-testid="refresh-catalog"]').trigger('click'); await flushPromises()
    expect(option(bps.wrapper, 'seed').attributes('aria-pressed')).toBe('false')
    expect(option(bps.wrapper, 'new').attributes('aria-pressed')).toBe('false')
    expect(native.wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(saveAccountModels).not.toHaveBeenCalled()
  })
  it('removes historical disabled and missing references but cannot add them again', async () => {
    const { wrapper } = setup({ modelValue: ['seed', 'disabled', 'missing'] })
    await open(wrapper)
    expect(option(wrapper, 'disabled').attributes('disabled')).toBeUndefined()
    await option(wrapper, 'disabled').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['seed', 'missing']])
    await wrapper.setProps({ modelValue: ['seed', 'missing'] })
    expect(option(wrapper, 'disabled').attributes('disabled')).toBeDefined()
    await option(wrapper, 'missing').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['seed']])
  })
  it('adds an enabled model, searches display names, and copies the exact ID', async () => {
    const { wrapper } = setup({ modelValue: ['seed'] })
    await open(wrapper)
    await option(wrapper, 'new').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['seed', 'new']])
    await wrapper.get('input').setValue('NEW MODEL')
    expect(wrapper.findAll('[data-testid="model-option"]')).toHaveLength(1)
    await wrapper.get('[data-testid="copy-model-id"]').trigger('click')
    expect(copy).toHaveBeenCalledWith('new')
  })
  it('refreshes the directory without reselecting a cancelled entry or adding new entries', async () => {
    const { wrapper, services } = setup({ modelValue: ['seed'] })
    await open(wrapper)
    vi.mocked(services.managedModelCatalog).mockResolvedValue([...rows(), { id: 'later', enabled: true, display_name: 'Later' }])
    await wrapper.get('[data-testid="refresh-catalog"]').trigger('click'); await flushPromises()
    expect(option(wrapper, 'new').attributes('aria-pressed')).toBe('false')
    expect(option(wrapper, 'later').attributes('aria-pressed')).toBe('false')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
  it('bulk selection allows only the enabled intersection across platforms', async () => {
    const h = setup({ platforms: ['anthropic', 'openai'], modelValue: ['seed'] })
    vi.mocked(h.services.managedModelCatalog).mockImplementation(async platform => platform === 'anthropic' ? rows() : rows().filter(row => row.id !== 'new'))
    await h.wrapper.get('[data-testid="refresh-catalog"]').trigger('click'); await open(h.wrapper)
    expect(option(h.wrapper, 'new').attributes('disabled')).toBeDefined()
  })
  it('uses the saved CAS reference, waits for confirmation, and adopts null inheritance', async () => {
    const request = deferred<{ models: string[] | null }>()
    vi.mocked(saveAccountModels).mockReturnValueOnce(request.promise).mockResolvedValueOnce({ models: null })
    const { wrapper } = setup({ modelValue: ['seed'], accountId: 7 })
    await open(wrapper); await option(wrapper, 'new').trigger('click')
    expect(saveAccountModels).toHaveBeenCalledWith(7, ['seed'], ['seed', 'new'], { signal: expect.any(AbortSignal) })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(option(wrapper, 'seed').attributes('disabled')).toBeDefined()
    request.resolve({ models: ['seed', 'new'] }); await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['seed', 'new']]])
    await wrapper.setProps({ modelValue: ['seed', 'new'] })
    await wrapper.get('[data-testid="inherit-models"]').trigger('click'); await flushPromises()
    expect(saveAccountModels).toHaveBeenLastCalledWith(7, ['seed', 'new'], [], { signal: expect.any(AbortSignal) })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
  })
  it('uses a later-loaded saved reference as expected instead of the initial empty value', async () => {
    vi.mocked(saveAccountModels).mockResolvedValueOnce({ models: ['seed', 'new'] })
    const { wrapper } = setup({ accountId: 7 })
    await open(wrapper); await wrapper.setProps({ modelValue: ['seed'] })
    await option(wrapper, 'new').trigger('click'); await flushPromises()
    expect(saveAccountModels).toHaveBeenCalledWith(7, ['seed'], ['seed', 'new'], { signal: expect.any(AbortSignal) })
  })
  it('retains a conflict draft with an explicit failure and never reports optimistic success', async () => {
    vi.mocked(saveAccountModels).mockRejectedValue({ status: 409, message: 'Version conflict' })
    const { wrapper, services } = setup({ modelValue: ['seed'], accountId: 7 })
    await open(wrapper); await option(wrapper, 'new').trigger('click'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Version conflict')
    expect(option(wrapper, 'new').attributes('aria-pressed')).toBe('true')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(services.notices.showSuccess).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="reload-current-models"]').exists()).toBe(true)
    expect(saveAccountModels).toHaveBeenCalledTimes(1)
  })
  it('recovers a 409 by reading current refs and explicitly merging only local changes', async () => {
    vi.mocked(saveAccountModels).mockRejectedValueOnce({ status: 409, message: 'Version conflict' }).mockResolvedValueOnce({ models: ['seed', 'remote', 'new'] })
    vi.mocked(accountsAPI.getById).mockResolvedValueOnce(accountFixture({ id: 7, credentials: { model_allowlist: ['seed', 'remote'], model_mapping: { old: 'old' } } }))
    const { wrapper, services } = setup({ modelValue: ['seed'], accountId: 7 })
    await open(wrapper); await option(wrapper, 'new').trigger('click'); await flushPromises()
    await wrapper.get('[data-testid="reload-current-models"]').trigger('click'); await flushPromises()
    expect(accountsAPI.getById).toHaveBeenCalledWith(7, { signal: expect.any(AbortSignal) })
    expect(wrapper.get('[data-testid="server-models"]').text()).toContain('seed, remote')
    expect(option(wrapper, 'new').attributes('aria-pressed')).toBe('true')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(services.notices.showInfo).toHaveBeenCalledWith('admin.accounts.modelReference.reloadUnconfirmed')
    expect(saveAccountModels).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid="merge-model-draft"]').trigger('click'); await flushPromises()
    expect(saveAccountModels).toHaveBeenLastCalledWith(7, ['seed', 'remote'], ['seed', 'remote', 'new'], { signal: expect.any(AbortSignal) })
    expect(wrapper.emitted('update:modelValue')).toEqual([[['seed', 'remote', 'new']]])
  })
  it('can explicitly discard the failed draft and adopt current inheritance without another write', async () => {
    vi.mocked(saveAccountModels).mockRejectedValueOnce({ status: 409, message: 'Version conflict' })
    vi.mocked(accountsAPI.getById).mockResolvedValueOnce(accountFixture({ id: 7, credentials: { model_allowlist: null, model_mapping: { old: 'old' } } }))
    const { wrapper } = setup({ modelValue: ['seed'], accountId: 7 })
    await open(wrapper); await option(wrapper, 'new').trigger('click'); await flushPromises()
    await wrapper.get('[data-testid="reload-current-models"]').trigger('click'); await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-testid="adopt-current-models"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[[]]])
    expect(saveAccountModels).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
  it('does not cancel a busy write or permit another write through refresh', async () => {
    const request = deferred<{ models: string[] }>()
    vi.mocked(saveAccountModels).mockReturnValueOnce(request.promise)
    const { wrapper, services } = setup({ modelValue: ['seed'], accountId: 7 })
    await open(wrapper); await option(wrapper, 'new').trigger('click')
    const signal = required(vi.mocked(saveAccountModels).mock.calls[0]?.[3]?.signal)
    wrapper.get('[data-testid="refresh-catalog"]').element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await option(wrapper, 'seed').trigger('click'); await flushPromises()
    expect(signal.aborted).toBe(false)
    expect(services.managedModelCatalog).toHaveBeenCalledTimes(1)
    expect(saveAccountModels).toHaveBeenCalledTimes(1)
    request.resolve({ models: ['seed', 'new'] }); await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['seed', 'new']]])
  })
  it.each(['account', 'scope', 'permission'] as const)('aborts private recovery GET and ignores its result after %s changes', async transition => {
    vi.mocked(saveAccountModels).mockRejectedValueOnce({ status: 409, message: 'Version conflict' })
    const request = deferred<Awaited<ReturnType<typeof accountsAPI.getById>>>()
    vi.mocked(accountsAPI.getById).mockReturnValueOnce(request.promise)
    const h = setup({ modelValue: ['seed'], accountId: 7 })
    await open(h.wrapper); await option(h.wrapper, 'new').trigger('click'); await flushPromises()
    await h.wrapper.get('[data-testid="reload-current-models"]').trigger('click')
    const signal = required(vi.mocked(accountsAPI.getById).mock.calls[0]?.[1]?.signal)
    if (transition === 'account') await h.wrapper.setProps({ accountId: 8 })
    if (transition === 'scope') h.scope.value = 'other-scope'
    if (transition === 'permission') h.access.canManage = false
    await flushPromises(); expect(signal.aborted).toBe(true)
    request.resolve(accountFixture({ id: 7, credentials: { model_allowlist: ['private-old-scope'] } }))
    await flushPromises()
    expect(h.wrapper.find('[data-testid="server-models"]').exists()).toBe(false)
    expect(h.wrapper.emitted('update:modelValue')).toBeUndefined()
  })
  it.each(['account', 'scope', 'permission', 'unmount'] as const)('aborts a save and rejects late results after %s changes', async transition => {
    const request = deferred<{ models: string[] }>()
    vi.mocked(saveAccountModels).mockReturnValueOnce(request.promise)
    const h = setup({ modelValue: ['seed'], accountId: 7 })
    await open(h.wrapper); await option(h.wrapper, 'new').trigger('click')
    const signal = required(vi.mocked(saveAccountModels).mock.calls[0]?.[3]?.signal)
    if (transition === 'account') await h.wrapper.setProps({ accountId: 8, modelValue: ['new'] })
    if (transition === 'scope') h.scope.value = 'supplier:read-only'
    if (transition === 'permission') h.access.canManage = false
    if (transition === 'unmount') h.wrapper.unmount()
    await flushPromises()
    expect(signal.aborted).toBe(true)
    request.resolve({ models: ['stale'] }); await flushPromises()
    expect(h.wrapper.emitted('update:modelValue')).toBeUndefined()
  })
  it('allows the independent account CAS with manage permission even without credential authorization', async () => {
    vi.mocked(saveAccountModels).mockResolvedValueOnce({ models: ['seed', 'new'] })
    const h = setup({ accountId: 7, modelValue: ['seed'] })
    h.access.canAuthorize = false
    await open(h.wrapper); await option(h.wrapper, 'new').trigger('click'); await flushPromises()
    expect(saveAccountModels).toHaveBeenCalledWith(7, ['seed'], ['seed', 'new'], { signal: expect.any(AbortSignal) })
    expect(h.wrapper.emitted('update:modelValue')).toEqual([[['seed', 'new']]])
  })
  it('accepts consumer-specific catalog read permission without granting editor writes', async () => {
    const h = setup({ modelValue: [], emptyMode: 'explicit' }, false, {}, true)
    await open(h.wrapper)
    expect(h.services.managedModelCatalog).toHaveBeenCalled()
    expect(option(h.wrapper, 'new').attributes('disabled')).toBeDefined()
    await option(h.wrapper, 'new').trigger('click')
    expect(h.wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(saveAccountModels).not.toHaveBeenCalled()
    expect(accountsAPI.getById).not.toHaveBeenCalled()
  })
  it('accepts a risk editor scope projection without account API writes', async () => {
    const h = setup({ modelValue: [], emptyMode: 'explicit' }, true, {}, true)
    h.scope.value = 'risk-editor'
    await open(h.wrapper); await option(h.wrapper, 'new').trigger('click')
    expect(h.wrapper.emitted('update:modelValue')).toEqual([[['new']]])
    expect(saveAccountModels).not.toHaveBeenCalled()
    expect(accountsAPI.getById).not.toHaveBeenCalled()
  })
  it('does not call admin APIs in a read-only supplier scope', async () => {
    const h = setup({ accountId: 7 }, false)
    h.scope.value = 'supplier:readonly'
    await flushPromises()
    await h.wrapper.get('[data-testid="refresh-catalog"]').trigger('click'); await open(h.wrapper)
    expect(h.services.managedModelCatalog).not.toHaveBeenCalled()
    expect(saveAccountModels).not.toHaveBeenCalled()
    expect(h.wrapper.get('[data-testid="inherit-models"]').attributes('disabled')).toBeDefined()
  })
  it('aborts pending catalog reads and discards old platform results', async () => {
    const h = setup()
    await flushPromises()
    const request = deferred<ResourceManagedModel[]>()
    vi.mocked(h.services.managedModelCatalog).mockReturnValueOnce(request.promise)
    await h.wrapper.get('[data-testid="refresh-catalog"]').trigger('click')
    const signal = required(vi.mocked(h.services.managedModelCatalog).mock.calls.at(-1)?.[1]?.signal)
    await h.wrapper.setProps({ platform: 'anthropic', modelValue: ['seed'] }); await flushPromises()
    expect(signal.aborted).toBe(true)
    request.resolve([{ id: 'stale', display_name: 'Stale', enabled: true }]); await open(h.wrapper)
    expect(h.wrapper.text()).not.toContain('Stale')
  })
  it('account forms keep a draft for their atomic save rather than issuing a second write', async () => {
    const { wrapper } = setup({ modelValue: ['seed'], accountId: 7, saveMode: 'draft' })
    await open(wrapper); await option(wrapper, 'new').trigger('click')
    expect(saveAccountModels).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['seed', 'new']]])
  })
})
