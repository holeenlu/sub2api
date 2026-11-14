import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import type { Component } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import DocsView from '../DocsView.vue'

vi.mock('vue-i18n', async importOriginal => {
  const labels: Record<string, string> = {
    'docs.search': 'Search documentation and models',
    'docs.searchApps': 'Search applications',
    'docs.close': 'Close',
    'docs.pages.responses.title': 'OpenAI Responses'
  }
  return {
    ...await importOriginal<typeof import('vue-i18n')>(),
    useI18n: () => ({ locale: ref('en'), t: (key: string) => labels[key] ?? key })
  }
})
vi.mock('@/features/site/ports', () => ({
  useSiteSettings: () => ({ load: vi.fn().mockResolvedValue(null) }),
  useSiteActor: () => ({ isAuthenticated: false, adminLandingPath: '' }),
  useSitePresentation: () => ({
    brand: { name: 'Test site', logo: '', apiBaseUrl: '' },
    createModelQuery: () => ({
      data: ref({ groups: [{ id: 1, models: [{ name: 'custom/model' }, { name: 'custom/model' }] }] }),
      loading: ref(false), loadFailed: ref(false), availability: ref('available'), load: vi.fn()
    })
  })
}))

enableAutoUnmount(afterEach)
afterEach(() => {
  document.body.innerHTML = ''
  localStorage.clear()
  vi.restoreAllMocks()
})

function element<T extends HTMLElement = HTMLElement>(selector: string): T {
  const found = document.querySelector<T>(selector)
  if (!found) throw new Error(`Missing element: ${selector}`)
  return found
}

function key(key: string, options: KeyboardEventInit = {}) {
  const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...options })
  ;(document.activeElement ?? document).dispatchEvent(event)
  return event
}

async function mountDocs(path = '/docs', article: Component | boolean = true) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/docs/:pathMatch(.*)*', component: DocsView },
      { path: '/apps/:pathMatch(.*)*', component: DocsView },
      { path: '/:pathMatch(.*)*', component: {
        template: '<main><button id="destination-focus">Destination</button></main>',
        mounted() { element('#destination-focus').focus() }
      } }
    ]
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(RouterView, {
    attachTo: document.body,
    global: {
      plugins: [router],
      stubs: {
        transition: true, Icon: true, LocaleSwitcher: { template: '<button>Language</button>' },
        DocsSidebar: { emits: ['navigate'], template: `<button @click="$emit('navigate')">Sidebar navigation</button>` },
        DocsHomeView: true, DocsModelsView: true, DocsModelView: true, DocsArticleView: article
      }
    }
  })
  await flushPromises()
  const triggerButton = wrapper.findAll<HTMLButtonElement>('header button').find(button => button.text().includes('⌘ K'))
  if (!triggerButton) throw new Error('Missing documentation search trigger')
  const trigger = triggerButton.element
  async function open() {
    trigger.focus()
    trigger.click()
    await flushPromises()
    return element<HTMLInputElement>('input')
  }
  return { wrapper, router, trigger, open }
}

async function search(input: HTMLInputElement, value: string) {
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
}

describe('documentation search accessibility', () => {
  it('opens a named modal with a labelled, focused input and filtered results', async () => {
    const { open } = await mountDocs()
    const input = await open()
    await search(input, ' Responses ')
    expect(document.activeElement).toBe(input)
    expect(element('a[href="/docs/api/chat/openai-responses"]')).toBeTruthy()
    const dialog = element('[role="dialog"]')
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    const titleId = dialog.getAttribute('aria-labelledby')
    expect(titleId && document.getElementById(titleId)?.textContent).toBe('Search documentation and models')
    expect(input.getAttribute('aria-label')).toBe('Search documentation and models')
    expect(dialog.querySelectorAll('a')).toHaveLength(1)
  })

  it.each(['Escape', 'close', 'backdrop'])('restores the trigger after %s dismissal', async dismissal => {
    const { open, trigger } = await mountDocs()
    const input = await open()
    await search(input, 'Responses')
    if (dismissal === 'Escape') key('Escape')
    else if (dismissal === 'close') input.parentElement?.querySelector('button')?.click()
    else (input.closest('.modal-overlay') ?? input.closest('.fixed'))?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('input')).toBeNull()
    expect(document.activeElement).toBe(trigger)
  })

  it('wraps Tab and Shift+Tab across the current search results', async () => {
    const { open } = await mountDocs()
    const input = await open()
    await search(input, 'Responses')
    const last = input.closest('[role="dialog"]')?.querySelector<HTMLAnchorElement>('a')
    expect(key('Tab', { shiftKey: true }).defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(last)
    expect(key('Tab').defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(input)
    expect(key('Tab').defaultPrevented).toBe(false)
  })

  it.each(['ctrlKey', 'metaKey'])('opens with %s+K and resets the previous query on reopening', async modifier => {
    const { trigger } = await mountDocs()
    trigger.focus()
    expect(key('K', { [modifier]: true }).defaultPrevented).toBe(true)
    await flushPromises()
    await search(element('input'), 'Responses')
    key('Escape')
    await flushPromises()
    key('k', { [modifier]: true })
    await flushPromises()
    expect(element<HTMLInputElement>('input').value).toBe('')
    expect(document.activeElement).toBe(element('input'))
  })

  it('hands focus to the destination after RouterLink result navigation', async () => {
    const { open, router, trigger } = await mountDocs()
    await search(await open(), 'Responses')
    element<HTMLAnchorElement>('a[href="/docs/api/chat/openai-responses"]').click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs/api/chat/openai-responses')
    expect(document.querySelector('input')).toBeNull()
    expect(document.activeElement).toBe(element('main'))
    expect(document.activeElement).not.toBe(trigger)
  })

  it('restores the trigger even when pointer activation did not focus it', async () => {
    const { trigger } = await mountDocs()
    expect(document.activeElement).toBe(document.body)
    trigger.click()
    await flushPromises()
    expect(document.activeElement).toBe(element('input'))
    expect(element('[role="dialog"] button').getAttribute('aria-label')).toBe('Close')
    key('Escape')
    await flushPromises()
    expect(document.activeElement).toBe(trigger)
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
  })

  it('keeps empty results keyboard-contained and re-evaluates Tab stops after filtering', async () => {
    const { open } = await mountDocs()
    const input = await open()
    await search(input, 'no-such-document')
    expect(element('[role="dialog"]').textContent).toContain('docs.noResults')
    expect(key('Tab', { shiftKey: true }).defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(element('[role="dialog"] button'))
    expect(key('Tab').defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(input)
    await search(input, 'Responses')
    key('Tab', { shiftKey: true })
    expect(document.activeElement).toBe(element('[role="dialog"] a'))
  })

  it('keeps deduplicated model search and application-only filtering', async () => {
    const { open, router } = await mountDocs()
    await search(await open(), 'custom/model')
    expect(element('[role="dialog"]').querySelectorAll('a')).toHaveLength(1)
    expect(element('[role="dialog"] a').getAttribute('href')).toBe('/docs/models/custom%2Fmodel')
    key('Escape')
    await flushPromises()
    await router.push('/apps')
    const input = await open()
    expect(input.getAttribute('aria-label')).toBe('Search applications')
    await search(input, 'custom/model')
    expect(element('[role="dialog"]').querySelectorAll('a')).toHaveLength(0)
    await search(input, 'codex')
    expect(element('[role="dialog"] a').getAttribute('href')).toBe('/apps/codex')
  })

  it('preserves native modified-click navigation without dismissing the current search', async () => {
    const { open, router } = await mountDocs()
    await search(await open(), 'Responses')
    const link = element('[role="dialog"] a')
    const event = new MouseEvent('click', { bubbles: true, cancelable: true, ctrlKey: true })
    // Prevent jsdom's unimplemented navigation only after checking our handler.
    link.addEventListener('click', e => { expect(e.defaultPrevented).toBe(false); e.preventDefault() }, { once: true })
    link.dispatchEvent(event)
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs')
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
  })

  it('keeps search open on an aborted route and closes a same-page result', async () => {
    const { open, router } = await mountDocs('/docs/api/chat/openai-responses')
    const removeGuard = router.beforeEach(() => false)
    await search(await open(), 'queryModels')
    element<HTMLAnchorElement>('[role="dialog"] a').click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs/api/chat/openai-responses')
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    removeGuard()
    await search(element('input'), 'Responses')
    element<HTMLAnchorElement>('[role="dialog"] a').click()
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(element('main'))
  })

  it('preserves focus that the destination content explicitly takes', async () => {
    const { open } = await mountDocs('/docs', {
      template: '<button id="article-focus">Read article</button>',
      mounted() { element('#article-focus').focus() }
    })
    await search(await open(), 'Responses')
    element<HTMLAnchorElement>('[role="dialog"] a').click()
    await flushPromises()
    expect(document.activeElement).toBe(element('#article-focus'))
  })

  it('does not restore a removed opener or steal focus when navigation unmounts docs', async () => {
    const { open, router, trigger } = await mountDocs()
    router.beforeEach(to => to.path.includes('openai-responses') ? '/away' : true)
    await search(await open(), 'Responses')
    const focus = vi.spyOn(trigger, 'focus')
    element<HTMLAnchorElement>('[role="dialog"] a').click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/away')
    expect(trigger.isConnected).toBe(false)
    expect(focus).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(element('#destination-focus'))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })

  it('does not let an old navigation close a newly opened search', async () => {
    const { open, router } = await mountDocs()
    let finishNavigation: ((allow: boolean) => void) | undefined
    router.beforeEach(() => new Promise<boolean>(resolve => { finishNavigation = resolve }))
    await search(await open(), 'Responses')
    element<HTMLAnchorElement>('[role="dialog"] a').click()
    await flushPromises()
    key('Escape')
    await flushPromises()
    const currentInput = await open()
    await search(currentInput, 'custom/model')
    expect(finishNavigation).toBeTypeOf('function')
    finishNavigation?.(true)
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs/api/chat/openai-responses')
    expect(element('input')).toBe(currentInput)
    expect(currentInput.value).toBe('custom/model')
    expect(document.activeElement).toBe(currentInput)
  })

  it('preserves mobile sidebar opening, navigation dismissal and Escape dismissal', async () => {
    const { wrapper, router } = await mountDocs()
    await wrapper.get('header button[title="docs.menu"]').trigger('click')
    expect(wrapper.find('.fixed aside').exists()).toBe(true)
    const sidebarLink = wrapper.get('.fixed aside > button')
    expect(sidebarLink.text()).toBe('Sidebar navigation')
    await sidebarLink.trigger('click')
    expect(wrapper.find('.fixed aside').exists()).toBe(false)
    await wrapper.get('header button[title="docs.menu"]').trigger('click')
    key('Escape')
    await nextTick()
    expect(wrapper.find('.fixed aside').exists()).toBe(false)
    await wrapper.get('header button[title="docs.menu"]').trigger('click')
    await router.push('/apps')
    expect(wrapper.find('.fixed aside').exists()).toBe(false)
  })

  it('releases dialog and shortcut listeners and scroll ownership on unmount', async () => {
    const windowAdd = vi.spyOn(window, 'addEventListener')
    const windowRemove = vi.spyOn(window, 'removeEventListener')
    const documentAdd = vi.spyOn(document, 'addEventListener')
    const documentRemove = vi.spyOn(document, 'removeEventListener')
    const { wrapper, open, trigger } = await mountDocs()
    await open()
    expect(document.body.classList.contains('modal-open')).toBe(true)
    const focus = vi.spyOn(trigger, 'focus')
    wrapper.unmount()
    await nextTick()
    for (const [type, handler] of windowAdd.mock.calls.filter(([type]) => type === 'keydown')) {
      expect(windowRemove).toHaveBeenCalledWith(type, handler)
    }
    for (const [type, handler] of documentAdd.mock.calls.filter(([type]) => type === 'keydown')) {
      expect(documentRemove).toHaveBeenCalledWith(type, handler)
    }
    expect(focus).not.toHaveBeenCalled()
    expect(document.body.classList.contains('modal-open')).toBe(false)
    expect(key('k', { ctrlKey: true }).defaultPrevented).toBe(false)
    expect(key('Tab').defaultPrevented).toBe(false)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
})
