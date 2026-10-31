import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import GroupModelAllowlistPanel from '../../panels/GroupModelAllowlistPanel.vue'
import { hydrateModelAllowlistState } from '../../model/groupModelAllowlist'
import { accessPolicyScopeKey, accessPolicyNotificationsKey } from '../../services'
const query = vi.hoisted(() => vi.fn())
vi.mock('../../api/groups', () => ({ groupsAPI: { getModelAllowlistCandidates: query } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('candidate query identity and saved selections', () => {
 it('drops a stale group result and keeps saved unknown models in the current group', async () => {
  let resolve!: (models: string[]) => void
  query.mockReturnValueOnce(new Promise<string[]>(yes => { resolve = yes })).mockResolvedValueOnce(['current-model'])
  const wrapper = mount(GroupModelAllowlistPanel, { props: { modelValue: hydrateModelAllowlistState({ enabled: true, models: ['saved-unknown'] }, []), platform: 'openai', groupId: 1 }, global: { provide: { [accessPolicyScopeKey as symbol]: ref('scope1'), [accessPolicyNotificationsKey as symbol]: { showError: vi.fn(), showSuccess: vi.fn() } }, stubs: { Toggle: true, Icon: true } } })
  await wrapper.setProps({ groupId: 2 }); await flushPromises()
  resolve(['stale-model']); await flushPromises()
  const updates = wrapper.emitted('update:modelValue') || []
  const state = updates.at(-1)?.[0] as ReturnType<typeof hydrateModelAllowlistState>
  expect(state.items.map(item => item.id)).toContain('current-model')
  expect(state.items.find(item => item.id === 'saved-unknown')?.selected).toBe(true)
  expect(state.items.map(item => item.id)).not.toContain('stale-model')
  wrapper.unmount()
 })
})
