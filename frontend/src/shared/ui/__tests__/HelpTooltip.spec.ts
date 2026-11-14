import { afterEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, enableAutoUnmount, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import HelpTooltip from '@/shared/ui/HelpTooltip.vue'

enableAutoUnmount(afterEach)

function getTooltipElement(): HTMLDivElement {
  const tooltip = document.body.querySelector('[role="tooltip"]')
  if (!(tooltip instanceof HTMLDivElement)) {
    throw new Error('tooltip element not found')
  }
  return tooltip
}

describe('HelpTooltip', () => {
  afterEach(() => {
    window.getSelection()?.removeAllRanges()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('keeps the existing hover interaction by default', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'hover details',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()

    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('mouseenter')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    await trigger.trigger('mouseleave')
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    wrapper.unmount()
  })

  it('keeps a hover tooltip open while the pointer moves between the trigger and the tooltip', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'copyable details',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()

    await trigger.trigger('mouseenter')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    await trigger.trigger('mouseleave', { relatedTarget: tooltip })
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    tooltip.dispatchEvent(new MouseEvent('mouseleave', { relatedTarget: trigger.element }))
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    tooltip.dispatchEvent(new MouseEvent('mouseleave', { relatedTarget: null }))
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    wrapper.unmount()
  })

  it('supports click-to-toggle details and closes on outside click', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'click details',
        trigger: 'click',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()

    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('click')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')
    expect(tooltip.textContent).toContain('click details')

    const closeButton = tooltip.querySelector('button[aria-label="Close"]')
    if (!(closeButton instanceof HTMLButtonElement)) {
      throw new Error('close button not found')
    }
    closeButton.click()
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('click')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    wrapper.unmount()
  })
  it('supports account details by hover, keyboard focus and touch click, with Escape dismissal', async () => {
    const wrapper = mount(HelpTooltip, { attachTo: document.body, props: { trigger: 'both', content: 'details' } })
    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()
    await trigger.trigger('focusin')
    await trigger.trigger('click')
    expect(tooltip.style.display).not.toBe('none')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(tooltip.style.display).toBe('none')
    await trigger.trigger('mouseenter')
    expect(tooltip.style.display).not.toBe('none')
    document.body.click()
    await nextTick()
    expect(tooltip.style.display).toBe('none')
    wrapper.unmount()
  })

  it.each(['default', 'brand', 'surface'] as const)('uses viewport coordinates for a fixed %s tooltip after document scrolling', async tone => {
    vi.stubGlobal('scrollX', 60)
    vi.stubGlobal('scrollY', 700)
    const wrapper = mount(HelpTooltip, { attachTo: document.body, props: { tone, content: 'Scrolled help' } })
    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(320, 400, 20, 20))
    vi.spyOn(tooltip, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 256, 80))
    await trigger.trigger('mouseenter')
    await nextTick()
    expect(tooltip.style.left).toBe('330px')
    expect(tooltip.style.top).toBe('calc(392px)')
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(300, 340, 20, 20))
    vi.stubGlobal('scrollX', 80)
    vi.stubGlobal('scrollY', 760)
    window.dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(tooltip.style.left).toBe('310px')
    expect(tooltip.style.top).toBe('calc(332px)')
  })

  it('keeps hover help available to a focused keyboard control after the pointer leaves', async () => {
    const wrapper = mount(HelpTooltip, { attachTo: document.body, props: { content: 'Keyboard help' } })
    const trigger = wrapper.get('.group')
    const button = wrapper.get('button')
    const tooltip = new DOMWrapper(getTooltipElement())
    button.element.focus()
    await nextTick()
    await trigger.trigger('mouseenter')
    await trigger.trigger('mouseleave')
    expect(tooltip.isVisible()).toBe(true)
    button.element.blur()
    await nextTick()
    await nextTick()
    expect(tooltip.isVisible()).toBe(false)
  })

  it('does not close hover help on keyboard blur while the pointer remains over the panel', async () => {
    const wrapper = mount(HelpTooltip, { attachTo: document.body, props: { content: 'Pointer help' } })
    const trigger = wrapper.get('.group')
    const button = wrapper.get('button')
    const tooltip = new DOMWrapper(getTooltipElement())
    button.element.focus()
    await nextTick()
    await trigger.trigger('mouseleave', { relatedTarget: tooltip.element })
    await tooltip.trigger('mouseenter')
    button.element.blur()
    await nextTick()
    await nextTick()
    expect(tooltip.isVisible()).toBe(true)
    await tooltip.trigger('mouseleave')
    expect(tooltip.isVisible()).toBe(false)
  })

})

describe('interactive account tooltip', () => {
  afterEach(() => {
    window.getSelection()?.removeAllRanges()
    vi.restoreAllMocks()
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  function mountDetails() {
    return mount(HelpTooltip, {
      attachTo: document.body,
      props: { trigger: 'both', content: 'Selectable details' },
      slots: { trigger: '<button type="button">Details</button>' }
    })
  }

  it('lets a pointer cross the gap and closes only after 250 ms outside', async () => {
    vi.useFakeTimers()
    const wrapper = mountDetails()
    const trigger = wrapper.get('.group')
    const panel = new DOMWrapper(getTooltipElement())
    await trigger.trigger('mouseenter')
    await trigger.trigger('mouseleave')
    await vi.advanceTimersByTimeAsync(150)
    expect(panel.isVisible()).toBe(true)
    await panel.trigger('mouseenter')
    await vi.advanceTimersByTimeAsync(500)
    expect(panel.isVisible()).toBe(true)
    await panel.trigger('mouseleave')
    await vi.advanceTimersByTimeAsync(249)
    expect(panel.isVisible()).toBe(true)
    await vi.advanceTimersByTimeAsync(1)
    expect(panel.isVisible()).toBe(false)
  })

  it('pins on activation, survives leaving both surfaces, and unpins on activation', async () => {
    vi.useFakeTimers()
    const wrapper = mountDetails()
    const panel = new DOMWrapper(getTooltipElement())
    await wrapper.get('button').trigger('click')
    await wrapper.get('.group').trigger('mouseleave')
    await panel.trigger('mouseleave')
    await vi.advanceTimersByTimeAsync(500)
    expect(panel.isVisible()).toBe(true)
    await wrapper.get('button').trigger('click')
    expect(panel.isVisible()).toBe(false)
  })

  it('keeps a drag selection available after leaving and releasing outside', async () => {
    vi.useFakeTimers()
    const wrapper = mountDetails()
    const panel = new DOMWrapper(getTooltipElement())
    await wrapper.get('.group').trigger('mouseenter')
    await panel.trigger('mouseenter')
    await panel.trigger('pointerdown', { button: 0 })
    await panel.trigger('mouseleave')
    await vi.advanceTimersByTimeAsync(500)
    expect(panel.isVisible()).toBe(true)
    const range = document.createRange()
    range.selectNodeContents(panel.element)
    window.getSelection()?.addRange(range)
    document.body.dispatchEvent(new MouseEvent('pointerup', { bubbles: true }))
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await vi.advanceTimersByTimeAsync(500)
    expect(window.getSelection()?.toString()).toBe('Selectable details')
    expect(panel.isVisible()).toBe(true)
    window.getSelection()?.removeAllRanges()
    document.dispatchEvent(new KeyboardEvent('keyup', { key: 'Shift' }))
    await vi.advanceTimersByTimeAsync(250)
    expect(panel.isVisible()).toBe(false)
  })

  it('does not retain an unrelated selection and releases a cancelled drag', async () => {
    vi.useFakeTimers()
    const wrapper = mountDetails()
    const panel = new DOMWrapper(getTooltipElement())
    await wrapper.get('.group').trigger('mouseenter')
    await panel.trigger('pointerdown', { button: 0 })
    await panel.trigger('mouseleave')
    document.dispatchEvent(new Event('pointercancel'))
    await vi.advanceTimersByTimeAsync(250)
    expect(panel.isVisible()).toBe(false)
    const outside = document.createElement('p')
    outside.textContent = 'Outside selection'
    document.body.append(outside)
    const range = document.createRange()
    range.selectNodeContents(outside)
    window.getSelection()?.addRange(range)
    await wrapper.get('.group').trigger('mouseenter')
    await panel.trigger('pointerdown', { button: 0 })
    await panel.trigger('mouseleave')
    document.body.dispatchEvent(new MouseEvent('pointerup', { bubbles: true }))
    await vi.advanceTimersByTimeAsync(250)
    expect(panel.isVisible()).toBe(false)
  })

  it('dismisses a pinned selection on the next deliberate outside gesture', async () => {
    const wrapper = mountDetails()
    const panel = new DOMWrapper(getTooltipElement())
    await wrapper.get('button').trigger('click')
    await panel.trigger('pointerdown', { button: 0 })
    const range = document.createRange()
    range.selectNodeContents(panel.element)
    window.getSelection()?.addRange(range)
    document.body.dispatchEvent(new MouseEvent('pointerup', { bubbles: true }))
    // Some browsers do not emit a click at the end of a text-selection drag.
    document.body.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true, button: 0 }))
    document.body.click()
    await nextTick()
    expect(panel.isVisible()).toBe(false)
  })

  it('keeps keyboard focus inside the panel and closes after focus leaves', async () => {
    vi.useFakeTimers()
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body, props: { trigger: 'both' },
      slots: { trigger: '<button>Details</button>', default: '<button>Copy</button><a href="#details">Link</a>' }
    })
    wrapper.get('button').element.focus()
    await nextTick()
    const panel = new DOMWrapper(getTooltipElement())
    panel.get('button').element.focus()
    await panel.trigger('mouseleave')
    await vi.advanceTimersByTimeAsync(500)
    expect(panel.isVisible()).toBe(true)
    panel.get('a').element.focus()
    await vi.advanceTimersByTimeAsync(500)
    expect(panel.isVisible()).toBe(true)
    panel.get('a').element.blur()
    await nextTick()
    await vi.advanceTimersByTimeAsync(250)
    expect(panel.isVisible()).toBe(false)
  })

  it('supports the default keyboard control, touch activation and Escape', async () => {
    const wrapper = mount(HelpTooltip, { attachTo: document.body, props: { trigger: 'both', content: 'Details' } })
    const button = wrapper.get('button')
    button.element.focus()
    await nextTick()
    expect(button.attributes('aria-expanded')).toBe('true')
    await button.trigger('keyup', { key: 'Enter' })
    button.element.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 0 }))
    await nextTick()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(button.attributes('aria-expanded')).toBe('false')
    await button.trigger('pointerdown', { pointerType: 'touch', button: 0 })
    await button.trigger('pointerup', { pointerType: 'touch', button: 0 })
    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('true')
    document.body.click()
    await nextTick()
    expect(button.attributes('aria-expanded')).toBe('false')
  })

  it('cancels timers and removes its document and viewport listeners on unmount', async () => {
    vi.useFakeTimers()
    const documentAdd = vi.spyOn(document, 'addEventListener')
    const documentRemove = vi.spyOn(document, 'removeEventListener')
    const windowAdd = vi.spyOn(window, 'addEventListener')
    const windowRemove = vi.spyOn(window, 'removeEventListener')
    const wrapper = mountDetails()
    await wrapper.get('.group').trigger('mouseenter')
    await wrapper.get('.group').trigger('mouseleave')
    expect(vi.getTimerCount()).toBeGreaterThan(0)
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
    for (const type of ['pointerdown', 'pointerup', 'pointercancel', 'click', 'keydown', 'keyup']) {
      const added = documentAdd.mock.calls.find(call => call[0] === type)
      expect(added, type).toBeDefined()
      expect(documentRemove.mock.calls).toContainEqual(added)
    }
    for (const type of ['scroll', 'resize']) {
      const added = windowAdd.mock.calls.find(call => call[0] === type)
      expect(added, type).toBeDefined()
      expect(windowRemove.mock.calls).toContainEqual(added)
    }
    await vi.advanceTimersByTimeAsync(500)
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
  })

  it('clamps the panel to the viewport and repositions on scroll and resize', async () => {
    const wrapper = mountDetails()
    const trigger = wrapper.get('.group')
    const panel = getTooltipElement()
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 1, 20, 20))
    vi.spyOn(panel, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 280, 120))
    await trigger.trigger('mouseenter')
    await nextTick()
    expect(panel.style.left).toBe('148px')
    expect(panel.style.top).toBe('29px')
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(window.innerWidth - 20, window.innerHeight - 20, 20, 20))
    window.dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(panel.style.left).toBe((window.innerWidth - 148) + 'px')
    expect(panel.style.top).toBe((window.innerHeight - 148) + 'px')
    window.dispatchEvent(new Event('resize'))
    await nextTick()
    expect(panel.style.left).toBe((window.innerWidth - 148) + 'px')
  })
})
