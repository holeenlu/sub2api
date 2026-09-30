import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const {
  copyToClipboard,
  showError,
  showSuccess,
  showInfo,
  showWarning,
  syncUpstreamModelsBulk,
  syncUpstreamModels,
  syncUpstreamModelsPreview, getModelCatalog, refreshModelCatalog
} = vi.hoisted(() => ({
  copyToClipboard: vi.fn().mockResolvedValue(true),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
  showWarning: vi.fn(),
  syncUpstreamModelsBulk: vi.fn(),
  syncUpstreamModels: vi.fn(),
  syncUpstreamModelsPreview: vi.fn(), getModelCatalog: vi.fn(), refreshModelCatalog: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      // 错误提示要能看到插值后的 message，否则分不清提取到了哪一句
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'common.copy') return '复制'
        if (key === 'admin.accounts.syncUpstreamModelsError') return `${key}: ${params?.message ?? ''}`
        if (key === 'admin.accounts.modelMappingConflict') return `Model mapping conflict: ${params?.from} → ${params?.to}`
        return key
      }
    })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo,
    showWarning
  })
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: {
    syncUpstreamModelsBulk,
    syncUpstreamModels,
    syncUpstreamModelsPreview
  }
}))

vi.mock('@/api/admin/modelCatalog', () => ({ getModelCatalog, refreshModelCatalog }))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

function mountSelector(props: Record<string, unknown> = {}) {
  return mount(ModelWhitelistSelector, {
    props: {
      modelValue: [],
      platform: 'openai',
      ...props,
    },
    global: {
      stubs: {
        ModelIcon: true
      }
    }
  })
}

function findModelRow(wrapper: ReturnType<typeof mountSelector>, modelId: string) {
  const row = wrapper
    .findAll('[data-testid="model-option"]')
    .find(candidate => candidate.text().includes(modelId))

  if (!row) {
    throw new Error(`Model row not found: ${modelId}`)
  }

  return row
}

describe('ModelWhitelistSelector', () => {
  beforeEach(() => {
    getModelCatalog.mockReset().mockResolvedValue({ status: 'ready', revision: 'saved-v1', checked_at: '2026-09-30T00:00:00Z', models: [{ id: 'gpt-5.6-sol', display_name: 'Sol', lifecycle: 'active', access: 'listed', missing: [], endpoints: [] }] })
    refreshModelCatalog.mockReset().mockImplementation(async (id: number) => {
      const result = await syncUpstreamModels(id)
      return { status: 'ready', revision: 'saved-v2', warnings: result.warnings ?? [], models: result.models.map((model: string) => ({ id: model, display_name: model, lifecycle: 'active', access: 'listed', missing: result.warnings?.length ? ['context_window'] : [], endpoints: [] })) }
    })
    copyToClipboard.mockClear()
    showError.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()
    showWarning.mockReset()
    syncUpstreamModelsBulk.mockReset()
    syncUpstreamModels.mockReset()
    syncUpstreamModelsPreview.mockReset()
  })

  it('rejects a custom whitelist model that is already mapped to a different target', async () => {
    const wrapper = mountSelector({ modelMappings: [{ from: 'gpt-latest', to: 'deepseek-chat' }] })
    await wrapper.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue(' gpt-latest ')
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')

    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showInfo).toHaveBeenCalledWith(expect.stringContaining('gpt-latest → deepseek-chat'))
  })

  it('keeps the existing duplicate identity warning before checking mappings', async () => {
    const wrapper = mountSelector({ modelValue: ['gpt-latest'], modelMappings: [{ from: 'gpt-latest', to: 'deepseek-chat' }] })
    await wrapper.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue('gpt-latest')
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    expect(showInfo).toHaveBeenCalledWith('admin.accounts.modelExists')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('allows matching identity mapping as a whitelist model', async () => {
    const wrapper = mountSelector({ modelMappings: [{ from: 'gpt-latest', to: 'gpt-latest' }] })
    await wrapper.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue('gpt-latest')
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-latest']]])
  })

  it('still allows custom models without a mapping prop', async () => {
    const wrapper = mountSelector()
    await wrapper.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue('custom-model')
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['custom-model']]])
  })

  it('copies a model ID without selecting the model', async () => {
    const wrapper = mountSelector()
    await flushPromises()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')

    const copyButton = row.get('[data-testid="copy-model-id"]')
    expect(copyButton.attributes('aria-label')).toBe('复制 gpt-5.6-sol')

    await copyButton.trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenCalledWith('gpt-5.6-sol')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps the existing model selection behavior', async () => {
    const wrapper = mountSelector()
    await flushPromises()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')
    await row.get('[data-testid="select-model"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-5.6-sol']]])
    expect(copyToClipboard).not.toHaveBeenCalled()
  })

  // 实时交集是「上游现在支持什么」，不是「白名单应该是什么」：映射别名与刚下架
  // 的旧模型都不在里面，所以默认只做合并。
  it('requires selecting the refreshed Anthropic intersection before changing the whitelist', async () => {
    syncUpstreamModelsBulk.mockResolvedValue({
      models: ['claude-sonnet-5', 'claude-opus-5'],
      failures: [],
      account_count: 2,
      aggregation: 'intersection',
      source: 'anthropic_v1_models'
    })
    const wrapper = mountSelector({
      modelValue: ['claude-alias'],
      platform: 'anthropic',
      accountIds: [11, 12]
    })

    await wrapper.get('[data-testid="sync-upstream-models-bulk"]').trigger('click')
    await flushPromises()

    expect(syncUpstreamModelsBulk).toHaveBeenCalledWith({
      account_ids: [11, 12],
      filters: undefined
    })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.findAll('button').find(button=>button.text()==='modelCatalog.selectAvailable')!.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([
      [['claude-alias', 'claude-sonnet-5', 'claude-opus-5']]
    ])
  })

  it('replaces the whitelist only after a second confirmation', async () => {
    syncUpstreamModelsBulk.mockResolvedValue({
      models: ['claude-sonnet-5'],
      failures: [],
      account_count: 1,
      aggregation: 'intersection',
      source: 'anthropic_v1_models'
    })
    const wrapper = mountSelector({
      modelValue: ['claude-alias'],
      platform: 'anthropic',
      accountIds: [11]
    })

    await wrapper.get('[data-testid="sync-upstream-models-bulk"]').trigger('click')
    await flushPromises()
    await wrapper.setProps({ modelValue: ['claude-alias', 'claude-sonnet-5'] })

    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    await wrapper.get('[data-testid="replace-with-live-models"]').trigger('click')
    expect(confirmSpy).toHaveBeenCalled()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    confirmSpy.mockReturnValue(true)
    await wrapper.get('[data-testid="replace-with-live-models"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([['claude-sonnet-5']])
    confirmSpy.mockRestore()
  })

  it('lists failed accounts and refuses to apply a partial intersection', async () => {
    syncUpstreamModelsBulk.mockResolvedValue({
      models: ['claude-sonnet-5'],
      failures: [{ account_id: 13, name: 'expired-oauth', error: 'Upstream returned HTTP 401' }],
      account_count: 2,
      aggregation: 'intersection',
      source: 'anthropic_v1_models'
    })
    const wrapper = mountSelector({ platform: 'anthropic', accountIds: [11, 13] })

    await wrapper.get('[data-testid="sync-upstream-models-bulk"]').trigger('click')
    await flushPromises()

    const failures = wrapper.get('[data-testid="bulk-upstream-sync-failures"]')
    expect(failures.text()).toContain('expired-oauth')
    expect(failures.text()).toContain('Upstream returned HTTP 401')
    expect(showError).toHaveBeenCalled()
    expect(showWarning).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  // 整批失败时后端回 200 带 error + failures：错误响应没有 data 字段，逐账号明细
  // 只能这样送到管理员眼前。
  it('shows the per-account detail when the whole batch fails', async () => {
    syncUpstreamModelsBulk.mockResolvedValue({
      models: [],
      failures: [
        { account_id: 21, name: 'expired-a', error: 'Upstream returned HTTP 401' },
        { account_id: 22, name: 'expired-b', error: 'Timed out while fetching /v1/models' }
      ],
      account_count: 2,
      aggregation: 'intersection',
      source: 'anthropic_v1_models',
      error: 'failed to fetch Anthropic /v1/models from every candidate account'
    })
    const wrapper = mountSelector({ platform: 'anthropic', accountIds: [21, 22] })

    await wrapper.get('[data-testid="sync-upstream-models-bulk"]').trigger('click')
    await flushPromises()

    const failures = wrapper.get('[data-testid="bulk-upstream-sync-failures"]')
    expect(failures.text()).toContain('expired-a')
    expect(failures.text()).toContain('expired-b')
    expect(showError).toHaveBeenCalledWith(
      'admin.accounts.syncUpstreamModelsError: failed to fetch Anthropic /v1/models from every candidate account'
    )
    expect(showInfo).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  // apiClient 的拒绝对象不是 Error 实例，只看 error.message 会永远退化成泛化文案。
  it('surfaces the server detail when the live sync fails', async () => {
    syncUpstreamModelsBulk.mockRejectedValue({
      response: { data: { detail: 'Live model sync requires an Anthropic-only account selection' } }
    })
    const wrapper = mountSelector({ platform: 'anthropic', accountIds: [11] })

    await wrapper.get('[data-testid="sync-upstream-models-bulk"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith(
      'admin.accounts.syncUpstreamModelsError: Live model sync requires an Anthropic-only account selection'
    )
    expect(wrapper.find('[data-testid="bulk-upstream-sync-failures"]').exists()).toBe(false)
  })

  // apiClient 的拦截器把后端文案压平成 { status, code, message, error }，其中
  // 部分错误只填 error。手写的提取器不认这个字段，用户只会看到泛化文案。
  it('surfaces the interceptor error field when the live sync fails', async () => {
    syncUpstreamModelsBulk.mockRejectedValue({
      status: 400,
      error: 'Live model sync requires an Anthropic-only account selection'
    })
    const wrapper = mountSelector({ platform: 'anthropic', accountIds: [11] })

    await wrapper.get('[data-testid="sync-upstream-models-bulk"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith(
      'admin.accounts.syncUpstreamModelsError: Live model sync requires an Anthropic-only account selection'
    )
  })

  it('hides the live sync action when no batch target is provided', () => {
    const wrapper = mountSelector({ platform: 'anthropic' })

    expect(wrapper.find('[data-testid="sync-upstream-models-bulk"]').exists()).toBe(false)
  })

  it('warns when model IDs sync but capability metadata is incomplete', async () => {
    syncUpstreamModels.mockResolvedValue({
      models: ['x-preview-f-free'],
      warnings: [
        {
          code: 'upstream_model_metadata_incomplete',
          message: 'Model IDs were synced, but capability metadata could not be updated.'
        }
      ]
    })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        accountId: 46
      },
      global: {
        stubs: {
          ModelIcon: true
        }
      }
    })

    const syncButton = wrapper
      .find('[data-testid="sync-upstream-models"]')
    expect(syncButton.exists()).toBe(true)
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.find('[data-testid="catalog-incomplete"]').exists()).toBe(true)
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('shows success and a partial warning when some capabilities were saved', async () => {
    syncUpstreamModels.mockResolvedValue({
      models: ['gpt-6-astra', 'gpt-image-2'],
      warnings: [
        {
          code: 'upstream_model_metadata_partial',
          message: 'Some model capabilities were saved; remaining models are still incomplete.'
        }
      ]
    })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        accountId: 46
      },
      global: {
        stubs: {
          ModelIcon: true
        }
      }
    })

    const syncButton = wrapper
      .find('[data-testid="sync-upstream-models"]')
    expect(syncButton.exists()).toBe(true)
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(refreshModelCatalog).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="catalog-incomplete"]').exists()).toBe(true)
  })

  it('reports a successful preview so account creation can persist metadata', async () => {
    syncUpstreamModelsPreview.mockResolvedValue({
      models: ['x-preview-f-free'],
      metadata: {
        'x-preview-f-free': {
          id: 'x-preview-f-free',
          reasoning: true,
          supported_reasoning_levels: ['low', 'high', 'max'],
        },
      },
    })
    const wrapper = mountSelector({
      syncCredentials: {
        platform: 'openai',
        type: 'apikey',
        base_url: 'https://opencode.ai/zen/v1',
        api_key: 'test-key',
      },
    })
    const syncButton = wrapper
      .find('[data-testid="sync-upstream-models"]')

    expect(syncButton.exists()).toBe(true)
    await syncButton?.trigger('click')
    await flushPromises()

    expect(syncUpstreamModelsPreview).toHaveBeenCalledOnce()
    expect(wrapper.emitted('upstream-synced')).toEqual([[]])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('shows the upstream sync button for OpenCode Go create-account credentials', () => {
    const wrapper = mountSelector({
      platform: 'opencode_go',
      syncCredentials: {
        platform: 'opencode_go',
        type: 'apikey',
        base_url: 'https://opencode.ai/zen/go/v1',
        api_key: 'sk-test',
      },
    })
    const syncButton = wrapper
      .find('[data-testid="sync-upstream-models"]')

    expect(syncButton.exists()).toBe(true)
    expect(syncButton?.exists()).toBe(true)
  })
  it('fetches the live OpenAI batch catalog and preserves existing whitelist entries', async () => {
    syncUpstreamModelsBulk.mockResolvedValue({ models: ['gpt-new', 'gpt-new'], failures: [] })
    const wrapper = mountSelector({ accountIds: [41, 42], modelValue: ['custom-alias'] })
    const button = wrapper.get('[data-testid="sync-upstream-models-bulk"]')
    expect(button.text()).toBe('admin.accounts.syncUpstreamModels')
    await button.trigger('click')
    await flushPromises()
    expect(syncUpstreamModelsBulk).toHaveBeenCalledWith({ account_ids: [41, 42], filters: undefined })
    expect(syncUpstreamModels).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.findAll('button').find(button=>button.text()==='modelCatalog.selectAvailable')!.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['custom-alias', 'gpt-new']]])
  })

  it('sends the filter snapshot for all matching OpenAI accounts', async () => {
    syncUpstreamModelsBulk.mockResolvedValue({ models: ['gpt-new'], failures: [] })
    const filters = { platform: 'openai', group: 'ungrouped', search: 'prod' }
    const wrapper = mountSelector({ syncFilters: filters })
    await wrapper.get('[data-testid="sync-upstream-models-bulk"]').trigger('click')
    await flushPromises()
    expect(syncUpstreamModelsBulk).toHaveBeenCalledWith({ account_ids: undefined, filters })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('does not offer replacement from a failed partial result', async () => {
    syncUpstreamModelsBulk.mockResolvedValue({
      models: ['gpt-partial'], failures: [{ account_id: 42, name: 'failed', error: 'Unauthorized' }]
    })
    const wrapper = mountSelector({ accountIds: [41, 42], modelValue: ['keep-me'] })
    await wrapper.get('[data-testid="sync-upstream-models-bulk"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.find('[data-testid="replace-with-live-models"]').exists()).toBe(false)
  })

  it('ignores late responses after the selection changes', async () => {
    let finish!: (value: unknown) => void
    syncUpstreamModelsBulk.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = mountSelector({ accountIds: [41, 42] })
    await wrapper.get('[data-testid="sync-upstream-models-bulk"]').trigger('click')
    expect(wrapper.get('[data-testid="sync-upstream-models-bulk"]').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ accountIds: [43] })
    finish({ models: ['old-target-model'], failures: [] })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('ignores a response after the editor has been closed', async () => {
    let finish!: (value: unknown) => void
    syncUpstreamModelsBulk.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const wrapper = mountSelector({ accountIds: [41] })
    await wrapper.get('[data-testid="sync-upstream-models-bulk"]').trigger('click')
    wrapper.unmount()
    finish({ models: ['late-model'], failures: [] })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
  })

})
