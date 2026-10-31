import { createAuthSourceDefaultsServices } from '../authSourceDefaults'
import { GroupBadge, GroupOptionItem } from '@/features/access-policy/presentation'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { readonly } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import Appearance from '@/features/site/settings/AppearanceSettingsPanel.vue'
import FastPolicy from '@/features/access-policy/settings/FastPolicySettingsPanel.vue'
import AuthDefaults from '@/features/identity/settings/AuthDefaultsSettingsPanel.vue'
import { projectAppearance } from '@/features/site/settings/appearanceModel'
import { projectFastPolicy } from '@/features/access-policy/settings/fastPolicyModel'
import { projectAuthDefaults } from '@/features/identity/settings/authDefaultsModel'
import { identitySettingsServicesKey } from '@/features/identity/settings/ports'
import { settingsFixture } from './fixture'
enableAutoUnmount(afterEach)
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const stubs = { Icon: true, ImageUpload: true, Select: true, Toggle: true, GroupBadge: true, GroupOptionItem: true, OpenAIFastPolicyUserSelector: true }
describe('domain settings drafts', () => {
  it('never mutates readonly input and emits only the edited site field', async () => {
    const value = readonly(projectAppearance(settingsFixture))
    const wrapper = mount(Appearance, { props: { modelValue: value }, global: { stubs } })
    expect(wrapper.emitted('patch')).toBeUndefined()
    await wrapper.get('input[type="text"]').setValue('New site')
    expect(value.site_name).toBe('Sub2API')
    expect(wrapper.emitted('patch')?.at(-1)).toEqual([{ site_name: 'New site' }])
  })
  it('accepts a new baseline without emitting phantom dirty fields', async () => {
    const wrapper = mount(Appearance, { props: { modelValue: projectAppearance(settingsFixture) }, global: { stubs } })
    await wrapper.setProps({ modelValue: projectAppearance({ ...settingsFixture, site_name: 'Server changed' }) })
    expect(wrapper.emitted('patch')).toBeUndefined()
    expect((wrapper.get('input[type="text"]').element as HTMLInputElement).value).toBe('Server changed')
  })
  it('does not write a missing Fast policy when mounted and edits without watch loops', async () => {
    const value = readonly(projectFastPolicy({ ...settingsFixture, openai_fast_policy_settings: undefined }))
    const wrapper = mount(FastPolicy, { props: { modelValue: value }, global: { stubs } })
    await flushPromises()
    expect(wrapper.emitted('patch')).toBeUndefined()
    const add = wrapper.findAll('button').find(button => button.text().includes('addRule'))
    if (!add) throw new Error('Add policy action is missing')
    await add.trigger('click'); await flushPromises()
    const changes = wrapper.emitted('patch') || []
    expect(changes.length).toBeGreaterThan(0)
    expect(changes.length).toBeLessThan(5)
    expect(value.openai_fast_policy_settings).toBeUndefined()
    expect(Object.keys(changes[changes.length - 1][0] as object)).toEqual(['openai_fast_policy_settings'])
  })
  it('edits one auth-source field without posting defaults for every auth source', async () => {
    const value = readonly(projectAuthDefaults({ ...settingsFixture, auth_source_default_email_balance: 0, auth_source_default_email_grant_on_signup: true }))
    const wrapper = mount(AuthDefaults, { props: { modelValue: value }, global: { stubs, provide: { [identitySettingsServicesKey as symbol]: { authDefaults: createAuthSourceDefaultsServices(() => ['openai']), components: { GroupBadge, GroupOptionItem }, groups: { getAll: vi.fn().mockResolvedValue([]) }, platformIds: () => ['openai'] } } } })
    await flushPromises()
    expect(wrapper.emitted('patch')).toBeUndefined()
    await wrapper.get('input[type="number"]').setValue(9)
    await flushPromises()
    const changes = wrapper.emitted('patch') || []
    expect(changes.at(-1)).toEqual([{ auth_source_default_email_balance: 9 }])
    expect(value.auth_source_default_email_balance).toBe(0)
  })
})
