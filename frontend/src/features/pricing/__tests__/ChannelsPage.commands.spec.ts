import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ChannelsPage from '../pages/ChannelsPage.vue'
import type { Channel } from '../api/types'
import { pricingNotificationsKey, pricingPermissionsKey, pricingScopeKey } from '../ports'

const api = vi.hoisted(() => ({ list: vi.fn(), update: vi.fn(), remove: vi.fn(), showError: vi.fn(), showSuccess: vi.fn() }))
vi.mock('../api/channels', () => ({ default: api }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
function channel(id: number): Channel {
  return { id, name: 'Channel ' + id, description: '', status: 'active', billing_model_source: 'channel_mapped',
    restrict_models: false, group_ids: [], model_pricing: [], model_mapping: {}, apply_pricing_to_account_stats: false,
    account_stats_pricing_rules: [], created_at: '', updated_at: '' }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
let server: Channel[]
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  api.update.mockReset()
  api.remove.mockReset()
  server = [channel(1), channel(2)]
  api.list.mockImplementation(async () => ({ items: structuredClone(server), total: server.length }))
})
async function render() {
  const scope = ref('epoch1:admin:1'), allowed = ref(true), statusWritable = ref(true)
  const wrapper = mount(ChannelsPage, { global: { provide: {
    [pricingScopeKey as symbol]: scope,
    [pricingPermissionsKey as symbol]: { isSuperAdmin: false, can: () => allowed.value,
      canEditAdminField: () => statusWritable.value, editableAdminFields: (_schema: string, value: object) => value },
    [pricingNotificationsKey as symbol]: { showError: api.showError, showSuccess: api.showSuccess },
  }, stubs: {
    TablePageLayout: { template: '<div><slot name="filters"/><slot name="table"/><slot name="pagination"/></div>' },
    DataTable: { props: ['data'], template: '<div><div v-for="row in data" :key="row.id" :data-row="row.id"><slot name="cell-status" :row="row"/><slot name="cell-actions" :row="row"/></div></div>' },
    Toggle: { name: 'Toggle', props: ['modelValue', 'disabled'], emits: ['update:modelValue'], template: '<button data-toggle :disabled="disabled" @click="$emit(\'update:modelValue\', !modelValue)">{{ modelValue }}</button>' },
    ConfirmDialog: { props: ['show', 'message', 'confirmText'], emits: ['confirm', 'cancel'], template: '<div v-if="show" data-confirm><p>{{ message }}</p><button data-confirm-command @click="$emit(\'confirm\')">{{ confirmText }}</button><button data-cancel-command @click="$emit(\'cancel\')">Cancel</button></div>' },
    ChannelEditorDialog: true, Pagination: true, EmptyState: true, Icon: true, Select: true,
  } } })
  await flushPromises()
  const toggle = (id: number) => wrapper.get('[data-row="' + id + '"] [data-toggle]')
  const deleteButton = (id: number) => wrapper.get('[data-row="' + id + '"]').findAll('button').find(button => button.text() === 'common.delete')!
  return { wrapper, scope, allowed, statusWritable, toggle, deleteButton }
}

describe('channel commands keep actor and target ownership', () => {
  it('accepts independent row completions without cancelling another row and blocks duplicate commands', async () => {
    const first = deferred<Channel>(), second = deferred<Channel>()
    api.update.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const view = await render()
    await view.toggle(1).trigger('click')
    view.wrapper.findAllComponents({ name: 'Toggle' })[0]!.vm.$emit('update:modelValue', false)
    await view.toggle(2).trigger('click')
    expect(api.update).toHaveBeenCalledTimes(2)
    server[0]!.status = 'disabled'; first.resolve(server[0]!); await flushPromises()
    expect(view.toggle(1).text()).toBe('false')
    expect(view.toggle(2).attributes('disabled')).toBeDefined()
    server[1]!.status = 'disabled'; second.resolve(server[1]!); await flushPromises()
    expect(view.toggle(2).text()).toBe('false')
    expect(view.toggle(1).attributes('disabled')).toBeUndefined()
    expect(view.toggle(2).attributes('disabled')).toBeUndefined()
  })

  it('freezes one delete target, ignores repeat confirm/cancel and only refreshes after confirmation', async () => {
    const command = deferred<void>(); api.remove.mockReturnValue(command.promise)
    const view = await render()
    await view.deleteButton(1).trigger('click')
    await view.wrapper.get('[data-confirm-command]').trigger('click')
    await view.wrapper.get('[data-confirm-command]').trigger('click')
    await view.wrapper.get('[data-cancel-command]').trigger('click')
    expect(view.wrapper.find('[data-confirm]').exists()).toBe(true)
    expect(api.remove).toHaveBeenCalledTimes(1)
    expect(api.remove).toHaveBeenCalledWith(1)
    expect(view.deleteButton(2).attributes('disabled')).toBeDefined()
    server = [server[1]!]; command.resolve(); await flushPromises()
    expect(view.wrapper.find('[data-confirm]').exists()).toBe(false)
    expect(view.wrapper.find('[data-row="1"]').exists()).toBe(false)
    expect(view.wrapper.find('[data-row="2"]').exists()).toBe(true)
  })

  it('does not let an old actor completion release the new actor pending row', async () => {
    const old = deferred<Channel>(), current = deferred<Channel>()
    api.update.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const view = await render(); await view.toggle(1).trigger('click')
    view.scope.value = 'epoch2:admin:2'; await flushPromises()
    await view.toggle(1).trigger('click')
    const reads = api.list.mock.calls.length
    old.resolve(channel(1)); await flushPromises()
    expect(api.list).toHaveBeenCalledTimes(reads)
    expect(view.toggle(1).attributes('disabled')).toBeDefined()
    server[0]!.status = 'disabled'; current.resolve(server[0]!); await flushPromises()
    expect(view.toggle(1).text()).toBe('false')
  })

  it('requires status field access and rejects late completion across revoke/regrant', async () => {
    const command = deferred<Channel>(); api.update.mockReturnValue(command.promise)
    const view = await render()
    view.statusWritable.value = false; await flushPromises()
    view.wrapper.findAllComponents({ name: 'Toggle' })[0]!.vm.$emit('update:modelValue', false)
    expect(api.update).not.toHaveBeenCalled()
    view.statusWritable.value = true; await flushPromises(); await view.toggle(1).trigger('click')
    const reads = api.list.mock.calls.length
    view.allowed.value = false; view.allowed.value = true
    command.reject(new Error('old policy rejection')); await flushPromises()
    expect(api.list).toHaveBeenCalledTimes(reads)
    expect(api.showError).not.toHaveBeenCalled()
  })
})
