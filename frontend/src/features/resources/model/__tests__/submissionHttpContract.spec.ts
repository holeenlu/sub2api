import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios, { AxiosError, AxiosHeaders, type AxiosAdapter, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import { ref } from 'vue'
import { apiClient, configureHttpClient } from '@/shared/http'
import type { SessionRequestConfig } from '@/shared/http/client'
import { installApplicationHttp } from '@/app/http/install'
import { readApiError } from '@/shared/lib/apiError'
import { globalAdminStepUp, isStepUpCancelled, StepUpCancelledError, useStepUp } from '@/features/identity/composables/useStepUp'
import accountsAPI from '../../accounts'
import { createResourceTaskScope } from '../../queries/resourceTaskScope'
import { useCreateSubmissionDraft } from '../createSubmission'
import { useEditSubmissionDraft, type EditSubmissionInput } from '../editSubmission'
import { accountFixture, commandFixture, translate } from './boundaryFixtures'

type Kind = 'create' | 'edit'
type ResponseMode = 'mixed' | 'step-up' | 'blocked' | 'failure'
interface SentRequest {
  method: string | undefined
  url: string | undefined
  body: Record<string, unknown>
  stepUpRetried: boolean
}

const originalAdapter = apiClient.defaults.adapter
const originalBaseURL = apiClient.defaults.baseURL
const mixedMessage = 'mixed_channel_warning: fixture group contains conflicting channels'
let mode: ResponseMode
let verified: boolean
let cancelled: InstanceType<typeof axios.CanceledError> | undefined
let requests: SentRequest[]
const responseObservers: number[] = []
const taskScopes: ReturnType<typeof createResourceTaskScope>[] = []

// Replace only the network boundary. Accounts API, response flattening, app
// recovery policy, global step-up controller and submission models are real.
// The 409 body matches admin/account_handler.go Create and Update exactly.
const adapter: AxiosAdapter = async (config: InternalAxiosRequestConfig) => {
  if (!['/admin/accounts', '/admin/accounts/7'].includes(config.url || '')) {
    throw new Error('Unexpected request in submission contract test: ' + config.url)
  }
  const body = typeof config.data === 'string' ? JSON.parse(config.data) : config.data
  requests.push({ method: config.method, url: config.url, body, stepUpRetried: (config as SessionRequestConfig)._stepUpRetried === true })
  if (cancelled) throw cancelled
  let status = 200
  let data: unknown = { code: 0, data: accountFixture() }
  if (mode === 'mixed' && body.confirm_mixed_channel_risk !== true) {
    status = 409
    data = { error: 'mixed_channel_warning', message: mixedMessage }
  } else if (mode === 'step-up' && !verified) {
    status = 403
    data = { code: 403, reason: 'STEP_UP_REQUIRED', message: 'Verification required' }
  } else if (mode === 'blocked') {
    status = 403
    data = { code: 403, reason: 'STEP_UP_ADMIN_API_KEY_FORBIDDEN', message: 'Step-up forbidden' }
  } else if (mode === 'failure') {
    status = 503
    data = { code: 503, message: 'Fixture service unavailable' }
  }
  const response: AxiosResponse = { data, status, statusText: String(status), config, headers: new AxiosHeaders({ 'Content-Type': 'application/json' }) }
  if (status >= 400) throw new AxiosError('Request failed with status code ' + status, (status >= 500 ? AxiosError.ERR_BAD_RESPONSE : AxiosError.ERR_BAD_REQUEST), config, undefined, response)
  return response
}

beforeEach(() => {
  mode = 'mixed'; verified = false; cancelled = undefined; requests = []
  localStorage.clear()
  localStorage.setItem('auth_token', 'fixture-session-token')
  localStorage.setItem('auth_user', JSON.stringify({ id: 1, role: 'super_admin', policy_version: 1 }))
  apiClient.defaults.baseURL = 'https://adapter-only.example.test/api/v1'
  apiClient.defaults.adapter = adapter
  installApplicationHttp()
})

afterEach(() => {
  globalAdminStepUp.onCancel()
  responseObservers.splice(0).forEach(id => apiClient.interceptors.response.eject(id))
  taskScopes.splice(0).forEach(scope => scope.dispose())
  apiClient.defaults.adapter = originalAdapter
  apiClient.defaults.baseURL = originalBaseURL
  configureHttpClient({})
  localStorage.clear()
  vi.restoreAllMocks()
})

const payload = { name: 'fixture account', platform: 'antigravity' as const, type: 'oauth' as const, credentials: { refresh_token: 'fixture-only' }, group_ids: [3] }

function setup(kind: Kind) {
  let canAuthorize = true
  const resourceTasks = createResourceTaskScope(() => 'admin-fixture:' + canAuthorize)
  taskScopes.push(resourceTasks)
  const confirmed = ref(false)
  const dialogs: { message?: string; onConfirm(): Promise<void> }[] = []
  const notices = { showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }
  const input = {
    resourceTasks,
    props: { account: accountFixture(), allowRateEdit: true, showCost: true, allowTransportEdit: true, allowAuthorize: true },
    form: { ...payload, notes: '', proxy_id: null, concurrency: 1, load_factor: null, priority: 50, rate_multiplier: 1, expires_at: null, status: 'active' as const },
    upstreamModelsPreviewed: ref(false), antigravityMixedChannelConfirmed: confirmed,
    clearMixedChannelDialog: vi.fn(), emit: vi.fn(),
    withAntigravityConfirmFlag: <T extends object>(value: T): T => confirmed.value ? { ...value, confirm_mixed_channel_risk: true } : value,
    persistGrokMediaEligibility: vi.fn(async (_id: number, account: ReturnType<typeof accountFixture>) => account),
    appStore: notices, t: translate, needsMixedChannelCheck: () => true,
    openMixedChannelDialog: (dialog: { message?: string; onConfirm(): Promise<void> }) => dialogs.push(dialog),
  }
  const draft = kind === 'create'
    ? useCreateSubmissionDraft(commandFixture<Parameters<typeof useCreateSubmissionDraft>[0]>(input))
    : useEditSubmissionDraft(commandFixture<EditSubmissionInput>(input))
  const submit = () => 'submitCreateAccount' in draft ? draft.submitCreateAccount(payload) : draft.submitUpdateAccount(7, payload)
  return { submit, draft, dialogs, notices, confirmed, emit: input.emit,
    revoke: () => { canAuthorize = false; input.props.allowAuthorize = false },
  }
}

function observeRejectedError() {
  let observed: unknown
  responseObservers.push(apiClient.interceptors.response.use(undefined, (error: unknown) => { observed = error; throw error }))
  return () => observed
}

describe('account submission over the real shared HTTP contract', () => {
  it('flattens the backend 409 response once at the HTTP boundary', async () => {
    const error = await accountsAPI.create(payload).catch((error: unknown) => error)
    expect(error).toMatchObject({ status: 409, error: 'mixed_channel_warning', message: mixedMessage })
    expect(error).not.toHaveProperty('response')
    expect(readApiError(error).response).toBeUndefined()
    expect(requests).toHaveLength(1)
  })

  it.each<Kind>(['create', 'edit'])('%s confirms the real 409 and retries only the original payload', async kind => {
    const h = setup(kind)
    await h.submit()
    expect(requests).toHaveLength(1)
    expect(requests[0].body).toEqual(payload)
    expect(h.dialogs).toHaveLength(1)
    expect(h.dialogs[0].message).toBe(mixedMessage)
    expect(h.notices.showError).not.toHaveBeenCalled()
    expect(h.emit).not.toHaveBeenCalled()
    await h.dialogs[0].onConfirm()
    expect(requests).toHaveLength(2)
    expect(requests[1]).toMatchObject({ method: kind === 'create' ? 'post' : 'put', url: kind === 'create' ? '/admin/accounts' : '/admin/accounts/7', body: { ...payload, confirm_mixed_channel_risk: true } })
    expect(h.notices.showSuccess).toHaveBeenCalledOnce()
  })

  it.each<Kind>(['create', 'edit'])('%s does not send a confirmation after authorization is revoked', async kind => {
    const h = setup(kind)
    await h.submit()
    expect(h.dialogs).toHaveLength(1)
    h.revoke()
    await h.dialogs[0].onConfirm()
    expect(requests).toHaveLength(1)
    expect(h.confirmed.value).toBe(false)
    expect(h.notices.showSuccess).not.toHaveBeenCalled()
    expect(h.emit).not.toHaveBeenCalled()
  })

  it.each<Kind>(['create', 'edit'])('%s silently ends when the actual step-up dialog is cancelled', async kind => {
    mode = 'step-up'
    const h = setup(kind)
    const pending = h.submit()
    await vi.waitFor(() => expect(globalAdminStepUp.visible.value).toBe(true))
    expect(requests).toHaveLength(1)
    globalAdminStepUp.onCancel()
    await pending
    expect(requests).toHaveLength(1)
    expect(h.notices.showError).not.toHaveBeenCalled()
    expect(h.notices.showSuccess).not.toHaveBeenCalled()
    expect(h.emit).not.toHaveBeenCalled()
    expect(h.draft.submitting.value).toBe(false)
  })

  it.each<Kind>(['create', 'edit'])('%s silently ends an original Axios cancellation', async kind => {
    cancelled = new axios.CanceledError('fixture cancellation')
    const h = setup(kind)
    await h.submit()
    expect(requests).toHaveLength(1)
    expect(h.notices.showError).not.toHaveBeenCalled()
    expect(h.notices.showSuccess).not.toHaveBeenCalled()
    expect(h.emit).not.toHaveBeenCalled()
    expect(h.draft.submitting.value).toBe(false)
  })

  it.each<Kind>(['create', 'edit'])('%s preserves the application step-up retry after verification', async kind => {
    mode = 'step-up'
    const h = setup(kind)
    const pending = h.submit()
    await vi.waitFor(() => expect(globalAdminStepUp.visible.value).toBe(true))
    verified = true
    globalAdminStepUp.onVerified()
    await pending
    expect(requests).toHaveLength(2)
    expect(requests[0].stepUpRetried).toBe(false)
    expect(requests[1].stepUpRetried).toBe(true)
    expect(requests[1].body).toEqual(payload)
    expect(h.notices.showError).not.toHaveBeenCalled()
    expect(h.notices.showSuccess).toHaveBeenCalledOnce()
  })

  it.each<Kind>(['create', 'edit'])('%s still reports a non-cancellation 403 and an ordinary server failure', async kind => {
    for (const failure of ['blocked', 'failure'] as const) {
      mode = failure; requests = []
      const h = setup(kind)
      await h.submit()
      expect(globalAdminStepUp.visible.value).toBe(false)
      expect(requests).toHaveLength(1)
      expect(h.notices.showError).toHaveBeenCalledTimes(1)
      expect(h.notices.showError).toHaveBeenCalledWith(failure === 'blocked' ? 'Step-up forbidden' : 'Fixture service unavailable')
      expect(h.notices.showSuccess).not.toHaveBeenCalled()
      expect(h.dialogs).toHaveLength(0)
    }
  })

  it('preserves Axios cancellation identity through the HTTP interceptor', async () => {
    cancelled = new axios.CanceledError('fixture cancellation')
    const error = await accountsAPI.create(payload).catch((error: unknown) => error)
    expect(error).toBe(cancelled)
  })

  it('preserves the real step-up cancellation identity through caller error classification', async () => {
    mode = 'step-up'
    const observed = observeRejectedError()
    const stepUp = useStepUp()
    const pending = stepUp.run(() => accountsAPI.create(payload)).catch((error: unknown) => error)
    await vi.waitFor(() => expect(globalAdminStepUp.visible.value).toBe(true))
    globalAdminStepUp.onCancel()
    const error = await pending
    expect(error).toBe(observed())
    expect(error).toBeInstanceOf(StepUpCancelledError)
    expect(isStepUpCancelled(error)).toBe(true)
    expect(readApiError(error).code).toBe('STEP_UP_CANCELLED')
    expect(error).toBe(observed())
    expect(stepUp.visible.value).toBe(false)
    expect(requests).toHaveLength(1)
  })

  it('preserves the final blocked error identity and reason without opening another prompt', async () => {
    mode = 'blocked'
    const observed = observeRejectedError()
    const stepUp = useStepUp()
    const error = await stepUp.run(() => accountsAPI.create(payload)).catch((error: unknown) => error)
    expect(error).toBe(observed())
    expect(stepUp.blockedReason.value).toBe('STEP_UP_ADMIN_API_KEY_FORBIDDEN')
    expect(stepUp.visible.value).toBe(false)
    expect(globalAdminStepUp.visible.value).toBe(false)
    expect(requests).toHaveLength(1)
  })
})
