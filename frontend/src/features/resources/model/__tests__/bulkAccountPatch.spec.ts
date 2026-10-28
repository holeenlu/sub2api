import { describe, expect, it, vi } from 'vitest'
import type { AccountPlatform } from '../../api/platformTypes'
import { buildBulkAccountPatch } from '../bulkAccountPatch'
import type { BulkAccountDraft } from '../bulkAccountPatch'
import type { BulkBPSDraft } from '../bulkOpenAIPatch'
import { bulkTargetEligibility } from '../bulkTargetEligibility'
import type { BulkEditPermissions } from '../bulkTargetEligibility'

const permissions: BulkEditPermissions = {
  allowAuthorize: true, allowTransportEdit: true, allowRateEdit: true
}
const target = (platforms: AccountPlatform[] = ['openai'], types = ['oauth']) =>
  bulkTargetEligibility(platforms, types, (_platform, type) => type === 'apikey')
const draft = (changes: Partial<BulkAccountDraft> = {}): BulkAccountDraft => ({
  fields: {}, credentials: {}, openai: {}, anthropic: {}, ...changes
})
const bps = (changes: Partial<BulkBPSDraft> = {}): BulkBPSDraft => ({
  enabled: true, models: ['gpt-6-astra'],
  cacheCreationAsInput: true, autoDisableOn403: true, autoRecoverOn403: true,
  recoveryIntervalMinutes: 15, omitUnsupportedTools: true, ignoreEncryptedContent: true,
  autoMoveOn403: true, targetGroupID: 0, ...changes
})

describe('bulk target eligibility', () => {
  it.each([
    { platforms: [], types: [] },
    { platforms: ['openai'], types: [] },
    { platforms: ['openai', 'anthropic'], types: ['oauth'] },
    { platforms: ['openai'], types: ['oauth', 'service_account'] }
  ] satisfies { platforms: AccountPlatform[]; types: string[] }[])(
    'does not grant OpenAI eligibility for incomplete or mixed targets: %j', ({ platforms, types }) => {
      const result = target(platforms, types)
      expect(result.openAIPassthrough).toBe(false)
      expect(result.openAIOAuth).toBe(false)
      expect(result.openAIOAuthOnly).toBe(false)
      expect(result.openAIAPIKey).toBe(false)
    }
  )

  it.each([
    { types: ['oauth'], oauth: true, oauthOnly: true, apiKey: false },
    { types: ['setup-token'], oauth: true, oauthOnly: false, apiKey: false },
    { types: ['oauth', 'setup-token'], oauth: true, oauthOnly: false, apiKey: false },
    { types: ['apikey'], oauth: false, oauthOnly: false, apiKey: true },
    { types: ['oauth', 'apikey'], oauth: false, oauthOnly: false, apiKey: false }
  ])('keeps OAuth-only features narrower than native transport: $types', ({ types, oauth, oauthOnly, apiKey }) => {
    expect(target(['openai'], types)).toMatchObject({
      openAIPassthrough: true, openAIOAuth: oauth, openAIOAuthOnly: oauthOnly, openAIAPIKey: apiKey
    })
  })

  it('uses the supplied header policy for every platform/type combination', () => {
    const policy = vi.fn((platform: AccountPlatform, type: string) => platform === 'grok' || type === 'apikey')
    expect(bulkTargetEligibility(['grok', 'openai'], ['apikey', 'oauth'], policy).headerOverride).toBe(false)
    expect(policy.mock.calls).toEqual([
      ['grok', 'apikey'], ['grok', 'oauth'], ['openai', 'apikey'], ['openai', 'oauth']
    ])
    expect(bulkTargetEligibility([], ['apikey'], policy).headerOverride).toBe(false)
    expect(bulkTargetEligibility(['grok'], [], policy).headerOverride).toBe(false)
  })

  it('allows billing probes across API-key platforms but not mixed credential types', () => {
    expect(target(['grok', 'anthropic'], ['apikey']).billingProbe).toBe(true)
    expect(target(['grok'], ['apikey', 'oauth']).billingProbe).toBe(false)
  })
})

describe('bulk patch selection, permissions and merge contract', () => {
  it('does not manufacture defaults or empty JSONB containers when no field is selected', () => {
    expect(buildBulkAccountPatch(draft(), permissions, target())).toBeNull()
    expect(buildBulkAccountPatch(draft({ credentials: { baseUrl: '  ' }, anthropic: { userMsgQueueMode: null } }), permissions, target())).toBeNull()
  })

  it('preserves explicit zero, false and empty collections across the wire', () => {
    const patch = buildBulkAccountPatch(draft({
      fields: { proxyId: null, loadFactor: null, priority: 0, rateMultiplier: 0, groupIds: [], upstreamBillingAutoProbe: 'disabled' },
      credentials: {
        modelRestriction: { mode: 'whitelist', allowedModels: [], mappings: [] },
        compactModelMappings: [], customErrorCodes: [], interceptWarmup: false,
        headerOverride: { enabled: false, rows: [{ name: 'x-old', value: 'ignored' }] }
      }
    }), permissions, target(['openai'], ['apikey']))
    expect(patch).toEqual({
      proxy_id: 0, load_factor: 0, priority: 0, rate_multiplier: 0, group_ids: [], upstream_billing_probe_enabled: false,
      credentials: {
        model_allowlist: null, compact_model_mapping: {}, custom_error_codes_enabled: true,
        custom_error_codes: [], intercept_warmup_requests: false, header_override_enabled: false, header_overrides: {}
      }
    })
    expect(JSON.parse(JSON.stringify(patch))).toEqual(patch)
  })

  it.each([null, NaN, Infinity, -Infinity, 0, -1, 2.5])('retains the existing load-factor clear contract for %s', loadFactor => {
    expect(buildBulkAccountPatch(draft({ fields: { loadFactor } }), permissions, target()))
      .toEqual({ load_factor: loadFactor === 2.5 ? 2.5 : 0 })
  })

  it('filters revoked field permissions while preserving unrestricted operational fields', () => {
    expect(buildBulkAccountPatch(draft({
      fields: { proxyId: 7, concurrency: 5, priority: 0, rateMultiplier: 2, loadFactor: 0, status: 'inactive', groupIds: [], upstreamBillingAutoProbe: 'enabled' },
      credentials: { baseUrl: 'https://example.test', customErrorCodes: [403], interceptWarmup: true, compactModelMappings: [] },
      openai: { cloud: true, bps: bps(), passthrough: true, endpointCapabilities: ['seedance'], responsesMode: 'force_responses', fingerprintMode: 'full' },
      anthropic: { rpm: { enabled: false, baseRpm: null, strategy: 'tiered', stickyBuffer: null }, userMsgQueueMode: 'serialize' }
    }), { allowAuthorize: false, allowTransportEdit: false, allowRateEdit: false }, target())).toEqual({
      concurrency: 5, priority: 0, load_factor: 0, status: 'inactive', group_ids: []
    })
  })

  it('keeps base URL under transport permission and header/model patches under authorization', () => {
    const input = draft({ credentials: {
      baseUrl: ' https://example.test/v1 ',
      headerOverride: { enabled: true, rows: [{ name: ' X-Test ', value: ' kept ' }] },
      modelRestriction: { mode: 'mapping', allowedModels: [], mappings: [{ from: ' gpt-* ', to: ' gpt-6 ' }, { from: 'bad*name', to: 'bad' }] }
    } })
    expect(buildBulkAccountPatch(input, { ...permissions, allowAuthorize: false }, target(['openai'], ['apikey'])))
      .toEqual({ credentials: { base_url: 'https://example.test/v1' } })
    expect(buildBulkAccountPatch(input, { ...permissions, allowTransportEdit: false }, target(['openai'], ['apikey'])))
      .toEqual({ credentials: { model_mapping: { 'gpt-*': 'gpt-6' }, header_override_enabled: true, header_overrides: { 'x-test': 'kept' } } })
  })

  it('preserves exact bulk whitelist names instead of introducing Edit/Create normalization', () => {
    expect(buildBulkAccountPatch(draft({ credentials: {
      modelRestriction: { mode: 'whitelist', allowedModels: ['model-x', 'raw-*', ' raw '], mappings: [] }
    } }), permissions, target(['grok'], ['oauth']))).toEqual({ credentials: { model_mapping: { 'model-x': 'model-x', 'raw-*': 'raw-*', ' raw ': ' raw ' } } })
  })

  it('suppresses selected model restrictions only for an enabled eligible passthrough edit', () => {
    const input = draft({ credentials: { modelRestriction: { mode: 'mapping', allowedModels: [], mappings: [] } }, openai: { passthrough: true } })
    expect(buildBulkAccountPatch(input, permissions, target())).toEqual({ extra: { openai_passthrough: true } })
    expect(buildBulkAccountPatch(input, permissions, target(['grok'], ['oauth']))).toEqual({ credentials: { model_mapping: {} } })
    expect(buildBulkAccountPatch({ ...input, openai: {} }, permissions, target())).toEqual({ credentials: { model_mapping: {} } })
  })

  it('does not let a revoked transport draft suppress an authorized model restriction', () => {
    const input = draft({
      credentials: { modelRestriction: { mode: 'whitelist', allowedModels: ['gpt-6-sol'], mappings: [] } },
      openai: { passthrough: true }
    })
    expect(buildBulkAccountPatch(input, { ...permissions, allowTransportEdit: false }, target()))
      .toEqual({ credentials: { model_allowlist: ['gpt-6-sol'] } })
    expect(input.openai.passthrough).toBe(true)
  })

  it('does not let revoked capability drafts clear an independently authorized Responses route', () => {
    const input = draft({ openai: { endpointCapabilities: ['embeddings'], responsesMode: 'force_responses' } })
    expect(buildBulkAccountPatch(input, { ...permissions, allowAuthorize: false }, target(['openai'], ['apikey'])))
      .toEqual({ extra: { openai_responses_mode: 'force_responses' } })
    expect(input.openai.endpointCapabilities).toEqual(['embeddings'])
  })

  it('detaches every outgoing array and mapping from subsequent form edits', () => {
    const groupIds = [3], errors = [403], capabilities: ('seedance' | 'embeddings')[] = ['seedance']
    const mappings = [{ from: 'model-a', to: 'model-b' }], rows = [{ name: 'x-test', value: 'before' }]
    const input = draft({ fields: { groupIds }, credentials: { customErrorCodes: errors, compactModelMappings: mappings, headerOverride: { enabled: true, rows } }, openai: { endpointCapabilities: capabilities } })
    const patch = buildBulkAccountPatch(input, permissions, target(['openai'], ['apikey']))
    groupIds.push(4); errors.push(500); capabilities.push('embeddings')
    mappings[0].to = 'after'; rows[0].value = 'after'
    expect(patch).toEqual({ group_ids: [3], credentials: {
      custom_error_codes_enabled: true, custom_error_codes: [403], compact_model_mapping: { 'model-a': 'model-b' },
      header_override_enabled: true, header_overrides: { 'x-test': 'before' }, openai_capabilities: ['seedance']
    }, extra: { openai_responses_mode: null } })
  })
})

describe('bulk OpenAI patches', () => {
  it.each([
    { models: [], allModels: false, expected: [] },
    { models: [' gpt-6-astra ', '', 'gpt-6-astra', 'gpt-6-sol'], allModels: false, expected: ['gpt-6-astra', 'gpt-6-sol'] },
    { models: ['explicit'], allModels: true, expected: ['explicit'] },
    { models: undefined, allModels: true, expected: [] },
    { models: null, allModels: true, expected: [] },
    { models: 'model', allModels: false, expected: [] },
    { models: [42, null, ' a ', '', 'a'], allModels: true, expected: ['a'] }
  ])('uses only explicit BPS selections even with obsolete flags: %j', ({ models, allModels, expected }) => {
    const patch = buildBulkAccountPatch(draft({ openai: { bps: Object.assign(bps(), { models, allModels }) } }), permissions, target())
    expect(patch?.extra).toEqual({
      openai_excel_bps: true, openai_excel_bps_models: expected,
      openai_excel_bps_cache_creation_as_input: true, openai_excel_bps_auto_disable_on_403: true,
      openai_excel_bps_auto_recover_on_403: true, openai_excel_bps_403_recovery_interval_minutes: 15,
      openai_excel_bps_omit_unsupported_tools: true, openai_excel_bps_ignore_encrypted_content: true,
      openai_excel_bps_auto_move_on_403: true, openai_excel_bps_403_target_group_id: 0
    })
    if (Array.isArray(models)) models.push('later')
    expect(patch?.extra?.openai_excel_bps_models).toEqual(expected)
  })

  it('clears BPS fields through JSONB merge without erasing unrelated extra keys', () => {
    const patch = buildBulkAccountPatch(draft({ openai: { bps: bps({ enabled: false }) } }), permissions, target())
    expect({ unrelated: 'keep', openai_excel_bps_models: ['old'], ...patch?.extra }).toEqual({
      unrelated: 'keep', openai_excel_bps: false, openai_excel_bps_models: null,
      openai_excel_bps_cache_creation_as_input: false, openai_excel_bps_auto_disable_on_403: false,
      openai_excel_bps_auto_recover_on_403: false, openai_excel_bps_403_recovery_interval_minutes: 15,
      openai_excel_bps_omit_unsupported_tools: false, openai_excel_bps_ignore_encrypted_content: false,
      openai_excel_bps_auto_move_on_403: false, openai_excel_bps_403_target_group_id: null
    })
  })

  it('gates 403 recovery by auto-disable and clears an unselected move target', () => {
    const patch = buildBulkAccountPatch(draft({ openai: { bps: bps({ autoDisableOn403: false, autoMoveOn403: false, recoveryIntervalMinutes: 'invalid' }) } }), permissions, target())
    expect(patch?.extra).toMatchObject({
      openai_excel_bps_auto_recover_on_403: false, openai_excel_bps_403_recovery_interval_minutes: 60,
      openai_excel_bps_auto_move_on_403: false, openai_excel_bps_403_target_group_id: null
    })
  })

  it.each(['setup-token', 'apikey', 'service_account'])('omits Cloud/BPS/namespace edits for %s targets', type => {
    expect(buildBulkAccountPatch(draft({ openai: { cloud: true, bps: bps(), flattenNamespaces: true } }), permissions, target(['openai'], [type]))).toBeNull()
  })

  it('omits supplier Cloud writes while retaining an explicitly selected OAuth BPS edit', () => {
    const patch = buildBulkAccountPatch(draft({ openai: { cloud: false, bps: bps() } }), { ...permissions, supplierScope: true }, target())
    expect(patch?.extra).not.toHaveProperty('openai_cloud')
    expect(patch?.extra?.openai_excel_bps).toBe(true)
  })

  it('resets native options explicitly and leaves unrelated JSONB keys untouched', () => {
    const patch = buildBulkAccountPatch(draft({ openai: {
      cloud: false, passthrough: false, flattenNamespaces: false, longContextBilling: false,
      oauthWSMode: 'off', apiKeyWSMode: 'ctx_pool', codexCLIOnly: false,
      codexCLIOnlyAppServer: true, fingerprintMode: 'off', compactMode: 'auto'
    } }), permissions, target())
    expect(patch).toEqual({ extra: {
      openai_cloud: false, openai_passthrough: false, openai_oauth_passthrough: false,
      openai_responses_flatten_namespaces: false, openai_long_context_billing_enabled: false,
      openai_oauth_responses_websockets_v2_mode: 'off', openai_oauth_responses_websockets_v2_enabled: false,
      codex_cli_only: false, codex_fingerprint_mode: 'off', openai_compact_mode: 'auto'
    } })
  })

  it.each(['off', 'ctx_pool', 'passthrough', 'http_bridge'] as const)('keeps API-key WS mode and enabled flag paired: %s', mode => {
    expect(buildBulkAccountPatch(draft({ openai: { apiKeyWSMode: mode, oauthWSMode: mode } }), permissions, target(['openai'], ['apikey'])))
      .toEqual({ extra: { openai_apikey_responses_websockets_v2_mode: mode, openai_apikey_responses_websockets_v2_enabled: mode !== 'off' } })
  })

  it.each([undefined, false, true])('writes the CLI child only with an explicitly enabled parent: %s', codexCLIOnly => {
    const patch = buildBulkAccountPatch(draft({ openai: { codexCLIOnly, codexCLIOnlyAppServer: false } }), permissions, target())
    if (codexCLIOnly === true) expect(patch).toEqual({ extra: { codex_cli_only: true, codex_cli_only_allow_app_server: false } })
    else if (codexCLIOnly === false) expect(patch).toEqual({ extra: { codex_cli_only: false } })
    else expect(patch).toBeNull()
  })

  it('retains two-capability Seedance lists while restoring the text/embedding defaults with null', () => {
    expect(buildBulkAccountPatch(draft({ openai: { endpointCapabilities: ['chat_completions', 'embeddings'] } }), permissions, target(['openai'], ['apikey'])))
      .toEqual({ credentials: { openai_capabilities: null } })
    expect(buildBulkAccountPatch(draft({ openai: { endpointCapabilities: ['chat_completions', 'seedance'] } }), permissions, target(['openai'], ['apikey'])))
      .toEqual({ credentials: { openai_capabilities: ['chat_completions', 'seedance'] } })
  })

  it('clears Responses routing when authorization removes text capability even without transport permission', () => {
    expect(buildBulkAccountPatch(draft({ openai: { endpointCapabilities: ['embeddings'], responsesMode: 'force_responses' } }),
      { ...permissions, allowTransportEdit: false }, target(['openai'], ['apikey'])))
      .toEqual({ credentials: { openai_capabilities: ['embeddings'] }, extra: { openai_responses_mode: null } })
  })

  it('keeps independently selected Responses mode when capabilities are not being edited', () => {
    expect(buildBulkAccountPatch(draft({ openai: { responsesMode: 'force_responses' } }), permissions, target(['openai'], ['apikey'])))
      .toEqual({ extra: { openai_responses_mode: 'force_responses' } })
    expect(buildBulkAccountPatch(draft({ openai: { responsesMode: 'auto' } }), permissions, target(['openai'], ['apikey'])))
      .toEqual({ extra: { openai_responses_mode: null } })
  })
})

describe('bulk Anthropic rate-limit patches', () => {
  it('clears RPM and legacy UMQ fields with explicit merge values', () => {
    expect(buildBulkAccountPatch(draft({ anthropic: { rpm: { enabled: false, baseRpm: 100, strategy: 'tiered', stickyBuffer: 10 }, userMsgQueueMode: '' } }), permissions, target(['anthropic'], ['oauth', 'setup-token'])))
      .toEqual({ extra: { base_rpm: 0, rpm_strategy: '', rpm_sticky_buffer: 0, user_msg_queue_mode: '', user_msg_queue_enabled: false } })
  })

  it('updates UMQ without selecting RPM and preserves an existing buffer if no positive replacement is supplied', () => {
    expect(buildBulkAccountPatch(draft({ anthropic: { userMsgQueueMode: 'serialize' } }), permissions, target(['anthropic'], ['oauth'])))
      .toEqual({ extra: { user_msg_queue_mode: 'serialize', user_msg_queue_enabled: false } })
    expect(buildBulkAccountPatch(draft({ anthropic: { rpm: { enabled: true, baseRpm: 20, strategy: 'sticky_exempt', stickyBuffer: null } } }), permissions, target(['anthropic'], ['setup-token'])))
      .toEqual({ extra: { base_rpm: 20, rpm_strategy: 'sticky_exempt' } })
  })

  it.each(['apikey', 'bedrock', 'service_account'])('does not send stale RPM/UMQ drafts to %s accounts', type => {
    expect(buildBulkAccountPatch(draft({ anthropic: { rpm: { enabled: true, baseRpm: 20, strategy: 'tiered', stickyBuffer: 5 }, userMsgQueueMode: 'throttle' } }), permissions, target(['anthropic'], [type]))).toBeNull()
  })
})
