import { accountFixture, commandFixture, translate } from './boundaryFixtures'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createResourceTaskScope } from '../../queries/resourceTaskScope'
import { useEditSubmissionDraft, type EditSubmissionInput } from '../editSubmission'
import { useCreateSubmissionDraft } from '../createSubmission'

const api = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn() }))
vi.mock('@/features/resources/accounts', () => ({ default: api }))

type Kind = 'create' | 'edit'
function setup(kind: Kind) {
  let actor = 'admin-session-a'
  let target = 7
  const resourceTasks = createResourceTaskScope(() => actor + ':' + target)
  const confirmed = ref(false)
  const dialogs: { onConfirm(): Promise<void> }[] = []
  const updated = accountFixture({ name: 'original account' })
  const persist = vi.fn(async () => updated)
  const notices = { showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }
  const emit = vi.fn()
  // Exercise command boundaries only. Payload-builder inputs are deliberately absent.
  const input = {
    resourceTasks,
    props: { show: true, account: updated, allowRateEdit: true, showCost: true, allowTransportEdit: true, allowAuthorize: true, saveAccount: undefined as EditSubmissionInput['props']['saveAccount'] },
    form: { name: 'original account', notes: '', platform: 'antigravity' as const, type: 'oauth' as const, credentials: {}, proxy_id: null, concurrency: 1, load_factor: null, priority: 50, rate_multiplier: 1, group_ids: [3], expires_at: null, status: 'active' as const },
    upstreamModelsPreviewed: ref(false),
    antigravityMixedChannelConfirmed: confirmed,
    clearMixedChannelDialog: vi.fn(),
    emit,
    withAntigravityConfirmFlag: <T extends object>(payload: T): T => confirmed.value ? { ...payload, confirm_mixed_channel_risk: true } : payload,
    persistGrokMediaEligibility: persist,
    appStore: notices,
    t: translate,
    needsMixedChannelCheck: () => true,
    openMixedChannelDialog: (dialog: { onConfirm(): Promise<void> }) => dialogs.push(dialog),
  }
  const payload = { name: 'original account', platform: 'antigravity' as const, type: 'oauth' as const, credentials: { refresh_token: 'fixture' }, group_ids: [3] }
  const send = kind === 'create' ? api.create : api.update
  send.mockResolvedValue(updated)
  const submit = kind === 'create'
    ? () => useCreateSubmissionDraft(commandFixture<Parameters<typeof useCreateSubmissionDraft>[0]>(input)).submitCreateAccount(payload)
    : () => useEditSubmissionDraft(commandFixture<EditSubmissionInput>(input)).submitUpdateAccount(7, payload)
  const warning = { status: 409, error: 'mixed_channel_warning', message: 'confirm risk' }
  return { input, submit, send, warning, dialogs, notices, emit, persist, confirmed, payload,
    changeActor: () => { actor = 'supplier-session-b' },
    changeTarget: () => { target = 8 },
    close: () => resourceTasks.invalidate(),
    dispose: () => resourceTasks.dispose(),
  }
}

beforeEach(() => { vi.resetAllMocks() })

describe.each<Kind>(['create', 'edit'])('%s command scope boundary', kind => {
  it.each(['changeActor', 'changeTarget', 'close', 'dispose'] as const)('rejects a delayed warning confirmation after %s', async transition => {
    const h = setup(kind)
    h.send.mockRejectedValueOnce(h.warning)
    await h.submit()
    expect(h.dialogs).toHaveLength(1)
    h[transition]()
    await h.dialogs[0].onConfirm()
    expect(h.send).toHaveBeenCalledTimes(1)
    expect(h.confirmed.value).toBe(false)
    expect(h.notices.showSuccess).not.toHaveBeenCalled()
    expect(h.emit).not.toHaveBeenCalled()
  })

  it('keeps an explicitly confirmed rejection retry on the original target and payload', async () => {
    const h = setup(kind)
    h.send.mockRejectedValueOnce(h.warning)
    await h.submit()
    await h.dialogs[0].onConfirm()
    expect(h.send).toHaveBeenCalledTimes(2)
    const body = { ...h.payload, confirm_mixed_channel_risk: true }
    expect(h.send).toHaveBeenLastCalledWith(...(kind === 'create' ? [body] : [7, body]))
    expect(h.notices.showSuccess).toHaveBeenCalledTimes(1)
  })

  it('does not replay a command when the server result is unknown', async () => {
    const h = setup(kind)
    h.send.mockRejectedValueOnce(new Error('connection lost after dispatch'))
    await h.submit()
    expect(h.send).toHaveBeenCalledTimes(1)
    expect(h.dialogs).toHaveLength(0)
    expect(h.notices.showSuccess).not.toHaveBeenCalled()
  })
})

it('does not reopen account-update confirmation after the account save has already succeeded', async () => {
  const h = setup('edit')
  h.persist.mockRejectedValueOnce(h.warning)
  await h.submit()
  expect(h.send).toHaveBeenCalledTimes(1)
  expect(h.persist).toHaveBeenCalledTimes(1)
  expect(h.dialogs).toHaveLength(0)
  expect(h.notices.showSuccess).not.toHaveBeenCalled()
})


it.each(['writer', 'permission'] as const)('rejects edit confirmation after the %s port changes', async transition => {
  const h = setup('edit')
  h.send.mockRejectedValueOnce(h.warning)
  await h.submit()
  const replacement = vi.fn().mockResolvedValue({ id: 7 })
  if (transition === 'writer') h.input.props.saveAccount = replacement
  else h.input.props.allowAuthorize = false
  await h.dialogs[0].onConfirm()
  expect(h.send).toHaveBeenCalledTimes(1)
  expect(replacement).not.toHaveBeenCalled()
})

it('does not bind a previously built payload to a replacement supplier save port after preflight', async () => {
  const h = setup('edit')
  let finish!: (allowed: boolean) => void
  const ready = new Promise<boolean>(resolve => { finish = resolve })
  const replacement = vi.fn().mockResolvedValue({ id: 7 })
  const input = { ...h.input,
    props: { ...h.input.props, allowTransportEdit: false, account: accountFixture({ type: 'apikey', rate_multiplier: 1 }) },
    editApiKey: ref(''),
    form: { ...h.input.form, name: 'original', load_factor: 0 },
    ensureAntigravityMixedChannelConfirmed: () => ready,
  }
  const draft = useEditSubmissionDraft(commandFixture<EditSubmissionInput>(input))
  const waiting = draft.handleSubmit()
  input.props.saveAccount = replacement
  finish(true)
  await waiting
  expect(replacement).not.toHaveBeenCalled()
  expect(h.send).not.toHaveBeenCalled()
})
