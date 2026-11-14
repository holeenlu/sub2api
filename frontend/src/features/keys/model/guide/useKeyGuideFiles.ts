import { BRAND_NAME, PROJECT_ID, CLIENT_PROVIDER_ID, CLIENT_API_KEY_ENV, CLIENT_BASE_URL_ENV } from '@/shared/config/project'
import type { GroupPlatform } from '@/features/access-policy/api/groupTypes';
import { computed, type Ref } from 'vue'
import type { Composer } from 'vue-i18n'
import {
  findCodexCatalogModel,
  formatCodexReasoningEffortTomlLine,
  parseCodexCatalogModels,
  selectCodexCatalogModel,
  selectCodexConfigReasoningEffort
} from '@/features/keys/model/codexCatalogConfig'

import { joinConfigPath, type FileConfig, type KeyGuideProps, type CodexAuthMode } from './types'
import { buildOpenCodeConfig } from './openCodeConfig'

export function useKeyGuideFiles(props: KeyGuideProps, t: Composer['t'], context: {
  activeTab: Ref<string>; activeClientTab: Ref<string>; codexAuthMode: Ref<CodexAuthMode>;
  codexModelManifestContent: Readonly<Ref<string>>; codexLocalCatalogToml: Readonly<Ref<string>>;
  codexModelCatalogMode: Ref<'remote' | 'file'>; buildCodexModelCatalogUrl: (baseUrl: string) => string
}) {
const { activeTab, activeClientTab, codexAuthMode, codexModelManifestContent,
  codexLocalCatalogToml, codexModelCatalogMode, buildCodexModelCatalogUrl } = context
const generateOpenCodeConfig = (platform: string, baseUrl: string, apiKey: string, pathLabel?: string) => buildOpenCodeConfig(t, platform, baseUrl, apiKey, pathLabel)
const codexCatalogModels = computed(() => parseCodexCatalogModels(codexModelManifestContent.value))

function codexReasoningEffortTomlLine(modelSlug: string): string {
  return formatCodexReasoningEffortTomlLine(
    selectCodexConfigReasoningEffort(findCodexCatalogModel(codexModelManifestContent.value, modelSlug))
  )
}

const escapeHtml = (value: string) => value
  .replace(/&/g, '&amp;')
  .replace(/</g, '&lt;')
  .replace(/>/g, '&gt;')
  .replace(/"/g, '&quot;')
  .replace(/'/g, '&#39;')

const wrapToken = (className: string, value: string) =>
  `<span class="${className}">${escapeHtml(value)}</span>`

const keyword = (value: string) => wrapToken('text-emerald-300', value)
const variable = (value: string) => wrapToken('text-sky-200', value)
const operator = (value: string) => wrapToken('text-slate-400', value)
const string = (value: string) => wrapToken('text-amber-200', value)
const comment = (value: string) => wrapToken('text-slate-500', value)

// Syntax highlighting helpers
// Generate file configs based on platform and active tab
const currentFiles = computed((): FileConfig[] => {
  const baseUrl = props.baseUrl || window.location.origin
  const apiKey = props.apiKey
  const baseRoot = baseUrl.replace(/\/v1\/?$/, '').replace(/\/+$/, '')
  const ensureV1 = (value: string) => {
    const trimmed = value.replace(/\/+$/, '')
    return trimmed.endsWith('/v1') ? trimmed : `${trimmed}/v1`
  }
  const apiBase = ensureV1(baseRoot)
  const antigravityBase = ensureV1(`${baseRoot}/antigravity`)
  const antigravityGeminiBase = (() => {
    const trimmed = `${baseRoot}/antigravity`.replace(/\/+$/, '')
    return trimmed.endsWith('/v1beta') ? trimmed : `${trimmed}/v1beta`
  })()
  const geminiBase = (() => {
    const trimmed = baseRoot.replace(/\/+$/, '')
    return trimmed.endsWith('/v1beta') ? trimmed : `${trimmed}/v1beta`
  })()

  if (activeClientTab.value === 'opencode') {
    switch (props.platform) {
      case 'anthropic':
        return [generateOpenCodeConfig('anthropic', apiBase, apiKey)]
      case 'openai':
        return [generateOpenCodeConfig('openai', apiBase, apiKey)]
      case 'gemini':
        return [generateOpenCodeConfig('gemini', geminiBase, apiKey)]
      case 'antigravity':
        return [
          generateOpenCodeConfig('antigravity-claude', antigravityBase, apiKey, 'opencode.json (Claude)'),
          generateOpenCodeConfig('antigravity-gemini', antigravityGeminiBase, apiKey, 'opencode.json (Gemini)')
        ]
      case 'grok':
        return [generateOpenCodeConfig('grok', apiBase, apiKey)]
      default:
        return [generateOpenCodeConfig('openai', apiBase, apiKey)]
    }
  }

  switch (props.platform) {
    case 'typesafe':
      return [generateSystemOneCurl(baseRoot, apiKey)]
    case 'openai':
      if (activeClientTab.value === 'claude') {
        // Anthropic clients append /v1/messages themselves.
        return generateAnthropicFiles(baseRoot, apiKey)
      }
      if (activeClientTab.value === 'codex-ws') {
        return generateOpenAIWsFiles(apiBase, apiKey)
      }
      // Codex appends /responses directly and does not add /v1.
      return generateOpenAIFiles(apiBase, apiKey)
    case 'gemini':
      if (activeClientTab.value === 'codex') {
        return generateRoutedCodexFiles(apiBase, apiKey, 'gemini')
      }
      return [generateGeminiCliContent(baseUrl, apiKey)]
    case 'antigravity':
      if (activeClientTab.value === 'codex') {
        return generateRoutedCodexFiles(apiBase, apiKey, 'antigravity')
      }
      if (activeClientTab.value === 'gemini') {
        return [generateGeminiCliContent(`${baseUrl}/antigravity`, apiKey)]
      }
      return generateAnthropicFiles(`${baseUrl}/antigravity`, apiKey)
    case 'grok':
      if (activeClientTab.value === 'claude') {
        return generateGrokClaudeFiles(baseRoot, apiKey)
      }
      if (activeClientTab.value === 'codex') {
        return generateGrokCodexFiles(apiBase, apiKey)
      }
      return generateGrokFiles(apiBase, apiKey)
    case 'deepseek':
      if (activeClientTab.value === 'codex') {
        return generateRoutedCodexFiles(apiBase, apiKey, 'deepseek')
      }
      return generateAnthropicFiles(baseRoot, apiKey)
    case 'minimax':
      if (activeClientTab.value === 'codex') {
        return generateRoutedCodexFiles(apiBase, apiKey, 'minimax')
      }
      return generateAnthropicFiles(baseRoot, apiKey)
    case 'composite':
      if (activeClientTab.value === 'codex') {
        return generateRoutedCodexFiles(apiBase, apiKey, 'composite')
      }
      return generateAnthropicFiles(baseRoot, apiKey)
    default:
      if (activeClientTab.value === 'codex' && props.platform) {
        return generateRoutedCodexFiles(apiBase, apiKey, props.platform)
      }
      return generateAnthropicFiles(baseUrl, apiKey)
  }
})

function generateSystemOneCurl(baseUrl: string, apiKey: string): FileConfig {
  const endpoint = `${baseUrl}/v1/systemone`
  const payload = `{
  "model": "jev-latest",
  "state": "Text to evaluate",
  "questions": {
    "safety": {
      "type": "noul",
      "instructions": "Evaluate whether the text is unsafe"
    }
  }
}`
  if (activeTab.value === 'powershell') {
    return {
      path: 'PowerShell',
      content: `$headers = @{ Authorization = "Bearer ${apiKey}" }
$body = @'
${payload}
'@
Invoke-RestMethod -Method Post -Uri "${endpoint}" -Headers $headers -ContentType "application/json" -Body $body`
    }
  }
  if (activeTab.value === 'cmd') {
    return {
      path: 'Command Prompt',
      content: `curl -X POST "${endpoint}" ^
  -H "Authorization: Bearer ${apiKey}" ^
  -H "Content-Type: application/json" ^
  --data "{\"model\":\"jev-latest\",\"state\":\"Text to evaluate\",\"questions\":{\"safety\":{\"type\":\"noul\",\"instructions\":\"Evaluate whether the text is unsafe\"}}}"`
    }
  }
  return {
    path: 'Terminal',
    content: `curl -X POST "${endpoint}" \\
  -H "Authorization: Bearer ${apiKey}" \\
  -H "Content-Type: application/json" \\
  --data '${payload}'`
  }
}

function generateAnthropicFiles(baseUrl: string, apiKey: string): FileConfig[] {
  let path: string
  let content: string

  switch (activeTab.value) {
    case 'unix':
      path = 'Terminal'
      content = `export ANTHROPIC_BASE_URL="${baseUrl}"
export ANTHROPIC_AUTH_TOKEN="${apiKey}"`
      break
    case 'cmd':
      path = 'Command Prompt'
      content = `set ANTHROPIC_BASE_URL=${baseUrl}
set ANTHROPIC_AUTH_TOKEN=${apiKey}`
      break
    case 'powershell':
      path = 'PowerShell'
      content = `$env:ANTHROPIC_BASE_URL="${baseUrl}"
$env:ANTHROPIC_AUTH_TOKEN="${apiKey}"`
      break
    default:
      path = 'Terminal'
      content = ''
  }

  const vscodeSettingsPath = activeTab.value === 'unix'
    ? '~/.claude/settings.json'
    : '%USERPROFILE%\\.claude\\settings.json'

  const vscodeContent = `{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "env": {
    "ANTHROPIC_BASE_URL": "${baseUrl}",
    "ANTHROPIC_AUTH_TOKEN": "${apiKey}"
  }
}`

  return [
    { path, content },
    {
      path: vscodeSettingsPath,
      content: vscodeContent,
      hint: t('keys.useKeyModal.claudeSettingsHint')
    }
  ]
}

function generateGrokClaudeFiles(baseUrl: string, apiKey: string): FileConfig[] {
  const environment = {
    ANTHROPIC_BASE_URL: baseUrl,
    ANTHROPIC_AUTH_TOKEN: apiKey,
    ANTHROPIC_MODEL: 'grok-4.5',
    ANTHROPIC_DEFAULT_OPUS_MODEL: 'grok-4.5',
    ANTHROPIC_DEFAULT_SONNET_MODEL: 'grok-4.5',
    ANTHROPIC_DEFAULT_HAIKU_MODEL: 'grok-4.5',
    ANTHROPIC_DEFAULT_FABLE_MODEL: 'grok-4.5',
    CLAUDE_CODE_SUBAGENT_MODEL: 'grok-4.5',
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1'
  }
  let path: string
  let content: string

  switch (activeTab.value) {
    case 'unix':
      path = 'Terminal'
      content = Object.entries(environment)
        .map(([name, value]) => `export ${name}="${value}"`)
        .join('\n')
      break
    case 'cmd':
      path = 'Command Prompt'
      content = Object.entries(environment)
        .map(([name, value]) => `set ${name}=${value}`)
        .join('\n')
      break
    case 'powershell':
      path = 'PowerShell'
      content = Object.entries(environment)
        .map(([name, value]) => `$env:${name}="${value}"`)
        .join('\n')
      break
    default:
      path = 'Terminal'
      content = ''
  }

  const settingsPath = activeTab.value === 'unix'
    ? '~/.claude/settings.json'
    : '%USERPROFILE%\\.claude\\settings.json'

  return [
    { path, content },
    {
      path: settingsPath,
      content: JSON.stringify({
        $schema: 'https://json.schemastore.org/claude-code-settings.json',
        env: environment
      }, null, 2),
      hint: t('keys.useKeyModal.claudeSettingsHint')
    }
  ]
}

function generateGeminiCliContent(baseUrl: string, apiKey: string): FileConfig {
  const model = 'gemini-2.0-flash'
  const modelComment = t('keys.useKeyModal.gemini.modelComment')
  let path: string
  let content: string
  let highlighted: string

  switch (activeTab.value) {
    case 'unix':
      path = 'Terminal'
      content = `export GOOGLE_GEMINI_BASE_URL="${baseUrl}"
export GEMINI_API_KEY="${apiKey}"
export GEMINI_MODEL="${model}"  # ${modelComment}`
      highlighted = `${keyword('export')} ${variable('GOOGLE_GEMINI_BASE_URL')}${operator('=')}${string(`"${baseUrl}"`)}
${keyword('export')} ${variable('GEMINI_API_KEY')}${operator('=')}${string(`"${apiKey}"`)}
${keyword('export')} ${variable('GEMINI_MODEL')}${operator('=')}${string(`"${model}"`)}  ${comment(`# ${modelComment}`)}`
      break
    case 'cmd':
      path = 'Command Prompt'
      content = `set GOOGLE_GEMINI_BASE_URL=${baseUrl}
set GEMINI_API_KEY=${apiKey}
set GEMINI_MODEL=${model}`
      highlighted = `${keyword('set')} ${variable('GOOGLE_GEMINI_BASE_URL')}${operator('=')}${string(baseUrl)}
${keyword('set')} ${variable('GEMINI_API_KEY')}${operator('=')}${string(apiKey)}
${keyword('set')} ${variable('GEMINI_MODEL')}${operator('=')}${string(model)}
${comment(`REM ${modelComment}`)}`
      break
    case 'powershell':
      path = 'PowerShell'
      content = `$env:GOOGLE_GEMINI_BASE_URL="${baseUrl}"
$env:GEMINI_API_KEY="${apiKey}"
$env:GEMINI_MODEL="${model}"  # ${modelComment}`
      highlighted = `${keyword('$env:')}${variable('GOOGLE_GEMINI_BASE_URL')}${operator('=')}${string(`"${baseUrl}"`)}
${keyword('$env:')}${variable('GEMINI_API_KEY')}${operator('=')}${string(`"${apiKey}"`)}
${keyword('$env:')}${variable('GEMINI_MODEL')}${operator('=')}${string(`"${model}"`)}  ${comment(`# ${modelComment}`)}`
      break
    default:
      path = 'Terminal'
      content = ''
      highlighted = ''
  }

  return { path, content, highlighted }
}

function generateOpenAIFiles(baseUrl: string, apiKey: string): FileConfig[] {
  const isWindows = activeTab.value === 'windows'
  const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'

  const model = selectCodexCatalogModel(codexCatalogModels.value, 'gpt-5.5')
  const reasoningEffortLine = codexReasoningEffortTomlLine(model)

  // config.toml content
  const configContent = `model_provider = "${CLIENT_PROVIDER_ID}"
model = "${model}"
review_model = "${model}"
${reasoningEffortLine}${codexLocalCatalogToml.value}
[model_providers.${CLIENT_PROVIDER_ID}]
name = "${escapeTomlBasicString(BRAND_NAME)}"
base_url = "${escapeTomlBasicString(baseUrl)}"
${codexModelCatalogMode.value === 'remote' ? `model_catalog_url = "${escapeTomlBasicString(buildCodexModelCatalogUrl(baseUrl))}"\n` : ''}wire_api = "responses"
supports_standalone_web_search = true
${generateCodexProviderAuthConfig(apiKey)}

[features]
${codexModelCatalogMode.value === 'remote' ? 'api_key_model_discovery = true\n' : ''}goals = true
standalone_web_search = true`

  return buildOpenAICodexFileConfigs(configDir, configContent, apiKey)
}

function generateCodexProviderAuthConfig(apiKey: string): string {
  if (codexAuthMode.value === 'api-key') {
    return `requires_openai_auth = false
experimental_bearer_token = "${escapeTomlBasicString(apiKey)}"
http_headers = { "x-openai-actor-authorization" = "local-image-extension" }`
  }

  return 'requires_openai_auth = true'
}

function buildOpenAICodexFileConfigs(
  configDir: string,
  configContent: string,
  apiKey: string
): FileConfig[] {
  const files: FileConfig[] = [
    {
      path: `${configDir}/config.toml`,
      content: configContent,
      hint: t('keys.useKeyModal.openai.configTomlHint'),
      downloadName: 'config.toml',
      usesModelCatalog: true
    }
  ]

  if (codexAuthMode.value === 'legacy') {
    files.push({
      path: `${configDir}/auth.json`,
      content: JSON.stringify({ OPENAI_API_KEY: apiKey }, null, 2),
      downloadName: 'auth.json'
    })
  }

  return files
}

function escapeTomlBasicString(value: string): string {
  // JSON escapes are valid TOML basic-string escapes; TOML also excludes raw DEL.
  return JSON.stringify(value).slice(1, -1).replace(/\u007f/g, '\\u007f')
}

function generateGrokFiles(baseUrl: string, apiKey: string): FileConfig[] {
  // Prefer unix/cmd/powershell when shell tabs are shown; fall back to windows tab.
  const shell = activeTab.value
  const isWindowsPath = shell === 'windows' || shell === 'cmd' || shell === 'powershell'
  const configDir = isWindowsPath ? '%userprofile%\\.grok' : '~/.grok'

  let envPath: string
  let envContent: string
  switch (shell) {
    case 'cmd':
      envPath = 'Command Prompt'
      envContent = `set "${CLIENT_BASE_URL_ENV}=${baseUrl}"
set "${CLIENT_API_KEY_ENV}=${apiKey}"
set "GROK_MODELS_BASE_URL=%${CLIENT_BASE_URL_ENV}%"
set "XAI_API_KEY=%${CLIENT_API_KEY_ENV}%"`
      break
    case 'powershell':
    case 'windows':
      envPath = 'PowerShell'
      envContent = `$env:${CLIENT_BASE_URL_ENV}="${baseUrl}"
$env:${CLIENT_API_KEY_ENV}="${apiKey}"
$env:GROK_MODELS_BASE_URL=$env:${CLIENT_BASE_URL_ENV}
$env:XAI_API_KEY=$env:${CLIENT_API_KEY_ENV}`
      break
    default:
      envPath = 'Terminal'
      envContent = `export ${CLIENT_BASE_URL_ENV}="${baseUrl}"
export ${CLIENT_API_KEY_ENV}="${apiKey}"
export GROK_MODELS_BASE_URL="$${CLIENT_BASE_URL_ENV}"
export XAI_API_KEY="$${CLIENT_API_KEY_ENV}"`
  }

  // Shape follows Grok Build user guide (~/.grok/docs + custom-models) and gateway configuration.
  // Text models only (Responses). Image/video: Imagine model IDs on media endpoints / feature overrides.
  // Credential order: api_key field → env_key → signed-in session → XAI_API_KEY global fallback.
  const modelsListUrl = `${baseUrl.replace(/\/+$/, '')}/models`
  const configContent = `# Grok Build CLI → ${PROJECT_ID} Grok group (API key auth).
# Docs: ~/.grok/docs/user-guide/05-configuration.md + 11-custom-models.md
# Verify after save: grok inspect
#
# IMPORTANT: api_backend must be "responses" for ${PROJECT_ID} Grok (POST /v1/responses).
# If omitted, Grok Build defaults to chat_completions (/v1/chat/completions).
# Keep api_backend = "responses" on every model entry.
#
# Prefer env_key over hardcoding api_key (never commit secrets).
# The shell block maps ${CLIENT_BASE_URL_ENV} / ${CLIENT_API_KEY_ENV} to native Grok variables.

# Global inference / catalog endpoints (same role as env GROK_MODELS_BASE_URL).
# When models_base_url is set, Grok uses API-key Bearer auth (no grok login required).
[endpoints]
models_base_url = "${escapeTomlBasicString(baseUrl)}"              # inference base; model list defaults to {base}/models
models_list_url = "${modelsListUrl}"        # optional override (env: GROK_MODELS_LIST_URL)
xai_api_base_url = "${escapeTomlBasicString(baseUrl)}"             # public xAI API base override for gateway routing
cli_chat_proxy_base_url = "${escapeTomlBasicString(baseUrl)}"      # CLI chat-proxy base (env: GROK_CLI_CHAT_PROXY_BASE_URL)

# Prefer API key when using a custom gateway (custom gateway).
# Requires XAI_API_KEY env or per-model env_key / api_key.
[auth]
preferred_method = "api_key"

[model."grok-4.5"]
model = "grok-4.5"                          # id sent to the API
name = "Grok 4.5"                           # shown in /model picker
description = "${escapeTomlBasicString('Grok 4.5 via ' + BRAND_NAME + ' (Responses)')}"
# base_url inherits from [endpoints].models_base_url; override only if needed:
# base_url = "${escapeTomlBasicString(baseUrl)}"
env_key = "${CLIENT_API_KEY_ENV}"                     # or: api_key = "${apiKey}"  (not recommended)
api_backend = "responses"                   # chat_completions | responses | messages
context_window = 500000                     # drives auto-compaction timing
# Optional sampling (global defaults can live under [models] instead):
# temperature = 0.7
# top_p = 0.95
# max_completion_tokens = 8192
# Server-side (backend) web_search tools — only if your gateway exposes them:
supports_backend_search = true

[model."grok-build-0.1"]
model = "grok-build-0.1"
name = "Grok Build"
description = "Coding / agent sessions (xAI recommends grok-build* for coding)"
env_key = "${CLIENT_API_KEY_ENV}"
api_backend = "responses"
context_window = 256000
supports_backend_search = true

# Text multi-agent / client web_search sub-agent (NOT Imagine image/video).
[model."grok-4.20-multi-agent-0309"]
model = "grok-4.20-multi-agent-0309"
name = "Grok 4.20 Multi Agent (text / web_search)"
description = "Text multi-agent; use for web_search sub-agent, not image/video"
env_key = "${CLIENT_API_KEY_ENV}"
api_backend = "responses"
context_window = 1000000
supports_backend_search = true

[model."grok-4.3"]
model = "grok-4.3"
name = "Grok 4.3"
env_key = "${CLIENT_API_KEY_ENV}"
api_backend = "responses"
context_window = 1000000
supports_backend_search = true

# Optional short alias for /model grok:
# [model."grok"]
# model = "grok-4.5"
# name = "Grok"
# env_key = "${CLIENT_API_KEY_ENV}"
# api_backend = "responses"
# context_window = 1000000
# supports_backend_search = true

[models]
# xAI recommends grok-build* for coding/agent sessions; use grok-4.5 for general chat.
default = "grok-4.5"
web_search = "grok-4.5"                     # client-side web_search tool model (must exist as [model.*])
image_description = "grok-4.5"              # vision/describe-image helper model
# Optional environment-wide sampling defaults (per-model values win):
# temperature = 0.7
# top_p = 0.95
# max_completion_tokens = 8192
# max_retries = 8

[session]
auto_compact_threshold_percent = 80         # auto-compact at this % of context_window (default 85)

# Imagine tools: model IDs go to the gateway media endpoints (not the text [model.*] catalog).
# Enable only if the Grok group allows image/video generation.
[features]
image_gen = true
video_gen = true
image_gen_model_override = "grok-imagine-image-quality"   # or grok-imagine-image
image_edit_model_override = "grok-imagine-edit"
# Optional feature flags (defaults shown in docs):
# telemetry = false
# remote_fetch = true                         # set false for air-gapped / pure-gateway catalogs
# lsp_tools = false`

  return [
    { path: envPath, content: envContent },
    {
      path: joinConfigPath(configDir, 'config.toml', isWindowsPath),
      content: configContent,
      hint: t('keys.useKeyModal.grok.configTomlHint')
    }
  ]
}

function generateGrokCodexFiles(baseUrl: string, apiKey: string): FileConfig[] {
  // Codex config reference: wire_api = "responses" only; prefer env_key over experimental_bearer_token.
  // Non-OpenAI gateways should set supports_websockets = false (HTTP/SSE).
  const shell = activeTab.value
  const isWindowsPath = shell === 'windows' || shell === 'cmd' || shell === 'powershell'
  const configDir = isWindowsPath ? '%userprofile%\\.codex' : '~/.codex'
  const model = selectCodexCatalogModel(codexCatalogModels.value, 'grok-4.5')

  let envPath: string
  let envContent: string
  switch (shell) {
    case 'cmd':
      envPath = 'Command Prompt'
      envContent = `set "${CLIENT_BASE_URL_ENV}=${baseUrl}"
set "${CLIENT_API_KEY_ENV}=${apiKey}"`
      break
    case 'powershell':
    case 'windows':
      envPath = 'PowerShell'
      envContent = `$env:${CLIENT_BASE_URL_ENV}="${baseUrl}"
$env:${CLIENT_API_KEY_ENV}="${apiKey}"`
      break
    default:
      envPath = 'Terminal'
      envContent = `export ${CLIENT_BASE_URL_ENV}="${baseUrl}"
export ${CLIENT_API_KEY_ENV}="${apiKey}"`
  }

  const configContent = `# Codex CLI → ${PROJECT_ID} Grok group
# Docs: Codex config reference (model_providers.*, wire_api = "responses")
#
# Text models only. Image/video: grok-imagine-image / grok-imagine-video on media endpoints.
# Switch model: grok-4.5 | grok-4.3 | grok-build-0.1 | grok-4.20-multi-agent-0309 (text / web_search)

model_provider = "${CLIENT_PROVIDER_ID}"
model = "${model}"
${codexLocalCatalogToml.value}# Optional:
# review_model = "${model}"
# model_reasoning_effort = "medium"
# model_context_window = 500000

[model_providers.${CLIENT_PROVIDER_ID}]
name = "${escapeTomlBasicString(BRAND_NAME + ' Grok')}"
base_url = "${escapeTomlBasicString(baseUrl)}"
${codexModelCatalogMode.value === 'remote' ? `model_catalog_url = "${escapeTomlBasicString(buildCodexModelCatalogUrl(baseUrl))}"\n` : ''}# Prefer env_key (variable NAME). Do not combine with experimental_bearer_token.
env_key = "${CLIENT_API_KEY_ENV}"
# Fallback only if you cannot set env (discouraged — keeps secret on disk):
# experimental_bearer_token = "${escapeTomlBasicString(apiKey)}"
wire_api = "responses"
# API-key providers: do not require ChatGPT OAuth login
requires_openai_auth = false
# Grok/gateway path is HTTP/SSE; disable WS (Codex may otherwise try WebSocket first)
supports_websockets = false

${codexModelCatalogMode.value === 'remote' ? '[features]\napi_key_model_discovery = true\n\n# Optional:' : '# Optional:\n# [features]'}
# goals = true`

  return [
    { path: envPath, content: envContent },
    {
      path: joinConfigPath(configDir, 'config.toml', isWindowsPath),
      content: configContent,
      hint: t('keys.useKeyModal.grok.codexConfigTomlHint'),
      downloadName: 'config.toml',
      usesModelCatalog: true
    }
  ]
}

function generateRoutedCodexFiles(
  baseUrl: string,
  apiKey: string,
  platform: GroupPlatform
): FileConfig[] {
  const isWindows = activeTab.value === 'windows'
  const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'
  const preferredModels: Partial<Record<GroupPlatform, string>> = {
    openai: 'gpt-5.5',
    anthropic: 'claude-sonnet-4-6',
    gemini: 'gemini-2.5-pro',
    antigravity: 'claude-sonnet-4-6',
    grok: 'grok-4.5',
    kimi: 'kimi-k2.5',
    zhipu: 'glm-4.7',
    deepseek: 'deepseek-v4-pro',
    minimax: 'MiniMax-M3',
    opencode_go: 'glm-5.3',
    composite: 'gpt-5.5'
  }
  const preferredModel = preferredModels[platform] || ''
  const model = selectCodexCatalogModel(codexCatalogModels.value, preferredModel)
  const labels: Record<GroupPlatform, string> = {
    anthropic: 'Anthropic',
    openai: 'OpenAI',
    gemini: 'Gemini',
    antigravity: 'Antigravity',
    grok: 'Grok',
    kimi: 'Kimi',
    zhipu: 'Zhipu',
    deepseek: 'DeepSeek',
    minimax: 'MiniMax',
    opencode_go: 'OpenCode',
    typesafe: 'TypeSafe / Jev',
    command_code: 'Command Code',
    cline: 'Cline',
    composite: 'Composite'
  }
  const label = labels[platform] || platform
  const envContent = isWindows
    ? `$env:${CLIENT_BASE_URL_ENV}="${baseUrl}"
$env:${CLIENT_API_KEY_ENV}="${apiKey}"`
    : `export ${CLIENT_BASE_URL_ENV}="${baseUrl}"
export ${CLIENT_API_KEY_ENV}="${apiKey}"`

  const configContent = `# Codex CLI -> ${PROJECT_ID} routed group
model_provider = "${CLIENT_PROVIDER_ID}"
model = "${model}"
review_model = "${model}"
${codexLocalCatalogToml.value}
[model_providers.${CLIENT_PROVIDER_ID}]
name = "${escapeTomlBasicString(BRAND_NAME + ' ' + label)}"
base_url = "${escapeTomlBasicString(baseUrl)}"
${codexModelCatalogMode.value === 'remote' ? `model_catalog_url = "${escapeTomlBasicString(buildCodexModelCatalogUrl(baseUrl))}"\n` : ''}env_key = "${CLIENT_API_KEY_ENV}"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false${codexModelCatalogMode.value === 'remote' ? '\n\n[features]\napi_key_model_discovery = true' : ''}`

  return [
    { path: isWindows ? 'PowerShell' : 'Terminal', content: envContent },
    {
      path: joinConfigPath(configDir, 'config.toml', isWindows),
      content: configContent,
      hint: t(
        platform === 'deepseek' || platform === 'minimax' || platform === 'composite'
          ? `keys.useKeyModal.${platform}.codexConfigTomlHint`
          : 'keys.useKeyModal.routedCodex.configTomlHint'
      ),
      downloadName: 'config.toml',
      usesModelCatalog: true
    }
  ]
}

function generateOpenAIWsFiles(baseUrl: string, apiKey: string): FileConfig[] {
  const isWindows = activeTab.value === 'windows'
  const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'
  const model = selectCodexCatalogModel(codexCatalogModels.value, 'gpt-5.5')
  const reasoningEffortLine = codexReasoningEffortTomlLine(model)

  // config.toml content with WebSocket v2
  const configContent = `model_provider = "${CLIENT_PROVIDER_ID}"
model = "${model}"
review_model = "${model}"
${reasoningEffortLine}${codexLocalCatalogToml.value}
[model_providers.${CLIENT_PROVIDER_ID}]
name = "${escapeTomlBasicString(BRAND_NAME)}"
base_url = "${escapeTomlBasicString(baseUrl)}"
${codexModelCatalogMode.value === 'remote' ? `model_catalog_url = "${escapeTomlBasicString(buildCodexModelCatalogUrl(baseUrl))}"\n` : ''}wire_api = "responses"
supports_websockets = true
supports_standalone_web_search = true
${generateCodexProviderAuthConfig(apiKey)}

[features]
${codexModelCatalogMode.value === 'remote' ? 'api_key_model_discovery = true\n' : ''}responses_websockets_v2 = true
goals = true
standalone_web_search = true`

  return buildOpenAICodexFileConfigs(configDir, configContent, apiKey)
}


return { currentFiles }
}
