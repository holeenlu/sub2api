import { accountFixture, commandFixture, translate } from './boundaryFixtures'
import { expect, it, vi } from 'vitest'
import { ref, computed } from 'vue'
import { applyCredentialUpdate } from '../editCredentialPayload'
import type { Account } from '@/features/resources/api/accountTypes';

type Kind = 'apikey' | 'upstream' | 'vertex' | 'bedrock-key' | 'bedrock-sigv4'
function setup(kind: Kind, allowAuthorize: boolean, redacted = false) {
  const credentials = redacted ? {} : { api_key: 'existing-key', aws_access_key_id: 'existing-id', aws_secret_access_key: 'existing-secret', aws_session_token: 'existing-session' }
  const type: Account['type'] = kind === 'vertex' ? 'service_account' : kind === 'bedrock-key' || kind === 'bedrock-sigv4' ? 'bedrock' : kind
  const account = accountFixture({ platform: kind === 'vertex' ? 'gemini' : 'anthropic', type, credentials, credentials_status: { has_api_key: true } })
  // Values are a projection of the actual credential builder input; no account API is invoked.
  const input = {
    props: { account, allowAuthorize },
    editApiKey: ref(' new-key '), editBaseUrl: ref('https://relay.example.invalid'), defaultBaseUrl: computed(() => 'https://relay.example.invalid'),
    openaiPassthroughEnabled: ref(false), isCNApiKeyAccount: computed(() => false),
    buildModelRestrictionMapping: () => null, poolModeEnabled: ref(false),
    explicitCacheUpstreamEnabled: ref(false), explicitCacheUpstreamMaxTTL: ref(0),
    customErrorCodesEnabled: ref(false), isHeaderOverrideCapable: () => false,
    interceptWarmupRequests: ref(false), applyAccountSchedulingThresholdOverridePatch: vi.fn(), applyTempUnschedConfig: () => true,
    isVertexAPIKeyMode: computed(() => true), editVertexApiKey: ref(' new-key '), editVertexProjectId: ref(''), editVertexLocation: ref('global'),
    editBedrockRegion: ref('us-east-1'), editBedrockForceGlobal: ref(false), isBedrockAPIKeyMode: computed(() => kind === 'bedrock-key'),
    editBedrockApiKeyValue: ref(' new-key '), editBedrockAccessKeyId: ref('new-id'), editBedrockSecretAccessKey: ref('new-secret'), editBedrockSessionToken: ref('new-session'),
    appStore: { showError: vi.fn(), showSuccess: vi.fn() }, t: translate,
  }
  const build = () => {
    const payload: Record<string, unknown> = {}
    expect(applyCredentialUpdate(commandFixture<Parameters<typeof applyCredentialUpdate>[0]>(input), account, payload)).toBe(true)
    return payload.credentials as Record<string, unknown>
  }
  return { input, build }
}

it.each<Kind>(['apikey', 'upstream', 'vertex', 'bedrock-key', 'bedrock-sigv4'])('%s ignores stale authorization drafts when allowAuthorize=false', kind => {
  const { build } = setup(kind, false)
  const result = build()
  expect(result.api_key).toBe('existing-key')
  expect(result.aws_access_key_id).toBe('existing-id')
  expect(result.aws_secret_access_key).toBe('existing-secret')
  expect(result.aws_session_token).toBe('existing-session')
})

it.each<Kind>(['apikey', 'upstream', 'vertex', 'bedrock-key', 'bedrock-sigv4'])('%s does not manufacture redacted credential fields in a delegated edit', kind => {
  const { build } = setup(kind, false, true)
  const result = build()
  for (const field of ['api_key', 'aws_access_key_id', 'aws_secret_access_key', 'aws_session_token']) expect(result).not.toHaveProperty(field)
})

it.each<Kind>(['apikey', 'upstream', 'vertex', 'bedrock-key', 'bedrock-sigv4'])('%s retains existing secrets when authorized credential inputs are blank', kind => {
  const { input, build } = setup(kind, true)
  input.editApiKey.value = ''
  input.editVertexApiKey.value = ''
  input.editBedrockApiKeyValue.value = ''
  input.editBedrockSecretAccessKey.value = ''
  input.editBedrockSessionToken.value = ''
  const result = build()
  expect(result.api_key).toBe('existing-key')
  expect(result.aws_secret_access_key).toBe('existing-secret')
  expect(result.aws_session_token).toBe('existing-session')
})

it.each<Kind>(['apikey', 'upstream', 'vertex', 'bedrock-key'])('%s still accepts an explicitly authorized key replacement', kind => {
  expect(setup(kind, true).build().api_key).toBe('new-key')
})
