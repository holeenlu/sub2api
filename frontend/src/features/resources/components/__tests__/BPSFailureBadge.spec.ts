import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { Account } from '@/features/resources/api/accountTypes'
import en from '@/features/resources/locales/en'
import zh from '@/features/resources/locales/zh'
import ja from '@/features/resources/locales/ja'
import zhTW from '@/features/resources/locales/zh-TW'
import BPSFailureBadge from '../BPSFailureBadge.vue'

vi.mock('@/shared/lib/format', async () => {
  const actual = await vi.importActual<typeof import('@/shared/lib/format')>('@/shared/lib/format')
  return { ...actual, formatDateTime: (date: Date) => date.toISOString() }
})

const badge = '[data-test="bps-failure-badge"]'
const copy = en.admin.accounts.openai
const messages = { en, zh, ja, 'zh-TW': zhTW }
const disabledAt = '2026-09-26T15:04:05Z'
const movedAt = '2026-09-27T01:02:03Z'
const nextProbeAt = '2026-10-27T03:00:00Z'
type Recovery = NonNullable<Account['excel_bps_403_recovery']>
type Locale = keyof typeof messages

function makeAccount(overrides: Partial<Account> = {}): Account {
  return {
    id: 1, name: 'account', platform: 'openai', type: 'oauth',
    proxy_id: null, concurrency: 1, priority: 1, status: 'active',
    error_message: null, last_used_at: null, expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
    schedulable: true, rate_limited_at: null, rate_limit_reset_at: null,
    overload_until: null, temp_unschedulable_until: null, temp_unschedulable_reason: null,
    session_window_start: null, session_window_end: null, session_window_status: null,
    extra: { openai_excel_bps: false },
    ...overrides
  }
}

// Positive fixtures use the actual DTO, including the required interval field.
function recovery(overrides: Partial<Recovery> = {}): Recovery {
  return { state: 'pending', interval_minutes: 60, next_probe_at: nextProbeAt, ...overrides }
}

function mountAccount(account: Account, groups?: { id: number; name: string }[], locale: Locale = 'en') {
  return mount(BPSFailureBadge, {
    props: { account, groups },
    global: { plugins: [createI18n({ legacy: false, locale, fallbackLocale: false, messages })] }
  })
}

// Inject malformed network values at the boundary without weakening the API type
// or mocking the production presentation helper.
function mountProjection(projection: unknown, overrides: Partial<Account> = {}, locale: Locale = 'en') {
  const account = makeAccount(overrides)
  Object.defineProperty(account, 'excel_bps_403_recovery', { value: projection, enumerable: true, configurable: true, writable: true })
  return mountAccount(account, undefined, locale)
}

function movedAccount(target: unknown = 7, groupIDs: number[] | undefined = [7], overrides: Partial<Account> = {}): Account {
  return makeAccount({
    ...overrides,
    group_ids: groupIDs,
    extra: {
      openai_excel_bps_403_moved_at: movedAt,
      openai_excel_bps_403_moved_group_id: target,
      ...overrides.extra
    }
  })
}

const pauseReasons = [
  ['global_disabled', 'excelBPSRecoveryGlobalDisabled'],
  ['cloud_enabled', 'excelBPSRecoveryCloudEnabled'],
  ['manual_disabled', 'excelBPSRecoveryManualDisabled'],
  ['recovery_disabled', 'excelBPSRecoveryDisabled'],
  ['no_text_model', 'excelBPSRecoveryNoTextModel'],
  ['account_ineligible', 'excelBPSRecoveryAccountIneligible'],
  ['account_inactive', 'excelBPSRecoveryAccountInactive'],
  ['scheduling_paused', 'excelBPSRecoverySchedulingPaused'],
  ['account_expired', 'excelBPSRecoveryAccountExpired'],
  ['invalid_timestamp', 'excelBPSRecoveryInvalidTimestamp']
] as const

const activeStates = [
  ['reauthorization_required', 'excelBPSFailureReauth'],
  ['paused', 'excelBPSRecoveryPaused'],
  ['pending', 'excelBPSRecoveryPending'],
  ['failed', 'excelBPSRecoveryFailed'],
  ['probing', 'excelBPSRecoveryProbing']
] as const

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-27T02:00:00Z'))
})
afterEach(() => vi.useRealTimers())

describe('BPSFailureBadge safe API projection', () => {
  it.each(activeStates)('renders %s from the DTO even without extra', (state, key) => {
    const wrapper = mountProjection(recovery({ state }), { extra: undefined })
    expect(wrapper.get(badge).text()).toBe(copy[key])
    expect(wrapper.get(badge).attributes('title')).toContain(copy.excelBPSFailureNote)
    expect(wrapper.find('button, a, input').exists()).toBe(false)
    expect(wrapper.emitted()).toEqual({})
    expect(vi.getTimerCount()).toBe(0)
  })

  it('accepts a JSON null extra while the safe DTO remains present', () => {
    const account = makeAccount({ excel_bps_403_recovery: recovery() })
    Object.defineProperty(account, 'extra', { value: null, enumerable: true })
    expect(mountAccount(account).get(badge).text()).toBe(copy.excelBPSRecoveryPending)
  })

  it('never reads a private failure record and does not mutate frozen input', () => {
    const extra = { openai_excel_bps: false }
    Object.defineProperty(extra, 'openai_excel_bps_failure', {
      // Vue Test Utils enumerates props while mounting. Keep the tripwire
      // non-enumerable so only an actual private-field read can trigger it.
      enumerable: false,
      get: () => { throw new Error('Private failure record must not be read') }
    })
    const projection = Object.freeze(recovery({ state: 'failed' }))
    const account = Object.freeze(makeAccount({ extra: Object.freeze(extra), excel_bps_403_recovery: projection }))
    const wrapper = mountAccount(account)
    expect(wrapper.get(badge).text()).toBe(copy.excelBPSRecoveryFailed)
    expect(account.excel_bps_403_recovery).toBe(projection)
    expect(account.extra?.openai_excel_bps).toBe(false)
    expect(wrapper.emitted()).toEqual({})
  })

  it.each([null, undefined, {}, false, { source: 'system', state: 'disabled', reauth_required: true }])(
    'ignores private records both with and without the public projection: %j', privateRecord => {
      const extra = {
        openai_excel_bps: false,
        openai_excel_bps_failure: privateRecord,
        openai_excel_bps_403_disabled_at: disabledAt
      }
      const baseline = mountProjection(recovery()).html()
      expect(mountProjection(recovery(), { extra }).html()).toBe(baseline)
      expect(mountProjection(undefined, { extra }).find(badge).exists()).toBe(false)
      expect(mountProjection(null, { extra }).find(badge).exists()).toBe(false)
    }
  )

  it.each(['admin', 'super_admin', 'vendor', 'user', undefined])(
    'does not derive recovery or permissions from creator role %s', role => {
      const overrides = { created_by_role: role, vendor_id: 7, authorization_modes: ['bps'] as ('native' | 'bps')[] }
      expect(mountProjection(recovery(), overrides).get(badge).text()).toBe(copy.excelBPSRecoveryPending)
      expect(mountProjection(undefined, overrides).find(badge).exists()).toBe(false)
    }
  )

  it.each([
    { platform: 'openai', type: 'apikey' }, { platform: 'openai', type: 'setup-token' },
    { platform: 'openai', type: 'upstream' }, { platform: 'openai', type: 'service_account' },
    { platform: 'anthropic', type: 'oauth' }, { platform: 'grok', type: 'oauth' },
    { platform: 'future-platform', type: 'oauth' }
  ] as const)('hides recovery and group history outside OpenAI OAuth: %j', scope => {
    const account = movedAccount(7, [7], { ...scope, excel_bps_403_recovery: recovery() })
    expect(mountAccount(account).find(badge).exists()).toBe(false)
  })

  it.each([undefined, null, '', false, 0, [], {}, 'unknown', 'disabled', 'recovered', 'future_state', 'constructor', 'toString', '__proto__', '<b>secret</b>'])(
    'shows only an unconfirmed status for unknown or invalid state %j', state => {
      const wrapper = mountProjection({ ...recovery(), state, reason: 'grant_revoked', reauth_required: true }, {
        extra: { openai_excel_bps_failure: { source: 'system', state: 'disabled' }, openai_excel_bps_403_disabled_at: disabledAt }
      })
      expect(wrapper.get(badge).text()).toBe(copy.excelBPSFailureUnknown)
      expect(wrapper.get(badge).attributes('title')).toBe(copy.excelBPSFailureNote + '\n· ' + copy.excelBPSFailureUnknown)
      expect(wrapper.html()).not.toContain('secret')
    }
  )

  it.each([false, true, 0, 1, 'private-payload', [], ['pending']])('does not reinterpret malformed projection %j', projection => {
    const wrapper = mountProjection(projection, { extra: { openai_excel_bps_403_disabled_at: disabledAt } })
    expect(wrapper.get(badge).text()).toBe(copy.excelBPSFailureUnknown)
    expect(wrapper.get(badge).attributes('title')).not.toContain('2026-')
  })

  it.each([true, false, null, undefined, 'true'])('uses reauthorization state regardless of obsolete reauth_required=%j', legacy => {
    const wrapper = mountProjection({ ...recovery({ state: 'reauthorization_required' }), reason: null, reauth_required: legacy })
    expect(wrapper.get(badge).text()).toBe(copy.excelBPSFailureReauth)
    expect(wrapper.get(badge).attributes('title')).toContain(copy.excelBPSFailureReauthRequired)
    expect(wrapper.get(badge).attributes('title')).not.toContain('2026-')
    expect(wrapper.get(badge).attributes('title')).not.toContain(copy.excelBPSFailureProbeUnavailable)
  })

  it.each(['pending', 'failed', 'probing', 'paused', 'unknown', 'history'])(
    'does not infer reauthorization from grant_revoked or obsolete flags in %s', state => {
      const wrapper = mountProjection({ ...recovery({ state, reason: 'grant_revoked' }), reauth_required: true })
      expect(wrapper.text()).not.toContain(copy.excelBPSFailureReauth)
      expect(wrapper.html()).not.toContain(copy.excelBPSFailureReauthRequired)
      if (['pending', 'failed', 'probing'].includes(state)) {
        expect(wrapper.get(badge).attributes('title')).toContain('2026-10-27T03:00:00.000Z')
      }
    }
  )

  it.each([
    ['grant_missing', 'excelBPSFailureGrantMissing'],
    ['authorization_unavailable', 'excelBPSFailureAuthorizationUnavailable'],
    ['invalid_grant', 'excelBPSFailureAuthorizationInvalid'],
    ['grant_revoked', 'excelBPSFailureAuthorizationRevoked'],
    ['token_revoked', 'excelBPSFailureAuthorizationRevoked'],
    ['refresh_grant_missing', 'excelBPSFailureRefreshGrantMissing'],
    ['refreshed_access_token_rejected', 'excelBPSFailureRefreshedTokenRejected']
  ] as const)('localizes reviewed authorization reason %s without exposing the code', (reason, key) => {
    const wrapper = mountProjection(recovery({ state: 'reauthorization_required', reason }))
    expect(wrapper.get(badge).attributes('title')).toContain(copy[key])
    expect(wrapper.html()).not.toContain(reason)
  })

  it.each(['en', 'zh', 'ja', 'zh-TW'] as const)('renders all server pause reasons in %s without a probe time', locale => {
    const localCopy = messages[locale].admin.accounts.openai
    for (const [reason, key] of pauseReasons) {
      const wrapper = mountProjection(recovery({ state: 'paused', reason }), {}, locale)
      expect(wrapper.get(badge).text()).toBe(localCopy.excelBPSRecoveryPaused)
      expect(wrapper.get(badge).attributes('title')).toContain(localCopy[key])
      expect(wrapper.html()).not.toContain(reason)
      expect(wrapper.html()).not.toContain('2026-')
      expect(wrapper.html()).not.toContain('admin.accounts.openai.')
    }
  })

  it.each([null, undefined, '', 'future_reason', 'constructor', 'toString', '__proto__', 'hasOwnProperty', 'invalid_grant_extra', '<b>raw secret</b>', { internal: 'raw secret' }])(
    'does not expose an unrecognized reason or use it to derive state: %j', reason => {
      for (const state of ['reauthorization_required', 'paused']) {
        const wrapper = mountProjection({ ...recovery({ state }), reason })
        expect(wrapper.get(badge).attributes('title')).toContain(copy.excelBPSFailureReasonUnknown)
        expect(wrapper.html()).not.toContain('raw secret')
        expect(wrapper.html()).not.toContain('future_reason')
        expect(wrapper.html()).not.toContain('invalid_grant_extra')
        expect(wrapper.html()).not.toContain('2026-')
      }
    }
  )

  it.each(['constructor', 'toString', '__proto__', 'hasOwnProperty'])(
    'does not let inherited key %s become a translation parameter in a probe state', reason => {
      const wrapper = mountProjection(recovery({ reason }))
      expect(wrapper.get(badge).text()).toBe(copy.excelBPSRecoveryPending)
      expect(wrapper.get(badge).attributes('title')).not.toContain(copy.excelBPSFailureReauthRequired)
      expect(wrapper.html()).not.toContain(reason)
      expect(wrapper.html()).not.toContain('[object')
      expect(wrapper.get(badge).attributes('title')).toContain('2026-10-27T03:00:00.000Z')
    }
  )

  it('keeps state authoritative when last_result is stale, unknown, or contains extra fields', () => {
    const results = [
      { status: 'restored', stage: 'restore', reason: 'success', duration_ms: 50, completed_at: movedAt },
      { status: 'future_status', stage: 'internal-stage', reason: 'internal_reason', duration_ms: 50, completed_at: movedAt },
      { status: 'failed', stage: { private: 'internal-stage' }, reason: '<b>raw secret</b>', error: 'raw body', credential: 'secret-token' }
    ]
    for (const lastResult of results) {
      const wrapper = mountProjection({ ...recovery({ state: 'failed' }), last_result: lastResult })
      expect(wrapper.get(badge).text()).toBe(copy.excelBPSRecoveryFailed)
      expect(wrapper.get(badge).attributes('title')).toContain(copy.excelBPS403HistoricalResult)
      expect(wrapper.get(badge).attributes('title')).not.toContain(copy.excelBPS403CurrentCycleResult)
      expect(wrapper.html()).not.toMatch(/raw secret|raw body|secret-token|internal-stage|internal_reason/)
    }
  })
})

describe('BPSFailureBadge probe plans and current state', () => {
  it.each([
    { name: 'future', time: nextProbeAt, line: 'excelBPSFailureProbeScheduled' },
    { name: 'elapsed', time: '2026-10-27T01:00:00Z', line: 'excelBPSFailureProbeElapsed' },
    { name: 'due now', time: '2026-10-27T02:00:00Z', line: 'excelBPSFailureProbeElapsed' },
    { name: 'offset timestamp', time: '2026-10-27T11:00:00+08:00', line: 'excelBPSFailureProbeScheduled' },
    { name: 'nanosecond timestamp', time: '2026-10-27T03:00:00.123456789Z', line: 'excelBPSFailureProbeScheduled' }
  ] as const)('shows the absolute %s plan for pending, failed, and probing states', ({ time, line }) => {
    for (const state of ['pending', 'failed', 'probing']) {
      const wrapper = mountProjection(recovery({ state, next_probe_at: time }))
      expect(wrapper.get(badge).attributes('title')).toContain(copy[line].replace('{time}', new Date(time).toISOString()))
      expect(wrapper.get(badge).attributes('title')).not.toContain(copy.excelBPSFailureReauthRequired)
      expect(vi.getTimerCount()).toBe(0)
    }
  })

  it.each([undefined, null, '', 'not-a-time', 1234, false, {}, [], '2026-10-27', '2026-10-27T03:00:00', '2026-13-27T03:00:00Z'])(
    'does not invent a probe time from %j, private data, or interval_minutes', time => {
      const wrapper = mountProjection({ ...recovery(), next_probe_at: time, disabled_at: disabledAt, last_probe_at: movedAt }, {
        extra: { openai_excel_bps_failure: { next_probe_at: nextProbeAt }, openai_excel_bps_403_disabled_at: disabledAt }
      })
      expect(wrapper.get(badge).attributes('title')).toContain(copy.excelBPSFailureProbeUnavailable)
      expect(wrapper.get(badge).attributes('title')).not.toContain('2026-')
    }
  )

  it.each([...activeStates.map(([state]) => state), 'unknown', 'future_state'])(
    'does not claim an active shutdown for %s when BPS is actually on', state => {
      const wrapper = mountProjection(recovery({ state }), { extra: { openai_excel_bps: true, openai_excel_bps_403_disabled_at: disabledAt } })
      expect(wrapper.find(badge).exists()).toBe(false)
    }
  )

  it.each([true, false, undefined, 'true', 'false', 1, null])('does not treat history as an active shutdown with switch %j', enabled => {
    const wrapper = mountProjection(recovery({ state: 'history', reason: 'grant_revoked', disabled_at: disabledAt }), {
      extra: { openai_excel_bps: enabled, openai_excel_bps_403_disabled_at: disabledAt }
    })
    expect(wrapper.find(badge).exists()).toBe(false)
  })

  it.each([false, undefined, 'true', 'false', 1, null])('does not treat non-boolean switch %j as proof of re-enablement', enabled => {
    expect(mountProjection(recovery(), { extra: { openai_excel_bps: enabled } }).get(badge).text()).toBe(copy.excelBPSRecoveryPending)
  })

  it('keeps the server pause reason authoritative over local flags', () => {
    const wrapper = mountProjection(recovery({ state: 'paused', reason: 'global_disabled' }), {
      status: 'active', schedulable: true,
      extra: { openai_excel_bps: false, openai_excel_bps_auto_recover_on_403: true, openai_excel_bps_auto_disable_on_403: true }
    })
    expect(wrapper.get(badge).attributes('title')).toContain(copy.excelBPSRecoveryGlobalDisabled)
    expect(wrapper.get(badge).attributes('title')).not.toContain('2026-')
  })

  it('reacts to safe DTO and switch changes without retaining stale status or mutating the account', async () => {
    const account = Object.freeze(makeAccount({ excel_bps_403_recovery: Object.freeze(recovery()) }))
    const wrapper = mountAccount(account)
    expect(wrapper.get(badge).text()).toBe(copy.excelBPSRecoveryPending)
    await wrapper.setProps({ account: makeAccount({ excel_bps_403_recovery: recovery({ state: 'probing' }) }) })
    expect(wrapper.get(badge).text()).toBe(copy.excelBPSRecoveryProbing)
    await wrapper.setProps({ account: makeAccount({ excel_bps_403_recovery: recovery({ state: 'history' }) }) })
    expect(wrapper.find(badge).exists()).toBe(false)
    await wrapper.setProps({ account: makeAccount({ excel_bps_403_recovery: recovery({ state: 'reauthorization_required' }) }) })
    expect(wrapper.get(badge).text()).toBe(copy.excelBPSFailureReauth)
    await wrapper.setProps({ account: makeAccount({ excel_bps_403_recovery: recovery(), extra: { openai_excel_bps: true } }) })
    expect(wrapper.find(badge).exists()).toBe(false)
    await wrapper.setProps({ account: makeAccount({ extra: { openai_excel_bps_403_disabled_at: disabledAt } }) })
    expect(wrapper.find(badge).exists()).toBe(false)
    expect(account.excel_bps_403_recovery?.state).toBe('pending')
    expect(wrapper.emitted()).toEqual({})
    expect(vi.getTimerCount()).toBe(0)
  })
})

describe('BPSFailureBadge still-valid historical group actions', () => {
  it.each(['history', 'unknown', 'pending', 'reauthorization_required'])(
    'retains matching group history with public state %s, without old shutdown inference', state => {
      const wrapper = mountAccount(movedAccount(7, [7], {
        excel_bps_403_recovery: recovery({ state }),
        extra: { openai_excel_bps: false, openai_excel_bps_403_disabled_at: disabledAt }
      }), [{ id: 7, name: 'BPS quarantine' }])
      const title = wrapper.get(badge).attributes('title')
      expect(wrapper.findAll(badge)).toHaveLength(1)
      expect(title).toContain('2026-09-27T01:02:03.000Z moved to group “BPS quarantine”')
      expect(title).not.toContain('2026-09-26')
      if (state === 'history') {
        expect(wrapper.get(badge).text()).toBe(copy.excelBPS403Badge)
        expect(title).toContain(copy.excelBPS403BadgeNote)
        expect(title).not.toContain('BPS disabled')
      }
    }
  )

  it('keeps only group history when the actual switch is on', () => {
    const wrapper = mountAccount(movedAccount(7, [7], {
      excel_bps_403_recovery: recovery({ state: 'reauthorization_required' }), extra: { openai_excel_bps: true }
    }))
    expect(wrapper.get(badge).text()).toBe(copy.excelBPS403Badge)
    expect(wrapper.get(badge).attributes('title')).toContain('group “#7”')
    expect(wrapper.get(badge).attributes('title')).not.toContain(copy.excelBPSFailureReauth)
  })

  it('shows historical leave-all-groups only with a known empty current membership', () => {
    const wrapper = mountAccount(movedAccount(0, []))
    expect(wrapper.get(badge).text()).toBe(copy.excelBPS403Badge)
    expect(wrapper.get(badge).attributes('title')).toBe(copy.excelBPS403BadgeNote + '\n· ' + copy.excelBPS403BadgeLeftGroups.replace('{time}', '2026-09-27T01:02:03.000Z'))
  })

  it.each([
    { target: 7, groups: [3] }, { target: 7, groups: [7, 3] }, { target: 0, groups: [3] },
    { target: 7, groups: undefined }, { target: 0, groups: undefined },
    { target: -1, groups: [] }, { target: 7.5, groups: [7] }, { target: '7', groups: [7] },
    { target: null, groups: [] }, { target: undefined, groups: [7] },
    { target: Number.MAX_SAFE_INTEGER + 1, groups: [] }
  ])('does not treat stale, unknown, or invalid membership as valid history: %j', ({ target, groups }) => {
    const account = movedAccount(target, [], { extra: { openai_excel_bps_403_moved_group_id: target } })
    account.group_ids = groups
    expect(mountAccount(account).find(badge).exists()).toBe(false)
  })

  it.each([undefined, null, 1, '', 'not-a-time', '2026-09-27', '2026-09-27T01:02:03', '2026-13-27T01:02:03Z', '2026-02-29T01:02:03Z', '2026-02-30T01:02:03Z', '2026-10-27T24:00:00Z'])(
    'does not display group history with invalid timestamp %j', time => {
      expect(mountAccount(movedAccount(7, [7], { extra: { openai_excel_bps_403_moved_at: time } })).find(badge).exists()).toBe(false)
    }
  )

  it('preserves valid historical offsets and fractional seconds across a UTC day boundary', () => {
    const wrapper = mountAccount(movedAccount(7, [7], { extra: { openai_excel_bps_403_moved_at: '2026-09-27T01:02:03.123456789+08:00' } }))
    expect(wrapper.get(badge).attributes('title')).toContain('2026-09-26T17:02:03.123Z moved to group “#7”')
  })

  it('removes obsolete history after current membership changes', async () => {
    const wrapper = mountAccount(movedAccount(7, [7]))
    expect(wrapper.find(badge).exists()).toBe(true)
    await wrapper.setProps({ account: movedAccount(7, [8]) })
    expect(wrapper.find(badge).exists()).toBe(false)
  })
})


const lastProbe: NonNullable<Recovery['last_result']> = {
  status: 'failed', stage: 'request', reason: 'http_rejected', http_status: 403, duration_ms: 42,
  disabled_at: disabledAt, attempted_at: movedAt, completed_at: '2026-09-27T01:02:04Z',
  upstream_request_id: 'req-safe-123', error_code: 'account_deactivated', error_type: 'permission_error',
}

describe('BPSFailureBadge probe history', () => {
  it.each([true, false, undefined])('distinguishes the server cycle flag %j without inferring current failure', currentCycle => {
    const wrapper = mountProjection(recovery({ state: 'pending', last_result_current_cycle: currentCycle, last_result: lastProbe }))
    expect(wrapper.get(badge).text()).toBe(copy.excelBPSRecoveryPending)
    const title = wrapper.get(badge).attributes('title')
    expect(title).toContain(currentCycle ? copy.excelBPS403CurrentCycleResult : copy.excelBPS403HistoricalResult)
    expect(title).not.toContain(currentCycle ? copy.excelBPS403HistoricalResult : copy.excelBPS403CurrentCycleResult)
    expect(title).toContain('2026-09-27T01:02:03.000Z')
    expect(title).toContain('2026-09-27T01:02:04.000Z')
    expect(title).toContain('HTTP 403, duration 42 ms')
    expect(title).toContain('account_deactivated')
    expect(title).toContain('permission_error')
    expect(title).toContain('req-safe-123')
    expect(title).toContain(copy.excelBPS403Result_http_rejected)
  })

  it.each([
    { state: 'history', enabled: false },
    { state: 'history', enabled: true },
    { state: 'failed', enabled: true },
  ])('keeps prior probe history visible without an active failure for $state / enabled=$enabled', ({ state, enabled }) => {
    const wrapper = mountProjection(recovery({ state, last_result_current_cycle: true, last_result: lastProbe }), {
      extra: { openai_excel_bps: enabled },
    })
    expect(wrapper.get(badge).text()).toBe(copy.excelBPS403State_history)
    expect(wrapper.get(badge).classes()).toContain('bg-gray-100')
    expect(wrapper.get(badge).classes()).not.toContain('bg-amber-400')
    expect(wrapper.get(badge).attributes('title')).toContain(copy.excelBPS403HistoricalResult)
    expect(wrapper.get(badge).attributes('title')).not.toContain(copy.excelBPS403CurrentCycleResult)
    expect(wrapper.get(badge).attributes('title')).not.toContain(copy.excelBPSRecoveryFailed)
  })

  it('reacts to a cycle-flag-only update without altering the server state', async () => {
    const account = makeAccount({ excel_bps_403_recovery: recovery({ last_result_current_cycle: false, last_result: lastProbe }) })
    const wrapper = mountAccount(account)
    expect(wrapper.get(badge).attributes('title')).toContain(copy.excelBPS403HistoricalResult)
    await wrapper.setProps({ account: { ...account, excel_bps_403_recovery: recovery({ last_result_current_cycle: true, last_result: lastProbe }) } })
    expect(wrapper.get(badge).text()).toBe(copy.excelBPSRecoveryPending)
    expect(wrapper.get(badge).attributes('title')).toContain(copy.excelBPS403CurrentCycleResult)
    expect(wrapper.get(badge).attributes('title')).not.toContain(copy.excelBPS403HistoricalResult)
  })

  it.each(Object.keys(messages) as Locale[])('translates probe diagnostics in %s', locale => {
    const wrapper = mountProjection(recovery({ last_result_current_cycle: false, last_result: lastProbe }), {}, locale)
    const title = wrapper.get(badge).attributes('title')
    expect(title).toContain(messages[locale].admin.accounts.openai.excelBPS403HistoricalResult)
    expect(title).not.toContain('admin.accounts.openai.')
  })
})
