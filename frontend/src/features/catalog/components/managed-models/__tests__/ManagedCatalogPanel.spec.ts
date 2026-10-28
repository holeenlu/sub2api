import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import ManagedCatalogPanel from '../ManagedCatalogPanel.vue'
import ModelMappingsEditor from '../ModelMappingsEditor.vue'
import type { MappingDraftState } from '../draft'
import type { ManagedModel, ModelMappings, ModelPolicy } from '../../../api/managedModelTypes'
import en from '../locales/en'
import zh from '../locales/zh'
import zhTW from '../locales/zh-TW'
import ja from '../locales/ja'

enableAutoUnmount(afterEach)

// Synthetic, contract-shaped policy/mappings input; no network or live upstream
// evidence. Use actual panel, child editors, BaseDialog and local i18n messages.
function model(id: string, overrides: Partial<ManagedModel> = {}): ManagedModel {
  return {
    id, display_name: id, kind: 'chat', enabled: true, sort: 1, aliases: ['friendly-model'],
    capabilities: { context_window: 200000, supports_effort: true, retained_fact: 'keep' },
    params: { max_tokens_default: 8192, max_tokens_cap: 64000, thinking_fallback_budget: 4096, on_adaptive: 'passthrough', on_enabled: 'strip', default_effort: 'high' },
    betas: { required: ['required-beta'], drop: ['dropped-beta'], allow_client: true },
    ...overrides,
  }
}
function policy(platform: ModelPolicy['platform'] = 'anthropic'): ModelPolicy {
  const chat = platform === 'anthropic' ? model('claude-sonnet-4-6', { display_name: 'Claude Sonnet' }) : model('gpt-6-luna', {
    display_name: 'GPT Luna',
    catalog: { context_window: 128000, max_context_window: 256000, max_output_tokens: 32000, supported_reasoning_levels: ['low', 'high'], default_reasoning_level: 'high', input_modalities: ['text', 'image'], codex_tool_capabilities: { shell: true } },
  })
  return {
    version: 7, platform, source: 'worker', updated_at: '2026-10-26T12:00:00Z', catalog_mode: 'policy_only',
    defaults: { enabled: false, max_tokens: 8192, thinking_fallback_budget: 2048, strip_context_1m: true, context_1m_whitelist: ['claude-sonnet-4-6'], normalize_thinking: true },
    aliases: { legacy: chat.id },
    models: {
      [chat.id]: chat,
      'gpt-image-1': model('gpt-image-1', { display_name: 'Image One', kind: 'image', enabled: false, sort: 2, aliases: [] }),
      ...(platform === 'openai' ? { 'gpt-disabled': model('gpt-disabled', { enabled: false, aliases: [] }) } : {}),
    },
  }
}
function mappings(): ModelMappings {
  return { version: 11, mappings: { 'codex-auto-review': 'unavailable-old-model', fast: 'gpt-6-luna' } }
}
function freeze<T>(value: T): T {
  if (value && typeof value === 'object') {
    Object.freeze(value)
    Object.values(value).forEach(freeze)
  }
  return value
}
function setup(overrides: Partial<{ policy: ModelPolicy; mappings: ModelMappings | null; canManage: boolean; canSync: boolean; busy: boolean; mappingTargets: readonly string[] }> = {}, locale = 'en') {
  const i18n = createI18n({ legacy: false, locale, fallbackLocale: 'en', messages: { en: { ui: { closeModal: 'Close dialog' } } }, missingWarn: false, fallbackWarn: false })
  const inputPolicy = overrides.policy ?? freeze(policy())
  const wrapper = mount(ManagedCatalogPanel, {
    attachTo: document.body,
    props: {
      policy: inputPolicy, mappings: null, canManage: true, canSync: true, busy: false,
      mappingTargets: Object.values(inputPolicy.models).filter(model => model.enabled && model.kind === 'chat').map(model => model.id),
      ...overrides,
    },
    global: { plugins: [i18n], stubs: { teleport: true } },
  })
  return { wrapper, i18n }
}
function lastPolicy(wrapper: VueWrapper): ModelPolicy {
  return wrapper.emitted('update:policy')!.at(-1)![0] as ModelPolicy
}
function lastMappings(wrapper: VueWrapper): ModelMappings {
  return wrapper.emitted('update:mappings')!.at(-1)![0] as ModelMappings
}
function lastDraftState(wrapper: VueWrapper): MappingDraftState {
  return wrapper.emitted('mapping-draft-state')!.at(-1)![0] as MappingDraftState
}
const get = (wrapper: VueWrapper, id: string) => wrapper.get(`[data-testid="${id}"]`)
const chatCard = (wrapper: VueWrapper, id = 'claude-sonnet-4-6') => wrapper.get(`[data-model-id="${id}"]`)

describe('ManagedCatalogPanel model drafts', () => {
  it('toggles chat/image models and emits independent clones without changing props or previous events', async () => {
    const input = freeze(policy())
    const before = JSON.stringify(input)
    const { wrapper } = setup({ policy: input })
    await chatCard(wrapper).get('input').setValue(false)
    const first = lastPolicy(wrapper)
    expect(first.models['claude-sonnet-4-6'].enabled).toBe(false)
    expect(first.version).toBe(7)
    expect(first.aliases).toEqual(input.aliases)
    await chatCard(wrapper, 'gpt-image-1').get('input').setValue(true)
    expect(lastPolicy(wrapper).models['gpt-image-1'].enabled).toBe(true)
    expect(lastPolicy(wrapper).models['claude-sonnet-4-6'].enabled).toBe(false)
    expect(first.models['gpt-image-1'].enabled).toBe(false)
    expect(JSON.stringify(input)).toBe(before)
    lastPolicy(wrapper).defaults.max_tokens = 1
    expect((get(wrapper, 'default-max_tokens').element as HTMLInputElement).value).toBe('8192')
  })

  it('searches case-insensitively by name, ID and alias and shows an empty result', async () => {
    const { wrapper } = setup()
    await get(wrapper, 'model-search').setValue('  SONNET  ')
    expect(wrapper.findAll('[data-model-id]')).toHaveLength(1)
    await get(wrapper, 'model-search').setValue('friendly-model')
    expect(wrapper.findAll('[data-model-id]')).toHaveLength(1)
    await get(wrapper, 'model-search').setValue('gpt-image-1')
    expect(wrapper.findAll('[data-model-id]')).toHaveLength(1)
    expect(wrapper.text()).toContain('Image One')
    await get(wrapper, 'model-search').setValue('no-match')
    expect(wrapper.get('[role="status"]').text()).toBe('No matching models')
  })

  it('emits only the requested save/reset/sync action and does not implicitly save edits', async () => {
    const { wrapper } = setup({ policy: freeze(policy('openai')), mappings: freeze(mappings()) })
    await chatCard(wrapper, 'gpt-6-luna').get('input').setValue(false)
    expect(wrapper.emitted('save-policy')).toBeUndefined()
    await get(wrapper, 'save-policy').trigger('click')
    expect(wrapper.emitted('save-policy')).toEqual([[]])
    expect(wrapper.emitted('save-mappings')).toBeUndefined()
    await get(wrapper, 'save-mappings').trigger('click')
    expect(wrapper.emitted('save-mappings')).toEqual([[]])
    await get(wrapper, 'reset-policy').trigger('click')
    await get(wrapper, 'open-sync').trigger('click')
    expect(wrapper.emitted('reset-policy')).toEqual([[]])
    expect(wrapper.emitted('open-sync')).toEqual([[]])
  })

  it('edits all Claude defaults and preserves the incoming nested policy', async () => {
    const input = freeze(policy())
    const { wrapper } = setup({ policy: input })
    await get(wrapper, 'catalog-mode').setValue('worker_intersect_policy')
    await get(wrapper, 'default-max_tokens').setValue('16384')
    await get(wrapper, 'default-thinking_fallback_budget').setValue('8192')
    await get(wrapper, 'default-enabled').setValue(true)
    await get(wrapper, 'default-normalize_thinking').setValue(false)
    await get(wrapper, 'default-strip_context_1m').setValue(false)
    await get(wrapper, 'context-whitelist').setValue('claude-a, claude-b, ,')
    expect(lastPolicy(wrapper).catalog_mode).toBe('worker_intersect_policy')
    expect(lastPolicy(wrapper).defaults).toEqual({ enabled: true, max_tokens: 16384, thinking_fallback_budget: 8192, normalize_thinking: false, strip_context_1m: false, context_1m_whitelist: ['claude-a', 'claude-b'] })
    expect(input.defaults.max_tokens).toBe(8192)
    const count = wrapper.emitted('update:policy')!.length
    await get(wrapper, 'default-max_tokens').setValue('')
    await get(wrapper, 'default-thinking_fallback_budget').setValue('-1')
    expect(wrapper.emitted('update:policy')).toHaveLength(count)
  })

  it('edits Claude model params, capabilities, aliases and all beta fields as draft updates', async () => {
    const input = freeze(policy())
    const { wrapper } = setup({ policy: input })
    await chatCard(wrapper).get('button').trigger('click')
    await get(wrapper, 'display-name').setValue('Sonnet custom')
    await get(wrapper, 'context-window').setValue('1000000')
    await get(wrapper, 'param-max_tokens_default').setValue('16000')
    await get(wrapper, 'param-max_tokens_cap').setValue('128000')
    await get(wrapper, 'param-thinking_fallback_budget').setValue('10000')
    await get(wrapper, 'default-effort').setValue('max')
    await get(wrapper, 'param-on_adaptive').setValue('convert_to_enabled')
    await get(wrapper, 'param-on_enabled').setValue('convert_to_adaptive')
    await get(wrapper, 'model-aliases').setValue('sonnet, custom, ,')
    await get(wrapper, 'beta-1m').setValue('pass')
    await get(wrapper, 'beta-allow-client').setValue(false)
    await get(wrapper, 'beta-required').setValue('beta-one, beta-two')
    await get(wrapper, 'beta-drop').setValue('beta-three')
    const edited = lastPolicy(wrapper).models['claude-sonnet-4-6']
    expect(edited.display_name).toBe('Sonnet custom')
    expect(edited.capabilities).toEqual({ context_window: 1000000, supports_effort: true, retained_fact: 'keep' })
    expect(edited.params).toEqual({ max_tokens_default: 16000, max_tokens_cap: 128000, thinking_fallback_budget: 10000, default_effort: 'max', on_adaptive: 'convert_to_enabled', on_enabled: 'convert_to_adaptive' })
    expect(edited.aliases).toEqual(['sonnet', 'custom'])
    expect(edited.betas).toEqual({ pass_context_1m: true, allow_client: false, required: ['beta-one', 'beta-two'], drop: ['beta-three'] })
    await get(wrapper, 'beta-1m').setValue('strip')
    expect(lastPolicy(wrapper).models['claude-sonnet-4-6'].betas.pass_context_1m).toBe(false)
    await get(wrapper, 'beta-1m').setValue('inherit')
    expect(lastPolicy(wrapper).models['claude-sonnet-4-6'].betas).not.toHaveProperty('pass_context_1m')
    expect(input.models['claude-sonnet-4-6'].display_name).toBe('Claude Sonnet')
    expect(wrapper.emitted('save-policy')).toBeUndefined()
  })

  it('displays configured xhigh effort and preserves it through unrelated edits and parent echoes', async () => {
    const input = policy()
    input.models['claude-sonnet-4-6'].params.default_effort = 'xhigh'
    freeze(input)
    const { wrapper } = setup({ policy: input })
    await chatCard(wrapper).get('button').trigger('click')
    const effort = get(wrapper, 'default-effort')
    expect((effort.element as HTMLSelectElement).value).toBe('xhigh')
    expect(effort.get('option:checked').text()).toBe('xhigh')
    expect(wrapper.emitted('update:policy')).toBeUndefined()
    await get(wrapper, 'display-name').setValue('Sonnet renamed')
    expect(lastPolicy(wrapper).models['claude-sonnet-4-6'].params.default_effort).toBe('xhigh')
    await wrapper.setProps({ policy: clone(lastPolicy(wrapper)) })
    expect((effort.element as HTMLSelectElement).value).toBe('xhigh')
    await effort.setValue('high')
    await effort.setValue('xhigh')
    expect(lastPolicy(wrapper).models['claude-sonnet-4-6'].params.default_effort).toBe('xhigh')
    expect(input.models['claude-sonnet-4-6'].params.default_effort).toBe('xhigh')
    expect(input.models['claude-sonnet-4-6'].display_name).toBe('Claude Sonnet')
  })

  it('disables unsupported effort and handles models without capabilities or catalog facts', async () => {
    const value = policy()
    delete value.models['claude-sonnet-4-6'].capabilities
    const { wrapper } = setup({ policy: freeze(value) })
    await chatCard(wrapper).get('button').trigger('click')
    expect(get(wrapper, 'default-effort').attributes('disabled')).toBeDefined()
    await get(wrapper, 'context-window').setValue('200000')
    expect(lastPolicy(wrapper).models['claude-sonnet-4-6'].capabilities).toEqual({ context_window: 200000 })
    await wrapper.setProps({ policy: freeze(policy('openai')) })
    await chatCard(wrapper, 'gpt-image-1').get('button').trigger('click')
    expect(get(wrapper, 'model-details').text()).toContain('No catalog details available')
  })

  it('closes details and clears search on platform changes even when the next platform reuses the model key', async () => {
    const input = policy()
    const { wrapper } = setup({ policy: freeze(input) })
    await get(wrapper, 'model-search').setValue('Sonnet')
    await chatCard(wrapper).get('button').trigger('click')
    const next = policy('openai')
    next.models['claude-sonnet-4-6'] = model('claude-sonnet-4-6', { display_name: 'New platform same key' })
    await wrapper.setProps({ policy: freeze(next), mappings: freeze(mappings()) })
    expect(wrapper.find('[data-testid="model-details"]').exists()).toBe(false)
    expect((get(wrapper, 'model-search').element as HTMLInputElement).value).toBe('')
    expect(wrapper.find('[data-testid="catalog-mode"]').exists()).toBe(false)
    await chatCard(wrapper, 'gpt-6-luna').get('button').trigger('click')
    const details = get(wrapper, 'model-details')
    expect(details.text()).toContain('128000')
    expect(details.text()).toContain('256000')
    expect(details.text()).toContain('low, high')
    expect(details.text()).toContain('text, image')
    expect(details.text()).toContain('"shell":true')
    expect(wrapper.find('[data-testid="display-name"]').exists()).toBe(false)
  })

  it('accepts refreshed policy props and closes a removed model detail', async () => {
    const { wrapper } = setup()
    await chatCard(wrapper).get('button').trigger('click')
    const next = policy()
    delete next.models['claude-sonnet-4-6']
    await wrapper.setProps({ policy: freeze(next) })
    expect(wrapper.find('[data-testid="model-details"]').exists()).toBe(false)
    expect(wrapper.find('[data-model-id="claude-sonnet-4-6"]').exists()).toBe(false)
  })
})

describe('ManagedCatalogPanel permission and busy boundaries', () => {
  it('requires canSync in addition to canManage only for the sync action', async () => {
    const { wrapper } = setup({ canManage: true, canSync: false })
    const button = get(wrapper, 'open-sync')
    expect(button.attributes('disabled')).toBeDefined()
    // Dispatch directly as well, so the action guard is checked independently
    // of the browser/VTU's treatment of disabled buttons.
    button.element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(wrapper.emitted('open-sync')).toBeUndefined()
    for (const id of ['save-policy', 'reset-policy']) {
      expect(get(wrapper, id).attributes('disabled')).toBeUndefined()
      await get(wrapper, id).trigger('click')
    }
    expect(wrapper.emitted('save-policy')).toEqual([[]])
    expect(wrapper.emitted('reset-policy')).toEqual([[]])
    await chatCard(wrapper).get('input').setValue(false)
    expect(lastPolicy(wrapper).models['claude-sonnet-4-6'].enabled).toBe(false)
    await wrapper.setProps({ canSync: true })
    await button.trigger('click')
    expect(wrapper.emitted('open-sync')).toEqual([[]])
  })

  it.each([{ canManage: false, busy: false }, { canManage: true, busy: true }])('disables every Claude mutation field and action for %j', async state => {
    const { wrapper } = setup(state)
    await chatCard(wrapper).get('button').trigger('click')
    for (const field of wrapper.findAll('input:not([type="search"]), select')) expect(field.attributes('disabled')).toBeDefined()
    for (const id of ['save-policy', 'reset-policy', 'open-sync']) {
      expect(get(wrapper, id).attributes('disabled')).toBeDefined()
      await get(wrapper, id).trigger('click')
    }
    // Synthetic events are also guarded when dispatched to a disabled field.
    await get(wrapper, 'display-name').setValue('forbidden')
    await get(wrapper, 'default-max_tokens').setValue('9999')
    await get(wrapper, 'detail-enabled').setValue(false)
    expect(wrapper.emitted('update:policy')).toBeUndefined()
    expect(wrapper.emitted('save-policy')).toBeUndefined()
    expect(wrapper.emitted('reset-policy')).toBeUndefined()
    expect(wrapper.emitted('open-sync')).toBeUndefined()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(wrapper.find('[data-testid="model-details"]').exists()).toBe(false)
  })

  it.each([{ canManage: false, busy: false }, { canManage: true, busy: true }])('disables every mapping mutation for %j', async state => {
    const { wrapper } = setup({ ...state, policy: freeze(policy('openai')), mappings: freeze(mappings()) })
    const editor = get(wrapper, 'mappings-editor')
    for (const control of editor.findAll('input, select, button')) expect(control.attributes('disabled')).toBeDefined()
    await get(wrapper, 'add-mapping').trigger('click')
    await get(wrapper, 'mapping-target').setValue('gpt-6-luna')
    await get(wrapper, 'save-mappings').trigger('click')
    expect(wrapper.findAll('[data-testid="mapping-row"]')).toHaveLength(2)
    expect(wrapper.emitted('update:mappings')).toBeUndefined()
    expect(wrapper.emitted('save-mappings')).toBeUndefined()
  })
})

describe('ManagedCatalogPanel GPT mapping drafts', () => {
  it('keeps saved chat mapping candidates stable through unsaved enabled edits and parent policy echoes', async () => {
    const targets = freeze(['gpt-6-luna'])
    const { wrapper } = setup({ policy: freeze(policy('openai')), mappings: freeze(mappings()), mappingTargets: targets })
    await chatCard(wrapper, 'gpt-6-luna').get('input').setValue(false)
    await chatCard(wrapper, 'gpt-disabled').get('input').setValue(true)
    await chatCard(wrapper, 'gpt-image-1').get('input').setValue(true)
    await wrapper.setProps({ policy: clone(lastPolicy(wrapper)) })
    const select = get(wrapper, 'mapping-target')
    expect(select.find('option[value="gpt-6-luna"]').exists()).toBe(true)
    expect(select.find('option[value="gpt-disabled"]').exists()).toBe(false)
    expect(select.find('option[value="gpt-image-1"]').exists()).toBe(false)
    await get(wrapper, 'add-mapping').trigger('click')
    const added = wrapper.findAll('[data-testid="mapping-row"]').at(-1)!
    expect((added.get('select').element as HTMLSelectElement).value).toBe('gpt-6-luna')
    expect(targets).toEqual(['gpt-6-luna'])
    await wrapper.setProps({ mappingTargets: freeze(['gpt-disabled']) })
    expect(select.find('option[value="gpt-disabled"]').exists()).toBe(true)
    expect(select.find('option[value="gpt-6-luna"]').exists()).toBe(false)
  })

  it('does not infer saved candidates from policy when optional mappingTargets is omitted', async () => {
    const { wrapper } = setup({ policy: freeze(policy('openai')), mappings: freeze(mappings()), mappingTargets: undefined })
    expect(get(wrapper, 'mapping-target').find('option[value="gpt-6-luna"]').exists()).toBe(false)
    await get(wrapper, 'add-mapping').trigger('click')
    const added = wrapper.findAll('[data-testid="mapping-row"]').at(-1)!
    await added.get('input').setValue('new-alias')
    expect((added.get('select').element as HTMLSelectElement).value).toBe('')
    expect(lastDraftState(wrapper)).toEqual({ dirty: true, valid: false })
    expect(wrapper.emitted('update:mappings')).toBeUndefined()
  })

  it('reports an untouched baseline on mount and an incomplete added row as a dirty invalid draft', async () => {
    const input = freeze(mappings())
    const { wrapper } = setup({ policy: freeze(policy('openai')), mappings: input })
    expect(lastDraftState(wrapper)).toEqual({ dirty: false, valid: true })
    const editor = wrapper.getComponent(ModelMappingsEditor)
    expect(editor.emitted('draft-state')).toEqual([[{ dirty: false, valid: true }]])
    await get(wrapper, 'add-mapping').trigger('click')
    expect(lastDraftState(wrapper)).toEqual({ dirty: true, valid: false })
    expect(editor.emitted('draft-state')!.at(-1)).toEqual([{ dirty: true, valid: false }])
    expect(wrapper.emitted('update:mappings')).toBeUndefined()
    expect(input.mappings).toEqual(mappings().mappings)
  })

  it.each(['fast', 'FAST'])('reports a duplicate alias %s without projecting away the extra row', async alias => {
    const { wrapper } = setup({ policy: freeze(policy('openai')), mappings: freeze(mappings()) })
    await get(wrapper, 'add-mapping').trigger('click')
    const row = wrapper.findAll('[data-testid="mapping-row"]').at(-1)!
    await row.get('input').setValue(alias)
    expect(lastDraftState(wrapper)).toEqual({ dirty: true, valid: false })
    expect(wrapper.emitted('update:mappings')).toBeUndefined()
    expect(get(wrapper, 'save-mappings').attributes('disabled')).toBeDefined()
  })

  it('rejects a named mapping with an empty target when no enabled chat targets exist', async () => {
    const value = policy('openai')
    Object.values(value.models).forEach(model => { model.enabled = false })
    const { wrapper } = setup({ policy: freeze(value), mappings: freeze(mappings()) })
    await get(wrapper, 'add-mapping').trigger('click')
    const row = wrapper.findAll('[data-testid="mapping-row"]').at(-1)!
    await row.get('input').setValue('new-alias')
    expect((row.get('select').element as HTMLSelectElement).value).toBe('')
    expect(row.get('select').attributes('aria-invalid')).toBe('true')
    expect(wrapper.get('[role="alert"]').text()).toBe(en.invalidTarget)
    expect(lastDraftState(wrapper)).toEqual({ dirty: true, valid: false })
    expect(get(wrapper, 'save-mappings').attributes('disabled')).toBeDefined()
    await get(wrapper, 'save-mappings').trigger('click')
    expect(wrapper.emitted('update:mappings')).toBeUndefined()
    expect(wrapper.emitted('save-mappings')).toBeUndefined()
  })

  it('returns to a clean valid baseline when an incomplete or duplicate row is removed', async () => {
    const { wrapper } = setup({ policy: freeze(policy('openai')), mappings: freeze(mappings()) })
    for (const alias of ['', 'fast']) {
      await get(wrapper, 'add-mapping').trigger('click')
      const row = wrapper.findAll('[data-testid="mapping-row"]').at(-1)!
      await row.get('input').setValue(alias)
      expect(lastDraftState(wrapper)).toEqual({ dirty: true, valid: false })
      await row.get('button').trigger('click')
      expect(lastDraftState(wrapper)).toEqual({ dirty: false, valid: true })
      expect(lastMappings(wrapper)).toEqual(mappings())
      expect(get(wrapper, 'save-mappings').attributes('disabled')).toBeUndefined()
    }
  })

  it('transfers a valid local change to parent mappingDirty after its echo, then reports further incomplete drafts', async () => {
    const { wrapper } = setup({ policy: freeze(policy('openai')), mappings: freeze(mappings()) })
    await get(wrapper, 'add-mapping').trigger('click')
    const row = wrapper.findAll('[data-testid="mapping-row"]').at(-1)!
    await row.get('input').setValue('new-alias')
    expect(lastDraftState(wrapper)).toEqual({ dirty: true, valid: true })
    const published = freeze(clone(lastMappings(wrapper)))
    await wrapper.setProps({ mappings: published })
    expect(lastDraftState(wrapper)).toEqual({ dirty: false, valid: true })
    await get(wrapper, 'add-mapping').trigger('click')
    expect(lastDraftState(wrapper)).toEqual({ dirty: true, valid: false })
    // A re-created echo must not discard the unpublished incomplete row.
    await wrapper.setProps({ mappings: clone(published) })
    expect(wrapper.findAll('[data-testid="mapping-row"]')).toHaveLength(4)
    expect(lastDraftState(wrapper)).toEqual({ dirty: true, valid: false })
    await wrapper.setProps({ mappings: freeze({ version: 12, mappings: { replacement: 'gpt-6-luna' } }) })
    expect(lastDraftState(wrapper)).toEqual({ dirty: false, valid: true })
    expect(wrapper.findAll('[data-testid="mapping-row"]')).toHaveLength(1)
  })

  it.each(['platform', 'mappings', 'unmount'])('clears the reported draft state when the editor leaves via %s', async leave => {
    const { wrapper } = setup({ policy: freeze(policy('openai')), mappings: freeze(mappings()) })
    const editor = wrapper.getComponent(ModelMappingsEditor)
    await get(wrapper, 'add-mapping').trigger('click')
    expect(lastDraftState(wrapper)).toEqual({ dirty: true, valid: false })
    if (leave === 'platform') await wrapper.setProps({ policy: freeze(policy()) })
    else if (leave === 'mappings') await wrapper.setProps({ mappings: null })
    else wrapper.unmount()
    expect(lastDraftState(wrapper)).toEqual({ dirty: false, valid: true })
    expect(editor.emitted('draft-state')!.at(-1)).toEqual([{ dirty: false, valid: true }])
  })

  it('keeps the reserved alias fixed and non-deletable while allowing target editing and reset', async () => {
    const input = freeze(mappings())
    const before = JSON.stringify(input)
    const { wrapper } = setup({ policy: freeze(policy('openai')), mappings: input })
    const fixed = wrapper.get('[data-fixed="true"]')
    expect(fixed.get('input').attributes('disabled')).toBeDefined()
    expect(fixed.find('[data-testid="remove-mapping"]').exists()).toBe(false)
    expect(fixed.get('select').text()).toContain('unavailable-old-model — Unavailable')
    expect(fixed.get('select').find('option[value="gpt-disabled"]').exists()).toBe(false)
    expect(fixed.get('select').find('option[value="gpt-image-1"]').exists()).toBe(false)
    await fixed.get('input').setValue('cannot-rename')
    expect(wrapper.emitted('update:mappings')).toBeUndefined()
    await fixed.get('select').setValue('gpt-6-luna')
    expect(lastMappings(wrapper)).toEqual({ version: 11, mappings: { 'codex-auto-review': 'gpt-6-luna', fast: 'gpt-6-luna' } })
    await get(wrapper, 'reset-mapping-target').trigger('click')
    expect(lastMappings(wrapper).mappings['codex-auto-review']).toBe('gpt-6-luna')
    expect(JSON.stringify(input)).toBe(before)
  })

  it('adds, renames and removes mappings without losing incomplete rows on parent echoes', async () => {
    const { wrapper } = setup({ policy: freeze(policy('openai')), mappings: freeze(mappings()) })
    await get(wrapper, 'add-mapping').trigger('click')
    await get(wrapper, 'add-mapping').trigger('click')
    expect(get(wrapper, 'save-mappings').attributes('disabled')).toBeDefined()
    const added = wrapper.findAll('[data-testid="mapping-row"]').at(-1)!
    await added.get('input').setValue('extra')
    // A different incomplete row still prevents submitting a partial draft.
    expect(wrapper.emitted('update:mappings')).toBeUndefined()
    await wrapper.findAll('[data-testid="mapping-row"]')[2].get('input').setValue('another')
    const published = lastMappings(wrapper)
    expect(published.mappings.extra).toBe('gpt-6-luna')
    await wrapper.setProps({ mappings: clone(published) })
    expect(wrapper.findAll('[data-testid="mapping-row"]')).toHaveLength(4)
    await added.get('input').setValue('renamed')
    expect(lastMappings(wrapper).mappings).toHaveProperty('renamed')
    expect(lastMappings(wrapper).mappings).not.toHaveProperty('extra')
    expect(published.mappings).toHaveProperty('extra')
    await added.get('button').trigger('click')
    expect(lastMappings(wrapper).mappings).not.toHaveProperty('renamed')
    expect(lastMappings(wrapper).mappings).toHaveProperty('codex-auto-review')
  })

  it.each(['', 'two words', 'FAST', 'Codex-Auto-Review'])('blocks invalid or duplicate aliases: %j', async alias => {
    const { wrapper } = setup({ policy: freeze(policy('openai')), mappings: freeze(mappings()) })
    await get(wrapper, 'add-mapping').trigger('click')
    const row = wrapper.findAll('[data-testid="mapping-row"]').at(-1)!
    await row.get('input').setValue(alias)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(get(wrapper, 'save-mappings').attributes('disabled')).toBeDefined()
    await get(wrapper, 'save-mappings').trigger('click')
    expect(wrapper.emitted('save-mappings')).toBeUndefined()
    expect(wrapper.emitted('update:mappings')).toBeUndefined()
  })

  it('replaces local rows for an external snapshot and hides mappings when unavailable or on Claude', async () => {
    const { wrapper } = setup({ policy: freeze(policy('openai')), mappings: freeze(mappings()) })
    await get(wrapper, 'add-mapping').trigger('click')
    await wrapper.setProps({ mappings: freeze({ version: 12, mappings: { 'codex-auto-review': 'gpt-6-luna' } }) })
    expect(wrapper.findAll('[data-testid="mapping-row"]')).toHaveLength(1)
    await wrapper.setProps({ mappings: null })
    expect(wrapper.find('[data-testid="mappings-editor"]').exists()).toBe(false)
    await wrapper.setProps({ mappings: mappings(), policy: policy() })
    expect(wrapper.find('[data-testid="mappings-editor"]').exists()).toBe(false)
  })
})

function clone<T>(value: T): T { return JSON.parse(JSON.stringify(value)) as T }

describe('ManagedCatalogPanel local language and accessible labels', () => {
  it.each([['en', en], ['zh', zh], ['zh-TW', zhTW], ['ja', ja]] as const)('renders complete local %s messages and labelled native fields', async (locale, messages) => {
    expect(Object.keys(messages).sort()).toEqual(Object.keys(en).sort())
    const { wrapper } = setup({}, locale)
    expect(wrapper.text()).toContain(messages.savePolicy)
    expect(wrapper.text()).toContain(messages.advanced)
    expect(wrapper.text()).not.toContain('managedModels.')
    await chatCard(wrapper).get('button').trigger('click')
    expect(get(wrapper, 'model-details').text()).toContain(messages.beta_required)
    for (const field of wrapper.findAll('input, select')) expect(field.element.closest('label')?.textContent?.trim()).toBeTruthy()
    expect(wrapper.get('[role="dialog"]').attributes('aria-labelledby')).toBeTruthy()
  })

  it('reacts to global language changes without registering global message keys', async () => {
    const { wrapper, i18n } = setup()
    i18n.global.locale.value = 'ja'
    await nextTick()
    expect(get(wrapper, 'save-policy').text()).toBe(ja.savePolicy)
    expect(i18n.global.te('managedModels.title')).toBe(false)
  })
})
