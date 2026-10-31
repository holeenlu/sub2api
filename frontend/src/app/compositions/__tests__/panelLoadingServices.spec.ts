import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, inject, onMounted, ref, type Component, type InjectionKey, type Ref } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { lazyPanel } from '../panelLoadingServices'
import * as resourcePanels from '@/features/resources/panelLoaders'
import * as identityPanels from '@/features/identity/panelLoaders'
import * as usagePanels from '@/features/usage/panelLoaders'
import * as operationPanels from '@/features/operations/panelLoaders'
import * as catalogPanels from '@/features/catalog/panelLoaders'
import * as announcementPanels from '@/features/site/announcementPanels'

vi.mock('vue-i18n', async original => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key, locale: ref('en') })
}))
enableAutoUnmount(afterEach)
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
const scopeKey: InjectionKey<Ref<string>> = Symbol('test-panel-scope')
function fixture(mounted: (scope: string) => void) {
  return defineComponent({
    name: 'TestPanelContract',
    props: { target: { type: Number, required: true } },
    emits: { confirm: (target: number) => Number.isInteger(target) },
    setup(props, { emit, slots }) {
      const scope = inject(scopeKey)!
      onMounted(() => mounted(scope.value))
      return () => h('button', { type: 'button', onClick: () => emit('confirm', props.target) }, [
        scope.value + ':' + props.target, slots.default?.()
      ])
    }
  })
}

// Keep a stable parent, as in real provider consumers; VTU unwraps async roots.
function host(Panel: Component) {
  return defineComponent({
    inheritAttrs: false,
    props: { target: { type: Number, required: true } },
    setup(props, { attrs, slots }) {
      return () => h(Panel, { ...attrs, target: props.target }, slots)
    }
  })
}

describe('lazy provider panel contracts', () => {
  it('loads only on mount and forwards current props, events, slots and injected scope', async () => {
    const mounted = vi.fn()
    const Panel = fixture(mounted)
    const request = deferred<typeof Panel>()
    const loader = vi.fn(() => request.promise)
    const Lazy = lazyPanel(loader)
    expect(loader).not.toHaveBeenCalled()
    const scope = ref('actor-a')
    const confirm = vi.fn()
    const click = vi.fn()
    const wrapper = mount(host(Lazy), { props: { target: 7, onConfirm: confirm, onClick: click }, slots: { default: 'slot content' }, global: { provide: { [scopeKey as symbol]: scope } } })
    expect(wrapper.get('[role="status"]').attributes('aria-label')).toBe('common.loading')
    expect(wrapper.get('[role="status"]').attributes('target')).toBeUndefined()
    await wrapper.get('[role="status"]').trigger('click')
    expect(click).not.toHaveBeenCalled()
    expect(confirm).not.toHaveBeenCalled()
    expect(mounted).not.toHaveBeenCalled()
    scope.value = 'actor-b'
    await wrapper.setProps({ target: 8 })
    request.resolve(Panel)
    await flushPromises()
    expect(wrapper.get('button').text()).toContain('actor-b:8')
    expect(wrapper.text()).toContain('slot content')
    expect(mounted).toHaveBeenCalledOnce()
    expect(mounted).toHaveBeenCalledWith('actor-b')
    await wrapper.get('button').trigger('click')
    expect(confirm).toHaveBeenCalledOnce()
    expect(confirm).toHaveBeenCalledWith(8)
    expect(click).toHaveBeenCalledOnce()
    expect(loader).toHaveBeenCalledOnce()
  })

  it('does not mount late content or run its effects after the owner unmounts', async () => {
    const mounted = vi.fn()
    const Panel = fixture(mounted)
    const request = deferred<typeof Panel>()
    const wrapper = mount(host(lazyPanel(() => request.promise)), { props: { target: 7 }, global: { provide: { [scopeKey as symbol]: ref('actor-a') } } })
    wrapper.unmount()
    request.resolve(Panel)
    await flushPromises()
    expect(mounted).not.toHaveBeenCalled()
  })

  it('keeps closed modal loading and failure UI hidden until show becomes true', async () => {
    const request = deferred<ReturnType<typeof fixture>>()
    const Lazy = lazyPanel(() => request.promise)
    const visible = ref(false)
    const report = vi.fn()
    const Host = defineComponent({ setup: () => () => h(Lazy, { target: 7, show: visible.value }) })
    const wrapper = mount(Host, { global: { config: { errorHandler: report } } })
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    visible.value = true
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    visible.value = false
    request.reject(new Error('synthetic closed-panel failure'))
    await flushPromises()
    expect(report).toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    visible.value = true
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[role="alert"]').text()).toBe('common.error')
  })

  it('renders and reports a chunk failure without retrying or invoking business handlers', async () => {
    const error = new Error('synthetic chunk failure')
    const request = deferred<ReturnType<typeof fixture>>()
    const loader = vi.fn(() => request.promise)
    const report = vi.fn()
    const confirm = vi.fn()
    const wrapper = mount(host(lazyPanel(loader)), { props: { target: 7, onConfirm: confirm }, global: { config: { errorHandler: report } } })
    request.reject(error)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('common.error')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(report).toHaveBeenCalled()
    expect(report.mock.calls[0][0]).toBe(error)
    expect(loader).toHaveBeenCalledOnce()
    expect(confirm).not.toHaveBeenCalled()
  })

  it('shows timeout failure and ignores a later resolution after unmount', async () => {
    vi.useFakeTimers()
    const mounted = vi.fn()
    const Panel = fixture(mounted)
    const request = deferred<typeof Panel>()
    const loader = vi.fn(() => request.promise)
    const report = vi.fn()
    const wrapper = mount(host(lazyPanel(loader)), { props: { target: 7 }, global: { provide: { [scopeKey as symbol]: ref('actor-a') }, config: { errorHandler: report } } })
    await vi.advanceTimersByTimeAsync(30_000)
    expect(wrapper.get('[role="alert"]').text()).toBe('common.error')
    expect(report).toHaveBeenCalled()
    wrapper.unmount()
    request.resolve(Panel)
    await flushPromises()
    expect(mounted).not.toHaveBeenCalled()
    expect(loader).toHaveBeenCalledOnce()
  })
})

describe('explicit domain panel loaders', () => {
  const loaders = { ...resourcePanels, ...identityPanels, ...usagePanels, ...operationPanels, ...catalogPanels, ...announcementPanels }
  it.each(Object.entries(loaders))('%s resolves the real named Vue component', async (name, loader) => {
    const loaded = await loader()
    expect(loaded.default.__name ?? loaded.default.name).toBe(name.replace(/^load/, ''))
    expect(loaded.default.setup).toBeTypeOf('function')
  })
})
