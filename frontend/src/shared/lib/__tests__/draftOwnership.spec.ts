import { describe, expect, it } from 'vitest'
import { defineComponent, reactive, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { cloneDraftValue, useDraftSection } from '../useDraftSection'
describe('owned form drafts', () => {
 it('drops an absent optional field when the parent replaces the section baseline', () => {
  const input = ref<{ name: string; limit?: number }>({ name: 'A', limit: 7 })
  const updates: unknown[] = []
  let local!: { name: string; limit?: number }
  const wrapper = mount(defineComponent({ setup() {
   local = useDraftSection(() => input.value, patch => updates.push(patch))
   return () => null
  } }))
  input.value = { name: 'B' }
  expect(local).toEqual({ name: 'B' })
  expect(updates).toEqual([])
  wrapper.unmount()
  input.value = { name: 'C', limit: 9 }
  expect(local).toEqual({ name: 'B' })
 })
 it('clones nested reactive arrays and preserves null and zero', () => {
  const input = reactive({ limit: null, rpm: 0, rules: [{ accounts: [1] }] })
  const copy = cloneDraftValue({ ...input }); copy.rules[0].accounts.push(2)
  expect(input.rules[0].accounts).toEqual([1]); expect(copy.limit).toBeNull(); expect(copy.rpm).toBe(0)
 })
 it('publishes only changed fields, never mutates props, and accepts a refreshed readonly baseline', async () => {
  const input = reactive({ name: 'A', rules: [{ accounts: [1] }], rpm: 0 })
  const updates: Array<Partial<typeof input>> = []; let local!: typeof input
  const wrapper = mount(defineComponent({ setup() { local = useDraftSection(() => input, patch => updates.push(patch)); return () => null } }))
  local.name = 'B'; expect(input.name).toBe('A'); expect(updates).toEqual([{ name: 'B' }])
  input.rpm = 50; expect(local.rpm).toBe(50); expect(updates).toHaveLength(1)
  local.rules[0].accounts.push(2); expect(input.rules[0].accounts).toEqual([1]); expect(updates.at(-1)).toEqual({ rules: [{ accounts: [1, 2] }] })
  wrapper.unmount()
 })
})
