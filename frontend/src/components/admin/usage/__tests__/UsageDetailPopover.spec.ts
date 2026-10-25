import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UsageDetailPopover from '../UsageDetailPopover.vue'

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })
it('uses the existing dialog on mobile and reacts to a desktop resize', async () => {
  vi.stubGlobal('innerWidth', 375)
  const wrapper = mount(UsageDetailPopover, {
    props: { show: true, title: 'Details', anchor: null },
    slots: { default: 'Long usage details' },
    global: { stubs: {
      Teleport: true,
      BaseDialog: { props: ['show'], template: '<section v-if="show" role="dialog"><button @click="$emit(\'close\')">close</button><slot /></section>' },
    } },
  })
  try {
    expect(wrapper.get('[role="dialog"]').text()).toContain('Long usage details')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    vi.stubGlobal('innerWidth', 1024)
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.find('[role="tooltip"]').exists()).toBe(true)
  } finally { wrapper.unmount() }
})

it.each([{ left: 730, top: 8 }, { left: 10, top: 500 }])('positions the measured desktop panel at viewport edges ($left,$top)', async ({ left, top }) => {
  vi.stubGlobal('innerWidth', 768)
  vi.stubGlobal('innerHeight', 600)
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ width: 360, height: 460 } as DOMRect)
  const wrapper = mount(UsageDetailPopover, {
    props: { show: true, title: 'Details', anchor: { left, right: left + 20, top, height: 20 } as DOMRect },
    slots: { default: '<p>Localized, long details</p>' },
    global: { stubs: { Teleport: true } },
  })
  try {
    await flushPromises()
    const panel = wrapper.get('[role="tooltip"]').element as HTMLElement
    const x = parseFloat(panel.style.left), y = parseFloat(panel.style.top)
    expect(x).toBeGreaterThanOrEqual(8)
    expect(x + 360).toBeLessThanOrEqual(760)
    expect(y).toBeGreaterThanOrEqual(8)
    expect(y + 460).toBeLessThanOrEqual(592)
    await wrapper.setProps({ anchor: { left: 400, right: 420, top: 290, height: 20 } as DOMRect })
    await flushPromises()
    expect(parseFloat((wrapper.get('[role="tooltip"]').element as HTMLElement).style.top)).toBe(70)
  } finally { wrapper.unmount() }
})
