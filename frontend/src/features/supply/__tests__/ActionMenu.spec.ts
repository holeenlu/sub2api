import { vi } from 'vitest'
import { afterEach, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ActionMenu from '../components/ActionMenu.vue'

afterEach(() => document.body.replaceChildren())

it('keeps a teleported action menu keyboard accessible and restores focus after Escape', async () => {
  const wrapper = mount(ActionMenu, {
    attachTo: document.body,
    props: { label: 'Account actions', compact: true },
    slots: {
      default:
        '<button role="menuitem">Details</button><button role="menuitem" disabled>Pause</button><button role="menuitem">Delete</button>'
    }
  })
  const trigger = wrapper.get('button')
  await trigger.trigger('click')
  await flushPromises()
  const menu = document.querySelector('[role=menu]')!
  const items = menu.querySelectorAll('button')
  expect(menu.parentElement).toBe(document.body)
  expect(document.activeElement).toBe(items[0])
  items[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  expect(document.activeElement).toBe(items[2])
  items[2].dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  await flushPromises()
  expect(document.querySelector('[role=menu]')).toBeNull()
  expect(document.activeElement).toBe(trigger.element)
  expect(trigger.attributes('aria-expanded')).toBe('false')
  wrapper.unmount()
})

vi.mock('@/features/supply/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/supply/services')>('@/features/supply/services')
  const { createSupplyTestServices } = await import('@/features/supply/__tests__/services')
  const services = createSupplyTestServices()
  return { ...actual, useSupplyServices: () => services }
})
