import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick, reactive, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import accountsAPI from '../../accounts'
import { editorTestServices } from '../../__tests__/editorTestServices'
import { accountFixture } from '../../model/__tests__/boundaryFixtures'
import { resourceEditorServicesKey } from '../../services'
import BulkEditAccountModal from '../BulkEditAccountModal.vue'
import EditAccountModal from '../EditAccountModal.vue'

vi.mock('../../accounts', () => ({ default: { getById: vi.fn(), bulkUpdate: vi.fn(), checkMixedChannelRisk: vi.fn() } }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Promise not initialized') }
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

const wrappers: VueWrapper[] = []
const result = { success: 2, failed: 0, results: [] }
const cloudAccount = (id: number) => accountFixture({
  id, platform: 'openai', type: 'oauth',
  credentials: { chatgpt_account_id: `account-${id}`, chatgpt_user_id: `user-${id}` }
})
function mountEditor(props: Partial<InstanceType<typeof BulkEditAccountModal>['$props']> = {}) {
  const services = editorTestServices()
  const scope = ref('actor-1:admin:0')
  const access = reactive({ isSuperAdmin: true, canAuthorize: true, canManage: true, isSimpleMode: true, canEditAdminField: (_schema: string, _field: string) => true })
  services.access = access
  services.scopeKey = () => scope.value
  const wrapper = mount(BulkEditAccountModal, {
    props: { show: true, accountIds: [1, 2], selectedPlatforms: ['openai'], selectedTypes: ['oauth'], proxies: [], groups: [], ...props },
    global: {
      provide: { [resourceEditorServicesKey as symbol]: services },
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        ConfirmDialog: true, EditAccountModal: true, ModelWhitelistSelector: true,
        ProxySelector: true, GroupSelector: true, HeaderOverrideEditor: true, Icon: true,
        Select: {
          props: ['modelValue', 'options'], emits: ['update:modelValue'],
          template: '<select v-bind="$attrs" :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>'
        }
      }
    }
  })
  wrappers.push(wrapper)
  return { wrapper, scope, access, notices: services.notices }
}
const submit = async (wrapper: VueWrapper) => {
  await wrapper.get('#bulk-edit-account-form').trigger('submit')
  await flushPromises()
}
const selectConcurrency = async (wrapper: VueWrapper) => {
  await wrapper.get('#bulk-edit-concurrency-enabled').setValue(true)
  await wrapper.get('#bulk-edit-concurrency').setValue(7)
}
const confirm = (wrapper: VueWrapper) => wrapper.getComponent({ name: 'ConfirmDialog' }).vm.$emit('confirm')

beforeEach(() => {
  vi.mocked(accountsAPI.bulkUpdate).mockReset().mockResolvedValue(result)
  vi.mocked(accountsAPI.getById).mockReset().mockImplementation(async id => cloudAccount(id))
  vi.mocked(accountsAPI.checkMixedChannelRisk).mockReset().mockResolvedValue({ has_risk: false })
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.restoreAllMocks()
})

describe('bulk patch permission dependencies', () => {
  it('does not retry or invent a shared condition when bulk BPS enable conflicts', async () => {
    vi.mocked(accountsAPI.bulkUpdate).mockRejectedValueOnce({ status: 409, reason: 'OPENAI_EXCEL_BPS_STATE_CONFLICT' })
    const { wrapper, notices } = mountEditor()
    await wrapper.get('#bulk-edit-excel-bps-enabled').setValue(true)
    if (wrapper.get('[data-testid="bulk-excel-bps-toggle"]').attributes('aria-checked') !== 'true') {
      await wrapper.get('[data-testid="bulk-excel-bps-toggle"]').trigger('click')
    }
    expect(wrapper.get('[data-testid="bulk-bps-conditional-hint"]').text()).toContain('excelBPSBulkStateConflict')
    await submit(wrapper)
    expect(accountsAPI.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(notices.showError).toHaveBeenCalledWith('admin.accounts.openai.excelBPSBulkStateConflict')
    const body = vi.mocked(accountsAPI.bulkUpdate).mock.calls[0]?.[1]
    expect(body?.extra).toBeDefined()
    expect(body?.extra).not.toHaveProperty('openai_excel_bps_expected_disabled_at')
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
  })
  it('restores the model selector after transport permission is revoked and submits its explicit clear', async () => {
    const { wrapper } = mountEditor()
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-passthrough-toggle').trigger('click')
    await wrapper.setProps({ allowTransportEdit: false })
    expect(wrapper.get('#bulk-edit-model-restriction-enabled').attributes('disabled')).toBeUndefined()
    await submit(wrapper)
    expect(accountsAPI.bulkUpdate).toHaveBeenCalledWith([1, 2], { credentials: { model_allowlist: null } })
  })

  it.each(['authorize', 'credentials field'])('keeps bulk model credentials behind %s permission', async boundary => {
    const { wrapper, access } = mountEditor()
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    const selector = wrapper.getComponent({ name: 'ModelWhitelistSelector' })
    selector.vm.$emit('update:modelValue', ['stale-draft'])
    await selectConcurrency(wrapper)
    if (boundary === 'authorize') access.canAuthorize = false
    else access.canEditAdminField = () => false
    await nextTick()
    expect(wrapper.find('#bulk-edit-model-restriction-enabled').exists()).toBe(false)
    await submit(wrapper)
    expect(accountsAPI.bulkUpdate).toHaveBeenCalledWith([1, 2], { concurrency: 7 })
  })

  it('restores independent Responses selection after capability permission is revoked', async () => {
    const { wrapper } = mountEditor({ selectedTypes: ['apikey'] })
    await wrapper.get('#bulk-edit-openai-endpoint-capabilities-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-endpoint-capability-chat_completions"]').setValue(false)
    await wrapper.setProps({ allowAuthorize: false })
    expect(wrapper.get('#bulk-edit-openai-responses-mode-enabled').attributes('disabled')).toBeUndefined()
    await wrapper.get('#bulk-edit-openai-responses-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-responses-mode-select"]').setValue('force_responses')
    await submit(wrapper)
    expect(accountsAPI.bulkUpdate).toHaveBeenCalledWith([1, 2], { extra: { openai_responses_mode: 'force_responses' } })
  })
})

describe('single BPS toggle retirement', () => {
  it('starts with no selected models and sends account options without a template mode', async () => {
    const { wrapper } = mountEditor()
    await wrapper.get('#bulk-edit-excel-bps-enabled').setValue(true)
    expect(wrapper.find('[data-testid="bulk-excel-bps-modes"]').exists()).toBe(false)
    await wrapper.get('[data-testid="bulk-excel-bps-toggle"]').trigger('click')
    await submit(wrapper)
    expect(accountsAPI.bulkUpdate).toHaveBeenCalledTimes(1)
    const saved = vi.mocked(accountsAPI.bulkUpdate).mock.calls[0]?.[1]?.extra
    expect(saved).toMatchObject({ openai_excel_bps: true, openai_excel_bps_models: [] })
    expect(wrapper.get('[data-testid="bulk-excel-bps-model-selection"]').getComponent({ name: 'ModelWhitelistSelector' }).props('emptyMode')).toBe('explicit')
    expect(vi.mocked(accountsAPI.bulkUpdate).mock.calls[0]?.[1]?.credentials).toBeUndefined()
    expect(saved).not.toHaveProperty('openai_excel_bps_config_mode')
  })
})

describe('bulk account BPS settings', () => {
  it.each([true, false])('does not edit until selected and toggles without refilling the draft (show=%s)', async show => {
    const { wrapper, notices } = mountEditor({ show })
    if (!show) await wrapper.setProps({ show: true })
    await submit(wrapper)
    expect(notices.showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.noFieldsSelected')
    expect(accountsAPI.bulkUpdate).not.toHaveBeenCalled()
    await wrapper.get('#bulk-edit-excel-bps-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-excel-bps-toggle"]').trigger('click')
    const selector = wrapper.getComponent({ name: 'ModelWhitelistSelector' })
    selector.vm.$emit('update:modelValue', ['chosen-model'])
    await wrapper.get('[data-testid="bulk-excel-bps-toggle"]').trigger('click')
    await wrapper.get('[data-testid="bulk-excel-bps-toggle"]').trigger('click')
    await submit(wrapper)
    expect(accountsAPI.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(vi.mocked(accountsAPI.bulkUpdate).mock.calls[0]?.[1]?.extra).toMatchObject({ openai_excel_bps: true, openai_excel_bps_models: ['chosen-model'] })
  })

  it('drops BPS writes after transport permission is revoked', async () => {
    const { wrapper } = mountEditor()
    await wrapper.get('#bulk-edit-excel-bps-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-excel-bps-toggle"]').trigger('click')
    await selectConcurrency(wrapper)
    await wrapper.setProps({ allowTransportEdit: false })
    await submit(wrapper)
    expect(accountsAPI.bulkUpdate).toHaveBeenCalledWith([1, 2], { concurrency: 7 })
  })

  it('preserves explicit BPS disable sentinels instead of omitting old JSONB keys', async () => {
    const { wrapper } = mountEditor()
    await wrapper.get('#bulk-edit-excel-bps-enabled').setValue(true)
    await submit(wrapper)
    expect(accountsAPI.bulkUpdate).toHaveBeenCalledWith([1, 2], { extra: {
      openai_excel_bps: false, openai_excel_bps_models: null,
      openai_excel_bps_cache_creation_as_input: false, openai_excel_bps_auto_disable_on_403: false,
      openai_excel_bps_auto_recover_on_403: false, openai_excel_bps_403_recovery_interval_minutes: 60,
      openai_excel_bps_omit_unsupported_tools: false, openai_excel_bps_ignore_encrypted_content: false,
      openai_excel_bps_auto_move_on_403: false, openai_excel_bps_403_target_group_id: null
    } })
  })
})

describe('bulk target and writer ownership', () => {
  it.each(['authorize', 'models', 'super admin', 'scope', 'unmount'])('discards a late submit result after %s changes', async transition => {
    const pending = deferred<typeof result>()
    const writer = vi.fn().mockReturnValueOnce(pending.promise)
    const { wrapper, access, scope, notices } = mountEditor({ saveBatch: writer })
    await selectConcurrency(wrapper)
    await submit(wrapper)
    await submit(wrapper)
    expect(writer).toHaveBeenCalledTimes(1)
    if (transition === 'authorize') access.canAuthorize = false
    if (transition === 'models') access.canManage = false
    if (transition === 'super admin') access.isSuperAdmin = false
    if (transition === 'scope') scope.value = 'actor-2:supplier:9'
    if (transition === 'unmount') wrapper.unmount()
    await nextTick()
    pending.resolve(result)
    await flushPromises()
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(notices.showSuccess).not.toHaveBeenCalled()
    expect(notices.showError).not.toHaveBeenCalled()
    expect(accountsAPI.bulkUpdate).not.toHaveBeenCalled()
  })

  it('does not revive confirmation after actor capability is revoked and restored', async () => {
    const writer = vi.fn().mockRejectedValueOnce({ status: 409, error: 'mixed_channel_warning' })
    const { wrapper, access } = mountEditor({ saveBatch: writer })
    await selectConcurrency(wrapper)
    await submit(wrapper)
    access.canAuthorize = false
    access.canAuthorize = true
    confirm(wrapper)
    await flushPromises()
    expect(writer).toHaveBeenCalledTimes(1)
  })

  it('does not fall back to admin endpoints when the supplier writer is absent', async () => {
    const { wrapper } = mountEditor({ supplierScope: true, allowAuthorize: false })
    await selectConcurrency(wrapper)
    await submit(wrapper)
    await wrapper.get('[data-testid="bulk-full-editor"]').trigger('click')
    await flushPromises()
    expect(accountsAPI.bulkUpdate).not.toHaveBeenCalled()
    expect(accountsAPI.getById).not.toHaveBeenCalled()
    expect(wrapper.findComponent(EditAccountModal).exists()).toBe(false)
  })

  it('does not use a reference account outside the captured selection', async () => {
    const load = vi.fn().mockResolvedValue(cloudAccount(99))
    const writer = vi.fn().mockResolvedValue(result)
    const { wrapper, notices } = mountEditor({ loadReferenceAccount: load, saveBatch: writer })
    await wrapper.get('[data-testid="bulk-full-editor"]').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(EditAccountModal).exists()).toBe(false)
    expect(notices.showError).toHaveBeenCalledWith('admin.accounts.failedToLoad')
    expect(writer).not.toHaveBeenCalled()
  })
})

describe('bulk panel draft integration', () => {
  async function editPanelDrafts(wrapper: VueWrapper) {
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    const mappingTab = wrapper.findAll('button').find(button => button.text().includes('admin.accounts.modelMapping'))
    expect(mappingTab).toBeDefined()
    await mappingTab?.trigger('click')
    await wrapper.get('[data-testid="bulk-model-mapping-add"]').trigger('click')
    await wrapper.get('input[aria-label="admin.accounts.requestModel"]').setValue('request-model')
    await wrapper.get('input[aria-label="admin.accounts.actualModel"]').setValue('upstream-model')
    await wrapper.get('#bulk-edit-custom-error-codes-enabled').setValue(true)
    await wrapper.get('#bulk-edit-custom-error-code-input').setValue('418')
    await wrapper.get('#bulk-edit-custom-error-code-input').trigger('keydown', { key: 'Enter' })
    await wrapper.get('#bulk-edit-openai-compact-model-mapping-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-compact-model-mapping-add"]').trigger('click')
    await wrapper.get('input[aria-label="admin.accounts.fromModel"]').setValue('request-model')
    await wrapper.get('input[aria-label="admin.accounts.toModel"]').setValue('compact-model')
    await wrapper.get('#bulk-edit-openai-ws-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-ws-mode-select"]').setValue('http_bridge')
  }

  const panelPatch = {
    credentials: {
      model_mapping: { 'request-model': 'upstream-model' },
      compact_model_mapping: { 'request-model': 'compact-model' },
      custom_error_codes_enabled: true, custom_error_codes: [418]
    },
    extra: { openai_oauth_responses_websockets_v2_mode: 'http_bridge', openai_oauth_responses_websockets_v2_enabled: true }
  }

  it('submits all panel edits once through the parent scoped writer using the frozen target', async () => {
    const writer = vi.fn().mockResolvedValue(result)
    const { wrapper } = mountEditor({
      accountIds: [99], selectedPlatforms: ['grok'], selectedTypes: ['apikey'],
      target: { mode: 'selected', accountIds: [3, 4], selectedPlatforms: ['openai'], selectedTypes: ['oauth'] },
      supplierScope: true, saveBatch: writer
    })
    await editPanelDrafts(wrapper)
    expect(writer).not.toHaveBeenCalled()
    expect(accountsAPI.getById).not.toHaveBeenCalled()
    expect(accountsAPI.bulkUpdate).not.toHaveBeenCalled()
    expect(wrapper.get<HTMLInputElement>('#bulk-edit-custom-error-code-input').element.value).toBe('')
    await submit(wrapper)
    expect(writer).toHaveBeenCalledTimes(1)
    expect(writer).toHaveBeenCalledWith([3, 4], panelPatch)
    expect(accountsAPI.bulkUpdate).not.toHaveBeenCalled()
  })

  it('keeps the confirmation payload detached from subsequent panel edits', async () => {
    const writer = vi.fn().mockRejectedValueOnce({ status: 409, error: 'mixed_channel_warning' }).mockResolvedValue(result)
    const { wrapper } = mountEditor({ saveBatch: writer })
    await editPanelDrafts(wrapper)
    await submit(wrapper)
    await wrapper.get('input[aria-label="admin.accounts.actualModel"]').setValue('later-model')
    await wrapper.get('input[aria-label="admin.accounts.toModel"]').setValue('later-compact')
    await wrapper.get('[data-testid="bulk-error-code-remove-418"]').trigger('click')
    await wrapper.get('[data-testid="bulk-edit-openai-ws-mode-select"]').setValue('off')
    confirm(wrapper)
    await flushPromises()
    expect(writer.mock.calls).toEqual([
      [[1, 2], panelPatch], [[1, 2], { ...panelPatch, confirm_mixed_channel_risk: true }]
    ])
  })

  it.each(['permission', 'mixed target'] as const)('omits hidden panel fields after %s changes', async transition => {
    const { wrapper } = mountEditor()
    await editPanelDrafts(wrapper)
    await selectConcurrency(wrapper)
    if (transition === 'permission') await wrapper.setProps({ allowAuthorize: false, allowTransportEdit: false })
    else await wrapper.setProps({ selectedPlatforms: ['openai', 'anthropic'] })
    expect(wrapper.find('#bulk-edit-openai-ws-mode-enabled').exists()).toBe(false)
    if (transition === 'permission') {
      expect(wrapper.find('#bulk-edit-model-restriction-enabled').exists()).toBe(false)
      expect(wrapper.find('#bulk-edit-custom-error-codes-enabled').exists()).toBe(false)
    } else {
      expect(wrapper.find('#bulk-edit-openai-compact-model-mapping-enabled').exists()).toBe(false)
    }
    await submit(wrapper)
    expect(accountsAPI.bulkUpdate).toHaveBeenCalledWith([1, 2], transition === 'permission'
      ? { concurrency: 7 }
      : { concurrency: 7, credentials: {
        model_mapping: { 'request-model': 'upstream-model' }, custom_error_codes_enabled: true, custom_error_codes: [418]
      } })
  })

  it('sends explicit empty mappings/codes and WS off after removing the edited values', async () => {
    const { wrapper } = mountEditor()
    await editPanelDrafts(wrapper)
    await wrapper.get('[data-testid="bulk-model-mapping-remove"]').trigger('click')
    await wrapper.get('[data-testid="bulk-edit-openai-compact-model-mapping-remove"]').trigger('click')
    await wrapper.get('[data-testid="bulk-error-code-remove-418"]').trigger('click')
    await wrapper.get('[data-testid="bulk-edit-openai-ws-mode-select"]').setValue('off')
    await submit(wrapper)
    expect(accountsAPI.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: { model_mapping: {}, compact_model_mapping: {}, custom_error_codes_enabled: true, custom_error_codes: [] },
      extra: { openai_oauth_responses_websockets_v2_mode: 'off', openai_oauth_responses_websockets_v2_enabled: false }
    })
  })

  it('preserves parent-owned drafts across panel permission remounts and resets them on close', async () => {
    const { wrapper, notices } = mountEditor()
    await editPanelDrafts(wrapper)
    await wrapper.get('#bulk-edit-custom-error-code-input').setValue('422')
    await wrapper.setProps({ allowAuthorize: false, allowTransportEdit: false })
    await wrapper.setProps({ allowAuthorize: true, allowTransportEdit: true })
    expect(wrapper.get<HTMLInputElement>('input[aria-label="admin.accounts.actualModel"]').element.value).toBe('upstream-model')
    expect(wrapper.get<HTMLInputElement>('#bulk-edit-custom-error-code-input').element.value).toBe('422')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    for (const id of ['model-restriction', 'custom-error-codes', 'openai-compact-model-mapping', 'openai-ws-mode']) {
      expect(wrapper.get<HTMLInputElement>(`#bulk-edit-${id}-enabled`).element.checked).toBe(false)
    }
    expect(wrapper.find('input[aria-label="admin.accounts.actualModel"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="bulk-edit-openai-compact-model-mapping-input"]')).toHaveLength(0)
    expect(wrapper.get<HTMLSelectElement>('[data-testid="bulk-edit-openai-ws-mode-select"]').element.value).toBe('off')
    await submit(wrapper)
    expect(notices.showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.noFieldsSelected')
    expect(accountsAPI.bulkUpdate).not.toHaveBeenCalled()
    await wrapper.get('#bulk-edit-custom-error-codes-enabled').setValue(true)
    expect(wrapper.get<HTMLInputElement>('#bulk-edit-custom-error-code-input').element.value).toBe('')
    expect(wrapper.find('[data-testid="bulk-error-code-remove-418"]').exists()).toBe(false)
  })

  it('routes duplicate/invalid error-code feedback to the parent without sending a mutation', async () => {
    const { wrapper, notices } = mountEditor()
    await wrapper.get('#bulk-edit-custom-error-codes-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-error-code-toggle-401"]').trigger('click')
    await wrapper.get('#bulk-edit-custom-error-code-input').setValue('401')
    await wrapper.get('[data-testid="bulk-error-code-add"]').trigger('click')
    expect(notices.showInfo).toHaveBeenCalledWith('admin.accounts.errorCodeExists')
    await wrapper.get('#bulk-edit-custom-error-code-input').setValue('')
    await wrapper.get('[data-testid="bulk-error-code-add"]').trigger('click')
    expect(notices.showError).toHaveBeenCalledWith('admin.accounts.invalidErrorCode')
    expect(accountsAPI.bulkUpdate).not.toHaveBeenCalled()
  })
})
