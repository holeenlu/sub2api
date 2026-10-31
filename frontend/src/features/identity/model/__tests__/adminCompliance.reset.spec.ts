import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import type { AdminComplianceStatus } from '@/features/identity/api/compliance'
import { useAdminComplianceStore } from '../adminCompliance'
import { useAuthStore } from '../auth'
import type { User } from '../../api/profileTypes'

const api = vi.hoisted(() => ({ getStatus: vi.fn(), accept: vi.fn() }))
vi.mock('@/features/identity/api/compliance', () => ({ default: api }))
const authRequests = vi.hoisted(() => ({ getCurrentUser: vi.fn() }))
vi.mock('@/features/identity/api/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/identity/api/auth')>()
  return { ...actual, authAPI: { ...actual.authAPI, getCurrentUser: authRequests.getCurrentUser } }
})
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))

function status(required: boolean, version = 'current'): AdminComplianceStatus {
  return {
    required, version, document_path_zh: '', document_path_en: '',
    document_url_zh: '', document_url_en: '', ack_phrase_zh: '测试', ack_phrase_en: 'test'
  }
}

function deferred() {
  let resolve!: (value: AdminComplianceStatus) => void
  let reject!: (error: Error) => void
  const promise = new Promise<AdminComplianceStatus>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

let pinia: ReturnType<typeof createPinia>
beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
  localStorage.clear()
  vi.resetAllMocks()
  authRequests.getCurrentUser.mockResolvedValue({ data: { id: 7, role: 'admin' } })
})
afterEach(() => disposePinia(pinia))

describe.each(['fetch', 'accept'] as const)('compliance %s across logout', (action) => {
  const mock = () => action === 'fetch' ? api.getStatus : api.accept
  const run = (store: ReturnType<typeof useAdminComplianceStore>) =>
    action === 'fetch' ? store.fetchStatus() : store.accept('test')
  const busy = (store: ReturnType<typeof useAdminComplianceStore>) =>
    action === 'fetch' ? store.loading : store.submitting

  it('does not repopulate cleared state when an old request succeeds', async () => {
    const pending = deferred()
    mock().mockReturnValueOnce(pending.promise)
    const store = useAdminComplianceStore()
    const request = run(store)
    store.reset()
    const response = status(true, 'old')
    pending.resolve(response)
    expect(await request).toEqual(response)
    expect(store.status).toBeNull()
    expect(store.initialized).toBe(false)
    expect(store.shouldShow).toBe(false)
    expect(busy(store)).toBe(false)
  })

  it.each(['resolve', 'reject'] as const)('keeps a new session pending when the old request %s', async (outcome) => {
    const old = deferred()
    const current = deferred()
    mock().mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const store = useAdminComplianceStore()
    const first = run(store).catch(error => error)
    store.reset()
    const second = run(store)
    if (outcome === 'resolve') old.resolve(status(false, 'old'))
    else old.reject(new Error('old failure'))
    await first
    expect(busy(store)).toBe(true)
    expect(store.status).toBeNull()
    current.resolve(status(true))
    await second
    expect(busy(store)).toBe(false)
    expect(store.status).toEqual(status(true))
    expect(store.shouldShow).toBe(true)
  })
})

it('still updates compliance status during a normal fetch and acceptance', async () => {
  api.getStatus.mockResolvedValue(status(true))
  api.accept.mockResolvedValue(status(false))
  const store = useAdminComplianceStore()
  await store.fetchStatus()
  expect(store.initialized).toBe(true)
  expect(store.shouldShow).toBe(true)
  await store.accept('test')
  expect(api.accept).toHaveBeenCalledWith({ phrase: 'test', language: 'en' })
  expect(store.status).toEqual(status(false))
  expect(store.shouldShow).toBe(false)
})


describe('compliance document and phrase ownership', () => {
  it('keeps acknowledgement required without inventing an external document URL', () => {
    const store = useAdminComplianceStore()
    store.requireAcknowledgement()
    expect(store.shouldShow).toBe(true)
    expect(store.required).toBe(true)
    expect(store.status?.document_url_zh).toBe('')
    expect(store.status?.document_url_en).toBe('')
    expect(store.expectedPhrase).toContain('sub2api')
  })

  it('preserves backend document links and exact acknowledgement phrases', async () => {
    const serverStatus = {
      ...status(true),
      document_url_en: 'https://operator.example.org/legal',
      ack_phrase_en: 'I agree to the operator deployment terms'
    }
    api.getStatus.mockResolvedValue(serverStatus)
    api.accept.mockResolvedValue(status(false))
    const store = useAdminComplianceStore()
    await store.fetchStatus()
    expect(store.status?.document_url_en).toBe(serverStatus.document_url_en)
    expect(store.expectedPhrase).toBe(serverStatus.ack_phrase_en)
    await store.accept(serverStatus.ack_phrase_en)
    expect(api.accept).toHaveBeenCalledWith({ phrase: serverStatus.ack_phrase_en, language: 'en' })
  })

  it('honors an explicitly cleared document URL without dismissing the gate', () => {
    const store = useAdminComplianceStore()
    store.requireAcknowledgement({ document_url_en: 'https://operator.example.org/legal' })
    store.requireAcknowledgement({ document_url_en: '' })
    expect(store.status?.document_url_en).toBe('')
    expect(store.shouldShow).toBe(true)
  })
})

function token(sid: string, exp: number) {
  return 'header.' + btoa(JSON.stringify({ user_id: 7, role: 'admin', sid, exp })) + '.signature'
}

describe('compliance identity ownership', () => {
  it('clears confirmation state for a new session of the same administrator', async () => {
    const auth = useAuthStore()
    auth.user = { id: 7, role: 'admin' } as User
    await auth.setToken(token('first-session', 100))
    const store = useAdminComplianceStore()
    store.requireAcknowledgement(status(true))
    const pending = deferred()
    api.getStatus.mockReturnValueOnce(pending.promise)
    const request = store.fetchStatus()
    const signal = api.getStatus.mock.calls[0][0].signal as AbortSignal
    await auth.setToken(token('second-session', 200))
    expect(signal.aborted).toBe(true)
    expect(store.status).toBeNull()
    pending.resolve(status(false, 'previous-session'))
    await request
    expect(store.status).toBeNull()
    expect(api.accept).not.toHaveBeenCalled()
  })

  it('keeps the current confirmation during same-session token rotation', async () => {
    const auth = useAuthStore()
    auth.user = { id: 7, role: 'admin' } as User
    await auth.setToken(token('same-session', 100))
    const store = useAdminComplianceStore()
    store.requireAcknowledgement(status(true))
    localStorage.setItem('auth_token', token('same-session', 200))
    auth.checkAuth()
    await Promise.resolve()
    expect(store.required).toBe(true)
    expect(store.status?.version).toBe('current')
    expect(api.accept).not.toHaveBeenCalled()
  })

  it('does not re-open an accepted commitment when an earlier status read arrives', async () => {
    const pending = deferred()
    api.getStatus.mockReturnValueOnce(pending.promise)
    api.accept.mockResolvedValueOnce(status(false))
    const store = useAdminComplianceStore()
    const read = store.fetchStatus()
    await store.accept('test')
    pending.resolve(status(true))
    await read
    expect(store.required).toBe(false)
    expect(store.submitting).toBe(false)
    expect(store.loading).toBe(false)
  })

  it('does not apply a status response after store disposal', async () => {
    const pending = deferred()
    api.getStatus.mockReturnValueOnce(pending.promise)
    const store = useAdminComplianceStore()
    const request = store.fetchStatus()
    store.$dispose()
    pending.resolve(status(true))
    await request
    expect(store.status).toBeNull()
    expect(store.shouldShow).toBe(false)
  })
})


describe('newer compliance requirements', () => {
  it('does not dismiss a new server gate when an older status query returns', async () => {
    const pending = deferred()
    api.getStatus.mockReturnValueOnce(pending.promise)
    const store = useAdminComplianceStore()
    const read = store.fetchStatus()
    store.requireAcknowledgement(status(true, 'new-version'))
    pending.resolve(status(false, 'old-version'))
    await read
    expect(store.required).toBe(true)
    expect(store.status?.version).toBe('new-version')
    expect(store.loading).toBe(false)
    expect(api.accept).not.toHaveBeenCalled()
  })

  it('does not apply manual acceptance of an earlier document to a newer version', async () => {
    const pending = deferred()
    api.accept.mockReturnValueOnce(pending.promise)
    const store = useAdminComplianceStore()
    store.requireAcknowledgement(status(true, 'old-version'))
    const acceptance = store.accept('test')
    store.requireAcknowledgement(status(true, 'new-version'))
    pending.resolve(status(false, 'old-version'))
    await acceptance
    expect(store.required).toBe(true)
    expect(store.status?.version).toBe('new-version')
    expect(store.submitting).toBe(false)
    expect(api.accept).toHaveBeenCalledOnce()
  })
})
