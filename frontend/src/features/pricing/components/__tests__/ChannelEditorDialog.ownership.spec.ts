import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import ChannelEditorDialog from '../ChannelEditorDialog.vue'
import type { Channel } from '../../api/types'
import { pricingScopeKey, pricingNotificationsKey, pricingPermissionsKey, pricingDirectoryKey, pricingCatalogKey, pricingWebSearchKey } from '../../ports'

const api = vi.hoisted(() => ({ list: vi.fn(), update: vi.fn(), create: vi.fn(), showError: vi.fn(), showSuccess: vi.fn() }))
vi.mock('../../api/channels', () => ({ default: api }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
function channel(id: number): Channel {
  return { id, name: 'Original ' + id, description: '', status: 'active', billing_model_source: 'channel_mapped',
    restrict_models: false, group_ids: [], model_pricing: [], model_mapping: {}, apply_pricing_to_account_stats: false,
    account_stats_pricing_rules: [], created_at: '', updated_at: '' }
}
enableAutoUnmount(afterEach)
beforeEach(() => { vi.clearAllMocks(); api.list.mockResolvedValue({ items: [], total: 0 }) })
async function render(target: Channel | null = channel(1)) {
  const scope = ref('epoch1:admin'), allowed = ref(true)
  const wrapper = shallowMount(ChannelEditorDialog, { props: { channel: target }, global: { provide: {
    [pricingScopeKey as symbol]: scope,
    [pricingPermissionsKey as symbol]: { isSuperAdmin: false, can: () => allowed.value, canEditAdminField: () => true, editableAdminFields: (_schema: string, value: object) => value },
    [pricingNotificationsKey as symbol]: { showSuccess: api.showSuccess, showError: api.showError },
    [pricingDirectoryKey as symbol]: { listGroups: vi.fn().mockResolvedValue([]) },
    [pricingCatalogKey as symbol]: { listPlatformIds: () => [], supportsPricingModelSync: () => false, platformLabel: (value: string) => value },
    [pricingWebSearchKey as symbol]: vi.fn().mockResolvedValue(false),
  }, stubs: { BaseDialog: { name: 'BaseDialog', props: ['show', 'showCloseButton', 'closeOnEscape'], template: '<section><slot/><slot name="footer"/></section>' } } } })
  await flushPromises()
  return { wrapper, scope, allowed }
}

describe('channel editor owns one target and pending save', () => {
  it('does not submit an old draft to a replaced target before the parent unmounts it', async () => {
    const { wrapper } = await render()
    await wrapper.get('input[placeholder="admin.channels.form.namePlaceholder"]').setValue('Old draft')
    await wrapper.setProps({ channel: channel(2) })
    await wrapper.get('form').trigger('submit')
    expect(api.update).not.toHaveBeenCalled(); expect(api.create).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('keeps the pending draft owned and dispatches only once while close is requested', async () => {
    let resolve!: (value: Channel) => void
    api.update.mockReturnValue(new Promise<Channel>(yes => { resolve = yes }))
    const { wrapper } = await render()
    await wrapper.get('form').trigger('submit')
    wrapper.getComponent({ name: 'BaseDialog' }).vm.$emit('close')
    await wrapper.get('form').trigger('submit')
    expect(api.update).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.getComponent({ name: 'BaseDialog' }).props('closeOnEscape')).toBe(false)
    resolve(channel(1)); await flushPromises()
    expect(wrapper.emitted('saved')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it.each(['scope', 'permission', 'target'] as const)('discards an in-flight save after %s invalidation', async change => {
    let resolve!: (value: Channel) => void
    api.update.mockReturnValue(new Promise<Channel>(yes => { resolve = yes }))
    const { wrapper, scope, allowed } = await render()
    await wrapper.get('form').trigger('submit')
    if (change === 'scope') scope.value = 'epoch2:admin'
    else if (change === 'permission') { allowed.value = false; allowed.value = true }
    else await wrapper.setProps({ channel: channel(2) })
    resolve(channel(1)); await flushPromises()
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(api.showSuccess).not.toHaveBeenCalled(); expect(api.showError).not.toHaveBeenCalled()
    await wrapper.get('form').trigger('submit'); expect(api.update).toHaveBeenCalledTimes(1)
  })
})
