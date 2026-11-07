import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { h, nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'
import Select from '../Select.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = '' })

const key = (key: string, shiftKey = false): KeyboardEvent => {
  const event = new KeyboardEvent('keydown', { key, shiftKey, bubbles: true, cancelable: true })
  ;(document.activeElement ?? document).dispatchEvent(event)
  return event
}
const open = (content = '', show = true) => mount(BaseDialog, {
  attachTo: document.body, props: { show, title: 'Editor', showCloseButton: false },
  slots: { default: content }, global: { stubs: { Icon: true } }
})
const element = (id: string): HTMLElement => document.getElementById(id)!

describe('dialog keyboard focus ownership', () => {
  it('focuses enabled controls and wraps Tab in both directions', async () => {
    open('<button disabled>Unavailable</button><input type="hidden"><button id="first">First</button><div hidden><button>Hidden</button></div><button id="last">Last</button>')
    await nextTick()
    expect(document.activeElement).toBe(element('first'))
    expect(key('Tab', true).defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(element('last'))
    expect(key('Tab').defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(element('first'))
    expect(key('Tab').defaultPrevented).toBe(false)
  })

  it('uses the panel as a focusable fallback while a form is inert', async () => {
    open('<form inert><button>Saving</button></form>')
    await nextTick()
    const panel = document.querySelector('.modal-content')
    expect(document.activeElement).toBe(panel)
    expect(key('Tab').defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(panel)
  })

  it('treats the selected native radio as its group Tab boundary', async () => {
    open('<input type="radio" name="mode" id="first-mode"><input type="radio" name="mode" id="chosen-mode" checked><button id="save">Save</button>')
    await nextTick()
    expect(document.activeElement).toBe(element('chosen-mode'))
    expect(key('Tab', true).defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(element('save'))
    expect(key('Tab').defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(element('chosen-mode'))
  })

  it('restores the caller on close and on component unmount', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()
    const wrapper = open('<button id="inner">Inside</button>')
    await nextTick()
    expect(document.activeElement).toBe(element('inner'))
    await wrapper.setProps({ show: false })
    expect(document.activeElement).toBe(trigger)
    await wrapper.setProps({ show: true })
    await nextTick()
    wrapper.unmount()
    expect(document.activeElement).toBe(trigger)
  })

  it('keeps nested dialogs isolated and returns focus to the parent', async () => {
    open('<button id="parent">Parent</button>')
    await nextTick()
    const child = open('<button id="child">Child</button>')
    await nextTick()
    expect(document.activeElement).toBe(element('child'))
    key('Tab')
    expect(document.activeElement).toBe(element('child'))
    child.unmount()
    expect(document.activeElement).toBe(element('parent'))
  })

  it('does not steal focus back when a background dialog closes', async () => {
    const parent = open('<button id="parent">Parent</button>')
    await nextTick()
    open('<button id="child">Child</button>')
    await nextTick()
    await parent.setProps({ show: false })
    expect(document.activeElement).toBe(element('child'))
  })

  it('returns to the parent panel if its original trigger becomes disabled', async () => {
    open('<button id="parent">Parent</button>')
    await nextTick()
    const child = open('<button id="child">Child</button>')
    await nextTick()
    const trigger = element('parent')
    trigger.setAttribute('disabled', '')
    child.unmount()
    expect(document.activeElement).toBe(trigger.closest('.modal-content'))
  })

  it('does not close when a nested control consumes Escape', async () => {
    const wrapper = open('<button id="inner">Dropdown</button>')
    await nextTick()
    element('inner').addEventListener('keydown', event => event.preventDefault())
    key('Escape')
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('resumes the dialog tab order from a teleported Select trigger', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body, props: { show: true, title: 'Editor', showCloseButton: false },
      slots: { default: () => [h(Select, { options: [{ value: 'one', label: 'One' }], modelValue: '', ariaLabel: 'Choose' }), h('button', { id: 'next' }, 'Next')] },
      global: { stubs: { Icon: true } }
    })
    await nextTick()
    const trigger = wrapper.getComponent(Select).get('button')
    await trigger.trigger('click')
    await nextTick()
    expect(document.activeElement?.getAttribute('role')).toBe('listbox')
    // jsdom does not perform native tab traversal, but the event must continue
    // from the trigger rather than be trapped back to an unrelated first item.
    expect(key('Tab').defaultPrevented).toBe(false)
    await nextTick()
    expect(document.activeElement).toBe(trigger.element)
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('does not focus a disposed dialog after its initial DOM tick', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()
    const wrapper = open('<button>Inside</button>')
    wrapper.unmount()
    await nextTick()
    expect(document.activeElement).toBe(trigger)
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })
})
