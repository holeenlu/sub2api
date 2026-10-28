import { vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AccountColumnChooser from '../components/AccountColumnChooser.vue'

describe('AccountColumnChooser', () => {
  it('keeps fixed account columns visible while allowing optional columns to toggle', async () => {
    const wrapper = mount(AccountColumnChooser, {
      props: {
        columns: [
          { key: 'name', label: 'Name' },
          { key: 'status', label: 'Status' },
          { key: 'notes', label: 'Notes' }
        ],
        hidden: new Set(['name', 'status', 'notes']),
        alwaysVisible: ['name', 'status'],
        label: 'Columns'
      },
      global: {
        stubs: { teleport: true },
        mocks: { $t: (key: string) => key }
      }
    })

    await wrapper.get('button[aria-label="Columns"]').trigger('click')
    const menuItems = wrapper.findAll('[role="menuitemcheckbox"]')
    const name = menuItems.find((item) => item.text() === 'Name')!
    const notes = menuItems.find((item) => item.text() === 'Notes')!

    expect(name.attributes('aria-checked')).toBe('true')
    expect(name.attributes('aria-disabled')).toBe('true')
    await name.trigger('click')
    expect(wrapper.emitted('toggle')).toBeUndefined()

    await notes.trigger('click')
    expect(wrapper.emitted('toggle')).toEqual([['notes']])
  })
})

vi.mock('@/features/supply/services', async () => {
  const actual = await vi.importActual<typeof import('@/features/supply/services')>('@/features/supply/services')
  const { createSupplyTestServices } = await import('@/features/supply/__tests__/services')
  const services = createSupplyTestServices()
  return { ...actual, useSupplyServices: () => services }
})
