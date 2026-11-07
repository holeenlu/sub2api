import { describe, expect, it } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { pricingScopeKey } from '../ports'
import { usePricingRequest } from '../queries/requestScope'
describe('pricing query scope', () => {
 it('invalidates both cancellation and acceptance tokens synchronously on scope change and disposal', () => {
  const scope = ref('epoch1:admin:1'); let owner!: ReturnType<typeof usePricingRequest>; let clears = 0
  const wrapper = mount(defineComponent({ setup() { owner = usePricingRequest(() => ++clears); return () => null } }), { global: { provide: { [pricingScopeKey as symbol]: scope } } })
  const old = owner.begin(); scope.value = 'epoch2:admin:1'
  expect(old.signal.aborted).toBe(true); expect(old.isCurrent()).toBe(false); expect(clears).toBe(1)
  const current = owner.begin(); expect(current.isCurrent()).toBe(true)
  wrapper.unmount(); expect(current.signal.aborted).toBe(true); expect(current.isCurrent()).toBe(false)
 })
})
