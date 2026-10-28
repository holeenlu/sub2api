import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import GroupSubscriptionPolicyPanel from '../GroupSubscriptionPolicyPanel.vue'
import { buildGroupSubscriptionPayload, createGroupSubscriptionDraft } from '../../groupEditor'

vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
describe('Group subscription type and quota ownership', () => {
  it('edits only its own quota and retains the existing immutable subscription-type control', async () => {
    const source = createGroupSubscriptionDraft(); source.subscription_type = 'subscription'
    const wrapper = mount(GroupSubscriptionPolicyPanel, { props: { modelValue: source, editable: () => true }, global: { stubs: { Select: { name: 'Select', props: ['disabled', 'modelValue', 'options'], template: '<select :disabled="disabled" />' } } } })
    expect(wrapper.get('select').attributes('disabled')).toBeDefined()
    await wrapper.findAll('input')[0]!.setValue('0')
    expect(wrapper.emitted('patch')).toEqual([[{ daily_limit_usd: 0 }]])
    expect(source.daily_limit_usd).toBeNull()
  })
  it('preserves unlimited quota normalization and never serializes pricing or admission fields', () => {
    const draft = Object.assign(createGroupSubscriptionDraft(), { daily_limit_usd: 0, weekly_limit_usd: -1, monthly_limit_usd: 25,
      is_exclusive: true, rate_multiplier: 4, peak_rate_enabled: true })
    expect(buildGroupSubscriptionPayload(draft)).toEqual({ subscription_type: 'standard', daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: 25 })
    expect(draft.daily_limit_usd).toBe(0)
  })
})
