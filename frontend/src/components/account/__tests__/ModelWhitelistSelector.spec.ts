import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import {
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog
} from '@/constants/platformCatalog'

const {
  copyToClipboard,
  showError,
  showSuccess,
  showInfo,
  showWarning,
  syncUpstreamModels,
  syncUpstreamModelsPreview,
  getModelAllowlistCandidates
} = vi.hoisted(() => ({
  copyToClipboard: vi.fn().mockResolvedValue(true),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
  showWarning: vi.fn(),
  syncUpstreamModels: vi.fn(),
  syncUpstreamModelsPreview: vi.fn(),
  getModelAllowlistCandidates: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) => key === 'common.copy' ? '复制' : key === 'admin.accounts.modelMappingConflict' ? `Model mapping conflict: ${params?.from} → ${params?.to}` : key === 'admin.accounts.syncUpstreamModelsError' ? `${key}: ${params?.message}` : key
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
    syncUpstreamModels,
    syncUpstreamModelsPreview
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

vi.mock('@/api/admin/groups', () => ({ getModelAllowlistCandidates }))

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
  it('loads and appends the group built-in catalog while preserving custom models', async () => {
    getModelAllowlistCandidates.mockResolvedValue(['gpt-reserve', 'gpt-6.1-sol'])
    const wrapper = mountSelector({ modelValue: ['custom', 'gpt-reserve'] })
    await flushPromises()
    await wrapper.get('div.cursor-pointer').trigger('click')
    expect(wrapper.findAll('[data-testid="model-option"]').map(row => row.text())).toEqual(['gpt-reserve', 'gpt-6.1-sol'])
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.fillRelatedModels')!.trigger('click')
    await flushPromises()
    expect(getModelAllowlistCandidates).toHaveBeenLastCalledWith(0, 'openai')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['custom', 'gpt-reserve', 'gpt-6.1-sol']]])
  })

  it('merges built-in catalogs for mixed platform targets without duplicates', async () => {
    getModelAllowlistCandidates.mockImplementation(async (_id: number, platform: string) =>
      platform === 'openai' ? ['gpt-reserve', 'shared'] : ['claude-haiku-5-5', 'shared'])
    const wrapper = mountSelector({ platforms: ['openai', 'anthropic'] })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.fillRelatedModels')!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-reserve', 'shared', 'claude-haiku-5-5']]])
  })

  it('keeps the draft on catalog failure and retries without a stale local fallback', async () => {
    const wrapper = mountSelector({ modelValue: ['custom'] })
    await flushPromises()
    getModelAllowlistCandidates.mockRejectedValueOnce(new Error('offline'))
    const syncButton = wrapper.findAll('button').find(button => button.text() === 'admin.accounts.fillRelatedModels')!
    await syncButton.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showError).toHaveBeenCalledWith('admin.accounts.modelSyncBuiltinFailed')
    getModelAllowlistCandidates.mockResolvedValueOnce(['gpt-reserve'])
    await syncButton.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['custom', 'gpt-reserve']]])
  })

  it.each(['switch', 'close'])('ignores pending built-in sync after %s', async (action) => {
    const wrapper = mountSelector()
    await flushPromises()
    let complete!: (models: string[]) => void
    getModelAllowlistCandidates.mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.fillRelatedModels')!.trigger('click')
    if (action === 'switch') await wrapper.setProps({ platform: 'anthropic' })
    else wrapper.unmount()
    complete(['gpt-reserve'])
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it.each(['anthropic', 'openai'])('bulk %s sync adds only common upstream models and preserves the draft', async (platform) => {
    const model = platform === 'openai' ? 'gpt-reserve' : 'claude-haiku-5-5'
    syncUpstreamModels.mockResolvedValueOnce({ models: [model, 'only-first'] })
      .mockResolvedValueOnce({ models: [` ${model} `, 'only-second'] })
    const wrapper = mountSelector({ platform, modelValue: ['custom'], syncAccountIds: async () => [1, 2, 1] })
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    expect(syncUpstreamModels.mock.calls).toEqual([[1], [2]])
    expect(wrapper.emitted('update:modelValue')).toEqual([[['custom', model]]])
    expect(wrapper.emitted('upstream-synced')).toBeUndefined()
  })

  it('stops a pending bulk sync after the selector is closed', async () => {
    let complete!: (value: { models: string[] }) => void
    syncUpstreamModels.mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    const wrapper = mountSelector({ platform: 'anthropic', syncAccountIds: async () => [1, 2] })
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    wrapper.unmount()
    complete({ models: ['claude-haiku-5-5'] })
    await flushPromises()
    expect(syncUpstreamModels).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it.each(['failure', 'empty', 'disjoint'])('bulk sync keeps the draft on %s instead of applying a partial or empty whitelist', async (mode) => {
    syncUpstreamModels.mockResolvedValueOnce({ models: ['claude-haiku-5-5'] })
    if (mode === 'failure') syncUpstreamModels.mockRejectedValueOnce(new Error('upstream unavailable'))
    else syncUpstreamModels.mockResolvedValueOnce({ models: mode === 'empty' ? [] : ['other-model'] })
    const wrapper = mountSelector({ platform: 'anthropic', modelValue: ['custom'], syncAccountIds: async () => [1, 2] })
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showError).toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
  })

  beforeEach(() => {
    copyToClipboard.mockClear()
    showError.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()
    showWarning.mockReset()
    syncUpstreamModels.mockReset()
    syncUpstreamModelsPreview.mockReset()
    getModelAllowlistCandidates.mockReset().mockResolvedValue(['gpt-5.6-sol'])
  })

  afterEach(() => {
    resetPlatformCatalog()
  })

  it.each(['anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'command_code', 'cline'])(
    'supports upstream sync for %s saved accounts and creation previews',
    (platform) => {
      const wrappers = [
        mountSelector({ platform, accountId: 46 }),
        mountSelector({ platform, syncCredentials: { platform, type: 'apikey', api_key: 'test-key' } })
      ]
      for (const wrapper of wrappers) {
        expect(wrapper.findAll('button').some(button => button.text() === 'admin.accounts.syncUpstreamModels')).toBe(true)
        wrapper.unmount()
      }
    }
  )

  it.each(['typesafe', 'unregistered'])(
    'hides upstream sync for unsupported %s saved accounts and creation previews',
    (platform) => {
      const wrappers = [
        mountSelector({ platform, accountId: 46 }),
        mountSelector({ platform, syncCredentials: { platform, type: 'apikey', api_key: 'test-key' } })
      ]
      for (const wrapper of wrappers) {
        expect(wrapper.findAll('button').some(button => button.text() === 'admin.accounts.syncUpstreamModels')).toBe(false)
        wrapper.unmount()
      }
      expect(syncUpstreamModels).not.toHaveBeenCalled()
      expect(syncUpstreamModelsPreview).not.toHaveBeenCalled()
    }
  )

  it('requires a supported request builder for newly registered platforms', async () => {
    const wrapper = mountSelector({ platform: 'acme_router', accountId: 46 })
    const platforms = [
      ...BUILTIN_PLATFORM_CATALOG.platforms,
      { id: 'acme_router', display_name: 'Acme Router', gateway: 'openai' as const, cn_provider: false }
    ]
    setPlatformCatalog({ ...BUILTIN_PLATFORM_CATALOG, platforms })
    await flushPromises()
    expect(wrapper.findAll('button').some(button => button.text() === 'admin.accounts.syncUpstreamModels')).toBe(false)

    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: platforms.map(spec => spec.id === 'acme_router'
        ? { ...spec, multi_protocol: { default_mode: 'default', routing: 'by_inbound', modes: [] } }
        : spec)
    })
    await flushPromises()
    expect(wrapper.findAll('button').some(button => button.text() === 'admin.accounts.syncUpstreamModels')).toBe(true)
    wrapper.unmount()
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
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([[['x-preview-f-free']]])
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsMetadataIncomplete')
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('shows the backend message when upstream sync is rejected', async () => {
    syncUpstreamModels.mockRejectedValue({
      status: 400,
      code: 'BAD_REQUEST',
      message: 'Vertex API key accounts do not support upstream model listing'
    })
    const wrapper = mountSelector({ platform: 'gemini', accountId: 174 })

    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith(
      'admin.accounts.syncUpstreamModelsError: Vertex API key accounts do not support upstream model listing'
    )
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
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-6-astra', 'gpt-image-2']]])
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsSuccess')
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsMetadataPartial')
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
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')

    expect(syncButton).toBeDefined()
    await syncButton?.trigger('click')
    await flushPromises()

    expect(syncUpstreamModelsPreview).toHaveBeenCalledOnce()
    expect(wrapper.emitted('upstream-synced')).toEqual([[]])
    expect(wrapper.emitted('update:modelValue')).toEqual([[['x-preview-f-free']]])
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
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')

    expect(syncButton).toBeDefined()
    expect(syncButton?.exists()).toBe(true)
  })
})
