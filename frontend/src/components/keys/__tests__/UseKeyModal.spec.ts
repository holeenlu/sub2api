import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const { copyToClipboardMock, saveAsMock } = vi.hoisted(() => ({
  copyToClipboardMock: vi.fn().mockResolvedValue(true),
  saveAsMock: vi.fn()
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: copyToClipboardMock
  })
}))

vi.mock('file-saver', () => ({
  saveAs: saveAsMock
}))

import UseKeyModal from '../UseKeyModal.vue'
import type { GroupPlatform } from '@/types'
import { parse as parseToml } from 'smol-toml'

function readBlobAsText(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.addEventListener('load', () => resolve(String(reader.result || '')))
    reader.addEventListener('error', () => reject(reader.error))
    reader.readAsText(blob)
  })
}


const stubs = {
  BaseDialog: {
    template: '<div><slot /><slot name="footer" /></div>'
  },
  Icon: {
    template: '<span />'
  }
}

function mountModal(platform: GroupPlatform, apiKey = 'sk-test') {
  return mount(UseKeyModal, {
    props: {
      show: true,
      apiKey,
      baseUrl: 'https://example.com/v1',
      platform
    },
    global: { stubs }
  })
}

async function clickButton(wrapper: ReturnType<typeof mountModal>, match: (text: string) => boolean) {
  const button = wrapper.findAll('button').find((candidate) => match(candidate.text()))
  expect(button).toBeDefined()
  await button!.trigger('click')
  await nextTick()
}

function findCodeBlock(wrapper: ReturnType<typeof mountModal>, marker: string): string {
  const block = wrapper.findAll('pre code').map((code) => code.text()).find((content) => content.includes(marker))
  expect(block).toBeDefined()
  return block!
}

function tomlValue(config: string, key: string): string | undefined {
  return config.match(new RegExp(`^${key} = "([^"]*)"$`, 'm'))?.[1]
}

function stubCatalog(slugs: string[] | 'error') {
  if (slugs === 'error') {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 502, json: async () => ({}) }))
    return
  }
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => ({ models: slugs.map((slug) => ({ slug })) })
  }))
}

describe('UseKeyModal', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    saveAsMock.mockClear()
  })

  it('omits the attribution override from every standard Claude Code setup form', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-anthropic-test',
        baseUrl: 'https://example.com/v1',
        platform: 'anthropic'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    for (const [shell, trafficSetting] of [
      ['macOS / Linux', 'export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1'],
      ['Windows CMD', 'set CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1'],
      ['PowerShell', '$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1']
    ]) {
      if (shell !== 'macOS / Linux') {
        const shellTab = wrapper.findAll('button').find(
          (button) => button.text().trim() === shell
        )
        expect(shellTab).toBeDefined()
        await shellTab!.trigger('click')
        await nextTick()
      }

      const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
      const allCode = codeBlocks.join('\n')
      const settings = JSON.parse(codeBlocks.find((content) => content.includes('"$schema"'))!)

      expect(allCode).not.toContain('CLAUDE_CODE_ATTRIBUTION_HEADER')
      expect(allCode).toContain(trafficSetting)
      expect(settings.env.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC).toBe('1')
      expect(settings.env).not.toHaveProperty('CLAUDE_CODE_ATTRIBUTION_HEADER')
    }
  })

  it('renders Grok Build and OpenCode setup for Grok groups', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-grok-test',
        baseUrl: 'https://example.com/v1',
        platform: 'grok'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const grokTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.grokCli')
    )
    expect(grokTab).toBeDefined()

    const allCode = wrapper.findAll('pre code').map((code) => code.text()).join('\n')
    expect(allCode).toContain('GROK_MODELS_BASE_URL')
    expect(allCode).toContain('XAI_API_KEY')
    expect(allCode).toContain('[model."grok-4.5"]')
    expect(allCode).toContain('[model."grok-build-0.1"]')
    expect(allCode).toContain('[model."grok-4.20-multi-agent-0309"]')
    expect(allCode).toContain('[model."grok-4.3"]')
    expect(allCode).toContain('default = "grok-4.5"')
    expect(allCode).toContain('models_base_url = "https://example.com/v1"')
    expect(allCode).toContain('models_list_url = "https://example.com/v1/models"')
    expect(allCode).toContain('xai_api_base_url = "https://example.com/v1"')
    expect(allCode).toContain('cli_chat_proxy_base_url = "https://example.com/v1"')
    expect(allCode).toContain('preferred_method = "api_key"')
    expect(allCode).toContain('image_description = "grok-4.5"')
    expect(allCode).toContain('auto_compact_threshold_percent = 80')
    expect(allCode).toContain('image_gen = true')
    expect(allCode).toContain('video_gen = true')
    expect(allCode).toContain('image_gen_model_override = "grok-imagine-image-quality"')
    expect(allCode).toContain('image_edit_model_override = "grok-imagine-edit"')
    expect(allCode).toContain('env_key = "XAI_API_KEY"')
    expect(allCode).toContain('Keep api_backend = "responses" on every model entry.')
    expect(allCode).toContain('grok-imagine-image')
    expect(allCode).toContain('grok-imagine-edit')
    expect(allCode).toMatch(/\[model\."grok-4\.5"\][\s\S]*?context_window = 500000/)
    expect(allCode).toMatch(/\[model\."grok-build-0\.1"\][\s\S]*?context_window = 256000/)
    // Prefer env_key; hardcode api_key only as commented alternative
    expect(allCode).not.toMatch(/^api_key = "sk-grok-test"$/m)

    const modelBlocks = allCode
      .split(/(?=^\[model\.)/m)
      .filter((block) => block.startsWith('[model."'))
    expect(modelBlocks.length).toBeGreaterThanOrEqual(4)
    for (const block of modelBlocks) {
      if (block.includes('# [model.')) continue
      expect(block).toContain('api_backend = "responses"')
    }

    const windowsTab = wrapper.findAll('button').find(
      (button) => button.text().trim() === 'Windows'
    )
    expect(windowsTab).toBeDefined()
    await windowsTab!.trigger('click')
    await nextTick()
    expect(wrapper.text().toLowerCase()).toContain('%userprofile%\\.grok\\config.toml')

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )
    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const parsed = JSON.parse(wrapper.find('pre code').text())
    expect(parsed.provider.grok.npm).toBe('@ai-sdk/openai-compatible')
    expect(parsed.provider.grok.name).toBe('Grok via Sub2API')
    expect(parsed.provider.grok.options).toEqual({
      baseURL: 'https://example.com/v1',
      apiKey: 'sk-grok-test'
    })
    expect(parsed.provider.grok.models['grok-4.5']).toBeDefined()
    expect(parsed.provider.grok.models['grok-4.5'].limit.context).toBe(500000)
    expect(parsed.provider.grok.models['grok-build-0.1']).toBeDefined()
    expect(parsed.provider.grok.models['grok-4.20-multi-agent-0309']).toBeDefined()
    expect(parsed.provider.grok.models['grok-composer-2.5-fast']).toBeDefined()
    expect(parsed.provider.grok.models['gpt-5.6']).toBeUndefined()
  })

  it('renders copyable Claude Code setup through the Grok Messages gateway', async () => {
    copyToClipboardMock.mockClear()
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-grok-claude-test',
        baseUrl: 'https://example.com/v1',
        platform: 'grok'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const claudeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.claudeCode')
    )
    expect(claudeTab).toBeDefined()
    await claudeTab!.trigger('click')
    await nextTick()

    let codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    expect(codeBlocks.join('\n')).toContain('ANTHROPIC_BASE_URL="https://example.com"')
    expect(codeBlocks.join('\n')).toContain('ANTHROPIC_AUTH_TOKEN="sk-grok-claude-test"')
    const unixConfig = codeBlocks.find((content) => content.startsWith('export ANTHROPIC_BASE_URL'))
    expect(unixConfig).toBeDefined()
    for (const name of [
      'ANTHROPIC_MODEL',
      'ANTHROPIC_DEFAULT_OPUS_MODEL',
      'ANTHROPIC_DEFAULT_SONNET_MODEL',
      'ANTHROPIC_DEFAULT_HAIKU_MODEL',
      'ANTHROPIC_DEFAULT_FABLE_MODEL',
      'CLAUDE_CODE_SUBAGENT_MODEL'
    ]) {
      expect(unixConfig).toContain(`export ${name}="grok-4.5"`)
    }
    const settingsConfig = codeBlocks.find((content) => content.includes('"$schema"'))
    expect(settingsConfig).toBeDefined()
    const parsedSettings = JSON.parse(settingsConfig!)
    expect(parsedSettings.$schema).toBe('https://json.schemastore.org/claude-code-settings.json')
    expect(parsedSettings.env.ANTHROPIC_MODEL).toBe('grok-4.5')
    expect(codeBlocks.join('\n')).not.toContain('CLAUDE_CODE_ATTRIBUTION_HEADER')
    expect(codeBlocks.join('\n')).toContain('CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC')
    expect(parsedSettings.env.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC).toBe('1')
    expect(parsedSettings.env).not.toHaveProperty('CLAUDE_CODE_ATTRIBUTION_HEADER')
    expect(wrapper.text()).toContain('keys.useKeyModal.claudeSettingsHint')
    expect(wrapper.text()).toContain('keys.useKeyModal.grok.claudeNote')
    expect(wrapper.find('nav[aria-label="Client"]').classes()).toContain('min-w-max')
    expect(wrapper.find('nav[aria-label="Client"]').element.parentElement?.classList.contains('overflow-x-auto')).toBe(true)

    const cmdTab = wrapper.findAll('button').find(
      (button) => button.text().trim() === 'Windows CMD'
    )
    expect(cmdTab).toBeDefined()
    await cmdTab!.trigger('click')
    await nextTick()

    codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    expect(codeBlocks.join('\n')).toContain('set ANTHROPIC_MODEL=grok-4.5')
    expect(codeBlocks.join('\n')).toContain('set ANTHROPIC_DEFAULT_FABLE_MODEL=grok-4.5')
    expect(codeBlocks.join('\n')).toContain('set CLAUDE_CODE_SUBAGENT_MODEL=grok-4.5')
    expect(codeBlocks.join('\n')).toContain('set CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1')
    expect(codeBlocks.join('\n')).not.toContain('CLAUDE_CODE_ATTRIBUTION_HEADER')
    const cmdSettings = JSON.parse(codeBlocks.find((content) => content.includes('"$schema"'))!)
    expect(cmdSettings.env.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC).toBe('1')
    expect(cmdSettings.env).not.toHaveProperty('CLAUDE_CODE_ATTRIBUTION_HEADER')

    const powershellTab = wrapper.findAll('button').find(
      (button) => button.text().trim() === 'PowerShell'
    )
    expect(powershellTab).toBeDefined()
    await powershellTab!.trigger('click')
    await nextTick()

    codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    expect(codeBlocks.join('\n')).toContain('$env:ANTHROPIC_BASE_URL="https://example.com"')
    expect(codeBlocks.join('\n')).toContain('$env:ANTHROPIC_MODEL="grok-4.5"')
    expect(codeBlocks.join('\n')).toContain('$env:ANTHROPIC_DEFAULT_FABLE_MODEL="grok-4.5"')
    expect(codeBlocks.join('\n')).toContain('$env:CLAUDE_CODE_SUBAGENT_MODEL="grok-4.5"')
    expect(codeBlocks.join('\n')).toContain('$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"')
    expect(codeBlocks.join('\n')).not.toContain('CLAUDE_CODE_ATTRIBUTION_HEADER')
    const powershellSettings = JSON.parse(codeBlocks.find((content) => content.includes('"$schema"'))!)
    expect(powershellSettings.env.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC).toBe('1')
    expect(powershellSettings.env).not.toHaveProperty('CLAUDE_CODE_ATTRIBUTION_HEADER')
    expect(wrapper.text()).toContain('%USERPROFILE%\\.claude\\settings.json')

    const copyButton = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.copy')
    )
    expect(copyButton).toBeDefined()
    await copyButton!.trigger('click')
    expect(copyToClipboardMock).toHaveBeenCalledWith(
      expect.stringContaining('ANTHROPIC_AUTH_TOKEN="sk-grok-claude-test"'),
      'keys.copied'
    )
  })

  it('renders Codex custom provider setup through the Grok Responses gateway', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-grok-codex-test',
        baseUrl: 'https://example.com/v1',
        platform: 'grok'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const codexTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.codexCli')
    )
    expect(codexTab).toBeDefined()
    await codexTab!.trigger('click')
    await nextTick()

    let codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    const configToml = codeBlocks.find((content) => content.includes('[model_providers.sub2api]'))
    expect(configToml).toBeDefined()
    expect(configToml).toContain('model_provider = "sub2api"')
    expect(configToml).toContain('model = "grok-4.5"')
    expect(configToml).toContain('base_url = "https://example.com/v1"')
    expect(configToml).toContain('env_key = "SUB2API_API_KEY"')
    expect(configToml).toContain('wire_api = "responses"')
    // API-key provider: Codex must not require a ChatGPT OAuth login.
    expect(configToml).toContain('requires_openai_auth = false')
    expect(configToml).toContain('supports_websockets = false')
    expect(configToml).toContain('grok-4.20-multi-agent-0309 (text / web_search)')
    expect(configToml).toContain('grok-imagine-image')
    expect(configToml).toContain('grok-imagine-video')
    // Hardcoded bearer is only a commented fallback when env cannot be set.
    expect(configToml).toMatch(/# experimental_bearer_token = "sk-grok-codex-test"/)
    expect(configToml).not.toContain('supports_websockets = true')
    expect(configToml).not.toContain('responses_websockets_v2')
    expect(wrapper.text()).not.toContain('auth.json')
    expect(codeBlocks.join('\n')).toContain('SUB2API_API_KEY')

    const windowsTab = wrapper.findAll('button').find(
      (button) => button.text().trim() === 'Windows'
    )
    expect(windowsTab).toBeDefined()
    await windowsTab!.trigger('click')
    await nextTick()

    codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    expect(wrapper.text().toLowerCase()).toContain('%userprofile%\\.codex\\config.toml'.toLowerCase())
    expect(codeBlocks.join('\n')).toContain('experimental_bearer_token = "sk-grok-codex-test"')
  })

  it('keeps legacy OpenAI Codex config as the default', () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    const configToml = codeBlocks.find((content) => content.includes('model_provider = "OpenAI"'))

    expect(configToml).toBeDefined()
    expect(configToml).toContain('model = "gpt-5.6-sol"')
    expect(configToml).toContain('review_model = "gpt-5.6-sol"')
    expect(configToml).not.toContain('model = "gpt-5.5"')
    expect(configToml).not.toContain('model_context_window')
    expect(configToml).not.toContain('model_auto_compact_token_limit')
    expect(configToml).toContain('requires_openai_auth = true')
    expect(configToml).not.toContain('experimental_bearer_token')
    expect(configToml).not.toContain('x-openai-actor-authorization')
    expect(configToml).not.toContain('env_key')
    expect(configToml).not.toContain('image_generation')
    expect(configToml).not.toContain('supports_websockets')
    expect(configToml).not.toContain('responses_websockets_v2')
    expect(configToml).toContain('[features]\ngoals = true')
    expect(configToml).not.toContain('model_reasoning_effort = "xhigh"')
    expect(codeBlocks).toContain('{\n  "OPENAI_API_KEY": "sk-test"\n}')
    expect(wrapper.text()).toContain('auth.json')
    expect(wrapper.find('[data-testid="codex-api-key-restart-notice"]').exists()).toBe(false)
  })

  it('renders API Key Mode authorization in OpenAI Codex config', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const apiKeyMode = wrapper.get('[data-testid="codex-auth-mode-api-key"]')
    await apiKeyMode.trigger('click')
    await nextTick()

    const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    const configToml = codeBlocks.find((content) => content.includes('model_provider = "OpenAI"'))

    expect(apiKeyMode.attributes('aria-checked')).toBe('true')
    expect(configToml).toBeDefined()
    expect(configToml).toContain('requires_openai_auth = false')
    expect(configToml).toContain('experimental_bearer_token = "sk-test"')
    expect(configToml).toContain('http_headers = { "x-openai-actor-authorization" = "local-image-extension" }')
    expect(configToml).not.toContain('env_key')
    expect(configToml).not.toContain('image_generation')
    expect(codeBlocks).not.toContain('{\n  "OPENAI_API_KEY": "sk-test"\n}')
    expect(wrapper.text()).not.toContain('auth.json')

    const restartNotice = wrapper.get('[data-testid="codex-api-key-restart-notice"]')
    expect(restartNotice.text()).toContain(
      'keys.useKeyModal.openai.authModeApiKeyRestartNotice'
    )

    await wrapper.get('[data-testid="codex-auth-mode-legacy"]').trigger('click')
    await nextTick()

    expect(wrapper.find('[data-testid="codex-api-key-restart-notice"]').exists()).toBe(false)
    expect(wrapper.findAll('pre code').map((code) => code.text()).join('\n')).not.toContain(
      'x-openai-actor-authorization'
    )
  })

  it('keeps legacy OpenAI Codex WebSocket config as the default', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const wsTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.codexCliWs')
    )

    expect(wsTab).toBeDefined()
    await wsTab!.trigger('click')
    await nextTick()

    const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    const configToml = codeBlocks.find((content) => content.includes('supports_websockets = true'))

    expect(configToml).toBeDefined()
    expect(configToml).toContain('model = "gpt-5.6-sol"')
    expect(configToml).toContain('review_model = "gpt-5.6-sol"')
    expect(configToml).not.toContain('model = "gpt-5.5"')
    expect(configToml).not.toContain('model_context_window')
    expect(configToml).not.toContain('model_auto_compact_token_limit')
    expect(configToml).toContain('requires_openai_auth = true')
    expect(configToml).not.toContain('experimental_bearer_token')
    expect(configToml).not.toContain('x-openai-actor-authorization')
    expect(configToml).not.toContain('env_key')
    expect(configToml).not.toContain('image_generation')
    expect(configToml).toContain('supports_websockets = true')
    expect(configToml).toContain('[features]\nresponses_websockets_v2 = true\ngoals = true')
    expect(codeBlocks).toContain('{\n  "OPENAI_API_KEY": "sk-test"\n}')
    expect(wrapper.text()).toContain('auth.json')
  })

  it('preserves API Key Mode when switching to OpenAI Codex WebSocket config', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const apiKeyMode = wrapper.get('[data-testid="codex-auth-mode-api-key"]')
    await apiKeyMode.trigger('click')

    const wsTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.codexCliWs')
    )
    expect(wsTab).toBeDefined()
    await wsTab!.trigger('click')
    await nextTick()

    const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    const configToml = codeBlocks.find((content) => content.includes('supports_websockets = true'))

    expect(wrapper.get('[data-testid="codex-auth-mode-api-key"]').attributes('aria-checked')).toBe('true')
    expect(configToml).toBeDefined()
    expect(configToml).toContain('requires_openai_auth = false')
    expect(configToml).toContain('experimental_bearer_token = "sk-test"')
    expect(configToml).toContain('http_headers = { "x-openai-actor-authorization" = "local-image-extension" }')
    expect(configToml).not.toContain('env_key')
    expect(configToml).not.toContain('image_generation')
    expect(configToml).toContain('supports_websockets = true')
    expect(configToml).toContain('[features]\nresponses_websockets_v2 = true\ngoals = true')
    expect(codeBlocks).not.toContain('{\n  "OPENAI_API_KEY": "sk-test"\n}')
    expect(wrapper.text()).not.toContain('auth.json')
  })

  it('resets Codex authentication mode when the modal reopens or platform changes', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    await wrapper.get('[data-testid="codex-auth-mode-api-key"]').trigger('click')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(wrapper.get('[data-testid="codex-auth-mode-legacy"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.findAll('pre code').map((code) => code.text()).join('\n')).toContain('requires_openai_auth = true')

    await wrapper.get('[data-testid="codex-auth-mode-api-key"]').trigger('click')
    await wrapper.setProps({ platform: 'gemini' })
    await wrapper.setProps({ platform: 'openai' })
    await nextTick()

    expect(wrapper.get('[data-testid="codex-auth-mode-legacy"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.findAll('pre code').map((code) => code.text()).join('\n')).not.toContain('x-openai-actor-authorization')
  })

  it('lists only the gpt-5.5, gpt-5.6 family and gpt-6 (astra) in OpenCode config', async () => {
    const wrapper = mountModal('openai')

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )
    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const models = JSON.parse(wrapper.find('pre code').text()).provider.openai.models
    expect(Object.keys(models)).toEqual([
      'gpt-5.5',
      'gpt-5.6',
      'gpt-5.6-sol',
      'gpt-5.6-terra',
      'gpt-5.6-luna',
      'gpt-6',
      'gpt-6-astra'
    ])
    for (const removed of ['gpt-5.2', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.3-codex-spark', 'codex-mini-latest']) {
      expect(models).not.toHaveProperty(removed)
    }

    // gpt-6-astra: limits come from the pricing source; variants mirror the backend
    // Codex catalog, which now advertises max for GPT-6 Astra. "gpt-6" is its alias.
    expect(models['gpt-6-astra'].name).toBe('GPT-6 Astra')
    expect(models['gpt-6-astra'].limit).toEqual({ context: 922000, output: 128000 })
    expect(models['gpt-6-astra'].options).toEqual({ store: false })
    expect(models['gpt-6-astra'].variants).toHaveProperty('max')
    expect(models['gpt-6'].limit).toEqual(models['gpt-6-astra'].limit)
  })

  it('renders GPT-5.6 and GPT-6 Astra capabilities in OpenCode config', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )
    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const parsed = JSON.parse(wrapper.find('pre code').text())
    const models = parsed.provider.openai.models
    for (const model of ['gpt-5.6', 'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna']) {
      expect(models[model]).toBeDefined()
      expect(models[model].variants).toHaveProperty('max')
      expect(models[model].variants).toHaveProperty('xhigh')
    }
    expect(models['gpt-5.6'].name).toBe('GPT-5.6')
    for (const model of ['gpt-5.6', 'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna']) {
      expect(models[model].limit).toEqual({ context: 922000, output: 128000 })
    }
    expect(models['gpt-5.5'].limit).toEqual({ context: 1050000, output: 128000 })
    for (const model of ['gpt-6', 'gpt-6-astra']) {
      expect(models[model].limit).toEqual({ context: 922000, output: 128000 })
      expect(models[model].variants).toEqual({ low: {}, medium: {}, high: {}, xhigh: {}, max: {} })
    }
    expect(models['gpt-6'].name).toBe('GPT-6 (Astra)')
    expect(models['gpt-6-astra'].name).toBe('GPT-6 Astra')
  })

  it('renders Claude Fable 5 OpenCode config with adaptive thinking', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'antigravity'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )

    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const claudeConfig = wrapper.findAll('pre code')
      .map((code) => code.text())
      .find((content) => content.includes('"antigravity-claude"'))

    expect(claudeConfig).toBeDefined()
    const parsed = JSON.parse(claudeConfig!)
    const fable51 = parsed.provider['antigravity-claude'].models['claude-fable-5-1']
    const fable = parsed.provider['antigravity-claude'].models['claude-fable-5']

    expect(fable51.name).toBe('Claude Fable 5.1')
    expect(fable51.limit).toEqual({ context: 1048576, output: 128000 })
    expect(fable51.options.thinking).toEqual({ type: 'adaptive' })
    expect(fable51.options.thinking).not.toHaveProperty('budgetTokens')
    expect(fable.name).toBe('Claude Fable 5')
    expect(fable.limit).toEqual({ context: 1048576, output: 128000 })
    expect(fable.options.thinking).toEqual({ type: 'adaptive' })
    expect(fable.options.thinking).not.toHaveProperty('budgetTokens')
  })

  // Scenario: API Key users can fetch a routed group catalog and reference it from config.toml.
  it('offers a downloadable Codex catalog for Composite API keys', async () => {
    const manifest = {
      models: [
        {
          slug: 'claude-opus-4-8',
          default_reasoning_level: 'medium',
          supported_reasoning_levels: [{ effort: 'max', description: 'Maximum reasoning depth' }],
          input_modalities: ['text'],
          model_messages: { instructions_template: 'Use the routed model.' }
        },
        {
          slug: 'grok-4.6',
          default_reasoning_level: 'high',
          supported_reasoning_levels: [{ effort: 'xhigh', description: 'Extra-high reasoning depth' }],
          input_modalities: ['text'],
          model_messages: { instructions_template: 'Use the routed model.' }
        }
      ]
    }
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => manifest
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-composite-test',
        baseUrl: 'https://example.com/v1',
        platform: 'composite'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const codexTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.codexCli')
    )
    expect(codexTab).toBeDefined()
    await codexTab!.trigger('click')
    await nextTick()

    const unixConfig = wrapper.findAll('pre code')
      .map((code) => code.text())
      .find((content) => content.includes('[model_providers.sub2api]'))
    expect(unixConfig).toContain('model_catalog_json = "~/.codex/codex-models.json"')
    expect(unixConfig).toContain('env_key = "SUB2API_API_KEY"')

    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      'https://example.com/v1/models?client_version=0.147.0',
      expect.objectContaining({
        headers: expect.objectContaining({ Authorization: 'Bearer sk-composite-test' })
      })
    )
    expect(wrapper.get('[data-testid="codex-model-catalog"]').text())
      .toContain('keys.useKeyModal.codexModelCatalog.download')

    const loadedUnixConfig = wrapper.findAll('pre code')
      .map((code) => code.text())
      .find((content) => content.includes('[model_providers.sub2api]'))
    expect(loadedUnixConfig).toContain('model = "claude-opus-4-8"')
    expect(loadedUnixConfig).toContain('review_model = "claude-opus-4-8"')
    expect(loadedUnixConfig).not.toContain('model = "gpt-5.6-sol"')

    const downloadButton = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.codexModelCatalog.download')
    )
    expect(downloadButton).toBeDefined()
    await downloadButton!.trigger('click')
    expect(saveAsMock).toHaveBeenCalledWith(expect.any(Blob), 'codex-models.json')
    const downloadedBlob = saveAsMock.mock.calls[0]?.[0] as Blob
    expect(JSON.parse(await readBlobAsText(downloadedBlob))).toEqual(manifest)

    const windowsTab = wrapper.findAll('button').find((button) => button.text().trim() === 'Windows')
    expect(windowsTab).toBeDefined()
    await windowsTab!.trigger('click')
    await nextTick()

    const windowsConfig = wrapper.findAll('pre code')
      .map((code) => code.text())
      .find((content) => content.includes('[model_providers.sub2api]'))
    expect(windowsConfig).toContain(
      'model_catalog_json = "%userprofile%\\\\.codex\\\\codex-models.json"'
    )
  })

  it.each(['anthropic', 'gemini', 'antigravity', 'kimi', 'zhipu'] as const)(
    'offers Codex catalog configuration for the %s routed group',
    async (platform) => {
      const wrapper = mount(UseKeyModal, {
        props: {
          show: true,
          apiKey: `sk-${platform}-test`,
          baseUrl: 'https://example.com/v1',
          platform
        },
        global: {
          stubs: {
            BaseDialog: {
              template: '<div><slot /><slot name="footer" /></div>'
            },
            Icon: {
              template: '<span />'
            }
          }
        }
      })

      const codexTab = wrapper.findAll('button').find((button) =>
        button.text().includes('keys.useKeyModal.cliTabs.codexCli')
      )
      expect(codexTab).toBeDefined()
      await codexTab!.trigger('click')
      await nextTick()

      expect(wrapper.find('[data-testid="codex-model-catalog"]').exists()).toBe(true)
      const config = wrapper.findAll('pre code')
        .map((code) => code.text())
        .find((content) => content.includes('[model_providers.sub2api]'))
      expect(config).toContain('model_catalog_json = "~/.codex/codex-models.json"')
      expect(config).toContain('base_url = "https://example.com/v1"')
      expect(config).toContain('wire_api = "responses"')
    }
  )

  // Scenario: the platform-preferred model remains selected when the downloaded catalog contains it.
  it('keeps the preferred Composite default when it exists in the catalog', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        models: [
          { slug: 'claude-opus-4-8' },
          { slug: 'gpt-5.6-sol' }
        ]
      })
    }))

    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-composite-test',
        baseUrl: 'https://example.com/v1',
        platform: 'composite'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const codexTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.codexCli')
    )
    expect(codexTab).toBeDefined()
    await codexTab!.trigger('click')
    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    await flushPromises()

    const config = wrapper.findAll('pre code')
      .map((code) => code.text())
      .find((content) => content.includes('[model_providers.sub2api]'))
    expect(config).toContain('model = "gpt-5.6-sol"')
    expect(config).toContain('review_model = "gpt-5.6-sol"')
  })

  it('derives OpenAI Codex reasoning effort from the selected catalog descriptor', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        models: [
          {
            slug: 'glm-5.3',
            default_reasoning_level: 'none',
            supported_reasoning_levels: [{ effort: 'none' }]
          }
        ]
      })
    }))

    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-openai-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    await flushPromises()

    const configToml = wrapper.findAll('pre code')
      .map((code) => code.text())
      .find((content) => content.includes('model_provider = "OpenAI"'))
    expect(configToml).toContain('model = "glm-5.3"')
    expect(configToml).not.toContain('model_reasoning_effort')
  })
  it.each([
    ['anthropic', 'macOS / Linux'],
    ['anthropic', 'Windows'],
    ['antigravity', 'macOS / Linux'],
    ['antigravity', 'Windows']
  ] as const)('defaults the %s routed Codex config to claude-sonnet-5 on %s', async (platform, osTab) => {
    const wrapper = mountModal(platform)
    await clickButton(wrapper, (text) => text.includes('keys.useKeyModal.cliTabs.codexCli'))
    await clickButton(wrapper, (text) => text.trim() === osTab)

    const config = findCodeBlock(wrapper, '[model_providers.sub2api]')
    expect(tomlValue(config, 'model')).toBe('claude-sonnet-5')
    expect(tomlValue(config, 'review_model')).toBe('claude-sonnet-5')
    expect(config).not.toContain('claude-sonnet-4-6')
  })

  it('keeps the OpenAI Codex review_model equal to the selected model', () => {
    const wrapper = mountModal('openai')
    const config = findCodeBlock(wrapper, 'model_provider = "OpenAI"')
    expect(tomlValue(config, 'model')).toBe('gpt-5.6-sol')
    expect(tomlValue(config, 'review_model')).toBe(tomlValue(config, 'model'))
  })

  // Resolve the main model once; review_model must follow even when the catalog
  // forces a fallback or contains Terra alongside the preferred Sol model.
  describe.each([
    ['Codex CLI', 'legacy'],
    ['Codex CLI', 'api-key'],
    ['Codex CLI (WebSocket)', 'legacy'],
    ['Codex CLI (WebSocket)', 'api-key']
  ] as const)('OpenAI %s default models in %s auth mode', (client, authMode) => {
    async function openConfig(catalog: string[] | 'error' | null) {
      if (catalog !== null) stubCatalog(catalog)
      const wrapper = mountModal('openai', 'sk-openai-test')
      if (client === 'Codex CLI (WebSocket)') {
        await clickButton(wrapper, (text) => text.includes('keys.useKeyModal.cliTabs.codexCliWs'))
      }
      if (authMode === 'api-key') {
        await wrapper.get('[data-testid="codex-auth-mode-api-key"]').trigger('click')
        await nextTick()
      }
      if (catalog !== null) {
        await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
        await flushPromises()
      }
      return findCodeBlock(wrapper, 'model_provider = "OpenAI"')
    }

    it.each([
      ['Sol and Terra present', ['gpt-5.6-sol', 'gpt-5.6-terra', 'other'], 'gpt-5.6-sol'],
      ['preferred model present after another model', ['zeta', 'gpt-5.6-sol'], 'gpt-5.6-sol'],
      ['Terra present without Sol', ['zeta', 'gpt-5.6-terra'], 'zeta'],
      ['neither Sol nor Terra present', ['alpha', 'beta'], 'alpha'],
      ['catalog not fetched', null, 'gpt-5.6-sol'],
      ['empty catalog', [], 'gpt-5.6-sol'],
      ['catalog fetch failed', 'error', 'gpt-5.6-sol']
    ] as const)('%s', async (_label, catalog, expectedModel) => {
      const config = await openConfig(catalog === null ? null : catalog === 'error' ? 'error' : [...catalog])
      expect(tomlValue(config, 'model')).toBe(expectedModel)
      expect(tomlValue(config, 'review_model')).toBe(expectedModel)
    })
  })

  it.each([
    ['macOS / Linux', ['gpt-5.6-sol', 'gpt-5.6-terra'], 'gpt-5.6-sol'],
    ['Windows', ['gpt-5.6-sol', 'gpt-5.6-terra'], 'gpt-5.6-sol'],
    ['macOS / Linux', ['gpt-5.6-terra'], 'gpt-5.6-terra'],
    ['Windows', ['gpt-5.6-terra'], 'gpt-5.6-terra']
  ] as const)('keeps Composite review_model aligned on %s with catalog %j', async (osTab, catalog, expectedModel) => {
    stubCatalog([...catalog])
    const wrapper = mountModal('composite')
    await clickButton(wrapper, (text) => text.includes('keys.useKeyModal.cliTabs.codexCli'))
    await clickButton(wrapper, (text) => text.trim() === osTab)
    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    await flushPromises()

    const config = findCodeBlock(wrapper, '[model_providers.sub2api]')
    expect(tomlValue(config, 'model')).toBe(expectedModel)
    expect(tomlValue(config, 'review_model')).toBe(tomlValue(config, 'model'))
  })
  // ---- Per-file download ----------------------------------------------------------
  //
  // Every real config file card (config.toml / auth.json / opencode.json) gets a Download
  // button next to Copy. Shell snippets are not files and get none. The saved Blob is the
  // exact card text, and for TOML/JSON it must parse with the config keys at the root.

  type DownloadedFile = { name: string; text: string; cardText: string; cardPath: string }

  async function downloadAllCards(wrapper: ReturnType<typeof mountModal>): Promise<DownloadedFile[]> {
    saveAsMock.mockClear()
    const buttons = wrapper.findAll('[data-testid="setup-file-download"]')
    const files: DownloadedFile[] = []
    for (const button of buttons) {
      const card = button.element.closest('div.relative') as HTMLElement
      const cardText = card.querySelector('pre code')?.textContent ?? ''
      const cardPath = card.querySelector('span.font-mono')?.textContent ?? ''
      await button.trigger('click')
      const call = saveAsMock.mock.calls.at(-1)
      expect(call).toBeDefined()
      const [blob, name] = call as [Blob, string]
      files.push({ name, text: await readBlobAsText(blob), cardText, cardPath })
    }
    return files
  }

  function expectRootKeys(config: Record<string, unknown>, keys: string[]) {
    for (const key of keys) {
      expect(config, `root key ${key}`).toHaveProperty(key)
    }
  }

  it('does not offer a download for shell snippets', () => {
    const wrapper = mountModal('anthropic')
    // Claude Code setup: a Terminal snippet plus ~/.claude/settings.json — neither is downloadable here.
    expect(wrapper.findAll('[data-testid="setup-file-download"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('Terminal')
  })

  describe.each([
    ['Codex CLI', 'legacy', 'macOS / Linux'],
    ['Codex CLI', 'legacy', 'Windows'],
    ['Codex CLI', 'api-key', 'macOS / Linux'],
    ['Codex CLI', 'api-key', 'Windows'],
    ['Codex CLI (WebSocket)', 'legacy', 'macOS / Linux'],
    ['Codex CLI (WebSocket)', 'legacy', 'Windows'],
    ['Codex CLI (WebSocket)', 'api-key', 'macOS / Linux'],
    ['Codex CLI (WebSocket)', 'api-key', 'Windows']
  ] as const)('OpenAI %s download in %s auth mode on %s', (client, authMode, osTab) => {
    it('downloads config.toml (and auth.json in legacy mode) matching the cards', async () => {
      const wrapper = mountModal('openai', 'sk-download-test')
      if (client === 'Codex CLI (WebSocket)') {
        await clickButton(wrapper, (text) => text.includes('keys.useKeyModal.cliTabs.codexCliWs'))
      }
      if (authMode === 'api-key') {
        await wrapper.get('[data-testid="codex-auth-mode-api-key"]').trigger('click')
        await nextTick()
      }
      await clickButton(wrapper, (text) => text.trim() === osTab)

      const files = await downloadAllCards(wrapper)
      const names = files.map((file) => file.name)
      expect(names).toEqual(authMode === 'legacy' ? ['config.toml', 'auth.json'] : ['config.toml'])

      // The shell/env snippet must never be downloadable.
      expect(wrapper.findAll('[data-testid="setup-file-download"]')).toHaveLength(files.length)

      const toml = files.find((file) => file.name === 'config.toml')!
      expect(toml.text).toBe(toml.cardText)
      expect(toml.cardPath).toMatch(/config\.toml$/)
      const parsed = parseToml(toml.text) as Record<string, unknown>
      // Root-level keys come before any [table]; the parser is the arbiter of that.
      expectRootKeys(parsed, ['model_provider', 'model', 'review_model', 'disable_response_storage', 'model_catalog_json', 'network_access', 'windows_wsl_setup_acknowledged'])
      expect(parsed.model_provider).toBe('OpenAI')
      expect(parsed.model).toBe('gpt-5.6-sol')
      expect(parsed.review_model).toBe(parsed.model)
      expect(parsed.model_catalog_json).toBe(
        osTab === 'Windows' ? '%userprofile%\\.codex\\codex-models.json' : '~/.codex/codex-models.json'
      )
      const providers = parsed.model_providers as Record<string, Record<string, unknown>>
      expect(providers.OpenAI.base_url).toBe('https://example.com/v1')
      expect(providers.OpenAI.wire_api).toBe('responses')
      if (authMode === 'api-key') {
        expect(providers.OpenAI.requires_openai_auth).toBe(false)
        expect(providers.OpenAI.experimental_bearer_token).toBe('sk-download-test')
      } else {
        expect(providers.OpenAI.requires_openai_auth).toBe(true)
        expect(providers.OpenAI).not.toHaveProperty('experimental_bearer_token')
      }
      const features = parsed.features as Record<string, unknown>
      expect(features.goals).toBe(true)
      if (client === 'Codex CLI (WebSocket)') {
        expect(providers.OpenAI.supports_websockets).toBe(true)
        expect(features.responses_websockets_v2).toBe(true)
      } else {
        expect(providers.OpenAI).not.toHaveProperty('supports_websockets')
      }

      if (authMode === 'legacy') {
        const auth = files.find((file) => file.name === 'auth.json')!
        expect(auth.text).toBe(auth.cardText)
        expect(JSON.parse(auth.text)).toEqual({ OPENAI_API_KEY: 'sk-download-test' })
      }
    })
  })

  it('downloads the routed Codex config.toml for Anthropic groups on both OS tabs', async () => {
    for (const osTab of ['macOS / Linux', 'Windows'] as const) {
      const wrapper = mountModal('anthropic', 'sk-anthropic-test')
      await clickButton(wrapper, (text) => text.includes('keys.useKeyModal.cliTabs.codexCli'))
      await clickButton(wrapper, (text) => text.trim() === osTab)

      const files = await downloadAllCards(wrapper)
      expect(files.map((file) => file.name)).toEqual(['config.toml'])
      const parsed = parseToml(files[0].text) as Record<string, unknown>
      expectRootKeys(parsed, ['model_provider', 'model', 'review_model', 'disable_response_storage', 'model_catalog_json'])
      expect(parsed.model_provider).toBe('sub2api')
      expect(parsed.model).toBe('claude-sonnet-5')
      const providers = parsed.model_providers as Record<string, Record<string, unknown>>
      expect(providers.sub2api.env_key).toBe('SUB2API_API_KEY')
      expect(providers.sub2api.requires_openai_auth).toBe(false)
    }
  })

  it.each([
    ['openai', 1],
    ['anthropic', 1],
    ['gemini', 1],
    ['grok', 1],
    ['antigravity', 2]
  ] as const)('downloads opencode.json for %s groups (%i file(s)) as parseable JSON', async (platform, count) => {
    const wrapper = mountModal(platform, 'sk-opencode-test')
    await clickButton(wrapper, (text) => text.includes('keys.useKeyModal.cliTabs.opencode'))

    const files = await downloadAllCards(wrapper)
    expect(files).toHaveLength(count)
    for (const file of files) {
      expect(file.name).toBe('opencode.json')
      expect(file.text).toBe(file.cardText)
      const parsed = JSON.parse(file.text)
      expect(parsed.$schema).toBe('https://opencode.ai/config.json')
      const providers = Object.values(parsed.provider) as Array<{ options: { apiKey: string } }>
      expect(providers).toHaveLength(1)
      expect(providers[0].options.apiKey).toBe('sk-opencode-test')
    }
    if (platform === 'antigravity') {
      expect(files.map((file) => file.cardPath)).toEqual(['opencode.json (Claude)', 'opencode.json (Gemini)'])
    }
  })

  it('downloads the Grok CLI and Grok Codex config.toml as parseable TOML', async () => {
    const wrapper = mountModal('grok', 'sk-grok-download')
    // Grok CLI tab is the default for grok groups.
    let files = await downloadAllCards(wrapper)
    expect(files.map((file) => file.name)).toEqual(['config.toml'])
    let parsed = parseToml(files[0].text) as Record<string, unknown>
    expect(parsed).toHaveProperty('endpoints')
    expect((parsed.models as Record<string, unknown>).default).toBe('grok-4.5')

    await clickButton(wrapper, (text) => text.includes('keys.useKeyModal.cliTabs.codexCli'))
    files = await downloadAllCards(wrapper)
    expect(files.map((file) => file.name)).toEqual(['config.toml'])
    parsed = parseToml(files[0].text) as Record<string, unknown>
    expectRootKeys(parsed, ['model_provider', 'model', 'model_catalog_json'])
    expect(parsed.model_provider).toBe('sub2api')
  })

  it('downloads the current key, not a stale one, after the apiKey prop changes', async () => {
    const wrapper = mountModal('openai', 'sk-first')
    await wrapper.setProps({ apiKey: 'sk-second' })
    await nextTick()

    const files = await downloadAllCards(wrapper)
    const auth = files.find((file) => file.name === 'auth.json')!
    expect(JSON.parse(auth.text)).toEqual({ OPENAI_API_KEY: 'sk-second' })
    expect(files.find((file) => file.name === 'config.toml')!.text).not.toContain('sk-first')
  })
})
