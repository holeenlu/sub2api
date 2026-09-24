import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CodexTicketDashboard from '../CodexTicketDashboard.vue'
import type { Account } from '@/types'
import Select from '@/components/common/Select.vue'

const { events, fingerprint, ownKeys, invalidation, diagnose, getById, harvest } = vi.hoisted(() => ({
  events: vi.fn(), fingerprint: vi.fn(), ownKeys: vi.fn(), invalidation: vi.fn(), diagnose: vi.fn(), getById: vi.fn(), harvest: vi.fn()
}))
vi.mock('@/api/admin/codexTickets', () => ({
  events, fingerprint, ownKeys, invalidation, diagnose, harvest, refreshFingerprint: vi.fn()
}))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getById } } }))
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  const { default: messages } = await import('@/i18n/locales/zh')
  const i18n = actual.createI18n({ legacy: false, locale: 'zh', messages: { zh: messages } })
  return { ...actual, useI18n: () => i18n.global }
})

const account = {
  id: 19, name: 'GPT Account', platform: 'openai', type: 'oauth',
  codex_turn_tickets: [{ model: 'gpt-5.6-sol', ready: true, length: 292, turn_state_present: true, cookie_present: false, harvest_enabled: true }]
} as Account
const attempt = { id: 1, model: 'gpt-5.6-sol', occurred_at: '2026-09-22T08:00:00Z', kind: 'miss', reason_code: 'fingerprint_mismatch' }
const expired = { id: 2, model: 'gpt-5.6-sol', occurred_at: '2026-09-22T09:00:00Z', kind: 'invalidation', reason_code: 'upstream_new_turn_state' }

function mountDashboard(value: Account = account) {
  return mount(CodexTicketDashboard, {
    props: { show: true, account: value },
    attachTo: document.body,
    global: {
      stubs: {
        Teleport: true, Select: true, DateRangePicker: true, Icon: true
      }
    }
  })
}

describe('Codex ticket dashboard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    diagnose.mockReset()
    fingerprint.mockResolvedValue({ commit: 'abc123', models: ['gpt-5.6-sol', 'gpt-5.5'] })
    getById.mockResolvedValue(account)
    ownKeys.mockResolvedValue([{ id: 4, name: 'Billing Key', status: 'active', quota: 0, quota_used: 0 }])
    events.mockImplementation((_id: number, params: { filter: string }) => Promise.resolve({
      items: params.filter === 'invalidation' ? [expired] : [attempt], total: 1
    }))
    invalidation.mockResolvedValue({ ...expired, original_ticket: 'old-secret', returned_ticket: 'new-secret' })
  })

  it('separates current tickets, failed attempts and invalidation history', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.text()).toContain('1 / 1')
    expect(wrapper.text()).not.toContain('fingerprint_mismatch')
    expect(events).not.toHaveBeenCalled()
    expect(ownKeys).not.toHaveBeenCalled()
    expect(wrapper.findAll('nav button').map(button => button.text())).toEqual(['当前票据', '打票流水', '票据过期历史'])

    const tab = (label: string) => wrapper.findAll('nav button').find(button => button.text() === label)!
    await tab('打票流水').trigger('click')
    await flushPromises()
    expect(events).toHaveBeenLastCalledWith(19, expect.objectContaining({ filter: 'attempts' }))
    expect(wrapper.text()).toContain('fingerprint_mismatch')
    expect(wrapper.text()).not.toContain('old-secret')

    await tab('票据过期历史').trigger('click')
    await flushPromises()
    expect(events).toHaveBeenLastCalledWith(19, expect.objectContaining({ filter: 'invalidation' }))
    expect(invalidation).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('old-secret')
    await wrapper.find('section[aria-label="票据过期历史"] button').trigger('click')
    await flushPromises()
    expect(invalidation).toHaveBeenCalledWith(19, 2)
    expect(wrapper.text()).toContain('old-secret')
    wrapper.unmount()
  })

  it('uses fresh account ticket models for cards and filters, and clears removed selections', async () => {
    const fresh = {
      ...account,
      codex_turn_tickets: [{ ...account.codex_turn_tickets![0], model: 'gpt-6-astra' }]
    }
    getById.mockResolvedValueOnce(fresh)
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.findAll('article h3').map(card => card.text())).toEqual(['gpt-6-astra'])
    await wrapper.findAll('nav button')[1].trigger('click')
    await flushPromises()
    const modelSelect = () => wrapper.findAllComponents(Select)[1]
    expect(modelSelect().props('options')).toEqual([
      { value: '', label: '全部模型' }, { value: 'gpt-6-astra', label: 'gpt-6-astra' }
    ])
    modelSelect().vm.$emit('update:modelValue', 'gpt-6-astra')
    await flushPromises()
    expect(events).toHaveBeenLastCalledWith(19, expect.objectContaining({ model: 'gpt-6-astra' }))

    await wrapper.findAll('header button').find(button => button.text() === '刷新')!.trigger('click')
    await flushPromises()
    expect(modelSelect().props('options')).toEqual([
      { value: '', label: '全部模型' }, { value: 'gpt-5.6-sol', label: 'gpt-5.6-sol' }
    ])
    expect(modelSelect().props('modelValue')).toBe('')
    expect(events).toHaveBeenLastCalledWith(19, expect.objectContaining({ model: undefined }))
    await wrapper.findAll('nav button')[0].trigger('click')
    expect(wrapper.findAll('article h3').map(card => card.text())).toEqual(['gpt-5.6-sol'])
    wrapper.unmount()
  })

})
