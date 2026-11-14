import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTestModal from '../AccountTestModal.vue'

const { getAvailableModels, copyToClipboard } = vi.hoisted(() => ({
  getAvailableModels: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/features/resources/accounts', () => ({ default: { getAvailableModels } }))

vi.mock('@/shared/lib/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'admin.accounts.imagePromptDefault': 'Generate a cute orange cat astronaut sticker on a clean pastel background.'
  }
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (key === 'admin.accounts.imageReceived' && params?.count) {
          return `received-${params.count}`
        }
        if (key === 'admin.accounts.imagePreviewAlt' && params?.index) {
          return `test-image-${params.index}`
        }
        return messages[key] || key
      }
    })
  }
})

function createStreamResponse(lines: string[]) {
  const encoder = new TextEncoder()
  const chunks = lines.map((line) => encoder.encode(line))
  let index = 0

  return {
    ok: true,
    body: {
      getReader: () => ({
        read: vi.fn().mockImplementation(async () => {
          if (index < chunks.length) {
            return { done: false, value: chunks[index++] }
          }
          return { done: true, value: undefined }
        })
      })
    }
  } as Response
}

function mountModal(account: Record<string, unknown> = {
  id: 42,
  name: 'Gemini Image Test',
  platform: 'gemini',
  type: 'apikey',
  status: 'active'
}) {
  return mount(AccountTestModal, {
    props: {
      show: false,
      account
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Select: { template: '<div class="select-stub"></div>' },
        TextArea: {
          props: ['modelValue'],
          emits: ['update:modelValue'],
          template: '<textarea class="textarea-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />'
        },
        Icon: true
      }
    }
  })
}

describe('AccountTestModal', () => {
  beforeEach(() => {
    getAvailableModels.mockResolvedValue([
      { id: 'gemini-2.0-flash', display_name: 'Gemini 2.0 Flash' },
      { id: 'gemini-2.5-flash-image', display_name: 'Gemini 2.5 Flash Image' },
      { id: 'gemini-3.1-flash-image', display_name: 'Gemini 3.1 Flash Image' }
    ])
    copyToClipboard.mockReset()
    Object.defineProperty(globalThis, 'localStorage', {
      value: {
        getItem: vi.fn((key: string) => (key === 'auth_token' ? 'test-token' : null)),
        setItem: vi.fn(),
        removeItem: vi.fn(),
        clear: vi.fn()
      },
      configurable: true
    })
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_start","model":"gemini-2.5-flash-image"}\n',
        'data: {"type":"image","image_url":"data:image/png;base64,QUJD","mime_type":"image/png"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('gemini 图片模型测试会携带提示词并渲染图片预览', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    const promptInput = wrapper.find('textarea.textarea-stub')
    expect(promptInput.exists()).toBe(true)
    await promptInput.setValue('draw a tiny orange cat astronaut')

    const buttons = wrapper.findAll('button')
    const startButton = buttons.find((button) => button.text().includes('admin.accounts.startTest'))
    expect(startButton).toBeTruthy()

    await startButton!.trigger('click')
    await flushPromises()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
      model_id: 'gemini-3.1-flash-image',
      prompt: 'draw a tiny orange cat astronaut'
    })

    const preview = wrapper.find('img[alt="test-image-1"]')
    expect(preview.exists()).toBe(true)
    expect(preview.attributes('src')).toBe('data:image/png;base64,QUJD')
  })

  it('grok 账号测试默认选择 Grok 模型', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'grok-4.3', display_name: 'Grok 4.3' },
      { id: 'grok-build-0.1', display_name: 'Grok Build 0.1' }
    ])
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_start","model":"grok-4.3"}\n',
        'data: {"type":"content","text":"ok"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any

    const wrapper = mountModal({
      id: 13,
      name: 'Grok Account',
      platform: 'grok',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    const buttons = wrapper.findAll('button')
    const startButton = buttons.find((button) => button.text().includes('admin.accounts.startTest'))
    expect(startButton).toBeTruthy()

    await startButton!.trigger('click')
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
      model_id: 'grok-4.3',
      prompt: '',
      mode: 'text'
    })
  })

  it('OpenAI Compact 探测会携带 compact 测试模式', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-5.4', display_name: 'GPT-5.4' }
    ])
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any

    const wrapper = mountModal({
      id: 42,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    ;(wrapper.vm as any).testMode = 'compact'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toMatchObject({
      model_id: 'gpt-5.4',
      prompt: '',
      mode: 'compact'
    })
  })
})

describe('Cloud compatibility test results', () => {
  afterEach(() => { vi.restoreAllMocks() })
  beforeEach(() => { getAvailableModels.mockResolvedValue([{ id: 'cloud-model', display_name: 'Cloud model', type: 'model' }]) })
  async function selectCloudMode(wrapper: ReturnType<typeof mountModal>) {
    await wrapper.setProps({ show: true }); await flushPromises()
    ;(wrapper.vm as any).testMode = 'cloud'
    await flushPromises()
  }

  it.each([true, false])('uses the terminal result for downloads (successful: %s)', async (success) => {
    const finalResult = {
      kind: 'cloud_result', response_id: 'response-1', status: success ? 'completed' : 'failed', model: 'cloud-model',
      output: [{ type: 'message', role: 'assistant', content: [{ type: 'output_text', text: 'Final answer' }] }],
      usage: { input_tokens: 12, output_tokens: 3, total_tokens: 15 }, created_at: 1, completed_at: 2,
      capabilities: { text: true, tools: success, usage: true, settlement: success, image_input: success, encrypted_context: success, web_search: success, image_generation: success }
    }
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([
      'data: {"type":"content","text":"partial output"}\n',
      `data: ${JSON.stringify({ type: 'test_complete', success, error: success ? undefined : 'tool verification failed', data: finalResult })}\n`
    ])) as any
    const wrapper = mountModal({ id: 42, name: 'Cloud account', platform: 'openai', type: 'oauth', status: 'active', credentials: {} })
    await selectCloudMode(wrapper)
    await flushPromises()
    await (wrapper.vm as any).startTest()
    await flushPromises()
    expect(JSON.parse((global.fetch as any).mock.calls[0][1].body)).toMatchObject({ mode: 'cloud', model_id: 'cloud-model' })
    expect(wrapper.find('[data-testid="cloud-download-result"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="cloud-download-answer"]').exists()).toBe(success)
    expect((wrapper.vm as any).cloudResult).toEqual(finalResult)
    await wrapper.setProps({ account: { id: 43, name: 'Another account', platform: 'openai', type: 'oauth', status: 'active' } as any })
    await flushPromises()
    expect(wrapper.find('[data-testid="cloud-download-result"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the safe nested Cloud failure when no top-level message is present', async () => {
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([
      `data: ${JSON.stringify({ type: 'test_complete', success: false, data: { kind: 'cloud_result', response_id: '', status: 'failed', model: 'cloud-model', output: [], usage: null, created_at: 1, completed_at: 2, capabilities: {}, error: { message: 'wham quota exhausted (429)' } } })}\n`
    ])) as any
    const wrapper = mountModal({ id: 42, name: 'Cloud account', platform: 'openai', type: 'oauth', status: 'active', credentials: {} })
    await selectCloudMode(wrapper)
    await (wrapper.vm as any).startTest(); await flushPromises()
    expect(wrapper.text()).toContain('wham quota exhausted (429)')
    wrapper.unmount()
  })

  it('does not report success when the Cloud terminal result is missing', async () => {
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse(['data: {"type":"test_complete","success":true}\n'])) as any
    const wrapper = mountModal({ id: 42, name: 'Cloud account', platform: 'openai', type: 'oauth', status: 'active', credentials: {} })
    await selectCloudMode(wrapper)
    await flushPromises()
    await (wrapper.vm as any).startTest(); await flushPromises()
    expect((wrapper.vm as any).status).toBe('error')
    expect(wrapper.find('[data-testid="cloud-download-result"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

vi.mock('@/features/resources/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/resources/services')>('@/features/resources/services')
  const { editorTestServices } = await import('../../__tests__/editorTestServices')
  return { ...actual, useResourceEditorServices: () => {
    const services = editorTestServices()
    services.openAccountTest = (id, request, signal) => global.fetch(String(id), { method: 'POST', body: JSON.stringify(request), signal })
    return services
  } }
})
