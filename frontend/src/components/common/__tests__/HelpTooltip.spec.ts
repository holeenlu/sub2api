import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'

function getTooltipElement(): HTMLDivElement {
  const tooltip = document.body.querySelector('[role="tooltip"]')
  if (!(tooltip instanceof HTMLDivElement)) {
    throw new Error('tooltip element not found')
  }
  return tooltip
}

describe('HelpTooltip', () => {
  afterEach(() => {
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

  it('allows crossing the hover gap and selecting text before dismissing', async () => {
    vi.useFakeTimers()
    const w=mount(HelpTooltip,{attachTo:document.body,props:{trigger:'both',content:'selectable account'}})
    const trigger=w.get('.group'), panel=getTooltipElement()
    await trigger.trigger('mouseenter');await trigger.trigger('mouseleave')
    await vi.advanceTimersByTimeAsync(150)
    expect(panel.style.display).not.toBe('none')
    panel.dispatchEvent(new MouseEvent('mouseenter'))
    await vi.advanceTimersByTimeAsync(500)
    expect(panel.style.display).not.toBe('none')
    panel.dispatchEvent(new MouseEvent('pointerdown',{button:0}))
    panel.dispatchEvent(new MouseEvent('mouseleave'))
    await vi.advanceTimersByTimeAsync(500)
    expect(panel.style.display).not.toBe('none')
    document.body.dispatchEvent(new MouseEvent('pointerup',{bubbles:true}))
    await vi.advanceTimersByTimeAsync(300)
    expect(panel.style.display).toBe('none')
    w.unmount()
  })
  it('pins details on click until explicitly dismissed, without hover closing them', async () => {
    vi.useFakeTimers()
    const w=mount(HelpTooltip,{attachTo:document.body,props:{trigger:'both',content:'pinned'}})
    const trigger=w.get('.group'),panel=getTooltipElement()
    await trigger.trigger('click');await trigger.trigger('mouseleave')
    panel.dispatchEvent(new MouseEvent('mouseleave'))
    await vi.advanceTimersByTimeAsync(500)
    expect(panel.style.display).not.toBe('none')
    await trigger.trigger('click');expect(panel.style.display).toBe('none')
    w.unmount()
  })

})
